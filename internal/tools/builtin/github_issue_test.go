package builtin

// NOTE: gate tests for the github-pr-ci tree (leaf 00T). This file is
// intentionally RED: the constructors and the githubsvc pool referenced
// below do not exist yet — they land in leaves 01/04. Written to compile
// once those land; a compile failure is the only expected failure in RED
// state.

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
// Issue-specific fakes (self-contained per file).
// ---------------------------------------------------------------------------

// issueTokenStub is a static TokenSource for the pool under test.
type issueTokenStub struct{}

func (issueTokenStub) Token(ctx context.Context) (string, error) {
	return "ghs_testtoken", nil
}

// issueGateFake is a minimal fake GitHub API server answering the issue
// endpoints the issue tools exercise.
type issueGateFake struct {
	*httptest.Server
	reqPaths []string
}

func newIssueGateFake() *issueGateFake {
	f := &issueGateFake{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.reqPaths = append(f.reqPaths, r.Method+" "+r.URL.Path)
		p := r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		switch {
		case p == "/repos/octo/cat/issues/5":
			_, _ = w.Write([]byte(`{"number":5,"title":"x","state":"open","html_url":"https://github.com/octo/cat/issues/5"}`))
		case p == "/repos/octo/cat/issues/5/comments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":789,"body":"done","html_url":"https://github.com/octo/cat/issues/5#issuecomment-789"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	return f
}

func (f *issueGateFake) recordedPaths() []string {
	out := make([]string, len(f.reqPaths))
	copy(out, f.reqPaths)
	return out
}

// newIssueGatePool builds a real *githubsvc.ClientPool (contract C1)
// pointing at the fake server — the hermetic BaseURL seam.
func newIssueGatePool(t *testing.T, fake *issueGateFake) *githubsvc.ClientPool {
	t.Helper()
	pool, err := githubsvc.NewClientPool(githubsvc.ClientPoolOptions{
		Tokens:  issueTokenStub{},
		BaseURL: fake.URL,
	})
	if err != nil {
		t.Fatalf("NewClientPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// issueLedgerStub is a minimal effects.Ledger recording every claimed key,
// for asserting the exact effects key an issue tool derives (contract C4).
type issueLedgerStub struct {
	claimed []string
}

func newIssueLedgerStub() *issueLedgerStub { return &issueLedgerStub{} }

func (l *issueLedgerStub) Claim(ctx context.Context, key string, meta effects.EffectMeta) (bool, *effects.EffectRecord, error) {
	l.claimed = append(l.claimed, key)
	return true, nil, nil
}
func (l *issueLedgerStub) RecordReceipt(ctx context.Context, key string, receipt json.RawMessage) error {
	return nil
}
func (l *issueLedgerStub) Complete(ctx context.Context, key string) error               { return nil }
func (l *issueLedgerStub) Abandon(ctx context.Context, key string, reason string) error { return nil }
func (l *issueLedgerStub) ReconcilePending(ctx context.Context) ([]effects.EffectRecord, error) {
	return nil, nil
}
func (l *issueLedgerStub) Get(ctx context.Context, key string) (*effects.EffectRecord, error) {
	return nil, effects.ErrUnknownKey
}
func (l *issueLedgerStub) Close() error { return nil }

// issueGateSha256 returns the lowercase hex SHA256 of s, matching the
// effects-key convention in contract C4 (sha256(body)).
func issueGateSha256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// issueContains is a small local substring helper.
func issueContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// issueResultJSON normalises a tool Execute result to a JSON string for
// shape assertions, unwrapping a *tools.ToolResult envelope when present.
func issueResultJSON(t *testing.T, res any) string {
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
	case *tools.ToolResult:
		if v == nil {
			return ""
		}
		b, err := json.Marshal(v.Result)
		if err != nil {
			t.Fatalf("marshal inner result: %v", err)
		}
		return string(b)
	case tools.ToolResult:
		b, err := json.Marshal(v.Result)
		if err != nil {
			t.Fatalf("marshal inner result: %v", err)
		}
		return string(b)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal result: %v", err)
		}
		return string(b)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestGitHubIssueGetTool verifies github_issue_get: Execute with number=5
// returns the issue's title / state / number. IsReadOnly must be true.
func TestGitHubIssueGetTool(t *testing.T) {
	fake := newIssueGateFake()
	pool := newIssueGatePool(t, fake)
	tool := NewGitHubIssueGetTool(pool, githubsvc.OwnerRepo{Owner: "octo", Repo: "cat"})

	if got := tool.Name(); got != "github_issue_get" {
		t.Errorf("tool.Name() = %q, want %q", got, "github_issue_get")
	}
	if !tool.IsReadOnly(map[string]any{"number": 5}) {
		t.Errorf("IsReadOnly = false, want true for github_issue_get")
	}

	res, err := tool.Execute(context.Background(), map[string]any{"number": 5})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	s := issueResultJSON(t, res)
	for _, field := range []string{`"number":5`, `"title":"x"`, `"state":"open"`} {
		if !issueContains(s, field) {
			t.Errorf("result missing %s: %s", field, s)
		}
	}

	// The fake must have received a GET to the issue endpoint.
	found := false
	for _, p := range fake.recordedPaths() {
		if p == "GET /repos/octo/cat/issues/5" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("fake server did not receive GET /repos/octo/cat/issues/5; got: %v", fake.recordedPaths())
	}
}

// TestGitHubIssueCommentTool verifies github_issue_comment: Execute with
// number=5 and body "done" succeeds, and the claimed effects key matches
// contract C4:
// EffectKey("github_issue_comment", owner, repo, number, sha256(body)).
func TestGitHubIssueCommentTool(t *testing.T) {
	fake := newIssueGateFake()
	pool := newIssueGatePool(t, fake)
	ledger := newIssueLedgerStub()
	tool := NewGitHubIssueCommentTool(pool, ledger, githubsvc.OwnerRepo{Owner: "octo", Repo: "cat"})

	if got := tool.Name(); got != "github_issue_comment" {
		t.Errorf("tool.Name() = %q, want %q", got, "github_issue_comment")
	}

	res, err := tool.Execute(context.Background(), map[string]any{
		"number": 5,
		"body":   "done",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_ = res

	// The fake must have received a POST to the issue comment endpoint.
	found := false
	for _, p := range fake.recordedPaths() {
		if p == "POST /repos/octo/cat/issues/5/comments" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("fake server did not receive POST /repos/octo/cat/issues/5/comments; got: %v", fake.recordedPaths())
	}

	// Assert the exact effects key per contract C4.
	want := effects.EffectKey("github_issue_comment", "octo", "cat", "5", issueGateSha256("done"))
	if len(ledger.claimed) != 1 {
		t.Fatalf("expected exactly 1 claimed effects key, got %d: %v", len(ledger.claimed), ledger.claimed)
	}
	if ledger.claimed[0] != want {
		t.Errorf("claimed effects key = %s, want %s", ledger.claimed[0], want)
	}
}
