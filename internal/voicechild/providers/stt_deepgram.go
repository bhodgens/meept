package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/voicechild"
	"github.com/coder/websocket"
	"github.com/gojargo/jargo/service/wsutil"
)

// closeQuiet closes a vendor session whose primary outcome already failed
// or finished; a close error on that path adds nothing actionable, so it is
// logged at Debug and never masks the real result.
func closeQuiet(s wsSession) {
	if err := s.Close(); err != nil {
		slog.Debug("deepgram stt: session close", "err", err)
	}
}

// deepgramBaseURL is the Deepgram API host; tests override it per adapter
// with setSTTDialer against an httptest WS fake (never the live vendor in
// CI).
const deepgramBaseURL = "api.deepgram.com"

// Deepgram STT wire messages (live transcription). Only Results carries a
// transcript; the rest are skipped by name.
const (
	dgMsgResults       = "Results"
	dgMsgMetadata      = "Metadata"
	dgMsgSpeechStarted = "SpeechStarted"
	dgMsgUtteranceEnd  = "UtteranceEnd"
)

// dgMessage is the subset of Deepgram's live transcription result used here.
type dgMessage struct {
	Type    string `json:"type"`
	Channel struct {
		Alternatives []struct {
			Transcript string `json:"transcript"`
		} `json:"alternatives"`
	} `json:"channel"`
	IsFinal      bool `json:"is_final"`
	FromFinalize bool `json:"from_finalize"`
}

// sttDialer opens the vendor WebSocket session. A package-level var so the
// httptest fake swaps the dial target without touching the live vendor.
var sttDialer = dialDeepgram

// dialDeepgram opens the live transcription WebSocket at path with the
// Authorization header set. The bounded-close wrapper is adapted to this
// package's simpler wsSession interface.
func dialDeepgram(ctx context.Context, path string, header wsHeader) (wsSession, error) {
	conn, err := wsutil.Dial(ctx, "wss://"+deepgramBaseURL+path, header.http(), wsutil.DefaultReadLimit)
	if err != nil {
		return nil, err
	}
	return &wsutilSession{Conn: conn}, nil
}

// wsutilSession adapts jargo's wsutil.Conn (whose Close takes a status and
// reason and whose Read returns websocket.MessageType) to wsSession's
// plain shapes.
type wsutilSession struct{ *wsutil.Conn }

func (s *wsutilSession) Write(ctx context.Context, messageType int, data []byte) error {
	return s.Conn.Write(ctx, websocket.MessageType(messageType), data)
}

func (s *wsutilSession) Read(ctx context.Context) (int, []byte, error) {
	mt, data, err := s.Conn.Read(ctx)
	return int(mt), data, err
}

func (s *wsutilSession) Close() error {
	return s.Conn.Close(1000, "done")
}

// wsHeader is the header set shipped to the dialer (kept as a plain map so
// tests never need the http package beyond setting one key).
type wsHeader map[string]string

// http renders h as an http.Header (single-valued keys).
func (h wsHeader) http() map[string][]string {
	out := make(map[string][]string, len(h))
	for k, v := range h {
		out[k] = []string{v}
	}
	return out
}

// wsSession is the half of wsutil.Conn the adapter uses; a fake implements
// it over an httptest WS server.
type wsSession interface {
	// Write sends one message. messageType follows the websocket spec
	// (1 text, 2 binary).
	Write(ctx context.Context, messageType int, data []byte) error
	// Read blocks for the next message.
	Read(ctx context.Context) (messageType int, data []byte, err error)
	// Close ends the session.
	Close() error
}

// DeepgramSTT streams PCM to Deepgram's live transcription WebSocket and
// surfaces interim+final transcripts (voicechild.STTProvider). Constructed
// per call from the C1 keys frame; the key lives in memory only.
//
// Reconnect policy (leaf contract): exactly ONE redial within a call when
// the vendor WS drops; a second failure ends the stream as an error. No
// infinite loop.
type DeepgramSTT struct {
	cfg    ProviderConfig
	apiKey string

	// dial overrides the session dialer (test seam).
	dial func(ctx context.Context, path string, header wsHeader) (wsSession, error)
}

// NewSTT builds a Deepgram streaming STT adapter from the call's key
// (C1 keys frame). A missing key is ErrMissingSTTKey; the child and other
// calls are unaffected. Construction logs vendor+model only, never the key.
func NewSTT(cfg ProviderConfig, key string) (voicechild.STTProvider, error) {
	if cfg.Vendor != "deepgram" {
		return nil, fmt.Errorf("%w: stt vendor %q", ErrUnknownVendor, cfg.Vendor)
	}
	if key == "" {
		return nil, ErrMissingSTTKey
	}
	if cfg.Model == "" {
		cfg.Model = sttVendorDefaultModel
	}
	return &DeepgramSTT{cfg: cfg, apiKey: key, dial: sttDialer}, nil
}

// setSTTDialer replaces the session dialer (test seam for the httptest
// fake vendor).
func (s *DeepgramSTT) setSTTDialer(dial func(ctx context.Context, path string, header wsHeader) (wsSession, error)) {
	s.dial = dial
}

// Stream implements voicechild.STTProvider. It opens the session, pumps
// PCM chunks in, and returns a channel carrying one Transcript per Deepgram
// result until the context ends, the session drops twice, or the caller
// drains the channel.
func (s *DeepgramSTT) Stream(ctx context.Context, pcm <-chan []byte) (<-chan voicechild.Transcript, error) {
	if s.apiKey == "" {
		return nil, ErrMissingSTTKey
	}
	out := make(chan voicechild.Transcript)
	go s.run(ctx, pcm, out)
	return out, nil
}

// run is Stream's body: dial (with the single in-call retry), pump audio,
// decode results, close out on terminal failure or context end.
func (s *DeepgramSTT) run(ctx context.Context, pcm <-chan []byte, out chan<- voicechild.Transcript) {
	defer close(out)

	sess, err := s.dialSession(ctx)
	if err != nil {
		return // one dial refusal (bad key) is permanent; no retry loop
	}
	pumpErr := s.pumpAndRead(ctx, sess, pcm, out)
	if pumpErr == nil || ctx.Err() != nil {
		closeQuiet(sess)
		return
	}

	// Session dropped mid-call: exactly one retry, then the stream ends
	// (callers see the closed channel; the child stays up). Audio written
	// into the dropped session's TCP buffer before the drop was detected
	// is unrecoverable - the same trade jargo's wsservice documents ("a
	// dropped session costs the audio spoken during the gap"); speech
	// after the reconnect transcribes normally.
	closeQuiet(sess)
	sess, err = s.dialSession(ctx)
	if err != nil {
		return
	}
	if pumpErr := s.pumpAndRead(ctx, sess, pcm, out); pumpErr != nil {
		slog.Debug("deepgram stt: retry pump ended", "err", pumpErr)
	}
	closeQuiet(sess)
}

// dialSession opens one vendor session, applying the single-retry policy at
// the Stream level rather than here.
func (s *DeepgramSTT) dialSession(ctx context.Context) (wsSession, error) {
	header := wsHeader{"Authorization": "Token " + s.apiKey}
	sess, err := s.dial(ctx, s.listenPath(), header)
	if err != nil {
		return nil, fmt.Errorf("deepgram stt: dial: %w", err)
	}
	return sess, nil
}

// listenPath is the live-transcription route with query parameters. Empty
// config values fall back to Deepgram defaults (nova-3, linear16, 1ch,
// interim results on) so the wire request is explicit about the rate the
// setup frame negotiated.
func (s *DeepgramSTT) listenPath() string {
	q := []string{
		"model=" + s.cfg.Model,
		"encoding=linear16",
		"channels=1",
		"interim_results=true",
	}
	if s.cfg.SampleRate > 0 {
		q = append(q, fmt.Sprintf("sample_rate=%d", s.cfg.SampleRate))
	}
	if s.cfg.Language != "" {
		q = append(q, "language="+s.cfg.Language)
	}
	return "/v1/listen?" + strings.Join(q, "&")
}

// pumpAndRead drives one session: a pump goroutine forwards PCM chunks to
// the vendor while the calling goroutine reads results. Separating them
// keeps audio flowing regardless of how fast transcripts come back (and
// vice versa); both stop when the session drops or the context ends.
func (s *DeepgramSTT) pumpAndRead(ctx context.Context, sess wsSession, pcm <-chan []byte, out chan<- voicechild.Transcript) error {
	readCtx, cancelRead := context.WithCancel(ctx)
	var pumpWG sync.WaitGroup
	// Join the pump before returning so the next session's pump (after a
	// reconnect) is the only consumer of the pcm channel.
	defer pumpWG.Wait()
	defer cancelRead()

	// Pump: forward caller audio into the session until the context ends,
	// the chunk stream closes (Finalize flush), or the reader below tears
	// the session down after a drop.
	pumpWG.Add(1)
	go func() {
		defer pumpWG.Done()
		for {
			select {
			case chunk, ok := <-pcm:
				if !ok {
					// Caller finished the utterance stream; ask Deepgram
					// to flush, then hold the session open so the final
					// can arrive.
					if ferr := sess.Write(readCtx, 1, []byte(`{"type":"Finalize"}`)); ferr != nil {
						return
					}
					continue
				}
				if len(chunk) > 0 {
					if werr := sess.Write(readCtx, 2, chunk); werr != nil {
						return
					}
				}
			case <-readCtx.Done():
				return
			}
		}
	}()

	// Read: decode vendor messages until the session fails or the context
	// ends. An error here drops the session, which unblocks the pump's
	// next Write and ends it via readCtx.
	for {
		mt, data, err := sess.Read(readCtx)
		if err != nil {
			cancelRead() // release the pump now; deferred calls finish the join
			return err
		}
		if mt == 1 {
			s.emit(ctx, data, out)
		}
	}
}

// emit decodes one vendor text message and forwards Results transcripts.
func (s *DeepgramSTT) emit(ctx context.Context, data []byte, out chan<- voicechild.Transcript) {
	var m dgMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return // a non-JSON frame is skipped, not fatal
	}
	if m.Type != dgMsgResults || len(m.Channel.Alternatives) == 0 {
		return // Metadata/SpeechStarted/UtteranceEnd carry no transcript
	}
	t := voicechild.Transcript{
		Text:  strings.TrimSpace(m.Channel.Alternatives[0].Transcript),
		Final: m.IsFinal || m.FromFinalize,
	}
	if t.Text == "" {
		return
	}
	select {
	case out <- t:
	case <-ctx.Done():
	}
}
