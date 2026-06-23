// Package cli parses the sbx command line.
// No build tag — pure logic, testable on any platform.
package cli

import (
	"fmt"
	"strings"
)

// Perm is the grant type for a +w/+r/+x directive.
type Perm int

const (
	PermRead  Perm = iota
	PermWrite Perm = iota
	PermExec  Perm = iota
)

// Grant is a single +w/+r/+x path grant.
type Grant struct {
	Perm Perm
	Path string
}

// Parsed is the result of tokenizing sbx's argv.
type Parsed struct {
	// sbx-level flags
	ConfigPath string
	TraceFile  string
	Workspace  string
	Trace      bool
	NoNetwork  bool
	Help       bool
	Detect     []string // --detect=a,b (comma-split, repeatable; later overrides earlier)

	// grants from +w/+r/+x in encounter order
	Grants []Grant

	// child command — everything after the first bare token or --
	Command []string
}

// Parse tokenizes argv (os.Args[1:]) into a Parsed value.
// It uses a two-state machine: SBX_OPTS → (first bare token or --) → CHILD.
// Once in CHILD state, every token is verbatim child argv.
func Parse(argv []string) (*Parsed, error) {
	p := &Parsed{}
	state := "opts"
	i := 0

	// lookAheadOK returns true when the next token is safe to use as a value argument
	// (not a flag, not a directive, not the -- separator).
	lookAhead := func() (string, error) {
		if i+1 >= len(argv) {
			return "", fmt.Errorf("missing required argument after %q", argv[i])
		}
		next := argv[i+1]
		// Allow bare "-" (stdin/stderr sentinel). Reject "--", other "-*" flags, and directives.
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

		// SBX_OPTS state
		switch {
		case tok == "--":
			state = "child"
			i++

		case tok == "+w" || tok == "+r" || tok == "+x":
			val, err := lookAhead()
			if err != nil {
				return nil, err
			}
			g := Grant{Path: val}
			switch tok {
			case "+w":
				g.Perm = PermWrite
			case "+r":
				g.Perm = PermRead
			case "+x":
				g.Perm = PermExec
			}
			p.Grants = append(p.Grants, g)
			i += 2

		case tok == "--help" || tok == "-h":
			p.Help = true
			i++

		case tok == "--trace":
			p.Trace = true
			i++

		case tok == "--no-network":
			p.NoNetwork = true
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

		case tok == "--workspace":
			val, err := lookAhead()
			if err != nil {
				return nil, err
			}
			p.Workspace = val
			i += 2

		case tok == "--detect":
			val, err := lookAhead()
			if err != nil {
				return nil, err
			}
			p.Detect = strings.Split(val, ",")
			i += 2

		case strings.HasPrefix(tok, "--config="):
			p.ConfigPath = strings.TrimPrefix(tok, "--config=")
			i++

		case strings.HasPrefix(tok, "--trace-file="):
			p.TraceFile = strings.TrimPrefix(tok, "--trace-file=")
			i++

		case strings.HasPrefix(tok, "--workspace="):
			p.Workspace = strings.TrimPrefix(tok, "--workspace=")
			i++

		case strings.HasPrefix(tok, "--detect="):
			p.Detect = strings.Split(strings.TrimPrefix(tok, "--detect="), ",")
			i++

		case strings.HasPrefix(tok, "--"):
			// Unknown sbx flag before any bare token is an error.
			return nil, fmt.Errorf("unknown flag %q (if this is the child command's flag, put it after the command or use --)", tok)

		case !strings.HasPrefix(tok, "-") && !isDirective(tok):
			// First bare token: this and everything after is the child command.
			state = "child"
			p.Command = append(p.Command, tok)
			i++

		default:
			return nil, fmt.Errorf("unexpected token %q", tok)
		}
	}

	return p, nil
}

// isDirective reports whether tok is one of the +w/+r/+x directive prefixes.
func isDirective(tok string) bool {
	return tok == "+w" || tok == "+r" || tok == "+x"
}

// Usage returns the one-line help text.
func Usage() string {
	return `Usage: sbx [options] [+w PATH] [+r PATH] [+x PATH] [--] <command> [args...]

Options:
  +w PATH              grant read+write to PATH (must exist; for exec also add +x PATH)
  +r PATH              grant read-only to PATH (must exist)
  +x PATH              grant exec to PATH (must exist; directory or file)
  --workspace PATH     set the sandbox workspace root (default: current directory)
  --no-network         deny all outbound network access
  --trace              capture denied operations to a trace file
  --trace-file PATH    trace output file (default: ./sbx-trace.log; - for stderr)
  --detect LIST        comma-separated detectors: homebrew,xcode,git,docker,claude,...
  --config PATH        config file (default: searches .leash.yaml, leash.yaml, ~/.config/leash/leash.yaml)
  --help, -h           show this help

Nothing is writable by default except /tmp and ~/Library/Caches.
Use +w . to make the current directory writable.
+w grants read+write only; use +x on the same path to also allow exec.
`
}
