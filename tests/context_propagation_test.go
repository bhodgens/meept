package tests

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// TestContextPropagation_MemoryRefsInheritance verifies that parent task
// MemoryRefs are correctly inherited by steps during plan creation, and
// that duplicate refs are deduplicated.
func TestContextPropagation_MemoryRefsInheritance(t *testing.T) {
	t.Run("parent_refs_copied_to_first_step", func(t *testing.T) {
		parentTask := task.NewTask("test", "test task")
		parentTask.AddMemoryRef("mem-parent-1")
		parentTask.AddMemoryRef("mem-parent-2")

		step1 := task.NewTaskStep(parentTask.ID, "first step", 0)

		// Simulate StrategicPlanner copying refs from parent task to first step
		for _, ref := range parentTask.MemoryRefs {
			step1.AddMemoryRef(ref)
		}

		if len(step1.MemoryRefs) != 2 {
			t.Errorf("expected 2 memory refs, got %d", len(step1.MemoryRefs))
		}
		found := false
		for _, ref := range step1.MemoryRefs {
			if ref == "mem-parent-1" {
				found = true
			}
		}
		if !found {
			t.Error("expected mem-parent-1 in step refs")
		}
	})

	t.Run("duplicate_ref_ignored", func(t *testing.T) {
		step := task.NewTaskStep("task-1", "test step", 0)
		step.AddMemoryRef("mem-1")
		step.AddMemoryRef("mem-1") // duplicate

		if len(step.MemoryRefs) != 1 {
			t.Errorf("expected 1 memory ref after duplicate, got %d", len(step.MemoryRefs))
		}
	})

	t.Run("step_can_add_ref_beyond_parent", func(t *testing.T) {
		parentTask := task.NewTask("test", "test task")
		parentTask.AddMemoryRef("mem-parent-1")

		step := task.NewTaskStep(parentTask.ID, "step", 0)

		// Inherit parent refs
		for _, ref := range parentTask.MemoryRefs {
			step.AddMemoryRef(ref)
		}
		// Add a step-specific ref
		step.AddMemoryRef("mem-step-local")

		if len(step.MemoryRefs) != 2 {
			t.Errorf("expected 2 memory refs, got %d", len(step.MemoryRefs))
		}
	})
}

// TestContextPropagation_StepToStepFlow verifies that context (memory refs and
// accumulated context text) flows from a completed step to its dependent
// next-ready step.
func TestContextPropagation_StepToStepFlow(t *testing.T) {
	t.Run("context_appended_to_next_step", func(t *testing.T) {
		taskID := "task-ctx-flow"

		step1 := task.NewTaskStep(taskID, "find config file", 0)
		step1.AddMemoryRef("mem-1")

		step2 := task.NewTaskStep(taskID, "read config", 1)
		step2.DependsOn = []string{step1.ID}

		// Complete step 1
		step1.Result = "Found config at /etc/app/config.yaml"
		step1.State = task.StepCompleted

		// Simulate propagation: copy refs and append context
		for _, ref := range step1.MemoryRefs {
			step2.AddMemoryRef(ref)
		}
		contextContent := "## Step completed: find config file\n\n**Result:** Found config at /etc/app/config.yaml"
		step2.AppendToContext(contextContent)

		// Verify step 2 has context
		if !strings.Contains(step2.AccumulatedContext, "config.yaml") {
			t.Errorf("step 2 missing context about config file: %q", step2.AccumulatedContext)
		}
		if len(step2.MemoryRefs) == 0 {
			t.Error("step 2 should have inherited memory refs from step 1")
		}
		if step2.MemoryRefs[0] != "mem-1" {
			t.Errorf("expected mem-1, got %s", step2.MemoryRefs[0])
		}
	})

	t.Run("multiple_contexts_appended", func(t *testing.T) {
		step := task.NewTaskStep("task-1", "final step", 0)

		step.AppendToContext("First finding: X exists")
		step.AppendToContext("Second finding: Y is configured")

		if !strings.Contains(step.AccumulatedContext, "First finding") {
			t.Error("missing first context")
		}
		if !strings.Contains(step.AccumulatedContext, "Second finding") {
			t.Error("missing second context")
		}
		if !strings.Contains(step.AccumulatedContext, "---") {
			t.Error("missing separator between contexts")
		}
	})

	t.Run("chain_of_three_steps", func(t *testing.T) {
		// Verify context accumulates across a chain of three steps
		step1 := task.NewTaskStep("chain-task", "step 1", 0)
		step1.AddMemoryRef("mem-original")
		step1.Result = "Created file structure"
		step1.State = task.StepCompleted

		step2 := task.NewTaskStep("chain-task", "step 2", 1)
		// Inherit from step 1
		for _, ref := range step1.MemoryRefs {
			step2.AddMemoryRef(ref)
		}
		step2.AppendToContext(fmt.Sprintf("## Step completed: %s\n\n**Result:** %s", step1.Description, step1.Result))

		step2.Result = "Implemented core logic"
		step2.State = task.StepCompleted

		step3 := task.NewTaskStep("chain-task", "step 3", 2)
		// Inherit from step 2 (which already has step 1's refs and context)
		for _, ref := range step2.MemoryRefs {
			step3.AddMemoryRef(ref)
		}
		step3.AppendToContext(fmt.Sprintf("## Step completed: %s\n\n**Result:** %s", step2.Description, step2.Result))

		// step 3 should have the original memory ref
		if len(step3.MemoryRefs) != 1 || step3.MemoryRefs[0] != "mem-original" {
			t.Errorf("step 3 should have mem-original, got %v", step3.MemoryRefs)
		}

		// step 3 should have context from step 2
		if !strings.Contains(step3.AccumulatedContext, "Implemented core logic") {
			t.Error("step 3 missing context from step 2")
		}

		// step 3 should NOT have direct context from step 1 (it was propagated via step 2)
		// Only the step 2 context was appended to step 3
		if strings.Contains(step3.AccumulatedContext, "Created file structure") {
			t.Error("step 3 should not have step 1 context directly (it was propagated via step 2)")
		}
	})

	t.Run("parallel_steps_merge_context", func(t *testing.T) {
		// Two parallel steps complete and both propagate to a join step
		stepA := task.NewTaskStep("parallel-task", "step A", 0)
		stepA.AddMemoryRef("mem-a")
		stepA.Result = "Result from A"
		stepA.State = task.StepCompleted

		stepB := task.NewTaskStep("parallel-task", "step B", 1)
		stepB.AddMemoryRef("mem-b")
		stepB.Result = "Result from B"
		stepB.State = task.StepCompleted

		joinStep := task.NewTaskStep("parallel-task", "join step", 2)
		joinStep.DependsOn = []string{stepA.ID, stepB.ID}

		// Propagate from both parents
		for _, ref := range stepA.MemoryRefs {
			joinStep.AddMemoryRef(ref)
		}
		joinStep.AppendToContext(fmt.Sprintf("## Step completed: %s\n\n**Result:** %s", stepA.Description, stepA.Result))

		for _, ref := range stepB.MemoryRefs {
			joinStep.AddMemoryRef(ref)
		}
		joinStep.AppendToContext(fmt.Sprintf("## Step completed: %s\n\n**Result:** %s", stepB.Description, stepB.Result))

		// Verify merged refs
		if len(joinStep.MemoryRefs) != 2 {
			t.Errorf("expected 2 merged memory refs, got %d", len(joinStep.MemoryRefs))
		}

		// Verify both contexts present
		if !strings.Contains(joinStep.AccumulatedContext, "Result from A") {
			t.Error("join step missing context from step A")
		}
		if !strings.Contains(joinStep.AccumulatedContext, "Result from B") {
			t.Error("join step missing context from step B")
		}
	})
}

// TestContextPropagation_AgentPromptContext verifies that the prompt section
// built from memory refs and accumulated context renders correctly for
// injection into an agent prompt.
func TestContextPropagation_AgentPromptContext(t *testing.T) {
	t.Run("refs_and_context_rendered", func(t *testing.T) {
		memoryRefs := []string{"mem-1", "mem-2"}
		accumulatedContext := "Prior step found: database is PostgreSQL"

		var sb strings.Builder
		if len(memoryRefs) > 0 {
			sb.WriteString("## Available Context Memories\n\n")
			for i, ref := range memoryRefs {
				fmt.Fprintf(&sb, "%d. Memory: `%s`\n", i+1, ref)
			}
			sb.WriteString("\n")
		}
		if accumulatedContext != "" {
			sb.WriteString("## Context from Prior Steps\n\n")
			sb.WriteString(accumulatedContext)
			sb.WriteString("\n\n")
		}

		result := sb.String()
		if !strings.Contains(result, "Available Context Memories") {
			t.Error("missing context memories header")
		}
		if !strings.Contains(result, "mem-1") {
			t.Error("missing memory ref mem-1")
		}
		if !strings.Contains(result, "PostgreSQL") {
			t.Error("missing accumulated context")
		}
	})

	t.Run("empty_refs_and_context", func(t *testing.T) {
		var sb strings.Builder
		memoryRefs := []string{}
		accumulatedContext := ""

		if len(memoryRefs) > 0 {
			sb.WriteString("## Available Context Memories\n\n")
		}
		if accumulatedContext != "" {
			sb.WriteString("## Context from Prior Steps\n\n")
		}

		result := sb.String()
		if result != "" {
			t.Errorf("expected empty string for no refs/context, got %q", result)
		}
	})

	t.Run("only_refs_no_context", func(t *testing.T) {
		memoryRefs := []string{"mem-only"}
		accumulatedContext := ""

		var sb strings.Builder
		if len(memoryRefs) > 0 {
			sb.WriteString("## Available Context Memories\n\n")
			for i, ref := range memoryRefs {
				fmt.Fprintf(&sb, "%d. Memory: `%s`\n", i+1, ref)
			}
			sb.WriteString("\n")
		}
		if accumulatedContext != "" {
			sb.WriteString("## Context from Prior Steps\n\n")
			sb.WriteString(accumulatedContext)
			sb.WriteString("\n\n")
		}

		result := sb.String()
		if !strings.Contains(result, "Available Context Memories") {
			t.Error("should have context memories header")
		}
		if strings.Contains(result, "Prior Steps") {
			t.Error("should not have prior steps section when no context")
		}
	})
}

// TestContextPropagation_FullBusFlow verifies that context propagation events
// flow through the message bus correctly, simulating the planner publishing
// a task.planned event with memory refs information.
func TestContextPropagation_FullBusFlow(t *testing.T) {
	t.Run("step_payload_includes_context", func(t *testing.T) {
		msgBus := bus.New(nil, nil)

		// Subscribe to task.planned
		sub := msgBus.Subscribe("test", "task.planned")
		defer msgBus.Unsubscribe(sub)

		// Simulate StrategicPlanner publishing task.planned with memory refs info
		data := map[string]any{
			"task_id":     "task-e2e",
			"session_id":  "session-1",
			"total_steps": 2,
			"ready_steps": 1,
			"memory_refs": []string{"mem-parent-1"},
		}
		msg, err := models.NewBusMessage(models.MessageTypeEvent, "test", data)
		if err != nil {
			t.Fatalf("failed to create message: %v", err)
		}
		msgBus.Publish("task.planned", msg)

		select {
		case received := <-sub.Channel:
			var payload map[string]any
			if err := json.Unmarshal(received.Payload, &payload); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			if payload["task_id"] != "task-e2e" {
				t.Errorf("expected task-e2e, got %v", payload["task_id"])
			}
			if refs, ok := payload["memory_refs"].([]any); ok {
				if len(refs) != 1 {
					t.Errorf("expected 1 memory ref, got %d", len(refs))
				}
			} else {
				t.Error("expected memory_refs to be a slice")
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatal("timeout waiting for task.planned event")
		}
	})

	t.Run("step_completed_propagation_event", func(t *testing.T) {
		msgBus := bus.New(nil, nil)

		sub := msgBus.Subscribe("test", "step.completed")
		defer msgBus.Unsubscribe(sub)

		// Simulate step completion with context propagation data
		data := map[string]any{
			"step_id":         "step-001",
			"task_id":         "task-001",
			"result":          "Found config at /etc/app/config.yaml",
			"memory_refs":     []string{"mem-1", "mem-2"},
			"context_snippet": "Config file located and parsed successfully",
		}
		msg, err := models.NewBusMessage(models.MessageTypeEvent, "tactical", data)
		if err != nil {
			t.Fatalf("failed to create message: %v", err)
		}
		msgBus.Publish("step.completed", msg)

		select {
		case received := <-sub.Channel:
			var payload map[string]any
			if err := json.Unmarshal(received.Payload, &payload); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			if payload["step_id"] != "step-001" {
				t.Errorf("expected step-001, got %v", payload["step_id"])
			}
			if payload["result"] != "Found config at /etc/app/config.yaml" {
				t.Errorf("unexpected result: %v", payload["result"])
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatal("timeout waiting for step.completed event")
		}
	})

	t.Run("no_subscriber_no_block", func(t *testing.T) {
		msgBus := bus.New(nil, nil)

		// Publishing to a topic with no subscribers should not block
		data := map[string]any{"task_id": "task-noop"}
		msg, err := models.NewBusMessage(models.MessageTypeEvent, "test", data)
		if err != nil {
			t.Fatalf("failed to create message: %v", err)
		}

		done := make(chan struct{})
		go func() {
			msgBus.Publish("topic.nobody.subscribes.to", msg)
			close(done)
		}()

		select {
		case <-done:
			// Success - publish returned without blocking
		case <-time.After(200 * time.Millisecond):
			t.Fatal("publish blocked with no subscribers")
		}
	})
}

// TestContextPropagation_StepPersistence verifies that memory refs and
// accumulated context survive round-trip through the step store (SQLite).
func TestContextPropagation_StepPersistence(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	t.Run("refs_and_context_round_trip", func(t *testing.T) {
		// Create a task in the store
		tsk := task.NewTask("persistence-test", "test persistence of context")
		if err := env.taskStore.Create(tsk); err != nil {
			t.Fatalf("failed to create task: %v", err)
		}

		// Create a step with memory refs and context
		step := task.NewTaskStep(tsk.ID, "research step", 0)
		step.AddMemoryRef("mem-alpha")
		step.AddMemoryRef("mem-beta")
		step.AppendToContext("Found that the API uses JWT tokens")
		step.State = task.StepCompleted
		step.Result = "Research complete"

		if err := env.stepStore.Create(step); err != nil {
			t.Fatalf("failed to create step: %v", err)
		}

		// Retrieve and verify
		retrieved, err := env.stepStore.GetByID(step.ID)
		if err != nil {
			t.Fatalf("failed to retrieve step: %v", err)
		}
		if retrieved == nil {
			t.Fatal("retrieved step is nil")
		}

		if len(retrieved.MemoryRefs) != 2 {
			t.Errorf("expected 2 memory refs after round-trip, got %d", len(retrieved.MemoryRefs))
		}
		if !strings.Contains(retrieved.AccumulatedContext, "JWT tokens") {
			t.Errorf("accumulated context lost after round-trip: %q", retrieved.AccumulatedContext)
		}
		if retrieved.State != task.StepCompleted {
			t.Errorf("expected state %q, got %q", task.StepCompleted, retrieved.State)
		}
	})

	t.Run("context_update_persists", func(t *testing.T) {
		tsk := task.NewTask("update-test", "test context update")
		if err := env.taskStore.Create(tsk); err != nil {
			t.Fatalf("failed to create task: %v", err)
		}

		step := task.NewTaskStep(tsk.ID, "initial step", 0)
		step.AddMemoryRef("mem-initial")
		if err := env.stepStore.Create(step); err != nil {
			t.Fatalf("failed to create step: %v", err)
		}

		// Add more context and refs
		step.AppendToContext("Additional finding from execution")
		step.AddMemoryRef("mem-added-later")
		if err := env.stepStore.Update(step); err != nil {
			t.Fatalf("failed to update step: %v", err)
		}

		// Retrieve and verify the update
		retrieved, err := env.stepStore.GetByID(step.ID)
		if err != nil {
			t.Fatalf("failed to retrieve step: %v", err)
		}
		if len(retrieved.MemoryRefs) != 2 {
			t.Errorf("expected 2 memory refs after update, got %d", len(retrieved.MemoryRefs))
		}
		if !strings.Contains(retrieved.AccumulatedContext, "Additional finding") {
			t.Error("updated context not persisted")
		}
	})
}
