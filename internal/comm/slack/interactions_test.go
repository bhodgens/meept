package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/acp"
)

const testSecret = "test-signing-secret"

// stubResolver records resolve calls; its returned error is injectable.
// Guarded by a mutex: the resolve runs on the handler's async goroutine
// while the test polls (the bus-subscriber test race class).
type stubResolver struct {
	mu       sync.Mutex
	calls    []resolveCall
	nextErr  error
	noPolicy func(id, verb string) error
}

type resolveCall struct {
	id       string
	verb     string
	approver acp.Approver
}

func (s *stubResolver) resolve(id, verb string, a acp.Approver) error {
	s.mu.Lock()
	s.calls = append(s.calls, resolveCall{id: id, verb: verb, approver: a})
	policy := s.noPolicy
	nextErr := s.nextErr
	s.mu.Unlock()
	if policy != nil {
		return policy(id, verb)
	}
	return nextErr
}

// snapshot copies the recorded calls under the guard.
func (s *stubResolver) snapshot() []resolveCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]resolveCall(nil), s.calls...)
}

type testHarness struct {
	handler *InteractionsHandler
	res     *stubResolver
	secret  string
	uid     string
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	h := &testHarness{
		res:    &stubResolver{},
		secret: testSecret,
		uid:    "U123ABC",
	}
	cfg := SlackConfig{
		Enabled:        true,
		BotToken:       "xoxb-t",
		SigningSecret:  testSecret,
		ChannelID:      "C1",
		AllowedUserIDs: []string{h.uid},
	}
	h.handler = NewInteractionsHandler(cfg, h.res.resolve, slog.Default())
	return h
}

// signedFormRequest builds the form-encoded block_actions POST the way
// Slack sends it, signed with the given secret/verb overrides.
func (h *testHarness) signedFormRequest(t *testing.T, actionValue, signature string, tsOverride int64) *http.Request {
	t.Helper()
	payload := fmt.Sprintf(
		`{"type":"block_actions","user":{"id":"%s"},"actions":[{"action_id":"a1","value":"%s"}],"response_url":"https://hooks.slack.test/responses/xyz"}`,
		h.uid, actionValue)
	form := url.Values{"payload": {payload}}
	body := form.Encode()

	ts := tsOverride
	if ts == 0 {
		ts = time.Now().Unix()
	}
	if signature == "" {
		mac := hmac.New(sha256.New, []byte(h.secret))
		mac.Write([]byte("v0:" + strconv.FormatInt(ts, 10) + ":" + body))
		signature = "v0=" + hex.EncodeToString(mac.Sum(nil))
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/slack/interactions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Slack-Signature", signature)
	return req
}

func TestInteractionsRejectsBadSignature(t *testing.T) {
	h := newTestHarness(t)
	req := h.signedFormRequest(t, ActionValue("r1", VerbAllow), "v0deadbeef", 0)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
	if len(h.res.snapshot()) != 0 {
		t.Fatalf("resolve called on a bad signature: %+v", h.res.snapshot())
	}
}

func TestInteractionsRejectsStaleTimestamp(t *testing.T) {
	h := newTestHarness(t)
	oldTS := time.Now().Add(-6 * time.Minute).Unix() // outside the 5-min window
	req := h.signedFormRequest(t, ActionValue("r1", VerbAllow), "", oldTS)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 for a stale timestamp", rec.Code)
	}
}

func TestInteractionsAcceptsFreshTimestamp(t *testing.T) {
	h := newTestHarness(t)
	freshTS := time.Now().Add(-4 * time.Minute).Unix() // inside the window
	req := h.signedFormRequest(t, ActionValue("r1", VerbAllow), "", freshTS)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 for a fresh in-window signature", rec.Code)
	}
	h.waitResolved(t)
	calls := h.res.snapshot()
	if len(calls) != 1 || calls[0].verb != VerbAllow {
		t.Fatalf("resolve calls = %+v, want one allow", calls)
	}
}

func TestInteractionsVerifiesRawBody(t *testing.T) {
	// Tampering with the body AFTER signing must fail: the signature is
	// computed over the RAW body, pre-deserialization.
	h := newTestHarness(t)
	req := h.signedFormRequest(t, ActionValue("r1", VerbAllow), "", 0)
	// Flip the action verb inside the body without re-signing.
	body, _ := url.ParseQuery(reqPostForm(t, req))
	body.Set("payload", strings.Replace(body.Get("payload"), VerbAllow, VerbAlways, 1))
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/slack/interactions", strings.NewReader(body.Encode()))
	req2.Header = req.Header.Clone()
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req2)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered body: got %d, want 401", rec.Code)
	}
}

func reqPostForm(t *testing.T, r *http.Request) string {
	t.Helper()
	buf := make([]byte, 4096)
	n, _ := r.Body.Read(buf)
	return string(buf[:n])
}

func TestInteractionsResolvesWithSlackApprover(t *testing.T) {
	h := newTestHarness(t)
	req := h.signedFormRequest(t, ActionValue("r1", VerbAlways), "", 0)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	h.waitResolved(t)
	calls := h.res.snapshot()
	c := calls[0]
	if c.id != "r1" || c.verb != VerbAlways {
		t.Fatalf("resolve = %+v, want r1/always", c)
	}
	if c.approver.Kind != acp.ApproverKindSlack || c.approver.Detail != h.uid {
		t.Fatalf("approver = %+v, want {slack %s}", c.approver, h.uid)
	}
	if c.approver.String() != "slack:"+h.uid {
		t.Fatalf("approver.String() = %q, want slack:%s", c.approver.String(), h.uid)
	}
}

func TestInteractionsRejectsDisallowedUser(t *testing.T) {
	h := newTestHarness(t)
	h.uid = "U999EVIL" // signed by the same app but not in AllowedUserIDs
	req := h.signedFormRequest(t, ActionValue("r1", VerbAllow), "", 0)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	// 200 ack (never a 500-retry loop) but NO resolve.
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	time.Sleep(50 * time.Millisecond)
	if len(h.res.snapshot()) != 0 {
		t.Fatalf("disallowed user reached the relay: %+v", h.res.snapshot())
	}
}

func TestInteractionsUnknownRequestStill200(t *testing.T) {
	// Unknown/expired requestID (the 120s fail-closed timeout already
	// fired upstream): the resolve returns ErrRelayNotFound; the handler
	// must have acked 200 already (3 s budget) and must NOT retry or 500.
	h := newTestHarness(t)
	h.res.noPolicy = func(id, verb string) error { return acp.ErrRelayNotFound }
	req := h.signedFormRequest(t, ActionValue("expired", VerbDeny), "", 0)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 for an unknown request id", rec.Code)
	}
	h.waitResolved(t)
	if calls := h.res.snapshot(); len(calls) != 1 || calls[0].id != "expired" {
		t.Fatalf("resolve calls = %+v", h.res.snapshot())
	}
}

func TestInteractionsRejectsBadActionGrammar(t *testing.T) {
	h := newTestHarness(t)
	for _, bad := range []string{
		"r1:allow",            // missing perm: prefix
		"perm:r1:open_sesame", // unknown verb
	} {
		req := h.signedFormRequest(t, bad, "", 0)
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("action %q: got %d, want 200 (polite ack)", bad, rec.Code)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if len(h.res.snapshot()) != 0 {
		t.Fatalf("bad grammar reached the relay: %+v", h.res.snapshot())
	}
}

func TestInteractionsNonBlockActionsTypePlainAck(t *testing.T) {
	h := newTestHarness(t)
	form := url.Values{"payload": {`{"type":"interactive_message","user":{"id":"U123ABC"},"actions":[{"value":"perm:r1:allow"}]}`}}
	body := form.Encode()
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write([]byte("v0:" + strconv.FormatInt(ts, 10) + ":" + body))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/slack/interactions", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	time.Sleep(50 * time.Millisecond)
	if len(h.res.snapshot()) != 0 {
		t.Fatalf("non-block_actions payload resolved: %+v", h.res.snapshot())
	}
}

func TestInteractionsSecretNeverLogged(t *testing.T) {
	// Exercise a signature failure and a resolve failure with a capturing
	// logger; the signing secret must not appear anywhere in the output.
	// The capture writer is mutex-guarded: the async resolve goroutine
	// logs while the test reads (race-safe capture).
	var mu sync.Mutex
	var sb strings.Builder
	safe := writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return sb.Write(p)
	})
	read := func() string {
		mu.Lock()
		defer mu.Unlock()
		return sb.String()
	}
	h := newTestHarness(t)
	h.handler.logger = slog.New(slog.NewTextHandler(safe, nil))
	h.handler.SetResolver(func(id, verb string, a acp.Approver) error {
		return fmt.Errorf("wrapped: %w", acp.ErrRelayNotFound)
	})
	req := h.signedFormRequest(t, ActionValue("r1", "bogus-verb-but-signed"), "v0bad", 0)
	h.handler.ServeHTTP(httptest.NewRecorder(), req)
	req2 := h.signedFormRequest(t, ActionValue("r2", VerbAllow), "", 0)
	h.handler.ServeHTTP(httptest.NewRecorder(), req2)
	// Only request 2 reaches the resolver (request 1 fails the signature).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(h.res.snapshot()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Give the Warn log line (if any) a beat to land, then read the capture.
	time.Sleep(50 * time.Millisecond)
	got := read()
	if strings.Contains(got, testSecret) || strings.Contains(got, "xoxb") {
		t.Fatalf("secret material leaked into logs:\n%s", got)
	}
}

// writerFunc adapts a func to io.Writer.
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// waitResolved polls until the async resolve landed (bounded).
func (h *testHarness) waitResolved(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(h.res.snapshot()) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("async resolve never landed")
}
