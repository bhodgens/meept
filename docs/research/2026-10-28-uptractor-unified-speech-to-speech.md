> **Source repo:** caimlas/uptractor-jobfile (`~/git/uptractor-jobfile`)
> **Source:** branch `claude/uptractor-repo-organization-5jusi4`, `backend/research/unified-speech-to-speech.md`
> **Imported verbatim 2026-11-10** (content below this header is byte-identical to the source).

# Research: Unified Speech-to-Speech vs Cascaded Pipeline

Date: 2026-10-28. Subagent research round 2 of 3.
Question: should the voice agent use one unified listen-think-talk model, or an
STT -> LLM -> TTS pipeline? Target: Mac M2 Ultra 64GB, sub-2s spoken responses,
tool calling (SQLite query, SMS trigger), near-zero hallucination tolerance.

## Verdict: PIPELINE wins, and it is not close

1. No self-hosted unified S2S model is realtime-class. Qwen3-Omni-30B-A3B fully
   local: ~145.7s time-to-first-audio; only the cloud DashScope Realtime API
   (~702ms) is fast. The self-hosted cascaded pipeline in the same benchmark:
   ~755ms with full function calling (arxiv 2603.05413).
2. Apple Silicon practitioner benchmarks are all pipelines: 800-930ms median
   end-of-speech-to-first-audio fully local (mthomas100/local-voice 874ms;
   kwindla/macos-local-voice-agents <800ms; shubhdotai/offline-voice-ai <1s
   with barge-in).
3. Tool calling + factual grounding is the unified models' weakness. Unified
   models that run on Mac (Moshi, PersonaPlex) have no function-calling
   protocol; audio-native tool decisions measure less reliable than text-based
   (LiveKit realtime-vs-cascade; FutureAGI 2026: cascaded is safer at 5+ tools
   with strict schemas).
4. The pipeline yields a full text audit trail of every spoken date, quantity,
   and dollar amount.
5. Telling signal: Kyutai built Moshi (unified) but shipped Unmute (a pipeline)
   as their recommended agent architecture.

Re-evaluate when: NemotronLabs VoiceChat gets a commercial license + Apple
Silicon port, or Qwen3-Omni speech-out gets a realtime MLX/GGUF serving path.

## Recommended stack

Telnyx Media Streams (L16 or OPUS 16kHz, no transcoding)
-> Silero VAD + Smart Turn v3
-> Parakeet TDT 0.6B (MLX) or Kyutai STT-1b-en_fr (MLX)
-> Qwen3-8B/14B or 30B-A3B 4-bit MLX brain with OpenAI-style function calling;
   tool results injected verbatim so spoken numbers come from the DB
-> Kokoro-82M streaming TTS (~74ms first chunk) or Qwen3-TTS-1.7B.

Speech stack: 3.6-5.4GB. Expected end-of-speech-to-first-audio ~0.8-1.2s.

## Model comparison (unified/omni)

| Model | Params | Mac runnability | Tool calling | Reasoning risk | License | Commercial safe |
|---|---|---|---|---|---|---|
| Qwen3-Omni-30B-A3B | 30B MoE/3B active | PARTIAL - MLX text-out only; speech-out in progress; ~146s/turn local | Text-out only | Low-med | Apache-2.0 | Yes |
| Qwen2.5-Omni-7B | 7B | Yes (mlx-lm-omni) but ~13s TTFA | No | High | Apache-2.0 | Yes (unusable) |
| Moshi 7B | 7B | Yes, first-class (~200ms, full duplex) | No | High (weak numerics) | CC-BY-4.0 weights / MIT-Apache code | Yes |
| GLM-4-Voice-9B | 9B | NO - CUDA only | No | High | Zhipu custom: registration + strings | CONDITIONAL |
| Step-Audio 2 mini | mini | NO - CUDA/vLLM only | Yes | Medium | Apache-2.0 | Yes (not Mac) |
| Mini-Omni | 0.5B | Weak, unmaintained | No | Very high | MIT | Yes (useless) |
| LLaMA-Omni 2 | 8B | NO - CUDA | No | Med-high | Weights non-commercial | NO |
| Ultravox v0.5 8B | 8B | Yes via mlx-lm-omni (speech->text, pairs with TTS) | Via backbone LLM | Low | MIT/Llama | Yes - future STT upgrade |
| PersonaPlex 7B | 7B | Yes at int8 (~68ms/step, full duplex) | No | High | NVIDIA Open Model License | Yes (upstream) |
| NemotronLabs VoiceChat 11B | 11B | NO - CUDA branch | Yes, native (82.5% tool F1) | Medium | Research-only weights | NO |

## Do-not-use list (license)

GLM-4-Voice (strings + CUDA), LLaMA-Omni weights (non-commercial),
NemotronLabs VoiceChat (research-only), AudioFlamingo (non-commercial),
Fugatto (weights never released).

## Sources

- https://arxiv.org/html/2603.05413
- https://github.com/kyutai-labs/moshi/
- https://kyutai.org/unmute/
- https://github.com/giangndml/mlx-lm-omni
- https://github.com/QwenLM/Qwen3-Omni/
- https://huggingface.co/zai-org/glm-4-voice-9b/blob/main/LICENSE
- https://github.com/stepfun-ai/Step-Audio2/
- https://github.com/fixie-ai/ultravox/
- https://huggingface.co/nvidia/personaplex-7b-v1
- https://github.com/mu-hashmi/personaplex-mlx
- https://livekit.com/blog/realtime-vs-cascade
- https://futureagi.com/blog/cascaded-voice-ai-vs-speech-to-speech-2026/
- https://github.com/mthomas100/local-voice
- https://github.com/kwindla/macos-local-voice-agents
- https://github.com/shubhdotai/offline-voice-ai
- https://developers.telnyx.com/docs/voice/programmable-voice/media-streaming
