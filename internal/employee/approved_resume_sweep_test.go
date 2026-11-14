package employee

// Tests for the stranded-approval fix (tree
// docs/plans/20261113-escalation-resume-and-hardening, bughunt wave):
//   - StartApprovedPlanResumeSweep reconciles approved-but-unexecuted plans
//     the plan.approved bus event missed, including the busy-loop TryLock
//     skip (the pre-sweep code stranded those forever: nothing revisits an
//     approved plan — pending_approval->approved is pinned one-way and
//     decideTier2/3 only create NEW plans);
//   - the SetActivePlan ActivePlanIDs clobber (a post-approval
//     SetActivePlan(B) once overwrote the list, dropping a concurrently
//     pending plan A tracked by AddActivePlan — breaking the
//     MaxActivePlans cap and lookupGoalByPlanID's resume linkage);
//   - the D4 empty-allowlist widening (a hint disjoint from the base
//     allowlist once assigned the EMPTY intersection, and empty means
//     inherit-everything — a silent widening past the restricted base).

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
)

// stubApprovedLister is a controllable PlanApprovedLister for sweep tests.
type stubApprovedLister struct {
	mu    sync.Mutex
	ids   []string
	err   error
	calls int
}

func (s *stubApprovedLister) ApprovedPlanIDs(_ context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return append([]string(nil), s.ids...), nil
}

func (s *stubApprovedLister) setIDs(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ids = ids
}

func (s *stubApprovedLister) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// waitForSweep polls until cond holds or the 3s deadline expires.
func waitForSweep(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("sweep condition not met within 3s")
}

// T5 (pin: busy-at-approval + sweep retry): the loop is busy mid-round at
// approve time — the direct ResumeApprovedPlan call TryLock-skips — then the
// sweep resumes the plan after the round completes (the invoke mutex is
// released). This is the exact stranded-approval shape: the pre-sweep code
// logged a false "next scheduled trigger resumes normal rounds" and the
// plan was never revisited.
func TestApprovedResumeSweep_ResumesAfterBusyRoundCompletes(t *testing.T) {
	m, gs, executor := newResumeTestManager(t)
	const planID = "plan-sweep-busy"
	seedActivePlan(t, m, gs, planID)
	lister := &stubApprovedLister{}
	lister.setIDs(planID)

	ctx := context.Background()
	// The loop is busy at approve/resume time.
	invokeMu := m.acquireInvokeMutex(resumeEmployeeID)
	invokeMu.Lock()

	if err := m.ResumeApprovedPlan(ctx, planID, "user"); err != nil {
		t.Fatalf("busy-time resume returned %v, want nil skip", err)
	}
	if got := executor.CallCount(); got != 0 {
		t.Fatalf("executor called %d times while busy, want 0 (TryLock skip)", got)
	}

	// Arm the sweep while still busy: the first pass must also skip.
	m.SetPlanApprovedLister(lister)
	m.StartApprovedPlanResumeSweep(ctx, 20*time.Millisecond)
	t.Cleanup(func() { m.StopApprovedPlanResumeSweep() })
	time.Sleep(60 * time.Millisecond)
	if got := executor.CallCount(); got != 0 {
		t.Fatalf("executor called %d times while loop still busy, want 0", got)
	}

	// The round completes.
	invokeMu.Unlock()

	// The sweep's next pass resumes the plan.
	waitForSweep(t, func() bool { return executor.CallCount() >= 1 })
	if got := executor.CallCount(); got != 1 {
		t.Errorf("executor called %d times after sweep resume, want exactly 1", got)
	}
}

// T6 (pin: boot-time sweep covers the restart shape): an approved plan
// persisted by a PREVIOUS process (empty resumedPlans — the fixture manager
// is fresh) is resumed by the sweep's immediate first pass, no bus event
// required. This is the restart-between-approval-and-resume coverage.
func TestApprovedResumeSweep_BootFirstPassResumesStrandedPlan(t *testing.T) {
	m, gs, executor := newResumeTestManager(t)
	const planID = "plan-sweep-boot"
	seedActivePlan(t, m, gs, planID)
	lister := &stubApprovedLister{}
	lister.setIDs(planID)

	m.SetPlanApprovedLister(lister)
	m.StartApprovedPlanResumeSweep(context.Background(), time.Hour) // one pass effectively
	t.Cleanup(func() { m.StopApprovedPlanResumeSweep() })

	waitForSweep(t, func() bool { return executor.CallCount() >= 1 })
	if got := executor.CallCount(); got != 1 {
		t.Errorf("executor called %d times after boot pass, want exactly 1", got)
	}
}

// T7 (pin: once-map idempotence): a plan already resumed by this process is
// a skip for the sweep — the sweep pass and a direct resume can never
// double-execute.
func TestApprovedResumeSweep_AlreadyResumedPlanSkips(t *testing.T) {
	m, gs, executor := newResumeTestManager(t)
	const planID = "plan-sweep-once"
	seedActivePlan(t, m, gs, planID)

	// Direct resume first.
	if err := m.ResumeApprovedPlan(context.Background(), planID, "user"); err != nil {
		t.Fatalf("direct resume error: %v", err)
	}

	lister := &stubApprovedLister{}
	lister.setIDs(planID)
	m.SetPlanApprovedLister(lister)
	m.StartApprovedPlanResumeSweep(context.Background(), 10*time.Millisecond)
	t.Cleanup(func() { m.StopApprovedPlanResumeSweep() })
	time.Sleep(100 * time.Millisecond)

	if got := executor.CallCount(); got != 1 {
		t.Errorf("executor called %d times (sweep ran over an already-resumed plan), want exactly 1", got)
	}
	if lister.callCount() == 0 {
		t.Error("sweep never listed approved plans (first pass did not run)")
	}
}

// T8 (pin: nil lister = sweep disabled): without a wired lister the sweep
// does not start and nothing panics.
func TestApprovedResumeSweep_NilListerNoop(t *testing.T) {
	m, _, _ := newResumeTestManager(t)
	m.StartApprovedPlanResumeSweep(context.Background(), 10*time.Millisecond)
	m.StopApprovedPlanResumeSweep() // must not hang: never started
}

// T9 (pin: lister error is a logged skip, sweep keeps running).
func TestApprovedResumeSweep_ListerErrorRetries(t *testing.T) {
	m, gs, _ := newResumeTestManager(t)
	seedActivePlan(t, m, gs, "plan-sweep-err")
	lister := &stubApprovedLister{err: errors.New("store down")}
	m.SetPlanApprovedLister(lister)
	m.StartApprovedPlanResumeSweep(context.Background(), 10*time.Millisecond)
	t.Cleanup(func() { m.StopApprovedPlanResumeSweep() })

	// The store recovers mid-life.
	waitForSweep(t, func() bool { return lister.callCount() >= 2 })
	lister.setIDs("plan-sweep-err")
	time.Sleep(10 * time.Millisecond)
	m.StopApprovedPlanResumeSweep() // must not hang even mid-retry
}

// ---------------------------------------------------------------------
// Fix 3: SetActivePlan must not clobber concurrently-pending plans.
// ---------------------------------------------------------------------

// TestGoal_SetActivePlan_PreservesPendingPlans: pending plan A (tracked by
// AddActivePlan during tier-2 creation) survives SetActivePlan(B) — the
// pre-fix code replaced ActivePlanIDs with [B], breaking the
// MaxActivePlans cap and the resume path's plan→goal linkage scan.
func TestGoal_SetActivePlan_PreservesPendingPlans(t *testing.T) {
	g := &Goal{ID: "goal-sap"}
	g.AddActivePlan("plan-A-pending")

	g.SetActivePlan("plan-B-approved")

	if got := g.ActivePlanID; got != "plan-B-approved" {
		t.Errorf("ActivePlanID = %q, want plan-B-approved", got)
	}
	ids := g.ActivePlans()
	if len(ids) != 2 || ids[0] != "plan-A-pending" || ids[1] != "plan-B-approved" {
		t.Errorf("ActivePlans = %v, want [plan-A-pending plan-B-approved]; pending plan A was clobbered", ids)
	}
}

// TestGoal_SetActivePlan_DedupsAndEmptyClearsPointerOnly: re-setting the
// current plan must not duplicate it; the empty-set clears the pointer but
// preserves pending entries (AddActivePlan/RemoveActivePlan own the list).
func TestGoal_SetActivePlan_DedupsAndEmptyClearsPointerOnly(t *testing.T) {
	g := &Goal{ID: "goal-sap2"}
	g.AddActivePlan("plan-p1")

	g.SetActivePlan("plan-p1")
	if ids := g.ActivePlans(); len(ids) != 1 {
		t.Errorf("re-set of current plan duplicated the entry: %v", ids)
	}

	g.SetActivePlan("")
	if got := g.ActivePlanID; got != "" {
		t.Errorf("ActivePlanID = %q after empty set, want empty", got)
	}
	if ids := g.ActivePlans(); len(ids) != 1 || ids[0] != "plan-p1" {
		t.Errorf("ActivePlans = %v after empty set, want [plan-p1] (empty set clears the pointer only)", ids)
	}
}

// ---------------------------------------------------------------------
// Fix 4: D4 empty-allowlist widening.
// ---------------------------------------------------------------------

// disjointHintChatter is an llm.Chatter stub returning a hint disjoint
// from any base allowlist.
type disjointHintChatter struct{ resp string }

func (c *disjointHintChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: c.resp}, nil
}

func (c *disjointHintChatter) ChatWithProgress(_ context.Context, _ []llm.ChatMessage, _ llm.ProgressCallback, _ ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(context.Background(), nil)
}
func (c *disjointHintChatter) Config() *llm.ModelConfig { return nil }

// TestApplyToolsAllowedHint_DisjointKeepsRestrictedBase: a hint with zero
// overlap against a restricted base allowlist keeps the RESTRICTED base —
// the pre-fix code assigned the EMPTY intersection, and empty means
// inherit-everything (constitution.go Tool gating doc; enforcement check 1b
// skips an empty allowlist), silently widening the constitution past the
// restricted base. The note names both lists.
func TestApplyToolsAllowedHint_DisjointKeepsRestrictedBase(t *testing.T) {
	mgr := &Manager{constitutions: make(map[string]Constitution), logger: slog.New(slog.DiscardHandler)}
	base := Constitution{Purpose: "restricted"}
	base.Constraints.ToolsAllowed = []string{"file_read", "web_search"}
	hint := []string{"shell", "web_fetch"}

	note := mgr.applyToolsAllowedHint(&base, hint)

	got := base.Constraints.ToolsAllowed
	if len(got) != 2 || got[0] != "file_read" || got[1] != "web_search" {
		t.Fatalf("disjoint hint: ToolsAllowed = %v, want the restricted base [file_read web_search] (empty assignment widens: empty = inherit-everything)", got)
	}
	if !strings.Contains(note, "disjoint") || !strings.Contains(note, "file_read") || !strings.Contains(note, "shell") {
		t.Errorf("note = %q, want it to name the disjointness and both lists", note)
	}
}

// TestApplyToolsAllowedHint_NilBaseInheritsHintInFull: an unrestricted
// (nil) base takes the hint in full — never an empty assignment.
func TestApplyToolsAllowedHint_NilBaseInheritsHintInFull(t *testing.T) {
	mgr := &Manager{constitutions: make(map[string]Constitution), logger: slog.New(slog.DiscardHandler)}
	base := Constitution{Purpose: "open"}

	note := mgr.applyToolsAllowedHint(&base, []string{"web_fetch", "shell"})

	got := base.Constraints.ToolsAllowed
	if len(got) != 2 {
		t.Fatalf("nil-base + hint: ToolsAllowed = %v, want the 2 hinted tools (never empty)", got)
	}
	if !strings.Contains(note, "2 tool(s)") {
		t.Errorf("note = %q, want the suggested-tool count", note)
	}
}

// TestApplyToolsAllowedHint_OverlappingIntersects: the non-disjoint path
// still intersects (D4) — only base members survive.
func TestApplyToolsAllowedHint_OverlappingIntersects(t *testing.T) {
	mgr := &Manager{constitutions: make(map[string]Constitution), logger: slog.New(slog.DiscardHandler)}
	base := Constitution{Purpose: "restricted"}
	base.Constraints.ToolsAllowed = []string{"file_read", "web_search"}

	note := mgr.applyToolsAllowedHint(&base, []string{"file_read", "shell"})

	got := base.Constraints.ToolsAllowed
	if len(got) != 1 || got[0] != "file_read" {
		t.Fatalf("overlapping hint: ToolsAllowed = %v, want [file_read] (hint must not widen the base)", got)
	}
	if !strings.Contains(note, "1 kept") || !strings.Contains(note, "1 dropped") {
		t.Errorf("note = %q, want the kept/dropped counts", note)
	}
}

// TestSynthesizeConstitution_DisjointHintEndToEnd pins the REAL merge path:
// a blank-only hint keeps the nil base untouched, and the merge block never
// produces an empty allowlist from a non-empty base (reached through
// synthesizeConstitutionWithLLM; the disjoint leg itself is pinned by
// TestApplyToolsAllowedHint_DisjointKeepsRestrictedBase because the
// conservative base's allowlist is always nil).
func TestSynthesizeConstitution_DisjointHintEndToEnd(t *testing.T) {
	mgr := &Manager{constitutions: make(map[string]Constitution), logger: slog.New(slog.DiscardHandler)}
	mc := &disjointHintChatter{resp: `{
  "purpose": "disjoint e2e",
  "role": "disjoint e2e",
  "never": [],
  "tools_allowed_hint": ["   ", ""]
}`}
	c, _, err := mgr.synthesizeConstitutionWithLLM(context.Background(), mc, migrateTestBot())
	if err != nil {
		t.Fatalf("synthesize error: %v", err)
	}
	if got := c.Constraints.ToolsAllowed; got != nil {
		t.Errorf("blank-only hint: ToolsAllowed = %v, want nil (base untouched)", got)
	}
}

// Compile-time guard: the stub lister satisfies the interface.
var _ PlanApprovedLister = (*stubApprovedLister)(nil)

// Guard against an unused-import regression on bot (kept for parity with
// the manager_resume_test fixture style).
var _ = bot.BotTrigger{}
