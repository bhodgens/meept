package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeSharedConfigs(t *testing.T) {
	tmpDir := t.TempDir()

	// Create mock checkout dir with shared config
	nodeID := "fake"
	sharedDir := filepath.Join(tmpDir, "checkout", "config", "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sharedConfig := `{"daemon": {"log_level": "info", "socket_path": "~/.meept/meept.sock"}}`
	if err := os.WriteFile(filepath.Join(sharedDir, "meept.json5"), []byte(sharedConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMerger(tmpDir, filepath.Join(tmpDir, "checkout"), nodeID, &testLogger{})
	result, err := m.Merge("abc123")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.FilesApplied) == 0 {
		t.Error("expected at least one file to be applied")
	}

	appliedFile := result.FilesApplied[0]
	if appliedFile != "meept.json5" {
		t.Errorf("expected meept.json5, got %s", appliedFile)
	}

	// Verify file was written
	dstPath := filepath.Join(tmpDir, "meept.json5")
	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("config file not written to %s: %v", dstPath, err)
	}
	if string(data) != sharedConfig {
		t.Errorf("config content mismatch: got %q, want %q", string(data), sharedConfig)
	}
}

func TestMergeNodeOverrides(t *testing.T) {
	tmpDir := t.TempDir()

	checkoutDir := filepath.Join(tmpDir, "checkout")
	nodeID := "node-a"

	// Create shared config
	sharedDir := filepath.Join(checkoutDir, "config", "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sharedConfig := `{"daemon": {"socket_path": "/tmp/meept.sock"}}`
	if err := os.WriteFile(filepath.Join(sharedDir, "meept.json5"), []byte(sharedConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create node override
	nodeDir := filepath.Join(checkoutDir, "config", "nodes", nodeID)
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	overrideConfig := `{"daemon": {"log_level": "debug"}}`
	if err := os.WriteFile(filepath.Join(nodeDir, "meept.json5"), []byte(overrideConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMerger(tmpDir, checkoutDir, nodeID, &testLogger{})
	result, err := m.Merge("def456")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.FilesApplied) == 0 {
		t.Error("expected at least one file to be applied")
	}
}

func TestMerge_NoChangesSkipped(t *testing.T) {
	tmpDir := t.TempDir()

	checkoutDir := filepath.Join(tmpDir, "checkout")
	nodeID := "node-b"

	sharedDir := filepath.Join(checkoutDir, "config", "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sharedConfig := `{"daemon": {"log_level": "info"}}`
	if err := os.WriteFile(filepath.Join(sharedDir, "meept.json5"), []byte(sharedConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMerger(tmpDir, checkoutDir, nodeID, &testLogger{})

	// First merge
	result1, err := m.Merge("aaa")
	if err != nil {
		t.Fatal(err)
	}
	if len(result1.FilesApplied) == 0 {
		t.Error("first merge: expected file applied")
	}

	// Second merge — same content, should be skipped
	result2, err := m.Merge("aaa")
	if err != nil {
		t.Fatal(err)
	}
	if len(result2.FilesSkipped) == 0 {
		t.Error("second merge: expected file to be skipped (no change)")
	}
}

func TestMerge_Blacklisted(t *testing.T) {
	tmpDir := t.TempDir()

	checkoutDir := filepath.Join(tmpDir, "checkout")

	sharedDir := filepath.Join(checkoutDir, "config", "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// This file should be ignored (not .json5 or .toml)
	if err := os.WriteFile(filepath.Join(sharedDir, "readme.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMerger(tmpDir, checkoutDir, "", &testLogger{})
	result, err := m.Merge("ghi")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.FilesApplied) != 0 {
		t.Errorf("expected no files applied, got %d", len(result.FilesApplied))
	}
}

func TestMerge_InvalidJSON5Skipped(t *testing.T) {
	tmpDir := t.TempDir()

	checkoutDir := filepath.Join(tmpDir, "checkout")

	sharedDir := filepath.Join(checkoutDir, "config", "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Invalid JSON5 with unquoted key and stray comma
	invalidConfig := `{"daemon": {"log_level": "info",,}}`
	if err := os.WriteFile(filepath.Join(sharedDir, "meept.json5"), []byte(invalidConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMerger(tmpDir, checkoutDir, "", &testLogger{})
	result, err := m.Merge("jkl")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.FilesApplied) != 0 {
		t.Error("invalid JSON5 should be skipped")
	}
	if len(result.FilesSkipped) == 0 {
		t.Error("invalid JSON5 should appear in skipped list")
	}
}

func TestSyncer_FileWouldChange(t *testing.T) {
	tmpDir := t.TempDir()

	path := filepath.Join(tmpDir, "test.json5")
	data := []byte(`{"key": "value"}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up shared config source
	checkoutDir := filepath.Join(tmpDir, "checkout")
	sharedDir := filepath.Join(checkoutDir, "config", "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sharedDir, "test.json5"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMerger(tmpDir, checkoutDir, "", &testLogger{})

	// File not in hash table → would change
	wouldChange, err := m.FileWouldChange(filepath.Join(sharedDir, "test.json5"))
	if err != nil {
		t.Fatal(err)
	}
	if !wouldChange {
		t.Error("new file should be reported as 'would change'")
	}

	// After applying a merge, check again
	result, err := m.Merge("mno")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.FilesApplied) == 0 {
		t.Fatal("expected file applied")
	}

	// The dest file is now in the hash table with the same content → would NOT change
	wouldChange, err = m.FileWouldChange(filepath.Join(sharedDir, "test.json5"))
	if err != nil {
		t.Fatal(err)
	}
	if wouldChange {
		t.Error("unchanged file should NOT be reported as 'would change'")
	}
}

// testLogger is a minimal logger implementation for tests.
type testLogger struct{}

func (l *testLogger) Info(msg string, keysAndValues ...interface{}) {}
func (l *testLogger) Warn(msg string, keysAndValues ...interface{}) {}
func (l *testLogger) Debug(msg string, keysAndValues ...interface{}) {}
