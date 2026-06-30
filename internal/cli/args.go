// Package cli parses the leash command line and assembles a Leash instance.
// No build tag — pure logic, testable on any platform.
package cli

import (
	"fmt"
	"os"
	"strings"

	leash "github.com/aka-rider/leash"
	"github.com/aka-rider/leash/config"
)

// Perm is the grant type for a +w/+r/+x or -w/-r/-x directive.
type Perm int

const (
	PermRead  Perm = iota
	PermWrite Perm = iota
	PermExec  Perm = iota
)

// Grant is a single +w/+r/+x (allow) or -w/-r/-x (deny) path directive.
type Grant struct {
	Perm Perm
	Path string
	Deny bool // true = deny rule (-w/-r/-x); false = allow rule (+w/+r/+x)
}

// Parsed is the result of tokenizing leash's argv.
type Parsed struct {
	// leash-level flags
	ConfigPath  string
	TraceFile   string
	NoNetwork   bool
	Help        bool
	Worktree    bool   // --worktree was set
	WorktreeName string // explicit name; empty = auto-generate

	// grants from +w/+r/+x/−w/−r/−x in encounter order
	Grants []Grant

	// child command — everything after the first bare token or --
	Command []string
}

// Parse tokenizes argv (os.Args[1:]) into a Parsed value.
// It uses a two-state machine: LEASH_OPTS → (first bare token or --) → CHILD.
// Once in CHILD state, every token is verbatim child argv.
func Parse(argv []string) (*Parsed, error) {
	p := &Parsed{}
	state := "opts"
	i := 0

	// lookAhead returns the next token when it is safe to use as a value argument.
	lookAhead := func() (string, error) {
		if i+1 >= len(argv) {
			return "", fmt.Errorf("missing required argument after %q", argv[i])
		}
		next := argv[i+1]
		if next == "--" || (strings.HasPrefix(next, "-") && next != "-") || isDirective(next) {
			return "", fmt.Errorf("argument after %q looks like a flag or directive: %q (use a real path)", argv[i], next)
		}
		return next, nil
	}

	for i < len(argv) {
		tok := argv[i]

		if state == "child" {
			p.Command = append(p.Command, tok)
			i++
			continue
		}

		// LEASH_OPTS state
		switch {
		case tok == "--":
			state = "child"
			i++

		case isDirective(tok):
			val, err := lookAhead()
			if err != nil {
				return nil, err
			}
			g := Grant{Path: val, Deny: tok[0] == '-'}
			switch tok[1] {
			case 'w':
				g.Perm = PermWrite
			case 'r':
				g.Perm = PermRead
			case 'x':
				g.Perm = PermExec
			}
			p.Grants = append(p.Grants, g)
			i += 2

		case tok == "--help" || tok == "-h":
			p.Help = true
			i++

		case tok == "--no-network":
			p.NoNetwork = true
			i++

		case tok == "--worktree":
			p.Worktree = true
			if i+1 < len(argv) {
				next := argv[i+1]
				if next != "--" && !strings.HasPrefix(next, "-") && !strings.HasPrefix(next, "+") {
					p.WorktreeName = next
					i++
				}
			}
			i++

		case strings.HasPrefix(tok, "--worktree="):
			p.Worktree = true
			p.WorktreeName = strings.TrimPrefix(tok, "--worktree=")
			i++

		case tok == "--config":
			val, err := lookAhead()
			if err != nil {
				return nil, err
			}
			p.ConfigPath = val
			i += 2

		case tok == "--trace-file":
			val, err := lookAhead()
			if err != nil {
				return nil, err
			}
			p.TraceFile = val
			i += 2

		case strings.HasPrefix(tok, "--config="):
			p.ConfigPath = strings.TrimPrefix(tok, "--config=")
			i++

		case strings.HasPrefix(tok, "--trace-file="):
			p.TraceFile = strings.TrimPrefix(tok, "--trace-file=")
			i++

		case strings.HasPrefix(tok, "--"):
			return nil, fmt.Errorf("unknown flag %q (if this is the child command's flag, put it after the command or use --)", tok)

		case !strings.HasPrefix(tok, "-") && !isDirective(tok):
			state = "child"
			p.Command = append(p.Command, tok)
			i++

		default:
			return nil, fmt.Errorf("unexpected token %q", tok)
		}
	}

	return p, nil
}

// Configure parses argv, resolves configuration, and assembles a ready-to-Execute leash.Leash.
// Returns (zero-value, parsed, nil) when parsed.Help is true or the command is empty — callers print usage.
func Configure(argv []string) (leash.Leash, *Parsed, error) {
	parsed, err := Parse(argv)
	if err != nil {
		return leash.Leash{}, nil, err
	}
	if parsed.Help || len(parsed.Command) == 0 {
		return leash.Leash{}, parsed, nil
	}

	cfg, err := config.Resolve(config.Overrides{
		ConfigPath: parsed.ConfigPath,
		NoNetwork:  parsed.NoNetwork,
	})
	if err != nil {
		return leash.Leash{}, nil, err
	}

	l := leash.Leash{
		Program:  parsed.Command[0],
		Args:     parsed.Command[1:],
		Network:  cfg.Network,
		Reads:    existing(cfg.Read),
		Writes:   existing(cfg.Write),
		Execs:    existing(cfg.Exec),
		ExtraEnv: cfg.ExtraEnv,
		ProxyEnv: cfg.ProxyEnv,
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
	}

	for _, g := range parsed.Grants {
		switch {
		case g.Deny && g.Perm == PermRead:
			l.DenyReads = append(l.DenyReads, g.Path)
		case g.Deny && g.Perm == PermWrite:
			l.DenyWrites = append(l.DenyWrites, g.Path)
		case g.Deny && g.Perm == PermExec:
			l.DenyExecs = append(l.DenyExecs, g.Path)
		case g.Perm == PermRead:
			l.Reads = append(l.Reads, g.Path)
		case g.Perm == PermWrite:
			l.Writes = append(l.Writes, g.Path)
		case g.Perm == PermExec:
			l.Execs = append(l.Execs, g.Path)
		}
	}

	return l, parsed, nil
}

// isDirective reports whether tok is one of the +w/+r/+x or -w/-r/-x directive prefixes.
func isDirective(tok string) bool {
	if len(tok) != 2 {
		return false
	}
	return (tok[0] == '+' || tok[0] == '-') && (tok[1] == 'w' || tok[1] == 'r' || tok[1] == 'x')
}

// existing returns only the paths that currently exist on disk.
// Used for config-file paths (yaml/env) which are optional by convention.
func existing(paths []string) []string {
	var out []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// Usage returns the help text for both leash and leash-trace.
func Usage() string {
	return `Usage: leash [options] [+w PATH] [+r PATH] [+x PATH] [-w PATH] [-r PATH] [-x PATH] [--] <command> [args...]

Options:
  +w PATH              grant read+write to PATH (must exist; for exec also add +x PATH)
  +r PATH              grant read-only to PATH (must exist)
  +x PATH              grant exec to PATH (must exist; directory or file)
  -w PATH              deny write to PATH (overrides all allows)
  -r PATH              deny read to PATH (overrides all allows, including implicit cwd read)
  -x PATH              deny exec to PATH (overrides all allows)
  --worktree [NAME]    create a git worktree at <repo-parent>/<NAME> and grant write; NAME is auto-generated if omitted
  --no-network         deny all outbound network access
  --trace-file PATH    trace output file (default: ./leash-trace.log; - for stderr; leash-trace only)
  --config PATH        config file (default: searches .leash.yaml, leash.yaml, ~/.config/leash/leash.yaml)
  --help, -h           show this help

The current directory is always readable by default (implicit +r .).
Use -r . to remove that default read grant.
Nothing is writable by default except /tmp and ~/Library/Caches.
Use +w . to make the current directory writable.
Deny rules (-w/-r/-x) override all allows, including tool detector grants.
`
}
