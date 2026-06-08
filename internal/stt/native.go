package stt

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// NativeEngine implements Transcriber using platform-native speech recognition.
//
// On macOS and Windows it records audio via ffmpeg/sox and then transcribes
// using a helper binary (meept-native-stt) if available. On Linux it is
// not supported and returns an error.
type NativeEngine struct {
	recorder  *Recorder
	recording bool
	mu        sync.Mutex
}

// NewNativeEngine creates a new NativeEngine.
// Returns an error on Linux where native STT is not supported.
func NewNativeEngine(cfg Config) (*NativeEngine, error) {
	if runtime.GOOS == "linux" {
		return nil, fmt.Errorf("stt: native STT not supported on Linux; use whisper or parakeet engine")
	}

	return &NativeEngine{
		recorder: NewRecorder(RecordingConfig{
			RecorderBin: cfg.Recording.RecorderBin,
			SampleRate:  cfg.Recording.SampleRate,
			Channels:    cfg.Recording.Channels,
			Format:      cfg.Recording.Format,
		}),
	}, nil
}

// Start begins recording audio using the shared recorder.
func (e *NativeEngine) Start(ctx context.Context, onResult func(Result)) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.recording {
		return fmt.Errorf("stt: native engine already recording")
	}

	slog.Debug("stt: starting native recording", "engine", e.Name())

	if err := e.recorder.Start(); err != nil {
		return fmt.Errorf("stt: native: start recording: %w", err)
	}

	e.recording = true
	return nil
}

// Stop stops recording, transcribes using the platform-native helper,
// and returns the transcribed text.
func (e *NativeEngine) Stop() (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.recording {
		return "", fmt.Errorf("stt: native engine not recording")
	}

	e.recording = false
	slog.Debug("stt: stopping native recording", "engine", e.Name())

	if err := e.recorder.Stop(); err != nil {
		e.recorder.Cleanup()
		return "", fmt.Errorf("stt: native: stop recording: %w", err)
	}

	filePath := e.recorder.FilePath()
	defer e.recorder.Cleanup()

	if filePath == "" {
		return "", fmt.Errorf("stt: native: no recording file")
	}

	// Check file exists and has content.
	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("stt: native: stat recording file: %w", err)
	}
	if info.Size() == 0 {
		return "", nil // silent discard for zero-length recording
	}

	text, err := e.transcribe(filePath)
	if err != nil {
		return "", err
	}

	return text, nil
}

// IsRecording returns whether the native engine is currently recording.
func (e *NativeEngine) IsRecording() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.recording
}

// Name returns the engine name for logging and UI display.
func (e *NativeEngine) Name() string {
	return "native"
}

// transcribe attempts platform-native transcription on the WAV file.
//
// It tries, in order:
//  1. meept-native-stt helper binary (alongside the current executable)
//  2. python3 with speech_recognition module
//  3. Returns error suggesting to install whisper or parakeet
func (e *NativeEngine) transcribe(filePath string) (string, error) {
	// Strategy 1: Try the meept-native-stt helper binary.
	helperBin, err := findHelperBinary()
	if err == nil {
		text, err := e.transcribeWithHelper(helperBin, filePath)
		if err == nil {
			return text, nil
		}
		slog.Debug("stt: native helper failed, trying fallback", "error", err)
	}

	// Strategy 2: Try Python with speech_recognition module.
	text, err := e.transcribeWithPython(filePath)
	if err == nil {
		return text, nil
	}
	slog.Debug("stt: python fallback failed", "error", err)

	// Strategy 3: No working native transcription available.
	return "", fmt.Errorf(
		"stt: native transcription unavailable on %s; install whisper or parakeet engine for speech-to-text",
		runtime.GOOS,
	)
}

// findHelperBinary locates the meept-native-stt helper binary.
// It searches in the same directory as the current executable.
func findHelperBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exeDir := filepath.Dir(exe)

	helper := filepath.Join(exeDir, "meept-native-stt")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}

	if _, err := os.Stat(helper); err == nil {
		return helper, nil
	}

	return "", fmt.Errorf("stt: meept-native-stt helper not found at %q", helper)
}

// transcribeWithHelper runs the meept-native-stt helper with the WAV file path.
func (e *NativeEngine) transcribeWithHelper(helperBin, filePath string) (string, error) {
	slog.Debug("stt: native: using helper binary", "bin", helperBin)

	cmd := exec.Command(helperBin, filePath)
	stdout, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("stt: native: helper failed: %w", err)
	}

	text := strings.TrimSpace(string(stdout))
	return text, nil
}

// transcribeWithPython attempts transcription using Python's speech_recognition module.
func (e *NativeEngine) transcribeWithPython(filePath string) (string, error) {
	if _, err := exec.LookPath("python3"); err != nil {
		return "", fmt.Errorf("stt: native: python3 not found")
	}

	// Check that speech_recognition is importable.
	check := exec.Command("python3", "-c", "import speech_recognition")
	if err := check.Run(); err != nil {
		return "", fmt.Errorf("stt: native: python3 speech_recognition module not installed")
	}

	slog.Debug("stt: native: using python3 speech_recognition")

	script := fmt.Sprintf(`
import speech_recognition as sr
import sys
r = sr.Recognizer()
with sr.AudioFile(%q) as source:
    audio = r.record(source)
try:
    text = r.recognize_google(audio)
    print(text)
except sr.UnknownValueError:
    print("", end="")
except Exception as e:
    print("", end="")
    sys.exit(1)
`, filePath)

	cmd := exec.Command("python3", "-c", script)
	stdout, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("stt: native: python transcription failed: %w", err)
	}

	text := strings.TrimSpace(string(stdout))
	return text, nil
}

// checkNativeAvailable verifies that native STT can work on the current platform.
func checkNativeAvailable() error {
	if runtime.GOOS == "linux" {
		return fmt.Errorf("stt: native STT not supported on Linux; use whisper or parakeet engine")
	}

	// Check if the helper binary exists.
	if _, err := findHelperBinary(); err == nil {
		return nil
	}

	// Check if python3 + speech_recognition is available.
	if _, err := exec.LookPath("python3"); err == nil {
		check := exec.Command("python3", "-c", "import speech_recognition")
		if err := check.Run(); err == nil {
			return nil
		}
	}

	// Native is "available" but may not have a backend. Return nil since the
	// engine can be constructed (it will fail at transcription time with a
	// helpful message). Log a warning.
	slog.Warn("stt: native engine available but no transcription backend found; install meept-native-stt helper or python3 speech_recognition")
	return nil
}
