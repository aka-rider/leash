// Package config loads and merges leash configuration from yaml, env, and CLI.
// No build tag — pure logic, testable on any platform.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aka-rider/leash/pkg/cli"
	"gopkg.in/yaml.v3"
)

// Config is the fully resolved, effective configuration for one sbx invocation.
type Config struct {
	// Detect is the list of tool detector names. Override-if-set (NOT union) — empty means not set.
	Detect []string `yaml:"detect"`
	// Read/Write/Exec are unioned across yaml + env + CLI grants.
	Read  []string `yaml:"read"`
	Write []string `yaml:"write"`
	Exec  []string `yaml:"exec"`
	// Network: true = allow outbound (default). Tracked via rawConfig.*bool for set-detection.
	Network bool `yaml:"network"`
	// ProxyEnv names from the host environment to forward (union).
	ProxyEnv []string `yaml:"proxy_env"`
	// ExtraEnv: per-key map merge; later sources override per-key.
	ExtraEnv map[string]string `yaml:"extra_env"`
	// Workspace: writable root. Empty → use cwd.
	Workspace string `yaml:"workspace"`
}

// rawConfig is the unmarshaled yaml with set-tracking on bool fields.
type rawConfig struct {
	Detect    []string          `yaml:"detect"`
	Read      []string          `yaml:"read"`
	Write     []string          `yaml:"write"`
	Exec      []string          `yaml:"exec"`
	Network   *bool             `yaml:"network"` // nil = not set in yaml
	ProxyEnv  []string          `yaml:"proxy_env"`
	ExtraEnv  map[string]string `yaml:"extra_env"`
	Workspace string            `yaml:"workspace"`
}

// Defaults returns the baseline configuration before any user overrides.
func Defaults() Config {
	return Config{
		Detect:  []string{"homebrew", "xcode", "git", "docker", "claude"},
		Network: true,
	}
}

// Find locates the config file to load, respecting precedence.
// Returns ("", false, nil) when no file is found (not an error).
// Returns ("", false, err) when an explicit path is set but missing.
func Find(cliPath string) (string, bool, error) {
	// $LEASH_CONFIG overrides everything (explicit, so must exist)
	if v := os.Getenv("LEASH_CONFIG"); v != "" {
		if _, err := os.Stat(v); err != nil {
			return "", false, fmt.Errorf("LEASH_CONFIG=%q: %w", v, err)
		}
		return v, true, nil
	}

	// --config is explicit, must exist
	if cliPath != "" {
		if _, err := os.Stat(cliPath); err != nil {
			return "", false, fmt.Errorf("--config %q: %w", cliPath, err)
		}
		return cliPath, true, nil
	}

	home, _ := os.UserHomeDir()
	// Search order: local first, then home
	candidates := []string{
		"./.leash.yaml",
		"./leash.yaml",
	}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, ".config", "leash", "leash.yaml"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, true, nil
		}
	}
	return "", false, nil
}

// Resolve applies the precedence chain defaults < yaml < env < CLI and returns the final Config.
func Resolve(cliP *cli.Parsed, cwd string) (Config, error) {
	eff := Defaults()

	// --- Load yaml file if present ---
	cfgPath := ""
	if cliP != nil {
		cfgPath = cliP.ConfigPath
	}
	path, found, err := Find(cfgPath)
	if err != nil {
		return Config{}, err
	}
	var raw rawConfig
	if found {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config %q: %w", path, err)
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return Config{}, fmt.Errorf("parse config %q: %w", path, err)
		}
		applyFile(&eff, raw)
	}

	// --- Env layer ---
	applyEnv(&eff)

	// --- CLI layer ---
	if cliP != nil {
		applyCLI(&eff, cliP)
	}

	// Workspace fallback: cwd
	if eff.Workspace == "" {
		eff.Workspace = cwd
	}

	dedupLists(&eff)
	return eff, nil
}

// applyFile merges yaml values into eff.
// Scalars: set only if present in yaml. Lists: union. detect: override.
func applyFile(eff *Config, raw rawConfig) {
	if len(raw.Detect) > 0 {
		eff.Detect = raw.Detect // override
	}
	eff.Read = append(eff.Read, raw.Read...)
	eff.Write = append(eff.Write, raw.Write...)
	eff.Exec = append(eff.Exec, raw.Exec...)
	if raw.Network != nil {
		eff.Network = *raw.Network
	}
	eff.ProxyEnv = append(eff.ProxyEnv, raw.ProxyEnv...)
	for k, v := range raw.ExtraEnv {
		if eff.ExtraEnv == nil {
			eff.ExtraEnv = make(map[string]string)
		}
		eff.ExtraEnv[k] = v
	}
	if raw.Workspace != "" {
		eff.Workspace = raw.Workspace
	}
}

// applyEnv merges LEASH_* environment variables.
func applyEnv(eff *Config) {
	if v := os.Getenv("LEASH_WORKSPACE"); v != "" {
		eff.Workspace = v
	}
	if v := os.Getenv("LEASH_NO_NETWORK"); isTruthy(v) {
		eff.Network = false
	}
	if v := os.Getenv("LEASH_DETECT"); v != "" {
		eff.Detect = splitList(v) // override
	}
	if v := os.Getenv("LEASH_READ"); v != "" {
		eff.Read = append(eff.Read, splitList(v)...)
	}
	if v := os.Getenv("LEASH_WRITE"); v != "" {
		eff.Write = append(eff.Write, splitList(v)...)
	}
	if v := os.Getenv("LEASH_EXEC"); v != "" {
		eff.Exec = append(eff.Exec, splitList(v)...)
	}
	if v := os.Getenv("LEASH_PROXY_ENV"); v != "" {
		eff.ProxyEnv = append(eff.ProxyEnv, splitList(v)...)
	}
}

// applyCLI merges CLI parsed values into eff.
// Scalars: set only if non-zero. Lists: union. detect: override when present.
func applyCLI(eff *Config, p *cli.Parsed) {
	if p.Workspace != "" {
		eff.Workspace = p.Workspace
	}
	if p.NoNetwork {
		eff.Network = false
	}
	if len(p.Detect) > 0 {
		eff.Detect = p.Detect // override
	}
	for _, g := range p.Grants {
		switch g.Perm {
		case cli.PermRead:
			eff.Read = append(eff.Read, g.Path)
		case cli.PermWrite:
			eff.Write = append(eff.Write, g.Path)
		case cli.PermExec:
			eff.Exec = append(eff.Exec, g.Path)
		}
	}
}

func dedupLists(eff *Config) {
	eff.Read = dedup(eff.Read)
	eff.Write = dedup(eff.Write)
	eff.Exec = dedup(eff.Exec)
	eff.ProxyEnv = dedup(eff.ProxyEnv)
	eff.Detect = dedup(eff.Detect)
}

func dedup(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	out := ss[:0]
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func splitList(s string) []string {
	// Accept both comma and colon as separators.
	s = strings.ReplaceAll(s, ",", ":")
	parts := strings.Split(s, ":")
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isTruthy(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "1" || s == "true" || s == "yes"
}
