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
		{"~/.local/share", sandbox.Exec},
		{"~", sandbox.Read},
	}
	for _, opt := range optionals {
		if _, err := p.AllowOptional(opt.path, opt.perm); err != nil {
			return p, fmt.Errorf("detect claude optional path %q: %w", opt.path, err)
		}
	}
	return p, nil
}
