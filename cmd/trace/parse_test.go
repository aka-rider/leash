package main

import (
	"encoding/json"
	"testing"
)

// buildRecord creates a minimal ndjson record from a real eventMessage string.
func buildRecord(eventMessage string) []byte {
	b, _ := json.Marshal(map[string]string{"eventMessage": eventMessage})
	return b
}

func TestParseRecord(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantNil bool
		wantOp  string
		wantTgt string
		wantCat Category
	}{
		{
			// Real-captured: log stream plain-text banner (not JSON)
			name:    "banner line skipped",
			input:   []byte(`Filtering the log data using "composedMessage CONTAINS "leash-capturetest99""`),
			wantNil: true,
		},
		{
			// Real-captured: process-exec* denial
			name:    "process-exec* denial",
			input:   buildRecord("Sandbox: sandbox-exec(22391) deny(1) process-exec* /bin/cat\nleash-capturetest99"),
			wantOp:  "process-exec*",
			wantTgt: "/bin/cat",
			wantCat: CatExec,
		},
		{
			// Real-captured: file-read-metadata denial
			name:    "file-read-metadata denial",
			input:   buildRecord("Sandbox: sandbox-exec(22391) deny(1) file-read-metadata /bin/cat\nleash-capturetest99"),
			wantOp:  "file-read-metadata",
			wantTgt: "/bin/cat",
			wantCat: CatRead,
		},
		{
			// Real-captured: file-read-data denial
			name:    "file-read-data denial",
			input:   buildRecord("Sandbox: sh(22938) deny(1) file-read-data /\nleash-capturetest99"),
			wantOp:  "file-read-data",
			wantTgt: "/",
			wantCat: CatRead,
		},
		{
			// Constructed from format: file-write-create
			name:    "file-write-create denial",
			input:   buildRecord("Sandbox: sh(99) deny(1) file-write-create /tmp/out.txt\nleash-abc123"),
			wantOp:  "file-write-create",
			wantTgt: "/tmp/out.txt",
			wantCat: CatWrite,
		},
		{
			// Constructed: network-outbound with host:port target
			name:    "network-outbound denial",
			input:   buildRecord("Sandbox: curl(99) deny(1) network-outbound 93.184.216.34:80\nleash-abc123"),
			wantOp:  "network-outbound",
			wantTgt: "93.184.216.34:80",
			wantCat: CatNetwork,
		},
		{
			// Constructed: mach-lookup
			name:    "mach-lookup denial",
			input:   buildRecord("Sandbox: node(99) deny(1) mach-lookup com.apple.something\nleash-abc123"),
			wantOp:  "mach-lookup",
			wantTgt: "com.apple.something",
			wantCat: CatMach,
		},
		{
			// Constructed: ipc-posix-shm-open
			name:    "ipc-posix-shm-open denial",
			input:   buildRecord("Sandbox: app(99) deny(1) ipc-posix-shm-open shm-name\nleash-abc123"),
			wantOp:  "ipc-posix-shm-open",
			wantTgt: "shm-name",
			wantCat: CatIPC,
		},
		{
			// Non-deny event (no "deny(" in eventMessage): skip
			name:    "non-deny event skipped",
			input:   buildRecord("Sandbox: sh(99) allow file-read-data /tmp/x\nleash-abc123"),
			wantNil: true,
		},
		{
			// Empty input
			name:    "empty line",
			input:   []byte{},
			wantNil: true,
		},
		{
			// Valid JSON but no eventMessage
			name:    "no eventMessage field",
			input:   []byte(`{"other":"field"}`),
			wantNil: true,
		},
		{
			// Denial with no target (op only)
			name:    "op-only denial (process-fork)",
			input:   buildRecord("Sandbox: sh(99) deny(1) process-fork\nleash-abc123"),
			wantOp:  "process-fork",
			wantTgt: "",
			wantCat: CatExec,
		},
		{
			// file-map-executable must be categorized as exec, not read
			name:    "file-map-executable is exec not read",
			input:   buildRecord("Sandbox: sh(99) deny(1) file-map-executable /usr/lib/libfoo.dylib\nleash-abc123"),
			wantOp:  "file-map-executable",
			wantTgt: "/usr/lib/libfoo.dylib",
			wantCat: CatExec,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRecord(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil Denial, got nil")
			}
			if got.Op != tc.wantOp {
				t.Errorf("Op: got %q, want %q", got.Op, tc.wantOp)
			}
			if got.Target != tc.wantTgt {
				t.Errorf("Target: got %q, want %q", got.Target, tc.wantTgt)
			}
			if got.Category != tc.wantCat {
				t.Errorf("Category: got %s, want %s", got.Category, tc.wantCat)
			}
		})
	}
}

func TestDenialLine(t *testing.T) {
	d := Denial{Category: CatRead, Target: "/etc/passwd", Op: "file-read-data"}
	if got := d.Line(); got != "read: /etc/passwd" {
		t.Errorf("Line() = %q, want %q", got, "read: /etc/passwd")
	}
}
