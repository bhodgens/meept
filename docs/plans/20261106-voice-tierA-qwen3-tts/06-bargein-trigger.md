# Barge-in via STT partial - Implementation Leaf (06)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> The gate tests ALREADY EXIST and are committed - make them pass; do NOT
> delete, skip, weaken, or rewrite any gate test. Do NOT commit - the
> orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once
> and stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** A caller's speech during playback stops that playback: the
  transcript consumer hooks noteBargeIn and emits the interrupt frame
  once per barge-in; the daemon's existing handleInterrupt path does the
  rest (turn cancel, clear, per-call isolation).
- **Dependencies:** 05 (transcript consumer exists)
- **Estimated Context:** 35K
- **Concurrency Group:** C (after 05 REVIEWED)

## Goal

Complete the interruption contract (user requirement R3): the model
pauses when the person starts responding, waits for them to finish, and
the finished utterance starts the next turn conditioned on what they
said. Everything downstream of the interrupt frame already works
(talkloop.go handleInterrupt:553 -> gen bump + turn cancel; child clear()
playGen retirement; splice bargeIn per call; fresh prompt spawns a new
turn). This leaf only connects the trigger.

## Context

- Detection choice (D1): Deepgram interim/partial transcripts are the
  speech detector - no separate VAD, no ONNX dependency for Tier A.
  Latency: Deepgram interims arrive in ~100-300 ms of speech onset.
  The existing noteBargeIn latch (pipeline.go:256-267) enforces
  exactly-once per barge-in and playbackRestarted (269-276) re-arms on
  the next say - both already pinned by tests (pipeline_resume_test.go).
- Daemon contract already handles the interrupt frame:
  internal/voice/talkloop.go:381-382 routes EventInterrupt ->
  handleInterrupt -> cancelPipeline (gen bump, turn ctx cancel) +
  Binding.PublishInterrupt + clear frame. Nothing daemon-side changes.
- Child clear() (pipeline.go:248-254) drops queued audio and retires
  the playing stream via playGen.
- The interrupt frame goes out BEFORE the prompt frame for the same
  utterance (C5 order) so playback stops before the new turn's say can
  start.

## Verified Anchors

- internal/voicechild/pipeline.go:256-276 - noteBargeIn +
  playbackRestarted latch (call sites needed)
- internal/voicechild/keys.go:76-82 - emitInterrupt (no production caller today)
- internal/voice/talkloop.go:381-382, 553-570 - daemon interrupt path (READ ONLY - do not modify)
- internal/voice/talkloop.go:363-380 - prompt path: partial = preview only; final = new turn
- internal/voicechild/pipeline.go:186-192 - playback sets speaking + resets latch

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// Transcript consumer partial branch becomes (C5, full form):
//   partial -> if p.noteBargeIn() { emitInterrupt(ctx, ccid) }
//              emitPrompt(ctx, ccid, tr.Text, false)
// Order pinned: noteBargeIn FIRST, emitInterrupt before emitPrompt.
// final -> emitPrompt(ctx, ccid, tr.Text, true)  (unchanged from leaf 05)
// No changes to daemon code. No changes to enqueueSay/playback/clear
// beyond reading p.speaking state through the existing latch.
```

### What This Leaf Consumes

- C5 trigger contract (master).
- Leaf 05's consumer loop and startTranscripts once-guard.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Hook the latch into the partial branch per C5 order | Modify: internal/voicechild/pipeline.go (transcript consumer) | `go test ./internal/voicechild/ -run TestBargeInViaPartial` | gate T5 RED (no caller) | gate T5 passes: speaking + partial -> exactly one interrupt frame; latch blocks the second partial | see contract |
| T2 | No interrupt when idle: partials while NOT speaking never emit interrupt | covered in gate T5 + leaf test if needed | `go test ./internal/voicechild/ -run TestBargeInViaPartial` | n/a | idle-partial assertions green | Given pipeline not speaking; When partial; Then no interrupt frame, prompt still emitted |
| T3 | Re-arm: after clear retires the stream and a new say restarts playback, the next partial emits interrupt again | covered by gate T5 + pipeline_resume_test.go (existing) | `go test ./internal/voicechild/ -run TestBargeInViaPartial` | n/a | full re-arm scenario green | Given interrupt at t0, new say at t1, partial at t2; Then second interrupt emitted |
| T4 | Order pin: in any emitted frame sequence for one partial, interrupt precedes prompt | leaf test (recording sender order assert) | `go test ./internal/voicechild/ -run TestBargeInFrameOrder` (new leaf test) | n/a | order assert green | Given speaking + partial; When frames recorded; Then interrupt index < prompt index |
| T5 | Package suite green end-to-end incl. concurrency tests | - | `go test -p 2 ./internal/voicechild/ ./internal/voice/` | n/a | green | - |

## Self-Verification Checklist

- [ ] Gate test F5 passes; no gate test modified
- [ ] Exactly-once per barge-in; re-arm on playback restart
- [ ] Interrupt-before-prompt order pinned by a test
- [ ] No daemon file touched (`git diff --stat` confirms)
- [ ] gofmt + vet clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** pipeline.go (+ new leaf test file if authored)
2. **Verify command + output:** gate T5 + package suites, last lines
3. **Deviations:** any (e.g. latch call placement)

## Review Checklist (For Review Agent)

- [ ] C5 order enforced: noteBargeIn -> emitInterrupt -> emitPrompt
- [ ] talkloop.go and splice.go byte-identical to pre-leaf state
- [ ] Latch semantics: per-barge-in (not per-call) - matches the F-1 fix generation
- [ ] No busy-work: no polling loops, no timers added

Output: APPROVED or specific gaps with file + line references.

## Notes

- Do NOT attempt echo cancellation: the caller's phone mutes the model's
  playout far enough (Telnyx half-duplex behavior varies); Deepgram may
  pick up model audio on some carriers - the supervisor gate + one
  interrupt per barge-in bound the damage. Note real-world tuning as a
  follow-up in the completion report if tests suggest it.
- Do NOT add a config flag for barge-in in this leaf (product decision,
  later); the behavior is on whenever STT delivers partials.
