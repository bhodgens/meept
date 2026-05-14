package scheduler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

func setupTestScheduler(t *testing.T) (*Scheduler, *RPCHandler) {
	t.Helper()
	tmpDir := t.TempDir()
	msgBus := bus.New(nil, nil)

	cfg := config.SchedulerConfig{
		Enabled:  true,
		Timezone: "UTC",
	}

	s, err := NewScheduler(cfg, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	t.Cleanup(func() {
		_ = s.Stop(ctx)
	})

	handler := NewRPCHandler(s)
	return s, handler
}

func TestRPCAddJob(t *testing.T) {
	_, handler := setupTestScheduler(t)
	ctx := context.Background()

	// Valid reminder job
	params := AddJobParams{
		ID:       "rpc-test-1",
		Name:     "RPC Test Job",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		Enabled:  true,
		ReminderConfig: &ReminderJobConfig{
			Message: "Test reminder",
		},
	}

	paramsJSON, _ := json.Marshal(params)
	result, err := handler.AddJob(ctx, paramsJSON)
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	addResult := result.(AddJobResult)
	if addResult.JobID != "rpc-test-1" {
		t.Errorf("expected job ID 'rpc-test-1', got %q", addResult.JobID)
	}

	if addResult.NextRun == nil {
		t.Error("expected NextRun to be set")
	}
}

func TestRPCAddJobAutoID(t *testing.T) {
	_, handler := setupTestScheduler(t)
	ctx := context.Background()

	// Job without ID
	params := AddJobParams{
		Name:     "Auto ID Job",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		Enabled:  true,
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	}

	paramsJSON, _ := json.Marshal(params)
	result, err := handler.AddJob(ctx, paramsJSON)
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	addResult := result.(AddJobResult)
	if addResult.JobID == "" {
		t.Error("expected auto-generated job ID")
	}
}

func TestRPCAddJobValidation(t *testing.T) {
	_, handler := setupTestScheduler(t)
	ctx := context.Background()

	// Missing name
	params := AddJobParams{
		ID:       "invalid-1",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
	}

	paramsJSON, _ := json.Marshal(params)
	_, err := handler.AddJob(ctx, paramsJSON)
	if err == nil {
		t.Error("expected error for missing name")
	}

	// Missing schedule
	params = AddJobParams{
		ID:   "invalid-2",
		Name: "Invalid",
		Type: JobTypeReminder,
	}

	paramsJSON, _ = json.Marshal(params)
	_, err = handler.AddJob(ctx, paramsJSON)
	if err == nil {
		t.Error("expected error for missing schedule")
	}

	// Missing type-specific config
	params = AddJobParams{
		ID:       "invalid-3",
		Name:     "Invalid",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
	}

	paramsJSON, _ = json.Marshal(params)
	_, err = handler.AddJob(ctx, paramsJSON)
	if err == nil {
		t.Error("expected error for missing reminder config")
	}
}

func TestRPCRemoveJob(t *testing.T) {
	scheduler, handler := setupTestScheduler(t)
	ctx := context.Background()

	// Add a job first
	jobCfg := JobConfig{
		ID:       "remove-test",
		Name:     "Remove Test",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		Enabled:  true,
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	}
	_, _ = scheduler.ScheduleConfig(jobCfg)

	// Remove it
	params := RemoveJobParams{JobID: "remove-test"}
	paramsJSON, _ := json.Marshal(params)

	result, err := handler.RemoveJob(ctx, paramsJSON)
	if err != nil {
		t.Fatalf("RemoveJob failed: %v", err)
	}

	resultMap := result.(map[string]any)
	if resultMap["success"] != true {
		t.Error("expected success true")
	}

	// Verify removed
	if scheduler.JobCount() != 0 {
		t.Error("job was not removed")
	}

	// Try to remove non-existent
	params = RemoveJobParams{JobID: "non-existent"}
	paramsJSON, _ = json.Marshal(params)

	_, err = handler.RemoveJob(ctx, paramsJSON)
	if err == nil {
		t.Error("expected error for non-existent job")
	}
}

func TestRPCListJobs(t *testing.T) {
	scheduler, handler := setupTestScheduler(t)
	ctx := context.Background()

	// Empty list
	result, err := handler.ListJobs(ctx, nil)
	if err != nil {
		t.Fatalf("ListJobs failed: %v", err)
	}

	resultMap := result.(map[string]any)
	if resultMap["count"].(int) != 0 {
		t.Error("expected empty list")
	}

	// Add jobs
	for i := range 3 {
		_, _ = scheduler.ScheduleConfig(JobConfig{
			ID:       string(rune('a' + i)),
			Name:     "Job",
			Type:     JobTypeReminder,
			Schedule: "@hourly",
			Enabled:  true,
			ReminderConfig: &ReminderJobConfig{
				Message: "Test",
			},
		})
	}

	// List all
	result, err = handler.ListJobs(ctx, nil)
	if err != nil {
		t.Fatalf("ListJobs failed: %v", err)
	}

	resultMap = result.(map[string]any)
	if resultMap["count"].(int) != 3 {
		t.Errorf("expected 3 jobs, got %v", resultMap["count"])
	}
}

func TestRPCListJobsWithFilters(t *testing.T) {
	scheduler, handler := setupTestScheduler(t)
	ctx := context.Background()

	// Add mixed jobs
	_, _ = scheduler.ScheduleConfig(JobConfig{
		ID:       "reminder-1",
		Name:     "Reminder 1",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		Enabled:  true,
		Tags:     []string{"important"},
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	})
	_, _ = scheduler.ScheduleConfig(JobConfig{
		ID:       "shell-1",
		Name:     "Shell 1",
		Type:     JobTypeShell,
		Schedule: "@hourly",
		Enabled:  true,
		ShellConfig: &ShellJobConfig{
			Command: "echo",
		},
	})

	// Filter by type
	params := ListJobsParams{Type: JobTypeReminder}
	paramsJSON, _ := json.Marshal(params)

	result, err := handler.ListJobs(ctx, paramsJSON)
	if err != nil {
		t.Fatalf("ListJobs with filter failed: %v", err)
	}

	resultMap := result.(map[string]any)
	if resultMap["count"].(int) != 1 {
		t.Errorf("expected 1 reminder job, got %v", resultMap["count"])
	}
}

func TestRPCRunJob(t *testing.T) {
	scheduler, handler := setupTestScheduler(t)
	ctx := context.Background()

	// Add a job
	_, _ = scheduler.ScheduleConfig(JobConfig{
		ID:       "run-test",
		Name:     "Run Test",
		Type:     JobTypeShell,
		Schedule: "@yearly",
		Enabled:  true,
		ShellConfig: &ShellJobConfig{
			Command: "echo",
			Args:    []string{"test"},
		},
	})

	// Run immediately
	params := RunJobParams{JobID: "run-test"}
	paramsJSON, _ := json.Marshal(params)

	result, err := handler.RunJob(ctx, paramsJSON)
	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}

	resultMap := result.(map[string]any)
	if resultMap["success"] != true {
		t.Error("expected success true")
	}

	// Run non-existent
	params = RunJobParams{JobID: "non-existent"}
	paramsJSON, _ = json.Marshal(params)

	_, err = handler.RunJob(ctx, paramsJSON)
	if err == nil {
		t.Error("expected error for non-existent job")
	}
}

func TestRPCGetJob(t *testing.T) {
	scheduler, handler := setupTestScheduler(t)
	ctx := context.Background()

	// Add a job
	_, _ = scheduler.ScheduleConfig(JobConfig{
		ID:       "get-test",
		Name:     "Get Test",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		Enabled:  true,
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	})

	// Get job
	params := GetJobParams{JobID: "get-test"}
	paramsJSON, _ := json.Marshal(params)

	result, err := handler.GetJob(ctx, paramsJSON)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}

	resultMap := result.(map[string]any)
	if resultMap["job"] == nil {
		t.Error("expected job info")
	}
	if resultMap["config"] == nil {
		t.Error("expected job config")
	}

	// Get non-existent
	params = GetJobParams{JobID: "non-existent"}
	paramsJSON, _ = json.Marshal(params)

	_, err = handler.GetJob(ctx, paramsJSON)
	if err == nil {
		t.Error("expected error for non-existent job")
	}
}

func TestRPCPauseResumeJob(t *testing.T) {
	scheduler, handler := setupTestScheduler(t)
	ctx := context.Background()

	// Add a job
	_, _ = scheduler.ScheduleConfig(JobConfig{
		ID:       "pause-test",
		Name:     "Pause Test",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		Enabled:  true,
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	})

	// Pause
	params := PauseJobParams{JobID: "pause-test"}
	paramsJSON, _ := json.Marshal(params)

	result, err := handler.PauseJob(ctx, paramsJSON)
	if err != nil {
		t.Fatalf("PauseJob failed: %v", err)
	}

	resultMap := result.(map[string]any)
	if resultMap["paused"] != true {
		t.Error("expected paused true")
	}

	// Resume
	resumeParams := ResumeJobParams{JobID: "pause-test"}
	resumeJSON, _ := json.Marshal(resumeParams)

	result, err = handler.ResumeJob(ctx, resumeJSON)
	if err != nil {
		t.Fatalf("ResumeJob failed: %v", err)
	}

	resultMap = result.(map[string]any)
	if resultMap["resumed"] != true {
		t.Error("expected resumed true")
	}
}

func TestRPCValidateSchedule(t *testing.T) {
	_, handler := setupTestScheduler(t)
	ctx := context.Background()

	testCases := []struct {
		schedule string
		valid    bool
	}{
		{"@hourly", true},
		{"@daily", true},
		{"@every 30m", true},
		{"* * * * *", true},
		{"invalid", false},
		{"", false},
	}

	for _, tc := range testCases {
		params := ValidateScheduleParams{Schedule: tc.schedule}
		paramsJSON, _ := json.Marshal(params)

		result, err := handler.ValidateSchedule(ctx, paramsJSON)
		if tc.schedule == "" {
			if err == nil {
				t.Error("expected error for empty schedule")
			}
			continue
		}
		if err != nil {
			t.Fatalf("ValidateSchedule failed for %q: %v", tc.schedule, err)
		}

		resultMap := result.(map[string]any)
		if resultMap["valid"].(bool) != tc.valid {
			t.Errorf("expected valid=%v for %q, got %v", tc.valid, tc.schedule, resultMap["valid"])
		}

		if tc.valid {
			examples := resultMap["examples"].([]string)
			if len(examples) == 0 {
				t.Errorf("expected examples for valid schedule %q", tc.schedule)
			}
		}
	}
}

func TestRPCStatus(t *testing.T) {
	scheduler, handler := setupTestScheduler(t)
	ctx := context.Background()

	// Add some jobs
	_, _ = scheduler.ScheduleConfig(JobConfig{
		ID:       "status-1",
		Name:     "Status 1",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		Enabled:  true,
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	})
	_, _ = scheduler.ScheduleConfig(JobConfig{
		ID:       "status-2",
		Name:     "Status 2",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		Enabled:  true,
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	})

	// Pause one
	_ = scheduler.PauseJob("status-2")

	result, err := handler.Status(ctx, nil)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	resultMap := result.(map[string]any)
	if resultMap["running"].(bool) != true {
		t.Error("expected running true")
	}
	if resultMap["timezone"].(string) != "UTC" {
		t.Errorf("expected timezone 'UTC', got %v", resultMap["timezone"])
	}
	if resultMap["total_jobs"].(int) != 2 {
		t.Errorf("expected 2 total jobs, got %v", resultMap["total_jobs"])
	}
	if resultMap["enabled_jobs"].(int) != 1 {
		t.Errorf("expected 1 enabled job, got %v", resultMap["enabled_jobs"])
	}
	if resultMap["disabled_jobs"].(int) != 1 {
		t.Errorf("expected 1 disabled job, got %v", resultMap["disabled_jobs"])
	}
}
