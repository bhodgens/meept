package employee

// Issue #148 (1): resume prompt loss. Manager.ResumeApprovedPlan re-enters
// ApproveAndExecute with PlanRef{ID} only — the candidate goal prompt is not
// on the resume path. The fix persists the prompt onto the plan record at
// GoalLoop.Plan time (PlanPromptSetter seam) and rehydrates it through the
// plan-brief lookup at Execute time. These tests prove a resumed execution
// carries the SAME prompt a same-process approve would have executed with.

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// promptStoringPlanner is a stubPlanner that also satisfies
// PlanPromptSetter: the persisted prompts map mirrors internal/plan's
// plans.prompt column, and promptBriefLookup mirrors the daemon's
// planBriefLookup adapter reading it back.
type promptStoringPlanner struct {
	stubPlanner

	mu        sync.Mutex
	prompts   map[string]string
	briefs    map[string]PlanBrief // title/description snapshots like plan rows
	promptErr error
}

func newPromptStoringPlanner() *promptStoringPlanner {
	return &promptStoringPlanner{
		stubPlanner: *newStubPlanner(),
		prompts:     map[string]string{},
		briefs:      map[string]PlanBrief{},
	}
}

func (p *promptStoringPlanner) CreatePlan(ctx context.Context, title, description, projectID, sessionID string) (PlanRef, error) {
	ref, err := p.stubPlanner.CreatePlan(ctx, title, description, projectID, sessionID)
	if err != nil {
		return ref, err
	}
	p.mu.Lock()
	p.briefs[ref.ID] = PlanBrief{Title: title, Description: description}
	p.mu.Unlock()
	return ref, nil
}

func (p *promptStoringPlanner) SetPlanPrompt(_ context.Context, planID, prompt string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.promptErr != nil {
		return p.promptErr
	}
	p.prompts[planID] = prompt
	return nil
}

// persistedPrompt returns the prompt stored for planID ("" = none).
func (p *promptStoringPlanner) persistedPrompt(planID string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.prompts[planID]
}

// briefLookup is the resume-side rehydration source, shaped exactly like
// the daemon's planBriefLookup adapter over PlanManager.GetPlan.
func (p *promptStoringPlanner) briefLookup() PlanBriefLookup {
	return func(_ context.Context, planID string) (PlanBrief, bool, error) {
		p.mu.Lock()
		defer p.mu.Unlock()
		brief, ok := p.briefs[planID]
		if !ok {
			return PlanBrief{}, false, nil
		}
		brief.Prompt = p.prompts[planID]
		return brief, true, nil
	}
}

// TestPlan_PersistsCandidatePrompt proves the write half of the fix:
// GoalLoop.Plan hands the candidate prompt to a planner implementing
// PlanPromptSetter so it lands on the plan record.
func TestPlan_PersistsCandidatePrompt(t *testing.T) {
	planner := newPromptStoringPlanner()
	loop := NewGoalLoop("emp-prompt-persist", testTier1Constitution(), nil, nil).
		WithPlanner(planner)

	const prompt = "perform the risky escresume-escalate action now"
	ref, err := loop.Plan(context.Background(), CandidatePlan{
		Title:       "escalated: ESCRESUME-01",
		Description: "the escalating candidate",
		Prompt:      prompt,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := planner.persistedPrompt(ref.ID); got != prompt {
		t.Errorf("persisted prompt = %q, want %q", got, prompt)
	}
	if ref.Prompt != prompt {
		t.Errorf("in-memory ref.Prompt = %q, want %q", ref.Prompt, prompt)
	}
}

// TestPlan_PromptSetterFailureIsNonFatal proves a failed prompt write never
// fails plan creation — the live approve still carries the in-memory
// prompt, and only the resume rehydration degrades to the brief fallback.
func TestPlan_PromptSetterFailureIsNonFatal(t *testing.T) {
	planner := newPromptStoringPlanner()
	planner.promptErr = fmt.Errorf("store down")
	loop := NewGoalLoop("emp-prompt-write-err", testTier1Constitution(), nil, nil).
		WithPlanner(planner)

	ref, err := loop.Plan(context.Background(), CandidatePlan{
		Title:  "t",
		Prompt: "do the thing",
	})
	if err != nil {
		t.Fatalf("Plan must survive a prompt-persist failure: %v", err)
	}
	if ref.Prompt != "do the thing" {
		t.Errorf("ref.Prompt = %q, want the candidate prompt", ref.Prompt)
	}
}

// TestResumeApprovedPlan_ExecutesPersistedPrompt is the end-to-end proof for
// the issue: a plan created through GoalLoop.Plan (prompt persisted via the
// setter seam), then resumed through Manager.ResumeApprovedPlan — which
// hands ApproveAndExecute ONLY PlanRef{ID} — executes with the exact same
// candidate prompt, not a generic mandate.
func TestResumeApprovedPlan_ExecutesPersistedPrompt(t *testing.T) {
	m, gs, _ := newResumeTestManager(t)

	const candidatePrompt = "perform the risky escresume-escalate action now"

	// Rebuild the loop with a prompt-storing planner + the matching brief
	// lookup (the daemon wires both; the resume fixture loop has neither).
	executor := &capturingExecutor{stubExecutor: *newStubExecutor()}
	planner := newPromptStoringPlanner()
	loop := m.GetGoalLoop(resumeEmployeeID)
	if loop == nil {
		t.Fatal("goal loop not registered")
	}
	loop.WithExecutor(executor)
	loop.WithPlanner(planner)
	loop.SetPlanBriefLookup(planner.briefLookup())

	// GoalLoop.Plan creates the plan row (ID from the stub planner) and
	// persists the candidate prompt through PlanPromptSetter.
	ref, err := loop.Plan(context.Background(), CandidatePlan{
		Title:       "escalated: ESCRESUME-01",
		Description: "the escalating candidate for escresume-escalate",
		Prompt:      candidatePrompt,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	// Seed the authoritative plan→goal linkage the resume resolves through.
	seedActivePlan(t, m, gs, ref.ID)

	// Resume: Manager.ResumeApprovedPlan calls ApproveAndExecute with
	// PlanRef{ID} only (the executor ref the resume actually uses carries
	// NO prompt — that is the whole defect surface).
	if err := m.ResumeApprovedPlan(context.Background(), ref.ID, "user"); err != nil {
		t.Fatalf("ResumeApprovedPlan: %v", err)
	}

	prompts := executor.Prompts()
	if len(prompts) != 1 {
		t.Fatalf("executor turns = %d, want 1", len(prompts))
	}
	if prompts[0] != candidatePrompt {
		t.Errorf("resumed execution prompt = %q, want the exact persisted candidate prompt %q",
			prompts[0], candidatePrompt)
	}
}

// TestResumeApprovedPlan_LegacyPlanFallsBackToBrief pins the compatibility
// posture: a plan row with NO persisted prompt (legacy data predating the
// column) resumes with the title/description mandate exactly as before —
// the fix never changes behavior for rows without prompts.
func TestResumeApprovedPlan_LegacyPlanFallsBackToBrief(t *testing.T) {
	m, gs, _ := newResumeTestManager(t)

	executor := &capturingExecutor{stubExecutor: *newStubExecutor()}
	loop := m.GetGoalLoop(resumeEmployeeID)
	if loop == nil {
		t.Fatal("goal loop not registered")
	}
	loop.WithExecutor(executor)
	// Brief exists but carries no prompt (pre-#148 row).
	loop.SetPlanBriefLookup(func(_ context.Context, _ string) (PlanBrief, bool, error) {
		return PlanBrief{Title: "Old plan", Description: "written before prompts persisted"}, true, nil
	})

	const planID = "plan-legacy-row"
	seedActivePlan(t, m, gs, planID)
	if err := m.ResumeApprovedPlan(context.Background(), planID, "user"); err != nil {
		t.Fatalf("ResumeApprovedPlan: %v", err)
	}
	prompts := executor.Prompts()
	if len(prompts) != 1 {
		t.Fatalf("executor turns = %d, want 1", len(prompts))
	}
	want := fmt.Sprintf(approvedPlanMandate, planID, "Old plan", "written before prompts persisted")
	if prompts[0] != want {
		t.Errorf("legacy resume prompt = %q, want brief mandate %q", prompts[0], want)
	}
}
