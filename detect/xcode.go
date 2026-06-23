//go:build darwin

package detect

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aka-rider/leash/sandbox"
)

// Xcode adds the active Xcode developer tools directory to p.
func Xcode(p sandbox.ToolProfile) (sandbox.ToolProfile, error) {
	out, err := exec.Command("xcode-select", "-p").Output()
	if err != nil {
		return p, nil
	}
	devDir := strings.TrimSpace(string(out))
	if devDir == "" {
		return p, nil
	}
	if _, err := os.Stat(devDir); err != nil {
		return p, nil
	}

	if err := p.Allow(devDir, sandbox.Exec); err != nil {
		return p, fmt.Errorf("detect xcode developer dir %q: %w", devDir, err)
	}
	if bundle := xcodeAppBundle(devDir); bundle != "" {
		if _, err := p.AllowOptional(bundle, sandbox.Exec); err != nil {
			return p, fmt.Errorf("detect xcode app bundle %q: %w", bundle, err)
		}
	}
	return p, nil
}

func xcodeAppBundle(devDir string) string {
	dir := filepath.Dir(devDir)
	for dir != "/" && dir != "." {
		if strings.HasSuffix(dir, ".app") {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return ""
}
