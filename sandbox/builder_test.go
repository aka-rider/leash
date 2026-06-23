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
