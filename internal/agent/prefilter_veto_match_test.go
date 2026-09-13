package agent

// Pin for bughunt-2026-09-12 F96: the original tfidf-veto test never called
// Match — it exercised p.veto.agrees directly, so deleting the veto
// fallthrough block in embedding_prefilter.go (the `if !p.veto.agrees(...)
// { suppress }` branch) left the suite green. These tests drive the real
// Match path end to end: a unanimous kNN vote that the veto contradicts must
// fall through (nil) with a Suppressed verdict, and the same index WITHOUT a
// veto must route — proving the suppression is the veto's doing.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// vetoTestEmbedder returns a fixed vector for every text so the kNN vote is
// deterministic. [1, 0.5] against the index's [1, 0] gives cosine ~0.894:
// above the 0.70 threshold and below the 0.999 self-match cutoff, so the one
// example counts as one vote.
type vetoTestEmbedder struct{ vec []float64 }

func (e vetoTestEmbedder) Embed(_ context.Context, _ string) ([]float64, error) {
	out := make([]float64, len(e.vec))
	copy(out, e.vec)
	return out, nil
}

// newVetoTestPrefilter builds a prefilter over a one-example "code" index
// with k=1, optionally carrying a single-class "chat" veto model that
// necessarily disagrees with the code vote.
func newVetoTestPrefilter(t *testing.T, withVeto bool) (*EmbeddingPrefilter, *[]PrefilterVerdict) {
	t.Helper()
	dir := t.TempDir()

	centroids := filepath.Join(dir, "centroids.json")
	const storeDoc = `{"model":"test","dimension":2,"built_at":"t","corpus":"test",
		"examples":[{"intent":"code","agent":"coder","vector":[1.0,0.0]}]}`
	if err := os.WriteFile(centroids, []byte(storeDoc), 0o600); err != nil {
		t.Fatal(err)
	}

	p := &EmbeddingPrefilter{
		embedder:  vetoTestEmbedder{vec: []float64{1.0, 0.5}},
		threshold: DefaultPrefilterThreshold,
		k:         1,
		dimension: 2,
		path:      centroids,
		logger:    testLogger(),
	}

	if withVeto {
		model := filepath.Join(dir, "veto.json")
		// Single class "chat": its argmax can never be "code".
		const vetoDoc = `{"vocab":{"zz":0},"idf":[1.0],"classes":["chat"],
			"coef":[[0.5]],"intercept":[0.0],
			"ngram_range":[2,4],"char_wb":true,"built_at":"t",
			"train_docs":1,"train_accuracy":1.0}`
		if err := os.WriteFile(model, []byte(vetoDoc), 0o600); err != nil {
			t.Fatal(err)
		}
		v, err := loadTfidfVeto(model)
		if err != nil {
			t.Fatalf("loadTfidfVeto: %v", err)
		}
		if v == nil {
			t.Fatal("veto model failed to load")
		}
		p.veto = v
	}

	var verdicts []PrefilterVerdict
	p.SetVerdictObserver(func(v PrefilterVerdict) { verdicts = append(verdicts, v) })
	return p, &verdicts
}

// TestPrefilter_MatchVetoDisagreementSuppresses drives Match with a
// conflicting veto: the unanimous code vote must NOT route; Match returns nil
// and the verdict is Suppressed (not merely abstained) with the knn intent
// recorded.
func TestPrefilter_MatchVetoDisagreementSuppresses(t *testing.T) {
	p, verdicts := newVetoTestPrefilter(t, true)

	got := p.Match(context.Background(), "please implement the parser change")
	if got != nil {
		t.Fatalf("Match routed %+v despite a veto disagreement; the fallthrough block is gone (F96)", got)
	}
	if len(*verdicts) != 1 {
		t.Fatalf("verdict observer calls = %d, want 1", len(*verdicts))
	}
	v := (*verdicts)[0]
	if !v.Suppressed {
		t.Errorf("verdict.Suppressed = false, want true (veto suppression must be observable, F96)")
	}
	if v.Routed {
		t.Error("verdict.Routed = true on a suppressed Match")
	}
	if v.AssertedIntent != "code" {
		t.Errorf("verdict.AssertedIntent = %q, want the kNN winner \"code\"", v.AssertedIntent)
	}
}

// TestPrefilter_MatchWithoutVetoRoutes is the control: the identical index
// and embedder, with no veto configured, routes normally. Without this the
// suppression test could pass for the wrong reason (a broken vote).
func TestPrefilter_MatchWithoutVetoRoutes(t *testing.T) {
	p, verdicts := newVetoTestPrefilter(t, false)

	got := p.Match(context.Background(), "please implement the parser change")
	if got == nil {
		t.Fatal("Match returned nil with no veto; the kNN vote itself is broken, so the suppression test proves nothing")
	}
	if got.Type != "code" {
		t.Errorf("routed intent = %q, want code", got.Type)
	}
	if got.AgentType != "coder" {
		t.Errorf("routed agent = %q, want coder", got.AgentType)
	}
	if len(*verdicts) != 1 || !(*verdicts)[0].Routed {
		t.Errorf("expected one routed verdict, got %+v", *verdicts)
	}
}
