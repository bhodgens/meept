package selfimprove

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeScanTracesFixture writes a two-trace JSONL fixture exercising the
// ScanTraces finding families: tool_error, refusal_loop, redundant_args.
func writeScanTracesFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "traces.jsonl")
	type span struct {
		TraceID   string `json:"trace_id"`
		SpanID    string `json:"span_id"`
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
		Service   string `json:"service,omitempty"`
		Model     string `json:"model,omitempty"`
		HasError  bool   `json:"has_error,omitempty"`
		ErrorType string `json:"error_type,omitempty"`
		ToolName  string `json:"tool_name,omitempty"`
	}
	now := time.Now().UTC().Format(time.RFC3339)
	spans := []span{
		{TraceID: "trace-a", SpanID: "a-1", StartTime: now, EndTime: now, ToolName: "shell_execute", HasError: true, ErrorType: "exit_nonzero", Model: "test"},
		{TraceID: "trace-a", SpanID: "a-2", StartTime: now, EndTime: now, ToolName: "web_fetch", HasError: true, ErrorType: "timeout", Model: "test"},
		{TraceID: "trace-b", SpanID: "b-1", StartTime: now, EndTime: now, ToolName: "tool_denied_by_policy", Model: "test"},
		{TraceID: "trace-b", SpanID: "b-2", StartTime: now, EndTime: now, ToolName: "read_file", Model: "test"},
		{TraceID: "trace-b", SpanID: "b-3", StartTime: now, EndTime: now, ToolName: "read_file", Model: "test"},
		{TraceID: "trace-b", SpanID: "b-4", StartTime: now, EndTime: now, ToolName: "read_file", Model: "test"},
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, s := range spans {
		if err := enc.Encode(s); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestScanTraces_FixtureProducesFindings is the end-to-end pin for the
// HALO RLM trace-analysis activation path: a fixture trace JSONL run
// through IssueDetector.ScanTraces (the plan-intended recurring consumer,
// docs/plans/2026-07-16-halo-augmentation-plan.md Task 2) produces
// findings in each family the detector recognizes.
func TestScanTraces_FixtureProducesFindings(t *testing.T) {
	fixture := writeScanTracesFixture(t)
	d := newTestDetector(t)

	issues, err := d.ScanTraces(context.Background(), fixture)
	if err != nil {
		t.Fatalf("ScanTraces: %v", err)
	}
	if len(issues) == 0 {
		t.Fatal("expected findings from fixture traces, got none")
	}

	byCategory := make(map[string]int)
	for _, issue := range issues {
		cat, _ := issue.Metadata["category"].(string)
		byCategory[cat]++
	}

	if byCategory["tool_error"] == 0 {
		t.Errorf("expected tool_error findings, got %v", byCategory)
	}
	if byCategory["refusal_loop"] == 0 {
		t.Errorf("expected refusal_loop findings, got %v", byCategory)
	}
	if byCategory["redundant_args"] == 0 {
		t.Errorf("expected redundant_args findings, got %v", byCategory)
	}
}

// TestScanTraces_EmptyStoreYieldsNoFindings pins the quiet path: a trace
// file with no error/refusal/repetition yields zero issues.
func TestScanTraces_EmptyStoreYieldsNoFindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traces.jsonl")
	span := map[string]string{
		"trace_id":   "trace-ok",
		"span_id":    "ok-1",
		"start_time": time.Now().UTC().Format(time.RFC3339),
		"end_time":   time.Now().UTC().Format(time.RFC3339),
		"tool_name":  "read_file",
	}
	data, _ := json.Marshal(span)
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	d := newTestDetector(t)
	issues, err := d.ScanTraces(context.Background(), path)
	if err != nil {
		t.Fatalf("ScanTraces: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("clean trace must yield no findings, got %d", len(issues))
	}
}

// TestScanTraces_MissingFileErrors pins the error path: a nonexistent
// store path returns a wrapped load error, never a silent zero-issues pass.
func TestScanTraces_MissingFileErrors(t *testing.T) {
	d := newTestDetector(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist.jsonl")
	if _, err := d.ScanTraces(context.Background(), missing); err == nil {
		t.Fatal("expected error for missing trace store path")
	}
}
