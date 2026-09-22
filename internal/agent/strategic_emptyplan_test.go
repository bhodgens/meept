package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
	pkgsecurity "github.com/caimlas/meept/pkg/security"
)

// emptyPlanChatter is a canned llm.Chatter stub for the empty-plan guard
// pins (issue #53 direction 3): the planner LLM's raw output is returned
// verbatim and every call is counted so tests can prove no replan loop runs.
type emptyPlanChatter struct {
	mu    sync.Mutex
	resp  string
	err   error
	calls int
}

func (c *emptyPlanChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	c.mu.Lock()
	c.calls++
	resp, chatErr := c.resp, c.err
	c.mu.Unlock()
	if chatErr != nil {
		return nil, chatErr
	}
	return &llm.Response{
		Content:      resp,
		FinishReason: "stop",
		Usage:        llm.TokenUsage{TotalTokens: 5},
	}, nil
}

func (c *emptyPlanChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *emptyPlanChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "empty-plan-test"}
}

func (c *emptyPlanChatter) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// newEmptyPlanTestRegistry builds a registry whose planner agent resolves to
// a real AgentLoop wired to the given canned chatter — the same shape the
// priority/guards loop tests use, injected into the "_default" task bucket
// that registry.Get(AgentIDPlanner) reads.
func newEmptyPlanTestRegistry(chatter llm.Chatter) *AgentRegistry {
	plannerLoop := NewAgentLoop("planner-emptyplan-test", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(NewPlaceholderToolRegistry()),
		WithSecurityChecker(pkgsecurity.NewPermissionChecker(pkgsecurity.Config{})),
		WithLoopLogger(slogDiscardLogger()),
		WithAgentConfig(AgentConfig{MaxIterations: 5}),
	)
	return &AgentRegistry{
		specs: map[string]*AgentSpec{
			config.AgentIDPlanner: {
				ID:      config.AgentIDPlanner,
				Name:    "Planner",
				Role:    RoleExecutor,
				Enabled: true,
			},
		},
		loops: map[string]map[string]*AgentLoop{
			config.AgentIDPlanner: {"_default": plannerLoop},
		},
		activeQueues:    make(map[string]*QueueEntry),
		logger:          slogDiscardLogger(),
		sharedConvStore: NewConversationStore(100),
	}
}

func newEmptyPlanTestPlanner(t *testing.T, chatter llm.Chatter) (*StrategicPlanner, *bus.MessageBus, *bus.Subscriber) {
	t.Helper()
	msgBus := bus.New(nil, slogDiscardLogger())
	t.Cleanup(func() { msgBus.Close() })

	taskStore, err := newTestTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	t.Cleanup(func() { taskStore.Close() })

	reg := newEmptyPlanTestRegistry(chatter)
	sp := NewStrategicPlanner(StrategicPlannerConfig{
		Registry:       reg,
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slogDiscardLogger(),
	})
	failedSub := msgBus.Subscribe("test-observer", "task.failed")
	return sp, msgBus, failedSub
}

// TestEmptyPlanGuard_SingleArtifactDeterministicStep pins the deterministic
// single-step degradation: an empty plan for a single-artifact description
// produces exactly ONE coder step carrying the task description verbatim,
// with no planner re-entry (no replan loop) and the task metadata marked
// 'degraded: deterministic single-step' (issue #53 direction 3).
func TestEmptyPlanGuard_SingleArtifactDeterministicStep(t *testing.T) {
	chatter := &emptyPlanChatter{resp: `{"steps": []}`}
	sp, msgBus, failedSub := newEmptyPlanTestPlanner(t, chatter)
	defer msgBus.Unsubscribe(failedSub)
	taskStore := sp.taskStore
	stepStore := sp.stepStore

	tsk := newTestTask("task-emptyplan-artifact", "create a config file for the scheduler")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	err := sp.Plan(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-emptyplan-artifact",
		Input:     "create a config file for the scheduler",
		Intent:    string(IntentQuickPlan),
		Mode:      "quick_plan",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Verbose pin: no planner re-entry — exactly one LLM call.
	if got := chatter.callCount(); got != 1 {
		t.Errorf("planner LLM calls = %d, want exactly 1 (no replan loop)", got)
	}

	steps, err := stepStore.ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("persisted steps = %d, want exactly 1", len(steps))
	}
	if steps[0].AgentID != config.AgentIDCoder {
		t.Errorf("step AgentID = %q, want %q", steps[0].AgentID, config.AgentIDCoder)
	}
	if steps[0].Description != "create a config file for the scheduler" {
		t.Errorf("step description = %q, want the task input verbatim", steps[0].Description)
	}

	// Task must proceed to executing (one real execution attempt).
	got, err := taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State != task.StateExecuting {
		t.Errorf("task state = %q, want %q", got.State, task.StateExecuting)
	}

	// Degraded marker must be present in the persisted task metadata.
	meta := map[string]any{}
	if err := json.Unmarshal(got.Metadata, &meta); err != nil {
		t.Fatalf("unmarshal task metadata: %v", err)
	}
	if meta["plan_quality"] != "degraded: deterministic single-step" {
		t.Errorf("metadata plan_quality = %v, want %q", meta["plan_quality"], "degraded: deterministic single-step")
	}

	// No failure event may fire on the success path.
	select {
	case msg := <-failedSub.Channel:
		t.Fatalf("unexpected task.failed event published: %s", msg.Topic)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestEmptyPlanGuard_NonArtifactTaskFailsHonestly pins the honest-failure
// path: an empty plan for a description without a single-artifact shape
// fails the task immediately with 'planner produced no plan; not a
// single-artifact task' — zero steps, no generic fallback noise.
func TestEmptyPlanGuard_NonArtifactTaskFailsHonestly(t *testing.T) {
	chatter := &emptyPlanChatter{resp: `{"steps": []}`}
	sp, msgBus, failedSub := newEmptyPlanTestPlanner(t, chatter)
	defer msgBus.Unsubscribe(failedSub)
	taskStore := sp.taskStore
	stepStore := sp.stepStore

	tsk := newTestTask("task-emptyplan-nonartifact", "explain the architecture of the project")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	err := sp.Plan(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-emptyplan-nonartifact",
		Input:     "explain the architecture of the project",
		Intent:    string(IntentQuickPlan),
		Mode:      "quick_plan",
	})
	if err == nil {
		t.Fatal("Plan must fail for a non-single-artifact task with an empty plan")
	}
	if !strings.Contains(err.Error(), "planner produced no plan; not a single-artifact task") {
		t.Errorf("error = %q, want it to contain the honest failure reason", err.Error())
	}

	// Exactly one LLM call: the honest failure must not re-enter the planner.
	if got := chatter.callCount(); got != 1 {
		t.Errorf("planner LLM calls = %d, want exactly 1", got)
	}

	// Zero steps persisted.
	steps, err := stepStore.ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("persisted steps = %d, want 0", len(steps))
	}

	// Task must be failed, with the failure published.
	got, err := taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State != task.StateFailed {
		t.Errorf("task state = %q, want %q", got.State, task.StateFailed)
	}
	select {
	case msg := <-failedSub.Channel:
		var event map[string]any
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			t.Fatalf("unmarshal task.failed payload: %v", err)
		}
		if event["task_id"] != tsk.ID {
			t.Errorf("task.failed task_id = %v, want %s", event["task_id"], tsk.ID)
		}
		if reason, _ := event["error"].(string); !strings.Contains(reason, "not a single-artifact task") {
			t.Errorf("task.failed error = %q, want the honest reason", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for task.failed event")
	}
}

// TestEmptyPlanGuard_RealPlanUnchanged pins the no-regression rule: when the
// planner returns a real (non-empty) plan, nothing about the flow changes —
// the parsed steps are used verbatim, no degraded marker is written, and the
// coder agent is not forced onto the steps.
func TestEmptyPlanGuard_RealPlanUnchanged(t *testing.T) {
	chatter := &emptyPlanChatter{resp: `{"steps": [{"description": "step one"}, {"description": "step two"}]}`}
	sp, msgBus, failedSub := newEmptyPlanTestPlanner(t, chatter)
	defer msgBus.Unsubscribe(failedSub)
	taskStore := sp.taskStore
	stepStore := sp.stepStore

	tsk := newTestTask("task-emptyplan-real", "create a report file from the metrics")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	err := sp.Plan(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-emptyplan-real",
		Input:     "create a report file from the metrics",
		Intent:    string(IntentQuickPlan),
		Mode:      "quick_plan",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if got := chatter.callCount(); got != 1 {
		t.Errorf("planner LLM calls = %d, want 1", got)
	}

	steps, err := stepStore.ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("persisted steps = %d, want 2 from the real plan", len(steps))
	}
	if steps[0].Description != "step one" || steps[1].Description != "step two" {
		t.Errorf("step descriptions = %q, %q; want the planner's verbatim", steps[0].Description, steps[1].Description)
	}
	for i, s := range steps {
		if s.AgentID != "" {
			t.Errorf("step %d AgentID = %q, want empty (real plans must not force coder)", i, s.AgentID)
		}
	}

	got, err := taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State != task.StateExecuting {
		t.Errorf("task state = %q, want %q", got.State, task.StateExecuting)
	}
	if len(got.Metadata) > 0 && strings.Contains(string(got.Metadata), "plan_quality") {
		t.Errorf("real plan must not carry a degraded marker; metadata = %s", got.Metadata)
	}

	select {
	case msg := <-failedSub.Channel:
		t.Fatalf("unexpected task.failed event published: %s", msg.Topic)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestEmptyPlanGuard_NonEmptyPlanParseFailureKeepsLegacyFallback pins the
// sentinel scoping: only the EMPTY-plan error takes the new paths. Other
// planner failures (here: no JSON at all) keep the legacy generic fallback
// behavior untouched.
func TestEmptyPlanGuard_NonEmptyPlanParseFailureKeepsLegacyFallback(t *testing.T) {
	chatter := &emptyPlanChatter{resp: "I could not produce a plan in JSON format."}
	sp, msgBus, failedSub := newEmptyPlanTestPlanner(t, chatter)
	defer msgBus.Unsubscribe(failedSub)
	taskStore := sp.taskStore
	stepStore := sp.stepStore

	tsk := newTestTask("task-emptyplan-legacy", "create a config file for the scheduler")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	err := sp.Plan(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-emptyplan-legacy",
		Input:     "create a config file for the scheduler",
		Intent:    string(IntentQuickPlan),
		Mode:      "quick_plan",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	steps, err := stepStore.ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("persisted steps = %d, want the legacy single fallback step", len(steps))
	}
	// Legacy fallback must NOT force the coder agent (unchanged behavior).
	if steps[0].AgentID != "" {
		t.Errorf("legacy fallback AgentID = %q, want empty", steps[0].AgentID)
	}

	select {
	case msg := <-failedSub.Channel:
		t.Fatalf("unexpected task.failed event published: %s", msg.Topic)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestIsSingleArtifactTask pins the detector: a single concrete artifact
// action (create/write/make/...) combined with a file/doc/config noun.
func TestIsSingleArtifactTask(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"create a config file for the scheduler", true},
		{"write the readme", true},
		{"make a doc summarizing the design", true},
		{"add a notes.md to the repo", true},
		{"generate a yaml manifest for the deployment", true},
		{"Create a Config File", true}, // case-insensitive
		{"explain the architecture of the project", false},
		{"what is the meaning of life", false},
		{"think about the approach", false},
		{"", false},
		{"write it down", false},              // action without an artifact noun
		{"the config file is missing", false}, // noun without an action
	}
	for _, tc := range cases {
		if got := isSingleArtifactTask(tc.input); got != tc.want {
			t.Errorf("isSingleArtifactTask(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
