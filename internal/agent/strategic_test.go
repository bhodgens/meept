package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

func TestExtractJSON_DirectJSON(t *testing.T) {
	input := `{"steps": [{"description": "do something", "tool_hint": "code"}]}`
	got := extractJSON(input)
	if got != input {
		t.Errorf("expected direct JSON, got %q", got)
	}
}

func TestExtractJSON_MarkdownFence(t *testing.T) {
	input := "Here is the plan:\n```json\n{\"steps\": [{\"description\": \"do something\"}]}\n```\nDone."
	got := extractJSON(input)
	if got != `{"steps": [{"description": "do something"}]}` {
		t.Errorf("unexpected result: %q", got)
	}
}

func TestExtractJSON_GenericFence(t *testing.T) {
	input := "Plan:\n```\n{\"steps\": [{\"description\": \"x\"}]}\n```"
	got := extractJSON(input)
	if got != `{"steps": [{"description": "x"}]}` {
		t.Errorf("unexpected result: %q", got)
	}
}

func TestExtractJSON_BraceExtraction(t *testing.T) {
	input := "Sure, here is your plan: {\"steps\": [{\"description\": \"test\"}]} I hope this helps!"
	got := extractJSON(input)
	if got != `{"steps": [{"description": "test"}]}` {
		t.Errorf("unexpected result: %q", got)
	}
}

func TestExtractJSON_NoJSON(t *testing.T) {
	got := extractJSON("This is just plain text with no JSON.")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestParsePlanOutput_Simple(t *testing.T) {
	sp := &StrategicPlanner{maxPlanSteps: 10, logger: slog.Default()}
	input := `{"steps": [
		{"description": "Write parser", "tool_hint": "code", "depends_on": []},
		{"description": "Write tests", "tool_hint": "code", "depends_on": [0]},
		{"description": "Commit", "tool_hint": "git", "depends_on": [0, 1]}
	]}`

	steps, err := sp.parsePlanOutput("task-123", input)
	if err != nil {
		t.Fatalf("parsePlanOutput failed: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}

	// Check step 0: no deps
	if len(steps[0].DependsOn) != 0 {
		t.Errorf("step 0 should have no deps, got %v", steps[0].DependsOn)
	}
	if steps[0].ToolHint != "code" {
		t.Errorf("step 0 tool_hint: expected 'code', got %q", steps[0].ToolHint)
	}

	// Check step 1: depends on step 0
	if len(steps[1].DependsOn) != 1 {
		t.Fatalf("step 1 should have 1 dep, got %d", len(steps[1].DependsOn))
	}
	if steps[1].DependsOn[0] != steps[0].ID {
		t.Errorf("step 1 should depend on step 0 ID %q, got %q", steps[0].ID, steps[1].DependsOn[0])
	}

	// Check step 2: depends on steps 0 and 1
	if len(steps[2].DependsOn) != 2 {
		t.Fatalf("step 2 should have 2 deps, got %d", len(steps[2].DependsOn))
	}
	if steps[2].ToolHint != "git" {
		t.Errorf("step 2 tool_hint: expected 'git', got %q", steps[2].ToolHint)
	}
}

func TestParsePlanOutput_MaxSteps(t *testing.T) {
	sp := &StrategicPlanner{maxPlanSteps: 2, logger: slog.Default()}
	input := `{"steps": [
		{"description": "step 1"},
		{"description": "step 2"},
		{"description": "step 3"},
		{"description": "step 4"}
	]}`

	steps, err := sp.parsePlanOutput("task-123", input)
	if err != nil {
		t.Fatalf("parsePlanOutput failed: %v", err)
	}
	if len(steps) != 2 {
		t.Errorf("expected 2 steps (capped), got %d", len(steps))
	}
}

func TestParsePlanOutput_EmptyPlan(t *testing.T) {
	sp := &StrategicPlanner{maxPlanSteps: 10, logger: slog.Default()}
	_, err := sp.parsePlanOutput("task-123", `{"steps": []}`)
	if err == nil {
		t.Error("expected error for empty plan")
	}
}

func TestParsePlanOutput_InvalidJSON(t *testing.T) {
	sp := &StrategicPlanner{maxPlanSteps: 10, logger: slog.Default()}
	_, err := sp.parsePlanOutput("task-123", "this is not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParsePlanOutput_SelfDependency(t *testing.T) {
	sp := &StrategicPlanner{maxPlanSteps: 10, logger: slog.Default()}
	input := `{"steps": [
		{"description": "step 0", "depends_on": [0]}
	]}`

	steps, err := sp.parsePlanOutput("task-123", input)
	if err != nil {
		t.Fatalf("parsePlanOutput failed: %v", err)
	}
	// Self-dependency should be filtered out
	if len(steps[0].DependsOn) != 0 {
		t.Errorf("step 0 should have no deps (self-ref filtered), got %v", steps[0].DependsOn)
	}
}

func TestCreateFallbackSteps(t *testing.T) {
	sp := &StrategicPlanner{}
	req := PlanRequest{
		TaskID: "task-1",
		Input:  "do the thing",
		Intent: "code",
	}

	steps := sp.createFallbackSteps(req, nil)
	if len(steps) != 1 {
		t.Fatalf("expected 1 fallback step, got %d", len(steps))
	}
	if steps[0].Description != "do the thing" {
		t.Errorf("expected description %q, got %q", "do the thing", steps[0].Description)
	}
	if steps[0].ToolHint != "code" {
		t.Errorf("expected tool_hint %q, got %q", "code", steps[0].ToolHint)
	}
	if steps[0].TaskID != "task-1" {
		t.Errorf("expected task_id %q, got %q", "task-1", steps[0].TaskID)
	}
}

// TestStrategicPlanner_PublishesEvents verifies that Plan() publishes both
// a "task.planned" event (for TUI consumers) and an "orchestrator.schedule"
// event (to trigger tactical scheduling). The test uses a real bus and SQLite
// stores but nil registry so it exercises the fallback step path.
// TestStrategicPlanner_CopyMemoryRefs verifies that when Plan() is called on a
// task that has MemoryRefs, the first step created by the planner inherits those
// refs. It exercises the full Plan() path (fallback) via a real task store.
func TestStrategicPlanner_CopyMemoryRefs(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	stepStore := taskStore.StepStore()

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		Registry:       nil, // triggers fallback path
		TaskStore:      taskStore,
		StepStore:      stepStore,
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slogDiscardLogger(),
	})

	t.Run("fallback_step_inherits_parent_refs", func(t *testing.T) {
		tsk := newTestTask("task-memref-test", "do something with memory")
		tsk.AddMemoryRef("mem-parent-alpha")
		tsk.AddMemoryRef("mem-parent-beta")
		if err := taskStore.Create(tsk); err != nil {
			t.Fatalf("failed to create task: %v", err)
		}

		req := PlanRequest{
			TaskID:    tsk.ID,
			SessionID: "session-memref",
			Input:     "do something with memory",
			Intent:    "chat", // simple intent -> fallback path
		}

		err = sp.Plan(context.Background(), req)
		if err != nil {
			t.Fatalf("Plan() failed: %v", err)
		}

		// Retrieve persisted steps and verify first step has parent refs
		steps, err := stepStore.ListByTaskID(tsk.ID)
		if err != nil {
			t.Fatalf("failed to list steps: %v", err)
		}
		if len(steps) == 0 {
			t.Fatal("expected at least one step")
		}

		firstStep := steps[0]
		if len(firstStep.MemoryRefs) != 2 {
			t.Errorf("expected first step to have 2 memory refs, got %d: %v",
				len(firstStep.MemoryRefs), firstStep.MemoryRefs)
		}

		// Verify the specific refs are present
		refSet := make(map[string]bool)
		for _, ref := range firstStep.MemoryRefs {
			refSet[ref] = true
		}
		if !refSet["mem-parent-alpha"] {
			t.Error("missing mem-parent-alpha in first step refs")
		}
		if !refSet["mem-parent-beta"] {
			t.Error("missing mem-parent-beta in first step refs")
		}
	})

	t.Run("no_refs_no_crash", func(t *testing.T) {
		tsk := newTestTask("task-norefs-test", "task without memory refs")
		if err := taskStore.Create(tsk); err != nil {
			t.Fatalf("failed to create task: %v", err)
		}

		req := PlanRequest{
			TaskID:    tsk.ID,
			SessionID: "session-norefs",
			Input:     "simple task",
			Intent:    "chat",
		}

		err = sp.Plan(context.Background(), req)
		if err != nil {
			t.Fatalf("Plan() failed: %v", err)
		}

		steps, err := stepStore.ListByTaskID(tsk.ID)
		if err != nil {
			t.Fatalf("failed to list steps: %v", err)
		}
		if len(steps) == 0 {
			t.Fatal("expected at least one step")
		}
		if len(steps[0].MemoryRefs) != 0 {
			t.Errorf("expected no memory refs, got %d", len(steps[0].MemoryRefs))
		}
	})
}

func TestStrategicPlanner_PublishesEvents(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	stepStore := taskStore.StepStore()

	// Subscribe to both expected event topics BEFORE creating the planner
	taskPlannedSub := msgBus.Subscribe("test-observer", "task.planned")
	defer msgBus.Unsubscribe(taskPlannedSub)

	orchScheduleSub := msgBus.Subscribe("test-observer", "orchestrator.schedule")
	defer msgBus.Unsubscribe(orchScheduleSub)

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		Registry:       nil, // triggers fallback path (no LLM needed)
		TaskStore:      taskStore,
		StepStore:      stepStore,
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slogDiscardLogger(),
	})

	// Create a task in the store so Plan() can look it up
	tsk := newTestTask("task-events-test", "implement auth module")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	req := PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "session-events-test",
		Input:     "implement auth module",
		Intent:    "code",
	}

	err = sp.Plan(context.Background(), req)
	if err != nil {
		t.Fatalf("Plan() failed: %v", err)
	}

	// Verify task.planned event was published
	select {
	case msg := <-taskPlannedSub.Channel:
		if msg.Topic != "task.planned" {
			t.Errorf("expected topic 'task.planned', got %q", msg.Topic)
		}
		var event map[string]any
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			t.Fatalf("failed to unmarshal task.planned payload: %v", err)
		}
		if event["task_id"] != tsk.ID {
			t.Errorf("task.planned task_id = %v, want %s", event["task_id"], tsk.ID)
		}
		if event["session_id"] != "session-events-test" {
			t.Errorf("task.planned session_id = %v, want session-events-test", event["session_id"])
		}
		totalSteps, ok := event["total_steps"].(float64)
		if !ok || totalSteps < 1 {
			t.Errorf("task.planned total_steps = %v, want >= 1", event["total_steps"])
		}
		readySteps, ok := event["ready_steps"].(float64)
		if !ok || readySteps < 1 {
			t.Errorf("task.planned ready_steps = %v, want >= 1", event["ready_steps"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for task.planned event")
	}

	// Verify orchestrator.schedule event was published
	select {
	case msg := <-orchScheduleSub.Channel:
		if msg.Topic != "orchestrator.schedule" {
			t.Errorf("expected topic 'orchestrator.schedule', got %q", msg.Topic)
		}
		var event map[string]any
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			t.Fatalf("failed to unmarshal orchestrator.schedule payload: %v", err)
		}
		if event["task_id"] != tsk.ID {
			t.Errorf("orchestrator.schedule task_id = %v, want %s", event["task_id"], tsk.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for orchestrator.schedule event")
	}
}

func TestShouldUsePairSession(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{Logger: slog.Default()})
	sp := &StrategicPlanner{pairManager: pm, logger: slog.Default()}

	tests := []struct {
		name string
		req  PlanRequest
		want bool
	}{
		{
			name: "compound intent always pairs",
			req:  PlanRequest{Intent: string(IntentCompound), Input: "do stuff"},
			want: true,
		},
		{
			name: "short code input no pair",
			req:  PlanRequest{Intent: string(IntentCode), Input: "fix typo in readme"},
			want: false,
		},
		{
			name: "long code input pairs",
			req:  PlanRequest{Intent: string(IntentCode), Input: strings.Repeat("implement the full authentication system with OAuth2 support ", 5)},
			want: true,
		},
		{
			name: "security keyword triggers pair",
			req:  PlanRequest{Intent: string(IntentCode), Input: "add security headers to API responses"},
			want: true,
		},
		{
			name: "chat intent no pair",
			req:  PlanRequest{Intent: string(IntentChat), Input: "how are you"},
			want: false,
		},
		{
			name: "nil pair manager no pair",
			req:  PlanRequest{Intent: string(IntentCompound), Input: "complex task"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "nil pair manager no pair" {
				spNoPM := &StrategicPlanner{pairManager: nil, logger: slog.Default()}
				got := spNoPM.shouldUsePairSession(tt.req)
				if got != tt.want {
					t.Errorf("shouldUsePairSession() = %v, want %v", got, tt.want)
				}
				return
			}
			got := sp.shouldUsePairSession(tt.req)
			if got != tt.want {
				t.Errorf("shouldUsePairSession() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractCriteria(t *testing.T) {
	sp := &StrategicPlanner{logger: slog.Default()}

	tests := []struct {
		name    string
		input   string
		wantMin int
	}{
		{
			name:    "single sentence",
			input:   "Implement the authentication module with JWT tokens",
			wantMin: 1,
		},
		{
			name:    "multi sentence",
			input:   "Write the parser. Add error handling. Include tests.",
			wantMin: 3,
		},
		{
			name:    "with headers",
			input:   "# Task\nWrite the code\n# Notes\nBe careful",
			wantMin: 1,
		},
		{
			name:    "short input uses whole input",
			input:   "fix bug",
			wantMin: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sp.extractCriteria(tt.input)
			if len(got) < tt.wantMin {
				t.Errorf("extractCriteria() returned %d criteria, want at least %d", len(got), tt.wantMin)
			}
		})
	}
}

func TestSelectActorAgent(t *testing.T) {
	sp := &StrategicPlanner{logger: slog.Default()}

	tests := []struct {
		intent string
		want   string
	}{
		{string(IntentCode), config.AgentIDCoder},
		{string(IntentCompound), config.AgentIDCoder},
		{string(IntentDebug), config.AgentIDDebugger},
		{"unknown", config.AgentIDCoder},
	}

	for _, tt := range tests {
		t.Run(tt.intent, func(t *testing.T) {
			got := sp.selectActorAgent(tt.intent)
			if got != tt.want {
				t.Errorf("selectActorAgent(%q) = %q, want %q", tt.intent, got, tt.want)
			}
		})
	}
}

func TestSelectReviewerAgent(t *testing.T) {
	sp := &StrategicPlanner{logger: slog.Default()}

	tests := []struct {
		intent string
		want   string
	}{
		{string(IntentCode), config.AgentIDPlanner},
		{string(IntentCompound), config.AgentIDPlanner},
		{string(IntentDebug), config.AgentIDAnalyst},
		{"unknown", config.AgentIDPlanner},
	}

	for _, tt := range tests {
		t.Run(tt.intent, func(t *testing.T) {
			got := sp.selectReviewerAgent(tt.intent)
			if got != tt.want {
				t.Errorf("selectReviewerAgent(%q) = %q, want %q", tt.intent, got, tt.want)
			}
		})
	}
}

func TestConductInterview_SkipWhenNoAnalysis(t *testing.T) {
	sp := &StrategicPlanner{logger: slogDiscardLogger()}
	req := PlanRequest{TaskID: "task-1", Input: "do something"}

	pctx, err := sp.ConductInterview(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pctx != nil {
		t.Error("expected nil PlanningContext when no TrueAnalysis")
	}
}

func TestConductInterview_SkipWhenLowAmbiguity(t *testing.T) {
	sp := &StrategicPlanner{logger: slogDiscardLogger()}
	req := PlanRequest{
		TaskID: "task-1",
		Input:  "fix the typo in readme",
		TrueAnalysis: &TrueIntentAnalysis{
			Goal:      "fix typo in readme",
			Ambiguity: 0.2,
			Scope:     "narrow",
			Category:  "fix",
			Confidence: 0.95,
		},
	}

	pctx, err := sp.ConductInterview(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pctx != nil {
		t.Error("expected nil PlanningContext when ambiguity is low")
	}
}

func TestConductInterview_AlreadyHasAnswers(t *testing.T) {
	sp := &StrategicPlanner{logger: slogDiscardLogger()}
	req := PlanRequest{
		TaskID: "task-1",
		Input:  "something complex",
		PlanningCtx: &plan.PlanningContext{
			InterviewQuestions: []string{"What scope?", "What constraints?"},
			InterviewAnswers:   []string{"Only the auth module", "Must use JWT"},
		},
	}

	pctx, err := sp.ConductInterview(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pctx == nil {
		t.Fatal("expected non-nil PlanningContext when answers exist")
	}
	if !pctx.InterviewCompleted {
		t.Error("expected InterviewCompleted = true when answers are provided")
	}
	if len(pctx.InterviewAnswers) != 2 {
		t.Errorf("expected 2 answers, got %d", len(pctx.InterviewAnswers))
	}
}

func TestConductInterview_SkipWhenNoRegistry(t *testing.T) {
	sp := &StrategicPlanner{registry: nil, logger: slogDiscardLogger()}
	req := PlanRequest{
		TaskID: "task-1",
		Input:  "something complex",
		TrueAnalysis: &TrueIntentAnalysis{
			Goal:      "build something",
			Ambiguity: 0.9,
			Scope:     "broad",
			Category:  "implementation",
			Confidence: 0.5,
		},
	}

	pctx, err := sp.ConductInterview(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When registry is nil, interview is skipped gracefully
	if pctx != nil {
		t.Error("expected nil PlanningContext when registry is nil")
	}
}

func TestParseInterviewQuestions_ValidJSON(t *testing.T) {
	sp := &StrategicPlanner{logger: slogDiscardLogger()}

	output := `{"questions": ["What is the scope?", "Any specific constraints?", "What timeline?"]}`
	questions := sp.parseInterviewQuestions(output)
	if len(questions) != 3 {
		t.Fatalf("expected 3 questions, got %d", len(questions))
	}
	if questions[0] != "What is the scope?" {
		t.Errorf("question[0] = %q, want %q", questions[0], "What is the scope?")
	}
}

func TestParseInterviewQuestions_MarkdownWrapped(t *testing.T) {
	sp := &StrategicPlanner{logger: slogDiscardLogger()}

	output := "Here are the questions:\n```json\n{\"questions\": [\"Q1?\", \"Q2?\"]}\n```"
	questions := sp.parseInterviewQuestions(output)
	if len(questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(questions))
	}
}

func TestParseInterviewQuestions_InvalidJSON(t *testing.T) {
	sp := &StrategicPlanner{logger: slogDiscardLogger()}

	questions := sp.parseInterviewQuestions("not json at all")
	if questions != nil {
		t.Errorf("expected nil for invalid JSON, got %v", questions)
	}
}

func TestParseInterviewQuestions_EmptyOutput(t *testing.T) {
	sp := &StrategicPlanner{logger: slogDiscardLogger()}

	questions := sp.parseInterviewQuestions("")
	if questions != nil {
		t.Errorf("expected nil for empty output, got %v", questions)
	}
}

func TestMergeMetadata(t *testing.T) {
	tests := []struct {
		name     string
		existing json.RawMessage
		kv       map[string]json.RawMessage
		wantKey  string
	}{
		{
			name:     "merge into empty",
			existing: nil,
			kv:       map[string]json.RawMessage{"a": json.RawMessage(`"val"`)},
			wantKey:  "a",
		},
		{
			name:     "merge into existing",
			existing: json.RawMessage(`{"existing": true}`),
			kv:       map[string]json.RawMessage{"new": json.RawMessage(`1`)},
			wantKey:  "new",
		},
		{
			name:     "existing key preserved",
			existing: json.RawMessage(`{"existing": true}`),
			kv:       map[string]json.RawMessage{"new": json.RawMessage(`1`)},
			wantKey:  "existing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeMetadata(tt.existing, tt.kv)
			var meta map[string]json.RawMessage
			if err := json.Unmarshal(result, &meta); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}
			if _, ok := meta[tt.wantKey]; !ok {
				t.Errorf("expected key %q in result, got %v", tt.wantKey, meta)
			}
		})
	}
}

func TestRemoveMetadataKey(t *testing.T) {
	existing := json.RawMessage(`{"a": 1, "b": 2, "c": 3}`)
	result := removeMetadataKey(existing, "b")
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(result, &meta); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if _, ok := meta["b"]; ok {
		t.Error("key 'b' should have been removed")
	}
	if _, ok := meta["a"]; !ok {
		t.Error("key 'a' should be preserved")
	}
	if _, ok := meta["c"]; !ok {
		t.Error("key 'c' should be preserved")
	}
}

func TestRemoveMetadataKey_Empty(t *testing.T) {
	result := removeMetadataKey(nil, "b")
	if result != nil {
		t.Errorf("expected nil for nil input, got %s", result)
	}
}

func TestAwaitUserApproval(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	stepStore := taskStore.StepStore()

	pendingSub := msgBus.Subscribe("test-observer", "task.pending_approval")
	defer msgBus.Unsubscribe(pendingSub)

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:      taskStore,
		StepStore:      stepStore,
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slogDiscardLogger(),
	})

	tsk := newTestTask("task-approval-test", "implement auth module")
	tsk.SetState(task.StatePlanning)
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	steps := []*task.TaskStep{
		task.NewTaskStep(tsk.ID, "Write the auth handler", 0),
		task.NewTaskStep(tsk.ID, "Write tests", 1),
		task.NewTaskStep(tsk.ID, "Commit changes", 2),
	}
	steps[2].DependsOn = []string{steps[0].ID, steps[1].ID}

	req := PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "session-approval",
		Input:     "implement auth module",
		Intent:    "code",
		PlanningCtx: &plan.PlanningContext{
			InterviewCompleted: true,
			UserApproved:       false,
		},
	}

	err = sp.awaitUserApproval(context.Background(), tsk, steps, req)
	if err != nil {
		t.Fatalf("awaitUserApproval failed: %v", err)
	}

	updated, err := taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if updated.State != task.StateAwaitingApproval {
		t.Errorf("task state = %q, want %q", updated.State, task.StateAwaitingApproval)
	}
	if updated.TotalJobs != 3 {
		t.Errorf("total_jobs = %d, want 3", updated.TotalJobs)
	}

	var meta map[string]json.RawMessage
	if err := json.Unmarshal(updated.Metadata, &meta); err != nil {
		t.Fatalf("failed to parse metadata: %v", err)
	}
	if _, ok := meta["pending_steps"]; !ok {
		t.Error("expected 'pending_steps' key in metadata")
	}

	select {
	case msg := <-pendingSub.Channel:
		if msg.Topic != "task.pending_approval" {
			t.Errorf("expected topic 'task.pending_approval', got %q", msg.Topic)
		}
		var event map[string]any
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}
		if event["task_id"] != tsk.ID {
			t.Errorf("task_id = %v, want %s", event["task_id"], tsk.ID)
		}
		if total, ok := event["total"].(float64); !ok || total != 3 {
			t.Errorf("total = %v, want 3", event["total"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for task.pending_approval event")
	}
}

func TestApprovePlan(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	stepStore := taskStore.StepStore()

	approvedSub := msgBus.Subscribe("test-observer", "task.approved")
	defer msgBus.Unsubscribe(approvedSub)

	schedSub := msgBus.Subscribe("test-observer", "orchestrator.schedule")
	defer msgBus.Unsubscribe(schedSub)

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:      taskStore,
		StepStore:      stepStore,
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slogDiscardLogger(),
	})

	tsk := newTestTask("task-approve-test", "implement auth module")
	tsk.SetState(task.StateAwaitingApproval)
	tsk.TotalJobs = 2

	steps := []*task.TaskStep{
		task.NewTaskStep(tsk.ID, "Write code", 0),
		task.NewTaskStep(tsk.ID, "Write tests", 1),
	}
	pendingData, _ := json.Marshal(steps)
	tsk.Metadata = mergeMetadata(nil, map[string]json.RawMessage{
		"pending_steps": pendingData,
	})

	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	err = sp.ApprovePlan(context.Background(), tsk.ID)
	if err != nil {
		t.Fatalf("ApprovePlan failed: %v", err)
	}

	updated, err := taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if updated.State != task.StateExecuting {
		t.Errorf("task state = %q, want %q", updated.State, task.StateExecuting)
	}

	persistedSteps, err := stepStore.ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(persistedSteps) != 2 {
		t.Errorf("persisted steps = %d, want 2", len(persistedSteps))
	}

	var meta map[string]json.RawMessage
	if json.Unmarshal(updated.Metadata, &meta) == nil {
		if _, ok := meta["pending_steps"]; ok {
			t.Error("'pending_steps' should have been removed from metadata")
		}
	}

	select {
	case msg := <-approvedSub.Channel:
		if msg.Topic != "task.approved" {
			t.Errorf("expected 'task.approved', got %q", msg.Topic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for task.approved event")
	}

	select {
	case msg := <-schedSub.Channel:
		if msg.Topic != "orchestrator.schedule" {
			t.Errorf("expected 'orchestrator.schedule', got %q", msg.Topic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for orchestrator.schedule event")
	}
}

func TestApprovePlan_WrongState(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slogDiscardLogger(),
	})

	tsk := newTestTask("task-wrong-state", "some task")
	tsk.SetState(task.StateExecuting)
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	err = sp.ApprovePlan(context.Background(), tsk.ID)
	if err == nil {
		t.Fatal("expected error for wrong state, got nil")
	}
}

func TestApprovePlan_NotFound(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slogDiscardLogger(),
	})

	err = sp.ApprovePlan(context.Background(), "nonexistent-task")
	if err == nil {
		t.Fatal("expected error for nonexistent task, got nil")
	}
}

func TestRejectPlan(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	rejectedSub := msgBus.Subscribe("test-observer", "task.rejected")
	defer msgBus.Unsubscribe(rejectedSub)

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slogDiscardLogger(),
	})

	tsk := newTestTask("task-reject-test", "implement auth module")
	tsk.SetState(task.StateAwaitingApproval)
	tsk.Metadata = mergeMetadata(nil, map[string]json.RawMessage{
		"pending_steps": json.RawMessage(`[]`),
	})
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	err = sp.RejectPlan(context.Background(), tsk.ID, "out of scope")
	if err != nil {
		t.Fatalf("RejectPlan failed: %v", err)
	}

	updated, err := taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if updated.State != task.StateRejected {
		t.Errorf("task state = %q, want %q", updated.State, task.StateRejected)
	}

	var meta map[string]json.RawMessage
	if json.Unmarshal(updated.Metadata, &meta) == nil {
		if _, ok := meta["pending_steps"]; ok {
			t.Error("'pending_steps' should have been removed from metadata")
		}
	}

	select {
	case msg := <-rejectedSub.Channel:
		if msg.Topic != "task.rejected" {
			t.Errorf("expected 'task.rejected', got %q", msg.Topic)
		}
		var event map[string]any
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}
		if event["reason"] != "out of scope" {
			t.Errorf("reason = %v, want 'out of scope'", event["reason"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for task.rejected event")
	}
}

func TestRejectPlan_WrongState(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slogDiscardLogger(),
	})

	tsk := newTestTask("task-reject-wrong-state", "some task")
	tsk.SetState(task.StatePlanning)
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	err = sp.RejectPlan(context.Background(), tsk.ID, "bad plan")
	if err == nil {
		t.Fatal("expected error for wrong state, got nil")
	}
}

func TestPlan_ApprovalGate(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	stepStore := taskStore.StepStore()

	pendingSub := msgBus.Subscribe("test-observer", "task.pending_approval")
	defer msgBus.Unsubscribe(pendingSub)

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		Registry:       nil,
		TaskStore:      taskStore,
		StepStore:      stepStore,
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slogDiscardLogger(),
	})

	tsk := newTestTask("task-gate-test", "complex task with interview")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	req := PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "session-gate",
		Input:     "complex task with interview",
		Intent:    "code",
		PlanningCtx: &plan.PlanningContext{
			InterviewCompleted: true,
			UserApproved:       false,
		},
	}

	err = sp.Plan(context.Background(), req)
	if err != nil {
		t.Fatalf("Plan() failed: %v", err)
	}

	updated, err := taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if updated.State != task.StateAwaitingApproval {
		t.Errorf("task state = %q, want %q", updated.State, task.StateAwaitingApproval)
	}

	persistedSteps, err := stepStore.ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(persistedSteps) != 0 {
		t.Errorf("expected 0 persisted steps during approval wait, got %d", len(persistedSteps))
	}

	select {
	case msg := <-pendingSub.Channel:
		if msg.Topic != "task.pending_approval" {
			t.Errorf("expected 'task.pending_approval', got %q", msg.Topic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for task.pending_approval event")
	}

	err = sp.ApprovePlan(context.Background(), tsk.ID)
	if err != nil {
		t.Fatalf("ApprovePlan failed: %v", err)
	}

	approved, err := taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if approved.State != task.StateExecuting {
		t.Errorf("task state after approval = %q, want %q", approved.State, task.StateExecuting)
	}

	finalSteps, err := stepStore.ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(finalSteps) != 1 {
		t.Errorf("expected 1 persisted step after approval, got %d", len(finalSteps))
	}
}
