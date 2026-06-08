package stt

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createMockWhisperCLI creates a fake whisper-cli binary that echoes a fixed
// transcription to stdout for testing purposes.
func createMockWhisperCLI(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "whisper-cli")

	// The mock script echoes a fixed line to stdout and exits 0.
	// It accepts any arguments (to mimic whisper-cli interface).
	script := `#!/bin/sh
# Mock whisper-cli: echoes transcription text to stdout.
echo "hello world this is a test transcription"
exit 0
`
	err := os.WriteFile(binPath, []byte(script), 0755)
	require.NoError(t, err)
	return binPath
}

// createMockWhisperCLIFailing creates a fake whisper-cli that exits with error.
func createMockWhisperCLIFailing(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "whisper-cli")

	script := `#!/bin/sh
echo "error: model failed to load" >&2
exit 1
`
	err := os.WriteFile(binPath, []byte(script), 0755)
	require.NoError(t, err)
	return binPath
}

func TestNewWhisperEngine_Success(t *testing.T) {
	binPath := createMockWhisperCLI(t)

	engine, err := NewWhisperEngine(Config{
		Engine: "whisper",
		Whisper: WhisperConfig{
			BinPath: binPath,
		},
		Recording: RecordingConfig{
			RecorderBin: "nonexistent-recorder",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, engine)
	assert.Equal(t, "whisper", engine.Name())
	assert.False(t, engine.IsRecording())
}

func TestNewWhisperEngine_BinaryNotFound(t *testing.T) {
	t.Parallel()

	// Use a binary that definitely doesn't exist.
	_, err := NewWhisperEngine(Config{
		Engine: "whisper",
		Whisper: WhisperConfig{
			BinPath: "/nonexistent/whisper-cli-test-binary",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "whisper-cli not found")
}

func TestNewWhisperEngine_DefaultBinPath(t *testing.T) {
	t.Parallel()

	// Default bin path is "whisper-cli" which likely doesn't exist.
	_, err := NewWhisperEngine(Config{Engine: "whisper"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "whisper-cli not found")
}

func TestWhisperEngine_Name(t *testing.T) {
	t.Parallel()

	binPath := createMockWhisperCLI(t)

	engine, err := NewWhisperEngine(Config{
		Whisper: WhisperConfig{BinPath: binPath},
	})
	require.NoError(t, err)
	assert.Equal(t, "whisper", engine.Name())
}

func TestWhisperEngine_IsRecording_InitiallyFalse(t *testing.T) {
	t.Parallel()

	binPath := createMockWhisperCLI(t)

	engine, err := NewWhisperEngine(Config{
		Whisper: WhisperConfig{BinPath: binPath},
	})
	require.NoError(t, err)
	assert.False(t, engine.IsRecording())
}

func TestWhisperEngine_Stop_WhenNotRecording(t *testing.T) {
	t.Parallel()

	binPath := createMockWhisperCLI(t)

	engine, err := NewWhisperEngine(Config{
		Whisper: WhisperConfig{BinPath: binPath},
	})
	require.NoError(t, err)

	text, err := engine.Stop()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not recording")
	assert.Empty(t, text)
}

func TestWhisperEngine_Start_WhenAlreadyRecording(t *testing.T) {
	t.Parallel()

	whisperBin := createMockWhisperCLI(t)
	recorderBin := createMockRecorder(t, "mock-ffmpeg-whisper-start", "sleep")

	engine, err := NewWhisperEngine(Config{
		Whisper: WhisperConfig{BinPath: whisperBin},
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

func TestWhisperEngine_TranscribeOutputParsing(t *testing.T) {
	t.Parallel()

	// We test the output parsing indirectly via the engine's transcribe method.
	// Create a mock whisper-cli that outputs lines with timestamps (should be filtered)
	// and plain text (should be kept).
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "whisper-cli")

	// Create a minimal fake WAV file.
	wavFile := filepath.Join(tmpDir, "test.wav")
	minimalWAV := []byte{
		0x52, 0x49, 0x46, 0x46, 0x2c, 0x00, 0x00, 0x00, // RIFF....
		0x57, 0x41, 0x56, 0x45, // WAVE
		0x66, 0x6d, 0x74, 0x20, 0x10, 0x00, 0x00, 0x00, // fmt ....
		0x01, 0x00, 0x01, 0x00, 0x40, 0x3e, 0x00, 0x00, // format, channels, sample rate
		0x40, 0x1f, 0x00, 0x00, 0x02, 0x00, 0x10, 0x00, // byte rate, block align, bits
		0x64, 0x61, 0x74, 0x61, 0x00, 0x00, 0x00, 0x00, // data....
	}
	err := os.WriteFile(wavFile, minimalWAV, 0644)
	require.NoError(t, err)

	// Mock that outputs a mix of timestamp lines and text lines.
	script := `#!/bin/sh
# Output lines similar to whisper-cli
echo "[00:00.000 --> 00:02.000]  hello world"
echo "this is plain text"
echo "[00:02.000 --> 00:04.000]  more speech here"
echo "whisper loading model..."
echo "and final text"
exit 0
`
	err = os.WriteFile(binPath, []byte(script), 0755)
	require.NoError(t, err)

	engine, err := NewWhisperEngine(Config{
		Whisper: WhisperConfig{
			BinPath: binPath,
		},
	})
	require.NoError(t, err)

	text, err := engine.transcribe(wavFile)
	require.NoError(t, err)
	// Should contain the plain text lines, not timestamp or whisper info lines.
	assert.Contains(t, text, "this is plain text")
	assert.Contains(t, text, "and final text")
	// Should not contain timestamp markers.
	assert.NotContains(t, text, "[00:00")
	// Should not contain whisper info line.
	assert.NotContains(t, text, "whisper loading model")
}

func TestWhisperEngine_TranscribeWithModelPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "whisper-cli")

	// Create a fake WAV file with non-zero content.
	wavFile := filepath.Join(tmpDir, "test.wav")
	err := os.WriteFile(wavFile, []byte("RIFF fake wav data for testing"), 0644)
	require.NoError(t, err)

	modelPath := filepath.Join(tmpDir, "fake-model.bin")
	err = os.WriteFile(modelPath, []byte("fake model data"), 0644)
	require.NoError(t, err)

	// Mock that echoes transcription and exits 0.
	script := `#!/bin/sh
echo "transcribed text with custom model"
exit 0
`
	err = os.WriteFile(binPath, []byte(script), 0755)
	require.NoError(t, err)

	engine, err := NewWhisperEngine(Config{
		Whisper: WhisperConfig{
			BinPath:   binPath,
			ModelPath: modelPath,
			Threads:   8,
		},
		Language: "fr",
	})
	require.NoError(t, err)

	text, err := engine.transcribe(wavFile)
	require.NoError(t, err)
	assert.Equal(t, "transcribed text with custom model", text)
}

func TestWhisperEngine_TranscribeFailure(t *testing.T) {
	t.Parallel()

	binPath := createMockWhisperCLIFailing(t)

	// Create a WAV file.
	tmpDir := t.TempDir()
	wavFile := filepath.Join(tmpDir, "test.wav")
	err := os.WriteFile(wavFile, []byte("RIFF fake wav data"), 0644)
	require.NoError(t, err)

	engine, err := NewWhisperEngine(Config{
		Whisper: WhisperConfig{BinPath: binPath},
	})
	require.NoError(t, err)

	text, err := engine.transcribe(wavFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "whisper-cli failed")
	assert.Empty(t, text)
}

func TestCheckWhisperAvailable_BinaryNotFound(t *testing.T) {
	t.Parallel()

	err := checkWhisperAvailable(Config{
		Whisper: WhisperConfig{BinPath: "/nonexistent/whisper-cli"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "whisper-cli not found")
}

func TestCheckWhisperAvailable_ModelNotFound(t *testing.T) {
	t.Parallel()

	// Create a mock whisper-cli that exists.
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "whisper-cli")
	err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0755)
	require.NoError(t, err)

	err = checkWhisperAvailable(Config{
		Whisper: WhisperConfig{
			BinPath:   binPath,
			ModelPath: "/nonexistent/model.bin",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model not found")
}

func TestResolveWhisperModelPath(t *testing.T) {
	t.Parallel()

	t.Run("explicit path returned as-is", func(t *testing.T) {
		t.Parallel()
		got := resolveWhisperModelPath("/my/custom/model.bin")
		assert.Equal(t, "/my/custom/model.bin", got)
	})

	t.Run("empty path defaults to home directory", func(t *testing.T) {
		t.Parallel()
		got := resolveWhisperModelPath("")
		home, err := os.UserHomeDir()
		require.NoError(t, err)
		expected := filepath.Join(home, ".meept", "models", "ggml-base.en.bin")
		assert.Equal(t, expected, got)
	})
}

func TestWhisperEngine_ImplementsTranscriber(t *testing.T) {
	t.Parallel()

	// Compile-time interface check.
	var _ Transcriber = (*WhisperEngine)(nil)
}
