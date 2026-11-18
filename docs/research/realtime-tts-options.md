# Realtime / local TTS options for meept's voice local leg

**Date:** 2026-11-18
**Scope:** find local/realtime TTS options faster than the current qwen3 leg, that can **preserve the `caimlas` voice clone `vc_ce7d47c6`**. Research only — no code changes, no commits.
**Hardware:** Mac **M2 Ultra 64 GB** (Apple Silicon / MLX / Core ML) and Linux **`socrates`** (CUDA GPU, tier-1 `faster-qwen3-tts`).
**Baseline:** qwen3-tts at `socrates:8001`, OpenAI-compatible streaming `POST /v1/audio/speech` (`response_format=pcm`, `stream=true`), voice `vc_ce7d47c6`. Reported TTFB **0.65–1.3 s**; live call say-to-speaker gap **2–6 s** (partly child startup + clip prewarm contention).

Evidence labels used below:
- **[LIVE]** — measured by me on 2026-11-18 against the real `socrates:8001` server with the real `vc_ce7d47c6` clone (commands and raw numbers reproduced in §2).
- **[M]** — measured/instrumented third-party number with a source URL.
- **[V]** — vendor/model-card/marketing claim (upper bound; validate before trusting).
- **[U]** — unverified / uncertain.

---

## 1. Executive summary — ranked recommendation

**The fastest, safest win is not a new model. It is a different route on the server you already run.** Everything else is higher risk because of the voice-clone constraint.

| Rank | Option | Preserves `vc_ce7d47c6`? | First audio | HW | Integration effort | Verdict |
|---|---|---|---|---|---|---|
| **1** | **Switch client to existing `/v1/audio/speech/pcm-stream` on socrates** | **Yes — same server, same model, same voice ID, zero re-clone** | **[LIVE] ~0.37 s** vs current ~2.0 s (5×), headers in ~7 ms | existing socrates GPU | **Low** — change one base URL/path + framing in `tts_openai.go` | **DO THIS FIRST** |
| **2** | **Tune qwen3 on socrates**: `chunk_size`↓, cache clone reference latents, keep model warm, enable WebSocket/persistent session | Yes | **[M] 0.15–0.4 s** achievable; chunk 1 → 240 ms on Jetson | existing | Low–medium (server-side) | Stack on top of #1 |
| **3** | **ZONOS3-Mini** (Zyphra) on M2 Ultra via native MLX server | Only by **re-cloning** from the original reference WAV (ID does not port) | **[V] <150 ms** M3 Air, 80 ms H100 (no independent local measurement **[U]**) | M2 Ultra (native MLX server) | Medium (new adapter; v2 API not exactly `/v1/audio/speech`) | Best *new* local engine if you want an alternative to qwen3 |
| **4** | **Kyutai Pocket TTS** (CPU, exportable voice) | Only by **re-cloning** from reference WAV; exportable `.safetensors` voice pins it cleanly | **[M] ~35 ms first chunk on Apple Silicon** (independent), ~6× CPU realtime | CPU-only — M2 Ultra or socrates CPU | Medium (OpenAI-ish local server, thin adapter; gated weights) | Best measured sub-100 ms local option; GPU-free |
| **5** | **mlx-audio** serving Qwen3-TTS Base on M2 Ultra | Yes — re-run clone from reference; same model family | **[M] 84.8 ms** model TTFB, but **[M] 4.3 s** first yield with stock `stream=true` (fix needed) | M2 Ultra | Low (already OpenAI-compatible `/v1/audio/speech`) | Good fallback; watch the "fake streaming" bug |
| **6** | **CosyVoice3 / Fish Speech / Chatterbox-Turbo / Orpheus** | Re-clone from reference WAV | 0.15–1.2 s (highly hardware-dependent) | see §5 | Medium–high | Viable but not clearly better than #1/#2 |
| **not viable** | **Kokoro, Piper, Kyutai TTS 1.6B, VibeVoice-Realtime, Supertonic** | **No zero-shot clone of a new speaker** | fast, but disqualified | — | — | Cannot preserve `vc_ce7d47c6` |

**Bottom line:** the clone constraint is the decisive filter, and it strongly favours staying on Qwen3-TTS. The current server is *already* on the fastest Qwen3 serving tier; the 2–6 s gap is an endpoint/buffering problem, not a model problem. Route the client to the token-level streaming endpoint (and tune `chunk_size`/reference caching), and you get a ~5× first-audio improvement while keeping the exact cloned voice — with **no new model, no re-clone, and no new license exposure**.

---

## 2. Current state, verified live (the key finding)

I probed `socrates:8001` directly. Sources: `curl -s http://socrates:8001/health`, `/v1/models`, `/openapi.json`.

**Health (verbatim):**
```json
{"status":"healthy","model_ready":true,"model_id":"Qwen/Qwen3-TTS-12Hz-1.7B-Base","backend":"faster-qwen3-tts (CUDA graphs, tier 1)","device":"cuda","voices":{"preset":9,"custom":2}}
```

**OpenAPI paths:** `/health`, `/metrics`, `/v1/audio/speech`, `/v1/audio/speech/pcm-stream`, `/v1/audio/speech/stream`, `/v1/models`, `/v1/voices`.

This means socrates is **already** the `malaiwah/qwen3-tts-server` / `andimarafioti/faster-qwen3-tts` "tier 1" backend with CUDA graphs — the fastest configuration documented upstream (~130 ms first chunk on an RTX 4080 SUPER) [https://github.com/malaiwah/qwen3-tts-server]. It uses the **1.7B-Base** checkpoint and has 2 custom (cloned) voices, consistent with `vc_ce7d47c6`.

### 2.1 The buffering bug, measured

I sent the same sentence with the same cloned voice to both endpoints and timestamped the raw HTTP response with a Python socket client (TTFB = time from request send to first audio-bearing body byte, after headers). Repo-local script: `/tmp/tts_cadence.py`; results:

**Current path — `/v1/audio/speech` with `stream=true, response_format=pcm` [LIVE]:**
```
t=1.982s  hdr HTTP/1.1 200 OK (audio/pcm)
t=1.982s  7240, 5792, 13032, 15928, 10136, 11584, 15928, 18824, ... (14 writes)
t=1.984s  last write
```
→ **Every byte of the utterance arrives in one ~2 ms burst at t≈1.98 s.** The `stream=true` flag is **not producing incremental audio to the client** — the whole utterance is synthesized and buffered before the response body starts. curl TTFB across 3 runs: **2.77 s / 1.91 s / 1.61 s**, and in each case `ttfb ≈ total`, confirming full buffering. This directly explains the 0.65–1.3 s "TTFB" broadening to 2–6 s under contention: it is time-to-*whole*-audio, not time-to-first-chunk.

**Token-level path — `/v1/audio/speech/pcm-stream` [LIVE]:**
```
t=0.003s  hdr HTTP/1.1 200 OK (application/octet-stream)
t=0.373s  7240, 7240, 892
t=0.669s  7240, 7240, 892
t=0.967s  ...
t=2.927s  final
```
→ **Headers in ~7 ms, first audio at ~0.37 s, then a steady ~300 ms cadence** of progressive PCM chunks. curl (3 runs) `time_starttransfer` = **0.0065 / 0.0070 / 0.0072 s**. With `chunk_size=4` (the endpoint default) this is **true token-level streaming that keeps the exact same voice**.

**Chunk-size probe [LIVE, noisy]:** `chunk_size=1` gave first audio 384 / 951 / 2149 ms and `chunk_size=2` gave 6112 / 2910 / 10691 ms across 3 runs each, with one timeout — the server was clearly under contention during the sweep (consistent with the operator's "clip prewarm contention" note). **Do not read the noisy tail as a regression**; the stable default `chunk_size=4` delivered a clean ~0.37 s. Upstream documents `chunk_size=1` → 240 ms and `chunk_size=4` → 362 ms on Jetson [https://github.com/andimarafioti/faster-qwen3-tts]. Re-benchmark `chunk_size` when the box is idle before choosing.

**Interpretation:** the current Go client (`internal/voicechild/providers/tts_openai.go`, `openAISpeechRoute = "/v1/audio/speech"`, `response_format=pcm`, `stream=true`) is calling the *single-shot* route and asking it to stream — the server ignores `stream` there and buffers. The streaming capability already exists on the same box. This is the highest-leverage, lowest-risk change available.

### 2.2 Why the clone makes every other option riskier

`vc_ce7d47c6` is a **saved voice ID on the socrates server** (the `/v1/voices` clone registry). Qwen3-TTS cloning is reference-audio based: the clone is created from a reference `.wav` + transcript via `create_voice_clone_prompt` [https://github.com/QwenLM/Qwen3-TTS; https://huggingface.co/Qwen/Qwen3-TTS-12Hz-1.7B-Base]. Consequences:

- **Saved embeddings / voice IDs do not port across engines or model sizes.** Cached speaker/x-vector/codec embeddings are model-specific; cross-model injection needs norm scaling, and a `.qvoice` cross-model format exists precisely because a plain embedding does not transfer [https://huggingface.co/Qwen/Qwen3-TTS-12Hz-1.7B-CustomVoice/discussions/45; https://github.com/gabriele-mastrapasqua/qwen3-tts].
- **The raw reference WAV + transcript do port** — you can re-clone the same speaker on any zero-shot engine (Fish, CosyVoice, Chatterbox, ZONOS3, Pocket TTS, mlx-audio Qwen3), but the result is a *re-render* of the same speaker, not a bit-identical voice, and must be A/B-tested [https://github.com/fishaudio/fish-speech; https://github.com/QwenAudio/CosyVoice; https://github.com/resemble-ai/chatterbox].
- Staying on Qwen3-TTS (ranks 1, 2, 5) is the only way to keep `vc_ce7d47c6` **without re-cloning**.

**Action:** locate and preserve the original reference audio + transcript for `vc_ce7d47c6` before evaluating any non-qwen3 engine. *(Observation, not acted on: the clone's source WAV was not inspected as part of this research.)*

---

## 3. Option 1 & 2 detail — making the existing qwen3 path fast

### 3.1 Route change (rank 1) — no model change

`malaiwah/qwen3-tts-server` exposes three speech routes [https://github.com/malaiwah/qwen3-tts-server]:
- `POST /v1/audio/speech` — OpenAI-compatible **single-shot** (WAV/MP3/Opus; `stream` not documented here). **This is what meept calls.**
- `POST /v1/audio/speech/stream` — sentence-chunked, length-prefixed framing, one complete audio file per sentence.
- `POST /v1/audio/speech/pcm-stream` — **token-level PCM streaming, ~130 ms first chunk**, params `text`, `voice`, `instruct`, `chunk_size` (default 4), `language`; streams abort cleanly on client disconnect (barge-in friendly). **[LIVE] confirmed on socrates.**

Integration into the existing `providers.OpenAITTS` pattern (`Speak(ctx, text) -> io.ReadCloser`): the adapter already consumes an HTTP response body incrementally and resamples 24 kHz→16 kHz through `newResamplingReader`. The change is (a) route `#{base}/v1/audio/speech/pcm-stream`, (b) drop `stream`/`response_format` from the body (the pcm-stream route uses query/body `text`,`voice`,`chunk_size`), and (c) keep the existing streaming read path. **The `TTSProvider` interface does not change.** Note the current `ttsTransport` sets `DisableKeepAlives: true` (leaf 04, stale-idle-connection fix); each utterance is one POST, so this remains correct.

### 3.2 Qwen3-TTS model & tuning facts

- **Models (Jan 2026):** 0.6B and 1.7B in **Base** (voice cloning + fine-tuning), **CustomVoice** (preset speakers), **VoiceDesign** (1.7B only). **License Apache-2.0** for code *and* weights [https://github.com/QwenLM/Qwen3-TTS; https://github.com/QwenLM/Qwen3-TTS/blob/main/LICENSE; https://arxiv.org/pdf/2601.15621]. No newer/faster official checkpoint family as of Nov 2026 **[U]** — "faster" variants are community serving engines/quantizations, not new Qwen weights.
- **12 Hz tokenizer** emits a packet every 80 ms; Qwen's own vLLM engine measured **101 ms first-packet (1.7B, 12 Hz, c=1)** vs 150 ms for 25 Hz; 0.6B/1.7B at c=1 measured 97/101 ms with RTF 0.288/0.313 [https://huggingface.co/Qwen/Qwen3-TTS-Tokenizer-12Hz; https://www.alphaxiv.org/abs/2601.15621]. These are vendor-optimized numbers, not a promise for every server.
- **`faster-qwen3-tts` measured TTFA (0.6B / 1.7B), CUDA graphs** [https://github.com/andimarafioti/faster-qwen3-tts]:
  | GPU | 0.6B | 1.7B |
  |---|---|---|
  | RTX 4090 | **156 ms** | **174 ms** |
  | H100 80GB | 228 ms | 241 ms |
  | Tesla T4 16GB | 901 ms | 1096 ms |
  | Jetson Orin | 597 ms | 693 ms |
  Baseline (no CUDA graphs) TTFA was 800–1811 ms → **~5–9× improvement** from the CUDA-graph tier. Socrates is on tier 1 already, so its ~0.37 s first audio is consistent with a mid-range GPU + network hop rather than a misconfigured backend.
- **Chunk size vs TTFA** (Jetson, 0.6B) [https://github.com/andimarafioti/faster-qwen3-tts]: `chunk_size` 1→240 ms, 2→266 ms, 4→362 ms, 8→556 ms, 12→753 ms. "Smaller chunks = lower latency but more decode overhead; chunk_size=2 is the smallest that stays real-time on Jetson."
- **Reference prefill is a fixed cost.** Full ICL clone mode prepends ~80+ prefill tokens / ~188 codec tokens vs ~10 for x-vector-only; the codec decoder must see reference tokens first or the voice is wrong [https://github.com/andimarafioti/faster-qwen3-tts; https://ocdevel.com/blog/20260302-qwen-tts-voice-cloning]. `faster-qwen3-tts` caches reference audio as `.spk`/`.rvq` latents after the first clone request and accepts precomputed references to skip encoding each request [https://github.com/andimarafioti/faster-qwen3-tts]. **Precompute/cache `vc_ce7d47c6`'s latents on socrates** — this removes a per-request fixed cost without touching the client.
- **Warm the model.** `model.warmup(prefill_len=100)` captures CUDA graphs before serving; "first-use initialization can be much slower than warm generation" [https://github.com/andimarafioti/faster-qwen3-tts].
- **vLLM-Omni small-first-chunk fix (merged PR #1583, 2026-03-05):** `initial_codec_chunk_frames` decouples warmup from steady-state chunking; measured `ic=0`→2030 ms, `ic=2`→205 ms, `ic=5`→437 ms avg TTFA, with total generation roughly flat. Ships as `initial_codec_chunk_frames: 1` and is a per-request override [https://github.com/vllm-project/vllm-omni/pull/1583; https://docs.vllm.ai/projects/vllm-omni/en/latest/serving/speech_api/; https://vllm.ai/blog/2026-06-23-vllm-omni-tts]. Caveat: at concurrency ≥4 on 3090/A100 and ≥6 on 4090, `ic<25` produced a ~1 s seam; fine at c=1.
- **Keep-alive / persistent session:** vLLM-Omni offers a WebSocket endpoint where `input.done` is a *flush*, not a disconnect — "a client that speaks repeatedly pays the handshake once" [https://docs.vllm.ai/projects/vllm-omni/en/latest/serving/speech_api/]. The `pcm-stream` route "streams abort cleanly on client disconnect" [https://github.com/malaiwah/qwen3-tts-server], which fits meept's existing one-POST-per-utterance + barge-in-cancel pattern, so a WebSocket rewrite is **not** required for the first win.
- **Format:** `pcm` (24 kHz s16le) is correct and fastest; WAV adds a 44-byte header, MP3/Opus require full-audio encoding [https://developers.openai.com/api/docs/guides/text-to-speech; https://github.com/andimarafioti/faster-qwen3-tts]. Format is **not** the bottleneck.
- **Resampler:** the local 24 kHz→16 kHz `newResamplingReader` should be checked for whole-chunk buffering; a windowed-sinc with a large block can add one output block of latency **[U — not measured]**. Likely small next to a 300 ms chunk.

**Ordered tuning checklist for socrates:**
1. Point the client at `/v1/audio/speech/pcm-stream` (biggest win; [LIVE] ~2.0 s → ~0.37 s).
2. Benchmark `chunk_size` 1/2/4 on an idle box; start at 4, try 2.
3. Precompute/cache the `vc_ce7d47c6` reference latents (`.spk`/`.rvq` or equivalent); avoid re-extracting per request.
4. Confirm model warmup on startup; separate reference-extraction time from generation time with `/metrics`.
5. Verify the server isn't sharing the GPU with STT/LLM under load (contention is the likely cause of the 2–6 s live gap).
6. Only then consider keep-alive reuse / WebSocket sessions.

---

## 4. Rank 3–5 detail — alternative local engines (all require re-cloning)

### 4.1 ZONOS3-Mini (Zyphra) — best new local engine
- **Clone:** zero-shot from a raw reference clip (codec-token prefix, up to 60 s, **no transcript needed**) or a 192-d MECT-B2 speaker embedding; 43 languages, 27 built-in voices [https://huggingface.co/Zyphra/ZONOS3-Mini; https://huggingface.co/blog/Zyphra/zonos3-mini]. **Re-clone from `vc_ce7d47c6`'s source WAV; the saved ID does not port.**
- **License:** weights **Apache-2.0**, runtime/reference code **MIT** — cleanest commercial story [https://github.com/Zyphra/ZONOS3-Mini].
- **TTFC:** **[V]** 80 ms median at c=8 on H100, **<150 ms on M3 MacBook Air**, 457× realtime peak; **no independent local measurement [U]** [https://huggingface.co/blog/Zyphra/zonos3-mini]. Native MLX server streams progressive prefix chunks with **early partial-frame decode** (first prefix chunk decodes before its frame is complete) — genuinely streaming, not whole-clip [https://github.com/Zyphra/ZONOS3-Mini/blob/main/servers/mlx/README.md].
- **Quality:** ranks first among open-weight models on ZTTS2-Eval; 15 Hz ZAC-1 dual codec, 100M active / 800M total MoE, 44.1 kHz [https://arxiv.org/abs/2611.16525].
- **HW:** native Apple-Silicon **MLX server** (`servers/mlx`, all networks in MLX from GGUF, bit-exact Q4/Q8) at `127.0.0.1:1919`; also C++/ggml, WebGPU, SGLang-Omni. Runs on M2 Ultra [https://github.com/Zyphra/ZONOS3-Mini/blob/main/servers/mlx/README.md].
- **Integration:** the MLX server documents a **v2 API** (`servers/api/openapi.yaml`) with streaming PCM and cloning via `references:[{audio_path}]`/`speaker_audio_base64`, **not exactly `/v1/audio/speech`** (SGLang-Omni gives OpenAI-compatible serving). A thin adapter route in `providers` is likely needed **[U — verify exact route/chunk shape]** [https://huggingface.co/Zyphra/ZONOS3-Mini].

### 4.2 Kyutai Pocket TTS — best measured sub-100 ms local, CPU-only
- **Clone:** zero-shot from ~5 s audio (`--voice file.wav`); reference encoded once into a KV-cache and **exportable to `.safetensors` via `export-voice`** — the cleanest way to pin one speaker locally. Consent/prohibited-use form required [https://kyutai.org/blog/2026-01-13-pocket-tts/; https://andrew.ooo/posts/pocket-tts-kyutai-cpu-voice-cloning-review/].
- **License:** code **MIT**, weights **CC-BY-4.0** (gated); some bundled voices CC-BY-NC — check the specific voice; commercial use OK with attribution [https://hamsanders.github.io/soundbox/models/pocket-tts/; https://huggingface.co/latent-variable/pocket-tts-cloning-en].
- **TTFC:** emits one 80 ms frame at a time; **[V]** ~200 ms first chunk / ~6× realtime on 2 CPU cores; **[M, independent, Apple Silicon] RTF ≈8.9×, first chunk ≈35 ms**; another **[M]** short-text first audio 71/82 ms. ~100M params [https://kyutai-labs.github.io/pocket-tts/; https://github.com/mewc/pocket-tts-lab].
- **Quality:** **[V]** WER 1.84 / audio ELO 2016 on LibriSpeech test-clean, claimed on par or better than F5-TTS / Kyutai 1.6B / Chatterbox Turbo; **[M, independent]** clone quality Pocket > Supertonic > Inflect-Nano > Kokoro [https://kyutai.org/blog/2026-01-13-pocket-tts/].
- **HW:** **CPU-only by design** — no GPU needed on M2 Ultra or socrates [https://andrew.ooo/posts/pocket-tts-kyutai-cpu-voice-cloning-review/].
- **Integration:** `pocket-tts serve` local HTTP with an "OpenAI-compatible-ish" `/v1/audio/speech` on :8000 plus `/v1/audio/voice-clone`; needs a thin adapter for meept's exact streaming PCM [https://github.com/kyutai-labs/pocket-tts; https://docs.librewebui.org/kyutai-tts].

### 4.3 mlx-audio (Blaizzy→Neywa-Labs) serving Qwen3-TTS on M2 Ultra
- **Clone:** server forwards `ref_audio` + `ref_text` to cloning-capable models (path or base64); supports Qwen3-TTS Base, Chatterbox, Spark, VoxCPM, etc. [https://github.com/Blaizzy/mlx-audio/blob/a7ef9860/mlx_audio/server.py; https://deepwiki.com/Blaizzy/mlx-audio/6.2-rest-api-endpoints]. Re-clone `vc_ce7d47c6` from reference.
- **License:** **MIT** code (per-model weights licenses) [https://github.com/Blaizzy/mlx-audio; https://pypi.org/project/mlx-audio/]. Current v0.6.6 (2026-11-16) [https://github.com/Neywa-Labs/mlx-audio/releases].
- **TTFC:** model README benchmark Qwen3-TTS 6-bit short prompt **avg TTFB 84.8 ms**, throughput 1.67×, 3.88 GB [https://github.com/Blaizzy/mlx-audio/blob/main/mlx_audio/tts/models/qwen3_tts/README.md]. **Critical caveat:** third-party `akaiHuang/mlx-qwen3-tts-real-streaming` measured stock `mlx-audio stream=True` first-audio yield **4.311 s (1 chunk)** on M1 Max / Qwen3-TTS-0.6B-CustomVoice-4bit; their prefix-partial-decode prototype got **0.317 s** (chunk_tokens=4) / 0.191 s (chunk_tokens=2) [https://github.com/akaiHuang/mlx-qwen3-tts-real-streaming]. Also issue #898: streaming emits a full container per chunk (audible seams) — `raw`/`pcm` is the unaffected path; verify fix status [https://github.com/Blaizzy/mlx-audio/issues/898].
- **HW/integration:** M1–M4 Apple Silicon, Python 3.11+, MLX ≥0.32.4; already an OpenAI-compatible `/v1/audio/speech` with `stream`, `response_format`, `streaming_interval` (default **2.0 s** — lower it for low TTFC) [https://github.com/Blaizzy/mlx-audio/blob/main/docs/guides/web-ui-api-server.md]. **Lowest integration effort of the alternative engines** (same API shape as today).

### 4.4 Other re-clone-capable engines (brief)
- **CosyVoice3-0.5B** — code+weights **Apache-2.0**, zero-shot/cross-lingual cloning + bi-streaming [https://github.com/QwenAudio/CosyVoice; https://arxiv.org/abs/2505.17589]. **[V]** 150 ms first packet; **[M]** ~210 ms first chunk on L20/Triton and ~223 ms via a GGUF/C++ runtime, but a GitHub issue reports **1.1–2 s** in practice and the maintainer says full causal streaming needs unreleased work — "treat sub-200 ms as unmet" [https://developer.nvidia.cn/blog/nvidia-triton-tensorrt-llm-tts-app/; https://huggingface.co/yang-qi1222/cosyvoice3_stream.cpp; https://github.com/FunAudioLLM/CosyVoice/issues/1798]. On M2 Max 32 GB, hybrid MLX (move only the LLM stage) reached RTF 0.757 but whole-clip (TTFA = full synthesis) [https://www.drmhse.com/posts/running-funaudio-on-mac-mlx-pytorch/]. Community OpenAI-shaped servers exist [https://github.com/hsiang-han/CosyVoice3-API].
- **Fish Speech (v1.5 / S2-Pro)** — strong zero-shot clone (English Tier-1) [https://github.com/fishaudio/fish-speech]. **License is the catch:** v1.5 weights CC-BY-NC-SA; current `main`/S2 uses Fish Audio Community License v1.0 (free commercial under **$500k annual revenue**, hosted services need a separate license, but "an assistant's spoken replies" are carved out as not a hosted service); S2-Pro weights are research-only [https://raw.githubusercontent.com/fishaudio/fish-speech/main/LICENSE; https://huggingface.co/fishaudio/s2-pro]. Latency: **[M]** RTX 4090 v1.5 first chunk 187 ms ("Hi") → 433 ms (short) → **1152 ms** (long) [https://github.com/fishaudio/fish-speech/issues/1020]; **[V]** S2-Pro TTFA ~100 ms on H200 [https://arxiv.org/html/2603.08823v1]; slow on Apple MPS, no MLX build [https://github.com/fishaudio/fish-speech/pull/461]. Local API is `/v1/tts`, **not** `/v1/audio/speech` — needs a shim [https://github.com/fishaudio/fish-speech/blob/main/tools/api_server.py].
- **Chatterbox-Turbo / Nano (Resemble AI)** — MIT code + MIT weights, zero-shot clone from >5 s reference, **cacheable `Conditionals`** (enroll once) [https://github.com/resemble-ai/chatterbox]. **Project retired/unmaintained since Aug 2026** [https://github.com/resemble-ai/chatterbox]. Every output is force-watermarked (Perth) [https://www.socialvik.com/blog/chatterbox-tts-cpu-commercial-use]. No native streaming in the base repo; third-party fork measured **[M] 0.472 s first chunk on 4090 but 4.3 s on T4** [https://github.com/davidbrowne17/chatterbox-streaming]. OpenAI-compatible wrapper exists but Linux/NVIDIA only [https://github.com/wuxuedaifu/vllm-chatterbox-stream]. On M2 Ultra realistic TTFA is ~1–2 s — **roughly parity with the qwen3 baseline, not a win**.
- **Orpheus 3B (Canopy)** — **Apache-2.0** code+weights, zero-shot clone from the pretrained base, GGUF via llama.cpp works on both hosts; **[M] ~160–200 ms** TTFA on RTX 4090, no Apple-Silicon number **[U]**; community **Orpheus-FastAPI** exposes OpenAI-compatible `/v1/audio/speech` (drop-in) [https://github.com/canopyai/Orpheus-TTS; https://github.com/taresh18/orpheus-streaming; https://localaimaster.com/blog/orpheus-tts-setup-guide]. Clone setup is less turnkey than ZONOS3/Pocket.
- **GPT-SoVITS** — MIT code+weights, zero-shot + faithful 1-min fine-tune clone; **[M]** RTF 0.014 on 4090; streaming only via community forks, macOS unsupported [https://github.com/RVC-Boss/GPT-SoVITS; https://github.com/Lucas1479/Aqua-TTS].

### 4.5 Disqualified — cannot clone a new speaker
- **Kokoro-82M** — **Apache-2.0** code+weights, extremely fast ([M] **126 ms TTFC** streaming via `sonic-speech/kokoro-82m-bf16` on M3 Max; [M] ~71–74 ms full-audio on M2 Ultra via `kokoro-coreml`), but the model card states **"Lacks voice cloning capability"** — 54 preset voices only [https://huggingface.co/hexgrad/Kokoro-82M; https://huggingface.co/sonic-speech/kokoro-82m-bf16; https://huggingface.co/mattmireles/kokoro-coreml]. Community clone-tuners only approximate ([M] ~0.32 normalized speaker similarity vs 1.0, or ~83 min of clips + GPU fine-tune) [https://huggingface.co/remsky/kokoro-inno-clone-tuner; https://github.com/hidude562/kokoro-finetune].
- **Piper** — already integrated in meept (`tts_piper.go`) but **fixed voices / per-voice training only**; GPL engine. Cannot preserve the clone.
- **Kyutai TTS 1.6B** ([M] ~300 ms, 900–1200 ms with word-by-word LLM input) and **VibeVoice-Realtime-0.5B** ([M] ~81 ms onset) deliberately withhold the voice embedding/tokenizer needed to clone a new speaker [https://github.com/kyutai-labs/moshi/issues/404; https://github.com/microsoft/VibeVoice/blob/main/docs/vibevoice-realtime-0.5b.md]. **LinnetTTS** ([M] ~10 ms first audio on Apple M5) requires a paid license to clone a new voice [https://github.com/VoxRT/LinnetTTS]. **Supertonic 3** uses OpenRAIL-M weights + paid Voice Builder for custom voices; **[M] ~450–650 ms first byte on M4** [https://pypi.org/project/supertonic-server/0.1.0/].
- **License traps:** F5-TTS (CC-BY-NC weights) [https://github.com/SWivid/F5-TTS], Spark-TTS (CC BY-NC-SA weights) [https://huggingface.co/SparkAudio/Spark-TTS-0.5B], IndexTTS-2 (custom bilibili license) [https://huggingface.co/IndexTeam/IndexTTS-2/blob/main/LICENSE.txt] — avoid for a commercial managed service.

---

## 5. Cloud fallbacks (not local — listed for completeness only)

All are paid APIs, need a new provider adapter (WebSocket-first, not `/v1/audio/speech`), and would send the clone's reference audio off-box. Measured TTFA figures are from Coval's benchmark (includes network RTT from us-east-1 and, since 2026-06-03, leading silence; not comparable to pre-June numbers) [https://www.coval.ai/blog/time-to-first-audio-ttfa-benchmark/].

- **Cartesia Sonic 3.6** — Instant Voice Clone from 10 s reference; **[M] ~291 ms TTFA**, vendor claim sub-90 ms; WebSocket-first [https://docs.cartesia.ai/build-with-cartesia/capability-guides/clone-voices; https://apibenchmarks.com/tts/cartesia].
- **ElevenLabs Flash v2.5 / v4 Turbo** — IVC from 10 s (voice-captcha), clones cannot be exported; **[M] ~194–198 ms TTFA**, one independent office-connection measurement 492 ms P50; WebSocket with `chunk_length_schedule`/`flush` [https://elevenlabs.io/docs/eleven-api/concepts/voice-cloning; https://elevenlabs.io/docs/eleven-api/guides/how-to/websockets/realtime-tts].
- **Fish Audio s2.1-pro** — cloud, instant clone via inline reference or `reference_id`, WebSocket streaming; **[M] ~145–359 ms TTFA** [https://docs.fish.audio/features/voice-cloning].
- **OpenAI gpt-4o-mini-tts** — the only cloud option already matching meept's `/v1/audio/speech` shape, but custom voices are gated and **[M] ~1.56 s P50 TTFA**, far behind the local baseline; models deprecated, shut down 2027-01-06 [https://verli.app/blog/choosing-a-text-to-speech-api; https://developers.openai.com/api/docs/guides/text-to-speech].
- **Deepgram Aura 2 / Flux** — no self-serve clone; **[M] 203–300 ms TTFA**; WebSocket adapter needed [https://deepgram.com/pricing; https://docs.deepgram.com/docs/streaming-text-to-speech].
- **Dead/unusable:** Hume Octave 2 sunset 2026-11-13 [https://www.versely.studio/blog/hume-octave-2-emotional-tts-2026].

None of these beats the [LIVE] local ~0.37 s path once network RTT is included, and all add recurring cost + data-egress concerns for the cloned voice.

---

## 6. Integration notes into the `internal/voicechild/providers` pattern

The existing TTS seam is deliberately tiny:
- `voicechild.TTSProvider`: `Speak(ctx context.Context, text string) (io.ReadCloser, error)` [https://github.com/caimlas/meept — `internal/voicechild/pipeline.go:41-43`, repo-local].
- `providers.NewTTS(cfg, key)` dispatches on `cfg.Vendor` (`"openai"`, `"qwen3"`, `"piper"`); qwen3 is keyless and requires `BaseURL`; both qwen3 and openai build an `OpenAITTS` with a `ttsTransport` that has `DisableKeepAlives: true` [repo-local `providers.go`, `tts_openai.go`].
- The keys frame (C1) carries `tts_vendor`/`tts_model`/`tts_base_url`/`tts_voice` + `tts_ref_hash`, resolved per call via `voice.ResolveProfile` [repo-local `internal/voice/AGENTS.md`, `internal/daemon/voice_keys.go`].

Implications:
1. **Route change (rank 1) needs no new provider file** — just parameterize `openAISpeechRoute` for the qwen3 vendor and adjust the request body/response framing. The incremental `io.ReadCloser` streaming path already works; the child's depth-2 synth prefetch (`tryLaunchPrefetch`) will naturally benefit from earlier first audio.
2. **Filler clip cache is unaffected** — `clipstore.ClipKey` hashes `tts_vendor|tts_model|tts_voice|refHash`, not the route; cached clips remain valid. If the voice identity changes (re-clone on a new engine), the key changes and clips auto-regenerate.
3. **New engines** (ZONOS3, Pocket, CosyVoice) need a new `TTSProvider` implementation (subprocess or HTTP) + a `NewTTS` vendor case + keys-frame wiring. For HTTP engines that already speak `/v1/audio/speech`, this is a near-copy of `OpenAITTS`; for the ZONOS3 v2 API or a Pocket TTS shim, write a small request/response adapter that still returns a streaming `io.ReadCloser`.
4. **Barge-in:** any new provider must honour `ctx` cancellation so the child's `clear()`/playGen gate can kill in-flight synthesis — the `pcm-stream` route explicitly "aborts cleanly on client disconnect" [https://github.com/malaiwah/qwen3-tts-server], which fits.
5. **Sample rate:** keep resampling to the canonical 16 kHz L16@16k child chunks; a new engine's native rate (e.g. ZONOS3 44.1 kHz, Kokoro 24 kHz) goes through `newResamplingReader`.

---

## 7. Recommendation & next steps

**Primary:** implement the route change to `/v1/audio/speech/pcm-stream` on socrates (rank 1). This keeps `vc_ce7d47c6` untouched, needs no new model or license review, and [LIVE] cuts first audio from ~2.0 s to ~0.37 s — a 5× improvement on the exact hardware in use. Then stack the server-side tuning from §3.2 (cache clone latents, warm model, tune `chunk_size`, check GPU contention).

**Contingency:** if the live 2–6 s gap remains after the route change, it is almost certainly child-startup / clip-prewarm contention or GPU sharing, not synthesis — instrument at three points (child `Speak` call, HTTP first-byte, first resampled PCM write) before changing engines.

**Only if you want to move off qwen3 entirely:** preserve the reference WAV + transcript for `vc_ce7d47c6` first, then A/B-test **ZONOS3-Mini on the M2 Ultra** (best license + native MLX streaming + likely sub-150 ms) with **Kyutai Pocket TTS** (best *measured* low latency, CPU-only, exportable voice) as the fallback. Both require re-cloning and a new adapter; neither is proven to reproduce the exact cloned speaker.

**Do not switch to Kokoro/Piper for this leg** — they cannot clone the voice.

---

## 8. Sources

**Live probes (2026-11-18):** `http://socrates:8001/health`, `/v1/models`, `/openapi.json`; socket-level TTFB/cadence scripts `/tmp/tts_cadence.py` and `/tmp/tts_chunk.py`.

**Qwen3-TTS / existing server:**
- https://github.com/QwenLM/Qwen3-TTS
- https://github.com/QwenLM/Qwen3-TTS/blob/main/LICENSE
- https://arxiv.org/pdf/2601.15621
- https://huggingface.co/Qwen/Qwen3-TTS-Tokenizer-12Hz
- https://huggingface.co/Qwen/Qwen3-TTS-12Hz-1.7B-Base
- https://huggingface.co/Qwen/Qwen3-TTS-12Hz-1.7B-CustomVoice/discussions/45
- https://github.com/andimarafioti/faster-qwen3-tts
- https://github.com/malaiwah/qwen3-tts-server
- https://github.com/vllm-project/vllm-omni/pull/1583
- https://docs.vllm.ai/projects/vllm-omni/en/latest/serving/speech_api/
- https://vllm.ai/blog/2026-06-23-vllm-omni-tts
- https://github.com/akaiHuang/mlx-qwen3-tts-real-streaming
- https://ocdevel.com/blog/20260302-qwen-tts-voice-cloning
- https://developers.openai.com/api/docs/guides/text-to-speech

**Kokoro / MLX / Core ML:**
- https://huggingface.co/hexgrad/Kokoro-82M
- https://github.com/hexgrad/kokoro
- https://huggingface.co/sonic-speech/kokoro-82m-bf16
- https://huggingface.co/mattmireles/kokoro-coreml
- https://github.com/FluidInference/FluidAudio/blob/main/Documentation/TTS/Benchmarks.md
- https://huggingface.co/remsky/kokoro-inno-clone-tuner
- https://github.com/hidude562/kokoro-finetune
- https://github.com/Blaizzy/mlx-audio
- https://github.com/Blaizzy/mlx-audio/blob/main/docs/guides/web-ui-api-server.md
- https://github.com/Blaizzy/mlx-audio/blob/main/mlx_audio/tts/models/qwen3_tts/README.md
- https://github.com/Blaizzy/mlx-audio/issues/898
- https://github.com/Neywa-Labs/mlx-audio/releases
- https://github.com/Blaizzy/mlx-audio-swift

**Zero-shot clone engines:**
- https://huggingface.co/blog/Zyphra/zonos3-mini
- https://github.com/Zyphra/ZONOS3-Mini
- https://github.com/Zyphra/ZONOS3-Mini/blob/main/servers/mlx/README.md
- https://arxiv.org/abs/2611.16525
- https://kyutai.org/blog/2026-01-13-pocket-tts/
- https://kyutai-labs.github.io/pocket-tts/
- https://github.com/mewc/pocket-tts-lab
- https://andrew.ooo/posts/pocket-tts-kyutai-cpu-voice-cloning-review/
- https://github.com/QwenAudio/CosyVoice
- https://arxiv.org/abs/2505.17589
- https://github.com/FunAudioLLM/CosyVoice/issues/1798
- https://www.drmhse.com/posts/running-funaudio-on-mac-mlx-pytorch/
- https://github.com/hsiang-han/CosyVoice3-API
- https://github.com/fishaudio/fish-speech
- https://raw.githubusercontent.com/fishaudio/fish-speech/main/LICENSE
- https://github.com/fishaudio/fish-speech/issues/1020
- https://github.com/resemble-ai/chatterbox
- https://github.com/davidbrowne17/chatterbox-streaming
- https://github.com/wuxuedaifu/vllm-chatterbox-stream
- https://github.com/canopyai/Orpheus-TTS
- https://github.com/taresh18/orpheus-streaming
- https://github.com/RVC-Boss/GPT-SoVITS

**Disqualified / license traps / cloud:**
- https://github.com/kyutai-labs/moshi/issues/404
- https://github.com/microsoft/VibeVoice/blob/main/docs/vibevoice-realtime-0.5b.md
- https://github.com/VoxRT/LinnetTTS
- https://pypi.org/project/supertonic-server/0.1.0/
- https://github.com/SWivid/F5-TTS
- https://huggingface.co/SparkAudio/Spark-TTS-0.5B
- https://huggingface.co/IndexTeam/IndexTTS-2/blob/main/LICENSE.txt
- https://www.coval.ai/blog/time-to-first-audio-ttfa-benchmark/
- https://docs.cartesia.ai/build-with-cartesia/capability-guides/clone-voices
- https://elevenlabs.io/docs/eleven-api/concepts/voice-cloning
- https://docs.fish.audio/features/voice-cloning
- https://verli.app/blog/choosing-a-text-to-speech-api
- https://developers.openai.com/api/docs/guides/text-to-speech
- https://deepgram.com/pricing

**Repo-local:** `internal/voice/AGENTS.md`, `internal/voicechild/pipeline.go`, `internal/voicechild/providers/{providers.go,tts_openai.go,tts_piper.go}`, `docs/research/2026-10-28-uptractor-voice-stt-tts-apple-silicon.md`.
