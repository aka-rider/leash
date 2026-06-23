//go:build darwin

package sandbox

import (
	"fmt"
	"sort"
	"strings"
)

// ProfileBuilder assembles a complete SBPL profile from system base rules,
// home, tmpdir, and zero or more tool snapshots.
type ProfileBuilder struct {
	home      string
	tmpDir    string
	snapshots []Snapshot
	// DenyMessage is embedded in the catch-all deny rule and is used by --trace to
	// filter this run's denials out of the kernel log. Defaults to "leash".
	DenyMessage string
	// NoNetwork omits the network-outbound allow block when true.
	NoNetwork bool
}

// NewProfileBuilder creates a builder with mandatory system paths.
func NewProfileBuilder(home string, tmpDir string) (*ProfileBuilder, error) {
	if home == "" {
		return nil, fmt.Errorf("profile builder: home is required")
	}
	if tmpDir == "" {
		return nil, fmt.Errorf("profile builder: tmpdir is required")
	}
	return &ProfileBuilder{
		home:        home,
		tmpDir:      tmpDir,
		DenyMessage: "leash",
	}, nil
}

// Add appends a tool snapshot to the builder.
func (b *ProfileBuilder) Add(s Snapshot) {
	b.snapshots = append(b.snapshots, s)
}

// Build produces the final SBPL profile string.
// macOS SBPL uses last-match-wins: deny entries are emitted AFTER all allow entries
// so they override any preceding allow for the same path.
func (b *ProfileBuilder) Build() (string, error) {
	msg := b.DenyMessage
	if msg == "" {
		msg = "leash"
	}

	var sb strings.Builder

	sb.WriteString("(version 1)\n")
	fmt.Fprintf(&sb, "(deny default (with message %q))\n", msg)

	// --- System base ---
	sb.WriteString(`
;; Process fundamentals (required for any process to start)
(allow file-read-metadata)
(allow sysctl-read)
(allow signal)
(allow process-info*)
(allow process-fork)

;; Root directory traversal (required by dyld/linker at process startup)
(allow file-read-data
  (literal "/")
  (literal "/private")
  (literal "/private/var"))

;; Dynamic linker + system libraries (read + map-executable)
(allow file-read* file-map-executable
  (subpath "/System")
  (subpath "/usr/lib")
  (subpath "/usr/share")
  (subpath "/var/db/dyld")
  (subpath "/Library/Frameworks")
  (subpath "/Library/Apple"))

;; System binaries (exec)
(allow process-exec
  (subpath "/usr/bin")
  (subpath "/usr/sbin")
  (subpath "/usr/libexec")
  (subpath "/bin")
  (subpath "/sbin"))

;; System binaries (read for shell builtins, scripting)
(allow file-read*
  (subpath "/usr/bin")
  (subpath "/usr/sbin")
  (subpath "/usr/libexec")
  (subpath "/bin")
  (subpath "/sbin"))

;; System config (READ-ONLY, not executable)
(allow file-read*
  (subpath "/private/etc")
  (subpath "/Library/Preferences")
  (subpath "/Library/Keychains")
  (subpath "/private/var/db/timezone")
  (subpath "/private/var/db/mds"))

`)
	fmt.Fprintf(&sb, `
;; Tmp (read + write)
(allow file-read* file-write*
  (subpath "/tmp")
  (subpath "/private/tmp")
  (subpath "/private/var/folders")
  (subpath "/var/folders")
  (subpath %q))
`, b.tmpDir)
	fmt.Fprintf(&sb, `
;; User library caches (read+write — Go build cache, npm, pip, etc.)
(allow file-read* file-write*
  (subpath %q))
`, b.home+"/Library/Caches")
	sb.WriteString(`
;; Devices (tty, null, random, fd — fd needed for bash process substitution)
(allow file-read* file-write*
  (regex "^/dev/(tty.*|null|zero|random|urandom|dtracehelper|fd/.*)"))
(allow file-read-metadata
  (literal "/dev/fd"))
(allow file-ioctl)

;; IPC (Keychain, Security.framework, DNS)
(allow mach-lookup)
(allow ipc-posix-shm*)
(allow file-read* (subpath "/private/var/run/mDNSResponder"))
`)

	// Network block — omitted when NoNetwork is set
	if !b.NoNetwork {
		sb.WriteString(`
;; Network (Full outbound allowed for dev tools/LLMs)
(allow system-socket)
(allow network-outbound)
(allow network-inbound (local ip "localhost:*"))
`)
	}

	// --- Tool profile allows ---
	for _, snap := range b.snapshots {
		if !hasEntries(snap.entries, false) {
			continue
		}
		sb.WriteString("\n;; Tool: " + snap.name + "\n")
		b.emitEntries(&sb, snap.entries, false)
	}

	// --- Explicit deny rules LAST (last-match-wins in SBPL — deny overrides all allows above) ---
	for _, snap := range b.snapshots {
		if !hasEntries(snap.entries, true) {
			continue
		}
		sb.WriteString("\n;; Deny: " + snap.name + "\n")
		b.emitEntries(&sb, snap.entries, true)
	}

	return sb.String(), nil
}

func hasEntries(entries []entry, deny bool) bool {
	for _, e := range entries {
		if e.deny == deny {
			return true
		}
	}
	return false
}

type groupKey struct {
	perm  Permission
	isDir bool
}

// emitEntries emits either deny or allow SBPL rules from entries, filtered by denyPass.
func (b *ProfileBuilder) emitEntries(sb *strings.Builder, entries []entry, denyPass bool) {
	groups := make(map[groupKey][]string)
	for _, e := range entries {
		if e.deny != denyPass {
			continue
		}
		k := groupKey{perm: e.perm, isDir: e.path.IsDir}
		groups[k] = append(groups[k], e.path.Resolved)
	}

	if len(groups) == 0 {
		return
	}

	keys := make([]groupKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].perm != keys[j].perm {
			return keys[i].perm < keys[j].perm
		}
		if keys[i].isDir != keys[j].isDir {
			return !keys[i].isDir
		}
		return false
	})

	verb := "allow"
	if denyPass {
		verb = "deny"
	}

	for _, k := range keys {
		paths := groups[k]
		sort.Strings(paths)

		var opsStr string
		if denyPass {
			opsStr = sbplDenyOps(k.perm)
		} else {
			opsStr = sbplOps(k.perm)
		}

		fmt.Fprintf(sb, "(%s %s", verb, opsStr)

		pathType := "literal"
		if k.isDir {
			pathType = "subpath"
		}

		for _, p := range paths {
			fmt.Fprintf(sb, "\n  (%s %q)", pathType, p)
		}
		sb.WriteString(")\n")
	}
}

// sbplOps returns the SBPL operation string for an allow rule.
func sbplOps(p Permission) string {
	switch {
	case p&Exec != 0:
		return "file-read* file-map-executable process-exec"
	case p&Write != 0:
		return "file-read* file-write*"
	default:
		return "file-read*"
	}
}

// sbplDenyOps returns the SBPL operation string for a deny rule.
// Deny rules are targeted: -r denies reads, -w denies writes, -x denies exec only.
func sbplDenyOps(p Permission) string {
	switch {
	case p&Exec != 0:
		return "file-map-executable process-exec"
	case p&Write != 0:
		return "file-write*"
	default:
		return "file-read*"
	}
}
