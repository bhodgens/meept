package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// collectVerdicts wires a collector observer and returns a pointer to the
// accumulated slice. The prefilter runs synchronously inside Match, so no
// extra synchronization is needed for single-goroutine tests.
func collectVerdicts(p *EmbeddingPrefilter) *[]PrefilterVerdict {
	var got []PrefilterVerdict
	p.SetVerdictObserver(func(v PrefilterVerdict) {
		got = append(got, v)
	})
	return &got
}

// tempPrefilterStore writes a kNN store JSON to a temp path and returns it
// (dispatcher-level helpers can't use knnPrefilter, which builds the
// prefilter itself).
func tempPrefilterStore(t *testing.T, storeJSON string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "centroids.json")
	if err := os.WriteFile(path, []byte(storeJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// prefilterTestConfig builds an enabled prefilter config over the given
// store path (dimension 4, matching the pfInlineStore/pfCodeStore fakes).
func prefilterTestConfig(path string) config.ClassifierPrefilterConfig {
	return config.ClassifierPrefilterConfig{
		Enabled:       true,
		CentroidsPath: path,
		Dimension:     4,
	}
}

// TestPrefilterVerdictObserver_RoutedEmits: a unanimous vote routes AND
// the observer sees {Routed: true, AssertedIntent, Confidence > 0,
// Margin >= 0} -- the Door-1 verdict is no longer dropped at the boundary.
func TestPrefilterVerdictObserver_RoutedEmits(t *testing.T) {
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return nearAxis0(), nil
	})
	p := knnPrefilter(t, emb, exampleSet(), nil)
	got := collectVerdicts(p)

	intent := p.Match(context.Background(), "please write code")
	if intent == nil {
		t.Fatal("expected direct route, got nil")
	}
	if intent.Type != "code" {
		t.Errorf("intent.Type = %q, want code", intent.Type)
	}

	verdicts := *got
	if len(verdicts) != 1 {
		t.Fatalf("observer got %d verdicts, want 1", len(verdicts))
	}
	v := verdicts[0]
	if !v.Routed {
		t.Error("Routed = false, want true")
	}
	if v.AssertedIntent != "code" {
		t.Errorf("AssertedIntent = %q, want code", v.AssertedIntent)
	}
	if v.Confidence <= 0 {
		t.Errorf("Confidence = %v, want > 0", v.Confidence)
	}
	if v.Margin < 0 {
		t.Errorf("Margin = %v, want >= 0", v.Margin)
	}
	if v.Suppressed {
		t.Error("Suppressed = true on routed path, want false")
	}
}

// TestPrefilterVerdictObserver_AbstainKeepsMargin: THE point of this
// leaf. A mixed top-5 (dissenting tilted code example) abstains -- Match
// returns nil -- but the observer still gets a verdict with the kNN
// margin intact (margin = winner floor 0.89 minus best losing neighbor
// 0.80, both above the threshold). Previously the vote was discarded.
func TestPrefilterVerdictObserver_AbstainKeepsMargin(t *testing.T) {
	tiltedCode := []float64{0.9, 0.44, 0, 0} // cos ~ 0.89 to the query
	ex := exampleSet()                       // 5 code on axis 0, 5 chat on axis 1
	ex[0] = map[string]any{
		"intent": "code", "agent": "coder", "text": "tilted code ex",
		"vector": tiltedCode,
	}
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return []float64{0.6, 0.8, 0, 0}, nil // ~0.80 chat, ~0.89 tilted code
	})
	p := knnPrefilter(t, emb, ex, nil)
	got := collectVerdicts(p)

	if intent := p.Match(context.Background(), "ambiguous input"); intent != nil {
		t.Fatalf("expected abstain (nil), got %+v", intent)
	}

	verdicts := *got
	if len(verdicts) != 1 {
		t.Fatalf("observer got %d verdicts, want 1", len(verdicts))
	}
	v := verdicts[0]
	if v.Routed {
		t.Error("Routed = true on abstain, want false")
	}
	if v.Suppressed {
		t.Error("Suppressed = true on plain abstain, want false")
	}
	// Tilted code ranks first: floor ~ 0.89, best losing chat ~ 0.80.
	if v.Margin <= 0.05 || v.Margin >= 0.15 {
		t.Errorf("Margin = %v, want ~ 0.09 (0.89 floor - 0.80 losing)", v.Margin)
	}
	if v.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0 (no winning vote)", v.Confidence)
	}
}

// TestPrefilterVerdictObserver_CueSuppressedEmits: a unanimous quickplan
// vote without orchestration cues is rejected by the cue guard (which is
// NOT modified here -- only observed): Match returns nil AND the observer
// got {Routed: false, Suppressed: true} with the vote's margin.
func TestPrefilterVerdictObserver_CueSuppressedEmits(t *testing.T) {
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return nearAxis2(), nil
	})
	p := knnPrefilter(t, emb, quickplanExampleSet(), nil)
	got := collectVerdicts(p)

	if intent := p.Match(context.Background(), "please handle this"); intent != nil {
		t.Fatalf("cue-less input: intent = %+v, want nil (cue guard)", intent)
	}

	verdicts := *got
	if len(verdicts) != 1 {
		t.Fatalf("observer got %d verdicts, want 1", len(verdicts))
	}
	v := verdicts[0]
	if v.Routed {
		t.Error("Routed = true on cue-guard suppression, want false")
	}
	if !v.Suppressed {
		t.Error("Suppressed = false on cue-guard suppression, want true")
	}
	if v.AssertedIntent != "quickplan" {
		t.Errorf("AssertedIntent = %q, want quickplan (the guarded vote)", v.AssertedIntent)
	}
	if v.Margin < 0 {
		t.Errorf("Margin = %v, want >= 0", v.Margin)
	}
}

// TestPrefilterVerdictObserver_EmptyIndexEmits: missing store -> prefilter
// inert. Match returns nil immediately AND the observer got
// {Routed: false, AssertedIntent: ""}.
func TestPrefilterVerdictObserver_EmptyIndexEmits(t *testing.T) {
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return nearAxis0(), nil
	})
	// knnPrefilter writes a store; point the prefilter at a nonexistent
	// path instead so loadIndex fails (the "empty index" path).
	p := knnPrefilter(t, emb, exampleSet(), nil)
	p.mu.Lock()
	p.path = p.path + ".missing"
	p.mu.Unlock()
	got := collectVerdicts(p)

	if intent := p.Match(context.Background(), "anything"); intent != nil {
		t.Fatalf("missing store: intent = %+v, want nil", intent)
	}

	verdicts := *got
	if len(verdicts) != 1 {
		t.Fatalf("observer got %d verdicts, want 1", len(verdicts))
	}
	v := verdicts[0]
	if v.Routed || v.Suppressed {
		t.Errorf("Routed/Suppressed = %v/%v, want false/false", v.Routed, v.Suppressed)
	}
	if v.AssertedIntent != "" {
		t.Errorf("AssertedIntent = %q, want empty (index empty)", v.AssertedIntent)
	}
}

// TestPrefilterVerdictObserver_NilObserverNoPanic: no observer wired ->
// zero behavior change: Match behaves exactly as before on both a routed
// and an abstaining input.
func TestPrefilterVerdictObserver_NilObserverNoPanic(t *testing.T) {
	// Input-keyed embedder: axis-0 vote for the routing input, empty
	// neighborhood for the abstaining input.
	emb := embedFunc(func(_ context.Context, text string) ([]float64, error) {
		if text == "please write code" {
			return nearAxis0(), nil
		}
		return basisVec(3), nil // out of distribution: no neighbors
	})
	p := knnPrefilter(t, emb, exampleSet(), nil)

	// Explicit nil must not panic (nil-guarded setter contract).
	p.SetVerdictObserver(nil)
	// A typed-nil func must also be ignored (typed-nil guard).
	var nilFn func(PrefilterVerdict)
	p.SetVerdictObserver(nilFn)

	intent := p.Match(context.Background(), "please write code")
	if intent == nil || intent.Type != "code" {
		t.Fatalf("routed Match with nil observer: got %+v, want code intent", intent)
	}
	if intent.Method != prefilterMethod {
		t.Errorf("method = %q, want %q", intent.Method, prefilterMethod)
	}
	if intent := p.Match(context.Background(), "out of distribution"); intent != nil {
		t.Errorf("abstain Match with nil observer: got %+v, want nil", intent)
	}
}

// TestDispatcher_PrefilterMarginPersistedRoutedAndAbstained: dispatcher
// persists DispatchEntry.Margin non-nil for BOTH Door-1 shapes -- a direct
// route (margin > 0) and an abstain that still carried a vote (the rows
// this leaf exists to harvest). The abstained row keeps the chain's
// intent_type; its margin survives.
func TestDispatcher_PrefilterMarginPersistedRoutedAndAbstained(t *testing.T) {
	d, store := newPrivacyTestDispatcher(t)
	path := tempPrefilterStore(t, pfInlineStore)
	// Enable the prefilter on the privacy-test dispatcher: fake embedder
	// pinned to axis 1 (chat examples), observer wired by hand because
	// this dispatcher was built via NewDispatcher without PrefilterConfig.
	pref := NewEmbeddingPrefilter(
		embedFunc(func(_ context.Context, _ string) ([]float64, error) {
			return []float64{0.1, 0.99, 0, 0}, nil
		}),
		prefilterTestConfig(path),
		testLogger(),
	)
	d.prefilter = pref
	pref.SetVerdictObserver(d.stashPrefilterVerdict)
	d.SetInputHasher(func(message string) string { return "0123456789abcdef" })

	// 1) Direct route: unanimous chat vote -> method=embedding_prefilter.
	res, err := d.ClassifyAndRoute(context.Background(),
		"hello there friend how are you doing today", "sess-margin-1", nil, "")
	if err != nil {
		t.Fatalf("routed ClassifyAndRoute: %v", err)
	}
	// recordDispatch is invoked by the HANDLER after ClassifyAndRoute
	// returns (handler.go's dispatch switch), not inside it -- mirror that
	// call order exactly so the stash is consumed at the persist site.
	d.recordDispatch("sess-margin-1", "route_to_agent", "hello", res, false, nil)
	// 2) Abstain: query far from every example -- no vote, full chain
	// (no classifier wired -> method stays empty on the row). The sparse
	// abstention still emits a verdict (margin 0 -- no vote was attempted),
	// so the row gets a non-nil margin: the observer->stash->persist path
	// survives pi == nil.
	emb2 := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return []float64{0.99, 0.1, 0, 0}, nil // pure axis 0, index is chat-only: sparse
	})
	pref.mu.Lock()
	pref.embedder = emb2
	pref.mu.Unlock()

	res2, err := d.ClassifyAndRoute(context.Background(),
		"something out of distribution entirely", "sess-margin-1", nil, "")
	if err != nil {
		t.Fatalf("abstained ClassifyAndRoute: %v", err)
	}
	d.recordDispatch("sess-margin-1", "route_to_agent", "ood input", res2, false, nil)

	rows, err := store.QueryDispatchLogBySession("sess-margin-1", 10)
	if err != nil {
		t.Fatalf("QueryDispatchLogBySession: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	// ORDER BY id DESC: rows[0] = abstain, rows[1] = routed.
	abstainRow, routedRow := rows[0], rows[1]

	if routedRow.ClassifierMethod != "embedding_prefilter" {
		t.Errorf("routed row method = %q, want embedding_prefilter", routedRow.ClassifierMethod)
	}
	if routedRow.Margin == nil {
		t.Fatal("routed row margin = NULL, want non-nil (Door-1 direct route)")
	}
	if *routedRow.Margin < 0 {
		t.Errorf("routed row margin = %v, want >= 0", *routedRow.Margin)
	}

	// THE POINT: the abstained dispatch persists a non-nil margin. This
	// dispatch's vote was attempted (embedder ran, kNN vote executed and
	// found no neighbors) -> verdict margin 0, but PRESENT, proving the
	// observer->stash->recordDispatch path survives pi == nil.
	if abstainRow.Margin == nil {
		t.Fatal("abstained row margin = NULL, want non-nil (Door-1 abstain)")
	}
	if *abstainRow.Margin != 0 {
		t.Errorf("abstained row margin = %v, want 0 (no neighbors in chat-only index)", *abstainRow.Margin)
	}
	// The abstained row's intent_type comes from the chain (not the
	// prefilter) -- here no classifier is wired, so it must not carry an
	// embedding_prefilter method.
	if abstainRow.ClassifierMethod == "embedding_prefilter" {
		t.Errorf("abstained row method = %q, want the chain's method (chain produced none here)",
			abstainRow.ClassifierMethod)
	}
}

// TestDispatcher_PrefilterCueSuppressedMarginPersisted: the cue guard
// fires in Match; the dispatcher-suppression paths (H6 gates) mark
// Suppressed and the row STILL persists the margin -- the verdict survives
// gate suppression too.
func TestDispatcher_PrefilterCueSuppressedMarginPersisted(t *testing.T) {
	d, store := newPrivacyTestDispatcher(t)
	path := tempPrefilterStore(t, pfCodeStore)
	pref := NewEmbeddingPrefilter(
		embedFunc(func(_ context.Context, _ string) ([]float64, error) {
			return []float64{0.99, 0.1, 0, 0}, nil // code vote -> H6 gate suppresses
		}),
		prefilterTestConfig(path),
		testLogger(),
	)
	d.prefilter = pref
	pref.SetVerdictObserver(d.stashPrefilterVerdict)
	d.SetInputHasher(func(message string) string { return "0123456789abcdef" })

	if _, err := d.ClassifyAndRoute(context.Background(),
		"please write some code for me", "sess-margin-2", nil, ""); err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	// Mirror the handler: recordDispatch runs after ClassifyAndRoute
	// returns and is where the verdict is consumed.
	d.recordDispatch("sess-margin-2", "route_to_agent", "code request", nil, false, nil)

	rows, err := store.QueryDispatchLogBySession("sess-margin-2", 10)
	if err != nil {
		t.Fatalf("QueryDispatchLogBySession: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Margin == nil {
		t.Fatal("suppressed row margin = NULL, want non-nil (Door-1 verdict observed)")
	}
}
