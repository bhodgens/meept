package config

import (
	"testing"

	"github.com/caimlas/meept/internal/tools/mcp"
)

// TestCatalogCuaDriverEntry verifies the shipped catalog parses and the
// cua-driver entry is present, disabled by default, and launches
// "cua-driver mcp" over stdio.
func TestCatalogCuaDriverEntry(t *testing.T) {
	cfg, err := LoadMCPConfig("../../config/mcp_servers.json5")
	if err != nil {
		t.Fatalf("LoadMCPConfig(catalog) failed: %v", err)
	}

	var found *mcp.ServerConfig
	for i := range cfg.Servers {
		if cfg.Servers[i].Name == "cua-driver" {
			found = &cfg.Servers[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("cua-driver entry missing from catalog (%d servers)", len(cfg.Servers))
	}

	if found.IsEnabled() {
		t.Error("cua-driver must ship enabled: false")
	}
	if got := len(found.Command); got != 2 || found.Command[0] != "cua-driver" || found.Command[1] != "mcp" {
		t.Errorf("command = %v, want [\"cua-driver\", \"mcp\"]", found.Command)
	}
	if found.Type != "" && found.Type != "stdio" {
		t.Errorf("type = %q, want stdio (or empty for default)", found.Type)
	}
	if found.Category != "automation" {
		t.Errorf("category = %q, want automation", found.Category)
	}
	if found.Description == "" {
		t.Error("description should be non-empty for TUI display")
	}
	if len(found.Env) != 0 {
		t.Errorf("env should be empty, got %v", found.Env)
	}
}
