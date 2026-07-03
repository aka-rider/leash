//go:build darwin

package sandbox

import (
	"strings"
	"testing"
)

func TestBuilder_DenyMessageDefault(t *testing.T) {
	b, err := NewProfileBuilder("/Users/test", "/private/tmp")
	if err != nil {
		t.Fatalf("NewProfileBuilder: %v", err)
	}
	sbpl, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(sbpl, `(deny default (with message "leash"))`) {
		t.Errorf("expected default deny message 'leash', got:\n%s", sbpl)
	}
}

func TestBuilder_DenyMessageNonce(t *testing.T) {
	b, err := NewProfileBuilder("/Users/test", "/private/tmp")
	if err != nil {
		t.Fatalf("NewProfileBuilder: %v", err)
	}
	b.DenyMessage = "leash-deadbeef"
	sbpl, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(sbpl, `(deny default (with message "leash-deadbeef"))`) {
		t.Errorf("expected nonce deny message, got:\n%s", sbpl)
	}
}

func TestBuilder_NoNetworkOmitsBlock(t *testing.T) {
	b, err := NewProfileBuilder("/Users/test", "/private/tmp")
	if err != nil {
		t.Fatalf("NewProfileBuilder: %v", err)
	}
	b.NoNetwork = true
	sbpl, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if strings.Contains(sbpl, "network-outbound") {
		t.Errorf("NoNetwork=true should omit network-outbound, got:\n%s", sbpl)
	}
	if strings.Contains(sbpl, "system-socket") {
		t.Errorf("NoNetwork=true should omit system-socket, got:\n%s", sbpl)
	}
}

func TestSbplOps_SingleBits(t *testing.T) {
	tests := []struct {
		perm Permission
		want string
	}{
		{Read, "file-read*"},
		{Write, "file-read* file-write*"},
		{Exec, "file-read* file-map-executable process-exec"},
	}
	for _, tc := range tests {
		if got := sbplOps(tc.perm); got != tc.want {
			t.Errorf("sbplOps(%v) = %q, want %q", tc.perm, got, tc.want)
		}
	}
}

func TestSbplOps_CombinedBits(t *testing.T) {
	// Write|Exec must keep BOTH file-write* and the exec ops — previously a
	// switch/case picked only the Exec branch and silently dropped write.
	got := sbplOps(Write | Exec)
	want := "file-read* file-write* file-map-executable process-exec"
	if got != want {
		t.Errorf("sbplOps(Write|Exec) = %q, want %q", got, want)
	}
}

func TestSbplDenyOps_SingleBits(t *testing.T) {
	tests := []struct {
		perm Permission
		want string
	}{
		{Read, "file-read*"},
		{Write, "file-write*"},
		{Exec, "file-map-executable process-exec"},
	}
	for _, tc := range tests {
		if got := sbplDenyOps(tc.perm); got != tc.want {
			t.Errorf("sbplDenyOps(%v) = %q, want %q", tc.perm, got, tc.want)
		}
	}
}

func TestSbplDenyOps_CombinedBits(t *testing.T) {
	// Write|Exec must deny the union of both ops, not just the exec branch.
	got := sbplDenyOps(Write | Exec)
	want := "file-write* file-map-executable process-exec"
	if got != want {
		t.Errorf("sbplDenyOps(Write|Exec) = %q, want %q", got, want)
	}

	got = sbplDenyOps(Read | Write | Exec)
	want = "file-read* file-write* file-map-executable process-exec"
	if got != want {
		t.Errorf("sbplDenyOps(Read|Write|Exec) = %q, want %q", got, want)
	}
}

func TestBuilder_NetworkDefaultAllowed(t *testing.T) {
	b, err := NewProfileBuilder("/Users/test", "/private/tmp")
	if err != nil {
		t.Fatalf("NewProfileBuilder: %v", err)
	}
	sbpl, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(sbpl, "network-outbound") {
		t.Errorf("default builder should allow network-outbound")
	}
}
