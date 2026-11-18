# Research: realtime / local STT options for meept's voice local leg

**Date:** 2026-11-18 · **Status:** research only, no code changes · **Audience:** voice local-leg owner

**Question:** the operator reports that the `heard` line lags speech by multiple seconds on the
local leg. What can reduce **time-to-final-transcript** — now and with a newer engine — on the
M2 Ultra 64 GB Mac (primary) and the Linux "socrates" server, without giving up the text-transcript
architecture, tool calling, or commercial-safe licensing?

---

## TL;DR — ranked recommendation

1. **Fix the current whisper.cpp adapter before swapping models.** The lag is dominated by
   policy + process overhead, not Whisper inference: a fresh `whisper-cli` subprocess per
   utterance (cold model load ~0.5–2 s), a fixed ~500 ms trailing-silence/wall-idle wait, and a
   900 ms `turn_confirm_ms` aggregation window — roughly **1.4 s of deliberate waiting plus a cold
   start** before `heard` can fire. A warm/persistent recognizer, VAD-driven endpointing (~200 ms),
   `-ac` context clamping, and a shorter confirm window are the highest-latency-per-effort changes
   and fit the existing `stt_whisper.go` adapter.
2. **Moonshine v2 streaming** — best STT *swap*: MIT code **and** models, 34–245 M params,
   native streaming with ~80 ms lookahead, reported ~13.5 ms TTFT / ~50–150 ms end-to-end on an
   M3, and one library spanning macOS/iOS/Linux/Windows + ONNX/C API. Closest drop-in replacement
   for whisper.cpp's role.
3. **Kyutai STT 1B** — best *semantic endpointing* story: native streaming with a 500 ms delay,
   built-in semantic VAD (replaces both the fixed silence wait and the fixed confirm window), the
   "flush trick" (~125 ms tail after end-of-speech), official MLX for the Mac and a production Rust
   WebSocket server for socrates. Weights CC BY 4.0 / code MIT+Apache-2.0. English/French only.
4. **Parakeet TDT v3 / Nemotron-Streaming via NeMo-Speech.cpp** — best accuracy (WER ~1.9% LS-clean
   for v3, CC BY 4.0, ggml C++ runtime with Metal backend and a realtime WS endpoint); but v2/v3 are
   **offline** models — true low latency needs the newer Nemotron-3.5-Streaming variant.
5. **Voxtral Realtime 4B** — Apache-2.0, native streaming at configurable <200 ms delay, already
   supported by llama.cpp (`/v1/realtime`) and MLX; heavier (~2.6–5.5 GB) but the cleanest license
   and the closest fit to meept's existing llama.cpp dependency.
6. **Not recommended now:** SenseVoice (explicitly **not** streaming; custom weight license),
   whisper-streaming / SimulStreaming (Python, ~3 s or GPU-bound, high integration cost, commercial
   questionnaire), faster-whisper (batch-only, CPU-only on Mac — use its Silero VAD *idea*, not the
   stack), and full speech-to-speech models (Moshi/PersonaPlex/Qwen3-Omni: forfeit the text
   transcript source, tools, and supervisor gate).

> All latency/WER figures are **vendor-, paper-, or benchmark-reported**, not measured on meept's
> hardware or datasets. Benchmarks use different chips and corpora — compare within a row, not
> across rows. Items flagged *reported* were not verified line-by-line against the primary page.

---

## 1. Why `heard` is slow: latency decomposition of the current path

Ground truth from the worktree (no code changed):

- `internal/voicechild/providers/stt_whisper.go`: `Stream` buffers PCM and calls `recognize()`
  only on (a) `whisperSilenceMs = 500` ms trailing silence, (b) `whisperWallIdleMs = 500` ms wall
  idle, (c) `whisperMaxWindowS = 10` s cap, (d) EOF, or (e) a barge-in `FlushNow` kick.
- `recognizeOne()` does `exec.CommandContext(... whisper-cli -m <model> -f <wav> -nt)` — a
  **new process per utterance**, including model load, with a 15 s per-segment timeout. No
  partial transcripts are emitted (`Transcript{Text, Final:true}` only).
- `internal/voice/AGENTS.md` (Frames C1): only `last:true` finals start a turn; consecutive finals
  inside `voice.turn_confirm_ms` (default **900 ms**) are aggregated into one turn before the
  daemon's talk loop dispatches. The `heard` telemetry event is appended in
  `VoiceWiring.routeFrame` when the FINAL prompt frame arrives — i.e. **after all of the above**.

So the felt delay ≈

```
  500 ms silence/wall-idle  +  cold whisper-cli model load (0.5–2 s, reported)
+ Whisper encode/decode      +  900 ms turn_confirm_ms
→ final frame → `heard`
```

Key consequences: (1) no amount of model swapping removes the ~1.4 s of deliberate waiting or the
cold subprocess; (2) whisper.cpp has **no token-level callback**, so the existing
partial-context-warm path cannot get true token partials from it; (3) the same fixed waits are
also why barge-in and end-of-turn feel sluggish. Prior internal research
(`docs/research/2026-10-28-uptractor-voice-stt-tts-apple-silicon.md`) picked models for accuracy,
not time-to-final — this report fills that gap.

---

## 2. Evaluation criteria

| Criterion | Why it matters here |
|---|---|
| **Time-to-final / first-token latency** | The reported defect. Prefer native streaming + semantic endpointing over batch throughput. |
| **License (code + weights)** | Prior uptractor research set a "commercial-safe" posture (product may become a managed service). MIT/Apache-2.0 ideal; CC BY 4.0 acceptable with attribution; CC BY-NC / custom "reference only" licenses are blockers. |
| **Local / self-host** | Local leg is keyless and carrier-free; no cloud dependency acceptable for the local path. |
| **Mac M2 Ultra 64 GB + Linux socrates** | Primary runtime is Apple Silicon; socrates is a Linux server. MLX/CoreML/ANE path for Mac, CPU or CUDA for Linux. |
| **Integration effort into *this* repo** | The child exposes a tiny seam: `voicechild.STTProvider.Stream(ctx, <-chan []byte) (<-chan Transcript, error)` with `Transcript{Text, Final}`, plus optional `FlushNow()`; vendors dispatch in `providers.NewSTT`, factories are injected in `cmd/meept-voice/main.go` via `SetProviderFactories`. A `keys` C1 frame carries vendor/model/base-url per call. |

---

## 3. Ranked options — detail

### 3.1 [Tier 1, do first] Tune the existing whisper.cpp adapter — no new model

This is not a new engine; it is the largest available latency win at the smallest risk, and it
keeps the Go subprocess contract intact.

| Lever | Finding | Source |
|---|---|---|
| **Warm process (biggest win)** | For 3–5 s snippets ~50% of time is model load; reloading small models costs 2–4 s/utterance on CPU; one measured 7.6 s sentence was 2.26 s cold vs 1.17 s warm. `whisper-server` keeps the model loaded and accepts WAV on `/inference` (`response_format=json\|text`); **`--host` accepts a `.sock` unix-socket path**, a natural fit for meept's loopback socket conventions. Alternative: Go cgo binding with a single `whisper.Context` reused (bindings exist; a state is not safe for concurrent use — serialize or use separate states). Warm up with 1 s of silence at startup: first-run Metal shader compile can take 15–17 s (~31 s for CoreML). | [server README](https://github.com/ggml-org/whisper.cpp/blob/master/examples/server/README.md), [PR #1375](https://github.com/ggerganov/whisper.cpp/pull/1375), [Go bindings](https://github.com/ggml-org/whisper.cpp/blob/0ccd896f/bindings/go/whisper.go), [#3309](https://github.com/ggml-org/whisper.cpp/discussions/3309), [FnFlow bench](https://fnflow.app/whisper-cpp-apple-silicon-benchmark) |
| **VAD endpointing (replace 500 ms silence wait)** | whisper.cpp `--vad` (Silero, merged 2025-05) is **offline preprocessing over an already-captured buffer** — it trims silence but cannot shorten the wait for the speaker to stop. Endpointing must be streaming, in meept's capture loop. Defaults: `--vad-threshold 0.50`, `--vad-min-speech-duration-ms 250`, `--vad-min-silence-duration-ms 100`, `--vad-speech-pad-ms 30`. Silero consumes 512 samples (32 ms @16 kHz); use `min_silence ≈150–250 ms`, pad 30–50 ms, threshold 0.5 with 0.15 hysteresis. Silero is ~686× realtime (effectively free). **meept already ships Silero VAD via jargo (`providers.NewSileroVAD`) for local-leg barge-in gating** — that is an existing seam to extend. | [CLI README](https://github.com/ggml-org/whisper.cpp/blob/master/examples/cli/README.md), [PR #3065](https://github.com/ggml-org/whisper.cpp/pull/3065), [VADIterator](https://github.com/ufal/whisper_streaming/blob/main/silero_vad_iterator.py), [parakeet.cpp VAD bench](https://github.com/mudler/parakeet.cpp/blob/master/docs/vad-benchmarks.md) |
| **`-ac` / `--audio-ctx`** | Encoder always processes a 30 s window; short audio is padded. `clamp(ceil(dur_s/30*1500)+128, 256, 1500)`: a 1.7 s fragment decoded in **221 ms at `audio_ctx=256` vs 1783 ms at 1500** (~8×). High-value for meept's short utterances. | [CLI README](https://github.com/ggml-org/whisper.cpp/blob/master/examples/cli/README.md), [Vocs analysis](https://dev.to/hackx-in/chunking-whisper-into-1-second-pieces-makes-dictation-slower-hmp) |
| **`-bs 1`, `-l en`, keep timestamps** | CLI default beam 5; greedy `-bs 1` cuts decode. Temperature fallback can multiply decode ~6×; `-nf` avoids it but **never combine with `-nt`** — without timestamp tokens the decoder has no stop signal and repeats the sentence (24× measured). Keep timestamps on. | [PR #291](https://github.com/ggml-org/whisper.cpp/pull/291), [Vocs analysis](https://dev.to/hackx-in/chunking-whisper-into-1-second-pieces-makes-dictation-slower-hmp) |
| **Model size (accuracy, not lag)** | whisper.cpp WER on one Mac mini: tiny 16.0%, base 11.6%, small 8.7%, medium 6.0%, large-v3 6.0%, large-v3-turbo 6.4%. Speed on M3 Ultra (Metal, 11 s clip): base 162 ms, small 310 ms, medium 714 ms, large-v3-turbo 623 ms. Turbo ≈15% *faster* than medium despite a bigger file. q8_0 matched f16 almost exactly and was faster cold; q5_0 was worse. **Do not expect a model swap to fix the lag.** | [TypeWhisper bench](https://typewhisper.github.io/typewhisper-benchmark/?models=whisper-cpp%3Atiny%2Cwhisper-cpp%3Abase%2Cwhisper-cpp%3Asmall%2Cwhisper-cpp%3Amedium%2Cwhisper-cpp%3Alarge-v3%2Cwhisper-cpp%3Alarge-v3-turbo&view=compare), [Mundwerk M3 Ultra](https://mundwerkapp.de/en/blog/whisper-benchmark-m3-ultra/), [FnFlow](https://fnflow.app/whisper-cpp-apple-silicon-benchmark) |
| **Streaming / partials** | No `--stream` flag and no per-token callback. `new_segment_callback` / `-pr` give **segment-granular** partials only; `whisper-stream` is a rolling SDL window (rewrite-in-place, pipe-unfriendly); `whisper-stream-pcm` (stdin PCM + VAD, PR #3653) is a better Go-subprocess shape — check it is in meept's pinned version. Expect ~1.6 s fixed per-chunk floor; don't chase 500 ms partials. | [stream README](https://github.com/ggml-org/whisper.cpp/blob/master/examples/stream/README.md), [whisper.h](https://github.com/ggml-org/whisper.cpp/blob/master/include/whisper.h), [PR #3653](https://github.com/ggml-org/whisper.cpp/pull/3653), [Breno Xavier](https://brenoxp.com/blog/real-time-whisper-on-device) |
| **`turn_confirm_ms` (policy)** | Reducing 900 ms once endpointing is VAD-driven can save up to ~700 ms, but is a product decision (breath-pause fragment aggregation). Per-employee override via `voice.employee_leg` row already exists — a safe A/B vehicle. | `config/meept.json5:1378`, `internal/voice/AGENTS.md` (Frames C1) |

**Integration effort:** low-to-medium — changes concentrated in `stt_whisper.go`, config template, and
a supervised sidecar if chosen (meept's spawn-guard conventions apply). **License:** whisper.cpp MIT /
OpenAI Whisper weights MIT — no issue. **Runs on:** both targets today.

---

### 3.2 [Tier 2a] Moonshine v2 streaming — best STT swap

| Field | Finding |
|---|---|
| **License** | **MIT code and MIT models**, explicitly "MIT by default ... in every language and at every size" — the only exceptions are *legacy non-streaming* non-English models (enumerated in LICENSE). Caveat: the GitHub repo's machine "License" badge reads "Other" — verify the exact streaming checkpoint's license file before shipping. |
| **Local / self-host** | Yes — fully on-device, "no account or API keys"; optimized for live streaming by working while the user is still talking. |
| **Streaming / latency** | Native sliding-window streaming with ~80 ms lookahead; **reported TTFT 13.5 ms (Tiny)** and ~50 / 148 / 258 ms end-to-end for Tiny/Small/Medium on an M3 ([paper](https://arxiv.org/html/2602.12241)). Autoregressive decoder → full-output latency grows with transcript length; English-optimized. |
| **Accuracy** | [Streaming cards](https://huggingface.co/moonshine-ai/moonshine-streaming-tiny): OpenASR 6.65 (245 M) / 7.84 (123 M) / 12.00 (34 M). Repo claims models "from higher accuracy than Whisper Large V3 down to tiny 1 MB models" (marketing claim). |
| **Hardware** | 34–245 M, <1 GB, 1 MB–250 MB models; CPU-capable. |
| **Apple / Linux** | **Best cross-platform story of any candidate:** one library across Python, JS/WASM, iOS, Android, **macOS**, Linux, Windows, Raspberry Pi; C API + native Swift MLX port (~2 ms/token reported). Runs on both M2 Ultra and socrates. |
| **Integration effort** | Medium — a C API exists, so a cgo binding paralleling whisper.cpp's Go binding is realistic behind the existing `STTProvider` seam (vendor dispatch `NewSTT` + factory in `cmd/meept-voice/main.go`). Native partials would unlock barge-in and context-warm without new plumbing. |

Sources: [repo](https://github.com/moonshine-ai/moonshine/) · [LICENSE](https://github.com/moonshine-ai/moonshine/blob/main/LICENSE) · [paper](https://arxiv.org/html/2602.12241) · [streaming-tiny card](https://huggingface.co/moonshine-ai/moonshine-streaming-tiny) · [MLX port](https://github.com/kylehowells/moonshine-mlx/)

---

### 3.3 [Tier 2b] Kyutai STT 1B — best semantic end-of-turn

| Field | Finding |
|---|---|
| **License** | Weights **CC BY 4.0** (commercial OK, attribution), code **MIT (Python) / Apache-2.0 (Rust)**. No known extra commercial tier. |
| **Local / self-host** | Yes — PyTorch (research), **Rust `moshi-server`** (production WS + batching, powers Unmute), **official MLX** (Mac/iPhone), HF Transformers ≥4.53. |
| **Streaming / latency** | Official site: 1B transcribes words **500 ms after spoken**; 2.6B has 2.5 s delay. Native semantic VAD predicts "done talking" from content/intonation — adapts the wait instead of a fixed silence timer (Rust server only). **Flush trick:** on VAD end-of-speech, reprocess buffered audio at ~4× realtime → 500 ms tail costs only **~125 ms**. H100: 400 concurrent streams; L40S: 64 at 3× realtime. *Reported* first-text compute 179 ms on MLX INT8 M5 Pro; ~1.0 s word→socket on an RTX 5090. |
| **Accuracy** | No official WER for the 1B. 2.6B @2.5 s: short-form ~6.4%, long-form ~7.9%, vs streaming Whisper large-v3 9.4% ([paper](https://arxiv.org/html/2509.08753)). |
| **Hardware** | 1B ≈ 1–2 GB (BF16 ~1.8 GB; 4-bit MLX ~1.1 GiB); GPU needed for realtime in PyTorch; CPU not realtime. |
| **Apple / Linux** | Official `stt-1b-en_fr-mlx` 4/8-bit checkpoints + mic/file streaming scripts; native Swift runtime exists; a 1.4 GiB / RTF 0.13 number is reported on M5 Pro — 64 GB M2 Ultra is comfortably above. Linux is first-class (Rust server + PyTorch + Docker). Caveat: MLX ports omit the semantic-VAD heads — use the Rust server on socrates for that feature. |
| **Integration effort** | Medium — a Go WebSocket client to the Rust server mirrors the existing Deepgram adapter pattern but local; use MLX on the Mac. 1B is **English/French only**. The semantic VAD is the direct replacement for the 500 ms silence + 900 ms confirm waits. |

Sources: [official STT page](https://kyutai.org/stt/) · [model card](https://huggingface.co/kyutai/stt-1b-en_fr) · [MLX card](https://huggingface.co/kyutai/stt-1b-en_fr-mlx) · [repo](https://github.com/kyutai-labs/delayed-streams-modeling) · [paper](https://arxiv.org/html/2509.08753)

---

### 3.4 [Tier 3] Parakeet TDT v3 / Nemotron-Streaming (NeMo-Speech.cpp)

| Field | Finding |
|---|---|
| **License** | Weights **CC BY 4.0** — card states "ready for commercial/non-commercial use" (attribution required); code Apache-2.0 (NeMo, NeMo-Speech.cpp). The NVIDIA NIM/NGC container uses a different Open Model License — that applies to the managed container, not self-hosted HF weights. |
| **Local / self-host** | Yes — NeMo Python, HF Transformers, official **NeMo-Speech.cpp** (ggml C++, Q8 GGUF 714 MB, HTTP `/v1/audio/transcriptions` + **realtime WS** + C SDK), sherpa-onnx. |
| **Streaming / latency** | ⚠️ **v2/v3 are offline full-attention models, NOT natively streaming**; NeMo-Speech.cpp explicitly rejects streaming for v3. Chunked inference `latency = chunk + right-context`; v3 default ≈ **4 s**, and WER collapses below ~2 s (LS-other 3.20% offline → 14.82% @0.96 s → 76.36% @0.48 s). For real low latency use **parakeet-unified-en-0.6b (160 ms min)** or **Nemotron-3.5-ASR-Streaming-0.6B (80 ms–1 s, cache-aware, *reported*)**. |
| **Accuracy** | v3 avg WER 6.34% / LS-clean 1.93% / LS-other 3.59%; NVIDIA paper v3 6.32% vs Whisper large-v3 7.44%; RTFx 3332 vs 145. Q8 GGUF ≈ no WER loss. |
| **Hardware** | 600 M, ~2 GB min; F32 2.51 GB / Q8 714 MB; CPU+int8 works but slow; GPU optimal. |
| **Apple / Linux** | **parakeet-mlx** (Apache-2.0, `transcribe_stream` partials, ~35× RTFx reported on M2 Air) and FluidAudio CoreML/ANE (~190× RTFx reported on M4 Pro); NeMo-Speech.cpp has a Metal backend. Linux is the primary tier. |
| **Integration effort** | Medium — NeMo-Speech.cpp is a C++ library with WS + C SDK, analogous to the whisper.cpp subprocess/sidecar pattern; but a streaming variant is needed for the latency goal. |

Sources: [v3 card](https://huggingface.co/nvidia/parakeet-tdt-0.6b-v3) · [v2 card](https://huggingface.co/nvidia/parakeet-tdt-0.6b-v2) · [NeMo-Speech.cpp](https://github.com/NVIDIA/NeMo-Speech.cpp) · [models doc](https://github.com/NVIDIA/NeMo-Speech.cpp/blob/main/docs/asr/models.md) · [parakeet-mlx](https://github.com/senstella/parakeet-mlx) · [Nemotron streaming card](https://huggingface.co/nvidia/nemotron-speech-streaming-en-0.6b)

---

### 3.5 [Tier 3] Voxtral Realtime 4B — Apache-2.0, llama.cpp-native streaming

| Field | Finding |
|---|---|
| **License** | **Apache-2.0** — the cleanest license in this list. |
| **Local / self-host** | Yes — MLX 4-bit (~2.6 GB), llama.cpp `/v1/realtime` websocket, ExecuTorch (Metal/CUDA). **meept already depends on llama.cpp**, so the serving surface is familiar. |
| **Streaming / latency** | Native streaming with configurable delay <200–2400 ms; reported to beat Whisper at 960 ms; En-Short WER 8.47 @480 ms ([paper](https://arxiv.org/html/2602.11298v2)). *Reported* llama.cpp stateful audio-stream support (PR #30629) and `/v1/realtime` endpoint (b11946) — verify against meept's pinned llama.cpp floor before relying on it. |
| **Accuracy / hardware / targets** | LS-clean 1.53–1.57 for the batch Voxtral Mini 3B; ~5.5 GB VRAM q8 / ~2.6 GB 4-bit. MLX on Mac + llama.cpp/ExecuTorch on both platforms; Linux primary. |
| **Integration effort** | Medium-high (heavier model, new realtime protocol), but Apache-2.0 + llama.cpp reuse make it the safest "true streaming, clean license" option if the footprint is acceptable. |

Sources: [paper](https://arxiv.org/html/2602.11298v2) · [weights](https://huggingface.co/mistralai/Voxtral-Mini-4B-Realtime-2602) · [llama.cpp PR #30629](https://github.com/ggml-org/llama.cpp/pull/30629) · [ExecuTorch example](https://github.com/pytorch/executorch/blob/main/examples/models/voxtral_realtime/README.md)

---

### 3.6 Considered, not recommended now

| Option | Why not |
|---|---|
| **SenseVoice-Small (FunASR)** | Fast whole-utterance inference (**<80 ms / RTF 0.007**, >5× Whisper-small, >15× Whisper-large) but the paper's Limitations section says verbatim **"SenseVoice is not designed for streaming transcription"** — encoder-only, non-autoregressive, no native partials; still needs a VAD/endpoint decision, so it does not fix the wait. English WER is worse than Whisper-large (LibriSpeech clean 3.15 vs 1.82; only Chinese wins). Weights use a custom FunASR Model License v1.1 ("for reference and learning purposes" disclaimer) with commercial status not formally settled. Good for offline re-transcription, not this latency fix. [paper](https://arxiv.org/abs/2407.04051) · [card](https://huggingface.co/FunAudioLLM/SenseVoiceSmall) |
| **whisper_streaming (UFAL, 2023)** | Genuine LocalAgreement partials, MIT end-to-end, explicit mlx-whisper backend (good Mac path), but **~3.3 s average latency** (1 s chunks, large-v2), no max-wait bound, and in maintenance mode. [repo](https://github.com/ufal/whisper_streaming) |
| **SimulStreaming (UFAL, 2025)** | AlignAtt policy, IWSLT 2025 SOTA, ~5× faster than WhisperStreaming; English ASR latency 494 ms–2.1 s. But **PyTorch-only upstream**, recommends ≥10 GB VRAM (CPU "too slow for real-time"), no official MLX/CoreML (Mac path only via WhisperLiveKit), and the authors circulate a **commercial-use questionnaire** despite an MIT LICENSE — resolve before shipping commercially. [repo](https://github.com/ufal/SimulStreaming) · [paper](https://arxiv.org/abs/2506.17077) |
| **faster-whisper + Silero VAD** | Both MIT; Silero is excellent and already partly in-repo. But faster-whisper's core is **batch/file-oriented (no native streaming)**, and on Apple Silicon it is CPU-only (`device="mps"` unsupported, no CoreML/ANE). Adopt the **VAD endpointing idea**, not the whole stack. [faster-whisper](https://github.com/SYSTRAN/faster-whisper) · [Silero VAD](https://github.com/snakers4/silero-vad) |
| **Full realtime speech-to-speech** (Moshi ~200 ms, PersonaPlex, Qwen3-Omni ~234 ms first audio, MiniCPM-o 4.5 <650 ms on M4 Max, `huggingface/speech-to-speech` OpenAI-Realtime-compatible local server) | These **replace the STT→LLM→TTS pipeline**. meept's architecture depends on a text transcript source ("TEXT stays the transcript source", `AGENTS.md`), the supervisor gate, tools, and per-sentence TTS; Moshi has no tool calling and a fixed voice. A much larger change with loss of current capabilities — park as a future architecture decision, not an STT swap. Revisit with `huggingface/speech-to-speech` if an OpenAI-Realtime-compatible local endpoint is ever wanted. [Moshi](https://github.com/kyutai-labs/moshi) · [Qwen3-Omni](https://github.com/QwenLM/Qwen3-Omni) · [hf s2s](https://github.com/huggingface/speech-to-speech/) |
| **Cloud-only** (OpenAI `gpt-realtime-mini` ~0.81 s TTFA, Seed-ASR, Lightning ASR) | No weights / not self-hostable; useful as latency benchmarks only. [gpt-realtime-mini](https://developers.openai.com/api/docs/models/gpt-realtime-mini) |

---

## 4. Cross-cutting integration notes for this repo

- **The seam is small.** Any new engine implements `voicechild.STTProvider.Stream(ctx, pcm) <-chan Transcript`; emit `Transcript{Text, Final:false}` for partials and `Final:true` for finals. `providers.NewSTT` dispatches on `cfg.Vendor`; add the vendor alongside `"deepgram"`/`"whisper"`; the factory is injected in `cmd/meept-voice/main.go` via `SetProviderFactories`. Keys are keyless for local engines — the `keys` C1 frame's `stt_key` can stay empty; reuse `stt_vendor`/`stt_model`/`stt_base_url` selectors (per-employee resolution via `voice.ResolveProfile`).
- **`FlushNow()` already exists** as the optional `sttFlusher` seam for barge-in. A streaming engine can implement it to force-finalize, and should keep whisper's discipline: never recognize an empty window, never inject silence.
- **Partials have downstream value.** Native partials (Moonshine/Kyutai/Voxtral) feed both the STT-partial barge-in trigger and `voice.context_warm` — today whisper.cpp cannot supply them. Note a policy consequence: the `heard` telemetry ring currently records **FINAL prompts only**; if operators want `heard` to reflect partials, that is a deliberate telemetry-policy change, not just an STT swap.
- **Licensing posture.** Prefer MIT/Apache-2.0 (Moonshine, Voxtral, whisper.cpp). CC BY 4.0 (Kyutai, Parakeet/NVIDIA) is acceptable with attribution. Avoid SenseVoice's custom license and resolve SimulStreaming's commercial questionnaire before shipping.
- **Endpointing belongs in the capture loop**, not inside the recognizer: whisper.cpp's `--vad` is offline trimming. A streaming Silero VAD (already in-repo via jargo) or Kyutai's semantic VAD is what actually removes the 500 ms fixed wait.
- **Warm-up must be off the first-turn path** (Metal/CoreML shader compile is 15–31 s on first run). A local engine sidecar must follow meept's runtime spawn-guard / orphan-sweep conventions.
- **Zero-config parity.** All changes should remain opt-in: the current `whisper` default and byte-identical behavior for existing deployments is the repo's invariant.

---

## 5. Phased plan (recommended order)

1. **Phase 0 — measure.** Add instrumentation for time-from-speech-end → `heard` on the local leg (the telemetry ring already carries `heard`; timestamp it). Establish the current baseline before tuning.
2. **Phase 1 — tune whisper.cpp (biggest win, lowest risk).** Warm recognizer (server sidecar with `.sock` or persistent cgo context) + startup silence warm-up; VAD-driven endpointing at ~200 ms; `-ac 256–512`, `-bs 1`, `-l en`, keep timestamps; reduce `turn_confirm_ms` behind a per-employee override. Validate against meept's own voice prompts (VAD changes output; q5_0 quantization was worse in one benchmark).
3. **Phase 2 — STT swap pilot.** Moonshine v2 streaming first (MIT, small, cross-platform); Kyutai STT 1B second if semantic VAD / en-fr quality is decisive. Compare time-to-`heard`, WER on meept prompts, and barge-in quality on both M2 Ultra and socrates.
4. **Phase 3 — optional heavy options.** Voxtral Realtime 4B (Apache-2.0, llama.cpp) or Nemotron-Streaming/Parakeet if accuracy demands it. Full s2s only as a separate architecture proposal.
5. **Before any commercial ship:** verify the exact checkpoint license for Moonshine (repo badge says "Other"), and resolve SimulStreaming's commercial-use questionnaire if that path is chosen.

---

## 6. Caveats / verification notes

- Most latency and WER numbers are third-party or vendor-reported; none were reproduced on meept's M2 Ultra or socrates. Benchmarks use different datasets/chips; compare only within a row.
- Several 2026 items (Voxtral Realtime llama.cpp PRs, Nemotron-3.5, Moonshine v2 paper) were gathered by web research and were **not** re-verified line-by-line against primary pages; spot-check before committing to a specific figure.
- Verified directly against primary sources during this task: Kyutai's 500 ms / 2.5 s delays, semantic VAD (Rust-only), ~125 ms flush trick and MLX support; SenseVoice's "not designed for streaming" limitation; SimulStreaming's MIT license and ~5× claim; Parakeet v3's CC BY 4.0 and offline full-attention design; whisper.cpp server's `.sock` host, VAD/`-ac` flags; Moonshine's README license statement.
- Moonshine's GitHub machine license field ("Other") conflicts with its README's "MIT by default" claim — treat as a checkpoint-level verification item.
- "socrates" hardware specifics were not available in-repo; Linux CPU vs CUDA GPU capability is stated generically. A CUDA-capable GPU is required for realtime Kyutai/Parakeet/SimulStreaming; small Moonshine and whisper.cpp are CPU-viable.

---

## 7. Source URLs

**Whisper.cpp tuning**
- https://github.com/ggml-org/whisper.cpp
- https://github.com/ggml-org/whisper.cpp/blob/master/examples/server/README.md
- https://github.com/ggml-org/whisper.cpp/blob/master/examples/cli/README.md
- https://github.com/ggml-org/whisper.cpp/blob/master/examples/stream/README.md
- https://github.com/ggml-org/whisper.cpp/blob/master/include/whisper.h
- https://github.com/ggml-org/whisper.cpp/blob/0ccd896f/bindings/go/whisper.go
- https://github.com/ggml-org/whisper.cpp/pull/3065 (Silero VAD)
- https://github.com/ggml-org/whisper.cpp/pull/3653 (stream-pcm)
- https://github.com/ggml-org/whisper.cpp/pull/291 (decode)
- https://github.com/ggerganov/whisper.cpp/pull/1375 (server warm process)
- https://github.com/ggml-org/whisper.cpp/discussions/3309
- https://github.com/snakers4/silero-vad
- https://github.com/ufal/whisper_streaming/blob/main/silero_vad_iterator.py
- https://github.com/mudler/parakeet.cpp/blob/master/docs/vad-benchmarks.md
- https://typewhisper.github.io/typewhisper-benchmark/?models=whisper-cpp%3Atiny%2Cwhisper-cpp%3Abase%2Cwhisper-cpp%3Asmall%2Cwhisper-cpp%3Amedium%2Cwhisper-cpp%3Alarge-v3%2Cwhisper-cpp%3Alarge-v3-turbo&view=compare
- https://mundwerkapp.de/en/blog/whisper-benchmark-m3-ultra/
- https://fnflow.app/whisper-cpp-apple-silicon-benchmark
- https://brenoxp.com/blog/real-time-whisper-on-device
- https://dev.to/hackx-in/chunking-whisper-into-1-second-pieces-makes-dictation-slower-hmp
- https://github.com/sk3llo/whisper_ggml/issues/26

**Streaming Whisper**
- https://github.com/ufal/whisper_streaming
- https://aclanthology.org/2023.ijcnlp-demo.3.pdf
- https://github.com/ufal/SimulStreaming
- https://arxiv.org/abs/2506.17077
- https://github.com/backspacetg/simul_whisper
- https://arxiv.org/abs/2406.10052
- https://github.com/QuentinFuxa/WhisperLiveKit

**SenseVoice / faster-whisper / Silero**
- https://github.com/FunAudioLLM/SenseVoice
- https://huggingface.co/FunAudioLLM/SenseVoiceSmall
- https://arxiv.org/abs/2407.04051
- https://github.com/modelscope/FunASR/blob/HEAD/MODEL_LICENSE
- https://github.com/FluidInference/FluidAudio
- https://github.com/SYSTRAN/faster-whisper
- https://github.com/SYSTRAN/faster-whisper/issues/911
- https://github.com/snakers4/silero-vad/blob/master/LICENSE
- https://github.com/snakers4/silero-vad/wiki/Performance-Metrics

**Parakeet / NeMo**
- https://huggingface.co/nvidia/parakeet-tdt-0.6b-v2
- https://huggingface.co/nvidia/parakeet-tdt-0.6b-v3
- https://github.com/NVIDIA-NeMo/Speech
- https://github.com/NVIDIA/NeMo-Speech.cpp
- https://github.com/NVIDIA/NeMo-Speech.cpp/blob/main/docs/asr/models.md
- https://github.com/NVIDIA/NeMo/pull/9106
- https://github.com/senstella/parakeet-mlx
- https://huggingface.co/nvidia/nemotron-speech-streaming-en-0.6b

**Kyutai**
- https://kyutai.org/stt/
- https://huggingface.co/kyutai/stt-1b-en_fr
- https://huggingface.co/kyutai/stt-1b-en_fr-mlx
- https://huggingface.co/kyutai/stt-2.6b-en
- https://github.com/kyutai-labs/delayed-streams-modeling
- https://arxiv.org/html/2509.08753
- https://github.com/kyutai-labs/delayed-streams-modeling/issues/136

**Moonshine / Voxtral / Qwen3-ASR / other new models**
- https://github.com/moonshine-ai/moonshine/
- https://github.com/moonshine-ai/moonshine/blob/main/LICENSE
- https://arxiv.org/html/2602.12241
- https://huggingface.co/moonshine-ai/moonshine-streaming-tiny
- https://github.com/kylehowells/moonshine-mlx/
- https://arxiv.org/html/2602.11298v2
- https://huggingface.co/mistralai/Voxtral-Mini-4B-Realtime-2602
- https://github.com/ggml-org/llama.cpp/pull/30629
- https://github.com/ggml-org/llama.cpp/releases/tag/b11946
- https://github.com/QwenLM/Qwen3-ASR/
- https://huggingface.co/FunAudioLLM/Fun-ASR-Nano-2512
- https://huggingface.co/ibm-granite/granite-speech-4.1-2b-nar

**Realtime speech-to-speech**
- https://github.com/kyutai-labs/moshi
- https://kyutai.org/Moshi.pdf
- https://github.com/NVIDIA/personaplex
- https://github.com/QwenLM/Qwen3-Omni
- https://github.com/vllm-project/vllm-omni/blob/main/docs/serving/full_duplex_api.md
- https://huggingface.co/openbmb/MiniCPM-o-4_5
- https://github.com/tc-mb/llama.cpp-omni/
- https://github.com/huggingface/speech-to-speech/
- https://developers.openai.com/api/docs/models/gpt-realtime-mini
