package configui

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// TestBuildProviderItems_LifecycleAbsent verifies that lifecycle fields are
// surfaced for providers that have no existing lifecycle block. This lets
// users add a new lifecycle via the TUI — the save path initializes the
// lifecycle pointer on first dirty lifecycle.* field.
func TestBuildProviderItems_LifecycleAbsent(t *testing.T) {
	providers := map[string]llm.ProviderConfig{
		"openai": {API: "openai"}, // no Lifecycle block
	}
	items := buildProviderItems(providers)
	if len(items) != 1 {
		t.Fatalf("expected 1 provider item, got %d", len(items))
	}
	item := items[0]
	if item.Name != "openai" {
		t.Errorf("expected provider name 'openai', got %q", item.Name)
	}
	// Verify at least one lifecycle field is present even though the provider
	// has no lifecycle block. The field set must include lifecycle.runtime.
	var hasLifecycleRuntime bool
	for _, f := range item.Fields {
		if f.Key() == "lifecycle.runtime" {
			hasLifecycleRuntime = true
		}
	}
	if !hasLifecycleRuntime {
		t.Errorf("expected lifecycle.runtime field for provider without lifecycle block; field keys: %v", fieldKeys(item.Fields))
	}
}

// TestBuildProviderItems_LifecyclePresent verifies that lifecycle fields are
// populated from the existing lifecycle block when present.
func TestBuildProviderItems_LifecyclePresent(t *testing.T) {
	providers := map[string]llm.ProviderConfig{
		"llama-cpp": {
			API: "openai",
			Lifecycle: &llm.RuntimeLifecycleConfig{
				Runtime:        "llama-cpp",
				AutoStart:      true,
				ModelPath:      "/models/foo.gguf",
				ModelPaths:     map[string]string{"default": "/models/foo.gguf"},
				SpawnCommand:   []string{"llama-server", "--port", "8080"},
				PIDFile:        "/tmp/llama.pid",
				SpawnTimeout:   5,
				HealthCheck:    llm.HealthCheckConfig{Endpoint: "/health", IntervalSeconds: 1},
				RestartPolicy:  llm.RestartPolicyConfig{Enabled: true, MaxAttempts: 3},
			},
		},
	}
	items := buildProviderItems(providers)
	if len(items) != 1 {
		t.Fatalf("expected 1 provider item, got %d", len(items))
	}
	item := items[0]
	// Find the lifecycle.runtime field and verify it reflects the config.
	var runtimeField *SelectField
	for _, f := range item.Fields {
		if sf, ok := f.(*SelectField); ok && f.Key() == "lifecycle.runtime" {
			runtimeField = sf
			break
		}
	}
	if runtimeField == nil {
		t.Fatalf("expected lifecycle.runtime field; field keys: %v", fieldKeys(item.Fields))
	}
	if runtimeField.Get() != "llama-cpp" {
		t.Errorf("expected lifecycle.runtime='llama-cpp', got %q", runtimeField.Get())
	}
}

// TestBuildProviderItems_LifecycleAbsentCanSave verifies the end-to-end
// behavior: when a user fills in lifecycle fields on a provider without a
// lifecycle block, saveModelsConfig creates the lifecycle block in the
// written config. This is the regression test for "add lifecycle when absent".
func TestBuildProviderItems_LifecycleAbsentCanSave(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/models.json5"

	origLoader := loadProvidersConfig
	origPath := ConfigFilePath
	t.Cleanup(func() {
		loadProvidersConfig = origLoader
		ConfigFilePath = origPath
	})

	// Provider starts with NO lifecycle block — user must be able to add one.
	loadProvidersConfig = func() (*llm.ProvidersConfig, error) {
		return &llm.ProvidersConfig{
			Providers: map[string]llm.ProviderConfig{
				"local": {API: "openai"},
			},
		}, nil
	}
	ConfigFilePath = func(name string) string { return path }

	// Simulate: TUI builder surfaces zero-value lifecycle fields → user fills
	// in a spawn_command → save round-trips it into a new lifecycle block.
	items := buildProviderItems(map[string]llm.ProviderConfig{
		"local": {API: "openai"},
	})
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	// Pull the surfaced lifecycle.spawn_command field and simulate the user
	// filling it in.
	fields := items[0].Fields
	var spawnField *TextField
	for _, f := range fields {
		if tf, ok := f.(*TextField); ok && f.Key() == "lifecycle.spawn_command" {
			spawnField = tf
		}
	}
	if spawnField == nil {
		t.Fatalf("expected lifecycle.spawn_command field surfaced for lifecycle-absent provider")
	}
	if err := spawnField.Set("llama-server --port 8080"); err != nil {
		t.Fatalf("set spawn_command: %v", err)
	}

	sm := NewDrilldownSectionModel(
		"models > providers > local", "models", "models.json5",
		"providers.local",
		fields,
	)
	if err := saveModelsConfig(sm); err != nil {
		t.Fatalf("saveModelsConfig: %v", err)
	}

	// Reload the written file and verify a lifecycle block exists with the
	// spawn_command set.
	cfg, err := llm.LoadProvidersConfig(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	provider, ok := cfg.Providers["local"]
	if !ok {
		t.Fatal("provider 'local' missing after save")
	}
	if provider.Lifecycle == nil {
		t.Fatalf("expected lifecycle block to be created on save; provider: %+v", provider)
	}
	if len(provider.Lifecycle.SpawnCommand) != 3 ||
		provider.Lifecycle.SpawnCommand[0] != "llama-server" ||
		provider.Lifecycle.SpawnCommand[1] != "--port" ||
		provider.Lifecycle.SpawnCommand[2] != "8080" {
		t.Errorf("unexpected spawn_command: %v", provider.Lifecycle.SpawnCommand)
	}
}

func fieldKeys(fields []Field) []string {
	keys := make([]string, 0, len(fields))
	for _, f := range fields {
		keys = append(keys, f.Key())
	}
	return keys
}
