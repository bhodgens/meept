package configui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools/mcp"
)

func TestAtomicWriteJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json5")

	data := map[string]string{"key": "value"}
	err := WriteConfigFile(path, data)
	if err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("expected value, got %s", got["key"])
	}
}

func TestAtomicWriteCreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "test.json5")

	err := WriteConfigFile(path, map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("WriteConfigFile with nested dir: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("file should exist")
	}
}

func TestAtomicWritePreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.json5")

	err := WriteConfigFile(path, map[string]string{"key": "secret"})
	if err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
}

func TestWriteMainConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")

	cfg := config.DefaultConfig()
	cfg.Daemon.LogLevel = "debug"

	err := WriteConfigFile(path, cfg)
	if err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var got config.Config
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Daemon.LogLevel != "debug" {
		t.Errorf("expected debug, got %s", got.Daemon.LogLevel)
	}
}

func TestConfigFilePath(t *testing.T) {
	// Verify ConfigFilePath returns the expected paths
	p := ConfigFilePath("meept.json5")
	if p == "" {
		t.Error("expected non-empty path")
	}
}

// --- Drilldown save tests ---
// These tests verify that the drilldown save handlers correctly apply field
// changes to nested config structures. They override the loader functions and
// ConfigFilePath to use test-controlled data and temp directories.

func TestSaveModelsConfigDrilldownProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json5")

	origLoader := loadProvidersConfig
	origPath := ConfigFilePath
	t.Cleanup(func() {
		loadProvidersConfig = origLoader
		ConfigFilePath = origPath
	})

	// Inject a config with an existing openai provider
	loadProvidersConfig = func() (*llm.ProvidersConfig, error) {
		return &llm.ProvidersConfig{
			Model: "default-model",
			Providers: map[string]llm.ProviderConfig{
				"openai": {
					API: "openai",
					Options: llm.ProviderOptionsConfig{
						BaseURL: "https://api.openai.com",
						APIKey:  "old-key",
						Timeout: 30,
					},
				},
			},
		}, nil
	}
	ConfigFilePath = func(name string) string { return path }

	// Create a drilldown section for the openai provider
	apiField := NewTextField("api", "api type", "openai")
	baseURLField := NewTextField("options.baseURL", "base url", "https://api.openai.com")
	apiKeyField := NewTextField("options.apiKey", "api key", "old-key")
	timeoutField := NewNumberField("options.timeout", "timeout", 30)

	// Simulate user changing the API key and base URL
	apiKeyField.Set("new-secret-key")
	baseURLField.Set("https://custom.proxy.com")

	sm := NewDrilldownSectionModel(
		"models > providers > openai", "models", "models.json5",
		"providers.openai",
		[]Field{apiField, baseURLField, apiKeyField, timeoutField},
	)

	if err := saveModelsConfig(sm); err != nil {
		t.Fatalf("saveModelsConfig: %v", err)
	}

	// Verify the saved config has updated values
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	providers := got["providers"].(map[string]any)
	openai := providers["openai"].(map[string]any)
	opts := openai["options"].(map[string]any)
	if opts["apiKey"] != "new-secret-key" {
		t.Errorf("expected apiKey 'new-secret-key', got %v", opts["apiKey"])
	}
	if opts["baseURL"] != "https://custom.proxy.com" {
		t.Errorf("expected baseURL 'https://custom.proxy.com', got %v", opts["baseURL"])
	}
	// Unchanged field should remain
	if openai["api"] != "openai" {
		t.Errorf("expected api 'openai', got %v", openai["api"])
	}
	if opts["timeout"] != float64(30) {
		t.Errorf("expected timeout 30, got %v", opts["timeout"])
	}
}

func TestSaveModelsConfigDrilldownNewProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json5")

	origLoader := loadProvidersConfig
	origPath := ConfigFilePath
	t.Cleanup(func() {
		loadProvidersConfig = origLoader
		ConfigFilePath = origPath
	})

	loadProvidersConfig = func() (*llm.ProvidersConfig, error) {
		return &llm.ProvidersConfig{
			Model:     "default-model",
			Providers: map[string]llm.ProviderConfig{},
		}, nil
	}
	ConfigFilePath = func(name string) string { return path }

	// Create a drilldown section for a new provider
	apiField := NewTextField("api", "api type", "")
	baseURLField := NewTextField("options.baseURL", "base url", "")

	// Simulate user setting values for new provider
	apiField.Set("anthropic")
	baseURLField.Set("https://api.anthropic.com")

	sm := NewDrilldownSectionModel(
		"models > providers > anthropic", "models", "models.json5",
		"providers.anthropic",
		[]Field{apiField, baseURLField},
	)

	if err := saveModelsConfig(sm); err != nil {
		t.Fatalf("saveModelsConfig: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	providers := got["providers"].(map[string]any)
	anthropic, ok := providers["anthropic"].(map[string]any)
	if !ok {
		t.Fatal("expected anthropic provider to be created")
	}
	if anthropic["api"] != "anthropic" {
		t.Errorf("expected api 'anthropic', got %v", anthropic["api"])
	}
}

func TestSaveMCPServersConfigDrilldown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp_servers.json5")

	origLoader := loadMCPConfig
	origPath := ConfigFilePath
	t.Cleanup(func() {
		loadMCPConfig = origLoader
		ConfigFilePath = origPath
	})

	loadMCPConfig = func() (*config.MCPServersConfig, error) {
		return &config.MCPServersConfig{
			Servers: []mcp.ServerConfig{
				{Name: "myserver", Type: "stdio", Command: []string{"node", "server.js"}},
			},
		}, nil
	}
	ConfigFilePath = func(name string) string { return path }

	// Create drilldown section for the MCP server
	nameField := NewTextField("name", "name", "myserver")
	typeField := NewSelectField("type", "type", "stdio", []string{"stdio", "http"})
	urlField := NewTextField("url", "url", "")

	// Simulate user changing type to http and adding url
	typeField.Set("http")
	urlField.Set("https://mcp.example.com/sse")

	sm := NewDrilldownSectionModel(
		"mcp servers > servers > myserver", "mcp_servers", "mcp_servers.json5",
		"servers.myserver",
		[]Field{nameField, typeField, urlField},
	)

	if err := saveMCPServersConfig(sm); err != nil {
		t.Fatalf("saveMCPServersConfig: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	servers := got["servers"].([]any)
	server := servers[0].(map[string]any)
	if server["type"] != "http" {
		t.Errorf("expected type 'http', got %v", server["type"])
	}
	if server["url"] != "https://mcp.example.com/sse" {
		t.Errorf("expected url 'https://mcp.example.com/sse', got %v", server["url"])
	}
}

func TestSavePresetsConfigDrilldown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "presets.json5")

	origLoader := loadPresetsConfig
	origPath := ConfigFilePath
	t.Cleanup(func() {
		loadPresetsConfig = origLoader
		ConfigFilePath = origPath
	})

	loadPresetsConfig = func() (*config.PresetConfig, error) {
		return &config.PresetConfig{
			Default: "development",
			Presets: map[string]*config.ModelPreset{
				"development": {
					Label:       "Development",
					Description: "Balanced for coding tasks",
					Params: config.ModelParams{
						Temperature: 0.3,
						TopP:        0.9,
					},
				},
			},
		}, nil
	}
	ConfigFilePath = func(name string) string { return path }

	// Create drilldown section for the development preset
	labelField := NewTextField("label", "label", "Development")
	tempField := NewFloatField("params.temperature", "temperature", 0.3)

	// Simulate user changing temperature
	tempField.Set("0.7")

	sm := NewDrilldownSectionModel(
		"presets > presets > development", "presets", "presets.json5",
		"presets.development",
		[]Field{labelField, tempField},
	)

	if err := savePresetsConfig(sm); err != nil {
		t.Fatalf("savePresetsConfig: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	presets := got["presets"].(map[string]any)
	dev := presets["development"].(map[string]any)
	params := dev["params"].(map[string]any)
	if params["temperature"] != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", params["temperature"])
	}
	if dev["label"] != "Development" {
		t.Errorf("expected label 'Development' (unchanged), got %v", dev["label"])
	}
}

func TestSaveModelsConfigTopLevel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json5")

	origLoader := loadProvidersConfig
	origPath := ConfigFilePath
	t.Cleanup(func() {
		loadProvidersConfig = origLoader
		ConfigFilePath = origPath
	})

	loadProvidersConfig = func() (*llm.ProvidersConfig, error) {
		return &llm.ProvidersConfig{
			Model: "old-model",
			Providers: map[string]llm.ProviderConfig{
				"openai": {API: "openai"},
			},
		}, nil
	}
	ConfigFilePath = func(name string) string { return path }

	// Non-drilldown (top-level) section should still work
	modelField := NewTextField("model", "default model", "old-model")
	modelField.Set("new-model")

	sm := NewSectionModel("models", "models", "models.json5", []Field{modelField})
	if err := saveModelsConfig(sm); err != nil {
		t.Fatalf("saveModelsConfig: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["model"] != "new-model" {
		t.Errorf("expected model 'new-model', got %v", got["model"])
	}
	// Provider should be preserved
	providers := got["providers"].(map[string]any)
	if _, ok := providers["openai"]; !ok {
		t.Error("expected openai provider to be preserved")
	}
}

// TestSaveModelsConfigDrilldownLifecycleModelPaths verifies that a dirty
// lifecycle.model_paths field, which is a JSON-encoded map[string]string in the
// text field, round-trips through saveModelsConfig into the written models.json5
// as a proper "model_paths" map under the provider's lifecycle block.
// This covers spec §5a (TUI lifecycle field save path for multi-model servers).
func TestSaveModelsConfigDrilldownLifecycleModelPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json5")

	origLoader := loadProvidersConfig
	origPath := ConfigFilePath
	t.Cleanup(func() {
		loadProvidersConfig = origLoader
		ConfigFilePath = origPath
	})

	loadProvidersConfig = func() (*llm.ProvidersConfig, error) {
		return &llm.ProvidersConfig{
			Providers: map[string]llm.ProviderConfig{
				"llama-cpp": {
					API: "openai",
					Options: llm.ProviderOptionsConfig{
						BaseURL: "http://127.0.0.1:8080",
					},
				},
			},
		}, nil
	}
	ConfigFilePath = func(name string) string { return path }

	// Create a drilldown section with a lifecycle.model_paths field. The TUI
	// renders this map as a JSON-encoded string; saveModelsConfig must parse
	// it back into a map[string]string.
	modelPathsField := NewTextField(
		"lifecycle.model_paths",
		"model paths (json)",
		`{}`,
	)
	modelPathsField.Set(`{"code":"/models/lfm-code.gguf","chat":"/models/lfm-chat.gguf"}`)

	sm := NewDrilldownSectionModel(
		"models > providers > llama-cpp", "models", "models.json5",
		"providers.llama-cpp",
		[]Field{modelPathsField},
	)

	if err := saveModelsConfig(sm); err != nil {
		t.Fatalf("saveModelsConfig: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	providers := got["providers"].(map[string]any)
	provider := providers["llama-cpp"].(map[string]any)
	lifecycle, ok := provider["lifecycle"].(map[string]any)
	if !ok {
		t.Fatalf("expected lifecycle block; provider contents: %v", provider)
	}
	modelPaths, ok := lifecycle["model_paths"].(map[string]any)
	if !ok {
		t.Fatalf("expected model_paths map under lifecycle; lifecycle contents: %v", lifecycle)
	}
	if modelPaths["code"] != "/models/lfm-code.gguf" {
		t.Errorf("expected model_paths[code]='/models/lfm-code.gguf', got %v", modelPaths["code"])
	}
	if modelPaths["chat"] != "/models/lfm-chat.gguf" {
		t.Errorf("expected model_paths[chat]='/models/lfm-chat.gguf', got %v", modelPaths["chat"])
	}
}

// TestSaveModelsConfigDrilldownLifecycleSpawnCommand verifies that a dirty
// lifecycle.spawn_command field (space-separated string in the TUI) is split
// into []string on save. This covers spec §5a (TUI lifecycle field save path).
func TestSaveModelsConfigDrilldownLifecycleSpawnCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json5")

	origLoader := loadProvidersConfig
	origPath := ConfigFilePath
	t.Cleanup(func() {
		loadProvidersConfig = origLoader
		ConfigFilePath = origPath
	})

	loadProvidersConfig = func() (*llm.ProvidersConfig, error) {
		return &llm.ProvidersConfig{
			Providers: map[string]llm.ProviderConfig{
				"llama-cpp": {API: "openai"},
			},
		}, nil
	}
	ConfigFilePath = func(name string) string { return path }

	spawnField := NewTextField(
		"lifecycle.spawn_command",
		"spawn command",
		``,
	)
	spawnField.Set("llama-server --port 8080 --model ${MODEL_PATH}")

	sm := NewDrilldownSectionModel(
		"models > providers > llama-cpp", "models", "models.json5",
		"providers.llama-cpp",
		[]Field{spawnField},
	)

	if err := saveModelsConfig(sm); err != nil {
		t.Fatalf("saveModelsConfig: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	provider := got["providers"].(map[string]any)["llama-cpp"].(map[string]any)
	lifecycle, ok := provider["lifecycle"].(map[string]any)
	if !ok {
		t.Fatalf("expected lifecycle block; provider: %v", provider)
	}
	cmd, ok := lifecycle["spawn_command"].([]any)
	if !ok {
		t.Fatalf("expected spawn_command array; lifecycle: %v", lifecycle)
	}
	want := []string{"llama-server", "--port", "8080", "--model", "${MODEL_PATH}"}
	if len(cmd) != len(want) {
		t.Fatalf("expected spawn_command len=%d, got %d (%v)", len(want), len(cmd), cmd)
	}
	for i, w := range want {
		if cmd[i] != w {
			t.Errorf("spawn_command[%d]: want %q, got %v", i, w, cmd[i])
		}
	}
}
