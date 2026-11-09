package llm

import (
	"encoding/binary"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

const mlxAdapterConfig = `{"fine_tune_type":"lora","num_layers":1,"lora_parameters":{"rank":8,"scale":2.0,"dropout":0.0}}`

func writeMLXAdapter(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "adapter_config.json"), []byte(mlxAdapterConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	hdr := []byte(`{"lora.lora_a":{"dtype":"F32","shape":[1,1],"data_offsets":[0,4]}}`)
	buf := make([]byte, 8+len(hdr)+4)
	binary.LittleEndian.PutUint64(buf[:8], uint64(len(hdr)))
	copy(buf[8:], hdr)
	if err := os.WriteFile(filepath.Join(dir, runtimeLoRAWeights), buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAllAdapters_PrefersHighestVersionAndRequiresArtifacts(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	v1 := filepath.Join(tmp, "code", "lfm2.5-8b-v1")
	if err := os.MkdirAll(v1, 0o755); err != nil {
		t.Fatal(err)
	}
	v2 := filepath.Join(tmp, "code", "lfm2.5-8b-v2")
	writeMLXAdapter(t, v2)
	gen := filepath.Join(tmp, "general", "lfm2.5-8b-v1")
	writeMLXAdapter(t, gen)
	other := filepath.Join(tmp, "code", "other-v1")
	writeMLXAdapter(t, other)

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
	applied, ok := code.Model.(*AppliedLoRA)
	if !ok || applied == nil || len(applied.Tensors) == 0 {
		t.Fatalf("code Model = %#v, want *AppliedLoRA with tensors", code.Model)
	}
	if applied.Field != runtimeLoRAField {
		t.Fatalf("applied field = %q, want %q", applied.Field, runtimeLoRAField)
	}
	if loader.Fallback == nil || loader.Fallback.Domain != "general" || loader.Fallback.Model == nil {
		t.Fatalf("fallback = %+v, want general with weights applied", loader.Fallback)
	}

	r := NewAdapterRouterFromLoader(loader)
	if got := r.SelectAdapter("code"); got != code {
		t.Errorf("SelectAdapter(code) = %v", got)
	}
	if got := r.SelectAdapter("security"); got == nil || got.Domain != "general" {
		t.Errorf("SelectAdapter(security) fallback = %+v", got)
	}
}

func TestLoadAdapter_PEFTFilesAreNotApplied(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "adapter_config.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "adapter_model.safetensors"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	loader := NewLFMLoader("lfm2.5-8b", "", slog.Default())
	if err := loader.LoadAllAdapters(&AdapterRegistry{Adapters: []AdapterEntry{{
		ID: "peft", Domain: "code", Model: "lfm2.5-8b", Path: dir, Enabled: true,
	}}}); err != nil {
		t.Fatal(err)
	}
	if len(loader.Adapters) != 0 {
		t.Fatalf("PEFT-only dir was selected: %+v", loader.Adapters)
	}
}

func TestLoadAdapter_CorruptWeightsAreNotReady(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "adapter_config.json"), []byte(mlxAdapterConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runtimeLoRAWeights), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	loader := NewLFMLoader("lfm2.5-8b", "", slog.Default())
	err := loader.LoadAllAdapters(&AdapterRegistry{Adapters: []AdapterEntry{{
		ID: "bad", Domain: "code", Model: "lfm2.5-8b", Path: dir, Enabled: true,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loader.Adapters["code"]; ok {
		t.Fatal("corrupt adapters.safetensors must not be ready")
	}
}

func TestLoadAdapter_WireFieldMatchesRuntimeApplyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMLXAdapter(t, dir)
	loader := NewLFMLoader("lfm2.5-8b", "", slog.Default())
	if err := loader.LoadAllAdapters(&AdapterRegistry{Adapters: []AdapterEntry{{
		ID: "code-v1", Domain: "code", Model: "lfm2.5-8b", Path: dir, Enabled: true,
	}}}); err != nil {
		t.Fatal(err)
	}
	got := loader.Adapters["code"]
	applied, ok := got.Model.(*AppliedLoRA)
	if !ok || applied.Field != runtimeLoRAField {
		t.Fatalf("Model = %#v, want AppliedLoRA.Field %q", got.Model, runtimeLoRAField)
	}
	_, payload, err := buildChatRequestForTest(t, []ChatMessage{{Role: RoleUser, Content: "hi"}}, &ModelConfig{
		ModelID: "test", Temperature: 0.1, MaxTokens: 8,
	}, []ChatOption{WithAdapter(got.Path)})
	if err != nil {
		t.Fatal(err)
	}
	if payload[applied.Field] != got.Path {
		t.Fatalf("payload[%q] = %v, want %q", applied.Field, payload[applied.Field], got.Path)
	}
	if _, ok := payload["adapter_path"]; ok {
		t.Fatal("adapter_path is not the runtime apply field")
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
	if hasAdapterArtifacts(tmp) {
		t.Error("unrelated safetensors must not count as applied weights")
	}
	writeMLXAdapter(t, filepath.Join(tmp, "mlx"))
	if !hasAdapterArtifacts(filepath.Join(tmp, "mlx")) {
		t.Error("mlx adapters.safetensors + config should count")
	}
}
