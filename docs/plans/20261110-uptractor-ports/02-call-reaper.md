# Call Reaper — Implementation Leaf (02)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks using TDD. The failing gate tests ALREADY EXIST and are
> committed — make them pass; do NOT delete, skip, weaken, or rewrite any gate
> test. Do NOT commit. Do NOT use read_file on existing source files — explore
> with search_files or terminal cat. After writing a file, do NOT read it back.

## Meta

- **Parent:** ./master.md
- **Scope:** C3 reaper goroutine in the voice Manager: probe lost hangups, force-end dead/overlong calls through releaseOnce, emit CallEventErrored.
- **Dependencies:** leaf 01 (GetCallStatus) — COMPLETE required
- **Estimated Context:** 85K
- **Concurrency Group:** B (serializes with 06 on manager.go)

## Goal

Close meept's live-process lost-hangup gap: SweepStaleLive only fixes strands
after a daemon restart; a call dead carrier-side while the daemon is alive
stays tracked forever today. The reaper periodically asks Telnyx whether each
long-lived phone session is alive; dead calls get a synthetic call.hangup
through the EXISTING releaseOnce funnel (never a second teardown path — frozen
20261109), and a CallEventErrored rides the CallLeg bus so the fault is
observable (reason strings frozen: "reaped:hangup-lost", "reaped:max-duration").

Port source: ~/git/uptractor-jobfile branch fix/coordinator-missed-hangup-watchdog,
phone/core/internal/callsvc/reaper.go — cat it; structure and defaults
(Interval 30s, MinAge 60s, MaxDuration 30m) are frozen (master C3).

## Context

- internal/voice/manager.go:218 releaseOnce — the single terminal funnel; :271 fireRelease; :539 FailOutcome (carries custom reasons — the reaper's release entry; do NOT use HandleCallEvent whose hangup branch hard-codes reason "call.hangup" at :417)
- internal/voice/callleg.go — CallEventErrored kind; phone adapter observer lanes are additive-only; AddAnsweredObserver/AddTerminalObserver lanes exist for the reaper's own tracking map
- internal/voice/telnyx/callstatus.go (from leaf 01) — CallStatusChecker dependency
- Manager's per-ccid maps (calls/stream/outbound/done/spliceErrored, manager.go ~72-100) carry NO timestamps and expose no session enumeration — the reaper therefore maintains its OWN {ccid: startedAt} map fed by the answered/terminal observer lanes (no Manager state or constructor change; matches the frozen-funnel doctrine)
- Only PHONE-leg sessions are probed (local/signal legs have no Telnyx authority); MaxDuration applies to phone sessions only in v1

## Verified Anchors

- internal/voice/manager.go:218 — `func (m *Manager) releaseOnce` — funnel the reaper feeds (via FailOutcome :539)
- internal/voice/manager.go:539 — `func (m *Manager) FailOutcome` — reaper release entry, carries the frozen reason strings
- internal/voice/callleg.go — CallEventKind enum (CallEventErrored pinned)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/voice
type CallStatusChecker interface {
    GetCallStatus(ctx context.Context, callControlID string) (telnyx.CallStatus, error)
}
type CallErrorLogger interface{ LogCallError(callControlID, message string) error } // optional; callstore implements
type ReaperConfig struct {
    Status      CallStatusChecker // required; nil => Error log, no goroutine
    Errors      CallErrorLogger   // optional
    Interval    time.Duration     // default 30s
    DeadAir     time.Duration     // default 60s — release only after this much caller silence
    MaxDuration time.Duration     // default 30m
}
func (m *Manager) RunReaper(ctx context.Context, cfg ReaperConfig)
```

DEAD-AIR SEMANTICS (owner-ratified 2026-11-11): the reaper does NOT reap by
call age. Its {ccid: lastActivity} map seeds at the answered observer and
REFRESHES on every caller-side audio/turn event; a session becomes reaper-
eligible only when now-lastActivity > DeadAir. MaxDuration stays an absolute
cap from call start.

### What This Leaf Consumes

- C1 telnyx.GetCallStatus (leaf 01) — *telnyx.Client satisfies CallStatusChecker
- C3 (master.md). Gate tests: internal/voice/reaper_test.go.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---|---|---|---|---|---|---|
| T1 | ReaperConfig + defaults + RunReaper loop (ticker, ctx cancel; own {ccid: lastActivity} map seeded at answered observer and REFRESHED on every caller-side audio/turn event; sweep copies the map then probes serially — never hold locks across HTTP) | Create internal/voice/reaper.go | `go test ./internal/voice -run Reaper` | gate RED | green | Given fake Status returns Alive=false for ccid X whose lastActivity is older than DeadAir; When one sweep; Then X released exactly once |
| T2 | Release via FailOutcome(ccid, outcome, "reaped:hangup-lost") — same releaseOnce funnel, custom reason rides; errored bus event on the leg; single release even if a real call.hangup lands concurrently (releaseOnce dedupes) | Modify internal/voice/reaper.go | `go test ./internal/voice -run Reaper` | gate RED | green | Given dead call reaped; When release fires; Then CallEventErrored reason "reaped:hangup-lost" on leg Events |
| T3 | MaxDuration force-hangup (carrier HangupCall best-effort, then release, reason "reaped:max-duration"); LogCallError when Errors set | Modify internal/voice/reaper.go | `go test ./internal/voice -run Reaper` | gate RED | green | Given session age > MaxDuration and Alive=true; When sweep; Then hung up + errored event |
| T4 | Wire: voice_wiring starts RunReaper for phone leg when telnyx configured; config keys voice.reaper_interval_s / reaper_dead_air_s / reaper_max_call_minutes (0 => defaults; negative disables) | Modify internal/daemon/voice_wiring.go; internal/config | `go build ./... && go test ./internal/daemon -run Voice` | n/a | green | Given daemon boots with telnyx; When startup; Then log "voice reaper started" with resolved values |
| T5 | callstore.LogCallError: append call_error row (additive table CREATE IF NOT EXISTS call_errors(ccid, at, message)) | Modify internal/voice/callstore.go | `go test ./internal/voice -run CallError` | gate RED | green | Given Errors logger set; When reap; Then row exists |

## Self-Verification Checklist

- [ ] `go test -p 2 ./internal/voice` fully green (not just -run Reaper)
- [ ] releaseOnce untouched in behavior; reaper is an input edge only (FailOutcome path)
- [ ] CallController interface NOT extended (GetCallStatus lives on *Client + standalone CallStatusChecker only)
- [ ] No Manager state/constructor change (reaper's own observer-fed map)
- [ ] Defaults exactly 30s/60s/30m; frozen reason strings exact; DEAD-AIR semantics (age alone never reaps; caller activity refreshes lastActivity)
- [ ] No lock held across an HTTP probe (snapshot-and-release pattern)
- [ ] gofmt + staticcheck clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list with rationale

## Review Checklist (For Review Agent)

- [ ] Single-terminal-funnel invariant holds (grep for any second cancel/teardown path in reaper.go; releases go through FailOutcome)
- [ ] Only phone sessions probed; local/signal untouched
- [ ] Reaper map entries removed on terminal observer (no unbounded growth)
- [ ] Probe errors (network) never kill the loop; logged and retried next tick
- [ ] Exactly-once: concurrent hangup webhook + reaper race releases once (gate test covers)
- [ ] No debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

- Uptractor reaper probes serially with a per-call timeout; keep that — a
  parallel probe fan-out adds races for no benefit at single-digit call counts.
- FailOutcome on an already-released ccid is a no-op via releaseOnce — the
  reaper never needs its own latching.
