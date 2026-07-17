package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// -----------------------------------------------------------------------
// AtomicTurnLogger tests
// -----------------------------------------------------------------------

func TestAtomicTurnLogger_WriteSnapshot(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAtomicTurnLogger(dir)
	if err != nil {
		t.Fatalf("NewAtomicTurnLogger: %v", err)
	}

	snap := TurnSnapshot{
		Sequence: 1,
		TurnID:   "turn-test-001",
		ToolCalls: []TurnRecord{
			{Name: "view_trace", Input: map[string]any{"trace_id": "abc123"}},
		},
		LLMMessages: []LLMMessage{
			{Role: "assistant", Content: "Let me check that trace."},
		},
		Output:   "trace found",
		Metadata: map[string]string{"employee": "test-agent"},
	}

	if err := logger.WriteSnapshot(snap); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}

	// Read it back.
	read, err := logger.ReadSnapshot("turn-test-001")
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}

	if read.Sequence != 1 {
		t.Errorf("Sequence: got %d, want 1", read.Sequence)
	}
	if read.TurnID != "turn-test-001" {
		t.Errorf("TurnID: got %s, want turn-test-001", read.TurnID)
	}
	if len(read.ToolCalls) != 1 {
		t.Fatalf("ToolCalls count: got %d, want 1", len(read.ToolCalls))
	}
	if read.ToolCalls[0].Name != "view_trace" {
		t.Errorf("ToolCalls[0].Name: got %s, want view_trace", read.ToolCalls[0].Name)
	}
	if read.Output != "trace found" {
		t.Errorf("Output: got %s, want 'trace found'", read.Output)
	}
	if read.Metadata["employee"] != "test-agent" {
		t.Errorf("Metadata[employee]: got %s, want test-agent", read.Metadata["employee"])
	}
}

func TestAtomicTurnLogger_AutoTurnID(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAtomicTurnLogger(dir)
	if err != nil {
		t.Fatalf("NewAtomicTurnLogger: %v", err)
	}

	snap := TurnSnapshot{Sequence: 5, Output: "auto-id test"}
	if err := logger.WriteSnapshot(snap); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}

	// List to find the generated turn ID.
	snapshots, err := logger.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("ListSnapshots count: got %d, want 1", len(snapshots))
	}

	id := snapshots[0].TurnID
	if !strings.HasPrefix(id, "turn_") {
		t.Errorf("TurnID should start with 'turn_': got %s", id)
	}
}

func TestAtomicTurnLogger_ListSnapshots(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAtomicTurnLogger(dir)
	if err != nil {
		t.Fatalf("NewAtomicTurnLogger: %v", err)
	}

	for i := 1; i <= 5; i++ {
		snap := TurnSnapshot{
			Sequence: i,
			TurnID:   "turn-seq-" + string(rune('0'+i)),
			Output:   string(rune('A' + i - 1)),
		}
		if err := logger.WriteSnapshot(snap); err != nil {
			t.Fatalf("WriteSnapshot[%d]: %v", i, err)
		}
	}

	snapshots, err := logger.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 5 {
		t.Fatalf("ListSnapshots count: got %d, want 5", len(snapshots))
	}
	// Should be sorted by sequence.
	for i, s := range snapshots {
		if s.Sequence != i+1 {
			t.Errorf("[%d].Sequence: got %d, want %d", i, s.Sequence, i+1)
		}
	}
}

func TestAtomicTurnLogger_ReadSnapshot_NotFound(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAtomicTurnLogger(dir)
	if err != nil {
		t.Fatalf("NewAtomicTurnLogger: %v", err)
	}

	_, err = logger.ReadSnapshot("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing snapshot, got nil")
	}
}

func TestAtomicTurnLogger_PruneSnapshots(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAtomicTurnLogger(dir)
	if err != nil {
		t.Fatalf("NewAtomicTurnLogger: %v", err)
	}

	for i := 1; i <= 10; i++ {
		snap := TurnSnapshot{
			Sequence: i,
			TurnID:   "turn-prune-" + string(rune('0'+i)),
		}
		if err := logger.WriteSnapshot(snap); err != nil {
			t.Fatalf("WriteSnapshot[%d]: %v", i, err)
		}
	}

	// Prune keeping last 3.
	deleted, err := logger.PruneSnapshots(3)
	if err != nil {
		t.Fatalf("PruneSnapshots: %v", err)
	}
	if deleted != 7 {
		t.Errorf("Deleted: got %d, want 7", deleted)
	}

	snapshots, err := logger.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 3 {
		t.Fatalf("ListSnapshots after prune: got %d, want 3", len(snapshots))
	}
	// Should be sequences 8, 9, 10.
	if snapshots[0].Sequence != 8 {
		t.Errorf("First remaining sequence: got %d, want 8", snapshots[0].Sequence)
	}
}

func TestAtomicTurnLogger_PruneSnapshots_NoOpWhenFew(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAtomicTurnLogger(dir)
	if err != nil {
		t.Fatalf("NewAtomicTurnLogger: %v", err)
	}

	for i := 1; i <= 3; i++ {
		if err := logger.WriteSnapshot(TurnSnapshot{
			Sequence: i,
			TurnID:   "turn-few-" + string(rune('0'+i)),
		}); err != nil {
			t.Fatalf("WriteSnapshot[%d]: %v", i, err)
		}
	}

	deleted, err := logger.PruneSnapshots(5) // want more than exist
	if err != nil {
		t.Fatalf("PruneSnapshots: %v", err)
	}
	if deleted != 0 {
		t.Errorf("Deleted (no-op): got %d, want 0", deleted)
	}
}

func TestAtomicTurnLogger_LastSequence(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAtomicTurnLogger(dir)
	if err != nil {
		t.Fatalf("NewAtomicTurnLogger: %v", err)
	}

	if seq := logger.LastSequence(); seq != 0 {
		t.Errorf("Empty LastSequence: got %d, want 0", seq)
	}

	for i := 1; i <= 5; i++ {
		snap := TurnSnapshot{Sequence: i, TurnID: "turn-ls-" + string(rune('0'+i))}
		if err := logger.WriteSnapshot(snap); err != nil {
			t.Fatalf("WriteSnapshot[%d]: %v", i, err)
		}
	}

	if seq := logger.LastSequence(); seq != 5 {
		t.Errorf("LastSequence: got %d, want 5", seq)
	}
}

func TestAtomicTurnLogger_CrashRecovery(t *testing.T) {
	dir := t.TempDir()

	// Write first snapshot.
	logger1, err := NewAtomicTurnLogger(dir)
	if err != nil {
		t.Fatalf("NewAtomicTurnLogger: %v", err)
	}
	snap1 := TurnSnapshot{Sequence: 1, TurnID: "turn-rec-1", Output: "pre-crash"}
	if err := logger1.WriteSnapshot(snap1); err != nil {
		t.Fatalf("WriteSnapshot pre-crash: %v", err)
	}

	// Write second snapshot via a raw file with invalid JSON to simulate a crash mid-write.
	corruptPath := filepath.Join(dir, "turn-rec-2-corrupt.json")
	if err := os.WriteFile(corruptPath, []byte("{invalid json"), 0o644); err != nil {
		t.Fatalf("WriteFile corrupt: %v", err)
	}

	// Create a fresh logger (simulating restart after crash).
	logger2, err := NewAtomicTurnLogger(dir)
	if err != nil {
		t.Fatalf("NewAtomicTurnLogger post-crash: %v", err)
	}

	// Should recover the first snapshot despite the corrupt second file.
	snapshots, err := logger2.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots post-crash: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("Recovered snapshots: got %d, want 1 (corrupt file skipped)", len(snapshots))
	}
	if snapshots[0].TurnID != "turn-rec-1" {
		t.Errorf("Recovered TurnID: got %s, want turn-rec-1", snapshots[0].TurnID)
	}
}

// -----------------------------------------------------------------------
// WriteSnapshot / ReadSnapshot round-trip with metadata
// -----------------------------------------------------------------------

func TestAtomicTurnLogger_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAtomicTurnLogger(dir)
	if err != nil {
		t.Fatalf("NewAtomicTurnLogger: %v", err)
	}

	original := TurnSnapshot{
		Sequence: 42,
		TurnID:   "turn-roundtrip",
		ToolCalls: []TurnRecord{
			{
				Name:    "prune_snapshots",
				Input:   map[string]any{"keep_last": 3},
				Output:  "deleted 7 files",
				Success: true,
			},
			{
				Name:    "read_snapshot",
				Input:   map[string]any{"turn_id": "turn-abc"},
				Output:  "not found",
				Success: false,
			},
		},
		LLMMessages: []LLMMessage{
			{Role: "user", Content: "Analyze the traces."},
			{Role: "assistant", Content: "I will analyze 3 traces."},
		},
		Output:    "analysis complete",
		Timestamp: time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC),
		Metadata: map[string]string{
			"employee":     "researcher-v2",
			"trace_source": "halo-traces-2026-07-15.jsonl",
		},
	}

	if err := logger.WriteSnapshot(original); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}

	read, err := logger.ReadSnapshot("turn-roundtrip")
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}

	if read.Sequence != original.Sequence {
		t.Errorf("Sequence: got %d, want %d", read.Sequence, original.Sequence)
	}
	if read.TurnID != original.TurnID {
		t.Errorf("TurnID: got %s, want %s", read.TurnID, original.TurnID)
	}
	if len(read.ToolCalls) != len(original.ToolCalls) {
		t.Fatalf("ToolCalls count: got %d, want %d", len(read.ToolCalls), len(original.ToolCalls))
	}
	for i, tc := range original.ToolCalls {
		if read.ToolCalls[i].Name != tc.Name {
			t.Errorf("ToolCalls[%d].Name: got %s, want %s", i, read.ToolCalls[i].Name, tc.Name)
		}
	}
	if len(read.LLMMessages) != len(original.LLMMessages) {
		t.Fatalf("LLMMessages count: got %d, want %d", len(read.LLMMessages), len(original.LLMMessages))
	}
	if read.Output != original.Output {
		t.Errorf("Output: got %q, want %q", read.Output, original.Output)
	}
	if read.Metadata["employee"] != original.Metadata["employee"] {
		t.Errorf("Metadata[employee]: got %s, want %s", read.Metadata["employee"], original.Metadata["employee"])
	}
	if read.Metadata["trace_source"] != original.Metadata["trace_source"] {
		t.Errorf("Metadata[trace_source]: got %s, want %s", read.Metadata["trace_source"], original.Metadata["trace_source"])
	}
}

// -----------------------------------------------------------------------
// on-disk format validation
// -----------------------------------------------------------------------

func TestAtomicTurnLogger_OnDiskFormat(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAtomicTurnLogger(dir)
	if err != nil {
		t.Fatalf("NewAtomicTurnLogger: %v", err)
	}

	snap := TurnSnapshot{
		Sequence: 1,
		TurnID:   "turn-format",
		Output:   "format check",
	}
	if err := logger.WriteSnapshot(snap); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "turn-format.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Verify it's valid JSON with expected fields.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("JSON parse: %v", err)
	}

	version, ok := raw["version"]
	if !ok {
		t.Fatal("missing 'version' field in on-disk format")
	}
	if version != float64(TurnSnapshotVersion) {
		t.Errorf("version: got %v, want %d", version, TurnSnapshotVersion)
	}

	snapObj, ok := raw["snapshot"]
	if !ok {
		t.Fatal("missing 'snapshot' field in on-disk format")
	}
	snapMap, ok := snapObj.(map[string]any)
	if !ok {
		t.Fatal("snapshot field is not an object")
	}
	if snapMap["turn_id"] != "turn-format" {
		t.Errorf("snapshot.turn_id: got %v, want turn-format", snapMap["turn_id"])
	}
	if snapMap["output"] != "format check" {
		t.Errorf("snapshot.output: got %v, want 'format check'", snapMap["output"])
	}
}

// -----------------------------------------------------------------------
// Sanitized filename tests
// -----------------------------------------------------------------------

func TestAtomicTurnLogger_SafeFilename(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAtomicTurnLogger(dir)
	if err != nil {
		t.Fatalf("NewAtomicTurnLogger: %v", err)
	}

	// Turn IDs with special chars should be sanitized.
	snap := TurnSnapshot{
		Sequence: 1,
		TurnID:   "turn/with:bad?chars",
		Output:   "safe filename",
	}
	if err := logger.WriteSnapshot(snap); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}

	// Listing should succeed (bad chars sanitized).
	_, err = logger.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots after special chars: %v", err)
	}
}

// -----------------------------------------------------------------------
// TurnCompactor tests (existing)
// -----------------------------------------------------------------------

func TestTurnCompactor_CompactToolCalls(t *testing.T) {
	tc := NewTurnCompactor()
	turns := []Turn{
		{Type: "tool_call", ToolName: "view_trace", ToolInput: map[string]any{"trace_id": "123"}, TokenCount: 50},
		{Type: "tool_response", Content: "trace data here", TokenCount: 200},
	}

	compacted := tc.CompactTurns(turns)

	if len(compacted) != 1 {
		t.Fatalf("expected 1 compacted turn, got %d", len(compacted))
	}

	ct := compacted[0]
	if ct.Type != "tool" {
		t.Errorf("expected type 'tool', got %q", ct.Type)
	}
	if ct.ToolName != "view_trace" {
		t.Errorf("expected tool 'view_trace', got %q", ct.ToolName)
	}
	if ct.ToolOutput != "trace data here" {
		t.Errorf("expected output 'trace data here', got %q", ct.ToolOutput)
	}
	if ct.TokenCount != 250 {
		t.Errorf("expected 250 tokens, got %d", ct.TokenCount)
	}
}

func TestTurnCompactor_CollapseThinking(t *testing.T) {
	tc := NewTurnCompactor()
	turns := []Turn{
		{Type: "thinking", Content: "thinking 1", TokenCount: 10},
		{Type: "thinking", Content: "thinking 2", TokenCount: 10},
		{Type: "thinking", Content: "thinking 3", TokenCount: 10},
		{Type: "thinking", Content: "thinking 4", TokenCount: 10},
		{Type: "final", Content: "final answer", TokenCount: 50},
	}

	compacted := tc.CompactTurns(turns)

	// Should have 2 turns: 1 collapsed thinking + 1 final.
	if len(compacted) != 2 {
		t.Fatalf("expected 2 compacted turns, got %d: %#v", len(compacted), compacted)
	}

	if compacted[0].Type != "thinking" {
		t.Errorf("expected first turn to be 'thinking', got %q", compacted[0].Type)
	}
	if compacted[1].Type != "final" {
		t.Errorf("expected second turn to be 'final', got %q", compacted[1].Type)
	}
	// Verify collapse markers are present.
	if compacted[0].Thinking == "" {
		t.Error("expected collapsed thinking to be non-empty")
	}
	if !strings.Contains(compacted[0].Thinking, "[...]") {
		t.Error("expected collapse marker [...] in thinking")
	}
}

func TestTurnCompactor_EmptyInput(t *testing.T) {
	tc := NewTurnCompactor()
	compacted := tc.CompactTurns([]Turn{})
	if len(compacted) != 0 {
		t.Errorf("expected empty result for empty input, got %d turns", len(compacted))
	}
}

func TestTurnCompactor_ToolCallWithoutResponse(t *testing.T) {
	tc := NewTurnCompactor()
	turns := []Turn{
		{Type: "tool_call", ToolName: "view_trace", ToolInput: map[string]any{"trace_id": "123"}, TokenCount: 50},
		// No tool_response - should be dropped
		{Type: "final", Content: "final answer", TokenCount: 50},
	}

	compacted := tc.CompactTurns(turns)

	// Should have 1 turn: just the final (tool_call without response is dropped)
	if len(compacted) != 1 {
		t.Fatalf("expected 1 compacted turn, got %d", len(compacted))
	}
	if compacted[0].Type != "final" {
		t.Errorf("expected final turn, got %q", compacted[0].Type)
	}
}

func TestTurnCompactor_ThinkingFlush(t *testing.T) {
	tc := NewTurnCompactor()
	turns := []Turn{
		{Type: "thinking", Content: "thinking 1", TokenCount: 10},
		{Type: "thinking", Content: "thinking 2", TokenCount: 10},
		{Type: "final", Content: "final answer", TokenCount: 50},
	}

	compacted := tc.CompactTurns(turns)

	// Should have 2 turns: 1 thinking (not collapsed, just flushed) + 1 final
	if len(compacted) != 2 {
		t.Fatalf("expected 2 compacted turns, got %d", len(compacted))
	}
	// Both thinking turns should be present (not collapsed since <= 2)
	if !strings.Contains(compacted[0].Thinking, "thinking 1") {
		t.Error("expected 'thinking 1' in output")
	}
	if !strings.Contains(compacted[0].Thinking, "thinking 2") {
		t.Error("expected 'thinking 2' in output")
	}
}
