# Ticket Intake Factory — Implementation Orchestrator (Issue #67)

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) run the test gate, (2) dispatch implementation agents,
> (3) review their work in-session, (4) re-dispatch if incomplete,
> (5) track completion. Do NOT implement code yourself.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 6 (1 test-gate leaf, 5 implementation leaves)
- **Scope:** Close the L2 work loop's front door: a GitHub Issues intake
  poller turns labeled tickets into employee goals (dedup + re-dispatch
  on re-arrival), and goal outcomes comment back on the ticket —
  reusing the #64 githubsvc client pool, the #64 effects ledger for
  idempotency, and the existing employee goal machinery.
- **Tracking issue:** bhodgens/meept#67

## Goal

Meept's goal system has no front door: goals start at chat. A factory
pipeline "ticket -> goal -> work -> status write-back" makes the loop
agent-executable from a ticket queue. This tree adds an
`internal/intake` package (poller interface + GitHub adapter), an
`[intake]` config block, daemon wiring on the proven
`employeeSchedulerAdapter.RunAtInterval` path, effects-ledger dedup keyed
by (ticket_id, mandate-hash), and outcome write-back comments via the
#64 `github_issue_comment` machinery.

## Architecture

- New package `internal/intake/`: the poller interface (the seam for
  Jira/Linear follow-ons) + `github.go` (label-gated issue watch) +
  `linkage.go` (ticket->goal mapping + dedup) + `writeback.go` (outcome
  comments).
- Poll scheduling rides the employee scheduler adapter
  (`internal/daemon/employee_scheduler_adapter.go` — `RunAtInterval`,
  proven wired at components.go:5121 for assess/audit jobs). NOT a new
  goroutine lifecycle; NOT cron-instruction rows (those are for parked
  resumes).
- Dedup: content-derived effects key
  `EffectKey("intake_ticket_goal", repo, ticket_id, sha256(mandate))`
  claimed through the #64 effects ledger. Identical re-arrival = no-op;
  changed body on an existing ticket = goal mandate updated + reset
  active (re-dispatch).
- Goal creation via `employee.Manager.CreateGoal(GoalSpec)` with
  `Source: "trigger"` (tier-2 implicit per goal.go:128-135).
- Completion signal: `GoalHealth.GoalHealthy` — set by GoalLoop reflect
  (internal/employee/goal_loop.go:1074-1080, `goal.Assess(health, now)`
  persisted via `GoalStore.Update`). Write-back subscribes here; park
  signals are `GoalAtRisk`/`GoalBroken` + the episode parker.
- Write-back comments ride `githubsvc.ClientPool` + effects-wrapped
  issue comments (the #64 `github_issue_comment` pattern,
  ProviderIdempotent=false, receipt = {id, html_url}).
- Autonomy (issue item 4): the ticket is the MANDATE, not the
  permission. Goals are created Source=trigger; the #65 authz broker
  still gates every access. No new authority machinery in this tree.

## Interface Contracts

All contracts below are FROZEN. Leaves implement to these exact shapes.

### Contract 1: Poller interface — `internal/intake/poller.go`

```go
package intake

// Ticket is the provider-agnostic intake unit.
type Ticket struct {
    ID         string    // provider ticket id (GitHub issue number as string)
    Repo       string    // "owner/repo"
    Title      string
    Body       string    // becomes the goal mandate
    Labels     []string
    URL        string
    Author     string
    UpdatedAt  time.Time
}

// Poller produces tickets matching the intake filter. Implementations:
// GitHubPoller (this tree); Jira/Linear are follow-ons — this interface
// is the seam.
type Poller interface {
    // Poll returns new-or-changed tickets since the last poll watermark.
    // Implementations track their own watermark; a first poll returns
    // all currently-matching tickets.
    Poll(ctx context.Context) ([]Ticket, error)
}

// Filter is the label gate: only tickets carrying ALL required labels
// (case-insensitive) are returned.
type Filter struct {
    RequiredLabels []string // e.g. {"factory"}; empty = no gate
}
```

Owner: 01. Consumers: 02, 03.

### Contract 2: Ticket->Goal linkage — `internal/intake/linkage.go`

```go
package intake

// LinkRecord is the durable ticket->goal mapping (stored via
// Goal.TriggerRef as "intake:<repo>#<ticket_id>" — no new table; the
// goal store is the source of truth, scanned by TriggerRef prefix).
const TriggerRefPrefix = "intake:"

// IntakeKey is the effects dedup key composition (Contract 4 of #64's
// effects package applies; effect tool name "intake_ticket_goal"):
//   EffectKey("intake_ticket_goal", repo, ticketID, sha256(body))
func IntakeKey(repo, ticketID, body string) string

// Linker binds tickets to goals.
type Linker struct { /* manager + pool + ledger + logger */ }

func NewLinker(mgr *employee.Manager, ledger effects.Ledger, logger *slog.Logger) *Linker

// EnsureGoal: create-or-update the goal for a ticket.
//   - No goal for this TriggerRef: claim IntakeKey; if granted,
//     CreateGoal(GoalSpec{EmployeeID: employeeID, Title: title,
//     Mandate: body, Source: "trigger",
//     TriggerRef: "intake:<repo>#<id>", Gate: gateSpec}) and Complete
//     the effect (the goal row IS the receipt — idempotency by
//     TriggerRef scan, not by effects receipt).
//   - Existing goal, identical body hash: no-op (no new claim — the
//     hash is part of the key; IntakeKey(oldBody)==IntakeKey(newBody)).
//   - Existing goal, changed body (different hash): update mandate +
//     reset state active + health unknown via GoalStore.Update
//     (re-dispatch). No effects claim (the goal exists; this is an
//     update, not an external effect).
func (l *Linker) EnsureGoal(ctx context.Context, t Ticket, employeeID string, gate *employee.GoalGateSpec) (*employee.Goal, error)
```

Owner: 02. Consumers: 03, 04.

### Contract 3: Outcome write-back — `internal/intake/writeback.go`

```go
package intake

// Outcome is the goal-state fact surfaced on the ticket.
type Outcome struct {
    GoalID    string
    State     string // "healthy" | "at_risk" | "broken" | "parked"
    Detail    string // one-line human summary (LLM output head, <=280 chars)
}

// WriteBack comments outcomes on tickets via the githubsvc pool.
// Effects-wrapped per comment:
//   EffectKey("intake_writeback", repo, ticketID, outcomeKey)
// where outcomeKey = state + "-" + goalID — one comment per (goal,
// state) transition, never a comment storm on repeated polls.
type WriteBack struct { /* pool + ledger + defaults */ }

func NewWriteBack(pool *githubsvc.ClientPool, ledger effects.Ledger,
    defaults githubsvc.OwnerRepo, logger *slog.Logger) *WriteBack

// Report posts the outcome comment (label-based completion: also adds
// the "factory:done" label on State=="healthy" — labels, never close;
// closing stays a human decision). Returns the comment id/url receipt.
func (w *WriteBack) Report(ctx context.Context, t Ticket, o Outcome) (commentID int64, url string, err error)
```

Owner: 03. Consumers: 04.

### Contract 4: Config — `[intake]` block in `internal/config/schema.go`

```go
type IntakeConfig struct {
    Enabled        bool   `json:"enabled"`          // default false (opt-in)
    DefaultEmployee string `json:"default_employee"` // REQUIRED when enabled
    PollInterval   string `json:"poll_interval"`    // Go duration string, default "5m"
    WatchRepo      string `json:"watch_repo"`       // "owner/repo" (required when enabled)
    RequiredLabels []string `json:"required_labels"` // default ["factory"]
    DoneLabel      string `json:"done_label"`       // default "factory:done"
    GateCommand    string `json:"gate_command"`     // optional quality gate for created goals
}
// + Intake IntakeConfig `json:"intake"` on the root config struct
```

NOTE (duration-loader gotcha, proven in the llm-resilience forest): the
json5 loader rewrites quoted duration-looking values to ns integers.
PollInterval must either be exempted in the loader's key-aware rewrite
set (preferred, follow the `queue.interactive_window` precedent) or
typed as int-nanos. The leaf MUST pin the round-trip with a regression
test covering BOTH sides. Owner: 04. Consumers: 03, 05.

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-gate-tests.md | leaf (test gate) | none | 45K | gate |
| 01 | 01-poller.md | leaf | none (after gate) | 50K | A |
| 02 | 02-linkage.md | leaf | 01 | 60K | B |
| 03 | 03-writeback.md | leaf | 01, 02 | 55K | B |
| 04 | 04-config.md | leaf | 02, 03 | 45K | C |
| 05 | 05-wiring-e2e.md | leaf | 01, 02, 03, 04 | 65K | C |

**Concurrency groups:** gate first; A alone; B = {02, 03} in parallel;
C = {04, 05} in parallel after B.

## Dispatch Protocol

For each concurrency group, in dependency order:

### Phase 0: Test Gate (MANDATORY — before ANY implementation dispatch)

1. Dispatch `00T-gate-tests.md` via `delegate_task`:
   - Goal: "Write ALL unit + gate tests from 00T-gate-tests.md — tests
     only, no implementation."
   - Context: full 00T text + all Interface Contracts INLINED + coding
     conventions below.
   - Include: "Write tests ONLY. Do NOT write or modify implementation
     files. Tests must fail RED for missing-implementation reasons —
     run once to confirm. Do NOT commit. Do NOT run git add."
2. Control-agent validation (IN-SESSION, main model — never delegated):
   run the tests, confirm compile + RED-for-the-right-reason, map every
   test to a Test Gate matrix row, apply the delta protocol (update the
   LESSER artifact to reflect the GREATER).
3. Commit gate tests with explicit paths (stage only what the gate
   touched — never `git add .`). NEW test files must be added by
   explicit filename, not by directory (tracked-path adds miss them —
   proven: github_readonly_test.go initially left uncommitted).
4. Tracking table: 00T -> TESTS_VALIDATED. Only then dispatch Phase 1.

### Phase 1: Dispatch Concurrency Group A

1. Read 01-poller.md, dispatch via `delegate_task`:
   - Context: full leaf text + Contracts 1, 2, 4 + conventions +
     "Gate tests already exist and are committed. Make them pass. Do
     NOT delete, skip, weaken, or rewrite any gate test; report
     suspected-wrong tests instead."
   - Include: "Do NOT commit. Do NOT run git add. Do NOT use read_file
     on existing source files — use search_files or terminal cat. After
     writing a file, do NOT read it back."

### Phase 2: Dispatch Concurrency Group B (parallel batch)

Dispatch 02-linkage.md and 03-writeback.md simultaneously (same
dispatch-context boilerplate as Phase 1, with Contracts 1-4 inlined).
Each dispatch context adds: "01 is implemented and committed;
internal/intake exists with Ticket/Filter/Poller per Contract 1."

### Phase 3: Dispatch Concurrency Group C (parallel batch)

Dispatch 04-config.md and 05-wiring-e2e.md with the same boilerplate.
05 gets the note: "all components exist; wire them, do not
re-implement. 04 lands the config schema your overlay uses — coordinate
through the committed schema, not through re-defining it."

### Phase 4: Review and Commit Each Child

- Review IN-SESSION (main model, never a delegated reviewer — children
  inherit delegation.model). Read changed files, check against leaf
  spec + contracts + Review Checklist below, run
  `go build ./... && go test -p 2 ./internal/intake/... ./internal/tools/builtin/... -race`.
- Gaps -> re-dispatch with specific feedback (max 3 cycles).
- Pass -> stage the leaf's EXACT file list — `git add` each NEW file
  BY NAME (untracked files are invisible to tracked-path staging) —
  commit `feat(intake): implement <leaf name>`, update tracking
  table -> REVIEWED.

### Phase 5: Integration Review

After all children REVIEWED:
1. `go build -o bin/meept-daemon ./cmd/meept-daemon && go vet ./internal/intake/... ./internal/config/...`
2. `go test -p 2 ./internal/intake/... ./internal/employee/... -race`
3. `make e2e-fast-area AREA=github-intake` (after 05 lands; manifest
   entry required — pre-commit check [18/18] blocks the package without
   it)
4. `python3 scripts/audit-secrets-value-callers.py` green (the poller
   rides the githubsvc pool; it must NOT call Broker.Value itself)
5. gofmt all changed files; corruption check:
   `grep -rcE '^\s+[0-9]+\|' --include='*.go' internal/intake/` returns zero
6. Commit integration fixes (if any); all children -> COMPLETE; report COMPLETE.

## Review Checklist

- [ ] All tasks from the leaf document implemented
- [ ] Interface contracts satisfied exactly (signatures, types, keys)
- [ ] All specified files at exact paths
- [ ] Committed gate tests pass GREEN; `git diff` shows no gate test
      modified, weakened, skipped, or deleted (any such diff = NEEDS_FIXES)
- [ ] Tests written and passing (TDD)
- [ ] Dedup keys byte-identical to Contract 2; write-back keys to Contract 3
- [ ] No secrets logged at info level; poller uses the githubsvc pool,
      never touches secrets.Broker directly (audit script enforced)
- [ ] No scope creep; no debug artifacts (no fmt.Println, TODOs, placeholders,
      commented-out code)
- [ ] No line-number corruption (`     N|` prefixes)
- [ ] Coding conventions followed (below)

## Coding Conventions

- **Language:** Go 1.26 (toolchain pinned in go.mod; do not bump).
- **Naming:** exported PascalCase, unexported camelCase.
- **Imports:** stdlib / third-party / module-local groups; no unused imports.
- **Error handling:** wrap with `%w`; sentinels exported where callers must
  `errors.Is`; never panic in library code; no ignored errors (pre-commit
  enforced).
- **Testing:** stdlib + repo's existing test style; table-driven where
  inputs vary.
- **Formatting:** gofmt before reporting completion.
- **No `time.Now().UnixNano()`/`math/rand` for IDs — `pkg/id.Generate()`
  (predid analyzer enforces).**
- **Tests hermetic:** no network in unit tests; the fake GitHub server is
  httptest behind the githubsvc pool's BaseURL (same pattern as
  internal/tools/builtin/github_*_test.go).
- **Duration config fields:** check the json5 loader rewrite pass before
  freezing the field type (Contract 4 note).

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-gate-tests.md | PENDING | 0 | |
| 01-poller.md | PENDING | 0 | |
| 02-linkage.md | PENDING | 0 | |
| 03-writeback.md | PENDING | 0 | |
| 04-config.md | PENDING | 0 | |
| 05-wiring-e2e.md | PENDING | 0 | |

## Integration Test Plan

1. Cross-contract: label-gated ticket -> goal created (Source=trigger,
   TriggerRef `intake:<repo>#<id>`) in one hermetic e2e scenario (05).
2. Dedup: identical re-poll (same ticket, same body) creates NO second
   goal and makes NO second effects claim (assert ledger key count).
3. Re-dispatch: changed issue body on a linked ticket updates the
   existing goal's mandate and resets it active; no duplicate goal.
4. Write-back: goal reflect -> GoalHealthy -> one comment + done label
   on the ticket; a second healthy reflect adds NO second comment
   (write-back key includes goalID+state).
5. Disabled by default: no [intake] block -> no poller scheduled, no
   intake surfaces.
6. Full suite: `go test -p 2 ./... -short` green;
   `make e2e-fast-area AREA=github-intake` green.

## Test Gate

Test leaf: 00T-gate-tests.md — dispatched in Phase 0, validated by the
control agent in-session. One gate leaf for the whole tree (same
deviation rationale as github-pr-ci: contracts are shared across three
packages; splitting would create cross-gate import cycles in RED state).

| # | Feature (from plan) | Source (leaf/spec §) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------|-----------|-----------|--------|
| F1 | Poller returns label-gated tickets; watermark advances | 01 Tasks 1-2 | TestGitHubPoller_LabelGate, TestGitHubPoller_Watermark, TestFilter_Matches | e2e intake scenario (05) | PENDING |
| F2 | EnsureGoal creates goal Source=trigger with TriggerRef | 02 Task 1 | TestLinker_EnsureGoal_Creates | e2e create scenario (05) | PENDING |
| F3 | Identical re-arrival is a no-op (dedup key) | 02 Task 2 | TestLinker_Dedup_IdenticalBody, TestIntakeKey_Stable | e2e dedup scenario (05) | PENDING |
| F4 | Changed body re-dispatches (mandate update, no duplicate) | 02 Task 3 | TestLinker_Redispatch_OnBodyChange | e2e re-dispatch scenario (05) | PENDING |
| F5 | WriteBack posts effects-wrapped outcome comment + done label on healthy | 03 Tasks 1-2 | TestWriteBack_Report_Comment, TestWriteBack_HealthyAddsLabel, TestWriteBack_NoStorm | e2e write-back scenario (05) | PENDING |
| F6 | [intake] config parses; duration round-trip survives the json5 loader | 04 Task 1 | TestIntakeConfig_Parse, TestIntakeConfig_DurationRoundTrip | n/a (config) | PENDING |
| F7 | Wiring: poller scheduled only when enabled; disabled default | 05 Task 1 | TestIntakeWiring_Enabled, TestIntakeWiring_DisabledDefault | e2e disabled scenario (05) | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED
Delta log:
- (none yet)

## Structural Completeness Check (Before Dispatch)

After writing every document in this tree, run:

    python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans/github-intake-factory --strict-leaves

Re-run until `ALL TREES COMPLIANT: True`, THEN commit the tree.

## Open Questions

Defaults applied without a user decision (pre-authoring Q&A issued
2026-10-26; no objections returned — recommendations stand as recorded).
Listed for review before dispatch; each notes the impact if reversed:

1. Poll scheduling rides `employeeSchedulerAdapter.RunAtInterval`
   (default 5m, `[intake].poll_interval`), NOT cron-instruction rows
   (those are parked-resume machinery) and NOT a bespoke goroutine.
   Impact if reversed: new job lifecycle + a second firing mechanism.
2. Goal routing: `[intake].default_employee` is REQUIRED when enabled.
   Impact if reversed (label routing): more config surface + failure
   modes; poller gains a label->employee map.
3. Completion write-back is label-based only (`factory:done` + outcome
   comment); tickets are NEVER closed by the agent. Impact if reversed
   (auto-close): an irreversible external effect outside the current
   effects gates — would need the #65 broker first.
4. TUI/GUI intake views are DEFERRED to a follow-up issue (wiring
   requirement exception, documented); v1 ships pipeline + CLI
   `meept intake status` (report-only, doctor convention). Impact:
   parity debt tracked openly instead of doubling this tree.
5. Re-arrival with a changed body updates the existing goal's mandate
   and resets it active (re-dispatch), keyed by (ticket_id,
   mandate-hash) so identical re-arrivals are no-ops. Impact if
   reversed (retire-and-create): cleaner audit rows, loses goal
   continuity.

## Notes

- e2e policy (AGENTS.md): new-feature tests go in the hermetic e2e tier
  — 05 adds `e2e/suites/github-intake/` + the `e2e/manifest.json` path
  entry in the same leaf. Pre-commit check [18/18] blocks the package
  without it.
- Reuse, do not re-implement: the githubsvc client pool + token sources
  (#64, `internal/githubsvc`), the effects ledger protocol
  (`internal/effects`), and `pkg/id.Generate` for any new IDs.
- The poller must NOT read `secrets.Broker` directly — it rides the
  pool; `scripts/audit-secrets-value-callers.py` (lint-ci) enforces.
- known gotcha: json5 duration rewrite (Contract 4 note) — regression
  test both sides.
- macOS full-sweep test runs MUST use `-p 2` (port exhaustion — AGENTS.md).
- Issue #67 non-goals are binding: no email/webhook intake; no
  human-approval step for ticket->goal mapping in v1 (broker-owned).
