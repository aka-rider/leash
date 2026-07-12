# Plan

## Goal
Add a `detect.Rust` detector (and the minimal sandbox primitive it needs) so a Homebrew or standalone Rust toolchain builds and runs real crates — build scripts, proc-macros, crates.io deps — under `leash cargo test` with no manual `+r/+w/+x` flags.

## Context
Facts verified by Read in the worktree `/Users/xiii/Developer/orqestra-wt/leash/1-add-homebrew-rust-toolchain-support-cargo`:

- **Detector signature is fixed.** `execute_darwin.go:49-52` iterates `[]func(sandbox.ToolProfile) (sandbox.ToolProfile, error)`; every detector must match exactly this signature. So cwd introspection must happen *inside* the detector via `os.Getwd()` — a cwd argument is not available without changing the loop and every detector. Justification for choosing `os.Getwd()`: it preserves the fixed signature and equals the child's cwd for the primary plain-invocation case (`leash` runs the child in its own cwd when `l.Dir == ""`, `execute_darwin.go:122-123`).
- **`detect/golang.go` is the closest analog:** stdlib-only, `//go:build darwin`, `exec.LookPath` → `errors.Is(err, exec.ErrNotFound)` early no-op → `filepath.EvalSymlinks` → `p.Allow(filepath.Dir(bin), sandbox.Exec)` → `AllowOptional` for caches. `AllowOptional` returns `(bool, error)` and golang.go discards the bool via `_, err :=`. Detectors that shell out use `probeOutput` (`detect/probe.go`, Setsid). Rust needs no shell-out.
- **`Config.ExtraEnv` is `map[string]string`, not `[]string`** (`sandbox/sandbox.go:20`, verified by Read). Test env overrides MUST be written as a map literal `map[string]string{"KEY": "value"}`, never `key=value` string slices.
- **`sbplOps` (`sandbox/builder.go:252-261`) treats Write and Exec as independent bits:** `Write` emits `file-read* file-write*` only (no `process-exec`); `Exec` emits `file-read* file-map-executable process-exec`. So a cwd Write grant does NOT make compiled binaries under it executable — this is gap #2. Every permission includes `file-read*`, so an `Exec` grant also grants read (rlibs, sysroot) and a `Write` grant also grants read (registry cache).
- **subpath vs literal is derived from `Path.IsDir`** (`sandbox/builder.go:236-239`): `IsDir=true` → `(subpath …)` (covers children), `IsDir=false` → `(literal …)` (that exact path only).
- **`ResolveFuturePath`/`AllowFuture` set `IsDir=false` for a not-yet-existing path** (`sandbox/path.go:75`, `sandbox/profile.go:64-71`) → emits a `literal` rule. A crate's `target/` typically does NOT exist when the SBPL profile is compiled (before cargo runs); the test binary and build-script binaries are created later under `target/debug/…`. A `literal` rule on `target` would NOT cover them. **Therefore `AllowFuture` alone cannot grant future-`target` exec** — a future-*directory* (subpath) primitive is required. This is why WP1 adds `AllowFutureDir`.
- **`AllowOptional` (`sandbox/profile.go:89-97`)** returns `(bool, error)`; it skips `os.ErrNotExist`, propagates other errors — use for `~/.cargo`, `~/.rustup`.
- **Homebrew detector already exec-grants `{prefix}/Cellar`** (`detect/homebrew.go:44`), where Homebrew cargo/rustc resolve; **Xcode detector** grants the linker (`detect/xcode.go`). These are composed in tests via `detect.Homebrew` + `detect.Xcode`.
- **Env is scrubbed and rebuilt** (`sandbox/env.go:16-38`): child gets `HOME`, `TMPDIR`, `PATH`(host PATH), plus `sandbox.Config.ExtraEnv`. cargo finds rustc via PATH and uses `$HOME/.cargo`.
- **Test conventions** (`detect/detect_test.go`): `sandboxRun` helper returns stdout only (prints both on failure); `resolveBin(t,name)` skips if absent, EvalSymlinks; `TestDetect_Git` composes Homebrew+Xcode+Git. Go 1.26.1 (`go.mod`) → `testing.T.Chdir` is available.
- **`make test`** runs `./detect/...` (no `-tags`, no `-race`); on macOS GOOS=darwin already satisfies `//go:build darwin`, so `TestDetect_Rust` runs there. **`make test-darwin`** runs `./sandbox/...` with `-race` — the new `AllowFutureDir` unit test runs there. No existing `detect/rust.go`.

## Constraints
- Do NOT grant blanket `Write|Exec` on cwd (binding decision #1 — it destroys leash's write≠exec property). The exec grant is the `target` build-output dir ONLY, and only when cwd contains `Cargo.toml`.
- Do NOT add third-party modules (repo is stdlib-only, zero `go.mod` deps, no `go.sum`).
- Do NOT introduce network in committed tests (binding decision #4): the crates.io/`cfg-if` scenario is a manual PR check, not CI.
- Do NOT change the detector-registration loop signature or other detectors' signatures.
- Do NOT reintroduce a `stopped bool`-style flag anywhere; not relevant here but keep the sandbox primitive minimal.
- Out of scope: editing README (not in the task's Done-when); rustup beyond the minimal `~/.rustup` Exec nod (decision #3); a cwd-aware detector hook for `--worktree` (documented limitation below).

## Risks
- **`--worktree` + cargo:** `os.Getwd()` returns the leash process cwd (original repo), not the child's worktree cwd (`l.Dir`), and `defaultCwdPermission` already drops the worktree cwd to read-only. So `leash --worktree … cargo test` will not build without explicit `+w`/`+x`. Mitigation: accept as a known limitation (matches the reviewer's sanctioned `os.Getwd()` mechanism); the primary `leash cargo test` case is unaffected because leash-process cwd == child cwd there. Documented in Gotchas.
- **`target` symlink correctness:** exec is checked against the resolved path. Mitigated by `ResolveFutureDir` EvalSymlink-resolving the existing dir, or resolving the parent and rejoining the base for the not-yet-existing case (mirrors `ResolveFuturePath`).
- **Hermetic test writing to real `~/.cargo`:** the detector's `AllowOptional("~/.cargo", Write)` still emits an SBPL rule permitting writes to the real `~/.cargo`, but the test redirects `CARGO_HOME` at a writable subdir of the temp crate via `Config.ExtraEnv` and sets `CARGO_NET_OFFLINE=1` (WP4), so no write actually lands in the real home and the test never hits the network. Sanctioned by decision #4.

## Work Packages

### 1. Add a future-directory subpath grant primitive to `sandbox`
**Steps:**
1. In `sandbox/path.go`, add `func ResolveFutureDir(raw, home string) (Path, error)`, modeled on `ResolveFuturePath` but always returning `IsDir: true`: expand via `expandAbs`; if `filepath.EvalSymlinks(expanded)` succeeds, `os.Stat` it and return an error `resolve future dir %q: not a directory` if it is not a directory, else `Path{Resolved: resolved, IsDir: true}`; otherwise EvalSymlink-resolve the parent (`filepath.Dir(expanded)`), `os.Stat` the parent (wrap failures with `%w`), and return `Path{Resolved: filepath.Join(parent, filepath.Base(expanded)), IsDir: true}`.
2. In `sandbox/profile.go`, add `func (p *ToolProfile) AllowFutureDir(raw string, perm Permission) error`, mirroring `AllowFuture` (`profile.go:64-71`) but calling `ResolveFutureDir`; append the entry, wrap errors as `profile %q: %w` with `p.name`.
3. In `sandbox/path_test.go`, add table cases for `ResolveFutureDir` following the existing style: (a) a non-existent subdir of an existing `t.TempDir()` returns `IsDir=true` and `Resolved == <resolvedTmp>/target`; (b) an existing directory returns `IsDir=true`; (c) an existing regular file returns an error.
4. Confirm the primitive emits a subpath rule by inspecting `emitEntries` behavior (no code change needed — `IsDir=true` already routes to `subpath` at `builder.go:236-239`).

**Done when:**
- `go test -tags darwin -race ./sandbox/ -run TestResolveFutureDir -v` passes.
- `grep -n "AllowFutureDir" sandbox/profile.go` and `grep -n "ResolveFutureDir" sandbox/path.go` each return a hit.

### 2. Add the `detect/rust.go` detector
**Steps:**
1. Create `detect/rust.go` with `//go:build darwin`, `package detect`, importing `errors`, `fmt`, `os`, `os/exec`, `path/filepath`, and `github.com/aka-rider/leash/sandbox`; declare `func Rust(p sandbox.ToolProfile) (sandbox.ToolProfile, error)`.
2. `exec.LookPath("cargo")`; on `errors.Is(err, exec.ErrNotFound)` return `p, nil`; on other error wrap and return. `filepath.EvalSymlinks` the path (keep original on resolve error), then `p.Allow(filepath.Dir(resolved), sandbox.Exec)` — covers Homebrew (Cellar) and standalone/rustup bin dirs (decision #2).
3. Grant the toolchain caches, discarding the `(bool, error)` first return via `_, err :=` (matches `detect/golang.go`): `_, err := p.AllowOptional("~/.cargo", sandbox.Write)` (gap #1, CARGO_HOME registry writes) and `_, err = p.AllowOptional("~/.rustup", sandbox.Exec)` (decision #3, rustup toolchain binaries + std read); wrap and return each non-nil `err`.
4. Compute the build-output dir: `cwd, err := os.Getwd()` (wrap+return on error); if `os.Stat(filepath.Join(cwd, "Cargo.toml"))` fails (not in a crate), return `p, nil` — no target grant. Add a one-line comment justifying `os.Getwd()` (fixed detector signature; equals child cwd for plain invocations).
5. Resolve the target dir: `target := os.Getenv("CARGO_TARGET_DIR")`; if empty use `filepath.Join(cwd, "target")`; if non-empty and `!filepath.IsAbs(target)` use `filepath.Join(cwd, target)`. Grant it with `p.AllowFutureDir(target, sandbox.Exec)` (subpath exec, future-tolerant, symlink-resolved via WP1); wrap the error. Return `p, nil`.

**Done when:**
- `go build ./...` succeeds and `ls detect/rust.go` exists.
- `go vet ./...` is clean for the new file.

### 3. Register `detect.Rust` in the unconditional probe list
**Steps:**
1. In `execute_darwin.go`, add `detect.Rust` to the slice literal at lines 50-51 (e.g. after `detect.Python`).

**Done when:**
- `grep -n "detect.Rust" execute_darwin.go` returns a hit.
- `go build ./...` succeeds.

### 4. Add hermetic `TestDetect_Rust` to `detect/detect_test.go`
**Steps:**
1. Add a `setupRustCrate(t *testing.T) string` helper: create a symlink-resolved `t.TempDir()`; write `Cargo.toml` (`[package] name="leashtest" version="0.0.0" edition="2021"` plus `[lib]` pointing at `src/lib.rs`), `src/lib.rs` containing one `#[cfg(test)] mod tests { #[test] fn it_works() { assert_eq!(2+2,4); } }`, and `build.rs` with `fn main(){ println!("cargo:warning=BUILDRS_RAN"); }`; return the dir.
2. Add `TestDetect_Rust`: `cargoBin := resolveBin(t, "cargo")` (skips if absent); `crateDir := setupRustCrate(t)`; `t.Chdir(crateDir)` so the detector's `os.Getwd()` sees the crate (grants `<crateDir>/target` exec).
3. Build the profile: `p := sandbox.NewToolProfile("detect", home)`; compose `detect.Homebrew`, `detect.Xcode`, then `detect.Rust` (Rust last, after `t.Chdir`); fail on any error.
4. Build the grant profile: `grant.Allow(filepath.Dir(cargoBin), sandbox.Exec)` and `grant.Allow(crateDir, sandbox.Write)` (writable cwd → subpath write covers `target`).
5. Construct `sandbox.Config{Profiles: []sandbox.Snapshot{grant.Snapshot(), p.Snapshot()}, ExtraEnv: map[string]string{"CARGO_HOME": filepath.Join(crateDir, "cargohome"), "CARGO_NET_OFFLINE": "1"}}` (note `ExtraEnv` is a `map[string]string`, verified at `sandbox/sandbox.go:20` — a `[]string` literal is a compile error); run `exec.Command(cargoBin, "test")` with `cmd.Dir = crateDir`, capturing both stdout and stderr buffers inline (do not reuse `sandboxRun`, which discards stderr); use a 120 s `context.WithTimeout`.
6. Assert exit code 0, that combined output contains `BUILDRS_RAN` (build-script warning, stderr), and that stdout contains `test result: ok` (test binary executed → gap #2 exercised).

**Done when:**
- `go test ./detect/ -run TestDetect_Rust -v` passes when cargo is installed (or prints `SKIP` when not), with no other `detect` test regressing.

### 5. Update the detector enumeration in `CLAUDE.md`
**Steps:**
1. In `CLAUDE.md` line 36, change the list `(claude, docker, git, go, homebrew, npm, python, xcode)` to include `rust` in alphabetical position: `(claude, docker, git, go, homebrew, npm, python, rust, xcode)`.

**Done when:**
- `grep -n "python, rust, xcode" CLAUDE.md` returns a hit.

## Verification
Run from the worktree root:
- `go build ./...` — exit 0.
- `make lint` — `gofmt -l .` prints nothing and `go vet ./...` exits 0.
- `make test` — passes (includes `./detect/...` → `TestDetect_Rust`).
- `make test-darwin` — passes with `-race` (includes `./sandbox/...` → `TestResolveFutureDir`).
- `make test-integration` — passes.
- Targeted: `go test -tags darwin -race ./sandbox/ -run TestResolveFutureDir -v` and `go test ./detect/ -run TestDetect_Rust -v`.
- Manual PR acceptance (non-CI, network): in a scratch crate with `[dependencies] cfg-if = "1.0"` and a `build.rs` printing a warning, run `leash cargo test` (no extra flags); confirm exit 0 and that both the build-script warning and `test result:` appear.

## Assumptions
- `cargo` is the detection anchor (per decision #2); a standalone install's cargo/rustc share a bin dir, so the resolved-bin-dir exec grant covers rustc without a separate `rustc` probe. Reviewer confirms no separate `rustc` LookPath is wanted.
- README is not edited (task Done-when lists only `CLAUDE.md` line 36).
- Adding the `AllowFutureDir`/`ResolveFutureDir` sandbox primitive is an acceptable in-scope mechanism (decision #1 grants mechanism latitude); it is required because `AllowFuture` emits a `literal` rule that cannot cover future binaries under `target/`.
- The hermetic test overrides `CARGO_HOME` to a temp dir and sets `CARGO_NET_OFFLINE=1`, so it does not depend on a real `~/.cargo` existing and never hits the network.

## Gotchas
- **`Config.ExtraEnv` is `map[string]string`** (`sandbox/sandbox.go:20`): write test env overrides as `map[string]string{"CARGO_HOME": …, "CARGO_NET_OFFLINE": "1"}`. A `[]string{"CARGO_HOME=…"}` literal is a compile error that would keep `TestDetect_Rust` from ever running (false-green trap on the `make test` Done-when).
- **`AllowOptional` returns `(bool, error)`** (`sandbox/profile.go:89`): discard the bool with `_, err :=` exactly as `detect/golang.go` does; a single-value assignment will not compile.
- **`Write` ≠ `Exec` in SBPL** (`sbplOps`): granting `target` Write would not make its binaries executable — the grant must carry the `Exec` bit. Do not "simplify" it to a single cwd `Write|Exec`.
- **Future path → `literal`, future dir → `subpath`:** using `AllowFuture` (not `AllowFutureDir`) for `target` would silently emit a literal rule that passes the build but denies exec of the compiled binary at runtime — a false-green trap. Use `AllowFutureDir`.
- **Exec grants must be symlink-resolved** (repo invariant, `CLAUDE.md`): `ResolveFutureDir` handles this for both the existing and not-yet-existing cases; the detector must not hand-roll an unresolved path.
- **`os.Getwd()` limitation under `--worktree`:** the leash process cwd is the original repo, not the child's worktree, and the worktree cwd is read-only by default — `--worktree … cargo` needs explicit `+w`/`+x` and is out of scope here.
- **`sandboxRun` discards stderr:** the build-script `cargo:warning=` lands on stderr, so `TestDetect_Rust` must capture both buffers itself rather than reuse the shared helper.
