package employee

// Gate tests for Contract C1 (tree docs/plans/20261113-escalation-resume-and-hardening,
// leaf 01): Manager.ResumeApprovedPlan(ctx, planID, callerID) resumes an
// APPROVED plan on the owning employee's live GoalLoop exactly once per
// process. Deliberate skips (unknown plan, busy loop, already resumed —
// including by the parked-episode resume path) return nil + INFO; real
// failures return a wrapped error.
//
// RED today: the method is undefined, so this file breaks compilation of the
// package's test binary naming m.ResumeApprovedPlan — that compile failure
// IS the T0 gate. Green after leaf 01 lands.

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
)

// resumeEmployeeID is the hired employee every resume fixture uses.
const resumeEmployeeID = "emp-resume"

// newResumeTestManager builds a Manager over real shared SQLite stores with
// one hired tier-2 employee and a live GoalLoop registered (stub executor +
// reflector, mirroring the goal_loop_test.go doubles — no hand-built
// manager doubles). The goal store is handed back so tests can seed
// ActivePlanIDs, the authoritative plan→goal linkage (G2) the resume path
// resolves through.
func newResumeTestManager(t *testing.T) (*Manager, *GoalStore, *stubExecutor) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "shared.db")
	botStore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	t.Cleanup(func() { botStore.Close() })
	constStore, err := NewConstitutionStore(dbPath, nil)
	if err != nil {
		t.Fatalf("constitution store: %v", err)
	}
	t.Cleanup(func() { constStore.Close() })
	goalStore, err := NewGoalStore(dbPath, nil)
	if err != nil {
		t.Fatalf("goal store: %v", err)
	}
	t.Cleanup(func() { goalStore.Close() })

	m := NewManagerWithStores(bot.NewManager(botStore, nil), botStore, constStore, goalStore, nil, slog.New(slog.DiscardHandler))

	ctx := context.Background()
	c := validConstitution() // tier 2, escalates_to ["user"]
	if _, err := m.Hire(ctx, HireRequest{
		ID:           resumeEmployeeID,
		Name:         resumeEmployeeID,
		Prompt:       "test prompt",
		Enabled:      true,
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Constitution: constitutionToMap(t, c),
	}); err != nil {
		t.Fatalf("hire: %v", err)
	}

	reflector := newStubReflector()
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)
	executor := newStubExecutor()
	// The loop carries a capturing episode parker so the park-episode resume
	// interplay (T4) can be exercised through the real ResumeGoalEpisode
	// path; the parker is idle unless a provider wait parks an episode.
	parker, _ := newCapturingParker(t, parkTestPolicy())
	loop := NewGoalLoop(resumeEmployeeID, &c, goalStore, slog.New(slog.DiscardHandler)).
		WithExecutor(executor).
		WithReflector(reflector).
		WithEpisodeParker(parker)
	m.RegisterGoalLoop(resumeEmployeeID, loop)

	return m, goalStore, executor
}

// seedActivePlan creates a goal for the fixture employee and records planID
// in its ActivePlanIDs (G2 authoritative linkage).
func seedActivePlan(t *testing.T, m *Manager, gs *GoalStore, planID string) {
	t.Helper()
	ctx := context.Background()
	goal, err := m.CreateGoal(ctx, GoalSpec{
		EmployeeID: resumeEmployeeID,
		Title:      "keep the resume fixture green",
		Mandate:    "pursue the resume fixture mandate to completion",
		Source:     "user",
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	goal.AddActivePlan(planID)
	if err := gs.Update(ctx, goal); err != nil {
		t.Fatalf("seed active plan: %v", err)
	}
}

// T1 (happy path): Given an approved plan owned by a goal with a live loop,
// When ResumeApprovedPlan runs, Then ApproveAndExecute ran once and the
// error is nil.
func TestManager_ResumeApprovedPlan_Executes(t *testing.T) {
	m, gs, executor := newResumeTestManager(t)
	const planID = "plan-resume-1"
	seedActivePlan(t, m, gs, planID)

	if err := m.ResumeApprovedPlan(context.Background(), planID, "user"); err != nil {
		t.Fatalf("ResumeApprovedPlan error: %v", err)
	}
	if got := executor.CallCount(); got != 1 {
		t.Errorf("executor called %d times, want exactly 1", got)
	}
}

// T2 (unknown-plan skip): a planID no goal owns is a nil-error INFO skip —
// no execution, no panic, nothing queued.
func TestManager_ResumeApprovedPlan_UnknownPlanSkips(t *testing.T) {
	m, gs, executor := newResumeTestManager(t)
	seedActivePlan(t, m, gs, "plan-resume-real")

	if err := m.ResumeApprovedPlan(context.Background(), "plan-resume-absent", "user"); err != nil {
		t.Fatalf("unknown-plan resume returned %v, want nil skip", err)
	}
	if got := executor.CallCount(); got != 0 {
		t.Errorf("executor called %d times on unknown-plan skip, want 0", got)
	}
}

// T3 (busy-loop skip): the employee's loop is mid-round (invoke mutex held —
// the same serialization triggerViaGoalLoop uses). Resume must SKIP with nil
// — never block/queue behind the mutex (C1: the next scheduled Trigger picks
// up normal rounds).
func TestManager_ResumeApprovedPlan_BusyLoopSkips(t *testing.T) {
	m, gs, executor := newResumeTestManager(t)
	const planID = "plan-resume-busy"
	seedActivePlan(t, m, gs, planID)

	invokeMu := m.acquireInvokeMutex(resumeEmployeeID)
	invokeMu.Lock()
	release := make(chan struct{})
	go func() {
		<-release
		invokeMu.Unlock()
	}()

	done := make(chan error, 1)
	go func() {
		done <- m.ResumeApprovedPlan(context.Background(), planID, "user")
	}()

	select {
	case err := <-done:
		close(release)
		if err != nil {
			t.Fatalf("busy-loop resume returned %v, want nil skip", err)
		}
	case <-time.After(300 * time.Millisecond):
		close(release)
		<-done // drain once unblocked so the goroutine never leaks
		t.Fatal("resume blocked on the busy loop's invoke mutex instead of skipping — C1 forbids queueing")
	}
	if got := executor.CallCount(); got != 0 {
		t.Errorf("executor called %d times during busy skip, want 0", got)
	}
}

// T4 (double-resume skip): resume is once-per-process per planID — a second
// call is a nil skip with execution count unchanged. The park-episode
// interplay is covered too: a plan ApproveAndExecute'd through the REAL
// parked-episode resume path (park.go "approve" phase) must also make the
// bus-side resume a skip, so the two paths can never double-execute (R5).
func TestManager_ResumeApprovedPlan_DoubleResumeSkips(t *testing.T) {
	t.Run("second direct resume skips", func(t *testing.T) {
		m, gs, executor := newResumeTestManager(t)
		const planID = "plan-resume-double"
		seedActivePlan(t, m, gs, planID)
		ctx := context.Background()

		if err := m.ResumeApprovedPlan(ctx, planID, "user"); err != nil {
			t.Fatalf("first resume error: %v", err)
		}
		if got := executor.CallCount(); got != 1 {
			t.Fatalf("executor called %d times after first resume, want 1", got)
		}
		if err := m.ResumeApprovedPlan(ctx, planID, "user"); err != nil {
			t.Fatalf("second resume returned %v, want nil skip", err)
		}
		if got := executor.CallCount(); got != 1 {
			t.Errorf("execution count %d after double resume, want 1", got)
		}
	})

	t.Run("park-resumed plan skips bus resume", func(t *testing.T) {
		m, gs, executor := newResumeTestManager(t)
		const planID = "plan-resume-park"
		seedActivePlan(t, m, gs, planID)
		ctx := context.Background()

		// Park-episode resume: the parked "approve" phase re-enters
		// ApproveAndExecute through GoalLoop.ResumeGoalEpisode (the ONLY
		// production caller today). This must record the planID exactly as
		// a direct resume would.
		loop := m.GetGoalLoop(resumeEmployeeID)
		if loop == nil {
			t.Fatal("goal loop not registered")
		}
		loop.ResumeGoalEpisode(ctx, agent.ParkedTurnRecord{
			AgentID:     resumeEmployeeID,
			TurnPayload: mustMarshalGoalPayload(t, goalTurnPayload{Phase: "approve", Plan: &PlanRef{ID: planID}}),
		})
		if got := executor.CallCount(); got != 1 {
			t.Fatalf("park resume executed %d times, want 1", got)
		}

		// The bus-side resume for the same plan must now be a skip.
		if err := m.ResumeApprovedPlan(ctx, planID, "user"); err != nil {
			t.Fatalf("resume after park execution returned %v, want nil skip", err)
		}
		if got := executor.CallCount(); got != 1 {
			t.Errorf("execution count %d after park+bus resume, want 1 (no double-execute)", got)
		}
	})
}
