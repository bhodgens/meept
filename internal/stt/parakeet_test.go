package stt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createMockParakeetCLI creates a fake parakeet-transcribe binary that echoes
// a fixed transcription to stdout.
func createMockParakeetCLI(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "parakeet-transcribe")

	script := `#!/bin/sh
# Mock parakeet-transcribe: echoes transcription text to stdout.
echo "parakeet transcription result"
exit 0
`
	err := os.WriteFile(binPath, []byte(script), 0755)
	require.NoError(t, err)
	return binPath
}

// createMockParakeetCLIFailing creates a fake parakeet that exits with error.
func createMockParakeetCLIFailing(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "parakeet-transcribe")

	script := `#!/bin/sh
echo "error: failed to load model" >&2
exit 1
`
	err := os.WriteFile(binPath, []byte(script), 0755)
	require.NoError(t, err)
	return binPath
}

func TestNewParakeetEngine_Success(t *testing.T) {
	t.Parallel()

	binPath := createMockParakeetCLI(t)

	engine, err := NewParakeetEngine(Config{
		Engine: "parakeet",
		Parakeet: ParakeetConfig{
			BinPath: binPath,
		},
		Recording: RecordingConfig{
			RecorderBin: "nonexistent-recorder",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, engine)
	assert.Equal(t, "parakeet", engine.Name())
	assert.False(t, engine.IsRecording())
}

func TestNewParakeetEngine_BinaryNotFound(t *testing.T) {
	t.Parallel()

	_, err := NewParakeetEngine(Config{
		Engine: "parakeet",
		Parakeet: ParakeetConfig{
			BinPath: "/nonexistent/parakeet-transcribe",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parakeet-transcribe not found")
}

func TestNewParakeetEngine_DefaultBinPath(t *testing.T) {
	t.Parallel()

	// Default bin path is "parakeet-transcribe" which likely doesn't exist.
	_, err := NewParakeetEngine(Config{Engine: "parakeet"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parakeet-transcribe not found")
}

func TestParakeetEngine_Name(t *testing.T) {
	t.Parallel()

	binPath := createMockParakeetCLI(t)

	engine, err := NewParakeetEngine(Config{
		Parakeet: ParakeetConfig{BinPath: binPath},
	})
	require.NoError(t, err)
	assert.Equal(t, "parakeet", engine.Name())
}

func TestParakeetEngine_IsRecording_InitiallyFalse(t *testing.T) {
	t.Parallel()

	binPath := createMockParakeetCLI(t)

	engine, err := NewParakeetEngine(Config{
		Parakeet: ParakeetConfig{BinPath: binPath},
	})
	require.NoError(t, err)
	assert.False(t, engine.IsRecording())
}

func TestParakeetEngine_Stop_WhenNotRecording(t *testing.T) {
	t.Parallel()

	binPath := createMockParakeetCLI(t)

	engine, err := NewParakeetEngine(Config{
		Parakeet: ParakeetConfig{BinPath: binPath},
	})
	require.NoError(t, err)

	text, err := engine.Stop()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not recording")
	assert.Empty(t, text)
}

func TestParakeetEngine_TranscribeOutputParsing(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "parakeet-transcribe")

	// Create a fake WAV file with content.
	wavFile := filepath.Join(tmpDir, "test.wav")
	err := os.WriteFile(wavFile, []byte("RIFF fake wav data for testing"), 0644)
	require.NoError(t, err)

	// Mock that outputs a mix of info lines and text lines.
	script := `#!/bin/sh
echo "parakeet-transcribe v1.0"
echo "Loading model..."
echo "transcribed line one"
echo "parakeet processing audio"
echo "transcribed line two"
exit 0
`
	err = os.WriteFile(binPath, []byte(script), 0755)
	require.NoError(t, err)

	engine, err := NewParakeetEngine(Config{
		Parakeet: ParakeetConfig{BinPath: binPath},
	})
	require.NoError(t, err)

	text, err := engine.transcribe(wavFile)
	require.NoError(t, err)
	// Should contain text lines, not info/progress lines.
	assert.Contains(t, text, "transcribed line one")
	assert.Contains(t, text, "transcribed line two")
	// Should not contain info lines.
	assert.NotContains(t, text, "parakeet-transcribe v1.0")
	assert.NotContains(t, text, "Loading model")
	assert.NotContains(t, text, "parakeet processing audio")
}

func TestParakeetEngine_TranscribeWithModelPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "parakeet-transcribe")

	wavFile := filepath.Join(tmpDir, "test.wav")
	err := os.WriteFile(wavFile, []byte("RIFF fake wav data for testing"), 0644)
	require.NoError(t, err)

	modelPath := filepath.Join(tmpDir, "fake-parakeet-model")
	err = os.WriteFile(modelPath, []byte("fake model data"), 0644)
	require.NoError(t, err)

	script := `#!/bin/sh
echo "custom model transcription"
exit 0
`
	err = os.WriteFile(binPath, []byte(script), 0755)
	require.NoError(t, err)

	engine, err := NewParakeetEngine(Config{
		Parakeet: ParakeetConfig{
			BinPath:   binPath,
			ModelPath: modelPath,
		},
	})
	require.NoError(t, err)

	text, err := engine.transcribe(wavFile)
	require.NoError(t, err)
	assert.Equal(t, "custom model transcription", text)
}

func TestParakeetEngine_TranscribeFailure(t *testing.T) {
	t.Parallel()

	binPath := createMockParakeetCLIFailing(t)

	tmpDir := t.TempDir()
	wavFile := filepath.Join(tmpDir, "test.wav")
	err := os.WriteFile(wavFile, []byte("RIFF fake wav data"), 0644)
	require.NoError(t, err)

	engine, err := NewParakeetEngine(Config{
		Parakeet: ParakeetConfig{BinPath: binPath},
	})
	require.NoError(t, err)

	text, err := engine.transcribe(wavFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parakeet-transcribe failed")
	assert.Empty(t, text)
}

func TestParakeetEngine_TranscribeEmptyOutput(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "parakeet-transcribe")

	wavFile := filepath.Join(tmpDir, "test.wav")
	err := os.WriteFile(wavFile, []byte("RIFF fake wav data for testing"), 0644)
	require.NoError(t, err)

	// Mock that outputs only info lines (no actual transcription).
	script := `#!/bin/sh
echo "parakeet-transcribe v1.0"
echo "Loading model..."
echo "parakeet processing audio"
exit 0
`
	err = os.WriteFile(binPath, []byte(script), 0755)
	require.NoError(t, err)

	engine, err := NewParakeetEngine(Config{
		Parakeet: ParakeetConfig{BinPath: binPath},
	})
	require.NoError(t, err)

	text, err := engine.transcribe(wavFile)
	require.NoError(t, err)
	// All output lines were filtered, so text should be empty.
	assert.Empty(t, text)
}

func TestCheckParakeetAvailable_BinaryNotFound(t *testing.T) {
	t.Parallel()

	err := checkParakeetAvailable(Config{
		Parakeet: ParakeetConfig{BinPath: "/nonexistent/parakeet-transcribe"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parakeet-transcribe not found")
}

func TestCheckParakeetAvailable_ModelNotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "parakeet-transcribe")
	err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0755)
	require.NoError(t, err)

	err = checkParakeetAvailable(Config{
		Parakeet: ParakeetConfig{
			BinPath:   binPath,
			ModelPath: "/nonexistent/model",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model not found")
}

func TestCheckParakeetAvailable_BothFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "parakeet-transcribe")
	err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0755)
	require.NoError(t, err)

	modelPath := filepath.Join(tmpDir, "model")
	err = os.WriteFile(modelPath, []byte("model data"), 0644)
	require.NoError(t, err)

	err = checkParakeetAvailable(Config{
		Parakeet: ParakeetConfig{
			BinPath:   binPath,
			ModelPath: modelPath,
		},
	})
	require.NoError(t, err)
}

func TestResolveParakeetModelPath(t *testing.T) {
	t.Parallel()

	t.Run("explicit path returned as-is", func(t *testing.T) {
		t.Parallel()
		got := resolveParakeetModelPath("/my/custom/model")
		assert.Equal(t, "/my/custom/model", got)
	})

	t.Run("empty path defaults to home directory", func(t *testing.T) {
		t.Parallel()
		got := resolveParakeetModelPath("")
		home, err := os.UserHomeDir()
		require.NoError(t, err)
		expected := filepath.Join(home, ".meept", "models", "parakeet-tdt-ctc-110m")
		assert.Equal(t, expected, got)
	})
}

func TestParakeetEngine_ImplementsTranscriber(t *testing.T) {
	t.Parallel()

	// Compile-time interface check.
	var _ Transcriber = (*ParakeetEngine)(nil)
}
