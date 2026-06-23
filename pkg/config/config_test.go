package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aka-rider/leash/pkg/cli"
)

func TestDefaults(t *testing.T) {
	d := Defaults()
	if !d.Network {
		t.Error("default Network should be true")
	}
	if len(d.Detect) == 0 {
		t.Error("default Detect should not be empty")
	}
}

func TestResolve_NilCLI_CwdFallback(t *testing.T) {
	cwd := t.TempDir()
	eff, err := Resolve(nil, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eff.Workspace != cwd {
		t.Errorf("Workspace: got %q, want %q (cwd)", eff.Workspace, cwd)
	}
}

func TestResolve_CLIWorkspaceOverridesCwd(t *testing.T) {
	cwd := t.TempDir()
	ws := t.TempDir()
	p := &cli.Parsed{Workspace: ws}
	eff, err := Resolve(p, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eff.Workspace != ws {
		t.Errorf("Workspace: got %q, want %q", eff.Workspace, ws)
	}
}

func TestResolve_NoNetworkCLI(t *testing.T) {
	eff, err := Resolve(&cli.Parsed{NoNetwork: true}, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eff.Network {
		t.Error("NoNetwork=true should set Network=false")
	}
}

func TestResolve_NetworkDefaultTrue(t *testing.T) {
	eff, err := Resolve(&cli.Parsed{}, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !eff.Network {
		t.Error("default Network should be true")
	}
}

func TestResolve_ListsUnion(t *testing.T) {
	// Grants from CLI are unioned with (empty) yaml and env
	p := &cli.Parsed{
		Grants: []cli.Grant{
			{Perm: cli.PermRead, Path: "/a"},
			{Perm: cli.PermWrite, Path: "/b"},
			{Perm: cli.PermExec, Path: "/c"},
		},
	}
	eff, err := Resolve(p, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(eff.Read, "/a") {
		t.Errorf("Read should contain /a, got %v", eff.Read)
	}
	if !contains(eff.Write, "/b") {
		t.Errorf("Write should contain /b, got %v", eff.Write)
	}
	if !contains(eff.Exec, "/c") {
		t.Errorf("Exec should contain /c, got %v", eff.Exec)
	}
}

func TestResolve_DetectCLIOverride(t *testing.T) {
	p := &cli.Parsed{Detect: []string{"git"}}
	eff, err := Resolve(p, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(eff.Detect) != 1 || eff.Detect[0] != "git" {
		t.Errorf("Detect should be overridden by CLI, got %v", eff.Detect)
	}
}

func TestResolve_DetectDefaultWhenNotSet(t *testing.T) {
	eff, err := Resolve(&cli.Parsed{}, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(eff.Detect) == 0 {
		t.Error("Detect should use defaults when not set")
	}
}

func TestResolve_YamlNetworkFalse(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".leash.yaml")
	os.WriteFile(f, []byte("network: false\n"), 0644)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	eff, err := Resolve(&cli.Parsed{}, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eff.Network {
		t.Error("yaml network:false should set Network=false")
	}
}

func TestResolve_YamlListUnion(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".leash.yaml")
	os.WriteFile(f, []byte("read:\n  - /yaml-read\n"), 0644)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	p := &cli.Parsed{Grants: []cli.Grant{{Perm: cli.PermRead, Path: "/cli-read"}}}
	eff, err := Resolve(p, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(eff.Read, "/yaml-read") {
		t.Errorf("Read should contain yaml entry, got %v", eff.Read)
	}
	if !contains(eff.Read, "/cli-read") {
		t.Errorf("Read should contain CLI grant, got %v", eff.Read)
	}
}

func TestResolve_DetectDedup(t *testing.T) {
	p := &cli.Parsed{Detect: []string{"git", "git", "homebrew"}}
	eff, err := Resolve(p, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	seen := make(map[string]int)
	for _, d := range eff.Detect {
		seen[d]++
	}
	for d, n := range seen {
		if n > 1 {
			t.Errorf("Detect has duplicate %q (%d times)", d, n)
		}
	}
}

func TestResolve_EnvNoNetwork(t *testing.T) {
	t.Setenv("LEASH_NO_NETWORK", "1")
	eff, err := Resolve(&cli.Parsed{}, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eff.Network {
		t.Error("LEASH_NO_NETWORK=1 should set Network=false")
	}
}

func TestResolve_EnvReadUnion(t *testing.T) {
	t.Setenv("LEASH_READ", "/env-read")
	p := &cli.Parsed{Grants: []cli.Grant{{Perm: cli.PermRead, Path: "/cli-read"}}}
	eff, err := Resolve(p, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(eff.Read, "/env-read") {
		t.Errorf("Read should contain env entry, got %v", eff.Read)
	}
	if !contains(eff.Read, "/cli-read") {
		t.Errorf("Read should contain CLI grant, got %v", eff.Read)
	}
}

func TestFind_ExplicitMissing(t *testing.T) {
	_, _, err := Find("/nonexistent/leash.yaml")
	if err == nil {
		t.Fatal("expected error for missing explicit config path")
	}
}

func TestFind_NoFile(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	_, found, err := Find("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("should not find a config file in empty dir")
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
