package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfig_KanbanDefaults pins the [kanban] block: the board projection
// and its browser gateway sidecar are default-on (plan tree
// tui-gui-consistency-fixes 04-config-docs/01), while the remaining knobs
// keep their enforced defaults (loopback bind, warn WIP, no periodic
// reconcile, GitHub sync off — issue #133).
func TestConfig_KanbanDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Kanban.Enabled {
		t.Errorf("Kanban.Enabled = false, want true")
	}
	if !cfg.Kanban.SidecarEnabled {
		t.Errorf("Kanban.SidecarEnabled = false, want true")
	}
	if got, want := cfg.Kanban.SidecarBind, "127.0.0.1:8790"; got != want {
		t.Errorf("Kanban.SidecarBind = %q, want %q", got, want)
	}
	if got, want := cfg.Kanban.WIPMode, "warn"; got != want {
		t.Errorf("Kanban.WIPMode = %q, want %q", got, want)
	}
	if got, want := cfg.Kanban.ReconcileInterval, "0s"; got != want {
		t.Errorf("Kanban.ReconcileInterval = %q, want %q", got, want)
	}
	if cfg.Kanban.GitHubSync.Enabled {
		t.Errorf("Kanban.GitHubSync.Enabled = true, want false")
	}
}

// TestKanbanConfigJSON5RoundTrip loads a config file carrying the full
// [kanban] stanza and asserts every field survives the load path —
// especially reconcile_interval, a duration-LOOKING STRING that the loader's
// preprocessDurations pass would otherwise rewrite to a nanosecond integer
// unless the key is registered in stringDurationKeys (the queue
// interactive_window precedent in json5_loader.go).
func TestKanbanConfigJSON5RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "kanban": {
    "enabled": true,
    "sidecar_enabled": true,
    "sidecar_bind": "0.0.0.0:9000",
    "wip_mode": "block",
    "reconcile_interval": "10m",
    "github_sync": {
      "enabled": true
    }
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !cfg.Kanban.Enabled {
		t.Error("Kanban.Enabled = false, want true")
	}
	if !cfg.Kanban.SidecarEnabled {
		t.Error("Kanban.SidecarEnabled = false, want true")
	}
	if got, want := cfg.Kanban.SidecarBind, "0.0.0.0:9000"; got != want {
		t.Errorf("Kanban.SidecarBind = %q, want %q", got, want)
	}
	if got, want := cfg.Kanban.WIPMode, "block"; got != want {
		t.Errorf("Kanban.WIPMode = %q, want %q", got, want)
	}
	if got, want := cfg.Kanban.ReconcileInterval, "10m"; got != want {
		t.Errorf("Kanban.ReconcileInterval = %q, want %q", got, want)
	}
	if !cfg.Kanban.GitHubSync.Enabled {
		t.Error("Kanban.GitHubSync.Enabled = false, want true")
	}
}

// TestKanbanReconcileIntervalDurationExemption pins BOTH sides of the
// stringDurationKeys exemption at the exact layer that can break it:
// reconcile_interval must stay a quoted string (exempt side — otherwise
// json.Unmarshal fails "cannot unmarshal number into string"), while a real
// time.Duration key still converts to nanoseconds (convert side — the
// exemption must not become over-broad).
func TestKanbanReconcileIntervalDurationExemption(t *testing.T) {
	exempt := preprocessDurations(`{"kanban":{"reconcile_interval":"5m"}}`)
	if !strings.Contains(exempt, `"5m"`) {
		t.Errorf("exempt key: reconcile_interval rewritten, want \"5m\" to survive; got %s", exempt)
	}

	convert := preprocessDurations(`{"daemon":{"shutdown_timeout":"90s"}}`)
	if !strings.Contains(convert, "90000000000") {
		t.Errorf("convert side: shutdown_timeout not converted to ns; got %s", convert)
	}
	if strings.Contains(convert, `"90s"`) {
		t.Errorf("convert side: shutdown_timeout left as quoted string; got %s", convert)
	}
}

// TestKanbanAllowedOriginsValidate pins the [kanban].allowed_origins
// entry grammar: exact scheme://host[:port] origins only. Empty/absent =
// valid (the byte-identical default); wildcards, paths/trailing slashes,
// non-http schemes, bare hosts, and credentials all fail load.
func TestKanbanAllowedOriginsValidate(t *testing.T) {
	ok := []string{
		"https://board.example.com",
		"http://localhost:8790",
		"https://board.example.com:8443",
	}
	for _, entry := range ok {
		if err := (KanbanConfig{AllowedOrigins: []string{entry}}).Validate(); err != nil {
			t.Errorf("valid entry %q rejected: %v", entry, err)
		}
	}

	bad := []string{
		"*",
		"https://*.example.com",
		"https://board.example.com/",
		"https://board.example.com/app",
		"board.example.com",
		"ftp://board.example.com",
		"https://user:pass@board.example.com",
		"https://board.example.com?x=1",
		"",
	}
	for _, entry := range bad {
		if err := (KanbanConfig{AllowedOrigins: []string{entry}}).Validate(); err == nil {
			t.Errorf("malformed entry %q accepted at load", entry)
		}
	}

	// The empty list — the default — validates (byte-identical).
	if err := (KanbanConfig{}).Validate(); err != nil {
		t.Fatalf("empty kanban block must validate: %v", err)
	}
	// wip_mode check survives the refactor.
	if err := (KanbanConfig{WIPMode: "yolo"}).Validate(); err == nil {
		t.Fatal("wip_mode validation regressed")
	}
}

// TestKanbanAllowedOriginsJSON5RoundTrip proves the key loads through
// the JSON5 path and lands on the struct.
func TestKanbanAllowedOriginsJSON5RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "kanban": {
    "enabled": true,
    "allowed_origins": ["https://board.example.com"]
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Kanban.AllowedOrigins) != 1 || cfg.Kanban.AllowedOrigins[0] != "https://board.example.com" {
		t.Fatalf("allowed_origins = %v, want the one entry", cfg.Kanban.AllowedOrigins)
	}
}
