package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

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
)

// ttsRoundTripper issues the vendor HTTP request. A package-level var so
// the httptest fake swaps the transport (never the live vendor in CI).
var ttsTransport = http.DefaultTransport

// openAITTSRequest is the /audio/speech request body. Every request is
// streaming (stream=true, response_format=pcm): the adapter never issues
// a buffered synthesis call, and streaming pcm does not support the
// speed parameter - the speed field is therefore omitted entirely.
type openAITTSRequest struct {
	Model          string `json:"model"`
	Voice          string `json:"voice"`
	Input          string `json:"input"`
	ResponseFormat string `json:"response_format"`
	Stream         bool   `json:"stream"`
}

// OpenAITTS synthesizes speech for one text unit and streams s16le mono
// PCM at the negotiated rate (voicechild.TTSProvider). The vendor's fixed
// 24 kHz output is resampled inside the adapter; audio is consumed
// incrementally from the HTTP body, so the first samples reach the caller
// as soon as the vendor sends them - never a full-buffer-then-play.
//
// Despite the historical name, the type is the dispatch core for every
// OpenAI-compatible TTS vendor (cloud openai, local qwen3); the vendor
// differences live in NewTTS (auth header presence, endpoint defaults).
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
	return &OpenAITTS{cfg: cfg, apiKey: key, transport: ttsTransport}, nil
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
// "http://h:port/" join to the same route.
func (t *OpenAITTS) speechURL() string {
	return strings.TrimSuffix(t.cfg.BaseURL, "/") + openAISpeechRoute
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
	body, err := json.Marshal(openAITTSRequest{
		Model:          t.cfg.Model,
		Voice:          t.cfg.Voice,
		Input:          text,
		ResponseFormat: "pcm",
		Stream:         true,
	})
	if err != nil {
		return nil, fmt.Errorf("openai tts: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.speechURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai tts: build request: %w", err)
	}
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Transport: t.transport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai tts: request: %w", err)
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
	if t.cfg.SampleRate > 0 && t.cfg.SampleRate != ttsSampleRate {
		return newResamplingReader(src, ttsSampleRate, t.cfg.SampleRate), nil
	}
	return src, nil
}
