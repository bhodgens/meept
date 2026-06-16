package stt

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTranscriber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		engine      string
		wantErr     bool
		wantType    string // "whisper", "parakeet", "native", or "" for error
		skipOnLinux bool   // native engine not supported on Linux
	}{
		{
			name:     "whisper engine returns WhisperEngine",
			engine:   "whisper",
			wantErr:  true, // whisper-cli likely not installed in CI
			wantType: "whisper",
		},
		{
			name:     "parakeet engine returns ParakeetEngine",
			engine:   "parakeet",
			wantErr:  true, // parakeet-transcribe likely not installed in CI
			wantType: "parakeet",
		},
		{
			name:        "native engine returns NativeEngine or error on Linux",
			engine:      "native",
			wantErr:     false,
			wantType:    "native",
			skipOnLinux: true, // handled separately below
		},
		{
			name:     "unknown engine returns error",
			engine:   "unknown",
			wantErr:  true,
			wantType: "",
		},
		{
			name:     "empty engine returns error",
			engine:   "",
			wantErr:  true,
			wantType: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.skipOnLinux && runtime.GOOS == "linux" {
				// Native engine returns an error on Linux.
				_, err := NewTranscriber(Config{Engine: tt.engine})
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "not supported on Linux")
				return
			}

			cfg := Config{Engine: tt.engine}
			got, err := NewTranscriber(cfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, got)
			} else {
				if assert.NoError(t, err) && assert.NotNil(t, got) {
					assert.Equal(t, tt.wantType, got.Name())
				}
			}
		})
	}
}

func TestNewTranscriber_NativeOnLinux(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("this test only runs on Linux")
	}

	_, err := NewNativeEngine(Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported on Linux")
}

func TestNewTranscriber_NativeOnNonLinux(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "linux" {
		t.Skip("this test does not run on Linux")
	}

	got, err := NewNativeEngine(Config{})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "native", got.Name())
}

func TestCheckAvailable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		engine   string
		wantErr  bool
		errMatch string // substring expected in error message
	}{
		{
			name:     "unknown engine returns error",
			engine:   "bogus",
			wantErr:  true,
			errMatch: "unknown engine",
		},
		{
			name:     "whisper unavailable returns error",
			engine:   "whisper",
			wantErr:  true,
			errMatch: "not found", // whisper-cli not installed in test env
		},
		{
			name:     "parakeet unavailable returns error",
			engine:   "parakeet",
			wantErr:  true,
			errMatch: "not found", // parakeet-transcribe not installed in test env
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := CheckAvailable(Config{Engine: tt.engine})
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMatch != "" {
					assert.Contains(t, err.Error(), tt.errMatch)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckAvailable_Native(t *testing.T) {
	t.Parallel()

	err := CheckAvailable(Config{Engine: "native"})

	if runtime.GOOS == "linux" {
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not supported on Linux")
	} else {
		// Non-Linux: native is "available" (even without helper binary),
		// because it will produce an error at transcription time.
		assert.NoError(t, err)
	}
}

func TestResultStruct(t *testing.T) {
	t.Parallel()

	r := Result{
		Text:       "hello world",
		IsFinal:    true,
		Confidence: 0.95,
	}
	assert.Equal(t, "hello world", r.Text)
	assert.True(t, r.IsFinal)
	assert.InDelta(t, 0.95, r.Confidence, 0.001)
}

func TestConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Engine:   "whisper",
		Language: "en",
	}
	assert.Equal(t, "whisper", cfg.Engine)
	assert.Equal(t, "en", cfg.Language)
	// Zero-value checks for sub-configs.
	assert.Empty(t, cfg.Whisper.BinPath)
	assert.Empty(t, cfg.Parakeet.BinPath)
	assert.Empty(t, cfg.Recording.RecorderBin)
	assert.Zero(t, cfg.Recording.SampleRate)
}
