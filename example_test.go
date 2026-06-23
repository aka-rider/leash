package leash_test

import (
	"context"
	"fmt"
	"os"

	leash "github.com/aka-rider/leash"
)

// Example shows the typical flow: populate a Leash struct with the directories
// the command needs, wire stdio, and run it inside the sandbox.
func Example() {
	l := leash.Leash{
		Program: "go",
		Args:    []string{"test", "./..."},
		Writes:  []string{"."},
		Network: true,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	}

	if err := leash.Execute(context.Background(), l); err != nil {
		fmt.Fprintf(os.Stderr, "leash: %v (exit %d)\n", err, leash.ExitCode(err))
	}
}
