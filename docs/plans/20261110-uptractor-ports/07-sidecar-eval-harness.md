# Sidecar Eval Harness — Implementation Leaf (07)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks using TDD. The failing gate tests ALREADY EXIST — make
> them pass; do NOT delete, skip, weaken, or rewrite any gate test. Do NOT
> commit. Do NOT use read_file on existing source files — explore with
> search_files or terminal cat. After writing a file, do NOT read it back.

## Meta

- **Parent:** ./master.md
- **Scope:** Persistent voice-sidecar eval lane: STT accuracy + prompt-leak detection, ported from the uptractor sidecar-cloud eval harness, running against meept's voicechild pipeline as a named make target.
- **Dependencies:** 00T only (T10 pins the make target existence)
- **Estimated Context:** 75K
- **Concurrency Group:** A/B (touches magefiles/Makefile + new eval dir only)

## Goal

Port ~/git/uptractor-jobfile (branch fix/coordinator-missed-hangup-watchdog)
phone/core/sidecar-cloud/eval/{accuracy.py,prompt_leak.py,README.md} into a
PERSISTENT meept test lane: `make test-voice-eval`. "Persistent" per owner
instruction: the lane runs committed WAV fixtures through the real STT path
(internal/voicechild providers, whisper) and fails loudly on (a) transcript
accuracy below the pinned threshold and (b) any system-prompt content leaking
into spoken output. The lane joins the existing test suite conventions
(make test-* lanes, -p 2 discipline) and must be deterministic: fixed
fixtures, pinned thresholds, no network, no live models beyond locally cached
whisper weights (skip-with-loud-warning if weights absent, never silent pass).

## Context

- Source eval scripts: cat the eval dir in the uptractor repo; port the CASE SETS and thresholds verbatim; rewrite the driver in Go (no Python in the meept test path — the repo is Go; python helpers allowed only if already conventional, check magefiles/)
- internal/voicechild/providers/stt_whisper.go — the STT path under eval
- internal/voicechild/providers/ — TTS outputs for prompt-leak detection (leaked prompt text in a synthesized reply)
- Makefile / magefiles — existing lane patterns (grep "test-doherty" for the newest lane example)
- Fixtures: commit small WAV files under testdata/voice-eval/ (reuse uptractor phone/core/sidecar/fixtures/demo-utterance.wav as one fixture; generate/collect 3-5 more short utterances covering proper nouns and numbers)

## Verified Anchors

- Makefile — `test-doherty` lane (newest pattern to copy)
- internal/voicechild/providers/stt_whisper.go — provider entry

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
make test-voice-eval        # the lane; exit non-zero on failure
internal/voice/eval/        # Go package: accuracy + prompt-leak evals
testdata/voice-eval/*.wav   # committed fixtures + fixtures/expected.json (case set, thresholds frozen from source eval scripts)
```

Accuracy metric + threshold: port verbatim from eval/accuracy.py (WER/CER
definition and pass bar). Prompt-leak: port the needle list from
prompt_leak.py; any needle substring in output = FAIL.

### What This Leaf Consumes

- Gate test T10 (make target exists; planted-leak fixture fails the lane; clean fixture passes).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---|---|---|---|---|---|---|
| T1 | eval package: fixture loader, accuracy scorer (ported metric), leak detector (ported needles) | Create internal/voice/eval/*.go | `go test ./internal/voice/eval` | gate RED | green | Given planted-leak fixture; When lane runs; Then FAIL naming the needle |
| T2 | make lane + skip-loud behavior when whisper weights absent | Modify Makefile (or magefiles) | `make test-voice-eval` | gate RED | green or loud skip (exit 0 + WARNING printed, never silent) | Given weights present; When make; Then real run |
| T3 | fixtures + expected.json with frozen thresholds | Create testdata/voice-eval/ | `make test-voice-eval` | n/a | green | Given clean fixture; When lane; Then PASS |

## Self-Verification Checklist

- [ ] Thresholds and needles verbatim from source eval scripts (cite line in report)
- [ ] Deterministic: same fixtures => same verdict; no network calls
- [ ] Loud skip path tested (rename weights dir in a scratch run)
- [ ] Lane runtime reasonable (< 3 min on M2 Ultra; record actual)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** 2. **Verify command + output:** last 20 lines 3. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] No silent-pass path (skip is loud; threshold drift flagged)
- [ ] Fixtures are committed and licensed/owned (no third-party audio)
- [ ] Metric port faithful (spot-check scoring against source python on one fixture)
- [ ] No debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

- This lane is the "persistent testing" the owner asked for on the voice
  sidecar: it must stay in CI-able shape so future STT/TTS swaps
  (Kokoro, Deepgram) are regression-checked, not vibes-checked.
