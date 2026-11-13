package voicechild

// TurnAnalyzer is the input-side COMPLETE-THOUGHT detector seam (jargo
// Smart Turn v3, an ONNX end-of-turn model). It is the ML replacement for
// the whisper-punctuation "is this an unfinished thought" oracle: instead
// of guessing from terminal punctuation whether a caller paused mid-
// sentence or finished, this model listens to the shape of the just-voiced
// turn (intonation, trailing words) and answers "was that a complete
// thought?".
//
// It blends with the Silero VAD (the input-speech discriminator): each mic
// chunk is fed to both, with the VAD's speaking state supplied as the turn
// analyzer's isSpeech signal, so the turn analyzer can track silence and
// apply its silence safety-net.
//
// Threading: the analyzer is touched by TWO goroutines — the audio pump
// feeds it (AppendAudio), and the transcript consumer asks for the verdict
// (AnalyzeEndOfTurn) when a final arrives. jargo's smartTurnBase has NO
// internal lock, so the pipeline guards all analyzer calls with a dedicated
// turnMu (never playMu), and the caller holds turnMu across no other lock
// (no lock nesting, mutexio-safe).
type TurnAnalyzer interface {
	// AppendAudio feeds one mono s16le chunk of turn audio along with
	// whether the input-side VAD considers it speech. Call on the audio
	// pump goroutine, under turnMu.
	AppendAudio(chunk []byte, isSpeech bool)
	// SetSampleRate configures the input rate (16k local-leg). Call once
	// at construction, before any AppendAudio.
	SetSampleRate(sampleRate int)
	// SetVADStartSecs informs the analyzer of the VAD onset delay so it
	// can widen its pre-speech buffer. Call once at construction.
	SetVADStartSecs(secs float64)
	// AnalyzeEndOfTurn runs the model over the buffered turn audio and
	// returns whether the caller has finished their thought, plus the
	// model's completion probability in [0,1]. Call when a whisper final
	// arrives (on the transcript consumer goroutine), under turnMu.
	AnalyzeEndOfTurn() (complete bool, probability float64, err error)
	// Clear resets the analyzer to its initial state, discarding the
	// buffered turn. Call at dispatch and teardown, under turnMu.
	Clear()
	// Close releases the model session. Call once at teardown, under
	// turnMu (or after all feeders stopped).
	Close() error
}

// TurnFactory constructs a TurnAnalyzer for one call's sample rate. It is
// the child-side mirror of the VAD/STT/TTS factories (injected by
// cmd/meept-voice so the child never hard-builds jargo deps). A nil factory
// keeps the whisper-punctuation endpointing oracle, byte-identical; a
// factory that errors (ONNX runtime lib absent) degrades the same way.
type TurnFactory func(sampleRate int) (TurnAnalyzer, error)
