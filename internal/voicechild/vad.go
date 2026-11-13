package voicechild

// VADAnalyzer is the input-side speech-vs-noise detector seam. It reports
// whether recent mic audio is human SPEECH (Silero: an ONNX voice-activity
// model run over 512-sample frames at 16 kHz) versus plain loud noise or
// self-echo. The RMS barge-in gate keys off this so a loud-but-not-speech
// window (background noise, a door slam, the agent's own TTS bleed) never
// trips the interrupt.
//
// The interface is deliberately tiny: AnalyzeAudio returns speaking (a
// smoothed quiet→starting→speaking→stopping state collapsed to "is the
// speaker on"), owner = the pipeline, and no lock is held across the call
// (the underlying model run is ~sub-ms but must not block the socket
// drain under playMu — see rmsBargeInCheck's call site).
type VADAnalyzer interface {
	// AnalyzeAudio feeds a mono s16le chunk and returns whether the
	// smoothed state machine considers the caller to be speaking.
	// Chunks need not align to the model frame (512 samples @16k);
	// the analyzer buffers internally. Must be safe to call on the
	// audio pump goroutine.
	AnalyzeAudio(chunk []byte) bool
	// Close releases the model session/resource.
	Close() error
}

// VADFactory constructs a VADAnalyzer for one call's sample rate. It is the
// child-side mirror of the STT/TTS provider factories (injected by
// cmd/meept-voice so the child never hard-builds jargo deps). A factory that
// returns an ANALYZER allows the pipeline to gate barge-in on real speech; a
// nil factory keeps the pipeline on the legacy RMS + STT-evidence gate,
// byte-identical (the ONNX lib absent case degrades gracefully, per jargo's
// own posture).
type VADFactory func(sampleRate int) (VADAnalyzer, error)
