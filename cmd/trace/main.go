//go:build darwin

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	leash "github.com/aka-rider/leash"
	"github.com/aka-rider/leash/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	l, parsed, err := cli.Configure(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "leash-trace: %v\nRun 'leash-trace --help' for usage.\n", err)
		return 2
	}
	if parsed.Help {
		fmt.Print(cli.Usage())
		return 0
	}
	if len(parsed.Command) == 0 {
		fmt.Fprint(os.Stderr, cli.Usage())
		return 2
	}

	// Generate per-run nonce embedded in the SBPL deny message so the tracer can
	// filter exactly this run's denials from the kernel log.
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		fmt.Fprintf(os.Stderr, "leash-trace: generate nonce: %v\n", err)
		return 1
	}
	nonce := "leash-" + hex.EncodeToString(buf[:])
	l.DenyTag = nonce

	sink, err := openTraceSink(parsed.TraceFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "leash-trace: trace: %v\n", err)
		return 1
	}
	if sink != os.Stderr {
		defer func() { _ = sink.Close() }()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	execErr := Run(ctx, Options{Nonce: nonce, Sink: sink}, func(ctx context.Context) error {
		return leash.Execute(ctx, l)
	})
	if execErr != nil {
		// *exec.ExitError means the child ran and exited non-zero; it already
		// printed its own diagnostics. Anything else is a leash-side setup
		// failure that would otherwise exit silently. Library errors already
		// carry a "leash: " prefix — trim it so the message isn't doubled.
		if _, ok := errors.AsType[*exec.ExitError](execErr); !ok {
			fmt.Fprintf(os.Stderr, "leash-trace: %s\n", strings.TrimPrefix(execErr.Error(), "leash: "))
		}
	}
	return leash.ExitCode(execErr)
}

// openTraceSink opens the trace output destination.
// "-" maps to stderr; all other paths use O_EXCL (error if file exists).
func openTraceSink(path string) (*os.File, error) {
	if path == "" {
		path = "./leash-trace.log"
	}
	if path == "-" {
		return os.Stderr, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("trace file already exists: %s (delete it or choose a different name)", path)
		}
		return nil, fmt.Errorf("open trace file %s: %w", path, err)
	}
	return f, nil
}
