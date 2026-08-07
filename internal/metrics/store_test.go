package metrics

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStore_DispatchLogRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "metrics.db")

	store, err := NewStore(&StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     1,
		FlushInterval: time.Hour, // disable background flush; RecordDispatch writes directly
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	entries := []DispatchEntry{
		{
			SessionID:        "conv-aaa",
			InputSummary:     "review the codebase",
			IntentType:       "review",
			AgentID:          "analyst",
			Confidence:       0.85,
			ClassifierMethod: "llm",
			HandlerCase:      "async_dispatch",
			TaskID:           "task-001",
			HasParts:         false,
		},
		{
			SessionID:        "conv-bbb",
			InputSummary:     "fix the bug in handler.go",
			IntentType:       "debug",
			AgentID:          "debugger",
			Confidence:       0.92,
			ClassifierMethod: "capability_matcher",
			HandlerCase:      "route_to_agent",
			TaskID:           "",
			HasParts:         true,
		},
	}

	for _, e := range entries {
		store.RecordDispatch(e)
	}

	results, err := store.QueryDispatchLog(10)
	if err != nil {
		t.Fatalf("QueryDispatchLog: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(results))
	}

	// Most recent first (ORDER BY id DESC)
	if results[0].SessionID != "conv-bbb" {
		t.Errorf("first result session = %q, want conv-bbb", results[0].SessionID)
	}
	if results[0].HasParts != true {
		t.Errorf("first result HasParts = false, want true")
	}
	if results[0].ClassifierMethod != "capability_matcher" {
		t.Errorf("first result method = %q, want capability_matcher", results[0].ClassifierMethod)
	}
	if results[0].HandlerCase != "route_to_agent" {
		t.Errorf("first result case = %q, want route_to_agent", results[0].HandlerCase)
	}

	if results[1].SessionID != "conv-aaa" {
		t.Errorf("second result session = %q, want conv-aaa", results[1].SessionID)
	}
	if results[1].Confidence != 0.85 {
		t.Errorf("second result confidence = %f, want 0.85", results[1].Confidence)
	}
}

func TestStore_QueryDispatchLogBySession(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "metrics.db")

	store, err := NewStore(&StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     1,
		FlushInterval: time.Hour, // disable background flush; RecordDispatch writes directly
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	entries := []DispatchEntry{
		{
			SessionID:        "session-A",
			InputSummary:     "first request",
			IntentType:       "review",
			AgentID:          "analyst",
			Confidence:       0.85,
			ClassifierMethod: "llm",
			HandlerCase:      "async_dispatch",
			TaskID:           "task-001",
			HasParts:         false,
		},
		{
			SessionID:        "session-A",
			InputSummary:     "second request",
			IntentType:       "debug",
			AgentID:          "debugger",
			Confidence:       0.92,
			ClassifierMethod: "capability_matcher",
			HandlerCase:      "route_to_agent",
			TaskID:           "task-002",
			HasParts:         true,
		},
		{
			SessionID:        "session-B",
			InputSummary:     "other session request",
			IntentType:       "general",
			AgentID:          "generalist",
			Confidence:       0.5,
			ClassifierMethod: "fallback",
			HandlerCase:      "default_route",
			TaskID:           "task-003",
			HasParts:         false,
		},
	}

	for _, e := range entries {
		store.RecordDispatch(e)
	}

	// Query session-A: expect 2 results, most recent first (debugger then analyst).
	aResults, err := store.QueryDispatchLogBySession("session-A", 10)
	if err != nil {
		t.Fatalf("QueryDispatchLogBySession session-A: %v", err)
	}
	if len(aResults) != 2 {
		t.Fatalf("expected 2 entries for session-A, got %d", len(aResults))
	}
	if aResults[0].AgentID != "debugger" {
		t.Errorf("first result agent = %q, want debugger", aResults[0].AgentID)
	}
	if aResults[1].AgentID != "analyst" {
		t.Errorf("second result agent = %q, want analyst", aResults[1].AgentID)
	}
	if aResults[0].HasParts != true {
		t.Errorf("first result HasParts = false, want true")
	}

	// Query session-B: expect 1 result.
	bResults, err := store.QueryDispatchLogBySession("session-B", 10)
	if err != nil {
		t.Fatalf("QueryDispatchLogBySession session-B: %v", err)
	}
	if len(bResults) != 1 {
		t.Fatalf("expected 1 entry for session-B, got %d", len(bResults))
	}
	if bResults[0].AgentID != "generalist" {
		t.Errorf("result agent = %q, want generalist", bResults[0].AgentID)
	}
	if bResults[0].SessionID != "session-B" {
		t.Errorf("result session = %q, want session-B", bResults[0].SessionID)
	}

	// Query a non-existent session: expect 0 results and no error.
	emptyResults, err := store.QueryDispatchLogBySession("nonexistent", 10)
	if err != nil {
		t.Fatalf("QueryDispatchLogBySession nonexistent: %v", err)
	}
	if len(emptyResults) != 0 {
		t.Errorf("expected 0 entries for nonexistent session, got %d", len(emptyResults))
	}
}
