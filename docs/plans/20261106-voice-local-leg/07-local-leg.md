# LocalLeg lifecycle - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. Do
> NOT commit — the orchestrator handles git. Explore with search_files or
> terminal cat, never read_file. Do NOT read back files you just wrote.
> This leaf runs ONLY after leaves 02,03,04,05,06 are reviewed and committed
> (wave C).

## Meta

- **Parent:** master.md
- **Scope:** internal/voice/local.go - the local-leg call lifecycle: open,
  binding, day brief, frames, teardown. Glue in voice_wiring.go.
- **Dependencies:** 03 (open handler: pipeline gets created + setup
  answered), 04 (sendOpen + ResolveProfile + per-call keys content), 02
  (local RMS barge-in), 05 (RunLocalSplice), 06 (registry leg). Also
  tierA 05/06 committed (transcripts + phone barge-in) or local voice
  cannot hold a conversation.
- **Estimated Context:** 90K
- **Concurrency Group:** C
- **Files this leaf owns:** internal/voice/local.go (new),
  internal/voice/local_test.go (new), internal/daemon/voice_wiring.go
  (glue only: construct LocalLeg when cfg.Local.Enabled, expose it),
  internal/daemon/voice_employee.go (day-brief reuse site)

## Goal

One callable unit that turns (employee, label) into a live local voice
call: registry slot, audio pair, child pipeline via open frame, talk loop
via the setup frame that now actually arrives, day brief + binding exactly
like a phone call, and a release fan-out identical to the phone path minus
the carrier.

## Context

Key files:
- internal/voice/dial.go DialFlow - read the whole outbound sequence. The
  local leg mirrors steps 3-8 MINUS steps 1-2 and minus effects/audit
  (CL5: deliberate, pinned by test).
- internal/daemon/voice_wiring.go routeFrame - the setup handler arms the
  talk loop from Registry.Live(); the local instance appears there because
  Open() acquires first. Confirm the interrupt frame path reaches the loop
  (it routes by ccid; nothing to add).
- binding.OnCallStart call site (grep `OnCallStart(` in internal/daemon +
  voice_call/dial paths) - mirror its day-brief build (BuildDayBrief with
  ActivitySource/CallTranscriptSource/MemorySource/GoalSource adapters) for
  direction "local", number "local:<label>".
- Manager.TrackOutbound / FailOutcome / WaitCall (internal/voice/manager.go)
  - the local leg rides all three; ccids "local-<id>".

## Interface Contracts (From Parent)

CL5 (verbatim) - LocalLeg, NewLocalLeg(cfg LocalLegConfig, deps LocalDeps),
Open(ctx, employeeID, label) (LocalCall, error), LocalCall{CallID,
SessionID, InPath, OutPath}, End(ccid, reason). LocalCall exposes the pair
PATHS; leaf 08's clients dial them. LocalDeps carries: Registry, ChildProc,
TextServer, Manager, Binding, DayBriefBuilder (func(ctx, employeeID,
number) (DayBrief, error) injected by daemon glue so internal/voice stays
dependency-free), Profile cfg, Logger.

Behavior pins:
- Open gates: voice.enabled && voice.local.enabled; empty employee -> named
  error; registry ErrBusy/ErrDuplicateTarget surface unchanged.
- ccid = "local-" + pkg/id.Generate().
- Direction "local" everywhere (open frame, binding row, progress events).
- No effects.Reserve, no audit row, no tel.* call anywhere in local.go
  (compile-pinned: local_test builds LocalLeg with a NIL carrier seam).
- Teardown: End -> Manager.FailOutcome(ccid, OutcomeHangup, reason); ctx
  cancel of the splice ctx; audio.Close(); childproc NoteCallEnd via the
  existing release hook (releaseCall already runs it).
- Idle/reaper: no silence-based auto-hangup override; talk loop's normal
  silence/max-call timers apply (same config), so a forgotten local call
  self-reaps.

## Tasks

| Task ID | Objective | Files | Verify | RED reason | PASS |
|---|---|---|---|---|---|
| T1 | Test: Open happy path against fakes (fake registry, fake text sink capturing frames, real AudioPair in tmpdir, manager with nil-carrier seam) - order: Acquire -> TrackOutbound -> pair -> open frame -> splice started; LocalCall fields correct | local_test.go | go test -p 2 ./internal/voice/ -run TestLocalLegOpen | no local.go | PASS |
| T2 | Test: duplicate employee acquire refuses; disabled config refuses; nil daybrief builder still opens (brief empty) | local_test.go | -run TestLocalLegGates | - | PASS |
| T3 | Test: End fans out once - FailOutcome -> release hook sinks (registry released, binding ended via fake, child note via fake); double End is nil-idempotent | local_test.go | -run TestLocalLegEnd | - | PASS |
| T4 | Implement local.go per CL5 | local.go | T1-T3 | - | PASS |
| T5 | Day brief for local calls wired through DayBriefBuilder at Open (binding.OnCallStart with direction "local") | local.go, daemon glue | -run TestLocalLegBrief (daemon pkg, fake sources) | - | PASS |
| T6 | Daemon glue: construct LocalLeg in WireVoice when cfg.Local.Enabled (nil otherwise); VoiceWiring.Local accessor; Start/Stop hookup | voice_wiring.go | go build ./internal/daemon/ && go test -p 2 ./internal/daemon/ -run TestWireVoice | - | PASS |
| T7 | gofmt/vet/race | - | go vet ./internal/voice/... ./internal/daemon/...; go test -p 2 -race ./internal/voice/ -run TestLocalLeg | - | clean |

## Interface Contract (What This Leaf Exposes)

voice.LocalLeg API (CL5) consumed by leaf 08 (RPC/CLI) and leaf 11 (e2e).

## Self-Verification Checklist

- [ ] local.go has zero imports of internal/voice/telnyx (grep pins it)
- [ ] releaseCall path unchanged; local release adds nothing to the hook
- [ ] sun_path: pair paths returned by LocalCall under SocketDir fit 103 B
- [ ] Existing phone tests green

## Review Checklist

- [ ] No effects/audit calls (CL5 difference pinned by test, not comment)
- [ ] binding/day-brief call ordering matches the phone site (read both)
- [ ] Typed-nil guards on every deps field (repo rule)
- [ ] ASCII only; no artifacts
