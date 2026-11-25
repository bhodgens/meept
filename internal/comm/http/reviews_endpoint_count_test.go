package http

// L5 (bughunt 2026-11-24) pin: GET /api/v1/metrics/reviews must issue
// exactly ONE CountReviewVerdicts query per request for workflow_excluded.
// The pre-fix handler counted twice (IncludeWorkflow=true and the default
// predicate); the default count is now derived by summing the Verdicts of
// the aggregate rows the handler already fetched — QueryReviewVerdicts
// partitions every in-window non-workflow row into exactly one group.

import (
	"fmt"
	"testing"

	"github.com/caimlas/meept/internal/metrics"
)

// countingReviewStore wraps a real *metrics.Store and counts
// CountReviewVerdicts calls. All other methods (QueryReviewVerdicts,
// QueryReviewVerdictBreakdown) promote through the embedded pointer, so the
// endpoint resolves the same seam interfaces it resolves in production.
type countingReviewStore struct {
	*metrics.Store
	countCalls int
	lastCountQ []metrics.ReviewVerdictQuery
}

func (c *countingReviewStore) CountReviewVerdicts(q metrics.ReviewVerdictQuery) (int64, error) {
	c.countCalls++
	c.lastCountQ = append(c.lastCountQ, q)
	return c.Store.CountReviewVerdicts(q)
}

// TestHTTP_L5_CountOncePerRequest asserts the handler issues exactly one
// total-count query per single-dimension request, and the derived
// workflow_excluded matches the two-count value the pre-fix handler produced
// (seed: 4 model-scope recent + 1 model-scope 40d-old + 2 workflow recent).
func TestHTTP_L5_CountOncePerRequest(t *testing.T) {
	store := newReviewsGateStore(t)
	provider := &countingReviewStore{Store: store}
	_, mux := newReviewsGateServer(t, store, provider)

	cases := []struct {
		window  string
		groupBy string
		we      float64
	}{
		{"30d", "model", 2}, // total 5 - (m-a 2 + m-b 1)
		{"all", "model", 2}, // total 6 - 4 non-workflow rows
		{"24h", "category", 2},
		{"7d", "judge", 2},
	}
	for _, tc := range cases {
		provider.countCalls = 0
		provider.lastCountQ = nil
		code, body := doReviewsGET(t, mux, "?window="+tc.window+"&group_by="+tc.groupBy)
		if code != 200 {
			t.Fatalf("window %s/group %s: status %d, want 200", tc.window, tc.groupBy, code)
		}
		if provider.countCalls != 1 {
			t.Errorf("window %s/group %s: CountReviewVerdicts called %d times, want exactly 1 (queries: %+v)",
				tc.window, tc.groupBy, provider.countCalls, provider.lastCountQ)
		}
		// The one count must be the IncludeWorkflow (total) variant.
		if len(provider.lastCountQ) == 1 && !provider.lastCountQ[0].IncludeWorkflow {
			t.Errorf("window %s/group %s: the single count query was the default variant, want IncludeWorkflow total",
				tc.window, tc.groupBy)
		}
		if we, ok := body["workflow_excluded"].(float64); !ok || we != tc.we {
			t.Errorf("window %s/group %s: workflow_excluded = %v (%T), want %v (body: %v)",
				tc.window, tc.groupBy, body["workflow_excluded"], body["workflow_excluded"], tc.we, body)
		}
	}
}

// TestHTTP_L5_BreakdownModeIssuesNoCount asserts group_by=model_category
// stays count-free (workflow_excluded is structurally 0 there) — the single
// total count must not sneak into the breakdown path either.
func TestHTTP_L5_BreakdownModeIssuesNoCount(t *testing.T) {
	store := newReviewsGateStore(t)
	provider := &countingReviewStore{Store: store}
	_, mux := newReviewsGateServer(t, store, provider)

	code, body := doReviewsGET(t, mux, "?window=30d&group_by=model_category")
	if code != 200 {
		t.Fatalf("status %d, want 200", code)
	}
	if provider.countCalls != 0 {
		t.Errorf("breakdown mode issued %d count queries, want 0", provider.countCalls)
	}
	if we, ok := body["workflow_excluded"].(float64); !ok || we != 0 {
		t.Errorf("breakdown mode workflow_excluded = %v, want 0", body["workflow_excluded"])
	}
}

// TestHTTP_L5_CountFailureStillDegrades asserts a failing total count keeps
// the frozen degrade posture: 200, workflow_excluded 0, rows populated.
func TestHTTP_L5_CountFailureStillDegrades(t *testing.T) {
	store := newReviewsGateStore(t)
	_, mux := newReviewsGateServer(t, store, errCountReviewProvider{})

	code, body := doReviewsGET(t, mux, "?window=all&group_by=model")
	if code != 200 {
		t.Fatalf("status %d, want 200 (degrade, never 5xx)", code)
	}
	if we, ok := body["workflow_excluded"].(float64); !ok || we != 0 {
		t.Errorf("count failure: workflow_excluded = %v, want 0", body["workflow_excluded"])
	}
	rows, _ := body["rows"].([]any)
	if len(rows) != 3 { // the provider's canned rows still surface
		t.Errorf("count failure: rows = %v, want the 3 canned rows", body["rows"])
	}
}

// errCountReviewProvider returns canned rows but its count always fails.
type errCountReviewProvider struct{}

func (errCountReviewProvider) QueryReviewVerdicts(_ metrics.ReviewVerdictQuery) ([]metrics.ReviewVerdictRow, error) {
	return []metrics.ReviewVerdictRow{
		{Group: "m-a", Verdicts: 2, Defects: 1},
		{Group: "m-b", Verdicts: 1, Defects: 1},
		{Group: "m-old", Verdicts: 1, Defects: 1},
	}, nil
}

func (errCountReviewProvider) CountReviewVerdicts(_ metrics.ReviewVerdictQuery) (int64, error) {
	return 0, fmt.Errorf("count unavailable (injected)")
}
