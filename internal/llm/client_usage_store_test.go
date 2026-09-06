package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	appmetrics "github.com/caimlas/meept/internal/metrics"
	"github.com/jmoiron/sqlx"
)

// newUsageTestStore builds a metrics.db store bound to a temp dir for
// client-wiring tests.
func newUsageTestStore(t *testing.T) *appmetrics.Store {
	t.Helper()
	s, _ := newUsageTestStoreAtPath(t, filepath.Join(t.TempDir(), "metrics.db"))
	return s
}

// newUsageTestStoreAtPath is newUsageTestStore with an explicit database path
// (tests that need a sidecar read connection on the same file).
func newUsageTestStoreAtPath(t *testing.T, dbPath string) (*appmetrics.Store, string) {
	t.Helper()
	s, err := appmetrics.NewStore(&appmetrics.StoreConfig{
		DatabasePath:  dbPath,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dbPath
}

// openLLMCallsSidecar opens an independent sqlx read connection on the
// store's database file (WAL mode permits concurrent readers) so llm-package
// tests can inspect raw llm_calls columns the exported query API does not
// expose (session_id, reasoning_tokens, ...).
func openLLMCallsSidecar(t *testing.T, dbPath string) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Connect("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sidecar: %v", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		t.Fatalf("sidecar busy_timeout: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// pollUntil reruns fn until it returns true or the deadline expires
// (recordUsageStore writes asynchronously from a goroutine).
func pollUntil(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestClientChat_RecordsUsageStore drives Client.Chat against a mock server
// that returns usage incl. prompt_tokens_details.cached_tokens, and verifies
// the llm_calls row + model_performance rollup land with agent attribution.
func TestClientChat_RecordsUsageStore(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id": "chatcmpl-1", "object": "chat.completion", "model": "test-model",
			"choices": []map[string]any{
				{"index": 0,
					"message":       map[string]any{"role": "assistant", "content": "hi"},
					"finish_reason": "stop"},
			},
			"usage": map[string]any{
				"prompt_tokens": 1000, "completion_tokens": 50, "total_tokens": 1050,
				"prompt_tokens_details": map[string]any{"cached_tokens": 800},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	store := newUsageTestStore(t)
	client := NewClient(&ModelConfig{BaseURL: server.URL, ModelID: "test-model", ProviderID: "prov-x"})
	client.SetUsageStore(store)

	_, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithAgentScope("coder"))
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// recordUsageStore writes async; poll briefly.
	deadline := time.Now().Add(3 * time.Second)
	var got []appmetrics.LLMUsageRow
	for time.Now().Before(deadline) {
		rows, qerr := store.QueryLLMCallUsage(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), true)
		if qerr == nil && len(rows) == 1 {
			got = rows
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 usage row, got %d", len(got))
	}
	r := got[0]
	if r.Provider != "prov-x" || r.AgentID != "coder" || r.Calls != 1 ||
		r.TokensSent != 1000 || r.TokensRecv != 50 || r.TokensCached != 800 || r.Errors != 0 {
		t.Errorf("usage row: got %+v", r)
	}
}

// TestClientChat_NoUsageStore ensures a client without a usage store is
// unaffected (nil-receiver discipline).
func TestClientChat_NoUsageStore(t *testing.T) {
	client := NewClient(&ModelConfig{BaseURL: "http://localhost", ModelID: "m"})
	client.SetUsageStore(nil) // must be a safe no-op
	// No store attached: recordUsageStore no-ops. Nothing to assert beyond
	// "did not panic"; skip an actual request (no server).
}

// TestClientChat_UsageStoreCarriesSessionIDAndReasoning verifies the generic
// OpenAI-compatible path stamps chatOptions.sessionID (WithTaskScope) and
// completion_tokens_details.reasoning_tokens into the llm_calls row
// (tokscale ingest leaf 01).
func TestClientChat_UsageStoreCarriesSessionIDAndReasoning(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id": "chatcmpl-2", "object": "chat.completion", "model": "test-model",
			"choices": []map[string]any{
				{"index": 0,
					"message":       map[string]any{"role": "assistant", "content": "hi"},
					"finish_reason": "stop"},
			},
			"usage": map[string]any{
				"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120,
				"completion_tokens_details": map[string]any{"reasoning_tokens": 7},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	store, dbPath := newUsageTestStoreAtPath(t, filepath.Join(t.TempDir(), "metrics.db"))
	db := openLLMCallsSidecar(t, dbPath)
	client := NewClient(&ModelConfig{BaseURL: server.URL, ModelID: "test-model", ProviderID: "prov-x"})
	client.SetUsageStore(store)

	_, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithAgentScope("coder"), WithTaskScope("task-1", "sess-9"))
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// recordUsageStore writes async; poll for the row via the sidecar
	// connection (QueryLLMCallUsage does not expose session/reasoning).
	var rows []struct {
		SessionID       string `db:"session_id"`
		ReasoningTokens int    `db:"reasoning_tokens"`
	}
	pollUntil(t, func() bool {
		return db.Select(&rows, `SELECT session_id, reasoning_tokens FROM llm_calls`) == nil &&
			len(rows) == 1
	})
	if len(rows) != 1 {
		t.Fatalf("expected 1 llm_calls row, got %d", len(rows))
	}
	if rows[0].SessionID != "sess-9" {
		t.Errorf("SessionID = %q, want sess-9", rows[0].SessionID)
	}
	if rows[0].ReasoningTokens != 7 {
		t.Errorf("ReasoningTokens = %d, want 7", rows[0].ReasoningTokens)
	}
}
