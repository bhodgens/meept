package models

import (
	"strings"
	"testing"
)

// TestRenderPlanNotificationFailed pins the plan.failed chat card (leaf 03):
// a plan that fails mid-execution must render a failure card with the
// reason, mirroring the rejected card shape.
func TestRenderPlanNotificationFailed(t *testing.T) {
	m := &ChatModel{}
	card := m.renderPlanNotification(PlanNotificationMsg{
		State:  "failed",
		Title:  "refactor the ingest worker",
		PlanID: "plan-abc123",
		Reason: "phase 2 compile error",
	})
	if !strings.Contains(card, "plan failed") {
		t.Fatalf("failed card missing 'plan failed' header:\n%s", card)
	}
	if !strings.Contains(card, "phase 2 compile error") {
		t.Fatalf("failed card missing reason:\n%s", card)
	}
	if !strings.Contains(card, "refactor the ingest worker") {
		t.Fatalf("failed card missing plan title:\n%s", card)
	}

	// No reason given: render a placeholder, never an empty line.
	empty := m.renderPlanNotification(PlanNotificationMsg{
		State: "failed",
		Title: "t",
	})
	if !strings.Contains(empty, "no reason given") {
		t.Fatalf("failed card without reason missing placeholder:\n%s", empty)
	}
}

// TestRenderPlanNotificationFailedDocProposal pins that a failed evolver
// document proposal (zero counts) still says what it IS, never "0 phases".
func TestRenderPlanNotificationFailedDocProposal(t *testing.T) {
	m := &ChatModel{}
	card := m.renderPlanNotification(PlanNotificationMsg{
		State:      "failed",
		Title:      "Skill evolution: improve foo",
		PlanID:     "plan-6205135a",
		PhaseCount: 0,
		StepCount:  0,
	})
	if strings.Contains(card, "0 phases") || strings.Contains(card, "0 steps") {
		t.Fatalf("failed card renders literal zero counts:\n%s", card)
	}
	if !strings.Contains(card, "document proposal") {
		t.Fatalf("failed card missing the document-proposal line:\n%s", card)
	}
}
