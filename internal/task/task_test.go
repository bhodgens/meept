package task

import (
	"testing"
)

func TestNewTask(t *testing.T) {
	task := NewTask("test-task", "a test task")

	if task.ID == "" {
		t.Error("expected ID to be set")
	}
	if task.Name != "test-task" {
		t.Errorf("expected name %q, got %q", "test-task", task.Name)
	}
	if task.Description != "a test task" {
		t.Errorf("expected description %q, got %q", "a test task", task.Description)
	}
	if task.State != StatePending {
		t.Errorf("expected state %q, got %q", StatePending, task.State)
	}
	if task.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestTask_Progress(t *testing.T) {
	task := NewTask("test", "test")

	if task.Progress() != 0 {
		t.Errorf("expected 0%% progress with no jobs, got %.1f%%", task.Progress())
	}

	task.TotalJobs = 4
	task.CompletedJobs = 1
	if got := task.Progress(); got != 25 {
		t.Errorf("expected 25%% progress, got %.1f%%", got)
	}

	task.CompletedJobs = 4
	if got := task.Progress(); got != 100 {
		t.Errorf("expected 100%% progress, got %.1f%%", got)
	}
}

func TestTask_StatePredicates(t *testing.T) {
	task := NewTask("test", "test")

	if !task.IsPending() {
		t.Error("new task should be pending")
	}
	if task.IsActive() {
		t.Error("new task should not be active")
	}
	if task.IsComplete() {
		t.Error("new task should not be complete")
	}

	task.SetState(StateExecuting)
	if task.IsPending() {
		t.Error("executing task should not be pending")
	}
	if !task.IsActive() {
		t.Error("executing task should be active")
	}

	task.SetState(StateCompleted)
	if !task.IsComplete() {
		t.Error("completed task should be complete")
	}
	if task.IsActive() {
		t.Error("completed task should not be active")
	}
}

func TestTask_LinkSession(t *testing.T) {
	task := NewTask("test", "test")

	task.LinkSession("session-1")
	if len(task.LinkedSessions) != 1 {
		t.Errorf("expected 1 linked session, got %d", len(task.LinkedSessions))
	}

	// Duplicate should be ignored
	task.LinkSession("session-1")
	if len(task.LinkedSessions) != 1 {
		t.Errorf("expected 1 linked session after duplicate, got %d", len(task.LinkedSessions))
	}

	task.LinkSession("session-2")
	if len(task.LinkedSessions) != 2 {
		t.Errorf("expected 2 linked sessions, got %d", len(task.LinkedSessions))
	}
}

func TestTask_UnlinkSession(t *testing.T) {
	task := NewTask("test", "test")
	task.LinkSession("session-1")
	task.LinkSession("session-2")

	task.UnlinkSession("session-1")
	if len(task.LinkedSessions) != 1 {
		t.Errorf("expected 1 linked session after unlink, got %d", len(task.LinkedSessions))
	}
	if task.LinkedSessions[0] != "session-2" {
		t.Errorf("expected remaining session to be session-2, got %s", task.LinkedSessions[0])
	}

	// Unlinking non-existent should be a no-op
	task.UnlinkSession("session-999")
	if len(task.LinkedSessions) != 1 {
		t.Errorf("expected 1 linked session after unlinking non-existent, got %d", len(task.LinkedSessions))
	}
}

func TestTask_JobTracking(t *testing.T) {
	task := NewTask("test", "test")

	task.IncrementJobs()
	task.IncrementJobs()
	task.IncrementJobs()

	if task.TotalJobs != 3 {
		t.Errorf("expected 3 total jobs, got %d", task.TotalJobs)
	}

	task.CompleteJob()
	if task.CompletedJobs != 1 {
		t.Errorf("expected 1 completed job, got %d", task.CompletedJobs)
	}

	task.FailJob()
	if task.FailedJobs != 1 {
		t.Errorf("expected 1 failed job, got %d", task.FailedJobs)
	}
}

func TestTask_Summary(t *testing.T) {
	task := NewTask("test-task", "test description")
	task.TotalJobs = 10
	task.CompletedJobs = 7
	task.LinkSession("s1")
	task.LinkSession("s2")

	summary := task.Summary()

	if summary.ID != task.ID {
		t.Errorf("expected summary ID %q, got %q", task.ID, summary.ID)
	}
	if summary.Name != "test-task" {
		t.Errorf("expected summary name %q, got %q", "test-task", summary.Name)
	}
	if summary.State != StatePending {
		t.Errorf("expected summary state %q, got %q", StatePending, summary.State)
	}
	if summary.Progress != 70 {
		t.Errorf("expected summary progress 70, got %.1f", summary.Progress)
	}
	if summary.LinkedSessions != 2 {
		t.Errorf("expected 2 linked sessions in summary, got %d", summary.LinkedSessions)
	}
}

func TestTask_WithBuilders(t *testing.T) {
	task := NewTask("test", "test").
		WithProjectDir("/tmp/project").
		WithWorkspaceDir("/tmp/workspace").
		WithGitRepo("https://github.com/test/repo").
		WithMemvidZone("task-test")

	if task.ProjectDir != "/tmp/project" {
		t.Errorf("expected project dir %q, got %q", "/tmp/project", task.ProjectDir)
	}
	if task.WorkspaceDir != "/tmp/workspace" {
		t.Errorf("expected workspace dir %q, got %q", "/tmp/workspace", task.WorkspaceDir)
	}
	if task.GitRepo != "https://github.com/test/repo" {
		t.Errorf("expected git repo %q, got %q", "https://github.com/test/repo", task.GitRepo)
	}
	if task.MemvidZone != "task-test" {
		t.Errorf("expected memvid zone %q, got %q", "task-test", task.MemvidZone)
	}
}

func TestTaskState_IsTerminal(t *testing.T) {
	tests := []struct {
		state    TaskState
		terminal bool
	}{
		{StatePending, false},
		{StatePlanning, false},
		{StateExecuting, false},
		{StateTesting, false},
		{StateCompleted, true},
		{StateFailed, true},
		{StateCancelled, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsTerminal(); got != tt.terminal {
				t.Errorf("TaskState(%s).IsTerminal() = %v, want %v", tt.state, got, tt.terminal)
			}
		})
	}
}

func TestTask_TokenUsage(t *testing.T) {
	task := NewTask("test", "test task")

	// Initial state
	if task.TokenUsage != 0 {
		t.Errorf("expected 0 initial tokens, got %d", task.TokenUsage)
	}

	// Add tokens
	task.AddTokenUsage(1500)
	if task.TokenUsage != 1500 {
		t.Errorf("expected 1500 tokens, got %d", task.TokenUsage)
	}

	// Add more tokens
	task.AddTokenUsage(500)
	if task.TokenUsage != 2000 {
		t.Errorf("expected 2000 tokens, got %d", task.TokenUsage)
	}
}
