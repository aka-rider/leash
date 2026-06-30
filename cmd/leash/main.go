//go:build darwin

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	leash "github.com/aka-rider/leash"
	"github.com/aka-rider/leash/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	l, parsed, err := cli.Configure(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "leash: %v\nRun 'leash --help' for usage.\n", err)
		return 2
	}
	if len(parsed.Command) == 0 {
		if parsed.Help {
			fmt.Print(cli.Usage())
			return 0
		}
		fmt.Fprint(os.Stderr, cli.Usage())
		return 2
	}

	if parsed.Worktree {
		wtPath, err := createWorktree(parsed.WorktreeName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "leash: %v\n", err)
			return 2
		}
		l.Writes = append(l.Writes, wtPath)
		l.Dir = wtPath
		fmt.Fprintf(os.Stderr, "leash: worktree: %s\n", wtPath)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return leash.ExitCode(leash.Execute(ctx, l))
}
