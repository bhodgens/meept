package stt

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"
)

// Recorder handles audio capture via an external binary (ffmpeg or sox).
// It manages the subprocess lifecycle and temporary WAV file.
type Recorder struct {
	cmd       *exec.Cmd
	tmpFile   *os.File
	config    RecordingConfig
	recording bool
	mu        sync.Mutex
}

// NewRecorder creates a Recorder with the given configuration.
func NewRecorder(cfg RecordingConfig) *Recorder {
	return &Recorder{
		config: cfg,
	}
}

// Start begins recording audio to a temporary WAV file.
// It detects the available recorder binary (ffmpeg preferred, sox fallback)
// and spawns the subprocess.
func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.recording {
		return fmt.Errorf("stt: recorder already active")
	}

	bin, err := r.detectRecorderBinary()
	if err != nil {
		return err
	}

	// Create temp file for recording output.
	tmp, err := os.CreateTemp("", "meept-stt-*.wav")
	if err != nil {
		return fmt.Errorf("stt: create temp file: %w", err)
	}
	r.tmpFile = tmp
	// Close the file handle so ffmpeg can write to it.
	tmp.Close() //nolint:mutexio // closing fresh temp file handle; no concurrent access possible

	sampleRate := r.config.SampleRate
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	channels := r.config.Channels
	if channels <= 0 {
		channels = 1
	}

	args := r.buildRecorderArgs(bin, tmp.Name(), sampleRate, channels)

	r.cmd = exec.Command(bin, args...)
	r.cmd.Stderr = nil // let stderr flow to parent

	slog.Debug("stt: starting recorder", "bin", bin, "args", args, "output", tmp.Name())

	if err := r.cmd.Start(); err != nil {
		os.Remove(tmp.Name())
		r.tmpFile = nil
		return fmt.Errorf("stt: start recorder: %w", err)
	}

	r.recording = true
	return nil
}

// Stop stops the recording subprocess and finalizes the temporary file.
// It sends SIGTERM, waits up to 5 seconds, then SIGKILL if needed.
func (r *Recorder) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording || r.cmd == nil || r.cmd.Process == nil {
		return nil
	}

	r.recording = false

	done := make(chan error, 1)
	go func() {
		done <- r.cmd.Wait()
	}()

	// Send SIGTERM for graceful stop.
	if runtime.GOOS == "windows" {
		// Windows: use taskkill for graceful termination.
		killCmd := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", r.cmd.Process.Pid), "/T", "/F")
		_ = killCmd.Run()
	} else {
		_ = r.cmd.Process.Signal(syscall.SIGTERM)
	}

	select {
	case err := <-done:
		if err != nil {
			slog.Debug("stt: recorder exited with error", "error", err)
		}
	case <-time.After(5 * time.Second):
		// Force kill if not exited after 5 seconds.
		slog.Debug("stt: recorder did not exit in time, sending SIGKILL")
		_ = r.cmd.Process.Kill()
		<-done
	}

	return nil
}

// Cleanup removes the temporary recording file.
func (r *Recorder) Cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.tmpFile != nil {
		name := r.tmpFile.Name()
		r.tmpFile = nil
		if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
			slog.Debug("stt: failed to remove temp file", "path", name, "error", err)
		}
	}
}

// FilePath returns the path to the temporary recording file.
// Returns empty string if no recording has been made.
func (r *Recorder) FilePath() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.tmpFile == nil {
		return ""
	}
	return r.tmpFile.Name()
}

// detectRecorderBinary finds an available recorder binary.
// Prefers ffmpeg, falls back to sox.
func (r *Recorder) detectRecorderBinary() (string, error) {
	preferred := r.config.RecorderBin
	if preferred != "" {
		if _, err := exec.LookPath(preferred); err == nil {
			return preferred, nil
		}
		slog.Debug("stt: configured recorder not found", "bin", preferred)
	}

	// Try ffmpeg first.
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		return "ffmpeg", nil
	}

	// Fall back to sox.
	if _, err := exec.LookPath("sox"); err == nil {
		return "sox", nil
	}

	return "", fmt.Errorf("stt: no recorder binary found (need ffmpeg or sox)")
}

// buildRecorderArgs constructs the command-line arguments for the recorder binary.
func (r *Recorder) buildRecorderArgs(bin, outputPath string, sampleRate, channels int) []string {
	switch bin {
	case "ffmpeg":
		return r.ffmpegArgs(outputPath, sampleRate, channels)
	case "sox":
		return r.soxArgs(outputPath, sampleRate, channels)
	default:
		// Unknown binary, try ffmpeg-style args.
		return r.ffmpegArgs(outputPath, sampleRate, channels)
	}
}

// ffmpegArgs returns arguments for ffmpeg recording.
// On macOS uses avfoundation, on Linux uses ALSA.
func (r *Recorder) ffmpegArgs(outputPath string, sampleRate, channels int) []string {
	var args []string

	switch runtime.GOOS {
	case "darwin":
		// macOS: use avfoundation input device ':0' (default microphone).
		args = []string{
			"-f", "avfoundation",
			"-i", ":0",
			"-ar", fmt.Sprintf("%d", sampleRate),
			"-ac", fmt.Sprintf("%d", channels),
			"-y",
			outputPath,
		}
	case "linux":
		// Linux: use ALSA default device.
		args = []string{
			"-f", "alsa",
			"-i", "default",
			"-ar", fmt.Sprintf("%d", sampleRate),
			"-ac", fmt.Sprintf("%d", channels),
			"-y",
			outputPath,
		}
	case "windows":
		// Windows: use dshow with default audio device.
		args = []string{
			"-f", "dshow",
			"-i", "audio=Default Audio Device",
			"-ar", fmt.Sprintf("%d", sampleRate),
			"-ac", fmt.Sprintf("%d", channels),
			"-y",
			outputPath,
		}
	default:
		// Generic fallback.
		args = []string{
			"-ar", fmt.Sprintf("%d", sampleRate),
			"-ac", fmt.Sprintf("%d", channels),
			"-y",
			outputPath,
		}
	}

	return args
}

// soxArgs returns arguments for sox recording.
func (r *Recorder) soxArgs(outputPath string, sampleRate, channels int) []string {
	return []string{
		"-d", // default audio device
		"-r", fmt.Sprintf("%d", sampleRate),
		"-c", fmt.Sprintf("%d", channels),
		outputPath,
	}
}
