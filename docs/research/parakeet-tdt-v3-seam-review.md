# Review: Parakeet TDT v3 for incoming voice-audio STT — can the `internal/stt` parakeet seam be reused?

**Date:** 2026-11-18 · **Status:** research only — no code changes, no commits · **Worktree:** `research/parakeet-tdt-v3` @ `5fdf0add`
**Question:** "I want to use Parakeet TDT v3 for incoming audio STT. I believe we're using parakeet elsewhere in meept for GUI/TUI interaction — do we need to implement a different seam?"

> **Citation style.** Local code facts carry a `file:line` reference read directly from this worktree. Every external claim carries an inline numbered citation `[n]` resolved in **Sources** at the end, and major claims also link the primary page actually fetched. All WER / RTFx / latency / footprint numbers are **vendor-, paper-, leaderboard-, or benchmark-reported — none were reproduced on meept's audio or on this host**. Benchmarks use different corpora, hardware and normalization: compare **within a row**, not across rows.

---

## Verdict

**Seam verdict: PARTIAL — a different seam is required, but it already exists.**

- **The `internal/stt` parakeet engine is NOT reusable for incoming voice audio.** It is a *client-side, whole-buffer* interface: `Transcriber.Start(ctx, onResult)` records locally, and `Stop()` shells out to `parakeet-transcribe` on a recorded WAV file (`internal/stt/parakeet.go:67-104,119-198`). The package header states outright that "all recording and transcription happens client-side; the daemon is not involved" (`internal/stt/transcriber.go:1-8`). Its only caller is the TUI chat model (`internal/tui/models/chat.go:224,449,467,3693-3728`). It also defaults to a *different checkpoint* — `parakeet-tdt-ctc-110m` (`internal/stt/parakeet.go:200-211`), a 110M CTC model, **not** TDT v3. This seam has no channel of incoming PCM from a phone/local leg, no incremental `Stream`, and no daemon/child path. It cannot be adapted in place.
- **But you do NOT need to invent a new seam.** Incoming voice-audio STT belongs to the **voice sidecar's existing `voicechild.STTProvider` interface** — `Stream(ctx, <-chan []byte) (<-chan Transcript, error)` (`internal/voicechild/pipeline.go:22-26`), with `Transcript{Text, Final}` (`:45-49`) and the optional `sttFlusher.FlushNow()` (`:28-37`). The correct implementation is a **new opt-in vendor adapter registered in `NewSTT`** (`internal/voicechild/providers/stt_deepgram.go:154-172`) — structurally the same as the existing cold-`whisper-cli` adapter (`internal/voicechild/providers/stt_whisper.go`), which is the closest analogue. No new interface, wire frame, or bus topic is needed.
- **Critical caveat about the model itself:** `parakeet-tdt-0.6b-v3` has **no supported true-streaming path with partial results** — not in NeMo, not in NVIDIA NIM, not in `parakeet.cpp`, not in NeMo-Speech.cpp's realtime WebSocket `[5][6][7][10][11][13]`. It is an outstanding **offline / whole-buffer** English transcriber (6.34% Open ASR Leaderboard avg WER `[1]`), which is exactly the *wrong* shape for low-latency turn-taking, but a fine fit if the adapter segments utterances itself (as `stt_whisper.go` already does) and emits one `Final:true` per utterance.

**Ranking for English for this use case (overall, weighted toward the known time-to-`heard` defect):**

1. **Kyutai STT 1B** — still the only option that attacks the actual latency defect (native streaming + semantic VAD, ~500 ms delay, ~125 ms flush); weakest raw accuracy on one independent bench, CC BY 4.0, en/fr only `[18][20]`.
2. **Nemotron-0.6B / Nemotron 3.5 (streaming)** — best latency/footprint/integration balance: true incremental streaming down to 80 ms, partials + clear over a realtime WS, ~0.7–1.5 GB, natively streaming `[5][12][14][19]`. If streaming matters, this is the NVIDIA family to pick — **not** TDT v3.
3. **Parakeet TDT v3 (this review)** — **best raw offline accuracy of the group** (6.34% avg `[1]`; 6.32% in the apples-to-apples paper `[12]`), CC BY 4.0, ~5.1 GB VRAM `[9]`. Ranked here because its **no-streaming / chunk-adapted** nature (9.22–12.83% chunked WER, BSF up to 2.03 `[12]`) makes it an accuracy play only, not a latency fix.
4. **Rumble-2-Flash** — 4.8% on the shared TypeWhisper dictation bench, Apache-2.0, warm server, **no streaming** `[18][20]`.
5. **Qwen3-ASR-0.6B** — 5.1% on the same bench, Apache-2.0; "streaming" is vLLM-only 2 s-chunk O(n²) re-feed, slower than meept's 500 ms endpointing `[18][20][21]`.
6. **whisper.cpp base.en (current)** — keep as default; 11.6% on that bench, no partials, cold subprocess `[18][20]`. Fix its tuning first.

**Recommendation:** do not switch the default. If the goal is *accuracy* on whole utterances, pilot TDT v3 as an additive opt-in `STTProvider` vendor (warm `parakeet.cpp`/NIM process, one `Final:true` per utterance, reusing `stt_whisper.go`'s segmentation pattern). If the goal is *time-to-`heard`*, TDT v3 is the wrong model — go to Nemotron streaming or Kyutai. In either case, implement against `voicechild.STTProvider`, not `internal/stt`.

---

## 1. The two existing STT seams — and why only one is relevant

Meept has **two unrelated STT seams**. Confusing them is the core of the user's question.

### 1.1 `internal/stt` — client-side GUI/TUI transcription (NOT reusable here)

| Fact | Evidence (local, read from worktree) |
|---|---|
| Package purpose | "provides speech-to-text transcription for the Meept TUI and Flutter clients … All recording and transcription happens client-side; the daemon is not involved." | `internal/stt/transcriber.go:1-8` |
| Interface | `Transcriber`: `Start(ctx, onResult func(Result))`, `Stop() (string, error)`, `IsRecording() bool`, `Name() string` | `internal/stt/transcriber.go:57-67` |
| Result type | `Result{Text string, IsFinal bool, Confidence float64}` | `internal/stt/transcriber.go:16-21` |
| Engines | `whisper` / `parakeet` / `native`, selected by `NewTranscriber(cfg)` | `internal/stt/transcriber.go:69-81` |
| Parakeet engine | `ParakeetEngine` shells out to `parakeet-transcribe --model <path> --input <wav-file>`; `Stop()` records to a WAV, calls `transcribe(filePath)`, returns one final string | `internal/stt/parakeet.go:16-22,67-104,119-198` |
| Parakeet config | `ParakeetConfig{BinPath, ModelPath}`; default model `~/.meept/models/parakeet-tdt-ctc-110m` | `internal/stt/parakeet.go:40-44,200-211` |
| Config surface | `[stt] engine: "whisper" \| "parakeet" \| "native"`; `stt.parakeet.{bin_path,model_path}` | `config/meept.json5:1311-1327`; selectors in `internal/configui/sections_stt.go:12,22-26` and `ui/flutter_ui/lib/features/settings/settings_panel.dart:1245` |
| Only caller | TUI chat model holds `transcriber stt.Transcriber`; `InitSTT` → `stt.NewTranscriber(cfg)`; start/stop recording drives `onResult` | `internal/tui/models/chat.go:224,449,467,3693-3728` |

**Why it cannot be reused for incoming voice audio:**

1. **Wrong interface shape.** `Start`/`Stop` owns the *recorder* and pushes results through a callback. The voice pipeline already owns the audio pump (`pumpAudioIn` → `pcmCh`) and needs `Stream(ctx, <-chan []byte)` to consume it (`internal/voicechild/pipeline.go:1457-1533,1535-1561`). There is no way to feed existing PCM into `ParakeetEngine` — its only entry point is a file path passed to a subprocess (`internal/stt/parakeet.go:119-131`).
2. **Wrong process boundary.** It is explicitly client-side; the voice daemon/child is server-side. Every recording/transcription in this seam happens in the TUI/Flutter client (`internal/stt/transcriber.go:8`).
3. **Wrong result semantics.** It returns one final string on `Stop()` with a `Confidence` field; the voice pipeline needs a stream of `Transcript{Text, Final}` with partials/barge-in and a `FlushNow()` early-finalization hook (`internal/voicechild/pipeline.go:24-37,1186-1205`).
4. **Wrong checkpoint.** It defaults to `parakeet-tdt-ctc-110m` — a 110M CTC model, not TDT v3 (`internal/stt/parakeet.go:200-211`). Even "reuse the parakeet engine" would not give you TDT v3 without changing the model path *and* the runtime.
5. **Wrong lifecycle.** `parakeet-transcribe` is invoked fresh per utterance (cold subprocess), the same anti-pattern already identified as the dominant latency term in the current whisper voice adapter `[19][20]`.

### 1.2 `internal/voicechild` — the voice sidecar seam (THE correct seam)

| Fact | Evidence (local) |
|---|---|
| Interface | `STTProvider.Stream(ctx, pcm <-chan []byte) (<-chan Transcript, error)` | `internal/voicechild/pipeline.go:22-26` |
| Transcript | `Transcript{Text string, Final bool}`; final starts a turn, partial is a preview | `internal/voicechild/pipeline.go:45-49,1186-1205` |
| Early-flush hook | optional `sttFlusher.FlushNow()` for adapters that segment utterances themselves | `internal/voicechild/pipeline.go:28-37`, implemented by whisper at `providers/stt_whisper.go:112-121` |
| Vendor switch | `NewSTT(cfg ProviderConfig, key string)` dispatches `"deepgram"` / `"whisper"`; added vendors slot in here | `internal/voicechild/providers/stt_deepgram.go:147-172` |
| Per-call config | `ProviderConfig{Vendor, Model, BaseURL, Voice, SampleRate, Language}` carried on the C1 keys frame (`stt_vendor` / `stt_model` / `stt_base_url`) | `internal/voicechild/providers_wiring.go:9-24` |
| Factory injection | `STTFactory func(ProviderConfig, string) (STTProvider, error)`; `cmd/meept-voice` injects real constructors; tests inject fakes | `internal/voicechild/providers_wiring.go:26-35,37-45` |
| Audio feed | `pumpAudioIn` reads the C2 `<ccid>.in` unix socket, taps VAD/barge-in, forwards 20 ms PCM chunks into bounded `pcmCh` (oldest-chunk drop) | `internal/voicechild/pipeline.go:1457-1561` |
| Resilience | exactly-once `Stream` startup + bounded 3× open retry; permanent failure leaves the call "deaf" | `internal/voicechild/pipeline.go:544-625` |
| Existing local analogue | `WhisperSTT`: buffers PCM, closes an utterance on trailing silence (500 ms) / wall-idle / `FlushNow`, writes a WAV, runs a cold CLI subprocess, emits one `Final:true` | `internal/voicechild/providers/stt_whisper.go:78-101,140-171,174-329,374-396` |

**Key insight:** a TDT v3 adapter is a *near-structural clone of `WhisperSTT`* — both are offline/whole-buffer recognizers fed PCM chunks. The adapter buffers voiced PCM, segments on silence/`FlushNow`/max-window, writes a WAV (`writeWAV16` at `stt_whisper.go:398-436` is already there and reusable), and calls a recognizer on the whole buffer. Because TDT v3 has no partial-results API `[5][10][11][13]`, the adapter would emit `Final:true` only — exactly what `WhisperSTT` does today (`stt_whisper.go:78-83`). This keeps every downstream contract (non-speech filtering, echo provenance, short-final gate, turn aggregation, C5 barge-in) byte-identical.

### 1.3 Side-by-side interface comparison

| Dimension | `internal/stt.Transcriber` (GUI/TUI) | `voicechild.STTProvider` (voice leg) |
|---|---|---|
| Input | mic owned by `Recorder` (client) | `pcm <-chan []byte` from C2 socket (daemon/child) |
| Method | `Start(ctx,onResult)` / `Stop()` | `Stream(ctx, pcm) <-chan Transcript` |
| Output | `Result{Text,IsFinal,Confidence}` via callback + final string on `Stop()` | `Transcript{Text,Final}` channel |
| Incremental feed | no | yes (20 ms chunks) |
| Early flush | no | optional `FlushNow()` |
| Process | client-side subprocess on a WAV file | child-side subprocess or warm server |
| Caller | TUI chat only (`chat.go`) | voice pipeline (phone + local legs) |
| Reusable for voice? | **No** | **Yes — this is the target** |

### 1.4 Is there shared code worth extracting?

**Very little, and none worth a cross-package extraction.** The tempting shared pieces are:

- **WAV writing** (`writeWAV16` in `stt_whisper.go:398-436`): could be lifted to a tiny `internal/voiceframe`/`internal/audio` helper so a TDT adapter and `ParakeetEngine` share it. But the TDT voice adapter would live in `internal/voicechild/providers`, which already reuses `stt_whisper.go`'s copy directly — one duplicated 40-line RIFF header is not worth a new shared package (and a new package trips the e2e new-package census per `AGENTS.md`). **Recommendation: copy, don't extract.**
- **Silence segmentation / RMS gate** (`rmsGate`, `whisperSilenceMs`, `whisperWallIdleMs` in `stt_whisper.go:26-53,331-368`): already in `providers`; a TDT adapter in the same package reuses them as-is. No extraction needed.
- **The `parakeet.cpp` binary/model resolution** in `internal/stt/parakeet.go:200-233`: client-side `~/.meept` path resolution is wrong for the child's `ProviderConfig` (model comes from the C1 keys frame). Not reusable.
- **Nothing from `cmd/meept-voice`/`voicechild` should move into `internal/stt`** — that would invert a deliberate boundary (`AGENTS.md`: jargo imported only by `cmd/meept-voice` + `internal/voicechild`; child confinement).

**Conclusion:** implement a new adapter in `internal/voicechild/providers` (e.g. `stt_parakeet.go`) and add a `"parakeet"` case to `NewSTT`. Reuse `stt_whisper.go`'s segmentation + WAV helpers locally. No new shared package.

---

## 2. What Parakeet TDT v3 actually is

| Property | Finding | Source |
|---|---|---|
| Checkpoint | `nvidia/parakeet-tdt-0.6b-v3` | [HF card][1] |
| Architecture | FastConformer encoder + Token-and-Duration Transducer (TDT) decoder, 600M params | [HF card][1], [NeMo docs][3] |
| Languages | 25 (English + 24 European); auto language detection | [HF card][1] |
| License (weights) | **CC BY 4.0** — "permissive", "ready for commercial/non-commercial use"; attribution required | [HF card][1] |
| Features | auto punctuation/capitalization, word + segment timestamps, long-form up to 24 min full attention (A100 80GB) / 3 h local attention | [HF card][1] |
| Technical report | arXiv:2509.14128 | [HF card][1] |
| Note on NeMo docs | NeMo's checkpoint table lists `parakeet-tdt-0.6b-v3` under "English" while the HF card says 25 languages — a vendor-doc inconsistency; treat the HF card (25) as authoritative for capability. | [NeMo docs][3] vs [HF card][1] |

**Checkpoint confusion to avoid:** `internal/stt` uses `parakeet-tdt-ctc-110m` (110M CTC) `[local: parakeet.go:210]`. That is a different, smaller model. "Parakeet" is a family name; TDT v3 is a specific 600M multilingual checkpoint. NVIDIA's own docs caution that the `stt_*_fastconformer_*`, Parakeet, and Nemotron checkpoints share the same FastConformer encoder and differ mainly in branding/decoder `[3]`.

---

## 3. English accuracy and speed

### 3.1 Official — HF Open ASR Leaderboard (greedy, no external LM, punctuation/case removed from refs) `[1]`

| Model | Avg WER | AMI | Earnings-22 | GigaSpeech | LS test-clean | LS test-other | SPGI | TED-LIUM-v3 | VoxPopuli |
|---|---|---|---|---|---|---|---|---|---|
| `parakeet-tdt-0.6b-v3` | **6.34%** | 11.31 | 11.42 | 9.59 | **1.93** | 3.59 | 3.97 | 2.75 | 6.14 |

The HF card publishes **no numeric RTFx/throughput** — only qualitative "high-throughput" and a supported-hardware list (A10/A100/A30/H100/L4/L40/T4/V100) `[1]`.

### 3.2 Independent GPU benchmark — E2E Networks, NVIDIA L4 (23 GB), NeMo 2.7.0 `[9]`

- Best config: **bf16, batch=8 → RTF 0.0042 (238.9× real-time), p50 latency 0.029 s, peak VRAM 5,151 MB, 50.9 W** `[9]`.
- fp16 baseline (batch=1): RTF 0.0125 (79.9×), p50 0.087 s, VRAM 5,171 MB `[9]`.
- fp32 gives no accuracy gain; beam search is *worse* than greedy `[9]`.
- VRAM stays ~5.1 GB through batch 32; jumps to 7,143 MB at batch 64 `[9]`.
- **Caveat:** the benchmark's WER (15.7%) is inflated ~2–3 pts by different normalization and is **not comparable** to the 6.34% card figure; no concurrent-stream latency or TTFT was measured `[9]`.

### 3.3 Independent CPU/streaming benchmark — arXiv:2604.14493 (Microsoft CoreAI) `[12]`

- Batch WER **6.32%** (apples-to-apples with other models in that paper) `[12]`.
- Chunked/streaming degrades sharply: **best chunked config 9.22%** (10.4 s total context, bf16) — a **46% relative increase** over batch; selected comparison configs reach **12.83%** (BSF 2.03, RTFx 1.38 at 2.4 s delay) `[12]`.
- A 3.2 s config blows up to 32.17% WER — extreme sensitivity to chunk boundaries `[12]`.
- Conclusion from the paper: models not designed for streaming incur substantial penalties when chunk-adapted; Nemotron's cache-aware architecture is the better streaming choice `[12]`.

---

## 4. Streaming — the decisive finding

**`parakeet-tdt-0.6b-v3` has no supported true-streaming / partial-results path anywhere in its runtimes.** This is the single most important fact for the seam decision.

| Runtime | Finding | Source |
|---|---|---|
| **NeMo chunked inference** (`speech_to_text_streaming_infer_rnnt.py`) | Inputs are `audio_dir` + `dataset_manifest` — **file/manifest-based offline chunking**, not live mic. "Streaming" = feeding audio files in small chunks (`chunk_secs=2`, `right_context_secs=2`, `left_context_secs=10`) while carrying decoder state. Latency ≈ `chunk_secs + right_context_secs`. | [NeMo script][6], [NeMo README][5] |
| **HF discussion #13** (user asked about mic chunks) | NVIDIA points to that script and PR #14759 "as a reference for chunked streaming with microphone." PR #14759 is an **open draft Gradio demo**, not a deployable server. No official live mic API for TDT v3. | [HF discussion #13][7], [NeMo PR #14759][8] |
| **`parakeet.cpp` (mudler/parakeet.cpp, LocalAI)** — note: there is no `ggml-org/parakeet.cpp` repo; ggml's work is in whisper.cpp PR #3735 | v3 is listed as plain "TDT / 0.6B" with **no streaming annotation**. `--stream` flag is only for `parakeet_realtime_eou_120m-v1` and Nemotron cache-aware checkpoints. Its C API has `parakeet_capi_stream_feed` but only for those other checkpoints, emitting **monotonic chunk finals that are never revised** — not revisable `is_final=false` interims. CLI stdin is WAV bytes (`--input -`), not raw incremental PCM. | [parakeet.cpp README][10], [C API docs][10b] |
| **`nemo-speech serve`** (NeMo-Speech.cpp) | Exists, with HTTP `POST /v1/audio/transcriptions` and a **realtime WebSocket** `/v1/audio/transcriptions/realtime` (binary PCM16 + `session.update`; `.delta` partials, `.completed` finals). **But:** "the offline-only Parakeet TDT model does not support streaming endpointing" — only buffered CTC / cache-aware RNNT checkpoints get `streaming: true` and a working WS. A TDT v3 session gets `streaming: false` and the WS is rejected. | [NeMo-Speech.cpp server][11], [API][11a], [config docs][13] |
| **NVIDIA NIM** | "The Parakeet TDT NIM supports only the offline API" (offline gRPC `transcription` + HTTP `/v1/audio/transcriptions`). Interim results via Riva gRPC `StreamingRecognize` are for CTC/RNNT NIMs, not TDT. | [NIM Parakeet TDT][14], [NIM protos][14b], [NIM CTC][14c] |

**Practical consequence for meept:** a TDT v3 adapter **must segment utterances itself** and emit `Final:true` per utterance (the `WhisperSTT` pattern at `stt_whisper.go:174-329`). It will **not** feed partial-driven barge-in or `voice.context_warm` (which relies on STT partials per `AGENTS.md`). If partials are required, TDT v3 is disqualified and the choice is a natively streaming checkpoint (Nemotron cache-aware RNNT via `nemo-speech` WS, or Parakeet CTC/RNNT via Riva gRPC) `[11][13][14]`.

---

## 5. Footprint

| Source | Figure | Source |
|---|---|---|
| On-disk weights (HF/NeMo) | 2.51 GB ( FP32 NeMo .nemo) | [arxiv paper, Table 1/5][12] |
| Peak VRAM, L4, fp16/bf16 | ~5.1 GB (5,143–5,171 MB) | [E2E benchmark][9] |
| GGUF (mudler/parakeet-cpp-gguf) CPU | F16 1441 MB, Q8_0 941 MB, Q6_K 813 MB, Q5_K 742 MB, Q4_K 675 MB | [HF API tree][15] |

On the M2 Ultra 64 GB primary host this is comfortable; on an RTX 3060 (~5.6 GB free with qwen3-tts resident, per prior research `[20]`) a 5.1 GB VRAM footprint is tight — a Q8/Q6 GGUF via `parakeet.cpp` (~0.9/0.8 GB) is the realistic local path, at the cost of losing the GPU throughput advantage.

---

## 6. Server / WS options — concretely

| Option | Protocol | Streaming for TDT v3? | Notes |
|---|---|---|---|
| `parakeet.cpp` HTTP server (`parakeet-server`) | `POST /v1/audio/transcriptions`, WAV only, one request at a time | **No** — whole-buffer only; no WS | Local, ggml, Metal/CUDA/CPU backend `[10]` |
| NeMo-Speech.cpp `nemo-speech serve` | HTTP + realtime WebSocket + playground; separate `riva_server` for Riva gRPC | **No** for TDT v3 (offline-only endpointing) — WS requires a cache-aware/CTC model | "NIM is the supported production deployment path; this server is intended for local use." `[11][13]` |
| NVIDIA NIM (Parakeet TDT) | HTTP REST + gRPC (batch + streaming for some models) + WS realtime | **No** — TDT NIM is offline-only | Managed container, separate license `[14]` |
| Custom gRPC/WS wrapper | roll your own around `parakeet.cpp` C API | Only monotonic chunk finals for non-TDT checkpoints; no revisable interims | Highest effort, no upside over the above `[10b]` |

**Bottom line:** there is no turnkey "TDT v3 streaming server." Any adapter would be a warm **offline** recognizer with client-side segmentation — same operating model as the current whisper adapter, just with a different engine and a much better WER.

---

## 7. English comparison table (shared paper + shared independent bench)

From arXiv:2604.14493 (CPU, batch=1, eight ESB datasets) `[12]` — the only source with a clean cross-model streaming comparison:

| Model | Size | Batch WER | Delay | Streaming WER | BSF | RTFx | Streaming? |
|---|---|---|---|---|---|---|---|
| Qwen3-ASR-1.7B | 4.70 GB | 5.90 | 2.4 s | 10.45 | 1.77 | 0.49 | chunk-adapted |
| Qwen3-ASR-0.6B | 1.88 GB | 6.69 | 2.4 s | 10.41 | ~1.56 | 46.3 | chunk-adapted |
| **Parakeet TDT-0.6B-v3** | 2.51 GB | **6.32** | 2.4 s | **12.83** | **2.03** | 1.38 | **chunk-adapted (offline)** |
| Canary-1B-v2 | 6.36 GB | 7.15 | 4.8 s | 12.45 | 1.74 | 2.93 | chunk-adapted |
| Nemotron-0.6B | 2.47 GB | 7.07 | 0.56 s | **7.28** | **1.03** | 2.46 | **natively streaming** |
| Whisper-v3-Turbo | 1.62 GB | 7.83 | — | no row | — | — | batch only |
| Conformer Trans. XL | 2.58 GB | — | 2.4 s | 11.06 | — | 1.27 | chunk-only |

From the shared TypeWhisper dictation bench (4,649 clips, normalized WER) `[18][20]` — the cleanest *independent* relative ranking:

| Model | WER EN | ms/clip | Streaming? |
|---|---|---|---|
| Rumble-2-Flash | **4.8%** | 93 | no |
| Qwen3-ASR-0.6B (8-bit) | 5.1% | 315 | pseudo (2 s chunks) |
| Nemotron-3.5 (FluidAudio, 10 s blocks) | 11.7% | 188 | yes (but fed in blocks here) |
| whisper.cpp base (current meept) | 11.6% | 590 | no |
| Kyutai STT 1B | 14.4% | — | yes |

**Reading:** TDT v3's accuracy is excellent in *batch/whole-utterance* mode (6.34% official / 6.32% paper — better than Nemotron, Whisper, Canary, Qwen-0.6 in that paper) but collapses when forced into chunks (BSF 2.03, worse than every natively streaming model). **Kyutai and Rumble have no row in the arxiv paper** `[12]`; the sibling reports' TypeWhisper bench is the only shared independent comparison available, and no credible head-to-head English benchmark was found that includes all of Kyutai, Rumble, and TDT v3. Treat cross-bench comparisons as directional only.

---

## 8. Integration estimate into the voicechild seam

Additive, opt-in, behind the existing seam — no interface changes, no wire changes, no new bus topics:

1. **New file** `internal/voicechild/providers/stt_parakeet.go`: `ParakeetSTT` struct implementing `voicechild.STTProvider`. Copy `WhisperSTT`'s segmentation loop (`stt_whisper.go:174-329`), RMS gate (`:331-368`), `FlushNow` kick (`:92-138`), and `writeWAV16` (`:398-436`); replace `recognizeOne` with a call to the configured engine:
   - local: warm `parakeet.cpp` HTTP `POST /v1/audio/transcriptions` on the WAV (one subprocess/server, not per-utterance cold start);
   - or server: NIM offline gRPC/HTTP `[14]`.
2. **Extend `NewSTT`** (`stt_deepgram.go:154-172`) with `case "parakeet":` → build the adapter from `ProviderConfig` (`Model`, `BaseURL`, `SampleRate`); no API key (local) — mirror `newWhisperSTT`'s `ErrLocalModelRequired` pattern (`stt_whisper.go:140-156`).
3. **Config/keys frame:** extend the daemon's keys-frame emitter to send `stt_vendor=parakeet` / `stt_model` / `stt_base_url` (the C1 frame already carries these fields — `providers_wiring.go:9-24`). Add an opt-in config selector; default stays `whisper` (byte-identical boot).
4. **Tests:** per repo policy, a hermetic e2e suite under `e2e/suites/<name>/` with a `manifest.json` entry (new *file* in an existing package is fine — no new package, so the census is satisfied). Unit test the fake-engine contract like `stt_whisper_test.go`.
5. **Docs:** update `docs/workflows/voice-sidecar.md` (STT vendor list) and this report's status.

**Effort:** moderate — essentially a copy/adapt of `stt_whisper.go` plus a recognizer client and one switch case. No new daemon/child protocol work. **Risk:** low to the call path (one more opt-in vendor, default unchanged); the main risk is *expectation* — a TDT v3 adapter will not improve time-to-`heard` and cannot feed partial-driven lanes.

---

## 9. Unresolved questions / honest limitations

- **Nothing was reproduced locally.** No model download, GPU run, WER measurement, or mic test was performed — all performance figures are reported by others (see citation style note).
- **No TDT v3 partial-results API** exists in any reviewed runtime `[5][7][10][11][13][14]`; the "streaming" scripts are offline chunk simulations or Gradio demos. If a live partials API appears, re-evaluate — the adapter would then need a different emit loop than `WhisperSTT`.
- **Kyutai / Rumble / TDT v3 are never compared head-to-head** in the arxiv paper `[12]`; the shared TypeWhisper bench `[18][20]` is the only common denominator and uses different runtimes (MLX 8-bit, cloud API, whisper.cpp CLI). Cross-bench ranking is directional.
- **VRAM on the RTX 3060** (prior research: ~5.6 GB free with qwen3-tts resident `[20]`) is tight for a 5.1 GB fp16 TDT v3; a Q8 GGUF path needs a `parakeet.cpp` server with unverified latency on that host.
- **The `internal/stt` parakeet engine's default model is CTC-110M, not TDT v3** (`parakeet.go:210`) — confirm the user does not actually mean the existing small CTC model by "parakeet."
- **License nuance:** CC BY 4.0 requires attribution; acceptable for self-hosting, but note it is *not* the Apache-2.0 tier of Rumble/Qwen/NeMo `[1][18][20]`.

---

## Sources

[1] https://huggingface.co/nvidia/parakeet-tdt-0.6b-v3 — official model card (license, features, Open ASR Leaderboard WER table, streaming script).
[1b] https://huggingface.co/nvidia/parakeet-tdt-0.6b-v3/raw/main/README.md — raw card (frontmatter license `cc-by-4.0`, governing terms).
[3] https://docs.nvidia.com/nemo/speech/latest/asr/asr_checkpoints.html — NeMo checkpoint table & FastConformer family note.
[5] https://github.com/NVIDIA/NeMo/blob/main/examples/asr/asr_chunked_inference/README.md — "streaming or buffered inference of audio files".
[6] https://github.com/NVIDIA/NeMo/blob/main/examples/asr/asr_chunked_inference/rnnt/speech_to_text_streaming_infer_rnnt.py — chunked inference script (inputs, chunk/context params).
[7] https://huggingface.co/nvidia/parakeet-tdt-0.6b-v3/discussions/13 — NVIDIA answer on mic chunks; points to script + PR #14759.
[8] https://github.com/NVIDIA/NeMo/pull/14759 — open draft Gradio streaming demo.
[9] https://www.e2enetworks.com/blog/benchmarking-asr-models-nvidia-l4-parakeet-whisper-nemotron — E2E Networks L4 benchmark (RTF, throughput, VRAM, power).
[10] https://github.com/mudler/parakeet.cpp/blob/master/README.md — parakeet.cpp model table, `--stream` scope, HTTP server.
[10b] https://github.com/mudler/parakeet.cpp/blob/master/docs/capi.md — C API `parakeet_capi_stream_feed` semantics.
[11] https://github.com/NVIDIA/NeMo-Speech.cpp/blob/main/docs/server.md — `nemo-speech serve` HTTP + realtime WS + riva gRPC server.
[11a] https://github.com/NVIDIA/NeMo-Speech.cpp/blob/main/docs/api.md — realtime WS protocol; offline models reject WS.
[12] https://arxiv.org/html/2604.14493v2 — "Pushing the Limits of On-Device Streaming ASR" (batch/streaming WER, BSF, RTFx, sizes). (abs: https://arxiv.org/abs/2604.14493)
[13] https://github.com/NVIDIA/NeMo-Speech.cpp/blob/main/docs/asr/configuration.md — "offline-only Parakeet TDT model does not support streaming endpointing".
[14] https://docs.nvidia.com/nim/speech/latest/asr/deploy-asr-models/parakeet-tdt.html — "Parakeet TDT NIM supports only the offline API".
[14b] https://docs.nvidia.com/nim/speech/latest/reference/api-references/asr/protos.html — Riva gRPC `StreamingRecognize` / `interim_results`.
[14c] https://docs.nvidia.com/nim/speech/latest/asr/deploy-asr-models/parakeet-ctc-en-us.html — streaming-capable Parakeet CTC NIM.
[15] https://huggingface.co/api/models/mudler/parakeet-cpp-gguf/tree/main?recursive=true — GGUF quant sizes.
[18] Prior local research: `docs/research/{qwen3-asr-06b-review.md,rumble2flash-review.md,nemotron-35-asr-review.md}` — shared TypeWhisper bench rows, license/ranking, GPU reasoning.
[19] Prior local research: `docs/research/realtime-stt-options.md` — time-to-`heard` diagnosis (cold subprocess + fixed waits), recommendation #4.
[20] Prior local research: `docs/research/nemotron-35-asr-review.md`, `docs/research/qwen3-asr-06b-review.md` — RTX 3060 headroom, TypeWhisper WER table, ranking.
[21] https://github.com/QwenLM/Qwen3-ASR — vLLM-only 2 s-chunk streaming, O(n²) re-feed (via prior local research).

**Local sources (worktree `research/parakeet-tdt-v3` @ `5fdf0add`):** `internal/stt/transcriber.go:1-8,16-21,57-81`; `internal/stt/parakeet.go:16-22,40-44,67-104,119-131,200-233`; `internal/tui/models/chat.go:224,449,467,3693-3728`; `internal/voicechild/pipeline.go:22-37,45-49,544-625,1186-1205,1457-1561`; `internal/voicechild/providers_wiring.go:9-45`; `internal/voicechild/providers/stt_deepgram.go:147-172`; `internal/voicechild/providers/stt_whisper.go:26-53,78-101,112-138,140-171,174-329,331-368,374-436`; `config/meept.json5:1311-1327`; `internal/configui/sections_stt.go:12,22-26`; `ui/flutter_ui/lib/features/settings/settings_panel.dart:1245`.
