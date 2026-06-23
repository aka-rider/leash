//go:build darwin

package detect

import (
	"strings"
	"testing"
)

func TestDetect_UnknownName(t *testing.T) {
	_, err := Detect(t.TempDir(), "", []string{"homebew"}) // typo
	if err == nil {
		t.Fatal("expected error for unknown detector name")
	}
	if !strings.Contains(err.Error(), "unknown detector") {
		t.Errorf("error should mention 'unknown detector': %v", err)
	}
}

func TestDetect_EmptyNames(t *testing.T) {
	snaps, err := Detect(t.TempDir(), "", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("expected 0 snapshots, got %d", len(snaps))
	}
}

func TestDetect_KnownNameNoError(t *testing.T) {
	// Homebrew may or may not be present; should not error on known name.
	_, err := Detect(t.TempDir(), "", []string{"homebrew"})
	if err != nil {
		t.Fatalf("unexpected error for 'homebrew': %v", err)
	}
}

func TestDetect_AllRegisteredNames(t *testing.T) {
	// All names in registry should dispatch without "unknown detector" error.
	for name := range registry {
		_, err := Detect(t.TempDir(), "", []string{name})
		if err != nil && strings.Contains(err.Error(), "unknown detector") {
			t.Errorf("registered name %q gave 'unknown detector' error: %v", name, err)
		}
	}
}
