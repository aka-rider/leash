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
	if raw == "" {
		return Path{}, fmt.Errorf("resolve path: empty path")
	}
	if home == "" {
		return Path{}, fmt.Errorf("resolve path: home is required for expansion")
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
			return Path{}, fmt.Errorf("resolve path: abs %q: %w", raw, absErr)
		}
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
