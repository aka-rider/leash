package cli

import (
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
		wantCfg          string
		wantTF           string
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
			name:    "--config space form",
			argv:    []string{"--config", "a.yaml", "sh"},
			wantCfg: "a.yaml",
			wantCmd: []string{"sh"},
		},
		{
			name:    "--config= form",
			argv:    []string{"--config=a.yaml", "sh"},
			wantCfg: "a.yaml",
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
			name:    "child +w is passed through",
			argv:    []string{"claude", "+w", "/x"},
			wantCmd: []string{"claude", "+w", "/x"},
		},
		// --worktree cases
		{
			name:         "--worktree bare, name from next token",
			argv:         []string{"--worktree", "my-fix", "sh"},
			wantWorktree: true, wantWorktreeName: "my-fix",
			wantCmd: []string{"sh"},
		},
		{
			name:         "--worktree= form",
			argv:         []string{"--worktree=my-fix", "sh"},
			wantWorktree: true, wantWorktreeName: "my-fix",
			wantCmd: []string{"sh"},
		},
		{
			name:         "--worktree bare before --, no name",
			argv:         []string{"--worktree", "--", "sh"},
			wantWorktree: true, wantWorktreeName: "",
			wantCmd: []string{"sh"},
		},
		{
			name:         "--worktree bare at end of argv, no name, no command",
			argv:         []string{"--worktree"},
			wantWorktree: true, wantWorktreeName: "",
		},
		{
			name:         "--worktree does not consume a flag as name",
			argv:         []string{"--worktree", "--no-network", "sh"},
			wantWorktree: true, wantWorktreeName: "",
			wantNet: true,
			wantCmd: []string{"sh"},
		},
		{
			name:         "--worktree does not consume a directive as name",
			argv:         []string{"--worktree", "+w", "/tmp", "sh"},
			wantWorktree: true, wantWorktreeName: "",
			wantGrant: []Grant{{Perm: PermWrite, Path: "/tmp"}},
			wantCmd:   []string{"sh"},
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
			name:    "--config missing value EOF",
			argv:    []string{"--config"},
			wantErr: true,
		},
		{
			name:    "--config value looks like flag",
			argv:    []string{"--config", "--no-network", "sh"},
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
			if got.ConfigPath != tc.wantCfg {
				t.Errorf("ConfigPath: got %q, want %q", got.ConfigPath, tc.wantCfg)
			}
			if got.TraceFile != tc.wantTF {
				t.Errorf("TraceFile: got %q, want %q", got.TraceFile, tc.wantTF)
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
