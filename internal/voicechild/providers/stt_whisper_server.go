package providers

// Persistent whisper.cpp server transport (feature F2, contract C1). When
// ProviderConfig.WhisperServerAddr is non-empty, WhisperSTT.recognizeOne
// ships the utterance WAV to a resident whisper.cpp HTTP server instead of
// exec'ing whisper-cli per segment. The model stays loaded in the server
// process, removing the ~350 ms cold subprocess per utterance. Exec mode
// (empty address) is byte-identical and untouched.
//
// Wire shape (whisper.cpp server /inference): a multipart form carrying the
// WAV in the "file" field; response_format=text yields a plain-text
// transcript. A JSON response (the server's default shape when
// response_format is not honored) is decoded by its "text" field so the
// returned transcript is identical to exec mode either way.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// whisperServerInferencePath is the whisper.cpp server's inference route.
const whisperServerInferencePath = "/inference"

// whisperServerMaxResponse bounds a transcript response so a misbehaving
// server cannot make us buffer unbounded data (transcripts are small).
const whisperServerMaxResponse = 1 << 20 // 1 MiB

// whisperServerTransport is a SHARED, persistent HTTP transport: all
// adapters across all calls reuse one connection pool, so a warm connection
// to the resident server survives between utterances and calls. Idle
// connections are bounded and time out (http.DefaultTransport defaults),
// so no per-call goroutine/connection leak accumulates. No new module
// dependency — stdlib net/http only.
var whisperServerHTTPClient = &http.Client{Transport: http.DefaultTransport}

// whisperServerClient is the opt-in persistent transport. It is stateless
// apart from the target address; every request carries its own bounded
// context so no request can outlive the call that started it.
type whisperServerClient struct {
	addr    string // normalized base URL (scheme + optional path)
	baseURL *url.URL
}

// newWhisperServerClient normalizes addr into a base URL. A bare
// host:port (the common whisper.cpp server form) gets an http:// scheme;
// an explicit http:// or https:// scheme is respected. A caller-supplied
// path is preserved; otherwise /inference is used per request.
func newWhisperServerClient(addr string) *whisperServerClient {
	normalized := strings.TrimSpace(addr)
	if !strings.Contains(normalized, "://") {
		normalized = "http://" + normalized
	}
	u, err := url.Parse(normalized)
	if err != nil {
		// A malformed address fails at recognize time with a clear error;
		// keep the original so the error names what the operator provided.
		return &whisperServerClient{addr: addr}
	}
	return &whisperServerClient{addr: addr, baseURL: u}
}

// inferenceURL builds the per-request URL: the configured path (or the
// whisper.cpp default /inference) with response_format=text requested.
func (c *whisperServerClient) inferenceURL() string {
	if c.baseURL == nil {
		return ""
	}
	u := *c.baseURL
	if u.Path == "" || u.Path == "/" {
		u.Path = whisperServerInferencePath
	}
	q := u.Query()
	q.Set("response_format", "text")
	u.RawQuery = q.Encode()
	return u.String()
}

// recognizeOnServer writes the segment WAV exactly as exec mode does,
// sends it to the resident server within the per-segment timeout, and
// returns the trimmed transcript. The segment context is always canceled
// before return, so an in-flight request can never outlive the call.
func (s *WhisperSTT) recognizeOnServer(ctx context.Context, dir string, seq int, buf []byte) (string, error) {
	wavPath := filepath.Join(dir, fmt.Sprintf("utt%d.wav", seq))
	if err := writeWAV16(wavPath, buf, s.cfg.SampleRate); err != nil {
		return "", fmt.Errorf("write wav: %w", err)
	}
	wav, err := os.ReadFile(wavPath)
	if err != nil {
		return "", fmt.Errorf("whisper server read wav: %w", err)
	}
	endpoint := s.server.inferenceURL()
	if endpoint == "" {
		return "", fmt.Errorf("whisper server: invalid address %q", s.server.addr)
	}
	segCtx, cancel := context.WithTimeout(ctx, whisperRecognizeTimeout)
	defer cancel() // no stale request outlives this call

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, err := mw.CreateFormFile("file", filepath.Base(wavPath))
	if err != nil {
		return "", fmt.Errorf("whisper server multipart part: %w", err)
	}
	if _, err := part.Write(wav); err != nil {
		return "", fmt.Errorf("whisper server multipart write: %w", err)
	}
	// F5 parity with exec mode (bughunt 2026-11-19 M5): non-empty
	// Vocabulary primes critical entities via the server's "prompt"
	// multipart field (whisper.cpp /inference initial prompt). Empty
	// vocabulary adds NO field — the wire stays byte-identical (R4).
	if terms := primingTerms(s.cfg.Vocabulary); len(terms) > 0 {
		if err := mw.WriteField("prompt", strings.Join(terms, " ")); err != nil {
			return "", fmt.Errorf("whisper server multipart prompt: %w", err)
		}
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("whisper server multipart close: %w", err)
	}

	req, err := http.NewRequestWithContext(segCtx, http.MethodPost, endpoint, body)
	if err != nil {
		return "", fmt.Errorf("whisper server request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := whisperServerHTTPClient.Do(req)
	if err != nil {
		if segCtx.Err() != nil {
			return "", fmt.Errorf("whisper server on segment: %w", segCtx.Err())
		}
		return "", fmt.Errorf("whisper server on segment: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body drain on teardown
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		tail, _ := io.ReadAll(io.LimitReader(resp.Body, whisperStderrTailBytes))
		return "", fmt.Errorf("whisper server on segment: HTTP %d (tail: %s)", resp.StatusCode, strings.TrimSpace(string(tail)))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, whisperServerMaxResponse))
	if err != nil {
		return "", fmt.Errorf("whisper server read response: %w", err)
	}
	return decodeWhisperServerTranscript(raw), nil
}

// decodeWhisperServerTranscript returns the plain-text body, or extracts
// the "text" field when the server answered with its default JSON shape.
// A successfully-decoded envelope ALWAYS yields its text field — an empty
// text means silence, never the raw JSON blob (bughunt 2026-11-19 L8).
// Only an undecodable (non-envelope) body falls back to the raw text.
func decodeWhisperServerTranscript(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, "{") {
		var envelope struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &envelope); err == nil {
			return strings.TrimSpace(envelope.Text)
		}
	}
	return text
}
