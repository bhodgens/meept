package configui

import (
	"os"
	"strings"
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
				Runtime:       "llama-cpp",
				AutoStart:     true,
				ModelPath:     "/models/foo.gguf",
				ModelPaths:    map[string]string{"default": "/models/foo.gguf"},
				SpawnCommand:  []string{"llama-server", "--port", "8080"},
				PIDFile:       "/tmp/llama.pid",
				SpawnTimeout:  5,
				HealthCheck:   llm.HealthCheckConfig{Endpoint: "/health", IntervalSeconds: 1},
				RestartPolicy: llm.RestartPolicyConfig{Enabled: true, MaxAttempts: 3},
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

// TestBuildProviderItems_AutoStopOnExitMatchesProviderAccessor pins F82
// (bughunt 2026-09-12 wave): the lifecycle toggle rendered
// lc.AutoStopOnExitOrDefault() on a SUBSTITUTED zero-value block, so every
// provider with no lifecycle block (openai/anthropic/…) displayed
// "auto stop on exit: on" — the opposite of ProviderConfig.IsAutoStopOnExit()
// (false), the accessor added by the same commit. An operator toggling the
// displayed value off marked the field dirty and save.go then materialized a
// brand-new lifecycle block for a provider that manages no runtime.
func TestBuildProviderItems_AutoStopOnExitMatchesProviderAccessor(t *testing.T) {
	tru, fal := true, false
	cases := []struct {
		name     string
		provider llm.ProviderConfig
		want     bool
	}{
		{
			name:     "no lifecycle block renders off",
			provider: llm.ProviderConfig{API: "openai"},
			want:     false,
		},
		{
			name:     "existing block with absent key still means on",
			provider: llm.ProviderConfig{API: "openai", Lifecycle: &llm.RuntimeLifecycleConfig{Runtime: "llama-cpp"}},
			want:     true,
		},
		{
			name:     "explicit false renders off",
			provider: llm.ProviderConfig{API: "openai", Lifecycle: &llm.RuntimeLifecycleConfig{AutoStopOnExit: &fal}},
			want:     false,
		},
		{
			name:     "explicit true renders on",
			provider: llm.ProviderConfig{API: "openai", Lifecycle: &llm.RuntimeLifecycleConfig{AutoStopOnExit: &tru}},
			want:     true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := buildProviderItems(map[string]llm.ProviderConfig{"p": tc.provider})
			if len(items) != 1 {
				t.Fatalf("expected 1 provider item, got %d", len(items))
			}
			toggle, ok := findField(items[0].Fields, "lifecycle.auto_stop_on_exit").(*ToggleField)
			if !ok {
				t.Fatalf("expected a ToggleField for lifecycle.auto_stop_on_exit; field keys: %v", fieldKeys(items[0].Fields))
			}
			wantStr := formatBool(tc.want)
			if got := toggle.Get(); got != wantStr {
				t.Errorf("rendered auto_stop_on_exit = %q, want %q", got, wantStr)
			}
			// The rendered value must equal the value the daemon acts on.
			if got := toggle.Get(); got != formatBool(tc.provider.IsAutoStopOnExit()) {
				t.Errorf("rendered %q but ProviderConfig.IsAutoStopOnExit() = %v", got, tc.provider.IsAutoStopOnExit())
			}
		})
	}
}

// findField returns the field with the given key, or nil.
func findField(fields []Field, key string) Field {
	for _, f := range fields {
		if f.Key() == key {
			return f
		}
	}
	return nil
}

// TestLifecycleAutoStopOnExitToggleDefaultsTrue verifies the config editor
// shows the effective value: an absent or null auto_stop_on_exit key means
// true, and only an explicit false shows up as off.
func TestLifecycleAutoStopOnExitToggleDefaultsTrue(t *testing.T) {
	cases := []struct {
		name string
		ptr  *bool
		want string
	}{
		{"absent key", nil, "true"},
		{"explicit true", new(true), "true"},
		{"explicit false", new(false), "false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			providers := map[string]llm.ProviderConfig{
				"local": {
					API: "openai",
					Lifecycle: &llm.RuntimeLifecycleConfig{
						Runtime:        "llama-cpp",
						AutoStopOnExit: tc.ptr,
					},
				},
			}
			items := buildProviderItems(providers)
			if len(items) != 1 {
				t.Fatalf("expected 1 provider item, got %d", len(items))
			}
			f := findField(items[0].Fields, "lifecycle.auto_stop_on_exit")
			if f == nil {
				t.Fatalf("expected lifecycle.auto_stop_on_exit field; field keys: %v", fieldKeys(items[0].Fields))
			}
			if got := f.Get(); got != tc.want {
				t.Errorf("auto stop on exit toggle = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSaveModelsConfigDrilldownAutoStopOnExitRoundTrip verifies an explicit
// toggle value survives a save: the *bool is written explicitly (never dropped
// to an absent key) and an explicit false still opts out of the shutdown path.
func TestSaveModelsConfigDrilldownAutoStopOnExitRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		initial *bool
		set     string
		want    bool
	}{
		{"toggle off from absent key", nil, "false", false},
		{"toggle on from explicit false", new(false), "true", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := dir + "/models.json5"

			origLoader := loadProvidersConfig
			origPath := ConfigFilePath
			t.Cleanup(func() {
				loadProvidersConfig = origLoader
				ConfigFilePath = origPath
			})

			provider := llm.ProviderConfig{
				API: "openai",
				Lifecycle: &llm.RuntimeLifecycleConfig{
					Runtime:        "llama-cpp",
					AutoStopOnExit: tc.initial,
				},
			}
			loadProvidersConfig = func() (*llm.ProvidersConfig, error) {
				return &llm.ProvidersConfig{
					Providers: map[string]llm.ProviderConfig{"local": provider},
				}, nil
			}
			ConfigFilePath = func(name string) string { return path }

			items := buildProviderItems(map[string]llm.ProviderConfig{"local": provider})
			if len(items) != 1 {
				t.Fatalf("expected 1 provider item, got %d", len(items))
			}
			toggle, ok := findField(items[0].Fields, "lifecycle.auto_stop_on_exit").(*ToggleField)
			if !ok {
				t.Fatalf("expected a ToggleField for lifecycle.auto_stop_on_exit; field keys: %v", fieldKeys(items[0].Fields))
			}
			if err := toggle.Set(tc.set); err != nil {
				t.Fatalf("set auto stop on exit: %v", err)
			}

			sm := NewDrilldownSectionModel(
				"models > providers > local", "models", "models.json5",
				"providers.local",
				items[0].Fields,
			)
			if err := saveModelsConfig(sm); err != nil {
				t.Fatalf("saveModelsConfig: %v", err)
			}

			// The value must be written explicitly, not dropped to an absent key.
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read saved models.json5: %v", err)
			}
			if !strings.Contains(string(raw), `"auto_stop_on_exit": `+tc.set) {
				t.Errorf("saved config does not carry an explicit auto_stop_on_exit=%s: %s", tc.set, raw)
			}

			cfg, err := llm.LoadProvidersConfig(path)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			saved, ok := cfg.Providers["local"]
			if !ok {
				t.Fatal("provider 'local' missing after save")
			}
			if saved.Lifecycle == nil {
				t.Fatal("lifecycle block missing after save")
			}
			if saved.Lifecycle.AutoStopOnExit == nil {
				t.Fatal("auto_stop_on_exit written as absent; an explicit value must round-trip")
			}
			if got := saved.IsAutoStopOnExit(); got != tc.want {
				t.Errorf("IsAutoStopOnExit() after save = %v, want %v", got, tc.want)
			}
		})
	}
}
