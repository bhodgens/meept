# Outbound dial - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The failing gate test ALREADY EXISTS
> (parked .staged from leaf 00T T6) - make it pass; do NOT modify gate
> tests. Do NOT commit. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. Write once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** C9 outbound dial: policy gates, effects reservation, audit-before-dial, DialOut, TrackOutbound, terminal-outcome release (hangup / no-answer timeout / streaming.failed)
- **Dependencies:** leaf 05 (Manager)
- **Estimated Context:** 75K
- **Concurrency Group:** C
- **Files this leaf owns:** internal/voice/dial.go

## Goal

The employee-callable dial path. Order is load-bearing: policy checks ->
audit row -> effects reserve -> POST /calls -> TrackOutbound. Any failure
before the POST leaves no side effect; any failure after completes the
effect with its outcome. A retry of the same request_id never rings twice.

## Context

Borrow flow from uptractor core/cmd/coordinator/main.go dialOutbound
(pre-dial refusals, event-before-dial, TrackOutbound, WaitCall block) but
as a daemon-side function with an injected policy set, NOT a CLI. The
uptractor trust ban (human-only dialing) is deliberately replaced: the
phone employee may dial, gated by allowlist + effects + audit (spec
"Borrowed from uptractor / Changed").
Policy gates, ALL before any HTTP: voice.enabled; target E.164 in
voice.allowed_numbers (exact match after normalization); public_stream_url
non-empty; connection_id non-empty; registry slot free (C8 Acquire); no
live call to target (Acquire's ErrDuplicateTarget). Each refusal returns a
typed error naming the reason (ErrVoiceDisabled, ErrNumberNotAllowed,
ErrNoPublicURL, ErrNoConnectionID, ErrBusy, ErrDuplicateTarget) - the
employee surfaces the reason, never a bare "dial failed".
Effects: internal/effects ledger, key (employee_id, target, request_id);
reserve BEFORE DialOut; complete with outcome on every terminal path.
Read internal/effects for the Reserve/Complete API before writing (search,
do not guess signatures).
Audit: internal/auditlog row (direction outgoing, target, employee_id,
request_id) written BEFORE DialOut so the attempt is attributable even if
the POST fails. Narrow interface AuditWriter injected (no auditlog import
if it cycles; check).
No-answer: timeout_secs (default 30) starts a timer at DialOut success;
if call.answered never arrives, Manager.Fail(ccid, "no_answer") path runs
-> release hook -> effect completed no_answer -> registry released. The
timer is cancelled on answered.
Dial result: returns ccid + a wait channel (Manager.WaitCall) so the
employee round can park until the call ends (leaf 12 wires parking).

## Verified Anchors

- ~/git/aiedge-uptractor/core/cmd/coordinator/main.go:288-326 - dialOutbound (flow shape)
- internal/voice/manager.go (leaf 05) - TrackOutbound, WaitCall, Fail, SetReleaseHook
- internal/effects - idempotency ledger API (read before use)
- internal/auditlog - canonical row API (read before use)
- master.md C8/C9

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
type DialRequest struct{ EmployeeID, Target, RequestID string }
type DialResult struct{ CallControlID string; Done <-chan struct{} }
type Dialer struct{ ... }
func NewDialer(cfg DialConfig, tel telnyx.CallController, mgr *Manager, reg Registry, fx EffectsLedger, audit AuditWriter, logger *slog.Logger) *Dialer
func (d *Dialer) Dial(ctx context.Context, req DialRequest) (DialResult, error)
// typed refusal errors: ErrVoiceDisabled, ErrNumberNotAllowed, ErrNoPublicURL, ErrNoConnectionID (+ registry ErrBusy/ErrDuplicateTarget passthrough)
type DialConfig struct{ Enabled bool; AllowedNumbers []string; PublicStreamURL, ConnectionID, FromNumber string; TimeoutSecs int }
```

### What This Leaf Consumes

- C4 DialOut (leaf 02); C5 TrackOutbound/WaitCall/Fail (leaf 05); C8 Registry; C9 effects+audit shapes

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Activate gate test; confirm RED | mv internal/voice/dial_test.go.staged -> dial_test.go | go test -p 2 ./internal/voice/ -run TestDial | undefined: NewDialer | RED confirmed | - |
| T2 | Policy gates: each refusal BEFORE any HTTP (fake telnyx asserts zero requests) with typed error | Create: internal/voice/dial.go | go test -p 2 ./internal/voice/ -run TestDialRefusals | - | PASS | Given empty connection_id; When Dial; Then ErrNoConnectionID, zero POSTs |
| T3 | Ordering: audit row before reserve before POST; failure at each stage completes/cleans the prior (no leaked reservation on POST failure) | (same) | go test -p 2 ./internal/voice/ -run TestDialOrdering | - | PASS | Given POST fails; When Dial; Then effect completed failed, audit row exists |
| T4 | Idempotency: same request_id twice -> one DialOut (second returns reserved result or ErrDuplicate per effects API semantics - read the ledger contract and pin it) | (same) | go test -p 2 ./internal/voice/ -run TestDialEffectsIdempotent | - | PASS | Given reserved request_id; When re-Dial; Then zero additional POSTs |
| T5 | No-answer release: answered never arrives -> timer fires at TimeoutSecs -> Fail(no_answer) -> registry released + effect completed | (same) | go test -p 2 ./internal/voice/ -run TestOutboundTimeoutRelease | - | PASS | Given dial with fake clock; When timeout elapses; Then slot free, outcome no_answer |
| T6 | Number normalization: +1XXXXXXXXXX vs XXXXXXXXXX variants match the allowlist entry (E.164 normalize before compare) | (same) | go test -p 2 ./internal/voice/ -run TestDialNormalization | - | PASS | Given allowlist +15551234567; When target 5551234567 with US default; Then allowed (or refused - pick one rule, document, pin) |
| T7 | Format/vet/race | - | gofmt -l internal/voice; go vet ./internal/voice/; go test -p 2 -race ./internal/voice/ | - | clean | - |

## Self-Verification Checklist

- [ ] Gate tests passing untouched
- [ ] No path dials without audit row + effect reservation (grep DialOut call sites: exactly one, inside the ordered block)
- [ ] Typed errors for every refusal; no bare fmt.Errorf at gates
- [ ] Timer cancelled on answered (no late Fail on a live call)
- [ ] ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] C9 ordering proven by tests that would catch a swap (audit-after-dial mutation goes RED)
- [ ] Effects API used per its real contract (read internal/effects; no invented signatures)
- [ ] WaitCall channel leaks: completed calls' channels cleaned or documented as bounded
- [ ] Registry Acquire/Release balanced on every terminal path

Output: APPROVED or specific gaps.

## Notes

Normalization rule choice (T6): default region US for bare 10-digit is
acceptable IF documented and pinned; refusing non-E.164 targets outright is
the safer alternative - pick one, the gate test pins whichever you choose.
