//go:build darwin

package detect

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/aka-rider/leash/sandbox"
)

// Rust adds the Rust toolchain paths (cargo/rustc bin dir, ~/.cargo,
// ~/.rustup) to p if cargo is found in PATH, and — when the process cwd
// contains a Cargo.toml — grants exec on the crate's build-output dir
// (target/, or $CARGO_TARGET_DIR) so compiled test binaries, build-script
// binaries, and proc-macro binaries can run.
func Rust(p sandbox.ToolProfile) (sandbox.ToolProfile, error) {
	cargoPath, err := exec.LookPath("cargo")
	if errors.Is(err, exec.ErrNotFound) {
		return p, nil
	}
	if err != nil {
		return p, fmt.Errorf("detect cargo binary: %w", err)
	}

	// Resolve symlinks: Homebrew cargo is a symlink into Cellar.
	if r, rerr := filepath.EvalSymlinks(cargoPath); rerr == nil {
		cargoPath = r
	}
	if err := p.Allow(filepath.Dir(cargoPath), sandbox.Exec); err != nil {
		return p, fmt.Errorf("detect cargo bin dir: %w", err)
	}

	// ~/.cargo holds CARGO_HOME: registry cache, downloaded crate sources,
	// and (for standalone/rustup installs) the cargo/rustc shims.
	if _, err := p.AllowOptional("~/.cargo", sandbox.Write); err != nil {
		return p, fmt.Errorf("detect rust cargo home: %w", err)
	}
	// ~/.rustup holds the active toolchain's rustc/std binaries when the
	// toolchain was installed via rustup rather than Homebrew.
	if _, err := p.AllowOptional("~/.rustup", sandbox.Exec); err != nil {
		return p, fmt.Errorf("detect rust rustup home: %w", err)
	}

	// The build-output dir is only relevant when the process cwd is inside
	// a crate. The detector-registration loop (execute_darwin.go) calls
	// every detector with the fixed sandbox.ToolProfile signature — no cwd
	// argument is threaded through — so os.Getwd() is the only way to
	// introspect cwd here. This equals the child's cwd for the primary
	// plain-invocation case (l.Dir == ""); it does not for --worktree runs
	// (documented limitation).
	cwd, err := os.Getwd()
	if err != nil {
		return p, fmt.Errorf("detect rust getwd: %w", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "Cargo.toml")); err != nil {
		// Not in a crate — no target grant.
		return p, nil
	}

	target := os.Getenv("CARGO_TARGET_DIR")
	switch {
	case target == "":
		target = filepath.Join(cwd, "target")
	case !filepath.IsAbs(target):
		target = filepath.Join(cwd, target)
	}
	if err := p.AllowFutureDir(target, sandbox.Exec); err != nil {
		return p, fmt.Errorf("detect rust target dir: %w", err)
	}

	return p, nil
}
