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

// reviewMetricWindows maps the frozen window tokens to their lookback.
// "all" is the zero-Since sentinel handled by the handler.
var reviewMetricWindows = map[string]time.Duration{
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// validReviewGroupBys is the frozen group_by whitelist (C5).
var validReviewGroupBys = map[string]bool{
	"model":    true,
	"judge":    true,
	"category": true,
	"rubric":   true,
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

// reviewsMetricsResponse is the JSON body of GET /api/v1/metrics/reviews.
// Rows is always emitted as an array (never null).
type reviewsMetricsResponse struct {
	Window           string                 `json:"window"`
	GroupBy          string                 `json:"group_by"`
	Rows             []reviewVerdictRowJSON `json:"rows"`
	WorkflowExcluded int64                  `json:"workflow_excluded"`
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
		Window:  window,
		GroupBy: groupBy,
		Rows:    []reviewVerdictRowJSON{},
	}
	if s.reviewVerdicts == nil {
		s.writeJSON(w, http.StatusOK, resp)
		return
	}

	q := metrics.ReviewVerdictQuery{GroupBy: groupBy}
	if d, bounded := reviewMetricWindows[window]; bounded {
		q.Since = time.Now().Add(-d)
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
		all := q
		all.IncludeWorkflow = true
		total, totalErr := counter.CountReviewVerdicts(all)
		defaultN, defaultErr := counter.CountReviewVerdicts(q)
		if totalErr != nil || defaultErr != nil {
			s.logger.Warn("review metrics: workflow exclusion count unavailable",
				"total_error", totalErr, "default_error", defaultErr)
		} else {
			resp.WorkflowExcluded = total - defaultN
		}
	}
	s.writeJSON(w, http.StatusOK, resp)
}
