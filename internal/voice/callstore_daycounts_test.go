package voice

// 02-daystate/00T §D — TodayCountsByEmployee test gate (NEW file;
// callstore_test.go untouched).
//
// FROZEN method contract (03-calls-counts.md):
//
//	func (s *CallStore) TodayCountsByEmployee(ctx context.Context,
//	    employeeID string, loc *time.Location) (CallDayCounts, error)
//	type CallDayCounts struct{ Today, Failed int }
//
// Window: WHERE employee_id = ? AND started_at >= local-midnight(loc),
// the TodayTranscripts (:266) idiom — number-scoped TodayTranscripts is
// NOT repurposed. Failed counts rows whose outcome is exactly
// OutcomeFailed.
//
// Outcome vocab pinned from source (internal/voice/manager.go:25-28):
//
//	OutcomeAnswered = "answered"; OutcomeNoAnswer = "no_answer";
//	OutcomeFailed   = "failed";   OutcomeHangup   = "hangup"
//
// Only the exact string "failed" (OutcomeFailed) increments Failed —
// SweepStaleLive stamps stranded rows 'failed' too, and the spec's
// 'Failed = outcome LIKE failed' reads as exact vocab membership; the
// other terminal outcomes (answered/no_answer/hangup) count in Today
// only. Live rows (outcome '') count in Today per the fixture shape.

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newDueTestCallStore(t *testing.T) *CallStore {
	t.Helper()
	s, err := NewCallStore(filepath.Join(t.TempDir(), "calls.db"), nil)
	if err != nil {
		t.Fatalf("NewCallStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestTodayCountsByEmployeeFixture — §D's pinned fixture: 3 today calls
// (2 ended ok, 1 failed), 1 yesterday, 1 other-employee -> {Today:3,
// Failed:1}.
func TestTodayCountsByEmployeeFixture(t *testing.T) {
	s := newDueTestCallStore(t)
	ctx := context.Background()
	loc, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skipf("load America/Denver: %v", err)
	}
	s.loc = loc

	now := time.Now().In(loc)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yesterday := midnight.Add(-2 * time.Hour)

	// Three calls today for emp-a.
	starts := []struct {
		id       string
		at       time.Time
		outcome  string
		employee string
	}{
		{"d-today-1", midnight.Add(1 * time.Hour), string(OutcomeAnswered), "emp-a"},
		{"d-today-2", midnight.Add(2 * time.Hour), string(OutcomeNoAnswer), "emp-a"},
		{"d-today-3", midnight.Add(3 * time.Hour), string(OutcomeFailed), "emp-a"},
		{"d-yest-1", yesterday, string(OutcomeAnswered), "emp-a"},
		{"d-other-1", midnight.Add(1 * time.Hour), string(OutcomeFailed), "emp-b"},
	}
	for _, c := range starts {
		if err := s.Start(ctx, CallRecord{
			CallID: c.id, Number: "+155****700" + c.id[len(c.id)-1:],
			EmployeeID: c.employee, Direction: DirectionIncoming,
			SessionID: "sess-" + c.id, StartedAt: c.at,
		}); err != nil {
			t.Fatalf("Start %s: %v", c.id, err)
		}
		if c.outcome != "" {
			if err := s.End(ctx, c.id, c.outcome); err != nil {
				t.Fatalf("End %s: %v", c.id, err)
			}
		}
	}

	counts, err := s.TodayCountsByEmployee(ctx, "emp-a", loc)
	if err != nil {
		t.Fatalf("TodayCountsByEmployee: %v", err)
	}
	if counts.Today != 3 {
		t.Errorf("Today = %d; want 3 (yesterday + other-employee excluded)", counts.Today)
	}
	if counts.Failed != 1 {
		t.Errorf("Failed = %d; want 1 (only outcome %q counts; answered/no_answer do not)",
			counts.Failed, OutcomeFailed)
	}
}

// TestTodayCountsByEmployeeLocalMidnight: the day boundary tracks loc,
// not UTC — same TZ-differential posture as TodayTranscripts. A call
// just before local midnight is yesterday; just after is today, even
// when both share one UTC day.
func TestTodayCountsByEmployeeLocalMidnight(t *testing.T) {
	for _, zone := range []string{"Pacific/Kiritimati", "Pacific/Pago_Pago"} {
		t.Run(zone, func(t *testing.T) {
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Skipf("load %s: %v", zone, err)
			}
			s := newDueTestCallStore(t)
			ctx := context.Background()
			s.loc = loc

			now := time.Now().In(loc)
			midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

			if err := s.Start(ctx, CallRecord{CallID: "z-before",
				Number: "+155****8001", EmployeeID: "emp-a",
				StartedAt: midnight.Add(-time.Minute)}); err != nil {
				t.Fatalf("Start before-midnight: %v", err)
			}
			if err := s.End(ctx, "z-before", string(OutcomeFailed)); err != nil {
				t.Fatalf("End before-midnight: %v", err)
			}
			if err := s.Start(ctx, CallRecord{CallID: "z-after",
				Number: "+155****8001", EmployeeID: "emp-a",
				StartedAt: midnight.Add(time.Minute)}); err != nil {
				t.Fatalf("Start after-midnight: %v", err)
			}
			if err := s.End(ctx, "z-after", string(OutcomeAnswered)); err != nil {
				t.Fatalf("End after-midnight: %v", err)
			}

			counts, err := s.TodayCountsByEmployee(ctx, "emp-a", loc)
			if err != nil {
				t.Fatalf("TodayCountsByEmployee: %v", err)
			}
			if counts.Today != 1 || counts.Failed != 0 {
				t.Errorf("counts = {Today:%d Failed:%d}; want {1 0} — boundary must be local midnight of loc, not UTC",
					counts.Today, counts.Failed)
			}
		})
	}
}

// TestTodayCountsByEmployeeUnknownEmployee: an employee with no calls
// yields zeroed counts, nil error — the daemon fail-soft (§E) expects a
// clean zero, not an error, from an empty store.
func TestTodayCountsByEmployeeUnknownEmployee(t *testing.T) {
	s := newDueTestCallStore(t)
	counts, err := s.TodayCountsByEmployee(context.Background(), "emp-none", time.UTC)
	if err != nil {
		t.Fatalf("TodayCountsByEmployee empty: %v", err)
	}
	if counts.Today != 0 || counts.Failed != 0 {
		t.Errorf("counts = %+v; want zeroed", counts)
	}
}
