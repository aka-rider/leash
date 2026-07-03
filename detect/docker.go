//go:build darwin

package detect

import (
	"fmt"
	"os"

	"github.com/aka-rider/leash/sandbox"
)

// Docker adds Docker paths to p if a Docker installation is detected.
func Docker(p sandbox.ToolProfile) (sandbox.ToolProfile, error) {
	for _, dir := range []string{
		"/Applications/Docker.app/Contents/Resources/bin",
		"/Applications/Docker.app/Contents/Resources/cli-plugins",
	} {
		if _, err := p.AllowOptional(dir, sandbox.Exec); err != nil {
			return p, fmt.Errorf("detect docker path %q: %w", dir, err)
		}
	}

	if _, err := p.AllowOptional("~/.docker/bin", sandbox.Exec); err != nil {
		return p, fmt.Errorf("detect docker bin: %w", err)
	}
	if _, err := p.AllowOptional("~/.docker/config.json", sandbox.Read); err != nil {
		return p, fmt.Errorf("detect docker config: %w", err)
	}
	if _, err := p.AllowOptional("~/.docker/contexts", sandbox.Read); err != nil {
		return p, fmt.Errorf("detect docker contexts: %w", err)
	}
	if _, err := p.AllowOptional("~/.docker/run", sandbox.Write); err != nil {
		return p, fmt.Errorf("detect docker run: %w", err)
	}

	for _, sock := range []string{"/var/run/docker.sock", "/private/var/run/docker.sock"} {
		if info, err := os.Stat(sock); err == nil && !info.IsDir() {
			if err := p.Allow(sock, sandbox.Write); err != nil {
				return p, fmt.Errorf("detect docker socket %q: %w", sock, err)
			}
		}
	}
	return p, nil
}
