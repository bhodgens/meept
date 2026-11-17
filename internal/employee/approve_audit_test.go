package employee

// Issue #148 (2): agents.goals.approve audit line. The unix RPC socket is
// owner-trusted by convention (no auth middleware — decision recorded in
// docs/plans/20261113-escalation-resume-and-hardening/master.md), so the
// approve mutation must at least leave a durable record: a structured log
// line plus an AuditFinding row surfaced by `meept agents audit`, mirroring
// the tier-3 escalation finding pattern.

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/bot"
)

// auditRecordingDisposer records the ApprovePlan handoff to the disposer.
type auditRecordingDisposer struct {
	mu       sync.Mutex
	approved []string
}

func (d *auditRecordingDisposer) ApprovePlan(_ context.Context, planID, _, by string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.approved = append(d.approved, planID+"|"+by)
	return nil
}

func (d *auditRecordingDisposer) RejectPlan(_ context.Context, _, _, _, _ string) error {
	return nil
}

// TestApprovePlan_WritesAuditFinding proves the approve mutation persists an
// AuditFinding (violated_rule "plan_approved") naming the plan, the goal,
// the approver, and the operator reason — the durable sibling of the
// structured "plan approval recorded" log line.
func TestApprovePlan_WritesAuditFinding(t *testing.T) {
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
	auditStore, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("audit store: %v", err)
	}
	t.Cleanup(func() { auditStore.Close() })

	m := NewManagerWithStores(bot.NewManager(botStore, nil), botStore, constStore, goalStore, auditStore, slog.New(slog.DiscardHandler))
	m.SetPlanDisposer(&auditRecordingDisposer{})

	ctx := context.Background()
	c := validConstitution()
	const empID = "emp-audit-approve"
	if _, err := m.Hire(ctx, HireRequest{
		ID:           empID,
		Name:         empID,
		Prompt:       "test prompt",
		Enabled:      true,
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Constitution: constitutionToMap(t, c),
	}); err != nil {
		t.Fatalf("hire: %v", err)
	}
	goal, err := m.CreateGoal(ctx, GoalSpec{
		EmployeeID: empID,
		Title:      "keep the audit fixture green",
		Mandate:    "pursue the audit fixture mandate to completion",
		Source:     "user",
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	if err := m.ApprovePlan(ctx, goal.ID, "plan-audit-1", "user", "looks safe, ship it"); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}

	findings, err := auditStore.List(ctx, AuditListFilter{EmployeeID: empID})
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}
	var found *AuditFinding
	for i := range findings {
		if findings[i].ViolatedRule == "plan_approved" {
			found = &findings[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no plan_approved finding written; got %d finding(s): %+v", len(findings), findings)
	}
	if found.Severity != SeverityInfo {
		t.Errorf("severity = %q, want %q", found.Severity, SeverityInfo)
	}
	if found.PlanID != "plan-audit-1" {
		t.Errorf("finding plan_id = %q, want plan-audit-1", found.PlanID)
	}
	if found.GoalID != goal.ID {
		t.Errorf("finding goal_id = %q, want %q", found.GoalID, goal.ID)
	}
	if !strings.Contains(found.Evidence, `"user"`) {
		t.Errorf("finding evidence missing approver identity: %q", found.Evidence)
	}
	// The operator reason must land in the durable record (it used to be
	// silently dropped at this layer).
	if !strings.Contains(found.Evidence, "looks safe, ship it") {
		t.Errorf("finding evidence missing reason: %q", found.Evidence)
	}
}

// TestApprovePlan_SurvivesNilAuditStore pins the best-effort posture: with
// no audit store wired (Manager built without one), the approve still
// succeeds — the audit line is a record, not a gate.
func TestApprovePlan_SurvivesNilAuditStore(t *testing.T) {
	m, _, _ := newResumeTestManager(t) // audit store absent by construction
	m.SetPlanDisposer(&auditRecordingDisposer{})

	goal, err := m.CreateGoal(context.Background(), GoalSpec{
		EmployeeID: resumeEmployeeID,
		Title:      "nil audit store goal",
		Mandate:    "pursue the nil audit store mandate",
		Source:     "user",
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	if err := m.ApprovePlan(context.Background(), goal.ID, "plan-no-audit", "user", "no audit store"); err != nil {
		t.Fatalf("ApprovePlan without an audit store: %v", err)
	}
}
