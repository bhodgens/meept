package stt

import (
	"context"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewNativeEngine_Linux(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("this test only runs on Linux")
	}

	_, err := NewNativeEngine(Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported on Linux")
}

func TestNewNativeEngine_NonLinux(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "linux" {
		t.Skip("this test does not run on Linux")
	}

	engine, err := NewNativeEngine(Config{})
	require.NoError(t, err)
	require.NotNil(t, engine)
	assert.Equal(t, "native", engine.Name())
}

func TestNativeEngine_Name(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "linux" {
		t.Skip("native engine not supported on Linux")
	}

	engine, err := NewNativeEngine(Config{})
	require.NoError(t, err)
	assert.Equal(t, "native", engine.Name())
}

func TestNativeEngine_IsRecording_InitiallyFalse(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "linux" {
		t.Skip("native engine not supported on Linux")
	}

	engine, err := NewNativeEngine(Config{})
	require.NoError(t, err)
	assert.False(t, engine.IsRecording())
}

func TestNativeEngine_Stop_WhenNotRecording(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "linux" {
		t.Skip("native engine not supported on Linux")
	}

	engine, err := NewNativeEngine(Config{})
	require.NoError(t, err)

	text, err := engine.Stop()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not recording")
	assert.Empty(t, text)
}

func TestNativeEngine_Start_WhenAlreadyRecording(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "linux" {
		t.Skip("native engine not supported on Linux")
	}

	recorderBin := createMockRecorder(t, "mock-ffmpeg-native-start", "sleep")

	engine, err := NewNativeEngine(Config{
		Recording: RecordingConfig{
			RecorderBin: recorderBin,
		},
	})
	require.NoError(t, err)

	ctx := context.Background()

	// First start should succeed.
	err = engine.Start(ctx, func(Result) {})
	require.NoError(t, err)
	assert.True(t, engine.IsRecording())

	// Second start should return "already recording" error.
	err = engine.Start(ctx, func(Result) {})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already recording")

	// Cleanup.
	engine.Stop()
}

func TestCheckNativeAvailable_Linux(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("this test only runs on Linux")
	}

	err := checkNativeAvailable()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported on Linux")
}

func TestCheckNativeAvailable_NonLinux(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "linux" {
		t.Skip("this test does not run on Linux")
	}

	// On macOS/Windows, native is "available" even without backends.
	err := checkNativeAvailable()
	// Should not error (it may log a warning but returns nil).
	assert.NoError(t, err)
}

func TestNativeEngine_ImplementsTranscriber(t *testing.T) {
	t.Parallel()

	// Compile-time interface check.
	var _ Transcriber = (*NativeEngine)(nil)
}

func TestNativeEngine_ConfigPropagation(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "linux" {
		t.Skip("native engine not supported on Linux")
	}

	cfg := Config{
		Engine: "native",
		Recording: RecordingConfig{
			RecorderBin: "ffmpeg",
			SampleRate:  44100,
			Channels:    2,
			Format:      "wav",
		},
	}

	engine, err := NewNativeEngine(cfg)
	require.NoError(t, err)
	require.NotNil(t, engine)
	// Verify the recorder was created with the config.
	assert.NotNil(t, engine.recorder)
}
