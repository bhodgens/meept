// Package http: GET /api/v1/metrics/reviews (model-rating-metrics-plan
// contract C5, docs/plans/model-rating-metrics-plan/03-surfaces/01-http-endpoint.md).
//
// Frozen response shape:
//
//	GET /api/v1/metrics/reviews?window=24h|7d|30d|all&group_by=model|judge|category|rubric
//	200 {"window","group_by","rows":[{group,verdicts,defects,pass_rate,label}],"workflow_excluded"}
//
// Degrade posture mirrors live_metrics_usage.go buildLiveMetricsResponse: a nil
// provider or a store error answers 200 with empty rows and
// workflow_excluded:0 — never 5xx. Bad window/group_by answer 400.
package http

import (
	"net/http"
	"time"

	"github.com/caimlas/meept/internal/metrics"
)

// ReviewVerdictProvider supplies review-verdict aggregates for
// GET /api/v1/metrics/reviews. One method mirroring
// metrics.Store.QueryReviewVerdicts (contract C2) — *metrics.Store and the
// daemon's metricsStoreWrapper implement it.
type ReviewVerdictProvider interface {
	QueryReviewVerdicts(q metrics.ReviewVerdictQuery) ([]metrics.ReviewVerdictRow, error)
}

// reviewVerdictCounter is the optional count half of the seam (contract C5's
// workflow_excluded needs COUNT of scope=workflow rows in-window). It is a
// separate optional interface so the required seam stays the single C2 method;
// providers that do not implement it defer the count to the MetricsService.
type reviewVerdictCounter interface {
	CountReviewVerdicts(q metrics.ReviewVerdictQuery) (int64, error)
}

// ReviewVerdictBreakdownProvider supplies the model_category breakdown
// (per-(reviewed model, category) counts). Separate from
// ReviewVerdictProvider so existing providers keep compiling; *metrics.Store
// and the daemon's metricsStoreWrapper both implement it, and the ambient
// MetricsService fallback resolves it the same way.
type ReviewVerdictBreakdownProvider interface {
	QueryReviewVerdictBreakdown(since time.Time) ([]metrics.ReviewVerdictBreakdownRow, error)
}

// reviewMetricWindows maps the frozen window tokens to their lookback.
// "all" is the zero-Since sentinel handled by the handler.
var reviewMetricWindows = map[string]time.Duration{
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// validReviewGroupBys is the frozen group_by whitelist (C5). "model_category"
// is the breakdown rollup (one row per reviewed model x category pair — the
// issue's question 2); its rows carry {model, category, count} instead of the
// single-dimension aggregate shape.
var validReviewGroupBys = map[string]bool{
	"model":          true,
	"judge":          true,
	"category":       true,
	"rubric":         true,
	"model_category": true,
}

// reviewVerdictRowJSON is one aggregate row of the frozen C5 shape. Label
// carries the rubric statement for rubric-grouped rows ("" otherwise).
type reviewVerdictRowJSON struct {
	Group    string  `json:"group"`
	Verdicts int64   `json:"verdicts"`
	Defects  int64   `json:"defects"`
	PassRate float64 `json:"pass_rate"`
	Label    string  `json:"label"`
}

// reviewVerdictBreakdownRowJSON is one row of the model_category breakdown:
// a (reviewed model, category) pair and its count.
type reviewVerdictBreakdownRowJSON struct {
	Model    string `json:"model"`
	Category string `json:"category"`
	Count    int64  `json:"count"`
}

// reviewsMetricsResponse is the JSON body of GET /api/v1/metrics/reviews.
// Rows is always emitted as an array (never null) — the frozen C5 shape.
// When group_by is model_category, Breakdown carries the per-pair rows
// ({model, category, count}) and Rows stays an empty array: one payload key
// per mode, both keys always present as arrays.
type reviewsMetricsResponse struct {
	Window           string                          `json:"window"`
	GroupBy          string                          `json:"group_by"`
	Rows             []reviewVerdictRowJSON          `json:"rows"`
	Breakdown        []reviewVerdictBreakdownRowJSON `json:"breakdown"`
	WorkflowExcluded int64                           `json:"workflow_excluded"`
}

// WithReviewVerdictProvider wires the review-verdict source used by
// GET /api/v1/metrics/reviews. When unset (or nil) the endpoint degrades to
// 200 with empty rows. Nil-guarded per the setter convention.
func WithReviewVerdictProvider(p ReviewVerdictProvider) ServerOption {
	return func(s *Server) {
		if p != nil {
			s.reviewVerdicts = p
		}
	}
}

// handleReviewsMetrics serves GET /api/v1/metrics/reviews. Stateless: every
// request re-queries the provider. Failure paths degrade to the empty 200
// shape, matching buildLiveMetricsResponse: a nil provider answers empty (the
// surface is off until the daemon wires it), a provider store error answers
// empty with a warning — never 5xx. A provider that reports (nil, nil) defers
// to the ambient MetricsService when it can answer directly (the canonical
// *metrics.Store path).
func (s *Server) handleReviewsMetrics(w http.ResponseWriter, r *http.Request) {
	window := r.URL.Query().Get("window")
	groupBy := r.URL.Query().Get("group_by")
	if _, bounded := reviewMetricWindows[window]; !bounded && window != "all" {
		s.writeError(w, http.StatusBadRequest, "invalid window (want 24h, 7d, 30d, or all)")
		return
	}
	if !validReviewGroupBys[groupBy] {
		s.writeError(w, http.StatusBadRequest, "invalid group_by (want model, judge, category, or rubric)")
		return
	}

	resp := reviewsMetricsResponse{
		Window:    window,
		GroupBy:   groupBy,
		Rows:      []reviewVerdictRowJSON{},
		Breakdown: []reviewVerdictBreakdownRowJSON{},
	}
	if s.reviewVerdicts == nil {
		s.writeJSON(w, http.StatusOK, resp)
		return
	}

	q := metrics.ReviewVerdictQuery{GroupBy: groupBy}
	if d, bounded := reviewMetricWindows[window]; bounded {
		q.Since = time.Now().Add(-d)
	}

	// Breakdown mode (group_by=model_category): the per-(model, category)
	// aggregate. Same degrade posture as the single-dimension mode — provider
	// miss consults the ambient MetricsService, a store error answers the
	// empty 200 shape, never 5xx. workflow_excluded stays 0 here: breakdown
	// rows are model-scope by construction, so there is no excluded set to
	// count.
	if groupBy == "model_category" {
		since := time.Time{}
		if d, bounded := reviewMetricWindows[window]; bounded {
			since = time.Now().Add(-d)
		}
		var rows []metrics.ReviewVerdictBreakdownRow
		if p, ok := s.reviewVerdicts.(ReviewVerdictBreakdownProvider); ok {
			if storeRows, storeErr := p.QueryReviewVerdictBreakdown(since); storeErr != nil {
				s.logger.Warn("review metrics: verdict breakdown unavailable", "error", storeErr)
			} else {
				rows = storeRows
			}
		}
		if rows == nil {
			if p, ok := s.metricsService.(ReviewVerdictBreakdownProvider); ok {
				if storeRows, storeErr := p.QueryReviewVerdictBreakdown(since); storeErr != nil {
					s.logger.Warn("review metrics: fallback verdict breakdown unavailable", "error", storeErr)
				} else {
					rows = storeRows
				}
			}
		}
		if rows != nil {
			resp.Breakdown = make([]reviewVerdictBreakdownRowJSON, 0, len(rows))
			for _, row := range rows {
				resp.Breakdown = append(resp.Breakdown, reviewVerdictBreakdownRowJSON{
					Model:    row.Model,
					Category: row.Category,
					Count:    row.Count,
				})
			}
		}
		s.writeJSON(w, http.StatusOK, resp)
		return
	}

	rows, err := s.reviewVerdicts.QueryReviewVerdicts(q)
	if err != nil {
		s.logger.Warn("review metrics: verdict aggregates unavailable", "error", err)
		s.writeJSON(w, http.StatusOK, resp)
		return
	}
	counter, hasCounter := s.reviewVerdicts.(reviewVerdictCounter)
	if rows == nil {
		// The provider added nothing; consult the ambient MetricsService when
		// it implements the C2 query itself (*metrics.Store and the daemon's
		// wrapper both do).
		if p, ok := s.metricsService.(ReviewVerdictProvider); ok {
			storeRows, storeErr := p.QueryReviewVerdicts(q)
			if storeErr != nil {
				s.logger.Warn("review metrics: fallback verdict aggregates unavailable", "error", storeErr)
			} else if storeRows != nil {
				rows = storeRows
				if c, ok := p.(reviewVerdictCounter); ok {
					counter, hasCounter = c, true
				}
			}
		}
	}

	if rows != nil {
		resp.Rows = make([]reviewVerdictRowJSON, 0, len(rows))
		for _, row := range rows {
			resp.Rows = append(resp.Rows, reviewVerdictRowJSON{
				Group:    row.Group,
				Verdicts: row.Verdicts,
				Defects:  row.Defects,
				PassRate: row.PassRate,
				Label:    row.Label,
			})
		}
	}

	// workflow_excluded: scope=workflow rows in-window = all rows minus the
	// default (workflow-excluding) count. A count failure degrades to 0 like
	// every other store failure on this endpoint.
	if hasCounter {
		// L5 (bughunt 2026-11-24): one COUNT per request instead of two.
		// The workflow-excluded base needs no second scan:
		// QueryReviewVerdicts partitions EVERY in-window non-workflow row
		// into exactly one group (GroupBy is a fixed whitelist column), so
		// summing the rows already fetched equals
		// CountReviewVerdicts(IncludeWorkflow=false) — previously issued
		// as a second full COUNT on an append-only table (VR-RET-1).
		// The clamped difference also fixes the old race where a
		// non-workflow row appended between the two COUNTs made
		// workflow_excluded negative.
		all := q
		all.IncludeWorkflow = true
		total, totalErr := counter.CountReviewVerdicts(all)
		if totalErr != nil {
			s.logger.Warn("review metrics: workflow exclusion count unavailable",
				"total_error", totalErr)
		} else {
			var defaultN int64
			if rows == nil {
				// Degenerate seam case: the provider answered (nil, nil) and
				// no fallback produced rows, so there is no sum to derive the
				// default count from — issue the explicit count (this is the
				// only path that still spends a second query).
				explicit, explicitErr := counter.CountReviewVerdicts(q)
				if explicitErr != nil {
					s.logger.Warn("review metrics: workflow exclusion count unavailable",
						"total_error", totalErr, "default_error", explicitErr)
					s.writeJSON(w, http.StatusOK, resp)
					return
				}
				defaultN = explicit
			} else {
				for _, row := range rows {
					defaultN += row.Verdicts
				}
			}
			if total > defaultN {
				resp.WorkflowExcluded = total - defaultN
			}
		}
	}
	s.writeJSON(w, http.StatusOK, resp)
}
