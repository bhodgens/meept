package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

	// Verify deep-merge semantics: the merged file should contain BOTH
	// the shared socket_path and the node-only log_level.
	dstPath := filepath.Join(tmpDir, "meept.json5")
	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("merged config not written: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "socket_path") {
		t.Errorf("deep-merge dropped shared key 'socket_path': %s", body)
	}
	if !strings.Contains(body, "log_level") {
		t.Errorf("deep-merge dropped node override key 'log_level': %s", body)
	}
	if !strings.Contains(body, "/tmp/meept.sock") {
		t.Errorf("deep-merge dropped shared value for socket_path: %s", body)
	}
	if !strings.Contains(body, "debug") {
		t.Errorf("deep-merge dropped node override value 'debug': %s", body)
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

// ---- deepMerge unit tests ----

func TestDeepMerge_NestedKeyMerge(t *testing.T) {
	dst := map[string]any{
		"daemon": map[string]any{
			"socket_path": "/tmp/meept.sock",
			"log_level":   "info",
		},
		"unchanged": "kept",
	}
	src := map[string]any{
		"daemon": map[string]any{
			"log_level": "debug",
		},
		"added": "new",
	}

	out := DeepMerge(dst, src)

	daemon, ok := out["daemon"].(map[string]any)
	if !ok {
		t.Fatalf("expected daemon map, got %T", out["daemon"])
	}
	if daemon["socket_path"] != "/tmp/meept.sock" {
		t.Errorf("expected shared nested key preserved, got %v", daemon["socket_path"])
	}
	if daemon["log_level"] != "debug" {
		t.Errorf("expected src nested override applied, got %v", daemon["log_level"])
	}
	if out["unchanged"] != "kept" {
		t.Errorf("expected top-level dst key preserved, got %v", out["unchanged"])
	}
	if out["added"] != "new" {
		t.Errorf("expected top-level src key added, got %v", out["added"])
	}
}

func TestDeepMerge_ArrayReplaceNotConcat(t *testing.T) {
	dst := map[string]any{
		"plugins": []any{"a", "b", "c"},
	}
	src := map[string]any{
		"plugins": []any{"x"},
	}

	out := DeepMerge(dst, src)

	got, ok := out["plugins"].([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", out["plugins"])
	}
	if len(got) != 1 || got[0] != "x" {
		t.Errorf("expected array replaced with ['x'], got %v", got)
	}
}

func TestDeepMerge_NullDeletesKey(t *testing.T) {
	dst := map[string]any{
		"keep":   1,
		"delete": "value",
	}
	src := map[string]any{
		"delete": nil,
	}

	out := DeepMerge(dst, src)

	if _, ok := out["delete"]; ok {
		t.Error("expected 'delete' key removed by null in src")
	}
	if out["keep"] != 1 {
		t.Errorf("expected 'keep' preserved, got %v", out["keep"])
	}
}

func TestDeepMerge_ScalarOverride(t *testing.T) {
	dst := map[string]any{
		"port":   float64(8080),
		"debug":  false,
		"title":  "old",
	}
	src := map[string]any{
		"port":  float64(9090),
		"debug": true,
	}

	out := DeepMerge(dst, src)

	if out["port"] != float64(9090) {
		t.Errorf("expected port overridden to 9090, got %v", out["port"])
	}
	if out["debug"] != true {
		t.Errorf("expected debug overridden to true, got %v", out["debug"])
	}
	if out["title"] != "old" {
		t.Errorf("expected title preserved, got %v", out["title"])
	}
}

func TestDeepMerge_DstNotMutated(t *testing.T) {
	dst := map[string]any{
		"daemon": map[string]any{"log_level": "info"},
	}
	src := map[string]any{
		"daemon": map[string]any{"log_level": "debug"},
	}

	_ = DeepMerge(dst, src)

	daemon, ok := dst["daemon"].(map[string]any)
	if !ok {
		t.Fatalf("expected dst daemon map intact, got %T", dst["daemon"])
	}
	if daemon["log_level"] != "info" {
		t.Errorf("deepMerge should not mutate dst; got log_level=%v want info", daemon["log_level"])
	}
}

func TestDeepMerge_DeepNested(t *testing.T) {
	dst := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": 1,
				"d": 2,
			},
			"e": 3,
		},
	}
	src := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"d": 20,
			},
		},
	}

	out := DeepMerge(dst, src)
	a, _ := out["a"].(map[string]any)
	b, _ := a["b"].(map[string]any)
	if b["c"] != 1 {
		t.Errorf("expected deep nested key c preserved, got %v", b["c"])
	}
	if b["d"] != 20 {
		t.Errorf("expected deep nested key d overridden, got %v", b["d"])
	}
	if a["e"] != 3 {
		t.Errorf("expected mid-level key e preserved, got %v", a["e"])
	}
}

// TestMerge_NodeOverrideDeepMergeViaJSON verifies the end-to-end deep-merge
// pipeline by reading back the merged file and parsing it.
func TestMerge_NodeOverrideDeepMergeViaJSON(t *testing.T) {
	tmpDir := t.TempDir()
	checkoutDir := filepath.Join(tmpDir, "checkout")
	nodeID := "node-deep"

	sharedDir := filepath.Join(checkoutDir, "config", "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sharedConfig := `{
		"daemon": {
			"socket_path": "/tmp/a.sock",
			"log_level": "info",
			"features": ["x", "y"]
		},
		"retain": true
	}`
	if err := os.WriteFile(filepath.Join(sharedDir, "meept.json5"), []byte(sharedConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	nodeDir := filepath.Join(checkoutDir, "config", "nodes", nodeID)
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	overrideConfig := `{
		"daemon": {
			"log_level": "warn",
			"features": ["z"]
		},
		"retain": null
	}`
	if err := os.WriteFile(filepath.Join(nodeDir, "meept.json5"), []byte(overrideConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMerger(tmpDir, checkoutDir, nodeID, &testLogger{})
	if _, err := m.Merge("deep1"); err != nil {
		t.Fatal(err)
	}

	dstPath := filepath.Join(tmpDir, "meept.json5")
	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("merged config not written: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("merged config is not valid JSON: %v\n%s", err, string(data))
	}

	daemon, ok := got["daemon"].(map[string]any)
	if !ok {
		t.Fatalf("expected daemon map, got %T", got["daemon"])
	}
	if daemon["socket_path"] != "/tmp/a.sock" {
		t.Errorf("expected shared socket_path preserved by deep merge, got %v", daemon["socket_path"])
	}
	if daemon["log_level"] != "warn" {
		t.Errorf("expected log_level overridden to warn, got %v", daemon["log_level"])
	}
	features, ok := daemon["features"].([]any)
	if !ok || len(features) != 1 || features[0] != "z" {
		t.Errorf("expected features array replaced (not concat), got %v", daemon["features"])
	}
	if _, ok := got["retain"]; ok {
		t.Error("expected 'retain' deleted by null override")
	}
}
