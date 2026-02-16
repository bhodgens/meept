package scheduler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

func TestNewScheduler(t *testing.T) {
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

	if s == nil {
		t.Fatal("scheduler is nil")
	}

	if s.Name() != "scheduler" {
		t.Errorf("expected name 'scheduler', got %q", s.Name())
	}

	if s.Running() {
		t.Error("scheduler should not be running before Start")
	}
}

func TestSchedulerTimezone(t *testing.T) {
	tmpDir := t.TempDir()
	msgBus := bus.New(nil, nil)

	testCases := []struct {
		name     string
		timezone string
		wantErr  bool
	}{
		{"UTC", "UTC", false},
		{"America/New_York", "America/New_York", false},
		{"Europe/London", "Europe/London", false},
		{"Asia/Tokyo", "Asia/Tokyo", false},
		{"Invalid", "Invalid/Timezone", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.SchedulerConfig{
				Enabled:  true,
				Timezone: tc.timezone,
			}

			s, err := NewScheduler(cfg, msgBus, WithDataDir(tmpDir))
			if tc.wantErr {
				if err == nil {
					t.Error("expected error for invalid timezone")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if s.Location().String() != tc.timezone {
				t.Errorf("expected timezone %q, got %q", tc.timezone, s.Location().String())
			}
		})
	}
}

func TestSchedulerStartStop(t *testing.T) {
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

	// Start
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !s.Running() {
		t.Error("scheduler should be running after Start")
	}

	// Double start should fail
	if err := s.Start(ctx); err == nil {
		t.Error("expected error on double start")
	}

	// Stop
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if s.Running() {
		t.Error("scheduler should not be running after Stop")
	}
}

func TestScheduleJob(t *testing.T) {
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
	defer s.Stop(ctx)

	// Create a reminder job
	jobCfg := JobConfig{
		ID:       "test-job-1",
		Name:     "Test Job",
		Type:     JobTypeReminder,
		Schedule: "@every 1h",
		Enabled:  true,
		ReminderConfig: &ReminderJobConfig{
			Message: "Test reminder",
		},
	}

	jobID, err := s.ScheduleConfig(jobCfg)
	if err != nil {
		t.Fatalf("ScheduleConfig failed: %v", err)
	}

	if jobID != "test-job-1" {
		t.Errorf("expected job ID 'test-job-1', got %q", jobID)
	}

	// Check job count
	if s.JobCount() != 1 {
		t.Errorf("expected 1 job, got %d", s.JobCount())
	}

	// List jobs
	jobs := s.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job in list, got %d", len(jobs))
	}

	if jobs[0].ID != "test-job-1" {
		t.Errorf("expected job ID 'test-job-1', got %q", jobs[0].ID)
	}

	if jobs[0].NextRun == nil {
		t.Error("expected NextRun to be set")
	}
}

func TestUnscheduleJob(t *testing.T) {
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
	defer s.Stop(ctx)

	// Schedule a job
	jobCfg := JobConfig{
		ID:       "test-job-remove",
		Name:     "Test Job",
		Type:     JobTypeReminder,
		Schedule: "@every 1h",
		Enabled:  true,
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	}

	if _, err := s.ScheduleConfig(jobCfg); err != nil {
		t.Fatalf("ScheduleConfig failed: %v", err)
	}

	if s.JobCount() != 1 {
		t.Fatalf("expected 1 job, got %d", s.JobCount())
	}

	// Unschedule
	if err := s.Unschedule("test-job-remove"); err != nil {
		t.Fatalf("Unschedule failed: %v", err)
	}

	if s.JobCount() != 0 {
		t.Errorf("expected 0 jobs, got %d", s.JobCount())
	}

	// Unschedule non-existent job should fail
	if err := s.Unschedule("non-existent"); err == nil {
		t.Error("expected error for non-existent job")
	}
}

func TestPauseResumeJob(t *testing.T) {
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
	defer s.Stop(ctx)

	// Schedule a job
	jobCfg := JobConfig{
		ID:       "test-job-pause",
		Name:     "Test Job",
		Type:     JobTypeReminder,
		Schedule: "@every 1h",
		Enabled:  true,
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	}

	if _, err := s.ScheduleConfig(jobCfg); err != nil {
		t.Fatalf("ScheduleConfig failed: %v", err)
	}

	// Pause
	if err := s.PauseJob("test-job-pause"); err != nil {
		t.Fatalf("PauseJob failed: %v", err)
	}

	// Check enabled status in store
	storedCfg, ok := s.Store().Get("test-job-pause")
	if !ok {
		t.Fatal("job not found in store")
	}
	if storedCfg.Enabled {
		t.Error("expected job to be disabled after pause")
	}

	// Resume
	if err := s.ResumeJob("test-job-pause"); err != nil {
		t.Fatalf("ResumeJob failed: %v", err)
	}

	storedCfg, _ = s.Store().Get("test-job-pause")
	if !storedCfg.Enabled {
		t.Error("expected job to be enabled after resume")
	}
}

func TestCronExpressions(t *testing.T) {
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

	testCases := []struct {
		name     string
		schedule string
		valid    bool
	}{
		// Predefined schedules
		{"@hourly", "@hourly", true},
		{"@daily", "@daily", true},
		{"@weekly", "@weekly", true},
		{"@monthly", "@monthly", true},
		{"@yearly", "@yearly", true},
		{"@annually", "@annually", true},

		// Custom intervals
		{"@every 30m", "@every 30m", true},
		{"@every 1h", "@every 1h", true},
		{"@every 24h", "@every 24h", true},
		{"@every 5s", "@every 5s", true},

		// Standard 5-field cron
		{"5-field minutely", "* * * * *", true},
		{"5-field hourly", "0 * * * *", true},
		{"5-field daily 9am", "0 9 * * *", true},
		{"5-field weekday", "0 9 * * 1-5", true},

		// Extended 6-field cron (with seconds)
		{"6-field with seconds", "0 0 * * * *", true},
		{"6-field every 10 seconds", "*/10 * * * * *", true},

		// Invalid expressions
		{"invalid empty", "", false},
		{"invalid random", "invalid", false},
		{"invalid too many fields", "* * * * * * *", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.ValidateSchedule(tc.schedule)
			if tc.valid && err != nil {
				t.Errorf("expected valid schedule, got error: %v", err)
			}
			if !tc.valid && err == nil {
				t.Error("expected invalid schedule, got no error")
			}
		})
	}
}

func TestJobExecution(t *testing.T) {
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

	// Track execution
	var executed atomic.Bool

	// Subscribe to job completion events
	sub := msgBus.Subscribe("test-sub", "scheduler.job.completed")

	// Schedule a shell job that runs immediately via RunNow
	jobCfg := JobConfig{
		ID:       "test-exec-job",
		Name:     "Test Execution",
		Type:     JobTypeShell,
		Schedule: "@yearly", // Won't actually trigger
		Enabled:  true,
		ShellConfig: &ShellJobConfig{
			Command:    "echo",
			Args:       []string{"hello"},
			CaptureOut: true,
			Timeout:    5 * time.Second,
		},
	}

	if _, err := s.ScheduleConfig(jobCfg); err != nil {
		t.Fatalf("ScheduleConfig failed: %v", err)
	}

	// Run immediately
	if err := s.RunNow("test-exec-job"); err != nil {
		t.Fatalf("RunNow failed: %v", err)
	}

	// Wait for execution
	select {
	case msg := <-sub.Channel:
		executed.Store(true)
		var result map[string]any
		if err := json.Unmarshal(msg.Payload, &result); err != nil {
			t.Fatalf("failed to unmarshal result: %v", err)
		}
		if result["job_id"] != "test-exec-job" {
			t.Errorf("expected job_id 'test-exec-job', got %v", result["job_id"])
		}
		if result["success"] != true {
			t.Errorf("expected success true, got %v", result["success"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for job execution")
	}

	if !executed.Load() {
		t.Error("job was not executed")
	}

	// Give time for job to fully complete before cleanup
	time.Sleep(100 * time.Millisecond)

	// Cleanup
	msgBus.Unsubscribe(sub)
	s.Stop(ctx)
}

func TestShellJob(t *testing.T) {
	tmpDir := t.TempDir()
	msgBus := bus.New(nil, nil)

	cfg := JobConfig{
		ID:       "shell-test",
		Name:     "Shell Test",
		Type:     JobTypeShell,
		Schedule: "@yearly",
		Enabled:  true,
		ShellConfig: &ShellJobConfig{
			Command:    "echo",
			Args:       []string{"hello world"},
			CaptureOut: true,
			Timeout:    5 * time.Second,
		},
	}

	job, err := NewShellJob(cfg, msgBus)
	if err != nil {
		t.Fatalf("NewShellJob failed: %v", err)
	}

	if job.ID() != "shell-test" {
		t.Errorf("expected ID 'shell-test', got %q", job.ID())
	}

	if job.Type() != JobTypeShell {
		t.Errorf("expected type 'shell', got %q", job.Type())
	}

	// Write test output to file
	outFile := filepath.Join(tmpDir, "out.txt")
	cfg.ShellConfig.Command = "sh"
	cfg.ShellConfig.Args = []string{"-c", "echo 'test output' > " + outFile}

	job, _ = NewShellJob(cfg, msgBus)
	ctx := context.Background()

	if err := job.Execute(ctx); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Check output file exists
	if _, err := os.Stat(outFile); os.IsNotExist(err) {
		t.Error("output file was not created")
	}
}

func TestAgentJob(t *testing.T) {
	msgBus := bus.New(nil, nil)

	cfg := JobConfig{
		ID:       "agent-test",
		Name:     "Agent Test",
		Type:     JobTypeAgent,
		Schedule: "@yearly",
		Enabled:  true,
		AgentConfig: &AgentJobConfig{
			Prompt:  "Hello, world!",
			Context: map[string]string{"key": "value"},
			Model:   "test-model",
		},
	}

	job, err := NewAgentJob(cfg, msgBus)
	if err != nil {
		t.Fatalf("NewAgentJob failed: %v", err)
	}

	if job.ID() != "agent-test" {
		t.Errorf("expected ID 'agent-test', got %q", job.ID())
	}

	if job.Type() != JobTypeAgent {
		t.Errorf("expected type 'agent', got %q", job.Type())
	}

	// Subscribe to agent.chat
	sub := msgBus.Subscribe("test", "agent.chat")
	defer msgBus.Unsubscribe(sub)

	ctx := context.Background()
	if err := job.Execute(ctx); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Check message was published
	select {
	case msg := <-sub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}
		if payload["prompt"] != "Hello, world!" {
			t.Errorf("expected prompt 'Hello, world!', got %v", payload["prompt"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestReminderJob(t *testing.T) {
	msgBus := bus.New(nil, nil)

	cfg := JobConfig{
		ID:       "reminder-test",
		Name:     "Reminder Test",
		Type:     JobTypeReminder,
		Schedule: "@yearly",
		Enabled:  true,
		ReminderConfig: &ReminderJobConfig{
			Message:  "Don't forget!",
			Channels: []string{"notification"},
			Priority: "high",
		},
	}

	job, err := NewReminderJob(cfg, msgBus)
	if err != nil {
		t.Fatalf("NewReminderJob failed: %v", err)
	}

	if job.ID() != "reminder-test" {
		t.Errorf("expected ID 'reminder-test', got %q", job.ID())
	}

	if job.Type() != JobTypeReminder {
		t.Errorf("expected type 'reminder', got %q", job.Type())
	}

	// Subscribe to reminder.notification
	sub := msgBus.Subscribe("test", "reminder.notification")
	defer msgBus.Unsubscribe(sub)

	ctx := context.Background()
	if err := job.Execute(ctx); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Check message was published
	select {
	case msg := <-sub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}
		if payload["message"] != "Don't forget!" {
			t.Errorf("expected message 'Don't forget!', got %v", payload["message"])
		}
		if payload["priority"] != "high" {
			t.Errorf("expected priority 'high', got %v", payload["priority"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestValidateJobConfig(t *testing.T) {
	testCases := []struct {
		name    string
		cfg     JobConfig
		wantErr bool
	}{
		{
			name: "valid agent job",
			cfg: JobConfig{
				ID:       "test",
				Name:     "Test",
				Type:     JobTypeAgent,
				Schedule: "@hourly",
				AgentConfig: &AgentJobConfig{
					Prompt: "Hello",
				},
			},
			wantErr: false,
		},
		{
			name: "valid shell job",
			cfg: JobConfig{
				ID:       "test",
				Name:     "Test",
				Type:     JobTypeShell,
				Schedule: "@hourly",
				ShellConfig: &ShellJobConfig{
					Command: "echo",
				},
			},
			wantErr: false,
		},
		{
			name: "valid reminder job",
			cfg: JobConfig{
				ID:       "test",
				Name:     "Test",
				Type:     JobTypeReminder,
				Schedule: "@hourly",
				ReminderConfig: &ReminderJobConfig{
					Message: "Hello",
				},
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			cfg: JobConfig{
				Name:     "Test",
				Type:     JobTypeReminder,
				Schedule: "@hourly",
			},
			wantErr: true,
		},
		{
			name: "missing name",
			cfg: JobConfig{
				ID:       "test",
				Type:     JobTypeReminder,
				Schedule: "@hourly",
			},
			wantErr: true,
		},
		{
			name: "missing schedule",
			cfg: JobConfig{
				ID:   "test",
				Name: "Test",
				Type: JobTypeReminder,
			},
			wantErr: true,
		},
		{
			name: "missing type",
			cfg: JobConfig{
				ID:       "test",
				Name:     "Test",
				Schedule: "@hourly",
			},
			wantErr: true,
		},
		{
			name: "agent without config",
			cfg: JobConfig{
				ID:       "test",
				Name:     "Test",
				Type:     JobTypeAgent,
				Schedule: "@hourly",
			},
			wantErr: true,
		},
		{
			name: "agent without prompt",
			cfg: JobConfig{
				ID:          "test",
				Name:        "Test",
				Type:        JobTypeAgent,
				Schedule:    "@hourly",
				AgentConfig: &AgentJobConfig{},
			},
			wantErr: true,
		},
		{
			name: "shell without command",
			cfg: JobConfig{
				ID:          "test",
				Name:        "Test",
				Type:        JobTypeShell,
				Schedule:    "@hourly",
				ShellConfig: &ShellJobConfig{},
			},
			wantErr: true,
		},
		{
			name: "reminder without message",
			cfg: JobConfig{
				ID:             "test",
				Name:           "Test",
				Type:           JobTypeReminder,
				Schedule:       "@hourly",
				ReminderConfig: &ReminderJobConfig{},
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateJobConfig(tc.cfg)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestJobPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	msgBus := bus.New(nil, nil)

	cfg := config.SchedulerConfig{
		Enabled:  true,
		Timezone: "UTC",
	}

	// Create scheduler and add jobs
	s1, err := NewScheduler(cfg, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}

	ctx := context.Background()
	if err := s1.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Add a job
	jobCfg := JobConfig{
		ID:       "persist-test",
		Name:     "Persistence Test",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		Enabled:  true,
		ReminderConfig: &ReminderJobConfig{
			Message: "Persisted reminder",
		},
	}

	if _, err := s1.ScheduleConfig(jobCfg); err != nil {
		t.Fatalf("ScheduleConfig failed: %v", err)
	}

	// Stop first scheduler
	if err := s1.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Create new scheduler with same data dir
	s2, err := NewScheduler(cfg, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler (2) failed: %v", err)
	}

	if err := s2.Start(ctx); err != nil {
		t.Fatalf("Start (2) failed: %v", err)
	}
	defer s2.Stop(ctx)

	// Check job was loaded
	if s2.JobCount() != 1 {
		t.Errorf("expected 1 job after restart, got %d", s2.JobCount())
	}

	jobs := s2.ListJobs()
	if len(jobs) != 1 || jobs[0].ID != "persist-test" {
		t.Error("persisted job not found after restart")
	}
}
