// Package trace captures and formats sandbox denial events from the macOS kernel log.
// category.go and parse.go are build-tag-free so they can be tested on any platform.
package main

import "strings"

// Category is the human-readable class of a sandbox denial operation.
type Category int

const (
	CatRead    Category = iota // file-read*
	CatWrite                   // file-write*
	CatExec                    // process-exec, file-map-executable, process-fork
	CatNetwork                 // network-outbound, network-inbound, system-socket
	CatMach                    // mach-lookup, mach-register, mach*
	CatIPC                     // ipc-posix-shm*, ipc-*
	CatOther                   // sysctl-read, signal, iokit-open, …
)

// String returns the human-readable lowercase label.
func (c Category) String() string {
	switch c {
	case CatRead:
		return "read"
	case CatWrite:
		return "write"
	case CatExec:
		return "exec"
	case CatNetwork:
		return "network"
	case CatMach:
		return "mach"
	case CatIPC:
		return "ipc"
	default:
		return "other"
	}
}

// Categorize maps a real seatbelt operation token to a Category.
// Precedence: file-map-executable checked before generic file- so exec wins.
// Input is the raw op token from the kernel eventMessage.
func Categorize(op string) Category {
	switch {
	// Exec: check file-map-executable before generic file- branches
	case op == "file-map-executable",
		strings.HasPrefix(op, "process-exec"),
		op == "process-fork":
		return CatExec

	// Write: file-write* variants
	case strings.HasPrefix(op, "file-write"):
		return CatWrite

	// Read: file-read* variants (after exec check)
	case strings.HasPrefix(op, "file-read"):
		return CatRead

	// Network
	case strings.HasPrefix(op, "network"),
		op == "system-socket":
		return CatNetwork

	// Mach IPC
	case strings.HasPrefix(op, "mach"):
		return CatMach

	// POSIX IPC
	case strings.HasPrefix(op, "ipc"):
		return CatIPC

	default:
		return CatOther
	}
}
