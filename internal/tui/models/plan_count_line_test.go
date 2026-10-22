package models

import (
	"strings"
	"testing"
)

// TestPlanCountLine pins the plan-card count row: a plan with no phases
// and no steps is a document proposal (evolver skill plans) and must NOT
// render the misleading literal "0 phases · 0 steps" (live complaint
// 2026-10-22: users read it as "nothing is happening").
func TestPlanCountLine(t *testing.T) {
	if got := planCountLine(0, 0); strings.Contains(got, "0 phase") || strings.Contains(got, "0 step") {
		t.Fatalf("planCountLine(0,0) = %q, must not render literal zero counts", got)
	}
	if got := planCountLine(0, 0); got != "document proposal · no executable steps" {
		t.Fatalf("planCountLine(0,0) = %q, want the document-proposal line", got)
	}
	if got := planCountLine(2, 5); got != "2 phases · 5 steps" {
		t.Fatalf("planCountLine(2,5) = %q, want %q", got, "2 phases · 5 steps")
	}
	if got := planCountLine(1, 1); got != "1 phase · 1 step" {
		t.Fatalf("planCountLine(1,1) = %q, want %q", got, "1 phase · 1 step")
	}
	if got := planCountLine(0, 3); got != "0 phases · 3 steps" {
		t.Fatalf("planCountLine(0,3) = %q, want literal counts when steps exist", got)
	}
}

// TestRenderPlanNotificationSubmittingNoZeroCounts pins the full card render.
func TestRenderPlanNotificationSubmittingNoZeroCounts(t *testing.T) {
	m := &ChatModel{}
	card := m.renderPlanNotification(PlanNotificationMsg{
		State:      "submitting",
		Title:      "Skill evolution: improve foo",
		PlanID:     "plan-6205135a",
		PhaseCount: 0,
		StepCount:  0,
	})
	if strings.Contains(card, "0 phases") || strings.Contains(card, "0 steps") {
		t.Fatalf("submitting card renders literal zero counts:\n%s", card)
	}
	if !strings.Contains(card, "document proposal") {
		t.Fatalf("submitting card missing the document-proposal line:\n%s", card)
	}
	if !strings.Contains(card, "plan ready for review") {
		t.Fatalf("submitting card missing header:\n%s", card)
	}
}
