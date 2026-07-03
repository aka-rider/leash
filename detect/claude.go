//go:build darwin

package detect

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/aka-rider/leash/sandbox"
)

// Claude adds the Claude CLI to p if the binary is found in PATH.
func Claude(p sandbox.ToolProfile) (sandbox.ToolProfile, error) {
	binPath, err := exec.LookPath("claude")
	if errors.Is(err, exec.ErrNotFound) {
		return p, nil
	}
	if err != nil {
		return p, fmt.Errorf("detect claude binary: %w", err)
	}
	if err := p.Allow(filepath.Dir(binPath), sandbox.Exec); err != nil {
		return p, fmt.Errorf("detect claude binary dir: %w", err)
	}

	// ~/.local/bin/claude is commonly a symlink into a versioned install dir
	// (e.g. ~/.local/share/claude/versions/X.Y.Z); Seatbelt's process-exec
	// check resolves symlinks first, so grant exec on the resolved binary's
	// directory too (same pattern as detect/golang.go and detect/python.go).
	if resolved, rerr := filepath.EvalSymlinks(binPath); rerr == nil && resolved != binPath {
		if err := p.Allow(filepath.Dir(resolved), sandbox.Exec); err != nil {
			return p, fmt.Errorf("detect claude resolved binary dir: %w", err)
		}
	}

	optionals := []struct {
		path string
		perm sandbox.Permission
	}{
		{"~/.claude.json", sandbox.Write},
		{"~/.claude.json.lock", sandbox.Write},
		{"~/.claude", sandbox.Write},
		{"~/.local/state/claude", sandbox.Write},
		{"~/Library/Caches/claude-cli-nodejs", sandbox.Write},
		{"~/.local/bin", sandbox.Exec},
		// Narrowed to the claude install dir specifically — NOT the whole
		// ~/.local/share, which would grant exec (and thus, via process-exec's
		// implied read, broad visibility) into every other tool's local data.
		{"~/.local/share/claude", sandbox.Exec},
	}
	for _, opt := range optionals {
		if _, err := p.AllowOptional(opt.path, opt.perm); err != nil {
			return p, fmt.Errorf("detect claude optional path %q: %w", opt.path, err)
		}
	}
	return p, nil
}
