package providers

// Child-side Rumble STT adapter (leaf 01). Structurally a clone of the
// Whisper adapter (stt_whisper.go) with an HTTP endpoint in place of a
// subprocess: buffer one voiced PCM window, POST it as base64 WAV to a
// keyless vLLM chat.completions server, and emit exactly one Final
// transcript. No auth header is ever sent and no request body / base64
// audio is logged.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/voicechild"
)

const (
	// rumbleChatCompletionsPath is the model-card vLLM route appended to
	// cfg.BaseURL. The earlier /v1/audio/transcriptions POST was the bug
	// leaf 01 fixes (contract C3).
	rumbleChatCompletionsPath = "/v1/chat/completions"
	// rumbleDefaultModel is used when cfg.Model is empty.
	rumbleDefaultModel = "Reson8/Rumble-2-Flash"
	// rumbleHTTPTimeout bounds one transcription POST (the caller's ctx is
	// the whole call lifetime; a wedged request must not starve the stream).
	rumbleHTTPTimeout = 10 * time.Second
	// rumbleMaxWindowS caps one buffered utterance at N seconds of audio
	// regardless of silence so a never-silent source cannot grow unbounded.
	rumbleMaxWindowS = 8
)

// rumbleRequest is the model-card chat.completions wire body (C3). The WAV
// travels as a data URI in messages[0].content[0].audio_url.url; the
// chat_template_kwargs map is nil when no C2 selector was set, so its key
// is omitted entirely on the legacy path.
type rumbleRequest struct {
	Model              string          `json:"model"`
	Messages           []rumbleMessage `json:"messages"`
	Temperature        int             `json:"temperature"`
	ChatTemplateKwargs map[string]any  `json:"chat_template_kwargs,omitempty"`
}

type rumbleMessage struct {
	Role    string              `json:"role"`
	Content []rumbleContentPart `json:"content"`
}

type rumbleContentPart struct {
	Type     string            `json:"type"`
	AudioURL rumbleAudioURLObj `json:"audio_url"`
}

type rumbleAudioURLObj struct {
	URL string `json:"url"`
}

// ErrMissingRumbleBaseURL is a constructor error: a BaseURL is required
// because Rumble STT talks to a warm server over HTTP (no cloud default).
var ErrMissingRumbleBaseURL = errors.New("voicechild/providers: missing Rumble base URL for call")

// RumbleSTT implements voicechild.STTProvider by batching one utterance and
// POSTing it to a keyless vLLM chat.completions endpoint. It emits exactly
// one Final Transcript per window and NEVER a partial.
type RumbleSTT struct {
	cfg ProviderConfig

	client *http.Client

	// kickMu guards the per-session flush-kick channels registered by
	// Stream and retired by run. FlushNow force-closes every open window
	// immediately (barge-in seam, mirrors WhisperSTT). The signal is a
	// non-blocking send on a buffered(1) channel, so a second pending kick
	// coalesces into the first; no I/O is performed under kickMu.
	kickMu sync.Mutex
	kicks  map[chan struct{}]struct{}
}

var _ voicechild.STTProvider = (*RumbleSTT)(nil)

// newRumbleSTT builds the Rumble STT adapter. An empty BaseURL fails the
// call with ErrMissingRumbleBaseURL; SampleRate defaults to 16 kHz (the C2
// local-leg rate) when unset. Empty Model falls back to rumbleDefaultModel.
func newRumbleSTT(cfg ProviderConfig) (voicechild.STTProvider, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("%w: stt_base_url", ErrMissingRumbleBaseURL)
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 16000
	}
	if cfg.Model == "" {
		cfg.Model = rumbleDefaultModel
	}
	return &RumbleSTT{
		cfg:    cfg,
		client: &http.Client{Timeout: rumbleHTTPTimeout},
	}, nil
}

// FlushNow force-closes the open window of every live Stream session
// (early-kick for barge-in latency). With nothing buffered it is a no-op:
// the run loop's kick case checks len(buf) > 0 before posting.
func (s *RumbleSTT) FlushNow() {
	s.kickMu.Lock()
	for ch := range s.kicks {
		select {
		case ch <- struct{}{}:
		default: // a kick is already pending: coalesce
		}
	}
	s.kickMu.Unlock()
}

// registerKick adds one session's flush-kick channel and returns its retire
// closure (deferred by run).
func (s *RumbleSTT) registerKick() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.kickMu.Lock()
	if s.kicks == nil {
		s.kicks = make(map[chan struct{}]struct{})
	}
	s.kicks[ch] = struct{}{}
	s.kickMu.Unlock()
	return ch, func() {
		s.kickMu.Lock()
		delete(s.kicks, ch)
		s.kickMu.Unlock()
	}
}

// Stream implements voicechild.STTProvider. It returns a channel carrying
// one Final Transcript per recognized window; the channel closes when pcm
// closes (with a final flush), the context ends, or a kick fires. Context
// cancellation ends the stream.
func (s *RumbleSTT) Stream(ctx context.Context, pcm <-chan []byte) (<-chan voicechild.Transcript, error) {
	dir, err := os.MkdirTemp("", "rumble-stt-")
	if err != nil {
		return nil, fmt.Errorf("rumble temp dir: %w", err)
	}
	out := make(chan voicechild.Transcript, 8)
	kick, retire := s.registerKick()
	go s.run(ctx, pcm, out, dir, kick, retire)
	return out, nil
}

// run is the segmentation loop. It reuses the Whisper RMS gate so voiced /
// silence decisions stay byte-identical with the local path. A window
// flushes on ~500 ms of trailing silence, on the max-window cap, on an
// explicit FlushNow kick, or on pcm close. On pcm close ANY buffered bytes
// are posted — including all-zero chunks — so the last utterance is never
// lost.
func (s *RumbleSTT) run(ctx context.Context, pcm <-chan []byte, out chan<- voicechild.Transcript, dir string, kick <-chan struct{}, retire func()) {
	defer close(out)
	defer retire()
	defer func() {
		if rerr := os.RemoveAll(dir); rerr != nil {
			slog.Debug("rumble temp cleanup failed", "dir", dir, "error", rerr)
		}
	}()

	var (
		buf            []byte // accumulated s16le bytes of the open window
		silenceSamples int    // trailing silent samples in the open window
		voicedSamples  int    // samples above threshold in the window
		voicedEnd      int    // byte offset just past the last voiced chunk
		seq            int
		gate           rmsGate // per-stream odd-boundary carry for RMS math
	)
	maxWindow := rumbleMaxWindowS * s.cfg.SampleRate * 2
	silenceCutoff := s.cfg.SampleRate * whisperSilenceMs / 1000

	// deliver emits one Final transcript for the buffered window. A
	// nil/empty buffer is a strict no-op (no POST on an empty window).
	// force=true (channel close / kick / max-window) posts whatever is
	// buffered even if it contains no voiced samples; otherwise only a
	// voiced window is posted and trailing silence is trimmed off.
	deliver := func(force bool) bool {
		data := buf
		if !force {
			if voicedSamples == 0 || len(data) < 2 {
				buf, silenceSamples, voicedSamples, voicedEnd = nil, 0, 0, 0
				return true
			}
			if voicedEnd < len(data) {
				data = data[:voicedEnd]
			}
		}
		if len(data) < 2 {
			buf, silenceSamples, voicedSamples, voicedEnd = nil, 0, 0, 0
			return true
		}
		text, perr := s.transcribe(ctx, dir, seq, data)
		seq++
		buf, silenceSamples, voicedSamples, voicedEnd = nil, 0, 0, 0
		if perr != nil {
			// Fail-soft: a transport/parse failure is an empty Final,
			// never a failed call. Log vendor+model only, never the
			// body or base64 audio.
			slog.Debug("rumble transcription failed", "vendor", s.cfg.Vendor, "model", s.cfg.Model, "error", perr)
		}
		select {
		case out <- voicechild.Transcript{Text: text, Final: true}:
			return true
		case <-ctx.Done():
			return false
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-kick:
			// Explicit flush (barge-in). Only close an open window;
			// with nothing buffered this is a strict no-op.
			if len(buf) > 0 {
				if !deliver(true) {
					return
				}
			}
		case chunk, ok := <-pcm:
			if !ok {
				// Channel close MUST flush even an all-zero window.
				deliver(true)
				return
			}
			if len(chunk) == 0 {
				continue
			}
			buf = append(buf, chunk...)
			n, voicedNow := gate.energy(chunk)
			if voicedNow {
				voicedSamples += n
				silenceSamples = 0
				voicedEnd = len(buf)
			} else {
				silenceSamples += n
			}
			if silenceSamples >= silenceCutoff || len(buf) >= maxWindow {
				if !deliver(false) {
					return
				}
			}
		}
	}
}

// rumbleKwargs builds chat_template_kwargs from the explicitly set C2
// selectors. An empty map is returned as nil so encoding/json omits the
// key entirely (server defaults then bind). A language NAME such as
// "English" is passed verbatim — it is not a BCP-47 tag.
func (s *RumbleSTT) rumbleKwargs() map[string]any {
	kwargs := make(map[string]any)
	if s.cfg.ChatTemplateLanguage != "" {
		kwargs["language"] = s.cfg.ChatTemplateLanguage
	}
	if s.cfg.ChatTemplateDigits != nil {
		kwargs["digits"] = *s.cfg.ChatTemplateDigits
	}
	if s.cfg.ChatTemplateFiller != nil {
		kwargs["filler"] = *s.cfg.ChatTemplateFiller
	}
	if s.cfg.ChatTemplateVerbatim != nil {
		kwargs["verbatim"] = *s.cfg.ChatTemplateVerbatim
	}
	if len(kwargs) == 0 {
		return nil
	}
	return kwargs
}

// transcribe writes the window as a WAV, POSTs the model-card
// chat.completions body to the Rumble endpoint, and returns the trimmed
// transcript from choices[0].message.content. It never logs the request
// body, base64 audio, or response content — only vendor+model at DEBUG on
// failure (the caller handles that).
func (s *RumbleSTT) transcribe(ctx context.Context, dir string, seq int, buf []byte) (string, error) {
	tmp, err := os.CreateTemp(dir, fmt.Sprintf("utt%d-*.wav", seq))
	if err != nil {
		return "", fmt.Errorf("create temp wav: %w", err)
	}
	wavPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp wav: %w", err)
	}
	if err := writeWAV16(wavPath, buf, s.cfg.SampleRate); err != nil {
		return "", fmt.Errorf("write wav: %w", err)
	}
	wav, err := os.ReadFile(wavPath)
	if err != nil {
		return "", fmt.Errorf("read wav: %w", err)
	}

	payload := rumbleRequest{
		Model: s.cfg.Model,
		Messages: []rumbleMessage{{
			Role: "user",
			Content: []rumbleContentPart{{
				Type: "audio_url",
				AudioURL: rumbleAudioURLObj{
					URL: "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(wav),
				},
			}},
		}},
		Temperature:        0,
		ChatTemplateKwargs: s.rumbleKwargs(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(s.cfg.BaseURL, "/") + rumbleChatCompletionsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Deliberately NO Authorization header: Rumble STT is keyless.

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("post transcription: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("post transcription: status %d", resp.StatusCode)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("decode response: no choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}
