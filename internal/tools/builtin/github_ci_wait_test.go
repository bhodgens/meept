package builtin

// NOTE: gate tests for the github-pr-ci tree (leaf 00T). This file is
// intentionally RED: the constructors, CIRef, ReArmSink and the githubsvc
// pool referenced below do not exist yet — they land in leaves 01/03.
// Written to compile once those land; a compile failure is the only
// expected failure in RED state.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/githubsvc"
)

// ---------------------------------------------------------------------------
// CI-specific fakes (separate from the PR tool fakes so each test file is
// self-contained and the fake can serve a state sequence for poll scenarios).
// ---------------------------------------------------------------------------

// ciTokenStub is a static TokenSource for the pool under test.
type ciTokenStub struct{}

func (ciTokenStub) Token(ctx context.Context) (string, error) {
	return "ghs_testtoken", nil
}

// ciGateFake is a minimal fake GitHub API server that can serve a sequence
// of check states, so poll-style tests can drive a "first in_progress, then
// success" or "always in_progress" trajectory. The checks endpoint is the
// one the CI tools poll; the PR + workflow endpoints are answered with
// canned shapes for the status tool.
type ciGateFake struct {
	*httptest.Server
	reqPaths []string
	// checkSeq drives the /check-runs response: the n-th call returns
	// checkSeq[n-1] (or the last entry if the sequence is shorter). Each
	// entry is "success" / "in_progress" / "failure".
	checkSeq []string
	checkHit int64
}

func newCIGateFake(checkSeq ...string) *ciGateFake {
	f := &ciGateFake{checkSeq: checkSeq}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.reqPaths = append(f.reqPaths, r.Method+" "+r.URL.Path)
		p := r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		switch {
		case p == "/repos/octo/cat/pulls/7":
			// v93 PullRequests.Get unmarshals into &PullRequest, which
			// rejects the string "mergeable":"yes" (now *bool).
			_, _ = w.Write([]byte(`{"number":7,"state":"open","draft":false,"head":{"sha":"abc123"}}`))
		case strings.Contains(p, "/check-runs"):
			f.writeChecks(w)
		case p == "/repos/octo/cat/actions/runs":
			_, _ = w.Write([]byte(`{"total_count":1,"workflow_runs":[{"name":"ci","status":"completed","conclusion":"success"}]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	return f
}

func (f *ciGateFake) writeChecks(w http.ResponseWriter) {
	state := "success"
	if len(f.checkSeq) > 0 {
		idx := int(atomic.AddInt64(&f.checkHit, 1)) - 1
		if idx >= 0 && idx < len(f.checkSeq) {
			state = f.checkSeq[idx]
		} else {
			state = f.checkSeq[len(f.checkSeq)-1]
		}
	}
	_, _ = w.Write([]byte(`{"total_count":1,"check_runs":[` +
		`{"name":"lint","status":"completed","conclusion":"` + state + `"}]}`))
}

// recordedPaths returns the request path log as a slice for assertions.
func (f *ciGateFake) recordedPaths() []string {
	out := make([]string, len(f.reqPaths))
	copy(out, f.reqPaths)
	return out
}

// newCIGatePool builds a real *githubsvc.ClientPool (contract C1) pointing
// at the fake server — the hermetic BaseURL seam.
func newCIGatePool(t *testing.T, fake *ciGateFake) *githubsvc.ClientPool {
	t.Helper()
	pool, err := githubsvc.NewClientPool(githubsvc.ClientPoolOptions{
		Tokens:  ciTokenStub{},
		BaseURL: fake.URL,
	})
	if err != nil {
		t.Fatalf("NewClientPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// testReArmSink records ScheduleReArm calls so the CI-wait gate tests can
// assert whether the sink fired and with what waitID. It implements the
// ReArmSink interface pinned in contract C3 (leaf 03).
type testReArmSink struct {
	calls    int
	lastRef  githubsvc.CIRef
	lastWait string
}

func (s *testReArmSink) ScheduleReArm(ref githubsvc.CIRef, at time.Time, waitID string) error {
	s.calls++
	s.lastRef = ref
	s.lastWait = waitID
	return nil
}

// ciContains is a small local substring helper (self-contained per file).
func ciContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// ciResultJSON normalises a tool Execute result to a JSON string for shape
// assertions. Handles string / map / *tools.ToolResult envelopes.
func ciResultJSON(t *testing.T, res any) string {
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

// TestGitHubPRStatusTool verifies github_pr_status end-to-end: the fake
// returns a PR + 2 successful checks + 1 successful workflow, so
// all_green must be true and the result must carry number / state /
// all_green fields.
func TestGitHubPRStatusTool(t *testing.T) {
	fake := newCIGateFake("success", "success")
	pool := newCIGatePool(t, fake)
	tool := NewGitHubPRStatusTool(pool, githubsvc.OwnerRepo{Owner: "octo", Repo: "cat"})

	if got := tool.Name(); got != "github_pr_status" {
		t.Errorf("tool.Name() = %q, want %q", got, "github_pr_status")
	}
	if !tool.IsReadOnly(map[string]any{"pr": 7}) {
		t.Errorf("IsReadOnly = false, want true for github_pr_status")
	}

	res, err := tool.Execute(context.Background(), map[string]any{"pr": 7})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	s := ciResultJSON(t, res)
	// all_green must be true and the result must carry number / state /
	// all_green fields. JSON marshalling is compact, so the colon has no
	// space; check the compact rendering.
	for _, field := range []string{`"number":7`, `"state":"open"`, `"all_green":true`} {
		if !ciContains(s, field) {
			t.Errorf("result missing %s: %s", field, s)
		}
	}
	// The fake must have received the PR, checks and workflow endpoints.
	// Note: v93 ListCheckRunsForRef hits .../commits/<sha>/check-runs, so
	// match the full path.
	paths := fake.recordedPaths()
	for _, want := range []string{
		"GET /repos/octo/cat/pulls/7",
		"GET /repos/octo/cat/commits/abc123/check-runs",
		"GET /repos/octo/cat/actions/runs",
	} {
		if !sliceContains(paths, want) {
			t.Errorf("fake server did not receive %s; got: %v", want, paths)
		}
	}
}

// sliceContains reports whether s contains target.
func sliceContains(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}

// TestGitHubCIWaitTool_TerminalGreen verifies github_ci_wait: the fake
// first returns an in_progress check, then success; Execute returns with
// all_green=true and a terminal result. The ReArmSink must NOT be called
// (green reached, no park).
func TestGitHubCIWaitTool_TerminalGreen(t *testing.T) {
	fake := newCIGateFake("in_progress", "success")
	pool := newCIGatePool(t, fake)
	sink := &testReArmSink{}
	tool := NewGitHubCIWaitTool(pool, githubsvc.OwnerRepo{Owner: "octo", Repo: "cat"}, sink)

	if got := tool.Name(); got != "github_ci_wait" {
		t.Errorf("tool.Name() = %q, want %q", got, "github_ci_wait")
	}
	if !tool.IsReadOnly(map[string]any{"pr": 7}) {
		t.Errorf("IsReadOnly = false, want true for github_ci_wait")
	}

	// A generous timeout so the poll loop can advance in_progress -> success
	// within the test window; the fake flips to success on the second hit.
	res, err := tool.Execute(context.Background(), map[string]any{
		"pr":              7,
		"timeout_seconds": 60,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	s := ciResultJSON(t, res)
	if !ciContains(s, `"all_green":true`) && !ciContains(s, `"all_green": true`) {
		t.Errorf("result does not reflect all_green=true: %s", s)
	}
	// Terminal: the result should indicate a terminal state (all_green true
	// is the terminal-green marker the contract exposes).
	if sink.calls != 0 {
		t.Errorf("ScheduleReArm called %d times on green, want 0 (no park)", sink.calls)
	}
}

// TestGitHubCIWaitTool_ParkOnTimeout verifies the park path: the fake
// always returns in_progress, the timeout is tiny, and Execute returns a
// result indicating parked AND the sink's ScheduleReArm is called exactly
// once with a waitID + ref.
func TestGitHubCIWaitTool_ParkOnTimeout(t *testing.T) {
	fake := newCIGateFake("in_progress")
	pool := newCIGatePool(t, fake)
	sink := &testReArmSink{}
	tool := NewGitHubCIWaitTool(pool, githubsvc.OwnerRepo{Owner: "octo", Repo: "cat"}, sink)

	// A 1-second timeout with the fake never turning green forces the park
	// path. The result must carry a park/resume marker and the sink must
	// have been re-armed exactly once.
	res, err := tool.Execute(context.Background(), map[string]any{
		"pr":              7,
		"timeout_seconds": 1,
	})
	// The park path returns either a parked result or an error wrapping
	// ErrCIWaitParked. Both are acceptable; assert the sink fired.
	_ = err

	s := ciResultJSON(t, res)
	// A parked result carries a park/resume marker. Accept common spellings.
	if !ciContains(s, "park") && !ciContains(s, "resume") && !ciContains(s, "rearm") && !ciContains(s, "re_arm") && !ciContains(s, "wait_id") && !ciContains(s, "waitid") {
		t.Errorf("parked result missing a park/resume marker; err=%v res=%s", err, s)
	}

	if sink.calls != 1 {
		t.Fatalf("ScheduleReArm called %d times on timeout, want exactly 1", sink.calls)
	}
	// The re-arm ref must carry the PR's owner/repo/number and a non-empty
	// waitID.
	if sink.lastRef.Number != 7 {
		t.Errorf("ScheduleReArm ref.Number = %d, want 7", sink.lastRef.Number)
	}
	if sink.lastWait == "" {
		t.Errorf("ScheduleReArm waitID is empty, want a non-empty id")
	}
}

// TestGitHubCIWaitTool_FastFail verifies fast-fail: the fake returns one
// check with conclusion "failure"; Execute returns quickly (all_green=false)
// without waiting out the full timeout, and the terminal reason is a
// failure marker.
func TestGitHubCIWaitTool_FastFail(t *testing.T) {
	fake := newCIGateFake("failure")
	pool := newCIGatePool(t, fake)
	sink := &testReArmSink{}
	tool := NewGitHubCIWaitTool(pool, githubsvc.OwnerRepo{Owner: "octo", Repo: "cat"}, sink)

	// A large timeout that we must NOT have to wait out: fast-fail returns
	// as soon as a check concludes failure.
	start := time.Now()
	res, err := tool.Execute(context.Background(), map[string]any{
		"pr":              7,
		"timeout_seconds": 300,
	})
	elapsed := time.Since(start)
	_ = err

	s := ciResultJSON(t, res)
	if ciContains(s, `"all_green":true`) {
		t.Errorf("fast-fail result should not have all_green=true: %s", s)
	}
	if !ciContains(s, `"all_green":false`) && !ciContains(s, `"all_green": false`) {
		t.Errorf("fast-fail result should reflect all_green=false: %s", s)
	}
	// The fast-fail path should not have re-armed the sink.
	if sink.calls != 0 {
		t.Errorf("ScheduleReArm called %d times on fast-fail, want 0", sink.calls)
	}
	// A fast-fail with a 300s timeout must return well under the timeout.
	if elapsed > 5*time.Second {
		t.Errorf("fast-fail took %s, expected it to return promptly (not wait out the 300s timeout)", elapsed)
	}
}
