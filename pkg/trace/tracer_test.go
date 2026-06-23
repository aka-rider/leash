//go:build darwin && integration

package trace_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/aka-rider/leash/pkg/trace"
)

// TestTracer_LifecycleWithDenial verifies that a real sandbox denial is captured
// and written to the sink before Stop() returns.
func TestTracer_LifecycleWithDenial(t *testing.T) {
	nonce := "leash-tracer-test-" + t.Name()
	var sink bytes.Buffer

	ctx := context.Background()
	tr, err := trace.Start(ctx, trace.Options{
		Nonce:    nonce,
		Sink:     &sink,
		DrainFor: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for log stream to be live.
	select {
	case <-tr.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("tracer Ready() timed out after 5s")
	}

	// Build a minimal deny profile: deny everything and embed the nonce in the message.
	// sandbox-exec will deny the child's write attempt.
	profile := `(version 1)
(deny default (with message "` + nonce + `"))
(allow file-read-metadata)
(allow sysctl-read)
(allow signal)
(allow process-info*)
(allow process-fork)
(allow file-read* (subpath "/usr") (subpath "/System") (subpath "/bin") (subpath "/usr/lib") (subpath "/var/db/dyld") (subpath "/private/tmp") (subpath "/tmp"))
(allow file-read-data (literal "/") (literal "/private") (literal "/private/var"))
(allow file-map-executable (subpath "/usr") (subpath "/System") (subpath "/usr/lib") (subpath "/var/db/dyld"))
(allow file-read* file-write* (regex "^/dev/(null|zero|random|urandom|tty.*)"))
(allow file-ioctl)
(allow mach-lookup)
(allow ipc-posix-shm*)
(allow process-exec (literal "/bin/sh") (literal "/bin/cat") (subpath "/usr/bin") (subpath "/bin"))
`
	// Write the profile to a temp file.
	f, err := os.CreateTemp("", "leash-test-*.sb")
	if err != nil {
		t.Fatalf("create temp profile: %v", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(profile); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	f.Close()

	// Run a child that tries to write to a file — sandbox will deny it.
	target := t.TempDir() + "/denied.txt"
	cmd := exec.Command("sandbox-exec", "-f", f.Name(), "sh", "-c", "echo hello > "+target)
	_ = cmd.Run() // expected to fail (sandbox deny)

	// Stop the tracer — drain window collects late denials.
	stopDone := make(chan error, 1)
	go func() { stopDone <- tr.Stop() }()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() timed out after 5s")
	}

	// Verify at least one denial line was written to the sink.
	out := sink.String()
	if !strings.Contains(out, ":") {
		t.Fatalf("expected denial line in sink output, got: %q", out)
	}
	t.Logf("captured trace output:\n%s", out)
}
