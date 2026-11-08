# STT pump + transcript wiring - Implementation Leaf (05)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> The gate tests ALREADY EXIST and are committed - make them pass; do NOT
> delete, skip, weaken, or rewrite any gate test. Do NOT commit - the
> orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once
> and stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** pumpAudioIn feeds the real STT adapter; transcripts become
  C1 prompt frames (final = last:true starts a turn; partial =
  last:false preview). Barge-in triggering is leaf 06 - leave a clean
  seam but do not implement it here.
- **Dependencies:** 02 (real providers construct from keys frames)
- **Estimated Context:** 45K
- **Concurrency Group:** C

## Goal

Close the ears half of the wiring gap: today pumpAudioIn reads
daemon->child PCM and drops it (pipeline.go:122-144, stub comment), so
the model never hears the caller. After this leaf, caller audio flows
through p.stt.Stream and every final transcript reaches the daemon as a
prompt frame that starts an employee turn.

## Context

- internal/voicechild/pipeline.go - pipeline struct (stt/tts fields,
  playMu-guarded), newPipeline (dials C2 pair), start (launches
  pumpAudioIn), pumpAudioIn (drains socket, drops chunks),
  enqueueSay/playback/clear (playGen machinery - DO NOT TOUCH beyond
  reading), close (cancel + socket close).
- internal/voicechild/providers/stt_deepgram.go - DeepgramSTT.Stream
  opens a websocket session and returns a Transcript channel. One
  in-call retry is inside the adapter.
- Child.emitPrompt (keys.go:63-74) already sends prompt frames with the
  last flag. Production callers: none yet - this leaf adds the first.
- Concurrency: Stream(ctx, pcm chan) - feed chunks, read transcripts.
  Stream must be called ONCE per pipeline (after providers construct in
  handleKeys; before that, keep draining the socket - a call's first
  seconds of audio before keys arrive are silence in practice; drain,
  do not block).
- Chunk sizes: codec-derived via voiceframe.ChunkBytes (existing code).
- Playback state (p.speaking) lives under playMu; the transcript
  consumer must not hold playMu across channel sends (mutexio).

## Verified Anchors

- internal/voicechild/pipeline.go:43-44 - stt/tts fields (now real
  providers post-leaf-02)
- internal/voicechild/pipeline.go:113-144 - start + pumpAudioIn
- internal/voicechild/pipeline.go:280-285 - close
- internal/voicechild/providers_wiring.go:82-98 - handleKeys constructs
  providers or fails the call
- internal/voicechild/keys.go:63-74 - emitPrompt
- internal/voicechild/providers/stt_deepgram.go:146-160 - Stream contract

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// pipeline gains (unexported):
pcmCh chan []byte        // fed by pumpAudioIn, consumed by Stream
// start(ctx) behavior after this leaf:
//   1. pumpAudioIn reads chunks -> forward buf[:n] into pcmCh
//      (non-blocking send with ctx.Done fallback: never block the
//      socket drain)
//   2. IF p.stt is non-nil at start time (keys arrived before setup's
//      start - the normal daemon order per C4), spawn the transcript
//      goroutine immediately; ELSE remember to spawn it when providers
//      construct (handleKeys hook point - add c.startTranscripts(p)
//      call in handleKeys after constructProviders succeeds)
// transcript consumer goroutine (C5):
//   for tr := range transcripts:
//       drop tr.Text == "" (Deepgram silence results)
//       final -> emitPrompt(ctx, ccid, tr.Text, true)
//       partial -> emitPrompt(ctx, ccid, tr.Text, false)
//                  (leaf 06 will hook the barge-in check here)
//   teardown: p.ctx cancel closes Stream session + goroutine
```

### What This Leaf Consumes

- C5 transcript/barge-in trigger contract (master) - implements the
  prompt half; the noteBargeIn hook point is leaf 06.
- voicechild.STTProvider (unchanged interface).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | pumpAudioIn forwards chunks to pcmCh (non-blocking, ctx-aware); socket always drains | Modify: internal/voicechild/pipeline.go | `go test ./internal/voicechild/ -run TestPipelineSTTWiring` | gate T4 RED (chunks dropped) | gate T4 chunk-forward assertions pass | Given pipeline + fake STT; When 3 chunks written to .in; Then fake STT records all 3 byte slices |
| T2 | Transcript consumer: final/partial -> emitPrompt with last flag; empty text dropped; runs after providers construct | Modify: pipeline.go (consumer + startTranscripts), providers_wiring.go (hook call) | gate T4 prompt assertions | emitPrompt has no production caller | gate T4 passes: prompt frames appear with correct last flags; empty transcripts produce nothing | see contract block |
| T3 | Ordering: keys-before-setup vs setup-before-keys both work (transcripts start exactly once) | Modify: pipeline.go (once-guard on startTranscripts) | `go test ./internal/voicechild/ -run TestTranscriptStartOnce` (new leaf test) | n/a (new behavior) | exactly one Stream call + one consumer per pipeline in both orders | Given keys at t0/start at t1 AND reversed; When both complete; Then single consumer running |
| T4 | Teardown: close() cancels ctx; Stream session ends; consumer exits; no goroutine leak (concurrency_test style) | Modify: pipeline.go close if needed | `go test ./internal/voicechild/ -run TestTranscriptTeardown` (new leaf test) | n/a | no leaked goroutine after close (assert via runtime.NumGoroutine delta or channel close observation) | Given live pipeline with fake STT; When end frame; Then consumer + Stream exit |
| T5 | Existing suite stays green (playback/clear/barge-in latch machinery untouched) | - | `go test ./internal/voicechild/` | n/a | full package green | Given changes; When suite runs; Then green |

## Self-Verification Checklist

- [ ] Gate test F4 passes; no gate test modified
- [ ] No mutex held across channel sends/reads (mutexio clean)
- [ ] Socket drain never blocks on a slow STT
- [ ] Exactly-once Stream + consumer semantics
- [ ] gofmt + vet clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** pipeline.go, providers_wiring.go (+ any new test file)
2. **Verify command + output:** gate T4 + package suite, last lines
3. **Deviations:** any ordering/hook-point adjustment with rationale

## Review Checklist (For Review Agent)

- [ ] Contract C5 prompt half implemented exactly (last flags, empty-drop)
- [ ] noteBargeIn NOT called here (leaf 06's seam - verify no early coupling)
- [ ] Drain path can never wedge the C2 .in socket
- [ ] playGen/clear/interrupt machinery untouched (git diff review)
- [ ] Stub comments removed where now false (pumpAudioIn comment)

Output: APPROVED or specific gaps with file + line references.

## Notes

- The stubSTT test type (pipeline.go:287-302) keeps working for tests
  that inject transcripts via emitPrompt - leave it.
- Buffer pcmCh with a small bound (e.g. 8 chunks = 160 ms); on overflow,
  drop the OLDEST chunk (audio latency beats completeness for a phone
  call) - document in a comment.
- Do NOT wire noteBargeIn into the partial path (leaf 06).
