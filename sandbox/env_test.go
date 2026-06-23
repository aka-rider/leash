//go:build darwin

package sandbox

import (
	"strings"
	"testing"
)

// toMap converts a "KEY=value" slice to a map for easy lookup in tests.
func toMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

// --- MergeEnv tests ---

func TestMergeEnv_LayerPrecedence(t *testing.T) {
	// All layers set the same key; extra (last) must win.
	t.Setenv("PROX_KEY", "proxy_val")

	base := []string{"X=base", "PATH=/usr/bin"}
	snap := Snapshot{env: []string{"X=tool"}}
	proxy := []string{"PROX_KEY"}
	extra := map[string]string{"X": "extra"}

	env, err := MergeEnv(base, []Snapshot{snap}, proxy, extra, nil)
	if err != nil {
		t.Fatalf("MergeEnv: %v", err)
	}
	m := toMap(env)
	if m["X"] != "extra" {
		t.Errorf("extra should win; got X=%q", m["X"])
	}
	if m["PROX_KEY"] != "proxy_val" {
		t.Errorf("proxy var not forwarded; got PROX_KEY=%q", m["PROX_KEY"])
	}
}

func TestMergeEnv_MissingProxyEnv(t *testing.T) {
	// ProxyEnv name absent from host must return an error.
	_, err := MergeEnv(nil, nil, []string{"LEASH_TEST_DEFINITELY_NOT_SET_XYZ"}, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing proxy env, got nil")
	}
}

func TestMergeEnv_PathExtensionDedupsAndAppends(t *testing.T) {
	base := []string{"PATH=/usr/bin:/bin"}
	extra := []string{"/usr/bin", "/opt/new/bin"} // /usr/bin is dup, /opt/new/bin is new

	env, err := MergeEnv(base, nil, nil, nil, extra)
	if err != nil {
		t.Fatalf("MergeEnv: %v", err)
	}
	m := toMap(env)
	parts := strings.Split(m["PATH"], ":")

	count := 0
	for _, p := range parts {
		if p == "/usr/bin" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("/usr/bin should appear once; got %d time(s) in PATH=%q", count, m["PATH"])
	}
	if !strings.Contains(m["PATH"], "/opt/new/bin") {
		t.Errorf("/opt/new/bin should be in PATH; got %q", m["PATH"])
	}
}

// --- ExtraPathDirs tests ---

func TestExtraPathDirs_SelectsBinDirsOnly(t *testing.T) {
	snap := Snapshot{
		entries: []entry{
			{path: Path{Resolved: "/opt/tool/bin", IsDir: true}, perm: Exec, deny: false},      // should include
			{path: Path{Resolved: "/opt/tool/lib", IsDir: true}, perm: Exec, deny: false},      // non-bin name
			{path: Path{Resolved: "/opt/read/bin", IsDir: true}, perm: Read, deny: false},      // not Exec
			{path: Path{Resolved: "/opt/deny/bin", IsDir: true}, perm: Exec, deny: true},       // deny
			{path: Path{Resolved: "/opt/tool/bin/exe", IsDir: false}, perm: Exec, deny: false}, // file not dir
		},
	}

	dirs := ExtraPathDirs([]Snapshot{snap})

	if len(dirs) != 1 || dirs[0] != "/opt/tool/bin" {
		t.Errorf("expected [\"/opt/tool/bin\"]; got %v", dirs)
	}
}

func TestExtraPathDirs_DedupsAndSorts(t *testing.T) {
	snap1 := Snapshot{
		entries: []entry{
			{path: Path{Resolved: "/z/bin", IsDir: true}, perm: Exec, deny: false},
			{path: Path{Resolved: "/a/bin", IsDir: true}, perm: Exec, deny: false},
		},
	}
	snap2 := Snapshot{
		entries: []entry{
			{path: Path{Resolved: "/z/bin", IsDir: true}, perm: Exec, deny: false}, // dup
			{path: Path{Resolved: "/m/bin", IsDir: true}, perm: Exec, deny: false},
		},
	}

	dirs := ExtraPathDirs([]Snapshot{snap1, snap2})

	want := []string{"/a/bin", "/m/bin", "/z/bin"} // sorted, deduped
	if len(dirs) != len(want) {
		t.Fatalf("expected %v; got %v", want, dirs)
	}
	for i, d := range dirs {
		if d != want[i] {
			t.Errorf("dirs[%d]: want %q, got %q", i, want[i], d)
		}
	}
}
