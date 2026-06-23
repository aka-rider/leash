//go:build darwin

package detect

import (
	"fmt"

	"github.com/aka-rider/leash/sandbox"
)

// Git adds git config paths to p if any are present.
func Git(p sandbox.ToolProfile) (sandbox.ToolProfile, error) {
	for _, path := range []string{"~/.gitconfig", "~/.config/git"} {
		if _, err := p.AllowOptional(path, sandbox.Read); err != nil {
			return p, fmt.Errorf("detect git path %q: %w", path, err)
		}
	}
	if _, err := p.AllowOptional("~/.ssh/known_hosts", sandbox.Read); err != nil {
		return p, fmt.Errorf("detect git ssh known_hosts: %w", err)
	}
	if _, err := p.AllowOptional("/Library/Developer/CommandLineTools", sandbox.Exec); err != nil {
		return p, fmt.Errorf("detect git clt: %w", err)
	}
	return p, nil
}
