# Voice fresh-session-per-call + live playback smoothing - master

> Root orchestrator. Dispatches leaves, review before commit (main model),
> commits after review with pathspec. Builds on the landed local-leg tree
> (docs/plans/20261106-voice-local-leg, DONE) and the 2026-11-08/09 voice
> fixes (a40e228f, 9566ad27, 4738bcfa, a7cb648e). Spec base:
> docs/workflows/voice-sidecar.md + docs/workflows/voice.md.

## Meta

- Tree: docs/plans/20261109-voice-fresh-session/ (flat, repo convention)
- Execution backend: direct dispatch (delegate_task), siblings may share the
  working tree - commits use `git commit -- <paths>` (sibling-racy index rule)
- Authoring date: 2026-11-09. Reconcile against the working diff before each
  dispatch; the 20261108-employee-contact-channels plan is ACTIVE in the same
  area (plan doc dirty in git status) - do not touch its files.

## Goal

Two independent defects surfaced by live voice testing on 2026-11-08/09:

1. **Session snowball.** Voice calls resolve ONE session per (employee,
   target) pair (`Binding.resolveSession`, internal/voice/binding.go:210 -
   the target defaults to `local:<hostname>`), so every call appends its
   turns to the same stored conversation and every later model call replays
   the pile. The companion drifted into rambling about its own old probe
   calls. The manual mitigation (`--label fresh1`, verified 2026-11-09: fresh
   session per label) must become an opt-in product behavior.
2. **Live-mode speaker jitter.** `runVoiceLocalLive` (cmd/meept/voice.go:427)
   builds `NewSpeakerSink(player, 0)` - zero gap between bursts. The daemon
   paces 20 ms chunks; oto's playout underflows between bursts => audible
   jitter. File mode's gap=0-per-read shape is correct for a WAV trailer;
   live mode needs a small pre-buffer/gap.

Non-goals: no summarizing-memory system (separate product decision - the day
brief remains the only designed cross-call channel); no TTS/STT vendor
changes; no phone-leg session changes beyond the same flag applying (it is
leg-agnostic at the binding).

## Design

### Config (opt-in defaults rule, docs/agents/opt-in-defaults.md)

- `[voice] fresh_session_per_call` bool, **default false**. Global switch.
- `[voice] employee_leg` rows gain an optional `fresh_session` tri-state
  (absent = inherit global; true/false = per-employee override) - same
  per-employee routing table CL4 already reads for leg/tts_voice. Secretary
  reads better per-call; companion may want continuity; the override makes
  that operator policy, not code.

### Binding seam

`Binding.OnCallStart` gains a per-call bool (struct or option - leaf 02
decides the least-invasive shape and records it here): when true, skip the
(employee,target) lookup and CREATE a session named
`voice <employee> <ccid>` instead; calls.db rows and the release fan-out are
unchanged (they key off ccid). When false, byte-identical to today. The
wiring resolves the config (global + per-employee row) once per call and
passes it in - internal/voice stays dependency-free (no internal/config
import; the daemon computes the bool, mirrors the Profile resolver shape).

### Live speaker smoothing

`runVoiceLocalLive` sink gap: replace 0 with a named const (leaf 03 picks
the value; start at ~120 ms pre-roll equivalent - the sink's gap parameter
semantics get documented at the const). If the sink's gap model cannot absorb
20 ms-paced arrivals, add a small pre-buffer before first write (leaf 03
owns the choice; must not delay the FIRST burst beyond ~250 ms).

## Leaves

| # | Leaf | Files (primary) | Depends on | Wave |
|---|------|-----------------|-----------|------|
| 01 | config: fresh_session_per_call + employee_leg.fresh_session + template comments | internal/config/schema.go, config/meept.json5, internal/config/voice_config_test.go | - | A |
| 02 | binding + wiring: per-call fresh session | internal/voice/binding.go, internal/daemon/voice_employee.go, voice_wiring.go, + tests | 01 | B |
| 03 | live speaker smoothing | cmd/meept/voice.go (+ voice/speaker sink test if gap semantics change) | - | A |
| 04 | e2e + docs + AGENTS summary | e2e/suites/voice-local/, docs/workflows/voice.md, voice-sidecar.md, internal/voice/AGENTS.md, root AGENTS.md voice row | 01,02 | C |

## Verification gates

- `go test -p 2 -count=1 ./internal/config/ ./internal/voice/ ./internal/daemon/ ./cmd/meept/ ./e2e/suites/voice-local/` green
- Live check (user-interactive, not CI): two back-to-back `--employee
  companion` calls with the flag ON land in DIFFERENT session ids
  (voice_calls.db session_id per call); flag OFF reproduces the shared
  session; no `[prior calls today]`-style transcript replay in the second
  call's model input (assert via daemon log session ids + calls.db).
- Jitter: operator ear-check (playback quality is not CI-assertable).

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 01 | DONE | fresh_session_per_call + employee_leg.fresh_session (*bool tri-state) + template keys; gofmt alignment note: keep the in-struct comment (gofmt -w absorbed it cleanly this time) |
| 02 | PENDING | |
| 03 | DONE | The jitter was NOT buffering: oto context hardcoded 22050 Hz while the leg streams L16@16k (oto plays bytes at context rate, no resample) -> ~38% fast. resolveSampleRate + MicSampleRate at the seam; TestAudioPlayerSampleRateConfig pins it. Pre-commit consumer gate additionally required the dsh SetPlanMode method on internal/services fakeStore (broken at HEAD by the sibling DSH-9c commit; fixed here) |
| 04 | PENDING | |

Statuses: PENDING -> IN_PROGRESS -> IMPLEMENTED -> REVIEWED -> DONE | BLOCKED.
Update this table after every transition (repo plan-tree rule).

## Dispatch notes

- 01+03 are independent (wave A, parallel). 02 needs 01's field names.
  04 last.
- Every leaf: no commits (shared tree); parent reviews + commits with
  pathspec; `--no-verify` allowed only when the deadcode gate fires on the
  known-stale sibling baseline (state it in the message).
