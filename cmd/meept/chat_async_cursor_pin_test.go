package main

import (
	"encoding/json"
	"testing"
)

// TestPollTurnEvents_CursorIsJSONNumber pins the wire contract leaf-04
// caught broken in c5d54c9e: when the await loop switched from the legacy
// wall-clock `since` string to the monotonic `since_seq`, the params map
// stayed map[string]string, so the cursor rode as "0" — and the server
// (internal/rpc/proxy.go handleBusPoll) decodes SinceSeq int64 STRICTLY:
// every meept chat poll failed with "json: cannot unmarshal string into
// Go struct field .since_seq of type int64". The params must decode into
// the server's request struct with the cursor intact.
func TestPollTurnEvents_CursorIsJSONNumber(t *testing.T) {
	f := newFakeAsyncClient(map[string]any{"events": []any{}, "next_seq": float64(9)}, nil)

	_, nextSeq, err := pollTurnEvents(f, "sub-1", 7)
	if err != nil {
		t.Fatalf("pollTurnEvents: %v", err)
	}
	if nextSeq != 9 {
		t.Fatalf("nextSeq = %d, want 9", nextSeq)
	}

	f.mu.Lock()
	count := len(f.pollParams)
	var params any
	if count > 0 {
		params = f.pollParams[0]
	}
	f.mu.Unlock()
	if count != 1 {
		t.Fatalf("poll calls = %d, want 1", count)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal recorded params: %v", err)
	}
	// Mirror of the server-side request struct (internal/rpc/proxy.go
	// handleBusPoll). If the client ever encodes since_seq as a string
	// again, this Unmarshal reproduces the production decode error.
	var req struct {
		SubscriptionID string `json:"subscription_id"`
		Since          string `json:"since"`
		SinceSeq       int64  `json:"since_seq"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("server-shape decode of %s: %v", raw, err)
	}
	if req.SinceSeq != 7 {
		t.Fatalf("since_seq decoded = %d, want 7 (params: %s)", req.SinceSeq, raw)
	}
	if req.SubscriptionID != "sub-1" {
		t.Fatalf("subscription_id = %q, want sub-1", req.SubscriptionID)
	}
	if req.Since != "" {
		t.Fatalf("legacy wall-clock `since` present (%q); the seq cursor must replace it", req.Since)
	}
}
