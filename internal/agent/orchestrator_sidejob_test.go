package agent

// 04-clutter TEST-GATE T4 (F11a): side-job completion noise pin.
//
// Frozen defect: F-ORCH-6 (MINOR, live-verified). orchestrator ERROR spam
// "failed to find step for job ...: step not found" after EVERY task
// completion - memory-capture/ambient side jobs carry empty step_id and
// task_id, so getStepByJobID returns ErrStepNotFound and the orchestrator
// logs the miss at ERROR (daemon.log 15:52:47/48, 15:53:28/29; two jobs per
// task). Log noise; the completion handler is invoked for non-step jobs it
// cannot map.
//
// Contract (01-server-reply-hygiene T4): a side-job completion (no step
// mapping) is quiet - Debug at most, never ERROR, and no chat event. Real
// step failures stay loud (task.error / task.failed keep flowing).
//
// RED today: handleJobCompleted logs "Failed to handle job completion" at
// ERROR for the unmapped side job. GREEN after 04-clutter/01.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// sideJobLogCapture is a concurrency-safe slog sink recording every record
// at Debug and above (the handler may run on a bus goroutine).
type sideJobLogCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *sideJobLogCapture) handler() slog.Handler {
	return &sideJobLogHandler{capture: c}
}

type sideJobLogHandler struct {
	capture *sideJobLogCapture
	attrs   []slog.Attr
}

func (h *sideJobLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *sideJobLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.capture.mu.Lock()
	defer h.capture.mu.Unlock()
	h.capture.records = append(h.capture.records, r.Clone())
	return nil
}

func (h *sideJobLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &sideJobLogHandler{capture: h.capture, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

func (h *sideJobLogHandler) WithGroup(string) slog.Handler { return h }

func (c *sideJobLogCapture) errorRecords() []slog.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []slog.Record
	for _, r := range c.records {
		if r.Level >= slog.LevelError {
			out = append(out, r)
		}
	}
	return out
}

// sideJobCompletedMsg builds one queue.job.completed bus message.
func sideJobCompletedMsg(t *testing.T, jobID string) *models.BusMessage {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"job_id": jobID,
		"result": map[string]any{"ok": true},
	})
	if err != nil {
		t.Fatalf("marshal job completed payload: %v", err)
	}
	return &models.BusMessage{
		ID:        "evt-sidejob-" + jobID,
		Type:      models.MessageTypeEvent,
		Topic:     "queue.job.completed",
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

// newSideJobOrchestrator wires an orchestrator with a real tactical
// scheduler over real stores and a no-op queue (mockQueue.Get returns nil,
// so extractTaskIDFromJob finds nothing - the side-job shape).
func newSideJobOrchestrator(t *testing.T, logger *slog.Logger) (*Orchestrator, *task.Store, *task.StepStore, *bus.MessageBus) {
	t.Helper()
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	t.Cleanup(func() { msgBus.Close() })
	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Queue:     &mockQueue{},
		Bus:       msgBus,
		Logger:    logger,
	})
	o := NewOrchestrator(OrchestratorDeps{
		Tactical:  ts,
		Bus:       msgBus,
		Logger:    logger,
		StepStore: stepStore,
	})
	return o, taskStore, stepStore, msgBus
}

// TestSideJobCompletionQuiet pins F-ORCH-6: unmapped side-job completions
// are quiet; real step failures stay loud. RED today: ERROR spam per side
// job completion.
func TestSideJobCompletionQuiet(t *testing.T) {
	t.Run("memory_capture_completion_logs_no_error", func(t *testing.T) {
		capture := &sideJobLogCapture{}
		logger := slog.New(capture.handler())
		o, _, _, _ := newSideJobOrchestrator(t, logger)

		// The live shape: a memory.l1_extract side job with no step row.
		o.handleJobCompleted(context.Background(), sideJobCompletedMsg(t, "job-memory-l1-extract-0001"))

		for _, r := range capture.errorRecords() {
			t.Errorf("F-ORCH-6: side-job completion logged at ERROR: %q (attrs: %v) - "+
				"unmapped side jobs must be Debug-quiet", r.Message, recordAttrs(r))
		}
	})

	t.Run("side_job_completion_emits_no_chat_event", func(t *testing.T) {
		o, _, _, msgBus := newSideJobOrchestrator(t, slogDiscardLogger())
		var subs []*bus.Subscriber
		for _, topic := range []string{"chat.response", "chat_message", "task.error", "task.completed", "task.failed"} {
			sub := msgBus.Subscribe("sidejob-t4-"+topic, topic)
			subs = append(subs, sub)
			defer msgBus.Unsubscribe(sub)
		}

		o.handleJobCompleted(context.Background(), sideJobCompletedMsg(t, "job-memory-ambient-0002"))

		deadline := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(deadline) {
			for _, sub := range subs {
				select {
				case msg := <-sub.Channel:
					t.Fatalf("side-job completion emitted a user-facing event on %q: %s", msg.Topic, msg.Payload)
				default:
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	})

	t.Run("repeated_side_jobs_stay_quiet", func(t *testing.T) {
		// The live spam was TWO side jobs per task completion, every task.
		capture := &sideJobLogCapture{}
		logger := slog.New(capture.handler())
		o, _, _, _ := newSideJobOrchestrator(t, logger)
		for i := 0; i < 4; i++ {
			o.handleJobCompleted(context.Background(), sideJobCompletedMsg(t, "job-memory-batch-"+string(rune('a'+i))))
		}
		if errs := capture.errorRecords(); len(errs) != 0 {
			t.Errorf("F-ORCH-6: %d ERROR records across 4 side-job completions (want 0)", len(errs))
		}
	})

	t.Run("real_step_failure_stays_loud", func(t *testing.T) {
		// Regression guard (GREEN today, must survive the fix): a REAL
		// step-backed job failure still surfaces task.error to chat and
		// terminalizes the step - the quiet gate must not swallow it.
		o, taskStore, stepStore, msgBus := newSideJobOrchestrator(t, slogDiscardLogger())
		tk := task.NewTask("task-sidejob-t4-real", "real step failure task")
		if err := taskStore.Create(tk); err != nil {
			t.Fatalf("create task: %v", err)
		}
		step := task.NewTaskStep(tk.ID, "the real step", 1)
		step.JobID = "job-real-step-t4"
		step.State = task.StepScheduled
		if err := stepStore.Create(step); err != nil {
			t.Fatalf("create step: %v", err)
		}

		errSub := msgBus.Subscribe("sidejob-t4-real-error", "task.error")
		defer msgBus.Unsubscribe(errSub)

		payload, err := json.Marshal(map[string]any{
			"job_id": "job-real-step-t4",
			"error":  "tool execution exploded",
		})
		if err != nil {
			t.Fatalf("marshal job failed payload: %v", err)
		}
		o.handleJobFailed(context.Background(), &models.BusMessage{
			ID:        "evt-sidejob-t4-failed",
			Type:      models.MessageTypeEvent,
			Topic:     "queue.job.failed",
			Source:    "test",
			Timestamp: time.Now().UTC(),
			Payload:   payload,
		})

		select {
		case msg := <-errSub.Channel:
			var data map[string]any
			if err := json.Unmarshal(msg.Payload, &data); err != nil {
				t.Fatalf("task.error decode: %v", err)
			}
			if data["task_id"] != tk.ID {
				t.Errorf("task.error for wrong task: %v", data)
			}
			if visible, _ := data["chat_visible"].(bool); !visible {
				t.Errorf("real step failure must stay chat-visible: %v", data)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("real step failure emitted no task.error - the quiet gate must not swallow real failures")
		}

		got, err := stepStore.GetByID(step.ID)
		if err != nil {
			t.Fatalf("reload step: %v", err)
		}
		if got.State != task.StepFailed {
			t.Errorf("step state = %q, want failed", got.State)
		}
	})
}

// recordAttrs renders a record's attributes for failure messages.
func recordAttrs(r slog.Record) string {
	var sb strings.Builder
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteString(a.Key)
		sb.WriteString("=")
		sb.WriteString(a.Value.String())
		sb.WriteString(" ")
		return true
	})
	return sb.String()
}
