package providers

// Child-side Rumble STT adapter. Native streaming: one WebSocket per call to
// the keyless /v1/audio/transcriptions/stream endpoint. Raw s16le mono 16 kHz
// PCM is forwarded as binary frames (unchanged), a flush is one empty binary
// frame, and the server's partial/final/end JSON text events are mapped to
// voicechild.Transcript values. No Authorization header, no base64, no WAV,
// no client-side pseudo-streaming.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"unicode"

	"github.com/caimlas/meept/internal/voicechild"
	"github.com/gojargo/jargo/service/wsutil"
)

const (
	// rumbleNativeStreamPath is the native streaming route appended to
	// cfg.BaseURL. Distinct name from the gate test file's rumbleStreamPath
	// const to avoid a package-level redeclaration collision.
	rumbleNativeStreamPath = "/v1/audio/transcriptions/stream"
	// rumbleDefaultModel is kept for config compatibility; it is not sent
	// on the wire by the native streaming endpoint.
	rumbleDefaultModel = "Reson8/Rumble-2-Flash"
)

// ErrMissingRumbleBaseURL is a constructor error: a BaseURL is required
// because Rumble STT talks to a locally-hosted server (no cloud default).
var ErrMissingRumbleBaseURL = errors.New("voicechild/providers: missing Rumble base URL for call")

// ErrRumbleDial is returned when the native streaming WebSocket cannot be
// opened for a call.
var ErrRumbleDial = errors.New("voicechild/providers: rumble stt dial failed")

// rumbleDialer opens the keyless native-streaming WebSocket. A package var so
// a test can swap it; production uses wsutil.Dial over the shared wsSession
// seam (http->ws / ws passthrough via dialURL).
var rumbleDialer = dialRumble

func dialRumble(ctx context.Context, base, path string, header wsHeader) (wsSession, error) {
	conn, err := wsutil.Dial(ctx, dialURL(base, path), header.http(), wsutil.DefaultReadLimit)
	if err != nil {
		return nil, err
	}
	return &wsutilSession{Conn: conn}, nil
}

// RumbleSTT implements voicechild.STTProvider via a native streaming WS and
// additionally exposes FlushNow() (the barge-in seam).
type RumbleSTT struct {
	cfg  ProviderConfig
	dial func(context.Context, string, string, wsHeader) (wsSession, error)

	// mu guards flushes only. FlushNow performs non-blocking sends under
	// it; no network I/O ever happens under this mutex.
	mu      sync.Mutex
	flushes map[chan struct{}]struct{}
}

var _ voicechild.STTProvider = (*RumbleSTT)(nil)
var _ interface{ FlushNow() } = (*RumbleSTT)(nil)

// newRumbleSTT builds the native streaming adapter. Empty BaseURL fails with
// ErrMissingRumbleBaseURL; SampleRate defaults to 16 kHz; empty Model falls
// back to rumbleDefaultModel (config compatibility, unused on the wire).
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
	return &RumbleSTT{cfg: cfg, dial: rumbleDialer}, nil
}

// FlushNow sends one coalesced kick to every live stream's pump. Each kick
// makes the pump emit exactly one empty binary flush frame. Non-blocking:
// a second kick while one is pending is dropped.
func (s *RumbleSTT) FlushNow() {
	s.mu.Lock()
	for ch := range s.flushes {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	s.mu.Unlock()
}

func (s *RumbleSTT) registerFlush(ch chan struct{}) {
	s.mu.Lock()
	if s.flushes == nil {
		s.flushes = make(map[chan struct{}]struct{})
	}
	s.flushes[ch] = struct{}{}
	s.mu.Unlock()
}

func (s *RumbleSTT) unregisterFlush(ch chan struct{}) {
	s.mu.Lock()
	delete(s.flushes, ch)
	s.mu.Unlock()
}

// Stream opens one native streaming WebSocket and returns a buffered channel
// carrying partial/final transcripts until the server's end event, a read
// error, or context cancellation. Dial failures are returned to the caller;
// mid-stream failures are fail-soft (the channel simply closes).
func (s *RumbleSTT) Stream(ctx context.Context, pcm <-chan []byte) (<-chan voicechild.Transcript, error) {
	sess, err := s.dial(ctx, s.cfg.BaseURL, rumbleNativeStreamPath, wsHeader{})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRumbleDial, err)
	}
	out := make(chan voicechild.Transcript, 16)
	go s.run(ctx, sess, pcm, out)
	return out, nil
}

// run owns the stream lifetime. The reader loop (this goroutine) is the sole
// owner of out's close; the pump goroutine is the sole WS writer.
func (s *RumbleSTT) run(ctx context.Context, sess wsSession, pcm <-chan []byte, out chan<- voicechild.Transcript) {
	defer close(out) // single owner

	readCtx, cancelRead := context.WithCancel(ctx)
	defer closeQuiet(sess) // release the WS on every exit path
	defer cancelRead()

	flushCh := make(chan struct{}, 1)
	s.registerFlush(flushCh)
	defer s.unregisterFlush(flushCh)

	// Sole writer. Forwards PCM chunks unchanged as binary frames and emits
	// one empty binary flush frame on pcm close or a FlushNow kick.
	go func() {
		for {
			select {
			case chunk, ok := <-pcm:
				if !ok {
					if werr := sess.Write(readCtx, 2, nil); werr != nil {
						slog.Warn("rumble flush write failed", "error", werr)
					}
					pcm = nil // disable this case; wait for end/cancel
					continue
				}
				if len(chunk) > 0 {
					if werr := sess.Write(readCtx, 2, chunk); werr != nil {
						return
					}
				}
			case <-flushCh:
				if werr := sess.Write(readCtx, 2, nil); werr != nil {
					slog.Warn("rumble kick flush write failed", "error", werr)
				} // one coalesced flush frame
			case <-readCtx.Done():
				return
			}
		}
	}()

	// Reader. Maps text events to transcripts; `end` is terminal.
	var finals []string
	for {
		mt, data, err := sess.Read(readCtx)
		if err != nil {
			return // fail-soft: no final, out closes via defer
		}
		if mt != 1 { // only text frames carry events
			continue
		}
		var ev struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(data, &ev) != nil {
			continue
		}
		text := strings.TrimSpace(ev.Text)
		switch ev.Type {
		case "partial":
			if text == "" {
				continue
			}
			select {
			case out <- voicechild.Transcript{Text: text, Final: false}:
			case <-ctx.Done():
				return
			}
		case "final":
			if text == "" {
				continue
			}
			finals = append(finals, text)
			select {
			case out <- voicechild.Transcript{Text: text, Final: true}:
			case <-ctx.Done():
				return
			}
		case "end":
			if rem := rumbleUncoveredRemainder(text, finals); rem != "" {
				select {
				case out <- voicechild.Transcript{Text: rem, Final: true}:
				case <-ctx.Done():
					cancelRead()
					return
				}
			}
			cancelRead() // stop the pump
			return       // terminal: out closes via defer
		}
	}
}

// rumbleUncoveredRemainder returns the part of the server's full `end`
// transcript not already covered by previously emitted finals. Matching
// ignores whitespace so "Yesterday it was"+"30 degrees" covers
// "Yesterday it was 30 degrees" and does not emit a duplicate final.
func rumbleUncoveredRemainder(full string, finals []string) string {
	if len(finals) == 0 {
		return full
	}
	covered := []rune(stripRumbleWS(strings.Join(finals, "")))
	ci := 0
	for i, r := range full {
		if unicode.IsSpace(r) {
			continue
		}
		if ci < len(covered) && r == covered[ci] {
			ci++
			continue
		}
		return strings.TrimSpace(full[i:])
	}
	return "" // full text consumed by the covered prefix
}

func stripRumbleWS(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}
