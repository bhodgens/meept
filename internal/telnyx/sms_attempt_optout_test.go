package telnyx

// New tests for leaf 02-send-leg/02: SMSAttempt sends through SendSMS2
// (Idempotency-Key header + per-message webhook_url from config), and
// consults contact.Record.MessagingOptOut BEFORE any HTTP call with the
// named contact.ErrOptedOut refusal. Existing sms_attempt_test.go is
// untouched.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/contact"
	"time"
)

type captureSMS struct {
	mu       sync.Mutex
	hits     int
	header   http.Header
	body     map[string]any
	bodyRaw  string
	respCode int
	respBody string
}

func (st *captureSMS) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		st.mu.Lock()
		st.hits++
		st.header = r.Header.Clone()
		st.bodyRaw = string(body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		st.body = m
		code, resp := st.respCode, st.respBody
		st.mu.Unlock()
		if code == 0 {
			code = http.StatusOK
		}
		if resp == "" {
			resp = `{"data":{"id":"msg-cap-1","status":"queued"}}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = io.WriteString(w, resp)
	}
}

func (st *captureSMS) snapshot() (int, http.Header, map[string]any) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.hits, st.header, st.body
}

// TestSMSAttemptUsesSendSMS2Pins pins the migrated send path: the
// attempt posts through SendSMS2 — a fresh Idempotency-Key header is
// present and webhook_url carries the configured per-message URL.
func TestSMSAttemptUsesSendSMS2Pins(t *testing.T) {
	st := &captureSMS{}
	srv := httptest.NewServer(st.handler())
	t.Cleanup(srv.Close)
	client, err := NewClient(srv.URL, WithFromNumber("+15559001"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	att := NewSMSAttempt(client, SMSAttemptConfig{
		FromNumber:  "+15559001",
		WaitSeconds: 1,
		WebhookURL:  "https://webhook.example/api/v1/voice/messaging/webhook",
	}, nil)
	go func() {
		// Resolve the DLR promptly so the bounded wait ends delivered.
		<-time.After(50 * time.Millisecond)
		_ = att.OnFinalized(Message{ID: "msg-cap-1", Status: "delivered", Direction: "outbound"})
	}()

	outcome, err := att.Attempt(context.Background(),
		contact.Record{ID: "c-1", Phone: "+15559002"}, "hi")
	if err != nil || outcome != contact.OutcomeDelivered {
		t.Fatalf("Attempt = %q %v, want delivered nil", outcome, err)
	}
	hits, hdr, body := st.snapshot()
	if hits != 1 {
		t.Fatalf("captured hits = %d, want 1", hits)
	}
	if hdr.Get("Idempotency-Key") == "" {
		t.Fatal("Idempotency-Key header missing — attempt must route through SendSMS2")
	}
	if got, _ := body["webhook_url"].(string); got != "https://webhook.example/api/v1/voice/messaging/webhook" {
		t.Fatalf("webhook_url = %v, want the configured per-message URL", body["webhook_url"])
	}
	if got, _ := body["to"].(string); got != "+15559002" {
		t.Fatalf("to = %v, want the record phone", body["to"])
	}
}

// TestSMSAttemptOmitsWebhookWhenUnset pins the D3 posture: with an empty
// WebhookURL config the webhook_url key is ABSENT from the body (the
// legacy/disabled-default shape stays byte-compatible).
func TestSMSAttemptOmitsWebhookWhenUnset(t *testing.T) {
	st := &captureSMS{}
	srv := httptest.NewServer(st.handler())
	t.Cleanup(srv.Close)
	client, err := NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	att := NewSMSAttempt(client, SMSAttemptConfig{
		FromNumber:  "+15559003",
		WaitSeconds: 1,
	}, nil)
	go func() {
		<-time.After(50 * time.Millisecond)
		_ = att.OnFinalized(Message{ID: "msg-cap-1", Status: "delivered", Direction: "outbound"})
	}()
	if _, err := att.Attempt(context.Background(),
		contact.Record{ID: "c-2", Phone: "+15559004"}, "hi"); err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	_, _, body := st.snapshot()
	if _, present := body["webhook_url"]; present {
		t.Fatalf("webhook_url present (%v), want key omitted when unset", body["webhook_url"])
	}
}

// TestSMSAttemptOptOutRefusesBeforeDial pins the D5 guard: a record with
// MessagingOptOut set is refused with contact.ErrOptedOut wrapped,
// outcome unavailable, and ZERO HTTP requests placed.
func TestSMSAttemptOptOutRefusesBeforeDial(t *testing.T) {
	st := &captureSMS{}
	srv := httptest.NewServer(st.handler())
	t.Cleanup(srv.Close)
	client, err := NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	att := NewSMSAttempt(client, SMSAttemptConfig{FromNumber: "+15559005", WaitSeconds: 1}, nil)
	rec := contact.Record{ID: "c-stop", Phone: "+15559006", MessagingOptOut: true}
	outcome, err := att.Attempt(context.Background(), rec, "you should not get this")
	if !errors.Is(err, contact.ErrOptedOut) {
		t.Fatalf("err = %v, want errors.Is(..., contact.ErrOptedOut)", err)
	}
	if outcome != contact.OutcomeUnavailable {
		t.Fatalf("outcome = %q, want %q", outcome, contact.OutcomeUnavailable)
	}
	if !strings.Contains(err.Error(), "c-stop") {
		t.Fatalf("err = %q, want the refusing contact id named", err.Error())
	}
	if hits, _, _ := st.snapshot(); hits != 0 {
		t.Fatalf("HTTP hits = %d, want 0 — the guard runs BEFORE dial", hits)
	}
	if att.pending.Len() != 0 {
		t.Fatalf("pending DLR map = %d, want 0 — a refusal opens no DLR slot", att.pending.Len())
	}
}
