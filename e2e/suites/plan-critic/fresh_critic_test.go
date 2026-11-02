//go:build e2e

// Gate tests for the fresh-context evidence critic (leaf 02; contracts
// C2, C3, C4 in docs/plans/20261101-fresh-context-critic/master.md).
//
// PARKED as .staged. Leaf 02 re-activates ONLY this file. It pins:
//
//	F1 TestFreshCriticFlagOffDoesNotSpawnChild (C2/C3: flag off keeps the
//	   planner-loop critic path byte-identical)
//	F2 TestFreshCriticChildHasNoTranscript (C3: the round-1 child sees
//	   exactly one user message: draft + PlanCritiqueInput.Render())
//	F3 TestEmptyParaphraseIsBlocking (C4: an empty paraphrase on a
//	   non-empty draft is one synthetic blocking objection)
//
// Self-contained on purpose: helpers here are uniquely prefixed so this
// file compiles alone when the sibling .staged files are still parked.
package plancritic

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// freshCriticED is the em-dash the plan-dialect artifact grammar requires
// in "- `name` (kind) <ed> description" lines, built from an escape so
// this source file stays pure ASCII for the repo ascii gate.
const freshCriticED = "\u2014"

// freshCriticChatter is a scripted llm.Chatter that records every prompt
// and returns canned responses in order (last one reused on exhaustion).
var _ llm.Chatter = (*freshCriticChatter)(nil)

type freshCriticChatter struct {
	mu      sync.Mutex
	resps   []string
	prompts []string
	callIdx int
}

func (c *freshCriticChatter) Chat(_ context.Context, messages []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sb strings.Builder
	for _, m := range messages {
		sb.WriteString(string(m.Role))
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	c.prompts = append(c.prompts, sb.String())
	i := c.callIdx
	c.callIdx++
	resp := c.resps[len(c.resps)-1]
	if i < len(c.resps) {
		resp = c.resps[i]
	}
	return &llm.Response{Content: resp, FinishReason: "stop", Usage: llm.TokenUsage{TotalTokens: 5}}, nil
}

func (c *freshCriticChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *freshCriticChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "fresh-critic-e2e-model"}
}

func (c *freshCriticChatter) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callIdx
}

func (c *freshCriticChatter) promptCopy() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.prompts...)
}

// freshCriticDraft is a minimal plan-dialect v1 document that
// plan.CompileSealed accepts (same shape the internal critique pins use).
func freshCriticDraft() string {
	return "# Plan: Fresh critic e2e\n\n## Meta\n\n- task_id: t-fresh-e2e\n- version: 1\n- status: draft\n- updated: 2026-11-01\n\n" +
		"## Goal\n\nProduce the artifact end to end.\n\n## Decisions\n\n- Decision: do it directly " + freshCriticED +
		" Rationale: scope is small.\n\n## Open Questions\n\n## Phases\n\n### Phase 1: Build\n\nBuild the artifact.\n\n" +
		"**Produces:**\n\n- `fresh-e2e-artifact` (file) " + freshCriticED + " the finished artifact\n\n**Consumes:** none\n\n" +
		"**Steps:**\n\n1. Implement the artifact end to end [analyze]\n2. Verify the artifact [analyze] (needs: Phase1.S1)\n"
}

// freshCriticPlanner builds a StrategicPlanner over the scripted chatter
// with the compile pipeline on, using only exported construction surface.
func freshCriticPlanner(t *testing.T, chatter llm.Chatter) freshCriticFixture {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	msgBus := bus.New(nil, logger)
	t.Cleanup(func() { msgBus.Close() })
	store, err := task.NewStore(filepath.Join(t.TempDir(), "tasks.db"), logger)
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	reg := agent.NewAgentRegistry(agent.RegistryConfig{
		Logger:         logger,
		PlannerChatter: chatter,
	})
	if err := reg.RegisterSpec(&agent.AgentSpec{
		ID:      config.AgentIDPlanner,
		Name:    "Planner",
		Role:    agent.RoleExecutor,
		Enabled: true,
	}); err != nil {
		t.Fatalf("register planner spec: %v", err)
	}
	sp := agent.NewStrategicPlanner(agent.StrategicPlannerConfig{
		Registry:       reg,
		TaskStore:      store,
		StepStore:      store.StepStore(),
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         logger,
	})
	sp.SetPlanCompilerEnabled(true)
	sp.SetCritiqueMaxRounds(2)
	sp.SetCritiqueChatter(chatter)
	return freshCriticFixture{SP: sp, Store: store}
}

// freshCriticFixture bundles the planner with the store it was built on
// (the same store the flow reads, so seeding through it is authoritative).
type freshCriticFixture struct {
	SP    *agent.StrategicPlanner
	Store *task.Store
}

func freshCriticTask(t *testing.T, fx freshCriticFixture, id string) string {
	t.Helper()
	tsk := task.NewTask("fresh-critic-e2e", "comprehensive avatar upload feature")
	tsk.ID = id
	tsk.SetState(task.StatePlanning)
	if err := fx.Store.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return id
}

func freshCriticReq(taskID string) agent.PlanRequest {
	return agent.PlanRequest{
		TaskID:    taskID,
		SessionID: "sess-fresh-e2e",
		Input:     "comprehensive avatar upload feature",
		Intent:    "code",
		Mode:      "plan",
	}
}

// F1 (C2/C3): flag off keeps the critic on the planner-loop path. The
// critique chatter receives the critic prompt; no fresh-critic child is
// consulted.
func TestFreshCriticFlagOffDoesNotSpawnChild(t *testing.T) {
	chatter := &freshCriticChatter{resps: []string{
		freshCriticDraft(),   // draft fill
		`{"objections": []}`, // critic round 1: clean
	}}
	sp := freshCriticPlanner(t, chatter)
	sp.SP.SetFreshCriticEnabled(false) // C2 setter (RED today: undefined)
	sp.SP.SetCriticChatter(nil)        // C2: nil = planner chatter allowed for round 1
	taskID := freshCriticTask(t, sp, "task-fresh-off")

	res, err := sp.SP.PlanCritiqueFlow(context.Background(), freshCriticReq(taskID), &agent.PlanCritiqueInput{})
	if err != nil {
		t.Fatalf("PlanCritiqueFlow: %v", err)
	}
	if res.RoundsUsed != 1 {
		t.Fatalf("F1 (C3): rounds_used = %d, want 1 (clean round on the planner path)", res.RoundsUsed)
	}
	sawCriticPrompt := false
	for _, p := range chatter.promptCopy() {
		if strings.Contains(p, "plan critic") {
			sawCriticPrompt = true
		}
	}
	if !sawCriticPrompt {
		t.Fatal("F1 (C3): flag off must keep the critic prompt on the planner-loop critic path")
	}
}

// F2 (C3): flag on routes round 1 through the isolated critic child, which
// sees exactly one user message (draft + rendered evidence) and no
// transcript; the planner path never sees the critic prompt.
func TestFreshCriticChildHasNoTranscript(t *testing.T) {
	chatter := &freshCriticChatter{resps: []string{
		freshCriticDraft(),   // draft fill
		`{"objections": []}`, // unused on the fresh path
	}}
	fx := freshCriticPlanner(t, chatter)
	fx.SP.SetFreshCriticEnabled(true) // C2 setter (RED today: undefined)
	critic := &freshCriticChatter{resps: []string{
		`{"paraphrase":{"proposes":"upload avatar images","system_context":"single-user deployment"},"objections":[]}`,
	}}
	fx.SP.SetCriticChatter(critic) // C2: the round-1 child chatter (RED today: undefined)
	taskID := freshCriticTask(t, fx, "task-fresh-child")

	if _, err := fx.SP.PlanCritiqueFlow(context.Background(), freshCriticReq(taskID), &agent.PlanCritiqueInput{}); err != nil {
		t.Fatalf("PlanCritiqueFlow: %v", err)
	}
	if critic.callCount() != 1 {
		t.Fatalf("F2 (C3): fresh critic child calls = %d, want exactly 1", critic.callCount())
	}
	prompts := critic.promptCopy()
	if len(prompts) != 1 {
		t.Fatalf("F2 (C3): child saw %d prompts, want 1", len(prompts))
	}
	// C3: the child conversation is empty before the single user message.
	if got := strings.Count(prompts[0], "user: "); got != 1 {
		t.Fatalf("F2 (C3): child message list must carry exactly one user message, saw %d in %q", got, prompts[0])
	}
	if got := strings.Count(prompts[0], "assistant: "); got != 0 {
		t.Fatalf("F2 (C3): child must inherit no transcript, saw %d assistant messages", got)
	}
	if !strings.Contains(prompts[0], "Build the artifact") {
		t.Fatal("F2 (C3): child prompt must contain the draft markdown")
	}
	// The planner-side path must not receive the critic prompt.
	for _, p := range chatter.promptCopy() {
		if strings.Contains(p, "You are the plan critic") {
			t.Fatal("F2 (C3): flag on must move the critic prompt off the planner path")
		}
	}
}

// F3 (C4): a fresh-path reply whose paraphrase is empty on a non-empty
// draft yields one synthetic blocking objection, so the round is not clean
// and a revise cycle runs.
func TestEmptyParaphraseIsBlocking(t *testing.T) {
	chatter := &freshCriticChatter{resps: []string{
		freshCriticDraft(),   // draft fill
		freshCriticDraft(),   // revise round 1
		`{"objections": []}`, // unused
	}}
	fx := freshCriticPlanner(t, chatter)
	fx.SP.SetFreshCriticEnabled(true)
	critic := &freshCriticChatter{resps: []string{
		`{"paraphrase":{"proposes":"","system_context":""},"objections":[]}`,
	}}
	fx.SP.SetCriticChatter(critic)
	taskID := freshCriticTask(t, fx, "task-fresh-paraphrase")

	res, err := fx.SP.PlanCritiqueFlow(context.Background(), freshCriticReq(taskID), &agent.PlanCritiqueInput{})
	if err != nil {
		t.Fatalf("PlanCritiqueFlow: %v", err)
	}
	if res.RoundsUsed != 2 {
		t.Fatalf("F3 (C4): rounds_used = %d, want 2 (empty paraphrase must block the round, not seal clean)", res.RoundsUsed)
	}
	if res.KnownRisks < 1 {
		t.Fatalf("F3 (C4): known_risks = %d, want >= 1 (the synthetic blocking objection must surface)", res.KnownRisks)
	}
}
