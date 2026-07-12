# leash

macOS seatbelt sandbox for developer tools. Wraps any command in `sandbox-exec`
with a deny-by-default profile, then grants back only the paths and capabilities
you explicitly allow.

`leash-trace` is a companion binary that runs a command the same way and captures
kernel sandbox denials to a log file.

## Installation

```sh
brew tap aka-rider/tap
brew install --cask leash
```

Or build from source (requires Go 1.26+, macOS only):

```sh
git clone https://github.com/aka-rider/leash
cd leash && make install
```

## Usage

```
leash [options] [+w PATH] [+r PATH] [+x PATH] [-w PATH] [-r PATH] [-x PATH] [--] <command> [args...]
```

```sh
leash claude --print "write a go server"        # cwd is writable by default
leash claude                                    # interactive commands work too (tty is proxied transparently)
leash +r ~/data python3 analyse.py              # extra read-only path
leash -x /usr/bin/curl go test ./...            # deny curl, cwd stays writable
leash --no-network go test ./...                # remove all network access (inbound and outbound)
leash --env FOO=bar -- sh -c 'echo $FOO'        # set an extra env var in the sandbox
leash --proxy-env HTTP_PROXY -- curl example.com # forward a var from the host env
leash -w . -- go build -v ./...                 # read-only cwd, pass child flags through --
leash --worktree my-fix -- go test ./...        # run in a fresh git worktree named my-fix (git add/commit work inside it)
leash-trace go test ./...                        # run like leash; log kernel denials to ./leash-trace.log
```

| Directive | Effect |
|-----------|--------|
| `+r PATH` | grant read on PATH (must exist) |
| `+w PATH` | grant read + write on PATH (must exist) |
| `+x PATH` | grant exec on PATH (must exist) |
| `-r PATH` | deny read on PATH — overrides all allows |
| `-w PATH` | deny write on PATH — overrides all allows |
| `-x PATH` | deny exec on PATH — overrides all allows |

The current directory is read+write by default; use `-w .` to make it
read-only, or `-r . -w .` to remove all access. `--worktree NAME` is the
exception — it keeps the original directory read-only and grants write on
the new worktree instead, and also wires up the main repo's `.git` internals
(`worktrees/<NAME>`, `objects`, `refs`, `logs`, and the `packed-refs` file +
its lock/tempfile) so `git add`/`git commit` work from inside the worktree;
the top-level `.git` (config, hooks) stays read-only, so e.g. `git branch -d`
may print a benign "could not lock config file" warning. NAME is mandatory
and must be followed by `--` before the command (e.g.
`leash --worktree my-fix -- go test ./...`).
Nothing else is writable by default except `/tmp` and `~/Library/Caches`.

Options: `--worktree NAME`, `--no-network`, `--env KEY=VALUE`, `--proxy-env NAME`, `--help`.
`leash` has no config file and reads no `LEASH_*` environment variables — everything
is CLI-only; `--env`/`--proxy-env` are the CLI replacement for injecting environment
variables into the sandbox.

Interactive commands (e.g. `leash claude` with no arguments, or `leash cat`) work
normally: stdin/stdout/stderr are proxied transparently, including terminal control
(Ctrl+C goes straight to the child).

## ⚠ Warning: Shell redirection and piping escape the sandbox

Redirection (`>`, `>>`) and pipes (`|`) typed on the same command line are set up by your *outer* shell, which opens the file or forks the piped-to process **before** `leash` (and therefore `sandbox-exec`) ever starts:

```sh
leash -w . -- echo 'hello' > file.txt      # zsh writes file.txt itself, before leash runs
leash -w . -- echo 'hello' | tee file.txt  # tee is a separate, unsandboxed process forked by the outer shell
```

To have redirection or piping enforced by the sandbox, push it inside the command leash executes, e.g. with `sh -c`:

```sh
leash -w . -- sh -c 'echo 'hello' > file.txt'     # denied: the write happens inside the sandbox
leash -w . -- sh -c 'echo 'hello' | tee file.txt' # denied: tee runs inside the sandbox too
```

## Environment

The sandboxed child gets a **scrubbed**, built-from-scratch environment — none of
the host's variables leak in except what's listed below.

Always set: `PATH` (inherited from the host, or `/usr/bin:/bin` if the host has
none), `HOME`, `TMPDIR`, `LANG`/`LC_ALL` (`en_US.UTF-8`), `USER`/`LOGNAME`, `SHELL`
(`/bin/sh`), and the terminal vars `TERM`, `COLORTERM`, `FORCE_COLOR`.
Proxied from the host only when present: `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`,
`TERM_FEATURES`. Always forwarded: every host `XPC_*` variable (system daemon IPC,
e.g. Keychain).

Everything else is dropped. Inject it explicitly with `--env KEY=VALUE`, or forward
it from the host with `--proxy-env NAME` — `--proxy-env` errors out if the named
variable is absent from the host environment.

## Network

By default outbound network access is fully allowed; inbound is allowed only on
`localhost`. `--no-network` removes the entire network allowance — both inbound
and outbound.

## How it works

Each invocation compiles an SBPL (Sandbox Profile Language) policy from the active
grants and denials and passes it to `sandbox-exec`. The child process runs inside
that policy; any access not explicitly allowed is denied by the kernel.

Every run also unconditionally probes a fixed set of developer tools — claude,
docker, git, go, homebrew, npm, python, rust, xcode — and grants each detected
tool its own paths and capabilities.

## Tracing denials

`leash-trace` runs the command exactly like `leash`, plus a `log stream` watcher
that correlates kernel sandbox denials with the run and writes one line per
denial as `<category>: <target>` — `target` is a file path, `host:port`, or mach
service name depending on the category. Categories: `read`, `write`, `exec`,
`network`, `mach`, `ipc`, `other`.

Output goes to `./leash-trace.log` by default. `--trace-file PATH` redirects it;
`--trace-file -` writes to stderr instead. A real `--trace-file` path refuses to
overwrite an existing file and errors with `trace file already exists: … (delete
it or choose a different name)`.

`leash-trace` needs the macOS `log` binary. If it's missing, or `log stream` fails
to start, `leash-trace` degrades gracefully: it runs the child directly (without
tracing) and writes a `# trace unavailable: …` note to the sink, preserving the
child's exit code either way.

After the child exits, `leash-trace` drains the log stream for about 3 seconds to
catch denials that arrive late, so traced runs take roughly 3 seconds longer than
the same command under plain `leash`.

## Use as a library

```go
import (
    "context"
    "os"

    leash "github.com/aka-rider/leash"
)

l := leash.Leash{
    Program: "go",
    Args:    []string{"test", "./..."},
    Network: true,
    Stdout:  os.Stdout,
    Stderr:  os.Stderr,
}
os.Exit(leash.ExitCode(leash.Execute(context.Background(), l)))
```

`Execute(ctx, l)` returns `nil` on success, `*exec.ExitError` on non-zero exit.
`ExitCode(err)` maps that to a shell exit code: 0 / child code / 128+signal / 1.
On non-macOS platforms, `Execute` returns `leash.ErrUnsupported`.

## License

MIT
