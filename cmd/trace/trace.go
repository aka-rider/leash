//go:build darwin

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const defaultDrain = 3 * time.Second

// Options configures a Run call.
type Options struct {
	// Nonce is embedded in the SBPL deny message and used to filter the kernel log.
	// Must match the DenyMessage set on sandbox.Config.
	Nonce string
	// Sink is where human-readable denial lines are written.
	Sink io.Writer
	// DrainFor is how long Run waits after fn returns to collect lagging denials.
	// Defaults to 3 seconds if zero.
	DrainFor time.Duration
}

// Run starts `log stream` filtered on opts.Nonce, waits for the stream to be
// live, calls fn, drains for DrainFor to collect late denials, then shuts down.
// Returns fn's error unchanged. Trace failures never block or kill the sandboxed
// program — if `log` is unavailable, fn is called directly.
func Run(ctx context.Context, opts Options, fn func(context.Context) error) error {
	if opts.Nonce == "" {
		return fmt.Errorf("trace.Run: nonce is required")
	}
	if opts.Sink == nil {
		opts.Sink = os.Stderr
	}
	drain := opts.DrainFor
	if drain == 0 {
		drain = defaultDrain
	}

	// Verify `log` is available; degrade gracefully if not.
	logBin, err := exec.LookPath("log")
	if err != nil {
		fmt.Fprintf(opts.Sink, "# trace unavailable: log binary not found: %v\n", err)
		return fn(ctx)
	}

	predicate := fmt.Sprintf("eventMessage CONTAINS %q", opts.Nonce)
	logCmd := exec.Command(logBin, "stream", "--style", "ndjson", "--level", "debug", "--predicate", predicate)
	logCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := logCmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(opts.Sink, "# trace unavailable: log stream pipe: %v\n", err)
		return fn(ctx)
	}
	if err := logCmd.Start(); err != nil {
		fmt.Fprintf(opts.Sink, "# trace unavailable: log stream start: %v\n", err)
		return fn(ctx)
	}

	// G1: scan log stream stdout, deduplicate, write to sink.
	// Readiness: log stream prints a plain-text banner as its FIRST line before any
	// events. ParseRecord returns (nil, nil) for non-JSON lines. The first nil
	// signals the stream is live.
	var (
		scanWg   sync.WaitGroup
		readyWg  sync.WaitGroup
		readOnce sync.Once
		seen     = make(map[Denial]struct{})
	)
	readyWg.Add(1)
	scanWg.Go(func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 256*1024), 256*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			d, _ := ParseRecord(line)
			if d == nil {
				// Non-JSON banner line → stream is live.
				readOnce.Do(func() { readyWg.Done() })
				continue
			}
			if _, dup := seen[*d]; !dup {
				seen[*d] = struct{}{}
				fmt.Fprintln(opts.Sink, d.Line())
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(opts.Sink, "# trace: scanner error: %v\n", err)
		}
		// EOF before banner (e.g. log process died early).
		readOnce.Do(func() { readyWg.Done() })
	})

	readyWg.Wait()

	fnErr := fn(ctx)

	select {
	case <-time.After(drain):
	case <-ctx.Done():
	}

	// Terminate log stream process group.
	if logCmd.Process != nil {
		pgid := -logCmd.Process.Pid
		_ = syscall.Kill(pgid, syscall.SIGTERM)
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(pgid, syscall.SIGKILL)
	}
	_ = stdout.Close() // force scanner EOF
	scanWg.Wait()
	_ = logCmd.Wait() // reap process

	return fnErr
}
