package learning

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConsolidateEmpty(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	rawPath := filepath.Join(tmp, "raw_captures.jsonl")
	datasetsDir := filepath.Join(tmp, "datasets")

	stats, err := Consolidate(rawPath, datasetsDir, 0.5)
	if err != nil {
		t.Fatalf("Consolidate failed: %v", err)
	}
	if stats.Processed != 0 {
		t.Errorf("expected 0 processed, got %d", stats.Processed)
	}
}

func TestConsolidateRoutesCorrectly(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	rawPath := filepath.Join(tmp, "raw_captures.jsonl")
	datasetsDir := filepath.Join(tmp, "datasets")

	// Create raw captures with different domains and quality.
	trajectories := []ResearchTrajectory{
		{
			ID:        "t1",
			SessionID: "s1",
			Domain:    "code",
			Query:     "how to parse json in go",
			Synthesis: "use encoding/json",
			TaskOutcome: TaskOutcome{
				Success: true,
			},
			ToolCalls:  []ToolCallRecord{{Tool: "file_read", Used: true}},
			Timestamp:  time.Now().UTC(),
		},
		{
			ID:        "t2",
			SessionID: "s2",
			Domain:    "debugging",
			Query:     "why does my code panic",
			Synthesis: "add nil guard",
			TaskOutcome: TaskOutcome{
				Success: true,
				Quality: 0.9,
			},
			ToolCalls: []ToolCallRecord{{Tool: "grep", Used: true}, {Tool: "file_read", Used: true}},
			Timestamp: time.Now().UTC(),
		},
	}

	var data []byte
	for _, traj := range trajectories {
		line, err := json.Marshal(traj)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(rawPath, data, 0o644); err != nil {
		t.Fatalf("write raw captures: %v", err)
	}

	stats, err := Consolidate(rawPath, datasetsDir, 0.5)
	if err != nil {
		t.Fatalf("Consolidate failed: %v", err)
	}
	if stats.Processed != 2 {
		t.Errorf("expected 2 processed, got %d", stats.Processed)
	}
	if stats.Added != 2 {
		t.Errorf("expected 2 added, got %d", stats.Added)
	}

	// Verify domain files were created.
	codePath := filepath.Join(datasetsDir, "code.jsonl")
	if _, err := os.Stat(codePath); err != nil {
		t.Fatalf("code.jsonl not created: %v", err)
	}
	debuggingPath := filepath.Join(datasetsDir, "debugging.jsonl")
	if _, err := os.Stat(debuggingPath); err != nil {
		t.Fatalf("debugging.jsonl not created: %v", err)
	}

	// Running consolidate again should detect duplicates.
	stats2, err := Consolidate(rawPath, datasetsDir, 0.5)
	if err != nil {
		t.Fatalf("second Consolidate failed: %v", err)
	}
	if stats2.Duplicates != 2 {
		t.Errorf("expected 2 duplicates on second pass, got %d", stats2.Duplicates)
	}
	if stats2.Added != 0 {
		t.Errorf("expected 0 added on second pass, got %d", stats2.Added)
	}
}

func TestConsolidateQualityFilter(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	rawPath := filepath.Join(tmp, "raw_captures.jsonl")
	datasetsDir := filepath.Join(tmp, "datasets")

	// A low-quality trajectory (no success, no used tools) will score 0.5.
	trajectories := []ResearchTrajectory{
		{
			ID:        "t1",
			SessionID: "s1",
			Domain:    "code",
			Query:     "low quality query",
			Synthesis: "",
			TaskOutcome: TaskOutcome{
				Success: false,
			},
			ToolCalls: nil,
			Timestamp: time.Now().UTC(),
		},
	}

	var data []byte
	for _, traj := range trajectories {
		line, err := json.Marshal(traj)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(rawPath, data, 0o644); err != nil {
		t.Fatalf("write raw captures: %v", err)
	}

	// Min quality 0.6 should skip the 0.5-score trajectory.
	stats, err := Consolidate(rawPath, datasetsDir, 0.6)
	if err != nil {
		t.Fatalf("Consolidate failed: %v", err)
	}
	if stats.Added != 0 {
		t.Errorf("expected 0 added with minQuality 0.6, got %d", stats.Added)
	}
	if stats.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", stats.Skipped)
	}
}
