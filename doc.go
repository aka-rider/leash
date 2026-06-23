// Package leash provides a library-first API for running commands inside a
// macOS Seatbelt (sandbox-exec) sandbox.
//
// Populate a [Leash] struct and pass it to [Execute]:
//
//	l := leash.Leash{
//		Program: "go",
//		Args:    []string{"test", "./..."},
//		Writes:  []string{"."},
//		Network: true,
//		Stdout:  os.Stdout,
//		Stderr:  os.Stderr,
//	}
//	err := leash.Execute(ctx, l)
//
// The sandbox is deny-by-default: nothing is writable except the system temp
// and cache directories until granted with Reads, Writes, or Execs fields.
// Tool detection (go, git, homebrew, docker, claude, …) is unconditional —
// every supported tool is probed and added to the sandbox profile automatically.
//
// Sandboxing is only available on macOS; on other platforms Execute returns
// [ErrUnsupported]. Tracing of sandbox denials is provided by the separate
// leash-trace binary, not by this package.
package leash
