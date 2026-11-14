package providers

import (
	"fmt"

	"github.com/caimlas/meept/internal/voicechild"
	"github.com/gojargo/jargo/audio/turn"
)

// smartTurnVAD is the voicechild.TurnAnalyzer implementation wrapping
// jargo's Smart Turn v3 ONNX end-of-turn detector. It is NOT thread-safe by
// itself — the child's pipeline guards every call with its own turnMu (the
// audio pump feeds, the transcript consumer queries).
type smartTurnVAD struct {
	a *turn.SmartTurnV3
}

// NewSmartTurnVAD builds the input-side complete-thought analyzer (jargo
// Smart Turn v3). Requires the ONNX Runtime shared library to be locatable
// (JARGO_ONNXRUNTIME_LIB or platform path); when absent the error surfaces
// here and the caller (the child pipeline) keeps the whisper-punctuation
// endpointing oracle — degradation, never a crash (mirrors jargo's own
// "no runtime → no VAD" posture).
//
// Rate: only 8k/16k are meaningful (16k local-leg); SetSampleRate is called
// by the child at construction.
func NewSmartTurnVAD(sampleRate int) (voicechild.TurnAnalyzer, error) {
	a, err := turn.NewSmartTurnV3()
	if err != nil {
		return nil, fmt.Errorf("turn: construct Smart Turn: %w", err)
	}
	s := &smartTurnVAD{a: a}
	s.SetSampleRate(sampleRate) // silence tracking in AppendAudio needs the rate
	return s, nil
}

// AppendAudio feeds one mono s16le chunk of turn audio with the VAD's
// speaking decision.
func (s *smartTurnVAD) AppendAudio(chunk []byte, isSpeech bool) {
	s.a.AppendAudio(chunk, isSpeech)
}

// SetSampleRate configures the input rate.
func (s *smartTurnVAD) SetSampleRate(sampleRate int) { s.a.SetSampleRate(sampleRate) }

// SetVADStartSecs informs the analyzer of the VAD onset delay.
func (s *smartTurnVAD) SetVADStartSecs(secs float64) { s.a.UpdateVADStartSecs(secs) }

// AnalyzeEndOfTurn runs the model over the buffered turn audio.
func (s *smartTurnVAD) AnalyzeEndOfTurn() (bool, float64, error) {
	state, metrics, err := s.a.AnalyzeEndOfTurn()
	if err != nil {
		return false, 0, err
	}
	// jargo v0.6.x returns a *MetricsData rather than a bare probability;
	// a nil metrics means the model ran on no audio.
	prob := 0.0
	if metrics != nil {
		prob = metrics.Probability
	}
	return state == turn.Complete, prob, nil
}

// Clear resets the analyzer.
func (s *smartTurnVAD) Clear() { s.a.Clear() }

// Close releases the model session. jargo v0.6.x renamed this Cleanup on
// SmartTurnV3 (it releases the session and leaves the analyzer reusable).
func (s *smartTurnVAD) Close() error { return s.a.Cleanup() }
