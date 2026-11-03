# dsh Convergence: Model-Facing Feature Surface - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 6 branches (01-delegation, 02-goals-plans, 03-session-storage,
  04-tool-surface, 05-schedule, 06-office-skills)
- **Scope:** Close the model-facing feature gaps found in the 2026-11-02
  dsh-vs-meept audit (/tmp/dsh-audit/). dsh's every-is-a-plugin harness exposes
  subagent, workflow, goal, plan, schedule, skill, and tool capabilities as
  first-class model tools + a versioned session-log substrate; meept ships the
  engine (sqlite, RPC, queue, LSP client) but often omits the model tool or
  session-mode that makes it usable. This tree surfaces those model-facing seams
  and matches the dsh delegation/goals/composition archetypes.

## Goal

Meept's delegation is exactly ONE mechanism (synchronous, in-process, fresh,
spec-modeled, no child handle), its goals defer an autonomous Tier-3 which
shipped under a different name, its schedule lacks RFC5545 timing kinds +
session-delivery receipts, and its engine-only features (LSP, queue, sqlite,
skills) lack model tools or session modes. This tree delivers:

- A full delegation seam modeled on dsh: async spawn + child handle, per-child
  model selection, steer/interrupt/list children, history-seeded fork, durable
  child descriptor + cold-resume, and ACP as a delegation provider.
- A model-authored orchestration (workflow) tool with a confined runtime.
- Fully autonomous goals: rounds cap + activation, model get/create/update_goal
  tools, and a round driver that queues round N+1 with zero human turns.
- A versioned append-only session event log (the substrate projections,
  telemetry, and export build on), plus a cross-session bounded reference and
  durable storage record-change events.
- Model-facing surface for the engine that exists: per-tool timeout on the Tool
  interface, an LSP navigation tool, skill hot-reload + durable per-session
  skill catalog + /name invocation, plan-mode present-for-review, message-quotes.
- RFC5545 timing kinds + session-delivery receipts in the scheduler.
- A skill-office capability (docx/pptx/xlsx) via a vetted OSS Go library.

## User decisions (frozen 2026-11-02, "Create a hierarchical plan ... 1. yes ... 14. yes")

| # | Decision | Source |
|---|----------|--------|
| D1 | Items 1-10, 13, 14 implement per this tree | user "yes" for workflow(1), session-ref(2), per-child model(3), child control+id(4), versioned session log(5), storage events(6), goal tools+rounds(7), per-tool timeout(8), plan-mode(9), LSP nav(10), skill hot-reload(13), message-quotes+/name(14) |
| D2 | Items 11 (image offload) and 12 (tmux/time-context) become GitHub issues, NOT leaves | user "Create a gh issue for this" |
| D3 | Full dsh delegation seam, copy the logic archetype from deepseek | user: "we want to have this full capability, it's necessary for child agent handling ... Copy the logic archetype from deepseek" |
| D4 | Goals fully autonomous now | user: "we were supposed to have implemented this fully, please do so now" |
| D5 | RFC5545 must be satisfied in meept | user: "Make sure RFC5545 is satisfied in meept" |
| D6 | skill-office uses a vetted many-star OSS Go library; unioffice is commercial-EULA and REJECTED; excelize (BSD-3-Clause, ~21k stars) is the xlsx choice | user: "find some good (many stars) online to use"; license gate (user: GPL/AGPL OK as subprocess deps, never vendor/link into shipped binaries) |
| D7 | Tests-first gate is a PREREQUISITE for all implementation | user: "the plan clearly indicates END TO END and unit tests for all features as a prerequisite for implementation" |
| D8 | 04-tool-surface LSP leaf reuses the existing LSP client that meept already has | user: "reuse what exists, if we can, within go availability" |
| D9 | Items 11/12 gh issues are created as part of this plan's docs leaf, not implementation | D2 |

## Architecture

```
model-facing tool/session-mode surface
   |
   +-- delegation seam  (01): async spawn+child-id, per-child model,
   |       steer/interrupt/list, fork-seed, descriptor+cold-resume, ACP provider,
   |       workflow orchestration tool (confined program -> fan-out -> JSON value)
   +-- goals/plans     (02): rounds cap+activation, model goal tools,
   |       autonomous round driver, plan-mode present-for-review
   +-- session/storage (03): versioned append-only event log + projections,
   |       cross-session bounded reference, storage record-change events
   +-- tool surface    (04): per-tool timeout (Tool.TimeoutMs), LSP nav tool,
   |       skill hot-reload + catalog + /name, message-quotes, plan/skill modes
   +-- schedule        (05): RFC5545 timing kinds, session-delivery receipts
   `-- office skills   (06): skill-office via excelize + docx/pptx picks
```

Delegation is the spine: workflow (01) and goals (02) both fan out through it.
Session log (03) is a shared substrate consumed by projections and telemetry.
Everything else is surface over existing engines.

## Interface Contracts

Cross-branch contracts live here once. Leaves consume exact signatures only;
they never restate full definitions.

### C1: Delegation seam (Owner: 01-delegation)

```go
// internal/agent/delegate.go (NEW seam; replaces the sync delegate_task tool
// as the primary delegation surface while keeping byte-compatible legacy).
type DelegateRequest struct {
    AgentID     string            `json:"agent_id"`
    Message     string            `json:"message"`
    Context     string            `json:"context,omitempty"`
    OutputSchema json.RawMessage   `json:"output_schema,omitempty"`
    Mode        string            `json:"mode,omitempty"`   // "spawn"|"fork"|"acp" (default spawn)
    Model       string            `json:"model,omitempty"`  // alias or model ref
    Provider    string            `json:"provider,omitempty"`
    ReasoningEffort string        `json:"reasoning_effort,omitempty"`
    Depth       int               `json:"depth,omitempty"`  // max nesting, default 1
}
type DelegateResult struct {      // EXPANDS today's (platform.go:309-314)
    AgentID      string `json:"agent_id"`
    Success      bool   `json:"success"`
    Response     string `json:"response,omitempty"`
    Error        string `json:"error,omitempty"`
    ChildID      string `json:"child_id,omitempty"`   // NEW: steer/interrupt key
    Status       string `json:"status,omitempty"`     // running|completed|interrupted
}
// Children: register in a per-session child registry keyed by parent session
// (childID -> ChildRecord{Mode,State,Model,CreatedAt}). Steer/interrupt/list
// operate on this registry + the existing queue/steer + ACP cancel paths.
// Consumers: 02-goals (round driver waits on children), 01-workflow leaf.
```

### C2: Child control (Owner: 01-delegation, control leaf)

```go
// Model-facing tools: subagent_send_message(req{child_id, message}),
// subagent_interrupt(req{child_id}), subagent_list_children().
// Steer wires to the existing MessageQueue.Steer + ACP process signal for a
// delegated child; list is derived from the C1 child registry.
// Consumers: 01-workflow (join), 02-goals (round driver).
```

### C3: Workflow orchestration tool (Owner: 01-delegation, workflow leaf)

```json
// Model-facing "workflow" or "orchestrate" tool executing a confined program:
// { engine: "dsh-archetype", script: "<json>, steps:[agent|parallel|pipeline]",
//   max_concurrent_agents, max_total_agents, run_in_background }
// Confinement: reuse meept's goja codemode VM (internal/codemode/sandbox.go) -
// no fs/network/timer/Node APIs; "the agents do the work". Steps fan out via
// C1 DelegateRequest and join via C2. Caps are FAIL-CLOSE (fatal, never
// fallible into a narrower config). Returns JSON value + optional job id.
// Consumers: 02-goals round driver MAY reuse the fan-out engine.
```

### C4: Goal autonomy (Owner: 02-goals)

```go
// internal/employee/goal.go gains:
//   MaxRounds int; RoundsStarted int; Activate() (arms continuation);
//   State adds "blocked"; IsBlockedAfterNConsecutive() default 3.
// Model tools: goal_get(show), goal_create, goal_update(self-complete/
//   self-block at N consecutive rounds). Human /goal command unchanged.
// Round driver: at quiescence+idle+armed, queue round N+1 as a user message
// (source kind="goal") re-entering the loop with ZERO human turns; waits on
// C1 children; auto-blocks on MaxRounds. Reuses scheduler/queue deferred-turn.
// Consumers: 04-tool-surface plan-mode (goal mode vs plan mode are distinct).
```

### C5: Versioned session log (Owner: 03-session-storage)

```go
// internal/session: add Version int to store + migration catalog (adjacent,
// forward-only; refuse stored-newer). Session-message rows carry a schema
// version; a SessionProjection registry fold-units derive queryable views
// (session-list, TUI/GUI). Opaque today; this is additive, not a rewrite.
// Consumers: C6 storage events, 04 session-modes (persisted per-session bools).
```

### C6: Storage record-change events (Owner: 03-session-storage)

```go
// After every persistent-store commit, emit on the bus:
// topic storage.record.changed, payload {store,key,op:"put|delete",value}
// (schema-validated, op-tagged, durable). Consumers: GUI/TUI live refresh.
```

### C7: Cross-session bounded reference (Owner: 03-session-storage, session-ref leaf)

```go
// Context plugin/tool: reference <otherSessionID> injects a bounded snapshot.
// Budget: max 3 refs, 0.2x context window with 64KiB floor; project user+
// assistant text only; drop-oldest then head-tail truncate; full transcript
// spilled to $MEEPT_HOME/spill + locator on truncation.
// Consumers: none downstream (standalone capability).
```

### C8: Per-tool timeout + LSP nav (Owner: 04-tool-surface)

```go
// internal/tools/interface.go: Tool interface gains
//   TimeoutMs() time.Duration  // 0 = no declared timeout
// Wrapper in the executor arms a context deadline when >0 and returns a
// structured timed-out isError result on expiry (mirrors dsh timeout-policy).
// LSP navigation: model-facing tool lsp_navigate{query,op:go_to_definition|
//   find_references|go_to_implementation|hover,workspace} reusing the EXISTING
// internal/code/lsp client (writethrough already spawns/serves it). Result caps
// + timeoutMs. Consumers: none.
```

### C9: Skill catalog (Owner: 04-tool-surface, skill leaf)

```go
// Durable per-session skill catalog + fsnotify hot-reload on ~/.meept/skills
// and project .meept/skills (replaces the process-cache relist in
// internal/agent/context_injector.go:112-136). Live reload: skills/change bus
// event + invalidation. /name direct invocation gesture (explicit slash, NOT
// the existing named_agent_route). Consumers: none.
```

### C10: Schedule timing + delivery (Owner: 05-schedule)

```go
// internal/scheduler: add timing kinds over the robfig/cron engine:
//   rrule (RFC5545, DST-safe wall-clock), daily, weekly, at, every.
// Delivery contract: AgentJob gains optional TargetSessionID; delivery is an
// inbox follow-up message (not fire-and-forget chat.request), with a durable
// receipt row (ScheduleDeliveryReceipt) + bounded history + in-flight recovery
// across restart via the claim store (at-least-once).
// Consumers: 04 GUI/tasks page parity note.
```

### C11: skill-office (Owner: 06-office-skills)

```go
// Bundled skill runtime for docx/pptx/xlsx. xlsx: github.com/xuri/excelize/v2
// (BSD-3-Clause, ~21k stars, Go>=1.26 pinned by meept). docx/pptx: vetted pick
// from the assignment leaf (MIT/Apache/BSD preferred, no commercial EULA -
// unioffice REJECTED). Skill frontmatter requires-tools gates the tools.
// Consumers: none.
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-delegation/ (branch) | branch | none | - | A |
| 02 | 02-goals-plans/ (branch) | branch | 01 (C4 waits on C1 children) | - | B (after 01 partial) |
| 03 | 03-session-storage/ (branch) | branch | none | - | A |
| 04 | 04-tool-surface/ (branch) | branch | none | - | A |
| 05 | 05-schedule/ (branch) | branch | none | - | A |
| 06 | 06-office-skills/ (branch) | branch | none | - | A |

Dispatch: Phase 0 gates per branch. Branches 01, 03, 04, 05, 06 dispatch in
parallel (independent). Branch 02 dispatches after 01 reaches REVIEWED (round
driver reuses C1 children + C3 fan-out). Items 11/12 gh issues are filed in
06-docs or as a top-level open-questions action - see master Open Questions.

## Dispatch Protocol

For each branch, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate (code branches, before ANY implementation dispatch)

Every branch orchestrator carries a `## Test Gate` coverage matrix + 00T-unit /
00T-e2e test leaves (hermetic e2e under e2e/suites/ + manifest path_map same
commit). Dispatch test leaves first: tests only, fail RED for
missing-implementation reasons. Validate in-session (never delegate review)
against the matrix; apply the delta protocol (update the LESSER artifact);
commit gate tests; set children TESTS_VALIDATED. Implementation may then
dispatch. Repo e2e rules: suites in e2e/suites/<name>/ with the `e2e` build
tag; manifest + path->suite in e2e/manifest.json same commit; scratch MEEPT_HOME
harness (harness.WithPreBootHook); assert through live RPC/Ws surface.

### Phase 1: Dispatch Concurrency Groups

Dispatch same-group branches in parallel via `delegate_task`: goal = "Implement
all tasks from <branch>"; context = branch text + the frozen contracts it
consumes + conventions + relevant source INLINED + the no-commit,
no-git-add, no-read_file-on-source, write-once-and-stop rules.

### Phase 2: Review and Commit Each Branch

Review in-session (the main model - never a delegated reviewer). Check branch
spec, contracts, Review Checklist; run the branch's verify commands; gate-test
diff must be empty. Pass: commit (explicit pathspecs, single-line messages;
orchestrator is the only committer) + status REVIEWED. Gaps: re-dispatch with
findings, max 3 cycles, then escalate.

Commit hygiene (repo-proven): run affected gates directly (`go test -p 2`,
gofmt, vet scoped); `git add` every NEW file BEFORE commit; normalize new .md
to ASCII (pre-commit-ascii gate); every commit reviews root AGENTS.md; U1000
scans the whole staged package - delete dead code rather than bypassing.

### Phase 3: Integration Review

After all branches REVIEWED: verify contracts C1-C11 across the combined tree,
`go build ./...`, `make test` (background, output to file), new e2e suites in
isolation (WITHOUT the worktree merge hazard: this tree runs in the isolation
worktree per hierarchical-planning), `make graphs` + `make graphs-check`
(RPC/tool/config surface changed), `make docs-generate` if config gendoc
drifted, `scripts/config-drift-check.py`. Gaps: re-dispatch the failing branch.
Pass: set branches COMPLETE.

## Review Checklist

The orchestrator (main model) verifies each branch in-session:

- [ ] All tasks from the branch document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
- [ ] Tests written and passing (e2e for features; unit pins only for bug fixes)
- [ ] Code follows project conventions (no ignored errors, no mutex across
      I/O, typed-nil guards on wiring seams, predictable-ID prevention)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders)
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] Every model-facing tool has its tool registration + RPC + TUI/GUI parity
      or an explicit documented deviation (root AGENTS.md wiring requirement)

## Coding Conventions

- **Language:** Go 1.22+ (repo pins GOTOOLCHAIN; x/tools analyzers need it)
- **Naming:** exported/unexported per repo; tool names snake_case
- **Imports:** stdlib group first, then third-party; no unused (U1000-strict)
- **Error handling:** wrap with context; no ignored errors; no bare panic(err)
- **Security:** predictable-ID prevention (pkg/id.Generate, never UnixNano);
      typed-nil guards on every Set*/wiring seam; scrub secrets in child env
- **Testing:** table-driven unit tests in-package; NEW feature tests go in
      hermetic e2e tier (root AGENTS.md e2e policy) - no new unit files for
      features unless the branch doc explicitly says so
- **Formatting:** gofmt before reporting; ASCII only in new .md
- **Model-facing rule:** every capability ships a user-facing surface (CLI,
      TUI, GUI, or HTTP) + agent wiring, or documents the deferral

## Requirement Traceability

| Req | Source (user words, verbatim) | Plan element | Verified by |
|-----|------------------------------|--------------|-------------|
| R1 | "child agent handling ... full capability ... Copy the logic archetype from deepseek" | 01-delegation (C1-C3) | 01 test gate + e2e |
| R2 | "Goals: we were supposed to have implemented this fully" | 02-goals (C4) | 02 test gate + e2e |
| R3 | "Make sure RFC5545 is satisfied in meept" | 05-schedule (C10) | 05 test gate |
| R4 | "END TO END and unit tests for all features as a prerequisite" | every branch Phase 0 gate | each branch Test Gate matrix |
| R5 | "find some good (many stars) online to use" + license gate | 06-office-skills (C11) | 06 assignment + gate |
| R6 | items 1-10,13,14 = moderate features; 11,12 = gh issues | all branches; Open Questions gh-issue action | per-branch gate |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-delegation/ | PENDING | 0 | spine |
| 02-goals-plans/ | PENDING | 0 | waits 01 partially |
| 03-session-storage/ | PENDING | 0 | |
| 04-tool-surface/ | PENDING | 0 | |
| 05-schedule/ | PENDING | 0 | |
| 06-office-skills/ | PENDING | 0 | research-gated |
| gh-issues (11,12) | PENDING | 0 | filed from docs leaf |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

- `go build ./...` then `make test` (short, -p 2) - all GREEN.
- `make e2e-fast` - delegation, goals, schedule, tool-surface suites.
- Cross-boundary: 02-goals round driver joins a 01-delegation child
  (subagent_send_message resume) -> e2e asserting a spawned goal round resumes
  a running child.
- 04 plan-mode present-for-review drives the existing ApprovePlan gate -> e2e
  asserting approve/send-back round-trips.
- `make graphs` + `make graphs-check`; `make docs-generate` if gendoc drifted.
- `scripts/config-drift-check.py` clean.

## Test Gate

Test leaves: every branch owns 00T-unit + 00T-e2e per branch orchestrator;
validate in branch Phase 0, control agent in-session. See each branch's Test
Gate matrix. Root-level e2e owns the cross-branch C1-C4 contract tests above.

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | delegation seam + child control + fork-seed | 01 (C1,C2) | branch unit | branch e2e | PENDING |
| F2 | per-child model selection | 01 | branch unit | branch e2e | PENDING |
| F3 | workflow orchestration tool | 01 (C3) | branch unit | branch e2e | PENDING |
| F4 | model goal tools + autonomous rounds | 02 (C4) | branch unit | branch e2e | PENDING |
| F5 | plan-mode present-for-review | 02 | branch unit | branch e2e | PENDING |
| F6 | versioned session log + projection | 03 (C5) | branch unit | branch e2e | PENDING |
| F7 | cross-session bounded reference | 03 (C7) | branch unit | branch e2e | PENDING |
| F8 | storage record-change events | 03 (C6) | branch unit | branch e2e | PENDING |
| F9 | per-tool timeout | 04 (C8) | branch unit | branch e2e | PENDING |
| F10 | LSP navigation tool | 04 (C8) | branch unit | branch e2e | PENDING |
| F11 | skill hot-reload + catalog + /name | 04 (C9) | branch unit | branch e2e | PENDING |
| F12 | message-quotes | 04 | branch unit | n/a (UI) | PENDING |
| F13 | RFC5545 + delivery receipts | 05 (C10) | branch unit | branch e2e | PENDING |
| F14 | skill-office docx/pptx/xlsx | 06 (C11) | branch unit | branch e2e | PENDING |

Delta log: [date, feature ID, direction, one-line reason]

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated
addenda. Never edit a dispatched leaf document mid-execution.

## Open Questions

1. gh issues for items 11 (image offload) + 12 (tmux/time-context) - filed as
   part of branch 04's docs task (current.sh: 04 ships a docs leaf that opens
   the two issues), not a code leaf. Owner: orchestrator at Phase 2.
2. docx/pptx library pick beyond excelize - resolved in 06-office-skills
   assignment leaf (research-gated); unioffice REJECTED (commercial EULA;
   violates user license gate). Await branch 06's vetted choice.
3. Session-log migration (C5) is additive versioning; DO NOT rewrite the
   existing opaque sqlite store. Projections read the raw log.

## Notes

- Delegation (01) supersedes the synchronous delegate_task tool while keeping
  it byte-compatible for existing callers (issue of "supersede existing
  functionality" - user D1).
- The whole tree SOURCES from the dsh audit (dsh HEAD ce7073d, deliverables at
  /tmp/dsh-audit/*.md). Grep the actual dsh source before porting any constant
  (thresholds, defaults) - the audit's file:line a is the re-read point, not
  the final say.
- License gate (user memory): GPL/AGPL acceptable ONLY as subprocess deps,
  never vendor/linked into shipped binaries. Office libs must pass this or be
  rejected.
