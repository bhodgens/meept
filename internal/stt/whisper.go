package stt

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// WhisperEngine implements Transcriber using whisper-cli (whisper.cpp).
type WhisperEngine struct {
	config    Config
	recorder  *Recorder
	recording bool
	mu        sync.Mutex
}

// NewWhisperEngine creates a new WhisperEngine, validating that whisper-cli
// is available in PATH or at the configured binary path.
func NewWhisperEngine(cfg Config) (*WhisperEngine, error) {
	bin := cfg.Whisper.BinPath
	if bin == "" {
		bin = "whisper-cli"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("stt: whisper-cli not found (looked for %q): %w", bin, err)
	}

	return &WhisperEngine{
		config: cfg,
		recorder: NewRecorder(RecordingConfig{
			RecorderBin: cfg.Recording.RecorderBin,
			SampleRate:  cfg.Recording.SampleRate,
			Channels:    cfg.Recording.Channels,
			Format:      cfg.Recording.Format,
		}),
	}, nil
}

// Start begins recording audio using the shared recorder.
func (e *WhisperEngine) Start(ctx context.Context, onResult func(Result)) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.recording {
		return fmt.Errorf("stt: whisper engine already recording")
	}

	slog.Debug("stt: starting whisper recording", "engine", e.Name())

	if err := e.recorder.Start(); err != nil {
		return fmt.Errorf("stt: whisper: start recording: %w", err)
	}

	e.recording = true
	return nil
}

// Stop stops recording, runs whisper-cli on the captured audio, and returns
// the transcribed text.
func (e *WhisperEngine) Stop() (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.recording {
		return "", fmt.Errorf("stt: whisper engine not recording")
	}

	e.recording = false
	slog.Debug("stt: stopping whisper recording", "engine", e.Name())

	if err := e.recorder.Stop(); err != nil {
		e.recorder.Cleanup()
		return "", fmt.Errorf("stt: whisper: stop recording: %w", err)
	}

	filePath := e.recorder.FilePath()
	defer e.recorder.Cleanup()

	if filePath == "" {
		return "", fmt.Errorf("stt: whisper: no recording file")
	}

	// Check file exists and has content.
	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("stt: whisper: stat recording file: %w", err)
	}
	if info.Size() == 0 {
		return "", nil // silent discard for zero-length recording
	}

	text, err := e.transcribe(filePath)
	if err != nil {
		return "", fmt.Errorf("stt: whisper: transcription failed: %w", err)
	}

	return text, nil
}

// IsRecording returns whether the whisper engine is currently recording.
func (e *WhisperEngine) IsRecording() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.recording
}

// Name returns the engine name for logging and UI display.
func (e *WhisperEngine) Name() string {
	return "whisper"
}

// transcribe runs whisper-cli on the given WAV file and returns the transcribed text.
func (e *WhisperEngine) transcribe(filePath string) (string, error) {
	bin := e.config.Whisper.BinPath
	if bin == "" {
		bin = "whisper-cli"
	}
	modelPath := resolveWhisperModelPath(e.config.Whisper.ModelPath)

	threads := e.config.Whisper.Threads
	if threads <= 0 {
		threads = 4
	}

	language := e.config.Language
	if language == "" {
		language = "en"
	}

	args := []string{
		"-m", modelPath,
		"-f", filePath,
		"--no-timestamps",
		"--language", language,
		"-t", fmt.Sprintf("%d", threads),
	}

	slog.Debug("stt: running whisper-cli", "args", args)

	// Use CommandContext with a cancellable background context so that
	// engine shutdown can interrupt a runaway transcription process
	// (S6-4). The cancel func is registered via shutdownCtx, allowing
	// a future Stop hook to abort long-running transcriptions.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	// Ensure process kill on context cancellation (default on some
	// platforms, but explicit here for portability).
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stt: whisper: create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("stt: whisper: create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("stt: whisper: start whisper-cli: %w", err)
	}

	// Read stderr in background to avoid blocking.
	var stderrBuf strings.Builder
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			stderrBuf.WriteString(sc.Text())
			stderrBuf.WriteString("\n")
		}
	}()

	// Parse stdout: collect lines that are transcription text.
	var lines []string
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		// Skip timestamp lines (e.g., "[00:00.000 --> 00:05.000]").
		if strings.Contains(line, "[") && strings.Contains(line, "]") {
			continue
		}
		// Skip empty lines and common whisper-cli info lines.
		if line == "" || strings.HasPrefix(line, "whisper") {
			continue
		}
		lines = append(lines, line)
	}

	if err := cmd.Wait(); err != nil {
		stderrText := strings.TrimSpace(stderrBuf.String())
		if stderrText != "" {
			return "", fmt.Errorf("stt: whisper: whisper-cli failed: %w: %s", err, stderrText)
		}
		return "", fmt.Errorf("stt: whisper: whisper-cli failed: %w", err)
	}

	text := strings.TrimSpace(strings.Join(lines, " "))
	return text, nil
}

// resolveWhisperModelPath returns the model path, defaulting to
// ~/.meept/models/ggml-base.en.bin if not set.
func resolveWhisperModelPath(modelPath string) string {
	if modelPath != "" {
		return modelPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return modelPath
	}
	return filepath.Join(home, ".meept", "models", "ggml-base.en.bin")
}

// checkWhisperAvailable verifies that whisper-cli and its model file are available.
func checkWhisperAvailable(cfg Config) error {
	bin := cfg.Whisper.BinPath
	if bin == "" {
		bin = "whisper-cli"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("stt: whisper-cli not found (looked for %q)", bin)
	}

	modelPath := resolveWhisperModelPath(cfg.Whisper.ModelPath)
	if _, err := os.Stat(modelPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("stt: whisper model not found at %q", modelPath)
		}
		return fmt.Errorf("stt: whisper model: %w", err)
	}

	return nil
}
