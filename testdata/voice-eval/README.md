# Voice eval fixtures (leaf 07, plan 20261110-uptractor-ports)

Driven by `make test-voice-eval` -> `go test ./internal/voice/eval/`
(TestLaneEndToEnd via the lane runner). The Go eval package lives at
`internal/voice/eval/` (distinct from the top-level `internal/eval`).

## Contents

- `expected.json` — the frozen case set: per-fixture expected transcript,
  pass threshold (`max_wer`), and optional key-terms floor
  (`min_key_hits`). Thresholds and the accuracy metric are ported
  VERBATIM from uptractor
  `phone/core/sidecar-cloud/eval/accuracy.py` (branch
  fix/coordinator-missed-hangup-watchdog): WER = word Levenshtein over
  the normalized text (lowercase, `-`→space, strip non-[a-z0-9 ]),
  divided by max(1, len(ref)); key terms = accuracy.py `KEY`; the
  vocabulary prompt = accuracy.py `PROMPT`. The prompt-leak needle list
  is ported from `eval/prompt_leak.py` (any needle substring in output
  = FAIL) — see `LeakNeedles` in `internal/voice/eval/eval.go`.
- `leak-silence.wav`, `leak-hiss.wav`, `leak-music.wav` — the
  prompt_leak.py non-speech case shapes (digital silence, line hiss,
  synthetic hold music; seeded RNG, shapes ported from
  `prompt_leak.py`'s CASES). Regenerable with `gen_leak_fixtures.py`
  (one-shot; outputs are committed). The lane requires their transcripts
  to contain NO prompt-vocabulary needle; `leak-silence.wav` additionally
  expects an EMPTY transcript (the production RMS gate suppresses true
  silence before whisper). Generic annotations like "(blender whirring)"
  are not needles — prompt_leak.py's bar is prompt vocabulary.
- `demo-utterance.wav` — committed operator WAV ported from uptractor
  `phone/core/sidecar/fixtures/demo-utterance.wav` (PCM16 mono 24 kHz,
  6.5 s; operator-provisioned there, committed here).
- `work-order-02.wav` … `log-drywall-08.wav` — the remaining seven lines
  of accuracy.py's `LINES` list, synthesized with the repo's piper voice
  (en_US-data_7024-medium, `~/git/piper-voice-models/Data`) using the
  exact `LINES` text as the expected transcript. The source harness uses
  the same synthesis route (Piper -> phone-line squeeze -> whisper), so
  provenance matches the ported thresholds' recording conditions.
  (demo-utterance.wav is the eighth source line's slot: it is the
  operator-recorded equivalent committed upstream.)

## Lane behavior

- Weights present (`~/.meept/models/ggml-base.en.bin`, override with
  `MEEPT_VOICE_EVAL_MODEL_DIR` for scratch runs): each fixture runs
  through the REAL local STT path (`internal/voicechild/providers`
  vendor "whisper" -> whisper.cpp CLI, no network) and is scored against
  its expected transcript + threshold.
- Weights ABSENT: the lane SKIPS LOUDLY — a WARNING naming the missing
  weights path, exit 0. Never a silent pass; the unit suite pins this.
- Deterministic: fixed fixtures, pinned thresholds, no network.
