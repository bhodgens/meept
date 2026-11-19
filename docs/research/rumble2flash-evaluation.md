# Evaluation record: Reson8/Rumble-2-Flash for issue #161

**Date:** 2026-11-19 · **Status:** evidence-based evaluation complete; live
pilot deferred (no software install or model download performed)
**Issue:** [#161](https://github.com/caimlas/meept/issues/161) ·
**Research:** [rumble2flash-review.md](rumble2flash-review.md) (`964fd536`)

> This record separates MEASURED facts from REPORTED facts. A number marked
> REPORTED comes from a vendor, leaderboard, or paper and was NOT reproduced
> on socrates or on meept's audio. A number marked MEASURED was produced by a
> command in this evaluation and has the command beside it.

## Verdict

Proceed with the additive, opt-in implementation in
`docs/plans/20261118-rumble-stt-provider/`, and keep whisper as the default.
Rumble is the ACCURACY track, not the `heard`-latency fix. The plan's batch-only
adapter matches the documented API. A live pilot on socrates is the remaining
acceptance evidence for WER, time-to-`heard`, hallucination behavior, and VRAM
coexistence; it needs a vLLM install and model downloads and is deferred.

## Evaluation criteria from #161, status per criterion

| # | Criterion | Status | Evidence |
|---|-----------|--------|----------|
| 1 | Time-to-`heard` on meept audio, warm server, qwen3-tts resident | DEFERRED (live) | No vLLM on socrates; no Rumble weights. Warm-server removal of the cold `whisper-cli` start is a MECHANISM claim, not a measured delta. |
| 2 | English WER on own prompts vs whisper base.en, Qwen3-ASR-0.6B, Kyutai | PARTIAL: whisper baseline MEASURED; Rumble/Qwen3-ASR/Kyutai REPORTED only | Baseline lane below; reported figures in research doc line 78. |
| 3 | Hallucination on silence/music ("silence stays silent") | PARTIAL: whisper MEASURED; Rumble REPORTED | Lane output below; reported claim at research doc line 35. |
| 4 | VRAM headroom beside qwen3-tts, explicit low GPU cap | MEASURED host state + PLAN PIN | `nvidia-smi` below; cap 0.20-0.25 in leaf 02. |
| 5 | Integration via `STTProvider.Stream`, opt-in, whisper default | VERIFIED in code + PLAN | Seam and factory anchors below. |
| 6 | License Apache-2.0 (code + weights) | CONFIRMED from committed primary-source citations | Research doc lines 17, 27-29. Live re-check blocked by consent gate; not retried. |

## MEASURED: whisper base.en baseline on meept's audio

Command (run from the repo root, weights downloaded to a scratch dir):

```
MEEPT_VOICE_EVAL_MODEL_DIR=/tmp/rumble-eval-models/model \
  go test -p 2 ./internal/voice/eval/ -run TestLaneEndToEnd -v -count=1
```

Weights: `ggml-base.en.bin`, 147,964,211 bytes, from the whisper.cpp HuggingFace
repo. Model bytes are data, not installed software; no package install was
performed. Result: **11 of 11 cases PASS** in 3.00 s.

| Fixture | WER | Key hits | Transcript (as heard) |
|---------|-----|----------|-----------------------|
| demo-utterance | 5% | 1/1 | "Hi, this is Mike. Please move the poor to Friday morning and add 10 yards of gravel to the job." |
| work-order-02 | 0% | 3/3 | "I need to add a work order for Dakota Drywall." |
| delivery-friday-03 | 44% | 1/2 | "Move the tri-wall delivery to Friday at 8am." |
| open-orders-04 | 0% | 1/1 | "Can you read me the open work orders?" |
| crew-late-05 | 0% | 0/0 | "Leave a message for the crew. I'll be late tomorrow." |
| meadow-open-06 | 14% | 0/1 | "What's open on the metal ridge job?" |
| roof-flashing-07 | 12% | 1/2 | "The roof flashing at meta Ridge is finished." |
| log-drywall-08 | 25% | 2/2 | "Lock 40 sheets of drywall for Dakota drywall." |

Mean WER: **12.5%** (simple mean of the eight per-case WER values; the lane's
pass bars are 34-45%, so PASS means "within threshold", not "accurate").

Reading of the errors: systematic proper-noun and domain-word substitution
(pool→poor, drywall→tri-wall, Meadow Ridge→metal ridge/meta Ridge, Log→Lock).
This is the error class Rumble's reported 6.60% English WER would target.

Non-speech behavior: `leak-silence.wav` → empty (RMS gate suppresses it);
`leak-hiss.wav` → "(blender whirring)"; `leak-music.wav` → "(eerie music)".
All three PASS the lane because its needles are PROMPT vocabulary, and none
appeared. Note honestly: the two non-silence fixtures DID produce text. That
is the behavior the Rumble "silence stays silent" claim would need to beat in
the live pilot; this baseline does not prove Rumble is better.

## MEASURED: socrates host state (read-only, 2026-11-19)

```
ssh caimlas@socrates nvidia-smi --query-gpu=name,memory.total,memory.free --format=csv,noheader
Tesla PG500-216, 32768 MiB, 3641 MiB
NVIDIA GeForce RTX 3060, 12288 MiB, 5242 MiB
```

- qwen3-tts is resident on GPU 1 (RTX 3060), PID 1548051,
  `.venv/bin/python server.py --port 8001`, cwd `/home/caimlas/git/qwen3-tts-server`.
- No vLLM binary or Python module in the qwen3-tts venv or system Python.
- No Rumble / Qwen3-ASR / Kyutai weights in the HuggingFace cache.
- Disk: 288 GB free on `/home`. Model download is feasible; it was not done.

VRAM analysis: the 3060 has ~5.2 GB free. vLLM's default
`--gpu-memory-utilization 0.92` is a fraction of TOTAL memory (~11.3 GB of
12 GB) and would crowd out or fail beside qwen3-tts. The plan pins
`--gpu-memory-utilization 0.20` (≈2.46 GB of the 12 GB) plus
`--max-model-len 4096`; the research estimate for a single stream is
~2.5-4 GB, so 0.20 may be tight and 0.25 (≈3.07 GB) is the documented
operator range. Verify with a live pilot before declaring coexistence proven.

## REPORTED facts used (not reproduced here)

From [rumble2flash-review.md](rumble2flash-review.md), each with a primary-source
link in that document:

- Rumble: 0.5B, BF16, Apache-2.0 weights and code, Qwen3-ASR architecture with
  no `trust_remote_code`; English avg WER 6.60 on Open ASR Leaderboard
  (H200 + vLLM, batch throughput — NOT interactive latency).
- Streaming: no documented incremental-input API. `vllm stream=True` emits
  output tokens only after the full audio arrives. Kyutai STT 1B is the
  latency track (native streaming, words ~500 ms after speech).
- Hallucination: "silence stays silent" (Baseten), less phantom text on
  music/background noise than Qwen3-ASR.
- Qwen3-ASR-0.6B: 8.7% WER on AppTek call-centre audio; Kyutai 1B has no
  official WER.

No cross-row comparison is valid: the reported WERs come from different
corpora and hardware than the 12.5% whisper baseline measured above.

## VERIFIED in code: integration seam

- `internal/voicechild/pipeline.go:24-26` — `STTProvider.Stream(ctx, pcm <-chan []byte) (<-chan Transcript, error)`.
- `internal/voicechild/pipeline.go:45-49` — `Transcript{Text string, Final bool}`.
- `internal/voicechild/providers/stt_deepgram.go:154-172` — `NewSTT` vendor switch.
- `internal/voicechild/providers_wiring.go:17-24` — `ProviderConfig` carries Vendor/Model/BaseURL.
- `internal/daemon/voice_keys.go:45-47,167-168,206` — daemon emits
  `stt_vendor`/`stt_model`/`stt_base_url` on the keys frame; child consumes
  them at `internal/voicechild/providers_wiring.go:89-91,177-179`.

The adapter adds one `NewSTT` case and one file; no new wire frame, bus topic,
config field, or enum. Whisper stays the default when `stt_provider` is unset.

## Live pilot: deferred, exact unblock steps

No software was installed and no comparison-model weights were downloaded,
per the user's standing rule. To complete criteria 1-3 and prove criterion 4:

1. On socrates, install vLLM in a dedicated venv (operator approval required).
2. Download `Reson8/Rumble-2-Flash` plus Qwen3-ASR-0.6B and Kyutai 1B weights.
3. Serve Rumble with `--gpu-memory-utilization 0.20 --max-model-len 4096`
   while qwen3-tts stays resident; verify `/health` and VRAM with `nvidia-smi`.
4. Re-run `make test-voice-eval` against the Rumble adapter for each case,
   score with the same `WER` / `KeyHits` / `FindLeaks` functions, and record
   time-to-`heard` from the voice event ring.
5. Compare Rumble against the 12.5% whisper baseline above on the SAME
   fixtures. Only then claim a WER improvement.

## Non-goals recorded

- No claim of time-to-first-word improvement. Rumble is batch-only.
- No change to whisper's default posture or existing config byte-for-byte.
- No model download or install performed in this evaluation.
