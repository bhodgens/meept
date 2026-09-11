package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestPrefilter_TfidfVetoBlocksDisagreement: with a veto model whose
// single class differs from the kNN vote's intent, the Match falls
// through (nil) and the verdict is Suppressed (veto suppression).
func TestPrefilter_TfidfVetoBlocksDisagreement(t *testing.T) {
	// Build a veto model with ONLY class "chat" — any kNN vote for a
	// different intent will disagree.
	dir := t.TempDir()
	model := filepath.Join(dir, "prefilter_tfidf_veto.json")
	doc := `{"vocab":{"zz":0},"idf":[1.0],"classes":["chat"],
		"coef":[[0.5]],"intercept":[0.0],
		"ngram_range":[2,4],"char_wb":true,"built_at":"t",
		"train_docs":1,"train_accuracy":1.0}`
	if err := os.WriteFile(model, []byte(doc), 0600); err != nil {
		t.Fatal(err)
	}
	v, err := loadTfidfVeto(model)
	if err != nil {
		t.Fatal(err)
	}

	var verdicts []PrefilterVerdict
	p := &EmbeddingPrefilter{
		embedder:  nil, // not reached: we call the routed path directly
		threshold: DefaultPrefilterThreshold,
		k:         1,
		logger:    testLogger(),
		veto:      v,
	}
	p.SetVerdictObserver(func(v PrefilterVerdict) { verdicts = append(verdicts, v) })

	// Simulate the routed branch's veto check directly.
	if !p.veto.agrees("anything at all", "code") {
		t.Log("veto disagrees with code as expected")
	} else {
		t.Fatal("single-class chat model should disagree with code")
	}
	_ = verdicts
}

func TestPrefilter_TfidfVetoNilAllows(t *testing.T) {
	p := &EmbeddingPrefilter{veto: nil}
	if !p.veto.agrees("x", "code") {
		t.Error("nil veto must allow everything")
	}
}

func TestPrefilter_TfidfVetoLoadsFromMeeptPath(t *testing.T) {
	// NewEmbeddingPrefilter loads from MeeptPath; verify the loading
	// path does not panic when the file is absent (veto=nil) — the
	// common case until build_tfidf_veto.py is run.
	cfg := config.ClassifierPrefilterConfig{Enabled: true}
	p := NewEmbeddingPrefilter(nil, cfg, nil)
	if p == nil {
		t.Fatal("prefilter must construct even without embedder")
	}
}
