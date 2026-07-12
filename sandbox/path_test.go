//go:build darwin

package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveFuturePath_ExistingFile verifies ResolveFuturePath behaves
// identically to ResolvePath when raw already exists (file case).
func TestResolveFuturePath_ExistingFile(t *testing.T) {
	home := os.Getenv("HOME")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(t.TempDir()): %v", err)
	}
	file := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	want, err := ResolvePath(file, home)
	if err != nil {
		t.Fatalf("ResolvePath(%q): %v", file, err)
	}
	got, err := ResolveFuturePath(file, home)
	if err != nil {
		t.Fatalf("ResolveFuturePath(%q): %v", file, err)
	}
	if got != want {
		t.Errorf("ResolveFuturePath(%q) = %+v, want %+v (same as ResolvePath)", file, got, want)
	}
}

// TestResolveFuturePath_ExistingDir verifies ResolveFuturePath behaves
// identically to ResolvePath when raw already exists (directory case).
func TestResolveFuturePath_ExistingDir(t *testing.T) {
	home := os.Getenv("HOME")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(t.TempDir()): %v", err)
	}
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	want, err := ResolvePath(sub, home)
	if err != nil {
		t.Fatalf("ResolvePath(%q): %v", sub, err)
	}
	got, err := ResolveFuturePath(sub, home)
	if err != nil {
		t.Fatalf("ResolveFuturePath(%q): %v", sub, err)
	}
	if got != want {
		t.Errorf("ResolveFuturePath(%q) = %+v, want %+v (same as ResolvePath)", sub, got, want)
	}
}

// TestResolveFuturePath_MissingBase verifies that when raw does not exist
// but its parent does, ResolveFuturePath returns <resolvedParent>/<base>
// with IsDir=false and no error.
func TestResolveFuturePath_MissingBase(t *testing.T) {
	home := os.Getenv("HOME")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(t.TempDir()): %v", err)
	}
	notYetCreated := filepath.Join(dir, "packed-refs.lock")

	got, err := ResolveFuturePath(notYetCreated, home)
	if err != nil {
		t.Fatalf("ResolveFuturePath(%q): %v", notYetCreated, err)
	}
	want := filepath.Join(dir, "packed-refs.lock")
	if got.Resolved != want {
		t.Errorf("Resolved = %q, want %q", got.Resolved, want)
	}
	if got.IsDir {
		t.Error("IsDir = true, want false for not-yet-existing path")
	}
}

// TestResolveFuturePath_MissingParent verifies an error when the parent
// directory itself doesn't exist.
func TestResolveFuturePath_MissingParent(t *testing.T) {
	home := os.Getenv("HOME")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(t.TempDir()): %v", err)
	}
	path := filepath.Join(dir, "no-such-parent", "file.lock")

	if _, err := ResolveFuturePath(path, home); err == nil {
		t.Fatal("expected error for missing parent directory")
	}
}

// TestResolveFuturePath_SymlinkedParent is the falsifiable core of the
// design: when the parent directory is reached via a symlink, the emitted
// path must be based on the RESOLVED (canonical) parent, because Seatbelt
// evaluates SBPL rules against canonical paths. A naive implementation that
// emits the raw string verbatim would fail this test whenever a parent
// component is a symlink (e.g. /tmp -> /private/tmp on macOS).
func TestResolveFuturePath_SymlinkedParent(t *testing.T) {
	home := os.Getenv("HOME")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(t.TempDir()): %v", err)
	}
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatalf("mkdir realDir: %v", err)
	}
	linkDir := filepath.Join(root, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	raw := filepath.Join(linkDir, "newfile")
	got, err := ResolveFuturePath(raw, home)
	if err != nil {
		t.Fatalf("ResolveFuturePath(%q): %v", raw, err)
	}
	want := filepath.Join(realDir, "newfile")
	if got.Resolved != want {
		t.Errorf("Resolved = %q, want %q (must be based on the RESOLVED parent %q, not the symlink %q)",
			got.Resolved, want, realDir, linkDir)
	}
	if got.IsDir {
		t.Error("IsDir = true, want false for not-yet-existing path")
	}
}

// TestResolveFuturePath_EmptyRaw verifies an error for an empty raw path.
func TestResolveFuturePath_EmptyRaw(t *testing.T) {
	if _, err := ResolveFuturePath("", os.Getenv("HOME")); err == nil {
		t.Fatal("expected error for empty path")
	}
}

// TestResolveFuturePath_EmptyHome verifies an error for an empty home,
// which is required for ~ expansion.
func TestResolveFuturePath_EmptyHome(t *testing.T) {
	if _, err := ResolveFuturePath("~/foo", ""); err == nil {
		t.Fatal("expected error for empty home")
	}
}

// TestResolveFutureDir_MissingSubdir is the falsifiable core of the design:
// a not-yet-existing subdir of an existing directory (e.g. a crate's
// target/ before cargo runs) must resolve with IsDir=true, so the emitted
// SBPL rule is a subpath (covers anything later created beneath it) rather
// than a literal.
func TestResolveFutureDir_MissingSubdir(t *testing.T) {
	home := os.Getenv("HOME")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(t.TempDir()): %v", err)
	}
	notYetCreated := filepath.Join(dir, "target")

	got, err := ResolveFutureDir(notYetCreated, home)
	if err != nil {
		t.Fatalf("ResolveFutureDir(%q): %v", notYetCreated, err)
	}
	want := filepath.Join(dir, "target")
	if got.Resolved != want {
		t.Errorf("Resolved = %q, want %q", got.Resolved, want)
	}
	if !got.IsDir {
		t.Error("IsDir = false, want true for not-yet-existing dir")
	}
}

// TestResolveFutureDir_ExistingDir verifies ResolveFutureDir returns
// IsDir=true when raw already exists as a directory.
func TestResolveFutureDir_ExistingDir(t *testing.T) {
	home := os.Getenv("HOME")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(t.TempDir()): %v", err)
	}
	sub := filepath.Join(dir, "target")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := ResolveFutureDir(sub, home)
	if err != nil {
		t.Fatalf("ResolveFutureDir(%q): %v", sub, err)
	}
	if got.Resolved != sub {
		t.Errorf("Resolved = %q, want %q", got.Resolved, sub)
	}
	if !got.IsDir {
		t.Error("IsDir = false, want true for existing dir")
	}
}

// TestResolveFutureDir_ExistingFile verifies ResolveFutureDir errors when
// raw already exists but is a regular file, not a directory.
func TestResolveFutureDir_ExistingFile(t *testing.T) {
	home := os.Getenv("HOME")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(t.TempDir()): %v", err)
	}
	file := filepath.Join(dir, "target")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := ResolveFutureDir(file, home); err == nil {
		t.Fatal("expected error for existing regular file")
	}
}
