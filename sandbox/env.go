//go:build darwin

package sandbox

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
)

// BaseEnv builds a minimal, scrubbed environment from scratch.
// Nothing from the host process leaks unless explicitly injected.
func BaseEnv(home, tmpDir string) []string {
	userName := "unknown"
	if u, err := user.Current(); err == nil {
		userName = u.Username
	}

	hostPath := os.Getenv("PATH")
	if hostPath == "" {
		hostPath = "/usr/bin:/bin"
	}
	env := []string{
		"HOME=" + home,
		"TMPDIR=" + tmpDir,
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"USER=" + userName,
		"LOGNAME=" + userName,
		"SHELL=/bin/sh",
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"FORCE_COLOR=1",
		"PATH=" + hostPath,
	}

	// Terminal env — proxy from host if present
	for _, key := range []string{"TERM_PROGRAM", "TERM_PROGRAM_VERSION", "TERM_FEATURES"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}

	// XPC vars for system daemon IPC (Keychain, etc.)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "XPC_") {
			env = append(env, kv)
		}
	}

	return env
}

// MergeEnv produces the final scrubbed environment by merging all layers in order.
// Later layers override earlier layers. ProxyEnv names must exist in the host
// environment or an error is returned.
func MergeEnv(
	base []string,
	snapshots []Snapshot,
	proxyEnv []string,
	extraEnv map[string]string,
	extraPathDirs []string,
) ([]string, error) {
	envMap := make(map[string]string, len(base)+16)
	insertOrder := make([]string, 0, len(base)+16)

	set := func(key, value string) {
		if _, exists := envMap[key]; !exists {
			insertOrder = append(insertOrder, key)
		}
		envMap[key] = value
	}

	// 1. Base
	for _, kv := range base {
		k, v, _ := strings.Cut(kv, "=")
		set(k, v)
	}

	// 2. Tool profile env
	for _, snap := range snapshots {
		for _, kv := range snap.env {
			k, v, _ := strings.Cut(kv, "=")
			set(k, v)
		}
	}

	// 3. ProxyEnv — names must exist in host env
	for _, name := range proxyEnv {
		val, ok := os.LookupEnv(name)
		if !ok {
			return nil, fmt.Errorf("proxy env %q not found in host environment", name)
		}
		set(name, val)
	}

	// 4. ExtraEnv
	for k, v := range extraEnv {
		set(k, v)
	}

	// Extend PATH with extra dirs
	if len(extraPathDirs) > 0 {
		currentPath := envMap["PATH"]
		parts := strings.Split(currentPath, ":")
		seen := make(map[string]bool, len(parts))
		for _, p := range parts {
			seen[p] = true
		}
		for _, d := range extraPathDirs {
			if !seen[d] {
				parts = append(parts, d)
				seen[d] = true
			}
		}
		envMap["PATH"] = strings.Join(parts, ":")
	}

	result := make([]string, 0, len(insertOrder))
	for _, k := range insertOrder {
		result = append(result, k+"="+envMap[k])
	}
	return result, nil
}

// ExtraPathDirs collects directories from snapshots that should be added to PATH.
// Only directories whose base name is "bin" are included.
func ExtraPathDirs(snapshots []Snapshot) []string {
	var dirs []string
	seen := make(map[string]bool)
	for _, snap := range snapshots {
		for _, e := range snap.entries {
			if e.deny {
				continue
			}
			if e.perm&Exec != 0 && e.path.IsDir {
				if filepath.Base(e.path.Resolved) == "bin" && !seen[e.path.Resolved] {
					dirs = append(dirs, e.path.Resolved)
					seen[e.path.Resolved] = true
				}
			}
		}
	}
	sort.Strings(dirs)
	return dirs
}
