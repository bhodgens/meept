package telnyx

// 01-webhook-hardening/00T §P — payload normalizer gate tests.
//
// Pins the frozen contract (master.md §1 item 2):
//
//	func ParseMessagingEvent(raw []byte) (Message, error)
//	func NormalizeE164US(s string) string
//	type Message struct { Event, EventID, Direction, Status string;
//	  IssuedAt time.Time; From, To string; Text string;
//	  ProviderMessageID string; MediaURLs []string;
//	  Errors []struct{ Code, Description string } }
//
// Pinned field semantics (this file is the executable spec for leaf 02):
//   - Event carries the RAW Telnyx data.event_type string ("message.received",
//     ...) so the handler's noop classification can inspect it.
//   - EventID = data.id (the webhook EVENT uuid — the nonce/dedupe key,
//     strategist normalizer.go:221 EventID: data.ID).
//   - ProviderMessageID = data.payload.id (the provider MESSAGE uuid the
//     send API returns — the DLR resolution key, normalizer.go:228
//     MessageID: data.Payload.ID; the master's "// data.id" comment refers
//     to the payload data object, matching the send-leg id contract).
//   - Direction: "inbound" for message.received (by EVENT TYPE, never the
//     payload.direction field), "outbound" otherwise — mirrors the
//     existing v1 ParseWebhookEvent routing the handler depends on.
//   - Status: sent/queued/sending pass through; message.finalized passes
//     payload.status through verbatim (delivered|failed|undelivered|
//     expired — meept KEEPS the raw value, unlike strategist's collapsed
//     map); message.failed / message.delivery_failed set Status "error".
//   - Unknown/noop event types (message.created/deleted, non-SMS) return
//     an error matching ErrUnknownEvent so the handler can 200-ack them;
//     malformed JSON must NOT match ErrUnknownEvent (handler maps it 400).
//
// Tolerant extractors ported from strategist-usa
// pkg/services/telnyx_provider.go:674-706 (extractStringOrPhoneField /
// extractToField); status map ported from go-web-hooks internal/webhook/
// normalizer.go:247-293; normalizeE164 ported from :297-325; media
// scheme filter from normalizeTelnyx :186-200; missing-payload-id guard
// from :140-142.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// cannedInbound is a REAL-shape Telnyx message.received webhook: from is
// an OBJECT {phone_number}, to is an ARRAY of objects (the type bug the
// old webhookEnvelope had, master survey :54-56). Numbers are
// human-formatted to also prove E.164 normalization inside the parse.
const cannedInbound = `{
  "data": {
    "event_type": "message.received",
    "recorded_at": "2026-11-10T18:00:01.500Z",
    "id": "evt-inb-0001",
    "occurred_at": "2026-11-10T18:00:00Z",
    "payload": {
      "id": "msg-inb-0001",
      "direction": "inbound",
      "from": {"phone_number": "+1 (555) 111-2222", "carrier": "Verizon", "line_type": "wireless", "region": "US"},
      "to": [
        {"phone_number": "555 999 8888", "carrier": "Telnyx", "line_type": "longcode", "name": "meept-outbound"},
        {"phone_number": "+15559999999", "carrier": "Telnyx", "line_type": "longcode"}
      ],
      "text": "is this still available?",
      "type": "SMS",
      "project_id": "proj-1",
      "to_name": null
    }
  }
}`

func TestParseMessagingEvent_InboundRealShape(t *testing.T) {
	m, err := ParseMessagingEvent([]byte(cannedInbound))
	if err != nil {
		t.Fatalf("ParseMessagingEvent(canned inbound) = %v, want nil", err)
	}
	if m.Event != "message.received" {
		t.Errorf("Event = %q, want %q (raw event_type)", m.Event, "message.received")
	}
	if m.EventID != "evt-inb-0001" {
		t.Errorf("EventID = %q, want %q (data.id = nonce key)", m.EventID, "evt-inb-0001")
	}
	if m.ProviderMessageID != "msg-inb-0001" {
		t.Errorf("ProviderMessageID = %q, want %q (data.payload.id = DLR key)", m.ProviderMessageID, "msg-inb-0001")
	}
	if m.Direction != "inbound" {
		t.Errorf("Direction = %q, want %q", m.Direction, "inbound")
	}
	if m.From != "+15551112222" {
		t.Errorf("From = %q, want %q (normalized E.164 from OBJECT {phone_number})", m.From, "+15551112222")
	}
	if m.To != "+15559998888" {
		t.Errorf("To = %q, want %q (first entry of to ARRAY, 10-digit US-biased)", m.To, "+15559998888")
	}
	if m.Text != "is this still available?" {
		t.Errorf("Text = %q", m.Text)
	}
	wantIssued := time.Date(2026, 11, 10, 18, 0, 0, 0, time.UTC)
	if !m.IssuedAt.Equal(wantIssued) {
		t.Errorf("IssuedAt = %v, want %v (occurred_at RFC3339)", m.IssuedAt, wantIssued)
	}
	if len(m.Errors) != 0 {
		t.Errorf("Errors = %+v, want none", m.Errors)
	}
}

func TestParseMessagingEvent_TolerantStringForms(t *testing.T) {
	// Legacy/alternate shapes must parse too (extractStringOrPhoneField /
	// extractToField, telnyx_provider.go:674-706): from plain string, to
	// plain string, to array-of-strings.
	body := `{"data":{"event_type":"message.received","id":"evt-tol-1",
	  "payload":{"id":"msg-tol-1","from":"+15551111111","to":["+15552222222"],"text":"hi"}}}`
	m, err := ParseMessagingEvent([]byte(body))
	if err != nil {
		t.Fatalf("tolerant string from/array-of-strings to = %v, want nil", err)
	}
	if m.From != "+15551111111" || m.To != "+15552222222" {
		t.Errorf("From/To = %q/%q, want +15551111111/+15552222222", m.From, m.To)
	}

	body2 := `{"data":{"event_type":"message.received","id":"evt-tol-2",
	  "payload":{"id":"msg-tol-2","from":"5551111111","to":"5552222222","text":"hi"}}}`
	m2, err := ParseMessagingEvent([]byte(body2))
	if err != nil {
		t.Fatalf("bare 10-digit string forms = %v, want nil", err)
	}
	if m2.From != "+15551111111" || m2.To != "+15552222222" {
		t.Errorf("From/To = %q/%q, want +15551111111/+15552222222 (US bias)", m2.From, m2.To)
	}
}

func TestParseMessagingEvent_DirectionFromEventTypeNotPayload(t *testing.T) {
	// message.received maps inbound BY EVENT TYPE (normalizer.go:249-250);
	// the payload.direction field must not flip it.
	body := `{"data":{"event_type":"message.received","id":"evt-dir-1",
	  "payload":{"id":"msg-dir-1","direction":"outbound","from":{"phone_number":"+15551111111"},"to":[{"phone_number":"+15552222222"}]}}}`
	m, err := ParseMessagingEvent([]byte(body))
	if err != nil {
		t.Fatalf("ParseMessagingEvent = %v, want nil", err)
	}
	if m.Direction != "inbound" {
		t.Errorf("Direction = %q, want inbound (event_type governs, not payload.direction)", m.Direction)
	}
}

func TestParseMessagingEvent_StatusMap(t *testing.T) {
	// Ported table (strategist normalizer.go:247-265), meept-pinned
	// variants per contract §2.
	cases := []struct {
		eventType   string
		payloadStat string
		wantDir     string
		wantStatus  string
	}{
		{"message.received", "received", "inbound", ""},
		{"message.sent", "sent", "outbound", "sent"},
		{"message.queued", "queued", "outbound", "queued"},
		{"message.sending", "sending", "outbound", "sending"},
		{"message.finalized", "delivered", "outbound", "delivered"},
		{"message.finalized", "failed", "outbound", "failed"},
		{"message.finalized", "undelivered", "outbound", "undelivered"},
		{"message.finalized", "expired", "outbound", "expired"},
		{"message.failed", "failed", "outbound", "error"},
		{"message.delivery_failed", "undelivered", "outbound", "error"},
	}
	for _, tc := range cases {
		body := []byte(`{"data":{"event_type":"` + tc.eventType + `","id":"evt-map",` +
			`"payload":{"id":"msg-map","status":"` + tc.payloadStat + `",` +
			`"from":{"phone_number":"+15551111111"},"to":[{"phone_number":"+15552222222"}]}}}`)
		m, err := ParseMessagingEvent(body)
		if err != nil {
			t.Errorf("%s/%s: err = %v, want nil", tc.eventType, tc.payloadStat, err)
			continue
		}
		if m.Direction != tc.wantDir {
			t.Errorf("%s/%s: Direction = %q, want %q", tc.eventType, tc.payloadStat, m.Direction, tc.wantDir)
		}
		if m.Status != tc.wantStatus {
			t.Errorf("%s/%s: Status = %q, want %q", tc.eventType, tc.payloadStat, m.Status, tc.wantStatus)
		}
	}
}

func TestParseMessagingEvent_FinalizedDeliveredRealShape(t *testing.T) {
	// Canned message.finalized delivered (real DLR shape with cost + parts).
	body := `{
	  "data": {
	    "event_type": "message.finalized",
	    "id": "evt-fin-deliv",
	    "occurred_at": "2026-11-10T18:05:00Z",
	    "payload": {
	      "id": "msg-out-777",
	      "direction": "outbound",
	      "from": {"phone_number": "+15559999999", "carrier": "Telnyx", "line_type": "longcode"},
	      "to": [{"phone_number": "+15553333333", "carrier": "AT&T", "line_type": "wireless"}],
	      "text": "your appointment is confirmed",
	      "type": "SMS",
	      "status": "delivered",
	      "to_carrier": "AT&T",
	      "cost": {"amount": "0.007900", "currency": "USD"},
	      "encoding": "ascii",
	      "parts": 1
	    }
	  },
	  "meta": {"attempt_number": 1, "delivered_to": "api.telnyx.com"}
	}`
	m, err := ParseMessagingEvent([]byte(body))
	if err != nil {
		t.Fatalf("finalized delivered = %v, want nil", err)
	}
	if m.Status != "delivered" || m.Direction != "outbound" || m.ProviderMessageID != "msg-out-777" {
		t.Errorf("got Status=%q Direction=%q MsgID=%q, want delivered/outbound/msg-out-777",
			m.Status, m.Direction, m.ProviderMessageID)
	}
	if m.To != "+15553333333" {
		t.Errorf("To = %q, want +15553333333", m.To)
	}
}

func TestParseMessagingEvent_FinalizedFailedErrorsArray(t *testing.T) {
	// Canned message.finalized failed with real Telnyx error object
	// (code/title/detail). Description pins title-first, detail-fallback
	// (strategist normalizeTelnyx :196-201 errorMessage semantics).
	body := `{
	  "data": {
	    "event_type": "message.finalized",
	    "id": "evt-fin-fail",
	    "payload": {
	      "id": "msg-out-888",
	      "from": {"phone_number": "+15559999999"},
	      "to": [{"phone_number": "+15554444444"}],
	      "status": "failed",
	      "errors": [
	        {"code": "30017", "title": "Sending not allowed", "detail": "Sending to this country is not allowed"},
	        {"code": "91000", "detail": "Carrier blocked"}
	      ]
	    }
	  }
	}`
	m, err := ParseMessagingEvent([]byte(body))
	if err != nil {
		t.Fatalf("finalized failed = %v, want nil", err)
	}
	if m.Status != "failed" || m.Direction != "outbound" {
		t.Errorf("Status/Direction = %q/%q, want failed/outbound", m.Status, m.Direction)
	}
	if len(m.Errors) != 2 {
		t.Fatalf("len(Errors) = %d, want 2 (%+v)", len(m.Errors), m.Errors)
	}
	if m.Errors[0].Code != "30017" || m.Errors[0].Description != "Sending not allowed" {
		t.Errorf("Errors[0] = %+v, want code 30017, Description from title", m.Errors[0])
	}
	if m.Errors[1].Code != "91000" || m.Errors[1].Description != "Carrier blocked" {
		t.Errorf("Errors[1] = %+v, want code 91000, Description falls back to detail", m.Errors[1])
	}
}

func TestParseMessagingEvent_ErrUnknownEvent(t *testing.T) {
	// Noop + unknown types ALL surface as ErrUnknownEvent so the handler
	// 200-acks to stop redelivery (posture table §H); malformed JSON must
	// stay distinguishable (400), asserted separately below.
	for _, et := range []string{"message.created", "message.deleted", "call.answered", "voice.recording.completed", "whatever", ""} {
		body := []byte(`{"data":{"event_type":"` + et + `","id":"evt-unk","payload":{"id":"msg-unk"}}}`)
		_, err := ParseMessagingEvent(body)
		if !errors.Is(err, ErrUnknownEvent) {
			t.Errorf("event_type %q: err = %v, want errors.Is(err, ErrUnknownEvent)", et, err)
		}
	}
}

func TestParseMessagingEvent_MalformedJSONNotErrUnknownEvent(t *testing.T) {
	for _, raw := range []string{"", "{", "not json at all", `{"data":`, `[]`} {
		_, err := ParseMessagingEvent([]byte(raw))
		if err == nil {
			t.Errorf("ParseMessagingEvent(%q) = nil error, want parse error", raw)
			continue
		}
		if errors.Is(err, ErrUnknownEvent) {
			t.Errorf("malformed JSON %q matched ErrUnknownEvent; the handler must map it 400, not 200-ack", raw)
		}
	}
}

func TestParseMessagingEvent_MissingMessageIDRejected(t *testing.T) {
	// Ported guard: strategist normalizeTelnyx :140-142 rejects a payload
	// with no message id (a parsed Message with no DLR key is useless).
	body := `{"data":{"event_type":"message.finalized","id":"evt-noid","payload":{"status":"delivered"}}}`
	if _, err := ParseMessagingEvent([]byte(body)); err == nil {
		t.Fatal("payload without id = nil error, want rejection")
	}
}

func TestParseMessagingEvent_MediaSchemeFilter(t *testing.T) {
	// MMS media: only http/https survive (strategist normalizeTelnyx
	// :186-200); empty URLs skipped; order preserved.
	body := `{"data":{"event_type":"message.received","id":"evt-mms",
	  "payload":{"id":"msg-mms",
	    "from":{"phone_number":"+15551111111"},
	    "to":[{"phone_number":"+15552222222"}],
	    "text":"look",
	    "media":[
	      {"url":"https://cdn.telnyx.com/a.png","content_type":"image/png","size":123},
	      {"url":"ftp://evil.example/x.png","content_type":"image/png","size":1},
	      {"url":"http://insecure.example/b.jpg","content_type":"image/jpeg","size":2},
	      {"url":"","content_type":"image/png","size":0},
	      {"url":"data:image/png;base64,AAAA","content_type":"image/png","size":3}
	    ]}}}`
	m, err := ParseMessagingEvent([]byte(body))
	if err != nil {
		t.Fatalf("MMS parse = %v, want nil", err)
	}
	want := []string{"https://cdn.telnyx.com/a.png", "http://insecure.example/b.jpg"}
	if len(m.MediaURLs) != len(want) {
		t.Fatalf("MediaURLs = %v, want %v (ftp/empty/data filtered)", m.MediaURLs, want)
	}
	for i := range want {
		if m.MediaURLs[i] != want[i] {
			t.Errorf("MediaURLs[%d] = %q, want %q", i, m.MediaURLs[i], want[i])
		}
	}
}

func TestParseMessagingEvent_BadOccurredAtNotFatal(t *testing.T) {
	// An unparseable/absent occurred_at must not fail the parse (the DLR
	// outcome is what the chain consumes). IssuedAt may be zero or a
	// now-fallback; assert only that it is not a garbage epoch.
	body := `{"data":{"event_type":"message.finalized","id":"evt-badts","occurred_at":"not-a-time",
	  "payload":{"id":"msg-badts","status":"delivered","from":{"phone_number":"+15551111111"}}}}`
	m, err := ParseMessagingEvent([]byte(body))
	if err != nil {
		t.Fatalf("bad occurred_at = %v, want nil", err)
	}
	if !m.IssuedAt.IsZero() && m.IssuedAt.Year() < 2020 {
		t.Errorf("IssuedAt = %v, want zero or recent fallback", m.IssuedAt)
	}
}

func TestParseMessagingEvent_EnvelopeWithoutDataTolerated(t *testing.T) {
	// Extra unknown fields never break the parse (tolerant normalizer);
	// json round-trip sanity of the canned fixtures themselves.
	for _, raw := range []string{cannedInbound} {
		if !json.Valid([]byte(raw)) {
			t.Fatalf("canned fixture is not valid JSON: %s", raw)
		}
	}
	body := `{"data":{"event_type":"message.received","id":"evt-x","future_field":{"nested":[1,2]},
	  "payload":{"id":"msg-x","from":{"phone_number":"+15551111111","weird":42},"to":[]}}}`
	m, err := ParseMessagingEvent([]byte(body))
	if err != nil {
		t.Fatalf("unknown fields = %v, want nil (tolerant parse)", err)
	}
	if m.To != "" {
		t.Errorf("To = %q, want empty for to=[] (extractToField first-entry guard)", m.To)
	}
}

func TestNormalizeE164US(t *testing.T) {
	// Ported table faithful to strategist normalizer.go:297-325
	// (normalizeE164): trim, strip non-digits, keep +, 11-digit-leading-1
	// passthrough, 10-digit US bias, everything else +'digits.
	cases := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"+15551111111", "+15551111111"},
		{"+1 (555) 111-2222", "+15551112222"},
		{"(555) 111-2222", "+15551112222"},    // 10-digit US bias
		{"5551112222", "+15551112222"},        // bare 10-digit US bias
		{"15551112222", "+15551112222"},       // 11-digit leading 1
		{"555-111-2222 x99", "+555111222299"}, // extension digits CONCATENATE then +'digits (port semantics: ALL non-digits stripped, strategist :310-324)
		{"+44 20 7946 0958", "+442079460958"},
		{"+15551111111", "+15551111111"},
		{"888", "+888"}, // short non-US: +'digits
	}
	for _, tc := range cases {
		if got := NormalizeE164US(tc.in); got != tc.want {
			t.Errorf("NormalizeE164US(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseMessagingEvent_NormalizesInsideParse(t *testing.T) {
	// From/To come out of ParseMessagingEvent ALREADY NormalizeE164US'd
	// (strategist normalizer.go:225-226 applies normalizeE164 at parse).
	body := `{"data":{"event_type":"message.received","id":"evt-n1",
	  "payload":{"id":"msg-n1","from":{"phone_number":"5551112222"},
	  "to":[{"phone_number":"1-555-999-8888"}]}}}`
	m, err := ParseMessagingEvent([]byte(body))
	if err != nil {
		t.Fatalf("parse = %v, want nil", err)
	}
	if m.From != "+15551112222" {
		t.Errorf("From = %q, want %s", m.From, "+15551112222")
	}
	if !strings.HasPrefix(m.To, "+1") {
		t.Errorf("To = %q, want normalized E.164 (+1-prefixed)", m.To)
	}
}
