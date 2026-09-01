# Generalized Turn Parker - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** TurnParker — the class-agnostic generalization of
  QuotaResumeWatcher (record, watcher loop, resume callback, MaxWait
  soft-stop); QuotaResumeWatcher becomes a delegating wrapper with
  byte-identical chat-quota behavior.
- **Dependencies:** tree 02 leaf 01 (FailureClass/PolicyVerdict types only)
- **Estimated Context:** 65K
- **Concurrency Group:** A
- **Decision references:** D9

## Goal

internal/agent/quota_resume.go works but is quota-specific: QuotaParkedTurn,
reset-time scheduling, quota-only resume. This leaf extracts the machine:

1. `ParkedTurnRecord` (master Contract 1; NOT `ParkedTurn` — that name is
   taken by budget_resume.go's budget watcher) — carries Class + generic TurnPayload
   instead of quota-specific fields.
2. `TurnParker` — same watcher semantics: ticker drain, resume at
   min(ResumeAt, now+MaxWait), Stop(), Pending(); PLUS per-class Next()
   queries (leaf 04's surfaces consume them).
3. `QuotaResumeWatcher` re-expressed as a thin adapter: Park() builds a
   Class=quota ParkedTurnRecord; every existing quota_resume test passes
   UNMODIFIED.

Persistence: mirror QuotaResumeWatcher exactly (investigate: if it is
memory-only, TurnParker is memory-only; STATE the finding in Deviations —
do not invent persistence in this leaf).

## Context

Key files:
- `internal/agent/quota_resume.go` — QuotaResumeWatcher (line 47),
  QuotaParkedTurn, Park (line 150), drainDue (line 208), Start/Stop,
  SetPollInterval, MaxWait soft-stop (lines 40-42), llm.DefaultQuotaMaxWait
  fallback (line 65). This is the extraction template.
- `internal/llm/failure_policy.go` — FailureClass (tree 02 leaf 01).
- The resume callback signature: quota's resumeFunc(ctx, turn) — the
  generalized callback receives ParkedTurnRecord and lets each consumer route
  it (leaf 02/03 own routing).

Design constraint: NO behavior change for existing quota parking. The
wrapper must produce identical scheduling decisions. If extraction
changes an edge case (ordering of equal ResumeAt, drain ticks), fix the
generalization, not the tests.

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly SHARED-CONVENTIONS §4.5 / master Contract 1:

```go
// internal/agent/parked_turn.go (frozen; SHARED-CONVENTIONS §4.5 —
// the type is ParkedTurnRecord there; package agent already has a
// ParkedTurn in budget_resume.go)
type ParkedTurnRecord struct {
    ConversationID string
    SessionID      string
    AgentID        string
    Class          llm.FailureClass
    ResumeAt       time.Time
    Attempt        int
    MaxAttempts    int
    TurnPayload    json.RawMessage
}
type TurnParker struct{ /* config: maxWait, pollInterval, now func, resume func */ }
func NewTurnParker(logger *slog.Logger, resume func(context.Context, ParkedTurnRecord), maxWait time.Duration) *TurnParker
func (p *TurnParker) Park(turn ParkedTurnRecord) bool
func (p *TurnParker) Pending() int
func (p *TurnParker) Next(class llm.FailureClass) (time.Time, bool)
func (p *TurnParker) Start(ctx context.Context)
func (p *TurnParker) Stop()
```

### What This Leaf Consumes

```go
// tree 02 leaf 01
llm.FailureClass (FailureQuota, FailureThrottle)
```

## Tasks

### Task 1: ParkedTurnRecord + TurnParker core

**Objective:** The generalized machine, clock-injected.

**Files:**
- Create: `internal/agent/parked_turn.go`
- Test: `internal/agent/parked_turn_test.go`

**Step 1:** Failing tests (injected now + captured resume calls): park →
resume fires at ResumeAt when soon; MaxWait soft-stop (ResumeAt beyond
MaxWait → scheduled at now+MaxWait, Park returns true); Park returns
false when even MaxWait scheduling is impossible (mirroring quota's
existing false semantics — check Park's current contract at
quota_resume.go:150 and mirror EXACTLY); Pending counts; Next(class)
returns earliest per class; Stop halts; resume callback errors do not
crash the watcher (log + drop or re-park per current quota behavior —
mirror it).
**Step 2:** FAIL. **Step 3:** Implement by MOVING quota_resume.go's
logic (not copying) into TurnParker. **Step 4:** PASS.

### Task 2: QuotaResumeWatcher wrapper

**Objective:** Legacy surface delegating to TurnParker.

**Files:**
- Modify: `internal/agent/quota_resume.go` (bodies become delegates)
- Test: `internal/agent/quota_resume_test.go` — run UNMODIFIED.

**Step 1:** Confirm existing tests currently pass (`go test
./internal/agent/ -run TestQuotaResume -count=1`) BEFORE touching
anything; record count. **Step 2:** Rewire QuotaResumeWatcher to hold a
TurnParker; QuotaParkedTurn ↔ ParkedTurn conversion helpers; Park/build
Class=quota. **Step 3:** `go test ./internal/agent/ -run TestQuotaResume
-count=1` — same count, all green, ZERO test edits. **Step 4:** Any test
needing an edit = Deviations entry + justification.

### Task 3: Wiring swap

**Objective:** Daemon constructs TurnParker; the quota watcher wraps it.

**Files:**
- Modify: the components site constructing QuotaResumeWatcher
  (search_files for NewQuotaResumeWatcher in internal/daemon/).
- Test: compile + `go build ./...`; existing integration tests.

**Verify:** one TurnParker instance serves the daemon; the watcher holds
a reference (not a second instance). `make analyzers` on
internal/agent/.

## Self-Verification Checklist

- [ ] Legacy quota tests green with ZERO modifications
- [ ] Injected clock throughout; no sleep >100ms in tests
- [ ] Next(class) semantics tested for both classes
- [ ] Mutex regime matches the original watcher (no new lock types)
- [ ] Persistence finding stated in Deviations
- [ ] gofmt/vet/analyzers clean

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; legacy tests untouched and green
- [ ] Contracts match master Contract 1 exactly
- [ ] No quota-specific fields leaked into ParkedTurnRecord
- [ ] Single TurnParker instance in daemon wiring

Output: APPROVED or specific gaps with file + line references.

## Notes

- drainDue ordering: if the original iterates a map (nondeterministic
  order), keep that; do not "improve" ordering — byte-identical means
  byte-identical.
- The `llm.DefaultQuotaMaxWait` fallback applies to the generalized
  MaxWait too (same default source, now per-parker).
