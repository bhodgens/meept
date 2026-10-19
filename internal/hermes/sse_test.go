package hermes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sseFlusher writes raw SSE bytes and flushes them to the client.
func sseFlusher(w http.ResponseWriter) http.Flusher {
	f, ok := w.(http.Flusher)
	if !ok {
		panic("httptest response is not a flusher")
	}
	return f
}

// drainEvents reads ch until it closes and returns everything delivered.
func drainEvents(t *testing.T, ch <-chan Event) []Event {
	t.Helper()
	var out []Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// TestStreamRunEvents_KeepaliveThenTerminal covers parse rule (a): the
// upstream emits ': keepalive' comment lines (every 10s in production) which
// MUST be skipped entirely — only the terminal run.completed event is
// delivered, and the channel closes after it.
func TestStreamRunEvents_KeepaliveThenTerminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs/run-1/events" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f := sseFlusher(w)
		for i := 0; i < 3; i++ {
			io.WriteString(w, ": keepalive\n\n")
			f.Flush()
		}
		io.WriteString(w, "event: run.completed\ndata: {\"status\":\"completed\"}\n\n")
		f.Flush()
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", 30*time.Second)
	ch, err := c.StreamRunEvents(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("StreamRunEvents: %v", err)
	}
	events := drainEvents(t, ch)
	if len(events) != 1 {
		t.Fatalf("want exactly 1 event (keepalives skipped), got %d: %+v", len(events), events)
	}
	if events[0].Type != "run.completed" {
		t.Errorf("event type = %q, want run.completed", events[0].Type)
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(events[0].Data, &payload); err != nil {
		t.Fatalf("event data is not valid JSON (%q): %v", events[0].Data, err)
	}
	if payload.Status != "completed" {
		t.Errorf("payload status = %q, want completed", payload.Status)
	}
}

// TestStreamRunEvents_MultiLineData covers parse rule (b): multiple data:
// lines in one frame are joined with '\n' per the SSE spec before the
// payload is handed to the consumer.
func TestStreamRunEvents_MultiLineData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := sseFlusher(w)
		// One JSON object split across three data: lines.
		io.WriteString(w, "event: tool.completed\ndata: {\"name\": \"terminal\",\ndata:  \"exit\": 0,\ndata:  \"note\": \"ok\"}\n\n")
		f.Flush()
		io.WriteString(w, "event: run.failed\ndata: {\"error\": \"boom\"}\n\n")
		f.Flush()
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", 30*time.Second)
	ch, err := c.StreamRunEvents(context.Background(), "run-ml")
	if err != nil {
		t.Fatalf("StreamRunEvents: %v", err)
	}
	events := drainEvents(t, ch)
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != "tool.completed" {
		t.Errorf("events[0].Type = %q, want tool.completed", events[0].Type)
	}
	if !strings.Contains(string(events[0].Data), "\n") {
		t.Errorf("multi-line data was not joined with newline: %q", events[0].Data)
	}
	var tool struct {
		Name string `json:"name"`
		Exit int    `json:"exit"`
		Note string `json:"note"`
	}
	if err := json.Unmarshal(events[0].Data, &tool); err != nil {
		t.Fatalf("joined data is not valid JSON (%q): %v", events[0].Data, err)
	}
	if tool.Name != "terminal" || tool.Exit != 0 || tool.Note != "ok" {
		t.Errorf("joined payload = %+v", tool)
	}
	if events[1].Type != "run.failed" {
		t.Errorf("events[1].Type = %q, want run.failed (terminal)", events[1].Type)
	}
}

// TestStreamChat_DeltaSequence covers parse rule (c): a chat stream emits
// assistant.delta frames (plus tool lifecycle frames) and ends with a
// terminal run.* event that closes the channel. Also pins the request
// shape: POST /api/sessions/{id}/chat/stream, Bearer auth, JSON body.
func TestStreamChat_DeltaSequence(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotContentType string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "text/event-stream")
		f := sseFlusher(w)
		for _, s := range []string{"Hel", "lo ", "world"} {
			io.WriteString(w, "event: assistant.delta\ndata: {\"text\": \""+s+"\"}\n\n")
			f.Flush()
		}
		io.WriteString(w, "event: tool.started\ndata: {\"name\": \"terminal\"}\n\n")
		f.Flush()
		io.WriteString(w, "event: tool.completed\ndata: {\"name\": \"terminal\"}\n\n")
		f.Flush()
		io.WriteString(w, "event: run.completed\ndata: {\"status\": \"completed\"}\n\n")
		f.Flush()
		// Anything after the terminal event must not be delivered (channel
		// is closed); give the client a moment, then exit.
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", 30*time.Second)
	ch, err := c.StreamChat(context.Background(), "sess-9", "say hello", ChatOptions{Model: "m1", Provider: "p1"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	events := drainEvents(t, ch)

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/sessions/sess-9/chat/stream" {
		t.Errorf("path = %q, want /api/sessions/sess-9/chat/stream", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth header = %q, want Bearer test-key", gotAuth)
	}
	if !strings.HasPrefix(gotContentType, "application/json") {
		t.Errorf("content type = %q, want application/json", gotContentType)
	}
	if gotBody["input"] != "say hello" || gotBody["model"] != "m1" || gotBody["provider"] != "p1" {
		t.Errorf("request body = %v", gotBody)
	}

	wantTypes := []string{
		"assistant.delta", "assistant.delta", "assistant.delta",
		"tool.started", "tool.completed", "run.completed",
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("want %d events, got %d: %+v", len(wantTypes), len(events), events)
	}
	var text strings.Builder
	for i, ev := range events {
		if ev.Type != wantTypes[i] {
			t.Errorf("events[%d].Type = %q, want %q", i, ev.Type, wantTypes[i])
		}
		if ev.Type == "assistant.delta" {
			var d struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(ev.Data, &d); err != nil {
				t.Fatalf("delta %d not JSON (%q): %v", i, ev.Data, err)
			}
			text.WriteString(d.Text)
		}
	}
	if text.String() != "Hello world" {
		t.Errorf("aggregated delta text = %q, want %q", text.String(), "Hello world")
	}
}

// TestStreamRunEvents_ContextCancelMidStream covers parse rule (d): a ctx
// cancel mid-stream closes the channel cleanly (no goroutine leak, no
// hang), with events received before the cancel preserved.
func TestStreamRunEvents_ContextCancelMidStream(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := sseFlusher(w)
		io.WriteString(w, "event: tool.started\ndata: {\"name\": \"first\"}\n\n")
		f.Flush()
		// Hold the stream open until the client goes away.
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	c := NewClient(srv.URL, "test-key", 30*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := c.StreamRunEvents(ctx, "run-cancel")
	if err != nil {
		t.Fatalf("StreamRunEvents: %v", err)
	}

	select {
	case ev, ok := <-ch:
		if !ok || ev.Type != "tool.started" {
			t.Fatalf("first read = (%+v, %v), want tool.started event", ev, ok)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first event")
	}

	cancel()

	closed := make(chan struct{})
	go func() {
		for range ch { // drain anything in flight; must terminate
		}
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("channel did not close after ctx cancel")
	}
}

// TestStreamRunEvents_Non2xxError covers parse rule (e): a non-2xx
// response becomes a typed error BEFORE any channel is returned.
func TestStreamRunEvents_Non2xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"internal boom"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", 30*time.Second)
	ch, err := c.StreamRunEvents(context.Background(), "run-500")
	if err == nil {
		t.Fatal("want error on 500, got nil")
	}
	if ch != nil {
		t.Errorf("channel must be nil when an error is returned")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", apiErr.StatusCode)
	}
}

// TestStreamChat_401MapsToErrHermesAuth covers parse rule (f): 401 maps
// onto the typed ErrHermesAuth sentinel before any channel is returned.
func TestStreamChat_401MapsToErrHermesAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "bad-key", 30*time.Second)
	ch, err := c.StreamChat(context.Background(), "sess-1", "hi", ChatOptions{})
	if err == nil {
		t.Fatal("want error on 401, got nil")
	}
	if ch != nil {
		t.Errorf("channel must be nil when an error is returned")
	}
	if !errors.Is(err, ErrHermesAuth) {
		t.Errorf("err = %v, want errors.Is ErrHermesAuth", err)
	}
}

// TestStreamChat_429MapsToErrHermesBusy pins the 429 -> ErrHermesBusy
// mapping on the stream path (same classification as the sync client).
func TestStreamChat_429MapsToErrHermesBusy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"busy"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", 30*time.Second)
	ch, err := c.StreamChat(context.Background(), "sess-1", "hi", ChatOptions{})
	if err == nil {
		t.Fatal("want error on 429, got nil")
	}
	if ch != nil {
		t.Errorf("channel must be nil when an error is returned")
	}
	if !errors.Is(err, ErrHermesBusy) {
		t.Errorf("err = %v, want errors.Is ErrHermesBusy", err)
	}
}

// TestStreamRunEvents_NotBoundedBySyncTimeout proves the stream reader is
// NOT bounded by the sync-turn http.Client timeout (a 100ms NewClient
// timeout must not kill a stream that delivers events over 400ms); the
// caller's ctx is the only bound.
func TestStreamRunEvents_NotBoundedBySyncTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := sseFlusher(w)
		for i := 0; i < 4; i++ {
			time.Sleep(100 * time.Millisecond)
			io.WriteString(w, "event: assistant.delta\ndata: {\"i\": "+string(rune('0'+i))+"}\n\n")
			f.Flush()
		}
		io.WriteString(w, "event: run.completed\ndata: {}\n\n")
		f.Flush()
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", 100*time.Millisecond) // would kill a sync request mid-stream
	ch, err := c.StreamRunEvents(context.Background(), "run-slow")
	if err != nil {
		t.Fatalf("StreamRunEvents: %v", err)
	}
	events := drainEvents(t, ch)
	if len(events) != 5 {
		t.Fatalf("want 5 events (4 deltas + terminal), got %d: %+v", len(events), events)
	}
	if events[4].Type != "run.completed" {
		t.Errorf("last event = %q, want run.completed", events[4].Type)
	}
}

// TestStreamRunEvents_RequiresRunID pins input validation: empty ids are
// rejected without an HTTP call.
func TestStreamRunEvents_RequiresRunID(t *testing.T) {
	c := NewClient("http://127.0.0.1:0", "test-key", time.Second)
	if ch, err := c.StreamRunEvents(context.Background(), ""); err == nil || ch != nil {
		t.Errorf("empty run id: want error + nil channel, got (%v, %v)", ch, err)
	}
	if ch, err := c.StreamChat(context.Background(), "", "hi", ChatOptions{}); err == nil || ch != nil {
		t.Errorf("empty session id: want error + nil channel, got (%v, %v)", ch, err)
	}
	if ch, err := c.StreamChat(context.Background(), "s1", "", ChatOptions{}); err == nil || ch != nil {
		t.Errorf("empty input: want error + nil channel, got (%v, %v)", ch, err)
	}
}
