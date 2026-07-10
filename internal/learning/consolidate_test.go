package learning

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConsolidateEmpty(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	rawPath := filepath.Join(tmp, "raw_captures.jsonl")
	datasetsDir := filepath.Join(tmp, "datasets")

	stats, err := Consolidate(rawPath, datasetsDir, 0.5, 0)
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

	stats, err := Consolidate(rawPath, datasetsDir, 0.5, 0)
	if err != nil {
		t.Fatalf("Consolidate failed: %v", err)
	}
	if stats.Processed != 2 {
		t.Errorf("expected 2 processed, got %d", stats.Processed)
	}
	if stats.Added != 2 {
		t.Errorf("expected 2 added, got %d", stats.Added)
	}

	// Verify DomainsTouched contains both domains.
	if len(stats.DomainsTouched) != 2 {
		t.Errorf("expected 2 domains touched, got %d: %v", len(stats.DomainsTouched), stats.DomainsTouched)
	}
	for _, want := range []string{"code", "debugging"} {
		found := false
		for _, d := range stats.DomainsTouched {
			if d == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected DomainsTouched to contain %q, got %v", want, stats.DomainsTouched)
		}
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
	stats2, err := Consolidate(rawPath, datasetsDir, 0.5, 0)
	if err != nil {
		t.Fatalf("second Consolidate failed: %v", err)
	}
	if stats2.Duplicates != 2 {
		t.Errorf("expected 2 duplicates on second pass, got %d", stats2.Duplicates)
	}
	if stats2.Added != 0 {
		t.Errorf("expected 0 added on second pass, got %d", stats2.Added)
	}
	if len(stats2.DomainsTouched) != 0 {
		t.Errorf("expected 0 domains touched on duplicate-only pass, got %v", stats2.DomainsTouched)
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
	stats, err := Consolidate(rawPath, datasetsDir, 0.6, 0)
	if err != nil {
		t.Fatalf("Consolidate failed: %v", err)
	}
	if stats.Added != 0 {
		t.Errorf("expected 0 added with minQuality 0.6, got %d", stats.Added)
	}
	if stats.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", stats.Skipped)
	}
	if len(stats.DomainsTouched) != 0 {
		t.Errorf("expected 0 domains touched when nothing added, got %v", stats.DomainsTouched)
	}
}

func TestConsolidateSkipsEmptySynthesis(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	rawPath := filepath.Join(tmp, "raw_captures.jsonl")
	datasetsDir := filepath.Join(tmp, "datasets")

	// High-score trajectory but empty synthesis (per-tool capture style).
	// Score would be 0.65 (base + used), above minQuality 0.6, but must skip.
	trajectories := []ResearchTrajectory{
		{
			ID:        "t-empty",
			SessionID: "s1",
			Domain:    "code",
			Query:     "how to parse json",
			Synthesis: "",
			TaskOutcome: TaskOutcome{
				Success: false,
			},
			ToolCalls: []ToolCallRecord{{Tool: "file_read", Used: true}},
			Timestamp: time.Now().UTC(),
		},
		{
			ID:        "t-good",
			SessionID: "s1",
			Domain:    "code",
			Query:     "how to parse json",
			Synthesis: "use encoding/json",
			TaskOutcome: TaskOutcome{
				Success: true,
			},
			ToolCalls: []ToolCallRecord{{Tool: "file_read", Used: true}},
			Timestamp: time.Now().UTC(),
		},
	}

	var data []byte
	for _, traj := range trajectories {
		line, err := json.Marshal(traj)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(rawPath, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	stats, err := Consolidate(rawPath, datasetsDir, 0.6, 0)
	if err != nil {
		t.Fatalf("Consolidate: %v", err)
	}
	if stats.Processed != 2 {
		t.Errorf("processed = %d, want 2", stats.Processed)
	}
	if stats.Skipped != 1 {
		t.Errorf("skipped = %d, want 1 (empty synthesis)", stats.Skipped)
	}
	if stats.Added != 1 {
		t.Errorf("added = %d, want 1 (full trajectory only)", stats.Added)
	}

	// Dataset should only contain the good example.
	domainPath := filepath.Join(datasetsDir, "code.jsonl")
	content, err := os.ReadFile(domainPath)
	if err != nil {
		t.Fatalf("read domain file: %v", err)
	}
	if !strings.Contains(string(content), "use encoding/json") {
		t.Error("expected good synthesis in dataset")
	}
	lines := 0
	for _, b := range content {
		if b == '\n' {
			lines++
		}
	}
	if lines != 1 {
		t.Errorf("expected 1 dataset line, got %d", lines)
	}
}

func TestConsolidateRetention(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	rawPath := filepath.Join(tmp, "raw_captures.jsonl")
	datasetsDir := filepath.Join(tmp, "datasets")
	if err := os.MkdirAll(datasetsDir, 0o755); err != nil {
		t.Fatalf("mkdir datasets: %v", err)
	}

	// Pre-create a domain file with multiple lines totaling > 100 bytes.
	domainPath := filepath.Join(datasetsDir, "code.jsonl")
	preLines := []string{}
	for i := 0; i < 10; i++ {
		// Each line ~30 bytes; 10 lines = ~300 bytes.
		preLines = append(preLines, `{"instruction":"old data padding line `+string(rune('a'+i))+`"}`)
	}
	preData := []byte{}
	for _, l := range preLines {
		preData = append(preData, []byte(l)...)
		preData = append(preData, '\n')
	}
	if err := os.WriteFile(domainPath, preData, 0o644); err != nil {
		t.Fatalf("write pre-existing dataset: %v", err)
	}

	// Verify pre-existing size exceeds cap.
	info, err := os.Stat(domainPath)
	if err != nil {
		t.Fatalf("stat pre-existing: %v", err)
	}
	preSize := info.Size()
	if preSize <= 100 {
		t.Fatalf("pre-existing file size %d must exceed 100 bytes for test", preSize)
	}

	// Create a raw capture that will append one new line to the "code" domain.
	traj := ResearchTrajectory{
		ID:        "t1",
		SessionID: "s1",
		Domain:    "code",
		Query:     "how to parse json in go",
		Synthesis: "use encoding/json",
		TaskOutcome: TaskOutcome{
			Success: true,
		},
		ToolCalls: []ToolCallRecord{{Tool: "file_read", Used: true}},
		Timestamp: time.Now().UTC(),
	}
	line, err := json.Marshal(traj)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if err := os.WriteFile(rawPath, append(line, '\n'), 0o644); err != nil {
		t.Fatalf("write raw captures: %v", err)
	}

	// Consolidate with maxDatasetSizeBytes=100.
	stats, err := Consolidate(rawPath, datasetsDir, 0.5, 100)
	if err != nil {
		t.Fatalf("Consolidate failed: %v", err)
	}
	if stats.Added != 1 {
		t.Errorf("expected 1 added, got %d", stats.Added)
	}

	// Verify file was trimmed to <= 100 bytes.
	info2, err := os.Stat(domainPath)
	if err != nil {
		t.Fatalf("stat after retention: %v", err)
	}
	postSize := info2.Size()
	if postSize > 100 {
		t.Errorf("expected file size <= 100 bytes after retention, got %d", postSize)
	}
	if postSize >= preSize {
		t.Errorf("expected file size to shrink from %d, got %d", preSize, postSize)
	}

	// Verify head lines were dropped: the trimmed file should not contain "old data padding line a".
	data, err := os.ReadFile(domainPath)
	if err != nil {
		t.Fatalf("read trimmed file: %v", err)
	}
	if strings.Contains(string(data), "old data padding line a") {
		t.Errorf("expected oldest line 'old data padding line a' to be trimmed, but it's still present")
	}
}
