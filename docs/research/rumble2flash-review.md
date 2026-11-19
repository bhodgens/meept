# Review: Reson8/Rumble-2-Flash as an additive STT option on socrates' RTX 3060

**Date:** 2026-11-18 · **Status:** research only — no code changes, no commits
**Question:** should `Reson8/Rumble-2-Flash` be added as an opt-in STT provider on socrates, and does it beat Kyutai STT or the current whisper.cpp path where it matters (time-to-final, accuracy, license, integration, risk)?

> **Citation style.** Every external claim carries an inline link to the primary page actually fetched for this review. All latency/WER numbers are vendor-, paper-, or leaderboard-reported; **none were reproduced on socrates or on meept's audio**. Compare within a row, not across rows. Local facts (GPU state, code, config) were read directly from the worktree / host and are marked as such.

---

## Verdict

**Conditional.** Rumble-2-Flash is worth piloting **additively** as an *accuracy*-focused, Apache-2.0 STT provider — **not** as the fix for the reported `heard` lag.

- **It does not reduce the dominant latency term.** The model card documents only whole-audio `generate()` and `chat.completions` calls ([HF card](https://huggingface.co/Reson8/Rumble-2-Flash)); there is no documented native streaming API, and Reson8 says live-conversation transcription and end-of-turn detection are reserved for a *bigger* model ([Reson8 blog](https://www.reson8.dev/blog/rumble-2-flash)). Its win is a **warm vLLM server** (kills the ~0.5–2 s cold `whisper-cli` subprocess) plus much better WER. It cannot emit partials, so it cannot feed `voice.context_warm` or partial-driven barge-in.
- **Kyutai STT 1B is the better latency engine**: native streaming, words out 500 ms after being spoken, semantic VAD, and a ~125 ms flush trick ([Kyutai](https://kyutai.org/stt/)).
- **VRAM is feasible but tight**: qwen3-tts is confirmed on the same RTX 3060 (6.6 GB used, ~5.6 GB free). Rumble at ~1 GB BF16 weights plus KV/activations fits, but vLLM's default `--gpu-memory-utilization 0.92` would try to take ~11.3 GB of the 12 GB card and must be explicitly overridden ([vLLM engine args](https://docs.vllm.ai/en/stable/configuration/engine_args/)).
- **License is the cleanest of any candidate reviewed**: Apache-2.0 weights ([HF card](https://huggingface.co/Reson8/Rumble-2-Flash)), Apache-2.0 base architecture (Qwen3-ASR), Apache-2.0 serving stack (vLLM).

**Recommended next step:** keep the existing whisper path as default; do the Phase-1 whisper tuning + baseline instrumentation from [the prior STT research](realtime-stt-options.md) first. Then, if accuracy on meept's own prompts is the goal, pilot Rumble behind a new opt-in provider via vLLM `/v1/audio/transcriptions` with a low explicit GPU-memory cap, measuring both time-to-`heard` and WER against `whisper base.en` and Kyutai — with qwen3-tts resident. If time-to-final is the goal, pilot Kyutai instead.

---

## 1. What Rumble-2-Flash actually is

| Property | Finding | Source |
|---|---|---|
| License | Apache-2.0 (weights and code) | [HF card](https://huggingface.co/Reson8/Rumble-2-Flash), [HF announcement](https://huggingface.co/blog/rumble-2-flash) |
| Size / precision | 0.5B params, BF16 safetensors; "Model size 0.5B params" | [HF card](https://huggingface.co/Reson8/Rumble-2-Flash) |
| Architecture | Qwen3-ASR architecture for day-0 Transformers/vLLM support, **no `trust_remote_code`**; distilled from Rumble-2 with on-policy distillation (GOLD) | [HF announcement](https://huggingface.co/blog/rumble-2-flash) |
| Languages | 40+ with automatic language detection; multilingual WERs published for DE/ES/FR/IT/NL/PT/HI/ZH | [HF announcement](https://huggingface.co/blog/rumble-2-flash), [HF card](https://huggingface.co/Reson8/Rumble-2-Flash) |
| Serving | `AutoProcessor` / `AutoModelForMultimodalLM`; `vllm serve Reson8/Rumble-2-Flash --max-model-len 4096`; OpenAI-compatible `chat.completions` with `audio_url`; `chat_template_kwargs` for `language`, `digits`, `filler`, `verbatim` | [HF card](https://huggingface.co/Reson8/Rumble-2-Flash) |
| Accuracy | Open ASR Leaderboard 18-11-2026 (v1.16.1, H200 + vLLM): **English avg WER 6.60, RTFx 6338**; multilingual avg 6.96% across 9 languages; AppTek 302 h call-centre (14 accents) 7.86% WER vs Nemotron-3.5-Transcribe 30B 7.78%, Qwen3-ASR-0.6B 8.7%, Qwen3-ASR-1.7B 7.4% | [HF card](https://huggingface.co/Reson8/Rumble-2-Flash), [HF announcement](https://huggingface.co/blog/rumble-2-flash) |
| Throughput | "an hour of audio in about half a second, with over 7,000 RTFx on full batches" — **batch** throughput, not interactive latency | [HF announcement](https://huggingface.co/blog/rumble-2-flash) |
| Realtime claims | Baseten's hosted realtime endpoint keeps p90 <1 s across up to 400 streams — that is **Baseten's Realtime Transcription API**, not an open-weights streaming protocol | [Baseten](https://www.baseten.co/library/rumble-2-flash/) |
| Hallucination | "Silence stays silent"; markedly less phantom text on music/background noise than Qwen3-ASR — relevant to false-turn suppression | [Baseten](https://www.baseten.co/library/rumble-2-flash/) |
| Not included | No speaker diarization and no end-of-turn detection: "Telling speakers apart and transcribing live conversations will be part of a bigger model" | [Reson8 blog](https://www.reson8.dev/blog/rumble-2-flash) |

### Streaming: honest assessment

The HF page shows two call shapes only: `model.generate(**inputs, ...)` and `client.chat.completions.create(...)` with a base64 `audio_url`. Both are **whole-audio request/response** ([HF card](https://huggingface.co/Reson8/Rumble-2-Flash)). There is no documented streaming endpoint, no partial-transcript callback, and no `FlushNow`-style force-finalize.

Two caveats worth stating explicitly rather than overclaiming:

1. **The architecture is not inherently non-streamable.** Qwen3-ASR's AuT encoder uses a dynamic flash-attention window (1–8 s) that "allows Qwen3-ASR to perform both streaming inference with short chunks and offline inference with long queries," and upstream Qwen3-ASR ships a vLLM-only Python streaming API using 2 s chunks with a 5-token rollback ([Qwen3-ASR report](https://arxiv.org/html/2601.21337), [Qwen3-ASR repo](https://github.com/QwenLM/Qwen3-ASR)). But that path re-feeds **all** accumulated audio every chunk (O(n²)) and disables batching and timestamps, and it is **not documented or supported for this distilled checkpoint**. For meept's 2–5 s local utterances, a 2 s chunk stride would not beat the current 500 ms endpointing.
2. **vLLM's own `/v1/realtime` WebSocket is not a drop-in here.** It requires architecturally causal models (the documented example is Voxtral); Qwen3-ASR's encoder uses bidirectional attention within windows ([vLLM realtime blog](https://vllm.ai/blog/2026-01-31-streaming-realtime), [Qwen3-ASR MODEL.md](https://github.com/antirez/qwen-asr/blob/main/MODEL.md)). vLLM's `stream=True` on `/v1/audio/transcriptions` streams **output tokens after the full audio is received** — it is a vLLM extension, not incremental input ([vLLM STT docs](https://docs.vllm.ai/en/latest/serving/online_serving/speech_to_text/)).

So the fair statement is: **Rumble-2-Flash is batch/non-streaming as documented and supported; a streaming path exists only in upstream Qwen3-ASR's Python API and is slower than the endpointing policy meept already uses.** Judge it on accuracy and warm-server latency, not on time-to-first-word.

---

## 2. socrates GPU reality (read-only, verified 2026-11-18)

Read via `ssh caimlas@socrates nvidia-smi` and `/proc/<pid>/{cmdline,cwd,environ}` — no service was restarted or stopped:

| GPU | Device | Used | Total | Resident process |
|---|---|---|---|---|
| 0 | Tesla PG500-216 | 28,854 MiB | 32,768 MiB | PID 2177463 `llama-server` (BTL-4-Q4_K_M, port 8082) |
| 1 | GeForce RTX 3060 | 6,665 MiB | 12,288 MiB | PID 1548051 `.venv/bin/python server.py --port 8001`, cwd `/home/caimlas/git/qwen3-tts-server`, `CUDA_VISIBLE_DEVICES=1` |

**Correction to the task context:** qwen3-tts is **on the RTX 3060 (GPU 1), not the Tesla**. Any STT placed on the 3060 will **share the card with qwen3-tts**; only ~5.6 GB is free and utilization was 0%.

### VRAM estimate for Rumble on the 3060

- 0.5B BF16 weights ≈ **1.0–1.5 GB** (0.5B × 2 bytes plus overhead); an independent third-party GGUF conversion puts `AD-Q5_K_M + mmproj Q8_0` at **0.50 GB** total ([AtomicChat GGUF card](https://huggingface.co/AtomicChat/Rumble-2-Flash-GGUF)).
- KV cache at `--max-model-len 4096`: Qwen3-ASR-0.6B-class decoder uses GQA (8 KV heads × 128 dim) over 28 layers → ~0.11 MiB/token → **~450 MiB per 4096-token sequence**; Rumble's distilled 14-layer decoder would be roughly half that ([antirez MODEL.md](https://github.com/antirez/qwen-asr/blob/main/MODEL.md), [AtomicChat GGUF card](https://huggingface.co/AtomicChat/Rumble-2-Flash-GGUF)).
- Add CUDA context (~0.5 GB), vLLM/audio-encoder workspace and framework overhead (~0.5–1 GB).
- **Realistic single-stream footprint: ~2.5–4 GB**, so it *fits* in 5.6 GB free — but with little headroom if qwen3-tts grows. vLLM's default `--gpu-memory-utilization 0.92` is a fraction of **total** GPU memory (~11.3 GB of 12 GB) and would fail or crowd out qwen3-tts; set it explicitly low (e.g. `0.20–0.25`) and/or use `--kv-cache-memory-bytes` ([vLLM engine args](https://docs.vllm.ai/en/stable/configuration/engine_args/)).

---

## 3. Comparison table

| Dimension | whisper.cpp (current) | Rumble-2-Flash (vLLM) | Kyutai STT 1B |
|---|---|---|---|
| **Streaming / partials** | None; one `Final:true` per utterance, no token callback ([code](../../internal/voicechild/providers/stt_whisper.go)) | **None documented**; whole-audio `generate()` / `chat.completions` ([HF card](https://huggingface.co/Reson8/Rumble-2-Flash)); upstream Qwen3-ASR has a slower vLLM-only 2 s-chunk path ([Qwen repo](https://github.com/QwenLM/Qwen3-ASR)) | **Native streaming**; words out 500 ms after spoken ([Kyutai](https://kyutai.org/stt/)) |
| **Endpointing / wait** | 500 ms silence + 500 ms wall-idle + 900 ms `turn_confirm_ms` + cold subprocess ([code](../../internal/voicechild/providers/stt_whisper.go), [config](../../config/meept.json5)) | Still needs the same external endpoint decision; warm server removes the cold start (~0.5–2 s). `stream=True` emits tokens only after full audio ([vLLM STT docs](https://docs.vllm.ai/en/latest/serving/online_serving/speech_to_text/)) | **Semantic VAD** replaces fixed timers (Rust server only); flush trick ≈125 ms tail after end-of-speech ([Kyutai](https://kyutai.org/stt/)) |
| **Expected effect on time-to-`heard`** | Baseline (reported lagging by seconds) | **Modest**: saves cold-start only; does not shorten the ~1.4 s of deliberate waiting. No partials → no `context_warm`/partial barge-in | **Largest**: removes fixed waits and shortens the tail; enables partials |
| **Accuracy** | whisper base.en ≈11.6% WER on one bench (prior research) | **6.60% English avg**, 6.96% multilingual, 7.86% on noisy call-centre audio ([HF card](https://huggingface.co/Reson8/Rumble-2-Flash), [HF announcement](https://huggingface.co/blog/rumble-2-flash)) | No official 1B WER; 2.6B ≈6.4% short-form / 7.9% long-form at 2.5 s delay (prior research) |
| **Languages** | en (base.en path) | **40+**, auto-detect ([HF announcement](https://huggingface.co/blog/rumble-2-flash)) | **en/fr only** for 1B ([HF card](https://huggingface.co/kyutai/stt-1b-en_fr)) |
| **License** | MIT code + MIT weights | **Apache-2.0** ([HF card](https://huggingface.co/Reson8/Rumble-2-Flash)) | Weights **CC BY 4.0** ([HF card](https://huggingface.co/kyutai/stt-1b-en_fr)) |
| **Footprint** | ~150 MB model + subprocess | ~2.5–4 GB with vLLM + KV (est.); **shares the 3060 with qwen3-tts** | ~1–2 GB BF16 (1B) ([HF card](https://huggingface.co/kyutai/stt-1b-en_fr)) |
| **Integration into `STTProvider` seam** | Existing | **Low-medium**: new `NewSTT` vendor + HTTP client to `/v1/audio/transcriptions` or `chat.completions`; reuse `stt_base_url`/`stt_model`/`stt_vendor`; keyless. Needs a new supervised vLLM sidecar (spawn-guard conventions) and an explicit GPU-memory cap | **Medium**: Go WS client to Rust `moshi-server`, mirroring the Deepgram adapter; semantic VAD needs the Rust server |
| **Operational risk** | Low, mature | **Medium-high**: brand-new model (Nov 2026), heavy Python/CUDA dep new to meept, OOM risk beside qwen3-tts, no streaming, batch benchmarks ≠ interactive latency | Medium: CC BY attribution, Rust server to supervise, en/fr only |
| **Verdict for the lag** | Fix policy first | **Does not fix it** (accuracy/warm-start only) | **Best direct fix** |

---

## 4. Integration notes for this repo

- The seam is small and additive: `voicechild.STTProvider.Stream(ctx, <-chan []byte) (<-chan Transcript, error)`, optional `sttFlusher.FlushNow()`, `providers.NewSTT` vendor dispatch, factories injected via `SetProviderFactories` in `cmd/meept-voice/main.go`, and `ProviderConfig{Vendor, Model, BaseURL, ...}` carried on the C1 keys frame ([pipeline.go](../../internal/voicechild/pipeline.go), [providers_wiring.go](../../internal/voicechild/providers_wiring.go), [stt_deepgram.go](../../internal/voicechild/providers/stt_deepgram.go)).
- A Rumble adapter would emit **one `Final:true` per HTTP response** and no partials — the existing `context_warm` and partial-barge-in lanes stay unchanged. `stt_base_url` is currently documented as a "Deepgram-compatible endpoint" ([config](../../config/meept.json5)); it would need to be generalized to an OpenAI-compatible transcription endpoint. Keep it opt-in (`stt_provider` default `whisper`, byte-identical boot).
- The local vLLM server must follow meept's runtime spawn-guard / orphan-sweep conventions; warm-up must be off the first-turn path. `--gpu-memory-utilization` must be set explicitly because qwen3-tts already occupies the card.
- **Alternative with lower integration cost:** a third-party GGUF conversion runs Rumble through **llama-server's** `/v1/audio/transcriptions` and `/v1/chat/completions` with `chat_template_kwargs`, and meept already depends on llama.cpp ([AtomicChat GGUF card](https://huggingface.co/AtomicChat/Rumble-2-Flash-GGUF)). But stock llama.cpp (b12521) mishandles trailing zero-padding and can append words never spoken; the fix was pending upstream, and the files come from a third party. Still batch/non-streaming. Worth benchmarking, not shipping blind.
- WER figures come from H200 / RTX PRO 6000-class hardware with full batches; **RTFx 6338–8000 is not an interactive-latency number** on a 3060 sharing a GPU ([HF card](https://huggingface.co/Reson8/Rumble-2-Flash), [Baseten](https://www.baseten.co/library/rumble-2-flash/)).

---

## 5. Risks and open questions

1. **It targets the wrong bottleneck.** The reported defect is seconds of lag dominated by policy + cold process; Rumble removes the cold process but not the ~1.4 s of silence/confirm waiting ([prior research](realtime-stt-options.md)).
2. **VRAM coexistence is unproven.** qwen3-tts is on the 3060 and may grow; no load test was run. vLLM's default memory fraction is unsafe here.
3. **Maturity.** Released 18 Nov 2026 with thin community validation; the third-party GGUF path has a known padding defect pending upstream at review time.
4. **No diarization/end-of-turn** in Flash — acceptable for the single-speaker local leg, but note this is the feature the *bigger* Rumble model adds.
5. **Not verified on meept hardware/audio.** Every WER/latency number above is reported by the vendor, leaderboard, or a third party; the recommended pilot is what would establish socrates-specific truth.

---

## Sources

- [Reson8/Rumble-2-Flash model card](https://huggingface.co/Reson8/Rumble-2-Flash) — license, size, API shapes, leaderboard tables, transcription options.
- [Rumble-2-Flash: an open 0.5B speech model for 40+ languages (HF blog)](https://huggingface.co/blog/rumble-2-flash) — architecture, distillation, multilingual/AppTek results, throughput.
- [Meet Rumble-2-Flash (Reson8 blog)](https://www.reson8.dev/blog/rumble-2-flash) — 40+ languages, per-dataset WER, features deferred to the bigger model.
- [Baseten Rumble 2 Flash](https://www.baseten.co/library/rumble-2-flash/) — hosted realtime claims, hallucination resistance.
- [AtomicChat Rumble-2-Flash-GGUF](https://huggingface.co/AtomicChat/Rumble-2-Flash-GGUF) — GGUF sizes, llama.cpp endpoints, architecture details, padding bug.
- [vLLM Speech-to-Text APIs](https://docs.vllm.ai/en/latest/serving/online_serving/speech_to_text/) — transcriptions streaming semantics, Realtime API protocol.
- [Streaming Requests & Realtime API in vLLM](https://vllm.ai/blog/2026-01-31-streaming-realtime) — causal-architecture requirement for true streaming.
- [vLLM Engine Arguments](https://docs.vllm.ai/en/stable/configuration/engine_args/) — `--gpu-memory-utilization` default 0.92 and fraction-of-total semantics.
- [Kyutai STT](https://kyutai.org/stt/) — streaming, 500 ms delay, semantic VAD, ~125 ms flush trick, throughput.
- [Kyutai STT 1B model card](https://huggingface.co/kyutai/stt-1b-en_fr) — architecture, languages, CC BY 4.0.
- [Qwen3-ASR Technical Report](https://arxiv.org/html/2601.21337) — AuT dynamic attention windows, streaming support, 2 s-chunk eval.
- [QwenLM/Qwen3-ASR repo](https://github.com/QwenLM/Qwen3-ASR) — vLLM-only streaming API constraints.
- [antirez/qwen-asr MODEL.md](https://github.com/antirez/qwen-asr/blob/main/MODEL.md) — encoder/decoder layer counts, KV-relevant architecture.
- Local, read-only: `nvidia-smi` + `/proc` on `caimlas@socrates`; [stt_whisper.go](../../internal/voicechild/providers/stt_whisper.go), [pipeline.go](../../internal/voicechild/pipeline.go), [providers_wiring.go](../../internal/voicechild/providers_wiring.go), [config/meept.json5](../../config/meept.json5), [prior STT research](realtime-stt-options.md).
