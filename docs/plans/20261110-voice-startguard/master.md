# Voice turn-aggregation + open-truncation fixes - master

> Worktree .hermes/worktrees/voice-startguard, branch plan/voice-startguard
> (off main 15261b71). User-reported issues from live call
> local-496a3fd0d2ab3fc6 (2026-11-10 14:58-14:59, CLI event log quoted
> below): (A) agent started speaking during the operator's brief
> breath-pause and (perceived) kept talking over him; (B) residual jitter
> + beginning of audio truncated at call start.

## Evidence from the operator's live call (verbatim log)

    14:59:11 heard: Yeah, I'm not sure.        <- operator MID-SENTENCE
    14:59:12 replied (33 words, 1100ms)        <- agent answers the FRAGMENT
    14:59:15 heard: Do you think you could explain to me?  <- continuation
    14:59:15 >> barge-in                        <- operator talks over the reply
    14:59:16 replied (16 words, 1023ms)         <- agent speaks again over him

Mechanism: the whisper provider now flushes an utterance after 500 ms of
wall-clock idle (fix from commit "whisper flush also on WALL-CLOCK idle",
required because link/pump silence is an ABSENCE of bytes). 500 ms is the
right SEGMENTATION latency but the wrong DISPATCH threshold: a normal
breath-pause is 300-600 ms, so mid-sentence pauses finalize fragments and
each fragment dispatches a full employee turn - the agent talks while the
operator is still talking. The barge-in cancellation WORKED (log shows
barge-in + new pipeline); "didn't stop" = fragment 2's reply starting
immediately. Root cause is dispatch eagerness, not cancel plumbing.

Open truncation: the CLI live-mode oto player opens its audio context
lazily on the FIRST Play (the link monitor hit the identical problem -
"warm the monitor's audio context before dialing the tap"; `voice local`
never received that warm-up). Plus no way to record the human-leg
playback to MEASURE it.

## Contracts

C1 (leaf 01) TURN AGGREGATION: child-side, between the segmenter and the
prompt frame: a FINAL transcript is held as pending; more finals arriving
inside voice.turn_confirm_ms (default 900) are APPENDED (single prompt);
dispatch happens only after turn_confirm_ms of no new final AND no active
mic RMS; stream EOF/hangup flushes pending immediately; turn_confirm_ms=0
= legacy per-final dispatch (backward-compat). Previews (last:false) and
phone-leg partial barge-in untouched. This is the "in-flight prevention
fence": no employee turn starts while the caller is mid-utterance.

C2 (leaf 02) OPEN-TRUNCATION + human-leg evidence: warm the CLI player
(silent 20 ms burst) BEFORE the live call's speaker loop starts (same fix
shape as monitor, cmd-side), and add `voice local --record DIR` reusing
the monitor's recorderPlayer so the operator's OWN playback is measurable
by scripts/voice-gap-analyze.py (head truncation = first-window RMS;
jitter = gap table).

## Leaves

| # | Leaf | Files | Depends | Wave |
|---|------|-------|---------|------|
| 01 | turn aggregation + confirm window | internal/voicechild/pipeline.go(+tests), internal/config/schema.go, internal/daemon/voice_keys.go, config/meept.json5, docs/workflows/voicechild.md | - | A |
| 02 | CLI player warm-up + voice local --record | cmd/meept/voice.go(+tests) | - | A |
| 03 | live verification (parent) | - | 01,02 | B |

## Verification gates

- Leaf 01: unit - two finals 400 ms apart -> ONE prompt_dispatch frame,
  text aggregated; breath pattern like the live log -> single dispatch;
  RMS-active hold; EOF flush; turn_confirm_ms=0 legacy pin; ALL existing
  pins green (provider-level TestWhisperPacedStreamFlushesWithoutClose
  stays - segmentation unchanged; aggregation is pipeline-level).
- Leaf 02: unit - warm-up fires before first real burst (fake player);
  --record writes <ccid>-<side>.raw + meta; validation errors.
- Leaf 03 (me, live): rebuild binaries, restart daemon, then:
  1. Paced replay test: a synthetic 24 kHz/16k WAV of two sentences
     separated by a ~500 ms pause, streamed at real time into a scratch
     daemon call (script), assert ONE dispatch + ONE reply.
  2. Human-leg live call: `./bin/meept voice local --employee secretary
     --record DIR` (interactive); analyzer on the recording: first-window
     RMS non-zero (no truncation) + gap/overlap table.
  3. link re-run 60 s: dispatches still both sides (aggregation window
     must not stall link floor telemetry), analyzer numbers reported.
- go test -p 2 ./internal/voicechild/ ./internal/config/ ./internal/
  daemon/ ./cmd/meept/ green per touch.

## Sibling-collision notes

- Main checkout has foreign dirty files (executor.go, signalleg/*,
  permissions.go, cmd/meept/voice.go modified) - the worktree base is
  15261b71; leaf 02 edits cmd/meept/voice.go IN THE WORKTREE only.
- Do not touch: internal/agent/*, internal/voice/link.go coordinator,
  providers/stt_whisper.go (segmentation stays 500 ms).

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 01 | PENDING | |
| 02 | PENDING | |
| 03 | PENDING | |
