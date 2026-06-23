# leash

macOS seatbelt sandbox for developer tools. Wraps any command in `sandbox-exec`
with a deny-by-default profile, then grants back only the paths and capabilities
you explicitly allow.

## Installation

```sh
brew tap aka-rider/tap
brew install leash
```

Or build from source (requires Go 1.22+, macOS only):

```sh
git clone https://github.com/aka-rider/leash
cd leash && make install   # installs to /usr/local/bin/sbx
```

## Usage

```
sbx [options] [+r PATH] [+w PATH] [+x PATH] [--] <command> [args...]
```

```sh
# Run claude with the current directory writable
sbx +w . claude --print "write a go server"

# Read-only access to an extra directory
sbx +r ~/data python3 analyse.py

# Block network access entirely
sbx --no-network +w . go test ./...

# Pass child flags through without ambiguity
sbx +w . -- go build -v ./...
```

## Grant flags

| Flag | Grants |
|------|--------|
| `+r PATH` | `file-read*` on PATH (must exist) |
| `+w PATH` | `file-read*` + `file-write*` on PATH (must exist; does **not** grant exec) |
| `+x PATH` | `file-read*` + `file-map-executable` + `process-exec` on PATH (must exist) |

All three flags require the path to exist at run time — a missing path is a hard
error rather than a silent skip. This is intentional: `+w /typo` should never
produce a confusingly unconstrained sandbox.

To make a directory both writable and executable, use both `+w` and `+x`:

```sh
sbx +w ~/bin +x ~/bin my-build-tool
```

## Options

| Flag | Description |
|------|-------------|
| `--workspace PATH` | Sandbox workspace root (default: current directory). The workspace is always readable; writable only with `+w .` or `--workspace`. |
| `--no-network` | Deny all outbound network connections. |
| `--trace` | Capture denied operations to a trace file (see `--trace-file`). |
| `--trace-file PATH` | Trace output destination (default: `./sbx-trace.log`; `-` for stderr). Fails with an error if the file already exists. |
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
```

**Environment variables** override the config file for scalar values:

| Variable | Equivalent flag |
|----------|----------------|
| `LEASH_WORKSPACE` | `--workspace` |
| `LEASH_NO_NETWORK=1` | `--no-network` |
| `LEASH_DETECT` | `--detect` (comma-separated) |
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

## --trace

`--trace` starts a `log stream` watcher that captures kernel sandbox denials for
this run and writes grepable lines to the trace file. Each line has the format:

```
<category>: <path>
```

Categories: `read`, `write`, `exec`, `network`, `mach`, `ipc`, `other`.

Example output:
```
write: /private/var/folders/.../T/work/out.bin
exec: /usr/local/bin/node
network: (outbound)
```

The trace file is opened with `O_EXCL` — if it already exists, `sbx` exits with an
error. Use `--trace-file -` to write denials to stderr instead.

Tracing requires the `log` binary (present on all macOS systems) and elevated log
verbosity. It adds roughly 100–200 ms to startup while the kernel log stream
initialises.

## Exit codes

`sbx` preserves the child's exit code exactly. If the child is killed by a signal,
the exit code is `128 + <signal number>` (POSIX convention).
