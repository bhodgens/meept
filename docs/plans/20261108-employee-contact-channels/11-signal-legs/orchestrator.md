# Signal legs branch (voice leg + messaging wiring) - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 2 (both leaves)
- **Scope:** Build the Signal voice-call leg (C4) and the Signal messaging channel (C2 attempt implementation) on top of the signal sidecar client (C3).

## Goal

Two channels over one signal-cli subprocess: (1) a full voice leg inside the existing sidecar registry/splice machinery - an employee places or answers a Signal call with the same supervisor-gated speech loop as a Telnyx call; (2) the signal_msg ChannelAttempt so the contact chain (#118) can deliver messages.

## Architecture

The voice leg is a daemon-side media adapter: internal/signalleg bridges signal-cli socket audio <-> the voice sidecar's per-call C2 unix pair (resample to L16 s16le@16k 640B/20ms in the adapter; child chunks NEVER resized - the wire codec sizes only transcode, per internal/voice/AGENTS.md H2). Registry entry uses leg "signal" with targets shaped `signal:<uuid>` (never E.164-normalized, mirroring the local-leg rule). Barge-in: STT-partial (phone posture). Effects + audit row BEFORE StartCall (phone-outbound posture). Messaging: a thin ChannelAttempt over Client.Send with throttle + allowlist; inbound envelopes bind (number=signal:<uuid>, employee) sessions through the existing Binding machinery like other legs.

## Interface Contracts

### C4: Signal voice leg (owned by leaf 11a)

```
// internal/voice/dial.go: LegSignal = "signal" constant; zero value STAYS phone.
// internal/signalleg/leg.go:
type Leg struct{}
func New(deps Deps) *Leg // Deps: *signal.Client, Registry, Manager, TextServer, Binding, allowlist, effects+audit seams
func (l *Leg) Place(ctx context.Context, employeeID, targetUUID, message string) (ccid string, err error)
// Gates BEFORE any side effect: enabled, allowlist, capacity (shared rules,
// dial.go:621-628), effects reservation, audit row. THEN Client.StartCall.
// callEvent CONNECTED -> registry live + talk loop arms (setup-equivalent);
// ENDED -> Manager.FailOutcome fan-out (release, unpark, effects complete).
// Audio: adapter goroutine bridges Client socket audio <-> C2 pair; outbound
// resampled to L16@16k 640B/20ms pacing; inbound chunked likewise.
```

### C2-attempt: signal_msg (owned by leaf 11b)

```
// internal/signalleg/msgchannel.go:
func (l *Leg) MessageAttempt() contact.ChannelAttempt
// Attempt: throttle (token bucket, default few/min), allowlist check,
// Client.Send -> outcome "delivered" on ts>0; send error / rate-limit ->
// outcome "unavailable"/"failed" (privacy-setting rejections are
// channel-unavailable, NOT daemon errors). Voice attempts (signal_voice)
// come from leaf 11a's Place with message carried as first spoken line.
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 11a | 11a-signal-voice-leg.md | leaf | 10 (client), 01 (registry touch) | 45K | B |
| 11b | 11b-signal-msg.md | leaf | 10 (client), 01 (contact seam) | 30K | B |

Same-letter children dispatch together after parents 10 and 01 are REVIEWED. 11a and 11b both touch internal/signalleg/ - 11a creates the package and leg.go; 11b adds msgchannel.go ONLY (file partition avoids same-package races; both depend on types from leg.go, pinned in C4 above).

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate

Parent gate tests (00T, committed at root) already cover F3-F5 e2e (signal-sidecar suite). No additional gate leaves in this branch: the branch's rows in the root Test Gate matrix are the gate. Set children TESTS_VALIDATED when the root gate reaches TESTS_VALIDATED.

### Phase 1: Dispatch Concurrency Group B

Dispatch same-letter children simultaneously via `delegate_task`: goal =
"Implement all tasks from [leaf]"; context = full leaf text + contracts +
conventions + relevant source INLINED; include the no-commit, no-git-add,
no-read_file, write-once-and-stop rules.

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check
leaf spec, contracts, Review Checklist; run tests; gate-test diff must be
empty. Pass: commit + status REVIEWED. Gaps: re-dispatch with findings,
max 3 cycles, then escalate to parent.

### Phase 3: Integration Review

After ALL children REVIEWED: verify contracts between children, run the
full suite, check no import cycles. Gaps: re-dispatch the failing child
with contract fixes (uncommitted). Pass: normalize formatting, run the
line-number corruption check, commit integration changes, set children
COMPLETE, report COMPLETE to parent.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
      (any such diff = automatic NEEDS_FIXES)
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders,
      commented-out code)
- [ ] No line-number corruption (`     N|` prefixes) in source files

Output: APPROVED or a list of specific gaps.

## Coding Conventions

(Identical to ../master.md Coding Conventions - passed verbatim to every dispatch.)

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element (leaf/task/contract) | Verified by |
|-----|---------------------------------------------|-----------------------------------|-------------|
| R3 | "Signal voice calls" | 11a (C4) | signal-sidecar e2e voice scenario |
| R2 | "Include signal messaging" | 11b (C2 attempt) | signal-sidecar e2e msg scenario |
| R1 | fallback chain channels | MessageAttempt + Place outcomes feed ChainRunner | contact-chain e2e |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 11a-signal-voice-leg | PENDING | 0 | |
| 11b-signal-msg | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until the branch's
test-gate leaves reach TESTS_VALIDATED.

## Integration Test Plan

- `go build ./...` clean; `go test ./internal/signal/... ./internal/signalleg/...` green.
- signal-sidecar e2e suite (root gate): voice scenario (stub callEvent RINGING_OUTGOING->CONNECTED->PCM flows->ENDED, registry empty after) + msg scenario (envelope -> session row; Send path throttled).
- `go test -tags e2e ./e2e/suites/contact-chain/` - signal_voice busy outcome advances the chain.

## Test Gate

Covered by the ROOT gate (../master.md Test Gate matrix rows F3, F4, F5).
No branch-local gate leaves. Delta protocol applies at root.

Statuses: inherited from root matrix.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda (dated, appended to the dispatch brief). Never edit a
dispatched leaf document mid-execution.

## Notes

- Audio mode is socket-audio FIRST; the virtual-audio-device path (PulseAudio null-sink / BlackHole) is a documented fallback in the leaf notes but NOT implemented in this tree - the daemon-side adapter only needs signal-cli's socket PCM.
- The Rust tunnel source (signal-call-tunnel, in signal-cli tree) is the framing authority for socket audio; the research archive carries the summary. If the stub disagrees with the archive, flag drift - do not invent framing.
