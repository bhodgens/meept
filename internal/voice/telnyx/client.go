// Package telnyx is the daemon-side Telnyx Call Control client: borrowed
// from uptractor (core/internal/telnyx) and adapted for meept. It dials,
// answers, streams media over bidirectional RTP, hangs up, parses call
// webhooks, and verifies Ed25519 signatures on both webhooks and media
// sockets. It does not send SMS.
//
// The API key is never logged and never appears in error text.
package telnyx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the production Telnyx API v2 root. Tests override
// Client.BaseURL with an httptest URL.
const DefaultBaseURL = "https://api.telnyx.com/v2"

// defaultHTTPTimeout bounds every outbound Telnyx HTTP request.
const defaultHTTPTimeout = 10 * time.Second

// defaultHTTPClient is the transport used unless a Client carries its own.
// TLS certificate verification is never disabled anywhere in this package.
var defaultHTTPClient = &http.Client{Timeout: defaultHTTPTimeout}

// drainFully reads the response body to EOF so the connection returns to
// the transport pool. Best-effort: callers log and continue.
func drainFully(r io.Reader) error {
	_, err := io.Copy(io.Discard, r)
	return err
}

// Client speaks to the Telnyx Call Control API. BaseURL is the API origin
// plus the version path (default DefaultBaseURL); tests replace it with an
// httptest URL. The API key is set at construction from an
// already-resolved string (the daemon wiring resolves it via the secrets
// Broker at leaf 14) and is never logged.
type Client struct {
	BaseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient returns a Client that authenticates with apiKey against
// baseURL (empty means DefaultBaseURL).
func NewClient(baseURL, apiKey string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: defaultHTTPClient,
	}
}

// postCall sends one authenticated POST to BaseURL+path with an optional
// JSON body. A non-2xx response is an %w-wrapped error carrying the status
// code and a sanitized body snippet (headers are never included, so the
// Authorization bearer token cannot leak through an error). The response
// body is always drained and closed so the connection returns to the
// transport pool for reuse.
func (c *Client) postCall(ctx context.Context, op, path string, body []byte) ([]byte, error) {
	var payload []byte
	if len(body) > 0 {
		payload = body
	}
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("telnyx %s: request: %w", op, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	hc := c.httpClient
	if hc == nil {
		hc = defaultHTTPClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telnyx %s: %w", op, err)
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if drainErr := drainFully(resp.Body); drainErr != nil {
		// Best-effort connection reuse; a failed drain never fails the call.
		log.Printf("telnyx %s: drain: %v", op, drainErr)
	}
	if readErr != nil {
		return nil, fmt.Errorf("telnyx %s: read response: %w", op, readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The body carries Telnyx's error detail; surface a snippet so a
		// failed live call names its reason instead of a bare status.
		// snippet() strips newlines and caps length; request headers are
		// never part of the message.
		return nil, fmt.Errorf("telnyx %s: status %d: %s", op, resp.StatusCode, snippet(respBody))
	}
	return respBody, nil
}

// snippet returns body as one short single-line string for error messages.
func snippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 300 {
		s = s[:300]
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, s)
}

// AnswerCall tells Telnyx to answer the call identified by callControlID.
// Only ever called on INBOUND legs: answering an outbound leg 404s and
// aborts the session (uptractor live drill).
func (c *Client) AnswerCall(ctx context.Context, callControlID string) error {
	_, err := c.postCall(ctx, "answer call", "/calls/"+url.PathEscape(callControlID)+"/answer", nil)
	return err
}

// HangupCall tells Telnyx to end the call identified by callControlID.
func (c *Client) HangupCall(ctx context.Context, callControlID string) error {
	_, err := c.postCall(ctx, "hangup call", "/calls/"+url.PathEscape(callControlID)+"/actions/hangup", nil)
	return err
}
