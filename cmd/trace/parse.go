package main

import (
	"encoding/json"
	"strings"
)

// Denial is one parsed, categorized sandbox denial.
type Denial struct {
	Category Category // human-readable class
	Target   string   // file path, host:port, mach service name, etc.
	Op       string   // raw seatbelt operation token (e.g. "file-read-data")
}

// Line renders one grepable output line: "<category>: <target>".
func (d Denial) Line() string {
	return d.Category.String() + ": " + d.Target
}

// logRecord is the minimal ndjson record shape from `log stream --style ndjson`.
type logRecord struct {
	EventMessage string `json:"eventMessage"`
}

// ParseRecord parses one line of `log stream --style ndjson` output.
// Returns (nil, nil) for non-JSON lines (including the banner), non-deny events,
// and lines without recognizable deny content.
// Real eventMessage format (critic-verified):
//
//	"Sandbox: sandbox-exec(22391) deny(1) process-exec* /bin/cat\nleash-<nonce>"
//
// The nonce is newline-appended; the op token follows "deny(N) "; the target
// is everything between the op and the trailing "\n<nonce>".
func ParseRecord(line []byte) (*Denial, error) {
	var rec logRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		// Not JSON — includes the plain-text banner line "Filtering the log data using …"
		return nil, nil
	}

	msg := rec.EventMessage
	if msg == "" {
		return nil, nil
	}

	// Find "deny("
	denyIdx := strings.Index(msg, "deny(")
	if denyIdx == -1 {
		return nil, nil
	}

	// Advance past "deny(N) " — find the ") " that closes the deny number
	rest := msg[denyIdx+len("deny("):]
	closeIdx := strings.Index(rest, ") ")
	if closeIdx == -1 {
		return nil, nil
	}
	rest = rest[closeIdx+2:] // now points at op token

	// Strip trailing nonce: everything from the first \n onwards
	if nl := strings.Index(rest, "\n"); nl != -1 {
		rest = rest[:nl]
	}
	rest = strings.TrimSpace(rest)

	// Split: op<space>target
	spaceIdx := strings.Index(rest, " ")
	var op, target string
	if spaceIdx == -1 {
		// No space: op only, no target (e.g. "process-fork")
		op = rest
	} else {
		op = rest[:spaceIdx]
		target = strings.TrimSpace(rest[spaceIdx+1:])
	}

	if op == "" {
		return nil, nil
	}

	return &Denial{
		Category: Categorize(op),
		Target:   target,
		Op:       op,
	}, nil
}
