# Review: Qwen3-ASR-0.6B as an additive English STT option on socrates' RTX 3060

**Date:** 2026-11-18 · **Status:** research only — no code changes, no commits
**Question:** should `Qwen/Qwen3-ASR-0.6B` be added as an opt-in English STT provider on socrates, and where does it rank for English against the current whisper.cpp path, Rumble-2-Flash, and Kyutai STT 1B?

> **Citation style.** Every external claim carries an inline numbered citation `[n]` resolved in the **Sources** list at the end. All WER / RTFx / latency numbers are **vendor-, leaderboard-, or community-reported** unless explicitly marked as locally measured; **none were reproduced on socrates or on meept's audio**. Benchmarks use different corpora, hardware and normalization — compare **within a row**, not across rows. Local facts (GPU state, code, config) were read directly from the worktree/host and are marked as such.

---

## Verdict

**Add it as an additive, opt-in provider — but as a *second* accuracy option, not a latency fix and not a replacement for Rumble.** For English on this hardware the ranking is:

1. **Kyutai STT 1B** — still the only option that attacks the actual latency defect (native streaming, 500 ms delay, semantic VAD, ~125 ms flush). Weakest raw WER of the four on one independent bench, but latency wins here.
2. **Rumble-2-Flash** — best English accuracy of the sub-1B batch models (6.6% Open ASR English track; 4.8% on TypeWhisper), Apache-2.0, no streaming. Already reviewed.
3. **Qwen3-ASR-0.6B** — essentially tied with Rumble on English accuracy (5.1% on the same TypeWhisper bench vs Rumble's 4.8%), **same clean Apache-2.0 code + weights license**, and the **only one of the three with documented native streaming** — but that streaming path is vLLM-only, 2-second-chunk, O(n²) re-feed, and **slower than meept's current 500 ms endpointing**. So its real value here is as a **warm-server accuracy peer / fallback to Rumble**, not as the low-latency answer.
4. **whisper.cpp base.en (current)** — 11.6% WER on the same bench, no partials, cold subprocess; keep as default and fix its tuning first.

**Recommendation:** pilot Qwen3-ASR-0.6B behind the same `STTProvider` seam as Rumble (one `Final:true` per response via a warm vLLM server), measure time-to-`heard` and WER against Rumble and whisper **with qwen3-tts resident**, and treat its streaming API as a research curiosity for this use case, not a shipping path. If accuracy is the goal and Rumble is already in, Qwen3-ASR-0.6B is a low-risk *second source* (same license, similar footprint, different vendor) — worthwhile but not urgent. If time-to-`heard` is the goal, neither Qwen nor Rumble helps; go to Kyutai.

---

## 1. What Qwen3-ASR-0.6B actually is

| Property | Finding | Source |
|---|---|---|
| HF model id | `Qwen/Qwen3-ASR-0.6B` (base, custom `qwen_asr` package / vLLM path, created 2026-01-28); Transformers-native variant `Qwen/Qwen3-ASR-0.6B-hf` (added 2026-06-26, requires `transformers>=5.13.0`). 1.7B sibling: `Qwen/Qwen3-ASR-1.7B` / `-hf`. | [HF card][1], [HF -hf card][2], [HF API][16] |
| License — code | **Apache-2.0** (repo `LICENSE`, "Copyright 2026 Alibaba Cloud"). | [GitHub repo][3] |
| License — weights | **Apache-2.0** on both base and `-hf` cards; HF `gated: false`. The official launch announcement likewise states the weights are open-sourced under Apache 2.0 [10]. **No commercial restrictions found** in primary sources. Same clean tier as Rumble, cleaner than Kyutai (CC BY 4.0). | [HF card][1], [HF -hf card][2], [HF API][16] |
| Architecture | AuT (audio) encoder → projector → Qwen3 LLM decoder. Encoder: 180M params, hidden 896, 18 layers, 14 heads, 128-d Mel, 8× downsample → 12.5 Hz token rate, dynamic flash-attention window 1–8 s. Decoder: Qwen3-0.6B, 28 layers, hidden 1024, **GQA 16 query / 8 KV heads**, head_dim 128. | [paper][4], [HF card][1] |
| Parameters / precision | Base: **938M params BF16** (~1.88 GB safetensors). `-hf`: **782M params BF16** (~1.58 GB) — the ~156M-param difference between the two repos is not explained in primary sources. | [HF API][16], [HF -hf card][2] |
| Languages | 30 languages + 22 Chinese dialects ("52 languages and dialects"); **English explicitly supported** (LibriSpeech/Fleurs-en/CV-en results published). | [HF card][1], [paper][4] |
| Serving | `pip install -U qwen-asr` (transformers backend, `Qwen3ASRModel.from_pretrained`); native HF `AutoProcessor`/`AutoModelForMultimodalLM` on the `-hf` repo; vLLM via `qwen-asr-serve Qwen/Qwen3-ASR-0.6B --gpu-memory-utilization 0.8 ...` (wrapper around `vllm serve`, day-0 support). `bfloat16`, FlashAttention-2 recommended. | [GitHub repo][3], [HF -hf card][2] |
| Quantizations | No official GGUF/AWQ from Qwen. Community: `ggml-org/Qwen3-ASR-0.6B-GGUF` (qwen3vl arch, 8-bit ≈805 MB, `llama-server -hf`), `mradermacher/...` (Q2_K 0.4 GB → Q8_0 0.9 GB, F16 1.6 GB + mmproj), `cstr/...` (Q4_K 631 MB, needs custom CrispASR runtime), INT4-AWQ `vrfai/Qwen3-ASR-0.6B-int4-QAD`. **Caveat:** GGUF paths use nonstandard runtimes; stock `llama-cli` ASR support is not documented by Qwen. | [ggml-org GGUF][14], [mradermacher GGUF][15], [GitHub repo][3] |

---

## 2. Streaming: honest assessment

Qwen3-ASR-0.6B is the only one of the three sub-1B candidates with a **documented native streaming API**, but it does not beat meept's endpointing for short utterances:

- **vLLM-only.** Verbatim: *"Qwen3-ASR fully supports streaming inference. Currently, streaming inference is only available with the vLLM backend. Note that streaming inference does not support batch inference or returning timestamps."* [3]
- **2-second chunks + 5-token rollback.** `init_streaming_state(unfixed_chunk_num=2, unfixed_token_num=5, chunk_size_sec=2.0)`; the paper describes *"a 2-second chunk size, a 5-token fallback, and keeping the last four chunks unfixed"* (note: code default `unfixed_chunk_num=2` vs paper "four" — a vendor doc discrepancy). [3][4]
- **Re-feeds all accumulated audio each chunk.** Verbatim source: *"Each time a new chunk is ready, we append it to audio_accum and re-feed all audio seen so far to the model (no padding)."* That is O(n²) recompute — community issue #148 calls it *"essentially pseudo-streaming because it does not perform incremental inference,"* reporting stutter/unresponsiveness on long streams. [3][11]
- **Comparator:** a 2 s chunk stride emits its first transcript ~2 s after speech starts; meept's current path already decides end-of-utterance on **500 ms** silence and emits a final per utterance. For 2–5 s local utterances, Qwen's streaming cannot reduce time-to-`heard`; it would add work. [local code: `stt_whisper.go`]

**Conclusion:** treat streaming as a feature that exists upstream but is **not useful for meept's latency goal**. The accuracy benefit of streaming is small and is a *cost* here: vendor Table 8 shows avg WER degrading 3.48 → 4.40 (0.6B) and 2.69 → 3.33 (1.7B) in streaming mode, with LibriSpeech-other jumping 4.55 → 6.27 for the 0.6B. [4]

---

## 3. socrates GPU reality and VRAM

**Live read-only check, 2026-11-18** (`ssh caimlas@socrates nvidia-smi`), matches the prior Rumble report exactly:

| GPU | Device | Used | Total | Resident |
|---|---|---|---|---|
| 0 | Tesla PG500-216 | 28,854 MiB | 32,768 MiB | llama-server (~full) |
| 1 | RTX 3060 | 6,665 MiB | 12,288 MiB | qwen3-tts (server.py, `CUDA_VISIBLE_DEVICES=1`); **~5.6 GB free**, 0% util |

Any STT on the 3060 **shares the card with qwen3-tts**.

**VRAM estimate for Qwen3-ASR-0.6B (no official measurement found):**
- BF16 weights ≈1.6–1.9 GB ([HF API][16]); Q4/Q5 GGUF ≈0.53–0.63 GB ([community GGUFs][15]) — but GGUF runtimes are nonstandard.
- Decoder KV (GQA 8 KV heads × 128 dim × 28 layers) ≈0.11 MiB/token → ~450 MiB per 4096-token sequence (same class of estimate used in the Rumble review; [paper][4] for architecture).
- CUDA context (~0.5 GB) + audio encoder / framework overhead (~0.5–1 GB).
- **Realistic single-stream vLLM footprint ≈2.5–4 GB**, so it fits in 5.6 GB free — but vLLM's default `--gpu-memory-utilization 0.92` would target ~11.3 GB of the 12 GB card and must be explicitly overridden (e.g. `0.20–0.25`, or `--kv-cache-memory-bytes`). [vLLM engine args][17] Note the Qwen repo's own examples use `0.7–0.8`, which are unsafe on a shared 3060. [3]

---

## 4. English accuracy — the comparison that matters

### 4.1 Vendor / leaderboard numbers (not comparable across rows)

| Source | Qwen3-ASR-0.6B | Notes |
|---|---|---|
| Open ASR Leaderboard (Qwen card badge) | **Mean WER 6.42, RTFx 166.23**, AMI 11.66 | Leaderboard-reported, no date/version on Qwen card. 1.7B: 5.76 / 147.93. [1] |
| Qwen paper, LibriSpeech clean/other | **2.11 / 4.55** | 1.7B 1.63/3.38; Whisper-large-v3 1.51/3.97 (Qwen re-ran Whisper). Vendor-reported. [4] |
| Qwen paper, Fleurs-en / CV-en | **4.39 / 9.92** | 1.7B 3.35 / 7.39. Vendor-reported. [4] |
| AppTek 302 h call-centre, 14 accents | **8.7%** (1.7B 7.4%; Rumble 7.86%) | Reson8-reported; see §5. [6] |
| Rumble Open ASR English track (v1.16.1, 18-11-2026, H200+vLLM) | Rumble **6.60** (RTFx 6338) | Rumble-reported. [5] |

### 4.2 One independent apples-to-apples row for English

The TypeWhisper benchmark ran **91 models over the same 4,649 clips on one Mac mini** (data stamp 18 Nov 2026; WER normalized for case/punctuation/number spelling; accented-speech suites excluded). This is the cleanest relative ranking found — all numbers below are **from the same run**: [7]

| Model (TypeWhisper row) | WER EN | ms/clip | Notes |
|---|---|---|---|
| Rumble-2-Flash | **4.8%** | 93 | rank 10 |
| Rumble-2-Flash (BF16, local) | 5.0% | 162 | |
| **Qwen3-ASR 0.6B (8-bit)** | **5.1%** | 315 | rank 16; **61× speed** |
| Qwen3-ASR 1.7B (8-bit) | 5.1% | 457 | |
| Whisper large-v3 (whisper.cpp) | 6.0% | 1,808 | |
| Whisper large-v3-turbo (whisper.cpp) | 6.4% | 907 | |
| **Whisper base (whisper.cpp) — current meept path** | **11.6%** | 590 | rank 70 |
| **Kyutai STT 1B (en/fr)** | **14.4%** | 4,932 | rank 75; 1.9× speed |

**Reading:** on this English bench Qwen3-ASR-0.6B ≈ Rumble (5.1 vs 4.8) and clearly beats Whisper large-v3 (6.0) and the current base.en (11.6) by a wide margin; it also beats Kyutai STT 1B (14.4). Caveats: Qwen/Rumble/Kyutai entries here are **8-bit MLX on a Mac mini**, whisper.cpp entries use its own timed `whisper-cli` invocation, Rumble's row is a cloud API — the benchmark itself warns these speed numbers are "rough orders of magnitude," and it is normalized WER, not the paper's protocol. A CPU-only LibriSpeech bench reached the same conclusion: *"Qwen 0.6B and large-v3-turbo are effectively tied... I would not choose either on accuracy."* [13]

---

## 5. Verifying the "8.7% AppTek 302 h" claim

**Verdict: the number is reported accurately, but its provenance is narrower than the prior review implies.**

- The Rumble-2-Flash HF launch blog states verbatim: *"On AppTek's call-centre benchmark (302 hours of helpline calls, 14 English accents, scored with AppTek's own script), Rumble-2-Flash reaches 7.86% WER, on par with NVIDIA's Nemotron 3.5 Transcribe (30B) at 7.78%. That's well ahead of Qwen3-ASR 0.6B (8.7%), from the same model family, and close to Qwen3-ASR 1.7B (7.4%), the only model in the comparison that does better."* [6]
- So the 8.7% figure is **vendor-reported by Reson8 (a competitor comparing against Qwen)** — not a Qwen-published number, and not found anywhere in the Qwen paper or cards. [4][1]
- **Discrepancy:** the only public matching paper describes a **128.6-hour** corpus (156 speakers, 1,746 recordings, 14 accents), where Qwen3-ASR-0.6B's WERs are 8.9/8.9/9.2/8.9/**8.7** and 1.7B's 7.9/8.0/8.3/7.8/**7.4** across five chunking conditions — the Reson8 "8.7 / 7.4" pair matches exactly the **Fixed-60s chunking** column. Whether "302 hours" is a larger private superset is **unconfirmed**. [12]
- Practical takeaway: treat 8.7% as "Reson8 says Qwen3-ASR-0.6B ≈8.7% on noisy call-centre audio, slightly worse than Rumble's 7.86%." It is a reasonable *relative* signal but should not be quoted as an independently verified Qwen benchmark.

---

## 6. Hallucination behavior

- **Vendor:** no explicit hallucination/phantom-text evaluation exists in the paper, cards, or README. The model has a defined no-speech output convention (`language None<asr_text>`), and SFT uses *"non-speech data, streaming-enhancement data and context biasing data"* plus RL for *"noise robustness, transcription stability"* — design intent, not a measured hallucination rate. [4][1]
- **Community (not vendor):** third-party reports exist of phantom text on silence/noise — an mlx-audio issue reports *"A zero-frame array is padded and sent to the model, which can hallucinate text"* and a 934 s quiet recording producing 1,108 characters; a vLLM issue reports `/v1/realtime` returning the previous turn's transcript on silence/noise under KV reuse. Both are runtime/backend-specific, not a model-card claim, and neither was reproduced here. [3][11]
- **Contrast:** Rumble markets *"Silence stays silent"* with markedly less phantom text than Qwen3-ASR (prior Rumble review). For meept's false-turn-suppression concern this is a point **against** Qwen relative to Rumble, but it is vendor-asserted, not independently measured on meept's audio.

---

## 7. Comparison table (English, this hardware)

| Dimension | whisper.cpp base.en (current) | Qwen3-ASR-0.6B (vLLM) | Rumble-2-Flash (vLLM) | Kyutai STT 1B |
|---|---|---|---|---|
| English WER, one shared bench | 11.6% [7] | **5.1%** (8-bit) [7] | **4.8%** [7] | 14.4% [7] |
| Open ASR English track | — | 6.42 (Qwen badge) [1] | 6.60 (v1.16.1) [5] | no official number |
| Noisy call-centre | — | 8.7% (Reson8-reported) [6] | 7.86% [6] | — |
| Streaming / partials | None; no token callback [local code] | vLLM-only 2 s chunks, 5-token rollback, re-feeds all audio [3][11] | None documented [5] | **Native, 500 ms delay** [8][9] |
| Endpointing | 500 ms silence + 900 ms confirm + cold subprocess [local code] | needs external endpoint; warm server removes cold start | needs external endpoint; warm server removes cold start | **Semantic VAD + ~125 ms flush** [8] |
| Effect on time-to-`heard` | baseline (lagging) | modest (warm start only) | modest (warm start only) | **largest** |
| License (code + weights) | MIT / MIT | **Apache-2.0 / Apache-2.0** [3][1] | **Apache-2.0 / Apache-2.0** [5] | MIT+Apache / **CC BY 4.0** [9] |
| Footprint on 3060 | ~150 MB + subprocess | ~2.5–4 GB vLLM (est.), ~1.6–1.9 GB BF16 [16] | ~2.5–4 GB vLLM (est.) | ~1–2 GB BF16 (est.) |
| Integration into seam | existing | **Low-medium** (same pattern as Rumble) | low-medium (prior review) | medium (Go WS → Rust server) |
| Maturity | high | 2026 model, active community, streaming criticized | 2026 model, brand new | production (powers Unmute) [8] |

---

## 8. Integration notes for this repo

- The seam is small and additive: `voicechild.STTProvider.Stream(ctx, <-chan []byte) (<-chan Transcript, error)`, optional `sttFlusher.FlushNow()`, `providers.NewSTT` vendor dispatch, factories injected via `SetProviderFactories` in `cmd/meept-voice/main.go`, `ProviderConfig{Vendor, Model, BaseURL, ...}` on the C1 keys frame. [local code: `internal/voicechild/providers/providers.go`, `stt_whisper.go`, `internal/voicechild/pipeline.go`, `providers_wiring.go`]
- A Qwen adapter would mirror the Rumble plan: emit **one `Final:true` per HTTP response**, no partials; reuse `stt_vendor`/`stt_model`/`stt_base_url`; keyless local server. `stt_base_url` is currently documented as a "Deepgram-compatible endpoint" — generalize to an OpenAI-compatible transcription endpoint if adding a second OpenAI-style vendor. Keep opt-in (`stt_provider` default `whisper`, byte-identical boot).
- Serve via a warm `qwen-asr-serve` / `vllm serve Qwen/Qwen3-ASR-0.6B` sidecar with an **explicit low `--gpu-memory-utilization`** because qwen3-tts owns the card. Warm-up off the first-turn path; sidecar must follow meept's spawn-guard / orphan-sweep conventions. [3][17]
- **Do not ship Qwen's streaming API** for the local leg: 2 s chunks and O(n²) re-feed are worse than the 500 ms endpointing policy, and it disables timestamps/batching. [3][11]
- If GGUF/llama.cpp is preferred for dependency reuse: community `ggml-org/Qwen3-ASR-0.6B-GGUF` works with `llama-server -hf` (qwen3vl arch, 8-bit ~805 MB), but it is **not** an official Qwen artifact and several community GGUFs need custom runtimes (CrispASR, Starling). Benchmark before shipping; do not trust stock `llama-cli` transcription. [14][15]

---

## 9. Risks and open questions

1. **Wrong bottleneck for latency.** Same finding as Rumble: Qwen removes only the cold start (~0.5–2 s), not the ~1.4 s of deliberate silence/confirm waiting; its streaming is slower than current endpointing for 2–5 s utterances.
2. **VRAM coexistence unproven.** No official VRAM number; ~2.5–4 GB estimate shares a 5.6 GB headroom with qwen3-tts. vLLM defaults are unsafe here; no load test was run.
3. **Hallucination posture weaker than Rumble** in vendor/community reports — relevant to false-turn suppression, though not independently measured.
4. **Streaming maturity.** vLLM-only, no batching/timestamps, community "pseudo-streaming" complaint, and a code-vs-paper discrepancy on the unfixed-chunk default.
5. **Benchmark provenance.** The headline 8.7% AppTek number is competitor-reported and the corpus size (302 h vs 128.6 h) is unresolved; the 5.1% vs 4.8% TypeWhisper result is MLX/8-bit on a Mac mini, not CUDA on a 3060. **Nothing here was reproduced on socrates or meept audio** — the pilot is what would establish local truth.

---

## Sources

[1] https://huggingface.co/Qwen/Qwen3-ASR-0.6B
[2] https://huggingface.co/Qwen/Qwen3-ASR-0.6B-hf
[3] https://github.com/QwenLM/Qwen3-ASR
[4] https://arxiv.org/html/2601.21337
[5] https://huggingface.co/Reson8/Rumble-2-Flash
[6] https://huggingface.co/blog/rumble-2-flash
[7] https://typewhisper.github.io/typewhisper-benchmark
[8] https://kyutai.org/stt
[9] https://huggingface.co/kyutai/stt-1b-en_fr
[10] https://www.alibabacloud.com/blog/qwen3-asr-%26-qwen3-forcedaligner-is-now-open-sourced-robust-streaming-and-multilingual_602843
[11] https://github.com/QwenLM/Qwen3-ASR/issues/148
[12] https://arxiv.org/html/2604.27543
[13] https://www.bigiron.cc/guides/qwen3-asr-vs-whisper-on-a-homelab-measured
[14] https://huggingface.co/ggml-org/Qwen3-ASR-0.6B-GGUF
[15] https://huggingface.co/mradermacher/Qwen3-ASR-0.6B-GGUF
[16] https://huggingface.co/api/models/Qwen/Qwen3-ASR-0.6B
[17] https://docs.vllm.ai/en/stable/configuration/engine_args/

Local, read-only: `nvidia-smi` on `caimlas@socrates` (2026-11-18); `internal/voicechild/providers/{providers.go,stt_whisper.go}`, `internal/voicechild/{pipeline.go,providers_wiring.go}`, `config/meept.json5`; prior `docs/research/{realtime-stt-options.md,rumble2flash-review.md}`.
