// Telnyx messaging (SMS) client (contact-channels plan, contract C5,
// leaf 12). One Telnyx account serves both Call Control (internal/voice/
// telnyx) and messaging; this package is the messaging half: the send
// client, the webhook payload parser for message.received /
// message.finalized, the named sentinels, and the telnyx_sms
// contact.ChannelAttempt adapter (sms_attempt.go).
//
// The Ed25519 webhook SIGNATURE verifier is NOT duplicated here: the
// receiver in internal/comm/http reuses
// voice/telnyx.VerifyWebhookSignature through its Verify seam.
//
// Ops note (documented, not coded): application-to-person traffic from
// local numbers requires 10DLC brand+campaign registration (days to
// weeks); the toll-free-verified path is recommended for this use case.
// The number must also carry a Messaging Profile (profile_id below).
package telnyx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DefaultMessagingBaseURL is the production Telnyx API v2 root. Tests
// override it via NewClient's baseURL argument (the injectable HTTP
// boundary).
const DefaultMessagingBaseURL = "https://api.telnyx.com"

// DefaultDLRWaitSeconds is the bounded wait for message.finalized before
// an attempt reports unavailable (T5 default; config overrides).
const DefaultDLRWaitSeconds = 30

// Named sentinels (repo rule): errors.Is-checkable by every consumer.
var (
	// ErrInvalidE164 is returned when a from/to number fails the strict
	// E.164 gate. The gate runs BEFORE any HTTP call is placed.
	ErrInvalidE164 = errors.New("telnyx messaging: number is not E.164")
	// ErrMessagingDisabled is returned by the SMS attempt adapter when
	// [voice].messaging.enabled is false, before any HTTP call.
	ErrMessagingDisabled = errors.New("telnyx messaging: messaging is disabled")
	// ErrUnknownMessage is returned when a DLR arrives for a message id
	// this process never sent (or already evicted from the bounded
	// pending table). Best-effort observability only.
	ErrUnknownMessage = errors.New("telnyx messaging: unknown message id")
)

// e164Pattern is the strict E.164 gate: leading +, non-zero first digit,
// 8-15 more digits.
var e164Pattern = regexp.MustCompile(`^\+[1-9]\d{7,15}$`)

// ValidE164 reports whether s is a strict E.164 number.
func ValidE164(s string) bool { return e164Pattern.MatchString(s) }

// Message is one SMS event, either an inbound text (message.received) or
// a terminal delivery report (message.finalized). Field set is pinned by
// contract C5.
type Message struct {
	ID         string
	From       string // E.164
	To         string // E.164
	Text       string
	Direction  string // "inbound"|"outbound"
	Status     string // ""|"queued"|"sent"|"delivered"|"failed" (DLR carries it)
	ReceivedAt time.Time
}

// Client speaks to the Telnyx Messaging API (POST /v2/messages). The API
// key is set at construction from an already-resolved string (the daemon
// wiring resolves it via the secrets Broker) and is never logged. baseURL
// is the injectable HTTP boundary: production uses
// DefaultMessagingBaseURL, tests pass an httptest URL.
type Client struct {
	baseURL    string
	apiKey     string
	profileID  string
	fromNumber string           // sender number for SendSMS2 (WithFromNumber)
	clock      func() time.Time // injectable clock (WithMessagingClock); nil = time.Now
	httpClient *http.Client
	log        *slog.Logger
}

// Option customizes a messaging Client at construction.
type Option func(*Client)

// WithAPIKey sets the bearer token used on every request.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithProfileID sets the Messaging Profile id sent on every send
// (Telnyx requires a profile bound to the from number).
func WithProfileID(profileID string) Option { return func(c *Client) { c.profileID = profileID } }

// WithFromNumber sets the client-level sender number used by SendSMS2
// (the frozen SendRequest carries no From field, so the sender is
// client state — construction seam pinned by the 00T send gate; mirrors
// WithProfileID).
func WithFromNumber(number string) Option { return func(c *Client) { c.fromNumber = number } }

// WithMessagingClock injects the clock used to resolve HTTP-date
// Retry-After values into RateLimitError.RetryAfter (rate_limit.go).
// nil keeps time.Now. Pinned seam of the 00T rate-limit gate.
func WithMessagingClock(clock func() time.Time) Option { return func(c *Client) { c.clock = clock } }

// WithMessagingHTTPClient overrides the HTTP transport (tests inject
// short timeouts; TLS verification is never disabled here).
func WithMessagingHTTPClient(hc *http.Client) Option { return func(c *Client) { c.httpClient = hc } }

// WithMessagingLogger overrides slog.Default().
func WithMessagingLogger(l *slog.Logger) Option { return func(c *Client) { c.log = l } }

// NewClient returns a messaging Client anchored at baseURL (empty means
// DefaultMessagingBaseURL). The error return exists for URL validation:
// a malformed base URL is a construction-time failure, never a
// mid-request surprise.
//
// §R3 https policy (ported from strategist-usa
// pkg/services/telnyx_provider.go:230-242, the F-401 fix): an http://
// base whose host is NOT localhost/127.0.0.1/::1 could dial an arbitrary
// host with the platform API key, so it is rejected — but the send path
// must never break, so instead of erroring NewClient logs loudly and
// fails safe to DefaultMessagingBaseURL. https bases and localhost http
// bases (the httptest seam) pass through verbatim; a scheme-less base
// stays a construction error (existing pinned behavior).
func NewClient(baseURL string, opts ...Option) (*Client, error) {
	if baseURL == "" {
		baseURL = DefaultMessagingBaseURL
	}
	if !strings.Contains(baseURL, "://") {
		return nil, fmt.Errorf("telnyx messaging: base URL %q has no scheme", baseURL)
	}
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: http.DefaultClient,
		log:        slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	// Ported from strategist-usa pkg/services/telnyx_client.go:1799-1819
	// (validateTelnyxBaseURL), adapted to the fail-safe posture of
	// strategist-usa pkg/services/telnyx_provider.go:230-242: fall back
	// to the known-good default, never error.
	if err := validateBaseURL(c.baseURL); err != nil {
		c.log.Error("telnyx messaging: invalid api base url, falling back to default",
			"baseURL", c.baseURL, "default", DefaultMessagingBaseURL, "error", err)
		c.baseURL = DefaultMessagingBaseURL
	}
	return c, nil
}

// validateBaseURL enforces the §R3 scheme policy: https always; http
// only for localhost hosts (localhost, 127.0.0.1, ::1). Anything else —
// including an unparseable URL — is an error the caller fail-safes on.
func validateBaseURL(base string) error {
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("telnyx messaging: invalid base URL %q: %w", base, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("telnyx messaging: base URL %q must be absolute with scheme and host", base)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return fmt.Errorf("telnyx messaging: base URL %q: http scheme only allowed for localhost", base)
		}
		return nil
	default:
		return fmt.Errorf("telnyx messaging: base URL %q: unsupported scheme %q (want https)", base, u.Scheme)
	}
}

// sendRequest posts one authenticated JSON request to base+path and
// decodes a 2xx body. A non-2xx response is an %w-wrapped error carrying
// the status and a sanitized body snippet (headers are never included,
// so the Authorization bearer token cannot leak through an error).
func (c *Client) sendRequest(ctx context.Context, op, path string, body any, out any) error {
	var payload []byte
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("telnyx %s: marshal: %w", op, err)
		}
		payload = buf
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("telnyx %s: request: %w", op, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	hc := c.httpClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("telnyx %s: %w", op, err)
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if drainErr := drain(resp.Body); drainErr != nil {
		// Best-effort connection reuse; a failed drain never fails the call.
		c.log.Debug("telnyx messaging: drain", "error", drainErr)
	}
	if readErr != nil {
		return fmt.Errorf("telnyx %s: read response: %w", op, readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telnyx %s: status %d: %s", op, resp.StatusCode, snippet(respBody))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("telnyx %s: decode response: %w", op, err)
		}
	}
	return nil
}

// drain reads the response body to EOF so the connection returns to the
// transport pool. Best-effort: callers log and continue.
func drain(r io.Reader) error {
	_, err := io.Copy(io.Discard, r)
	return err
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

type sendSMSRequest struct {
	From              string `json:"from"`
	To                string `json:"to"`
	Text              string `json:"text"`
	MessagingProfiled string `json:"messaging_profile_id"`
}

type sendSMSResponse struct {
	Data struct {
		ID string `json:"id"`
	} `json:"data"`
}

// SendSMS sends one outbound SMS and returns the Telnyx message id
// (data.id). Both numbers pass the strict E.164 gate BEFORE any HTTP
// call; a violation is ErrInvalidE164 (errors.Is-checkable).
//
// Deprecated: the hardened SendSMS2 (Idempotency-Key + per-message
// webhook_url + typed RateLimitError) replaced every production caller
// in leaf 02-send-leg/02. This method stays for the pinned legacy tests
// only; new code must use SendSMS2.
func (c *Client) SendSMS(ctx context.Context, from, to, text string) (string, error) {
	if !ValidE164(from) {
		return "", fmt.Errorf("telnyx messaging: from %q: %w", from, ErrInvalidE164)
	}
	if !ValidE164(to) {
		return "", fmt.Errorf("telnyx messaging: to %q: %w", to, ErrInvalidE164)
	}
	var out sendSMSResponse
	req := sendSMSRequest{From: from, To: to, Text: text, MessagingProfiled: c.profileID}
	if err := c.sendRequest(ctx, "send sms", "/v2/messages", req, &out); err != nil {
		return "", err
	}
	if out.Data.ID == "" {
		return "", errors.New("telnyx send sms: response carries no data.id")
	}
	return out.Data.ID, nil
}

// ----------------------------------------------------------------------------
// SendSMS2 — hardened send path (telnyx-texting plan master.md §3, §R1-§R2).
// Legacy SendSMS above is untouched; contact-channel callers migrate in
// leaf 02-send-leg/02.
// ----------------------------------------------------------------------------

// SendRequest is one hardened send (shape FROZEN by the 00T gate:
// messaging_send2_test.go header). There is deliberately NO From field —
// the sender number is client state set with WithFromNumber — and NO
// webhook option on the Client: the per-message webhook surface is
// SendRequest.WebhookURL ONLY, derived by the contact channel from
// [voice.messaging].webhook_base_url.
type SendRequest struct {
	To, Text  string
	MediaURLs []string
	// IdempotencyKey is sent byte-exact as the Idempotency-Key header
	// when non-empty (caller-side retry dedupe); empty mints a fresh
	// uuid per call so two logical sends never collapse into one.
	IdempotencyKey string
	// WebhookURL, when non-empty, becomes body webhook_url — the
	// per-message DLR/inbound webhook. The key is OMITTED entirely when
	// empty (Telnyx treats "" as a real failover-target shape).
	WebhookURL string
}

// SendResult carries the accepted message identity back from Telnyx
// (response data.id / data.status).
type SendResult struct {
	ProviderMessageID string
	Status            string
}

// send2Request is the POST /v2/messages body for SendSMS2. Optional keys
// are omitted, never sent empty — shape ported from strategist-usa
// pkg/services/telnyx_provider.go (telnyxSendRequest: Type omitempty,
// set to "MMS" only with media).
type send2Request struct {
	From               string   `json:"from,omitempty"`
	To                 string   `json:"to"`
	Text               string   `json:"text"`
	MessagingProfileID string   `json:"messaging_profile_id,omitempty"`
	WebhookURL         string   `json:"webhook_url,omitempty"`
	Type               string   `json:"type,omitempty"`
	MediaURLs          []string `json:"media_urls,omitempty"`
}

// send2Response decodes the accepted-message envelope.
type send2Response struct {
	Data struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"data"`
}

// SendSMS2 posts one message to /v2/messages with the send hardening:
// Idempotency-Key header (caller key passed through byte-exact, else a
// fresh uuid), per-message webhook_url only when set, media_urls +
// type "MMS" for media sends, a typed *RateLimitError on HTTP 429, and
// non-2xx errors that carry the status code plus a 512B-truncated body
// snippet (PII hygiene). Both numbers pass the strict E.164 gate BEFORE
// any HTTP call; a violation is ErrInvalidE164 (errors.Is-checkable).
//
// Idempotency key minting: pkg/id has Generate(prefix)/TraceID (hex, no
// uuid helper), so this uses github.com/google/uuid (direct dep, used
// in-repo already) — the fresh-key-per-call posture is ported from
// strategist-usa pkg/services/telnyx_provider.go:307-318 (a caller-side
// retry is a new logical send, never a replay; Telnyx dedupes on
// Idempotency-Key).
func (c *Client) SendSMS2(ctx context.Context, req SendRequest) (SendResult, error) {
	if !ValidE164(req.To) {
		return SendResult{}, fmt.Errorf("telnyx messaging: to %q: %w", req.To, ErrInvalidE164)
	}
	// from is client state (WithFromNumber); when unset the profile
	// alone routes the send, matching the gate's construction seam.
	if c.fromNumber != "" && !ValidE164(c.fromNumber) {
		return SendResult{}, fmt.Errorf("telnyx messaging: from %q: %w", c.fromNumber, ErrInvalidE164)
	}
	key := req.IdempotencyKey
	if key == "" {
		key = uuid.NewString()
	}
	body := send2Request{
		From:               c.fromNumber,
		To:                 req.To,
		Text:               req.Text,
		MessagingProfileID: c.profileID,
		WebhookURL:         req.WebhookURL,
	}
	if len(req.MediaURLs) > 0 {
		body.Type = "MMS"
		body.MediaURLs = req.MediaURLs
	}

	var out send2Response
	err := c.postJSON(ctx, "send sms2", "/v2/messages", body, map[string]string{
		"Idempotency-Key": key,
	}, &out, true)
	if err != nil {
		return SendResult{}, err
	}
	if out.Data.ID == "" {
		return SendResult{}, errors.New("telnyx send sms2: response carries no data.id")
	}
	return SendResult{ProviderMessageID: out.Data.ID, Status: out.Data.Status}, nil
}

// postJSON posts one authenticated JSON request to base+path with extra
// headers and decodes a 2xx body — the SendSMS2 transport (the legacy
// sendRequest above stays byte-identical for its callers). Error posture
// is ported from strategist-usa pkg/services/telnyx_provider.go:307-318
// and :340-353:
//   - HTTP 429 -> *RateLimitError (typed; callers branch with errors.As
//     to sleep the server-provided window instead of burning generic
//     retries).
//   - any other non-2xx -> error carrying the STATUS CODE plus a body
//     snippet truncated to 512B (PII hygiene: the full body, which may
//     carry phone numbers, is never interpolated).
func (c *Client) postJSON(ctx context.Context, op, path string, body any, extraHeaders map[string]string, out any, typedRateLimit bool) error {
	var payload []byte
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("telnyx %s: marshal: %w", op, err)
		}
		payload = buf
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("telnyx %s: request: %w", op, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	hc := c.httpClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("telnyx %s: %w", op, err)
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if drainErr := drain(resp.Body); drainErr != nil {
		c.log.Debug("telnyx messaging: drain", "error", drainErr)
	}
	if readErr != nil {
		return fmt.Errorf("telnyx %s: read response: %w", op, readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Port: 429 must surface as the typed rate-limit error
		// (telnyx_provider.go:307-318) — never fold it into the generic
		// status error, the send workers branch on it for backoff.
		if typedRateLimit && resp.StatusCode == http.StatusTooManyRequests {
			return newRateLimitError(resp.Header, c.clock)
		}
		c.log.Warn("telnyx messaging: error response",
			"op", op, "statusCode", resp.StatusCode, "bodySnippet", errorSnippet(respBody))
		return fmt.Errorf("telnyx %s: status %d: %s", op, resp.StatusCode, errorSnippet(respBody))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("telnyx %s: decode response: %w", op, err)
		}
	}
	return nil
}

// errorSnippet returns the first 512 bytes of an error body for
// messages/logs, marked when cut. Ported from strategist-usa
// pkg/services/telnyx_provider.go:344-348 (maxSnippet = 512 +
// "...(truncated)"):
// the window is measured in BYTES of the raw body (PII hygiene pins it;
// the legacy 300-rune snippet() above is a different, older shape).
func errorSnippet(body []byte) string {
	const maxSnippet = 512
	s := string(body)
	if len(s) > maxSnippet {
		s = s[:maxSnippet] + "...(truncated)"
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '	' {
			return ' '
		}
		return r
	}, s)
}

// webhookEnvelope is the canonical Telnyx messaging webhook shape:
// {"data":{"event_type":..., "payload":{...}}}.
type webhookEnvelope struct {
	Data struct {
		EventType string `json:"event_type"`
		Payload   struct {
			ID         string `json:"id"`
			From       string `json:"from"`
			To         string `json:"to"`
			Text       string `json:"text"`
			Status     string `json:"status"`
			ReceivedAt string `json:"received_at"`
		} `json:"payload"`
	} `json:"data"`
}

// ParseWebhookEvent parses a messaging webhook body into a Message.
// Handled event types: "message.received" (inbound text; Direction
// inbound) and "message.finalized" (terminal DLR; Direction outbound,
// Status taken from the payload - delivered|failed). Any other event
// type is an error so a misrouted webhook can never be acted on as if it
// were an SMS event.
func ParseWebhookEvent(payload []byte) (Message, error) {
	var env webhookEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return Message{}, fmt.Errorf("telnyx messaging webhook: %w", err)
	}
	switch env.Data.EventType {
	case "message.received", "message.finalized":
	default:
		return Message{}, fmt.Errorf("telnyx messaging webhook: unsupported event type %q", env.Data.EventType)
	}
	p := env.Data.Payload
	msg := Message{
		ID:        p.ID,
		From:      p.From,
		To:        p.To,
		Text:      p.Text,
		Status:    p.Status,
		Direction: "outbound",
	}
	if env.Data.EventType == "message.received" {
		msg.Direction = "inbound"
	}
	if p.ReceivedAt != "" {
		if ts, err := time.Parse(time.RFC3339, p.ReceivedAt); err == nil {
			msg.ReceivedAt = ts
		}
		// An unparseable timestamp keeps the zero time: the DLR outcome
		// (id/status) is what the chain consumes, never the timestamp.
	}
	return msg, nil
}
