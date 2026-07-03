//go:build darwin

package detect

import (
	"fmt"

	"github.com/aka-rider/leash/sandbox"
)

// NPM adds npm/nvm paths to p if any are present.
func NPM(p sandbox.ToolProfile) (sandbox.ToolProfile, error) {
	for _, path := range []string{"~/.npmrc", "~/.npm"} {
		if _, err := p.AllowOptional(path, sandbox.Read); err != nil {
			return p, fmt.Errorf("detect npm path %q: %w", path, err)
		}
	}
	if _, err := p.AllowOptional("~/.nvm", sandbox.Exec); err != nil {
		return p, fmt.Errorf("detect npm nvm exec: %w", err)
	}
	return p, nil
}
