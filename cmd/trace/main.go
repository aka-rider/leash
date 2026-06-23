//go:build darwin

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
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
	if len(parsed.Command) == 0 {
		if parsed.Help {
			fmt.Print(cli.Usage())
			return 0
		}
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

	err = Run(ctx, Options{Nonce: nonce, Sink: sink}, func(ctx context.Context) error {
		return leash.Execute(ctx, l)
	})
	return leash.ExitCode(err)
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
