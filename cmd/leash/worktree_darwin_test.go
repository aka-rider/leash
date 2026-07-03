//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepo creates a throwaway git repo with one empty commit under a
// fresh temp dir and returns its (symlink-resolved) root path. Identity is
// passed inline via -c so the test doesn't depend on ambient git config.
func initTestRepo(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(t.TempDir()): %v", err)
	}
	repoDir := filepath.Join(root, "repo")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("-c", "user.name=leash-test", "-c", "user.email=leash-test@example.com",
		"commit", "--allow-empty", "-q", "-m", "init")
	return repoDir
}

func TestCreateWorktree(t *testing.T) {
	repoDir := initTestRepo(t)
	t.Chdir(repoDir)

	path, repoRoot, err := createWorktree("wt-root")
	if err != nil {
		t.Fatalf(`createWorktree("wt-root"): %v`, err)
	}
	if repoRoot != repoDir {
		t.Errorf("repoRoot = %q, want %q", repoRoot, repoDir)
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		t.Errorf("worktree path %q not created: %v", path, err)
	}
}

// TestCreateWorktree_FromSubdirectory is the regression test for the bug
// this change fixes: repoRoot must be the repo's top level even when leash
// is invoked from underneath it, not wherever the caller's cwd happens to be.
func TestCreateWorktree_FromSubdirectory(t *testing.T) {
	repoDir := initTestRepo(t)
	subDir := filepath.Join(repoDir, "src", "pkg")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	t.Chdir(subDir)

	_, repoRoot, err := createWorktree("wt")
	if err != nil {
		t.Fatalf(`createWorktree("wt"): %v`, err)
	}
	if repoRoot != repoDir {
		t.Errorf("repoRoot = %q, want %q (must anchor on repo top level, not cwd %q)", repoRoot, repoDir, subDir)
	}
}

// TestCreateWorktree_RejectsEscapingNames is the regression test for
// --worktree ../../foo escaping the repo's parent directory via
// filepath.Join's path cleaning.
func TestCreateWorktree_RejectsEscapingNames(t *testing.T) {
	repoDir := initTestRepo(t)
	t.Chdir(repoDir)

	for _, name := range []string{"../escape", "../../escape", "a/b", "/abs", ".", "..", ""} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := createWorktree(name); err == nil {
				t.Fatalf("createWorktree(%q): expected error, got nil", name)
			}
		})
	}
}

func TestValidateWorktreeName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"my-fix", false},
		{"wt_1.2", false},
		{"", true},
		{".", true},
		{"..", true},
		{"a/b", true},
		{"../a", true},
		{"/abs", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWorktreeName(tc.name)
			if tc.wantErr && err == nil {
				t.Errorf("validateWorktreeName(%q): expected error, got nil", tc.name)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateWorktreeName(%q): unexpected error: %v", tc.name, err)
			}
		})
	}
}

// TestCreateWorktree_NotAGitRepo verifies the underlying git error and
// stderr are preserved in the wrapped error message, not swallowed behind a
// generic "not a git repository" string.
func TestCreateWorktree_NotAGitRepo(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(t.TempDir()): %v", err)
	}
	t.Chdir(dir)

	_, _, err = createWorktree("wt")
	if err == nil {
		t.Fatal("createWorktree in a non-git directory: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "git rev-parse --show-toplevel") {
		t.Errorf("error should mention the underlying git command, got: %v", err)
	}
}
