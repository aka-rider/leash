//go:build darwin

package leash

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aka-rider/leash/sandbox"
)

// scratchDir creates a fresh HOME-based test directory (NOT t.TempDir() --
// TempDir resolves under /private/var/folders, which the sandbox's system
// base profile allows read+write unconditionally, defeating negative
// assertions; see sandbox/sandbox_test.go's comments for the same gotcha).
// The directory name is derived from t.Name() (plus an optional suffix) so
// concurrent test runs (make test and make test-darwin both cover this
// file) can't collide on the same path.
func scratchDir(t *testing.T, suffix string) string {
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

// TestExecute_NoHomeReadByDefault is the regression test for detect.Claude
// formerly granting {"~", sandbox.Read} whenever claude was in PATH, which
// made all of $HOME readable by every sandboxed command and defeated
// deny-by-default. Must pass whether or not claude is installed on the
// machine running the test.
func TestExecute_NoHomeReadByDefault(t *testing.T) {
	// The canary lives directly under $HOME (not nested in a scratch
	// subdirectory) to mirror the exact shape of the bug: {"~", sandbox.Read}
	// granted read on all of $HOME, canary included.
	canary := filepath.Join(os.Getenv("HOME"), ".leash-test-canary-"+strings.ReplaceAll(t.Name(), "/", "_"))
	if err := os.WriteFile(canary, []byte("TOP_SECRET_HOME_CONTENT"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(canary) })

	cwd := scratchDir(t, "cwd")
	t.Chdir(cwd)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	l := Leash{Program: "cat", Args: []string{canary}, Stdout: &stdout, Stderr: &stderr}
	if err := Execute(ctx, l); err == nil {
		t.Fatal("expected read of $HOME file with no grants to be denied")
	}
	if strings.Contains(stdout.String(), "TOP_SECRET_HOME_CONTENT") {
		t.Fatalf("SECURITY FAILURE: $HOME is readable by default: %q", stdout.String())
	}
}

func TestDefaultCwdPermission(t *testing.T) {
	if got := defaultCwdPermission(""); got != sandbox.Write {
		t.Errorf("empty Dir: got %v, want sandbox.Write", got)
	}
	if got := defaultCwdPermission("/some/other/dir"); got != sandbox.Read {
		t.Errorf("non-empty Dir: got %v, want sandbox.Read", got)
	}
}

func TestExecute_CwdWritableByDefault(t *testing.T) {
	dir := scratchDir(t, "")
	t.Chdir(dir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	l := Leash{Program: "sh", Args: []string{"-c", "echo WRITE_OK > out.txt"}, Stderr: &bytes.Buffer{}}
	if err := Execute(ctx, l); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil || !bytes.Contains(content, []byte("WRITE_OK")) {
		t.Fatalf("cwd should be writable by default: content=%q err=%v", content, err)
	}
}

// TestExecute_DirSetKeepsOriginalCwdReadOnly mirrors --worktree: when Dir
// points elsewhere, the original cwd must stay readable but not writable,
// while Dir itself is writable.
func TestExecute_DirSetKeepsOriginalCwdReadOnly(t *testing.T) {
	origDir := scratchDir(t, "orig")
	wtDir := scratchDir(t, "wt")
	t.Chdir(origDir)

	seed := filepath.Join(origDir, "seed.txt")
	if err := os.WriteFile(seed, []byte("SEED"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	base := func() Leash {
		return Leash{Dir: wtDir, Writes: []string{wtDir}, Stderr: &bytes.Buffer{}}
	}

	t.Run("original dir stays readable", func(t *testing.T) {
		l := base()
		l.Program, l.Args = "cat", []string{seed}
		var stdout bytes.Buffer
		l.Stdout = &stdout
		if err := Execute(ctx, l); err != nil {
			t.Fatalf("expected read to succeed: %v", err)
		}
		if !bytes.Contains(stdout.Bytes(), []byte("SEED")) {
			t.Errorf("unexpected content: %q", stdout.String())
		}
	})

	t.Run("original dir stays not writable", func(t *testing.T) {
		l := base()
		l.Program, l.Args = "sh", []string{"-c", "echo BREACH > " + filepath.Join(origDir, "breach.txt")}
		if err := Execute(ctx, l); err == nil {
			t.Fatal("expected write to original dir to be denied")
		}
		if _, err := os.Stat(filepath.Join(origDir, "breach.txt")); err == nil {
			t.Error("SECURITY: original directory should stay read-only when Dir is set")
		}
	})

	t.Run("worktree dir is writable", func(t *testing.T) {
		l := base()
		l.Program, l.Args = "sh", []string{"-c", "echo WRITE_OK > wt-out.txt"}
		if err := Execute(ctx, l); err != nil {
			t.Fatalf("expected write to worktree dir to succeed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(wtDir, "wt-out.txt")); err != nil {
			t.Errorf("worktree dir file not created: %v", err)
		}
	})
}

// TestExecute_MinusWOptsOutOfCwdWrite is the regression test for the
// documented opt-out: -w . must still restore the old read-only-cwd
// behavior instead of a new flag.
func TestExecute_MinusWOptsOutOfCwdWrite(t *testing.T) {
	dir := scratchDir(t, "")
	t.Chdir(dir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	l := Leash{Program: "sh", Args: []string{"-c", "echo BREACH > out.txt"}, DenyWrites: []string{"."}, Stderr: &bytes.Buffer{}}
	if err := Execute(ctx, l); err == nil {
		t.Fatal("expected write denial with -w .")
	}
	if _, err := os.Stat(filepath.Join(dir, "out.txt")); err == nil {
		t.Error("SECURITY: -w . should have kept cwd read-only")
	}
}

// TestExecute_MinusRAloneLeavesCwdWriteOnly pins the documented consequence
// of the new default: -r . alone no longer fully locks out cwd (only the
// read half of the implicit grant is denied); full lockout needs -r . -w .
// together. See README / Usage().
func TestExecute_MinusRAloneLeavesCwdWriteOnly(t *testing.T) {
	dir := scratchDir(t, "")
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("SEED"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readL := Leash{Program: "cat", Args: []string{"seed.txt"}, DenyReads: []string{"."}, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if err := Execute(ctx, readL); err == nil {
		t.Error("expected read denial with -r .")
	}

	writeL := Leash{Program: "sh", Args: []string{"-c", "echo STILL_WRITABLE > out2.txt"}, DenyReads: []string{"."}, Stderr: &bytes.Buffer{}}
	if err := Execute(ctx, writeL); err != nil {
		t.Errorf("expected write to still succeed with -r . alone (documented consequence): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "out2.txt")); err != nil {
		t.Error("expected -r . alone to leave cwd writable (documented consequence)")
	}
}
