> **Source repo:** caimlas/uptractor-jobfile (`~/git/uptractor-jobfile`)
> **Source:** branch `claude/uptractor-repo-organization-5jusi4`, `backend/research/recommendation.md`
> **Imported verbatim 2026-11-10** (content below this header is byte-identical to the source).

# Research: Final Voice Stack Recommendation

Date: 2026-10-28. Synthesis of rounds 1-3 (see sibling files for detail).

## Decision: cascaded local pipeline on Mac, not unified speech-to-speech

| Stage | Pick | License | Why |
|---|---|---|---|
| STT | Parakeet TDT 0.6B (MLX) | CC-BY-4.0 | Streaming, word-level partials, proven on Apple Silicon |
| VAD/turn | Silero VAD + Smart Turn v3 | MIT | Barge-in, end-of-turn |
| Brain | Qwen3-8B/14B 4-bit MLX (30B-A3B for headroom) | Apache-2.0 | Mature function calling; no license cliff |
| TTS | Kokoro-82M via mlx-audio | Apache-2.0 | ~74ms first chunk, true streaming, OpenAI-compatible server |

- Speech stack: 3.6-5.4GB resident. Mac M2 Ultra 64GB holds brain + speech
  stack with 50GB+ headroom.
- Latency: 0.8-1.2s end-of-speech to first audio measured on Apple Silicon;
  target under 2s holds even with a tool call plus spoken filler.
- Telnyx: Media Streams with L16 or OPUS 16kHz to avoid transcoding.
- All numbers spoken by the agent come from tool results (SQLite), injected
  verbatim - the model never recites quantities from memory.

## Swap points (config changes, not redesigns)

- Brain: LFM2.5-8B-A1B (already on disk, 253 tok/s, native tool calling) or
  LFM2.5-24B-A2B (quality tier). LFM Open License = free commercial use only
  under $10M group revenue - swap consciously.
- TTS persona voice: Chatterbox-Turbo zero-shot clone (MIT, streaming).
- STT alternative: Kyutai STT-1b (built-in semantic VAD).
- Future consolidation: LFM3-Audio-2B once MLX streaming support lands.

## Rejected (license or capability)

Unified S2S (Moshi, PersonaPlex - no tool calling, weak numerics); F5-TTS and
XTTS-v2 weights (non-commercial); GLM-4-Voice (strings + CUDA); LLaMA-Omni
weights (non-commercial); NemotronLabs VoiceChat (research-only); VibeVoice
(research-only card + sub-realtime on Mac); MAI-Voice (API only); Magpie TTS
(license contradiction + CUDA); AudioFlamingo 3 (non-commercial).
