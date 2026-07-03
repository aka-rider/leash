// Package cli parses the leash command line and assembles a Leash instance.
// No build tag — pure logic, testable on any platform.
package cli

import (
	"fmt"
	"os"
	"strings"

	leash "github.com/aka-rider/leash"
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
	TraceFile    string
	NoNetwork    bool
	Help         bool
	Worktree     bool   // --worktree was set
	WorktreeName string // name given to --worktree; always non-empty when Worktree is true

	// Env holds --env KEY=VALUE directives, keyed by KEY (last one wins on repeats).
	Env map[string]string
	// ProxyEnv holds --proxy-env NAME directives in encounter order.
	ProxyEnv []string

	// grants from +w/+r/+x/−w/−r/−x in encounter order
	Grants []Grant

	// child command — everything after the first bare token or --
	Command []string
}

// Parse tokenizes argv (os.Args[1:]) into a Parsed value.
// It uses a two-state machine: LEASH_OPTS → (first bare token or --) → CHILD.
// Once in CHILD state, every token is verbatim child argv.
// Exception: once --worktree NAME has been parsed, the first-bare-token
// shortcut is disabled — only an explicit -- can start CHILD state — so a
// forgotten -- is a parse error instead of a silently wrong worktree name
// and command line.
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
			val, err := lookAhead()
			if err != nil {
				return nil, err
			}
			p.Worktree = true
			p.WorktreeName = val
			i += 2

		case strings.HasPrefix(tok, "--worktree="):
			name := strings.TrimPrefix(tok, "--worktree=")
			if name == "" {
				return nil, fmt.Errorf("missing required argument after %q", tok)
			}
			p.Worktree = true
			p.WorktreeName = name
			i++

		case tok == "--trace-file":
			val, err := lookAhead()
			if err != nil {
				return nil, err
			}
			p.TraceFile = val
			i += 2

		case strings.HasPrefix(tok, "--trace-file="):
			p.TraceFile = strings.TrimPrefix(tok, "--trace-file=")
			i++

		case tok == "--env":
			val, err := lookAhead()
			if err != nil {
				return nil, err
			}
			if err := setEnv(p, val); err != nil {
				return nil, err
			}
			i += 2

		case strings.HasPrefix(tok, "--env="):
			if err := setEnv(p, strings.TrimPrefix(tok, "--env=")); err != nil {
				return nil, err
			}
			i++

		case tok == "--proxy-env":
			val, err := lookAhead()
			if err != nil {
				return nil, err
			}
			p.ProxyEnv = append(p.ProxyEnv, val)
			i += 2

		case strings.HasPrefix(tok, "--proxy-env="):
			name := strings.TrimPrefix(tok, "--proxy-env=")
			if name == "" {
				return nil, fmt.Errorf("missing required argument after %q", tok)
			}
			p.ProxyEnv = append(p.ProxyEnv, name)
			i++

		case strings.HasPrefix(tok, "--"):
			return nil, fmt.Errorf("unknown flag %q (if this is the child command's flag, put it after the command or use --)", tok)

		case p.Worktree:
			return nil, fmt.Errorf("unexpected token %q after --worktree %q (add -- to mark where the command starts)", tok, p.WorktreeName)

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

// setEnv parses "KEY=VALUE" from a --env directive and records it on p.Env.
// Returns an error when val has no '=' separator.
func setEnv(p *Parsed, val string) error {
	key, value, ok := strings.Cut(val, "=")
	if !ok || key == "" {
		return fmt.Errorf("--env value %q must be KEY=VALUE", val)
	}
	if p.Env == nil {
		p.Env = make(map[string]string)
	}
	p.Env[key] = value
	return nil
}

// Configure parses argv and assembles a ready-to-Execute leash.Leash.
// Returns (zero-value, parsed, nil) when parsed.Help is true or the command is empty — callers print usage.
func Configure(argv []string) (leash.Leash, *Parsed, error) {
	parsed, err := Parse(argv)
	if err != nil {
		return leash.Leash{}, nil, err
	}
	if parsed.Help || len(parsed.Command) == 0 {
		return leash.Leash{}, parsed, nil
	}

	l := leash.Leash{
		Program:  parsed.Command[0],
		Args:     parsed.Command[1:],
		Network:  !parsed.NoNetwork,
		ExtraEnv: parsed.Env,
		ProxyEnv: parsed.ProxyEnv,
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

// Usage returns the help text for both leash and leash-trace.
func Usage() string {
	return `Usage: leash [options] [+w PATH] [+r PATH] [+x PATH] [-w PATH] [-r PATH] [-x PATH] [--] <command> [args...]

Options:
  +w PATH              grant read+write to PATH (must exist; for exec also add +x PATH)
  +r PATH              grant read-only to PATH (must exist)
  +x PATH              grant exec to PATH (must exist; directory or file)
  -w PATH              deny write to PATH (overrides all allows)
  -r PATH              deny read to PATH (overrides all allows, including implicit cwd read; cwd write survives unless -w is also given)
  -x PATH              deny exec to PATH (overrides all allows)
  --env KEY=VALUE      set an extra environment variable in the sandbox (repeatable)
  --proxy-env NAME     forward NAME from the host environment into the sandbox (repeatable; errors if NAME is unset on the host)
  --worktree NAME      create a git worktree at <repo-parent>/<NAME>, grant read on the repo root and write on the worktree; requires -- before the command
  --no-network         deny all outbound network access
  --trace-file PATH    trace output file (default: ./leash-trace.log; - for stderr; leash-trace only)
  --help, -h           show this help

The current directory is read+write by default (implicit +w .).
Use -w . to make it read-only, or -r . -w . to remove all access to it.
--worktree NAME is the exception: the original directory stays read-only
and the new worktree gets write instead. NAME is mandatory and must be
followed by -- before the command, e.g. --worktree my-fix -- go test.
Nothing else is writable by default except /tmp and ~/Library/Caches.
Deny rules (-w/-r/-x) override all allows, including tool detector grants.
`
}
