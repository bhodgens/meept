package telnyx

// Gate tests for call recording (leaf 06, master "Recording" matrix
// row). Client-side half: StartRecording POSTs the dual-channel mp3
// record_start action; recording.saved is a real event ParseCallEvent
// must expose (NEVER classified as IgnoredEventError — that would
// swallow the URL capture). Ported in case-set from uptractor
// phone/core/internal/telnyx/recording_test.go (branch
// fix/coordinator-missed-hangup-watchdog), adapted to meept's
// NewClient(baseURL, apiKey) and the RecordOptions contract
// (frozen default {Format:"mp3", Channels:"dual"}).
//
// RED until leaf 06 (StartRecording / RecordOptions / RecordingSavedEvent
// / CallEvent.RecordingURL undefined).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// recordStub captures the HTTP request StartRecording issues.
type recordStub struct {
	path   string
	method string
	auth   string
	body   map[string]string
}

func startRecordStub(t *testing.T, code int, resp string) (*httptest.Server, *recordStub) {
	t.Helper()
	stub := &recordStub{body: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.path = r.URL.Path
		stub.method = r.Method
		stub.auth = r.Header.Get("Authorization")
		var raw map[string]string
		_ = json.NewDecoder(r.Body).Decode(&raw)
		stub.body = raw
		w.WriteHeader(code)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return srv, stub
}

// TestStartRecordingBody pins the request shape: POST
// /calls/{id}/actions/record_start with bearer auth and a payload of
// EXACTLY {format: mp3, channels: dual} (dual-channel keeps caller/agent
// separable for eval + brief accuracy).
func TestStartRecordingBody(t *testing.T) {
	srv, stub := startRecordStub(t, 200, `{"data":{"result":"ok"}}`)
	c := NewClient(srv.URL, "test-key")

	if err := c.StartRecording(context.Background(), "cc-1", RecordOptions{}); err != nil {
		t.Fatalf("StartRecording: %v", err)
	}
	if stub.path != "/calls/cc-1/actions/record_start" || stub.method != "POST" {
		t.Fatalf("request = %s %s, want POST /calls/cc-1/actions/record_start", stub.method, stub.path)
	}
	if stub.auth != "Bearer test-key" {
		t.Fatalf("auth = %q, want bearer test-key", stub.auth)
	}
	if len(stub.body) != 2 || stub.body["format"] != "mp3" || stub.body["channels"] != "dual" {
		t.Fatalf("body = %v, want exactly {format:mp3, channels:dual} (frozen default RecordOptions)", stub.body)
	}
}

// TestStartRecordingExplicitOptions pins the option struct: explicit
// RecordOptions ride the payload verbatim (the struct exists so future
// formats ship without a signature change).
func TestStartRecordingExplicitOptions(t *testing.T) {
	srv, stub := startRecordStub(t, 200, `{"data":{"result":"ok"}}`)
	c := NewClient(srv.URL, "k")
	opts := RecordOptions{Format: "wav", Channels: "single"}
	if err := c.StartRecording(context.Background(), "cc-2", opts); err != nil {
		t.Fatalf("StartRecording: %v", err)
	}
	if stub.body["format"] != "wav" || stub.body["channels"] != "single" {
		t.Fatalf("body = %v, want explicit options verbatim", stub.body)
	}
}

// TestStartRecordingNon2xx pins the error path: a non-2xx from the
// record_start action is an error (never swallowed).
func TestStartRecordingNon2xx(t *testing.T) {
	srv, _ := startRecordStub(t, 422, `{"errors":[{"title":"bad"}]}`)
	c := NewClient(srv.URL, "k")
	if err := c.StartRecording(context.Background(), "cc-1", RecordOptions{}); err == nil {
		t.Fatal("want error on 422")
	}
}

// TestParseCallEventRecordingSaved pins URL capture across the three
// Telnyx payload shapes, priority order (recording_urls wins over
// download_urls), and the no-url case. Ported verbatim from the uptractor
// recording_test.go table.
func TestParseCallEventRecordingSaved(t *testing.T) {
	tests := []struct {
		name, urls, want string
	}{
		{"recording_urls", `"recording_urls":{"mp3":"https://r/a.mp3"}`, "https://r/a.mp3"},
		{"public_recording_urls", `"public_recording_urls":{"mp3":"https://r/p.mp3"}`, "https://r/p.mp3"},
		{"download_urls fallback", `"download_urls":{"mp3":"https://r/d.mp3"}`, "https://r/d.mp3"},
		{"recording_urls wins", `"download_urls":{"mp3":"https://r/d.mp3"},"recording_urls":{"mp3":"https://r/a.mp3"}`, "https://r/a.mp3"},
		{"no url", `"recording_urls":{}`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"data":{"event_type":"call.recording.saved","payload":{"call_control_id":"cc-9","call_session_id":"cs-9",` + tc.urls + `}}}`
			ev, err := ParseCallEvent([]byte(body))
			if err != nil {
				t.Fatalf("ParseCallEvent: %v", err)
			}
			if ev.EventType != RecordingSavedEvent || ev.CallControlID != "cc-9" || ev.CallSessionID != "cs-9" {
				t.Fatalf("event = %+v", ev)
			}
			if ev.RecordingURL != tc.want {
				t.Fatalf("RecordingURL = %q, want %q", ev.RecordingURL, tc.want)
			}
		})
	}
}

// TestRecordingSavedIsNeverIgnored pins the leaf-06/leaf-01 interaction:
// call.recording.saved must NOT classify as IgnoredEventError — the
// classification change must never reopen the retry-storm fix by eating
// the URL-bearing event.
func TestRecordingSavedIsNeverIgnored(t *testing.T) {
	_, err := ParseCallEvent([]byte(`{"data":{"event_type":"call.recording.saved","payload":{"call_control_id":"cc-9","recording_urls":{"mp3":"https://r/a.mp3"}}}}`))
	if err != nil {
		t.Fatalf("call.recording.saved must parse cleanly (got %v), never as IgnoredEventError", err)
	}
	if IsIgnoredEvent(err) {
		t.Fatal("call.recording.saved classified as ignored — the URL would be lost")
	}
}
