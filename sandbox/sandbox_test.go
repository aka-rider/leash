//go:build darwin

package sandbox

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPath_EmptyString(t *testing.T) {
	_, err := ResolvePath("", os.Getenv("HOME"))
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestPath_NullByteInjection(t *testing.T) {
	_, err := ResolvePath("/opt/dir\x00/hidden", os.Getenv("HOME"))
	if err == nil {
		t.Fatal("expected error for null byte in path")
	}
}

func TestPath_RelativePath(t *testing.T) {
	home := os.Getenv("HOME")
	// "." should resolve to the current working directory — not an error
	p, err := ResolvePath(".", home)
	if err != nil {
		t.Fatalf("relative path '.' should resolve without error: %v", err)
	}
	if !p.IsDir {
		t.Error("cwd should be a directory")
	}
}

func TestPath_RecursiveSymlink(t *testing.T) {
	dir := t.TempDir()
	resolvedDir, _ := filepath.EvalSymlinks(dir)
	a := filepath.Join(resolvedDir, "a")
	b := filepath.Join(resolvedDir, "b")
	os.Symlink(b, a)
	os.Symlink(a, b)

	_, err := ResolvePath(a, os.Getenv("HOME"))
	if err == nil {
		t.Fatal("expected error for recursive symlink")
	}
}

func TestPath_BrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	resolvedDir, _ := filepath.EvalSymlinks(dir)
	link := filepath.Join(resolvedDir, "broken")
	os.Symlink("/nonexistent/target/xyz", link)

	_, err := ResolvePath(link, os.Getenv("HOME"))
	if err == nil {
		t.Fatal("expected error for broken symlink")
	}
}

func TestPath_DirectoryTraversal(t *testing.T) {
	home := os.Getenv("HOME")
	p, err := ResolvePath("~/../../../../etc", home)
	if err != nil {
		t.Skipf("traversal path doesn't exist (fine): %v", err)
	}
	if strings.Contains(p.Resolved, "..") {
		t.Errorf("resolved path contains ..: %q", p.Resolved)
	}
}

func TestPath_MaxLengthExceeded(t *testing.T) {
	longName := strings.Repeat("a", 1100)
	_, err := ResolvePath("/"+longName, os.Getenv("HOME"))
	if err == nil {
		t.Fatal("expected error for path exceeding PATH_MAX")
	}
}

func TestSandbox_New_MinimalConfig(t *testing.T) {
	// New() with empty Config should succeed — no required paths
	sb, err := New(Config{})
	if err != nil {
		t.Fatalf("New with empty config failed: %v", err)
	}
	defer sb.Close()
}

func TestBaseEnv_NoLeak(t *testing.T) {
	sensitiveVars := []string{
		"AWS_SECRET_ACCESS_KEY", "GITHUB_TOKEN", "OPENAI_API_KEY", "ANTHROPIC_API_KEY",
	}
	for _, v := range sensitiveVars {
		t.Setenv(v, "LEAKED_"+v)
	}

	env := BaseEnv("/Users/test", "/private/tmp")
	envStr := strings.Join(env, "\n")

	for _, v := range sensitiveVars {
		if strings.Contains(envStr, "LEAKED_"+v) {
			t.Errorf("BaseEnv leaked sensitive variable: %s", v)
		}
	}
}

func TestDeny_ReadFileOutsideAllowlist(t *testing.T) {
	home := os.Getenv("HOME")
	secretDir := filepath.Join(home, ".seatbelt-test-deny-read")
	os.MkdirAll(secretDir, 0755)
	defer os.RemoveAll(secretDir)
	secretFile := filepath.Join(secretDir, "secret.txt")
	os.WriteFile(secretFile, []byte("TOP_SECRET_DATA"), 0644)

	sb, err := New(Config{})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer sb.Close()

	var stdout bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/cat", secretFile)
	cmd.Stdout = &stdout
	cmd.Stderr = &bytes.Buffer{}

	if err := sb.Run(ctx, cmd); err == nil {
		t.Error("expected non-zero exit from denied process (confirms child ran and was denied)")
	}

	if strings.Contains(stdout.String(), "TOP_SECRET_DATA") {
		t.Fatal("SECURITY FAILURE: sandbox leaked secret file content")
	}
}

func TestDeny_WriteOutsideGrantedPaths(t *testing.T) {
	home := os.Getenv("HOME")
	targetDir := filepath.Join(home, ".seatbelt-test-deny-write")
	os.MkdirAll(targetDir, 0755)
	defer os.RemoveAll(targetDir)
	targetFile := filepath.Join(targetDir, "written.txt")

	sb, err := New(Config{})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer sb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "echo BREACH > '"+targetFile+"'")
	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &bytes.Buffer{}

	if err := sb.Run(ctx, cmd); err == nil {
		t.Error("expected non-zero exit from denied process (confirms child ran and was denied)")
	}

	if _, err := os.Stat(targetFile); err == nil {
		content, _ := os.ReadFile(targetFile)
		os.Remove(targetFile)
		t.Fatalf("SECURITY FAILURE: wrote outside granted paths: %s", string(content))
	}
}

func TestAllow_ReadFromProfile(t *testing.T) {
	allowDir := t.TempDir()
	resolvedAllowDir, _ := filepath.EvalSymlinks(allowDir)
	allowFile := filepath.Join(resolvedAllowDir, "readable.txt")
	os.WriteFile(allowFile, []byte("ALLOWED_CONTENT"), 0644)

	home := os.Getenv("HOME")
	p := NewToolProfile("test", home)
	p.Allow(resolvedAllowDir, Read)

	sb, err := New(Config{Profiles: []Snapshot{p.Snapshot()}})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer sb.Close()

	var stdout bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/cat", allowFile)
	cmd.Stdout = &stdout
	cmd.Stderr = &bytes.Buffer{}

	if err := sb.Run(ctx, cmd); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !strings.Contains(stdout.String(), "ALLOWED_CONTENT") {
		t.Errorf("expected to read allowed file, got: %q", stdout.String())
	}
}

func TestAllow_WriteWithGrant(t *testing.T) {
	writeDir := t.TempDir()
	resolvedDir, _ := filepath.EvalSymlinks(writeDir)

	home := os.Getenv("HOME")
	p := NewToolProfile("test", home)
	p.Allow(resolvedDir, Write)

	sb, err := New(Config{Profiles: []Snapshot{p.Snapshot()}})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer sb.Close()

	targetFile := filepath.Join(resolvedDir, "output.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "echo WRITE_OK > "+targetFile)
	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &bytes.Buffer{}

	if err := sb.Run(ctx, cmd); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	content, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if !strings.Contains(string(content), "WRITE_OK") {
		t.Errorf("wrong content: %q", string(content))
	}
}

func TestDeny_ExecArbitraryBinary(t *testing.T) {
	home := os.Getenv("HOME")
	binDir := filepath.Join(home, ".seatbelt-test-deny-exec")
	os.MkdirAll(binDir, 0755)
	defer os.RemoveAll(binDir)

	src, err := os.ReadFile("/bin/echo")
	if err != nil {
		t.Fatalf("read /bin/echo: %v", err)
	}
	binFile := filepath.Join(binDir, "echo")
	os.WriteFile(binFile, src, 0755)

	sb, err := New(Config{})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer sb.Close()

	var stdout bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binFile, "EXEC_LEAKED")
	cmd.Stdout = &stdout
	cmd.Stderr = &bytes.Buffer{}

	if err := sb.Run(ctx, cmd); err == nil {
		t.Error("expected non-zero exit from denied process (confirms child ran and was denied)")
	}

	if strings.Contains(stdout.String(), "EXEC_LEAKED") {
		t.Fatal("SECURITY FAILURE: sandbox allowed exec of arbitrary binary outside profile")
	}
}

func TestAllow_ExecFromProfile(t *testing.T) {
	execDir := t.TempDir()
	resolvedDir, _ := filepath.EvalSymlinks(execDir)
	binDir := filepath.Join(resolvedDir, "bin")
	os.MkdirAll(binDir, 0755)
	scriptFile := filepath.Join(binDir, "myecho.sh")
	os.WriteFile(scriptFile, []byte("#!/bin/sh\necho EXEC_OK\n"), 0755)

	home := os.Getenv("HOME")
	p := NewToolProfile("test", home)
	p.Allow(binDir, Exec)

	sb, err := New(Config{Profiles: []Snapshot{p.Snapshot()}})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer sb.Close()

	var stdout bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, scriptFile)
	cmd.Stdout = &stdout
	cmd.Stderr = &bytes.Buffer{}

	if err := sb.Run(ctx, cmd); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !strings.Contains(stdout.String(), "EXEC_OK") {
		t.Errorf("expected to exec script from granted dir, got: %q", stdout.String())
	}
}

func TestWrap_EmptyCommand(t *testing.T) {
	sb, err := New(Config{})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer sb.Close()

	cmd := &exec.Cmd{}
	if err := sb.Wrap(cmd); err == nil {
		t.Fatal("expected error wrapping empty command")
	}
}

func TestAllow_NoWriteWithoutGrant(t *testing.T) {
	// Write to a path that has no explicit +w grant should be denied.
	// Must use a HOME-based dir — temp dirs fall under /private/var/folders
	// which the system base allows for read+write.
	home := os.Getenv("HOME")
	writeDir := filepath.Join(home, ".seatbelt-test-readonly-grant")
	os.MkdirAll(writeDir, 0755)
	defer os.RemoveAll(writeDir)
	resolvedDir, _ := filepath.EvalSymlinks(writeDir)

	// Grant read only — no write
	p := NewToolProfile("test", home)
	p.Allow(resolvedDir, Read)

	sb, err := New(Config{Profiles: []Snapshot{p.Snapshot()}})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer sb.Close()

	targetFile := filepath.Join(resolvedDir, "breach.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "echo BREACH > '"+targetFile+"'")
	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &bytes.Buffer{}

	sb.Run(ctx, cmd)

	if _, err := os.Stat(targetFile); err == nil {
		os.Remove(targetFile)
		t.Fatal("SECURITY FAILURE: readonly grant allowed write")
	}
}

func TestDeny_DenyOverridesAllow(t *testing.T) {
	// A deny rule should override a user allow rule for the same path.
	// Must use a HOME-based dir — temp dirs are in /private/var/folders which
	// the system base allows unconditionally, so path-specific denies cannot
	// override system base grants for those paths.
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".seatbelt-test-deny-overrides")
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)
	resolvedDir, _ := filepath.EvalSymlinks(dir)
	secretFile := filepath.Join(resolvedDir, "secret.txt")
	os.WriteFile(secretFile, []byte("SECRET_DATA"), 0644)

	allowProf := NewToolProfile("allow", home)
	allowProf.Allow(resolvedDir, Read)

	denyProf := NewToolProfile("deny", home)
	denyProf.Deny(resolvedDir, Read)

	sb, err := New(Config{
		Profiles: []Snapshot{allowProf.Snapshot(), denyProf.Snapshot()},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer sb.Close()

	var stdout bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/cat", secretFile)
	cmd.Stdout = &stdout
	cmd.Stderr = &bytes.Buffer{}

	if err := sb.Run(ctx, cmd); err == nil {
		t.Error("expected non-zero exit from denied process (confirms child ran and was denied)")
	}

	if strings.Contains(stdout.String(), "SECRET_DATA") {
		t.Fatal("SECURITY FAILURE: deny rule did not override allow rule")
	}
}
