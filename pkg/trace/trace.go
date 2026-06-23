//go:build darwin

package trace

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

// Options configures a Tracer.
type Options struct {
	// Nonce is embedded in the SBPL deny message and used to filter the kernel log.
	// Must match the DenyMessage set on sandbox.Config.
	Nonce string
	// Sink is where human-readable denial lines are written.
	Sink io.Writer
	// DrainFor is how long Stop waits after child exit to collect lagging denials.
	// Defaults to 3 seconds if zero.
	DrainFor time.Duration
}

// Tracer captures sandbox denial events from the macOS kernel log and writes
// grepable human-readable lines to a sink.
type Tracer struct {
	ready    chan struct{}
	rawCh    chan Denial
	logCmd   *exec.Cmd
	logPipe  io.ReadCloser // stdout pipe from log stream; Close() forces G1 scanner to EOF
	g1Done   chan struct{}
	g2Done   chan struct{}
	sink     io.Writer
	drain    time.Duration
	once     sync.Once
}

// disabled returns a no-op Tracer whose Ready is already closed.
func disabled() *Tracer {
	t := &Tracer{ready: make(chan struct{}), g1Done: make(chan struct{}), g2Done: make(chan struct{})}
	close(t.ready)
	close(t.g1Done)
	close(t.g2Done)
	return t
}

// Start launches `log stream` filtered on the nonce and begins capturing denials.
// After Start returns, call <-t.Ready() before launching the sandboxed child — this
// guarantees the stream is live and no early denials are missed.
// If `log` is unavailable, Start returns a disabled no-op Tracer; trace failures
// never block or kill the sandboxed program.
func Start(_ context.Context, opt Options) (*Tracer, error) {
	if opt.Nonce == "" {
		return nil, fmt.Errorf("trace.Start: nonce is required")
	}
	if opt.Sink == nil {
		opt.Sink = os.Stderr
	}
	drain := opt.DrainFor
	if drain == 0 {
		drain = defaultDrain
	}

	// Verify `log` is available; degrade gracefully if not.
	logBin, err := exec.LookPath("log")
	if err != nil {
		fmt.Fprintf(opt.Sink, "# trace unavailable: log binary not found: %v\n", err)
		return disabled(), nil
	}

	// Start `log stream` filtered on our nonce.
	// Use exec.Command (not CommandContext) so Stop() has full lifecycle control —
	// CommandContext's internal kill goroutine conflicts with our pgid-kill + Wait() sequence.
	predicate := fmt.Sprintf("eventMessage CONTAINS %q", opt.Nonce)
	logCmd := exec.Command(logBin, "stream", "--style", "ndjson", "--level", "debug", "--predicate", predicate)
	logCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := logCmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(opt.Sink, "# trace unavailable: log stream pipe: %v\n", err)
		return disabled(), nil
	}
	if err := logCmd.Start(); err != nil {
		fmt.Fprintf(opt.Sink, "# trace unavailable: log stream start: %v\n", err)
		return disabled(), nil
	}

	t := &Tracer{
		ready:   make(chan struct{}),
		rawCh:   make(chan Denial, 1024),
		logCmd:  logCmd,
		logPipe: stdout,
		g1Done:  make(chan struct{}),
		g2Done:  make(chan struct{}),
		sink:    opt.Sink,
		drain:   drain,
	}

	// G1: scan log stream stdout, parse records, push to rawCh.
	// Readiness strategy: log stream prints a plain-text banner as its FIRST line before any
	// events. ParseRecord returns (nil, nil) for non-JSON lines. The first nil line signals
	// that the stream is live — no probe probe-path detection needed.
	go func() {
		defer close(t.g1Done)
		defer close(t.rawCh)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 256*1024), 256*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			d, _ := ParseRecord(line)
			if d == nil {
				// Non-JSON line (banner or unrecognized) → stream is live.
				t.once.Do(func() { close(t.ready) })
				continue
			}
			select {
			case t.rawCh <- *d:
			default:
				// Buffer full — drop rather than blocking G1 (rare burst).
			}
		}
		// EOF from log stream: close ready if banner never arrived.
		t.once.Do(func() { close(t.ready) })
	}()

	// G2: sole sink owner — live dedup + write.
	go func() {
		defer close(t.g2Done)
		seen := make(map[Denial]struct{})
		for d := range t.rawCh {
			if _, dup := seen[d]; dup {
				continue
			}
			seen[d] = struct{}{}
			fmt.Fprintln(t.sink, d.Line())
		}
	}()

	return t, nil
}

// Ready returns a channel that is closed once the log stream is confirmed live
// (via probe denial) or has been determined to be unavailable.
func (t *Tracer) Ready() <-chan struct{} {
	return t.ready
}

// Stop signals that the sandboxed child has exited, drains for DrainFor to collect
// lagging denials, then terminates log stream and waits for all goroutines to exit.
func (t *Tracer) Stop() error {
	// Drain window: let late denials arrive before killing log stream.
	select {
	case <-time.After(t.drain):
	case <-t.g1Done:
		// log stream already exited (e.g. ctx cancelled before Stop)
	}

	// Terminate log stream process group.
	if t.logCmd != nil && t.logCmd.Process != nil {
		pgid := -t.logCmd.Process.Pid
		_ = syscall.Kill(pgid, syscall.SIGTERM) // fire-and-forget: process may have exited
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(pgid, syscall.SIGKILL) // fire-and-forget: ensure termination
	}

	// Close the pipe read end: forces G1's scanner to see EOF even if the
	// process hasn't flushed yet. Must happen before logCmd.Wait().
	if t.logPipe != nil {
		_ = t.logPipe.Close() // fire-and-forget: unblocks G1 scanner immediately
	}

	// Wait for G1 (scanner returned EOF) → rawCh closed → G2 drained.
	<-t.g1Done
	<-t.g2Done

	// Reap the process — safe now that the pipe is closed and G1 has exited.
	if t.logCmd != nil {
		_ = t.logCmd.Wait() // fire-and-forget: process already dead; just collect exit status
	}
	return nil
}
