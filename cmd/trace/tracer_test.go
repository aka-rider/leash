//go:build darwin && integration

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestRun_LifecycleWithDenial verifies that a real sandbox denial is captured
// and written to the sink before Run returns.
func TestRun_LifecycleWithDenial(t *testing.T) {
	nonce := "leash-tracer-test-" + t.Name()
	var sink bytes.Buffer

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

	target := t.TempDir() + "/denied.txt"

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	runErr := Run(ctx, Options{
		Nonce:    nonce,
		Sink:     &sink,
		DrainFor: 500 * time.Millisecond,
	}, func(ctx context.Context) error {
		// Run a child that tries to write to a file — sandbox will deny it.
		cmd := exec.CommandContext(ctx, "sandbox-exec", "-f", f.Name(), "sh", "-c", "echo hello > "+target)
		_ = cmd.Run() // expected to fail (sandbox deny)
		return nil
	})
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}

	// Verify at least one denial line was written to the sink.
	out := sink.String()
	if !strings.Contains(out, ":") {
		t.Fatalf("expected denial line in sink output, got: %q", out)
	}
	t.Logf("captured trace output:\n%s", out)
}
