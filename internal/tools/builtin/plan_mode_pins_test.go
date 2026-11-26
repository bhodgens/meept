package builtin

// Bughunt 2026-11-25 pins for exit_plan_mode:
//   - A6-11: classifyDecision must NOT approve on a substring match —
//     "do not approve" / "disapprove" / "don't approve" / "reject" all mean
//     keep-planning; only a clear word-boundary affirmative approves.
//   - A6-6: one shared tool instance with per-turn injections must isolate
//     concurrent loops: the per-turn tool CONTEXT (session + responder) is
//     authoritative, and the mutex-guarded fields never race.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/tools"
)

func TestClassifyDecision_NegationsNeverApprove(t *testing.T) {
	negations := []string{
		"do not approve",
		"Do NOT approve",
		"don't approve",
		"dont approve this",
		"disapprove",
		"I disapprove of this plan",
		"reject",
		"reject it, the scope is wrong",
		"keep planning",
		"not yet",
		"can't approve this",
		"cannot approve",
		"",
		"maybe later",
		"hold on, the retry budget is wrong",
	}
	for _, s := range negations {
		if got := classifyDecision(s); got != keepPlanning {
			t.Errorf("classifyDecision(%q) = %v, want keepPlanning (negation/ambiguous must never approve)", s, got)
		}
	}
}

func TestClassifyDecision_AffirmativesApprove(t *testing.T) {
	affirmatives := []string{
		"approve",
		"Approve",
		"approved",
		"approve it",
		"I approve",
		"lgtm",
		"LGTM, ship it",
		"go ahead",
		"proceed",
	}
	for _, s := range affirmatives {
		if got := classifyDecision(s); got != approvePlan {
			t.Errorf("classifyDecision(%q) = %v, want approvePlan (clear affirmative)", s, got)
		}
	}
}

func TestMatchWordBoundary(t *testing.T) {
	// Non-vacuity for the anchoring itself: a substring inside a longer
	// word must not match.
	if matchWordBoundary("disapprove", "approve") {
		t.Error("matchWordBoundary(disapprove, approve) = true, want false")
	}
	if matchWordBoundary("unapproved", "approve") {
		t.Error("matchWordBoundary(unapproved, approve) = true, want false")
	}
	if !matchWordBoundary("approve it", "approve") {
		t.Error("edge-delimited match expected")
	}
	if !matchWordBoundary("I approve.", "approve") {
		t.Error("punctuation-delimited match expected")
	}
}

// recordingGate captures the session id each CreatePlan/ApprovePlan call
// was made under (the cross-session leak observable for A6-6).
type recordingGate struct {
	mu    sync.Mutex
	calls []string // "<op>:<sessionID>"
	n     int
}

func (g *recordingGate) CreatePlan(_ context.Context, title, _, _, _, sessionID string) (*plan.Plan, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, "create:"+sessionID)
	g.n++
	return &plan.Plan{ID: fmt.Sprintf("plan-%d", g.n), Title: title}, nil
}

func (g *recordingGate) SubmitPlan(_ context.Context, planID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, "submit:"+planID)
	return nil
}

func (g *recordingGate) ApprovePlan(_ context.Context, planID, sessionID, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, "approve:"+sessionID)
	return nil
}

func (g *recordingGate) snapshot() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.calls...)
}

// TestExitPlanMode_ConcurrentLoopsAreIsolated is the A6-6 pin: ONE shared
// tool instance, two concurrent session loops. Each turn's context carries
// its OWN session id and responder; the fields hold the OTHER loop's
// LAST-INJECTED values (exactly the racing shape the old struct-field
// design had: session B's injection overwrote session A's between A's
// injection and A's Execute). Each Execute must resolve its own session at
// the gate and present through its own responder — never the injected field
// owner's.
func TestExitPlanMode_ConcurrentLoopsAreIsolated(t *testing.T) {
	ctrl := NewPlanModeController(nil)
	if err := ctrl.SetPlanMode("sess-A", true); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.SetPlanMode("sess-B", true); err != nil {
		t.Fatal(err)
	}

	gate := &recordingGate{}
	tool := NewExitPlanModeTool(ctrl)
	tool.SetPlanGate(gate)

	// Simulate loop B's per-turn injection landing on the SHARED fields
	// (the old race window): whatever A's turn reads from the fields would
	// be B's state.
	var mu sync.Mutex
	bPresented := 0
	tool.SetSessionID("sess-B")
	tool.SetResponseFunc(func(_ context.Context, _ string, _ []string) (string, error) {
		mu.Lock()
		bPresented++
		mu.Unlock()
		return "approve", nil
	})

	// A's turn: responder + session ride the per-turn context only.
	aCtx := tools.ContextWithSessionID(context.Background(), "sess-A")
	aCtx = tools.ContextWithTurnResponder(aCtx, func(_ context.Context, _ string, _ []string) (string, error) {
		return "approve", nil
	})

	// Non-vacuity pre-image: with the old shape (plain t.sessionID read)
	// this Execute would file the plan under sess-B. With the fix it must
	// land on sess-A.
	if _, err := tool.Execute(aCtx, map[string]any{"plan": "# A plan"}); err != nil {
		t.Fatalf("A execute: %v", err)
	}
	for _, c := range gate.snapshot() {
		if strings.Contains(c, "sess-B") {
			t.Fatalf("A's turn leaked into session B at the gate: %v", gate.snapshot())
		}
	}
	if !containsStr(gate.snapshot(), "create:sess-A") {
		t.Fatalf("A's plan must land on sess-A, got %v", gate.snapshot())
	}
	// Responder isolation: A's Execute presented through A's ctx-carried
	// responder — B's field-injected responder was never invoked.
	mu.Lock()
	bp := bPresented
	mu.Unlock()
	if bp != 0 {
		t.Fatalf("A's turn presented through B's injected responder (%d calls) — responder leaked across loops", bp)
	}

	// Concurrent hammering of the injection seam WHILE executes run: the
	// mutex-guarded fields must not race (run this test with -race).
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				tool.SetSessionID(fmt.Sprintf("sess-%d", i))
				tool.SetResponseFunc(func(_ context.Context, _ string, _ []string) (string, error) {
					return "keep planning", nil
				})
				return
			}
			ctx := tools.ContextWithSessionID(context.Background(), "sess-A")
			ctx = tools.ContextWithTurnResponder(ctx, func(_ context.Context, _ string, _ []string) (string, error) {
				return "keep planning", nil
			})
			// Plan mode for sess-A may have been cleared by the first
			// approve; either branch is race-free — the point is the
			// concurrent field access.
			_, _ = tool.Execute(ctx, map[string]any{"plan": "# race probe"})
		}(i)
	}
	wg.Wait()
}

// TestExitPlanMode_LegacyFieldFallback pins that direct callers whose ctx
// carries no session/responder still work off the injected fields (the
// pre-existing wiring tests' shape) — the fix is additive, not a behavior
// cliff.
func TestExitPlanMode_LegacyFieldFallback(t *testing.T) {
	ctrl := NewPlanModeController(nil)
	if err := ctrl.SetPlanMode("sess-legacy", true); err != nil {
		t.Fatal(err)
	}
	gate := &recordingGate{}
	tool := NewExitPlanModeTool(ctrl)
	tool.SetPlanGate(gate)
	tool.SetSessionID("sess-legacy")
	tool.SetResponseFunc(func(_ context.Context, _ string, _ []string) (string, error) {
		return "approve", nil
	})
	if _, err := tool.Execute(context.Background(), map[string]any{"plan": "# legacy"}); err != nil {
		t.Fatalf("legacy execute: %v", err)
	}
	if !containsStr(gate.snapshot(), "create:sess-legacy") {
		t.Fatalf("legacy field fallback broken: %v", gate.snapshot())
	}
}

func containsStr(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
