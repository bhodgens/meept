# Signal voice leg - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The failing tests ALREADY EXIST and
> are committed (test gate 00T) - your job is to make them pass; do NOT
> delete, skip, weaken, or rewrite any gate test (report suspected-wrong
> gate tests to the orchestrator instead). Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** The Signal voice-call leg (contract C4): registry leg "signal", call-event state machine bridged to Manager fan-out, socket-audio adapter into the sidecar C2 pair.
- **Dependencies:** 10-signal-sidecar (client, REVIEWED), 01-contact-foundation (REVIEWED)
- **Estimated Context:** 45K
- **Concurrency Group:** B

## Goal

An employee places a Signal call with the full voice sidecar loop: supervisor-gated speech, STT-partial barge-in, transcript + session persistence, registry capacity shared with phone/local legs. Outbound first (Place); inbound accept is a thin wrapper (AcceptCall -> same pipeline).

## Context

Meept voice sidecar at /Users/caimlas/git/meept: the daemon adapts each transport into per-call unix PCM pairs consumed by the jargo child (contract: internal/voice/AGENTS.md - READ IT; frame/socket/splice/release contracts are pinned there). The local leg (internal/voice/local.go, localsplice.go) is the carrier-free template: this leaf's adapter copies its shape but MUST transcode (signal-cli socket audio arrives at RingRTC's native rate - resample inbound and outbound to L16 s16le@16k, chunk 640B/20ms outbound paced). The local leg's ErrL16Only refusal does NOT apply here; the adapter owns the resample. callEvent CONNECTED arms the talk loop (equivalent of the open->setup handshake); ENDED runs Manager.FailOutcome exactly like a phone hangup.

## Verified Anchors

- internal/voice/AGENTS.md - the full contract (frames C1, sockets C2, splice, release fan-out, leg matrix)
- internal/voice/dial.go:555-569 - Leg constants (zero value = phone; add LegSignal = "signal")
- internal/voice/dial.go:621-633 - shared capacity + the no-E.164-mangle rule for non-phone targets
- internal/voice/local.go:222-256 - LocalLeg.Open gates/AcquireLeg/TrackOutbound order (mirror for Place)
- internal/voice/localsplice.go - pacing rule (one 640B chunk per 20ms) to replicate with resample
- internal/voice/manager.go - TrackOutbound/FailOutcome/release fan-out seams
- internal/voice/binding.go:199-231 - OnCallStart binds (number, employee) session + call row
- internal/daemon/voice_wiring.go:683-745 - open-frame producer + keys emission site (talk-loop arming reference)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/voice/dial.go: LegSignal constant added.
// internal/signalleg/leg.go (NEW package):
type Deps struct {
    Client     *signal.Client
    Registry   *voice.Registry   // AcquireLeg shared
    Manager    *voice.Manager    // TrackOutbound/FailOutcome
    TextServer *voice.TextServer // open/say/clear frames
    Binding    voice.Binding
    Allowlist  func(uuid string) bool
    Effects    EffectsSeam   // reservation + completion (phone-outbound posture)
    Audit      AuditSeam     // row BEFORE StartCall
}
func New(deps Deps) (*Leg, error) // nil-dep refusals, named errors (local.go:191 posture)
func (l *Leg) Place(ctx context.Context, employeeID, targetUUID, firstLine string) (ccid string, err error)
func (l *Leg) Accept(ctx context.Context, callEvent signal.CallEvent, employeeID string) (ccid string, err error)
func (l *Leg) End(ccid string) error // idempotent; foreign id = ErrUnknownSignalCall
// Registry: AcquireLeg(ccid, employeeID, "signal:<uuid>", "signal");
// ccid = "signal-" + pkg/id.Generate().
// State machine: RINGING_OUTGOING -> (CONNECTED: mark answered, arm talk loop,
// emit open frame + keys) -> ENDED -> FailOutcome(OutcomeByReason(reason)).
// busy/timeout -> outcome distinct from hard failure (FailOutcome posture).
// Audio adapter: goroutine pair; OUTBOUND paced 640B@20ms L16@16k after
// resample; INBOUND chunked 640B; child NEVER sees non-L16@16k chunks.
```

### What This Leaf Consumes

- C3 signal.Client (leaf 10) - call methods + RegisterCallEvents
- voice.Registry/Manager/TextServer/Binding seams (existing)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | LegSignal constant + dial gates | Modify: internal/voice/dial.go (LegSignal="signal"; target "signal:<uuid>" skips NormalizeE164) | `go test ./internal/voice/ -run Registry` | LegSignal undefined | signal leg target never E.164-mangled; shared capacity rules enforced vs phone/local | Given live signal call uuid U + live phone call same E.164-derived target; Then AcquireLeg respects per-target uniqueness |
| T2 | leg.go Place gates | Create: internal/signalleg/leg.go; Test: internal/voice/signal_leg_test.go (exists, gate RED) | `go test ./internal/voice/ -run SignalLeg` | leg absent | Place: gates in order (enabled, allowlist, capacity, effects reserve, audit row) BEFORE Client.StartCall; any gate refusal = named error, no side effects | Given uuid not allowlisted; When Place; Then ErrNotAllowedlisted AND no StartCall invoked AND no audit row |
| T3 | callEvent state machine | Modify: internal/signalleg/leg.go | `go test ./internal/voice/ -run SignalLeg` | no state machine | CONNECTED -> registry live + open frame + talk loop armed + Binding.OnCallStart(direction outbound, number signal:<uuid>); ENDED -> FailOutcome with reason-mapped outcome; RECONNECTING tolerated (no teardown) | Given stub CONNECTED then ENDED(busy); When events fire; Then FailOutcome receives the busy-distinct outcome and registry releases |
| T4 | audio adapter | Create: internal/signalleg/audio.go | `go test ./internal/signalleg/ -run Audio` | adapter absent | outbound: resample -> paced 640B@20ms L16@16k into C2 .out; inbound: chunked 640B into C2 .in; backpressure error ends the leg with named error | Given 48k source PCM; Then C2 bytes are L16@16k at 640B/20ms pacing (timing asserted with fake clock) |
| T5 | Accept + End | Modify: internal/signalleg/leg.go | `go test ./internal/voice/ -run SignalLeg` | accept/end absent | Accept arms same pipeline with direction inbound; End idempotent (double End nil); foreign ccid = ErrUnknownSignalCall | Given End twice on same ccid; Then second returns nil |
| T6 | daemon wiring | Modify: internal/daemon/voice_wiring.go (construct Leg when [signal].enabled; nil degrades) | `go build ./...` | leg never constructed | wiring nil-safe; disabled signal config leaves daemon booting clean (degrade posture mirrors voice wiring) | Given [signal].enabled=false; Then no Leg constructed, daemon boots, no warnings beyond info |

## Self-Verification Checklist

- [ ] All tasks implemented; gate tests passing (signal_leg_test GREEN)
- [ ] Interface contracts satisfied exactly (C4)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] Do NOT COMMIT.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests passing, gate tests untouched
- [ ] Interface contracts match exactly (signatures, types, paths)
- [ ] internal/signalleg imports NO telnyx code; child chunk sizing rule honored (640B L16@16k always)
- [ ] Conventions followed; no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- RMS barge-in is FORBIDDEN on this leg (phone posture: STT-partial trigger). The open frame carries NO local:true / barge_in_rms fields for signal legs.
- Allowlist config: [signal] section gains allowed_uuids []string (mirror voice.allowed_numbers posture; inbound from non-allowlisted = do not accept).
- This leaf does NOT implement inbound ring UX (auto-accept policy is a follow-up); Accept exists as the seam.
