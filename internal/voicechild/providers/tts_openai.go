package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/voicechild"
)

// ttsSampleRate is the rate of OpenAI's "pcm" response format (24 kHz
// s16le mono). Output is resampled to the negotiated rate inside the
// adapter via jargo's pure-Go resampler.
const ttsSampleRate = 24000

// openAIDefaultBaseURL is the cloud TTS API base used when
// ProviderConfig.BaseURL is empty; the route below is appended to it
// (or to the caller-supplied base). Moved from a full-URL const (leaf 08)
// to base+path so a keys frame can point the adapter at an
// OpenAI-compatible endpoint (C1/D3, e.g. local qwen3).
const (
	openAIDefaultBaseURL = "https://api.openai.com"
	openAISpeechRoute    = "/v1/audio/speech"
	// qwen3PCMRoute is the token-level streaming route exposed by the
	// local qwen3 server (faster-qwen3-tts tier 1). Unlike
	// openAISpeechRoute — which buffers the whole utterance before its
	// body starts — this route emits progressive codec-frame PCM, so
	// the first audio reaches the caller in milliseconds (measured
	// header_ttfb ~7 ms, first audio ~0.37 s vs ~2 s, socrates:8001).
	// Its response is a length-prefixed frame stream (see pcmstream.go).
	qwen3PCMRoute = "/v1/audio/speech/pcm-stream"
	// qwen3PCMChunkSize is the codec frames per yield requested from
	// the pcm-stream route. 4 is the server default and the measured
	// low-latency sweet spot on socrates (chunk_size=1/2 were noisier).
	qwen3PCMChunkSize = 4
)

// ttsRoundTripper issues the vendor HTTP request. A package-level var so
// the httptest fake swaps the transport (never the live vendor in CI).
//
// Connection hygiene (leaf 04 / link-72a04aebac963cdb): the local
// OpenAI-compatible TTS servers (uvicorn faster-qwen3-tts) drop idle
// keep-alives after ~5 s, and the provider is constructed at call setup —
// the greeting POST can land on a stale pooled connection a second later
// and die with "http: server closed idle connection" (silently dead
// greeting). Every utterance is exactly one POST and a drip stream gains
// nothing from pooling, so keep-alives are disabled outright: each Speak
// dials fresh. The timeout set mirrors http.DefaultTransport's sane
// defaults (no IdleConnTimeout: with keep-alives off there are no idle
// connections to time out).
var ttsTransport http.RoundTripper = &http.Transport{
	DisableKeepAlives: true,
	Proxy:             http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ForceAttemptHTTP2:     true,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	ResponseHeaderTimeout: 30 * time.Second,
}

// ttsCloudIdleConnTimeout bounds how long an idle keep-alive connection is
// pooled before the transport drops it. The cloud OpenAI endpoint keeps
// keep-alives open well beyond the ~5 s the local uvicorn server tolerates,
// so pooling is safe here; the bound prevents leaking idle sockets for the
// lifetime of the child process (F6).
const ttsCloudIdleConnTimeout = 90 * time.Second

// ttsCloudTransport is the cloud-OpenAI transport (F6). Unlike the local
// qwen3 transport above, it enables keep-alives so consecutive Speak calls
// reuse the TCP+TLS connection instead of paying a fresh handshake per
// sentence. It mirrors ttsTransport's sane timeouts + env proxy and adds a
// bounded IdleConnTimeout. A package-level var so the httptest fake can
// swap it (same seam as ttsTransport).
var ttsCloudTransport http.RoundTripper = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ForceAttemptHTTP2:     true,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	ResponseHeaderTimeout: 30 * time.Second,
	IdleConnTimeout:       ttsCloudIdleConnTimeout,
}

// ttsMaxSpeakAttempts bounds the transport-error retry: one fresh-dial
// retry, never more (leaf 04).
const ttsMaxSpeakAttempts = 2

// WithCallID stamps the call id via the voicechild-owned ctx key
// (providers imports voicechild, never the reverse).
func WithCallID(ctx context.Context, callID string) context.Context {
	return voicechild.WithTTSContext(ctx, callID)
}

// callIDFromContext reads the voicechild-stamped call id (leaf 04:
// voicechild owns the ctx key - providers imports voicechild, never the
// reverse). Empty when the call site did not stamp.
func callIDFromContext(ctx context.Context) string {
	return voicechild.TTSCallIDFromContext(ctx)
}

// isTTSTransportError classifies narrow: only dial/keep-alive-class
// transport failures are worth a fresh-dial retry. HTTP status failures
// never reach here (they return after the response is received); a
// cancelled/deadline-exceeded context is never retried.
func isTTSTransportError(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return true
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "idle connection") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "EOF")
}

// openAITTSRequest is the /audio/speech request body. Every request is
// streaming (stream=true, response_format=pcm): the adapter never issues
// a buffered synthesis call, and streaming pcm does not support the
// speed parameter - the speed field is therefore omitted entirely.
// This shape is for the cloud "openai" vendor only.
type openAITTSRequest struct {
	Model          string `json:"model"`
	Voice          string `json:"voice"`
	Input          string `json:"input"`
	ResponseFormat string `json:"response_format"`
	Stream         bool   `json:"stream"`
}

// qwen3PCMStreamRequest is the request body for the local qwen3
// /v1/audio/speech/pcm-stream route. That route's own schema names the
// fields text/voice/chunk_size — NOT the OpenAI input/stream/
// response_format fields. No model field is sent: the server synthesizes
// with its loaded default (an empty cfg.Model is the "server default"
// sentinel, and a bogus model value is ignored by the route).
type qwen3PCMStreamRequest struct {
	Text      string `json:"text"`
	Voice     string `json:"voice"`
	ChunkSize int    `json:"chunk_size"`
}

// OpenAITTS synthesizes speech for one text unit and streams s16le mono
// PCM at the negotiated rate (voicechild.TTSProvider). The vendor's fixed
// 24 kHz output is resampled inside the adapter; audio is consumed
// incrementally from the HTTP body, so the first samples reach the caller
// as soon as the vendor sends them - never a full-buffer-then-play.
//
// Despite the historical name, the type is the dispatch core for every
// OpenAI-compatible TTS vendor (cloud openai, local qwen3); the vendor
// differences live in NewTTS (auth header presence, endpoint defaults)
// and in Speak's request/response shape (qwen3 uses the token-level
// pcm-stream route, cloud openai the buffered /audio/speech route).
type OpenAITTS struct {
	cfg    ProviderConfig
	apiKey string // cloud vendors only; qwen3 constructs with "" (D2)

	// transport overrides the HTTP round tripper (test seam).
	transport http.RoundTripper
}

// NewTTS dispatches on cfg.Vendor (C1 keys frame -> C2 config): "openai"
// (or the historical empty default) builds the cloud adapter (key
// required), "qwen3" the keyless local adapter (cfg.BaseURL required).
// An unknown vendor is ErrUnknownVendor. Construction logs vendor+model
// only, never the key.
func NewTTS(cfg ProviderConfig, key string) (voicechild.TTSProvider, error) {
	switch cfg.Vendor {
	case "", TTSVendorOpenAI:
		return newOpenAITTS(cfg, key)
	case TTSVendorQwen3:
		return newQwen3TTS(cfg, key)
	case "piper":
		return newPiperTTS(cfg)
	default:
		return nil, fmt.Errorf("%w: tts vendor %q", ErrUnknownVendor, cfg.Vendor)
	}
}

// newOpenAITTS is the cloud-vendor construction path: cfg.BaseURL empty
// falls back to api.openai.com, cfg.Voice empty to the vendor default;
// a missing key is ErrMissingTTSKey (D2: a missing key fails the call,
// not the child).
func newOpenAITTS(cfg ProviderConfig, key string) (voicechild.TTSProvider, error) {
	if key == "" {
		return nil, ErrMissingTTSKey
	}
	if cfg.Model == "" {
		cfg.Model = ttsVendorDefaultModel
	}
	if cfg.Voice == "" {
		cfg.Voice = ttsDefaultVoice
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = openAIDefaultBaseURL
	}
	return &OpenAITTS{cfg: cfg, apiKey: key, transport: ttsCloudTransport}, nil
}

// newQwen3TTS is the keyless local-vendor construction path (D2): it
// constructs and speaks with key "". BaseURL is REQUIRED - an empty one
// is ErrMissingTTSBaseURL (a local server has no meaningful default).
func newQwen3TTS(cfg ProviderConfig, _ string) (voicechild.TTSProvider, error) {
	if cfg.BaseURL == "" {
		return nil, ErrMissingTTSBaseURL
	}
	if cfg.Voice == "" {
		cfg.Voice = ttsDefaultVoice
	}
	// cfg.Model stays as sent: "" means the server's loaded default
	// (leaf 04 contract); no cloud fallback model applies here.
	return &OpenAITTS{cfg: cfg, apiKey: "", transport: ttsTransport}, nil
}

// setTTSTransport replaces the HTTP transport (test seam for the httptest
// fake vendor).
func (t *OpenAITTS) setTTSTransport(rt http.RoundTripper) {
	t.transport = rt
}

// speechURL joins the per-instance base with the speech route. A base
// with a trailing slash is trimmed so both "http://h:port" and
// "http://h:port/" join to the same route. The qwen3 vendor uses the
// token-level pcm-stream route; every other vendor keeps the cloud
// /v1/audio/speech route byte-identically.
func (t *OpenAITTS) speechURL() string {
	route := openAISpeechRoute
	if t.cfg.Vendor == TTSVendorQwen3 {
		route = qwen3PCMRoute
	}
	return strings.TrimSuffix(t.cfg.BaseURL, "/") + route
}

// usesPCMStream reports whether this provider speaks the token-level
// qwen3 route (different request body and length-prefixed response).
func (t *OpenAITTS) usesPCMStream() bool {
	return t.cfg.Vendor == TTSVendorQwen3
}

// Speak implements voicechild.TTSProvider: it posts the synthesis request
// and returns a reader that yields target-rate s16le mono PCM.
func (t *OpenAITTS) Speak(ctx context.Context, text string) (io.ReadCloser, error) {
	if t.apiKey == "" && t.cfg.Vendor != TTSVendorQwen3 {
		return nil, ErrMissingTTSKey
	}
	if text == "" {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	// Build the vendor-specific body. qwen3's pcm-stream route uses its
	// own field names and has no stream/response_format/model fields;
	// cloud openai keeps the OpenAI /audio/speech shape byte-identically.
	var body []byte
	var err error
	if t.usesPCMStream() {
		body, err = json.Marshal(qwen3PCMStreamRequest{
			Text:      text,
			Voice:     t.cfg.Voice,
			ChunkSize: qwen3PCMChunkSize,
		})
	} else {
		body, err = json.Marshal(openAITTSRequest{
			Model:          t.cfg.Model,
			Voice:          t.cfg.Voice,
			Input:          text,
			ResponseFormat: "pcm",
			Stream:         true,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("openai tts: encode request: %w", err)
	}
	client := &http.Client{Transport: t.transport}
	var resp *http.Response
	for attempt := 1; ; attempt++ {
		// Rebuild the request each attempt: the marshalled body bytes
		// are stable (fresh bytes.Reader), so a retry never replays a
		// half-consumed body.
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.speechURL(), bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("openai tts: build request: %w", err)
		}
		if t.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+t.apiKey)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err = client.Do(req)
		if err == nil {
			break
		}
		// One narrow retry (leaf 04): a stale-keep-alive/dial-class
		// transport failure gets exactly one fresh attempt. Never retry
		// on HTTP status (those errors return below, after a response
		// was received) and never on a cancelled context
		// (isTTSTransportError checks ctx.Err()).
		if attempt >= ttsMaxSpeakAttempts || !isTTSTransportError(ctx, err) {
			return nil, fmt.Errorf("openai tts: request: %w", err)
		}
		slog.Warn("openai tts: retrying failed speech request",
			"err", err, "call_id", callIDFromContext(ctx), "attempt", attempt+1)
	}
	if resp.StatusCode != http.StatusOK {
		if closeErr := resp.Body.Close(); closeErr != nil {
			slog.Debug("openai tts: body close", "err", closeErr)
		}
		return nil, fmt.Errorf("openai tts: unexpected status %d", resp.StatusCode)
	}

	// Stream the body incrementally through the resampler: the reader
	// below pulls fixed-size chunks and converts each as it arrives, so
	// first audio leaves this function's control as soon as the vendor
	// emits it.
	src := resp.Body
	if t.usesPCMStream() {
		// The pcm-stream route wraps every codec frame in a BE uint32
		// length header; the shared resampler expects raw s16le mono
		// samples. Strip the envelope first, without buffering frames.
		src = newPCMStreamFrameReader(resp.Body)
	}
	if t.cfg.SampleRate > 0 && t.cfg.SampleRate != ttsSampleRate {
		return newResamplingReader(src, ttsSampleRate, t.cfg.SampleRate), nil
	}
	return src, nil
}
