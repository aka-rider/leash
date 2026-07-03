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
leash --no-network go test ./...                # block all outbound network
leash --env FOO=bar -- sh -c 'echo $FOO'        # set an extra env var in the sandbox
leash --proxy-env HTTP_PROXY -- curl example.com # forward a var from the host env
leash -w . -- go build -v ./...                 # read-only cwd, pass child flags through --
leash --worktree my-fix -- go test ./...        # run in a fresh git worktree named my-fix (git add/commit work inside it)
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

## How it works

Each invocation compiles an SBPL (Sandbox Profile Language) policy from the active
grants and denials and passes it to `sandbox-exec`. The child process runs inside
that policy; any access not explicitly allowed is denied by the kernel.

`leash-trace` attaches a `log stream` watcher to correlate kernel denials with the
run and writes them to `./leash-trace.log` as `<category>: <path>` lines. Use
`--trace-file PATH` to redirect, or `--trace-file -` for stderr.

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
