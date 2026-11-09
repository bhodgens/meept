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
	"regexp"
	"strings"
	"time"
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

// WithMessagingHTTPClient overrides the HTTP transport (tests inject
// short timeouts; TLS verification is never disabled here).
func WithMessagingHTTPClient(hc *http.Client) Option { return func(c *Client) { c.httpClient = hc } }

// WithMessagingLogger overrides slog.Default().
func WithMessagingLogger(l *slog.Logger) Option { return func(c *Client) { c.log = l } }

// NewClient returns a messaging Client anchored at baseURL (empty means
// DefaultMessagingBaseURL). The error return exists for URL validation:
// a malformed base URL is a construction-time failure, never a
// mid-request surprise.
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
	return c, nil
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
