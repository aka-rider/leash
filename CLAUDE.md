# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`leash` wraps any command in a macOS Seatbelt (`sandbox-exec`) sandbox with a deny-by-default SBPL profile. `leash-trace` is a companion binary that runs the same way and captures kernel sandbox denials. The root package is also a public Go library (`github.com/aka-rider/leash`). macOS-only at runtime; Go 1.26+.

## Commands

```sh
make build              # builds bin/leash and bin/leash-trace
make test               # platform-independent tests: ./internal/cli/... ./detect/... .
make test-darwin        # darwin-tagged tests with -race: ./sandbox/... ./cmd/leash/... .
make test-integration   # -tags 'darwin integration': actually spawns sandbox-exec / log stream
make lint               # gofmt check + go vet
make install            # copies binaries to /usr/local/bin
```

Single test: `go test -race -run TestName ./sandbox/`. Integration tests (`cmd/trace/tracer_test.go`) additionally need `-tags integration`.

## Hard constraints

- **Stdlib only.** `go.mod` has zero dependencies and there is no `go.sum`. Do not add third-party modules — even `golang.org/x/*` (e.g. `isTerminal` in `sandbox/sandbox.go` uses a raw ioctl instead of `x/term`).
- **CLI-only configuration.** No config file, no `LEASH_*` env vars. Environment injection into the sandbox happens only via `--env KEY=VALUE` / `--proxy-env NAME`.
- **Grant paths must exist.** `ToolProfile.Allow` resolves (`~` expansion, symlinks, stat) and errors on failure — never swallows.
- Uses Go 1.26 APIs, e.g. `errors.AsType[*exec.ExitError](err)`.

## Architecture

Execution flow: `cmd/leash/main.go` → `internal/cli.Configure` (argv → `leash.Leash`) → `leash.Execute` (`execute_darwin.go`) → builds profile snapshots → `sandbox.New` compiles SBPL to a temp `.sb` file → `Sandbox.Run` re-writes the `exec.Cmd` to `sandbox-exec -f profile.sb <resolved-bin> args…`.

- **Root package `leash`** — public API only: the `Leash` struct, `Execute(ctx, l)`, `ExitCode(err)`. All real logic is behind `//go:build darwin`; `execute_other.go` returns `ErrUnsupported` elsewhere.
- **`internal/cli`** — argv parsing, deliberately no build tag so it tests on any platform. Two-state machine: leash options → (first bare token or `--`) → child argv verbatim. `+r/+w/+x` grant, `-r/-w/-x` deny. After `--worktree NAME` the first-bare-token shortcut is disabled: an explicit `--` is required before the command.
- **`sandbox`** — `ToolProfile` accumulates allow/deny entries and env vars; `ProfileBuilder` emits the final SBPL (system base rules + snapshots). The child env is scrubbed and rebuilt from scratch (`BaseEnv` + `MergeEnv`); nothing from the host leaks unless a detector or `--env`/`--proxy-env` adds it.
- **`detect`** — one file per tool (claude, docker, git, go, homebrew, npm, python, xcode). Every detector is probed unconditionally on each run from `execute_darwin.go`; a missing binary is a silent no-op, any other failure is fatal.
- **`cmd/leash`** — CLI entry plus `--worktree`: creates a detached git worktree at `<repo-parent>/<NAME>`, sets it as the child's cwd (write-granted), keeps the original repo read-only, and write-grants only the `.git` internals a linked worktree needs (`worktrees/<name>`, `objects`, `refs`, `logs`) — top-level `.git` (config, hooks) stays read-only. Worktree names must be a single path component (traversal guard).
- **`cmd/trace`** — `leash-trace` shares `internal/cli`, generates a random nonce as `Leash.DenyTag`, and runs a `log stream` watcher filtered on that nonce to correlate kernel denials with this run. Trace failure degrades gracefully — it never blocks the sandboxed program.

Profile layering in `execute_darwin.go`: grants profile (implicit cwd + user `+` directives), then optional deny profile, then the detect profile. SBPL is **last-match-wins**, and `ProfileBuilder.Build` emits deny entries after all allows — that ordering is what makes `-r/-w/-x` override everything, including detector grants.

## Hard-won invariants (do not "simplify" these)

- **Seatbelt checks `process-exec` against the symlink-RESOLVED path.** Any exec grant (and the binary actually exec'd) must use `filepath.EvalSymlinks` first. This pattern repeats in `execute_darwin.go`, `detect/golang.go`, `detect/python.go`, `detect/claude.go`.
- **Process-group/tty handling in `sandbox/sandbox.go`** (`Setpgid`, `Foreground`/`Ctty` for tty stdin, the `TIOCSPGRP` reclaim with `SIGTTOU` ignored, and exec'ing `sandbox-exec` directly instead of via `/bin/sh`) each fix a specific hang or signal bug documented in the comments at those sites.
- **Detector probes run with `Setsid`** (`detect/probe.go`): a same-session interpreted child can wedge a later sandboxed child's exit on signal death.
- By default only cwd is read+write; `/tmp` and `~/Library/Caches` writable; everything else denied. `defaultCwdPermission` drops cwd to read-only when `Leash.Dir` points elsewhere (the `--worktree` case).
