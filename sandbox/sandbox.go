//go:build darwin

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Config configures a seatbelt sandbox instance.
type Config struct {
	Profiles    []Snapshot        // tool snapshots; may contain allow and deny entries
	ProxyEnv    []string          // env var NAMES to forward from host — MUST exist or error
	ExtraEnv    map[string]string // explicit key=value pairs
	// DenyMessage is the message tag embedded in the SBPL deny rule. Used by --trace to
	// match this run's denials in the kernel log. Defaults to "leash" when empty.
	DenyMessage string
	// NoNetwork omits the network-outbound allow rules when true.
	NoNetwork bool
}

// Sandbox wraps sandbox-exec execution with an SBPL profile.
type Sandbox struct {
	sbplPath string
	env      []string
}

// New creates a Sandbox after validating configuration and compiling the SBPL profile.
func New(cfg Config) (*Sandbox, error) {
	// Verify sandbox-exec is available
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
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
		sbplPath: sbplPath,
		env:      env,
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

	// 3. Apply Resource Limits via trampoline & sandbox-exec wrapper.
	originalBin := cmd.Path
	originalArgs := cmd.Args[1:]

	trampolineScript := `ulimit -n 4096 2>/dev/null; SB="$1"; BIN="$2"; shift 2; exec sandbox-exec -f "$SB" "$BIN" "$@"`

	newArgs := []string{"sh", "-c", trampolineScript, "sh", s.sbplPath, originalBin}
	newArgs = append(newArgs, originalArgs...)

	cmd.Path = "/bin/sh"
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
	go func() {
		<-ctx.Done()
		_ = syscall.Kill(pgid, syscall.SIGKILL)
	}()

	err := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
