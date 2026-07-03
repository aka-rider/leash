//go:build darwin

package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Path is a validated, absolute, resolved filesystem path.
type Path struct {
	Resolved string // absolute, symlinks evaluated
	IsDir    bool   // true → SBPL subpath; false → SBPL literal
}

// ResolvePath expands ~ to home, resolves to absolute, evaluates symlinks,
// and stats to determine file vs directory. Returns error if the path
// doesn't exist or can't be resolved.
func ResolvePath(raw string, home string) (Path, error) {
	expanded, err := expandAbs(raw, home)
	if err != nil {
		return Path{}, err
	}

	// Evaluate symlinks — path must exist
	resolved, err := filepath.EvalSymlinks(expanded)
	if err != nil {
		return Path{}, fmt.Errorf("resolve path %q: %w", raw, err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return Path{}, fmt.Errorf("resolve path %q: %w", raw, err)
	}

	return Path{Resolved: resolved, IsDir: info.IsDir()}, nil
}

// ResolveFuturePath resolves a path that is allowed to not exist yet (it
// may be created inside the sandbox — lock files, tempfiles). Behavior:
// if raw exists, identical to ResolvePath. Otherwise the PARENT directory
// must exist: it is symlink-resolved and the base name is re-joined, so
// the emitted SBPL literal matches the canonical path the kernel checks
// when the child later creates the file. Returns IsDir=false for the
// not-yet-existing case.
func ResolveFuturePath(raw string, home string) (Path, error) {
	expanded, err := expandAbs(raw, home)
	if err != nil {
		return Path{}, err
	}

	// Fast path: raw already exists — behave exactly like ResolvePath.
	if resolved, err := filepath.EvalSymlinks(expanded); err == nil {
		info, err := os.Stat(resolved)
		if err != nil {
			return Path{}, fmt.Errorf("resolve future path %q: %w", raw, err)
		}
		return Path{Resolved: resolved, IsDir: info.IsDir()}, nil
	}

	// Not-yet-existing: the parent must exist. Resolve it and rejoin the
	// base name, so the emitted literal matches the canonical path the
	// kernel will check when the child creates the file (e.g. if a parent
	// component is a symlink such as /tmp -> /private/tmp).
	parent, err := filepath.EvalSymlinks(filepath.Dir(expanded))
	if err != nil {
		return Path{}, fmt.Errorf("resolve future path %q: parent: %w", raw, err)
	}
	if _, err := os.Stat(parent); err != nil {
		return Path{}, fmt.Errorf("resolve future path %q: parent: %w", raw, err)
	}

	return Path{Resolved: filepath.Join(parent, filepath.Base(expanded)), IsDir: false}, nil
}

// expandAbs expands a leading ~ to home and makes the result absolute,
// joining relative paths to cwd. It performs no filesystem access.
func expandAbs(raw, home string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("resolve path: empty path")
	}
	if home == "" {
		return "", fmt.Errorf("resolve path: home is required for expansion")
	}

	// Expand ~
	expanded := raw
	if strings.HasPrefix(expanded, "~/") {
		expanded = filepath.Join(home, expanded[2:])
	} else if expanded == "~" {
		expanded = home
	}

	// Make absolute — relative paths are joined to cwd
	if !filepath.IsAbs(expanded) {
		var absErr error
		expanded, absErr = filepath.Abs(expanded)
		if absErr != nil {
			return "", fmt.Errorf("resolve path: abs %q: %w", raw, absErr)
		}
	}

	return expanded, nil
}
