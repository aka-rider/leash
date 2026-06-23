//go:build darwin

package detect

import (
	"fmt"

	"github.com/aka-rider/leash/pkg/sandbox"
)

// UserPerms carries the user-configured filesystem grants (from CLI +r/+w/+x and yaml config).
// This replaces the internal/config.SandboxConfig coupling from the orqestra version.
type UserPerms struct {
	Read  []string
	Write []string
	Exec  []string
}

// UserProfile compiles a seatbelt profile from config-file filesystem permissions.
// Paths that do not exist are silently skipped (AllowOptional semantics).
// For CLI +r/+w/+x grants, use sandbox.NewToolProfile + Allow directly so that
// missing paths produce an immediate error rather than a silent skip.
func UserProfile(home string, p UserPerms) (sandbox.Snapshot, error) {
	prof := sandbox.NewToolProfile("user-config", home)
	for _, path := range p.Read {
		if err := prof.AllowOptional(path, sandbox.Read); err != nil {
			return sandbox.Snapshot{}, fmt.Errorf("user profile read %q: %w", path, err)
		}
	}
	for _, path := range p.Write {
		if err := prof.AllowOptional(path, sandbox.Write); err != nil {
			return sandbox.Snapshot{}, fmt.Errorf("user profile write %q: %w", path, err)
		}
	}
	for _, path := range p.Exec {
		if err := prof.AllowOptional(path, sandbox.Exec); err != nil {
			return sandbox.Snapshot{}, fmt.Errorf("user profile exec %q: %w", path, err)
		}
	}
	return prof.Snapshot(), nil
}

// registry maps detector names to their detection functions.
// All functions return (*sandbox.Snapshot, error) where nil means "not found".
var registry = map[string]func(home string) (*sandbox.Snapshot, error){
	"homebrew": DetectHomebrew,
	"docker":   DetectDocker,
	"xcode":    DetectXcodeDeveloper,
	"git":      DetectGit,
	"npm":      DetectNPM,
	"go":       DetectGo,
	"python":   DetectPython,
}

// Detect runs the named detectors and returns their snapshots.
// "claude" is handled specially — it is always attempted when present and requires claudeBin.
// An unknown name returns an error immediately (fail loud on typos).
func Detect(home, claudeBin string, names []string) ([]sandbox.Snapshot, error) {
	var snaps []sandbox.Snapshot
	for _, name := range names {
		if name == "claude" {
			snap, err := DetectClaude(home, claudeBin)
			if err != nil {
				return nil, fmt.Errorf("detect claude: %w", err)
			}
			snaps = append(snaps, snap)
			continue
		}

		fn, ok := registry[name]
		if !ok {
			return nil, fmt.Errorf("unknown detector %q (available: homebrew, docker, xcode, git, npm, go, python, claude)", name)
		}
		snap, err := fn(home)
		if err != nil {
			return nil, fmt.Errorf("detect %s: %w", name, err)
		}
		if snap != nil {
			snaps = append(snaps, *snap)
		}
	}
	return snaps, nil
}
