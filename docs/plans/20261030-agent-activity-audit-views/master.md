# Agent Activity + Audit Views - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 7 leaf documents under this node
- **Scope:** A unified, read-only view over every agent's activity, audit
  findings, and drift scores - queryable chronologically / by task / by plan
  via RPC, and exposed at parity in the TUI and the Flutter GUI, with an
  optional Langfuse OTLP trace sink.

## Goal

Meept logs agent activity across four silos (tasks.db, sessions.db,
audit_chain.db, employee audit findings) with no cross-store read path, no
drift RPC, and no UI surface. An operator cannot answer "what has every agent
been doing in the last hour, what did they find, and are any of them
drifting?" without direct sqlite access. This tree builds the read-only
aggregator, the RPC query surface, and first-class TUI + GUI views; findings
and drift scores get their own views. An optional leaf adds an OTLP trace
export so an operator who self-hosts Langfuse gets web-based trace
exploration, without any requirement depending on it.

Source discussion (2026-10-30): user asked what meept has for agent
monitoring and a common activity queue; survey confirmed the aggregator and
drift RPC are net-new. User requirements, verbatim intent: "expose it via an
option in both tui and gui", "see the audit log and sort chronologically, by
task and plan", "a view to see findings and drift scores".

## Architecture

A new `internal/activity` package owns a read-only `Aggregator` that queries
the four existing stores through their EXISTING Go types (no new write path,
no schema change): task store + step store (`internal/task`), session store
(`internal/session`), auditlog chain store (`internal/auditlog`), and the
employee AuditStore (`internal/employee`). It runs purely on the daemon side
against handles already on `Components`, merges rows into a common
`ActivityRecord`, and exposes them two ways: new RPC methods (direct
`RegisterHandler` closures in `internal/daemon/activity_rpc.go`, pattern:
`memory_rpc.go`) and a bounded in-memory ring buffer of recent bus events
subscribing to `queue.job.*`, `task.*`, `agent.progress`, `turn.terminal`
for the live-tail view. The TUI gets a new view (pattern: `TasksModel`) with
three sort modes plus findings/drift panels; the GUI gets a new feature page
(pattern: `tasks_list.dart`) with the same modes, reading the same RPC via
HTTP handlers; both surfaces update the shared parity manifest
`internal/tui/testdata/gui_inventory.json`. The optional Langfuse leaf maps
turn/step boundaries to OTLP spans through a new trace sink next to the
existing metrics-only OTLP exporter. Plan grouping walks the INVERTED
linkage: `plans.task_id` -> `tasks.id` -> `task_steps.task_id` (tasks carry
no plan_id); findings carry their own `plan_id`.

## Interface Contracts

### C1: ActivityRecord

```
// internal/activity/types.go - Owner: 01-aggregator
// Consumers: 02-rpc, 03-tui, 04-gui (via JSON), 05-ring
type ActivityRecord struct {
    Kind        string            `json:"kind"` // "task_step"|"session_message"|"audit_record"|"bus_event"
    At          time.Time         `json:"at"`   // RFC3339; primary sort key
    AgentID     string            `json:"agent_id,omitempty"`
    SessionID   string            `json:"session_id,omitempty"`
    TaskID      string            `json:"task_id,omitempty"`
    StepID      string            `json:"step_id,omitempty"`
    PlanID      string            `json:"plan_id,omitempty"` // resolved via plans.task_id walk
    State       string            `json:"state,omitempty"`
    Severity    string            `json:"severity,omitempty"` // info|warning|critical (findings/audit)
    Summary     string            `json:"summary"`            // one-line, envelope-stripped, <=200 runes
    Detail      map[string]string `json:"detail,omitempty"`
}
```

### C2: Activity query RPC contract

```
// Registered in internal/daemon/activity_rpc.go (direct closures, pattern memory_rpc.go)
// Consumers: 03-tui (via internal/tui/rpc.go wrappers), 04-gui (via HTTP api_handlers)
activity.query   params: {"since"?:RFC3339,"until"?:RFC3339,"sort"?:"chrono"|"task"|"plan",
                          "task_id"?:string,"plan_id"?:string,"agent_id"?:string,
                          "kinds"?:[string],"limit"?:int,"offset"?:int}
                  result: {"records":[ActivityRecord],"total":int}
activity.recent  params: {"limit"?:int} -> {"records":[ActivityRecord],"dropped":int}  // ring buffer tail
activity.overview params: {} -> {"by_agent":[{"agent_id","active_tasks","last_activity",
                          "drift_score","open_findings"}],"generated_at"}
activity.findings params: {"employee_id"?:string,"since"?:RFC3339,"severity"?:string,
                          "resolved"?:bool,"limit"?:int,"offset"?:int}
                  result: {"findings":[AuditFinding-as-JSON],"total":int}
```

HTTP parity (leaf 04): `GET /api/v1/activity`, `GET /api/v1/activity/recent`,
`GET /api/v1/activity/overview`, `GET /api/v1/activity/findings` - JSON
field names IDENTICAL to the RPC result shapes (one mapping table in leaf 04).

### C3: Ring buffer

```
// internal/activity/ring.go - Owner: 05-ring
// Consumers: 02-rpc (activity.recent), daemon wiring
func NewRing(capacity int) *Ring
func (r *Ring) Append(rec ActivityRecord)            // drops oldest; atomic under mutex
func (r *Ring) Tail(n int) ([]ActivityRecord, int)   // newest-last slice, dropped-counter total
// Wired via bus.SubscribeT/raw Subscribe on: queue.job.*, task.completed,
// task.failed, agent.progress, turn.terminal - best-effort, never blocks publishers.
```

### C4: Drift + findings RPC exposure

```
// leaf 02 registers:
agents.review  params: {"employee_id":string} -> Manager.Review(ctx, id) result as JSON
               (Review struct: DriftScore + RecentFindings; internal/employee/manager.go:2220)
// Findings reuse Manager.ListAuditFindings behind activity.findings (C2).
```

### C5: OTLP trace sink (optional leaf)

```
// internal/metrics/otlp_trace_sink.go - Owner: 07-langfuse
// Config (internal/config/schema.go, sibling of metrics.otlp):
type OTLPTracesConfig struct { Enabled bool `json:"enabled"`; Endpoint string `json:"endpoint"`; ... }
// Spans: turn-level (name "chat.turn", attrs session_id/agent_id/model) and
// step-level (name "task.step", attrs task_id/step_id/state). OTLP/HTTP JSON
// protobuf-encoded spans to Endpoint, batched, best-effort, never fails a turn.
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-aggregator.md | leaf | none | 75K | A |
| 02 | 02-rpc.md | leaf | 01, 05 | 85K | B |
| 03 | 03-tui.md | leaf | 02 | 95K | C |
| 04 | 04-gui.md | leaf | 02 | 95K | C |
| 05 | 05-ring.md | leaf | 01 | 55K | A |
| 06 | 06-e2e-suite.md | leaf | 01, 02 | 75K | D |
| 07 | 07-langfuse-sink.md | leaf | none | 80K | A |

Dispatch order: batch A = {01, 05, 07} together; batch B = {02} after 01+05
REVIEWED; batch C = {03, 04} together after 02 REVIEWED; batch D = {06}
after 02 REVIEWED (06 exercises RPC + aggregator end to end; TUI/GUI are
out of its scope).

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate (code branches, before ANY implementation dispatch)

Dispatch 00T-/01T- test leaves first: tests only, fail RED for
missing-implementation reasons. Validate in-session (never delegate
review) against the Test Gate matrix; apply the delta protocol; commit
gate tests; set children to TESTS_VALIDATED. Then implementation may
dispatch. See references/tests-first-gate.md.

This tree runs NO separate test-gate leaves. Justification: every leaf is
TDD-ordered internally (each task writes its failing test first), and the
consuming surfaces (03/04) are thin render layers over frozen contracts.
The e2e leaf 06 plus each leaf's in-package tests carry the coverage matrix
below. Deviation from the default gate is deliberate and recorded here.

### Phase 1: Dispatch Concurrency Group [A]

Dispatch same-letter children simultaneously via `delegate_task`: goal =
"Implement all tasks from [leaf]"; context = full leaf text + contracts +
conventions + relevant source INLINED; include the no-commit, no-git-add,
no-read_file, write-once-and-stop rules.

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check
leaf spec, contracts, Review Checklist; run tests; gate-test diff must be
empty (no gate leaves in this tree - instead: contracts unchanged). Pass:
commit + status REVIEWED. Gaps: re-dispatch with findings, max 3 cycles,
then escalate to parent.

### Phase 3: Integration Review

After ALL children REVIEWED: verify contracts between children, run the
full suite, check no import cycles. Gaps: re-dispatch the failing child
with contract fixes (uncommitted). Pass: normalize formatting, run the
line-number corruption check, commit integration changes, set children
COMPLETE, report COMPLETE to parent (if branch).

Integration must also verify: `make graphs` regenerated (new bus
subscriptions in 05 change topology; typed SubscribeT sites will be
UNDER-reported by the generator - note that in the PR, never chase it),
`make graphs-check` green, and the TUI/GUI parity test pair green
(`internal/tui/parity_test.go` + `ui/flutter_ui/test/widgets/command_palette_test.dart`).

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Contracts C1-C5 byte-identical to this master (any drift = NEEDS_FIXES)
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions
- [ ] No scope creep; no obvious bugs or security issues
- [ ] Read-only guarantee intact: no INSERT/UPDATE/DELETE on the four stores
- [ ] No debug artifacts (print debugging, TODOs, placeholders,
      commented-out code)
- [ ] No line-number corruption (`     N|` prefixes) in source files

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language/Framework:** Go 1.26 (repo pins GOTOOLCHAIN in go.mod/Makefile -
  use make targets or export GOTOOLCHAIN=go1.26.5 for bare go); Dart/Flutter
  for leaf 04.
- **Naming:** Go exported for cross-package seams (C1-C5), unexported
  internals; table-driven tests.
- **Imports:** stdlib, then internal, then external; no unused imports
  (U1000 pre-commit gate scans the whole staged package - no dead symbols).
- **Error handling:** wrap with %w; sentinel errors for not-found; never
  swallow.
- **Testing:** `go test -p 2 <pkg>` (never unbounded ./... on macOS);
  Dart: `flutter test` for touched widget tests only.
- **Formatting:** gofmt / `dart format` on all touched files before reporting.
- **UI text lowercase** (both surfaces); TUI tables sized via
  `internal/tui/tableutil` Size + SetRows only.
- **Do NOT commit. Do NOT run git add.** Orchestrator handles git.
- **No read_file on existing sources** (line-number corruption); explore
  with grep/cat; write each file once and stop.

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element (leaf/task/contract) | Verified by |
|-----|---------------------------------------------|-----------------------------------|-------------|
| R1 | "a common queue where task activities, findings... are logged" / unified view | 01-aggregator (cross-store read), C1/C2 | 01 in-package tests + 06 e2e |
| R2 | "sort chronologically, by task and plan" | C2 sort modes; 03-T2/T3, 04-T2 | 02 RPC tests + UI tests |
| R3 | "expose it via an option in both tui and gui" | 03-tui (ViewType + palette + keybinding), 04-gui (GoRoute + palette), parity manifest updates | parity_test.go + command_palette_test.dart |
| R4 | "see the audit log" (hash-chained auditlog) | 01-aggregator audit_record kind; activity.query | 01 tests + 06 |
| R5 | "a view to see findings and drift scores" | C4 agents.review RPC; 03 findings/drift panels; 04 findings/drift sections | 02 tests + UI tests |
| R6 | live visibility of what agents are doing now | 05-ring + activity.recent; 03 live-tail toggle; 04 recent panel | 05 tests + 02 tests |
| R7 | optional Langfuse trace exploration (hybrid decision) | 07-langfuse-sink, C5 | 07 in-package tests |
| R8 | findings sortable/filterable (since/severity/resolved) | activity.findings params (C2) | 02 tests + 06 |

Deferred: none. Silent omission is a defect.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-aggregator | REVIEWED | 1 | Committed 36f4211a 2026-10-31. In-session review: C1 byte-identical (gold JSON test), 12 tests green + -race clean, read-only verified, no internal/plan import. Deviations all pre-approved addenda. Plan-stamp note: resolver stamps batch-carried + queried planIDs (leaf 02 passes resolver; overview relies on Query.PlanID). |
| 02-rpc | REVIEWED | 1 | Committed 7829a857 2026-10-31. In-session review: all 5 methods registered, nil-guarded, -race green in overlay; findings since/resolved/offset enforced in-closure (Manager limits documented). Deviation accepted: agents.review as direct closure. |
| 03-tui | REVIEWED | 2 | Committed 25f3a460 (view+model+palette+manifest) + 043520d7 (tail-order contract fix). In-session review: suites+parity green; caught leaf-03's test fabricating a newest-first wire the daemon never sends (real order: chronological ascending, e2e-pinned) - replaced with a contract-true test. gui_inventory deviation flips gui:true at leaf 04. |
| 04-gui | REVIEWED | 3 | CLOSED. T1 f389ef0b (HTTP handlers over the s.rpcCall seam), T2/T3 0a023292 (parity flipped gui:true, TestParity green), generated graphs a5a8b7d9, salvage 3dba2286. Salvage content: activity_list.dart declared a feature-local provider returning SdkApiClient(host:'localhost', port:8080), bypassing providers/providers.dart:36 sdkClientProvider (SdkApiClient.storage reads the persisted host/port/API KEY) - wrong server, unauthenticated, on any non-default install; drift + findings panels constructed the client directly (same bug, plus no mock seam). All four sites now ref.read(sdkClientProvider); grep "SdkApiClient(host" over lib/features/activity/ returns nothing. Added the leaf T3 coverage that was impossible before the mock seam: drift 0.40, findings severity/resolved, recent drop counter. Final gates: flutter test test/features/activity 6/6, command_palette 11/11, test/e2e gui-flows 23/23, analyze zero warnings, dart format clean, go build ./... ok, activity + comm/http + tui suites ok, e2e-fast-area activity-views ok (16.8s). Attempt 1 was a DISPATCH ERROR on my side: the brief forbade Go edits, but leaf 04 T1 requires HTTP handlers in internal/comm/http (the GUI talks HTTP, not the RPC socket - leaf doc lines 32-34). Attempt 2 split correctly (T1 Go + Dart fixer). My staged T1 files were consumed by the co-orchestrator's commit f389ef0b - the git INDEX is shared across sessions, same crossing-write class as the manifest; all 19 pre-commit gates passed on that commit. |
| 05-ring | REVIEWED | 2 | Salvaged+committed 7829a857 (writer session went silent mid-flight - ghost class). Second pass b08874c1 fixed 4 contract defects the first review waived: Tail order was newest-FIRST against C3 newest-LAST (the sibling's own e2e asserts oldest-first at activity_views_test.go:477-480, proving the contract was right and the code wrong); bridge subscribed to task.ev.completed/failed which nothing publishes (taskEventPrefix, internal/task/registry.go:69, wraps COMMAND topics only - real events are bare task.completed internal/agent/tactical.go:2196 and task.failed internal/agent/strategic.go:598), so the live tail showed no task completions; queue job_id/worker_id were written into TaskID/AgentID and agent.progress conversation_id into SessionID (identity-space violation, now in Detail); recover() was deferred around the range loop so one bad payload killed the subscription for the daemon's life. WireBusBridge now returns []*bus.Subscriber and stopActivityRing unsubscribes them (c.activityBridgeSubs) instead of only nil-ing the ring. |
| 06-e2e-suite | REVIEWED | 1 | Committed e8b1f7c5 2026-10-31. Orchestrator re-ran the suite independently: green (16.5s) on the combined tree incl. b08874c1 ring fixes; scenarios activity-views-01..04 registered by the co-orchestrator session; the suite pins the authoritative tail wire order (insertion-ordered oldest first, :480). |
| 07-langfuse-sink | REVIEWED | 1 | Committed 1510efc1 2026-10-31. In-session review: default-OFF inert proven, bus-driven producers (no sync loop hook), circuit+drop counters, config in new otlp_traces.go; schema.go staged with ONLY the OTLPTraces field (intake hunks left to sibling). Producer split (bus decode in daemon wiring) pre-approved cycle-avoidance. |

Dispatch schedule (user constraint: 2 concurrent, raised to 3 on request
2026-10-31): wave 1 = {01, 07, 05}; wave 2 = {02}; wave 3 = {03, 06};
wave 4 = {04}. 04 follows 03 because both write the shared
internal/tui/testdata/gui_inventory.json (one-writer-per-file).

Attempt 1 (2026-10-30 23:31, {01, 07}) FAILED with zero files written.
Cause was the subagent tool layer, not the task: the host runs
`ulimit -n 256`, and both children spent 45 minutes on terminal calls that
timed out at 420s plus `write_file` failures with
`[Errno 35] Resource temporarily unavailable`; one also hit
`search_files: Content search requires ripgrep (rg) or grep`. The
delegation owner exited before recording a terminal result. Verified
afterwards: no internal/activity/, no internal/metrics/otlp_trace*, no
ActivityRecord symbol anywhere — nothing to salvage, `go build ./...` clean.
Attempt 2 (2026-10-31) re-dispatched {01, 07} plus 05, with per-leaf briefs
that add: batch terminal calls (<25 per leaf), short test commands, a
retry-once-then-switch-tool rule, and strict file-ownership lists.
05 is dispatched as a CONTRACT-FROZEN leaf: it authors ring.go/busbridge.go
verbatim against C1 without defining ActivityRecord, so it can run beside 01
(RED-BUILD-BLOCKED until 01 lands is the expected state, not a failure).
05's task T3 (daemon wiring in components.go) is MOVED to leaf 02's wiring
task so only one agent ever edits internal/daemon/components.go.

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./...` (clean worktree if siblings hold WIP; GOTOOLCHAIN pinned
   in-repo) - zero errors.
2. `go test -p 2 ./internal/activity/... ./internal/daemon/... ./internal/employee/... ./internal/metrics/...` - green.
3. `make e2e-fast-area AREA=activity-views` - the new suite (leaf 06) green:
   scratch daemon, seed sandbox stores, `activity.query` with each sort mode
   returns expected ordering, `activity.recent` reflects bus events,
   `activity.overview` carries drift scores, chain verify still ok.
4. `go test -p 2 ./internal/tui/ -run TestParity` and
   `cd ui/flutter_ui && flutter test test/widgets/command_palette_test.dart`
   - both green (R3 parity).
5. `make graphs && make graphs-check` - fresh (05 adds subscriptions).
6. Manual live gate (scratch MEEPT_HOME, remapped ports, no cloud): boot,
   one chat turn, `meept` RPC call to activity.recent shows the turn's
   progress events; TUI new view renders; GUI page renders.

## Test Gate

No separate test-gate leaves (see Dispatch Protocol Phase 0 for the recorded
deviation). Coverage matrix - each row is validated by the listed leaf's own
RED-first tests plus the e2e suite; orchestrator validates against this
matrix at each review:

| # | Feature (from plan) | Source (leaf/spec section) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------------|-----------|-----------|--------|
| F1 | cross-store chronological merge | 01-aggregator T1-T2 | internal/activity tests | activity-views suite | PENDING |
| F2 | sort chrono/task/plan + filters | 01 T3-T4, 02 T2 | internal/activity + rpc tests | activity-views | PENDING |
| F3 | live-tail ring (bus -> recent) | 05 T1-T2 | internal/activity ring tests | activity-views | PENDING |
| F4 | activity RPC surface + HTTP parity | 02 T1-T4 | rpc closure tests | activity-views (RPC leg) | PENDING |
| F5 | drift + findings exposure (C4) | 02 T5 | employee handler tests | activity-views | PENDING |
| F6 | TUI view + parity entry | 03 T1-T4 | internal/tui model tests | parity_test (manifest) | PENDING |
| F7 | GUI page + palette parity | 04 T1-T3 | flutter widget tests | command_palette_test | PENDING |
| F8 | OTLP trace spans to endpoint | 07 T1-T2 | internal/metrics trace sink tests | n/a (opt-in, no daemon dependency) | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: none yet.

## Open Questions

- Q1 Keybinding: "a" proposed for the TUI activity view (unused today).
  Any objection?  - default: proceed with "a".
- Q2 Polling cadence: 2s proposed for both surfaces' live tail. Cheaper
  (5s) available if daemon load matters.  - default: 2s.
- Q3 Ring capacity: 1000 events (~1MB) proposed.  - default: 1000.
- Q4 Langfuse leaf (07): dispatch with batch A or hold until the core
  (01-06) lands?  - default: hold until 02 is REVIEWED, then dispatch.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda (dated, appended to the dispatch brief). Never edit a
dispatched leaf document mid-execution.

## Notes

- Baseline (2026-10-30, before any dispatch): `go build ./...` clean. Scoped
  test baseline `go test -p 2 -count=1 ./internal/{task,session,auditlog,
  employee,metrics,config,daemon,tui,comm/http}` = all green except
  `internal/tui` `TestACPModal_ResolvedToastCarriesApprover`
  (modal_flows_test.go:295 "resolved frame did not toast"), which passed on
  an isolated `-count=2` re-run: a pre-existing flake, not tree-caused.
  Attribute any NEW failure in those packages to this tree.
- Survey facts (2026-10-30, file:line) are inlined in each leaf's Verified
  Anchors; re-grep before editing - siblings move lines.
- The drift RPC is net-new: `Manager.Review` (internal/employee/manager.go:2220)
  has NO RPC/HTTP caller today; agents.review (C4) is the first exposure.
- Plan grouping is INVERTED: `plans.task_id` (internal/plan/store_sqlite.go:73),
  no plan_id on tasks. Findings carry their own plan_id
  (internal/employee/enforcement.go:1662).
- audit_chain.db lives inside the memory data dir and sessions.db has a
  legacy rename migration (internal/memory/migrate.go:34-64): the aggregator
  resolves paths via the Components handles it is HANDED, never via
  hardcoded ~/.meept paths.
- OTLP today is metrics-only (internal/metrics/otlp_http_sink.go, wiring
  internal/daemon/otlp_wiring.go:31-67); no spans exist anywhere. Leaf 07 is
  the first span producer.
- The generator under-reports typed bus.SubscribeT subscribers in
  docs/generated (known); never treat the graph as evidence of absence.
- Langfuse posture: external MIT service, OTLP over HTTP, opt-in, never a
  dependency of the binary - consistent with the subprocess-deps rule.
- e2e manifest: new suite needs `suites[]` entry + `path_map` entries for
  internal/activity/ in the SAME commit (fail-closed pre-commit gate).
