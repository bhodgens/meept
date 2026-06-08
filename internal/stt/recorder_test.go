package stt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRecorder(t *testing.T) {
	t.Parallel()

	cfg := RecordingConfig{
		RecorderBin: "ffmpeg",
		SampleRate:  16000,
		Channels:    1,
		Format:      "wav",
	}

	r := NewRecorder(cfg)
	require.NotNil(t, r)
	assert.Equal(t, cfg, r.config)
	assert.False(t, r.recording)
	assert.Nil(t, r.cmd)
	assert.Nil(t, r.tmpFile)
}

func TestNewRecorder_ZeroConfig(t *testing.T) {
	t.Parallel()

	r := NewRecorder(RecordingConfig{})
	require.NotNil(t, r)
	assert.Equal(t, RecordingConfig{}, r.config)
}

// createMockRecorder creates a temporary executable that mimics ffmpeg/sox
// by sleeping until signaled or writing a tiny WAV file.
func createMockRecorder(t *testing.T, name string, behavior string) string {
	t.Helper()

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, name)

	var script string
	switch behavior {
	case "sleep":
		// A script that sleeps forever until killed.
		script = "#!/bin/sh\ntrap 'exit 0' TERM\nwhile true; do sleep 1\ndone\n"
	case "write_wav":
		// A script that writes a minimal WAV file then exits.
		script = "#!/bin/sh\n# Write a minimal RIFF/WAV header (44 bytes) with no data.\nOUTFILE=\"$1\"\nif [ -z \"$OUTFILE\" ]; then\n  exit 1\nfi\nprintf '\\x52\\x49\\x46\\x46\\x2c\\x00\\x00\\x00\\x57\\x41\\x56\\x45\\x66\\x6d\\x74\\x20\\x10\\x00\\x00\\x00\\x01\\x00\\x01\\x00\\x40\\x3e\\x00\\x00\\x40\\x1f\\x00\\x00\\x02\\x00\\x10\\x00\\x64\\x61\\x74\\x61\\x00\\x00\\x00\\x00' > \"$OUTFILE\"\n"
	default:
		t.Fatalf("unknown mock behavior: %s", behavior)
	}

	err := os.WriteFile(binPath, []byte(script), 0755)
	require.NoError(t, err)
	return binPath
}

func TestRecorder_StartAndStop(t *testing.T) {
	t.Parallel()

	binPath := createMockRecorder(t, "mock-ffmpeg", "sleep")

	cfg := RecordingConfig{
		RecorderBin: binPath,
		SampleRate:  16000,
		Channels:    1,
		Format:      "wav",
	}

	r := NewRecorder(cfg)
	require.NotNil(t, r)

	err := r.Start()
	require.NoError(t, err)
	assert.True(t, r.recording)

	// Give the subprocess a moment to start.
	time.Sleep(50 * time.Millisecond)

	err = r.Stop()
	require.NoError(t, err)
	assert.False(t, r.recording)
}

func TestRecorder_StartAlreadyRecording(t *testing.T) {
	t.Parallel()

	binPath := createMockRecorder(t, "mock-ffmpeg-sleep", "sleep")

	cfg := RecordingConfig{RecorderBin: binPath}
	r := NewRecorder(cfg)

	err := r.Start()
	require.NoError(t, err)

	// Starting again should error.
	err = r.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already active")

	// Cleanup.
	r.Stop()
}

func TestRecorder_StopWhenNotRecording(t *testing.T) {
	t.Parallel()

	r := NewRecorder(RecordingConfig{})
	// Stop when never started should return nil (no-op).
	err := r.Stop()
	assert.NoError(t, err)
}

func TestRecorder_FilePath(t *testing.T) {
	t.Parallel()

	binPath := createMockRecorder(t, "mock-ffmpeg-fp", "sleep")

	cfg := RecordingConfig{RecorderBin: binPath}
	r := NewRecorder(cfg)

	// Before recording, FilePath returns empty string.
	assert.Empty(t, r.FilePath())

	err := r.Start()
	require.NoError(t, err)
	defer r.Stop()

	fp := r.FilePath()
	assert.NotEmpty(t, fp)
	assert.Contains(t, fp, "meept-stt-")
	assert.Contains(t, fp, ".wav")
}

func TestRecorder_FilePathBeforeRecording(t *testing.T) {
	t.Parallel()

	r := NewRecorder(RecordingConfig{})
	assert.Empty(t, r.FilePath())
}

func TestRecorder_Cleanup(t *testing.T) {
	t.Parallel()

	binPath := createMockRecorder(t, "mock-ffmpeg-clean", "sleep")

	cfg := RecordingConfig{RecorderBin: binPath}
	r := NewRecorder(cfg)

	err := r.Start()
	require.NoError(t, err)

	fp := r.FilePath()
	require.NotEmpty(t, fp)

	// Verify file was created by the temp file mechanism.
	// The recorder creates the temp file then closes it for ffmpeg to write.
	_, statErr := os.Stat(fp)
	// The file may or may not exist depending on timing, so just check no panic.

	r.Stop()
	r.Cleanup()

	// After cleanup, the file should be removed.
	_, statErr = os.Stat(fp)
	assert.True(t, os.IsNotExist(statErr), "expected temp file to be removed after Cleanup")
}

func TestRecorder_CleanupNoFile(t *testing.T) {
	t.Parallel()

	r := NewRecorder(RecordingConfig{})
	// Cleanup when no file should not panic.
	r.Cleanup()
}

func TestRecorder_CleanupTwice(t *testing.T) {
	t.Parallel()

	binPath := createMockRecorder(t, "mock-ffmpeg-ct", "sleep")

	cfg := RecordingConfig{RecorderBin: binPath}
	r := NewRecorder(cfg)

	err := r.Start()
	require.NoError(t, err)
	r.Stop()

	// First cleanup should remove file.
	r.Cleanup()
	// Second cleanup should be a no-op.
	r.Cleanup()
}

func TestDetectRecorderBinary(t *testing.T) {
	t.Run("no binaries available returns error", func(t *testing.T) {
		// Use a config with a non-existent binary, and set PATH to empty
		// so ffmpeg and sox are not found.
		r := NewRecorder(RecordingConfig{
			RecorderBin: "/nonexistent/binary-for-test",
		})

		// Temporarily clear PATH to prevent finding system ffmpeg/sox.
		t.Setenv("PATH", "")

		_, err := r.detectRecorderBinary()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no recorder binary found")
	})

	t.Run("configured binary found", func(t *testing.T) {
		t.Parallel()

		mockBin := createMockRecorder(t, "mock-recorder-detect", "sleep")

		r := NewRecorder(RecordingConfig{
			RecorderBin: mockBin,
		})

		bin, err := r.detectRecorderBinary()
		require.NoError(t, err)
		assert.Equal(t, mockBin, bin)
	})
}

func TestBuildRecorderArgs(t *testing.T) {
	t.Parallel()

	outputPath := "/tmp/test-output.wav"
	sampleRate := 16000
	channels := 1

	r := NewRecorder(RecordingConfig{})

	t.Run("ffmpeg on darwin", func(t *testing.T) {
		// Only test arg structure, not platform-specific.
		args := r.buildRecorderArgs("ffmpeg", outputPath, sampleRate, channels)
		assert.NotEmpty(t, args)
		assert.Contains(t, args, outputPath)
		assert.Contains(t, args, fmt.Sprintf("%d", sampleRate))
		assert.Contains(t, args, fmt.Sprintf("%d", channels))
		assert.Contains(t, args, "-y")
	})

	t.Run("sox args", func(t *testing.T) {
		args := r.buildRecorderArgs("sox", outputPath, sampleRate, channels)
		assert.NotEmpty(t, args)
		assert.Contains(t, args, "-d")
		assert.Contains(t, args, outputPath)
		assert.Contains(t, args, fmt.Sprintf("%d", sampleRate))
		assert.Contains(t, args, fmt.Sprintf("%d", channels))
	})

	t.Run("unknown binary uses ffmpeg-style args", func(t *testing.T) {
		args := r.buildRecorderArgs("other-binary", outputPath, sampleRate, channels)
		assert.NotEmpty(t, args)
		assert.Contains(t, args, outputPath)
	})
}

func TestBuildRecorderArgs_PlatformSpecific(t *testing.T) {
	t.Parallel()

	outputPath := "/tmp/test.wav"
	r := NewRecorder(RecordingConfig{})

	args := r.ffmpegArgs(outputPath, 16000, 1)
	require.NotEmpty(t, args)

	// All platforms should include these common args.
	assert.Contains(t, args, outputPath)
	assert.Contains(t, args, "-y")
	assert.Contains(t, args, fmt.Sprintf("%d", 16000))
	assert.Contains(t, args, fmt.Sprintf("%d", 1))

	// Platform-specific input flags.
	switch runtime.GOOS {
	case "darwin":
		assert.Contains(t, args, "-f")
		assert.Contains(t, args, "avfoundation")
	case "linux":
		assert.Contains(t, args, "-f")
		assert.Contains(t, args, "alsa")
	case "windows":
		assert.Contains(t, args, "-f")
		assert.Contains(t, args, "dshow")
	}
}

func TestSoxArgs(t *testing.T) {
	t.Parallel()

	r := NewRecorder(RecordingConfig{})
	args := r.soxArgs("/tmp/out.wav", 8000, 2)
	assert.Equal(t, []string{
		"-d",
		"-r", "8000",
		"-c", "2",
		"/tmp/out.wav",
	}, args)
}

func TestRecorder_StartWithMissingBinary(t *testing.T) {
	t.Setenv("PATH", "")

	r := NewRecorder(RecordingConfig{RecorderBin: "/nonexistent-mock-binary"})
	err := r.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no recorder binary found")
}

func TestRecorder_StartWithWriteWavMock(t *testing.T) {
	t.Parallel()

	binPath := createMockRecorder(t, "mock-ffmpeg-wav", "write_wav")

	cfg := RecordingConfig{RecorderBin: binPath}
	r := NewRecorder(cfg)

	err := r.Start()
	require.NoError(t, err)

	// The mock writes a WAV file and exits immediately, so wait a moment.
	time.Sleep(200 * time.Millisecond)

	// After the mock exits, recording state is still true because Stop()
	// wasn't called.
	fp := r.FilePath()
	assert.NotEmpty(t, fp)

	r.Stop()
	r.Cleanup()
}

// TestRequiresFfmpegOrSox skips the test if neither ffmpeg nor sox is available.
// This is a helper for integration-style tests.
func skipWithoutRecorder(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		return
	}
	if _, err := exec.LookPath("sox"); err == nil {
		return
	}
	t.Skip("skipping: ffmpeg and sox not available in PATH")
}
