//go:build darwin

package detect

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/aka-rider/leash/sandbox"
)

// Python adds Python toolchain paths to p if python3 or python is found in PATH.
func Python(p sandbox.ToolProfile) (sandbox.ToolProfile, error) {
	var pyPath string
	for _, bin := range []string{"python3", "python"} {
		if path, err := exec.LookPath(bin); err == nil {
			pyPath = path
			break
		}
	}
	if pyPath == "" {
		return p, nil
	}

	// Resolve symlinks: Homebrew python3 is a symlink into the Cellar.
	// Track whether resolution changed the path — only Homebrew-style installs
	// (symlinked binaries) need the Cellar package root grant below.
	resolved := pyPath
	if r, rerr := filepath.EvalSymlinks(pyPath); rerr == nil {
		resolved = r
	}
	if err := p.Allow(filepath.Dir(resolved), sandbox.Exec); err != nil {
		return p, fmt.Errorf("detect python bin dir: %w", err)
	}
	// Homebrew Python's Cellar package root (parent of bin/) contains both the
	// shared library (dylib loading) and Python.app (posix_spawn target).
	// Only needed when the binary was a symlink — system Python at /usr/bin
	// doesn't require this and granting /usr wholesale would be too broad.
	if resolved != pyPath {
		if err := p.Allow(filepath.Dir(filepath.Dir(resolved)), sandbox.Exec); err != nil {
			return p, fmt.Errorf("detect python cellar root: %w", err)
		}
	}
	pyPath = resolved
	for _, path := range []string{
		"~/.local/lib/python3",
		"~/.local/lib",
		"~/.pyenv",
	} {
		if _, err := p.AllowOptional(path, sandbox.Exec); err != nil {
			return p, fmt.Errorf("detect python path %q: %w", path, err)
		}
	}
	if _, err := p.AllowOptional("~/.local/bin", sandbox.Exec); err != nil {
		return p, fmt.Errorf("detect python local bin: %w", err)
	}
	if _, err := p.AllowOptional("~/.cache/pip", sandbox.Write); err != nil {
		return p, fmt.Errorf("detect python pip cache: %w", err)
	}
	return p, nil
}
