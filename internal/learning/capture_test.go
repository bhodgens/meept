package learning

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewCaptureRecorder(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	rec, err := NewCaptureRecorder(tmp)
	if err != nil {
		t.Fatalf("NewCaptureRecorder failed: %v", err)
	}
	if rec == nil {
		t.Fatal("expected non-nil recorder")
	}

	// Empty data dir should error.
	if _, err := NewCaptureRecorder(""); err == nil {
		t.Fatal("expected error for empty data dir")
	}
}

func TestRecordResearch(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	rec, err := NewCaptureRecorder(tmp)
	if err != nil {
		t.Fatalf("NewCaptureRecorder failed: %v", err)
	}

	ctx := context.Background()
	if err := rec.RecordResearch(ctx, "session-1", "how to parse json in go", "file_read", "package main import encoding/json"); err != nil {
		t.Fatalf("RecordResearch failed: %v", err)
	}

	// Verify the file exists and has one line.
	capturesFile := filepath.Join(tmp, "raw_captures.jsonl")
	f, err := os.Open(capturesFile)
	if err != nil {
		t.Fatalf("open captures file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		var traj ResearchTrajectory
		if err := json.Unmarshal(scanner.Bytes(), &traj); err != nil {
			t.Fatalf("unmarshal trajectory: %v", err)
		}
		if traj.SessionID != "session-1" {
			t.Errorf("expected sessionID 'session-1', got %q", traj.SessionID)
		}
		if traj.ID == "" {
			t.Error("expected non-empty ID")
		}
		if traj.Domain == "" {
			t.Error("expected non-empty domain")
		}
		if len(traj.ToolCalls) != 1 {
			t.Errorf("expected 1 tool call, got %d", len(traj.ToolCalls))
		}
		if traj.ToolCalls[0].Tool != "file_read" {
			t.Errorf("expected tool 'file_read', got %q", traj.ToolCalls[0].Tool)
		}
	}
	if lineCount != 1 {
		t.Errorf("expected 1 line, got %d", lineCount)
	}
}

func TestRecordResearchMultiple(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	rec, err := NewCaptureRecorder(tmp)
	if err != nil {
		t.Fatalf("NewCaptureRecorder failed: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := rec.RecordResearch(ctx, "session-multi", "query", "grep", "output"); err != nil {
			t.Fatalf("RecordResearch[%d] failed: %v", i, err)
		}
	}

	capturesFile := filepath.Join(tmp, "raw_captures.jsonl")
	data, err := os.ReadFile(capturesFile)
	if err != nil {
		t.Fatalf("read captures: %v", err)
	}
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 5 {
		t.Errorf("expected 5 lines, got %d", lines)
	}
}

func TestRecordTrajectory(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	rec, err := NewCaptureRecorder(tmp)
	if err != nil {
		t.Fatalf("NewCaptureRecorder failed: %v", err)
	}

	ctx := context.Background()
	intent := "how do I parse json in go?"
	synthesis := "use encoding/json with json.Unmarshal"
	toolNames := []string{"memory_search", "file_read", "grep"}
	success := true

	if err := rec.RecordTrajectory(ctx, "session-traj", intent, synthesis, toolNames, success); err != nil {
		t.Fatalf("RecordTrajectory failed: %v", err)
	}

	capturesFile := filepath.Join(tmp, "raw_captures.jsonl")
	f, err := os.Open(capturesFile)
	if err != nil {
		t.Fatalf("open captures file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		var traj ResearchTrajectory
		if err := json.Unmarshal(scanner.Bytes(), &traj); err != nil {
			t.Fatalf("unmarshal trajectory: %v", err)
		}
		if traj.Intent != intent {
			t.Errorf("expected intent %q, got %q", intent, traj.Intent)
		}
		if traj.Synthesis != synthesis {
			t.Errorf("expected synthesis %q, got %q", synthesis, traj.Synthesis)
		}
		if traj.Query != intent {
			t.Errorf("expected query %q, got %q", intent, traj.Query)
		}
		if !traj.TaskOutcome.Success {
			t.Error("expected success=true")
		}
		if len(traj.ToolCalls) != len(toolNames) {
			t.Errorf("expected %d tool calls, got %d", len(toolNames), len(traj.ToolCalls))
		}
		if traj.ID == "" {
			t.Error("expected non-empty ID")
		}
		// Trajectory captures should use the ltraj- prefix.
		if len(traj.ID) < 6 || traj.ID[:6] != "ltraj-" {
			t.Errorf("expected ltraj- prefix, got %q", traj.ID)
		}
	}
	if lineCount != 1 {
		t.Errorf("expected 1 line, got %d", lineCount)
	}
}

func TestRecordResearchIncludeToolsFilter(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	rec, err := NewCaptureRecorder(tmp)
	if err != nil {
		t.Fatalf("NewCaptureRecorder failed: %v", err)
	}
	rec.Configure([]string{"memory_search", "file_read"})

	ctx := context.Background()
	// Allowed tools
	if err := rec.RecordResearch(ctx, "s1", "query", "memory_search", "hits"); err != nil {
		t.Fatalf("memory_search: %v", err)
	}
	if err := rec.RecordResearch(ctx, "s1", "query", "file_read", "content"); err != nil {
		t.Fatalf("file_read: %v", err)
	}
	// Filtered out
	if err := rec.RecordResearch(ctx, "s1", "query", "shell_exec", "output"); err != nil {
		t.Fatalf("shell_exec should be silent skip, got: %v", err)
	}
	if err := rec.RecordResearch(ctx, "s1", "query", "web_search", "results"); err != nil {
		t.Fatalf("web_search should be silent skip, got: %v", err)
	}

	capturesFile := filepath.Join(tmp, "raw_captures.jsonl")
	data, err := os.ReadFile(capturesFile)
	if err != nil {
		t.Fatalf("read captures: %v", err)
	}
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 2 {
		t.Errorf("expected 2 captured lines (allowlisted only), got %d", lines)
	}
}

func TestRecordResearchAllToolsWhenUnconfigured(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	rec, err := NewCaptureRecorder(tmp)
	if err != nil {
		t.Fatalf("NewCaptureRecorder failed: %v", err)
	}
	// No Configure → capture all tools

	ctx := context.Background()
	if err := rec.RecordResearch(ctx, "s1", "q", "shell_exec", "out"); err != nil {
		t.Fatalf("RecordResearch: %v", err)
	}
	capturesFile := filepath.Join(tmp, "raw_captures.jsonl")
	data, err := os.ReadFile(capturesFile)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected capture when no allowlist configured")
	}
}
