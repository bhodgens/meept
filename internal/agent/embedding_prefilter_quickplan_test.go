package agent

import (
	"context"
	"fmt"
	"testing"
)

// quickplanExampleSet returns 5 identical quickplan examples on axis 2
// (mirrors exampleSet's all-identical-vector fakes) so a near-axis
// query produces a unanimous quickplan vote - the only intent gated by
// the cue guard.
func quickplanExampleSet() []map[string]any {
	ex := make([]map[string]any, 0, 5)
	for i := 0; i < 5; i++ {
		ex = append(ex, map[string]any{
			"intent": "quickplan", "agent": "orchestrator",
			"text":   fmt.Sprintf("quickplan ex %d", i),
			"vector": basisVec(2),
		})
	}
	return ex
}

// nearAxis2 returns a query vector at cos ~0.99 to axis 2 (same
// geometry as nearAxis0, different axis).
func nearAxis2() []float64 {
	return []float64{0, 0.1, 0.99, 0}
}

// TestPrefilter_QuickPlanCueGuard: a unanimous quickplan vote only
// routes directly when the input carries orchestration cue evidence.
// Identical embedding geometry - only the cue differs.
func TestPrefilter_QuickPlanCueGuard(t *testing.T) {
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return nearAxis2(), nil
	})
	p := knnPrefilter(t, emb, quickplanExampleSet(), nil)

	// WITH cue: unanimous quickplan vote + orchestration evidence ->
	// direct route.
	intent := p.Match(context.Background(), "implement tasks 3 and 4")
	if intent == nil {
		t.Fatal("cue-bearing input: expected direct route, got nil")
	}
	if intent.Type != "quickplan" {
		t.Errorf("intent = %q, want quickplan", intent.Type)
	}

	// WITHOUT cue: same geometry, but no orchestration evidence ->
	// guard rejects the vote, falls through (nil).
	if intent := p.Match(context.Background(), "please handle this"); intent != nil {
		t.Errorf("cue-less input: intent = %+v, want nil (cue guard must reject)", intent)
	}

	// Inspect the vote directly for the ok flag on both paths.
	p.mu.RLock()
	_, okCue := p.vote(nearAxis2(), "using subagents, fix them")
	_, okNoCue := p.vote(nearAxis2(), "please handle this")
	p.mu.RUnlock()
	if !okCue {
		t.Error("vote(cue-bearing input) ok = false, want true")
	}
	if okNoCue {
		t.Error("vote(cue-less input) ok = true, want false")
	}
}
