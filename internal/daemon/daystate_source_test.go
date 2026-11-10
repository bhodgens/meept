package daemon

// 02-daystate/00T §E — daemon composite daystate.Source test gate (NEW
// file; no existing daemon file touched).
//
// CLOCK/CONSTRUCTION SEAMS PINNED HERE (impl leaf 02/02 follows; do not
// silently reshape):
//
//   - newLedgerDayStateSource(d dayStateSourceDeps) daystate.Source
//     lives in awareness_wiring.go (01-02 creates the file, 02-02 edits
//     it serially per master.md).
//   - Overdue predicate uses deps.Now (func() time.Time, default
//     time.Now) — NO wall-clock in tests: seed due_at around a fixed now.
//   - Signals mapping (frozen contract):
//       OpenCommitments = len(QueryOpen rows) (status IN open,
//                         waiting_on_me, waiting_on_them by store)
//       Overdue         = rows with !Due.IsZero() && Due.Before(now)
//       ActiveGoals     = goals with State == employee.GoalActive
//       BlockedGoals    = goals with State == employee.GoalBlocked
//       CallsToday / CallsFailedToday = voice.CallDayCounts fields
//       ActivityItemsToday = len(Aggregator.Query records) issued with
//         Query.AgentID == employeeID, Query.Limit == 1000, Query.Since
//         == local midnight of the employee loc (fake asserts and
//         records the Query below).
//   - Bounded + fail-soft (master D-contract): per source ctx budget
//     2s; any source error -> that contribution stays zero, others
//     intact, Signals(..., err=nil); ALL sources erroring -> zeroed
//     Signals, nil error (render glue then omits the line — §F).
//
// Cross-branch RED symbols (expected until their leaves land):
// ledger.State.Due / ledger.DraftRecord.Due (02/01),
// voice.CallDayCounts + TodayCountsByEmployee (02/03),
// newLedgerDayStateSource + dayStateSourceDeps (02/02). daystate pkg
// symbols (02/02) are same-branch and gated in internal/daystate.

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/activity"
	"github.com/caimlas/meept/internal/daystate"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/voice"
)

var errSourceDown = errors.New("daystate gate: source down")

// fakeGoalLister records calls and returns canned goals.
type fakeGoalLister struct {
	goals []*employee.Goal
	err   error
	calls int
}

func (f *fakeGoalLister) ListActive(ctx context.Context, employeeID string) ([]*employee.Goal, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	var out []*employee.Goal
	for _, g := range f.goals {
		if g.EmployeeID == employeeID {
			out = append(out, g)
		}
	}
	return out, nil
}

// fakeCallCounter serves canned day counts (stands in for
// voice.CallStore's new method until leaf 02/03).
type fakeCallCounter struct {
	counts voice.CallDayCounts
	err    error
	calls  int
}

func (f *fakeCallCounter) TodayCountsByEmployee(ctx context.Context, employeeID string, loc *time.Location) (voice.CallDayCounts, error) {
	f.calls++
	if f.err != nil {
		return voice.CallDayCounts{}, f.err
	}
	return f.counts, nil
}

// fakeActivityQuerier records the Query the source must issue
// (AgentID/Limit/Since pins) and returns canned records.
type fakeActivityQuerier struct {
	records []activity.ActivityRecord
	err     error
	calls   int
	lastQ   activity.Query
}

func (f *fakeActivityQuerier) Query(ctx context.Context, q activity.Query) ([]activity.ActivityRecord, int, error) {
	f.calls++
	f.lastQ = q
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.records, len(f.records), nil
}

// newDayStateLedger seeds one employee's ledger: open subjects with the
// given due offsets from now (zero = no due), all around the fixed seam
// time so the overdue predicate is deterministic.
func newDayStateLedger(t *testing.T, employeeID string, now time.Time, dues map[string]time.Duration) *ledger.Store {
	t.Helper()
	s, err := ledger.Open(filepath.Join(t.TempDir(), "ledger.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("ledger.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	for subject, off := range dues {
		d := ledger.DraftRecord{
			EmployeeID: employeeID,
			Kind:       ledger.KindCommitment,
			Subject:    subject,
			Source:     ledger.SourceDesk,
			SetStatus:  ledger.StatusOpen,
			Summary:    "seed " + subject,
		}
		if off != 0 {
			d.Due = now.Add(off).UTC().Truncate(time.Second)
		}
		if _, err := s.Append(context.Background(), d); err != nil {
			t.Fatalf("seed append %s: %v", subject, err)
		}
	}
	return s
}

func dayStateGateNow() time.Time {
	return time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)
}

// TestDayStateSourceExactSignalsMapping — all four sources healthy:
// each Signals field maps from its source, no cross-contamination.
func TestDayStateSourceExactSignalsMapping(t *testing.T) {
	now := dayStateGateNow()
	const emp = "emp-ds-1"

	// 3 open commitments: 1 overdue (-1h), 1 due later, 1 no due.
	ls := newDayStateLedger(t, emp, now, map[string]time.Duration{
		"s-late": -time.Hour,
		"s-soon": time.Hour,
		"s-none": 0,
	})

	goals := &fakeGoalLister{goals: []*employee.Goal{
		{ID: "g1", EmployeeID: emp, State: employee.GoalActive},
		{ID: "g2", EmployeeID: emp, State: employee.GoalActive},
		{ID: "g3", EmployeeID: emp, State: employee.GoalBlocked},
		{ID: "g4", EmployeeID: emp, State: employee.GoalPaused},
		{ID: "g5", EmployeeID: "other", State: employee.GoalBlocked},
	}}
	calls := &fakeCallCounter{counts: voice.CallDayCounts{Today: 5, Failed: 2}}
	acts := &fakeActivityQuerier{records: []activity.ActivityRecord{
		{Kind: "task_step", AgentID: emp}, {Kind: "task_step", AgentID: emp},
		{Kind: "session_message", AgentID: emp}, {Kind: "session_message", AgentID: emp},
		{Kind: "audit_record", AgentID: emp}, {Kind: "audit_record", AgentID: emp},
		{Kind: "bus_event", AgentID: emp},
	}}

	src := newLedgerDayStateSource(dayStateSourceDeps{
		Ledger:   ls,
		Goals:    goals,
		Calls:    calls,
		Activity: acts,
		Now:      func() time.Time { return now },
		Loc:      time.UTC,
		Logger:   slog.New(slog.DiscardHandler),
	})

	got, err := src.Signals(context.Background(), emp)
	if err != nil {
		t.Fatalf("Signals: %v", err)
	}
	want := daystate.Signals{
		OpenCommitments:    3,
		Overdue:            1,
		ActiveGoals:        2,
		BlockedGoals:       1,
		CallsToday:         5,
		CallsFailedToday:   2,
		ActivityItemsToday: 7,
	}
	if got != want {
		t.Errorf("Signals = %+v; want %+v", got, want)
	}

	// Activity query pins: AgentID scoping, limit 1000, local-midnight Since.
	if acts.lastQ.AgentID != emp {
		t.Errorf("activity Query.AgentID = %q; want %q", acts.lastQ.AgentID, emp)
	}
	if acts.lastQ.Limit != 1000 {
		t.Errorf("activity Query.Limit = %d; want 1000", acts.lastQ.Limit)
	}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if !acts.lastQ.Since.Equal(midnight) {
		t.Errorf("activity Query.Since = %v; want local midnight %v", acts.lastQ.Since, midnight)
	}
}

// TestDayStateSourceFailSoftPerSource — one source erroring contributes
// ZERO, every other source's contribution survives, and the composite
// still returns nil error (contract: NEVER blocks a turn).
func TestDayStateSourceFailSoftPerSource(t *testing.T) {
	now := dayStateGateNow()
	const emp = "emp-ds-2"

	newDeps := func() dayStateSourceDeps {
		return dayStateSourceDeps{
			Ledger: newDayStateLedger(t, emp, now, map[string]time.Duration{
				"a": -time.Hour, "b": time.Hour,
			}),
			Goals: &fakeGoalLister{goals: []*employee.Goal{
				{ID: "g1", EmployeeID: emp, State: employee.GoalActive},
			}},
			Calls:    &fakeCallCounter{counts: voice.CallDayCounts{Today: 4, Failed: 1}},
			Activity: &fakeActivityQuerier{records: []activity.ActivityRecord{{Kind: "task_step", AgentID: emp}}},
			Now:      func() time.Time { return now },
			Loc:      time.UTC,
			Logger:   slog.New(slog.DiscardHandler),
		}
	}

	t.Run("goals_down", func(t *testing.T) {
		d := newDeps()
		d.Goals.(*fakeGoalLister).err = errSourceDown
		got, err := newLedgerDayStateSource(d).Signals(context.Background(), emp)
		if err != nil {
			t.Fatalf("err = %v; want nil (fail-soft)", err)
		}
		want := daystate.Signals{OpenCommitments: 2, Overdue: 1, CallsToday: 4, CallsFailedToday: 1, ActivityItemsToday: 1}
		if got != want {
			t.Errorf("Signals = %+v; want %+v (goal contribution zeroed)", got, want)
		}
	})

	t.Run("calls_down", func(t *testing.T) {
		d := newDeps()
		d.Calls.(*fakeCallCounter).err = errSourceDown
		got, err := newLedgerDayStateSource(d).Signals(context.Background(), emp)
		if err != nil {
			t.Fatalf("err = %v; want nil (fail-soft)", err)
		}
		if got.CallsToday != 0 || got.CallsFailedToday != 0 {
			t.Errorf("call fields = %+v; want zeroed", got)
		}
		if got.OpenCommitments != 2 || got.Overdue != 1 || got.ActiveGoals != 1 || got.ActivityItemsToday != 1 {
			t.Errorf("other sources damaged: %+v", got)
		}
	})

	t.Run("ledger_down", func(t *testing.T) {
		// A ledger store whose db is closed mid-flight errors the query.
		d := newDeps()
		ls := d.Ledger.(*ledger.Store)
		if err := ls.Close(); err != nil {
			t.Fatalf("close ledger: %v", err)
		}
		got, err := newLedgerDayStateSource(d).Signals(context.Background(), emp)
		if err != nil {
			t.Fatalf("err = %v; want nil (fail-soft)", err)
		}
		if got.OpenCommitments != 0 || got.Overdue != 0 {
			t.Errorf("ledger fields = %+v; want zeroed contributions", got)
		}
		if got.ActiveGoals != 1 || got.CallsToday != 4 || got.ActivityItemsToday != 1 {
			t.Errorf("other sources damaged: %+v", got)
		}
	})

	t.Run("activity_down", func(t *testing.T) {
		d := newDeps()
		d.Activity.(*fakeActivityQuerier).err = errSourceDown
		got, err := newLedgerDayStateSource(d).Signals(context.Background(), emp)
		if err != nil {
			t.Fatalf("err = %v; want nil (fail-soft)", err)
		}
		if got.ActivityItemsToday != 0 {
			t.Errorf("ActivityItemsToday = %d; want 0", got.ActivityItemsToday)
		}
		if got.OpenCommitments != 2 || got.ActiveGoals != 1 || got.CallsToday != 4 {
			t.Errorf("other sources damaged: %+v", got)
		}
	})
}

// TestDayStateSourceAllDownZeroed — all sources error -> zeroed Signals
// + nil error. The render glue (awareness_wiring, §F/02-02) then omits
// the day-state line entirely (master: "if all sources fail, Render
// omits the day-state line").
func TestDayStateSourceAllDownZeroed(t *testing.T) {
	now := dayStateGateNow()
	const emp = "emp-ds-3"

	ls := newDayStateLedger(t, emp, now, nil)
	if err := ls.Close(); err != nil {
		t.Fatalf("close ledger: %v", err)
	}
	src := newLedgerDayStateSource(dayStateSourceDeps{
		Ledger:   ls,
		Goals:    &fakeGoalLister{err: errSourceDown},
		Calls:    &fakeCallCounter{err: errSourceDown},
		Activity: &fakeActivityQuerier{err: errSourceDown},
		Now:      func() time.Time { return now },
		Loc:      time.UTC,
		Logger:   slog.New(slog.DiscardHandler),
	})

	got, err := src.Signals(context.Background(), emp)
	if err != nil {
		t.Fatalf("err = %v; want nil — daystate compute NEVER blocks a turn", err)
	}
	if got != (daystate.Signals{}) {
		t.Errorf("Signals = %+v; want fully zeroed", got)
	}
}
