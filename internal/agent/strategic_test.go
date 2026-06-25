package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
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
	got := ExtractJSON(input)
	if got != input {
		t.Errorf("expected direct JSON, got %q", got)
	}
}

func TestExtractJSON_MarkdownFence(t *testing.T) {
	input := "Here is the plan:\n```json\n{\"steps\": [{\"description\": \"do something\"}]}\n```\nDone."
	got := ExtractJSON(input)
	if got != `{"steps": [{"description": "do something"}]}` {
		t.Errorf("unexpected result: %q", got)
	}
}

func TestExtractJSON_GenericFence(t *testing.T) {
	input := "Plan:\n```\n{\"steps\": [{\"description\": \"x\"}]}\n```"
	got := ExtractJSON(input)
	if got != `{"steps": [{"description": "x"}]}` {
		t.Errorf("unexpected result: %q", got)
	}
}

func TestExtractJSON_BraceExtraction(t *testing.T) {
	input := "Sure, here is your plan: {\"steps\": [{\"description\": \"test\"}]} I hope this helps!"
	got := ExtractJSON(input)
	if got != `{"steps": [{"description": "test"}]}` {
		t.Errorf("unexpected result: %q", got)
	}
}

func TestExtractJSON_NoJSON(t *testing.T) {
	got := ExtractJSON("This is just plain text with no JSON.")
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

	t.Run("no_planning_ctx_preserves_input", func(t *testing.T) {
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
	})

	t.Run("planning_ctx_prepends_verified_context", func(t *testing.T) {
		req := PlanRequest{
			TaskID: "task-ctx",
			Input:  "implement the feature",
			Intent: "code",
			PlanningCtx: &plan.PlanningContext{
				InterviewCompleted: true,
				TrueGoal:           "ship login page",
				Requirements:        []string{"OAuth2 flow", "session persistence"},
				Constraints:         map[string]string{"timeline": "this week"},
			},
		}

		steps := sp.createFallbackSteps(req, nil)
		if len(steps) != 1 {
			t.Fatalf("expected 1 fallback step, got %d", len(steps))
		}
		desc := steps[0].Description
		if !strings.HasPrefix(desc, "## Verified Context") {
			t.Errorf("expected Verified Context header, got: %s", desc)
		}
		if !strings.Contains(desc, "True goal: ship login page") {
			t.Errorf("expected true goal in description, got: %s", desc)
		}
		if !strings.Contains(desc, "- OAuth2 flow") {
			t.Errorf("expected requirement bullet, got: %s", desc)
		}
		if !strings.Contains(desc, "- timeline: this week") {
			t.Errorf("expected constraint bullet, got: %s", desc)
		}
		if !strings.Contains(desc, "implement the feature") {
			t.Errorf("expected original input preserved, got: %s", desc)
		}
	})

	t.Run("empty_planning_ctx_keeps_behavior", func(t *testing.T) {
		// InterviewCompleted but no actual content — should keep current
		// behavior (no verified-context section prepended).
		req := PlanRequest{
			TaskID: "task-empty-ctx",
			Input:  "do the thing",
			Intent: "code",
			PlanningCtx: &plan.PlanningContext{
				InterviewCompleted: true,
			},
		}

		steps := sp.createFallbackSteps(req, nil)
		if len(steps) != 1 {
			t.Fatalf("expected 1 fallback step, got %d", len(steps))
		}
		if steps[0].Description != "do the thing" {
			t.Errorf("expected description %q (no context section), got %q", "do the thing", steps[0].Description)
		}
	})
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

func TestExtractCriteria(t *testing.T) {
	sp := &StrategicPlanner{logger: slog.Default()}

	tests := []struct {
		name      string
		input     string
		wantMin   int
		wantExact []string // when set, require these exact substrings to be present
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
		{
			// "e.g." must NOT cause a split. The sentence boundary detector
			// only splits on punctuation followed by whitespace+capital/digit.
			name:      "abbreviation_e.g._no_split",
			input:     "Use OAuth e.g. Auth0 for the login flow.",
			wantMin:   1,
			wantExact: []string{"Use OAuth e.g. Auth0 for the login flow."},
		},
		{
			// "i.e." must NOT cause a split.
			name:      "abbreviation_i.e._no_split",
			input:     "See docs i.e. the spec for more details on this",
			wantMin:   1,
			wantExact: []string{"See docs i.e. the spec for more details on this"},
		},
		{
			// Decimal numbers must NOT cause a split.
			name:      "decimal_no_split",
			input:     "Configurable in 3.14 seconds or less total runtime",
			wantMin:   1,
			wantExact: []string{"Configurable in 3.14 seconds or less total runtime"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sp.extractCriteria(tt.input)
			if len(got) < tt.wantMin {
				t.Errorf("extractCriteria() returned %d criteria, want at least %d", len(got), tt.wantMin)
			}
			for _, want := range tt.wantExact {
				found := false
				for _, c := range got {
					if c == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("extractCriteria() expected criterion %q to be present exactly, got %v", want, got)
				}
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
	if !errors.Is(err, ErrInterviewNotNeeded) {
		t.Fatalf("expected ErrInterviewNotNeeded, got %v", err)
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
			Goal:       "fix typo in readme",
			Ambiguity:  0.2,
			Scope:      "narrow",
			Category:   "fix",
			Confidence: 0.95,
		},
	}

	pctx, err := sp.ConductInterview(context.Background(), req)
	if !errors.Is(err, ErrInterviewNotNeeded) {
		t.Fatalf("expected ErrInterviewNotNeeded, got %v", err)
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
			Goal:       "build something",
			Ambiguity:  0.9,
			Scope:      "broad",
			Category:   "implementation",
			Confidence: 0.5,
		},
	}

	pctx, err := sp.ConductInterview(context.Background(), req)
	if !errors.Is(err, ErrInterviewNoRegistry) {
		t.Fatalf("expected ErrInterviewNoRegistry, got %v", err)
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

func TestRequiresApproval(t *testing.T) {
	tests := []struct {
		name              string
		approvalThreshold int
		req               PlanRequest
		numSteps          int
		want              bool
	}{
		{
			name:              "interview_completed_not_approved",
			approvalThreshold: 5,
			req: PlanRequest{
				PlanningCtx: &plan.PlanningContext{
					InterviewCompleted: true,
					UserApproved:       false,
				},
			},
			numSteps: 1,
			want:     true,
		},
		{
			name:              "interview_completed_and_approved",
			approvalThreshold: 5,
			req: PlanRequest{
				PlanningCtx: &plan.PlanningContext{
					InterviewCompleted: true,
					UserApproved:       true,
				},
			},
			numSteps: 2,
			want:     false,
		},
		{
			name:              "no_interview_below_threshold",
			approvalThreshold: 5,
			req:               PlanRequest{},
			numSteps:          3,
			want:              false,
		},
		{
			name:              "no_interview_meets_threshold",
			approvalThreshold: 5,
			req:               PlanRequest{},
			numSteps:          5,
			want:              true,
		},
		{
			name:              "no_interview_above_threshold",
			approvalThreshold: 5,
			req:               PlanRequest{},
			numSteps:          10,
			want:              true,
		},
		{
			name:              "threshold_zero_disables_complexity_gate",
			approvalThreshold: 0,
			req:               PlanRequest{},
			numSteps:          100,
			want:              false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp := &StrategicPlanner{
				logger:                slogDiscardLogger(),
				approvalStepThreshold: tt.approvalThreshold,
			}
			steps := make([]*task.TaskStep, tt.numSteps)
			for i := range steps {
				steps[i] = task.NewTaskStep("t-1", "step", i)
			}
			got := sp.requiresApproval(tt.req, steps)
			if got != tt.want {
				t.Errorf("requiresApproval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReplanFailedTask_RemainingWork(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	stepStore := taskStore.StepStore()

	// Capture the replan input by subscribing to the bus. The replan flows
	// through Plan() which (with nil registry) takes the fallback path and
	// creates a single step whose description contains the replan text.
	plannedSub := msgBus.Subscribe("test-observer", "task.planned")
	defer msgBus.Unsubscribe(plannedSub)

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		// Empty registry: Get("planner") returns an error, so generatePlan
		// fails and Plan() falls through to the single-step fallback path —
		// whose description is the replan text we want to verify.
		Registry:       NewAgentRegistry(RegistryConfig{Logger: slogDiscardLogger()}),
		TaskStore:      taskStore,
		StepStore:      stepStore,
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slogDiscardLogger(),
	})

	tsk := newTestTask("task-replan-test", "build the auth module")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create three steps: one completed, one failed (uncompleted), one pending.
	completedStep := task.NewTaskStep(tsk.ID, "Research OAuth2 providers", 0)
	completedStep.State = task.StepCompleted
	if err := stepStore.Create(completedStep); err != nil {
		t.Fatalf("failed to create completed step: %v", err)
	}

	failedStep := task.NewTaskStep(tsk.ID, "Implement login endpoint", 1)
	failedStep.State = task.StepFailed
	if err := stepStore.Create(failedStep); err != nil {
		t.Fatalf("failed to create failed step: %v", err)
	}

	pendingStep := task.NewTaskStep(tsk.ID, "Write integration tests", 2)
	pendingStep.State = task.StepPending
	if err := stepStore.Create(pendingStep); err != nil {
		t.Fatalf("failed to create pending step: %v", err)
	}

	// Call ReplanFailedTask. With nil registry, Plan() takes the fallback
	// path and persists a single step whose description is the replan text.
	err = sp.ReplanFailedTask(context.Background(), tsk.ID, "coder agent panicked")
	if err != nil {
		t.Fatalf("ReplanFailedTask failed: %v", err)
	}

	// The persisted fallback step description should contain the remaining
	// (uncompleted) work, not just the original task description.
	steps, err := stepStore.ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}

	// Find the fallback step (the most recently created one with sequence 0
	// from the fallback path — its description contains "RE-PLAN").
	var fallbackDesc string
	for _, s := range steps {
		if strings.Contains(s.Description, "RE-PLAN") {
			fallbackDesc = s.Description
			break
		}
	}
	if fallbackDesc == "" {
		t.Fatalf("no RE-PLAN fallback step found; steps: %+v", steps)
	}

	// Completed step should appear in the "do not redo" section.
	if !strings.Contains(fallbackDesc, "Research OAuth2 providers") {
		t.Errorf("expected completed step description in replan, got: %s", fallbackDesc)
	}
	// Uncompleted steps should appear in the remaining section.
	if !strings.Contains(fallbackDesc, "Implement login endpoint") {
		t.Errorf("expected failed step description in remaining work, got: %s", fallbackDesc)
	}
	if !strings.Contains(fallbackDesc, "Write integration tests") {
		t.Errorf("expected pending step description in remaining work, got: %s", fallbackDesc)
	}
	// The remaining-work section should be labeled distinctly.
	if !strings.Contains(fallbackDesc, "Remaining (uncompleted) steps to retry or finish:") {
		t.Errorf("expected remaining-steps header in replan, got: %s", fallbackDesc)
	}
	// Sanity: the original description should also be referenced in the header.
	if !strings.Contains(fallbackDesc, "build the auth module") {
		t.Errorf("expected original task description in replan header, got: %s", fallbackDesc)
	}

	// Verify the task.planned event fired (proves the replan flowed through Plan).
	select {
	case <-plannedSub.Channel:
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for task.planned event from replan")
	}
}

func TestConductInterview_ErrorTypes(t *testing.T) {
	t.Run("no_analysis_returns_not_needed", func(t *testing.T) {
		sp := &StrategicPlanner{logger: slogDiscardLogger()}
		req := PlanRequest{TaskID: "t-1", Input: "do something"}
		_, err := sp.ConductInterview(context.Background(), req)
		if !errors.Is(err, ErrInterviewNotNeeded) {
			t.Errorf("expected ErrInterviewNotNeeded, got %v", err)
		}
	})

	t.Run("low_ambiguity_returns_not_needed", func(t *testing.T) {
		sp := &StrategicPlanner{logger: slogDiscardLogger()}
		req := PlanRequest{
			TaskID: "t-1",
			TrueAnalysis: &TrueIntentAnalysis{
				Ambiguity: 0.1,
				Scope:      "narrow",
			},
		}
		_, err := sp.ConductInterview(context.Background(), req)
		if !errors.Is(err, ErrInterviewNotNeeded) {
			t.Errorf("expected ErrInterviewNotNeeded, got %v", err)
		}
	})

	t.Run("nil_registry_returns_no_registry", func(t *testing.T) {
		sp := &StrategicPlanner{registry: nil, logger: slogDiscardLogger()}
		req := PlanRequest{
			TaskID: "t-1",
			TrueAnalysis: &TrueIntentAnalysis{
				Ambiguity: 0.9,
				Scope:      "broad",
			},
		}
		_, err := sp.ConductInterview(context.Background(), req)
		if !errors.Is(err, ErrInterviewNoRegistry) {
			t.Errorf("expected ErrInterviewNoRegistry, got %v", err)
		}
	})

	t.Run("missing_planner_returns_planner_missing", func(t *testing.T) {
		// Construct an empty registry directly (bypassing NewAgentRegistry,
		// which calls loadAgentDefinitions and may pick up AGENT.md files
		// from the developer's home directory, polluting the test). With no
		// "planner" spec, Get returns an error.
		reg := &AgentRegistry{
			specs:           make(map[string]*AgentSpec),
			loops:           make(map[string]*AgentLoop),
			activeQueues:    make(map[string]*QueueEntry),
			logger:          slogDiscardLogger(),
			sharedConvStore: NewConversationStore(10),
		}
		sp := &StrategicPlanner{registry: reg, logger: slogDiscardLogger()}
		req := PlanRequest{
			TaskID: "t-1",
			TrueAnalysis: &TrueIntentAnalysis{
				Ambiguity: 0.9,
				Scope:      "broad",
			},
		}
		_, err := sp.ConductInterview(context.Background(), req)
		if !errors.Is(err, ErrInterviewPlannerMissing) {
			t.Errorf("expected ErrInterviewPlannerMissing, got %v", err)
		}
	})

	t.Run("already_has_answers_returns_nil_error", func(t *testing.T) {
		sp := &StrategicPlanner{logger: slogDiscardLogger()}
		req := PlanRequest{
			TaskID: "t-1",
			PlanningCtx: &plan.PlanningContext{
				InterviewAnswers: []string{"yes"},
			},
		}
		pctx, err := sp.ConductInterview(context.Background(), req)
		if err != nil {
			t.Errorf("expected nil error for already-answered interview, got %v", err)
		}
		if pctx == nil || !pctx.InterviewCompleted {
			t.Error("expected non-nil completed PlanningContext")
		}
	})
}

func TestParsePlanOutput_InvalidDeps(t *testing.T) {
	// Use a buffering handler to verify the invalid-dep debug log fires.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	sp := &StrategicPlanner{maxPlanSteps: 10, logger: logger}
	input := `{"steps": [
		{"description": "step 0"},
		{"description": "step 1", "depends_on": [0, 5, -1, 1]},
		{"description": "step 2", "depends_on": [0]}
	]}`

	steps, err := sp.parsePlanOutput("task-deps-test", input)
	if err != nil {
		t.Fatalf("parsePlanOutput failed: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}

	// Step 1 should depend only on step 0 — the out-of-range (5, -1) and
	// self-reference (1) indices must be filtered out.
	if len(steps[1].DependsOn) != 1 {
		t.Errorf("step 1 should have 1 valid dep, got %d (%v)", len(steps[1].DependsOn), steps[1].DependsOn)
	}
	if steps[1].DependsOn[0] != steps[0].ID {
		t.Errorf("step 1 dep = %q, want %q", steps[1].DependsOn[0], steps[0].ID)
	}

	// The debug log should mention "filtering invalid dependency index" for
	// each of the three invalid indices.
	logOutput := buf.String()
	if !strings.Contains(logOutput, "filtering invalid dependency index") {
		t.Errorf("expected debug log for invalid deps, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "task-deps-test") {
		t.Errorf("expected task_id in debug log, got: %s", logOutput)
	}
}

// TestStrategicPlanner_TemplateOverrideProjectLocal verifies that a
// project-local template (the highest-precedence tier) is picked up by the
// loader and actually changes the rendered prompt. This closes the loop on
// the 4-tier discovery: bundled → system → user → project-local.
func TestStrategicPlanner_TemplateOverrideProjectLocal(t *testing.T) {
	// Create a project-local override that produces obviously-distinct output.
	tmp := t.TempDir()
	override := "---\nname: planner.decompose\n---\nOVERRIDE_MARKER {{.Input}}"
	if err := os.MkdirAll(filepath.Join(tmp, "planner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "planner", "decompose.md"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := newPlannerTemplateLoader(tmp)
	got, err := loader.render("planner/decompose.md", map[string]any{"Input": "x"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(got, "OVERRIDE_MARKER x") {
		t.Errorf("override did not apply; got %q", got)
	}
}

func TestStrategicPlanner_inferLegacyMode(t *testing.T) {
	cases := []struct {
		name string
		req  PlanRequest
		want string
	}{
		{name: "compound -> spec_pair", req: PlanRequest{Intent: string(IntentCompound), IsCompound: true}, want: "spec_pair"},
		{name: "chat intent -> direct", req: PlanRequest{Intent: string(IntentChat), Input: "what's the weather"}, want: "direct"},
		{name: "code intent + long input -> plan", req: PlanRequest{Intent: string(IntentCode), Input: strings.Repeat("a", 150)}, want: "plan"},
		{name: "plan intent -> spec_plan", req: PlanRequest{Intent: string(IntentPlan)}, want: "spec_plan"},
		{name: "empty intent + short input -> direct", req: PlanRequest{Intent: "", Input: "hi"}, want: "direct"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sp := &StrategicPlanner{simpleInputMaxChars: 100, pairInputMinChars: 200}
			got := sp.inferLegacyMode(c.req)
			if got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestStrategicPlanner_shouldInterview(t *testing.T) {
	sp := &StrategicPlanner{interviewAmbiguity: 0.6}
	cases := []struct {
		mode string
		req  PlanRequest
		want bool
	}{
		{mode: "direct", req: PlanRequest{}, want: false},
		{mode: "spec_plan", req: PlanRequest{}, want: true},
		{mode: "plan", req: PlanRequest{TrueAnalysis: &TrueIntentAnalysis{Ambiguity: 0.3}}, want: false},
		{mode: "plan", req: PlanRequest{TrueAnalysis: &TrueIntentAnalysis{Ambiguity: 0.7}}, want: true},
		{mode: "spec_pair", req: PlanRequest{}, want: false},
	}
	for _, c := range cases {
		got := sp.shouldInterview(c.req, c.mode)
		if got != c.want {
			t.Errorf("mode=%s got %v want %v", c.mode, got, c.want)
		}
	}
}

// TestStrategicPlanner_PairSessionNilManagerNoPanic verifies that a nil
// pairManager (misconfigured env or test) does not cause a panic in
// planPairSession. Regression test for Plan D Task 5 (commit f1c08c8b):
// the refactor deleted shouldUsePairSession's nil guard, which would
// panic on sp.pairManager.CreateSession when pairManager is nil.
func TestStrategicPlanner_PairSessionNilManagerNoPanic(t *testing.T) {
	sp := &StrategicPlanner{
		// pairManager intentionally nil
		logger:             slogDiscardLogger(),
		simpleInputMaxChars: 100,
	}
	req := PlanRequest{
		Intent:     string(IntentCompound),
		IsCompound: true,
		Input:      "do two things",
	}

	// Should return error, not panic.
	_, err := sp.planPairSession(context.Background(), req, nil)
	if err == nil {
		t.Fatal("want error when pairManager is nil")
	}
	if !strings.Contains(err.Error(), "pair manager") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestStrategicPlanner_DefaultTemplateLoaderRegistersSpecFallback verifies that
// NewStrategicPlanner, when constructed without an explicit TemplateLoader,
// registers all three planner fallbacks — including decompose_spec.md. Without
// the spec fallback, spec-plan rendering silently degrades to planSinglePhase.
func TestStrategicPlanner_DefaultTemplateLoaderRegistersSpecFallback(t *testing.T) {
	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Bus:            bus.New(nil, slogDiscardLogger()),
		Logger:         slogDiscardLogger(),
		PlannerTimeout: 10 * time.Second,
	})

	if sp.templateLoader == nil {
		t.Fatalf("templateLoader is nil; want *plannerTemplateLoader")
	}
	loader := sp.templateLoader

	want := []string{
		"planner/decompose.md",
		"planner/interview.md",
		"planner/decompose_spec.md",
	}
	for _, name := range want {
		if _, ok := loader.fallbacks[name]; !ok {
			t.Errorf("default templateLoader missing fallback for %q", name)
		}
	}
}
