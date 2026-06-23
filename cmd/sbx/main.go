//go:build darwin

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/aka-rider/leash/pkg/cli"
	"github.com/aka-rider/leash/pkg/config"
	"github.com/aka-rider/leash/pkg/detect"
	"github.com/aka-rider/leash/pkg/sandbox"
	"github.com/aka-rider/leash/pkg/trace"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	parsed, err := cli.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "sbx: %v\nRun 'sbx --help' for usage.\n", err)
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

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sbx: getwd: %v\n", err)
		return 1
	}

	cfg, err := config.Resolve(parsed, cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sbx: %v\n", err)
		return 1
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sbx: home dir: %v\n", err)
		return 1
	}

	// Resolve the child binary before entering the sandbox so we get a clear
	// error message if it is not found (sandbox denials are less readable).
	childBin, err := exec.LookPath(parsed.Command[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "sbx: %v\n", err)
		return 1
	}

	// --- build per-run nonce for --trace ---
	var nonce string
	if parsed.Trace {
		var buf [4]byte
		if _, err := rand.Read(buf[:]); err != nil {
			fmt.Fprintf(os.Stderr, "sbx: generate nonce: %v\n", err)
			return 1
		}
		nonce = "leash-" + hex.EncodeToString(buf[:])
	}

	// --- auto-detect tool profiles ---
	claudeBin, _ := exec.LookPath("claude") // fire-and-forget: claude binary is optional; DetectClaude handles empty path
	detectedSnaps, err := detect.Detect(home, claudeBin, cfg.Detect)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sbx: detect: %v\n", err)
		return 1
	}

	profiles := make([]sandbox.Snapshot, 0, 2+len(detectedSnaps))

	// Config paths: optional — yaml may reference paths that don't exist yet.
	cfgPerms := detect.UserPerms{Read: cfg.Read, Write: cfg.Write, Exec: cfg.Exec}
	if len(cfgPerms.Read)+len(cfgPerms.Write)+len(cfgPerms.Exec) > 0 {
		cfgSnap, err := detect.UserProfile(home, cfgPerms)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sbx: config grant: %v\n", err)
			return 1
		}
		profiles = append(profiles, cfgSnap)
	}

	// CLI grants: strict — user-supplied paths must exist.
	if len(parsed.Grants) > 0 {
		cliProf := sandbox.NewToolProfile("cli-grants", home)
		for _, g := range parsed.Grants {
			if err := cliProf.Allow(g.Path, cliSandboxPerm(g.Perm)); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					fmt.Fprintf(os.Stderr, "sbx: %s %q: does not exist\n", permFlag(g.Perm), g.Path)
				} else {
					fmt.Fprintf(os.Stderr, "sbx: %s %q: %v\n", permFlag(g.Perm), g.Path, err)
				}
				return 1
			}
		}
		profiles = append(profiles, cliProf.Snapshot())
	}

	profiles = append(profiles, detectedSnaps...)

	workspace := cfg.Workspace
	if workspace == "" {
		workspace = cwd
	}

	denyMessage := "leash"
	if nonce != "" {
		denyMessage = nonce
	}

	sbCfg := sandbox.Config{
		RepoPath:    workspace,
		RepoWritable: false,
		Profiles:    profiles,
		DenyMessage: denyMessage,
		NoNetwork:   !cfg.Network,
	}
	sb, err := sandbox.New(sbCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sbx: sandbox: %v\n", err)
		return 1
	}
	defer func() { _ = sb.Close() }() // fire-and-forget: temp profile cleanup

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- start tracer if requested ---
	var tr *trace.Tracer
	if parsed.Trace {
		sink, err := openTraceSink(parsed.TraceFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sbx: trace: %v\n", err)
			return 1
		}
		if sink != os.Stderr {
			defer func() { _ = sink.Close() }() // fire-and-forget: trace file flush on run() exit
		}
		tr, err = trace.Start(ctx, trace.Options{Nonce: nonce, Sink: sink})
		if err != nil {
			fmt.Fprintf(os.Stderr, "sbx: trace start: %v\n", err)
			return 1
		}
		// Wait for log stream to be live before spawning the child.
		<-tr.Ready()
	}

	// --- build and start the child process ---
	cmd := exec.Command(childBin, parsed.Command[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := sb.Wrap(cmd); err != nil {
		fmt.Fprintf(os.Stderr, "sbx: wrap: %v\n", err)
		return 1
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "sbx: start child: %v\n", err)
		return 1
	}

	// Forward SIGINT and SIGTERM to the child's process group.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for sig := range sigCh {
			if s, ok := sig.(syscall.Signal); ok && cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, s) // fire-and-forget: process may have exited
			}
		}
	}()

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			if code := ee.ExitCode(); code != -1 {
				exitCode = code
			} else {
				// Signaled — encode as 128+signal
				if st, ok := ee.Sys().(*syscall.WaitStatus); ok && st.Signaled() {
					exitCode = 128 + int(st.Signal())
				} else {
					exitCode = 1
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "sbx: wait: %v\n", err)
			exitCode = 1
		}
	}

	signal.Stop(sigCh)
	close(sigCh)

	if tr != nil {
		if err := tr.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "sbx: trace stop: %v\n", err)
		}
	}

	return exitCode
}

// permFlag returns the CLI flag string for a permission type (e.g. "+w").
func permFlag(p cli.Perm) string {
	switch p {
	case cli.PermWrite:
		return "+w"
	case cli.PermExec:
		return "+x"
	default:
		return "+r"
	}
}

// cliSandboxPerm maps a CLI permission type to the sandbox Permission type.
func cliSandboxPerm(p cli.Perm) sandbox.Permission {
	switch p {
	case cli.PermWrite:
		return sandbox.Write
	case cli.PermExec:
		return sandbox.Exec
	default:
		return sandbox.Read
	}
}

// openTraceSink opens the trace output destination.
// "-" maps to stderr (no exclusivity check).
// All other paths use O_CREATE|O_EXCL — errors if the file already exists.
func openTraceSink(path string) (*os.File, error) {
	if path == "" {
		path = "./sbx-trace.log"
	}
	if path == "-" {
		return os.Stderr, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("trace file already exists: %s (delete it or choose a different name)", path)
		}
		return nil, fmt.Errorf("open trace file %s: %w", path, err)
	}
	return f, nil
}
