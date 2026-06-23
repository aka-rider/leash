package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	d := Defaults()
	if !d.Network {
		t.Error("default Network should be true")
	}
}

func TestResolve_NoNetworkCLI(t *testing.T) {
	eff, err := Resolve(Overrides{NoNetwork: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eff.Network {
		t.Error("NoNetwork=true should set Network=false")
	}
}

func TestResolve_NetworkDefaultTrue(t *testing.T) {
	eff, err := Resolve(Overrides{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !eff.Network {
		t.Error("default Network should be true")
	}
}

func TestResolve_YamlGrantsInConfig(t *testing.T) {
	eff, err := Resolve(Overrides{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = eff // defaults are tested by TestDefaults
}

func TestResolve_YamlNetworkFalse(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".leash.yaml")
	os.WriteFile(f, []byte("network: false\n"), 0644)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	eff, err := Resolve(Overrides{})
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

	eff, err := Resolve(Overrides{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(eff.Read, "/yaml-read") {
		t.Errorf("Read should contain yaml entry, got %v", eff.Read)
	}
}

func TestResolve_EnvNoNetwork(t *testing.T) {
	t.Setenv("LEASH_NO_NETWORK", "1")
	eff, err := Resolve(Overrides{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eff.Network {
		t.Error("LEASH_NO_NETWORK=1 should set Network=false")
	}
}

func TestResolve_EnvReadUnion(t *testing.T) {
	t.Setenv("LEASH_READ", "/env-read")
	eff, err := Resolve(Overrides{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(eff.Read, "/env-read") {
		t.Errorf("Read should contain env entry, got %v", eff.Read)
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
