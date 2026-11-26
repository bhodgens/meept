package activity

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
)

// slowFindings is a FindingLister that yields inside the read, widening the
// window between Query's plan-interest registration and stampPlanIDs'
// range of it — the exact interleaving the bughunt H2/A1-2 race needs to
// be caught reliably by -race.
type slowFindings struct {
	rows []FindingRow
}

func (f *slowFindings) ListFindings(ctx context.Context, employeeID, severity string, since time.Time, limit int) ([]FindingRow, error) {
	runtime.Gosched()
	return f.rows, nil
}

// TestAggregator_Query_Concurrent_PlanAndPlanless pins bughunt H2/A1-2:
// one Aggregator is shared by concurrent RPC handlers (activity.query,
// activity.overview, the awareness seam). The old code stored the caller's
// Query.PlanID on a shared field (a.pendingPlanIDs) with no lock while
// stampPlanIDs ranged it — a -race detector violation, and functionally a
// plan-scoped query's stamping could be wiped by a concurrent plan-less
// query. Run this with -race: the old shape fails with a data race report.
func TestAggregator_Query_Concurrent_PlanAndPlanless(t *testing.T) {
	t0 := base()
	steps := fakeSteps{rows: []StepRow{
		{ID: "s1", TaskID: "task-a", Description: "a", State: "running", UpdatedAt: t0.Add(time.Minute)},
		{ID: "s2", TaskID: "task-b", Description: "b", State: "running", UpdatedAt: t0.Add(2 * time.Minute)},
	}}
	findings := &slowFindings{rows: []FindingRow{
		{ID: "f-1", EmployeeID: "emp-1", PlanID: "plan-1", Severity: "info", ViolatedRule: "r", DetectedAt: t0},
		{ID: "f-2", EmployeeID: "emp-1", PlanID: "plan-2", Severity: "info", ViolatedRule: "r", DetectedAt: t0},
	}}
	resolver := func(ctx context.Context, planID string) ([]string, error) {
		runtime.Gosched()
		switch planID {
		case "plan-1":
			return []string{"task-a"}, nil
		case "plan-2":
			return []string{"task-b"}, nil
		}
		return nil, nil
	}
	a := newAggregator(fakeTasks{}, steps, nil, nil, findings, Options{PlanResolver: resolver})

	var wg sync.WaitGroup
	const rounds = 50
	for i := range rounds {
		wg.Add(2)
		// Plan-scoped query (registers its plan interest, then the stamp
		// pass resolves it).
		go func(i int) {
			defer wg.Done()
			planID := "plan-1"
			if i%2 == 0 {
				planID = "plan-2"
			}
			if _, _, err := a.Query(context.Background(), Query{PlanID: planID, Sort: SortPlan}); err != nil {
				t.Errorf("plan query: %v", err)
			}
		}(i)
		// Plan-less query (used to reset the shared field mid-flight).
		go func() {
			defer wg.Done()
			if _, _, err := a.Query(context.Background(), Query{Sort: SortChrono}); err != nil {
				t.Errorf("planless query: %v", err)
			}
		}()
	}
	wg.Wait()

	// Functional assertion beyond -race: with per-query plan interest, a
	// plan-scoped query must ALWAYS resolve its own plan, even after
	// interleaved plan-less queries. task-a belongs to plan-1 only.
	for range 20 {
		if _, _, err := a.Query(context.Background(), Query{}); err != nil {
			t.Fatalf("planless query: %v", err)
		}
		recs, _, err := a.Query(context.Background(), Query{PlanID: "plan-1"})
		if err != nil {
			t.Fatalf("plan query: %v", err)
		}
		found := false
		for _, r := range recs {
			if r.Kind == KindTaskStep && r.TaskID == "task-a" {
				found = true
				if r.PlanID != "plan-1" {
					t.Fatalf("task-a step PlanID = %q, want plan-1 (plan interest wiped by concurrent query)", r.PlanID)
				}
			}
		}
		if !found {
			t.Fatal("task-a step missing from plan-1 filtered result")
		}
	}
}
