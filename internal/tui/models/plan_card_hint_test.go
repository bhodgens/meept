package models

import (
	"strings"
	"testing"
)

// 09-cli-affordances TEST-GATE (task 3): the plan card's "[5] plans tab"
// hint must render the CONFIGURED plans-view key from the TUI keybinding
// config, not a hardcoded literal 5. Overlaps leaf 08 task 7 — whichever
// leaf lands second verifies-or-applies. RED before renderPlanNotification
// consumes the configured key.

// TestPlanCardHintUsesConfiguredPlansKey pins: the submitting-state plan
// card names the configured ViewPlans key in its hint line.
func TestPlanCardHintUsesConfiguredPlansKey(t *testing.T) {
	m := &ChatModel{plansViewKey: "6"}

	card := m.renderPlanNotification(PlanNotificationMsg{
		State:  "submitting",
		PlanID: "plan-hint-1",
		Title:  "hint plan",
	})
	if !strings.Contains(card, "[6]") {
		t.Fatalf("09-gate: plan card hint shows hardcoded key; want the configured [6]:\n%s", card)
	}
	if strings.Contains(card, "[5] plans tab") {
		t.Fatalf("09-gate: plan card hint still carries the hardcoded [5]:\n%s", card)
	}
	if !strings.Contains(card, "plans tab") {
		t.Fatalf("09-gate: plan card hint lost the plans-tab wording:\n%s", card)
	}
}

// TestPlanCardHintDefaultKey pins the zero-value default: a model with no
// configured key renders the shipped default "5".
func TestPlanCardHintDefaultKey(t *testing.T) {
	m := &ChatModel{}

	card := m.renderPlanNotification(PlanNotificationMsg{
		State:  "submitting",
		PlanID: "plan-hint-2",
		Title:  "hint plan two",
	})
	if !strings.Contains(card, "[5] plans tab") {
		t.Fatalf("09-gate: default plans key must stay 5:\n%s", card)
	}
}

// TestPlanCardConfirmHintUsesConfiguredPlansKey covers the completed
// state card's confirm hint.
func TestPlanCardConfirmHintUsesConfiguredPlansKey(t *testing.T) {
	m := &ChatModel{plansViewKey: "7"}

	card := m.renderPlanNotification(PlanNotificationMsg{
		State:      "completed",
		PlanID:     "plan-hint-3",
		Title:      "hint plan three",
		PhaseCount: 2,
		StepCount:  4,
	})
	if !strings.Contains(card, "[7] plans tab") {
		t.Fatalf("09-gate: completed card hint must use the configured key:\n%s", card)
	}
}
