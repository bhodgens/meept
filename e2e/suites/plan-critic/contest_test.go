//go:build e2e

// Gate test for the contest rebuttal path (leaf 04; contract C6 in
// docs/plans/20261101-fresh-context-critic/master.md).
//
// PARKED as .staged. Leaf 04 re-activates ONLY this file. It pins:
//
//	F6 TestContestWithoutEvidenceNotApplied (C6: a contest whose Evidence
//	   is not a verbatim substring of PlanCritiqueInput.Render() is not a
//	   contest; it is recorded contested-unproven, rolls into Known
//	   Risks, and never clears the objection as answered)
//
// Self-contained on purpose: helpers are uniquely prefixed so this file
// compiles alone when the sibling .staged files are still parked.
package plancritic

import (
	"context"
	"encoding/json"
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

// contestED is the plan-dialect em-dash via escape (ASCII-only source).
const contestED = "\u2014"

// contestChatter is a scripted llm.Chatter with a fixed model ref.
type contestChatter struct {
	modelID string
	mu      sync.Mutex
	resps   []string
	prompts []string
	callIdx int
}

func (c *contestChatter) Chat(_ context.Context, messages []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
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

func (c *contestChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *contestChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: c.modelID}
}

func (c *contestChatter) promptCopy() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.prompts...)
}

// contestDraft is a minimal plan-dialect v1 document the compiler
// accepts (leaf-05 zero-decision rule included: the phase's step
// carries a mutating [code] hint, so Produces must name a decision
// artifact).
func contestDraft() string {
	return "# Plan: Contest e2e\n\n## Meta\n\n- task_id: t-contest-e2e\n- version: 1\n- status: draft\n- updated: 2026-11-01\n\n" +
		"## Goal\n\nProduce the artifact end to end.\n\n## Decisions\n\n- Decision: do it directly " + contestED +
		" Rationale: scope is small.\n\n## Open Questions\n\n## Phases\n\n### Phase 1: Build\n\nBuild the artifact.\n\n" +
		"**Produces:**\n\n- `contest-e2e-artifact` (file) " + contestED + " the finished artifact\n- `contest-e2e-decision` (decision) " + contestED + " build approach settled: single-phase direct build\n\n**Consumes:** none\n\n" +
		"**Steps:**\n\n1. Implement the artifact end to end [code]\n2. Verify the artifact [analyze] (needs: Phase1.S1)\n"
}

// contestFixture bundles the planner with the store it was built on.
type contestFixture struct {
	SP    *agent.StrategicPlanner
	Store *task.Store
}

// contestPlanner builds a planner wired to the scripted chatter.
func contestPlanner(t *testing.T, chatter llm.Chatter) contestFixture {
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
	return contestFixture{SP: sp, Store: store}
}

func contestTask(t *testing.T, fx contestFixture, id string) string {
	t.Helper()
	tsk := task.NewTask("contest-e2e", "comprehensive avatar upload feature")
	tsk.ID = id
	tsk.SetState(task.StatePlanning)
	if err := fx.Store.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return id
}

// F6 (C6): the planner tries to contest a blocking objection with a
// rebuttal whose Evidence quotes nothing from the rendered critique
// input. The contest must NOT be applied: the objection is not answered
// in-cycle (no clean seal on that round) and the failed contest lands in
// Known Risks as contested-unproven.
func TestContestWithoutEvidenceNotApplied(t *testing.T) {
	chatter := &contestChatter{modelID: "contest-model", resps: []string{
		contestDraft(), // draft fill
		// critic round 1: one blocking objection on the Phases section
		`{"objections": [{"section": "Phases", "objection": "the build step names no failure handling", "severity": "blocking"}]}`,
		// revise round 1: revised draft plus an evidence-free contest
		contestDraft() + "\n```json\n{\"rebuttals\": [{\"section\": \"Phases\", \"targets\": \"the build step names no failure handling\", \"evidence\": \"team lore says uploads always worked\", \"verdict\": \"contest\"}]}\n```\n",
		// critic round 2: still blocking; the unproven contest answered nothing
		`{"objections": [{"section": "Phases", "objection": "still no failure handling", "severity": "blocking"}]}`,
	}}
	fx := contestPlanner(t, chatter)
	taskID := contestTask(t, fx, "task-contest-unproven")

	// Evidence the critic could have read: a FailureBlock that Render()
	// emits. The planner's quoted "team lore" sentence above is NOT in
	// this render, so the C6 quote check must reject it.
	input := &agent.PlanCritiqueInput{FailureBlock: "## Previous attempt failed\n\nthe previous run failed on avatar sizing."}

	res, err := fx.SP.PlanCritiqueFlow(context.Background(), agent.PlanRequest{
		TaskID:    taskID,
		SessionID: "sess-contest-e2e",
		Input:     "comprehensive avatar upload feature",
		Intent:    "code",
		Mode:      "plan",
	}, input)
	if err != nil {
		t.Fatalf("PlanCritiqueFlow: %v", err)
	}

	// The contest was unproven, so the blocking objection it tried to
	// answer stays open: round 2 must run and end exhausted, not clean.
	if res.RoundsUsed < 2 {
		t.Fatalf("F6 (C6): rounds_used = %d, want >= 2 (an evidence-free contest must not close the round)", res.RoundsUsed)
	}
	if res.KnownRisks < 1 {
		t.Fatalf("F6 (C6): known_risks = %d, want >= 1 (contested-unproven must roll into Known Risks)", res.KnownRisks)
	}
	// The revised draft in the store must carry the failed contest in
	// its Known Risks as contested-unproven - never silently accepted,
	// never silently dropped. The draft rides the task's plan_draft
	// metadata bag (PlanDraft JSON); parse it locally.
	tsk, err := fx.Store.GetByID(taskID)
	if err != nil || tsk == nil {
		t.Fatalf("task after flow: %v", err)
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(tsk.Metadata, &meta); err != nil {
		t.Fatalf("unmarshal task metadata: %v", err)
	}
	rawDraft, ok := meta["plan_draft"]
	if !ok {
		t.Fatal("F6 (C6): no plan_draft on task metadata after the flow")
	}
	var bag struct {
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(rawDraft, &bag); err != nil {
		t.Fatalf("unmarshal plan draft bag: %v", err)
	}
	if !strings.Contains(bag.Markdown, "[contested-unproven]") {
		t.Fatalf("F6 (C6): the failed contest must be recorded as contested-unproven in the draft's Known Risks; draft tail: %.300s", bag.Markdown[max(0, len(bag.Markdown)-300):])
	}
	// The revise prompt must carry the objection (it was not accepted as
	// answered) and Known Risks must surface the failed contest.
	sawObjection := false
	for _, p := range chatter.promptCopy() {
		if strings.Contains(p, "the build step names no failure handling") {
			sawObjection = true
		}
	}
	if !sawObjection {
		t.Fatal("F6 (C6): the contested objection was silently accepted; it must stay open in the refine path")
	}
}
