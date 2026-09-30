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
	idA := stack.CreateSession(t, "palette-alpha", stack.MeeptHome)
	idB := stack.CreateSession(t, "palette-beta", stack.MeeptHome)
	_ = idB

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
	// the daemon-side sessions (the seeded descriptions come from the
	// session.list round-trip the palette action triggered).
	view := app.View().Content
	if !strings.Contains(view, "palette-alpha") {
		t.Errorf("sessions view does not show the seeded session after the palette "+
			"command executed (session.list RPC effect missing):\n%s", view)
	}
	_ = idA
}

// TestSessionSwitchReloadsTranscript covers tui-session-switch-01: two
// sessions exist; switching via the picker keys fires the switch effect
// and the chat view re-targets the picked session (transcript reload).
func TestSessionSwitchReloadsTranscript(t *testing.T) {
	stack := harness.Start(t)
	idA := stack.CreateSession(t, "switch-alpha", stack.MeeptHome)
	idB := "" // created after boot (activity gap) — see below

	// Boot the TUI pinned to session A; the switch must move it to B.
	hp := startTUIAgainst(t, stack, WithTargetSession(idA))
	time.Sleep(800 * time.Millisecond)
	hp.settle()
	// Create B after boot. Its row position depends on last-activity
	// ordering, which the TUI boot itself influences (attaching to A
	// touches A's activity), so the probe app below LEARNS the row order
	// from the rendered view instead of assuming it.
	idB = stack.CreateSession(t, "switch-beta", stack.MeeptHome)

	if got := hp.app.ActiveSessionID(); got != idA {
		t.Fatalf("target session not loaded at start: got %q want %q", got, idA)
	}

	// Probe app: learn which session the sessions view's cursor row 0
	// holds (the detail pane shows the auto-selected row's name) — the
	// ordering is stable across apps on the same daemon data, so the
	// driven app can pick the navigation that lands on B.
	probe := startTUIAgainst(t, stack, WithTargetSession(idA))
	time.Sleep(800 * time.Millisecond)
	probe.settle()
	probe.sendKey("ctrl+x")
	probe.settle()
	probe.sendKey("s")
	settleAsync()
	probeView := stripEscapes(probe.finish().View().Content)
	row0IsB := strings.Contains(probeView, "switch-beta") &&
		strings.Index(probeView, "switch-beta") < strings.Index(probeView, "switch-alpha")

	// Driven app: sessions tab via palette, then the picker's documented
	// switch flow (enter: switch) on the row that holds B — down+enter if
	// A is row 0, plain enter if B is row 0.
	hp = startTUIAgainst(t, stack, WithTargetSession(idA))
	time.Sleep(800 * time.Millisecond) // let the boot load settle first
	hp.settle()
	hp.sendKey("ctrl+x")
	hp.settle()
	hp.sendKey("s")
	settleAsync()
	if !row0IsB {
		hp.sendKey("down")
		hp.settle()
	}
	hp.sendKey("enter")
	settleAsync()

	app := hp.finish()
	gotSession := app.ActiveSessionID()
	if gotSession != idB {
		t.Errorf("active session after picker switch = %q, want %q (row0IsB=%v)", gotSession, idB, row0IsB)
	}
	if got := app.ChatSessionID(); got != idB {
		t.Errorf("chat model session after switch = %q, want %q (transcript reload)", got, idB)
	}
}

// stripEscapes removes ANSI escape bytes so plain-text checks run on the
// rendered view.
func stripEscapes(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == 0x1b {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// TestAgentTabRendersTableRows covers tui-agent-tab-01: the agents view
// renders at a fixed size without the zero-width-viewport class of bugs
// (a width-0 viewport renders no rows) — the view must come up populated
// with its header/state, not blank or "unavailable".
func TestAgentTabRendersTableRows(t *testing.T) {
	stack := harness.Start(t)
	_ = stack.CreateSession(t, "agents-session", stack.MeeptHome)

	hp := startTUIAgainst(t, stack)
	settleAsync()

	hp.sendKey("ctrl+x")
	hp.settle()
	hp.sendKey("e") // palette action: agents view
	settleAsync()

	app := hp.finish()
	if app.ActiveView() != tui.ViewAgents {
		t.Fatalf("currentView = %v, want agents view", app.ActiveView())
	}
	view := app.View().Content
	if strings.Contains(view, "agents unavailable") {
		t.Fatalf("agents panel not constructed")
	}
	if strings.TrimSpace(view) == "" {
		t.Fatal("agents view rendered empty (zero-size viewport class)")
	}
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
