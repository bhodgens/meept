# Review: NVIDIA Nemotron 3.5 ASR / Nemotron-Streaming as an additive English STT option on socrates' RTX 3060

**Date:** 2026-11-18 · **Status:** research only — no code changes, no commits
**Question:** should the model family prior research called the "Nemotron-3.5-Streaming variant" be added as an opt-in English STT provider on socrates, and where does it rank for English against whisper.cpp base.en, Rumble-2-Flash, Qwen3-ASR-0.6B and Kyutai STT 1B?

> **Citation style.** Every external claim carries an inline numbered citation `[n]` resolved in **Sources** at the end, and major claims also carry a link to the primary page actually fetched for this review. All WER / RTFx / latency / throughput numbers are **vendor-, paper-, leaderboard-, or benchmark-reported**; **none were reproduced on socrates or on meept's audio**. Benchmarks use different corpora, hardware and normalization — compare **within a row**, not across rows. Local facts (GPU state, code, config) were read directly from the worktree/host and are marked as such.

---

## Verdict

**Yes — add it as an additive, opt-in English STT provider, behind the existing `STTProvider` seam, as the *second* latency-focused option after Kyutai.** It is the only candidate besides Kyutai with **true incremental streaming** (cache-aware, non-overlapping chunks — not Qwen's O(n²) re-feed and not Rumble's no-streaming batch path), and it packages that in a **tiny ~0.7–1.5 GB footprint** with a **purpose-built realtime WebSocket that already emits partials and accepts a buffer-clear** — both of which map directly onto meept's `Transcript{Final}` / `FlushNow()` contract. It fits the RTX 3060's ~5.6 GB free alongside qwen3-tts with far more headroom than the vLLM-based Rumble/Qwen options, and its runtime (NeMo-Speech.cpp, Apache-2.0) is a single warm C++ process with no vLLM GPU-fraction tuning.

**Ranking for English on this hardware (overall, weighted toward the reported time-to-final defect):**

1. **Kyutai STT 1B** — still the direct fix for the lag (native streaming + *semantic* VAD replaces both fixed waits); weaknesses: no official 1B WER, worst raw accuracy on one independent bench, CC BY 4.0, en/fr only.
2. **Nemotron (this review)** — **best balance of latency, footprint, license and integration cleanliness.** True incremental streaming down to 80 ms, partials + clear over a loopback WebSocket, ~0.7–1.5 GB, commercial-open weights, Apache-2.0 runtime. Main gaps: no built-in *semantic* end-of-utterance (use Silero VAD endpointing), and only middling English accuracy on the one independent dictation bench.
3. **Rumble-2-Flash** — best raw English accuracy on that same bench (4.8%), Apache-2.0, warm server; **no streaming**, ~2.5–4 GB.
4. **Qwen3-ASR-0.6B** — accuracy peer of Rumble (5.1%), Apache-2.0; "streaming" is 2 s-chunk O(n²) re-feed and slower than meept's current 500 ms endpointing, ~2.5–4 GB.
5. **whisper.cpp base.en (current)** — keep as default; 11.6% on the same bench, no partials, cold subprocess. Fix its tuning (Phase 1 of prior research) before any swap.

**Important English nuance:** NVIDIA itself recommends the **English-only sibling** `nvidia/nemotron-speech-streaming-en-0.6b` ("Nemotron 3 ASR") for English-only use, not the 40-locale `nemotron-3.5-asr-streaming-0.6b`. Both share the same architecture, runtime and permissive commercial-open license; for meept the **English-only checkpoint is the better English choice**. This review covers both and disambiguates them from the unrelated offline 30B "Nemotron 3.5 Transcribe" (§1.1).

**Recommended next step:** pilot the English-only checkpoint via `nemo-speech serve` (Q8 GGUF, realtime WebSocket) behind a new opt-in vendor, measure time-to-`heard` and WER against whisper base.en and Kyutai **with qwen3-tts resident**, and compare `endpointing_ms`/Silero settings. Do not switch the default: byte-identical boot with `stt_provider = whisper` is the invariant.

---

## 1. What the model actually is — three similarly-named things

Prior research (`realtime-stt-options.md` §3.4) referred to a "Nemotron-3.5-Streaming variant" and, in the Rumble review, to "Nemotron-3.5-Transcribe 30B". These are **three distinct models**; only the first two are relevant to an additive English STT slot:

| Model id | Role | Params | License (weights) | Released | Streaming? |
|---|---|---|---|---|---|
| `nvidia/nemotron-3.5-asr-streaming-0.6b` | multilingual extension, 40 locales, language-ID prompt | 600M | **OpenMDW-1.1** `[1][6]` | 2026-06-04 `[1]` | **Yes, true incremental** |
| `nvidia/nemotron-speech-streaming-en-0.6b` | **English-only** predecessor ("Nemotron 3 ASR") | 600M | **NVIDIA Open Model License** `[2]` | 2026-03-13 `[2]` | **Yes, true incremental** |
| `nvidia/NVIDIA-Nemotron-3.5-Transcribe-30B-A4B` | offline speech-LLM transcription/diarization/translation | 30B MoE (~4.3B active) | OpenMDW-1.1 `[11]` | 2026-10-28 `[11]` | **No** — offline, up to 1 h recordings |

The 30B "Transcribe" model is **not a candidate here**: it is an offline, promptable speech LLM (hybrid Mamba–Transformer MoE) that needs a large GPU, does not stream, and is the model the Rumble review compared against on call-centre WER. It cannot reduce time-to-`heard`. The subject of this review is the **0.6B streaming family** — specifically the 3.5 multilingual checkpoint the task names, with the English-only checkpoint noted as the recommended English variant.

### 1.1 Architecture, params, languages

- **FastConformer-CacheAware-RNNT**: 24-layer cache-aware streaming FastConformer encoder (8× downsampling) + RNN-T decoder; 600M params `[1][2][5]`. The multilingual 3.5 adds a 128-dim language-ID one-hot prompt fused with the acoustic representation `[1]`.
- **Cache-aware = genuinely incremental.** Caches are kept for *all* self-attention and convolution layers; each frame is processed **exactly once, non-overlapping** — contrasted explicitly with "buffered" streaming that re-encodes overlapping windows `[1][2][5]`. Latency is chosen at inference time via `att_context_size` right-context (in 80 ms frames): 80 / 160 / 320 / 560 / 1120 ms `[1][2]`.
- **English WER is reported on different corpora for the two checkpoints** (§3): the English-only model on the 9 Open ASR Leaderboard sets; the 3.5 multilingual on FLEURS.
- Output: native punctuation + capitalization, no post-processing; 16 kHz mono input `[1][2]`.
- 3.5 supports **40 locales** (19 transcription-ready incl. en-US/en-GB, 13 broad-coverage, 8 adaptation-ready); **English is transcription-ready** `[1]`. The English-only model is en-US only. `[2]`

---

## 2. License — code and weights (commercial use)

| Component | License | Commercial use | Notes |
|---|---|---|---|
| `nemotron-3.5-asr-streaming-0.6b` weights | **OpenMDW-1.1** (HF `license: other`, `license_name: openmdw-1.1`) `[1][6]` | **Yes** — "permission … to deal in the Model Materials without restriction, including under all copyright, patent … rights" `[7]` | Must retain the license + copyright/origin notices on any distribution; **patent-retaliation clause** (rights terminate if you sue over the model's IP); no restrictions on generated outputs. `[7]` |
| `nemotron-speech-streaming-en-0.6b` weights | **NVIDIA Open Model License Agreement** (HF `license: other`) `[2]` | **Yes** — card says "ready for commercial/non-commercial use" `[2]` | Standard NVIDIA open-model terms; review before shipping. |
| NeMo-Speech.cpp runtime (C++/ggml) | **Apache-2.0** (NVIDIA-authored code; third-party notices separate) `[3][12]` | Yes | Same license tier as the Rumble/Qwen serving stacks. |
| NVIDIA NeMo (Python) | Apache-2.0 `[1][2]` | Yes | Training / fine-tuning / inference scripts. |

**Assessment:** commercially usable, but **not** the "cleanest" tier — OpenMDW-1.1 / NVIDIA OMLA carry notice-retention and patent-retaliation terms that Rumble/Qwen's plain Apache-2.0 do not. This is still far better than Kyutai's CC BY 4.0 (attribution) and vastly better than any CC BY-NC / "reference only" license. The managed NIM container is governed separately by the NVIDIA Software License Agreement / API Trial Terms `[4]` — irrelevant for self-hosting. **Action before commercial ship:** have legal read OpenMDW-1.1 (patent clause) for the exact checkpoint chosen.

---

## 3. English accuracy — the comparison that matters

### 3.1 NVIDIA self-reported (vendor, not independent)

**English-only `nemotron-speech-streaming-en-0.6b`**, Open ASR Leaderboard sets, whisper-normalized, w/o PnC `[2]`:

| Chunk | Avg | AMI | Earnings22 | GigaSpeech | LS-clean | LS-other | SPGI | TEDLIUM | VoxPopuli |
|---|---|---|---|---|---|---|---|---|---|
| **1.12 s** | **6.93** | 11.73 | 12.52 | 9.66 | **2.32** | **4.84** | 2.97 | 3.50 | 7.91 |
| 0.56 s | 7.07 | 11.88 | 12.82 | 9.78 | 2.46 | 5.07 | 3.03 | 3.54 | 8.00 |
| 0.16 s | 7.67 | 14.71 | 13.01 | 10.34 | 2.56 | 5.57 | 3.25 | 3.77 | 8.18 |
| 0.08 s | 8.43 | 18.29 | 13.16 | 11.17 | 2.80 | 6.01 | 3.43 | 4.10 | 8.46 |

**Multilingual `nemotron-3.5-asr-streaming-0.6b`**, FLEURS, WER % `[1]` (note: **not** the same corpus as the table above — do not compare across them):

| Language (LangID) | 80 ms | 160 ms | 320 ms | 560 ms | 1.12 s |
|---|---|---|---|---|---|
| **English (en-US/en-GB)** | 9.43 | 8.88 | 8.27 | 7.99 | **7.91** |
| Auto-detect English | 9.72 | 9.34 | 8.84 | 8.80 | 8.84 |

NVIDIA's own note: *"We would recommend Nemotron ASR Streaming (English) model for English-only transcription use cases"* `[1]` — i.e. the 7.91% FLEURS English of the 3.5 model is a multilingual-tax number; the English-only model's 2.32% LS-clean is the stronger English figure, on a different bench.

The dev blog reports the latency/accuracy trade directly: WER **7.84% → 7.22%** as chunk grows 0.16 → 0.56 s (English-only, unspecified subset), and "up to 3× higher efficiency than traditional buffered systems" `[5]`. Throughput claims (H100): **560 concurrent streams at 320 ms** chunk (3× baseline) `[5]`; the 3.5 card claims 240 streams at 80 ms vs 14 for Parakeet RNNT 1.1B (~17×) `[1]`. **These are H100 concurrency numbers, not interactive latency on a shared 3060.**

### 3.2 One independent apples-to-apples row for English

The TypeWhisper benchmark ran **91 models over the same 4,649 clips** (data stamp `6ecd841349`, built 18 Nov 2026; WER normalized for case/punctuation/number spelling; accented-speech suites excluded) `[9][10]`. The Nemotron 3.5 entry and the four comparator models are all in **this same run**, so this is the cleanest relative ranking found — all numbers below are from that run:

| Model (TypeWhisper row) | WER EN | ms/clip | Notes |
|---|---|---|---|
| Rumble-2-Flash | **4.8%** | 93 | prior review `[10]` |
| Qwen3-ASR 0.6B (8-bit) | **5.1%** | 315 | prior review `[10]` |
| whisper base (whisper.cpp) — **current meept path** | **11.6%** | 590 | prior review `[10]` |
| **Nemotron 3.5 ASR Streaming 0.6B** | **11.7%** | 188 | **this review** — rank 62/71, 55× speed `[9]` |
| Nemotron 3.5 ASR Streaming 0.6B (+1 s padding probe) | **9.9%** | 151 | rank 58/71; probe, not an app setting `[10]` |
| Kyutai STT 1B (en/fr) | **14.4%** | 4,932 | prior review `[10]` |

**Reading:** on this independent dictation bench, Nemotron 3.5 lands **essentially tied with the current whisper base (11.7% vs 11.6%)** and clearly worse than Rumble/Qwen on English — a striking gap versus its strong self-reported leaderboard numbers. The benchmark runs Nemotron through **FluidAudio Core ML on Apple Silicon, fed in 10 s blocks**, and its weak English suites are noisy (5.7%), number formatting (30.0%) and LibriSpeech (4.5%) `[9]`; the benchmark warns these are "rough orders of magnitude" for speed and its normalization differs from NVIDIA's protocol. So the honest statement is: **Nemotron's English accuracy is strong on NVIDIA's own ASR Leaderboard setup (especially the English-only checkpoint) but only middling on an independent dictation-style bench** — do not assume it will beat Rumble/Qwen on meept's prompts without measuring. What it *does* beat them on decisively is latency architecture and footprint.

---

## 4. Streaming: true incremental vs the alternatives

This is Nemotron's strongest differentiator and the reason to consider it for the lag:

- **True incremental, cache-aware**: processes only new audio chunks while reusing cached encoder context; no overlapping recomputation, non-overlapping frames, arbitrary-length streams with no fixed duration limit `[1][2][5]`. Configurable lookahead 80 ms–1.12 s at inference time, no retraining `[1]`.
- **It is *not* Qwen's "pseudo-streaming".** Qwen3-ASR re-feeds all accumulated audio per 2 s chunk (O(n²)) `[10]`; Nemotron's cache makes each frame single-pass.
- **It is *not* Rumble's whole-audio batch path.** Rumble has no documented streaming API `[10]`.
- **Caveat — no built-in semantic end-of-utterance.** The streaming model itself does not decide "done talking". NVIDIA ships a *separate* joint ASR+EOU model (`parakeet_realtime_eou_120m-v1`) for cache resets at turn boundaries `[5]`, and NeMo-Speech.cpp supplies **Silero VAD-driven endpointing** plus an `endpointing_ms` socket option `[3][8]`. That means the 500 ms silence + 900 ms `turn_confirm_ms` waits still need an external endpoint decision — **Kyutai's semantic VAD is still the only option that directly replaces both fixed waits.** Nemotron instead makes each chunk cheap and partial-rich, which feeds `voice.context_warm` and partial-driven barge-in.
- **"Fastest time-to-final" claim, carefully read:** the dev blog reports **24 ms median time-to-final** for Nemotron vs 90 ms (local, L40) and 200 ms+ (API) `[5]`. That is a *post-audio-end server finalization* number, **not** speech-to-word delay like Kyutai's "words out 500 ms after being spoken" — do not conflate them. It does indicate finalization is not a bottleneck once the endpoint fires.

---

## 5. socrates GPU reality and VRAM fit (live, read-only)

Live `nvidia-smi` on `caimlas@socrates` (2026-11-18), matching both prior reviews:

| GPU | Device | Used | Total | Resident |
|---|---|---|---|---|
| 0 | Tesla PG500-216 | 28,854 MiB | 32,768 MiB | llama-server (~full) |
| 1 | RTX 3060 | 6,665 MiB | 12,288 MiB | qwen3-tts (`server.py`, `CUDA_VISIBLE_DEVICES=1`); **~5.6 GB free**, 0% util |

Any STT on the 3060 **shares the card with qwen3-tts**; the Tesla is effectively full.

**Nemotron footprint (measured artifact sizes, from source):**

- Q8 GGUF: **742 MB** (3.5) / **730 MB** (English); F16 ~1.24 GB; F32 ~2.37–2.47 GB `[1][2][13]`. TypeWhisper lists the 3.5 at ~665 MB `[9]`.
- NeMo-Speech.cpp loads the model once and is deliberately lightweight: streaming ASR (Q8_0, 160 ms chunks) runs **2.3 ms/chunk at 67× realtime on an RTX 4090 and 27 ms/chunk at 6× realtime on CPU** `[3]`. It has CUDA, CPU, Metal and Vulkan backends; the Linux CUDA build supports Turing-class (compute 7.5) and newer — the RTX 3060 (Ampere, 8.6) is supported `[12]`.
- **Realistic single-stream footprint on the 3060: ~1–1.5 GB** (Q8 GGUF + CUDA context + small caches), with **no vLLM GPU-fraction tuning needed** because it is a dedicated ggml process, not a general LLM server.

**Comparison:** this is the *smallest* GPU-resident option after whisper.cpp's ~150 MB subprocess — far below the ~2.5–4 GB estimated for Rumble/Qwen via vLLM and below Kyutai's ~1–2 GB BF16. It leaves most of the 5.6 GB headroom intact for qwen3-tts growth. **No load test was run; VRAM coexistence remains to be proven in the pilot.**

---

## 6. Inference paths — what is real today

| Path | Status for the 0.6B streaming models | Fit for meept |
|---|---|---|
| **NeMo-Speech.cpp (ggml C++)** | **Official, day-0, best fit** `[1][2][3]`. Q8 GGUF, `nemo-speech transcribe`/`--stream`/`--live`; `nemo-speech serve` exposes HTTP `POST /v1/audio/transcriptions`, **realtime WebSocket `/v1/audio/transcriptions/realtime` with partial `.delta` + final `.completed` events and `input_audio_buffer.clear`**, optional API key, TLS, loopback bind `127.0.0.1` by default `[8]`; native C SDK. | **Chosen path.** One warm process, realtime protocol maps to `STTProvider`, tiny footprint. |
| **NVIDIA NeMo (Python)** | Official; cache-aware streaming script + pipeline (`cache_aware_rnnt.yaml`), NeMo 26.06 (3.5) / 25.11 (English) `[1][2]`. | Training/fine-tuning; heavier Python/CUDA dep for serving. |
| **HuggingFace Transformers** | `Nemotron3_5AsrForRNNT` / `AutoModelForRNNT`, `transformers>=5.13.0`; offline + experimental `TextIteratorStreamer` streaming with `set_num_lookahead_tokens` `[1][14]`. | Convenient for evaluation, not the shipping path (Python, experimental streaming API). |
| **NVIDIA NIM / Riva gRPC** | Managed container + hosted API, gRPC streaming, Triton batching `[1][4][5]`. | Self-hosting preferred; NIM container has separate license `[4]`. |
| **TensorRT-Edge-LLM** | Experimental RNN-T runtime, OpenAI-compatible `/v1/audio/transcriptions`, batch 1 `[15]`. | Edge/experimental; not needed. |
| **vLLM** | **Not supported.** NVIDIA maintainer: *"vLLM is not the right serving framework for transducer ASR models"* — recommend NIM / NeMo+Triton / sherpa-onnx `[16]`. Community plugins in progress (vllm #47455, RFC PRs #65543/#65544/#65546, Nov 2026) but **not production-ready** `[17]`. | Do not plan around vLLM for Nemotron. (Contrast: Rumble/Qwen *are* vLLM-modeled.) |
| **sherpa-onnx / ONNX int4** | Community ONNX path `[16]`; `onnx-community/nemotron-3.5-asr-streaming-0.6b-onnx-int4` exists `[18]`. | Possible CPU fallback; third-party. |
| **MLX (Mac)** | `mlx-community/nemotron-3.5-asr-streaming-0.6b` `[19]`. | Mac leg only; moot for socrates. |
| **llama.cpp** | **No.** Nemotron RNN-T is a ggml model served by NeMo-Speech.cpp, not llama.cpp (which is the path for Voxtral/Qwen GGUF). A separate community ggml port (`m1el/nemotron-asr.cpp`) exists but is not llama.cpp `[13]`. | Reusing meept's llama.cpp dep is **not** available here. |

---

## 7. Comparison table (English, this hardware)

| Dimension | whisper.cpp base.en (current) | **Nemotron 0.6B streaming** | Rumble-2-Flash | Qwen3-ASR-0.6B | Kyutai STT 1B |
|---|---|---|---|---|---|
| English WER, one shared bench | 11.6% `[10]` | **11.7%** (9.9% +1 s pad probe) `[9][10]` | **4.8%** `[10]` | **5.1%** `[10]` | 14.4% `[10]` |
| Vendor/bench best English | — | 6.93% avg / 2.32% LS-clean (English-only, OAL) `[2]`; 7.91% FLEURS (3.5) `[1]` | 6.60% OAL English `[10]` | 6.42% OAL badge; 2.11/4.55 LS `[10]` | no official 1B WER `[10]` |
| Streaming / partials | None; no token callback | **True incremental, 80–1120 ms chunks; partial deltas** `[1][8]` | None documented `[10]` | vLLM-only 2 s-chunk O(n²) re-feed `[10]` | **Native, 500 ms delay, semantic VAD** `[10]` |
| Endpointing | 500 ms silence + 900 ms confirm + cold subprocess | Silero VAD / `endpointing_ms` in realtime socket `[8]`; no semantic EOU `[5]` | needs external endpoint; warm server | needs external endpoint; warm server | **Semantic VAD + ~125 ms flush** `[10]` |
| Effect on time-to-`heard` | baseline (lagging) | **Large** (warm, incremental, partials; 24 ms reported finalization `[5]`) | modest (warm start only) | modest; streaming slower than current endpoint | **Largest** (semantic VAD) |
| License (code + weights) | MIT / MIT | Apache-2.0 runtime + **OpenMDW-1.1 / NVIDIA OMLA** weights `[1][2][3]` | Apache-2.0 / Apache-2.0 `[10]` | Apache-2.0 / Apache-2.0 `[10]` | MIT+Apache / **CC BY 4.0** `[10]` |
| Footprint on shared 3060 | ~150 MB + subprocess | **~0.7–1.5 GB** (Q8 GGUF) `[1][2][3]` | ~2.5–4 GB vLLM (est.) `[10]` | ~2.5–4 GB vLLM (est.) `[10]` | ~1–2 GB BF16 (est.) `[10]` |
| Integration into seam | existing | **Medium** (Go WS client to `nemo-speech serve`, sidecar) | low-medium (vLLM HTTP) | low-medium (vLLM HTTP) | medium (Go WS → Rust server) |
| Maturity | high | 2026 model, official runtime, active | brand new (Nov 2026) | 2026 model, streaming criticized | production (powers Unmute) `[10]` |

---

## 8. Integration notes for this repo

- The seam is small and additive: `voicechild.STTProvider.Stream(ctx, <-chan []byte) (<-chan Transcript, error)`, optional `sttFlusher.FlushNow()`, `providers.NewSTT` vendor dispatch, factories injected via `SetProviderFactories` in `cmd/meept-voice/main.go`, and `ProviderConfig{Vendor, Model, BaseURL, ...}` on the C1 keys frame (local code: `internal/voicechild/providers/{providers.go,stt_whisper.go}`, `internal/voicechild/{pipeline.go,providers_wiring.go}`).
- **The realtime protocol maps cleanly:** NeMo-Speech.cpp's `conversation.item.input_audio_transcription.delta` → `Transcript{Text, Final:false}`; `.completed` → `Final:true`; `input_audio_buffer.clear` / `response.cancel` → `FlushNow()` (barge-in) `[8]`. This finally gives meept native partials for `voice.context_warm` and partial-driven barge-in — which whisper.cpp cannot supply — without a new protocol.
- Keep it keyless and loopback: `nemo-speech serve` binds `127.0.0.1` by default with optional bearer key `[8]`; reuse `stt_vendor`/`stt_model`/`stt_base_url`. Generalize `stt_base_url` (currently documented as "Deepgram-compatible") to a generic realtime endpoint, as the Rumble/Qwen reviews also noted.
- Sidecar must follow meept's runtime spawn-guard / orphan-sweep conventions; warm the model **off** the first-turn path. Unlike vLLM, **no GPU-memory-fraction tuning is needed** — this is a dedicated ggml process. Use the **English-only checkpoint** for the English slot; select `nemotron-3.5` only if multilingual later matters.
- Endpointing: start with Silicon VAD + `endpointing_ms` in the server; a per-employee `voice.turn_confirm_ms` override is the safe A/B vehicle. Kyutai's semantic VAD remains the only direct replacement for the fixed confirm window.
- Keep the default byte-identical: `stt_provider = whisper` unchanged; Nemotron is opt-in.

---

## 9. Risks and open questions

1. **Wrong bottleneck?** It attacks latency architecture (warm, incremental, partials) but does not by itself replace the fixed silence/confirm waits — that still needs VAD endpointing, and only Kyutai has semantic EOU. Net time-to-`heard` gain must be measured.
2. **Independent English accuracy is only middling** (11.7% vs whisper base 11.6%, Rumble 4.8%, Qwen 5.1% on the same bench) `[9][10]`. The strong self-reported numbers use a different corpus/protocol. Do not assume an accuracy win.
3. **License fine print.** OpenMDW-1.1 patent-retaliation + notice retention `[7]`; NVIDIA OMLA for the English checkpoint `[2]`. Legal review before commercial ship; neither is as plain as Apache-2.0.
4. **vLLM is a dead end** for this architecture `[16][17]` — do not assume the Rumble/Qwen serving work transfers.
5. **VRAM coexistence unproven.** ~1–1.5 GB estimate beside qwen3-tts; no load test was run.
6. **Nothing reproduced locally.** Every WER/latency/throughput figure is vendor-, leaderboard-, or benchmark-reported (TypeWhisper used FluidAudio Core ML on Apple Silicon, fed in 10 s blocks) — the pilot establishes socrates-specific truth.

---

## Sources

[1] https://huggingface.co/nvidia/nemotron-3.5-asr-streaming-0.6b — multilingual model card: architecture, 40 locales, FLEURS WER tables, OpenMDW-1.1, NeMo-Speech.cpp + NeMo + Transformers usage, throughput.
[2] https://huggingface.co/nvidia/nemotron-speech-streaming-en-0.6b — English-only model card: architecture, Open ASR Leaderboard WER by chunk, NVIDIA Open Model License, recommendation for English-only use.
[3] https://github.com/NVIDIA/NeMo-Speech.cpp — official ggml C++ runtime: models, Apache-2.0, Q8 GGUF, perf table, server/SDK, backends.
[4] https://build.nvidia.com/nvidia/nemotron-asr-streaming/modelcard — NIM model card: two profiles, license separation (NVIDIA SLA vs OpenMDW-1.1).
[5] https://huggingface.co/blog/nvidia/nemotron-speech-asr-scaling-voice-agents — NVIDIA dev blog (Jan 2026): cache-aware design, 560-stream H100 result, 24 ms median time-to-final, WER/latency trade, EOU model note.
[6] https://huggingface.co/nvidia/nemotron-3.5-asr-streaming-0.6b/raw/main/README.md — raw card front matter (`license: other`, `openmdw-1.1`) + tables.
[7] https://openmdw.ai/license/1-1/ — OpenMDW-1.1 full text: commercial permission, notice retention, patent-retaliation clause, outputs unrestricted.
[8] https://github.com/NVIDIA/NeMo-Speech.cpp/blob/main/docs/server.md and https://github.com/NVIDIA/NeMo-Speech.cpp/blob/main/docs/api.md — HTTP + realtime WebSocket routes/events, `endpointing_ms`, `input_audio_buffer.clear`, loopback bind, optional key/TLS.
[9] https://typewhisper.github.io/typewhisper-benchmark/?model=fluidaudio%3Anemotron-3.5-asr-streaming-0.6b&view=model — independent TypeWhisper row for Nemotron 3.5 (WER EN 11.7%, ms/clip 188, data stamp 6ecd841349).
[10] Prior local research: `docs/research/{realtime-stt-options.md,rumble2flash-review.md,qwen3-asr-06b-review.md}` — comparator WER/latency/license figures, seam details, GPU reasoning, and the shared TypeWhisper row.
[11] https://research.nvidia.com/labs/nemotron/NVIDIA-Nemotron-3.5-Transcribe/ and https://huggingface.co/nvidia/NVIDIA-Nemotron-3.5-Transcribe-30B-A4B — the unrelated offline 30B speech-LLM (disambiguation).
[12] https://github.com/NVIDIA/NeMo-Speech.cpp/blob/main/docs/install.md — backend support (CUDA/CPU/Metal/Vulkan), Turing+ GPU floor, native-build guidance.
[13] https://huggingface.co/nvidia/nemotron-3.5-asr-streaming-0.6b/tree/main and https://github.com/handy-computer/transcribe.cpp/blob/main/docs/models/nemotron-speech-streaming-en-0.6b.md — GGUF artifact sizes (742/730 MB Q8, F16/F32); community ggml WER validation.
[14] https://huggingface.co/docs/transformers/model_doc/nemotron3_5_asr — Transformers `Nemotron3_5Asr` usage and streaming latency API.
[15] https://nvidia.github.io/TensorRT-Edge-LLM/developer_guide/models/nemotron3_5_asr.html — experimental TensorRT RNN-T runtime, OpenAI-compatible transcription, batch 1.
[16] https://huggingface.co/nvidia/nemotron-3.5-asr-streaming-0.6b/discussions/6 — NVIDIA maintainer: vLLM not suited to transducer ASR; use NIM/Triton/ONNX/sherpa-onnx.
[17] https://github.com/vllm-project/vllm/issues/47455 — community vLLM support effort + streaming-plugin RFCs (in progress, Nov 2026).
[18] https://huggingface.co/onnx-community/nemotron-3.5-asr-streaming-0.6b-onnx-int4 — community INT4 ONNX conversion.
[19] https://huggingface.co/mlx-community/nemotron-3.5-asr-streaming-0.6b — community MLX conversion (Mac).

Local, read-only: `nvidia-smi` on `caimlas@socrates` (2026-11-18); `internal/voicechild/providers/{providers.go,stt_whisper.go}`, `internal/voicechild/{pipeline.go,providers_wiring.go}`, `config/meept.json5`; prior `docs/research/` reviews.
