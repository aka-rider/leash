//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
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
	if parsed.Help {
		fmt.Print(cli.Usage())
		return 0
	}
	if len(parsed.Command) == 0 {
		fmt.Fprint(os.Stderr, cli.Usage())
		return 2
	}

	if parsed.Worktree {
		var err error
		l, err = applyWorktree(l, parsed.WorktreeName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "leash: %v\n", err)
			return 2
		}
		fmt.Fprintf(os.Stderr, "leash: worktree: %s\n", l.Dir)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	execErr := leash.Execute(ctx, l)
	if execErr != nil {
		// *exec.ExitError means the child ran and exited non-zero; it already
		// printed its own diagnostics. Anything else is a leash-side setup
		// failure (bad grant, missing sandbox-exec, ...) that would otherwise
		// exit silently. Library errors already carry a "leash: " prefix —
		// trim it so the message isn't doubled.
		if _, ok := errors.AsType[*exec.ExitError](execErr); !ok {
			fmt.Fprintf(os.Stderr, "leash: %s\n", strings.TrimPrefix(execErr.Error(), "leash: "))
		}
	}
	return leash.ExitCode(execErr)
}

// applyWorktree creates a worktree for name and wires its grants into l:
// read on the repo root, write on the new worktree, and the worktree as l.Dir.
//
// It also grants write on the parts of the main repo's .git a linked
// worktree needs for `git add`/`git commit` to work: worktrees/<name> (this
// worktree's private HEAD/index/locks), objects (new blobs/trees/commits),
// refs (branch ref updates), and logs (reflogs — may not exist in edge-case
// repos, so it's only granted when present).
//
// It also grants FutureWrites on packed-refs, packed-refs.lock, and
// packed-refs.new: `git commit`'s post-commit cleanup runs a ref DELETION
// transaction (for CHERRY_PICK_HEAD/REVERT_HEAD pseudorefs), and git's files
// ref-backend takes the packed-refs lock for every deletion transaction even
// when packed-refs doesn't exist yet — without this grant, commit prints a
// benign but alarming "Unable to create '….git/packed-refs.lock': Operation
// not permitted". `git pack-refs` writes packed-refs.new then renames it,
// and deleting a packed ref rewrites packed-refs itself, so all three names
// are granted as exact (not-yet-existing) paths via FutureWrites rather than
// a subpath grant.
//
// The top-level .git directory itself is deliberately left read-only beyond
// these specific grants: it holds repo-wide config and hooks that a
// sandboxed command should not be able to rewrite. One consequence: `git
// branch -d` may print a benign "could not lock config file .git/config"
// warning even though the branch deletion itself succeeds — that's the
// read-only boundary working as intended, not a bug.
func applyWorktree(l leash.Leash, name string) (leash.Leash, error) {
	wtPath, repoRoot, err := createWorktree(name)
	if err != nil {
		return l, err
	}
	l.Reads = append(l.Reads, repoRoot)
	l.Writes = append(l.Writes, wtPath)
	l.Dir = wtPath

	gitDir := filepath.Join(repoRoot, ".git")
	for _, p := range []string{
		filepath.Join(gitDir, "worktrees", name),
		filepath.Join(gitDir, "objects"),
		filepath.Join(gitDir, "refs"),
		filepath.Join(gitDir, "logs"),
	} {
		if _, err := os.Stat(p); err == nil {
			l.Writes = append(l.Writes, p)
		}
	}

	l.FutureWrites = append(l.FutureWrites,
		filepath.Join(gitDir, "packed-refs"),
		filepath.Join(gitDir, "packed-refs.lock"),
		filepath.Join(gitDir, "packed-refs.new"),
	)

	return l, nil
}
