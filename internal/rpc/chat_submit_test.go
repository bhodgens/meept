package rpc

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// fakeSubmitRegistry records Register calls without importing internal/agent
// (the rpc package must not depend on agent; production wiring adapts
// *agent.TurnRegistry to the chatSubmitRegistry interface structurally).
type fakeSubmitRegistry struct {
	mu       sync.Mutex
	calls    []string
	existing map[string]bool
}

func newFakeSubmitRegistry() *fakeSubmitRegistry {
	return &fakeSubmitRegistry{existing: map[string]bool{}}
}

func (f *fakeSubmitRegistry) Register(turnID, conversationID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, turnID+"|"+conversationID)
	if f.existing[turnID] {
		return true
	}
	f.existing[turnID] = true
	return false
}

func (f *fakeSubmitRegistry) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// waitForChatRequest blocks until a chat.request arrives on sub and returns
// its decoded payload.
func waitForChatRequest(t *testing.T, sub *bus.Subscriber) map[string]any {
	t.Helper()
	select {
	case msg := <-sub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("chat.request payload unmarshal: %v", err)
		}
		if msg.Topic != "chat.request" {
			t.Errorf("message topic = %q, want chat.request", msg.Topic)
		}
		return payload
	case <-time.After(5 * time.Second):
		t.Fatal("no chat.request published")
		return nil
	}
}

func TestChatSubmit_HappyPath(t *testing.T) {
	msgBus := bus.New(nil, nil)
	h := NewSubmitHandler(msgBus, nil, nil)

	sub := msgBus.Subscribe("test-submit", "chat.request")
	defer func() { msgBus.Unsubscribe(sub) }()

	params := json.RawMessage(`{"message":"do the thing","session_id":"sess-1","conversation_id":"conv-1","source_client":"tui"}`)
	start := time.Now()
	ackAny, err := h.BuildChatSubmitAck(context.Background(), params)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("chat.submit returned error: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("ack took %v; submit must never block on agent work", elapsed)
	}

	ack, ok := ackAny.(map[string]any)
	if !ok {
		t.Fatalf("ack type = %T, want map[string]any", ackAny)
	}
	if ack["accepted"] != true {
		t.Errorf("accepted = %v, want true", ack["accepted"])
	}
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Error("turn_id must be non-empty")
	}
	if ack["conversation_id"] != "conv-1" {
		t.Errorf("conversation_id = %v, want conv-1 (client-supplied preserved)", ack["conversation_id"])
	}
	if ack["session_id"] != "sess-1" {
		t.Errorf("session_id = %v, want sess-1", ack["session_id"])
	}
	if note, _ := ack["note"].(string); note == "" {
		t.Error("note must be non-empty on the happy path")
	}
	// Frozen five-key ack contract.
	if len(ack) != 5 {
		t.Errorf("ack has %d keys, want exactly 5 (turn_id, conversation_id, session_id, accepted, note)", len(ack))
	}

	payload := waitForChatRequest(t, sub)
	if payload["turn_id"] != turnID {
		t.Errorf("published turn_id = %v, want the acked %q", payload["turn_id"], turnID)
	}
	if payload["message"] != "do the thing" {
		t.Errorf("published message = %v", payload["message"])
	}
	if payload["conversation_id"] != "conv-1" {
		t.Errorf("published conversation_id = %v", payload["conversation_id"])
	}
	if _, ok := payload["session_id"]; !ok {
		t.Error("published payload missing session_id key")
	}
}

func TestChatSubmit_GeneratesConversationWhenOmitted(t *testing.T) {
	msgBus := bus.New(nil, nil)
	h := NewSubmitHandler(msgBus, nil, nil)

	sub := msgBus.Subscribe("test-submit-gen", "chat.request")
	defer func() { msgBus.Unsubscribe(sub) }()

	ackAny, err := h.BuildChatSubmitAck(context.Background(), json.RawMessage(`{"message":"hello"}`))
	if err != nil {
		t.Fatalf("chat.submit error: %v", err)
	}
	ack := ackAny.(map[string]any)
	turnID, _ := ack["turn_id"].(string)
	convID, _ := ack["conversation_id"].(string)
	if convID == "" {
		t.Fatal("conversation_id must be generated when omitted")
	}
	if turnID == "" {
		t.Fatal("turn_id must be generated")
	}

	payload := waitForChatRequest(t, sub)
	if payload["conversation_id"] != convID {
		t.Errorf("published conversation_id = %v, want the generated %q", payload["conversation_id"], convID)
	}
}

func TestChatSubmit_EmptyMessageRejected(t *testing.T) {
	msgBus := bus.New(nil, nil)
	h := NewSubmitHandler(msgBus, nil, nil)

	sub := msgBus.Subscribe("test-submit-empty", "chat.request")
	defer func() { msgBus.Unsubscribe(sub) }()

	for _, body := range []string{`{"message":""}`, `{"message":"   "}`, `{}`} {
		ackAny, err := h.BuildChatSubmitAck(context.Background(), json.RawMessage(body))
		if err != nil {
			t.Fatalf("chat.submit(%s) returned error: %v", body, err)
		}
		ack := ackAny.(map[string]any)
		if ack["accepted"] != false {
			t.Errorf("accepted = %v, want false for %s", ack["accepted"], body)
		}
		if note, _ := ack["note"].(string); note == "" {
			t.Errorf("note must be non-empty on rejection (%s)", body)
		}
		if turnID, _ := ack["turn_id"].(string); turnID != "" {
			t.Errorf("rejected submit must not mint a turn_id, got %q (%s)", turnID, body)
		}
	}

	select {
	case msg := <-sub.Channel:
		t.Fatalf("invalid submit published %s", msg.Payload)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestChatSubmit_WhitespaceOnlyMessageRejected(t *testing.T) {
	msgBus := bus.New(nil, nil)
	h := NewSubmitHandler(msgBus, nil, nil)

	sub := msgBus.Subscribe("test-submit-ws", "chat.request")
	defer func() { msgBus.Unsubscribe(sub) }()

	ackAny, err := h.BuildChatSubmitAck(context.Background(), json.RawMessage(`{"message":" \t\n "}`))
	if err != nil {
		t.Fatalf("chat.submit error: %v", err)
	}
	ack := ackAny.(map[string]any)
	if ack["accepted"] != false {
		t.Errorf("accepted = %v, want false for whitespace-only message", ack["accepted"])
	}

	select {
	case msg := <-sub.Channel:
		t.Fatalf("whitespace submit published %s", msg.Payload)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestChatSubmit_IdempotentRetrySameTurnID(t *testing.T) {
	msgBus := bus.New(nil, nil)
	reg := newFakeSubmitRegistry()
	h := NewSubmitHandler(msgBus, reg, nil)

	sub := msgBus.Subscribe("test-submit-idem", "chat.request")
	defer func() { msgBus.Unsubscribe(sub) }()

	params := json.RawMessage(`{"message":"run once","conversation_id":"conv-1","turn_id":"turn-explicit-1"}`)

	firstAny, err := h.BuildChatSubmitAck(context.Background(), params)
	if err != nil {
		t.Fatalf("first submit error: %v", err)
	}
	first := firstAny.(map[string]any)
	if first["accepted"] != true || first["turn_id"] != "turn-explicit-1" {
		t.Fatalf("first ack = %v", first)
	}
	payload := waitForChatRequest(t, sub)
	if payload["turn_id"] != "turn-explicit-1" {
		t.Fatalf("published turn_id = %v, want turn-explicit-1", payload["turn_id"])
	}

	// Same explicit turn_id again: same ack, NO second publish.
	secondAny, err := h.BuildChatSubmitAck(context.Background(), params)
	if err != nil {
		t.Fatalf("retry error: %v", err)
	}
	second := secondAny.(map[string]any)
	if second["turn_id"] != "turn-explicit-1" {
		t.Errorf("retry turn_id = %v, want turn-explicit-1 (same id returned)", second["turn_id"])
	}
	if second["accepted"] != true {
		t.Errorf("retry accepted = %v, want true", second["accepted"])
	}

	select {
	case msg := <-sub.Channel:
		t.Fatalf("retry published a SECOND chat.request: %s", msg.Payload)
	case <-time.After(300 * time.Millisecond):
		// Exactly one publish total — correct.
	}
	if reg.callCount() != 2 {
		// Both submissions hit the registry (that's how dedupe is detected);
		// the SECOND one was reported as existing.
		t.Logf("registry calls: %d", reg.callCount())
	}
}

func TestChatSubmit_NilRegistryStillWorks(t *testing.T) {
	msgBus := bus.New(nil, nil)
	h := NewSubmitHandler(msgBus, nil, nil) // registry nil — must not panic

	sub := msgBus.Subscribe("test-submit-nilreg", "chat.request")
	defer func() { msgBus.Unsubscribe(sub) }()

	ackAny, err := h.BuildChatSubmitAck(context.Background(), json.RawMessage(`{"message":"work without registry"}`))
	if err != nil {
		t.Fatalf("chat.submit error: %v", err)
	}
	ack := ackAny.(map[string]any)
	if ack["accepted"] != true {
		t.Errorf("accepted = %v, want true (registry is optional)", ack["accepted"])
	}
	waitForChatRequest(t, sub)
}

func TestChatSubmit_InvalidJSONParams(t *testing.T) {
	h := NewSubmitHandler(bus.New(nil, nil), nil, nil)
	if _, err := h.BuildChatSubmitAck(context.Background(), json.RawMessage(`{not-json`)); err == nil {
		t.Fatal("malformed params must return an error")
	}
}
