package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/caimlas/meept/internal/voicechild"
)

// defaultPiperBin is the piper TTS binary name. piper is GPL and runs as a
// subprocess only - never vendored or linked (leaf 10 GPL rule).
const defaultPiperBin = "piper"

// piperStderrTailBytes caps how much of the subprocess stderr is carried
// into errors, so a chatty model cannot grow the error unbounded.
const piperStderrTailBytes = 200

var (
	// ErrPiperBinMissing is a constructor error: the piper binary is not
	// in PATH. It fails ONLY that call; the child and other calls survive.
	ErrPiperBinMissing = errors.New("voicechild/providers: piper binary not found (install piper-tts or set PATH)")
	// ErrPiperExit names every runtime piper failure: non-zero exit, kill
	// on context cancel, or a broken stdin write.
	ErrPiperExit = errors.New("voicechild/providers: piper subprocess failed")
	// ErrLocalModelRequired is a constructor error for local vendors:
	// ProviderConfig.Model must carry the model path (voice.local.stt_model
	// / voice.local.tts_model feed it). No invented defaults - a wrong
	// silent default would synthesize the wrong voice.
	ErrLocalModelRequired = errors.New("voicechild/providers: local vendor requires Model to carry the model path")
)

// PiperTTS synthesizes speech as s16le mono PCM at the negotiated rate by
// spawning the piper binary per utterance (voicechild.TTSProvider). The
// text rides stdin, audio streams from stdout through the returned reader
// - first samples reach the caller while piper is still running, never a
// whole-utterance buffer. piper's own sample rate (read from its sidecar
// <model>.json when present) is resampled to the negotiated rate with the
// package's streaming resampler.
type PiperTTS struct {
	cfg   ProviderConfig
	bin   string // resolved at construction (test seam: PATH)
	model int    // piper model sample rate; 0 = unknown (passthrough)
}

// newPiperTTS builds the local TTS adapter. No key is needed - this vendor
// never touches the network. Empty Model is ErrLocalModelRequired; a
// missing binary is ErrPiperBinMissing and fails only that call.
func newPiperTTS(cfg ProviderConfig) (voicechild.TTSProvider, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("%w: tts_model", ErrLocalModelRequired)
	}
	bin, err := exec.LookPath(defaultPiperBin)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrPiperBinMissing, defaultPiperBin, err)
	}
	return &PiperTTS{cfg: cfg, bin: bin, model: piperModelRate(cfg.Model)}, nil
}

// Speak starts one piper subprocess for text and returns its streaming
// stdout. Empty text returns an empty reader and starts no process.
// Subprocess death rides the context (exec.CommandContext); Close reaps.
func (p *PiperTTS) Speak(ctx context.Context, text string) (io.ReadCloser, error) {
	if text == "" {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	cmd := exec.CommandContext(ctx, p.bin, "--model", p.cfg.Model, "--output-raw")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("piper stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("piper stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start piper: %w", err)
	}
	st := &piperStream{cmd: cmd, src: stdout, stderr: &stderr, werr: make(chan error, 1)}
	// Feed text on a goroutine: a big text can fill the stdin pipe while
	// piper is still reading stdout, so a synchronous write could
	// deadlock. Once the process is gone the write ends (EPIPE) and the
	// buffered channel holds the error for Close/Read to inspect.
	go func() {
		_, werr := io.WriteString(stdin, text+"\n")
		if cerr := stdin.Close(); werr == nil {
			werr = cerr
		}
		st.werr <- werr
	}()
	var r io.ReadCloser = st
	if p.model > 0 && p.cfg.SampleRate > 0 && p.model != p.cfg.SampleRate {
		r = newResamplingReader(st, p.model, p.cfg.SampleRate)
	}
	return r, nil
}

// piperModelRate reads piper's sidecar config for audio.sample_rate. The
// canonical piper layout is <model>.json where <model> is the FULL model
// path, i.e. voice.onnx -> voice.onnx.json (docs/workflows/tts.md:113 and
// the piper -c convention at :207). An older layout stripped ".onnx" first
// (voice.json); it is kept as a fallback so existing deployments keep
// working. Missing or unreadable config yields 0, which means passthrough -
// the daemon's rate negotiation keeps the common case matched.
func piperModelRate(modelPath string) int {
	paths := []string{modelPath + ".json"}
	if base := strings.TrimSuffix(modelPath, ".onnx"); base != modelPath {
		paths = append(paths, base+".json")
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var conf struct {
			Audio struct {
				SampleRate int `json:"sample_rate"`
			} `json:"audio"`
		}
		if err := json.Unmarshal(data, &conf); err != nil {
			continue
		}
		if conf.Audio.SampleRate > 0 {
			return conf.Audio.SampleRate
		}
	}
	return 0
}

// piperStream is piper's stdout plus the reaper. cmd.Wait owns the pipe
// close (StdoutPipe contract), so Close never double-closes: finish runs
// Wait once, and the process exit status becomes the stream's sticky
// error - io.EOF on success. Read blocks naturally: no reader goroutine.
type piperStream struct {
	cmd    *exec.Cmd
	src    io.Reader
	stderr *bytes.Buffer
	werr   chan error // stdin feed result, delivered once
	waited bool
	sticky error
}

func (s *piperStream) Read(p []byte) (int, error) {
	if s.sticky != nil {
		return 0, s.sticky
	}
	n, err := s.src.Read(p)
	if err == io.EOF {
		s.finish()
		return n, s.sticky
	}
	return n, err
}

// Close reaps the process and surfaces its exit status. After piper exits
// the leftover audio is gone, so a caller dropping mid-stream (barge-in)
// just eats a Wait for the short remaining write burst - the speaking
// context, not Close, carries the real cancel.
func (s *piperStream) Close() error {
	s.finish()
	if s.sticky != nil && !errors.Is(s.sticky, io.EOF) {
		return s.sticky
	}
	return nil
}

// finish waits the process once and turns its outcome into the sticky
// error: nil-feed + zero exit = io.EOF; anything else wraps ErrPiperExit
// with the stderr tail.
func (s *piperStream) finish() {
	if s.waited {
		return
	}
	s.waited = true
	// Drain the feed result without blocking: the goroutine can only be
	// outstanding while the child is alive, and Wait below outlives it
	// either way (child death ends the write with EPIPE).
	select {
	case werr := <-s.werr:
		if werr != nil {
			s.stdinFailed(werr)
		}
	default:
	}
	if err := s.cmd.Wait(); err != nil {
		s.sticky = fmt.Errorf("%w: %w (stderr tail: %s)", ErrPiperExit, err, tailBytes(s.stderr.Bytes(), piperStderrTailBytes))
		return
	}
	if s.sticky == nil {
		s.sticky = io.EOF
	}
}

// stdinFailed records a broken text feed only while no worse error exists.
func (s *piperStream) stdinFailed(werr error) {
	if s.sticky == nil {
		s.sticky = fmt.Errorf("%w: writing text to piper stdin: %w", ErrPiperExit, werr)
	}
}

// tailBytes returns at most n bytes from the end of b, for error tails.
func tailBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[len(b)-n:])
}

// compile-time pins for the local adapters.
var (
	_ voicechild.TTSProvider = (*PiperTTS)(nil)
	_ voicechild.STTProvider = (*WhisperSTT)(nil)
)
