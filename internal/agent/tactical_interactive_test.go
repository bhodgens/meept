package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
)

// capturedQueue records enqueued jobs so tests can assert on the stamped
// Interactive flag and payload SessionID (tree 04 leaf 02, audit R4).
type capturedQueue struct {
	mockQueue
	jobs []*queue.Job
}

func (c *capturedQueue) Enqueue(_ context.Context, job *queue.Job) error {
	c.jobs = append(c.jobs, job)
	return nil
}

func newInteractiveTacticalFixture(t *testing.T) (*TacticalScheduler, *capturedQueue, *task.Store) {
	t.Helper()
	taskStore, stepStore := newTestTaskAndStepStore(t)

	cq := &capturedQueue{}
	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Queue:     cq,
		Bus:       bus.New(nil, nil),
		Logger:    slogDiscardLogger(),
	})
	return scheduler, cq, taskStore
}

// seedInteractiveReadyStep creates a task (optionally linked to sessionID)
// with one ready step the scheduler can schedule.
func seedInteractiveReadyStep(t *testing.T, taskStore *task.Store, sessionID string) *task.TaskStep {
	t.Helper()
	tk := task.NewTask("task-interactive-1", "interactive stamping test")
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if sessionID != "" {
		if err := taskStore.LinkSession(tk.ID, sessionID); err != nil {
			t.Fatalf("link session: %v", err)
		}
	}
	step := task.NewTaskStep(tk.ID, "interactive step", 1)
	step.State = task.StepReady
	step.ToolHint = "code"
	if err := taskStore.StepStore().Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}
	return step
}

// TestScheduleStep_InteractiveStampFromLinkedSession pins R4 (a)+(b): the
// scheduled step job's payload carries the task's linked session, and when
// that session is interactive the job is stamped Interactive=true.
func TestScheduleStep_InteractiveStampFromLinkedSession(t *testing.T) {
	scheduler, captured, taskStore := newInteractiveTacticalFixture(t)

	// Interactive origin session: recent user message within the window.
	sess := session.NewMemoryStore(nil)
	created, err := sess.Create("origin")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	originID := created.ID
	if err := sess.SetLastUserMessage(originID, time.Now().UTC()); err != nil {
		t.Fatalf("set last user message: %v", err)
	}

	step := seedInteractiveReadyStep(t, taskStore, originID)
	scheduler.SetSessionStore(sess)

	if err := scheduler.ScheduleReadySteps(context.Background(), step.TaskID); err != nil {
		t.Fatalf("ScheduleReadySteps: %v", err)
	}
	if len(captured.jobs) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(captured.jobs))
	}

	job := captured.jobs[0]
	if !job.Interactive {
		t.Errorf("step job Interactive = false, want true (linked session had a recent user message)")
	}

	// R4 (a): the payload carries the originating session ID.
	var payload StepJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.SessionID != originID {
		t.Errorf("payload SessionID = %q, want %q", payload.SessionID, originID)
	}

	// R4 (a): the session provenance is persisted on the step store row.
	stored, err := taskStore.StepStore().GetByID(step.ID)
	if err != nil {
		t.Fatalf("get stored step: %v", err)
	}
	if stored.SessionID != originID {
		t.Errorf("stored step SessionID = %q, want %q", stored.SessionID, originID)
	}
}

// TestScheduleStep_BackgroundWhenSessionQuiet pins the negative case: a
// linked session past the window (Q1 default) stamps Interactive=false.
func TestScheduleStep_BackgroundWhenSessionQuiet(t *testing.T) {
	scheduler, captured, taskStore := newInteractiveTacticalFixture(t)

	sess := session.NewMemoryStore(nil)
	quiet, err := sess.Create("quiet")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	// Last user message 10 minutes ago — outside the 5m Q1 window, and not
	// foreground. Per audit R4 the stamp is evaluated once, at enqueue.
	if err := sess.SetLastUserMessage(quiet.ID, time.Now().UTC().Add(-10*time.Minute)); err != nil {
		t.Fatalf("set last user message: %v", err)
	}

	step := seedInteractiveReadyStep(t, taskStore, quiet.ID)
	scheduler.SetSessionStore(sess)

	if err := scheduler.ScheduleReadySteps(context.Background(), step.TaskID); err != nil {
		t.Fatalf("ScheduleReadySteps: %v", err)
	}
	if len(captured.jobs) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(captured.jobs))
	}
	if captured.jobs[0].Interactive {
		t.Errorf("quiet-session job Interactive = true, want false")
	}
}

// TestScheduleStep_NoSessionStampsFalse pins R4 (c): tasks with no linked
// session stamp Interactive=false BY CONSTRUCTION — documented accepted
// semantics, not a gap.
func TestScheduleStep_NoSessionStampsFalse(t *testing.T) {
	scheduler, captured, taskStore := newInteractiveTacticalFixture(t)

	step := seedInteractiveReadyStep(t, taskStore, "")
	scheduler.SetSessionStore(nil)

	if err := scheduler.ScheduleReadySteps(context.Background(), step.TaskID); err != nil {
		t.Fatalf("ScheduleReadySteps: %v", err)
	}
	if len(captured.jobs) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(captured.jobs))
	}
	if captured.jobs[0].Interactive {
		t.Errorf("session-less job Interactive = true, want false by construction (R4)")
	}
}

// TestForegroundSessionStampsInteractive covers the second D11 input: the
// client-declared foreground flag qualifies even without a recent message.
func TestForegroundSessionStampsInteractive(t *testing.T) {
	scheduler, captured, taskStore := newInteractiveTacticalFixture(t)

	sess := session.NewMemoryStore(nil)
	fg, err := sess.Create("fg")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := sess.SetForeground(fg.ID, true); err != nil {
		t.Fatalf("set foreground: %v", err)
	}

	step := seedInteractiveReadyStep(t, taskStore, fg.ID)
	scheduler.SetSessionStore(sess)

	if err := scheduler.ScheduleReadySteps(context.Background(), step.TaskID); err != nil {
		t.Fatalf("ScheduleReadySteps: %v", err)
	}
	if len(captured.jobs) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(captured.jobs))
	}
	if !captured.jobs[0].Interactive {
		t.Errorf("foreground-session job Interactive = false, want true")
	}
}

// TestValidationRetryJob_InheritsInteractiveStamp pins that the
// validation-retry path rebuilds the payload from the persisted step (which
// now carries SessionID) and re-stamps consistently.
func TestValidationRetryJob_InheritsInteractiveStamp(t *testing.T) {
	// The retry payload construction lives in handleStepExecution; here we
	// pin the building block: payloadFromStep restores SessionID from the
	// stored step so retry jobs re-stamp identically to first-schedule jobs.
	step := task.NewTaskStep("t-retry", "retry step", 1)
	step.SessionID = "origin-retry"

	payload := stepJobPayloadFromStep(step)
	if payload.SessionID != "origin-retry" {
		t.Errorf("stepJobPayloadFromStep SessionID = %q, want %q", payload.SessionID, "origin-retry")
	}
}
