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
| 01 | VALIDATED | Merged in 42d727d5 (merge parent 4cb51951). Mechanically verified at HEAD (2026-11-16): `internal/voicechild/pipeline.go:178-183` aggregation fields (turnConfirm, pendingFinal*, pendingFinalSeq, micHoldRearms), `confirmWindow()` :902, `accumulateFinal` :911 with 8 s age cap + timer-seq stale-closure guard; config surface `internal/config/schema.go:5237` (TurnConfirmMs default 900) + `:5646/:5654` doc field + `:5924` negative-clamps-to-0 validation + `:6041` employee_leg tri-state selector; daemon emission `internal/daemon/voice_keys.go:52-59/:174-180`; child decode `internal/voicechild/keys.go:42-47/:191-197`; config template `config/meept.json5:1361`; doc `docs/workflows/voicechild.md` (+12 lines in the merge). Gates green (2026-11-16, `-count=1`): `go test ./internal/voicechild/ -run 'Aggregation|ShortFinal'` — 22/22 PASS incl. TestAggregationTwoFinalsOnePrompt, TestAggregationBreathPatternSingleDispatch, TestAggregationLegacyZeroDispatchesPerFinal, TestAggregationEOFTranscriptStreamFlushes, TestAggregationHangupFlushes, TestAggregationMicHoldBounded/LivePump, TestAggregationCapForceDispatch; `./internal/daemon/ -run VoiceKeys` — 7/7 PASS incl. TestVoiceKeysEmissionTurnConfirmSelector + TestVoiceKeysEmissionDefaultConfigSends900; `./internal/config/ -run 'TurnConfirm|Voice'` PASS; segmentation unchanged: `./internal/voicechild/providers/ -run WhisperPaced` PASS (TestWhisperPacedStreamFlushesWithoutClose stays green). |
| 02 | VALIDATED | Merged in 7bc515ed (merge parent 4cb51951). Mechanically verified at HEAD (2026-11-16): `cmd/meept/voice.go:93` `--record` flag; `:114-126` live-mode-only validation (--record refused in file mode) + MkdirAll; `:856-938` setupLiveSpeaker — warm via silent `liveWarmBurstBytes` burst BEFORE dial (:906), warm-through-recorder so the burst never lands in the capture (:925-927); `:1070-1077` runVoiceLocalLive constructs + warms the chain before dialing; recorderPlayer tee writes `<ccid>-<side>.raw` + meta JSON. Gates green (2026-11-16): `go test ./cmd/meept/ -run 'SetupLiveSpeaker|TeardownLiveSpeaker|VoiceLocalRecord'` — 7/7 PASS incl. TestSetupLiveSpeakerWarmsBeforeDial, TestSetupLiveSpeakerWarmFailureNeverDials, TestSetupLiveSpeakerRecordsWithoutWarmBytes, TestVoiceLocalRecordFlagValidation. |
| 03 | UNVERIFIED (live drill pending) — hermetic gates green | 2026-11-16 hermetic reconciliation pass: leaf-01/02 unit gates above ALL green at HEAD (post-4cb51951); hermetic e2e `make e2e-fast-area AREA=voice-local` PASS (49.8 s) — the full local leg end-to-end through the REAL STT/TTS adapters + real pipeline (single-final dispatch shape; the two-finals aggregation window itself is pinned at unit level by TestAggregationTwoFinalsOnePrompt/TestAggregationBreathPatternSingleDispatch). The three LIVE steps below were NOT run (no live daemon, no live LLM — out of scope for this reconciliation pass). Owner steps when running the drill: (1) rebuild `make build` and restart the daemon; (2) paced-replay: synthesize a 24 kHz/16-bit mono WAV of two sentences separated by a ~500 ms pause, stream it at real time into a scratch-daemon `voice local` call, assert ONE `prompt_dispatch` + ONE reply in the telemetry ring (`voice.local.events`); (3) human-leg: `./bin/meept voice local --employee secretary --record /tmp/vsg-rec` (interactive), then `scripts/voice-gap-analyze.py /tmp/vsg-rec` — first-window RMS must be non-zero (no open truncation) and the gap/overlap table is the jitter evidence; (4) `meept voice link` re-run 60 s — dispatches must still fire both sides (confirm window must not stall link floor telemetry), report analyzer numbers. Record the outcomes here to close leaf 03. |

## Delta log

- **2026-11-16 (reconciliation wave)**: Tree arrived all-PENDING although the work
  merged in 4cb51951 (parents 42d727d5 leaf 01, 7bc515ed leaf 02; confirmed via
  `git log --oneline 4cb51951` + `git show --stat 4cb51951`, 16 files, +1778/-64).
  Every deliverable was re-verified mechanically at HEAD: symbols grepped with
  file:line citations (see table), and every verification-gate test command from
  this master was actually run green with `-count=1` (22 aggregation/short-final
  unit tests, 7 voice-keys emission tests, config schema/voice tests, whisper
  paced-flush segmentation pin, 7 CLI warm/record tests, plus the hermetic
  voice-local e2e suite). Leaf 03's live drill remains OPEN by design — this pass
  ran hermetic proof only (no live daemon, no live LLM calls, per the
  reconciliation constraints); the exact owner steps are recorded in the leaf 03
  row. Table updated PENDING -> VALIDATED (01, 02) and PENDING ->
  TESTS-VALIDATED/live-open (03).
