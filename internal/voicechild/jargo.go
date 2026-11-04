package voicechild

// This file carried the leaf-07 jargo skeleton chain (buildJargoWorker,
// MeeptCallInProcessor, StubSink). Leaf 08 replaced the wire-up path:
// providers are constructed via the STTFactory/TTSFactory seams
// (providers_wiring.go) instead of slotting jargo service processors into a
// skeleton chain, so the skeleton symbols became dead code and were removed
// per the repo unused-code rule. The jargo API-drift notes that lived here
// are preserved below; the jargo dependency itself remains pinned for the
// audio/resample + wsutil internals that internal/voicechild/providers uses.
//
// jargo v0.1.0 API drift vs the leaf-07 brief (all verified against the
// pinned module):
//   - the leaf's stub interfaces (STTProvider/TTSProvider) do NOT exist in
//     jargo; jargo composes service processors (stt.SegmentService with a
//     Transcriber; tts.Base with a Synthesizer). Our interfaces are the
//     seams; internal/voicechild/providers bridges them.
//   - Worker.Run(ctx) blocks until EndFrame/CancelFrame.
//   - jargo/pipeline collides with our pipeline type: import alias jargo.
//   - TranscriptionFrame constructor: NewTranscriptionFrame(text, userID,
//     timestamp string).
//   - processors embed *processor.Base constructed with processor.New.
//   - StartFrame is queued by NewWorker's Run, not pushed manually.
