package voicechild

// This file carried the leaf-07 jargo skeleton chain (buildJargoWorker,
// MeeptCallInProcessor, StubSink). Leaf 08 replaced the wire-up path:
// providers are constructed via the STTFactory/TTSFactory seams
// (providers_wiring.go) instead of slotting jargo service processors into a
// skeleton chain, so the skeleton symbols became dead code and were removed
// per the repo unused-code rule. The jargo API-drift notes that lived here
// are preserved below; the jargo dependency itself is pinned for the
// audio/resample + audio/vad + audio/turn + wsutil packages that
// internal/voicechild/providers uses.
//
// jargo v0.6.1 API drift vs v0.1.0 (upgraded 2026-11-13, verified against
// the pinned module):
//   - turn.SmartTurnV3.AnalyzeEndOfTurn returns (EndOfTurnState,
//     *MetricsData, error), not a bare probability float64. The confidence
//     is MetricsData.Probability; a nil *MetricsData means the model ran on
//     no audio. Adapted in providers/turn_smart.go.
//   - SmartTurnV3 has no Close; the session-release method is Cleanup.
//     Silero KEPT Close (it gained shared-session ownership: Close is a
//     no-op when the session came from a SileroModel).
//   - resample: Quality is deprecated (one polyphase windowed-sinc filter
//     for every conversion, ~90 dB rejection), so a per-quality tail table
//     is meaningless. ClearAfter's zero value INVERTED: v0.1.0 read zero as
//     the 200 ms DefaultClearAfter, v0.6.x reads zero as never-clear;
//     resample.NeverClear now names the intent any negative also expresses.
//   - resample.Process keeps output IN PACE with input (the filter delays
//     the audio a few ms instead of holding frames back), so a completed
//     stream yields the full frame count with nothing clipped. Therefore
//     do NOT add a Flush at end of stream on top of a complete stream -
//     v0.6.x flush() pads delay frames of zeros THROUGH process(), emitting
//     58 frames of ringing artifact at 24k->16k (measured: 48058 vs 48000).
//   - resample's doc claims Process carries "any fraction of a frame into
//     the next call". IT DOES NOT: sincStream has no residue field and
//     process() computes inFrames = len(pcm)/(2*channels), truncating an
//     odd trailing byte. Verified empirically: all-odd 639-byte reads
//     straight to the library give 17829 out-of-slew frames (worst 15.3x
//     the natural delta). providers/resample.go must keep its own
//     sub-sample carry.
//   - jargo's ONNX binding moved from github.com/yalue/onnxruntime_go to
//     github.com/microsoft/onnxruntime/go behind internal/onnxrt, with a
//     cgo build path. The runtime-lib env seam (JARGO_ONNXRUNTIME_LIB, fed
//     from VOICE_ONNX_LIB by cmd/meept-voice) is unchanged.
//   - go-resample is no longer a dependency (jargo ships its own sinc);
//     go mod tidy drops it.
//
// jargo v0.1.0 API drift vs the leaf-07 brief (all verified against the
// then-pinned module):
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
