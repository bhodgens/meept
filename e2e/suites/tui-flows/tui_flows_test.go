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
	"fmt"
	"regexp"
	"strconv"
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

// activityStamps reads the last_activity strings of two sessions from the
// daemon's session.list payload — the exact sort key the sessions picker
// consumes. RFC3339 (as emitted by the store) compares chronologically as
// plain strings.
func activityStamps(t testing.TB, rpc *harness.RPCClient, idA, idB string) (string, string) {
	t.Helper()
	res := rpc.CallResult("session.list", nil)
	list, _ := res["sessions"].([]any)
	stampA, stampB := "", ""
	for _, s := range list {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		la, _ := m["last_activity"].(string)
		switch id {
		case idA:
			stampA = la
		case idB:
			stampB = la
		}
	}
	if stampA == "" || stampB == "" {
		t.Fatalf("session.list missing a seeded session (A=%q B=%q): %v", idA, idB, res)
	}
	return stampA, stampB
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
	// sessions view sorts by last activity (newest first), so the picker
	// must land with B as row 0 — the row Enter switches to.
	idB := stack.CreateSession(t, "switch-beta", stack.MeeptHome)
	stack.Fake.SetPostToolText("activity bump turn")
	stack.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"tui-flows bump pin"}`)
	stack.ChatTurn(t, idB, "bump activity", 60*time.Second)

	// Determinism guard on the ORDERING, not on timing: the daemon stores
	// last_activity at RFC3339 SECOND precision (store_sqlite.go
	// updateSession/UpdateActivity) and the sessions model sorts those
	// strings (models/sessions.go sortSessions). When A's stamp and B's
	// bump truncate to the same wall-clock second — routine on fast
	// runners, where the boot wait leaves A stamped ~0.9s before B — the
	// sort ties and SQLite's ORDER BY falls back to creation order: row 0
	// is A, Enter "switches" to A (a visible no-op), and no number of
	// Enter retries can converge (CI run 45523252285's signature: active
	// stayed on A through every retry; locally reproduced 3/3 by forcing
	// a tie via session.attach). Re-touch B through the real session.attach
	// write path (a distinct client_id bumps last_activity to now) until
	// its stamp is STRICTLY later than A's, read back from the very
	// session.list payload the picker will fetch. Nothing re-touches A
	// afterwards (verified: boot attach path, sidebar/changes polls are
	// read-only), so strict order then holds for the whole picker flow.
	probe := harness.DialRPC(t, stack.SocketPath)
	if stampA, stampB := activityStamps(t, probe, idA, idB); stampB <= stampA {
		resolved := false
		for attempt := 0; attempt < 5 && !resolved; attempt++ {
			// Cross a second boundary BEFORE the next write so the new
			// stamp cannot truncate back onto A's second.
			time.Sleep(1100 * time.Millisecond)
			probe.CallResult("session.attach", map[string]any{
				"session_id": idB, "client_id": fmt.Sprintf("e2e-order-guard-%d", attempt),
			})
			stampA, stampB = activityStamps(t, probe, idA, idB)
			resolved = stampB > stampA
		}
		if !resolved {
			t.Fatalf("could not force B's last_activity strictly after A's: A=%q B=%q", stampA, stampB)
		}
	}

	// Sessions tab via palette, then Enter = the picker's documented
	// switch flow (enter: switch) on row 0 = B.
	hp.sendKey("ctrl+x")
	hp.settle()
	hp.sendKey("s")
	// The sessions view fetches session.list via an async RPC, and Enter
	// on a not-yet-populated table is a SILENT no-op (selectedSession()
	// returns nil — internal/tui/models/sessions.go). The old fixed
	// 8x250ms settle ladder then one Enter therefore only worked while
	// the ladder happened to outlast the fetch; under CI runner load it
	// intermittently did not, leaving the switch un-fired. No bound on
	// the ladder can promise that, so the wait is poll-until-converged:
	// each cycle retries Enter, uses the settle barrier (Send blocks
	// until the loop receives it, so every earlier message is handled),
	// and reads the accessors mid-poll — the same discipline the
	// pre-switch ActiveSessionID check above uses. Retrying Enter is
	// safe: before the fetch lands it is a no-op, and with the ordering
	// guard above every populated-table Enter targets B, so the loop
	// breaks on the first cycle that observes the switch. The 30s bound
	// is a failure detector, not a timing assumption.
	switched := false
	for i := 0; i < 60 && !switched; i++ {
		hp.sendKey("enter")
		hp.settle()
		time.Sleep(100 * time.Millisecond)
		if hp.app.ActiveSessionID() == idB {
			switched = true
			break
		}
	}
	if !switched {
		t.Logf("session switch did not converge after 60 Enter retries (last active %q)",
			hp.app.ActiveSessionID())
	}
	// Give any async transcript reload (chat.SetSession cmd) one settle
	// cycle before finish() asserts on the finished model.
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
//     the table — nothing else in the app emits them; column titles are
//     accepted in the fitted-truncation form the tableutil budget renders
//     at the fixed size (05d24dd2 precedent: the fitted form IS the
//     correct render, not a missing column);
//   - the employee row can only come from the agents.roster RPC (the
//     unified fleet: seeded registry agents + bots + hired employees),
//     so it is a cross-boundary assertion, not a rendering echo; the
//     count badge asserts the seeded-minus-empty DELTA, never a
//     hardcoded fleet size, and the marker row renders at a viewport
//     tall enough to hold the whole fleet (the last roster row lives
//     below the fold at 100x30);
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

	// Absence FIRST on its own stack: the same view with NO employee
	// cannot carry the row, so the marker below comes from the fetch and
	// not from the shell. The hermetic harness seeds the repo roster
	// (config/agents -> MEEPT_HOME, seedRoster), and the panel serves
	// the UNIFIED roster (agents.roster: registry agents + bots +
	// employees, a28f36e5), so the empty stack is not literally zero
	// rows: the load-bearing count assertion is seeded minus empty ==
	// exactly one employee.
	emptyStack := harness.Start(t)
	_ = emptyStack.CreateSession(t, "agents-empty", emptyStack.MeeptHome)
	emptyApp := openAgentsView(t, emptyStack)
	emptyView := emptyApp.View().Content
	if strings.Contains(emptyView, marker) {
		t.Fatalf("agents view shows the employee row %q without any employee "+
			"hired — the assertion above is not load-bearing:\n%s", marker, emptyView)
	}
	emptyCount := mustAgentCount(t, emptyView)

	// Leg 1: the default fixed size (100x30).
	app := openAgentsView(t, stack)
	view := app.View().Content

	// The panel's own identifying content — all render only from
	// AgentsPanel.View(). Column titles are asserted in either the
	// full or the fitted-truncation form (05d24dd2 precedent): at the
	// 100-cell default the tableutil budget leaves the 5+ char titles
	// ellipsized (drift -> "dri…"), and that is the CORRECT rendered
	// form, not a missing column.
	for _, want := range []string{
		"agents",                      // renderHeader title
		"list",                        // active sub-view tab
		"approvals",                   // sub-view tabs
		"audit",                       // sub-view tabs
		"r: refresh | enter: details", // renderHelpHint
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("agents view missing %q — the panel did not render its own "+
				"content (zero-width-viewport regression class):\n%s", want, view)
		}
	}
	for _, title := range [][2]string{
		{"id", "id"},            // column title (full at any fitted width)
		{"drift", "dri…"},       // column title
		{"findings", "fin…"},    // column title
		{"last run", "last r…"}, // column title
	} {
		if !strings.Contains(view, title[0]) && !strings.Contains(view, title[1]) {
			t.Fatalf("agents view missing column title %q (fitted form %q) — the "+
				"panel did not render its own content:\n%s", title[0], title[1], view)
		}
	}
	// The count badge proves the ROW COUNT reached the model, not just a
	// hardcoded header: renderHeader prints "(N agents)", and the seeded
	// stack carries exactly one more row than the empty stack.
	seededCount := mustAgentCount(t, view)
	if seededCount != emptyCount+1 {
		t.Fatalf("agents view header count = %d, want empty(%d)+1 — the "+
			"agents.roster fetch did not include the hired employee:\n%s", seededCount, emptyCount, view)
	}

	// Leg 1b: the marker ROW. The unified roster is the seeded repo
	// fleet (config/agents) plus the hired employee, so the row is the
	// LAST of the fleet and below the fold at 100x30. Render at a
	// viewport tall enough to hold the whole fleet (the panel sizes its
	// table height through the same tableutil path): the row's presence
	// in any fitted truncation form is the cross-boundary assertion —
	// only the roster RPC can put quartzwatcher on screen.
	tall := openAgentsView(t, stack, WithSize(100, 80)).View().Content
	if !containsAnyForm(tall, marker) {
		t.Fatalf("agents view at 100x80 does not show the employee row %q (or a fitted "+
			"truncation form) with the whole fleet on screen:\n%s", marker, tall)
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

	// Leg 2: a COMPACT viewport (width < 80 -> LayoutCompact, sidebar
	// hidden) still renders the header and the row — the panel sizes its
	// table on both axes and must not fall off a size cliff. At 40
	// columns the id column's fitted width is 6 (FitWidthsMin over a
	// 24-cell budget), so bubbles' MaxWidth truncation renders the
	// marker as "quart…" (5 chars + the ellipsis) — the compact leg
	// asserts against the fitted form and stays marker-anchored
	// (05d24dd2 precedent). Height 60 puts the whole seeded fleet on
	// one page: the regression class under test is the WIDTH cliff, and
	// a 20-row viewport would clip the last row at the default cursor
	// for an unrelated (height) reason.
	compact := openAgentsView(t, stack, WithSize(40, 60)).View().Content
	if !containsAnyForm(compact, marker) {
		t.Fatalf("agents view at 40x60 dropped the employee row %q (checked forms %v) "+
			"(degenerate-resize regression class):\n%s", marker, truncForms(marker), compact)
	}
	if mustAgentCount(t, compact) != emptyCount+1 {
		t.Fatalf("agents view at 40x60 lost its header/count:\n%s", compact)
	}

	// Leg 3: the resize ROUND TRIP — boot compact, then grow back to
	// the wide size and re-render. SetSize repopulates the table from
	// the cached agents; a regression that only clears rows would show
	// an empty body here while the header still renders. Both sizes
	// keep the whole fleet on one page so the marker assertion stays
	// about the repopulation, not the viewport fold.
	rtHP := startTUIAgainst(t, stack, WithSize(40, 60))
	settleAsync()
	rtHP.sendKey("ctrl+x")
	rtHP.settle()
	rtHP.sendKey("e") // palette action: agents view
	settleAsync()
	settleAsync()
	rtHP.program.Send(tea.WindowSizeMsg{Width: 100, Height: 60}) // grow back
	settleAsync()
	roundTrip := rtHP.finish().View().Content
	// The row must SURVIVE the round-trip in SOME truncation form —
	// whether the grow lands before finish() is timing- and
	// layout-dependent, so all fitted forms are accepted (probe-verified
	// 2026-10-14; the header-count assertion below pins the fetch state).
	if !containsAnyForm(roundTrip, marker) {
		t.Fatalf("agents view lost the employee row %q (checked forms %v) after a compact→wide "+
			"resize round-trip:\n%s", marker, truncForms(marker), roundTrip)
	}
	if mustAgentCount(t, roundTrip) != emptyCount+1 {
		t.Fatalf("agents view header count lost after the resize round-trip:\n%s",
			roundTrip)
	}
}

// mustAgentCount extracts the renderHeader "(N agents)" count. The
// unified roster is seeded by the harness (config/agents), so the count
// is environment-shaped: the assertions are seeded-minus-empty deltas,
// never a hardcoded fleet size.
func mustAgentCount(t *testing.T, view string) int {
	t.Helper()
	re := regexp.MustCompile(`\((\d+) agents\)`)
	m := re.FindStringSubmatch(view)
	if m == nil {
		t.Fatalf("agents view carries no \"(N agents)\" header badge:\n%s", view)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("parse agents count %q: %v", m[1], err)
	}
	return n
}

// truncForms lists the accepted fitted truncation forms of a row marker:
// the full text plus the ellipsis forms bubbles/table renders for
// columns narrower than the marker. The fleet is 9 columns wide, so at
// 40 cells the id column fits to 4 (renders "qua…") and at 100 cells it
// fits to ~12 ("quartzwatch…"); a 13+ cell column renders the full
// marker. Forms probed against the live render (05d24dd2 precedent:
// accept the fitted form, anchor on the row's presence).
func truncForms(marker string) []string {
	forms := []string{marker}
	for _, n := range []int{11, 5, 3} {
		if len(marker) > n {
			forms = append(forms, marker[:n]+"…")
		}
	}
	return forms
}

func containsAnyForm(view, marker string) bool {
	for _, form := range truncForms(marker) {
		if strings.Contains(view, form) {
			return true
		}
	}
	return false
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
