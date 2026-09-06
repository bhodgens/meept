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

// TestCatalogObscuraEntry verifies the shipped catalog parses and the
// obscura entry is present exactly once, enabled by default, and launches
// "obscura mcp" (PATH lookup, matching the cua-driver native-binary
// pattern) over stdio in the browser category.
func TestCatalogObscuraEntry(t *testing.T) {
	cfg, err := LoadMCPConfig("../../config/mcp_servers.json5")
	if err != nil {
		t.Fatalf("LoadMCPConfig(catalog) failed: %v", err)
	}

	var found *mcp.ServerConfig
	hits := 0
	for i := range cfg.Servers {
		if cfg.Servers[i].Name == "obscura" {
			hits++
			found = &cfg.Servers[i]
		}
	}
	if found == nil {
		t.Fatalf("obscura entry missing from catalog (%d servers)", len(cfg.Servers))
	}
	if hits != 1 {
		t.Errorf("obscura entry appears %d times; server names must be unique", hits)
	}

	if !found.IsEnabled() {
		t.Error("obscura must ship enabled: true")
	}
	wantCmd := []string{"obscura", "mcp"}
	if got := found.Command; len(got) != 2 || got[0] != wantCmd[0] || got[1] != wantCmd[1] {
		t.Errorf("command = %v, want %v (bare binary name: PATH lookup, not an absolute build path)", got, wantCmd)
	}
	if found.Type != "" && found.Type != "stdio" {
		t.Errorf("type = %q, want stdio (or empty for default)", found.Type)
	}
	if found.Category != "browser" {
		t.Errorf("category = %q, want browser", found.Category)
	}
	if found.Description == "" {
		t.Error("description should be non-empty for TUI display")
	}
	if len(found.Env) != 0 {
		t.Errorf("env should be empty, got %v", found.Env)
	}
}

// TestCatalogExcelEntry verifies the shipped catalog parses and the excel
// fallback entry is present, disabled by default, and launches
// "uvx excel-mcp-server stdio" over stdio in the data category.
func TestCatalogExcelEntry(t *testing.T) {
	cfg, err := LoadMCPConfig("../../config/mcp_servers.json5")
	if err != nil {
		t.Fatalf("LoadMCPConfig(catalog) failed: %v", err)
	}

	var found *mcp.ServerConfig
	hits := 0
	for i := range cfg.Servers {
		if cfg.Servers[i].Name == "excel" {
			hits++
			found = &cfg.Servers[i]
		}
	}
	if found == nil {
		t.Fatalf("excel entry missing from catalog (%d servers)", len(cfg.Servers))
	}
	if hits != 1 {
		t.Errorf("excel entry appears %d times; server names must be unique", hits)
	}

	if found.IsEnabled() {
		t.Error("excel must ship enabled: false")
	}
	wantCmd := []string{"uvx", "excel-mcp-server", "stdio"}
	if got := found.Command; len(got) != 3 || got[0] != wantCmd[0] || got[1] != wantCmd[1] || got[2] != wantCmd[2] {
		t.Errorf("command = %v, want %v", got, wantCmd)
	}
	if found.Type != "" && found.Type != "stdio" {
		t.Errorf("type = %q, want stdio (or empty for default)", found.Type)
	}
	if found.Category != "data" {
		t.Errorf("category = %q, want data", found.Category)
	}
	if found.Description == "" {
		t.Error("description should be non-empty for TUI display")
	}
	if len(found.Env) != 0 {
		t.Errorf("env should be empty, got %v", found.Env)
	}
}

// TestCatalogInstallHints is the regression fence for catalog additions:
// every stdio entry must carry a non-empty install_hint (the shell command
// a user can run to install the server's missing dependency); http-transport
// entries must NOT (they have no external binary dependency).
func TestCatalogInstallHints(t *testing.T) {
	cfg, err := LoadMCPConfig("../../config/mcp_servers.json5")
	if err != nil {
		t.Fatalf("LoadMCPConfig(catalog) failed: %v", err)
	}

	stdio, http := 0, 0
	for i := range cfg.Servers {
		s := &cfg.Servers[i]
		isStdio := s.Type == "stdio" || (s.Type == "" && len(s.Command) > 0)
		switch {
		case isStdio:
			stdio++
			if s.InstallHint == "" {
				t.Errorf("stdio entry %q has empty install_hint (needed so doctor can tell the user how to install the dependency)", s.Name)
			}
		case s.Type == "http":
			http++
			if s.InstallHint != "" {
				t.Errorf("http entry %q must not set install_hint, got %q", s.Name, s.InstallHint)
			}
		default:
			t.Errorf("entry %q has unknown transport: type=%q command=%v url=%q", s.Name, s.Type, s.Command, s.URL)
		}
	}
	if stdio+http != len(cfg.Servers) {
		t.Errorf("classified %d of %d entries", stdio+http, len(cfg.Servers))
	}
	if stdio == 0 {
		t.Error("catalog has no stdio entries; TestCatalogInstallHints is vacuous")
	}
	t.Logf("install hints verified: %d stdio, %d http", stdio, http)
}
