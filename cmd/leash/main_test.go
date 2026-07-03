//go:build darwin

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	leash "github.com/aka-rider/leash"
)

func TestApplyWorktree_GrantsRepoRootReadAndWorktreeWrite(t *testing.T) {
	repoDir := initTestRepo(t)
	t.Chdir(repoDir)

	l, err := applyWorktree(leash.Leash{}, "wt")
	if err != nil {
		t.Fatalf(`applyWorktree(..., "wt"): %v`, err)
	}

	if !slices.Contains(l.Reads, repoDir) {
		t.Errorf("Reads = %v, want to contain repo root %q", l.Reads, repoDir)
	}
	if len(l.Writes) == 0 {
		t.Fatalf("Writes = %v, want at least the worktree path", l.Writes)
	}
	wtPath := l.Writes[0]
	if l.Dir != wtPath {
		t.Errorf("Dir = %q, want %q (the worktree path)", l.Dir, wtPath)
	}
	if fi, err := os.Stat(wtPath); err != nil || !fi.IsDir() {
		t.Errorf("worktree path %q not created: %v", wtPath, err)
	}

	// The main repo's .git internals a linked worktree needs for
	// add/commit should also be granted write.
	gitDir := filepath.Join(repoDir, ".git")
	for _, want := range []string{
		filepath.Join(gitDir, "worktrees", "wt"),
		filepath.Join(gitDir, "objects"),
		filepath.Join(gitDir, "refs"),
	} {
		if !slices.Contains(l.Writes, want) {
			t.Errorf("Writes = %v, want to contain %q", l.Writes, want)
		}
	}
	// logs may not exist in all repos; only assert it when present on disk.
	if logs := filepath.Join(gitDir, "logs"); dirExists(logs) {
		if !slices.Contains(l.Writes, logs) {
			t.Errorf("Writes = %v, want to contain %q (logs dir exists)", l.Writes, logs)
		}
	}
	// The top-level .git dir itself must stay read-only — it holds config
	// and hooks that a sandboxed command should not be able to rewrite.
	if slices.Contains(l.Writes, gitDir) {
		t.Error("SECURITY: top-level .git directory should not be granted write")
	}

	// packed-refs (and its lock/tempfile) are granted as FutureWrites, not
	// Writes, because they may not exist yet at grant time — see
	// applyWorktree's doc comment for why git needs all three.
	wantFuture := []string{
		filepath.Join(gitDir, "packed-refs"),
		filepath.Join(gitDir, "packed-refs.lock"),
		filepath.Join(gitDir, "packed-refs.new"),
	}
	if len(l.FutureWrites) != len(wantFuture) {
		t.Fatalf("FutureWrites = %v, want exactly %v", l.FutureWrites, wantFuture)
	}
	for _, want := range wantFuture {
		if !slices.Contains(l.FutureWrites, want) {
			t.Errorf("FutureWrites = %v, want to contain %q", l.FutureWrites, want)
		}
	}
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// homeScratchDir creates a fresh HOME-based test directory (NOT t.TempDir()
// -- TempDir resolves under /private/var/folders, which the sandbox's base
// profile allows read+write unconditionally, defeating negative assertions;
// see leash/execute_darwin_test.go's scratchDir for the same gotcha).
func homeScratchDir(t *testing.T, suffix string) string {
	t.Helper()
	name := strings.ReplaceAll(t.Name(), "/", "_")
	if suffix != "" {
		name += "-" + suffix
	}
	dir := filepath.Join(os.Getenv("HOME"), ".leash-test-"+name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	return resolved
}

// initTestRepoAtHome is initTestRepo but rooted under $HOME instead of
// t.TempDir(), for tests that assert sandbox DENIALS (which t.TempDir()
// cannot do — see homeScratchDir).
func initTestRepoAtHome(t *testing.T) string {
	t.Helper()
	root := homeScratchDir(t, "repo-root")
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

// TestWorktree_GitAddCommitInsideSandbox is the end-to-end regression test
// for --worktree: a linked worktree's index/HEAD/locks live under the main
// repo's .git/worktrees/<name>/, new objects under .git/objects/, and
// branch refs under .git/refs/ — all of which must be writable from inside
// the sandbox for `git add`/`git commit` to succeed. It also asserts the
// negative: the original repo's working tree must stay unwritable.
func TestWorktree_GitAddCommitInsideSandbox(t *testing.T) {
	repoDir := initTestRepoAtHome(t)
	t.Chdir(repoDir)

	l, err := applyWorktree(leash.Leash{}, "wt")
	if err != nil {
		t.Fatalf("applyWorktree: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	commitL := l
	commitL.Program = "sh"
	commitL.Args = []string{"-c",
		"echo x > f.txt && /usr/bin/git add f.txt && " +
			"/usr/bin/git -c user.name=t -c user.email=t@t commit -qm test"}
	var stdout, stderr bytes.Buffer
	commitL.Stdout, commitL.Stderr = &stdout, &stderr
	if err := leash.Execute(ctx, commitL); err != nil {
		t.Fatalf("git add/commit inside worktree sandbox failed: %v\nstdout: %s\nstderr: %s",
			err, stdout.String(), stderr.String())
	}
	// Regression: git commit's post-commit cleanup used to fail to take the
	// packed-refs lock and print a benign but alarming warning to stderr.
	if strings.Contains(stderr.String(), "packed-refs") {
		t.Errorf("stderr mentions packed-refs (regression): %s", stderr.String())
	}

	// Negative: writing into the ORIGINAL repo's working tree from inside
	// the sandbox must still fail — the worktree grants must not leak write
	// access to the main repo's checkout.
	breachFile := filepath.Join(repoDir, "breach.txt")
	breachL := l
	breachL.Program = "sh"
	breachL.Args = []string{"-c", "echo BREACH > " + breachFile}
	var breachStderr bytes.Buffer
	breachL.Stderr = &breachStderr
	if err := leash.Execute(ctx, breachL); err == nil {
		t.Error("expected write into original repo working tree to be denied")
	}
	if _, err := os.Stat(breachFile); err == nil {
		t.Error("SECURITY: write into original repo working tree succeeded")
	}

	// Negative: `git config` writes to .git/config, which stays read-only on
	// purpose — this pins the security boundary the packed-refs grants must
	// not widen. Compare file content byte-for-byte before/after.
	configPath := filepath.Join(repoDir, ".git", "config")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read .git/config before: %v", err)
	}
	configL := l
	configL.Program = "/usr/bin/git"
	configL.Args = []string{"config", "user.name", "evil"}
	var configStderr bytes.Buffer
	configL.Stderr = &configStderr
	if err := leash.Execute(ctx, configL); err == nil {
		t.Error("SECURITY: git config user.name evil inside worktree sandbox should have failed")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read .git/config after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("SECURITY: .git/config changed:\nbefore: %s\nafter: %s", before, after)
	}
}

// TestWorktree_PackRefsAndBranchDelete is the end-to-end regression test for
// the packed-refs FutureWrites grant: `git branch -d` (after `git
// pack-refs`) deletes a PACKED ref, which requires git to rewrite
// packed-refs — and rewriting packed-refs takes the packed-refs.lock. All
// three names (packed-refs, packed-refs.lock, packed-refs.new) must be
// writable for this to succeed. `git branch -d` may still print a benign
// "could not lock config file .git/config" warning (the top-level .git stays
// read-only on purpose), so this test asserts exit code + packed-refs
// existence + the branch actually being gone, not stderr emptiness.
func TestWorktree_PackRefsAndBranchDelete(t *testing.T) {
	repoDir := initTestRepoAtHome(t)
	t.Chdir(repoDir)

	l, err := applyWorktree(leash.Leash{}, "wt")
	if err != nil {
		t.Fatalf("applyWorktree: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	l.Program = "sh"
	l.Args = []string{"-c",
		"/usr/bin/git branch pk-test && " +
			"/usr/bin/git pack-refs --all && " +
			"/usr/bin/git branch -d pk-test"}
	var stdout, stderr bytes.Buffer
	l.Stdout, l.Stderr = &stdout, &stderr
	if err := leash.Execute(ctx, l); err != nil {
		t.Fatalf("branch/pack-refs/branch -d inside worktree sandbox failed: %v\nstdout: %s\nstderr: %s",
			err, stdout.String(), stderr.String())
	}

	packedRefs := filepath.Join(repoDir, ".git", "packed-refs")
	if _, err := os.Stat(packedRefs); err != nil {
		t.Errorf("expected %q to exist after git pack-refs: %v", packedRefs, err)
	}

	listCmd := exec.Command("git", "branch", "--list", "pk-test")
	listCmd.Dir = repoDir
	out, err := listCmd.Output()
	if err != nil {
		t.Fatalf("git branch --list pk-test: %v", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("branch pk-test still listed after git branch -d: %q", out)
	}
}
