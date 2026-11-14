package employee

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// capturing user message comes from capturingExecutor in goal_loop_test.go.

func TestExecute_EmptyPromptFillsPlanBrief(t *testing.T) {
	const (
		planID = "plan-roof-1"
		title  = "Roof repair"
		desc   = "Call the client and confirm the crew finished"
	)
	exec := &capturingExecutor{}
	var calls atomic.Int32
	loop := NewGoalLoop("emp-brief", testTier1Constitution(), nil, nil).
		WithExecutor(exec)
	loop.SetPlanBriefLookup(func(ctx context.Context, id string) (PlanBrief, bool, error) {
		calls.Add(1)
		if id != planID {
			t.Errorf("lookup plan id = %q, want %q", id, planID)
		}
		return PlanBrief{Title: title, Description: desc}, true, nil
	})

	if _, err := loop.Execute(context.Background(), PlanRef{ID: planID}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	prompts := exec.Prompts()
	if len(prompts) != 1 {
		t.Fatalf("executor calls = %d, want 1", len(prompts))
	}
	msg := prompts[0]
	for _, want := range []string{planID, title, desc, "Title:", "Description:", "Approved plan"} {
		if !strings.Contains(msg, want) {
			t.Errorf("user message missing %q:\n%s", want, msg)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("lookup calls = %d, want 1", calls.Load())
	}
}

func TestExecute_PlanBriefMissUsesGenericMandate(t *testing.T) {
	const planID = "plan-missing"
	exec := &capturingExecutor{}
	loop := NewGoalLoop("emp-brief-miss", testTier1Constitution(), nil, nil).
		WithExecutor(exec)
	loop.SetPlanBriefLookup(func(ctx context.Context, id string) (PlanBrief, bool, error) {
		return PlanBrief{}, false, nil
	})

	if _, err := loop.Execute(context.Background(), PlanRef{ID: planID}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	prompts := exec.Prompts()
	if len(prompts) != 1 {
		t.Fatalf("executor calls = %d, want 1", len(prompts))
	}
	want := "[plan " + planID + "] execute per your mandate"
	if prompts[0] != want {
		t.Errorf("user message = %q, want %q", prompts[0], want)
	}
}

func TestExecute_PlanBriefErrorUsesGenericMandate(t *testing.T) {
	const planID = "plan-err"
	exec := &capturingExecutor{}
	loop := NewGoalLoop("emp-brief-err", testTier1Constitution(), nil, nil).
		WithExecutor(exec)
	loop.SetPlanBriefLookup(func(ctx context.Context, id string) (PlanBrief, bool, error) {
		return PlanBrief{}, false, errors.New("store down")
	})

	if _, err := loop.Execute(context.Background(), PlanRef{ID: planID}); err != nil {
		t.Fatalf("Execute must not fail when brief lookup errors: %v", err)
	}
	prompts := exec.Prompts()
	if len(prompts) != 1 {
		t.Fatalf("executor calls = %d, want 1", len(prompts))
	}
	want := "[plan " + planID + "] execute per your mandate"
	if prompts[0] != want {
		t.Errorf("user message = %q, want %q", prompts[0], want)
	}
}

func TestExecute_NonEmptyPromptSkipsPlanBriefLookup(t *testing.T) {
	const prompt = "do exactly this and nothing else"
	exec := &capturingExecutor{}
	var calls atomic.Int32
	loop := NewGoalLoop("emp-brief-skip", testTier1Constitution(), nil, nil).
		WithExecutor(exec)
	loop.SetPlanBriefLookup(func(ctx context.Context, id string) (PlanBrief, bool, error) {
		calls.Add(1)
		return PlanBrief{Title: "should not appear", Description: "neither should this"}, true, nil
	})

	if _, err := loop.Execute(context.Background(), PlanRef{ID: "plan-has-prompt", Prompt: prompt}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("lookup calls = %d, want 0 (non-empty Prompt)", calls.Load())
	}
	prompts := exec.Prompts()
	if len(prompts) != 1 {
		t.Fatalf("executor calls = %d, want 1", len(prompts))
	}
	if prompts[0] != prompt {
		t.Errorf("user message = %q, want verbatim prompt %q", prompts[0], prompt)
	}
}

func TestExecute_NilPlanBriefLookupUsesGenericMandate(t *testing.T) {
	const planID = "plan-nolookup"
	exec := &capturingExecutor{}
	loop := NewGoalLoop("emp-brief-nil", testTier1Constitution(), nil, nil).
		WithExecutor(exec)

	if _, err := loop.Execute(context.Background(), PlanRef{ID: planID}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	prompts := exec.Prompts()
	if len(prompts) != 1 {
		t.Fatalf("executor calls = %d, want 1", len(prompts))
	}
	want := "[plan " + planID + "] execute per your mandate"
	if prompts[0] != want {
		t.Errorf("user message = %q, want %q", prompts[0], want)
	}
}

func TestSetPlanBriefLookup_NilIgnored(t *testing.T) {
	loop := NewGoalLoop("emp-brief-set", testTier1Constitution(), nil, nil)
	loop.SetPlanBriefLookup(func(ctx context.Context, id string) (PlanBrief, bool, error) {
		return PlanBrief{Title: "kept"}, true, nil
	})
	loop.SetPlanBriefLookup(nil)
	loop.mu.Lock()
	fn := loop.planBriefLookup
	loop.mu.Unlock()
	if fn == nil {
		t.Fatal("SetPlanBriefLookup(nil) cleared an installed lookup")
	}
}

func TestExecute_EmptyBriefFieldsStillLabeled(t *testing.T) {
	const planID = "plan-empty-fields"
	exec := &capturingExecutor{}
	loop := NewGoalLoop("emp-brief-empty", testTier1Constitution(), nil, nil).
		WithExecutor(exec)
	loop.SetPlanBriefLookup(func(ctx context.Context, id string) (PlanBrief, bool, error) {
		return PlanBrief{Title: "Only title"}, true, nil
	})

	if _, err := loop.Execute(context.Background(), PlanRef{ID: planID}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	msg := exec.Prompts()[0]
	if !strings.Contains(msg, "Title: Only title") || !strings.Contains(msg, "Description:") {
		t.Errorf("empty description must still carry the label:\n%s", msg)
	}
}
