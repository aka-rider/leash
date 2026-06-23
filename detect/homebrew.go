//go:build darwin

package detect

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aka-rider/leash/sandbox"
)

// Homebrew adds the Homebrew installation to p if brew is found in PATH.
func Homebrew(p sandbox.ToolProfile) (sandbox.ToolProfile, error) {
	brewPath, err := exec.LookPath("brew")
	if errors.Is(err, exec.ErrNotFound) {
		return p, nil
	}
	if err != nil {
		return p, fmt.Errorf("detect homebrew binary: %w", err)
	}

	prefix, err := exec.Command(brewPath, "--prefix").Output()
	if err != nil {
		return p, fmt.Errorf("detect homebrew prefix: %w", err)
	}
	basePath := strings.TrimSpace(string(prefix))

	p.AddEnv("HOMEBREW_PREFIX", basePath)
	p.AddEnv("HOMEBREW_CELLAR", filepath.Join(basePath, "Cellar"))
	p.AddEnv("HOMEBREW_REPOSITORY", basePath)

	if err := p.Allow(basePath, sandbox.Read); err != nil {
		return p, fmt.Errorf("detect homebrew core read: %w", err)
	}
	if _, err := p.AllowOptional(filepath.Join(basePath, "bin"), sandbox.Exec); err != nil {
		return p, fmt.Errorf("detect homebrew bin: %w", err)
	}
	for _, sub := range []string{"opt", "lib"} {
		if _, err := p.AllowOptional(filepath.Join(basePath, sub), sandbox.Read); err != nil {
			return p, fmt.Errorf("detect homebrew %s: %w", sub, err)
		}
	}
	// brew uses lock files when installing/managing Ruby.
	if _, err := p.AllowOptional(filepath.Join(basePath, "var", "homebrew"), sandbox.Write); err != nil {
		return p, fmt.Errorf("detect homebrew var: %w", err)
	}
	// brew manages its vendor Ruby under Library/Homebrew — needs both write
	// (to reinstall/update portable-ruby) and exec (to run the ruby binary).
	brewLib := filepath.Join(basePath, "Library", "Homebrew")
	if _, err := p.AllowOptional(brewLib, sandbox.Write); err != nil {
		return p, fmt.Errorf("detect homebrew library write: %w", err)
	}
	if _, err := p.AllowOptional(brewLib, sandbox.Exec); err != nil {
		return p, fmt.Errorf("detect homebrew library exec: %w", err)
	}
	return p, nil
}
