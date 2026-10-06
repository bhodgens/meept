//go:build e2e

// Package tuiflows drives the REAL TUI App model (internal/tui) against
// the hermetic harness daemon — the five top operator flows from
// docs/workflows/tui-e2e-plan.md Phase 2 / plan leaf 02:
//
//	tui-palette-01        open palette (ctrl+x), execute a command, assert
//	                      the RPC-driven effect (session list populates the
//	                      picker) and the view change
//	tui-session-switch-01 two sessions exist; switch via the picker keys;
//	                      assert the switch effect + transcript reload
//	tui-agent-tab-01      agents view renders at a fixed size (the
//	                      zero-width-viewport regression class, in situ)
//	tui-quota-01          quota surfaces as the wait indicator, not an error
//	tui-streaming-01      scripted SSE streaming turn finalizes in chat
//
// Mechanics: the App runs under a headless bubbletea v2 Program (all
// input via program.Send — ordered delivery, no TTY), pointed at
// harness.Stack.SocketPath via the REAL entry-point constructor
// tui.NewApp. Daemon teardown order matters: the TUI program quit is
// registered in t.Cleanup BEFORE the daemon stop (also t.Cleanup; LIFO
// runs the TUI quit first). No wall-clock assertions.
//
// Coverage map (manifest scenarios):
//
//	tui-palette-01        TestPaletteFlowOpensExecutesAndIssuesRPC
//	tui-session-switch-01 TestSessionSwitchReloadsTranscript
//	tui-agent-tab-01      TestAgentTabRendersTableRows
//	tui-quota-01          TestQuotaWaitSurfacesAsWaitIndicator
//	tui-streaming-01      TestStreamingTurnAccumulatesAndFinalizes
package tuiflows

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/tui"
)

// settleAsync gives async cmd goroutines (fetch results, acks) time to
// deliver through the program loop before the app is finished and
// asserted. Harness round-trips answer in milliseconds; the delay is
// generous and every assertion runs on the FINISHED model (loop stopped
// → race-free reads).
func settleAsync() { time.Sleep(250 * time.Millisecond) }

// TestPaletteFlowOpensExecutesAndIssuesRPC covers tui-palette-01: ctrl+x
// opens the palette; the sessions action key executes the command and the
// sessions-view fetch (the RPC the command issues) populates the picker
// with the seeded sessions — the effect is observable only if the RPC
// round-tripped.
func TestPaletteFlowOpensExecutesAndIssuesRPC(t *testing.T) {
	stack := harness.Start(t)
	_ = stack.CreateSession(t, "palette-alpha", stack.MeeptHome)

	hp := startTUIAgainst(t, stack)
	hp.sendKey("ctrl+x")
	hp.settle()
	if got := hp.app.ActiveModal(); got != tui.ModalCommandPalette {
		t.Fatalf("palette did not open on ctrl+x: activeModal=%v", got)
	}

	hp.sendKey("s") // palette action: switch to sessions view
	settleAsync()
	app := hp.finish()

	if app.ActiveView() != tui.ViewSessions {
		t.Errorf("currentView = %v after palette action, want sessions view", app.ActiveView())
	}
	if app.ActiveModal() != tui.ModalNone {
		t.Errorf("palette still open after action: activeModal=%v", app.ActiveModal())
	}
	// The command's RPC effect: the sessions view fetched and rendered
	// the daemon-side sessions (the seeded description comes from the
	// session.list round-trip the palette action triggered).
	view := app.View().Content
	if !strings.Contains(view, "palette-alpha") {
		t.Errorf("sessions view does not show the seeded session after the palette "+
			"command executed (session.list RPC effect missing):\n%s", view)
	}
}

// TestSessionSwitchReloadsTranscript covers tui-session-switch-01: two
// sessions exist; switching via the picker keys fires the switch effect
// and the chat view re-targets the picked session (transcript reload).
func TestSessionSwitchReloadsTranscript(t *testing.T) {
	stack := harness.Start(t)
	idA := stack.CreateSession(t, "switch-alpha", stack.MeeptHome)

	// Boot the TUI pinned to session A; the switch must move it to B.
	// The pre-switch wait matters for ORDERING: the startup loadSession's
	// SessionLoadedMsg → chat.SetSession(A) runs on an async cmd — a
	// switch issued before it lands would be clobbered back to A.
	hp := startTUIAgainst(t, stack, WithTargetSession(idA))
	time.Sleep(800 * time.Millisecond)
	hp.settle()
	if got := hp.app.ActiveSessionID(); got != idA {
		t.Fatalf("target session not loaded at start: got %q want %q", got, idA)
	}

	// Create B now and bump its activity with a real chat turn. The
	// sessions view sorts by last activity (newest first): B's bump must
	// land AFTER the boot attach touched A (so B is cursor row 0, the row
	// Enter switches to) but BEFORE the picker's session.list fetch. The
	// fetch is an async RPC whose landing time the TUI-side cannot observe
	// safely mid-loop (reads race the event loop), so instead of a
	// condition poll we give it a bounded settle ladder — and the steer
	// flow's topic subscriptions (4f92b6ca) increased bus volume enough
	// that the old single 250ms settle stopped covering it.
	idB := stack.CreateSession(t, "switch-beta", stack.MeeptHome)
	stack.Fake.SetPostToolText("activity bump turn")
	stack.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"tui-flows bump pin"}`)
	stack.ChatTurn(t, idB, "bump activity", 60*time.Second)

	// Sessions tab via palette, then Enter = the picker's documented
	// switch flow (enter: switch) on row 0 = B.
	hp.sendKey("ctrl+x")
	hp.settle()
	hp.sendKey("s")
	// The sessions view fetches session.list via an async RPC; a single
	// 250ms settle stopped covering that round-trip once the TUI
	// subscribed to the 3/4-segment agent topics (extra bus volume).
	// Wait boundedly: ~2s of settle cycles before enter. No safe mid-loop
	// read exists (reads race the event loop), so this is a bounded
	// settle ladder, not a condition poll.
	for range 8 {
		settleAsync()
	}
	hp.sendKey("enter")
	settleAsync()

	app := hp.finish()
	gotSession := app.ActiveSessionID()
	if gotSession != idB {
		t.Errorf("active session after picker switch = %q, want %q (row 0 = newest)", gotSession, idB)
	}
	if got := app.ChatSessionID(); got != idB {
		t.Errorf("chat model session after switch = %q, want %q (transcript reload)", got, idB)
	}
}

// employeeDefinition is the agents.create payload for the panel fixture.
// It satisfies every validation the hire path enforces — a trigger
// (BotDefinition.Validate), a constitution whose amendment_policy
// requires_approval is true (a design invariant), and a well-formed
// constraints block — so agents.list returns a row the panel can render.
func employeeDefinition(id, marker string) map[string]any {
	return map[string]any{
		"id":      id,
		"name":    "Quartz Watcher",
		"prompt":  "You are the Quartz Watcher for the e2e agents panel.",
		"tools":   []string{"web_fetch"},
		"enabled": true,
		"triggers": []map[string]any{
			{"type": "cron", "schedule": "0 4 * * *", "enabled": true},
		},
		"constitution": map[string]any{
			"purpose":       "watch the " + marker + " fixtures",
			"role":          "Quartz Watcher",
			"charter":       "Watch. Never delete.",
			"autonomy_tier": "tier_2_propose",
			"escalates_to":  []string{"user"},
			"never":         []string{"merge to main"},
			"constraints": map[string]any{
				"tools_allowed":      []string{"web_fetch"},
				"tools_forbidden":    []string{"shell_execute"},
				"risk_ceiling":       "medium",
				"daily_budget_cents": 50,
			},
			"amendment_policy": map[string]any{
				"self_propose_allowed": false,
				"requires_approval":    true,
				"frozen_fields":        []string{"constraints.never", "constraints.risk_ceiling"},
			},
			"version":     1,
			"authored_by": "user",
		},
	}
}

// seedEmployee hires one employee through the REAL agents.create RPC and
// proves agents.list returns it — the exact seam the panel's
// fetchAgents/agentsListMsg path consumes (internal/tui/agents_panel.go:308).
// Without a store-backed row the panel can only ever render its empty
// header, and "renders something" stops proving anything about the table.
func seedEmployee(t *testing.T, stack *harness.Stack, id, marker string) {
	t.Helper()
	created := harness.DialRPC(t, stack.SocketPath).CallResult("agents.create",
		employeeDefinition(id, marker))
	if got, _ := created["id"].(string); got != id {
		t.Fatalf("agents.create returned id %q, want %q: %v", got, id, created)
	}
	list := harness.DialRPC(t, stack.SocketPath).CallResult("agents.list", nil)
	agents, _ := list["agents"].([]any)
	found := false
	for _, a := range agents {
		m, ok := a.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := m["id"].(string); got == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("agents.list did not return the created employee %q: %v", id, list)
	}
}

// openAgentsView boots the TUI, switches to the agents view through the
// palette key flow, and returns the finished model. The palette switch is
// the real operator path (ctrl+x → 'e') and it also runs the view's
// Init → agents.list fetch, so the panel is populated before View().
func openAgentsView(t *testing.T, stack *harness.Stack, opts ...tuiOption) *tui.App {
	t.Helper()
	hp := startTUIAgainst(t, stack, opts...)
	settleAsync()
	hp.sendKey("ctrl+x")
	hp.settle()
	hp.sendKey("e") // palette action: agents view
	// Two settles: the view switch runs initCurrentView → AgentsPanel.Init
	// (agents.list round-trip) and then the list response lands as a
	// separate message; one settle can race the response.
	settleAsync()
	settleAsync()
	app := hp.finish()
	if app.ActiveView() != tui.ViewAgents {
		t.Fatalf("currentView = %v, want agents view", app.ActiveView())
	}
	return app
}

// TestAgentTabRendersTableRows covers tui-agent-tab-01: the agents view
// renders POPULATED — its own header, its column titles, the store-backed
// employee row, and its help line — and does so at a fixed size AND after
// a resize round-trip back out of a compact viewport.
//
// Why each assertion can fail (the pre-fix version asserted only
// `!strings.Contains(view, "agents unavailable")` and
// `strings.TrimSpace(view) != ""`, both structurally unreachable: the
// first string is only emitted when a.agents == nil and NewApp always
// constructs the panel, and View() always appends the status bar, so
// deleting AgentsPanel.View() outright still passed):
//
//   - the header ("agents" + the list/approvals/audit tab strip) and
//     the column titles come ONLY from AgentsPanel.View → renderHeader /
//     the table — nothing else in the app emits them;
//   - the employee row can only come from the agents.list RPC, so it is
//     a cross-boundary assertion, not a rendering echo;
//   - the absence check pins that these markers are agents-view-specific
//     rather than incidental to any view;
//   - the compact leg is the zero-width-viewport regression class itself:
//     SetSize sizes the table on BOTH axes and repopulates from cache, so
//     a regression that drops the repopulation renders the header with an
//     empty body.
func TestAgentTabRendersTableRows(t *testing.T) {
	const marker = "quartzwatcher"
	stack := harness.Start(t)
	_ = stack.CreateSession(t, "agents-session", stack.MeeptHome)
	seedEmployee(t, stack, marker, marker)

	// Leg 1: the default fixed size (100x30).
	app := openAgentsView(t, stack)
	view := app.View().Content

	// The panel's own identifying content — all three render only from
	// AgentsPanel.View().
	for _, want := range []string{
		"agents",                      // renderHeader title
		"list",                        // active sub-view tab
		"approvals",                   // sub-view tabs
		"audit",                       // sub-view tabs
		"id",                          // column title
		"drift",                       // column title
		"findings",                    // column title
		"last run",                    // column title
		"r: refresh | enter: details", // renderHelpHint
		marker,                        // the store-backed row
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("agents view missing %q — the panel did not render its own "+
				"content (zero-width-viewport regression class):\n%s", want, view)
		}
	}
	// The count badge proves the ROW COUNT reached the model, not just a
	// hardcoded header: renderHeader prints "(N agents)".
	if !strings.Contains(view, "(1 agents)") {
		t.Fatalf("agents view header count is not \"(1 agents)\" — the "+
			"agents.list fetch did not populate the panel:\n%s", view)
	}

	// Absence: the same view with NO employee cannot carry the row, so
	// the marker above came from the fetch and not from the shell.
	emptyStack := harness.Start(t)
	_ = emptyStack.CreateSession(t, "agents-empty", emptyStack.MeeptHome)
	emptyApp := openAgentsView(t, emptyStack)
	emptyView := emptyApp.View().Content
	if strings.Contains(emptyView, marker) {
		t.Fatalf("agents view shows the employee row %q without any employee "+
			"hired — the assertion above is not load-bearing:\n%s", marker, emptyView)
	}
	if !strings.Contains(emptyView, "(0 agents)") {
		t.Fatalf("empty agents view header count is not \"(0 agents)\":\n%s", emptyView)
	}

	// Absence in a DIFFERENT view: the sessions view renders neither the
	// agents sub-view tabs nor the employees panel columns. The help lines
	// are NOT usable discriminators here — the sessions view ends with
	// "r: refresh | enter: details" too — so the markers are the panel's
	// own sub-view tabs and column titles.
	sessionsApp := openViewWithKey(t, stack, "s") // palette action: sessions
	sessionsView := sessionsApp.View().Content
	for _, foreign := range []string{
		"1: list | 2: approvals | 3: audit", // agents sub-view help line
		"last run",                          // agents column title
		"findings",                          // agents column title
	} {
		if strings.Contains(sessionsView, foreign) {
			t.Fatalf("the sessions view renders the agents-panel marker %q — the "+
				"agents markers are not agents-view-specific:\n%s", foreign, sessionsView)
		}
	}

	// Leg 2: a COMPACT viewport (width < 80 → LayoutCompact, sidebar
	// hidden) still renders the header and the row — the panel sizes its
	// table on both axes and must not fall off a size cliff. At 40
	// columns the id column truncates the marker to "quartzwatch…"
	// (truncate(str, 18) + the table's own ellipsis), so the compact leg
	// asserts the TRUNCATED form: the row is present, the marker is
	// simply narrower.
	compact := openAgentsView(t, stack, WithSize(40, 20)).View().Content
	compactMarker := marker
	if len(compactMarker) > 11 {
		compactMarker = compactMarker[:11] + "…"
	}
	if !strings.Contains(compact, compactMarker) {
		t.Fatalf("agents view at 40x20 dropped the employee row %q (or its truncated form %q) "+
			"(degenerate-resize regression class):\n%s", marker, compactMarker, compact)
	}
	if !strings.Contains(compact, "agents") || !strings.Contains(compact, "(1 agents)") {
		t.Fatalf("agents view at 40x20 lost its header/count:\n%s", compact)
	}

	// Leg 3: the resize ROUND TRIP — boot compact, then grow back to the
	// fixed size and re-render. SetSize repopulates the table from the
	// cached agents; a regression that only clears rows would show an
	// empty body here while the header still renders.
	hp := startTUIAgainst(t, stack, WithSize(40, 20))
	settleAsync()
	hp.sendKey("ctrl+x")
	hp.settle()
	hp.sendKey("e") // palette action: agents view
	settleAsync()
	settleAsync()
	hp.program.Send(tea.WindowSizeMsg{Width: 100, Height: 30}) // grow back
	settleAsync()
	roundTrip := hp.finish().View().Content
	// The re-grown render is 100 columns wide, so the full marker is
	// visible again — but assert BOTH forms so the leg stays honest about
	// what it proves (the row survived the round-trip, not that the
	// truncation is a specific width).
	if !strings.Contains(roundTrip, marker) && !strings.Contains(roundTrip, marker[:11]+"…") {
		t.Fatalf("agents view lost the employee row %q after a compact→wide "+
			"resize round-trip:\n%s", marker, roundTrip)
	}
	if !strings.Contains(roundTrip, "(1 agents)") {
		t.Fatalf("agents view header count lost after the resize round-trip:\n%s",
			roundTrip)
	}
}

// openViewWithKey boots the TUI, runs one palette action key, and returns
// the finished model. Used for the absence leg: another view, same driver.
func openViewWithKey(t *testing.T, stack *harness.Stack, key string) *tui.App {
	t.Helper()
	hp := startTUIAgainst(t, stack)
	settleAsync()
	hp.sendKey("ctrl+x")
	hp.settle()
	hp.sendKey(key)
	settleAsync()
	return hp.finish()
}

// TestQuotaWaitSurfacesAsWaitIndicator covers tui-quota-01: a parked
// quota turn surfaces the WAIT indicator, never an error bubble. The
// rendering path is the pure QuotaWaitLabel/RenderAgentStatus functions
// the agents tab uses (the live park/resume flow is covered end-to-end
// by e2e/suites/quota-park); here we assert the exact operator-visible
// strings the TUI renders for a quota_wait event payload.
func TestQuotaWaitSurfacesAsWaitIndicator(t *testing.T) {
	unblock := time.Now().Add(90 * time.Minute)
	if got := tui.QuotaWaitLabel("quota", "quota_wait", unblock); !strings.HasPrefix(got, "quota_wait · reset ") {
		t.Errorf("QuotaWaitLabel = %q, want quota_wait · reset HH:MM prefix", got)
	}
	if got := tui.RenderAgentStatus("quota_wait"); got != "quota wait" {
		t.Errorf("RenderAgentStatus(quota_wait) = %q, want %q", got, "quota wait")
	}
	if got := tui.RenderAgentStatus("blocked"); got != "blocked · action required" {
		t.Errorf("RenderAgentStatus(blocked) = %q, want the action-required label", got)
	}
}

// TestStreamingTurnAccumulatesAndFinalizes covers tui-streaming-01: a
// scripted streaming completion round-trips the daemon; the TUI then
// loads that session and the chat view renders the finalized reply from
// the persisted transcript.
func TestStreamingTurnAccumulatesAndFinalizes(t *testing.T) {
	stack := harness.Start(t)
	sessionID := stack.CreateSession(t, "stream-session", stack.MeeptHome)

	// Pin the classifier to the plain chat lane (the naive-user-chat
	// pin pattern) so the turn takes the main loop, and script the
	// completion text: the loop always carries tools, so the fake serves
	// executor-shaped turns whose FINAL reply is postToolText — streamed
	// as SSE when the client asks for stream:true.
	stack.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"tui-flows streaming pin"}`)
	stack.Fake.SetPostToolText("streamed reply ends here")
	reply := stack.ChatTurn(t, sessionID, "say the streaming phrase", 60*time.Second)
	if !strings.Contains(reply, "streamed reply") {
		t.Fatalf("chat turn reply %q does not contain the scripted streaming text", reply)
	}

	// The TUI loads the same session through the real transcript RPC and
	// renders the reply.
	hp := startTUIAgainst(t, stack, WithTargetSession(sessionID))
	settleAsync()

	app := hp.finish()
	if got := app.ActiveSessionID(); got != sessionID {
		t.Fatalf("target session not loaded: got %q want %q", got, sessionID)
	}
	view := app.View().Content
	if !strings.Contains(view, "streamed reply") {
		t.Errorf("chat view does not show the finalized streaming reply:\n%s", view)
	}
}

// TestDeterministicRepeat is the leaf self-check: a full flow driven
// twice produces the same final model state (no leakage via MEEPT_HOME —
// harness.Stack sandboxes it per Start call).
func TestDeterministicRepeat(t *testing.T) {
	run := func() string {
		stack := harness.Start(t)
		id := stack.CreateSession(t, "repeat", stack.MeeptHome)
		hp := startTUIAgainst(t, stack, WithTargetSession(id))
		settleAsync()
		app := hp.finish()
		return app.View().Content
	}
	first := run()
	second := run()
	if first != second {
		t.Errorf("repeat run diverged (state leakage):\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}
