package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestKnowledgeToolsDisabledByDefault pins the opt-in gate
// (SHARED-CONVENTIONS §2.5): with memory.knowledge.enabled=false (the
// default) NO knowledge_* tool is registered and no knowledge store
// directory is created — nothing enters prompts and nothing touches disk.
func TestKnowledgeToolsDisabledByDefault(t *testing.T) {
	cfg, tmpDir := skillToolsTestConfig(t)
	if cfg.Memory.Knowledge.Enabled {
		t.Fatal("KnowledgeConfig.Enabled must default to false")
	}
	comps := newTranscriptWiringComponents(t, cfg)
	for _, name := range []string{"knowledge_search", "knowledge_read", "knowledge_ingest"} {
		if tool := comps.ToolRegistry.Get(name); tool != nil {
			t.Errorf("%s registered with knowledge disabled (must be absent)", name)
		}
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "knowledge")); !os.IsNotExist(err) {
		t.Errorf("knowledge data dir created while disabled: %v", err)
	}
}

// TestKnowledgeToolsEnabledRegistersAllThree verifies the enabled path:
// all three knowledge_* tools are registered with full schemas, and the
// store directory exists after registration (the store opens eagerly at
// registration when enabled).
func TestKnowledgeToolsEnabledRegistersAllThree(t *testing.T) {
	cfg, tmpDir := skillToolsTestConfig(t)
	cfg.Memory.Knowledge = config.KnowledgeConfig{
		Enabled:   true,
		IngestDir: "",
		MaxPages:  100,
	}
	comps := newTranscriptWiringComponents(t, cfg)
	for _, name := range []string{"knowledge_search", "knowledge_read", "knowledge_ingest"} {
		tool := comps.ToolRegistry.Get(name)
		if tool == nil {
			t.Errorf("%s not registered with knowledge enabled", name)
			continue
		}
		if tool.Name() != name {
			t.Errorf("registry name = %q, want %q", tool.Name(), name)
		}
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "knowledge")); err != nil {
		t.Errorf("knowledge data dir not created when enabled: %v", err)
	}
}

// TestKnowledgeToolsEnabledRoundTrip executes knowledge_ingest →
// knowledge_search → knowledge_read through the REAL registered tools to
// prove the wiring carries a live store (not nil-backed stubs).
func TestKnowledgeToolsEnabledRoundTrip(t *testing.T) {
	cfg, _ := skillToolsTestConfig(t)
	cfg.Memory.Knowledge = config.KnowledgeConfig{Enabled: true, MaxPages: 10}
	comps := newTranscriptWiringComponents(t, cfg)

	ingestRes, err := executeSkillTool(t, comps, "knowledge_ingest", map[string]any{
		"title":  "Wiring Proof",
		"body":   "the quokka migration shipped in wave two",
		"source": "user",
	})
	if err != nil {
		t.Fatalf("knowledge_ingest: %v", err)
	}
	requireSuccess(t, ingestRes, "knowledge_ingest")
	pageID, _ := ingestRes.Result.(map[string]any)["page_id"].(string)
	if pageID == "" {
		t.Fatalf("knowledge_ingest returned no page_id: %#v", ingestRes.Result)
	}

	searchRes, err := executeSkillTool(t, comps, "knowledge_search", map[string]any{"query": "quokka"})
	if err != nil {
		t.Fatalf("knowledge_search: %v", err)
	}
	requireSuccess(t, searchRes, "knowledge_search")

	readRes, err := executeSkillTool(t, comps, "knowledge_read", map[string]any{"page_id": pageID})
	if err != nil {
		t.Fatalf("knowledge_read: %v", err)
	}
	requireSuccess(t, readRes, "knowledge_read")
	title, _ := readRes.Result.(map[string]any)["title"].(string)
	if title != "Wiring Proof" {
		t.Errorf("knowledge_read title = %q, want %q", title, "Wiring Proof")
	}
}
