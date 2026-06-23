//go:build darwin

package leash

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/aka-rider/leash/detect"
	"github.com/aka-rider/leash/sandbox"
)

// Execute runs l.Program inside a macOS Seatbelt sandbox.
// Returns nil on exit code 0, *exec.ExitError on non-zero exit, and other errors for setup failures.
// Signal handling (SIGINT/SIGTERM) should be done by the caller via signal.NotifyContext.
func Execute(ctx context.Context, l Leash) error {
	if l.Program == "" {
		return errors.New("leash: program is required")
	}

	bin, err := exec.LookPath(l.Program)
	if err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("leash: home dir: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("leash: getwd: %w", err)
	}

	// Detection profile: unconditionally probe all supported tools.
	detectProf := sandbox.NewToolProfile("detect", home)
	for _, fn := range []func(sandbox.ToolProfile) (sandbox.ToolProfile, error){
		detect.Claude, detect.Homebrew, detect.Docker,
		detect.Git, detect.NPM, detect.Xcode, detect.Go, detect.Python,
	} {
		if detectProf, err = fn(detectProf); err != nil {
			return fmt.Errorf("leash: detect: %w", err)
		}
	}

	profiles := make([]sandbox.Snapshot, 0, 3)

	// Grant profile: implicit cwd read + user grants (+r/+w/+x).
	grantProf := sandbox.NewToolProfile("leash-grants", home)
	if err := grantProf.Allow(cwd, sandbox.Read); err != nil {
		return fmt.Errorf("leash: implicit +r cwd: %w", err)
	}
	for _, p := range l.Reads {
		if err := grantProf.Allow(p, sandbox.Read); err != nil {
			return fmt.Errorf("leash: +r %q: %w", p, err)
		}
	}
	for _, p := range l.Writes {
		if err := grantProf.Allow(p, sandbox.Write); err != nil {
			return fmt.Errorf("leash: +w %q: %w", p, err)
		}
	}
	for _, p := range l.Execs {
		if err := grantProf.Allow(p, sandbox.Exec); err != nil {
			return fmt.Errorf("leash: +x %q: %w", p, err)
		}
	}
	profiles = append(profiles, grantProf.Snapshot())

	// Deny profile: explicit deny flags (-r/-w/-x). Emitted last in SBPL so they win (last-match-wins).
	if len(l.DenyReads)+len(l.DenyWrites)+len(l.DenyExecs) > 0 {
		denyProf := sandbox.NewToolProfile("leash-denies", home)
		for _, p := range l.DenyReads {
			if err := denyProf.Deny(p, sandbox.Read); err != nil {
				return fmt.Errorf("leash: -r %q: %w", p, err)
			}
		}
		for _, p := range l.DenyWrites {
			if err := denyProf.Deny(p, sandbox.Write); err != nil {
				return fmt.Errorf("leash: -w %q: %w", p, err)
			}
		}
		for _, p := range l.DenyExecs {
			if err := denyProf.Deny(p, sandbox.Exec); err != nil {
				return fmt.Errorf("leash: -x %q: %w", p, err)
			}
		}
		profiles = append(profiles, denyProf.Snapshot())
	}

	profiles = append(profiles, detectProf.Snapshot())

	sb, err := sandbox.New(sandbox.Config{
		Profiles:    profiles,
		ExtraEnv:    l.ExtraEnv,
		ProxyEnv:    l.ProxyEnv,
		NoNetwork:   !l.Network,
		DenyMessage: l.DenyTag,
	})
	if err != nil {
		return fmt.Errorf("leash: sandbox: %w", err)
	}
	defer func() { _ = sb.Close() }()

	cmd := exec.Command(bin, l.Args...)
	cmd.Stdin = l.Stdin
	cmd.Stdout = l.Stdout
	cmd.Stderr = l.Stderr

	return sb.Run(ctx, cmd)
}
