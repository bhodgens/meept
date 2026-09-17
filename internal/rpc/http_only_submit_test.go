package rpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/bus"
)

// Week bughunt 2026-09-17 Group 1 pin (finding 17): POST /api/v1/chat/submit
// must be served when the Unix RPC transport is disabled. Pre-fix the shared
// submit handler was constructed only inside the `rpcServer != nil && proxy
// != nil` gate, so an HTTP-only daemon answered 503.

// TestHTTPOnlySubmitHandlerConstructed pins the composition contract at the
// handler level: a SubmitHandler built with just a bus and registry (no RPC
// server involved) serves the HTTP Submit seam and publishes chat.request.
func TestHTTPOnlySubmitHandlerConstructed(t *testing.T) {
	msgBus := bus.New(nil, nil)
	defer msgBus.Close()

	sub := msgBus.Subscribe("http-only-submit", "chat.request")
	defer msgBus.Unsubscribe(sub)

	// The exact construction an HTTP-only daemon performs (no rpc.Server).
	h := NewSubmitHandler(msgBus, nil, nil)

	params, err := json.Marshal(map[string]any{
		"message":         "hello over http",
		"conversation_id": "conv-http-only",
		"session_id":      "sess-http-only",
	})
	if err != nil {
		t.Fatal(err)
	}

	ack, err := h.Submit(context.Background(), params)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ackMap, ok := ack.(map[string]any)
	if !ok {
		t.Fatalf("ack = %T, want map", ack)
	}
	if ackMap["accepted"] != true {
		t.Errorf("accepted = %v, want true", ackMap["accepted"])
	}
	if ackMap["turn_id"] == "" {
		t.Error("turn_id must be minted")
	}

	// The chat.request must still be published for the ChatHandler.
	select {
	case <-sub.Channel:
	case <-t.Context().Done():
		t.Fatal("no chat.request published by the HTTP-only submit handler")
	}
}
