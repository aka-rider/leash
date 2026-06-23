//go:build !darwin

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "leash: leash is macOS-only (requires sandbox-exec)")
	os.Exit(1)
}
