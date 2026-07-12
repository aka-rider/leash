# Plan

## Goal
Expand `README.md` so a reader can predict exactly what environment variables, network access, and `leash-trace` output they get, without reading Go source — covering the scrubbed from-scratch child environment, the true default network semantics, and `leash-trace`'s full usage/output/degradation behavior.

## Context
All facts below were verified by Read against the worktree `/Users/xiii/Developer/orqestra-wt/leash/4-document-leash-cli-usage-gaps-env`. This is a docs-only change; the Worker touches **only** `README.md` in that worktree.

- **README today** (`README.md`): intro (1–8); Installation (10–22); `## Usage` fenced examples block (26–40); directive table (42–49) + prose incl. Options line (51–66); interactive-commands note (68–70); `## ⚠ Warning` redirection (72–86); `## How it works` (88–96) whose second paragraph (94–96) is the only current `leash-trace` mention; `## Use as a library` (98–120); `## License` (122–124). Existing voice is terse and warning-happy — match it.
- **Environment** (`sandbox/env.go:14-55` `BaseEnv`, `57-127` `MergeEnv`): the child env is built from scratch, not inherited. Always set: `HOME`, `TMPDIR`, `LANG=en_US.UTF-8`, `LC_ALL=en_US.UTF-8`, `USER`, `LOGNAME`, `SHELL=/bin/sh`, `TERM=xterm-256color`, `COLORTERM=truecolor`, `FORCE_COLOR=1`, and `PATH` (host `PATH`, or `/usr/bin:/bin` if unset). Proxied from host only if present: `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`, `TERM_FEATURES`. Always forwarded: all host `XPC_*` vars (system daemon IPC / Keychain). Everything else must be injected via `--env KEY=VALUE` or `--proxy-env NAME`; `MergeEnv` returns an error if a `--proxy-env` name is absent from the host env (`env.go:92-98`).
- **Network** (`sandbox/builder.go:144-152`): by default the profile emits `(allow system-socket)`, `(allow network-outbound)`, `(allow network-inbound (local ip "localhost:*"))` — outbound fully allowed, inbound allowed only on `localhost`. `--no-network` omits the **entire** block, so both inbound and outbound are removed. (README line 35's current comment "block all outbound network" is incomplete and gets corrected.)
- **leash-trace** (`cmd/trace/main.go`, `cmd/trace/trace.go`, `cmd/trace/parse.go`, `cmd/trace/category.go`): runs the command exactly like `leash` but attaches a per-run-nonce-filtered `log stream --style ndjson --level debug` watcher. Output line format is `<category>: <target>` (`parse.go:15-18`), target = file path / host:port / mach service name. Categories: `read`, `write`, `exec`, `network`, `mach`, `ipc`, `other` (`category.go:10-38`). Default sink `./leash-trace.log`; `--trace-file PATH` redirects; `--trace-file -` = stderr (`main.go:81-96`). A real path uses `O_CREATE|O_EXCL` → refuses to overwrite with `trace file already exists: … (delete it or choose a different name)` (`main.go:88-92`). Requires the macOS `log` binary; if `log` is missing or `log stream` fails to start, it degrades gracefully — runs the child directly and writes `# trace unavailable: …` to the sink, preserving the child's exit code (`trace.go:47-66`). After the child exits it drains the stream ~3s (`trace.go:17` `defaultDrain = 3 * time.Second`) to catch late denials, so traced runs take ~3s longer.
- **Tool detection** (`execute_darwin.go:47-51`): unconditionally probes, in this order, `detect.Claude, detect.Homebrew, detect.Docker, detect.Git, detect.NPM, detect.Xcode, detect.Go, detect.Python, detect.Rust` — i.e. claude, docker, git, go, homebrew, npm, python, rust, xcode. Detection is unconditional; each detected tool contributes its own per-tool grants.
- **`make lint`** (`Makefile:19-21`): `gofmt -l .` check + `go vet ./...`. A `.md`-only edit cannot affect either, so it must still exit 0.

## Constraints
- Edit **only** `README.md`; do not create `docs/usage.md` or any other file (reviewer decision 1).
- Do not touch any `.go` file — the `leash-trace --help` misnaming is out of scope (issue #5, decision 3); document actual current behavior, not the desired behavior.
- Tool-detection coverage is a **named list plus a one-line note only** — no per-tool grant tables (decision 2).
- Do not restructure or reword sections that are already accurate (directives table, worktree prose, library section) beyond the additions specified here.
- Preserve the existing terse, warning-happy voice; do not add marketing prose.

## Risks
- **Documenting aspirational instead of actual behavior.** Mitigation: every claim traces to a cited `file:line` in Context; the Verification proofread pass re-checks each against source.
- **A grep in "Done when" matching pre-existing text elsewhere and giving a false pass.** Mitigation: each package's Done-when pins the match to its new section by pairing the grep with the required surrounding phrase/meaning (e.g. `localhost` must carry the inbound-default meaning; `already exists` must be in the trace section).

## Work Packages

### 1. Add a `leash-trace` example and correct the `--no-network` comment in the Usage block
**Steps:**
1. In `README.md`, inside the fenced `## Usage` examples code block (currently lines 30–40), add one runnable line, e.g. `leash-trace go test ./...                        # run like leash; log kernel denials to ./leash-trace.log`.
2. In the same block, change the `--no-network` example comment (line 35) from "block all outbound network" to reflect that it removes both inbound and outbound, e.g. `# remove all network access (inbound and outbound)`.
3. Keep column alignment of the trailing `#` comments consistent with the surrounding lines.

**Done when:**
- `grep -n "leash-trace" README.md` shows the `leash-trace …` invocation, and that line falls inside the fenced `## Usage` examples code block.
- `grep -n "no-network" README.md` shows the corrected comment no longer says only "outbound".

### 2. Add an Environment section documenting the scrubbed, from-scratch child env
**Steps:**
1. Add a new `## Environment` H2 section (place it after the `## ⚠ Warning` section and before `## How it works`) stating that the sandboxed child gets a **scrubbed**, built-from-scratch environment — the host's variables do NOT leak in.
2. List the always-set defaults: `PATH` (inherited from the host, or `/usr/bin:/bin` if unset), `HOME`, `TMPDIR`, `LANG`/`LC_ALL` (`en_US.UTF-8`), `USER`/`LOGNAME`, `SHELL` (`/bin/sh`), the terminal vars `TERM`/`COLORTERM`/`FORCE_COLOR`, `TERM_PROGRAM*` (only when present on the host), and all host `XPC_*` vars (system daemon IPC / Keychain).
3. State that everything else is dropped and must be injected explicitly with `--env KEY=VALUE` or forwarded from the host with `--proxy-env NAME`, and that `--proxy-env` errors if the named variable is absent from the host environment.

**Done when:**
- `grep -n "scrubbed" README.md` hits within the new `## Environment` section.
- `grep -n "proxy-env" README.md` shows the note that a missing `--proxy-env` name is an error.
- The section names each of: `PATH`, `HOME`, `TMPDIR`, `LANG`, `USER`, `SHELL`, `TERM_PROGRAM`, `XPC_`.

### 3. Add a Network section stating the true default and `--no-network` semantics
**Steps:**
1. Add a new `## Network` H2 section (adjacent to `## Environment`) stating the default: outbound is fully allowed; inbound is allowed **only on `localhost`**.
2. State that `--no-network` removes the entire network allowance — **both** inbound and outbound.

**Done when:**
- `grep -n "localhost" README.md` hits in the new `## Network` section with the meaning "inbound allowed only on localhost by default".
- The section explicitly says `--no-network` removes both inbound and outbound.

### 4. Expand the `leash-trace` documentation (replace the terse "How it works" paragraph)
**Steps:**
1. Replace the current `leash-trace` paragraph in `## How it works` (lines 94–96) with fuller documentation (either an expanded paragraph there or a dedicated `## Tracing denials` section — keep it terse).
2. Document the output line format `<category>: <target>` and enumerate all categories: `read`, `write`, `exec`, `network`, `mach`, `ipc`, `other`.
3. Document sinks: default `./leash-trace.log`; `--trace-file PATH` redirects; `--trace-file -` writes to stderr; a real `--trace-file` path refuses to overwrite an existing file and errors with `trace file already exists`.
4. Document the `log` dependency and graceful degradation: `leash-trace` needs the macOS `log` binary; if it is unavailable it **degrades gracefully** — runs the child directly and prints a `# trace unavailable: …` note (child exit code preserved).
5. Note the ~3s post-exit drain: traced runs take about 3 seconds longer than the same command under `leash` while it collects late denials.

**Done when:**
- `grep -n "mach\|ipc" README.md` hits in the trace documentation (categories enumerated).
- `grep -n "trace unavailable\|degrades gracefully" README.md` hits.
- `grep -n "already exists" README.md` hits within the trace section.
- `grep -n "leash-trace.log" README.md` shows the default sink and `grep -n "trace-file" README.md` shows the `-`/stderr and redirect behavior.

### 5. Add the auto-detected tools list to "How it works"
**Steps:**
1. In `## How it works` (or the tracing/how-it-works area), add a one-line note that on every run `leash` unconditionally probes a fixed set of developer tools and grants each detected tool its own paths/capabilities.
2. Name the detected tools: claude, docker, git, go, homebrew, npm, python, rust, xcode. Names and this pointer only — no per-tool grant tables.

**Done when:**
- `grep -n "unconditional" README.md` hits (or an equivalent phrasing stating detection always runs).
- `grep -ni "claude" README.md` and `grep -ni "homebrew" README.md` both hit in the tools list, and the list contains all nine names.

## Verification
Run from the worktree root:
1. `make lint` — must exit 0 (docs-only; no `.go` file changed). Confirm `git status --porcelain` shows only `README.md` modified (plus this plan file).
2. Run every ticket grep and confirm each hits in the intended section:
   - `grep -n "leash-trace" README.md` (inside the `## Usage` fenced block)
   - `grep -n "localhost" README.md` (Network default meaning)
   - `grep -n "mach\|ipc" README.md` (trace categories)
   - `grep -n "trace unavailable\|degrades gracefully" README.md`
   - `grep -n "already exists" README.md` (trace section)
   - `grep -n "scrubbed" README.md` (Environment section)
3. Proofread pass: read the final `README.md` end to end and check every newly documented claim against its cited source — Environment vs `sandbox/env.go:14-127`; Network vs `sandbox/builder.go:144-152`; trace format/categories/sinks/degradation/drain vs `cmd/trace/{main.go,trace.go,parse.go,category.go}`; tools list vs `execute_darwin.go:47-51`. Fix any drift, then re-run steps 1–2.

## Assumptions
- New sections are added as top-level `##` headings (`## Environment`, `## Network`, and trace either expanded in place or as `## Tracing denials`), placed between the existing `## ⚠ Warning` and `## Use as a library` sections; exact heading titles are the author's choice as long as the Done-when greps hit.
- The `leash-trace` example command (`leash-trace go test ./...`) is representative; any runnable `leash-trace …` invocation inside the Usage block satisfies the requirement.
- "~3s longer" is an acceptable user-facing approximation of the fixed 3-second drain constant.

## Gotchas
- The `leash-trace --help` output currently misnames things (issue #5); document the **actual** runtime behavior, not `--help` text, and change no `.go` file.
- README line 95 already says `<category>: <path>`; the verified format is `<category>: <target>` (target may be a host:port or mach service name, not only a path) — use `<target>` and note the broader meaning.
- `make lint` includes a `gofmt` check but that only scans `.go` files, so a README-only edit cannot break it; still run it to prove the docs-only invariant held.
- Do not delete the accurate existing `--no-network`, `--env`, `--proxy-env`, and worktree material — the new Environment/Network sections complement the Options line (README 63–66), they do not replace it.
