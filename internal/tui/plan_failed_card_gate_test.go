package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tui/types"
)

// Leaf 03 gate: a plan.failed bus event must render a chat-visible
// failure card in the SAME session that created the plan. The event
// arrives through the exact seam the real bus.poll loop produces
// (EventStreamDataMsg), mirroring the approval-card gate seam
// (approval_card_test.go).

// gatePlanFailedEvent builds a plan.failed bus event in the
// transformBusEventToWS payload shape (plan_id, session_id, title,
// phase_count, step_count, reason).
func gatePlanFailedEvent(planID, sessionID, title, reason string) BusEvent {
	return BusEvent{
		Topic: "plan.failed",
		Type:  "event",
		Payload: map[string]any{
			"plan_id":     planID,
			"session_id":  sessionID,
			"title":       title,
			"phase_count": float64(3),
			"step_count":  float64(9),
			"reason":      reason,
		},
	}
}

// TestPlanFailedCardRendersInChat pins leaf 03: a plan.failed event for
// the ACTIVE session renders a failure card in the chat transcript
// carrying the plan title and the failure reason. RED today: the App
// event switch's plan.* case set has no plan.failed - the event falls
// out of the switch and the transcript never shows a card.
func TestPlanFailedCardRendersInChat(t *testing.T) {
	hp, _ := gateApprovalWorld(t)

	const sessID = "sess-plan-failed"
	hp.send(SessionLoadedMsg{Session: &types.Session{ID: sessID, Name: "plan failed", ConversationID: "conv-plan-failed"}})
	hp.settle(100, 30)

	hp.send(EventStreamDataMsg{Events: []BusEvent{
		gatePlanFailedEvent("plan-failed-01", sessID, "refactor the ingest worker", "phase 2 compile error"),
	}})
	hp.settle(100, 30)

	app := hp.finish(100, 30)
	view := app.View().Content
	if !strings.Contains(view, "refactor the ingest worker") {
		t.Fatalf("leaf 03: no plan.failed card in the owning session's chat (plan title absent); the App plan.* case set has no plan.failed.\ntranscript tail:\n%s", gateTailLines(view, 25))
	}
	if !strings.Contains(view, "phase 2 compile error") {
		t.Fatalf("leaf 03: plan.failed card missing the failure reason.\ntranscript tail:\n%s", gateTailLines(view, 25))
	}
	if !strings.Contains(strings.ToLower(view), "failed") {
		t.Fatalf("leaf 03: plan.failed card missing a failure affordance/header.\ntranscript tail:\n%s", gateTailLines(view, 25))
	}
}

// TestPlanFailedCardSessionScoped pins the leaf 03 review checklist: the
// failure card renders ONLY in the owning session's chat - a plan.failed
// for another session must not flood the active transcript (an
// explicitly empty session_id also drops, matching the existing plan.*
// scoping rule). The positive control proves the negative is not vacuous.
func TestPlanFailedCardSessionScoped(t *testing.T) {
	hp, _ := gateApprovalWorld(t)

	hp.send(SessionLoadedMsg{Session: &types.Session{ID: "sess-mine", Name: "mine", ConversationID: "conv-mine"}})
	hp.settle(100, 30)

	// Foreign-session event must NOT render into the active chat.
	hp.send(EventStreamDataMsg{Events: []BusEvent{
		gatePlanFailedEvent("plan-other-session", "sess-other", "foreign plan", fmt.Sprintf("foreign reason %d", 1)),
	}})
	hp.settle(100, 30)
	// Empty session_id (evolver sink) must NOT render either.
	hp.send(EventStreamDataMsg{Events: []BusEvent{
		gatePlanFailedEvent("plan-evolver-sink", "", "evolver plan", "sink reason"),
	}})
	hp.settle(100, 30)

	app := hp.finish(100, 30)
	view := app.View().Content
	if strings.Contains(view, "foreign plan") || strings.Contains(view, "plan-evolver-sink") {
		t.Fatalf("leaf 03: cross-session or evolver-sink plan.failed card flooded the active chat (card must be scoped to the owning session):\n%s", gateTailLines(view, 25))
	}

	// Positive control on a fresh world: the owning session's event DOES render.
	hp2, _ := gateApprovalWorld(t)
	hp2.send(SessionLoadedMsg{Session: &types.Session{ID: "sess-mine", Name: "mine", ConversationID: "conv-mine"}})
	hp2.settle(100, 30)
	hp2.send(EventStreamDataMsg{Events: []BusEvent{
		gatePlanFailedEvent("plan-mine-session", "sess-mine", "my plan", "my reason"),
	}})
	hp2.settle(100, 30)
	app2 := hp2.finish(100, 30)
	if !strings.Contains(app2.View().Content, "my reason") {
		t.Fatalf("leaf 03: owning-session plan.failed card never rendered:\n%s", gateTailLines(app2.View().Content, 25))
	}
}
