package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// -----------------------------------------------------------------------
// WriteCheckpoint / ReadCheckpoint round-trip
// -----------------------------------------------------------------------

func TestRunRecoverer_WriteReadCheckpoint(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	cp := &Checkpoint{
		RunID:   "run-test-001",
		AgentID: "researcher-v2",
		Depth:   2,
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 5, Limit: 20},
		ToolCalls: []TurnRecord{
			{Name: "view_trace", Output: "trace data", Success: true},
		},
		LLMMessages: []LLMMessage{
			{Role: "user", Content: "Analyze this trace."},
			{Role: "assistant", Content: "Found the issue."},
		},
		Outputs: "trace identified",
	}

	if err := r.WriteCheckpoint(cp); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	// Read it back.
	read, err := r.ReadCheckpoint("run-test-001")
	if err != nil {
		t.Fatalf("ReadCheckpoint: %v", err)
	}

	if read.RunID != "run-test-001" {
		t.Errorf("RunID: got %s, want run-test-001", read.RunID)
	}
	if read.AgentID != "researcher-v2" {
		t.Errorf("AgentID: got %s, want researcher-v2", read.AgentID)
	}
	if read.Depth != 2 {
		t.Errorf("Depth: got %d, want 2", read.Depth)
	}
	if read.TurnCounter.Current != 5 {
		t.Errorf("TurnCounter.Current: got %d, want 5", read.TurnCounter.Current)
	}
	if read.TurnCounter.Limit != 20 {
		t.Errorf("TurnCounter.Limit: got %d, want 20", read.TurnCounter.Limit)
	}
	if len(read.ToolCalls) != 1 {
		t.Fatalf("ToolCalls: got %d, want 1", len(read.ToolCalls))
	}
	if read.ToolCalls[0].Name != "view_trace" {
		t.Errorf("ToolCalls[0].Name: got %s, want view_trace", read.ToolCalls[0].Name)
	}
	if len(read.LLMMessages) != 2 {
		t.Fatalf("LLMMessages: got %d, want 2", len(read.LLMMessages))
	}
	if read.Outputs != "trace identified" {
		t.Errorf("Outputs: got %s, want 'trace identified'", read.Outputs)
	}
}

func TestRunRecoverer_WriteNilCheckpoint(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	err = r.WriteCheckpoint(nil)
	if err == nil {
		t.Fatal("expected error for nil checkpoint, got nil")
	}
}

func TestRunRecoverer_AutoRunID(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	cp := &Checkpoint{
		AgentID: "auto-agent",
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 1, Limit: 10},
	}

	if err := r.WriteCheckpoint(cp); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	// Verify a file was written.
	runs, err := r.ListIncompleteRuns()
	if err != nil {
		t.Fatalf("ListIncompleteRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("Expected 1 incomplete run, got %d", len(runs))
	}

	// Read it back by the auto-generated ID.
	read, err := r.ReadCheckpoint(runs[0])
	if err != nil {
		t.Fatalf("ReadCheckpoint: %v", err)
	}
	if read.AgentID != "auto-agent" {
		t.Errorf("AgentID: got %s, want auto-agent", read.AgentID)
	}
}

func TestRunRecoverer_ReadCheckpoint_NotFound(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	_, err = r.ReadCheckpoint("nonexistent-run")
	if err == nil {
		t.Fatal("expected error for missing checkpoint, got nil")
	}
}

// -----------------------------------------------------------------------
// MarkComplete / IsComplete
// -----------------------------------------------------------------------

func TestRunRecoverer_MarkComplete(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	cp := &Checkpoint{
		RunID: "run-complete",
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 10, Limit: 20},
	}

	if err := r.WriteCheckpoint(cp); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	// Should be incomplete.
	incomplete, err := r.ListIncompleteRuns()
	if err != nil {
		t.Fatalf("ListIncompleteRuns before complete: %v", err)
	}
	if len(incomplete) != 1 {
		t.Fatalf("Incomplete before mark: got %d, want 1", len(incomplete))
	}

	if r.IsComplete("run-complete") {
		t.Fatal("expected run to be incomplete before MarkComplete")
	}

	// Mark complete.
	if err := r.MarkComplete("run-complete"); err != nil {
		t.Fatalf("MarkComplete: %v", err)
	}

	// Should no longer be in incomplete list.
	incomplete, err = r.ListIncompleteRuns()
	if err != nil {
		t.Fatalf("ListIncompleteRuns after complete: %v", err)
	}
	if len(incomplete) != 0 {
		t.Errorf("Incomplete after mark: got %d, want 0", len(incomplete))
	}

	// Should show in completed list.
	completed, err := r.ListCompletedRuns()
	if err != nil {
		t.Fatalf("ListCompletedRuns: %v", err)
	}
	if len(completed) != 1 {
		t.Fatalf("Completed: got %d, want 1", len(completed))
	}
	if completed[0] != "run-complete" {
		t.Errorf("Completed[0]: got %s, want run-complete", completed[0])
	}

	// IsComplete should be true.
	if !r.IsComplete("run-complete") {
		t.Error("IsComplete should return true after MarkComplete")
	}
}

// -----------------------------------------------------------------------
// ListIncompleteRuns
// -----------------------------------------------------------------------

func TestRunRecoverer_ListIncompleteRuns(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	// Empty should return nil/empty.
	runs, err := r.ListIncompleteRuns()
	if err != nil {
		t.Fatalf("ListIncompleteRuns (empty): %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("Empty incomplete runs: got %d, want 0", len(runs))
	}

	// Write some checkpoints.
	for _, runID := range []string{"run-b", "run-a", "run-c"} {
		cp := &Checkpoint{
			RunID: runID,
			TurnCounter: struct {
				Current int `json:"current"`
				Limit   int `json:"limit"`
			}{Current: 3, Limit: 10},
		}
		if err := r.WriteCheckpoint(cp); err != nil {
			t.Fatalf("WriteCheckpoint(%s): %v", runID, err)
		}
	}

	runs, err = r.ListIncompleteRuns()
	if err != nil {
		t.Fatalf("ListIncompleteRuns: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("Incomplete runs: got %d, want 3", len(runs))
	}
	// Should be sorted.
	if runs[0] != "run-a" || runs[1] != "run-b" || runs[2] != "run-c" {
		t.Errorf("Not sorted: got %v", runs)
	}

	// Mark one complete and verify it is removed from incomplete.
	r.MarkComplete("run-b")
	runs, err = r.ListIncompleteRuns()
	if err != nil {
		t.Fatalf("ListIncompleteRuns after partial complete: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("Incomplete after partial complete: got %d, want 2", len(runs))
	}
}

// -----------------------------------------------------------------------
// ShouldCheckpoint
// -----------------------------------------------------------------------

func TestRunRecoverer_ShouldCheckpoint(t *testing.T) {
	tests := []struct {
		name     string
		interval int
		turn     int
		want     bool
	}{
		{"disabled zero interval", 0, 1, false},
		{"disabled negative interval", -1, 5, false},
		{"turn 0 never", 3, 0, false},
		{"every 3: turn 3", 3, 3, true},
		{"every 3: turn 4", 3, 4, false},
		{"every 3: turn 6", 3, 6, true},
		{"every 1: every turn", 1, 1, true},
		{"every 1: turn 0", 1, 0, false},
		{"interval 5: turn 10", 5, 10, true},
		{"interval 5: turn 9", 5, 9, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			r, err := NewRunRecoverer(dir, tt.interval)
			if err != nil {
				t.Fatalf("NewRunRecoverer: %v", err)
			}
			got := r.ShouldCheckpoint(tt.turn)
			if got != tt.want {
				t.Errorf("ShouldCheckpoint(%d): got %v, want %v", tt.turn, got, tt.want)
			}
		})
	}
}

// -----------------------------------------------------------------------
// Resume / ResumeResult
// -----------------------------------------------------------------------

func TestRunRecoverer_Resume(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	// Write a checkpoint.
	cp := &Checkpoint{
		RunID:   "run-resume",
		AgentID: "researcher-v1",
		Depth:   3,
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 7, Limit: 20},
		ToolCalls: []TurnRecord{
			{Name: "read_file", Output: "file contents", Success: true},
			{Name: "search_code", Output: "match found", Success: true},
		},
		LLMMessages: []LLMMessage{
			{Role: "user", Content: "Find the bug."},
			{Role: "assistant", Content: "Searching codebase."},
			{Role: "assistant", Content: "Found suspicious call."},
		},
		Outputs: "likely null ptr at line 42",
	}

	if err := r.WriteCheckpoint(cp); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	// Resume.
	result, err := r.Resume("run-resume")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if !result.Restored {
		t.Error("ResumeResult.Restored should be true")
	}
	if result.RunID != "run-resume" {
		t.Errorf("RunID: got %s, want run-resume", result.RunID)
	}
	if result.SkippedTurns != 7 {
		t.Errorf("SkippedTurns: got %d, want 7", result.SkippedTurns)
	}
	if result.Checkpoint == nil {
		t.Fatal("Checkpoint should not be nil")
	}
	if result.Checkpoint.AgentID != "researcher-v1" {
		t.Errorf("Checkpoint.AgentID: got %s, want researcher-v1", result.Checkpoint.AgentID)
	}
	if result.Checkpoint.Depth != 3 {
		t.Errorf("Checkpoint.Depth: got %d, want 3", result.Checkpoint.Depth)
	}
	if result.Checkpoint.Outputs != "likely null ptr at line 42" {
		t.Errorf("Checkpoint.Outputs: got %s", result.Checkpoint.Outputs)
	}

	// Verify State is nil (caller populates).
	if result.State != nil {
		t.Error("State should be nil before caller populates")
	}

	// Verify ResumedAt is set.
	if result.ResumedAt.IsZero() {
		t.Error("ResumedAt should not be zero")
	}
}

func TestRunRecoverer_ResumeNonExistent(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	_, err = r.Resume("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent run, got nil")
	}
}

func TestRunRecoverer_ResumeFromLatest(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	// Write checkpoints with different timestamps.
	older := time.Now().Add(-1 * time.Hour)
	younger := time.Now()

	cp1 := &Checkpoint{
		RunID:   "older-run",
		AgentID: "agent-a",
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 3, Limit: 10},
		Timestamp: older,
	}
	if err := r.WriteCheckpoint(cp1); err != nil {
		t.Fatalf("WriteCheckpoint cp1: %v", err)
	}

	cp2 := &Checkpoint{
		RunID:   "younger-run",
		AgentID: "agent-b",
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 5, Limit: 15},
		Timestamp: younger,
	}
	if err := r.WriteCheckpoint(cp2); err != nil {
		t.Fatalf("WriteCheckpoint cp2: %v", err)
	}

	result, runID, err := r.ResumeFromLatest()
	if err != nil {
		t.Fatalf("ResumeFromLatest: %v", err)
	}

	if runID != "younger-run" {
		t.Errorf("RunID: got %s, want younger-run", runID)
	}
	if result == nil {
		t.Fatal("ResumeResult should not be nil")
	}
	if !result.Restored {
		t.Error("Should be restored")
	}
	if result.Checkpoint.RunID != "younger-run" {
		t.Errorf("Checkpoint.RunID: got %s, want younger-run", result.Checkpoint.RunID)
	}
}

func TestRunRecoverer_ResumeFromLatest_NoIncomplete(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	_, _, err = r.ResumeFromLatest()
	if err == nil {
		t.Fatal("expected error when no incomplete runs, got nil")
	}
}

func TestRunRecoverer_CheckpointAtTurnZero(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	// Write a checkpoint at turn 0.
	cp := &Checkpoint{
		RunID:   "turn-zero",
		AgentID: "new-agent",
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 0, Limit: 10},
	}
	if err := r.WriteCheckpoint(cp); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	result, err := r.Resume("turn-zero")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if result.SkippedTurns != 0 {
		t.Errorf("SkippedTurns: got %d, want 0", result.SkippedTurns)
	}
	if result.Warning == "" {
		t.Error("expected warning for checkpoint at turn 0")
	} else if !strings.Contains(result.Warning, "turn 0") {
		t.Errorf("warning should mention turn 0: %s", result.Warning)
	}
}

// -----------------------------------------------------------------------
// Turn-based checkpoint gating
// -----------------------------------------------------------------------

func TestRunRecoverer_CheckpointIntervalIntegration(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 5)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	// Only turn 5, 10 should trigger checkpoints.
	for turn := 1; turn <= 12; turn++ {
		should := r.ShouldCheckpoint(turn)
		isCheckpoint := (turn % 5 == 0) && turn > 0
		if should != isCheckpoint {
			t.Errorf("turn %d: ShouldCheckpoint=%v, want %v",
				turn, should, isCheckpoint)
		}
	}
}

// -----------------------------------------------------------------------
// SnapshotForTurn helper
// -----------------------------------------------------------------------

func TestSnapshotForTurn(t *testing.T) {
	cp := SnapshotForTurn(
		"run-123",
		"analyst-v1",
		1,
		8,
		15,
		[]TurnRecord{
			{Name: "read_file", Output: "data"},
		},
		[]LLMMessage{
			{Role: "user", Content: "analyze"},
		},
		"analysis result",
	)

	if cp.RunID != "run-123" {
		t.Errorf("RunID: got %s, want run-123", cp.RunID)
	}
	if cp.AgentID != "analyst-v1" {
		t.Errorf("AgentID: got %s, want analyst-v1", cp.AgentID)
	}
	if cp.Depth != 1 {
		t.Errorf("Depth: got %d, want 1", cp.Depth)
	}
	if cp.TurnCounter.Current != 8 {
		t.Errorf("Current: got %d, want 8", cp.TurnCounter.Current)
	}
	if cp.TurnCounter.Limit != 15 {
		t.Errorf("Limit: got %d, want 15", cp.TurnCounter.Limit)
	}
	if len(cp.ToolCalls) != 1 {
		t.Errorf("ToolCalls: got %d, want 1", len(cp.ToolCalls))
	}
	if cp.ToolCalls[0].Name != "read_file" {
		t.Errorf("ToolCalls[0].Name: got %s", cp.ToolCalls[0].Name)
	}
	if cp.Outputs != "analysis result" {
		t.Errorf("Outputs: got %s", cp.Outputs)
	}
}

// -----------------------------------------------------------------------
// Cleanup
// -----------------------------------------------------------------------

func TestRunRecoverer_CleanupCompleted(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	// Write and complete some runs.
	for _, runID := range []string{"run-a", "run-b", "run-c"} {
		cp := &Checkpoint{
			RunID:   runID,
			AgentID: "test",
			TurnCounter: struct {
				Current int `json:"current"`
				Limit   int `json:"limit"`
			}{Current: 1, Limit: 5},
		}
		if err := r.WriteCheckpoint(cp); err != nil {
			t.Fatalf("WriteCheckpoint(%s): %v", runID, err)
		}
		if err := r.MarkComplete(runID); err != nil {
			t.Fatalf("MarkComplete(%s): %v", runID, err)
		}
	}

	// All runs are complete and done.
	runs, _ := r.ListIncompleteRuns()
	if len(runs) != 0 {
		t.Errorf("Incomplete after all complete: got %d, want 0", len(runs))
	}

	// Clean up completed runs.
	removed, err := r.CleanupCompleted("run-a", "run-b")
	if err != nil {
		t.Fatalf("CleanupCompleted: %v", err)
	}
	if removed != 2 { // 1 file per run (only .done after MarkComplete renames .json to .done)
		t.Errorf("Removed: got %d, want 2", removed)
	}

	// run-c should still exist in completed.
	completed, _ := r.ListCompletedRuns()
	if len(completed) != 1 {
		t.Errorf("Completed after cleanup: got %d, want 1", len(completed))
	}

	// Cleanup non-existent should be no-op.
	n, _ := r.CleanupCompleted("nonexistent")
	if n != 0 {
		t.Errorf("Cleanup non-existent: got %d, want 0", n)
	}
}

func TestRunRecoverer_CleanupAllCompleted(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	// Write and complete 3 runs.
	for i := 'a'; i <= 'c'; i++ {
		runID := string(i)
		cp := &Checkpoint{
			RunID:   runID,
			AgentID: "test",
			TurnCounter: struct {
				Current int `json:"current"`
				Limit   int `json:"limit"`
			}{Current: 1, Limit: 5},
		}
		if err := r.WriteCheckpoint(cp); err != nil {
			t.Fatalf("WriteCheckpoint: %v", err)
		}
		if err := r.MarkComplete(runID); err != nil {
			t.Fatalf("MarkComplete: %v", err)
		}
	}

	// Also create an incomplete run that should survive.
	cp := &Checkpoint{
		RunID:   "still-running",
		AgentID: "test",
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 1, Limit: 5},
	}
	if err := r.WriteCheckpoint(cp); err != nil {
		t.Fatalf("WriteCheckpoint incomplete: %v", err)
	}

	removed, err := r.CleanupAllCompleted()
	if err != nil {
		t.Fatalf("CleanupAllCompleted: %v", err)
	}
	_ = removed

	incomplete, _ := r.ListIncompleteRuns()
	if len(incomplete) != 1 {
		t.Errorf("Incomplete after cleanup: got %d, want 1", len(incomplete))
	}
	if len(incomplete) > 0 && incomplete[0] != "still-running" {
		t.Errorf("Incomplete run: got %s, want still-running", incomplete[0])
	}
}

func TestRunRecoverer_CleanupBefore(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	// Write a checkpoint from 2 hours ago.
	cutoff := time.Now().Add(-1 * time.Hour)

	cp := &Checkpoint{
		RunID:   "old-run",
		AgentID: "test",
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 3, Limit: 10},
		Timestamp: cutoff.Add(-1 * time.Hour), // 3 hours ago
	}
	if err := r.WriteCheckpoint(cp); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	// And one recent checkpoint.
	cp2 := &Checkpoint{
		RunID:   "new-run",
		AgentID: "test",
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 1, Limit: 10},
		Timestamp: time.Now(),
	}
	if err := r.WriteCheckpoint(cp2); err != nil {
		t.Fatalf("WriteCheckpoint recent: %v", err)
	}

	removed, err := r.CleanupBefore(cutoff)
	if err != nil {
		t.Fatalf("CleanupBefore: %v", err)
	}
	if removed != 1 {
		t.Errorf("Removed: got %d, want 1", removed)
	}

	// Only new-run should remain.
	incomplete, _ := r.ListIncompleteRuns()
	if len(incomplete) != 1 {
		t.Errorf("Incomplete after cleanup: got %d, want 1", len(incomplete))
	}
	if len(incomplete) > 0 && incomplete[0] != "new-run" {
		t.Errorf("Remaining incomplete: got %s, want new-run", incomplete[0])
	}
}

// -----------------------------------------------------------------------
// IncompleteRunCount
// -----------------------------------------------------------------------

func TestRunRecoverer_IncompleteRunCount(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	count, err := r.IncompleteRunCount()
	if err != nil {
		t.Fatalf("IncompleteRunCount: %v", err)
	}
	if count != 0 {
		t.Errorf("Empty: got %d, want 0", count)
	}

	r.WriteCheckpoint(&Checkpoint{
		RunID:   "run-1",
		AgentID: "test",
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 1, Limit: 10},
	})
	r.WriteCheckpoint(&Checkpoint{
		RunID:   "run-2",
		AgentID: "test",
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 2, Limit: 10},
	})

	count, err = r.IncompleteRunCount()
	if err != nil {
		t.Fatalf("IncompleteRunCount: %v", err)
	}
	if count != 2 {
		t.Errorf("After 2 checkpoints: got %d, want 2", count)
	}
}

// -----------------------------------------------------------------------
// SetCheckpointInterval getter setter
// -----------------------------------------------------------------------

func TestRunRecoverer_CheckpointIntervalMutators(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 5)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	if r.GetCheckpointInterval() != 5 {
		t.Errorf("Initial: got %d, want 5", r.GetCheckpointInterval())
	}

	r.SetCheckpointInterval(10)
	if r.GetCheckpointInterval() != 10 {
		t.Errorf("After set: got %d, want 10", r.GetCheckpointInterval())
	}
}

// -----------------------------------------------------------------------
// Crash recovery: corrupt file + valid checkpoint
// -----------------------------------------------------------------------

func TestRunRecoverer_CrashRecovery(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	// Write a valid checkpoint.
	cp1 := &Checkpoint{
		RunID:   "pre-crash",
		AgentID: "researcher-v1",
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 5, Limit: 20},
		LLMMessages: []LLMMessage{
			{Role: "assistant", Content: "I found the bug."},
		},
	}
	if err := r.WriteCheckpoint(cp1); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	// Write a corrupt checkpoint file to simulate crash mid-write.
	corruptPath := filepath.Join(dir, checkpointPrefix+"post_crash.json")
	if err := os.WriteFile(corruptPath, []byte("{invalid json"), 0o644); err != nil {
		t.Fatalf("WriteFile corrupt: %v", err)
	}

	// ListIncompleteRuns should pick up both as filenames (they have the
	// right naming pattern). The recoverer does not validate content on
	// listing -- it just returns filenames.
	runs, err := r.ListIncompleteRuns()
	if err != nil {
		t.Fatalf("ListIncompleteRuns after crash: %v", err)
	}
	if len(runs) != 2 {
		// Both pre-crash and post_crash files are found in the directory
		t.Errorf("Incomplete runs after crash: got %d, want 2 (one valid, one corrupt)", len(runs))
	}

	// Resume of pre-crash should work.
	result, err := r.Resume("pre-crash")
	if err != nil {
		t.Fatalf("Resume pre-crash: %v", err)
	}
	if !result.Restored {
		t.Error("pre-crash should be restored")
	}
	if len(result.Checkpoint.LLMMessages) != 1 {
		t.Errorf("LLMMessages: got %d, want 1", len(result.Checkpoint.LLMMessages))
	}

	// Resume of corrupt should fail.
	_, err = r.Resume("post_crash")
	if err == nil {
		t.Fatal("Resume of corrupt checkpoint should fail")
	}
}

// -----------------------------------------------------------------------
// Resume warning: turn limit exceeded
// -----------------------------------------------------------------------

func TestRunRecoverer_ResumeAtLimit(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	cp := &Checkpoint{
		RunID:   "at-limit",
		AgentID: "researcher-v1",
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 10, Limit: 10},
	}

	if err := r.WriteCheckpoint(cp); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	result, err := r.Resume("at-limit")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if result.Warning == "" {
		t.Error("expected warning for checkpoint at turn limit")
	}
}

// -----------------------------------------------------------------------
// Resume warning: stale checkpoint
// -----------------------------------------------------------------------

func TestRunRecoverer_ResumeStaleCheckpoint(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	cp := &Checkpoint{
		RunID:   "stale",
		AgentID: "researcher-v1",
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 3, Limit: 10},
		Timestamp: time.Now().Add(-8 * 24 * time.Hour), // 8 days ago
	}

	if err := r.WriteCheckpoint(cp); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	result, err := r.Resume("stale")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if result.Warning == "" {
		t.Error("expected warning for stale checkpoint")
	}
}

// -----------------------------------------------------------------------
// Resume round-trip with SnapshotForTurn
// -----------------------------------------------------------------------

func TestRunRecoverer_ResumeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 5)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	original := SnapshotForTurn(
		"roundtrip",
		"analyst-v2",
		0,
		12,
		30,
		[]TurnRecord{
			{Name: "grep", Input: map[string]any{"pattern": "TODO"}, Output: "found 3 matches"},
			{Name: "view_file", Input: map[string]any{"path": "/src/main.go"}, Output: "file contents"},
		},
		[]LLMMessage{
			{Role: "user", Content: "Find all TODO comments."},
			{Role: "assistant", Content: "Scanning for TODOs..."},
			{Role: "assistant", Content: "Found 3 TODOs in main.go and utils.go."},
		},
		"3 TODO comments found across 2 files.",
	)

	if err := r.WriteCheckpoint(original); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	result, err := r.Resume("roundtrip")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if !result.Restored {
		t.Fatal("Should be restored")
	}
	if result.SkippedTurns != 12 {
		t.Errorf("SkippedTurns: got %d, want 12", result.SkippedTurns)
	}
	if result.Checkpoint.RunID != "roundtrip" {
		t.Errorf("RunID: got %s, want roundtrip", result.Checkpoint.RunID)
	}
	if result.Checkpoint.AgentID != "analyst-v2" {
		t.Errorf("AgentID: got %s, want analyst-v2", result.Checkpoint.AgentID)
	}
	if result.Checkpoint.Depth != 0 {
		t.Errorf("Depth: got %d, want 0", result.Checkpoint.Depth)
	}
	if result.Checkpoint.TurnCounter.Current != 12 {
		t.Errorf("Current: got %d, want 12", result.Checkpoint.TurnCounter.Current)
	}
	if result.Checkpoint.TurnCounter.Limit != 30 {
		t.Errorf("Limit: got %d, want 30", result.Checkpoint.TurnCounter.Limit)
	}
	if len(result.Checkpoint.ToolCalls) != 2 {
		t.Errorf("ToolCalls: got %d, want 2", len(result.Checkpoint.ToolCalls))
	}
	if result.Checkpoint.ToolCalls[0].Name != "grep" {
		t.Errorf("ToolCalls[0].Name: got %s", result.Checkpoint.ToolCalls[0].Name)
	}
	if len(result.Checkpoint.LLMMessages) != 3 {
		t.Errorf("LLMMessages: got %d, want 3", len(result.Checkpoint.LLMMessages))
	}
	if result.Checkpoint.Outputs != "3 TODO comments found across 2 files." {
		t.Errorf("Outputs: got %s", result.Checkpoint.Outputs)
	}
}

// -----------------------------------------------------------------------
// Multiple resume-from-latest picks newest timestamp
// -----------------------------------------------------------------------

func TestRunRecoverer_ResumeFromLatest_PicksNewest(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	time1 := time.Now().Add(-5 * time.Minute)
	time2 := time.Now().Add(-3 * time.Minute)
	time3 := time.Now().Add(-1 * time.Minute)

	for i, tc := range []struct {
		runID   string
		ts      time.Time
		turn    int
	}{
		{"z-last-written", time1, 1},
		{"a-first-written", time3, 3},
		{"m-middle-written", time2, 2},
	} {
		cp := &Checkpoint{
			RunID:   tc.runID,
			AgentID: "agent",
			Depth:   i,
			TurnCounter: struct {
				Current int `json:"current"`
				Limit   int `json:"limit"`
			}{Current: tc.turn, Limit: 10},
			Timestamp: tc.ts,
		}
		if err := r.WriteCheckpoint(cp); err != nil {
			t.Fatalf("WriteCheckpoint %s: %v", tc.runID, err)
		}
	}

	result, runID, err := r.ResumeFromLatest()
	if err != nil {
		t.Fatalf("ResumeFromLatest: %v", err)
	}

	// Should pick the one with the newest timestamp: a-first-written
	if runID != "a-first-written" {
		t.Errorf("RunID: got %s, want a-first-written", runID)
	}
	if result.Checkpoint.RunID != "a-first-written" {
		t.Errorf("Checkpoint.RunID: got %s, want a-first-written", result.Checkpoint.RunID)
	}
}

// -----------------------------------------------------------------------
// MarkComplete on non-existent run is idempotent
// -----------------------------------------------------------------------

func TestRunRecoverer_MarkCompleteIdempotent(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	// Marking non-existent run is not an error.
	err = r.MarkComplete("nonexistent")
	if err != nil {
		t.Fatalf("MarkComplete on non-existent: %v", err)
	}
}

// -----------------------------------------------------------------------
// FinalSentinelContent
// -----------------------------------------------------------------------

func TestFinalSentinelContent(t *testing.T) {
	sentinel := FinalSentinelContent()
	if sentinel != checkpointFinalSentinel {
		t.Errorf("sentinel: got %s, want %s", sentinel, checkpointFinalSentinel)
	}
}

// -----------------------------------------------------------------------
// ListIncompleteRuns ignores non-checkpoint files
// -----------------------------------------------------------------------

func TestRunRecoverer_ListIgnoresUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunRecoverer(dir, 3)
	if err != nil {
		t.Fatalf("NewRunRecoverer: %v", err)
	}

	// Write a checkpoint.
	r.WriteCheckpoint(&Checkpoint{
		RunID:   "run-a",
		AgentID: "test",
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: 1, Limit: 5},
	})

	// Add unrelated files.
	os.WriteFile(filepath.Join(dir, "some-log.txt"), []byte("log data"), 0o644)
	os.WriteFile(filepath.Join(dir, "checkpoint_backup.json.bak"), []byte{}, 0o644)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)

	runs, err := r.ListIncompleteRuns()
	if err != nil {
		t.Fatalf("ListIncompleteRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("Incomplete runs: got %d, want 1", len(runs))
	}
	if runs[0] != "run-a" {
		t.Errorf("Incomplete run: got %s, want run-a", runs[0])
	}
}
