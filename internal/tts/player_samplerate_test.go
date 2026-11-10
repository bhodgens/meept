package tts

import "testing"

// Pins the local-leg playback rate fix: oto v3 plays raw bytes at the
// CONTEXT rate (no WAV parsing, no resampling). The voice local leg's
// pair sockets carry L16 s16le@16000; the old hardcoded 22050 Hz oto
// context played every burst ~38% fast - chipmunk pitch, unintelligible
// to ears and whisper-class STT. resolveSampleRate must honor
// PlaybackConfig.SampleRate and keep the historical piper default when
// unset (the piper path relies on 22050).
func TestResolveSampleRate(t *testing.T) {
	tests := []struct {
		name string
		cfg  PlaybackConfig
		want int
	}{
		{"unset keeps piper default", PlaybackConfig{}, piperSampleRateDefault},
		{"local leg 16k honored", PlaybackConfig{SampleRate: 16000}, 16000},
		{"negative falls back to default", PlaybackConfig{SampleRate: -5}, piperSampleRateDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveSampleRate(tt.cfg); got != tt.want {
				t.Fatalf("resolveSampleRate(%+v) = %d, want %d", tt.cfg, got, tt.want)
			}
		})
	}
}
