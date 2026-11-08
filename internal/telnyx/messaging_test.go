package telnyx

// Gate tests for contract C5 (master.md, quoted verbatim): the Telnyx
// messaging client. Written BEFORE implementation (00T-gate-tests.md,
// T5 telnyx half). RED until leaf 12 lands messaging.go.
//
// Pinned signatures:
//
//	type Message struct{ ID, From, To, Text, Direction, Status string;
//	                    ReceivedAt time.Time }
//	func (c *Client) SendSMS(ctx, from, to, text) (id string, err error)
//
// SendSMS parses data.id from the messaging API response and REJECTS
// non-E.164 input (the E.164 gate). Webhook payload parsing:
// "message.received" (inbound text) and "message.finalized" (terminal
// DLR) both build Message values; Status for DLR comes from the payload.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestClient dials a Client at the stub base URL. The constructor
// shape is the leaf's choice; the contract pins SendSMS + payload
// parsing, so the test uses the package's own constructor seam and the
// stub only fixes the HTTP boundary. RED until leaf 12 defines both.
func newTestClient(t *testing.T, base string) *Client {
	t.Helper()
	c, err := NewClient(base)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// TestSendSMSParsesDataID pins SendSMS: POST to the messages API, parse
// data.id from the JSON response body, return it.
func TestSendSMSParsesDataID(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": {"id": "msg-abc-123", "to": "+15550000016",
			"from": "+15559999998", "text": "sms from meept"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	id, err := c.SendSMS(context.Background(), "+15559999998", "+15550000016", "sms from meept")
	if err != nil {
		t.Fatalf("SendSMS: %v", err)
	}
	if id != "msg-abc-123" {
		t.Errorf("SendSMS id = %q, want data.id %q", id, "msg-abc-123")
	}
	if gotPath != "/v2/messages" {
		t.Errorf("POST path = %q, want /v2/messages", gotPath)
	}
}

// TestSendSMSRejectsNonE164 pins the E.164 gate: from/to that are not
// E.164 are refused client-side, with no HTTP call placed.
func TestSendSMSRejectsNonE164(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"data": {"id": "x"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	for _, tc := range []struct{ from, to string }{
		{"5550000017", "+15550000018"},   // from missing +
		{"+15550000019", "1-555-000-00"}, // to not E.164
		{"", "+15550000020"},             // empty from
	} {
		if _, err := c.SendSMS(context.Background(), tc.from, tc.to, "m"); err == nil {
			t.Errorf("SendSMS(%q, %q) = nil error, want E.164 rejection", tc.from, tc.to)
		}
	}
	if called {
		t.Errorf("E.164-invalid SendSMS reached the HTTP API; the gate must refuse before dialing")
	}
}

// TestParseMessageReceived pins the inbound webhook payload parser: a
// "message.received" event body builds a Message with Direction inbound,
// the text, and E.164 from/to.
func TestParseMessageReceived(t *testing.T) {
	payload := []byte(`{"data": {
		"event_type": "message.received",
		"payload": {
			"id": "inb-1",
			"from": "+15550000021",
			"to": "+15559999997",
			"text": "reply yes to confirm",
			"received_at": "2026-11-08T12:00:00Z"
		}
	}}`)

	msg, err := ParseWebhookEvent(payload)
	if err != nil {
		t.Fatalf("ParseWebhookEvent: %v", err)
	}
	if msg.ID != "inb-1" || msg.From != "+15550000021" || msg.To != "+15559999997" {
		t.Errorf("Message identity = %+v, want (inb-1, +15550000021 -> +15559999997)", msg)
	}
	if msg.Text != "reply yes to confirm" {
		t.Errorf("Text = %q, want inbound body", msg.Text)
	}
	if msg.Direction != "inbound" {
		t.Errorf("Direction = %q, want inbound for message.received", msg.Direction)
	}
}

// TestParseMessageFinalized pins the DLR payload parser (the leaf's
// canonical Given/When/Then: payload message.finalized status=failed;
// When parse; Then Message.Status=="failed").
func TestParseMessageFinalized(t *testing.T) {
	payload := []byte(`{"data": {
		"event_type": "message.finalized",
		"payload": {
			"id": "out-9",
			"from": "+15559999996",
			"to": "+15550000022",
			"status": "failed"
		}
	}}`)

	msg, err := ParseWebhookEvent(payload)
	if err != nil {
		t.Fatalf("ParseWebhookEvent: %v", err)
	}
	if msg.Status != "failed" {
		t.Errorf("Message.Status = %q, want %q (DLR feeds the chain's failure detection, C5)",
			msg.Status, "failed")
	}
	if msg.Direction != "outbound" {
		t.Errorf("Direction = %q, want outbound for a finalized DLR", msg.Direction)
	}
}
