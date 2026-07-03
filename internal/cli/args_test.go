package cli

import (
	"maps"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name             string
		argv             []string
		wantCmd          []string
		wantGrant        []Grant
		wantHelp         bool
		wantNet          bool // true = want NoNetwork
		wantTF           string
		wantEnv          map[string]string
		wantProxyEnv     []string
		wantWorktree     bool
		wantWorktreeName string
		wantErr          bool
	}{
		{
			name:    "bare command",
			argv:    []string{"claude", "--print"},
			wantCmd: []string{"claude", "--print"},
		},
		{
			name:      "write grant before command",
			argv:      []string{"+w", "/tmp/foo", "sh", "-c", "ls"},
			wantGrant: []Grant{{Perm: PermWrite, Path: "/tmp/foo"}},
			wantCmd:   []string{"sh", "-c", "ls"},
		},
		{
			name:      "read grant before command",
			argv:      []string{"+r", "/usr/local", "sh"},
			wantGrant: []Grant{{Perm: PermRead, Path: "/usr/local"}},
			wantCmd:   []string{"sh"},
		},
		{
			name:      "exec grant",
			argv:      []string{"+x", "/usr/local/bin", "sh"},
			wantGrant: []Grant{{Perm: PermExec, Path: "/usr/local/bin"}},
			wantCmd:   []string{"sh"},
		},
		{
			name: "multiple grants",
			argv: []string{"+w", "/a", "+r", "/b", "+x", "/c", "cmd"},
			wantGrant: []Grant{
				{Perm: PermWrite, Path: "/a"},
				{Perm: PermRead, Path: "/b"},
				{Perm: PermExec, Path: "/c"},
			},
			wantCmd: []string{"cmd"},
		},
		{
			name:      "deny read flag",
			argv:      []string{"-r", "/some/path", "sh"},
			wantGrant: []Grant{{Perm: PermRead, Path: "/some/path", Deny: true}},
			wantCmd:   []string{"sh"},
		},
		{
			name:      "deny write flag",
			argv:      []string{"-w", "/some/path", "sh"},
			wantGrant: []Grant{{Perm: PermWrite, Path: "/some/path", Deny: true}},
			wantCmd:   []string{"sh"},
		},
		{
			name:      "deny exec flag",
			argv:      []string{"-x", "/some/path", "sh"},
			wantGrant: []Grant{{Perm: PermExec, Path: "/some/path", Deny: true}},
			wantCmd:   []string{"sh"},
		},
		{
			name: "deny and allow mixed",
			argv: []string{"+r", "/allowed", "-r", "/denied", "sh"},
			wantGrant: []Grant{
				{Perm: PermRead, Path: "/allowed"},
				{Perm: PermRead, Path: "/denied", Deny: true},
			},
			wantCmd: []string{"sh"},
		},
		{
			name:    "claude --config is child's",
			argv:    []string{"claude", "--config", "x"},
			wantCmd: []string{"claude", "--config", "x"},
		},
		{
			name:    "-- separator makes everything child argv",
			argv:    []string{"--", "-w", "foo"},
			wantCmd: []string{"-w", "foo"},
		},
		{
			name:    "-- passes +w to child",
			argv:    []string{"--", "+w", "/x", "claude"},
			wantCmd: []string{"+w", "/x", "claude"},
		},
		{
			name:    "empty argv",
			argv:    []string{},
			wantCmd: nil,
		},
		{
			name:     "--help flag",
			argv:     []string{"--help"},
			wantHelp: true,
		},
		{
			name:     "-h flag",
			argv:     []string{"-h"},
			wantHelp: true,
		},
		{
			name:    "--no-network flag",
			argv:    []string{"--no-network", "sh"},
			wantNet: true,
			wantCmd: []string{"sh"},
		},
		{
			name:    "--trace-file space form",
			argv:    []string{"--trace-file", "out.log", "sh"},
			wantTF:  "out.log",
			wantCmd: []string{"sh"},
		},
		{
			name:    "--trace-file= form",
			argv:    []string{"--trace-file=out.log", "sh"},
			wantTF:  "out.log",
			wantCmd: []string{"sh"},
		},
		{
			name:    "--trace-file - (stderr sentinel)",
			argv:    []string{"--trace-file", "-", "sh"},
			wantTF:  "-",
			wantCmd: []string{"sh"},
		},
		{
			name:    "child argv contains -- again",
			argv:    []string{"--", "git", "log", "--", "file"},
			wantCmd: []string{"git", "log", "--", "file"},
		},
		{
			name:    "--env space form",
			argv:    []string{"--env", "FOO=bar", "sh"},
			wantEnv: map[string]string{"FOO": "bar"},
			wantCmd: []string{"sh"},
		},
		{
			name:    "--env= form",
			argv:    []string{"--env=FOO=bar", "sh"},
			wantEnv: map[string]string{"FOO": "bar"},
			wantCmd: []string{"sh"},
		},
		{
			name:    "--env value contains its own = signs",
			argv:    []string{"--env", "FOO=bar=baz", "sh"},
			wantEnv: map[string]string{"FOO": "bar=baz"},
			wantCmd: []string{"sh"},
		},
		{
			name:    "repeated --env keeps last value per key",
			argv:    []string{"--env", "FOO=1", "--env", "FOO=2", "sh"},
			wantEnv: map[string]string{"FOO": "2"},
			wantCmd: []string{"sh"},
		},
		{
			name:    "--env missing = is an error",
			argv:    []string{"--env", "FOOBAR", "sh"},
			wantErr: true,
		},
		{
			name:    "--env= missing = is an error",
			argv:    []string{"--env=FOOBAR", "sh"},
			wantErr: true,
		},
		{
			name:    "--env missing value EOF",
			argv:    []string{"--env"},
			wantErr: true,
		},
		{
			name:         "--proxy-env space form",
			argv:         []string{"--proxy-env", "HTTP_PROXY", "sh"},
			wantProxyEnv: []string{"HTTP_PROXY"},
			wantCmd:      []string{"sh"},
		},
		{
			name:         "--proxy-env= form",
			argv:         []string{"--proxy-env=HTTP_PROXY", "sh"},
			wantProxyEnv: []string{"HTTP_PROXY"},
			wantCmd:      []string{"sh"},
		},
		{
			name:         "repeated --proxy-env accumulates",
			argv:         []string{"--proxy-env", "A", "--proxy-env", "B", "sh"},
			wantProxyEnv: []string{"A", "B"},
			wantCmd:      []string{"sh"},
		},
		{
			name:    "--proxy-env missing value EOF",
			argv:    []string{"--proxy-env"},
			wantErr: true,
		},
		{
			name:    "--proxy-env= with empty value is an error",
			argv:    []string{"--proxy-env=", "sh"},
			wantErr: true,
		},
		{
			name:    "child +w is passed through",
			argv:    []string{"claude", "+w", "/x"},
			wantCmd: []string{"claude", "+w", "/x"},
		},
		// --worktree cases
		{
			name:         "--worktree space-separated name, with --",
			argv:         []string{"--worktree", "my-fix", "--", "sh"},
			wantWorktree: true, wantWorktreeName: "my-fix",
			wantCmd: []string{"sh"},
		},
		{
			name:         "--worktree= form, with --",
			argv:         []string{"--worktree=my-fix", "--", "sh"},
			wantWorktree: true, wantWorktreeName: "my-fix",
			wantCmd: []string{"sh"},
		},
		{
			name:    "--worktree immediately followed by -- is an error (name is mandatory)",
			argv:    []string{"--worktree", "--", "sh"},
			wantErr: true,
		},
		{
			name:    "--worktree with no name at all (EOF) is an error",
			argv:    []string{"--worktree"},
			wantErr: true,
		},
		{
			name:    "--worktree followed by a flag (no name given) is an error",
			argv:    []string{"--worktree", "--no-network", "sh"},
			wantErr: true,
		},
		{
			name:    "--worktree followed by a directive (no name given) is an error",
			argv:    []string{"--worktree", "+w", "/tmp", "sh"},
			wantErr: true,
		},
		{
			name:    "--worktree= with empty value is an error",
			argv:    []string{"--worktree=", "sh"},
			wantErr: true,
		},
		{
			name:         "other flags still parse normally between NAME and --",
			argv:         []string{"--worktree", "my-fix", "--no-network", "--", "sh"},
			wantWorktree: true, wantWorktreeName: "my-fix",
			wantNet: true,
			wantCmd: []string{"sh"},
		},
		{
			name:         "directives still parse normally between NAME and --",
			argv:         []string{"--worktree", "my-fix", "+w", "/tmp", "--", "sh"},
			wantWorktree: true, wantWorktreeName: "my-fix",
			wantGrant: []Grant{{Perm: PermWrite, Path: "/tmp"}},
			wantCmd:   []string{"sh"},
		},
		{
			name:         "--worktree NAME with nothing after (no --, no command) is not an error",
			argv:         []string{"--worktree", "my-fix"},
			wantWorktree: true, wantWorktreeName: "my-fix",
		},
		{
			name:         "--worktree NAME -- with nothing after is not an error",
			argv:         []string{"--worktree", "my-fix", "--"},
			wantWorktree: true, wantWorktreeName: "my-fix",
		},
		{
			name:    "regression: --worktree go test ./... with no -- must error, not silently misname the worktree",
			argv:    []string{"--worktree", "go", "test", "./..."},
			wantErr: true,
		},
		{
			name:    "regression: --worktree sh -c ... with no -- (original bug repro) must error",
			argv:    []string{"--worktree", "sh", "-c", "echo hi"},
			wantErr: true,
		},
		{
			name:    "regression: unambiguous explicit name still requires -- (accepted trade-off)",
			argv:    []string{"--worktree", "myname", "go", "test", "./..."},
			wantErr: true,
		},
		{
			name:         "happy path: --worktree NAME -- <command>",
			argv:         []string{"--worktree", "myname", "--", "go", "test", "./..."},
			wantWorktree: true, wantWorktreeName: "myname",
			wantCmd: []string{"go", "test", "./..."},
		},
		// Error cases
		{
			name:    "+w missing path EOF",
			argv:    []string{"+w"},
			wantErr: true,
		},
		{
			name:    "+w followed by a flag",
			argv:    []string{"+w", "--no-network", "sh"},
			wantErr: true,
		},
		{
			name:    "+w followed by --",
			argv:    []string{"+w", "--", "sh"},
			wantErr: true,
		},
		{
			name:    "+r missing path EOF",
			argv:    []string{"+r"},
			wantErr: true,
		},
		{
			name:    "-r missing path EOF",
			argv:    []string{"-r"},
			wantErr: true,
		},
		{
			name:    "-w followed by deny directive",
			argv:    []string{"-w", "-r", "sh"},
			wantErr: true,
		},
		{
			name:    "unknown flag before command",
			argv:    []string{"--badflag", "sh"},
			wantErr: true,
		},
		{
			name:    "--env value looks like flag",
			argv:    []string{"--env", "--no-network", "sh"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.argv)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (parsed: %+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slicesEqual(got.Command, tc.wantCmd) {
				t.Errorf("Command: got %v, want %v", got.Command, tc.wantCmd)
			}
			if !grantsEqual(got.Grants, tc.wantGrant) {
				t.Errorf("Grants: got %v, want %v", got.Grants, tc.wantGrant)
			}
			if got.Help != tc.wantHelp {
				t.Errorf("Help: got %v, want %v", got.Help, tc.wantHelp)
			}
			if got.NoNetwork != tc.wantNet {
				t.Errorf("NoNetwork: got %v, want %v", got.NoNetwork, tc.wantNet)
			}
			if got.TraceFile != tc.wantTF {
				t.Errorf("TraceFile: got %q, want %q", got.TraceFile, tc.wantTF)
			}
			if !maps.Equal(got.Env, tc.wantEnv) {
				t.Errorf("Env: got %v, want %v", got.Env, tc.wantEnv)
			}
			if !slicesEqual(got.ProxyEnv, tc.wantProxyEnv) {
				t.Errorf("ProxyEnv: got %v, want %v", got.ProxyEnv, tc.wantProxyEnv)
			}
			if got.Worktree != tc.wantWorktree {
				t.Errorf("Worktree: got %v, want %v", got.Worktree, tc.wantWorktree)
			}
			if got.WorktreeName != tc.wantWorktreeName {
				t.Errorf("WorktreeName: got %q, want %q", got.WorktreeName, tc.wantWorktreeName)
			}
		})
	}
}

func TestConfigure_NetworkDefaultsTrue(t *testing.T) {
	l, _, err := Configure([]string{"sh"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !l.Network {
		t.Error("Network should default to true")
	}
}

func TestConfigure_NoNetworkDisablesNetwork(t *testing.T) {
	l, _, err := Configure([]string{"--no-network", "sh"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l.Network {
		t.Error("--no-network should set Network=false")
	}
}

func TestConfigure_EnvAndProxyEnvWiring(t *testing.T) {
	l, _, err := Configure([]string{"--env", "FOO=bar", "--proxy-env", "HTTP_PROXY", "sh"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l.ExtraEnv["FOO"] != "bar" {
		t.Errorf("ExtraEnv[FOO] = %q, want %q", l.ExtraEnv["FOO"], "bar")
	}
	if !slicesEqual(l.ProxyEnv, []string{"HTTP_PROXY"}) {
		t.Errorf("ProxyEnv = %v, want [HTTP_PROXY]", l.ProxyEnv)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func grantsEqual(a, b []Grant) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
