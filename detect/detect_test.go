//go:build darwin

package detect_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aka-rider/leash/detect"
	"github.com/aka-rider/leash/sandbox"
)

// setupRustCrate writes a minimal cargo crate with a lib, a test, and a
// build script to a symlink-resolved t.TempDir(). Returns the crate dir.
func setupRustCrate(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	must(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[package]
name = "leashtest"
version = "0.0.0"
edition = "2021"

[lib]
path = "src/lib.rs"
`), 0600))
	must(t, os.WriteFile(filepath.Join(dir, "build.rs"),
		[]byte(`fn main(){ println!("cargo:warning=BUILDRS_RAN"); }`), 0600))
	must(t, os.MkdirAll(filepath.Join(dir, "src"), 0755))
	must(t, os.WriteFile(filepath.Join(dir, "src", "lib.rs"),
		[]byte(`#[cfg(test)]
mod tests {
    #[test]
    fn it_works() {
        assert_eq!(2 + 2, 4);
    }
}
`), 0600))
	return dir
}

// sandboxRun creates a sandbox from cfg, wires Stdout/Stderr buffers onto cmd,
// runs it inside the sandbox (60 s timeout), and returns stdout.
// t.Fatal on sandbox setup failure or non-zero exit.
func sandboxRun(t *testing.T, cfg sandbox.Config, cmd *exec.Cmd) string {
	t.Helper()
	sb, err := sandbox.New(cfg)
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	defer sb.Close()
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := sb.Run(ctx, cmd); err != nil {
		t.Fatalf("run %v: %v\nstdout: %s\nstderr: %s",
			cmd.Args, err, out.String(), errBuf.String())
	}
	return out.String()
}

// setupGoModule writes a minimal go.mod + main.go to a symlink-resolved t.TempDir().
// Running "go run ." in that dir prints "ok".
func setupGoModule(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	must(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0600))
	must(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte(`package main; import "fmt"; func main() { fmt.Println("ok") }`), 0600))
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
}

// resolveBin returns the symlink-resolved absolute path of name.
// Skips the test if name is not in PATH.
func resolveBin(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not installed", name)
	}
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks %s: %v", name, err)
	}
	return r
}

// resolvedTmpDir returns a symlink-resolved t.TempDir().
// The base SBPL profile allows /private/var/folders (where t.TempDir() lives),
// so granting Read on this path is sufficient for the sandbox to cd into it.
func resolvedTmpDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks tmpDir: %v", err)
	}
	return dir
}

// TestDetect_Go verifies that the Go toolchain runs inside the detect profile.
// Exercises: GOROOT stdlib (fmt), compiler toolchain, build cache.
func TestDetect_Go(t *testing.T) {
	home := os.Getenv("HOME")
	p := sandbox.NewToolProfile("detect", home)

	var err error
	p, err = detect.Go(p)
	if err != nil {
		t.Fatalf("detect.Go: %v", err)
	}

	goBin := resolveBin(t, "go")
	modDir := setupGoModule(t)

	grant := sandbox.NewToolProfile("grant", home)
	must(t, grant.Allow(filepath.Dir(goBin), sandbox.Exec))
	must(t, grant.Allow(modDir, sandbox.Read))

	cfg := sandbox.Config{Profiles: []sandbox.Snapshot{grant.Snapshot(), p.Snapshot()}}
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = modDir

	out := sandboxRun(t, cfg, cmd)
	if !strings.Contains(out, "ok") {
		t.Errorf("go run: unexpected output %q", out)
	}
}

// TestDetect_Python verifies that Python can import its stdlib inside the detect profile.
// Exercises: Python interpreter lib dir (stdlib access).
func TestDetect_Python(t *testing.T) {
	home := os.Getenv("HOME")
	p := sandbox.NewToolProfile("detect", home)

	var err error
	p, err = detect.Python(p)
	if err != nil {
		t.Fatalf("detect.Python: %v", err)
	}

	// Try python3 first, fall back to python.
	var pyBin string
	for _, name := range []string{"python3", "python"} {
		if raw, lookErr := exec.LookPath(name); lookErr == nil {
			if r, rErr := filepath.EvalSymlinks(raw); rErr == nil {
				pyBin = r
				break
			}
		}
	}
	if pyBin == "" {
		t.Skip("python not installed")
	}

	tmpDir := resolvedTmpDir(t)

	grant := sandbox.NewToolProfile("grant", home)
	must(t, grant.Allow(filepath.Dir(pyBin), sandbox.Exec))
	must(t, grant.Allow(tmpDir, sandbox.Read))

	cfg := sandbox.Config{Profiles: []sandbox.Snapshot{grant.Snapshot(), p.Snapshot()}}
	cmd := exec.Command(pyBin, "-c", "import sys; sys.exit(0)")
	cmd.Dir = tmpDir

	sandboxRun(t, cfg, cmd)
}

// TestDetect_Git verifies that git can read global config inside the detect profile.
// Exercises: ~/.gitconfig read, CLT / Xcode developer dir exec.
// detect.Xcode is composed because /usr/bin/git calls xcrun, which needs the active
// developer dir (xcode-select -p). detect.Homebrew is composed because Homebrew-installed
// git links against Homebrew libraries (e.g. pcre2) that live in {prefix}/opt.
func TestDetect_Git(t *testing.T) {
	home := os.Getenv("HOME")
	p := sandbox.NewToolProfile("detect", home)

	var err error
	p, err = detect.Homebrew(p)
	if err != nil {
		t.Fatalf("detect.Homebrew: %v", err)
	}
	p, err = detect.Xcode(p)
	if err != nil {
		t.Fatalf("detect.Xcode: %v", err)
	}
	p, err = detect.Git(p)
	if err != nil {
		t.Fatalf("detect.Git: %v", err)
	}

	gitBin := resolveBin(t, "git")
	tmpDir := resolvedTmpDir(t)

	grant := sandbox.NewToolProfile("grant", home)
	must(t, grant.Allow(filepath.Dir(gitBin), sandbox.Exec))
	must(t, grant.Allow(tmpDir, sandbox.Read))

	cfg := sandbox.Config{Profiles: []sandbox.Snapshot{grant.Snapshot(), p.Snapshot()}}
	// Reads ~/.gitconfig — the primary path detect.Git grants.
	cmd := exec.Command(gitBin, "config", "--global", "--list")
	cmd.Dir = tmpDir

	sandboxRun(t, cfg, cmd)
}

// TestDetect_Homebrew verifies that brew can inspect its own config inside the detect profile.
// Exercises: HOMEBREW_PREFIX dirs (read), HOMEBREW_* env vars (set via AddEnv).
// detect.Xcode is composed because brew config probes the Xcode/CLT version via xcrun and
// reads /Applications/Xcode.app/Contents/version.plist.
func TestDetect_Homebrew(t *testing.T) {
	home := os.Getenv("HOME")
	p := sandbox.NewToolProfile("detect", home)

	var err error
	p, err = detect.Xcode(p)
	if err != nil {
		t.Fatalf("detect.Xcode: %v", err)
	}
	p, err = detect.Homebrew(p)
	if err != nil {
		t.Fatalf("detect.Homebrew: %v", err)
	}

	brewBin := resolveBin(t, "brew")
	tmpDir := resolvedTmpDir(t)

	grant := sandbox.NewToolProfile("grant", home)
	must(t, grant.Allow(filepath.Dir(brewBin), sandbox.Exec))
	must(t, grant.Allow(tmpDir, sandbox.Read))

	cfg := sandbox.Config{Profiles: []sandbox.Snapshot{grant.Snapshot(), p.Snapshot()}}
	cmd := exec.Command(brewBin, "config")
	cmd.Dir = tmpDir

	sandboxRun(t, cfg, cmd)
}

// TestDetect_NPM verifies that npm can read its config inside the detect profile.
// Exercises: ~/.npmrc read.
// Note: npm is often a shell shim not visible to Go processes; test skips if absent.
func TestDetect_NPM(t *testing.T) {
	home := os.Getenv("HOME")
	p := sandbox.NewToolProfile("detect", home)

	var err error
	p, err = detect.NPM(p)
	if err != nil {
		t.Fatalf("detect.NPM: %v", err)
	}

	npmBin := resolveBin(t, "npm")
	tmpDir := resolvedTmpDir(t)

	grant := sandbox.NewToolProfile("grant", home)
	must(t, grant.Allow(filepath.Dir(npmBin), sandbox.Exec))
	must(t, grant.Allow(tmpDir, sandbox.Read))

	cfg := sandbox.Config{Profiles: []sandbox.Snapshot{grant.Snapshot(), p.Snapshot()}}
	cmd := exec.Command(npmBin, "config", "list")
	cmd.Dir = tmpDir

	sandboxRun(t, cfg, cmd)
}

// TestDetect_Xcode verifies that xcrun can locate and run clang inside the detect profile.
// Exercises: developer dir exec grant — xcrun looks up clang inside it.
func TestDetect_Xcode(t *testing.T) {
	home := os.Getenv("HOME")
	p := sandbox.NewToolProfile("detect", home)

	var err error
	p, err = detect.Xcode(p)
	if err != nil {
		t.Fatalf("detect.Xcode: %v", err)
	}

	xcrunBin := resolveBin(t, "xcrun")
	tmpDir := resolvedTmpDir(t)

	grant := sandbox.NewToolProfile("grant", home)
	must(t, grant.Allow(filepath.Dir(xcrunBin), sandbox.Exec))
	must(t, grant.Allow(tmpDir, sandbox.Read))

	cfg := sandbox.Config{Profiles: []sandbox.Snapshot{grant.Snapshot(), p.Snapshot()}}
	cmd := exec.Command(xcrunBin, "clang", "--version")
	cmd.Dir = tmpDir

	sandboxRun(t, cfg, cmd)
}

// TestDetect_Claude verifies that the Claude CLI runs inside the detect profile.
// Exercises: binary exec + optional config paths (~/.claude.json, ~/.claude).
func TestDetect_Claude(t *testing.T) {
	home := os.Getenv("HOME")
	p := sandbox.NewToolProfile("detect", home)

	var err error
	p, err = detect.Claude(p)
	if err != nil {
		t.Fatalf("detect.Claude: %v", err)
	}

	claudeBin := resolveBin(t, "claude")
	tmpDir := resolvedTmpDir(t)

	grant := sandbox.NewToolProfile("grant", home)
	must(t, grant.Allow(filepath.Dir(claudeBin), sandbox.Exec))
	must(t, grant.Allow(tmpDir, sandbox.Read))

	cfg := sandbox.Config{Profiles: []sandbox.Snapshot{grant.Snapshot(), p.Snapshot()}}
	// --version is a purely local operation (no network, no auth, no
	// ~/.claude.json read) — deliberately chosen over a subcommand like
	// "config list" that requires a live, authenticated session against the
	// Anthropic API and would make this test flaky/environment-dependent.
	cmd := exec.Command(claudeBin, "--version")
	cmd.Dir = tmpDir

	sandboxRun(t, cfg, cmd)
}

// TestDetect_Docker verifies that docker can list contexts inside the detect profile.
// Exercises: ~/.docker/contexts and ~/.docker/config.json reads.
func TestDetect_Docker(t *testing.T) {
	home := os.Getenv("HOME")
	p := sandbox.NewToolProfile("detect", home)

	var err error
	p, err = detect.Docker(p)
	if err != nil {
		t.Fatalf("detect.Docker: %v", err)
	}

	dockerBin := resolveBin(t, "docker")
	tmpDir := resolvedTmpDir(t)

	grant := sandbox.NewToolProfile("grant", home)
	must(t, grant.Allow(filepath.Dir(dockerBin), sandbox.Exec))
	must(t, grant.Allow(tmpDir, sandbox.Read))

	cfg := sandbox.Config{Profiles: []sandbox.Snapshot{grant.Snapshot(), p.Snapshot()}}
	// context ls reads ~/.docker/contexts and ~/.docker/config.json.
	cmd := exec.Command(dockerBin, "context", "ls")
	cmd.Dir = tmpDir

	sandboxRun(t, cfg, cmd)
}

// TestDetect_Rust verifies that cargo can build and run a crate's tests
// inside the detect profile. Exercises: cargo/rustc bin dir exec, CARGO_HOME
// write (registry cache), and — the gap #2 case — exec on the crate's
// target/ dir for the build-script binary and the compiled test binary,
// both of which are created after the SBPL profile is compiled.
func TestDetect_Rust(t *testing.T) {
	home := os.Getenv("HOME")

	cargoBin := resolveBin(t, "cargo")
	crateDir := setupRustCrate(t)

	// Rust's detector introspects the process cwd via os.Getwd() (the
	// detector-registration loop has a fixed signature — see rust.go), so
	// the crate must be the process cwd before calling detect.Rust.
	t.Chdir(crateDir)

	p := sandbox.NewToolProfile("detect", home)
	var err error
	p, err = detect.Homebrew(p)
	if err != nil {
		t.Fatalf("detect.Homebrew: %v", err)
	}
	p, err = detect.Xcode(p)
	if err != nil {
		t.Fatalf("detect.Xcode: %v", err)
	}
	p, err = detect.Rust(p)
	if err != nil {
		t.Fatalf("detect.Rust: %v", err)
	}

	grant := sandbox.NewToolProfile("grant", home)
	must(t, grant.Allow(filepath.Dir(cargoBin), sandbox.Exec))
	must(t, grant.Allow(crateDir, sandbox.Write))

	cfg := sandbox.Config{
		Profiles: []sandbox.Snapshot{grant.Snapshot(), p.Snapshot()},
		ExtraEnv: map[string]string{
			"CARGO_HOME":        filepath.Join(crateDir, "cargohome"),
			"CARGO_NET_OFFLINE": "1",
		},
	}
	sb, err := sandbox.New(cfg)
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	defer sb.Close()

	cmd := exec.Command(cargoBin, "test")
	cmd.Dir = crateDir
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := sb.Run(ctx, cmd); err != nil {
		t.Fatalf("run %v: %v\nstdout: %s\nstderr: %s",
			cmd.Args, err, out.String(), errBuf.String())
	}

	combined := out.String() + errBuf.String()
	if !strings.Contains(combined, "BUILDRS_RAN") {
		t.Errorf("build script did not run (or was not allowed to exec): combined output %q", combined)
	}
	if !strings.Contains(out.String(), "test result: ok") {
		t.Errorf("cargo test: unexpected stdout %q", out.String())
	}
}
