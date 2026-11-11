# Call Brief + Commitments to Ledger — Implementation Leaf (04)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks using TDD. The failing gate tests ALREADY EXIST — make
> them pass; do NOT delete, skip, weaken, or rewrite any gate test. Do NOT
> commit. Do NOT use read_file on existing source files — explore with
> search_files or terminal cat. After writing a file, do NOT read it back.

## Meta

- **Parent:** ./master.md
- **Scope:** C4 structured call brief (stub-at-answer, finalize-at-release, idempotent) + C7 commitments filed to the ledger.
- **Dependencies:** 00T only (reaper leaf 02 also emits abandoned outcomes — coordinate via the frozen status enum, no code dependency)
- **Estimated Context:** 95K
- **Concurrency Group:** B

## Goal

Replace meept's one-line transcript summary with the structured call brief
(Uptractor README-callbrief shape, master C4): an LLM-generated record with
summary, action_items[], commitments[], risk_flags[], stored as a stub
(status in_progress) when the call is answered and finalized at release
(completed / failed / abandoned). Brief generation runs post-release, off the
voice hot path, through the employee's existing LLM access; a failed analysis
stores status=failed with the error and is NEVER retried (frozen guarantee:
exactly one brief per call, idempotent across webhook replay AND restart).
Commitments inside a finalized brief are filed as ledger rows via
Store.Append (kind=commitment, source=voice, source_ref=ccid), deduped
exactly-once per (ccid, commitment index). Uptractor-domain fields
(follow_up_sms, crm_note) are NOT typed in meept — they ride the opaque
`ext` JSON that meept-uptractor owns (assumption A2).

## Context

- Source: cat ~/git/uptractor-jobfile (branch fix/coordinator-missed-hangup-watchdog) phone/core/README-callbrief.md + phone/core/internal/agent/callbrief.go + phone/core/internal/jobfile/callbrief.go — idempotency and stub/finalize semantics port from there
- internal/voice/callstore.go:41 voice_calls schema; :92 NewCallStore; :178 SweepStaleLive (startup => brief status abandoned per O1: startup sweep rows get outcome failed + brief abandoned; reaped rows get brief abandoned via leaf 02's reason strings)
- internal/ledger/store.go:316 Append(ctx, DraftRecord)
- Brief prompt: port the analysis prompt shape from phone/core/internal/agent/callbrief.go but keep it GENERIC (no Uptractor jobs/work-orders vocabulary; meept employees supply domain context)

## Verified Anchors

- internal/voice/callstore.go:92 — NewCallStore — migration point for brief+ext columns
- internal/voice/callstore.go:198 — SweepStaleLive (func) — abandoned-brief hook
- internal/ledger/store.go:316 — Append — commitment filing
- internal/voice/manager.go:271 — fireRelease — finalize trigger point

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/voice
type CallBrief struct {
    Status string          `json:"status"` // in_progress|completed|failed|abandoned
    Error  string          `json:"error,omitempty"`
    From   string          `json:"from"`
    To     string          `json:"to"`
    Brief  BriefBody       `json:"brief"`
    Ext    json.RawMessage `json:"ext,omitempty"` // opaque; meept never parses
}
type BriefBody struct {
    Transcript  string   `json:"transcript"`
    Summary     string   `json:"summary"`
    ActionItems []string `json:"action_items"`
    Commitments []string `json:"commitments"`
    RiskFlags   []string `json:"risk_flags"`
}
type BriefAnalyzer interface { // LLM seam; employee stack provides, tests fake
    Analyze(ctx context.Context, transcript string, ccid string) (*BriefBody, error)
}
func (s *CallStore) WriteBriefStub(ctx context.Context, ccid, from, to string) error
func (s *CallStore) FinalizeBrief(ctx context.Context, ccid string, b *CallBrief) error // idempotent
func (s *CallStore) Brief(ctx context.Context, ccid string) (*CallBrief, error)
func (s *CallStore) SetBriefExt(ctx context.Context, ccid string, ext json.RawMessage) error // meept-uptractor entry point
```

Additive schema: `ALTER TABLE voice_calls ADD COLUMN brief TEXT NOT NULL DEFAULT ''` and `... ext TEXT NOT NULL DEFAULT ''`, guarded (ignore duplicate-column error), idempotent across opens.

### What This Leaf Consumes

- C4, C7 (master.md); ledger DraftRecord shape (read internal/ledger/store.go). Gate tests: callbrief_test.go, brief_commitments_test.go.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---|---|---|---|---|---|---|
| T1 | Schema migration + CallBrief types + WriteBriefStub/FinalizeBrief/Brief/SetBriefExt | Modify internal/voice/callstore.go; Create internal/voice/callbrief.go | `go test ./internal/voice -run Brief` | gate RED | green incl. double-open idempotence | Given store opened twice on same file; When second open; Then no migration error |
| T2 | Wire stub at answer + finalize at release; BriefAnalyzer seam on the wiring (employee LLM; nil analyzer => brief stays stub, log warn, no failure) | Modify internal/daemon/voice_wiring.go, internal/voice/manager.go (fireRelease observer, additive) | `go test ./internal/voice && go build ./...` | gate RED | green | Given answered call; When release fires; Then analyzer called once, brief completed |
| T3 | Idempotence: replayed hangup + restart-mid-call => exactly one brief; failed analysis => status=failed stored, never retried | covered by gate tests | `go test ./internal/voice -run Brief` | gate RED | green | Given finalize called twice; When second call; Then no change, no error |
| T4 | Commitments -> ledger: on finalize(completed), for each commitment index i, if no ledger row (source=voice, source_ref=ccid, kind=commitment, payload idx=i) exists, Append it. POSTURE: the ledger-append adapter lives in internal/daemon wiring (internal/voice must not import internal/ledger — existing repo posture); internal/voice exposes only a CommitmentFiler seam interface | Create internal/daemon/brief_commitments.go; Create internal/voice/brief_seam.go (CommitmentFiler interface) | `go test ./internal/voice -run Commitments && go test ./internal/daemon -run BriefCommitments` | gate RED | green, dedupe exact | Given brief with 2 commitments finalized twice; When done; Then exactly 2 ledger rows |
| T5 | SweepStaleLive hook: rows swept at startup get brief status abandoned (only if stub present); leaf-02 reaped calls reach abandoned through the same finalize path via outcome/reason mapping | Modify internal/voice/callstore.go | `go test ./internal/voice` full | gate RED | green | Given live row with stub at startup; When SweepStaleLive; Then brief abandoned |

## Self-Verification Checklist

- [ ] `go test -p 2 ./internal/voice ./internal/ledger` fully green
- [ ] ext never parsed/typed in meept (grep for follow_up_sms => zero hits in Go)
- [ ] Frozen JSON shape matches master C4 byte-for-byte on round-trip test
- [ ] Analyzer runs OFF the voice hot path (post-release goroutine with ctx timeout; never blocks teardown)
- [ ] SQLite discipline: no query while Rows/Tx open

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** 2. **Verify command + output:** last 20 lines 3. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] Exactly-one-brief guarantee holds under replay AND restart AND reaper race
- [ ] Commitment dedupe query+append is transactional or idempotent under crash between them
- [ ] fireRelease funnel unchanged in behavior (observer pattern only)
- [ ] Generic prompt: no Uptractor domain vocabulary
- [ ] No debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

- The brief column stores the FULL C4 JSON (status+envelope), not just BriefBody, so ext and status travel together.
- meept-uptractor will later read brief via the dashboard (read-only SQL) and WRITE ext via SetBriefExt only if it ever gets a write lane (out of scope both repos for now).
