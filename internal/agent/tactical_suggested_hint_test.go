package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// TestStepJobPayload_SuggestedNextHintRoundTrip pins the successor-hints
// payload contract (agent-routing tree leaf 03): stepJobPayloadFromStep
// carries step.SuggestedNextHint onto the queue payload with the
// suggested_next_hint json tag, and the payload unmarshals back onto a step
// carrying the same hint — the round trip survives the queue boundary.
// omitempty must keep an empty hint out of the wire JSON entirely.
func TestStepJobPayload_SuggestedNextHintRoundTrip(t *testing.T) {
	t.Run("step to payload carries hint", func(t *testing.T) {
		step := task.NewTaskStep("task-hint", "finished step", 0)
		step.SuggestedNextHint = "debugger"

		payload := stepJobPayloadFromStep(step)
		if payload.SuggestedNextHint != "debugger" {
			t.Errorf("payload.SuggestedNextHint = %q, want %q", payload.SuggestedNextHint, "debugger")
		}

		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		if !strings.Contains(string(data), `"suggested_next_hint":"debugger"`) {
			t.Errorf("marshaled payload missing suggested_next_hint: %s", data)
		}
	})

	t.Run("payload to step carries hint", func(t *testing.T) {
		step := task.NewTaskStep("task-hint", "finished step", 0)
		step.SuggestedNextHint = "reviewer"
		payload := stepJobPayloadFromStep(step)

		wire, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}

		var decoded StepJobPayload
		if err := json.Unmarshal(wire, &decoded); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if decoded.SuggestedNextHint != "reviewer" {
			t.Errorf("decoded.SuggestedNextHint = %q, want %q", decoded.SuggestedNextHint, "reviewer")
		}
	})

	t.Run("empty stays empty with omitempty", func(t *testing.T) {
		step := task.NewTaskStep("task-hint", "step without hint", 0)
		payload := stepJobPayloadFromStep(step)

		if payload.SuggestedNextHint != "" {
			t.Errorf("payload.SuggestedNextHint = %q, want empty", payload.SuggestedNextHint)
		}

		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		if strings.Contains(string(data), "suggested_next_hint") {
			t.Errorf("empty hint serialized into payload JSON (omitempty broken): %s", data)
		}
	})
}

// TestTacticalScheduler_OnJobCompletedAdoptsSuggestedNextHint pins the
// successor-hints adoption contract (agent-routing tree leaf 03): a
// completion envelope carrying suggested_next_hint persists it onto the
// TaskStep and publishes exactly one routing.telemetry event with
// step_id/task_id/suggested_next_hint. An envelope WITHOUT the field must
// not publish anything and must leave the persisted field untouched.
func TestTacticalScheduler_OnJobCompletedAdoptsSuggestedNextHint(t *testing.T) {
	t.Run("envelope with hint persists and emits telemetry", func(t *testing.T) {
		ts, msgBus, cleanup := newTacticalTestSetup(t)
		defer cleanup()

		telemetrySub := msgBus.Subscribe("test-hint-telemetry", "routing.telemetry")
		defer msgBus.Unsubscribe(telemetrySub)

		parentTask := task.NewTask("hint-task-1", "task with a hinting step")
		parentTask.TotalJobs = 1
		parentTask.SetState(task.StateExecuting)
		if err := ts.taskStore.Create(parentTask); err != nil {
			t.Fatalf("failed to create task: %v", err)
		}

		step := task.NewTaskStep(parentTask.ID, "investigate the flake", 0)
		if err := ts.stepStore.Create(step); err != nil {
			t.Fatalf("failed to create step: %v", err)
		}
		if err := ts.stepStore.SetJobID(step.ID, "job-hint-adopt"); err != nil {
			t.Fatalf("failed to set job ID: %v", err)
		}

		resultJSON, _ := json.Marshal(map[string]any{
			"success":             true,
			"result":              "investigation done",
			"suggested_next_hint": "debugger",
		})
		if err := ts.OnJobCompleted(t.Context(), "job-hint-adopt", resultJSON); err != nil {
			t.Fatalf("OnJobCompleted: %v", err)
		}

		got, err := ts.stepStore.GetByID(step.ID)
		if err != nil {
			t.Fatalf("failed to reload step: %v", err)
		}
		if got.SuggestedNextHint != "debugger" {
			t.Errorf("persisted SuggestedNextHint = %q, want %q", got.SuggestedNextHint, "debugger")
		}

		select {
		case msg := <-telemetrySub.Channel:
			var payload map[string]any
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("failed to unmarshal routing.telemetry event: %v", err)
			}
			if payload["step_id"] != step.ID {
				t.Errorf("routing.telemetry step_id = %v, want %s", payload["step_id"], step.ID)
			}
			if payload["task_id"] != parentTask.ID {
				t.Errorf("routing.telemetry task_id = %v, want %s", payload["task_id"], parentTask.ID)
			}
			if payload["suggested_next_hint"] != "debugger" {
				t.Errorf("routing.telemetry suggested_next_hint = %v, want \"debugger\"", payload["suggested_next_hint"])
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for routing.telemetry event")
		}

		// Exactly one event: a duplicated publish would double-count usage
		// metrics — the whole point of the telemetry.
		select {
		case msg := <-telemetrySub.Channel:
			t.Errorf("unexpected second routing.telemetry event: %v", msg.Payload)
		case <-time.After(100 * time.Millisecond):
		}
	})

	t.Run("envelope without hint stays empty and silent", func(t *testing.T) {
		ts, msgBus, cleanup := newTacticalTestSetup(t)
		defer cleanup()

		telemetrySub := msgBus.Subscribe("test-hint-silent", "routing.telemetry")
		defer msgBus.Unsubscribe(telemetrySub)

		parentTask := task.NewTask("hint-task-2", "task without hints")
		parentTask.TotalJobs = 1
		parentTask.SetState(task.StateExecuting)
		if err := ts.taskStore.Create(parentTask); err != nil {
			t.Fatalf("failed to create task: %v", err)
		}

		step := task.NewTaskStep(parentTask.ID, "plain step", 0)
		if err := ts.stepStore.Create(step); err != nil {
			t.Fatalf("failed to create step: %v", err)
		}
		if err := ts.stepStore.SetJobID(step.ID, "job-hint-absent"); err != nil {
			t.Fatalf("failed to set job ID: %v", err)
		}

		resultJSON, _ := json.Marshal(map[string]any{"success": true, "result": "done"})
		if err := ts.OnJobCompleted(t.Context(), "job-hint-absent", resultJSON); err != nil {
			t.Fatalf("OnJobCompleted: %v", err)
		}

		got, err := ts.stepStore.GetByID(step.ID)
		if err != nil {
			t.Fatalf("failed to reload step: %v", err)
		}
		if got.SuggestedNextHint != "" {
			t.Errorf("SuggestedNextHint = %q, want empty (no hint in envelope)", got.SuggestedNextHint)
		}

		select {
		case msg := <-telemetrySub.Channel:
			t.Errorf("unexpected routing.telemetry event without a hint: %v", msg.Payload)
		case <-time.After(300 * time.Millisecond):
		}
	})
}
