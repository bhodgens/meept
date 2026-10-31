package intake

// NOTE: gate tests for the github-intake-factory tree (leaf 00T). This
// file is intentionally RED: internal/intake has no implementation yet —
// the Contract 1 types (Ticket, Filter, Poller) and the GitHubPoller
// adapter land in leaf 01. A compile failure naming the missing intake
// symbols is the ONLY expected failure in RED state; every assertion
// targets the exact contract shapes (C1) so a compile failure proves the
// contract is unimplemented and nothing else.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/githubsvc"
	"log/slog"
)

// ---------------------------------------------------------------------------
// Shared fakes (self-contained; package-local per the gate convention —
// same shape as internal/tools/builtin/github_*_test.go).
// ---------------------------------------------------------------------------

// intakeTokenStub is a static TokenSource for the pool under test.
type intakeTokenStub struct{}

func (intakeTokenStub) Token(ctx context.Context) (string, error) {
	return "ghs_intake_testtoken", nil
}

// ghIssue is a wire-shaped GitHub issue served by intakeFake. Only the
// fields the poller maps into Ticket are modeled.
type ghIssue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	Labels      []ghLabel `json:"labels,omitempty"`
	HTMLURL     string    `json:"html_url"`
	User        ghUser    `json:"user"`
	UpdatedAt   time.Time `json:"updated_at"`
	PullRequest *struct{} `json:"pull_request,omitempty"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghUser struct {
	Login string `json:"login"`
}

// intakeFake is a fake GitHub issues-list endpoint. It serves a scripted
// slice of issues and records every GET with its query string so tests
// can assert the watermark (Since) and the label gate parameters.
type intakeFake struct {
	*httptest.Server

	mu     sync.Mutex
	issues []ghIssue
	gets   []string // "GET /repos/o/r/issues?<raw query>"
}

func newIntakeFake(issues []ghIssue) *intakeFake {
	f := &intakeFake{issues: issues}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.gets = append(f.gets, "GET "+r.URL.Path+"?"+r.URL.RawQuery)
		out, _ := json.Marshal(f.issues)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	})
	f.Server = httptest.NewServer(mux)
	return f
}

func (f *intakeFake) recordedGets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.gets))
	copy(out, f.gets)
	return out
}

// newIntakePool builds a real *githubsvc.ClientPool pointing at the fake
// server — the hermetic BaseURL seam (contract C1's pool requirement).
func newIntakePool(t *testing.T, fake *intakeFake) *githubsvc.ClientPool {
	t.Helper()
	pool, err := githubsvc.NewClientPool(githubsvc.ClientPoolOptions{
		Tokens:  intakeTokenStub{},
		BaseURL: fake.URL,
	})
	if err != nil {
		t.Fatalf("NewClientPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// intakeIssue is a tiny builder for the wire issues the fakes serve.
func intakeIssue(num int, title string, updatedAt time.Time, labels ...string) ghIssue {
	gi := ghIssue{
		Number:    num,
		Title:     title,
		Body:      fmt.Sprintf("body of issue %d", num),
		HTMLURL:   fmt.Sprintf("https://github.com/o/r/issues/%d", num),
		User:      ghUser{Login: "octo"},
		UpdatedAt: updatedAt,
	}
	for _, l := range labels {
		gi.Labels = append(gi.Labels, ghLabel{Name: l})
	}
	return gi
}

// ---------------------------------------------------------------------------
// F1: label gate + watermark (leaf 01 Tasks 1-2)
// ---------------------------------------------------------------------------

// TestGitHubPoller_LabelGate: the fake serves issues with mixed labels;
// only issues carrying ALL required labels come back as Tickets (F1).
// PR entries (pull_request field present) are never returned.
func TestGitHubPoller_LabelGate(t *testing.T) {
	now := time.Now().UTC()
	issues := []ghIssue{
		intakeIssue(1, "factory ticket", now, "factory", "bug"),
		intakeIssue(2, "missing factory label", now),
		intakeIssue(3, "partial labels", now, "factory"),
		intakeIssue(4, "case variant", now, "FACTORY", "BUG"),
	}
	// Issue 5 is a PR wearing the factory label — must be excluded.
	pr := intakeIssue(5, "a PR, not an issue", now, "factory")
	pr.PullRequest = &struct{}{}
	issues = append(issues, pr)

	fake := newIntakeFake(issues)
	poller := NewGitHubPoller(newIntakePool(t, fake),
		githubsvc.OwnerRepo{Owner: "o", Repo: "r"},
		Filter{RequiredLabels: []string{"factory", "bug"}},
		slog.DiscardHandler,
	)

	tickets, err := poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	var gotNums []string
	for _, tk := range tickets {
		if tk.Repo != "o/r" {
			t.Errorf("ticket Repo = %q, want %q", tk.Repo, "o/r")
		}
		gotNums = append(gotNums, tk.ID)
	}
	want := []string{"1", "4"} // full-label matches only; case-insensitive
	if len(gotNums) != len(want) {
		t.Fatalf("Poll returned tickets %v, want %v", gotNums, want)
	}
	for i, id := range want {
		if gotNums[i] != id {
			t.Fatalf("Poll returned tickets %v, want %v", gotNums, want)
		}
	}
}

// TestGitHubPoller_Watermark: the second Poll must only surface issues
// updated after the first poll's watermark. The fake swaps its issue list
// between polls: poll 1 sees issues 1-2 (both updated at T0); poll 2 sees
// a new issue 3 (updated after T0) plus the old ones — only issue 3 may
// come back (F1).
func TestGitHubPoller_Watermark(t *testing.T) {
	t0 := time.Now().UTC().Add(-time.Hour)
	issue1 := intakeIssue(1, "early ticket", t0, "factory")
	issue2 := intakeIssue(2, "early ticket 2", t0, "factory")
	issue3 := intakeIssue(3, "changed ticket", t0.Add(10*time.Minute), "factory")

	fake := newIntakeFake([]ghIssue{issue1, issue2})
	poller := NewGitHubPoller(newIntakePool(t, fake),
		githubsvc.OwnerRepo{Owner: "o", Repo: "r"},
		Filter{RequiredLabels: []string{"factory"}},
		slog.DiscardHandler,
	)

	first, err := poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("first Poll: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("first Poll returned %d tickets, want 2 (all currently-matching on a zero watermark)", len(first))
	}

	// Second scan window: issue 2's body changed (re-dispatch fodder) and
	// issue 3 appeared; issue 1 is untouched.
	fake.mu.Lock()
	fake.issues = []ghIssue{issue1, issue2, issue3}
	fake.mu.Unlock()

	second, err := poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("second Poll: %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("second Poll returned %d tickets, want 2 (issue 2 changed + issue 3 new; issue 1 is behind the watermark)", len(second))
	}
	seen := map[string]bool{}
	for _, tk := range second {
		seen[tk.ID] = true
	}
	if !seen["2"] || !seen["3"] {
		t.Fatalf("second Poll returned %v, want tickets 2 and 3 only", seen)
	}

	// The second GET must have carried the watermark as `since`.
	gets := fake.recordedGets()
	if len(gets) < 2 {
		t.Fatalf("fake saw %d GETs, want >= 2", len(gets))
	}
	if !containsSubstring(gets[1], "since=") {
		t.Errorf("second GET missing since=<watermark> parameter: %q", gets[1])
	}
}

// TestFilter_Matches: table-driven contract for the label gate — empty
// filter matches everything; matching is case-insensitive; missing one of
// several required labels fails (F1, leaf 01 Task 1).
func TestFilter_Matches(t *testing.T) {
	cases := []struct {
		name  string
		f     Filter
		input []string
		want  bool
	}{
		{"empty filter matches all", Filter{}, []string{"anything"}, true},
		{"empty filter matches none", Filter{}, nil, true},
		{"exact match", Filter{RequiredLabels: []string{"factory"}}, []string{"factory"}, true},
		{"case-insensitive match", Filter{RequiredLabels: []string{"factory"}}, []string{"FACTORY"}, true},
		{"case-insensitive required side", Filter{RequiredLabels: []string{"FACTORY"}}, []string{"factory"}, true},
		{"missing label fails", Filter{RequiredLabels: []string{"factory"}}, []string{"bug"}, false},
		{"all of several required", Filter{RequiredLabels: []string{"factory", "bug"}}, []string{"BUG", "Factory"}, true},
		{"missing one of several fails", Filter{RequiredLabels: []string{"factory", "bug"}}, []string{"factory"}, false},
		{"extra labels are fine", Filter{RequiredLabels: []string{"factory"}}, []string{"factory", "bug", "help"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.f.Matches(tc.input); got != tc.want {
				t.Errorf("Filter(%v).Matches(%v) = %v, want %v", tc.f.RequiredLabels, tc.input, got, tc.want)
			}
		})
	}
}

// containsSubstring is a small local helper (no strings import churn in
// assertions above beyond this).
func containsSubstring(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
