package learning

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMetadata_MissingFile(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadMetadata(dir)
	if err != nil {
		t.Fatalf("LoadMetadata returned error: %v", err)
	}
	if m == nil {
		t.Fatal("LoadMetadata returned nil")
	}
	if m.LastConsolidatedAt != (time.Time{}) {
		t.Errorf("LastConsolidatedAt = %v, want zero", m.LastConsolidatedAt)
	}
	if m.DomainStats == nil {
		t.Error("DomainStats map is nil, want empty map")
	}
	if len(m.DomainStats) != 0 {
		t.Errorf("DomainStats has %d entries, want 0", len(m.DomainStats))
	}
	if m.RawCapturesCount != 0 {
		t.Errorf("RawCapturesCount = %d, want 0", m.RawCapturesCount)
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := &LearningMetadata{
		LastConsolidatedAt: time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		DomainStats: map[string]DomainStat{
			"code": {
				ExampleCount: 42,
				Bytes:        1024,
				ModifiedAt:   time.Date(2026, 7, 9, 11, 0, 0, 0, time.UTC),
			},
			"debugging": {
				ExampleCount: 10,
				Bytes:        256,
				ModifiedAt:   time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC),
			},
		},
		RawCapturesCount: 100,
	}

	if err := SaveMetadata(dir, original); err != nil {
		t.Fatalf("SaveMetadata error: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(filepath.Join(dir, "metadata.json")); err != nil {
		t.Fatalf("metadata.json not written: %v", err)
	}

	loaded, err := LoadMetadata(dir)
	if err != nil {
		t.Fatalf("LoadMetadata error: %v", err)
	}

	if !loaded.LastConsolidatedAt.Equal(original.LastConsolidatedAt) {
		t.Errorf("LastConsolidatedAt = %v, want %v", loaded.LastConsolidatedAt, original.LastConsolidatedAt)
	}
	if loaded.RawCapturesCount != original.RawCapturesCount {
		t.Errorf("RawCapturesCount = %d, want %d", loaded.RawCapturesCount, original.RawCapturesCount)
	}
	if loaded.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", loaded.SchemaVersion)
	}
	if len(loaded.DomainStats) != 2 {
		t.Fatalf("DomainStats has %d entries, want 2", len(loaded.DomainStats))
	}
	codeStat, ok := loaded.DomainStats["code"]
	if !ok {
		t.Fatal("missing 'code' domain stat")
	}
	if codeStat.ExampleCount != 42 {
		t.Errorf("code ExampleCount = %d, want 42", codeStat.ExampleCount)
	}
	if codeStat.Bytes != 1024 {
		t.Errorf("code Bytes = %d, want 1024", codeStat.Bytes)
	}
}

func TestSaveMetadata_NilIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := SaveMetadata(dir, nil); err != nil {
		t.Fatalf("SaveMetadata(nil) error: %v", err)
	}
	// File should not exist.
	if _, err := os.Stat(filepath.Join(dir, "metadata.json")); !os.IsNotExist(err) {
		t.Errorf("expected metadata.json to not exist, got err=%v", err)
	}
}

func TestRefreshDomainStats(t *testing.T) {
	dir := t.TempDir()
	datasetsDir := filepath.Join(dir, "datasets")
	if err := os.MkdirAll(datasetsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create two jsonl files.
	codeContent := "{\"instruction\":\"a\"}\n{\"instruction\":\"b\"}\n{\"instruction\":\"c\"}\n"
	if err := os.WriteFile(filepath.Join(datasetsDir, "code.jsonl"), []byte(codeContent), 0o644); err != nil {
		t.Fatal(err)
	}
	debugContent := "{\"instruction\":\"x\"}\n{\"instruction\":\"y\"}\n"
	if err := os.WriteFile(filepath.Join(datasetsDir, "debugging.jsonl"), []byte(debugContent), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-jsonl file should be ignored.
	if err := os.WriteFile(filepath.Join(datasetsDir, "README.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &LearningMetadata{}
	result, err := RefreshDomainStats(dir, m)
	if err != nil {
		t.Fatalf("RefreshDomainStats error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if len(result.DomainStats) != 2 {
		t.Fatalf("DomainStats has %d entries, want 2", len(result.DomainStats))
	}

	codeStat, ok := result.DomainStats["code"]
	if !ok {
		t.Fatal("missing 'code' domain")
	}
	if codeStat.ExampleCount != 3 {
		t.Errorf("code ExampleCount = %d, want 3", codeStat.ExampleCount)
	}
	if codeStat.Bytes != int64(len(codeContent)) {
		t.Errorf("code Bytes = %d, want %d", codeStat.Bytes, len(codeContent))
	}

	debugStat, ok := result.DomainStats["debugging"]
	if !ok {
		t.Fatal("missing 'debugging' domain")
	}
	if debugStat.ExampleCount != 2 {
		t.Errorf("debugging ExampleCount = %d, want 2", debugStat.ExampleCount)
	}
}

func TestRefreshDomainStats_MissingDatasetsDir(t *testing.T) {
	dir := t.TempDir()
	m := &LearningMetadata{DomainStats: map[string]DomainStat{"old": {ExampleCount: 5}}}
	result, err := RefreshDomainStats(dir, m)
	if err != nil {
		t.Fatalf("RefreshDomainStats error on missing dir: %v", err)
	}
	// No datasets dir: should not error, should set DomainStats to empty map.
	if len(result.DomainStats) != 0 {
		t.Errorf("DomainStats has %d entries, want 0 (missing datasets dir)", len(result.DomainStats))
	}
}
