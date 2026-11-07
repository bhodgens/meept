# Local-leg RMS barge-in - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. Do
> NOT commit — the orchestrator handles git. Explore with search_files or
> terminal cat, never read_file. Do NOT read back files you just wrote.
> RECONCILE FIRST: the tierA tree (docs/plans/20261106-voice-tierA-qwen3-tts/)
> owns pumpAudioIn->STT->prompt frames and the STT-partial barge-in for
> phone legs (its leaves 05/06). Those MUST be committed before you start.
> If they are not, STOP and report - do not duplicate them here.

## Meta

- **Parent:** master.md
- **Scope:** The LOCAL-leg barge-in trigger: playback-side RMS VAD, gated
  to Local==true, plus the trigger-matrix test pins. (master CL3 matrix.)
- **Dependencies:** tierA 05/06 (committed), leaf 01 (Frame.Local,
  Frame.BargeInRms), leaf 03 (pipeline stores local/bargeInRms)
- **Estimated Context:** 45K
- **Concurrency Group:** B
- **Files this leaf owns:** internal/voicechild/pipeline.go (RMS helper +
  trigger call only), internal/voicechild/bargein_rms_test.go (new)

## Goal

On the local leg the STT is whisper (segmented, finals only), so tierA's
partial-based latch can never fire mid-sentence there. This leaf gives the
local leg its own interrupt source: while the child is speaking, measure
the outgoing audio's energy; sustained loudness over the configured RMS
threshold trips the existing one-interrupt-per-barge-in latch. Phone legs
keep tierA's trigger; RMS never runs there.

## Context

Key files:
- internal/voicechild/pipeline.go - playback() reads chunks under playMu;
  noteBargeIn() already rate-limits (F3 latch); post-tierA-05 the pipeline
  has pcmCh/Stream machinery - do not disturb it.
- leaf 03 lands pipeline fields `local bool` + `bargeInRms int` (from the
  open frame). This leaf reads them.
- RMS on s16le mono: uint64 sum of squares over 320 int16 samples per 640
  B chunk; integer-sqrt-free threshold compare (compare sum against
  threshold^2 * sampleCount) - coarse VAD, no floats, no new deps.

## Interface Contracts (From Parent)

CL3 matrix (verbatim): phone leg = tierA STT-partial trigger; local leg =
RMS VAD. While p.speaking && local: each outgoing 640 B chunk's RMS vs
bargeInRms; 10 consecutive over-threshold chunks (200 ms) -> noteBargeIn()
-> emitInterrupt once (latch owns dedup); window resets via
playbackRestarted()/clear. bargeInRms <= 0 disables the VAD (named no-op).
RMS path MUST be unreachable when local == false.

## Tasks

| Task ID | Objective | Files | Verify | RED reason | PASS |
|---|---|---|---|---|---|
| T1 | Unit test the RMS helper (known waveforms -> threshold compare) | bargein_rms_test.go | go test -p 2 ./internal/voicechild/ -run TestRMSChunk | no helper | PASS |
| T2 | Test: local pipeline speaking + injected loud PCM through playback() -> exactly one interrupt frame on the text socket; quiet audio -> none; second barge-in after restart -> second interrupt | same | -run TestRMSBargeIn | no trigger call | PASS |
| T3 | Test: local==false never evaluates RMS (assert via a spy or by frame silence under loud input) | same | -run TestRMSDisabledOnPhoneLeg | - | PASS |
| T4 | Implement helper + trigger in playback() per CL3 matrix | pipeline.go | T1-T3 | - | PASS |
| T5 | gofmt/vet/race + full voicechild suite | - | go test -p 2 -race ./internal/voicechild/... | - | clean |

## Interface Contract (What This Leaf Exposes)

Nothing exported. Behavior: local calls interrupt within ~200 ms of
sustained caller speech; phone calls byte-identical to tierA.

## Self-Verification Checklist

- [ ] tierA tests (stt wiring, partial barge-in) still green
- [ ] No new module dependencies; integer math only
- [ ] playMu never held across channel sends (mutexio green)

## Review Checklist

- [ ] RMS unreachable on phone legs (read the guard path, not just tests)
- [ ] Latch reuse: no second dedup mechanism invented
- [ ] ASCII only; no artifacts
