package llm

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAllAdapters_PrefersHighestVersionAndRequiresArtifacts(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	v1 := filepath.Join(tmp, "code", "lfm2.5-8b-v1")
	if err := os.MkdirAll(v1, 0o755); err != nil {
		t.Fatal(err)
	}
	v2 := filepath.Join(tmp, "code", "lfm2.5-8b-v2")
	if err := os.MkdirAll(v2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v2, "adapter_config.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	gen := filepath.Join(tmp, "general", "lfm2.5-8b-v1")
	if err := os.MkdirAll(gen, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gen, "adapter_model.safetensors"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(tmp, "code", "other-v1")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "adapter_config.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := &AdapterRegistry{
		Adapters: []AdapterEntry{
			{ID: "code-v1", Domain: "code", Model: "lfm2.5-8b", Path: v1, Enabled: true},
			{ID: "code-v2", Domain: "code", Model: "lfm2.5-8b", Path: v2, Enabled: true},
			{ID: "general-v1", Domain: "general", Model: "lfm2.5-8b", Path: gen, Enabled: true},
			{ID: "code-other", Domain: "code", Model: "lfm2.5-1.2b", Path: other, Enabled: true},
		},
	}

	loader := NewLFMLoader("lfm2.5-8b", "", slog.Default())
	if err := loader.LoadAllAdapters(reg); err != nil {
		t.Fatal(err)
	}
	if len(loader.Adapters) != 2 {
		t.Fatalf("adapters = %d, want 2 (code + general)", len(loader.Adapters))
	}
	code := loader.Adapters["code"]
	if code == nil || code.Version != 2 || !code.Ready {
		t.Fatalf("code adapter = %+v, want v2 ready", code)
	}
	if loader.Fallback == nil || loader.Fallback.Domain != "general" {
		t.Fatalf("fallback = %+v, want general", loader.Fallback)
	}

	r := NewAdapterRouterFromLoader(loader)
	if got := r.SelectAdapter("code"); got != code {
		t.Errorf("SelectAdapter(code) = %v", got)
	}
	if got := r.SelectAdapter("security"); got == nil || got.Domain != "general" {
		t.Errorf("SelectAdapter(security) fallback = %+v", got)
	}
}

func TestHasAdapterArtifacts(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	if hasAdapterArtifacts(tmp) {
		t.Error("empty dir should not have artifacts")
	}
	if err := os.WriteFile(filepath.Join(tmp, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if hasAdapterArtifacts(tmp) {
		t.Error("readme-only dir should not have artifacts")
	}
	if err := os.WriteFile(filepath.Join(tmp, "weights.safetensors"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasAdapterArtifacts(tmp) {
		t.Error("safetensors should count as artifact")
	}
}
