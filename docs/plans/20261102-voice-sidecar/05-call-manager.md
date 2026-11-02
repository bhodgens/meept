# Call manager - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The failing gate tests ALREADY EXIST
> (parked .staged from leaf 00T T3) - make them pass; do NOT modify gate
> tests. Do NOT commit. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. Write once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** internal/voice Manager (C5): call lifecycle keyed by call_control_id - inbound answer, outbound track, streaming start, idempotent media registration, hangup teardown, WaitCall
- **Dependencies:** leaf 02 (telnyx client + CallController interface)
- **Estimated Context:** 75K
- **Concurrency Group:** B
- **Files this leaf owns:** internal/voice/manager.go

## Goal

The per-call state machine. Borrowed from uptractor
core/internal/callsvc/manager.go with one addition: a Release hook so leaf
11/12 can unpark goals and free registry slots on ANY terminal outcome
(hangup, no-answer timeout, streaming.failed).

## Context

Borrow source (terminal cat, outside repo):
- ~/git/aiedge-uptractor/core/internal/callsvc/manager.go - full file.
  Keep: Config.StreamURL + SetStreamURL/StreamURL (mutex-guarded, quick
  tunnel URL changes per run), SessionSpawner, CallController interface,
  maps calls/stream/outbound/done keyed by ccid, HandleCallEvent switch
  (initiated-incoming -> Answer+spawn; answered -> tracked check +
  StartStreaming, NEVER AnswerCall on outbound; hangup -> idempotent
  teardown + close done channel), OnMediaStart idempotent registration,
  TrackOutbound, WaitCall.
Changes vs uptractor:
1. StartStreaming takes the leaf-02 Codec param (L16/16000 default from
   ManagerConfig).
2. New: SetReleaseHook(fn func(ccid string, outcome Outcome)) invoked once
   per call on teardown (hangup, timeout expiry, or explicit Fail(ccid,
   reason) for streaming.failed / no-answer). Hook errors logged, never
   fatal. Outcome is a small enum: answered, no_answer, failed, hangup.
3. Public-URL-empty refusal stays at initiated (inbound) AND is re-checked
   at dial time in leaf 11.

Do not import internal/employee or internal/agent here; the release hook
is the seam (leaf 12/14 inject real behavior).

## Verified Anchors

- ~/git/aiedge-uptractor/core/internal/callsvc/manager.go:90-165 - HandleCallEvent (borrow shape)
- ~/git/aiedge-uptractor/core/internal/callsvc/manager.go:167-218 - OnMediaStart/TrackOutbound/WaitCall (borrow verbatim, module-adapt)
- internal/voice/telnyx (leaf 02) - CallController, Codec

## Interface Contracts (From Parent)

### What This Leaf Exposes

C5 verbatim (master.md) plus:
```
type Outcome string // answered|no_answer|failed|hangup
type ManagerConfig struct{ StreamURL string; Codec telnyx.Codec }
func (m *Manager) SetReleaseHook(fn func(ccid string, outcome Outcome))
func (m *Manager) Fail(ccid string, reason string) // streaming.failed / timeout path -> Release(hook, failed|no_answer)
```

### What This Leaf Consumes

- C4 CallController + Codec (leaf 02)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Activate gate tests; confirm RED | mv internal/voice/manager_test.go.staged -> manager_test.go | go test -p 2 ./internal/voice/ -run TestManager | undefined: NewManager | RED confirmed | - |
| T2 | Borrow manager, module-adapt, add Codec plumbing | Create: internal/voice/manager.go | go build ./internal/voice/ | - | compiles | - |
| T3 | Release hook: fires exactly once per ccid on hangup/Fail; double hangup fires once; unknown ccid no-op | (same) | go test -p 2 -race ./internal/voice/ -run TestManager | - | PASS | Given tracked call; When hangup then duplicate hangup; Then hook called once with outcome hangup |
| T4 | Outbound: answered -> StartStreaming only; fake CallController asserts AnswerCall count == 0 | (same) | go test -p 2 ./internal/voice/ -run TestManagerOutboundNoAnswer | - | PASS | Given TrackOutbound id; When call.answered; Then streaming started, zero answers |
| T5 | Untracked answered refused with named error; empty StreamURL refuses initiated before AnswerCall | (same) | go test -p 2 ./internal/voice/ -run TestManagerRefusals | - | PASS | Given unknown ccid; When answered; Then error names untracked |
| T6 | Format/vet/race | - | gofmt -l internal/voice; go vet ./internal/voice/; go test -p 2 -race ./internal/voice/ | - | clean | - |

## Self-Verification Checklist

- [ ] Gate tests passing untouched
- [ ] No AnswerCall path reachable for outbound (grep before reporting)
- [ ] Release hook idempotent per ccid (-race clean)
- [ ] All map access mutex-guarded
- [ ] ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list vs uptractor source

## Review Checklist (For Review Agent)

- [ ] C5 signatures exact
- [ ] Diff vs uptractor: only the three named changes
- [ ] Teardown idempotency pinned by a test that would catch a double-close panic
- [ ] No employee/agent imports

Output: APPROVED or specific gaps.

## Notes

The done-channel retention trick from uptractor (close once, keep for late
WaitCall) is load-bearing for the dial path blocking until hangup - keep
it verbatim.
