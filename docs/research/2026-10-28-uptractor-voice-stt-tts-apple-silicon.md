> **Source repo:** caimlas/uptractor-jobfile (`~/git/uptractor-jobfile`)
> **Source:** branch `claude/uptractor-repo-organization-5jusi4`, `backend/research/voice-stt-tts-apple-silicon.md`
> **Imported verbatim 2026-11-10** (content below this header is byte-identical to the source).

# Research: Local STT/TTS Models for Apple Silicon (M2 Ultra 64GB)

Date: 2026-10-28. Subagent research round 1 of 3. Baseline to beat: Piper TTS.
Constraint: commercial-safe licenses only (product may become a managed service).

## Verdict

Classic streaming pipeline (STT -> LLM -> TTS) beats unified speech-to-speech
for a tool-calling assistant. TTS: Kokoro-82M replaces Piper as default.
Backbone: mlx-audio (MIT) for model-swappable MLX serving.

## TTS comparison

| Model | Size | Mac latency | Streaming | Custom voice | License | Commercial safe |
|---|---|---|---|---|---|---|
| Kokoro-82M | 82M | 47 ms / 10 words (M3 Max, bf16); ~74 ms first chunk; ~200 MB | Yes, sentence-chunk | No - 54 preset voices | Apache-2.0 | Yes |
| Chatterbox-Turbo | 350M | ~0.93x RTF on Mac (real-time-ish) | Yes | Yes - zero-shot clone 5-20 s, emotion tags | MIT | Yes |
| Chatterbox-Nano | 110M | ~3x realtime on CPU | Partial | Yes - 5 s clone | MIT | Yes |
| MeloTTS | 198MB | RTF 0.17x | Yes (MLX) | No - preset speakers | MIT | Yes |
| Qwen3-TTS | 0.6B/1.7B | RTF ~0.74x | Yes (mlx-audio) | Yes - cloning + VoiceDesign | Apache-2.0 | Yes |
| Piper (baseline) | ~16M | RTF 0.04x, ~338 MB | Limited | Per-voice training only | MIT models / GPL engine | Yes (subprocess) |
| F5-TTS | 0.34B | RTF ~2.7x | No | Yes | MIT code / CC-BY-NC weights | NO - weights non-commercial |
| XTTS-v2 | 0.47B | RTF ~1.7x | Limited | Yes | CPML (Coqui dead, no commercial path) | NO |
| ElevenLabs Flash v2.5 | API | ~75 ms + network | Yes (WebSocket) | Yes | Proprietary; Starter $6/mo min for commercial | Yes (cloud fallback) |

## STT

- Parakeet TDT 0.6B (CC-BY-4.0, MLX ports, streaming, word-level partials) - winner.
- Kyutai STT-1b (MLX, built-in semantic VAD) - alternative.
- whisper large-v3-turbo (whisper.cpp / mlx-whisper) - solid batch fallback.
- Voxtral, Distil-Whisper successors - no advantage found on Mac for streaming use.

## Turn-taking glue

- Silero VAD + Smart Turn v3 for end-of-turn and barge-in.

## Pipeline frameworks worth reusing

- mlx-audio (Blaizzy): MIT, 25+ TTS models, streaming with configurable interval,
  OpenAI-compatible /v1/audio/speech server, mlx-audio-swift for on-device.
- kokoro-mlx: dependency-light, gapless streaming.
- sonic-tts: clause-level streaming wrapper, TTFC ~126 ms.
- OrcaBot open-weight TTS benchmark harness: template for RTF/WER/PESQ on target hw.

## Upgrade path

1. MVP: Kokoro-82M via mlx-audio behind OpenAI-compatible server.
2. Persona voice: Chatterbox-Turbo zero-shot clone.
3. Designed voices: Qwen3-TTS 1.7B VoiceDesign.
4. Ship-in-app: kokoro-coreml (12-79x realtime on M-series) or mlx-audio-swift.
5. Cloud fallback for hero quality: ElevenLabs Flash v2.5 (paid plan required).

Avoid for commercial release: F5-TTS weights, XTTS-v2.

## Sources

- https://huggingface.co/hexgrad/Kokoro-82M
- https://github.com/gabrimatic/kokoro-mlx
- https://huggingface.co/sonic-speech/kokoro-82m-bf16
- https://huggingface.co/anayak16/kokoro-coreml
- https://github.com/resemble-ai/chatterbox
- https://www.resemble.ai/learn/models/chatterbox-turbo
- https://orcabot.com/benchmarks/open-weight-tts
- https://github.com/SWivid/F5-TTS
- https://huggingface.co/coqui/XTTS-v2/blob/main/LICENSE.txt
- https://github.com/coqui-ai/TTS/discussions/4304
- https://blaizzy.github.io/mlx-audio/
- https://elevenlabs.io/docs/overview/models
- https://elevenlabs.io/pricing
