package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// TestActivityRPC_OverviewDistinctActiveTasks pins the DISTINCT-task
// semantics of activity.overview: an agent with N non-terminal steps
// across M distinct tasks reports active_tasks = M, not 1 (regression
// guard for the map-single-value defect the leaf-03 review surfaced).
func TestActivityRPC_OverviewDistinctActiveTasks(t *testing.T) {
	fix := newActivityRPCFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-30 * time.Minute)

	// Fixture baseline: emp-1 has ONE running step on task-0. Add:
	// (a) a second RUNNING step for emp-1 on the SAME task-0, and
	// (b) a new task-2 with a RUNNING step for emp-1.
	// Distinct active tasks for emp-1 must be 2, not 1 and not 3.
	extra := []struct {
		taskIdx int
		stepID  string
		newTask bool
	}{
		{taskIdx: 0, stepID: "step-1b", newTask: false},
		{taskIdx: 1, stepID: "step-3", newTask: true},
	}
	for _, e := range extra {
		tkID := fix.taskIDs[e.taskIdx]
		if e.newTask {
			tk, err := fix.reg.Create(ctx, fmt.Sprintf("task-%d", e.taskIdx), "distinct seed task")
			if err != nil {
				t.Fatalf("registry.Create: %v", err)
			}
			tk.AssignedAgent = "emp-1"
			if err := fix.reg.Update(ctx, tk); err != nil {
				t.Fatalf("registry.Update: %v", err)
			}
			tkID = tk.ID
		}
		step := &task.TaskStep{
			ID:          e.stepID,
			TaskID:      tkID,
			Description: "distinct pin step",
			AgentID:     "emp-1",
			State:       task.StepRunning,
			Sequence:    9,
			CreatedAt:   ts,
			UpdatedAt:   ts,
		}
		if err := fix.reg.StepStore().Create(step); err != nil {
			t.Fatalf("StepStore.Create: %v", err)
		}
	}

	raw, err := callActivity(t, fix.srv, "activity.overview", `{}`)
	if err != nil {
		t.Fatalf("activity.overview: %v", err)
	}
	rawJSON, _ := json.Marshal(raw)
	var resp struct {
		ByAgent []struct {
			AgentID     string `json:"agent_id"`
			ActiveTasks int    `json:"active_tasks"`
		} `json:"by_agent"`
	}
	if err := json.Unmarshal(rawJSON, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := map[string]int{}
	for _, a := range resp.ByAgent {
		byID[a.AgentID] = a.ActiveTasks
	}
	if byID["emp-1"] != 2 {
		t.Errorf("emp-1 active_tasks = %d, want 2 distinct (task-0 + task-2; terminal steps excluded)", byID["emp-1"])
	}
	if byID["emp-2"] != 0 {
		t.Errorf("emp-2 active_tasks = %d, want 0 (only step completed)", byID["emp-2"])
	}
}
