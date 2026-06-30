//go:build darwin

package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// createWorktree creates a detached git worktree at <repo-parent>/<name>.
// If name is empty, one is generated as <repo-name>-<8 random hex chars>.
// Returns the absolute path on success. Any error should be treated as fatal by the caller.
func createWorktree(name string) (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository")
	}
	repoRoot := strings.TrimSpace(string(out))

	if name == "" {
		suffix, err := randomHex(4)
		if err != nil {
			return "", fmt.Errorf("generate worktree name: %w", err)
		}
		name = filepath.Base(repoRoot) + "-" + suffix
	}

	path := filepath.Join(filepath.Dir(repoRoot), name)

	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("worktree path already exists: %s", path)
	}

	cmd := exec.Command("git", "worktree", "add", "--detach", path)
	cmd.Dir = repoRoot
	if combined, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %s", strings.TrimSpace(string(combined)))
	}

	return path, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}
