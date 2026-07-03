//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// createWorktree creates a detached git worktree at <repo-parent>/<name>.
// name must be non-empty — the CLI parser (internal/cli.Parse) makes
// --worktree's NAME argument mandatory, so callers are responsible for
// never invoking this with an empty name. name must also be a single path
// component (no separators, not "." or "..") — otherwise it could escape
// the repo's parent directory via filepath.Join's path cleaning.
// Returns the absolute worktree path and the repo's root directory (as
// reported by `git rev-parse --show-toplevel`) on success. Any error should
// be treated as fatal by the caller.
func createWorktree(name string) (string, string, error) {
	if err := validateWorktreeName(name); err != nil {
		return "", "", err
	}

	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		stderr := ""
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		return "", "", fmt.Errorf("not a git repository? git rev-parse --show-toplevel: %w (%s)", err, stderr)
	}
	repoRoot := strings.TrimSpace(string(out))

	path := filepath.Join(filepath.Dir(repoRoot), name)

	if _, err := os.Stat(path); err == nil {
		return "", "", fmt.Errorf("worktree path already exists: %s", path)
	}

	cmd := exec.Command("git", "worktree", "add", "--detach", path)
	cmd.Dir = repoRoot
	if combined, err := cmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("git worktree add: %s", strings.TrimSpace(string(combined)))
	}

	return path, repoRoot, nil
}

// validateWorktreeName rejects names that are not a single path component:
// empty, containing a path separator (either os.PathSeparator or a literal
// '/', which matters when running on a platform whose separator isn't '/'),
// or the special "." / ".." entries. Without this check, --worktree ../../foo
// would escape the repo's parent directory once filepath.Join cleans it.
func validateWorktreeName(name string) error {
	if name == "" {
		return fmt.Errorf("worktree name must not be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("worktree name %q is not a valid directory name", name)
	}
	if strings.ContainsRune(name, os.PathSeparator) || strings.ContainsRune(name, '/') {
		return fmt.Errorf("worktree name %q must be a single path component (no %q)", name, string(os.PathSeparator))
	}
	return nil
}
