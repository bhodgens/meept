---
title: Voicechild
---

# Voicechild (jargo voice child)

Status: In progress. Part of the voice phone channel; see
docs/workflows/voice.md and docs/workflows/voice-sidecar.md.

## Overview

internal/voicechild and cmd/meept-voice are the child side of the voice
sidecar: a separate binary that owns the ears (streaming speech-to-text),
turn-taking and barge-in detection, and the mouth (text-to-speech) for a
phone call. The child never sees Telnyx and never holds secrets across
restarts: the daemon hands it provider keys over the unix socket at call
setup, and the child keeps them in memory only.

## Problem

The daemon must not run audio models or vendor sockets inline (a linked
model cannot be unloaded, and a slow call would block chat traffic). A
separate child isolates the model heap and lets the daemon kill it idle
without touching the main process.

## Behavior

- internal/voiceframe: the C1 frame codec shared by daemon and child
  (stdlib-only; pinned by a test).
- internal/voicechild: Child connects the shared text socket, routes
  setup/say/clear/end per call, one jargo-backed pipeline per call.
  Dispatch shape (voice-startguard C1): consecutive FINAL transcripts
  inside `voice.turn_confirm_ms` (default 900, keys-frame selector; 0 =
  legacy per-final dispatch) aggregate into ONE last:true prompt frame,
  dispatched after the window passes with no newer final and no active
  mic (the pumpAudioIn speaking-floor tap re-arms the window, bounded
  at two re-arms); the 8 s buffer cap force-dispatches; transcript-
  stream EOF and hangup flush the pending text; `clear` (barge-in)
  drops it. Finals arriving during live playback or with the barge-in
  latch tripped dispatch immediately (merged with the pending buffer)
  — the interrupt path stays on arrival. Partials stay per-preview.
  Telemetry: one INFO 'voicechild: final prompt dispatched aggregated'
  per dispatch with call_id, parts, chars, reason.
  Short-final gate (`voice.min_turn_words`, keys selector, default 0 =
  off): at N>0 a FINAL whose bracket/asterisk-STRIPPED residue carries
  fewer than N whitespace-separated words drops before evidence
  stamping and aggregation (dispatches nothing, seeds no pending
  buffer). The gate ships with four improvements (voice-peragent leaf
  01), keyed by additive selectors on the same frame and ALL strictly
  inert while min_turn_words is 0 — zero config stays the plain
  word-count gate:
  - exempt_phrases (the daemon always sends the effective set —
    default stop, cancel, yes, no, thanks, thank you, okay, ok, mm-hm,
    mmhm, uh-huh, go ahead; an old daemon's absent line = NO
    exemptions, the legacy gate): a short final whose WHOLE stripped
    residue matches one phrase (case-insensitive, outer punctuation
    trimmed) is exempted, never dropped — one-word commands survive
    any N;
  - reply_exempt_s (default 15, 0 = off): a short final arriving
    within S seconds of the CHILD's own last spoken-audio write is a
    REPLY to the agent, not background — exempted (no extra wire
    traffic: the pipeline's playback write loop already stamps it);
  - energy_gate + min_rms (default off): with the gate ON, a short
    final drops ONLY when the utterance window's average incoming mic
    RMS sits BELOW min_rms (the pump's per-chunk energy tap already
    feeds the barge-in VAD; the gate accumulates the same rmsEnergy
    over the aggregation window) — quiet far-field TV fragments drop,
    a loud near-field "Thank you." passes as exempted reason=energy;
  - fragment_decay_s (default 0 = off): after K consecutive drops
    within S seconds, the effective threshold rises to N + min(K, 3);
    any dispatched final resets the streak. Chatter detected =>
    stricter for a moment. State is per pipeline instance: bounded
    and stateless-safe across calls.
  Telemetry: INFO 'voicechild: short final dropped' with
  reason=wordcount|energy per drop, and INFO 'voicechild: short final
  exempted' with reason=phrase|reply|energy whenever an improvement
  saved a would-be drop — the operator's tuning signal in the log.
  The playback worker emits the C1 `played` frame (last:true) once per
  turn-final (`last:true` say) stream, strictly after its final byte
  reaches the .out socket - the daemon's link-floor drain watch arms
  from that edge; aborted/error/non-final streams emit nothing.
  Providers: Deepgram streaming STT and OpenAI TTS adapters behind the
  STTProvider/TTSProvider seams (internal/voicechild/providers), with a
  single reconnect per call, streamed first audio, and chunked s16le
  resampling to the negotiated rate.
- cmd/meept-voice: --text-socket and --audio-dir flags; the start token
  arrives on stdin, never argv or environment.
- Filler clip cache (plan 20261113-filler-clips-and-context-warm,
  internal/voicechild/clips.go): the child renders and serves the
  pre-synthesized filler/greeting/silence/farewell PCM.
  `clip_render` frames (daemon -> child, <=32 {key,text} specs) diff
  against the content-addressed store under $MEEPT_HOME/voice_clips/:
  warm entries answer `clip_ready ok:true` with zero synth, misses
  render in the BACKGROUND at canonical mono s16le 16 kHz (never the
  call's negotiated rate) and store Put + `clip_ready`; entries that
  arrive before the keys frame queue (cap 256, drop-oldest + WARN) and
  drain once providers exist; the render worker dies with pipeline
  teardown. A `say` frame carrying `clip_id` whose store entry is
  valid streams the cached PCM into the SAME playback lane as
  synthesized audio (pacing, barge-in clear, and the `played` edge
  untouched - the swap happens at the reader); any miss, invalid key,
  or open error falls through to live Speak(text) byte-identically,
  with no opportunistic backfill. The keys frame's `tts_ref_hash`
  (hex sha256 of the clone reference audio, "" = preset voice)
  participates in the C1 clip-key formula ONLY - the reference never
  rides a TTS request. No PCM ever crosses the text socket: clip
  frames carry hex keys + text.

## Configuration options

Provider selection and model names come from the daemon's voice config
(see docs/workflows/voice.md) and ride the setup and keys frames. The
child reads no meept config files.

## Observability

Child RSS is logged by the daemon at spawn, first call, and idle stop.
Key material never appears in logs: the keys frame redacts itself.

## Edge Cases

- Missing provider key: that call fails with a named error; other calls
  and the child keep running.
- Vendor websocket drop: exactly one reconnect per call, then the stream
  ends; audio spoken during the gap is lost (same trade as jargo's
  wsservice).
- jargo is imported only here (and internal/voiceframe never): pinned by
  TestNoJargoImport.
