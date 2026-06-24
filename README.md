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
leash +w . claude --print "write a go server"   # make cwd writable
leash +r ~/data python3 analyse.py              # extra read-only path
leash -x /usr/bin/curl +w . go test ./...       # deny curl, allow cwd writes
leash --no-network +w . go test ./...           # block all outbound network
leash +w . -- go build -v ./...                 # pass child flags through --
```

| Directive | Effect |
|-----------|--------|
| `+r PATH` | grant read on PATH (must exist) |
| `+w PATH` | grant read + write on PATH (must exist) |
| `+x PATH` | grant exec on PATH (must exist) |
| `-r PATH` | deny read on PATH — overrides all allows |
| `-w PATH` | deny write on PATH — overrides all allows |
| `-x PATH` | deny exec on PATH — overrides all allows |

The current directory is always readable by default; use `-r .` to remove it.
Nothing is writable by default except `/tmp` and `~/Library/Caches`.

Options: `--no-network`, `--config PATH`, `--help`.

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
    Writes:  []string{"."},
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
