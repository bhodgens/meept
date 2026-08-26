package llm_test

import (
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func TestRegisterLocalModel_WiresLlamaCppConfig(t *testing.T) {
	mgr := llm.NewRuntimeManager(slog.Default())

	modelPath := createTempModelFile(t)
	rec := llm.ModelRecord{
		Name:   "org/repo-m-q4_k_m",
		RepoID: "org/repo",
		File:   modelPath,
	}

	if err := mgr.RegisterLocalModel(rec); err != nil {
		t.Fatalf("RegisterLocalModel: %v", err)
	}

	statuses := mgr.Status()
	if len(statuses) != 1 {
		t.Fatalf("want 1 registered provider, got %d", len(statuses))
	}
	st := statuses[0]
	if st.ProviderID != llm.LocalModelsProviderID {
		t.Errorf("ProviderID = %q, want %q", st.ProviderID, llm.LocalModelsProviderID)
	}
	if st.Runtime != string(llm.RuntimeLlamaCpp) {
		t.Errorf("Runtime = %q, want llama-cpp", st.Runtime)
	}
	if st.ModelPath != modelPath {
		t.Errorf("ModelPath = %q, want %q", st.ModelPath, modelPath)
	}
}

func TestRegisterLocalModel_MergesIntoSameEndpoint(t *testing.T) {
	mgr := llm.NewRuntimeManager(slog.Default())
	rec1 := llm.ModelRecord{Name: "a/m1", File: createTempModelFile(t)}
	rec2 := llm.ModelRecord{Name: "b/m2", File: createTempModelFile(t)}

	if err := mgr.RegisterLocalModel(rec1); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RegisterLocalModel(rec2); err != nil {
		t.Fatal(err)
	}

	statuses := mgr.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected merge into single endpoint provider, got %d providers", len(statuses))
	}
	if len(statuses[0].InUseModels) < 2 {
		t.Errorf("expected both models merged; got %v", statuses[0].InUseModels)
	}
}

func TestRegisterLocalModel_MissingFile(t *testing.T) {
	mgr := llm.NewRuntimeManager(slog.Default())
	err := mgr.RegisterLocalModel(llm.ModelRecord{Name: "x/y", File: "/nonexistent/model.gguf"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	err2 := mgr.RegisterLocalModel(llm.ModelRecord{Name: "x/y"})
	if err2 == nil {
		t.Fatal("expected error for empty file path")
	}
}
