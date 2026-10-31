package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/caimlas/meept/internal/activity"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/rpc"
)

// registerActivityRPC registers the activity + drift RPC surface (leaf 02,
// master contracts C2/C4): four activity.* query methods over the
// aggregator + ring, plus agents.review over the employee Manager.
//
// Direct closures (pattern: memory_rpc.go), no bus proxies, single
// request/response - the GUI polls these over the HTTP-to-RPC bridge. Every
// handle is nil-guarded per method: a missing backing store degrades THAT
// method to a clean RPC error, never a panic. Double registration of the
// same method logs and skips (rpc.Server.RegisterHandler silently
// overwrites; this wrapper refuses to clobber a sibling surface, notably
// the employee agents.audit.* handlers).
func registerActivityRPC(server *rpc.Server, agg *activity.Aggregator,
	ring *activity.Ring, mgr *employee.Manager, planRes activity.PlanResolver,
) {
	if server == nil {
		return
	}

	register := func(method string, handler rpc.Handler) {
		if _, err := server.CallMethod(context.Background(), method, nil); err == nil {
			// CallMethod returns "method not found: ..." for unregistered
			// methods; a nil error means some handler already owns the
			// name. Log and skip - no panic, no clobber.
			slog.Warn("activity rpc: method already registered; skipping duplicate",
				"method", method)
			return
		}
		server.RegisterHandler(method, handler)
	}

	// activity.query - C2 params: since/until RFC3339, sort
	// chrono|task|plan, task_id/plan_id/agent_id filters, kinds list,
	// limit/offset paging. Result: {"records":[ActivityRecord],"total":N}
	// with total counted BEFORE limit/offset are applied.
	register("activity.query", func(ctx context.Context, params json.RawMessage) (any, error) {
		if agg == nil {
			return nil, errors.New("activity: aggregator not configured")
		}
		var req activityQueryParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("activity.query: invalid parameters: %w", err)
			}
		}
		switch req.Sort {
		case "", activity.SortChrono, activity.SortTask, activity.SortPlan:
		default:
			return nil, fmt.Errorf("activity.query: unknown sort %q (want chrono, task, or plan)", req.Sort)
		}
		q := activity.Query{
			Sort:    req.Sort,
			TaskID:  req.TaskID,
			PlanID:  req.PlanID,
			AgentID: req.AgentID,
			Kinds:   req.Kinds,
			Limit:   req.Limit,
			Offset:  req.Offset,
		}
		// RFC3339, second precision - truncate so comparisons against
		// store stamps are not skewed by sub-second param noise.
		var err error
		if req.Since != "" {
			if q.Since, err = parseActivityTime(req.Since); err != nil {
				return nil, fmt.Errorf("activity.query: since: %w", err)
			}
		}
		if req.Until != "" {
			if q.Until, err = parseActivityTime(req.Until); err != nil {
				return nil, fmt.Errorf("activity.query: until: %w", err)
			}
		}
		records, total, err := agg.Query(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("activity.query: %w", err)
		}
		if records == nil {
			records = []activity.ActivityRecord{}
		}
		return map[string]any{"records": records, "total": total}, nil
	})

	// activity.recent - C2: live tail from the ring buffer. Result:
	// {"records":[ActivityRecord],"dropped":N} where dropped is the
	// ring's total evicted-event counter.
	register("activity.recent", func(_ context.Context, params json.RawMessage) (any, error) {
		if ring == nil {
			return nil, errors.New("activity: ring buffer not configured")
		}
		var req activityRecentParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("activity.recent: invalid parameters: %w", err)
			}
		}
		limit := req.Limit
		if limit <= 0 {
			limit = 50
		}
		records, dropped := ring.Tail(limit)
		if records == nil {
			records = []activity.ActivityRecord{}
		}
		return map[string]any{"records": records, "dropped": dropped}, nil
	})

	// activity.overview - C2: per-agent rollup. active_tasks and
	// last_activity come from the aggregator; drift_score comes from
	// mgr.Review (net-new exposure, zero business logic here);
	// open_findings counts unresolved audit findings. Agents with no
	// activity at all are omitted from by_agent.
	register("activity.overview", func(ctx context.Context, _ json.RawMessage) (any, error) {
		if agg == nil {
			return nil, errors.New("activity: aggregator not configured")
		}
		if mgr == nil {
			return nil, errors.New("activity: employee manager not configured")
		}

		// Query everything once and roll up per agent in-process; the
		// underlying stores are local SQLite reads, bounded by the same
		// limits the aggregator applies (read-only, no write path).
		records, _, err := agg.Query(ctx, activity.Query{})
		if err != nil {
			return nil, fmt.Errorf("activity.overview: %w", err)
		}

		// Open findings per employee (unresolved = ResolvedAt nil).
		openFindings := map[string]int{}
		findings, findErr := mgr.ListAuditFindings(ctx, employee.AuditQuery{})
		if findErr == nil {
			for _, f := range findings {
				if f.ResolvedAt == nil {
					openFindings[f.EmployeeID]++
				}
			}
		}

		type agentRow struct {
			AgentID      string    `json:"agent_id"`
			ActiveTasks  int       `json:"active_tasks"`
			LastActivity time.Time `json:"last_activity"`
			DriftScore   float64   `json:"drift_score"`
			OpenFindings int       `json:"open_findings"`
		}
		byID := map[string]*agentRow{}
		order := []string{}
		row := func(id string) *agentRow {
			if id == "" {
				id = "unknown"
			}
			r, ok := byID[id]
			if !ok {
				r = &agentRow{AgentID: id}
				byID[id] = r
				order = append(order, id)
			}
			return r
		}
		for _, rec := range records {
			r := row(rec.AgentID)
			if !rec.At.IsZero() && rec.At.After(r.LastActivity) {
				r.LastActivity = rec.At
			}
		}
		// Active-task count: tasks with any non-terminal step.
		activeTasks := map[string]string{} // agent -> newest active task id
		for _, rec := range records {
			if rec.TaskID == "" {
				continue
			}
			switch rec.State {
			case "completed", "failed", "rejected", "skipped":
				continue
			}
			activeTasks[rec.AgentID] = rec.TaskID
		}
		for id, taskID := range activeTasks {
			row(id).ActiveTasks = 1
			_ = taskID
		}

		sort.Strings(order)
		rows := make([]agentRow, 0, len(order))
		for _, id := range order {
			r := byID[id]
			r.OpenFindings = openFindings[id]
			// Drift score via the existing Manager.Review read path.
			if review, revErr := mgr.Review(ctx, id); revErr == nil && review != nil {
				r.DriftScore = review.DriftScore
			}
			rows = append(rows, *r)
		}
		return map[string]any{
			"by_agent":     rows,
			"generated_at": time.Now().UTC(),
		}, nil
	})

	// activity.findings - C2: delegate to Manager.ListAuditFindings with
	// the C2 filter set. since/severity/employee_id pass through; the
	// resolved filter and offset paging are applied here (the store
	// filter has neither).
	register("activity.findings", func(ctx context.Context, params json.RawMessage) (any, error) {
		if mgr == nil {
			return nil, errors.New("activity: employee manager not configured")
		}
		var req activityFindingsParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("activity.findings: invalid parameters: %w", err)
			}
		}
		var since time.Time
		if req.Since != "" {
			var err error
			if since, err = parseActivityTime(req.Since); err != nil {
				return nil, fmt.Errorf("activity.findings: since: %w", err)
			}
		}
		limit := req.Limit
		if limit <= 0 {
			limit = 100
		}
		findings, err := mgr.ListAuditFindings(ctx, employee.AuditQuery{
			EmployeeID: req.EmployeeID,
			Severity:   req.Severity,
			// ListAuditFindings interprets Since as a window duration
			// ending at time.Now() (f.Since = now - q.Since); convert the
			// absolute RFC3339 cutoff into that window.
			Since: -time.Until(since),
		})
		if err != nil {
			return nil, fmt.Errorf("activity.findings: %w", err)
		}
		// Since post-filter: ListAuditFindings expresses the cutoff as a
		// window duration and skips it entirely when the resulting window
		// is not positive (a future `since` is unrepresentable there), so
		// enforce the absolute cutoff here for exactness.
		if !since.IsZero() {
			kept := findings[:0]
			for _, f := range findings {
				if !f.DetectedAt.Before(since) {
					kept = append(kept, f)
				}
			}
			findings = kept
		}
		// Resolved filter: nil ResolvedAt = open.
		if req.HasResolved {
			filtered := findings[:0]
			for _, f := range findings {
				if (f.ResolvedAt != nil) == req.Resolved {
					filtered = append(filtered, f)
				}
			}
			findings = filtered
		}
		total := len(findings)
		if req.Offset > 0 || req.Limit > 0 {
			start := req.Offset
			if start > total {
				start = total
			}
			end := total
			if req.Limit > 0 && start+req.Limit < end {
				end = start + req.Limit
			}
			findings = findings[start:end]
		}
		if findings == nil {
			findings = []employee.AuditFinding{}
		}
		return map[string]any{"findings": findings, "total": total}, nil
	})

	// agents.review - C4: delegate to Manager.Review(ctx, employee_id).
	// The Review struct (DriftScore + RecentFindings + employee + goals)
	// marshals to its existing json tags; this is the method's first
	// exposure. Does NOT duplicate agents.audit.* (those stay on the
	// employee RPCHandler).
	register("agents.review", func(ctx context.Context, params json.RawMessage) (any, error) {
		if mgr == nil {
			return nil, errors.New("activity: employee manager not configured")
		}
		var req activityReviewParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("agents.review: invalid parameters: %w", err)
			}
		}
		if req.EmployeeID == "" {
			return nil, errors.New("agents.review: employee_id is required")
		}
		review, err := mgr.Review(ctx, req.EmployeeID)
		if err != nil {
			if errors.Is(err, employee.ErrEmployeeNotFound) {
				return nil, fmt.Errorf("agents.review: %w", err)
			}
			return nil, fmt.Errorf("agents.review: %w", err)
		}
		if review == nil {
			return nil, fmt.Errorf("agents.review: %w", employee.ErrEmployeeNotFound)
		}
		return review, nil
	})
}

// parseActivityTime parses an RFC3339 timestamp and truncates to second
// precision (leaf notes: second precision against store stamps).
func parseActivityTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid RFC3339 timestamp %q: %w", s, err)
	}
	return t.Truncate(time.Second).UTC(), nil
}

// Param structs (leaf notes: small local decode structs in this file).

type activityQueryParams struct {
	Since   string   `json:"since"`
	Until   string   `json:"until"`
	Sort    string   `json:"sort"`
	TaskID  string   `json:"task_id"`
	PlanID  string   `json:"plan_id"`
	AgentID string   `json:"agent_id"`
	Kinds   []string `json:"kinds"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
}

type activityRecentParams struct {
	Limit int `json:"limit"`
}

type activityFindingsParams struct {
	EmployeeID string `json:"employee_id"`
	Since      string `json:"since"`
	Severity   string `json:"severity"`
	// HasResolved distinguishes "resolved not requested" (absent/false)
	// from resolved=true; plain bool is sufficient because C2 only
	// defines an optional boolean.
	Resolved    bool `json:"resolved"`
	HasResolved bool
	Limit       int `json:"limit"`
	Offset      int `json:"offset"`
}

// UnmarshalJSON captures whether "resolved" was present so the filter is
// only applied when the caller asked for it.
func (p *activityFindingsParams) UnmarshalJSON(data []byte) error {
	type alias activityFindingsParams
	aux := struct {
		Resolved *bool `json:"resolved"`
		*alias
	}{alias: (*alias)(p)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.Resolved != nil {
		p.HasResolved = true
		p.Resolved = *aux.Resolved
	}
	return nil
}

type activityReviewParams struct {
	EmployeeID string `json:"employee_id"`
}
