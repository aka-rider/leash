//go:build darwin

package sandbox

import (
	"errors"
	"fmt"
	"os"
)

// Permission defines filesystem access levels for SBPL rules.
type Permission uint8

const (
	Read  Permission = 1 << iota // SBPL: file-read*
	Write                        // SBPL: file-read* + file-write*
	Exec                         // SBPL: file-read* + file-map-executable + process-exec
)

// entry pairs a resolved path with a permission.
type entry struct {
	path Path
	perm Permission
	deny bool // true = SBPL deny rule; false = allow rule
}

// ToolProfile accumulates filesystem access rules and specific env variables for a tool.
type ToolProfile struct {
	name    string
	home    string
	entries []entry
	env     []string // KEY=VALUE pairs
}

// NewToolProfile creates a new profile for the named tool.
func NewToolProfile(name, home string) ToolProfile {
	return ToolProfile{name: name, home: home}
}

// AddEnv records an environment variable required by this tool.
func (p *ToolProfile) AddEnv(key, value string) {
	p.env = append(p.env, key+"="+value)
}

// Allow is the ONLY mutation method. Resolves raw path (expands ~, resolves symlinks, stats).
// Returns error on any failure — never swallows.
// Exec on a file emits a (literal …) process-exec rule for that specific binary.
// Exec on a directory emits a (subpath …) process-exec rule allowing any binary beneath it.
func (p *ToolProfile) Allow(raw string, perm Permission) error {
	path, err := ResolvePath(raw, p.home)
	if err != nil {
		return fmt.Errorf("profile %q: %w", p.name, err)
	}
	p.entries = append(p.entries, entry{path: path, perm: perm})
	return nil
}

// AllowFuture grants perm on a path that may not exist yet (lock files,
// tempfiles the child creates inside the sandbox, e.g. git's packed-refs
// lock). The parent directory must exist: it is symlink-resolved and the
// base name is re-joined so the emitted SBPL literal matches the canonical
// path the kernel checks when the child later creates the file. Returns
// error on any failure — never swallows.
func (p *ToolProfile) AllowFuture(raw string, perm Permission) error {
	path, err := ResolveFuturePath(raw, p.home)
	if err != nil {
		return fmt.Errorf("profile %q: %w", p.name, err)
	}
	p.entries = append(p.entries, entry{path: path, perm: perm})
	return nil
}

// AllowFutureDir grants perm on a directory that may not exist yet (e.g. a
// build-output dir populated after the SBPL profile is compiled). The
// entry is always emitted as an SBPL subpath rule (covers anything later
// created beneath it), unlike AllowFuture which emits a literal rule
// matching only the exact not-yet-existing path. Returns error on any
// failure — never swallows.
func (p *ToolProfile) AllowFutureDir(raw string, perm Permission) error {
	path, err := ResolveFutureDir(raw, p.home)
	if err != nil {
		return fmt.Errorf("profile %q: %w", p.name, err)
	}
	p.entries = append(p.entries, entry{path: path, perm: perm})
	return nil
}

// Deny records a deny rule for raw path and perm. Deny rules are emitted last
// in the SBPL profile so they win under SBPL last-match-wins semantics.
func (p *ToolProfile) Deny(raw string, perm Permission) error {
	path, err := ResolvePath(raw, p.home)
	if err != nil {
		return fmt.Errorf("profile %q: %w", p.name, err)
	}
	p.entries = append(p.entries, entry{path: path, perm: perm, deny: true})
	return nil
}

// AllowOptional skips paths that do not exist, but propagates permission denied,
// symlink loops, and all other non-not-exist errors.
// Returns (true, nil) when the path exists and the entry was added.
// Returns (false, nil) when the path does not exist.
// Returns (false, err) on any other error.
func (p *ToolProfile) AllowOptional(raw string, perm Permission) (bool, error) {
	if err := p.Allow(raw, perm); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Snapshot is an immutable, opaque value for ProfileBuilder.
type Snapshot struct {
	name    string
	entries []entry
	env     []string // KEY=VALUE pairs
}

// Snapshot returns an immutable deep copy of the profile state.
func (p *ToolProfile) Snapshot() Snapshot {
	cp := make([]entry, len(p.entries))
	copy(cp, p.entries)
	envCp := make([]string, len(p.env))
	copy(envCp, p.env)
	return Snapshot{name: p.name, entries: cp, env: envCp}
}
