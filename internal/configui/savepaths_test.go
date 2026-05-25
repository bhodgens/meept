package configui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tui"
	"github.com/tailscale/hujson"
)

func TestResolveFullPathCases(t *testing.T) {
	tests := []struct {
		prefix   string
		fieldKey string
		want     string
	}{
		{"daemon", "log_level", "daemon.log_level"},
		{"llm", "llm.budget.hourly_token_limit", "llm.budget.hourly_token_limit"},
		{"agent", "cache.enabled", "agent.cache.enabled"},
		{"transport", "transport.rpc.enabled", "transport.rpc.enabled"},
		{"security", "sanitize_inputs", "security.sanitize_inputs"},
	}
	for _, tt := range tests {
		got := resolveFullPath(tt.prefix, tt.fieldKey)
		if got != tt.want {
			t.Errorf("resolveFullPath(%q, %q) = %q, want %q", tt.prefix, tt.fieldKey, got, tt.want)
		}
	}
}

func TestSaveMainConfigDrilldownSubStruct(t *testing.T) {
	origLoader := loadMainConfig
	origPath := ConfigFilePath
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	t.Cleanup(func() {
		loadMainConfig = origLoader
		ConfigFilePath = origPath
	})

	loadMainConfig = func() (*config.Config, error) {
		cfg := config.DefaultConfig()
		return cfg, nil
	}
	ConfigFilePath = func(name string) string { return path }

	cacheEnabledField := NewToggleField("cache.enabled", "enabled", true)
	cacheEnabledField.Set("false")

	sm := NewDrilldownSectionModel(
		"agent loop > cache > cache", "agent", "meept.json5",
		"agent",
		[]Field{cacheEnabledField},
	)

	if err := saveMainConfigSection(sm); err != nil {
		t.Fatalf("saveMainConfigSection: %v", err)
	}

	cfg2, err := config.LoadJSON5Config(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg2.Agent.Cache.Enabled {
		t.Error("expected agent.cache.enabled = false after save")
	}
}

func TestSaveMainConfigMapDrilldown(t *testing.T) {
	origLoader := loadMainConfig
	origPath := ConfigFilePath
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	t.Cleanup(func() {
		loadMainConfig = origLoader
		ConfigFilePath = origPath
	})

	loadMainConfig = func() (*config.Config, error) {
		cfg := config.DefaultConfig()
		cfg.CodeIntel.LSP.Servers = map[string]config.LSPServerConfig{
			"golang": {Command: "gopls", Transport: "stdio"},
		}
		return cfg, nil
	}
	ConfigFilePath = func(name string) string { return path }

	cmdField := NewTextField("command", "command", "gopls")
	cmdField.Set("gopls-new")
	portField := NewNumberField("port", "port", 0)
	portField.Set("8080")

	sm := NewDrilldownSectionModel(
		"code intel > lsp servers > golang", "code_intel", "meept.json5",
		"lsp.servers.golang",
		[]Field{cmdField, portField},
	)

	if err := saveMainConfigSection(sm); err != nil {
		t.Fatalf("saveMainConfigSection: %v", err)
	}

	cfg2, err := config.LoadJSON5Config(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	srv, ok := cfg2.CodeIntel.LSP.Servers["golang"]
	if !ok {
		t.Fatal("expected golang server to exist")
	}
	if srv.Command != "gopls-new" {
		t.Errorf("expected command 'gopls-new', got %q", srv.Command)
	}
	if srv.Port != 8080 {
		t.Errorf("expected port 8080, got %d", srv.Port)
	}
}

func TestSaveClientConfigMapStringString(t *testing.T) {
	origLoader := loadClientConfig
	origPath := ConfigFilePath
	dir := t.TempDir()
	path := filepath.Join(dir, "client.json5")
	t.Cleanup(func() {
		loadClientConfig = origLoader
		ConfigFilePath = origPath
	})

	loadClientConfig = func() (*tui.ClientConfig, error) {
		cfg := tui.DefaultClientConfig()
		cfg.Vim.Normal = map[string]string{
			"dd": "delete_line",
			"yy": "yank_line",
		}
		return cfg, nil
	}
	ConfigFilePath = func(name string) string { return path }

	// Simulate editing one item's value and having all items available
	ddField := NewTextField("value", "dd", "delete_line")
	ddField.Set("delete_line_v2")
	yyField := NewTextField("value", "yy", "yank_line")

	sm := NewMapStringStringDrilldownSectionModel(
		"client / tui > vim normal bindings > dd", "client", "client.json5",
		"vim.normal", "vim.normal",
		[]Field{ddField},
		[]DrilldownItem{
			{Name: "dd", Fields: []Field{ddField}},
			{Name: "yy", Fields: []Field{yyField}},
		},
	)

	if err := saveClientConfig(sm); err != nil {
		t.Fatalf("saveClientConfig: %v", err)
	}

	// Verify by loading the written file
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	standardJSON, err := hujson.Standardize(data)
	if err != nil {
		t.Fatalf("standardize json5: %v", err)
	}
	var cfg2 tui.ClientConfig
	if err := json.Unmarshal(standardJSON, &cfg2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg2.Vim.Normal["dd"] != "delete_line_v2" {
		t.Errorf("expected dd='delete_line_v2', got %q", cfg2.Vim.Normal["dd"])
	}
	if cfg2.Vim.Normal["yy"] != "yank_line" {
		t.Errorf("expected yy='yank_line', got %q", cfg2.Vim.Normal["yy"])
	}
}
