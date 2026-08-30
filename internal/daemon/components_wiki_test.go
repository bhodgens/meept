package daemon

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/selfimprove"
	"github.com/caimlas/meept/internal/skills/lifecycle"
)

// wikiTestPattern builds a minimal valid LearnedPattern for wiki wiring tests.
func wikiTestPattern(id, contentHash string) *selfimprove.LearnedPattern {
	now := time.Now()
	return &selfimprove.LearnedPattern{
		ID:          id,
		Type:        selfimprove.PatternTypeHeuristic,
		Status:      selfimprove.PatternStatusActive,
		Domain:      "testing",
		Description: "wiring test pattern",
		Pattern:     "prefer the race detector on daemon tests",
		Confidence:  0.9,
		UseCount:    1,
		CreatedAt:   now,
		UpdatedAt:   now,
		ContentHash: contentHash,
	}
}

// TestWireSkillKnowledgeStores_OffLeavesNil verifies the disabled path: with
// skills.wiki.enabled=false, wireSkillKnowledgeStores returns nil stores and
// creates no directory (05-config-wiring.md Task 2).
func TestWireSkillKnowledgeStores_OffLeavesNil(t *testing.T) {
	tmp := t.TempDir()
	absWiki := filepath.Join(tmp, "wiki-should-not-exist")
	cfg := config.DefaultConfig()
	cfg.Skills.Wiki.Enabled = false
	cfg.Skills.Wiki.Dir = absWiki

	ts, ws := wireSkillKnowledgeStores(cfg, nil, slog.Default())
	if ts != nil || ws != nil {
		t.Fatalf("disabled wiki must yield nil stores, got ts=%v ws=%v", ts, ws)
	}
	if _, err := os.Stat(absWiki); !os.IsNotExist(err) {
		t.Fatalf("disabled wiki must not create %s (stat err: %v)", absWiki, err)
	}
}

// TestWireSkillKnowledgeStores_OnCreatesStoresAndSetWikiStore verifies the
// enabled path: non-nil stores rooted at the config dir, the directory
// created, and the wiki store handed to the learning pipeline via
// SetWikiStore (asserted by the pipeline's observable behavior: a stored
// pattern is persisted into the wired wiki store's patterns directory).
func TestWireSkillKnowledgeStores_OnCreatesStoresAndSetWikiStore(t *testing.T) {
	tmp := t.TempDir()
	absWiki := filepath.Join(tmp, "wiki")
	cfg := config.DefaultConfig()
	cfg.Skills.Wiki.Enabled = true
	cfg.Skills.Wiki.Dir = absWiki

	lp := selfimprove.NewLearningPipeline(
		selfimprove.DefaultLearningConfig(),
		nil, // llmClient — not needed for wiki wiring
		filepath.Join(tmp, "learning"),
		slog.Default(),
	)

	ts, ws := wireSkillKnowledgeStores(cfg, lp, slog.Default())
	if ts == nil || ws == nil {
		t.Fatalf("enabled wiki must yield non-nil stores, got ts=%v ws=%v", ts, ws)
	}
	if st, err := os.Stat(absWiki); err != nil || !st.IsDir() {
		t.Fatalf("enabled wiki must create dir %s (err: %v)", absWiki, err)
	}

	// Observable SetWikiStore effect: a pattern stored on the initialized
	// pipeline must land in the wired wiki store's patterns dir.
	if err := lp.Initialize(t.Context()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	pattern := wikiTestPattern("wiki-wiring-test-1", strings.Repeat("a", 16))
	if err := lp.StorePattern(t.Context(), pattern); err != nil {
		t.Fatalf("StorePattern: %v", err)
	}
	loaded, err := ws.LoadPatterns()
	if err != nil {
		t.Fatalf("LoadPatterns: %v", err)
	}
	found := false
	for _, p := range loaded {
		if p != nil && p.ID == pattern.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("pattern %s not persisted through wired wiki store (got %d patterns)", pattern.ID, len(loaded))
	}
}

// TestWireSkillKnowledgeStores_TildeExpansion verifies that a "~"-prefixed
// wiki dir (the default) resolves under the user's home directory, using the
// store's observable write behavior rather than unexported fields.
func TestWireSkillKnowledgeStores_TildeExpansion(t *testing.T) {
	cfg := config.DefaultConfig() // wiki enabled, dir "~/.meept/wiki"
	_, ws := wireSkillKnowledgeStores(cfg, nil, slog.Default())
	if ws == nil {
		t.Fatal("default config must wire the wiki store")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	pattern := wikiTestPattern("tilde-expansion-check", strings.Repeat("b", 16))
	if _, err := ws.UpsertPattern(pattern); err != nil {
		t.Fatalf("UpsertPattern: %v", err)
	}
	wantDir := filepath.Join(home, ".meept", "wiki", "patterns")
	if _, err := os.Stat(wantDir); err != nil {
		t.Fatalf("expected pattern dir under expanded home path %s: %v", wantDir, err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(filepath.Join(home, ".meept", "wiki")); err != nil {
			t.Logf("cleanup: remove default wiki dir: %v", err)
		}
	})
}

// TestTraceStoreProvider_SampleDelegation verifies the 5-line adapter
// satisfies lifecycle.TraceProvider by delegating to TraceStore.Sample
// pass-through (empty store ⇒ zero records, no error).
func TestTraceStoreProvider_SampleDelegation(t *testing.T) {
	ts := selfimprove.NewTraceStore(t.TempDir(), slog.Default())
	var tp lifecycle.TraceProvider = &traceStoreProvider{ts: ts}
	recs, err := tp.Sample(2, 1, 100)
	if err != nil {
		t.Fatalf("Sample on empty store: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("empty store must sample zero records, got %d", len(recs))
	}
}
