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

// ParakeetEngine implements Transcriber using parakeet-transcribe (parakeet.cpp).
type ParakeetEngine struct {
	config    Config
	recorder  *Recorder
	recording bool
	mu        sync.Mutex
}

// NewParakeetEngine creates a new ParakeetEngine, validating that the
// parakeet-transcribe binary is available.
func NewParakeetEngine(cfg Config) (*ParakeetEngine, error) {
	bin := cfg.Parakeet.BinPath
	if bin == "" {
		bin = "parakeet-transcribe"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("stt: parakeet-transcribe not found (looked for %q): %w", bin, err)
	}

	return &ParakeetEngine{
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
func (e *ParakeetEngine) Start(ctx context.Context, onResult func(Result)) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.recording {
		return fmt.Errorf("stt: parakeet engine already recording")
	}

	slog.Debug("stt: starting parakeet recording", "engine", e.Name())

	if err := e.recorder.Start(); err != nil {
		return fmt.Errorf("stt: parakeet: start recording: %w", err)
	}

	e.recording = true
	return nil
}

// Stop stops recording, runs parakeet-transcribe on the captured audio,
// and returns the transcribed text.
func (e *ParakeetEngine) Stop() (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.recording {
		return "", fmt.Errorf("stt: parakeet engine not recording")
	}

	e.recording = false
	slog.Debug("stt: stopping parakeet recording", "engine", e.Name())

	if err := e.recorder.Stop(); err != nil {
		e.recorder.Cleanup()
		return "", fmt.Errorf("stt: parakeet: stop recording: %w", err)
	}

	filePath := e.recorder.FilePath()
	defer e.recorder.Cleanup()

	if filePath == "" {
		return "", fmt.Errorf("stt: parakeet: no recording file")
	}

	// Check file exists and has content.
	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("stt: parakeet: stat recording file: %w", err)
	}
	if info.Size() == 0 {
		return "", nil // silent discard for zero-length recording
	}

	text, err := e.transcribe(filePath)
	if err != nil {
		return "", fmt.Errorf("stt: parakeet: transcription failed: %w", err)
	}

	return text, nil
}

// IsRecording returns whether the parakeet engine is currently recording.
func (e *ParakeetEngine) IsRecording() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.recording
}

// Name returns the engine name for logging and UI display.
func (e *ParakeetEngine) Name() string {
	return "parakeet"
}

// transcribe runs parakeet-transcribe on the given WAV file and returns
// the transcribed text.
func (e *ParakeetEngine) transcribe(filePath string) (string, error) {
	bin := e.config.Parakeet.BinPath
	if bin == "" {
		bin = "parakeet-transcribe"
	}
	modelPath := resolveParakeetModelPath(e.config.Parakeet.ModelPath)

	args := []string{
		"--model", modelPath,
		"--input", filePath,
	}

	slog.Debug("stt: running parakeet-transcribe", "args", args)

	// CommandContext for cancellability on shutdown (S6-4).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stt: parakeet: create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("stt: parakeet: create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("stt: parakeet: start parakeet-transcribe: %w", err)
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

	// Parse stdout: capture all non-empty lines as transcription text.
	var lines []string
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		// Skip common info/progress lines from parakeet.
		if strings.HasPrefix(line, "parakeet") || strings.HasPrefix(line, "Loading") {
			continue
		}
		lines = append(lines, line)
	}

	if err := cmd.Wait(); err != nil {
		stderrText := strings.TrimSpace(stderrBuf.String())
		if stderrText != "" {
			return "", fmt.Errorf("stt: parakeet: parakeet-transcribe failed: %w: %s", err, stderrText)
		}
		return "", fmt.Errorf("stt: parakeet: parakeet-transcribe failed: %w", err)
	}

	text := strings.TrimSpace(strings.Join(lines, " "))
	return text, nil
}

// resolveParakeetModelPath returns the model path, defaulting to
// ~/.meept/models/parakeet-tdt-ctc-110m if not set.
func resolveParakeetModelPath(modelPath string) string {
	if modelPath != "" {
		return modelPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return modelPath
	}
	return filepath.Join(home, ".meept", "models", "parakeet-tdt-ctc-110m")
}

// checkParakeetAvailable verifies that parakeet-transcribe and its model file
// are available.
func checkParakeetAvailable(cfg Config) error {
	bin := cfg.Parakeet.BinPath
	if bin == "" {
		bin = "parakeet-transcribe"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("stt: parakeet-transcribe not found (looked for %q)", bin)
	}

	modelPath := resolveParakeetModelPath(cfg.Parakeet.ModelPath)
	if _, err := os.Stat(modelPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("stt: parakeet model not found at %q", modelPath)
		}
		return fmt.Errorf("stt: parakeet model: %w", err)
	}

	return nil
}
