//go:build darwin

package detect

import (
	"os/exec"
	"syscall"
)

// probeOutput runs cmd and returns its stdout, same contract as (*exec.Cmd).Output.
// It always runs cmd in its own session (Setsid), detached from the caller's
// controlling terminal.
//
// This matters even for a probe whose own output we throw away on error and
// which exits long before the caller does anything else: some detectors
// shell out to interpreted scripts (e.g. Homebrew's `brew`, a bash script).
// Confirmed by isolation testing — a bash-interpreted child that shares our
// session, even after it exits and is fully reaped, can wedge a LATER,
// unrelated Setpgid child's exit when that later child is killed by a
// signal while attached to the same controlling terminal (the leash process
// itself then hangs in exit(), unkillable short of SIGKILL). Giving the
// probe its own session (no controlling terminal at all) avoids it entirely.
// Root cause not fully understood; presumed macOS pty/session kernel quirk.
func probeOutput(cmd *exec.Cmd) ([]byte, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	return cmd.Output()
}
