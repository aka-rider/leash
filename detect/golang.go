//go:build darwin

package detect

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aka-rider/leash/sandbox"
)

// Go adds the Go toolchain paths to p if go is found in PATH.
func Go(p sandbox.ToolProfile) (sandbox.ToolProfile, error) {
	goPath, err := exec.LookPath("go")
	if errors.Is(err, exec.ErrNotFound) {
		return p, nil
	}
	if err != nil {
		return p, fmt.Errorf("detect go binary: %w", err)
	}

	// Resolve symlinks: Homebrew go is a symlink into Cellar.
	if r, rerr := filepath.EvalSymlinks(goPath); rerr == nil {
		goPath = r
	}
	if err := p.Allow(filepath.Dir(goPath), sandbox.Exec); err != nil {
		return p, fmt.Errorf("detect go bin dir: %w", err)
	}

	// go run compiles to a subdirectory of $GOTMPDIR (os.TempDir()) then execs
	// from there. The base SBPL grants read+write for that path but not
	// process-exec. The grant is intentionally broad (the whole tmpdir subtree)
	// because the exact build subdir is not knowable ahead of time.
	if err := p.Allow(os.TempDir(), sandbox.Exec); err != nil {
		return p, fmt.Errorf("detect go tmpdir exec: %w", err)
	}

	// Fetch GOROOT, GOPATH, GOMODCACHE in a single subprocess invocation.
	envOut, err := probeOutput(exec.Command(goPath, "env", "GOROOT", "GOPATH", "GOMODCACHE"))
	if err == nil {
		lines := strings.SplitN(strings.TrimRight(string(envOut), "\n"), "\n", 3)
		if len(lines) == 3 {
			goRoot, goPathDir, goModCache := lines[0], lines[1], lines[2]
			if goRoot != "" {
				if _, err := p.AllowOptional(goRoot, sandbox.Exec); err != nil {
					return p, fmt.Errorf("detect go GOROOT: %w", err)
				}
			}
			if goPathDir != "" {
				if _, err := p.AllowOptional(goPathDir, sandbox.Write); err != nil {
					return p, fmt.Errorf("detect go GOPATH: %w", err)
				}
				if _, err := p.AllowOptional(filepath.Join(goPathDir, "bin"), sandbox.Exec); err != nil {
					return p, fmt.Errorf("detect go GOPATH/bin: %w", err)
				}
			}
			if goModCache != "" {
				if _, err := p.AllowOptional(goModCache, sandbox.Write); err != nil {
					return p, fmt.Errorf("detect go GOMODCACHE: %w", err)
				}
			}
		}
	}
	return p, nil
}
