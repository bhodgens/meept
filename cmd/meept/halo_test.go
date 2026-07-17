package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/agent"
)

func TestParseTurnsFromJSON_Array(t *testing.T) {
	turns := []agent.Turn{
		{Type: "tool_call", ToolName: "view_trace", ToolInput: map[string]any{"trace_id": "123"}, TokenCount: 50},
		{Type: "tool_response", Content: "trace data", TokenCount: 200},
	}

	data, err := json.Marshal(turns)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := parseTurnsFromJSON(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(parsed) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(parsed))
	}
	if parsed[0].ToolName != "view_trace" {
		t.Errorf("expected view_trace, got %s", parsed[0].ToolName)
	}
}

func TestParseTurnsFromJSON_Session(t *testing.T) {
	session := struct {
		Turns []agent.Turn `json:"turns"`
	}{
		Turns: []agent.Turn{
			{Type: "thinking", Content: "thinking 1", TokenCount: 10},
			{Type: "final", Content: "final answer", TokenCount: 50},
		},
	}

	data, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := parseTurnsFromJSON(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(parsed) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(parsed))
	}
	if parsed[0].Content != "thinking 1" {
		t.Errorf("expected 'thinking 1', got %s", parsed[0].Content)
	}
}

func TestParseTurnsFromJSON_Invalid(t *testing.T) {
	_, err := parseTurnsFromJSON([]byte(`{"invalid": "json"}`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSumTurnTokens(t *testing.T) {
	turns := []agent.Turn{
		{Type: "tool_call", TokenCount: 50},
		{Type: "tool_response", TokenCount: 100},
		{Type: "final", TokenCount: 25},
	}

	total := sumTurnTokens(turns)
	if total != 175 {
		t.Errorf("expected 175, got %d", total)
	}
}

func TestSumCompactTurnTokens(t *testing.T) {
	turns := []agent.CompactTurn{
		{Type: "tool", TokenCount: 250},
		{Type: "thinking", TokenCount: 30},
		{Type: "final", TokenCount: 50},
	}

	total := sumCompactTurnTokens(turns)
	if total != 330 {
		t.Errorf("expected 330, got %d", total)
	}
}

func TestGenerateReport_Empty(t *testing.T) {
	report := generateReport([]agent.FailureMode{})

	if report == "" {
		t.Fatal("expected non-empty report")
	}
	if !contains(report, "No failure modes detected") {
		t.Error("expected 'No failure modes detected' in report")
	}
}

func TestGenerateReport_WithFailureModes(t *testing.T) {
	modes := []agent.FailureMode{
		{
			ID:          "test-1",
			Description: "Test failure description",
			Severity:    "high",
			Category:    "semantic",
			TraceIDs:    []string{"trace-1", "trace-2"},
		},
	}

	report := generateReport(modes)

	if !contains(report, "Failure modes found: **1**") {
		t.Error("expected failure count in report")
	}
	if !contains(report, "HIGH Severity Issues") {
		t.Error("expected severity section")
	}
	if !contains(report, "Test failure description") {
		t.Error("expected failure description")
	}
	if !contains(report, "trace-1, trace-2") {
		t.Error("expected trace IDs")
	}
}

func TestGenerateReport_GroupsBySeverity(t *testing.T) {
	modes := []agent.FailureMode{
		{ID: "1", Description: "Critical issue", Severity: "critical", Category: "panic"},
		{ID: "2", Description: "High issue", Severity: "high", Category: "error"},
		{ID: "3", Description: "Low issue", Severity: "low", Category: "semantic"},
	}

	report := generateReport(modes)

	if !contains(report, "CRITICAL Severity Issues") {
		t.Error("expected CRITICAL section")
	}
	if !contains(report, "HIGH Severity Issues") {
		t.Error("expected HIGH section")
	}
	if !contains(report, "LOW Severity Issues") {
		t.Error("expected LOW section")
	}
	// MEDIUM should not appear
	if contains(report, "MEDIUM Severity Issues") {
		t.Error("should not have MEDIUM section with no medium items")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && containsSubstring(s, substr)
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Integration test for CLI command
func TestHaloCompactCmd_Integration(t *testing.T) {
	// Create temp file with test turns
	dir := t.TempDir()
	sessionFile := filepath.Join(dir, "session.json")

	turns := []agent.Turn{
		{Type: "tool_call", ToolName: "view_trace", ToolInput: map[string]any{"trace_id": "123"}, TokenCount: 50},
		{Type: "tool_response", Content: "trace data", TokenCount: 200},
		{Type: "final", Content: "done", TokenCount: 25},
	}

	data, _ := json.MarshalIndent(turns, "", "  ")
	if err := os.WriteFile(sessionFile, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Run compact command
	cmd := newHaloCompactCmd()
	cmd.SetArgs([]string{sessionFile, "--format", "summary"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error: input file is required via flag, but got none")
	}
}
