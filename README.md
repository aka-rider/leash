# leash

macOS seatbelt sandbox for developer tools. Wraps any command in `sandbox-exec`
with a deny-by-default profile, then grants back only the paths and capabilities
you explicitly allow.

leash is **library-first**: the `leash` binary is a thin wrapper around the
`github.com/aka-rider/leash` Go package (see [Use as a library](#use-as-a-library)).
A companion binary, `leash-trace`, runs a command the same way but also captures
the kernel sandbox denials it triggers.

## Installation

```sh
brew tap aka-rider/tap
brew install leash
```

Or build from source (requires Go 1.26+, macOS only):

```sh
git clone https://github.com/aka-rider/leash
cd leash && make install   # installs leash and leash-trace to /usr/local/bin
```

## Usage

```
leash [options] [+r PATH] [+w PATH] [+x PATH] [--] <command> [args...]
```

```sh
# Run claude with the current directory writable
leash +w . claude --print "write a go server"

# Read-only access to an extra directory
leash +r ~/data python3 analyse.py

# Block network access entirely
leash --no-network +w . go test ./...

# Pass child flags through without ambiguity
leash +w . -- go build -v ./...
```

## Grant flags

| Flag | Grants |
|------|--------|
| `+r PATH` | `file-read*` on PATH (must exist) |
| `+w PATH` | `file-read*` + `file-write*` on PATH (must exist; does **not** grant exec) |
| `+x PATH` | `file-read*` + `file-map-executable` + `process-exec` on PATH (must exist) |

All three flags require the path to exist at run time — a missing path is a hard
error rather than a silent skip. This is intentional: `+w /typo` should never
produce a confusingly unconstrained sandbox. Relative paths (including `.`) are
resolved against the current directory.

To make a directory both writable and executable, use both `+w` and `+x`:

```sh
leash +w ~/bin +x ~/bin my-build-tool
```

## Options

| Flag | Description |
|------|-------------|
| `--workspace PATH` | Sandbox workspace root (default: current directory). The workspace is always readable; writable only with `+w .` or `--workspace`. |
| `--no-network` | Deny all outbound network connections. |
| `--detect LIST` | Comma-separated list of tool detectors to run automatically (see Detectors). |
| `--config PATH` | Config file path (default: searches `.leash.yaml`, `leash.yaml`, then `~/.config/leash/leash.yaml`). |
| `--help`, `-h` | Print usage. |

## Configuration file

Leash merges settings from multiple sources. Later sources win for scalar values;
lists are **unioned** across sources.

**Precedence (lowest → highest):** built-in defaults → config file → `LEASH_*` env vars → CLI flags

```yaml
# .leash.yaml — project config (or ~/.config/leash/leash.yaml for user global)

# Which tool environments to auto-allow (overrides the default set when present)
detect:
  - homebrew
  - git
  - go

# Extra read-only paths (unioned with +r CLI grants)
read:
  - ~/.config/myapp

# Extra read-write paths (unioned with +w grants; no exec)
write:
  - ~/wrk/project

# Extra exec paths (unioned with +x grants)
exec:
  - ~/bin

# Sandbox workspace root
workspace: ~/wrk/project

# Allow outbound network (default: true; false = same as --no-network)
network: true

# Extra environment variables for the child
extra_env:
  MY_FLAG: "1"

# Host environment variable NAMES to forward into the sandbox (must exist)
proxy_env:
  - HTTPS_PROXY
```

**Environment variables** override the config file for scalar values:

| Variable | Equivalent flag |
|----------|----------------|
| `LEASH_WORKSPACE` | `--workspace` |
| `LEASH_NO_NETWORK=1` | `--no-network` |
| `LEASH_DETECT` | `--detect` (comma-separated) |
| `LEASH_READ` / `LEASH_WRITE` / `LEASH_EXEC` | extra `+r` / `+w` / `+x` paths |
| `LEASH_PROXY_ENV` | `proxy_env` (comma-separated) |
| `LEASH_CONFIG` | `--config` |

## Detectors

Detectors auto-allow the paths a tool needs to run. Pass `--detect homebrew,git`
or set `detect:` in the config.

| Name | What it allows |
|------|----------------|
| `homebrew` | Homebrew prefix, Cellar, bin, lib |
| `git` | System git, credential helpers, user `.gitconfig` |
| `go` | GOROOT, GOPATH, module cache |
| `npm` | Node global prefix, npm cache |
| `python` | Active Python interpreter and site-packages |
| `docker` | Docker socket, CLI, config |
| `xcode` | Xcode.app Developer directory |
| `claude` | Claude CLI binary and its config directory |

Unknown detector names are a hard error.

## Tracing denials (leash-trace)

`leash-trace` runs a command exactly like `leash`, but also starts a `log stream`
watcher that captures this run's kernel sandbox denials and writes grepable lines
to a trace file. It accepts the same grant flags and options as `leash`.

```sh
leash-trace +w . go test ./...
```

Each line has the format `<category>: <path>`, where category is one of `read`,
`write`, `exec`, `network`, `mach`, `ipc`, `other`:

```
write: /private/var/folders/.../T/work/out.bin
exec: /usr/local/bin/node
network: (outbound)
```

The trace file defaults to `./leash-trace.log` and is opened with `O_EXCL` — if it
already exists, `leash-trace` exits with an error. Use `--trace-file -` to write
denials to stderr instead, or `--trace-file PATH` to choose the destination.

Tracing requires the `log` binary (present on all macOS systems). It adds roughly
100–200 ms to startup while the kernel log stream initialises.

## Use as a library

Build a command with `New`, configure it with the fluent `With*` methods, then call
`Execute`. `Execute` returns `nil` on success and an `*exec.ExitError` on a non-zero
exit; `ExitCode` maps any `Execute` error to a process exit code.

```go
package main

import (
	"context"
	"os"

	leash "github.com/aka-rider/leash"
)

func main() {
	l := leash.New("go", "test", "./...").
		WithAutodetectFrameworks(). // allow installed toolchains (go, git, ...)
		WithWrite(".").             // make the current directory writable
		WithStdout(os.Stdout).
		WithStderr(os.Stderr)

	os.Exit(leash.ExitCode(l.Execute(context.Background())))
}
```

| Method | Effect |
|--------|--------|
| `New(program, args...)` | Create a `*Leash` for a program + args (network enabled by default). |
| `WithAutodetectFrameworks()` | Auto-allow installed toolchains (homebrew, git, go, npm, python, docker, xcode). |
| `WithDetect(names...)` | Run specific detectors (including `claude`). |
| `WithRead` / `WithWrite` / `WithExec(paths...)` | Grant read / read+write / exec on paths (must exist; `.` and relative paths resolve against cwd). |
| `WithWorkspace(dir)` | Set the sandbox root and child working directory (default: cwd). |
| `WithNetwork(bool)` | Enable or disable outbound network (default: enabled). |
| `WithEnv(key, value)` | Set an extra environment variable for the child. |
| `WithProxyEnv(names...)` | Forward named host environment variables into the sandbox. |
| `WithStdin` / `WithStdout` / `WithStderr(...)` | Wire stdio (`io.Reader` / `io.Writer`; unset = discard). |
| `WithDenyTag(tag)` | Set the SBPL deny-message tag (used by `leash-trace` for denial correlation). |
| `Execute(ctx)` | Run the command sandboxed. |
| `ExitCode(err)` | Map an `Execute` error to a process exit code (0 / code / 128+signal / 1). |

Sandboxing is macOS-only; on other platforms `Execute` returns `leash.ErrUnsupported`.

## Exit codes

`leash` preserves the child's exit code exactly. If the child is killed by a signal,
the exit code is `128 + <signal number>` (POSIX convention).
