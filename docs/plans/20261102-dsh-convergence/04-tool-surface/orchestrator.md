# 04 Tool Surface - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../../master.md (root master, branch 04)
- **Children:** 00T-tests.md, 01-tool-timeout.md, 02-lsp-navigation.md,
  03-skill-catalog.md, 04-skill-invocation.md, 05-message-quotes.md,
  06-gh-issues.md (this branch is a flat leaf set under one orchestrator)
- **Scope:** Surface the model-facing features that sit on top of engines meept
  already ships: per-tool timeout on the Tool interface, an LSP navigation
  tool reusing the existing LSP client, skill hot-reload + durable per-session
  skill catalog + `/name` invocation, message-quotes (UI), and the two
  GitHub-issues docs action (items 11/12, user D2/D9).

## Goal

Meept already owns the engine (LSP client for writethrough, sqlite session
storage, a skill discovery/loader, a TUI), but the model-facing surface is
missing: tools cannot declare a co-operative timeout budget, there is no
read-only LSP navigation tool, skills only hot-appear after a restart/sync
(the process-cache relist), there is no durable per-session skill catalog and
no `/name` direct-invocation gesture, and there is no message-quote affordance.
This branch closes those gaps by (a) wiring a cooperative timeout wrapper over
the live executor path, (b) exposing `lsp_navigate` over the existing
`internal/code/lsp` client, (c) replacing the cached-only skill relist with a
fsnotify hot-reload + durable catalog + `skills/change` event, (d) adding the
`/name` gesture, (e) adding message-quotes with a documented UI-parity note,
and (f) writing the two GitHub issue bodies for image-offload and
tmux/time-context (docs, NOT code). No new engines are built; everything is
surface over existing infrastructure, per master C8/C9.

## Architecture

One executor choke point (`internal/agent/executor.go` `executeToolWithRetry`)
already brackets every live tool call with permission + gate + retry; the
timeout wrapper slots in there and reads a new `TimeoutMs()` on the Tool
interface (B1). The LSP nav tool reuses the same `internal/code/lsp.Manager`
the writethrough notifier spawns (B2). Skill hot-reload mounts a fsnotify
watcher over the two discovery roots and invalidates the existing
`LazySkillLoader`; the durable per-session catalog is a message materialized
at turn-start from the (now fresh) catalog, digest-deduplicated, and survives
resume because it is a session record (B3). `/name` scans the user input with
the dsh `SKILL_GESTURE` regex and injects the skill body before the LLM call
(B4). Message-quotes is TUI/GUI presentational surface. The gh-issues leaf is
a docs write of two issue bodies to be filed by the orchestrator at Phase 2.

## Interface Contracts

### B1: Per-tool cooperative timeout (Owner: 01-tool-timeout.md; Consumers: 02)

```go
// internal/tools/interface.go - add to the Tool interface:
TimeoutMs() time.Duration // 0 = no declared timeout; >0 = co-operative budget
// ToolDefaults (interface.go:60) returns TimeoutMs() time.Duration { return 0 }
//
// internal/agent/executor.go - the live dispatch wrapper (executeToolWithRetry,
// executor.go:1429; tool.Execute at :1480): when TimeoutMs() > 0, derive a
// context deadline with context.WithTimeout around the single tool.Execute /
// ExecuteStreaming body; on expiry return a structured error:
//   ErrToolTimeout = errors.New("tool timed out")          // sentinel (errors.Is)
//   ToolResult{Success:false, ErrCode:"TOOL_TIMEOUT",      // machine gear
//     Error:"tool call timed out after Nms"}
// No declared budget -> delegate unchanged (opt-in per tool). The wrapper must
// NOT swallow context cancellation from the turn-level deadline: only this
// tool's own timer substitutes the TOOL_TIMEOUT result.
// Consumers: 02 declares TimeoutMs() == 60s on lsp_navigate.
```

### B2: LSP navigation tool (Owner: 02-lsp-navigation.md)

```go
// internal/tools/builtin/lsp_navigate.go (NEW) - single read-only tool:
//   Name() "lsp_navigate"
//   Parameters: operation ("goToDefinition"|"findReferences"|
//       "goToImplementation"|"hover"), file_path (string, req),
//       line (int, one-based), character (int, one-based UTF-16),
//       workspace (string, optional)
// Implements TimeoutMs() -> 60s (B1). Reuses internal/code/lsp.Manager
// (manager.go:52 NewManager, :67 GetServerForLanguage) + Client methods
// client.go:282 GotoDefinition, :308 FindReferences, :331 Hover;
// ADD GotoImplementation (client lacks it today). Result caps
// DEFAULT_MAX_LOCATIONS=100, DEFAULT_MAX_RESULT_CHARS=16000.
// Workspace-required: mirror tools.ErrNoWorkingDir sentinel semantics
// (internal/agent/AGENTS.md) - the tool returns a detectable
// 'lsp tool requires a session workspace cwd' error when unresolvable.
// IsReadOnly -> true.
```

### B3: Durable skill catalog + hot-reload (Owner: 03-skill-catalog.md; Consumers: 04)

```go
// internal/skills/watcher.go (NEW) - fsnotify Watcher over the two discovery
// roots (project .meept/skills + ~/.meept/skills, discovery.go:31-53); on an
// add/change/remove under <root>/<skill>/SKILL.md or <root>/<skill>.md:
//   loader.Invalidate(name) or loader.Clear() (lazy_loader.go:238/:260)
//   re-Discover() (discovery.go:136) -> loader.SetIndex (lazy_loader.go:307)
//   publish bus event skills/change (typed bus.Topic semantics per root
//   AGENTS.md; payload {source:"skills"})
// Replaces the process-cache relist in context_injector.go:112-136
// (visibleSkillLines) with a DURABLE catalog: a session message materialized
// at turn start listing every visible model-invocable skill (C4 lines),
// digest-deduplicated (sha256 over sorted name+description), so the catalog
// is reconstructable after resume/restart, not just from the live cache.
// Discovery stays Authoritative for directory bundles and flat-markdown
// <name>.md files both discovered (dsh 1b).
```

### B4: `/name` direct invocation (Owner: 04-skill-invocation.md; Consumer: 03 context)

```go
// SKILL_GESTURE = /(^|\s)\/([a-z0-9]+(?:-[a-z0-9]+)*)(?=\s|$)/g
// Scan ONLY the claimed user text input (never tool/assistant content). For
// each matched <name> verified present in the catalog/loader (the loaded-body
// lookup decides; unknown or disable-model-invocation names stay prose),
// inject the rendered skill body as an explicit 'user asked for this skill'
// instruction appended to the context, closest to the answer. This is the
// explicit slash gesture, DISTINCT from named_agent_route.go (which routes to
// a named AGENT, not a skill). /usr/bin paths and 5/8 fractions must NOT match.
// Consumers: none downstream (builds on B3 catalog + LazySkillLoader).
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-tests.md | test leaf | none | 60K | Phase 0 |
| 01 | 01-tool-timeout.md | leaf | B1 | 18K | A |
| 02 | 02-lsp-navigation.md | leaf | B1, B2 | 22K | A |
| 03 | 03-skill-catalog.md | leaf | B3 | 24K | A |
| 04 | 04-skill-invocation.md | leaf | B3, B4 | 16K | A |
| 05 | 05-message-quotes.md | leaf | none | 16K | A |
| 06 | 06-gh-issues.md | docs/action leaf | none | 10K | A |

Code leaves 01-06 are mutually independent (no shared-file writes except
01's wrapper is consumed by 02 — leave B1 to 01 only). All dispatch together
after TESTS_VALIDATED. 06 is docs-only and may dispatch in Phase 0 with the
test leaf or in Phase 1 with the code leaves.

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution` + references/tests-first-gate.md.

### Phase 0: Test Gate (before ANY implementation dispatch)

Dispatch 00T-tests.md as a subagent: tests ONLY, fail RED for
missing-implementation reasons. Validate IN-SESSION (never delegate review)
against the `## Test Gate` matrix below; apply the delta protocol (update the
LESSER artifact; coverage converges up); commit gate tests; set the branch to
TESTS_VALIDATED. Then implementation leaves 01-06 may dispatch. 06 (gh-issues,
docs-only) may dispatch independently in Phase 0 or 1 - it is not
implementation-gated.

### Phase 1: Dispatch Concurrency Group [A]

Dispatch 01, 02, 03, 04, 05, 06 together via `delegate_task`: goal =
"Implement all tasks from [leaf]"; context = full leaf text + contracts B1-B4
INLINED + conventions + relevant source INLINED (anchor snippets) + the
no-commit, no-git-add, no-read_file-on-source, write-once-and-stop rules. All
six are independent; batch to the concurrency cap. 06 is a docs write (no
code, no tests).

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check leaf
spec, contracts, Review Checklist; run the leaf's verify commands; gate-test
diff must be empty. Pass: commit + status REVIEWED. Gaps: re-dispatch with
findings, max 3 cycles, then escalate to parent.

### Phase 3: Integration Review

After ALL children REVIEWED: verify contracts B1-B4 across the combined
branch, `go build ./...`, `make test` (short, -p 2), the new tool-surface e2e
suite in isolation, `make graphs` + `make graphs-check` (the RPC/tool surface
gained lsp_navigate + skills/change), `make docs-generate` if gendoc drifted.
Then file the two gh issues from 06 (user D2/D9, master Open Questions #1 -
the ORCHESTRATOR files them, not the 06 leaf). Gaps: re-dispatch the failing
leaf. Pass: set children COMPLETE and report COMPLETE to parent.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts B1-B4 satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
- [ ] Tests written and passing (e2e for features; unit pins for the
      non-e2e-coverable facets this branch explicitly authorizes)
- [ ] Code follows project conventions (no ignored errors, no mutex across
      I/O, typed-nil guards, predictable-ID prevention)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders)
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] Every model-facing tool (lsp_navigate, skills/change) has its tool
      registration + RPC + a TUI/GUI surface or an explicit documented
      deviation (root AGENTS.md wiring requirement)
- [ ] 06-gh-issues: both issue bodies present, grounded, no code; ready to file

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go 1.22+ (repo pins GOTOOLCHAIN). All new code Go; 06 is prose.
- **Naming:** exported/unexported per repo; model tool names snake_case
  (`lsp_navigate`).
- **Imports:** stdlib group first, then third-party; no unused (U1000-strict);
  adding `fsnotify` = new go.mod dep - verify it is already pinned before
  adding (Q1 in Open Questions).
- **Error handling:** wrap with context; no ignored errors; no bare panic(err);
  the timeout wrapper keeps `errors.Is(ErrToolTimeout)` routeable.
- **Security:** predictable-ID prevention (pkg/id.Generate, never UnixNano);
  typed-nil guards on every Set*/wiring seam; the LSP tool is read-only
  (IsReadOnly true); never inject untrusted skill text without the existing
  fence/sanitizer path it already rides.
- **Testing:** NEW feature tests go in the hermetic e2e tier (root AGENTS.md);
  THIS branch explicitly authorizes focused unit tests for the
  non-e2e-coverable facets (arg parse, digest, truncation, sentinels,
  renderer) - see Test Gate. e2e suite in `e2e/suites/tool-surface/` +
  `e2e/manifest.json` path_map same commit.
- **Formatting:** gofmt before reporting; ASCII only in new .md.
- **Model-facing rule:** every capability ships a user-facing surface (CLI,
  TUI, GUI, or HTTP) + agent wiring, or the leaf documents the deferral
  (message-quotes documents its UI-surface scope explicitly).

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element (leaf/task/contract) | Verified by |
|-----|---------------------------------------------|-----------------------------------|-------------|
| R8 | "per-tool timeout(8)" + "reuse what exists, if we can" (D8) | 01-tool-timeout.md (B1) | 01 gate + e2e (F9) |
| R10 | "LSP nav(10)" + "reuse ... existing LSP client" (D8) | 02-lsp-navigation.md (B2) | 02 gate + e2e (F10) |
| R13 | "skill hot-reload(13)" | 03-skill-catalog.md (B3) | 03 gate + e2e (F11) |
| R14 | "message-quotes+/name(14)" | 04-skill-invocation.md (B4) + 05-message-quotes.md | 04 gate + 05 TUI-headless (F11/F12) |
| R6 | items 11/12 become GitHub issues (D2, D9) | 06-gh-issues.md (issues only) | issue bodies present, filed at Phase 2 |

DEFERRED rows name where the work goes: a literal dsh `skill` loader tool is
NOT built - meept's model load path is the existing `[load: file_read]` C4
line (context_injector.go:132), which satisfies the effector role without a
new tool. Note in Notes.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-tests | PENDING | 0 | Phase 0 gate |
| 01-tool-timeout | PENDING | 0 | |
| 02-lsp-navigation | PENDING | 0 | |
| 03-skill-catalog | PENDING | 0 | |
| 04-skill-invocation | PENDING | 0 | |
| 05-message-quotes | PENDING | 0 | |
| 06-gh-issues | PENDING | 0 | docs; filed at Phase 2 |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: code children 01-05 stay PENDING until the branch test-gate leaf
reaches TESTS_VALIDATED. 06 is not implementation-gated.

## Integration Test Plan

- `go build ./...` ; `make test` (short, -p 2) - all GREEN.
- `make e2e-fast-area AREA=tool-surface` - the hermetic tool-surface suite.
- Cross-boundary: e2e asserting `lsp_navigate` goToDefinition returns a
  location against the writethrough-spawned LSP server (proves B1+B2 compose:
  a hung LSP query returns TOOL_TIMEOUT, not a turn hang).
- e2e asserting a skill added to `~/.meept/skills` appears in the session
  catalog WITHOUT a daemon restart (hot-reload), and the catalog survives a
  daemon restart (durability).
- `make graphs` + `make graphs-check` (lsp_navigate + skills/change new bus
  edge); `make docs-generate` if gendoc drifted; `scripts/config-drift-check.py`
  clean.
- gh issues 11/12 filed from 06 bodies (Phase 2 closeout, master Open
  Questions #1).

## Test Gate

Test leaves: 00T-tests.md (single test leaf covering F9-F12) - dispatched in
Phase 0, validated by the control agent (orchestrator, in-session).

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F9 | per-tool timeout (B1) | 01-tool-timeout.md | TestExecutor_Timeout_ArmsDeadline, TestToolDefaults_ZeroTimeout_NoDeadline, TestExecutor_Timeout_StructuredTOOL_TIMEOUT | e2e_tool_timeout: sleep tool > budget returns TOOL_TIMEOUT | PENDING |
| F10 | LSP navigation tool (B2) | 02-lsp-navigation.md | TestLSPNavigate_ParseArgs, TestLSPNavigate_ResultCaps, TestLSPNavigate_WorkspaceRequired, TestLSP_GotoImplementationAdded | e2e_lsp_nav: gopls/stub goToDefinition + hover round-trip | PENDING |
| F11 | skill hot-reload + catalog (B3) | 03-skill-catalog.md | TestSkillWatcher_EmitsOnChange, TestSkillCatalog_DigestDedup, TestSkillCatalog_DurableRebuild | e2e_skill_hotreload: new skill appears w/o restart; catalog survives daemon restart | PENDING |
| F12 | message-quotes (UI) | 05-message-quotes.md | TestQuoteSelect_BlockquoteReference (TUI headless), markdown blockquote render pin | n/a (presentational UI; covered by TUI-headless/render tests) | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: [date, feature ID, direction, one-line reason]

Note: root AGENTS.md's "no new unit files for new features" is EXPLICITLY
overridden for F9-F12's non-e2e-coverable facets (argument parsing, digest,
truncation, sentinels, TUI headless render) per this branch spec. Every
user-visible contract path (timeout through the live executor, LSP through a
real server, skill hot-reload through a live daemon) is e2e-covered in
`e2e/suites/tool-surface/`.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated
addenda. Never edit a dispatched leaf document mid-execution.

## Open Questions

1. fsnotify dependency: confirm peckmez/radovskyb gopkg `fsnotify` is already
   in go.mod before leaf 03 adds the import; if absent, the leaf vendors the
   pinned version (record the resolved module path in the dispatch addendum).
   Do not let the watcher leaf choose an unvetted fork.
2. `goToImplementation` request: meept's LSP client has no GotoImplementation
   method. Leaf 02 ADDS one wired to `textDocument/implementation`; if the
   target server's capabilities report `implementationProvider:false` the tool
   returns a clear "not supported by server" result, never a hang.
3. Message-quotes is presentational UI: it gets a TUI-headless test but NO
   live-daemon e2e (master F12 froze E2E as "n/a (UI)"); the GUI parity is
   documented as a deviation with a justification in 05, not silently omitted.

## Notes

- The whole branch SOURCES from /tmp/dsh-audit/runtime-safety.md (timeout
  F, LSP F) and /tmp/dsh-audit/skills-schedule-storage.md (1b watcher, 1c
  catalog, 1d /name). Grep the actual dsh source before porting any constant
  (thresholds, defaults); the audit file:line is the re-read point, not the
  final say.
- Honest scope note: `internal/agent/executor.go` ToolActionMap:404-407
  already lists `lsp_goto_definition`/`lsp_find_references`/`lsp_hover`/
  `lsp_workspace_symbols`/`lsp_diagnostics` mapped to file_read, but NO builtin
  registers any tool with those names (verified: grep finds zero constructors).
  They are dead map entries, harmless. This branch adds ONE `lsp_navigate`
  tool and does NOT create the individual lsp_* tools; add a ToolActionMap
  entry `lsp_navigate` -> file_read in leaf 02 so the new tool is not
  "Unknown action"-denied (bughunt H7 class).
- A literal dsh `skill` model-loader tool is NOT built (leaf scope): meept's
  model-facing load path is the existing `[load: file_read <path>]` C4 line
  emitted by visibleSkillLines (context_injector.go:132). B3 makes that load
  discoverable via a durable catalog; the effector stays file_read, gated by
  requires-tools. Deferred per the Traceability rule.
- License gate (user memory): GPL/AGPL acceptable only as subprocess deps,
  never vendored. fsnotify (BSD) and any LSP test server must pass or be
  rejected; gopls (BSD-3-Clause) is acceptable for the e2e test server.
