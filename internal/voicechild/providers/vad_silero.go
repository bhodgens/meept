package providers

import (
	"fmt"

	"github.com/caimlas/meept/internal/voicechild"
	"github.com/gojargo/jargo/audio/vad"
)

// sileroVAD is the voicechild.VADAnalyzer implementation wrapping jargo's
// Silero ONNX voice-activity detector. It feeds each mic chunk to the model's
// state machine and collapses the smoothed state to a single "is the caller
// speaking" boolean for the RMS barge-in gate.
type sileroVAD struct {
	a *vad.Silero
}

// NewSileroVAD builds the input-side speech-vs-noise analyzer (jargo Silero
// VAD). It requires the ONNX Runtime shared library to be locatable (via
// JARGO_ONNXRUNTIME_LIB or the platform search path); when the runtime is
// absent the error surfaces here, and the caller (the child pipeline) keeps
// the legacy RMS + STT-evidence gate — degradation, never a crash (mirrors
// jargo's own "no runtime → no VAD" posture).
//
// Rate: only 8k and 16k are supported; 16k is the local-leg C2 rate.
func NewSileroVAD(sampleRate int) (voicechild.VADAnalyzer, error) {
	a, err := vad.NewSilero()
	if err != nil {
		return nil, fmt.Errorf("vad: construct Silero: %w", err)
	}
	if err := a.SetSampleRate(sampleRate); err != nil {
		return nil, fmt.Errorf("vad: set sample rate %d: %w", sampleRate, err)
	}
	return &sileroVAD{a: a}, nil
}

// AnalyzeAudio feeds one mono s16le chunk to the detector and reports whether
// the caller is speaking. Confirmed speech (the model's smoothed Speaking
// state) is true; quiet/starting/stopping are not yet "confirmed speech" and
// keep the gate from tripping on unconfirmed loud input.
func (s *sileroVAD) AnalyzeAudio(chunk []byte) bool {
	state := s.a.AnalyzeAudio(chunk)
	return state == vad.StateSpeaking
}

// Close releases the model session.
func (s *sileroVAD) Close() error {
	return s.a.Close()
}
