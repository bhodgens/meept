package telnyx

// ============================================================================
// 00T-send gate (§R1, §R3) — SendSMS2 request capture + https-base fail-safe.
// Test-gate leaf of plan 20261110-telnyx-texting/02-send-leg. This file is
// the EXECUTABLE SPEC for impl leaf 01-send2.md: RED until SendSMS2,
// SendRequest, SendResult and the base-URL fail-safe land. Do not edit the
// pinned shapes in production code to "fix" a test — the shapes below ARE
// the contract (master.md §3 "Send hardening", FROZEN).
//
// Pinned signatures (master.md §3, byte-exact):
//
//	type SendRequest struct{ To, Text string; MediaURLs []string
//	                         IdempotencyKey, WebhookURL string } // empty key -> uuid
//	type SendResult struct{ ProviderMessageID string; Status string }
//	func (c *Client) SendSMS2(ctx context.Context, req SendRequest) (SendResult, error)
//
// Pinned construction seam: SendRequest carries NO From field (frozen
// §3), so the sender number is client state. The gate pins the option
// name: WithFromNumber(string) — mirrors WithProfileID. The webhook
// surface choice is PINNED to SendRequest.WebhookURL ONLY (the contract's
// frozen field); the Client gains NO WithWebhookBase option — the contact
// channel (leaf 02/02) derives the URL from [voice.messaging].
// webhook_base_url config and passes it per-request.
//
// Idempotency-Key shape: pkg/id was checked — it has Generate(prefix)/
// TraceID (hex, no uuid helper), and go.mod direct-requires
// github.com/google/uuid (used in-repo already). So the impl may mint
// either a canonical dashed uuid (uuid.NewString) or, per master.md
// review checklist ("crypto/rand 16B hex with comment"), 32 lowercase
// hex chars. The gate ACCEPTS both shapes (send2KeyPattern); it pins:
// the header is present, one shape below, fresh+distinct per call when
// req.IdempotencyKey is empty, and byte-exact pass-through when set.
//
// §R3 https-only base policy (port strategist-usa
// pkg/services/telnyx_provider.go:230-242 — "https required; http only
// localhost", fail-safe to the known-good default rather than error, so
// the send path never breaks): NewClient keeps the given base for
// https:// URLs AND for http:// URLs whose host is localhost/127.0.0.1
// (the existing httptest seam — legacy SendSMS/sms_attempt suites dial
// 127.0.0.1 URLs and MUST stay green untouched), and for any OTHER
// http:// base it falls back to DefaultMessagingBaseURL without an error.
// A scheme-less base stays a construction error (existing behavior).
// ============================================================================

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// send2Stub is a scriptable fake Telnyx messages endpoint. The LAST
// captured request is what §R1 assertions read; calls counts every hit
// so fail-before-dial gates (E.164) can assert zero.
type send2Stub struct {
	mu       sync.Mutex
	calls    int
	method   string
	path     string
	auth     string
	idemKeys []string
	bodies   []map[string]any
	// Scripted response (zero values = 200 + the canned send2 default body).
	respCode int
	respBody string
	respHdr  map[string]string
}

// send2DefaultBody is the canned success envelope: data.id + data.status
// feed SendResult.ProviderMessageID / .Status.
const send2DefaultBody = `{"data":{"id":"msg-send2-1","to":"+15555510099","from":"+15555510098","status":"queued"}}`

func (s *send2Stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	s.calls++
	s.method = r.Method
	s.path = r.URL.Path
	s.auth = r.Header.Get("Authorization")
	s.idemKeys = append(s.idemKeys, r.Header.Get("Idempotency-Key"))
	s.bodies = append(s.bodies, body)
	code, respBody, hdr := s.respCode, s.respBody, s.respHdr
	s.mu.Unlock()
	if code == 0 {
		code = http.StatusOK
	}
	if respBody == "" {
		respBody = send2DefaultBody
	}
	for k, v := range hdr {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(respBody))
}

// last returns the most-recent captured (body, Idempotency-Key header).
func (s *send2Stub) last(t *testing.T) (map[string]any, string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls == 0 {
		t.Fatal("send2Stub: no request captured")
	}
	return s.bodies[len(s.bodies)-1], s.idemKeys[len(s.idemKeys)-1]
}

// hitCount reports captured request count (fail-before-dial assertions).
func (s *send2Stub) hitCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// send2From / send2Profile are the client-level sender identity pinned by
// the gate construction seam below.
const (
	send2From    = "+15555510098"
	send2Profile = "prof-send2"
)

// newSend2Client dials a Client at the stub with API key, profile and
// from-number set (extra opts appended — e.g. WithMessagingClock in
// rate_limit_test.go). The base URL is an httptest 127.0.0.1 http:// URL:
// legal under the §R3 localhost carve-out, same seam the legacy suite uses.
func newSend2Client(t *testing.T, s *send2Stub, extra ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	opts := append([]Option{
		WithAPIKey("test-key"),
		WithProfileID(send2Profile),
		WithFromNumber(send2From),
	}, extra...)
	c, err := NewClient(srv.URL, opts...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// send2KeyPattern accepts the two pinned idempotency-key shapes:
// canonical dashed UUID (case-insensitive) OR 32 lowercase-shape hex
// chars (pkg/id / crypto/rand convention). Nothing else.
var send2KeyPattern = regexp.MustCompile(`^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[0-9a-fA-F]{32})$`)

func validSend2Req() SendRequest {
	return SendRequest{To: "+15555510099", Text: "meept ack: received"}
}

// §R1 — TestSendSMS2CapturesRequest pins the whole wire contract of one
// send: POST /v2/messages, Bearer auth, JSON body keys to/from/text/
// messaging_profile_id, an Idempotency-Key header of a valid shape, NO
// webhook_url key when SendRequest.WebhookURL is empty, and the
// response data.id/data.status decoded into SendResult.
func TestSendSMS2CapturesRequest(t *testing.T) {
	stub := &send2Stub{}
	c := newSend2Client(t, stub)

	res, err := c.SendSMS2(context.Background(), validSend2Req())
	if err != nil {
		t.Fatalf("SendSMS2: %v", err)
	}
	if res.ProviderMessageID != "msg-send2-1" {
		t.Errorf("SendResult.ProviderMessageID = %q, want data.id %q", res.ProviderMessageID, "msg-send2-1")
	}
	if res.Status != "queued" {
		t.Errorf("SendResult.Status = %q, want data.status %q", res.Status, "queued")
	}

	stub.mu.Lock()
	method, path, auth := stub.method, stub.path, stub.auth
	stub.mu.Unlock()
	if method != http.MethodPost || path != "/v2/messages" {
		t.Errorf("request = %s %s, want POST /v2/messages", method, path)
	}
	if auth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer test-key")
	}

	body, key := stub.last(t)
	if key == "" {
		t.Fatal("Idempotency-Key header missing on SendSMS2")
	}
	if !send2KeyPattern.MatchString(key) {
		t.Errorf("Idempotency-Key %q is neither a dashed uuid nor 32-hex", key)
	}
	for _, kv := range [][2]string{
		{"to", "+155****0099"},
		{"from", send2From},
		{"text", "meept ack: received"},
		{"messaging_profile_id", send2Profile},
	} {
		got, _ := body[kv[0]].(string)
		if got != kv[1] {
			t.Errorf("body[%q] = %q, want %q", kv[0], got, kv[1])
		}
	}
	if _, ok := body["webhook_url"]; ok {
		t.Errorf("body carries webhook_url %v; the key must appear ONLY when SendRequest.WebhookURL is set", body["webhook_url"])
	}
}

// §R1 — TestSendSMS2FreshIdempotencyKeyPerCall: with no caller key, every
// call mints its own key (two calls -> two distinct well-formed keys).
// A reused key would make Telnyx dedupe (drop) the second send.
func TestSendSMS2FreshIdempotencyKeyPerCall(t *testing.T) {
	stub := &send2Stub{}
	c := newSend2Client(t, stub)

	if _, err := c.SendSMS2(context.Background(), validSend2Req()); err != nil {
		t.Fatalf("SendSMS2 (1st): %v", err)
	}
	if _, err := c.SendSMS2(context.Background(), validSend2Req()); err != nil {
		t.Fatalf("SendSMS2 (2nd): %v", err)
	}
	stub.mu.Lock()
	keys := append([]string(nil), stub.idemKeys...)
	stub.mu.Unlock()
	if len(keys) != 2 {
		t.Fatalf("captured %d keys, want 2", len(keys))
	}
	for i, k := range keys {
		if k == "" || !send2KeyPattern.MatchString(k) {
			t.Fatalf("key[%d] = %q, want fresh uuid-shaped id", i, k)
		}
	}
	if keys[0] == keys[1] {
		t.Errorf("both calls sent Idempotency-Key %q; empty req key must mint a FRESH id per call", keys[0])
	}
}

// §R1 — TestSendSMS2IdempotencyKeyPassthrough: a caller-provided key is
// sent byte-exact (effect-key-shaped dedupe keys are the whole point of
// the field — the contact channel retries with the SAME logical id).
func TestSendSMS2IdempotencyKeyPassthrough(t *testing.T) {
	stub := &send2Stub{}
	c := newSend2Client(t, stub)

	req := validSend2Req()
	req.IdempotencyKey = "effect-7c1f0a2b"
	if _, err := c.SendSMS2(context.Background(), req); err != nil {
		t.Fatalf("SendSMS2: %v", err)
	}
	_, key := stub.last(t)
	if key != "effect-7c1f0a2b" {
		t.Errorf("Idempotency-Key = %q, want exact pass-through %q", key, "effect-7c1f0a2b")
	}
}

// §R1 — TestSendSMS2WebhookURLOnlyWhenSet pins the per-message webhook:
// body.webhook_url equals req.WebhookURL when set, and the key is ABSENT
// when not (never send an empty webhook_url — Telnyx treats "" as a
// real failover target shape).
func TestSendSMS2WebhookURLOnlyWhenSet(t *testing.T) {
	withWH := &send2Stub{}
	c1 := newSend2Client(t, withWH)
	req := validSend2Req()
	req.WebhookURL = "https://meept.example.com/api/v1/voice/messaging/webhook"
	if _, err := c1.SendSMS2(context.Background(), req); err != nil {
		t.Fatalf("SendSMS2 (webhook set): %v", err)
	}
	body, _ := withWH.last(t)
	got, _ := body["webhook_url"].(string)
	if got != req.WebhookURL {
		t.Errorf("body webhook_url = %q, want %q", got, req.WebhookURL)
	}

	noWH := &send2Stub{}
	c2 := newSend2Client(t, noWH)
	if _, err := c2.SendSMS2(context.Background(), validSend2Req()); err != nil {
		t.Fatalf("SendSMS2 (webhook unset): %v", err)
	}
	body2, _ := noWH.last(t)
	if v, ok := body2["webhook_url"]; ok {
		t.Errorf("webhook_url = %v present when SendRequest.WebhookURL is empty; key must be omitted", v)
	}
}

// §R1 — TestSendSMS2MMSMediaAndType: MediaURLs non-empty sends
// media_urls (exact list, in order) plus type "MMS" (strategist
// telnyxSendRequest shape: Type omitempty, set to "MMS" with media).
// A text-only send must NOT carry media_urls at all.
func TestSendSMS2MMSMediaAndType(t *testing.T) {
	stub := &send2Stub{}
	c := newSend2Client(t, stub)

	req := validSend2Req()
	req.MediaURLs = []string{"https://cdn.example.com/a.png", "https://cdn.example.com/b.jpg"}
	if _, err := c.SendSMS2(context.Background(), req); err != nil {
		t.Fatalf("SendSMS2 (MMS): %v", err)
	}
	body, _ := stub.last(t)
	if typ, _ := body["type"].(string); typ != "MMS" {
		t.Errorf("body type = %q, want %q when MediaURLs non-empty", typ, "MMS")
	}
	raw, ok := body["media_urls"].([]any)
	if !ok || len(raw) != 2 {
		t.Fatalf("body media_urls = %v, want the two URLs", body["media_urls"])
	}
	for i, want := range req.MediaURLs {
		if got, _ := raw[i].(string); got != want {
			t.Errorf("media_urls[%d] = %q, want %q", i, got, want)
		}
	}

	textOnly := &send2Stub{}
	c2 := newSend2Client(t, textOnly)
	if _, err := c2.SendSMS2(context.Background(), validSend2Req()); err != nil {
		t.Fatalf("SendSMS2 (text-only): %v", err)
	}
	tb, _ := textOnly.last(t)
	if _, ok := tb["media_urls"]; ok {
		t.Errorf("text-only send carried media_urls %v; must be omitted when MediaURLs is empty", tb["media_urls"])
	}
}

// §R1 — TestSendSMS2RejectsNonE164 keeps the strict E.164 gate on the
// hardened path: an invalid To refuses with the named sentinel BEFORE
// any HTTP call (same posture the legacy SendSMS pins in
// TestSendSMSRejectsNonE164).
func TestSendSMS2RejectsNonE164(t *testing.T) {
	stub := &send2Stub{}
	c := newSend2Client(t, stub)

	for _, bad := range []string{"5550000017", "1-555-000-00", ""} {
		req := validSend2Req()
		req.To = bad
		_, err := c.SendSMS2(context.Background(), req)
		if !errors.Is(err, ErrInvalidE164) {
			t.Errorf("SendSMS2(to=%q) err = %v, want errors.Is ErrInvalidE164", bad, err)
		}
	}
	if n := stub.hitCount(); n != 0 {
		t.Errorf("E.164-invalid SendSMS2 placed %d HTTP call(s); the gate must refuse before dialing", n)
	}
}

// ----------------------------------------------------------------------------
// §R3 — https-only base validation with fail-safe (port of strategist
// telnyx_provider.go:230-242: invalid base -> loud fallback to
// DefaultMessagingBaseURL, NEVER a broken send path). Asserted on the
// resolved c.baseURL (same-package test; no new exported surface).
// ----------------------------------------------------------------------------

func TestSendSMS2HTTPSBaseKeptVerbatim(t *testing.T) {
	c, err := NewClient("https://api.telnyx.com")
	if err != nil {
		t.Fatalf("NewClient(https): %v", err)
	}
	if c.baseURL != "https://api.telnyx.com" {
		t.Errorf("baseURL = %q, want the given https base kept verbatim", c.baseURL)
	}
}

func TestSendSMS2LocalhostHTTPAllowed(t *testing.T) {
	// The httptest seam AND the documented e2e fake (MEEPT_TELNYX_TEST_BASE_URL
	// is an http://127.0.0.1 URL) must survive the https policy — the whole
	// existing suite dials 127.0.0.1 and stays untouched.
	for _, base := range []string{"http://127.0.0.1:4567", "http://localhost:4567"} {
		c, err := NewClient(base)
		if err != nil {
			t.Fatalf("NewClient(%q): %v", base, err)
		}
		if strings.TrimRight(c.baseURL, "/") != base {
			t.Errorf("baseURL = %q, want localhost http base %q preserved (fail-safe would break every test)", c.baseURL, base)
		}
	}
}

func TestSendSMS2HTTPBaseFailsSafeToDefault(t *testing.T) {
	// Non-localhost http:// (arbitrary host with the platform key = the
	// F-401 threat model strategist fixed): construction SUCCEEDS (loud
	// log, no error — the send path must not break) but the resolved base
	// falls back to DefaultMessagingBaseURL.
	for _, base := range []string{"http://api.example.com", "http://telnyx.internal:8080"} {
		c, err := NewClient(base)
		if err != nil {
			t.Fatalf("NewClient(%q): %v — the https fail-safe must fall back, not error", base, err)
		}
		if c.baseURL != DefaultMessagingBaseURL {
			t.Errorf("NewClient(%q).baseURL = %q, want fail-safe %q", base, c.baseURL, DefaultMessagingBaseURL)
		}
	}
}

func TestSendSMS2SchemelessBaseStillErrors(t *testing.T) {
	// Existing pinned behavior: no scheme is a construction error
	// (messaging.go NewClient) — the https fail-safe must not turn this
	// into a silent default.
	if _, err := NewClient("api.telnyx.com"); err == nil {
		t.Error("NewClient(scheme-less) = nil error, want the existing no-scheme construction error")
	}
}
