package activity

import (
	"context"
	"sort"
	"time"

	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
)

// PlanResolver resolves the INVERTED plan linkage (plans.task_id points at
// the parent task): given a plan ID, return the task IDs that belong to it.
// The daemon wiring supplies the resolver from the plan store so this
// package never imports internal/plan.
type PlanResolver func(ctx context.Context, planID string) (taskIDs []string, err error)

// Options tunes the aggregator. All fields optional.
type Options struct {
	// Now overrides time.Now for the findings window (tests).
	Now func() time.Time
	// PlanResolver resolves plan -> task IDs for plan stamping, the plan
	// filter, and the plan sort mode. Optional: without it, plan grouping
	// degrades to records' own PlanID values.
	PlanResolver PlanResolver
}

// Aggregator merges rows from the four activity silos into ActivityRecords.
// All handles are optional: a nil handle simply degrades to fewer record
// kinds, never an error.
type Aggregator struct {
	tasks    TaskLister
	steps    StepLister
	sessions SessionReader
	chain    ChainLister
	findings FindingLister
	opts     Options

	// pendingPlanIDs carries plan IDs declared via Query.PlanID for the
	// current query so stampPlanIDs can resolve them even when no record
	// in the batch carries a plan id. Reset per Query call.
	pendingPlanIDs map[string]bool

	// plans memoizes plan -> task resolutions across queries.
	plans planIndex
}

// NewAggregator builds an aggregator over the given store handles (any of
// which may be nil; a nil handle degrades to fewer record kinds, never an
// error). This is the contract C1-facing constructor: it adapts the
// concrete store types onto this package's narrow read seams (fakes for
// tests go through newAggregator).
func NewAggregator(tasks *task.Registry, steps *task.StepStore, sessions session.Store, chain *auditlog.Store, findings *employee.AuditStore, opts Options) *Aggregator {
	var (
		taskLister    TaskLister
		stepLister    StepLister
		sessionReader SessionReader
		chainLister   ChainLister
		findingLister FindingLister
	)
	if tasks != nil {
		taskLister = NewTaskRegistryAdapter(tasks)
	}
	if steps != nil {
		stepLister = NewStepStoreAdapter(steps)
	}
	if sessions != nil {
		sessionReader = NewSessionStoreAdapter(sessions)
	}
	if chain != nil {
		chainLister = NewChainStoreAdapter(chain)
	}
	if findings != nil {
		findingLister = NewAuditStoreAdapter(findings)
	}
	return newAggregator(taskLister, stepLister, sessionReader, chainLister, findingLister, opts)
}

// newAggregator is the seam-level constructor used by tests (fakes) and by
// NewAggregator.
func newAggregator(tasks TaskLister, steps StepLister, sessions SessionReader, chain ChainLister, findings FindingLister, opts Options) *Aggregator {
	return &Aggregator{
		tasks:          tasks,
		steps:          steps,
		sessions:       sessions,
		chain:          chain,
		findings:       findings,
		opts:           opts,
		pendingPlanIDs: map[string]bool{},
	}
}

// Query returns merged activity records matching q, sorted per q.Sort, and
// the total count BEFORE filters' effect on the page is confused with
// paging: total is the count of records matching the FILTERS but BEFORE
// Limit/Offset are applied (for pagination UIs). An empty Sort is
// chronological.
func (a *Aggregator) Query(ctx context.Context, q Query) ([]ActivityRecord, int, error) {
	// Register the caller's plan interest BEFORE collect so the stamp pass
	// can resolve plan -> tasks for steps with no plan-bearing sibling.
	if q.PlanID != "" {
		a.pendingPlanIDs = map[string]bool{q.PlanID: true}
	} else {
		a.pendingPlanIDs = map[string]bool{}
	}

	records, err := a.collect(ctx, q)
	if err != nil {
		return nil, 0, err
	}

	// Plan stamping for task_step records (plan filter/sort need it;
	// harmless and useful for the other modes too).
	if a.opts.PlanResolver != nil {
		a.stampPlanIDs(ctx, records)
	}

	records = applyFilters(a, records, q)
	total := len(records)
	sortRecords(records, q.Sort)

	if q.Offset > 0 || q.Limit > 0 {
		records = paginate(records, q.Offset, q.Limit)
	}
	return records, total, nil
}

// collect gathers rows from every non-nil handle. A nil handle contributes
// nothing; a per-handle read error skips that silo rather than failing the
// whole query (the error is a degraded view, not a query failure — the
// other silos' data is still user-visible). Task-name enrichment failure is
// likewise non-fatal: the record keeps its description summary.
func (a *Aggregator) collect(ctx context.Context, q Query) ([]ActivityRecord, error) {
	var records []ActivityRecord

	if a.tasks != nil && a.steps != nil {
		recs, err := a.taskStepRecords(ctx, q)
		if err == nil {
			records = append(records, recs...)
		}
	}

	if a.sessions != nil {
		recs, err := a.sessionMessageRecords(q)
		if err == nil {
			records = append(records, recs...)
		}
	}

	if a.chain != nil {
		recs, err := a.auditRecords(ctx)
		if err == nil {
			records = append(records, recs...)
		}
	}

	if a.findings != nil {
		recs, err := a.findingRecords(ctx, q)
		if err == nil {
			records = append(records, recs...)
		}
	}

	return records, nil
}

// paginate slices records after filters+sort. Offset beyond the end yields
// an empty (non-nil) slice; Limit <= 0 after an offset means "to the end".
func paginate(records []ActivityRecord, offset, limit int) []ActivityRecord {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(records) {
		return []ActivityRecord{}
	}
	records = records[offset:]
	if limit > 0 && limit < len(records) {
		records = records[:limit]
	}
	return records
}

// applyFilters narrows records by kinds, task, agent, session, plan, and
// the since/until window. Zero Since/Until mean unbounded.
func applyFilters(a *Aggregator, records []ActivityRecord, q Query) []ActivityRecord {
	kindSet := make(map[string]bool, len(q.Kinds))
	for _, k := range q.Kinds {
		kindSet[k] = true
	}
	var planTasks map[string]bool
	if q.PlanID != "" && a != nil && a.opts.PlanResolver != nil {
		if taskIDs, err := a.opts.PlanResolver(context.Background(), q.PlanID); err == nil {
			planTasks = make(map[string]bool, len(taskIDs))
			for _, id := range taskIDs {
				planTasks[id] = true
			}
		}
	}
	filtered := make([]ActivityRecord, 0, len(records))
	for _, r := range records {
		if len(kindSet) > 0 && !kindSet[r.Kind] {
			continue
		}
		if q.TaskID != "" && r.TaskID != q.TaskID {
			continue
		}
		if q.AgentID != "" && r.AgentID != q.AgentID {
			continue
		}
		if q.SessionID != "" && r.SessionID != q.SessionID {
			continue
		}
		if q.PlanID != "" && r.PlanID != q.PlanID && !(planTasks != nil && r.TaskID != "" && planTasks[r.TaskID]) {
			continue
		}
		if !q.Since.IsZero() && r.At.Before(q.Since) {
			continue
		}
		if !q.Until.IsZero() && r.At.After(q.Until) {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// sortRecords orders records per mode:
//
//	chrono: At ascending (zero At last), Kind as tiebreak.
//	task:   group by TaskID (empty last), At ascending within group.
//	plan:   group by PlanID (empty last), At ascending within group.
//
// Group modes sort by (hasGroup, groupKey) first, then At; because a group
// key never equals another group's key, a single stable comparison on the
// tuple (groupKey, At) with the empty-key-last rule is equivalent to two
// passes.
func sortRecords(records []ActivityRecord, mode string) {
	var less func(i, j int) bool
	switch mode {
	case SortTask:
		less = func(i, j int) bool {
			a, b := records[i], records[j]
			if (a.TaskID == "") != (b.TaskID == "") {
				return b.TaskID == ""
			}
			if a.TaskID != b.TaskID {
				return a.TaskID < b.TaskID
			}
			return timeAtLess(a.At, b.At)
		}
	case SortPlan:
		less = func(i, j int) bool {
			a, b := records[i], records[j]
			if (a.PlanID == "") != (b.PlanID == "") {
				return b.PlanID == ""
			}
			if a.PlanID != b.PlanID {
				return a.PlanID < b.PlanID
			}
			return timeAtLess(a.At, b.At)
		}
	default: // chrono
		less = func(i, j int) bool {
			a, b := records[i], records[j]
			if a.At.IsZero() != b.At.IsZero() {
				return b.At.IsZero()
			}
			if !a.At.Equal(b.At) {
				return a.At.Before(b.At)
			}
			return a.Kind < b.Kind
		}
	}
	sort.SliceStable(records, less)
}

// timeAtLess orders At ascending with zero times last (consistent with the
// chrono mode's nil-At-last rule inside groups too).
func timeAtLess(a, b time.Time) bool {
	if a.IsZero() != b.IsZero() {
		return b.IsZero()
	}
	return a.Before(b)
}
