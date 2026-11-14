package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfig_KanbanDefaults pins the [kanban] block of C9: every surface is
// opt-in (docs/agents/opt-in-defaults.md) — the whole feature is inert at
// default config.
func TestConfig_KanbanDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Kanban.Enabled {
		t.Errorf("Kanban.Enabled = true, want false")
	}
	if cfg.Kanban.SidecarEnabled {
		t.Errorf("Kanban.SidecarEnabled = true, want false")
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
