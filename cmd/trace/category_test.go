package main

import "testing"

func TestCategorize(t *testing.T) {
	tests := []struct {
		op   string
		want Category
	}{
		// Read
		{"file-read-data", CatRead},
		{"file-read-metadata", CatRead},
		{"file-read-xattr", CatRead},
		// Write
		{"file-write-create", CatWrite},
		{"file-write-data", CatWrite},
		{"file-write-unlink", CatWrite},
		{"file-write-flags", CatWrite},
		{"file-write-mode", CatWrite},
		// Exec — file-map-executable must win over file- prefix
		{"file-map-executable", CatExec},
		{"process-exec", CatExec},
		{"process-exec*", CatExec},
		{"process-fork", CatExec},
		// Network
		{"network-outbound", CatNetwork},
		{"network-inbound", CatNetwork},
		{"network-bind", CatNetwork},
		{"system-socket", CatNetwork},
		// Mach
		{"mach-lookup", CatMach},
		{"mach-register", CatMach},
		// IPC
		{"ipc-posix-shm", CatIPC},
		{"ipc-posix-shm-open", CatIPC},
		{"ipc-posix-sem", CatIPC},
		// Other
		{"sysctl-read", CatOther},
		{"signal", CatOther},
		{"iokit-open", CatOther},
		{"file-ioctl", CatOther},
	}

	for _, tc := range tests {
		t.Run(tc.op, func(t *testing.T) {
			got := Categorize(tc.op)
			if got != tc.want {
				t.Errorf("Categorize(%q) = %s, want %s", tc.op, got, tc.want)
			}
		})
	}
}
