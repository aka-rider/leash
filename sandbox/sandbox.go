//go:build darwin

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"unsafe"
)

// Config configures a seatbelt sandbox instance.
type Config struct {
	Profiles []Snapshot        // tool snapshots; may contain allow and deny entries
	ProxyEnv []string          // env var NAMES to forward from host — MUST exist or error
	ExtraEnv map[string]string // explicit key=value pairs
	// DenyMessage is the message tag embedded in the SBPL deny rule. Used by --trace to
	// match this run's denials in the kernel log. Defaults to "leash" when empty.
	DenyMessage string
	// NoNetwork omits the network-outbound allow rules when true.
	NoNetwork bool
}

// Sandbox wraps sandbox-exec execution with an SBPL profile.
type Sandbox struct {
	sbplPath        string
	sandboxExecPath string
	env             []string
}

// New creates a Sandbox after validating configuration and compiling the SBPL profile.
func New(cfg Config) (*Sandbox, error) {
	// Verify sandbox-exec is available
	sandboxExecPath, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return nil, fmt.Errorf("seatbelt: sandbox-exec not found in PATH: %w", err)
	}

	home := os.Getenv("HOME")
	if home == "" {
		return nil, fmt.Errorf("seatbelt: HOME environment variable is not set")
	}

	// Resolve tmpdir
	realTmpDir := os.TempDir()
	if resolved, err := filepath.EvalSymlinks(realTmpDir); err == nil {
		realTmpDir = resolved
	}

	// Build SBPL profile
	builder, err := NewProfileBuilder(home, realTmpDir)
	if err != nil {
		return nil, fmt.Errorf("seatbelt: create profile builder: %w", err)
	}
	builder.DenyMessage = cfg.DenyMessage
	builder.NoNetwork = cfg.NoNetwork
	for _, snap := range cfg.Profiles {
		builder.Add(snap)
	}

	sbpl, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("seatbelt: build profile: %w", err)
	}

	// Write SBPL to temp file (chmod 0400)
	tmpFile, err := os.CreateTemp("", "leash-sbx-*.sb")
	if err != nil {
		return nil, fmt.Errorf("seatbelt: create profile tempfile: %w", err)
	}
	sbplPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(sbpl); err != nil {
		tmpFile.Close()
		os.Remove(sbplPath)
		return nil, fmt.Errorf("seatbelt: write profile: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(sbplPath)
		return nil, fmt.Errorf("seatbelt: close profile: %w", err)
	}
	if err := os.Chmod(sbplPath, 0400); err != nil {
		os.Remove(sbplPath)
		return nil, fmt.Errorf("seatbelt: chmod profile: %w", err)
	}

	// Build scrubbed environment
	base := BaseEnv(home, realTmpDir)
	extraPath := ExtraPathDirs(cfg.Profiles)
	env, err := MergeEnv(base, cfg.Profiles, cfg.ProxyEnv, cfg.ExtraEnv, extraPath)
	if err != nil {
		os.Remove(sbplPath)
		return nil, fmt.Errorf("seatbelt: build environment: %w", err)
	}

	return &Sandbox{
		sbplPath:        sbplPath,
		sandboxExecPath: sandboxExecPath,
		env:             env,
	}, nil
}

// Close removes the temporary SBPL profile file.
func (s *Sandbox) Close() error {
	if s.sbplPath != "" {
		return os.Remove(s.sbplPath)
	}
	return nil
}

// Wrap mutates an *exec.Cmd so it will run inside sandbox-exec with the scrubbed env.
// It does NOT run the command and therefore does NOT own timeout cleanup.
// The child process inherits the working directory from the parent process.
func (s *Sandbox) Wrap(cmd *exec.Cmd) error {
	if cmd.Path == "" || len(cmd.Args) == 0 {
		return fmt.Errorf("sandbox: cannot wrap empty command")
	}

	// 1. Apply absolute environment boundary
	cmd.Env = s.env

	// 2. Apply Process Group Isolation (Anti-Zombie)
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true

	// 2b. When stdin is the controlling terminal, put the child's process group
	// in the foreground so its first tty read doesn't earn it a SIGTTIN (which
	// stops the process — interactive commands like `claude` or `cat` would
	// otherwise hang forever). Non-tty stdin (pipes, /dev/null, CI, tests)
	// keeps the plain Setpgid-only behavior above.
	if f, ok := cmd.Stdin.(*os.File); ok && isTerminal(f) {
		cmd.SysProcAttr.Foreground = true
		cmd.SysProcAttr.Ctty = int(f.Fd())
	}

	// 3. Wrap in sandbox-exec directly. Earlier versions routed this through
	// `/bin/sh -c "ulimit -n 4096; exec sandbox-exec ..."` to raise the
	// child's fd limit (Go's os/exec unconditionally resets a child's
	// RLIMIT_NOFILE to the pre-Go value, so it can't be done from the Go
	// parent alone — see go.dev/issue/46279). That extra /bin/sh hop turned
	// out to wedge process exit: when the sandboxed child was terminated by
	// a signal (e.g. Ctrl+C) while /bin/sh had been anywhere in its exec
	// chain, the process never got reaped — confirmed by isolating each
	// factor (Foreground/Ctty, Setpgid, and even no SysProcAttr at all still
	// hung; swapping /bin/sh for /usr/bin/env or exec'ing sandbox-exec
	// directly always exited promptly). exec'ing sandbox-exec directly
	// avoids the bug entirely; the tradeoff is the child keeps whatever
	// RLIMIT_NOFILE its environment already had (typically already generous
	// on modern macOS) instead of a forced floor of 4096.
	originalBin := cmd.Path
	originalArgs := cmd.Args[1:]

	newArgs := []string{s.sandboxExecPath, "-f", s.sbplPath, originalBin}
	newArgs = append(newArgs, originalArgs...)

	cmd.Path = s.sandboxExecPath
	cmd.Args = newArgs

	return nil
}

// Run wraps, starts, and waits for cmd inside the sandbox.
// Canceling ctx sends SIGKILL to the child process group and returns ctx.Err().
func (s *Sandbox) Run(ctx context.Context, cmd *exec.Cmd) error {
	if err := s.Wrap(cmd); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("sandbox: start: %w", err)
	}

	pgid := -cmd.Process.Pid // Setpgid guarantees pgid == pid; Process is non-nil after Start
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(pgid, syscall.SIGKILL)
		case <-done:
		}
	}()

	err := cmd.Wait()
	close(done)

	// Reclaim the terminal's foreground process group for ourselves if Wrap
	// handed it to the child (see the Foreground/Ctty comment above): once
	// the child is gone its process group is a dangling reference, and
	// whoever is waiting on us (a shell, typically) expects control back.
	// SIGTTOU must be ignored around the ioctl: we ARE a background process
	// group here (we gave the foreground away), and TIOCSPGRP from a
	// non-orphaned background group — the normal case when leash is run from
	// an interactive shell — otherwise raises SIGTTOU, which stops this very
	// process. Shells do the same dance around tcsetpgrp(3). Best-effort:
	// e.g. EIO for an orphaned process group (leash as session leader with
	// no job-control parent) is a normal, harmless outcome, not a bug.
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Foreground {
		signal.Ignore(syscall.SIGTTOU)
		pgrp := int32(syscall.Getpgrp())
		_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, uintptr(cmd.SysProcAttr.Ctty), uintptr(syscall.TIOCSPGRP), uintptr(unsafe.Pointer(&pgrp)))
		signal.Reset(syscall.SIGTTOU)
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// isTerminal reports whether f refers to an actual terminal device, as
// opposed to merely any character-special device — /dev/null, /dev/zero,
// etc. are char devices too but must NOT be treated as a controlling tty.
// Implemented with a raw TIOCGETA ioctl (stdlib syscall only, no
// golang.org/x/term or x/sys/unix dependency): the ioctl succeeds only when
// fd refers to a tty.
func isTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGETA, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}
