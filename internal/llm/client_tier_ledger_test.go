package llm

// Serving-tier ledger attribution tests (llm-serving-tiers TIER-1
// residual): a response carrying the llm_tier* metadata keys (written by
// the tier gate's stampMetadata) lands the triple in the llm_calls row,
// and a metadata-less response (tier feature off / error path) lands the
// disabled-path defaults '' / false / -1 — never a fabricated depth 0.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// chatTierMockServer returns an OpenAI-shaped mock that echoes the
// requested tier header as response metadata keys on the wire — the
// metadata itself is stamped in-process by the tier gate, so the client
// test drives the seam directly: Chat() + a manual stamp of the response
// is NOT how the production path flows, so instead we assert through
// recordUsageStore via the exported Chat path with a wrapped roundtripper
// is overkill; the production plumbing is Chat → parseResponse →
// ProviderManager stampMetadata → recordUsageStore. The manager seam is
// exercised in provider-manager tests; here we pin the client half with
// the real Chat path and metadata injected at the parse boundary via the
// stampMetadata helper (same writer the production path uses).
func TestClientChat_TierMetadataLandsInLedger(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id": "chatcmpl-tier", "object": "chat.completion", "model": "test-model",
			"choices": []map[string]any{
				{"index": 0,
					"message":       map[string]any{"role": "assistant", "content": "hi"},
					"finish_reason": "stop"},
			},
			"usage": map[string]any{
				"prompt_tokens": 100, "completion_tokens": 10, "total_tokens": 110,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	// The sidecar reads the same file the store owns (WAL permits
	// concurrent readers).
	store, dbPath := newUsageTestStoreAtPath(t, filepath.Join(t.TempDir(), "metrics.db"))
	client := NewClient(&ModelConfig{BaseURL: server.URL, ModelID: "tier-model", ProviderID: "prov-tier"})
	client.SetUsageStore(store)

	resp, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// Stamp the tier metadata exactly as the ProviderManager's tierGate
	// does on the completed response (tier_policy.go stampMetadata) —
	// recordUsageStore reads the SAME *Response object the caller got.
	gate := &tierGate{signals: true}
	gate.stampMetadata(resp, "prov-tier", "tier-model", 2)

	// recordUsageStore is the production writer; drive it directly with
	// the stamped response (the Chat call site passes resp verbatim).
	client.recordUsageStore("prov-tier", "tier-model", "agent-t", "sess-t",
		resp.Usage, false, "", 55, &chatOptions{}, resp)

	db := openLLMCallsSidecar(t, dbPath)
	pollUntil(t, func() bool {
		var n int
		_ = db.Get(&n, `SELECT COUNT(*) FROM llm_calls WHERE llm_tier != ''`)
		return n == 1
	})
	var tier string
	var degraded bool
	var depth int
	if err := db.QueryRow(
		`SELECT llm_tier, llm_tier_degraded, llm_tier_depth FROM llm_calls WHERE llm_tier != ''`).
		Scan(&tier, &degraded, &depth); err != nil {
		t.Fatalf("attributed row: %v", err)
	}
	if tier != "prov-tier/tier-model" {
		t.Errorf("llm_tier = %q, want %q", tier, "prov-tier/tier-model")
	}
	if !degraded {
		t.Errorf("llm_tier_degraded = false, want true (depth 2 > 0)")
	}
	if depth != 2 {
		t.Errorf("llm_tier_depth = %d, want 2", depth)
	}
}

// TestClientChat_MetadataLessResponseLandsDefaults pins the disabled
// path: no tier metadata on the response (tier feature off — the default
// posture) writes ” / false / -1. Depth stays -1, never the Go zero 0:
// 0 is reserved for a tier-ENABLED primary call.
func TestClientChat_MetadataLessResponseLandsDefaults(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id": "chatcmpl-plain", "object": "chat.completion", "model": "test-model",
			"choices": []map[string]any{
				{"index": 0,
					"message":       map[string]any{"role": "assistant", "content": "hi"},
					"finish_reason": "stop"},
			},
			"usage": map[string]any{
				"prompt_tokens": 50, "completion_tokens": 5, "total_tokens": 55,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	store, dbPath := newUsageTestStoreAtPath(t, filepath.Join(t.TempDir(), "metrics.db"))
	client := NewClient(&ModelConfig{BaseURL: server.URL, ModelID: "plain-model", ProviderID: "prov-plain"})
	client.SetUsageStore(store)

	resp, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Metadata != nil {
		t.Fatalf("precondition: metadata-less mock produced %v", resp.Metadata)
	}

	client.recordUsageStore("prov-plain", "plain-model", "", "",
		resp.Usage, false, "", 42, &chatOptions{}, resp)

	db := openLLMCallsSidecar(t, dbPath)
	pollUntil(t, func() bool {
		var n int
		_ = db.Get(&n, `SELECT COUNT(*) FROM llm_calls WHERE provider = 'prov-plain'`)
		return n == 1
	})
	var tier string
	var degraded bool
	var depth int
	if err := db.QueryRow(
		`SELECT llm_tier, llm_tier_degraded, llm_tier_depth FROM llm_calls WHERE provider = 'prov-plain'`).
		Scan(&tier, &degraded, &depth); err != nil {
		t.Fatalf("disabled-path row: %v", err)
	}
	if tier != "" {
		t.Errorf("llm_tier = %q, want '' (disabled path)", tier)
	}
	if degraded {
		t.Errorf("llm_tier_degraded = true, want false")
	}
	if depth != -1 {
		t.Errorf("llm_tier_depth = %d, want -1 (0 is the tier-enabled primary value)", depth)
	}

	// The read path counts this window as zero tier-attributed calls.
	rows, err := store.QueryLLMCallUsage(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), false)
	if err != nil {
		t.Fatalf("QueryLLMCallUsage: %v", err)
	}
	if len(rows) != 1 || rows[0].TierCalls != 0 || rows[0].TierDegradedCalls != 0 {
		t.Errorf("tier counts = %+v, want TierCalls 0 / TierDegradedCalls 0", rows)
	}
}
