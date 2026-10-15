package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Decision-model plan tree, leaf 01 (docs/plans/decision-model/01-config-slot.md):
// pins the decision_model slot and every decision-feature knob end to end
// through the models.json5 load paths (config.ModelsConfig for the daemon's
// load path; llm.ProvidersConfig is pinned in internal/llm — that package
// carries its own struct-tag-driven pin).
//
// All knobs are struct-tag driven: an absent key must leave the field at its
// zero value (features disabled), and an explicit key must parse through.

// TestDefaultConfig_DecisionKnobs pins DefaultConfig() values for the
// decision knobs that live in the main Config (schema.go side): the
// compaction lane knobs. Default OFF — the existing summarizer stays the
// default compaction lane (master.md user decision 4).
func TestDefaultConfig_DecisionKnobs(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Compaction.DecisionEnabled {
		t.Fatalf("compaction.decision_enabled = true, want false (opt-in lane; summarizer stays default)")
	}
	if cfg.Compaction.DecisionKeepBar != 2 {
		t.Fatalf("compaction.decision_keep_bar = %d, want 2", cfg.Compaction.DecisionKeepBar)
	}
}

// TestLoadModelsConfig_DecisionModel pins the decision_model slot end to end
// through LoadModelsConfig: parses when set, stays empty (features disabled)
// when absent. Never defaults to small_model or classifier_model.
func TestLoadModelsConfig_DecisionModel(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "models.json5")
		content := `{
  // dedicated decision model (Jev-style typed decision endpoint)
  "model": "main/primary",
  "decision_model": "local/decision-8b",
}`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write models.json5: %v", err)
		}
		cfg, err := LoadModelsConfig(path)
		if err != nil {
			t.Fatalf("LoadModelsConfig: %v", err)
		}
		if cfg.DecisionModel != "local/decision-8b" {
			t.Fatalf("DecisionModel = %q, want %q", cfg.DecisionModel, "local/decision-8b")
		}
	})

	t.Run("empty_default", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "models.json5")
		content := `{"model": "main/primary"}`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write models.json5: %v", err)
		}
		cfg, err := LoadModelsConfig(path)
		if err != nil {
			t.Fatalf("LoadModelsConfig: %v", err)
		}
		if cfg.DecisionModel != "" {
			t.Fatalf("DecisionModel = %q, want empty (slot absent = features disabled, never small_model fallback)", cfg.DecisionModel)
		}
	})
}

// TestLoadModelsConfig_DecisionGateKnobs pins the decision-gate knobs
// (decision_gate_enabled / decision_gate_tools) through LoadModelsConfig.
func TestLoadModelsConfig_DecisionGateKnobs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json5")
	content := `{
  "model": "main/primary",
  "decision_model": "local/decision-8b",
  // opt-in irreversibility gate on destructive tool calls
  "decision_gate_enabled": true,
  "decision_gate_tools": ["shell", "file_delete"],
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}
	cfg, err := LoadModelsConfig(path)
	if err != nil {
		t.Fatalf("LoadModelsConfig: %v", err)
	}
	if !cfg.DecisionGateEnabled {
		t.Fatal("DecisionGateEnabled = false, want true")
	}
	if len(cfg.DecisionGateTools) != 2 || cfg.DecisionGateTools[0] != "shell" || cfg.DecisionGateTools[1] != "file_delete" {
		t.Fatalf("DecisionGateTools = %v, want [shell file_delete]", cfg.DecisionGateTools)
	}
}

// TestLoadModelsConfig_DecisionGateKnobsAbsent pins the zero-value defaults:
// gate off, tool list nil, threshold 0 (escalate-everything semantics live in
// the leaf 02 consumer, not the loader).
func TestLoadModelsConfig_DecisionGateKnobsAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json5")
	content := `{"model": "main/primary"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}
	cfg, err := LoadModelsConfig(path)
	if err != nil {
		t.Fatalf("LoadModelsConfig: %v", err)
	}
	if cfg.DecisionGateEnabled {
		t.Fatal("DecisionGateEnabled = true, want false (opt-in)")
	}
	if cfg.DecisionGateTools != nil {
		t.Fatalf("DecisionGateTools = %v, want nil (gate picks its own defaults)", cfg.DecisionGateTools)
	}
}

// TestLoadModelsConfig_DecisionEscalationThreshold pins the
// decision_escalation_threshold knob through LoadModelsConfig.
func TestLoadModelsConfig_DecisionEscalationThreshold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json5")
	content := `{
  "model": "main/primary",
  "decision_escalation_threshold": 0.85,
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}
	cfg, err := LoadModelsConfig(path)
	if err != nil {
		t.Fatalf("LoadModelsConfig: %v", err)
	}
	if cfg.DecisionEscalationThreshold != 0.85 {
		t.Fatalf("DecisionEscalationThreshold = %v, want 0.85", cfg.DecisionEscalationThreshold)
	}
}

// TestLoadModelsConfig_DecisionCompactionKnobs pins the compaction-section
// decision knobs when set explicitly in user config (JSON5 round trip).
func TestLoadModelsConfig_DecisionCompactionKnobs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  // decision-model-only compaction lane (opt-in)
  "compaction": {
    "decision_enabled": true,
    "decision_keep_bar": 3
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write meept.json5: %v", err)
	}
	cfg, err := LoadJSON5Config(path)
	if err != nil {
		t.Fatalf("LoadJSON5Config: %v", err)
	}
	if !cfg.Compaction.DecisionEnabled {
		t.Fatal("compaction.decision_enabled = false, want true")
	}
	if cfg.Compaction.DecisionKeepBar != 3 {
		t.Fatalf("compaction.decision_keep_bar = %d, want 3", cfg.Compaction.DecisionKeepBar)
	}
}
