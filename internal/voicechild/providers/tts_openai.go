package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/caimlas/meept/internal/voicechild"
)

// ttsSampleRate is the rate of OpenAI's "pcm" response format (24 kHz
// s16le mono). Output is resampled to the negotiated rate inside the
// adapter via jargo's pure-Go resampler.
const ttsSampleRate = 24000

// openAISpeechPath is the TTS route appended to the API base.
const openAISpeechPath = "https://api.openai.com/v1/audio/speech"

// ttsRoundTripper issues the vendor HTTP request. A package-level var so
// the httptest fake swaps the transport (never the live vendor in CI).
var ttsTransport = http.DefaultTransport

// openAITTSRequest is the /audio/speech request body.
type openAITTSRequest struct {
	Model          string  `json:"model"`
	Voice          string  `json:"voice"`
	Input          string  `json:"input"`
	ResponseFormat string  `json:"response_format"`
	Speed          float64 `json:"speed,omitempty"`
}

// OpenAITTS synthesizes speech for one text unit and streams s16le mono
// PCM at the negotiated rate (voicechild.TTSProvider). The vendor's fixed
// 24 kHz output is resampled inside the adapter; audio is consumed
// incrementally from the HTTP body, so the first samples reach the caller
// as soon as the vendor sends them - never a full-buffer-then-play.
type OpenAITTS struct {
	cfg    ProviderConfig
	apiKey string

	// transport overrides the HTTP round tripper (test seam).
	transport http.RoundTripper
}

// NewTTS builds an OpenAI TTS adapter from the call's key (C1 keys frame).
// A missing key is ErrMissingTTSKey; the child and other calls are
// unaffected. Construction logs vendor+model only, never the key.
func NewTTS(cfg ProviderConfig, key string) (voicechild.TTSProvider, error) {
	if cfg.Vendor != "openai" {
		return nil, fmt.Errorf("%w: tts vendor %q", ErrUnknownVendor, cfg.Vendor)
	}
	if key == "" {
		return nil, ErrMissingTTSKey
	}
	if cfg.Model == "" {
		cfg.Model = ttsVendorDefaultModel
	}
	return &OpenAITTS{cfg: cfg, apiKey: key, transport: ttsTransport}, nil
}

// setTTSTransport replaces the HTTP transport (test seam for the httptest
// fake vendor).
func (t *OpenAITTS) setTTSTransport(rt http.RoundTripper) {
	t.transport = rt
}

// Speak implements voicechild.TTSProvider: it posts the synthesis request
// and returns a reader that yields target-rate s16le mono PCM.
func (t *OpenAITTS) Speak(ctx context.Context, text string) (io.ReadCloser, error) {
	if t.apiKey == "" {
		return nil, ErrMissingTTSKey
	}
	if text == "" {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	body, err := json.Marshal(openAITTSRequest{
		Model:          t.cfg.Model,
		Voice:          "alloy",
		Input:          text,
		ResponseFormat: "pcm",
	})
	if err != nil {
		return nil, fmt.Errorf("openai tts: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAISpeechPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai tts: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+t.apiKey)
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
