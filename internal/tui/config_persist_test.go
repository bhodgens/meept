package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPersistVerbosity_UpdatesExistingFile verifies the helper writes
// chat.verbosity into the on-disk client.json5 without clobbering other keys.
func TestPersistVerbosity_UpdatesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.json5")

	seed := `{
  // comment
  "chat": {
    "verbosity": "normal",
    "scroll_speed": 3
  },
  "theme": "monokai"
}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := persistVerbosity(path, "verbose"); err != nil {
		t.Fatalf("persistVerbosity: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	// File must be valid JSON (comments stripped by Standardize).
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("on-disk file not valid JSON: %v\n%s", err, raw)
	}
	chat, _ := got["chat"].(map[string]any)
	if chat["verbosity"] != "verbose" {
		t.Errorf("verbosity = %v, want verbose", chat["verbosity"])
	}
	if chat["scroll_speed"] != float64(3) {
		t.Errorf("scroll_speed not preserved: %v", chat["scroll_speed"])
	}
	if got["theme"] != "monokai" {
		t.Errorf("theme not preserved: %v", got["theme"])
	}
}

// TestPersistVerbosity_CreatesFileWhenMissing verifies the helper
// bootstraps a minimal client.json5 when none exists.
func TestPersistVerbosity_CreatesFileWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.json5")

	if err := persistVerbosity(path, "quiet"); err != nil {
		t.Fatalf("persistVerbosity: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("on-disk file not valid JSON: %v\n%s", err, raw)
	}
	chat, _ := got["chat"].(map[string]any)
	if chat["verbosity"] != "quiet" {
		t.Errorf("verbosity = %v, want quiet", chat["verbosity"])
	}
}

// TestPersistVerbosity_InvalidJSON5 verifies an unparseable existing
// file surfaces an error.
func TestPersistVerbosity_InvalidJSON5(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.json5")

	if err := os.WriteFile(path, []byte("{bad json5"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := persistVerbosity(path, "normal"); err == nil {
		t.Fatal("expected error for invalid JSON5, got nil")
	}
}
