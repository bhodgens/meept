# master.md — Dependency Visibility, Opt-in Installs, Skill Tool Requirements, Skill Packaging

Closes: #32 (launchd/menubar PATH) at branch 01 completion.

## Goal

Make meept's external tool dependencies visible, diagnosable, and — with
explicit consent — installable. Four required workstreams plus one
packaging fix:

1. **Doctor learns MCP dependencies** — `meept doctor` checks every
   enabled catalog entry's binary via `exec.LookPath` and prints a
   per-server install hint. Daemon launch failures log the effective PATH.
2. **Opt-in installs** — `meept doctor --fix --install-missing` runs the
   install hints after displaying each one. Never silent (user rule:
   never install software without explicit OK).
3. **Skill tool requirements** — skills declare
   `requires-tools: [server.tool, ...]` in frontmatter; the executor
   checks availability and fails with a routed message instead of
   mid-skill tool-not-found.
4. **Launch environments carry a usable PATH** — fixes #32 across the
   launchd plist, the kardianos/service config, and the menubar app.
5. **Bundled skills are packaged** — `make install` copies
   `config/skills/` into `~/.meept/skills/` (no-clobber) so shipped
   skills (web-browsing, computer-use) actually reach users. Today NO
   install step copies them; discovery tiers (internal/skills/discovery.go:31)
   never see the repo's config/skills.

## Architecture Overview

- Branch 01 (Go + one Swift leaf): `InstallHint` field on
  `mcp.ServerConfig`, catalog entries annotated, doctor check, launch
  PATH fixes (closes #32), daemon failure warning.
- Branch 02 (Go): doctor `--install-missing` executor consuming branch
  01's hints.
- Branch 03 (Go + docs): frontmatter `requires-tools` parse + executor
  availability check + annotations on bundled skills.
- Branch 04 (Makefile + doctor): skills packaging step.

Dependency spine: 01/01 (hint field) gates 01/02 and 01/03; 02/01 gates
on 01/02; 03/01 gates 03/02; 01/04 (Swift) and 04/01 are independent.

## Interface Contracts (frozen)

### Contract A: InstallHint field (branch 01, leaf 01)

```go
// internal/tools/mcp/manager.go — add to ServerConfig:
    // InstallHint is the shell command that installs the server's
    // dependency when it is missing from PATH (e.g. "npm install -g
    // @modelcontextprotocol/server-github", "brew install uv",
    // "cargo install --path <obscura-checkout>/crates/obscura"). Empty
    // means the entry has no external binary dependency (http transport)
    // or no known installer. Display-only: meept NEVER executes it
    // without explicit user consent (see doctor --install-missing).
    InstallHint string `json:"install_hint,omitempty"`
```

Every stdio catalog entry in `config/mcp_servers.json5` gets a truthful
`install_hint` (npx entries: `npm install -g <pkg>`; uvx entries:
`brew install uv` or `pip install uv` note — uvx auto-fetches the
package itself so the hint covers the RUNTIME; native binaries:
actual install command). HTTP entries omit the field.

### Contract B: doctor mcp-dependencies check (branch 01, leaf 02)

One doctorCheck line per ENABLED stdio catalog entry:

- present:  `[ok]   mcp:<name>         <binary> found in PATH`
- missing:  `[fail] mcp:<name>         <binary> not found — install: <install_hint>`

Name prefix `mcp:` distinguishes from core checks. Missing binaries do
NOT abort doctor; they count in the failed summary. A new
`--json` flag is OUT of scope. Runtime lookup is `exec.LookPath(cmd[0])`
only for bare names; absolute-path commands are stat()'d instead.

### Contract C: canonical daemon PATH (branch 01, leaf 03) — closes #32

New file `internal/daemon/daemonpath.go` (package daemon):

```go
// DaemonPath returns the PATH value the daemon guarantees for itself and
// its subprocesses (launchd plists, MCP server launches). Order: the
// inherited PATH first (preserves shell-launched setups), then the
// guaranteed dirs appended if absent: /opt/homebrew/bin, /usr/local/bin,
// $HOME/.local/bin, $HOME/go/bin, $HOME/.cargo/bin, /usr/bin:/bin:/usr/sbin:/sbin.
func DaemonPath() string
```

Consumers:
1. launchd.go:329 plist EnvironmentVariables PATH → `DaemonPath()`.
2. kardianos/service install path: pass `svc.Config{Env: []string{"PATH=" + DaemonPath()}}`
   (verify kardianos API field name from go doc before writing; adjust
   the comment if the API differs and report).
3. MCP server launch failure (manager.go StartServer error path): log
   `"mcp server %q failed: %v (daemon PATH=%s)"` — one line, effective
   PATH included, so "works in shell, fails under launchd" is diagnosable
   from the log alone.

### Contract D: menubar PATH (branch 01, leaf 04, Swift)

`menubar/MeeptMenuBar/Services/DaemonController.swift`: when spawning
`/bin/launchctl` (process at :123), set
`process.environment = ProcessInfo.processInfo.environment.merging(["PATH": <same dir list as Contract C>])`
The dir list is duplicated in Swift with a comment cross-referencing
`internal/daemon/daemonpath.go` (single-line comment: keep the two lists
in sync; a doc-check leaf greps both).

### Contract E: skill requires-tools (branch 03, leaf 01)

```go
// internal/context/skill_parser.go — extend skillFrontmatter:
    RequiresTools []string `yaml:"requires-tools"`
// surfaced as Skill.RequiresTools []string (models.go + index.go gain the field)
```

Semantics: each entry is a full registered tool name, server-qualified
(`cua-driver.capture`) or bare-builtin (`web_fetch`). Availability =
tool is in the live registry (or MCP manager AllTools for server-qualified).
Executor (internal/skills/executor.go) checks BEFORE execution when
`validatePrerequisites` is enabled; failure returns ExecutorError with
message: `skill <name> requires unavailable tool(s): <list> — run 'meept doctor' to diagnose` (lowercase per UI convention).

### Contract F: skills packaging (branch 04, leaf 01)

Makefile `install` target gains, before the "Install complete" echo:

```make
	@echo "Installing bundled skills (no-clobber)..."
	@mkdir -p $(MEEPT_HOME)/skills
	@for d in config/skills/*/; do \
		name=$$(basename $$d); \
		if [ ! -d "$(MEEPT_HOME)/skills/$$name" ]; then \
			cp -R "$$d" "$(MEEPT_HOME)/skills/$$name"; \
			echo "  installed skill $$name"; \
		else \
			echo "  skipping $$name (exists)"; \
		fi; \
	done
```

Existing-user skills are never overwritten (user skills shadow bundled
anyway per discovery priorities — document that in
docs/workflows/skills.md "Bundled skills" section).

## Child Index

| Doc | Type | Scope | Est. context | Dependencies |
|-----|------|-------|--------------|--------------|
| 01-dependency-visibility/ | branch | | | |
| 01-dependency-visibility/01-install-hint-field.md | leaf | ServerConfig field + catalog annotations + loader test | ~35K | none |
| 01-dependency-visibility/02-doctor-mcp-checks.md | leaf | doctor check per Contract B | ~40K | 01 |
| 01-dependency-visibility/03-daemonpath-go.md | leaf | daemonpath.go + launchd + kardianos + manager warning | ~50K | none (parallel) |
| 01-dependency-visibility/04-menubar-path.md | leaf | Swift DaemonController env | ~20K | none (parallel) |
| 02-doctor-install/01-install-missing.md | leaf | --install-missing executor | ~45K | 01/02 |
| 03-skill-tool-requirements/01-frontmatter-parse.md | leaf | parse + executor check + tests | ~50K | none (parallel) |
| 03-skill-tool-requirements/02-skill-annotations.md | leaf | SKILL.md annotations + skills.md docs | ~25K | 03/01 |
| 04-skill-packaging/01-make-install-skills.md | leaf | Makefile + docs + test | ~30K | none (parallel) |

Concurrency waves:
- Wave 1 (parallel): 01/01, 01/03, 01/04, 03/01, 04/01
- Wave 2: 01/02 (needs 01/01), 03/02 (needs 03/01)
- Wave 3: 02/01 (needs 01/02)

## Dispatch Protocol

Per leaf: dispatch via `delegate_task` with the leaf document + the
relevant contract verbatim + this project's coding rules. Every dispatch
includes: "Do NOT commit. Do NOT run git add. Write code, run tests,
report results only. The orchestrator handles all git operations."
Review in-session (build + tests + contract-verbatim diff + stray-artifact
grep); re-dispatch on gaps max 3 times; commit per leaf after review;
update tracking tables here and in branch orchestrators.

Issue closure: when 01/03 AND 01/04 are both COMPLETE, the closing commit
message ends with `Closes #32`. Branch 01's orchestrator verifies the
issue body's three failure sites are each addressed before closing.

## Coding Conventions

- Go: AGENTS.md rules (no ignored errors, two-value assertions, typed-nil
  guards in Set*, no os.Getwd in daemon code, error wrap
  `fmt.Errorf("context: %w", err)`).
- All user-facing strings lowercase (meept UI convention).
- New user-facing commands/flags documented in the same commit:
  `docs/workflows/` feature doc + `docs/reference/` if CLI surface.
- Tests table-driven beside source; no network; `go test -p 2` with
  TEST_PACKAGE_PARALLELISM=2 for full-suite runs.
- Swift: match DaemonController.swift's existing style; no new files.
- Verify third-party APIs with `go doc` before use (kardianos/service
  Config fields; cobra flag wiring) — trust go doc over briefs.

## Completion Tracking Table

| Doc | Status | Notes |
|-----|--------|-------|
| 01-dependency-visibility/01-install-hint-field.md | COMPLETE | 2026-09-06. Field landed via 2c609260 (same sibling commit sweep as 01/03 — content verified: Contract A verbatim, gofmt realigned); catalog hints + TestCatalogInstallHints committed in 7f93374d. 22/22 stdio entries hinted; cua-driver hint corrected to documented curl installer. Suites green. |
| 01-dependency-visibility/02-doctor-mcp-checks.md | COMPLETE | 2026-09-06. COMMITTED f40fde38. Orchestrator re-ran doctor live: 7 mcp: lines incl. `mcp:obscura obscura found in path`. Full cmd/meept suite green. Scratch-HOME missing-binary smoke denied at gate — scenario covered by unit tests instead (accepted). |
| 01-dependency-visibility/03-daemonpath-go.md | COMPLETE | 2026-09-06. Content landed in 2c609260 (orig 256e620a — combined with sibling phase-worktree commit; message fixed via filter-branch to close #32). Orchestrator verified: DaemonPath pure/per-call HOME, plist interpolation, kardianos EnvVars at Install only, warning logs runtime PATH. Tests re-run green (DaemonPath suite + full mcp). #32 CLOSED (with e4bdcb5e). |
| 01-dependency-visibility/04-menubar-path.md | COMPLETE | 2026-09-06. COMMITTED e4bdcb5e. Orchestrator re-verified swift build + read daemonPATH() (dedup + order correct, NSHomeDirectory runtime expansion). One spawn site (runLaunchctl) — confirmed only Process() in file. #32 half-closed (Go half = leaf 01/03). |
| 02-doctor-install/01-install-missing.md | COMPLETE | 2026-09-06. COMMITTED 29b19377. Orchestrator verified: explicit --fix coupling (required-together would break --fix alone — correct), TTY refusal pre-prompt, verbatim-hint execution, runInstallHintFn seam, single-catalog-pass checks+entries coherence. Full cmd/meept suite green (25 doctor tests). |
| 03-skill-tool-requirements/01-frontmatter-parse.md | COMPLETE | 2026-09-06. COMMITTED 07d4225c. Orchestrator verified: kebab key, gate inside validatePrerequisites pre-CheckPrerequisites (side-effect-free ordering), typed-nil guard, raw-field preserves exact contract message. Extra context.Skill struct field was required (contract's file list drifted) — accepted. yaml scalar → fail-loud. 8 subtests + 3 parser cases green. |
| 03-skill-tool-requirements/02-skill-annotations.md | COMPLETE | 2026-09-06. COMMITTED e2adcf1b. 3/15 annotated (web-browsing, computer-use, learn-from-video — verdict table recorded); 12 reasoning-only reviewed and left clean. skills.md Tool requirements section added. All skills parse; suites green. Also shipped the previously-untracked web-browsing SKILL.md. |
| 04-skill-packaging/01-make-install-skills.md | COMPLETE | 2026-09-06. COMMITTED f127fe3d (Makefile; skills.md section rode in e2adcf1b). Root cause of the earlier blocked commits: stray sibling files sat in the index — a staged-5-file commit was being rejected by the U1000 pre-commit hook on SIBLING code, not this leaf's. Un-staged the siblings; Makefile-only commit passed clean. Orchestrator verified hunks (tabs, Contract G snippet at :254, safe uninstall diff -r at :863-865) + skills.md section. Subagent sandbox proof: 15 installed, 15 skipped second run, user edit survived. |

## Review Checklist (root)

- [ ] Every contract A-F verified verbatim against implementation
- [ ] `go build ./...` clean; `go vet` on touched packages clean
- [ ] `go test -p 2 -short ./internal/... ./cmd/meept/` green (accepting
      sibling-session churn outside this tree's scope)
- [ ] `meept doctor` output shows mcp: checks with hints (manual smoke)
- [ ] Every catalog stdio entry has install_hint (grep count == stdio count)
- [ ] Issue #32 closed with the branch-01 closing commit
- [ ] No debug artifacts, TODOs, placeholder values
- [ ] Line-number corruption grep 0 hits on touched dirs
- [ ] AGENTS.md updated (doctor flags, requires-tools convention, skills
      packaging) in the final integration commit
- [ ] docs/workflows/skills.md + tool-routing.md updated

## Integration Test Plan

1. `make build` + `go test -p 2 -short ./internal/config/ ./internal/skills/ ./internal/context/ ./internal/daemon/ ./cmd/meept/`.
2. Manual smoke: `meept doctor` lists mcp: entries incl. a deliberate
   missing-binary case (temporarily PATH-strip obscura); hint text prints.
3. `meept doctor --fix --install-missing` on a scratch config with one
   benign hint (`true` as the "install command") — prompt shown, consent
   required, command runs once.
4. Skill with `requires-tools: [cua-driver.capture]` while server disabled
   → ExecutorError with the routed doctor message.
5. `make install` in a sandbox HOME creates ~/.meept/skills/<name> for all
   14 bundled skills; re-run skips all.
6. launchd: reinstall service, `launchctl print` shows the augmented PATH;
   `launchctl print gui/$(id -u)/com.caimlas.meept-daemon | grep PATH`.

## Open Questions

None blocking. Recorded decisions: (1) install hints are display-only
strings in config, not structured exec specs — keeps config safe to
share; (2) menubar PATH list is a documented duplicate of Contract C
(Swift cannot import Go), kept in sync by review checklist grep.
