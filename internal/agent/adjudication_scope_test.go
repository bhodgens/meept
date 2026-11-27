package agent

// Fixwave 2026-11-27 bughunt A-7 (L3) pin: the approval-adjudication
// side-send (adjudicateApproval) resolves the session scope exactly like
// the primary analysis call — the session-affinity header the primary
// carried must ride on the adjudication request too (empty scope sends no
// header; a wired/threaded scope sends the SAME session id).
//
// Observable through the REAL client on the scopeHeaderServer harness
// (scope_threading_test.go): WithTaskScope's session id lands in the
// ${session_id} extra header, so the header recorded per request proves
// which scope each call carried. The canned-response lane rides the SAME
// server so primary/adjudication slots line up (mirrors
// approval_adjudication_test.go's sequencedServer).

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// scopedSequencedServer records every request body (slot order: primary
// call first, adjudication second) and replies with canned contents.
type scopedSequencedServer struct {
	Server *httptest.Server

	mu     sync.Mutex
	bodies [][]byte
	respon []string
}

// newScopedSequencedServer starts the canned server and returns an
// analyzer on the GIVEN client (which points at the scope-header server),
// retargeted to the canned server — the two harnesses share one endpoint
// so headers AND slot contents are observable on the same requests.
//
// Actually simpler and equally exact: one server does both jobs — it
// records the request's scope header AND replies with the canned body.
func newScopedSequencedServer(t *testing.T, responses ...string) (*scopedSequencedServer, *IntentAnalyzer, *scopeHeaderLog) {
	t.Helper()
	log := &scopeHeaderLog{}
	const headerName = "X-Meept-Scope-Session"
	ss := &scopedSequencedServer{respon: responses}
	ss.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		log.record(r.Header.Get(headerName))
		ss.mu.Lock()
		idx := len(ss.bodies)
		ss.bodies = append(ss.bodies, body)
		ss.mu.Unlock()
		content := responses[len(responses)-1]
		if idx < len(responses) {
			content = responses[idx]
		}
		w.Header().Set("Content-Type", "application/json")
		resp := `{"choices":[{"message":{"role":"assistant","content":` + strconv.Quote(content) + `}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(ss.Server.Close)
	client := llm.NewClient(&llm.ModelConfig{
		BaseURL:      ss.Server.URL,
		ModelID:      "scope-model",
		ProviderID:   llm.ProviderIDOpenAI,
		ExtraHeaders: map[string]string{headerName: "${session_id}"},
	})
	ia := newIntentAnalyzerWithConfig(IntentAnalyzerConfig{}, client, digestTestLogger())
	return ss, ia, log
}

// TestAdjudicateApprovalCarriesSessionScope pins the L3 fix: with a scope
// threaded via WithScopeOptions (the per-call seam the primary uses), BOTH
// the primary analysis call and the fallback adjudication call carry the
// same session-affinity scope.
func TestAdjudicateApprovalCarriesSessionScope(t *testing.T) {
	_, ia, log := newScopedSequencedServer(t,
		`{"goal":"g","ambiguity":0.1,"scope":"medium","category":"quickplan","suggested_questions":[],"confidence":0.9,"suggested_mode":"quick_plan","approval_requested":false}`,
		`{"approval_required":true}`)
	loop := scopedLoop(t, "sess-adjudication-scope")
	ctx := WithScopeOptions(context.Background(), loop.scopeOptions())

	// Primary vote false + veto-suspect phrasing fires the adjudication.
	got, err := ia.AnalyzeTrueIntent(ctx, "plan the rollout, hold off coding until I sign off", nil)
	if err != nil {
		t.Fatalf("AnalyzeTrueIntent: %v", err)
	}
	if !got.ApprovalRequested {
		t.Fatal("adjudication did not override the degraded false vote; fallback never ran")
	}
	headers := log.all()
	if len(headers) < 2 {
		t.Fatalf("expected 2 LLM calls (primary + adjudication), saw %d", len(headers))
	}
	if headers[0] != "sess-adjudication-scope" {
		t.Errorf("primary call header = %q, want sess-adjudication-scope", headers[0])
	}
	if headers[1] != "sess-adjudication-scope" {
		t.Errorf("adjudication call header = %q, want sess-adjudication-scope (L3: side-send skipped the session scope)", headers[1])
	}
}

// TestAdjudicateApprovalUnscopedStaysUnscoped pins the negative side: with
// no scope anywhere, the adjudication sends NO header — no placeholder
// session id is invented (the affinity contract's empty-scope rule).
func TestAdjudicateApprovalUnscopedStaysUnscoped(t *testing.T) {
	_, ia, log := newScopedSequencedServer(t,
		`{"goal":"g","ambiguity":0.1,"scope":"medium","category":"quickplan","suggested_questions":[],"confidence":0.9,"suggested_mode":"quick_plan","approval_requested":false}`,
		`{"approval_required":false}`)
	if _, err := ia.AnalyzeTrueIntent(context.Background(), "plan the rollout, hold off coding until I sign off", nil); err != nil {
		t.Fatalf("AnalyzeTrueIntent: %v", err)
	}
	headers := log.all()
	if len(headers) < 2 {
		t.Fatalf("expected 2 LLM calls, saw %d", len(headers))
	}
	for i, h := range headers {
		if h != "" {
			t.Errorf("unscoped call %d sent header %q; empty scope must send no header", i, h)
		}
	}
}
