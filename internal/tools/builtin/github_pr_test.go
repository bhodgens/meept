package builtin

// NOTE: gate tests for the github-pr-ci tree (leaf 00T). This file is
// intentionally RED: the constructors and the githubsvc pool referenced
// below do not exist yet — they land in leaves 01-05. The file is written
// to compile cleanly once those land, and the assertions target the exact
// contract shapes (C1, C2, C4) so a compile failure is the only expected
// failure in RED state.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/githubsvc"
	"github.com/caimlas/meept/internal/tools"
)

// ---------------------------------------------------------------------------
// Shared fakes for the PR tool tests (self-contained, no shared helper file).
// ---------------------------------------------------------------------------

// prTokenStub is a static TokenSource for the pool under test.
type prTokenStub struct{}

func (prTokenStub) Token(ctx context.Context) (string, error) {
	return "ghs_testtoken", nil
}

// prGateFake is a minimal fake GitHub API server answering the endpoints the
// PR tools exercise. It records request paths so tests can assert which
// endpoints were hit, and returns canned JSON matching go-github's shapes.
type prGateFake struct {
	*httptest.Server
	reqPaths     []string
	reviewBodies []string // decoded {"event":...,"body":...} per reviews POST
	checkState   string   // "success" or "in_progress"
	wfState      string
}

func newPrGateFake() *prGateFake {
	f := &prGateFake{checkState: "success", wfState: "success"}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.reqPaths = append(f.reqPaths, r.Method+" "+r.URL.Path)
		p := r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		switch {
		case p == "/repos/octo/cat/pulls":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":7,"state":"open","html_url":"https://github.com/octo/cat/pulls/7"}`))
		case p == "/repos/octo/cat/pulls/7/comments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":123,"body":"hi","html_url":"https://github.com/octo/cat/pulls/7#issuecomment-123"}`))
		case p == "/repos/octo/cat/pulls/7/reviews":
			var raw map[string]any
			_ = json.NewDecoder(r.Body).Decode(&raw)
			event, _ := raw["event"].(string)
			f.reviewBodies = append(f.reviewBodies, "event="+event)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":456,"state":"commented"}`))
		case p == "/repos/octo/cat/pulls/7/merge":
			_, _ = w.Write([]byte(`{"merged":true,"sha":"def456","message":"Merged"}`))
		case p == "/repos/octo/cat/pulls/7":
			_, _ = w.Write([]byte(`{"number":7,"state":"open","draft":false,"mergeable":true,"head":{"sha":"abc123"}}`))
		case p == "/repos/octo/cat/check-runs":
			f.writeChecks(w)
		case p == "/repos/octo/cat/actions/runs":
			f.writeWorkflows(w)
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	return f
}

func (f *prGateFake) writeChecks(w http.ResponseWriter) {
	// First check reflects checkState; second check is always success, so
	// "in_progress" yields a non-green summary and "success" yields all-green.
	name, status, conclusion := "lint", "completed", "success"
	if f.checkState == "in_progress" {
		status, conclusion = "in_progress", ""
	}
	_, _ = w.Write([]byte(`{"total_count":2,"check_runs":[` +
		`{"name":"` + name + `","status":"` + status + `","conclusion":"` + conclusion + `"},` +
		`{"name":"build","status":"completed","conclusion":"success"}]}`))
}

func (f *prGateFake) writeWorkflows(w http.ResponseWriter) {
	status := "completed"
	conclusion := "success"
	if f.wfState == "in_progress" {
		status, conclusion = "in_progress", ""
	}
	_, _ = w.Write([]byte(`{"total_count":1,"workflow_runs":[{"name":"ci","status":"` + status + `","conclusion":"` + conclusion + `"}]}`))
}

// recordedPaths returns the request path log as a slice for assertions.
func (f *prGateFake) recordedPaths() []string {
	out := make([]string, len(f.reqPaths))
	copy(out, f.reqPaths)
	return out
}

// newPrGatePool builds a real *githubsvc.ClientPool (contract C1) pointing
// at the fake server. This is the seam that keeps the tool tests
// hermetic: no network, BaseURL overridden to the httptest server.
func newPrGatePool(t *testing.T, fake *prGateFake) *githubsvc.ClientPool {
	t.Helper()
	pool, err := githubsvc.NewClientPool(githubsvc.ClientPoolOptions{
		Tokens:  prTokenStub{},
		BaseURL: fake.URL,
	})
	if err != nil {
		t.Fatalf("NewClientPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// ---------------------------------------------------------------------------
// Effects ledger stub.
// ---------------------------------------------------------------------------

// prLedgerStub is a minimal effects.Ledger that records every claimed key so
// tests can assert the exact effects key a tool derived (contract C4).
type prLedgerStub struct {
	claimed []string
}

func newPrLedgerStub() *prLedgerStub { return &prLedgerStub{} }

func (l *prLedgerStub) Claim(ctx context.Context, key string, meta effects.EffectMeta) (bool, *effects.EffectRecord, error) {
	l.claimed = append(l.claimed, key)
	return true, nil, nil
}
func (l *prLedgerStub) RecordReceipt(ctx context.Context, key string, receipt json.RawMessage) error {
	return nil
}
func (l *prLedgerStub) Complete(ctx context.Context, key string) error               { return nil }
func (l *prLedgerStub) Abandon(ctx context.Context, key string, reason string) error { return nil }
func (l *prLedgerStub) ReconcilePending(ctx context.Context) ([]effects.EffectRecord, error) {
	return nil, nil
}
func (l *prLedgerStub) Get(ctx context.Context, key string) (*effects.EffectRecord, error) {
	return nil, effects.ErrUnknownKey
}
func (l *prLedgerStub) Close() error { return nil }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// builtinGateSha256 returns the lowercase hex SHA256 of s, matching the
// effects-key convention in contract C4 (sha256(body)).
func builtinGateSha256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// resultJSONString normalises a tool Execute result to a JSON string for
// shape assertions. Tools return either a map[string]any or a JSON string;
// the concrete envelope type varies with the leaf implementation, so we
// handle the known shapes and fall back to a raw marshal.
func resultJSONString(t *testing.T, res any) string {
	t.Helper()
	switch v := res.(type) {
	case string:
		return v
	case map[string]any:
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal result map: %v", err)
		}
		return string(b)
	default:
		// *tools.ToolResult / tools.ToolResult envelopes: their Result
		// field is the inner payload. Use reflection-free access for the
		// two concrete shapes.
		if b, ok := jsonMarshalResult(t, res); ok {
			return string(b)
		}
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("marshal result: %v", err)
		}
		return string(b)
	}
}

// jsonMarshalResult extracts the inner payload from a *tools.ToolResult
// envelope when present. The tools package's ToolResult carries a .Result
// field; the inner payload is what the contract shapes (C2/C3) assert on.
func jsonMarshalResult(t *testing.T, res any) ([]byte, bool) {
	t.Helper()
	switch v := res.(type) {
	case *tools.ToolResult:
		if v == nil {
			return nil, false
		}
		b, err := json.Marshal(v.Result)
		if err != nil {
			t.Fatalf("marshal inner result: %v", err)
		}
		return b, true
	case tools.ToolResult:
		b, err := json.Marshal(v.Result)
		if err != nil {
			t.Fatalf("marshal inner result: %v", err)
		}
		return b, true
	default:
		return nil, false
	}
}

// expectedPRCreateKey builds the contract-C4 effects key for github_pr_create.
func expectedPRCreateKey(owner, repo, head, title string) string {
	return effects.EffectKey("github_pr_create", owner, repo, head, title)
}

// prGateContains is a small local substring helper (avoids a strings import
// proliferation across the four gate test files).
func prGateContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestGitHubPRCreateTool verifies github_pr_create end-to-end against a fake
// GitHub server: the tool returns the created PR's number and a URL containing
// "/pulls/7".
func TestGitHubPRCreateTool(t *testing.T) {
	fake := newPrGateFake()
	pool := newPrGatePool(t, fake)
	ledger := newPrLedgerStub()
	tool := NewGitHubPRCreateTool(pool, ledger, githubsvc.OwnerRepo{Owner: "octo", Repo: "cat"})

	// Verify the C2 tool name.
	if got := tool.Name(); got != "github_pr_create" {
		t.Errorf("tool.Name() = %q, want %q", got, "github_pr_create")
	}

	res, err := tool.Execute(context.Background(), map[string]any{
		"base":  "main",
		"head":  "feat/x",
		"title": "t",
		"body":  "b",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The result must carry number=7 and a URL containing /pulls/7.
	s := resultJSONString(t, res)
	if !prGateContains(s, `"number":7`) && !prGateContains(s, `"number": 7`) {
		t.Errorf("result does not contain number=7: %s", s)
	}
	if !prGateContains(s, "/pulls/7") {
		t.Errorf("result does not contain a URL with /pulls/7: %s", s)
	}

	// The fake must have received a POST to the create endpoint.
	found := false
	for _, p := range fake.recordedPaths() {
		if p == "POST /repos/octo/cat/pulls" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("fake server did not receive POST /repos/octo/cat/pulls; got: %v", fake.recordedPaths())
	}
}

// TestGitHubPRCreateTool_EffectsKey verifies the exact effects key the create
// tool claims matches contract C4:
// EffectKey("github_pr_create", owner, repo, head, title).
func TestGitHubPRCreateTool_EffectsKey(t *testing.T) {
	fake := newPrGateFake()
	pool := newPrGatePool(t, fake)
	ledger := newPrLedgerStub()
	tool := NewGitHubPRCreateTool(pool, ledger, githubsvc.OwnerRepo{Owner: "octo", Repo: "cat"})

	_, _ = tool.Execute(context.Background(), map[string]any{
		"base":  "main",
		"head":  "feat/x",
		"title": "t",
		"body":  "b",
	})

	want := expectedPRCreateKey("octo", "cat", "feat/x", "t")
	if len(ledger.claimed) != 1 {
		t.Fatalf("expected exactly 1 claimed effects key, got %d: %v", len(ledger.claimed), ledger.claimed)
	}
	if ledger.claimed[0] != want {
		t.Errorf("claimed effects key = %s, want %s", ledger.claimed[0], want)
	}
}

// TestGitHubPRCommentTool verifies github_pr_comment: Execute with pr=7 and
// body "hi" succeeds, and the claimed effects key matches contract C4:
// EffectKey("github_pr_comment", owner, repo, pr, sha256(body)).
func TestGitHubPRCommentTool(t *testing.T) {
	fake := newPrGateFake()
	pool := newPrGatePool(t, fake)
	ledger := newPrLedgerStub()
	tool := NewGitHubPRCommentTool(pool, ledger, githubsvc.OwnerRepo{Owner: "octo", Repo: "cat"})

	if got := tool.Name(); got != "github_pr_comment" {
		t.Errorf("tool.Name() = %q, want %q", got, "github_pr_comment")
	}

	res, err := tool.Execute(context.Background(), map[string]any{
		"pr":   7,
		"body": "hi",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_ = res

	// The fake must have received a POST to the comment endpoint.
	found := false
	for _, p := range fake.recordedPaths() {
		if p == "POST /repos/octo/cat/pulls/7/comments" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("fake server did not receive POST /repos/octo/cat/pulls/7/comments; got: %v", fake.recordedPaths())
	}

	// Assert the exact effects key per contract C4.
	want := effects.EffectKey("github_pr_comment", "octo", "cat", "7", builtinGateSha256("hi"))
	if len(ledger.claimed) != 1 {
		t.Fatalf("expected exactly 1 claimed effects key, got %d: %v", len(ledger.claimed), ledger.claimed)
	}
	if ledger.claimed[0] != want {
		t.Errorf("claimed effects key = %s, want %s", ledger.claimed[0], want)
	}
}

// TestGitHubPRReviewTool verifies github_pr_review: with an explicit event
// the review is posted as that event; when the event is absent the tool
// defaults to "COMMENT" (asserted by checking the recorded POST body or the
// fake's response for the COMMENT state).
func TestGitHubPRReviewTool(t *testing.T) {
	fake := newPrGateFake()
	pool := newPrGatePool(t, fake)
	ledger := newPrLedgerStub()
	tool := NewGitHubPRReviewTool(pool, ledger, githubsvc.OwnerRepo{Owner: "octo", Repo: "cat"})

	if got := tool.Name(); got != "github_pr_review" {
		t.Errorf("tool.Name() = %q, want %q", got, "github_pr_review")
	}

	// Explicit event: APPROVE.
	_, err := tool.Execute(context.Background(), map[string]any{
		"pr":    7,
		"body":  "b",
		"event": "APPROVE",
	})
	if err != nil {
		t.Fatalf("Execute(APPROVE): %v", err)
	}

	// No event: must default to COMMENT.
	_, err = tool.Execute(context.Background(), map[string]any{
		"pr":   7,
		"body": "b",
	})
	if err != nil {
		t.Fatalf("Execute(no event): %v", err)
	}

	// The fake must have received 2 POSTs to the reviews endpoint.
	reviewHits := 0
	for _, p := range fake.recordedPaths() {
		if p == "POST /repos/octo/cat/pulls/7/reviews" {
			reviewHits++
		}
	}
	if reviewHits != 2 {
		t.Errorf("expected 2 POST /repos/octo/cat/pulls/7/reviews calls, got %d; paths: %v", reviewHits, fake.recordedPaths())
	}

	// Assert the event captured on each review POST: the explicit-APPROVE
	// call must carry event=APPROVE; the no-event call must default to
	// event=COMMENT.
	if len(fake.reviewBodies) != 2 {
		t.Fatalf("expected 2 review bodies captured, got %d: %v", len(fake.reviewBodies), fake.reviewBodies)
	}
	if !prGateContains(fake.reviewBodies[0], "event=APPROVE") {
		t.Errorf("first review (explicit event) should carry event=APPROVE, got %q", fake.reviewBodies[0])
	}
	if !prGateContains(fake.reviewBodies[1], "event=COMMENT") {
		t.Errorf("second review (no event) should default to event=COMMENT, got %q", fake.reviewBodies[1])
	}
}

// TestGitHubPRMergeTool verifies github_pr_merge: Execute with pr=7 defaults
// to strategy "merge", and the claimed effects key matches contract C4:
// EffectKey("github_pr_merge", owner, repo, pr, head).
func TestGitHubPRMergeTool(t *testing.T) {
	fake := newPrGateFake()
	pool := newPrGatePool(t, fake)
	ledger := newPrLedgerStub()
	tool := NewGitHubPRMergeTool(pool, ledger, githubsvc.OwnerRepo{Owner: "octo", Repo: "cat"})

	if got := tool.Name(); got != "github_pr_merge" {
		t.Errorf("tool.Name() = %q, want %q", got, "github_pr_merge")
	}

	res, err := tool.Execute(context.Background(), map[string]any{
		"pr": 7,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	s := resultJSONString(t, res)
	// Default strategy "merge" — the result should reflect a successful merge.
	if !prGateContains(s, `"merged":true`) && !prGateContains(s, `"merged": true`) {
		t.Errorf("result does not indicate a merge happened: %s", s)
	}

	// The fake must have received a POST to the merge endpoint.
	found := false
	for _, p := range fake.recordedPaths() {
		if p == "PUT /repos/octo/cat/pulls/7/merge" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("fake server did not receive PUT /repos/octo/cat/pulls/7/merge; got: %v", fake.recordedPaths())
	}

	// Assert the exact effects key per contract C4. head = the PR's head SHA
	// ("abc123" from the fake PR response).
	want := effects.EffectKey("github_pr_merge", "octo", "cat", "7", "abc123")
	if len(ledger.claimed) != 1 {
		t.Fatalf("expected exactly 1 claimed effects key, got %d: %v", len(ledger.claimed), ledger.claimed)
	}
	if ledger.claimed[0] != want {
		t.Errorf("claimed effects key = %s, want %s", ledger.claimed[0], want)
	}
}
