package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// TestLoadClientConfigPath_FallbackToUserConfig verifies that when no
// client.json5 exists (neither project-local nor user-global), the
// returned path points at the user-global location (~/.meept/client.json5)
// so persistVerbosity has a valid target.
func TestLoadClientConfigPath_FallbackToUserConfig(t *testing.T) {
	// Change to a temp dir so .meept/client.json5 definitely doesn't exist.
	tmp := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	cfg, path := LoadClientConfigPath()

	// Config must be non-nil (defaults).
	if cfg == nil {
		t.Fatal("expected non-nil default config")
	}

	// Path must be absolute (under the user's home).
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %q", path)
	}
	if !strings.HasSuffix(path, filepath.Join(".meept", "client.json5")) {
		t.Errorf("expected path to end with .meept/client.json5, got %q", path)
	}
}

// TestLoadClientConfigPath_ProjectLocal verifies the project-local file
// is picked up first when present.
func TestLoadClientConfigPath_ProjectLocal(t *testing.T) {
	tmp := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	// Create .meept/client.json5 in the temp dir.
	meeptDir := filepath.Join(tmp, ".meept")
	if err := os.MkdirAll(meeptDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	localPath := filepath.Join(meeptDir, "client.json5")
	if err := os.WriteFile(localPath, []byte(`{"chat":{"verbosity":"quiet"}}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, path := LoadClientConfigPath()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Chat.Verbosity != "quiet" {
		t.Errorf("verbosity = %q, want quiet", cfg.Chat.Verbosity)
	}
	// Path should be the relative project-local path.
	wantPath := filepath.Join(".meept", "client.json5")
	if path != wantPath {
		t.Errorf("path = %q, want %q", path, wantPath)
	}
}
