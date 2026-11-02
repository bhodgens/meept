package telnyx

// Gate tests for contract C4 (master.md, quoted verbatim). Borrowed-shape
// client: hand-rolled HTTP, no SDK. These tests are parked .staged and
// activate at leaf 02.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---- ParseCallEvent (C4) ----

// TestParseCallEvent covers call.initiated|answered|hangup parsing with the
// flexible from/to shape (plain string or {"phone_number": "..."}), and the
// streaming.* events parsing as no-ops instead of errors.
func TestParseCallEvent(t *testing.T) {
	flat := `{"data":{"event_type":"call.initiated","payload":{"call_control_id":"cc-1","call_session_id":"cs-1","from":"+15551230001","to":"+15551230002","direction":"incoming"}}}`
	ev, err := ParseCallEvent([]byte(flat))
	if err != nil {
		t.Fatalf("ParseCallEvent(flat): %v", err)
	}
	if ev.EventType != "call.initiated" || ev.CallControlID != "cc-1" || ev.CallSessionID != "cs-1" {
		t.Errorf("parsed = %+v, want initiated/cc-1/cs-1", ev)
	}
	if ev.From != "+15551230001" || ev.To != "+15551230002" || ev.Direction != "incoming" {
		t.Errorf("parsed = %+v, want from/to/direction exact", ev)
	}

	nested := `{"data":{"event_type":"call.answered","payload":{"call_control_id":"cc-2","from":{"phone_number":"+15551230001"},"to":{"phone_number":"+15551230002"},"direction":"outgoing"}}}`
	ev, err = ParseCallEvent([]byte(nested))
	if err != nil {
		t.Fatalf("ParseCallEvent(nested phone_number): %v", err)
	}
	if ev.EventType != "call.answered" || ev.From != "+15551230001" || ev.To != "+15551230002" || ev.Direction != "outgoing" {
		t.Errorf("parsed = %+v, want nested from/to unwrapped", ev)
	}

	for _, et := range []string{"streaming.started", "streaming.stopped", "streaming.failed"} {
		body := `{"data":{"event_type":"` + et + `","payload":{"call_control_id":"cc-1"}}}`
		ev, err := ParseCallEvent([]byte(body))
		if err != nil {
			t.Errorf("ParseCallEvent(%s): %v, want no-op parse (redelivery otherwise)", et, err)
			continue
		}
		if ev.EventType != et {
			t.Errorf("ParseCallEvent(%s) EventType = %q, want preserved", et, ev.EventType)
		}
	}

	bad := `{"data":{"event_type":"call.fax.finished","payload":{}}}`
	if _, err := ParseCallEvent([]byte(bad)); err == nil {
		t.Errorf("ParseCallEvent accepted unknown event_type, want error")
	}
	garbage := []byte("not json")
	if _, err := ParseCallEvent(garbage); err == nil {
		t.Errorf("ParseCallEvent accepted invalid JSON, want error")
	}
}

// ---- signature verification (C4) ----

// signFor builds the Telnyx v2 style signature: Ed25519 base64 over
// "{timestamp}|{message}" where message is the body (webhook) or the stream
// url (media socket).
func signFor(t *testing.T, priv ed25519.PrivateKey, ts string, msg []byte) string {
	t.Helper()
	signed := append(append([]byte(ts), '|'), msg...)
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, signed))
}

func sigHeaders(sig, ts string) http.Header {
	h := http.Header{}
	h.Set("Telnyx-Signature-Ed25519", sig)
	h.Set("Telnyx-Timestamp", ts)
	return h
}

func TestVerifyWebhookSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	body := []byte(`{"data":{"event_type":"call.initiated","payload":{"call_control_id":"cc-9"}}}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	if err := VerifyWebhookSignature(body, sigHeaders(signFor(t, priv, ts, body), ts), pub); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	tampered := append(append([]byte{}, body...), 'x')
	if err := VerifyWebhookSignature(tampered, sigHeaders(signFor(t, priv, ts, body), ts), pub); err == nil {
		t.Errorf("tampered body accepted, want signature mismatch error")
	}

	stale := strconv.FormatInt(time.Now().Add(-6*time.Minute).Unix(), 10)
	if err := VerifyWebhookSignature(body, sigHeaders(signFor(t, priv, stale, body), stale), pub); err == nil {
		t.Errorf("stale timestamp accepted, want timestamp skew error")
	}

	if err := VerifyWebhookSignature(body, sigHeaders("!!!not-base64!!!", ts), pub); err == nil {
		t.Errorf("garbage signature accepted, want error")
	}
}

func TestVerifyMediaSocket(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	streamURL := "wss://tunnel.example/media"
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	if err := VerifyMediaSocket(sigHeaders(signFor(t, priv, ts, []byte(streamURL)), ts), streamURL, pub); err != nil {
		t.Fatalf("valid media socket signature rejected: %v", err)
	}

	other := "wss://tunnel.example/media/other"
	if err := VerifyMediaSocket(sigHeaders(signFor(t, priv, ts, []byte(streamURL)), ts), other, pub); err == nil {
		t.Errorf("signature over a different url accepted, want error")
	}
}

// ---- DialOut payload keys (C4, exact) ----

// TestDialOutPayloadKeys pins the dial request body to exactly the four
// contract keys: connection_id, to, from, timeout_secs.
func TestDialOutPayloadKeys(t *testing.T) {
	var gotBody map[string]any
	var gotAuth, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(r.Body)
		_ = json.Unmarshal(buf.Bytes(), &gotBody)
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{"data":{"call_control_id":"cc-dial-1"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	ccid, err := c.DialOut(t.Context(), "+15551230001", "+15551239999", "conn-1")
	if err != nil {
		t.Fatalf("DialOut: %v", err)
	}
	if ccid != "cc-dial-1" {
		t.Errorf("DialOut ccid = %q, want cc-dial-1", ccid)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", gotAuth)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
	want := map[string]any{
		"connection_id": "conn-1",
		"to":            "+15551230001",
		"from":          "+15551239999",
		"timeout_secs":  float64(30),
	}
	if len(gotBody) != len(want) {
		t.Errorf("dial body keys = %v, want exactly %v (C4: connection_id,to,from,timeout_secs)", gotBody, want)
	}
	for k, v := range want {
		if gotBody[k] != v {
			t.Errorf("dial body[%q] = %v, want %v", k, gotBody[k], v)
		}
	}
}

// ---- StartStreaming body (C4) ----

// TestStartStreamingBody pins stream_bidirectional_mode=rtp plus the codec
// and sampling rate on the streaming_start request, for both the L16/16000
// default and the PCMU/8000 fallback.
func TestStartStreamingBody(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(r.Body)
		_ = json.Unmarshal(buf.Bytes(), &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")

	cases := []struct {
		name  string
		codec Codec
	}{
		{"L16 default", Codec{BidirectionalCodec: "L16", SamplingRate: 16000}},
		{"PCMU fallback", Codec{BidirectionalCodec: "PCMU", SamplingRate: 8000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotBody = nil
			if err := c.StartStreaming(t.Context(), "cc-stream-1", "wss://tunnel.example/media", tc.codec); err != nil {
				t.Fatalf("StartStreaming: %v", err)
			}
			if gotBody["stream_bidirectional_mode"] != "rtp" {
				t.Errorf("stream_bidirectional_mode = %v, want rtp", gotBody["stream_bidirectional_mode"])
			}
			if gotBody["stream_bidirectional_codec"] != tc.codec.BidirectionalCodec {
				t.Errorf("stream_bidirectional_codec = %v, want %s", gotBody["stream_bidirectional_codec"], tc.codec.BidirectionalCodec)
			}
			if gotBody["stream_bidirectional_sampling_rate"] != float64(tc.codec.SamplingRate) {
				t.Errorf("stream_bidirectional_sampling_rate = %v, want %d", gotBody["stream_bidirectional_sampling_rate"], tc.codec.SamplingRate)
			}
			if _, ok := gotBody["stream_url"]; !ok && gotBody["stream_bidirectional_mode"] == nil {
				t.Errorf("streaming_start body carried no recognizable keys: %v", gotBody)
			}
		})
	}
}
