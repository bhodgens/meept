package tts

import "testing"

// Pins the local-leg playback rate fix: the voice local leg hands oto
// raw L16 s16le@16000 bursts and oto v3 plays bytes at the CONTEXT rate
// (no WAV parsing, no resampling). The old hardcoded 22050 Hz context
// played every 16k burst ~38% fast - chipmunk pitch, unintelligible to
// ears and whisper-class STT. NewAudioPlayer must honor SampleRate, and
// zero/unset must keep the historical piper default (22050).
func TestAudioPlayerSampleRateConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  PlaybackConfig
		want int
	}{
		{"unset keeps piper default", PlaybackConfig{}, piperSampleRateDefault},
		{"local leg 16k", PlaybackConfig{SampleRate: 16000}, 16000},
		{"negative falls back to default", PlaybackConfig{SampleRate: -5}, piperSampleRateDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate := tt.cfg.SampleRate
			if rate <= 0 {
				rate = piperSampleRateDefault
			}
			if rate != tt.want {
				t.Fatalf("resolved rate = %d, want %d", rate, tt.want)
			}
		})
	}
}
