package plan

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// TestRestoreMappingsCoversAllExecutingPlans pins bughunt 2026-11-24 H5:
// RestoreMappings listed only the newest 100 executing plans
// (ORDER BY updated_at DESC LIMIT 100). Plan 101+ lost its
// phaseTaskMap/taskPlanMap rows, OnStepCompleted/OnTaskCompleted returned
// nil silently for them, and the plan never completed. updated_at freezes
// at synthesis, so the SAME 100 won every restart — the rest were
// permanently orphaned. RestoreMappings now pages through every
// task-bearing plan.
func TestRestoreMappingsCoversAllExecutingPlans(t *testing.T) {
	ctx := t.Context()
	logger := slog.Default()
	dbPath := filepath.Join(t.TempDir(), "restore.db")
	store, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	const total = 150
	base := time.Now().UTC().Add(-time.Hour)
	for i := range total {
		p := NewPlan(fmt.Sprintf("plan %03d", i), "d", "", "", "")
		p.State = StateExecuting
		p.TaskID = fmt.Sprintf("task-%03d", i)
		// Distinct updated_at so the pre-fix ORDER BY ... DESC LIMIT 100
		// deterministically drops the OLDEST 50.
		p.UpdatedAt = base.Add(time.Duration(i) * time.Second)
		if err := store.CreatePlan(ctx, p); err != nil {
			t.Fatalf("CreatePlan %d: %v", i, err)
		}
	}

	mgr := NewPlanManager(store, nil, config.PlansConfig{}, nil, logger)
	if err := mgr.RestoreMappings(ctx); err != nil {
		t.Fatalf("RestoreMappings: %v", err)
	}

	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	missing := 0
	for i := range total {
		taskID := fmt.Sprintf("task-%03d", i)
		if _, ok := mgr.taskPlanMap[taskID]; !ok {
			missing++
		}
	}
	if missing != 0 {
		t.Fatalf("%d of %d executing plans lost their task mapping (pre-fix shape: the oldest %d beyond the 100 cap)", missing, total, total-100)
	}
	if len(mgr.taskPlanMap) < total {
		t.Fatalf("taskPlanMap has %d entries, want >= %d", len(mgr.taskPlanMap), total)
	}
}
