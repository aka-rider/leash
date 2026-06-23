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
	RepoPath     string            // absolute path to the repository root, mandatory
	SessionPath  string            // absolute path to the session directory (optional)
	WorktreePath string            // absolute path to the git worktree (optional)
	RepoWritable bool              // if false, repo root is read-only
	Profiles     []Snapshot        // tool snapshots from detect package
	HarnessEnv   []string          // exact key=value env (e.g. ANTHROPIC_BASE_URL=...)
	ProxyEnv     []string          // env var NAMES to forward from host — MUST exist or error
	ExtraEnv     map[string]string // explicit key=value pairs
	// DenyMessage is the message tag embedded in the SBPL deny rule. Used by --trace to
	// match this run's denials in the kernel log. Defaults to "leash" when empty.
	DenyMessage string
	// NoNetwork omits the network-outbound allow rules when true.
	NoNetwork bool
}

// Sandbox wraps sandbox-exec execution with an SBPL profile.
type Sandbox struct {
	sbplPath     string
	env          []string
	repoPath     string
	sessionPath  string
	worktreePath string
}

// New creates a Sandbox after validating configuration and compiling the SBPL profile.
func New(cfg Config) (*Sandbox, error) {
	if cfg.RepoPath == "" {
		return nil, fmt.Errorf("seatbelt: repo path is required")
	}

	absRepo, err := filepath.Abs(cfg.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("seatbelt: resolve repo path: %w", err)
	}
	absRepo, err = filepath.EvalSymlinks(absRepo)
	if err != nil {
		return nil, fmt.Errorf("seatbelt: resolve repo symlinks: %w", err)
	}

	info, err := os.Stat(absRepo)
	if err != nil {
		return nil, fmt.Errorf("seatbelt: repo %q: %w", absRepo, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("seatbelt: repo %q is not a directory", absRepo)
	}

	// Resolve session path if provided.
	var absSession string
	if cfg.SessionPath != "" {
		absSession, err = filepath.Abs(cfg.SessionPath)
		if err != nil {
			return nil, fmt.Errorf("seatbelt: resolve session path: %w", err)
		}
		absSession, err = filepath.EvalSymlinks(absSession)
		if err != nil {
			return nil, fmt.Errorf("seatbelt: resolve session symlinks: %w", err)
		}
		sInfo, sErr := os.Stat(absSession)
		if sErr != nil {
			return nil, fmt.Errorf("seatbelt: session %q: %w", absSession, sErr)
		}
		if !sInfo.IsDir() {
			return nil, fmt.Errorf("seatbelt: session %q is not a directory", absSession)
		}
	}

	// Resolve worktree path if provided.
	var absWorktree string
	if cfg.WorktreePath != "" {
		absWorktree, err = filepath.Abs(cfg.WorktreePath)
		if err != nil {
			return nil, fmt.Errorf("seatbelt: resolve worktree path: %w", err)
		}
		absWorktree, err = filepath.EvalSymlinks(absWorktree)
		if err != nil {
			return nil, fmt.Errorf("seatbelt: resolve worktree symlinks: %w", err)
		}
		wtInfo, wtErr := os.Stat(absWorktree)
		if wtErr != nil {
			return nil, fmt.Errorf("seatbelt: worktree %q: %w", absWorktree, wtErr)
		}
		if !wtInfo.IsDir() {
			return nil, fmt.Errorf("seatbelt: worktree %q is not a directory", absWorktree)
		}
	}

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

	repoRoot := Path{Resolved: absRepo, IsDir: true}

	// Build SBPL profile
	builder, err := NewProfileBuilder(repoRoot, home, realTmpDir)
	if err != nil {
		return nil, fmt.Errorf("seatbelt: create profile builder: %w", err)
	}
	builder.RepoWritable = cfg.RepoWritable
	builder.DenyMessage = cfg.DenyMessage
	builder.NoNetwork = cfg.NoNetwork
	if absSession != "" {
		builder.SessionPath = &Path{Resolved: absSession, IsDir: true}
	}
	if absWorktree != "" {
		builder.WorktreePath = &Path{Resolved: absWorktree, IsDir: true}
	}
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
	base := BaseEnv(home, realTmpDir, absRepo)
	extraPath := ExtraPathDirs(cfg.Profiles)
	env, err := MergeEnv(base, cfg.Profiles, cfg.HarnessEnv, cfg.ProxyEnv, cfg.ExtraEnv, extraPath)
	if err != nil {
		os.Remove(sbplPath)
		return nil, fmt.Errorf("seatbelt: build environment: %w", err)
	}

	return &Sandbox{
		sbplPath:     sbplPath,
		env:          env,
		repoPath:     absRepo,
		sessionPath:  absSession,
		worktreePath: absWorktree,
	}, nil
}

// Close removes the temporary SBPL profile file.
func (s *Sandbox) Close() error {
	if s.sbplPath != "" {
		return os.Remove(s.sbplPath)
	}
	return nil
}

// Workspace returns the configured repo path (primary workspace).
func (s *Sandbox) Workspace() string {
	return s.repoPath
}

// Wrap mutates an *exec.Cmd so it will run inside sandbox-exec with the scrubbed env.
// It does NOT run the command and therefore does NOT own timeout cleanup.
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

	// Set working directory: worktree takes precedence over repo root
	if cmd.Dir == "" {
		if s.worktreePath != "" {
			cmd.Dir = s.worktreePath
		} else {
			cmd.Dir = s.repoPath
		}
	}

	return nil
}

// Run owns the secure lifecycle: wrap, start, wait, and kill the process group on cancel.
func (s *Sandbox) Run(ctx context.Context, cmd *exec.Cmd) error {
	if err := s.Wrap(cmd); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("sandbox: start: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // fire-and-forget: best-effort cleanup after caller cancellation
		}
		err := <-done
		if err != nil {
			return fmt.Errorf("sandbox: canceled: %w", ctx.Err())
		}
		return ctx.Err()
	}
}
