package http

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPatchClientConfig_MergesNestedKey verifies a patch is deep-merged
// onto the on-disk client.json5. Existing sibling keys inside the merged
// object and unrelated top-level keys must be preserved. The on-disk file
// must be valid JSON after the write (JSON5 comments stripped).
func TestPatchClientConfig_MergesNestedKey(t *testing.T) {
	dir := t.TempDir()
	cs := &ConfigService{meeptDir: dir}

	// Seed an existing client.json5 with JSON5 (comment + trailing comma,
	// which hujson standardizes to strict JSON for parsing).
	seed := `{
  // client config
  "theme": "system",
  "chat": {
    "verbosity": "normal",
    "scroll_speed": 3,
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "client.json5"), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	patch := map[string]any{
		"chat": map[string]any{
			"verbosity": "verbose",
		},
	}

	merged, err := cs.PatchClientConfig(patch)
	if err != nil {
		t.Fatalf("PatchClientConfig: %v", err)
	}

	// Returned map reflects the merge.
	chat, ok := merged["chat"].(map[string]any)
	if !ok {
		t.Fatalf("expected chat map, got %T", merged["chat"])
	}
	if chat["verbosity"] != "verbose" {
		t.Errorf("verbosity = %v, want verbose", chat["verbosity"])
	}
	if chat["scroll_speed"] != float64(3) {
		t.Errorf("scroll_speed not preserved: got %v", chat["scroll_speed"])
	}
	if merged["theme"] != "system" {
		t.Errorf("theme not preserved: got %v", merged["theme"])
	}

	// On-disk file is valid JSON (comments stripped by Standardize).
	reread, err := os.ReadFile(filepath.Join(dir, "client.json5"))
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(reread, &onDisk); err != nil {
		t.Fatalf("on-disk file is not valid JSON: %v\n%s", err, reread)
	}
	chat2, _ := onDisk["chat"].(map[string]any)
	if chat2["verbosity"] != "verbose" {
		t.Errorf("on-disk verbosity = %v, want verbose", chat2["verbosity"])
	}
}

// TestPatchClientConfig_NullDeletesKey verifies RFC 7396 null-deletes-key.
func TestPatchClientConfig_NullDeletesKey(t *testing.T) {
	dir := t.TempDir()
	cs := &ConfigService{meeptDir: dir}

	seed := `{"keep": 1, "drop": "x"}`
	if err := os.WriteFile(filepath.Join(dir, "client.json5"), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	patch := map[string]any{"drop": nil}
	merged, err := cs.PatchClientConfig(patch)
	if err != nil {
		t.Fatalf("PatchClientConfig: %v", err)
	}
	if _, exists := merged["drop"]; exists {
		t.Errorf("expected 'drop' deleted by null patch")
	}
	if merged["keep"] != float64(1) {
		t.Errorf("expected 'keep' preserved, got %v", merged["keep"])
	}
}

// TestPatchClientConfig_AddsNewKey verifies a new top-level key is added.
func TestPatchClientConfig_AddsNewKey(t *testing.T) {
	dir := t.TempDir()
	cs := &ConfigService{meeptDir: dir}

	seed := `{"a": 1}`
	if err := os.WriteFile(filepath.Join(dir, "client.json5"), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	patch := map[string]any{"b": float64(2)}
	merged, err := cs.PatchClientConfig(patch)
	if err != nil {
		t.Fatalf("PatchClientConfig: %v", err)
	}
	if merged["a"] != float64(1) {
		t.Errorf("expected 'a' preserved, got %v", merged["a"])
	}
	if merged["b"] != float64(2) {
		t.Errorf("expected 'b' added, got %v", merged["b"])
	}
}

// TestPatchClientConfig_PreservesUnrelatedKeys verifies that patching one
// nested object does not disturb unrelated sibling objects.
func TestPatchClientConfig_PreservesUnrelatedKeys(t *testing.T) {
	dir := t.TempDir()
	cs := &ConfigService{meeptDir: dir}

	seed := `{
  "keybindings": {"chat": "/"},
  "chat": {"verbosity": "normal"}
}`
	if err := os.WriteFile(filepath.Join(dir, "client.json5"), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	patch := map[string]any{
		"chat": map[string]any{"verbosity": "quiet"},
	}
	merged, err := cs.PatchClientConfig(patch)
	if err != nil {
		t.Fatalf("PatchClientConfig: %v", err)
	}
	kb, ok := merged["keybindings"].(map[string]any)
	if !ok {
		t.Fatalf("expected keybindings map preserved, got %T", merged["keybindings"])
	}
	if kb["chat"] != "/" {
		t.Errorf("keybindings.chat not preserved: got %v", kb["chat"])
	}
	chat, _ := merged["chat"].(map[string]any)
	if chat["verbosity"] != "quiet" {
		t.Errorf("verbosity = %v, want quiet", chat["verbosity"])
	}
}

// TestPatchClientConfig_AtomicWrite verifies the atomic-write contract:
// after a successful call, no .tmp file lingers and the target file is
// complete and valid JSON.
func TestPatchClientConfig_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	cs := &ConfigService{meeptDir: dir}

	seed := `{"x": 1}`
	if err := os.WriteFile(filepath.Join(dir, "client.json5"), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	if _, err := cs.PatchClientConfig(map[string]any{"y": float64(2)}); err != nil {
		t.Fatalf("PatchClientConfig: %v", err)
	}

	// No .tmp file remains.
	if _, err := os.Stat(filepath.Join(dir, "client.json5.tmp")); err == nil {
		t.Errorf("temp file client.json5.tmp should not exist after successful rename")
	} else if !os.IsNotExist(err) {
		t.Errorf("unexpected error checking temp file: %v", err)
	}

	// Target file is complete and valid JSON.
	data, err := os.ReadFile(filepath.Join(dir, "client.json5"))
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("on-disk file is not valid JSON after atomic write: %v\n%s", err, data)
	}
	if onDisk["x"] != float64(1) {
		t.Errorf("x not preserved: got %v", onDisk["x"])
	}
	if onDisk["y"] != float64(2) {
		t.Errorf("y not written: got %v", onDisk["y"])
	}
}

// TestPatchClientConfig_FileMissing_CreatesWithDefaults verifies that when
// client.json5 does not exist, the default content block (the same one used
// by LoadClientConfig) is used as the base and merged with the patch.
func TestPatchClientConfig_FileMissing_CreatesWithDefaults(t *testing.T) {
	dir := t.TempDir()
	cs := &ConfigService{meeptDir: dir}

	patch := map[string]any{
		"chat": map[string]any{"verbosity": "verbose"},
	}
	merged, err := cs.PatchClientConfig(patch)
	if err != nil {
		t.Fatalf("PatchClientConfig: %v", err)
	}

	// Patch applied.
	chat, ok := merged["chat"].(map[string]any)
	if !ok {
		t.Fatalf("expected chat map, got %T", merged["chat"])
	}
	if chat["verbosity"] != "verbose" {
		t.Errorf("verbosity = %v, want verbose", chat["verbosity"])
	}

	// Defaults from LoadClientConfig preserved.
	if merged["theme"] != "system" {
		t.Errorf("default theme not preserved: got %v", merged["theme"])
	}
	if merged["language"] != "en" {
		t.Errorf("default language not preserved: got %v", merged["language"])
	}
	notifications, ok := merged["notifications"].(map[string]any)
	if !ok {
		t.Fatalf("expected default notifications map, got %T", merged["notifications"])
	}
	if notifications["enabled"] != true {
		t.Errorf("default notifications.enabled not preserved: got %v", notifications["enabled"])
	}
	menubar, ok := merged["menubar"].(map[string]any)
	if !ok {
		t.Fatalf("expected default menubar map, got %T", merged["menubar"])
	}
	if menubar["show_status"] != true {
		t.Errorf("default menubar.show_status not preserved: got %v", menubar["show_status"])
	}

	// File exists on disk now.
	if _, err := os.Stat(filepath.Join(dir, "client.json5")); err != nil {
		t.Errorf("expected file created: %v", err)
	}
}

// TestPatchClientConfig_InvalidJSON5_ReturnsError verifies an unparseable
// existing file surfaces an error rather than silently corrupting state.
func TestPatchClientConfig_InvalidJSON5_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	cs := &ConfigService{meeptDir: dir}

	bad := []byte("{not valid json5")
	if err := os.WriteFile(filepath.Join(dir, "client.json5"), bad, 0o600); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	_, err := cs.PatchClientConfig(map[string]any{"x": float64(1)})
	if err == nil {
		t.Fatal("expected error parsing invalid JSON5, got nil")
	}
}
