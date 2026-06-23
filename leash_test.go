package leash

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
)

func TestExitCode_Nil(t *testing.T) {
	if ExitCode(nil) != 0 {
		t.Error("nil error should give exit code 0")
	}
}

func TestExitCode_Generic(t *testing.T) {
	if ExitCode(errors.New("some error")) != 1 {
		t.Error("generic error should give exit code 1")
	}
}

func TestExitCode_ExitError(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 42")
	err := cmd.Run()
	if code := ExitCode(err); code != 42 {
		t.Errorf("exit 42 should give code 42, got %d", code)
	}
}

func TestExitCode_SignaledProcess(t *testing.T) {
	cmd := exec.Command("tail", "-f", "/dev/null")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	_ = cmd.Process.Signal(syscall.SIGKILL)
	err := cmd.Wait()
	code := ExitCode(err)
	want := 128 + int(syscall.SIGKILL)
	if code != want {
		t.Errorf("signaled exit code: got %d, want %d", code, want)
	}
}
