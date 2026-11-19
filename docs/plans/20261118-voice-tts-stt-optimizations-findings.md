# Voice TTS/STT optimization review - findings index

Companion to `20261118-voice-tts-stt-optimizations/`. Kept OUTSIDE the tree
directory so the plan-tree checker does not treat it as a leaf.

## Scope

Review of meept voice TTS/STT path against skills: voice-latency-audit,
voice-speech-pipeline, voice-audio-frontends, voice-turn-taking, whisper.
All findings code-verified on 2026-11-18 by three parallel subagents and
re-verified in-session with grep/read_file.

## Findings

| ID | Title | Anchor | Plan element | Priority |
|----|-------|--------|--------------|----------|
| F1 | Smart Turn verdict discarded; model still fed every chunk | internal/voicechild/pipeline.go:220-223,1506-1510 | 01-foundation/02-smart-turn.md | P0 |
| F2 | Whisper cold subprocess per utterance and per barge-in flush | internal/voicechild/providers/stt_whisper.go:374-396 | 02-stt-path/02-whisper-persistent.md | P1 |
| F3 | Clip prewarm burst contends with first-audio on setup | internal/daemon/voice_wiring.go:1232; voice_clip_planner.go:162 | 03-tts-path/02-clip-yield.md | P1 |
| F4 | Deepgram speech_final and UtteranceEnd dropped; timer-only endpointing | internal/voicechild/providers/stt_deepgram.go:34-37,332-338 | 02-stt-path/03-deepgram-endpoint.md | P1 |
| F5 | No critical-entity priming (Deepgram keyterm, Whisper -p prompt) | stt_deepgram.go:250-264; stt_whisper.go:385 | 02-stt-path/04-entity-priming.md | P2 |
| F6 | Cloud OpenAI TTS pays qwen3 no-keepalive transport | internal/voicechild/providers/tts_openai.go:59-70,191,206 | 03-tts-path/03-transport-split.md | P2 |
| F7 | Context warm cannot fire on Whisper leg; debounce inherits 900 ms window | internal/voice/contextwarm.go:225; stt_whisper.go:81-83 | 04-context-warm/01-separate-window.md | P2 |
| F8 | Resampler allocates 640-byte scratch per Read (~270 KB churn/utterance) | internal/voicechild/providers/resample.go:111,180,190 | 03-tts-path/04-resampler-reuse.md | P3 |
| F9 | qwen3 chunk_size hardcoded to 4 | internal/voicechild/providers/tts_openai.go:40-43,252 | 03-tts-path/05-chunk-size-knob.md | P3 |
| F10 | Background clip renders do not yield to live synthesis on barge-in | internal/voicechild/clips.go:217-346 | 03-tts-path/02-clip-yield.md | P2 |

F1 and F10 both touch the analyzer/clear path; F10 is folded into the clip-yield
leaf so barge-in yield and setup contention share one gate.

## Non-findings (already implemented; explicitly rejected)

- Token-level pcm-stream + incremental framing + odd-byte alignment (tts_openai.go, pcmstream.go)
- Depth-2 head-of-burst synth prefetch (pipeline.go tryLaunchPrefetch:1849)
- playGen-based barge-in cancellation of all live synths (pipeline.go:2202-2239)
- Silero VAD gating + Smart Turn async init barrier (AGENTS.md)
- played-edge drain watch and holder-only link release (AGENTS.md)
- Filler clip cache + lane pools + farewell + context warm (plan 20261113)
- Fragment budget + mic-hold re-arms (pipeline.go:940-1101)
- ConnectSentences for supervisor-off path (talkloop.go:165-168)

## Verification discipline

Every latency benefit is UNMEASURED. Findings name mechanism and direction only.
No threshold change may be justified from this review alone; paired fixtures
and A/B runs are the acceptance evidence for latency claims.
