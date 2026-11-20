package tui

// 06-planning TEST-GATE T4 (F13c): mode badge + evolver document render.
//
// RED today for missing-implementation reasons:
//   - the only mode signal is the async-ack markdown "**mode:**" line
//     (handler.go modeToLabel / FormatEnhancedAsyncTaskAck). The TUI
//     chat renders no mode badge from the C1 terminal fields
//     (intent_type / handler_case): a plan/quickplan/direct/recall turn
//     is indistinguishable in the transcript (W17 delineation).
//   - the plans tab/detail render literal zero counts for evolver
//     document proposals (models/plans.go updatePlansTable phasesStr /
//     stepsStr; renderPlanDetailModal) instead of reusing planCountLine
//     ("document proposal - no executable steps", models/chat.go). The
//     sidebar skips 0-phase plans entirely (sidebar.go refreshData).
//
// Evolver pin: the render must REUSE planCountLine wording (no fork).
// The expected line is derived from the EXISTING chat-card render
// (renderPlanNotification, which already calls planCountLine), so a
// parallel wording implementation in the plans tab fails the pin.
//
// Gate tests: do not weaken. GREEN after 06-planning/03 T2/T3.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// gateModeTerminal builds a C1 turn.terminal payload for a tracked turn.
// intent_type / handler_case are the frozen C1 keys the badge consumes.
func gateModeTerminal(turnID, intentType, handlerCase string) map[string]any {
	return map[string]any{
		"turn_id":      turnID,
		"status":       "completed",
		"reply":        "the work is done",
		"intent_type":  intentType,
		"handler_case": handlerCase,
		"duration_ms":  float64(10),
	}
}

// gateSubmitTurn drives the REAL chat submit path so the turn is tracked
// exactly as in production: seed the recorder's chat.submit ack, type a
// message, press Enter (chat.Update -> doSendMessage -> submitChatAsync),
// then deliver the resulting submitTurnResultMsg back into the loop.
func gateSubmitTurn(t *testing.T, hp *headlessProgram, rec *gateApprovalRecorder, turnID, sessID string) {
	t.Helper()
	rec.setResult("chat.submit", map[string]any{
		"turn_id":         turnID,
		"conversation_id": "conv-" + sessID,
		"session_id":      sessID,
		"accepted":        true,
	})
	hp.app.chat.SetInputValue("do the mode fixture work")
	cmd := hp.app.chat.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("chat Enter produced no submit command (fixture broken)")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			if sub := c(); sub != nil {
				hp.send(sub)
			}
		}
	} else if msg != nil {
		hp.send(msg)
	}
	hp.settle(100, 30)
}

// TestModeBadgeDistinguishesModes pins F13c delineation: after a
// plan/quickplan/direct/recall turn resolves, the chat transcript carries
// a visible mode badge derived from the C1 fields - four distinct labels,
// never the raw handler_case enum. RED today: the terminal handler
// renders only the reply bubble; no badge exists.
func TestModeBadgeDistinguishesModes(t *testing.T) {
	cases := []struct {
		name        string
		intentType  string
		handlerCase string
		wantLabel   string // user-facing label substring (mapped, not raw enum)
	}{
		{"plan turn", "plan", "async_dispatch", "plan"},
		{"quickplan turn", "quickplan", "async_dispatch", "quick"},
		{"direct turn", "code", "direct_mode", "direct"},
		{"recall turn", "recall", "direct_response", "recall"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hp, rec := gateApprovalWorld(t)
			sessID := "sess-mode-" + tc.intentType
			hp.send(SessionLoadedMsg{Session: &types.Session{ID: sessID, Name: "mode", ConversationID: "conv-" + sessID}})
			hp.settle(100, 30)

			turnID := "turn-mode-" + tc.intentType
			gateSubmitTurn(t, hp, rec, turnID, sessID)

			hp.send(EventStreamDataMsg{Events: []BusEvent{{
				Topic:   "turn.terminal",
				Type:    "event",
				Payload: gateModeTerminal(turnID, tc.intentType, tc.handlerCase),
			}}})
			hp.settle(100, 30)

			app := hp.finish(100, 30)
			// Assert on the CHAT TRANSCRIPT only - the sidebar carries a
			// "plan" panel header that would vacuously satisfy a "plan"
			// badge substring against the full app view.
			view := strings.ToLower(app.chat.View())
			if !strings.Contains(view, tc.wantLabel) {
				t.Fatalf("F13c: no %q mode badge after a %s turn (intent_type=%s handler_case=%s); the transcript cannot delineate planning from direct execution:\n%s",
					tc.wantLabel, tc.name, tc.intentType, tc.handlerCase, gateTailLines(app.chat.View(), 20))
			}
			// The raw enum must never leak to the user (mapped labels only).
			if strings.Contains(view, tc.handlerCase) {
				t.Errorf("F13c: raw handler_case enum %q leaked into the transcript (badge must use mapped labels)", tc.handlerCase)
			}
		})
	}
}

// gateEvolverCountLine is the EXACT planCountLine(0,0) wording frozen by
// internal/tui/models/plan_count_line_test.go (TestPlanCountLine, GREEN).
// The plans tab/detail/sidebar must reuse planCountLine and render this
// same line - no wording fork. Hardcoding the frozen string here (rather
// than calling the unexported renderer across packages) keeps the gate
// compile-safe while still failing any parallel wording implementation.
const gateEvolverCountLine = "document proposal \u00b7 no executable steps"

// TestEvolverDocRenderPlansTab pins the evolver document-proposal render
// in the plans TAB: a 0-phase/0-step plan (evolver skill proposal) must
// render the planCountLine wording ("document proposal"), not literal
// zeros. RED today: updatePlansTable writes fmt.Sprintf("%d", 0) columns.
func TestEvolverDocRenderPlansTab(t *testing.T) {
	want := gateEvolverCountLine
	hp, rec := gateApprovalWorld(t)
	rec.setResult("plan.list_by_session", map[string]any{
		"plans": []any{map[string]any{
			"id":              "plan-evolver-tab",
			"title":           "Skill evolution: improve foo",
			"state":           "draft",
			"file_path":       "/home/u/.meept/plans/evolver-foo.md",
			"total_steps":     0,
			"completed_steps": 0,
			"phases":          []any{},
		}},
	})
	hp.send(SessionLoadedMsg{Session: &types.Session{ID: "sess-evolver-tab", Name: "evolver", ConversationID: "conv-evolver-tab"}})
	hp.settle(100, 30)

	switchView(t, hp, "p")
	settleAsync()
	app := hp.finish(100, 30)
	view := app.plans.View()

	if !strings.Contains(view, want) {
		t.Fatalf("F13c: plans tab does not render the evolver document-proposal line %q (literal zero counts today; must reuse planCountLine):\n%s", want, gateTailLines(view, 25))
	}
}

// TestEvolverDocRenderPlansDetail pins the evolver render in the plans
// DETAIL modal: opening a 0-phase plan shows the document-proposal line
// and the file path (open-file affordance), not a bare zero-phase card.
// RED today: renderPlanDetailModal skips the phases section entirely for
// 0-phase plans and never mentions the document nature or file path.
func TestEvolverDocRenderPlansDetail(t *testing.T) {
	want := gateEvolverCountLine
	hp, rec := gateApprovalWorld(t)
	rec.setResult("plan.list_by_session", map[string]any{
		"plans": []any{map[string]any{
			"id":              "plan-evolver-detail",
			"title":           "Skill evolution: improve bar",
			"state":           "draft",
			"file_path":       "/home/u/.meept/plans/evolver-bar.md",
			"total_steps":     0,
			"completed_steps": 0,
			"phases":          []any{},
		}},
	})
	hp.send(SessionLoadedMsg{Session: &types.Session{ID: "sess-evolver-detail", Name: "evolver", ConversationID: "conv-evolver-detail"}})
	hp.settle(100, 30)

	switchView(t, hp, "p")
	settleAsync()
	// Open the detail modal on the first row (Enter).
	hp.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	hp.settle(100, 30)
	app := hp.finish(100, 30)
	view := app.plans.View()

	if !strings.Contains(view, want) {
		t.Fatalf("F13c: plans detail modal does not render the evolver document-proposal line %q (must reuse planCountLine):\n%s", want, gateTailLines(view, 30))
	}
	if !strings.Contains(view, "evolver-bar.md") {
		t.Fatalf("F13c: plans detail modal does not show the document file path (open-file affordance):\n%s", gateTailLines(view, 30))
	}
}

// TestEvolverDocRenderSidebar pins the sidebar half: a 0-phase evolver
// plan must not be silently skipped - the sidebar plan panel surfaces it
// as a document proposal. RED today: refreshData skips plans with
// len(Phases)==0 (sidebar.go), so the panel shows nothing.
func TestEvolverDocRenderSidebar(t *testing.T) {
	hp, rec := gateApprovalWorld(t)
	rec.setResult("plan.list", map[string]any{
		"plans": []any{map[string]any{
			"id":        "plan-evolver-sidebar",
			"title":     "Skill evolution: improve baz",
			"state":     "draft",
			"file_path": "/home/u/.meept/plans/evolver-baz.md",
			"phases":    []any{},
		}},
	})
	hp.settle(100, 30)
	// Drive a sidebar refresh through the real tick path.
	hp.send(SidebarRefreshTick{})
	settleAsync()
	hp.settle(100, 30)
	app := hp.finish(100, 30)
	view := app.sidebar.View()

	if !strings.Contains(view, "evolver") && !strings.Contains(strings.ToLower(view), "document proposal") {
		t.Fatalf("F13c: sidebar skips the 0-phase evolver document proposal (refreshData drops len(Phases)==0 plans); it must surface the document:\n%s", gateTailLines(view, 30))
	}
}
