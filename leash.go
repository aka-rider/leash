package leash

import (
	"errors"
	"io"
	"os/exec"
	"syscall"
)

// Leash holds the configuration for a sandboxed command.
// Populate fields directly and pass to Execute.
type Leash struct {
	Program    string
	Args       []string
	Network bool
	Reads   []string
	Writes     []string
	Execs      []string
	DenyReads  []string
	DenyWrites []string
	DenyExecs  []string
	ExtraEnv   map[string]string
	ProxyEnv   []string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	// DenyTag is the SBPL deny-rule message tag embedded in the sandbox profile.
	// An external tracer (leash-trace) filters the kernel log for this tag to capture denials.
	// When empty, the sandbox uses the default tag "leash".
	DenyTag string
}

// ExitCode maps an Execute error to a process exit code.
// nil → 0; *exec.ExitError → exit code or 128+signal; other → 1.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		if code := ee.ExitCode(); code != -1 {
			return code
		}
		if st, ok := ee.Sys().(syscall.WaitStatus); ok && st.Signaled() {
			return 128 + int(st.Signal())
		}
		return 1
	}
	return 1
}
