package builtin

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/scheduler"
)

// TestScheduleCreateToolTiming covers the T5 wire (leaf 05-schedule/01-timing-kinds):
// the schedule_create tool exposes an optional "timing" string param, exclusive
// with "schedule" (Required keeps name+job_type), and a timing-only invocation
// builds a JobConfig from the parsed TimingSpec and schedules it.
func TestScheduleCreateToolTiming(t *testing.T) {
	sched, err := scheduler.NewScheduler(config.SchedulerConfig{}, nil, scheduler.WithDataDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}
	tool := NewScheduleCreateTool(sched)

	// Parameters(): timing documented, schedule no longer required.
	params := tool.Parameters()
	if _, ok := params.Properties["timing"]; !ok {
		t.Error("expected a 'timing' property in schedule_create Parameters()")
	}
	required := map[string]bool{}
	for _, r := range params.Required {
		required[r] = true
	}
	if !required["name"] || !required["job_type"] {
		t.Errorf("expected name+job_type to remain required, got %v", params.Required)
	}
	if required["schedule"] {
		t.Error("schedule should no longer be required")
	}

	ctx := context.Background()

	// Timing-only invocation schedules a job.
	result, err := tool.Execute(ctx, map[string]any{
		"name":     "timing reminder",
		"job_type": "reminder",
		"message":  "Daily",
		"timing":   "daily 08:00:00 UTC",
	})
	if err != nil {
		t.Fatalf("Execute with timing failed: %v", err)
	}
	createResult := result.(ScheduleCreateResult)
	if !createResult.Success {
		t.Fatalf("expected success scheduling with timing, got error %q", createResult.Error)
	}
	if createResult.JobID == "" {
		t.Error("expected a job_id from timing-only scheduling")
	}

	// Both schedule and timing are mutually exclusive.
	result, err = tool.Execute(ctx, map[string]any{
		"name":     "both",
		"job_type": "reminder",
		"message":  "X",
		"schedule": "0 8 * * *",
		"timing":   "daily 08:00:00 UTC",
	})
	if err != nil {
		t.Fatalf("Execute with both failed: %v", err)
	}
	if createResult := result.(ScheduleCreateResult); createResult.Success {
		t.Error("expected failure when both schedule and timing are provided")
	}

	// Neither schedule nor timing provided.
	result, err = tool.Execute(ctx, map[string]any{
		"name":     "none",
		"job_type": "reminder",
		"message":  "X",
	})
	if err != nil {
		t.Fatalf("Execute with neither failed: %v", err)
	}
	if createResult := result.(ScheduleCreateResult); createResult.Success {
		t.Error("expected failure when neither schedule nor timing is provided")
	}
}
