package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
)

func TestNewSession(t *testing.T) {
	traceIDs := []string{"trace-1", "trace-2"}
	failureModes := []agent.FailureMode{
		{ID: "fm-1", Description: "test failure", Severity: "high", Category: "hallucination"},
	}

	session := NewSession(traceIDs, failureModes)

	if session.SessionID == "" {
		t.Error("expected non-empty session ID")
	}
	if len(session.TraceIDs) != len(traceIDs) {
		t.Errorf("expected %d trace IDs, got %d", len(traceIDs), len(session.TraceIDs))
	}
	if len(session.FailureModes) != len(failureModes) {
		t.Errorf("expected %d failure modes, got %d", len(failureModes), len(session.FailureModes))
	}
	if session.State != SessionActive {
		t.Errorf("expected state active, got %s", session.State)
	}
	if len(session.Turns) != 0 {
		t.Error("expected empty turns slice")
	}
	if session.Metadata == nil {
		t.Error("expected non-nil metadata map")
	}
	if session.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if session.UpdatedAt.IsZero() {
		t.Error("expected non-zero UpdatedAt")
	}
}

func TestAnalysisSession_AddTurn(t *testing.T) {
	session := NewSession([]string{"trace-1"}, nil)

	turn := session.AddTurn("What traces are affected?", "3 traces show the issue.", nil, nil)
	if turn == nil {
		t.Fatal("expected turn, got nil")
	}
	if turn.TurnID == "" {
		t.Error("expected non-empty turn ID")
	}
	if turn.UserQuery != "What traces are affected?" {
		t.Errorf("unexpected query: %s", turn.UserQuery)
	}
	if turn.AnalystResponse != "3 traces show the issue." {
		t.Errorf("unexpected response: %s", turn.AnalystResponse)
	}
	if session.TurnCount() != 1 {
		t.Errorf("expected 1 turn, got %d", session.TurnCount())
	}

	// Add a second turn with trace/span references.
	turn2 := session.AddTurn("Can you deepen the analysis?", "Focusing on the high-severity failure.",
		[]string{"trace-1", "trace-3"}, []string{"span-10", "span-20"})
	if turn2 == nil {
		t.Fatal("expected second turn, got nil")
	}
	if len(turn2.ReferencedTraceIDs) != 2 {
		t.Errorf("expected 2 trace refs, got %d", len(turn2.ReferencedTraceIDs))
	}
	if len(turn2.ReferencedSpanIDs) != 2 {
		t.Errorf("expected 2 span refs, got %d", len(turn2.ReferencedSpanIDs))
	}
	if session.TurnCount() != 2 {
		t.Errorf("expected 2 turns, got %d", session.TurnCount())
	}
}

func TestAnalysisSession_AddTurn_Completed(t *testing.T) {
	session := NewSession(nil, nil)
	session.AddTurn("first", "response", nil, nil)
	session.Close()

	// Adding a turn after Close should return nil.
	turn := session.AddTurn("second", "response", nil, nil)
	if turn != nil {
		t.Error("expected nil turn after session closed")
	}
	if session.State != SessionCompleted {
		t.Errorf("expected completed state, got %s", session.State)
	}
	if session.TurnCount() != 1 {
		t.Errorf("expected 1 turn after close, got %d", session.TurnCount())
	}
}

func TestAnalysisSession_LastTurn(t *testing.T) {
	session := NewSession(nil, nil)
	if session.LastTurn() != nil {
		t.Error("expected nil for empty session")
	}

	session.AddTurn("q", "a", nil, nil)
	last := session.LastTurn()
	if last == nil {
		t.Fatal("expected last turn")
	}
	if last.UserQuery != "q" {
		t.Errorf("unexpected query: %s", last.UserQuery)
	}
}

func TestAnalysisSession_StateTransitions(t *testing.T) {
	session := NewSession(nil, nil)

	if session.State != SessionActive {
		t.Error("expected initial state active")
	}

	session.Pause()
	if session.State != SessionPaused {
		t.Errorf("expected paused after Pause(), got %s", session.State)
	}

	// Pause on paused should be a no-op.
	session.Pause()
	if session.State != SessionPaused {
		t.Error("Pause() on paused should be a no-op")
	}

	session.Resume()
	if session.State != SessionActive {
		t.Errorf("expected active after Resume(), got %s", session.State)
	}

	session.Close()
	if session.State != SessionCompleted {
		t.Errorf("expected completed after Close(), got %s", session.State)
	}

	// Resume on completed should be a no-op.
	session.Resume()
	if session.State != SessionCompleted {
		t.Error("Resume() on completed should be a no-op")
	}
}

func TestAnalysisSession_FollowUpSuggestions(t *testing.T) {
	session := NewSession([]string{"trace-1"}, []agent.FailureMode{
		{ID: "fm-1", Description: "critical hallucination detected", Severity: "critical", Category: "hallucination"},
	})

	session.AddTurn("Analyze this trace.", "Found critical hallucination pattern.", nil, nil)

	suggestions := session.GetFollowUpSuggestions()
	if len(suggestions) == 0 {
		t.Fatal("expected follow-up suggestions")
	}

	// The suggestion should reference the failure mode category and description.
	if suggestions[0] == "" {
		t.Error("expected non-empty follow-up suggestion")
	}
}

func TestAnalysisSession_FollowUpSuggestionsFallback(t *testing.T) {
	// No failure modes — should get generic suggestions.
	session := NewSession([]string{"trace-1"}, nil)
	session.AddTurn("Analyze.", "Something was found.", nil, nil)

	suggestions := session.GetFollowUpSuggestions()
	if len(suggestions) != 3 {
		t.Errorf("expected 3 generic suggestions, got %d: %v", len(suggestions), suggestions)
	}
}

func TestAnalysisSession_FollowUpSuggestions_Empty(t *testing.T) {
	session := NewSession(nil, nil)

	suggestions := session.GetFollowUpSuggestions()
	if suggestions != nil {
		t.Errorf("expected nil for session with no turns, got %v", suggestions)
	}
}

func TestAnalysisSession_SetMetadata(t *testing.T) {
	session := NewSession(nil, nil)
	originalUpdatedAt := session.UpdatedAt

	session.SetMetadata("key1", "value1")
	if session.Metadata["key1"] != "value1" {
		t.Error("metadata not set correctly")
	}
	if !session.UpdatedAt.After(originalUpdatedAt) {
		t.Error("UpdatedAt should have been updated")
	}

	// Overwrite.
	session.SetMetadata("key1", "value2")
	if session.Metadata["key1"] != "value2" {
		t.Error("metadata overwrite failed")
	}
}

func TestAnalysisSession_ExportJSON(t *testing.T) {
	session := NewSession([]string{"trace-1"}, []agent.FailureMode{
		{ID: "fm-1", Description: "test", Severity: "high", Category: "semantic"},
	})
	session.AddTurn("query a", "response a", []string{"trace-1"}, nil)
	session.SetMetadata("analyst", "auto")

	data, err := session.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON failed: %v", err)
	}

	var decoded struct {
		SessionID    string            `json:"session_id"`
		TraceIDs     []string          `json:"trace_ids"`
		FailureModes []agent.FailureMode `json:"failure_modes"`
		Turns        []ConversationTurn `json:"turns"`
		State        string            `json:"state"`
		Metadata     map[string]string `json:"metadata"`
	}

	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.SessionID != session.SessionID {
		t.Errorf("session ID mismatch: got %s, want %s", decoded.SessionID, session.SessionID)
	}
	if len(decoded.TraceIDs) != 1 {
		t.Errorf("expected 1 trace ID, got %d", len(decoded.TraceIDs))
	}
	if len(decoded.FailureModes) != 1 {
		t.Errorf("expected 1 failure mode, got %d", len(decoded.FailureModes))
	}
	if len(decoded.Turns) != 1 {
		t.Errorf("expected 1 turn, got %d", len(decoded.Turns))
	}
	if decoded.Turns[0].ReferencedTraceIDs[0] != "trace-1" {
		t.Error("trace ref not preserved in export")
	}
	if decoded.State != "active" {
		t.Errorf("expected state 'active', got %s", decoded.State)
	}
	if decoded.Metadata["analyst"] != "auto" {
		t.Error("metadata not preserved in export")
	}
}

func TestAnalysisSession_ExportJSON_CompletedState(t *testing.T) {
	session := NewSession(nil, nil)
	session.AddTurn("q", "a", nil, nil)
	session.Close()

	data, err := session.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON failed: %v", err)
	}

	var decoded struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.State != "completed" {
		t.Errorf("expected state 'completed', got %s", decoded.State)
	}
}

func TestSessionManager_CreateGetDelete(t *testing.T) {
	mgr := NewAnalysisSessionManager(t.TempDir())

	traceIDs := []string{"trace-1", "trace-2"}
	failureModes := []agent.FailureMode{
		{ID: "fm-1", Description: "test", Severity: "low", Category: "semantic"},
	}

	session := mgr.CreateSession(traceIDs, failureModes)
	if session == nil {
		t.Fatal("expected session from CreateSession")
	}

	// Create another.
	session2 := mgr.CreateSession([]string{"trace-3"}, nil)
	if session2 == nil {
		t.Fatal("expected second session")
	}

	// GetSession should return the same session.
	retrieved := mgr.GetSession(session.SessionID)
	if retrieved == nil {
		t.Fatal("GetSession returned nil")
	}
	if retrieved.SessionID != session.SessionID {
		t.Error("GetSession returned wrong session")
	}

	// GetSession for non-existent should return nil.
	missing := mgr.GetSession("nonexistent")
	if missing != nil {
		t.Error("expected nil for missing session")
	}
}

func TestSessionManager_ListSessions(t *testing.T) {
	mgr := NewAnalysisSessionManager(t.TempDir())

	// Create sessions with staggered timestamps for deterministic ordering.
	_ = mgr.CreateSession([]string{"trace-a"}, nil)
	time.Sleep(time.Millisecond)
	_ = mgr.CreateSession([]string{"trace-b"}, nil)
	time.Sleep(time.Millisecond)
	s3 := mgr.CreateSession([]string{"trace-c"}, nil)

	all := mgr.ListSessions()
	if len(all) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(all))
	}

	// Verify order: newest first.
	if all[0] == s3 || all[1] == s3 || all[2] == s3 {
		// s3 was created last, should be first.
		if all[0] != s3 {
			t.Error("expected s3 as first (newest)")
		}
	} else {
		t.Error("sessions not sorted newest first")
	}
}

func TestSessionManager_DeleteSession(t *testing.T) {
	mgr := NewAnalysisSessionManager(t.TempDir())

	session := mgr.CreateSession(nil, nil)

	err := mgr.DeleteSession(session.SessionID)
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	retrieved := mgr.GetSession(session.SessionID)
	if retrieved != nil {
		t.Error("expected nil after delete")
	}

	// Delete again should error.
	err = mgr.DeleteSession(session.SessionID)
	if err == nil {
		t.Error("expected error deleting non-existent session")
	}
}

func TestSessionManager_ExportSession(t *testing.T) {
	dir := t.TempDir()
	mgr := NewAnalysisSessionManager(dir)

	session := mgr.CreateSession([]string{"trace-1"}, nil)
	session.AddTurn("query", "response", []string{"trace-1"}, []string{"span-1"})

	err := mgr.ExportSession(session.SessionID, "test-export.json")
	if err != nil {
		t.Fatalf("ExportSession failed: %v", err)
	}

	path := filepath.Join(dir, "test-export.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	// Verify the exported JSON contains expected data.
	var decoded struct {
		SessionID string `json:"session_id"`
		TraceIDs  []string `json:"trace_ids"`
		Turns     []ConversationTurn `json:"turns"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.SessionID != session.SessionID {
		t.Error("session ID mismatch in export")
	}
	if len(decoded.TraceIDs) != 1 {
		t.Error("trace IDs not preserved in export")
	}
	if len(decoded.Turns) != 1 {
		t.Error("turns not preserved in export")
	}
}

func TestSessionManager_SessionCount(t *testing.T) {
	mgr := NewAnalysisSessionManager(t.TempDir())

	if mgr.SessionCount() != 0 {
		t.Errorf("expected 0 sessions, got %d", mgr.SessionCount())
	}

	mgr.CreateSession(nil, nil)
	mgr.CreateSession(nil, nil)

	if mgr.SessionCount() != 2 {
		t.Errorf("expected 2 sessions, got %d", mgr.SessionCount())
	}
}

func TestSessionManager_ExportSession_NotFound(t *testing.T) {
	mgr := NewAnalysisSessionManager(t.TempDir())

	err := mgr.ExportSession("nonexistent", "output.json")
	if err == nil {
		t.Error("expected error for non-existent session")
	}
}

func TestSessionState_String(t *testing.T) {
	tests := []struct {
		state  SessionState
		expect string
	}{
		{SessionActive, "active"},
		{SessionPaused, "paused"},
		{SessionCompleted, "completed"},
		{SessionExported, "exported"},
		{SessionState(99), "unknown"},
	}

	for _, tt := range tests {
		if tt.state.String() != tt.expect {
			t.Errorf("State(%d).String() = %s, want %s", tt.state, tt.state.String(), tt.expect)
		}
	}
}
