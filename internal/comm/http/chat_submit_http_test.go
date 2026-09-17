package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/services"
)

// fakeChatSubmitter records the params passed through the ChatSubmitter seam.
type fakeChatSubmitter struct {
	gotParams string
	gotCalls  int
	ack       map[string]any
	err       error
}

func (f *fakeChatSubmitter) Submit(ctx context.Context, params json.RawMessage) (any, error) {
	f.gotCalls++
	f.gotParams = string(params)
	if f.err != nil {
		return nil, f.err
	}
	return f.ack, nil
}

func TestHandleChatSubmit_PassesThroughSharedAck(t *testing.T) {
	ack := map[string]any{
		"turn_id":         "turn-http-1",
		"conversation_id": "conv-http",
		"session_id":      "sess-1",
		"accepted":        true,
		"note":            "accepted; result arrives via turn.terminal",
	}
	fake := &fakeChatSubmitter{ack: ack}
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil, WithChatSubmitter(fake))

	body := `{"message":"submit over http","session_id":"sess-1","conversation_id":"conv-http"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/submit", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.handleChatSubmit(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if got["turn_id"] != "turn-http-1" || got["accepted"] != true {
		t.Errorf("ack = %v", got)
	}
	if fake.gotCalls != 1 {
		t.Errorf("submitter calls = %d, want 1", fake.gotCalls)
	}
	// The params the shared function received must round-trip the body.
	var passed services.ChatSubmitRequest
	if err := json.Unmarshal([]byte(fake.gotParams), &passed); err != nil {
		t.Fatalf("params unmarshal: %v", err)
	}
	if passed.Message != "submit over http" || passed.SessionID != "sess-1" || passed.ConversationID != "conv-http" {
		t.Errorf("params = %+v", passed)
	}
}

func TestHandleChatSubmit_NilSubmitterServiceUnavailable(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/submit", strings.NewReader(`{"message":"hi"}`))
	w := httptest.NewRecorder()
	server.handleChatSubmit(w, req)

	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 when no submitter wired", w.Result().StatusCode)
	}
}

func TestHandleChatSubmit_EmptyMessageStill200WithAck(t *testing.T) {
	// MATCH the existing /api/v1/chat convention: chat-service validation
	// errors surface via the ack body, not 400 — the shared function returns
	// accepted=false + note, which passes through untouched.
	ack := map[string]any{
		"turn_id":         "",
		"conversation_id": "",
		"session_id":      "",
		"accepted":        false,
		"note":            "message is required",
	}
	fake := &fakeChatSubmitter{ack: ack}
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil, WithChatSubmitter(fake))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/submit", strings.NewReader(`{"message":""}`))
	w := httptest.NewRecorder()
	server.handleChatSubmit(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (ack carries accepted=false)", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if got["accepted"] != false {
		t.Errorf("accepted = %v, want false", got["accepted"])
	}
	if note, _ := got["note"].(string); note == "" {
		t.Error("note must be non-empty on rejection")
	}
}

func TestHandleChatSubmit_MalformedBody400(t *testing.T) {
	fake := &fakeChatSubmitter{}
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil, WithChatSubmitter(fake))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/submit", strings.NewReader(`not json`))
	w := httptest.NewRecorder()
	server.handleChatSubmit(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for malformed JSON (readJSON convention)", w.Result().StatusCode)
	}
	if fake.gotCalls != 0 {
		t.Errorf("submitter must not be called on malformed body, got %d calls", fake.gotCalls)
	}
}

func TestHandleChatSubmit_GetMethodNotAllowed(t *testing.T) {
	fake := &fakeChatSubmitter{}
	server := NewServer(ServerConfig{RESTEnabled: true}, nil, nil, nil, nil, nil, WithChatSubmitter(fake))

	mux := http.NewServeMux()
	server.setupRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/chat/submit")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", resp.StatusCode)
	}
}

// TestHandleChatSubmit_LiveRPCSubmitter is the full-stack proof: the HTTP
// endpoint drives a REAL rpc.SubmitHandler over a real bus, the chat.request
// lands on the bus with turn_id, and the ack carries the frozen five-key
// contract. This is the same construction the daemon wiring performs.
func TestHandleChatSubmit_LiveRPCSubmitter(t *testing.T) {
	msgBus := bus.New(nil, nil)
	handler := rpc.NewSubmitHandler(msgBus, nil, nil)
	server := NewServer(ServerConfig{RESTEnabled: true}, nil, nil, nil, nil, nil, WithChatSubmitter(handler))

	mux := http.NewServeMux()
	server.setupRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	sub := msgBus.Subscribe("test-http-submit", "chat.request")
	defer func() { msgBus.Unsubscribe(sub) }()

	start := time.Now()
	resp, err := http.Post(srv.URL+"/api/v1/chat/submit", "application/json",
		strings.NewReader(`{"message":"live http submit","session_id":"sess-live","source_client":"gui"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if elapsed > 2*time.Second {
		t.Errorf("submit took %v; must never block on agent work", elapsed)
	}

	var ack map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if len(ack) != 5 {
		t.Errorf("ack has %d keys, want exactly 5 (frozen contract)", len(ack))
	}
	if ack["accepted"] != true {
		t.Errorf("accepted = %v, want true", ack["accepted"])
	}
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatal("turn_id must be non-empty")
	}

	select {
	case msg := <-sub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("payload unmarshal: %v", err)
		}
		if payload["turn_id"] != turnID {
			t.Errorf("published turn_id = %v, want acked %q", payload["turn_id"], turnID)
		}
		if payload["source_client"] != "gui" {
			t.Errorf("published source_client = %v, want gui", payload["source_client"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no chat.request published over the live path")
	}
}
