# 00T Tool-Surface Tests - Test Gate Leaf

> **For the implementing agent:** You are the TEST implementer for this leaf,
> part of the branch's tests-first gate. Implement ALL test tasks below. Tests
> only - you MUST NOT write or modify implementation files. The tests will
> fail RED against the current code (missing feature) - that is the expected
> end state. Run them once to confirm they fail for the RIGHT reason (missing
> implementation, not syntax errors or import failures). Do NOT commit - the
> orchestrator validates, then commits the gate tests after review. Do NOT use
> read_file on existing source - explore with search_files or terminal cat.
> After writing a file, do NOT read it back - write once and stop.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** The full unit + hermetic-e2e test suite for the branch's four
  features (F9 per-tool timeout, F10 LSP navigation, F11 skill hot-reload +
  catalog, F12 message-quotes). Tests only; every test fails RED for
  missing-implementation reasons.
- **Dependencies:** contracts B1-B4 from ../orchestrator.md
- **Estimated Context:** 60K
- **Concurrency Group:** Phase 0 (first)
- **Audit references:** /tmp/dsh-audit/runtime-safety.md (timeout F, LSP F);
  /tmp/dsh-audit/skills-schedule-storage.md (1b/1c/1d)

## Goal

Produce the gate tests that drive the branch's implementation: focused unit
tests for the facets the e2e tier cannot reach (argument parsing, digest
dedup, truncation, sentinels, TUI headless render), and hermetic e2e tests for
every user-visible contract path (timeout through the live executor, LSP
through a real language server, skill hot-reload + catalog durability through
a live daemon). The branch implements to make these pass; you only write them.

## Context

- The live tool dispatch path is `internal/agent/executor.go`
  `executeToolWithRetry` (:1429), which calls `tool.Execute(ctx, args)` at
  :1480. The Tool interface (`internal/tools/interface.go:24-54`) has NO
  TimeoutMs today; `ToolDefaults` (:60-68) returns zero for its safety
  queries. The wrapper leaf adds `TimeoutMs() time.Duration` to the interface +
  arms a deadline in the executor.
- LSP: `internal/code/lsp/client.go` has GotoDefinition(:282),
  FindReferences(:308), Hover(:331), TypeDefinition(:461) but NO
  GotoImplementation. `internal/code/lsp/manager.go` has NewManager(:52),
  GetServerForLanguage(:67), StartServerForLanguage(:86). The writethrough
  notifier (`internal/tools/builtin/lsp_writethrough.go:81`) already spawns
  the Manager. WATERYTHROUGH's Manager is the seam the nav tool reuses.
- Skills: `internal/skills/discovery.go:136 DiscoverWithContext`,
  `:31-53 DefaultTiers`; `internal/skills/lazy_loader.go` NewLazySkillLoader
  (:56), Load(:73), Get(:214), Invalidate(:238), Clear(:260), CachedNames
  (:294), SetIndex(:307). The process-cache relist lives at
  `internal/agent/context_injector.go:115-135 visibleSkillLines()`. No fsnotify
  watcher exists today; no `skills/change` bus event exists.
- Message-quotes: no feature exists. The TUI markdown renderer already detects
  blockquotes (`internal/tui/render/markdown.go:97` `\n> `).
- Repo e2e rules (root AGENTS.md): suites in `e2e/suites/<name>/` with the
  `e2e` build tag; `e2e/manifest.json` path->suite mapping same commit; scratch
  MEEPT_HOME harness; assert through the live RPC/bus surface.

## Verified Anchors

- internal/tools/interface.go:24-54 - Tool interface (no TimeoutMs)
- internal/tools/interface.go:60-68 - ToolDefaults zero safety queries
- internal/agent/executor.go:1429-1528 - executeToolWithRetry (tool.Execute :1480)
- internal/code/lsp/client.go:282/308/331/461 - GotoDefinition/FindReferences/Hover/TypeDefinition (no GotoImplementation)
- internal/code/lsp/manager.go:52/67/86/265 - Manager + GetServer/StartServerForLanguage
- internal/tools/builtin/lsp_writethrough.go:81 - NewLSPWriteNotifier (Manager reuse seam)
- internal/skills/discovery.go:31-53/136 - DefaultTiers / DiscoverWithContext
- internal/skills/lazy_loader.go:56/214/238/260/294/307 - loader seams
- internal/agent/context_injector.go:115-135 - visibleSkillLines (replaced by durable catalog)
- internal/tui/render/markdown.go:97 - blockquote detection (existing render anchor)

## Interface Contracts (From Parent)

### What This Leaf Exposes

Nothing runtime-visible. Test files only, at these paths:
- `internal/tools/interface_test.go` + `internal/agent/executor_timeout_test.go`
- `internal/tools/builtin/lsp_navigate_test.go` + `internal/code/lsp/client_navigation_test.go`
- `internal/skills/watcher_test.go` + `internal/agent/context_catalog_test.go`
- `internal/tui/messagequote_test.go`
- `e2e/suites/tool-surface/` (suite dir) + `e2e/manifest.json` (path_map entry)

### What This Leaf Consumes

- B1 Per-tool timeout - `TimeoutMs() time.Duration`, `ErrToolTimeout`
  sentinel, `TOOL_TIMEOUT` ErrCode
- B2 LSP nav - `lsp_navigate` tool + `Client.GotoImplementation`
- B3 skill catalog/hot-reload - `skills/change` bus event, durable catalog
- B4 `/name` gesture (covered by the catalog test credence + 04's own tests;
  NOT in scope for the gate e2e)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|--------------|
| T1 | F9 per-tool timeout unit tests | internal/tools/interface_test.go; internal/agent/executor_timeout_test.go | `go test -p 2 ./internal/tools ./internal/agent -run 'Timeout|TimeoutMs' -v` | FAIL: no `TimeoutMs` method on Tool/ToolDefaults; no timeout arm in executor | PASS: ToolDefaults yields TimeoutMs()==0; executor arms NO new deadline and the tool runs unchanged | Given a tool returns `TimeoutMs()==0` (ToolDefaults); When the executor runs it via executeToolWithRetry; Then the context has NO new deadline and the tool runs to completion unchanged |
| T1b | same row (optional merge) | internal/tools/interface_test.go | as T1 | FAIL compile: TimeoutMs undefined | PASS | Given a `TimeoutTool` returns `TimeoutMs()==time.Second`; When the executor runs it; Then the call returns an `isError` result with `ErrCode=="TOOL_TIMEOUT"` and `errors.Is(err,ErrToolTimeout)` and the message contains "timed out after 1s" - never a raw context.DeadlineExceeded string |
| T2 | F10 LSP nav unit tests | internal/tools/builtin/lsp_navigate_test.go; internal/code/lsp/client_navigation_test.go | `go test -p 2 ./internal/tools/builtin ./internal/code/lsp -run 'Navigate|GotoImplementation' -v` | FAIL: `lsp_navigate` unregistered; `GotoImplementation` undefined | PASS | Given args `{operation:"goToDefinition",file_path:"a.go",line:5,character:1}`; When parseLspArgs runs; Then one-based UTF-16 coords map correctly to zero-based seam positions and a tool result is produced with a capped locations list |
| T2b | workspace-required sentinel | internal/tools/builtin/lsp_navigate_test.go | as T2 | FAIL: no workspace sentinel path | PASS | Given an unresolvable workspace / no working dir; When lsp_navigate executes; Then it returns a detectable error containing "workspace cwd" (mirrors tools.ErrNoWorkingDir stays), isError true |
| T3 | F11 skill watcher + catalog unit | internal/skills/watcher_test.go; internal/agent/context_catalog_test.go | `go test -p 2 ./internal/skills ./internal/agent -run 'Watcher|Catalog|SkillChange' -v` | FAIL: no watcher type, no catalog, no digest, no skills/change publish | PASS | Given an fsnotify event for `<root>/<skill>/SKILL.md` add; When the watcher handles it; Then the loader cache is invalidated for that skill and a `skills/change` event is published (bus topic with payload source:"skills") |
| T3b | durable catalog digest | internal/agent/context_catalog_test.go | as T3 | FAIL: no catalog `digest`, no rebuild-from-record | PASS | Given a catalog message built at turn start from the (fresh) discovery; When the same visible set is re-materialized; Then the digest is identical and no duplicate catalog message is appended; a rebuilt catalog from the persisted session record equals the original set |
| T4 | F12 message-quotes (TUI headless) | internal/tui/messagequote_test.go | `go test -p 2 ./internal/tui -run 'Quote' -v` | FAIL: no quote-select affordance | PASS | Given a user selects a chat message (headless TUI model); When they trigger quote-select; Then the input gains a `> ` blockquote line prefixed with a `[msg <id>]` reference; the blockquote renderer (`\n> `) displays it as a quote. E2E: n/a (presentational UI) |
| T5 | F9/F10/F11 hermetic e2e | e2e/suites/tool-surface/ (e2e tag) | `make e2e-fast-area AREA=tool-surface` | FAIL: no suite registered; RED on missing features | PASS | Given scratch-MEEPT_HOME daemon; When (a) a tool whose TimeoutMs exceeds its sleep is invoked, (b) lsp_navigate goToDefinition against the running LSP server returns a location, (c) a skill file is added to ~/.meept/skills then a daemon restart; Then (a) TOOL_TIMEOUT result, (b) a location with capped results, (c) new skill appears in the catalog without restart AND survives the restart |

**Worked example - T1b shape:**

T1b: Timeout tool returns structured TOOL_TIMEOUT.
1. `internal/tools/interface_test.go` - declare `type timeoutTool struct { tools.ToolDefaults }` + `func (timeoutTool) TimeoutMs() time.Duration { return time.Second }`. RED: `tools.Tool` has no `TimeoutMs`, `tools.ToolDefaults` has no `TimeoutMs` -> compile FAIL "undefined: TimeoutMs", which is a RED for the right reason.
2. Verify: `go test -p 2 ./internal/tools/... -run Timeout -v` -> FAIL (missing method).
3. The gate leaves the fix to the implementation leaf 01.

Implementer note (gate discipline): T1/T1b RED means "feature not implemented". If a test fails for a *different* reason (a pre-existing unrelated failure in the same package, import cycle, or an existing test that the gate would newly break), STOP and report to the orchestrator - do not weaken the test. The orchestrator runs the delta protocol.

## Self-Verification Checklist

- [ ] All test files compile (or fail-to-compile with "undefined: TimeoutMs"/"no method" the ONLY reason)
- [ ] Every test fails RED for a missing-implementation reason (confirmed by running once)
- [ ] No implementation file modified (only `_test.go`, suite files, and `e2e/manifest.json`)
- [ ] e2e suite has the `e2e` build tag and a manifest path_map entry in the same commit
- [ ] Deferred/weakened gate tests: none (report any suspected-wrong gate test to the orchestrator instead)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created path, incl. e2e suite files + manifest entry]
2. **Verify command + output:** [the commands in Tasks; last 20 lines showing the RED]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every F9-F12 matrix row has ≥1 unit test present AND the e2e-coverable paths are covered in `e2e/suites/tool-surface/`
- [ ] Tests assert behavior (values, state transitions), not implementation details; no tautologies
- [ ] No gate test weakened or deleted; RED reasons are missing-implementation only
- [ ] manifest.json path_map added; e2e build tag present
- [ ] Do NOT merge the gate commit until the orchestrator validates IN-SESSION against the matrix and applies the delta protocol

Output: APPROVED or specific gaps with file + line references.

## Notes

- The dsh timeouts: declare-timeout mechanism (packages/guard/timeout-policy/src/index.ts:57-59 reads `ctx.tools.get(...).timeoutMs`, swaps exec.signal, restores upstream). Port the semantics: our Go wrapper derives a context deadline and substitutes a TOOL_TIMEOUT result only when its own timer fired. Port constants CAREFULLY from real dsh source - grep the .ts before fixing a threshold.
- The e2e LSP server: prefer configuring the LSP manager to spawn `gopls` (BSD-3) if present in the harness PATH; the test must SKIP (not fail) when gopls is absent, since a hermetic CI runner may lack it. Optionally ship a tiny stdio stub server speaking the JSON-RPC LSP protocol for the timeout/lifecycle assertions - that keeps the full contract hermetic. Both are acceptable; pick per harness reality and record the choice in the report.
