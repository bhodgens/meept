package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/models"
	"github.com/caimlas/meept/internal/tui/types"
)

// Phase 1 golden batch (leaf 01): chat empty, chat loaded (one assistant
// msg), sessions view, tasks view, palette open, status bar. Phase 3
// extends this file with queue/memory/plans/search + the goldens-only
// surfaces (sidebar, vim, viz, prompts).
//
// All captures run at 80x24 unless the test name says otherwise. See
// golden_test.go for the determinism rules and strip list.
//
// Concurrency rule: the program event-loop goroutine owns ALL model
// mutation. Tests drive it exclusively through hp.send (key presses and
// domain messages) — App.Update routes domain messages to the current
// view's sub-model, so seeding data is a Send, never a direct Update
// call. hp.settle after each sequence is the event-loop barrier.

const goldenWidth, goldenHeight = 80, 24

// switchView opens the palette (ctrl+x) and presses the given palette
// action key, exercising the real key flow (handleModalKey).
func switchView(t *testing.T, hp *headlessProgram, key string) {
	t.Helper()
	hp.send(keyPress('x', tea.ModCtrl))
	hp.send(keyPress(rune(key[0]), 0))
}


// retryWithFreshApp runs attempt up to 4 times, each with a freshly built
// app, and returns on the first attempt whose finished view contains
// wantIn. The remaining nondeterminism this absorbs is goroutine
// scheduling of async cmd results (fetch/load) relative to the scripted
// sends — an attempt whose load landed late produces a wrong view and is
// discarded whole (fresh app, fresh goroutines); no state leaks between
// attempts.
func retryWithFreshApp(t *testing.T, wantIn string, attempt func(t *testing.T) *App) *App {
	t.Helper()
	var last *App
	for i := 0; i < 4; i++ {
		app := attempt(t)
		last = app
		if strings.Contains(app.View().Content, wantIn) {
			return app
		}
	}
	t.Fatalf("view never reached expected state %q after 4 attempts; last:\n%s", wantIn, last.View().Content)
	return last
}

func TestGoldenChatEmpty(t *testing.T) {
	app := retryWithFreshApp(t, "welcome to meept", func(t *testing.T) *App {
		hp := newHeadlessApp(t, goldenWidth, goldenHeight)
		settleAsync()
		// chat.Init adds the welcome bubble only when the transcript
		// is empty; the startup loadSession's SetSession clears it, so
		// fire the view init again (the real Ctrl+X-c path) after the
		// load settled.
		hp.send(keyPress('x', tea.ModCtrl))
		hp.send(keyPress('c', 0))
		settleAsync()
		return hp.finish(goldenWidth, goldenHeight)
	})
	assertGolden(t, "chat_empty", captureView(t, app))
}

func TestGoldenChatLoadedOneAssistant(t *testing.T) {
	// Task result bubble: addMessage stamps time.Now(); the golden
	// post-processor replaces the HH:MM header with a fixed token.
	app := retryWithFreshApp(t, "task completed", func(t *testing.T) *App {
		hp := newHeadlessApp(t, goldenWidth, goldenHeight)
		// Give the startup loadSession's SetSession (async cmd) time to
		// land BEFORE the bubble: it resets the transcript, and a bubble
		// sent before it is wiped. settleAsync's 100ms is normally ample;
		// attempts that still lose the race are discarded by the retry.
		settleAsync()
		hp.send(models.ChatTaskResultMsg{
			State:         "completed",
			TaskID:        "golden-task",
			ResultSummary: "wrote hello.txt with the word hello",
		})
		hp.settle(goldenWidth, goldenHeight)
		settleAsync()
		return hp.finish(goldenWidth, goldenHeight)
	})
	assertGolden(t, "chat_loaded", captureView(t, app))
}

func TestGoldenSessionsView(t *testing.T) {
	app := retryWithFreshApp(t, "alpha session", func(t *testing.T) *App {
		hp := newHeadlessAppSessions(t, goldenWidth, goldenHeight)
		switchView(t, hp, "s")
		settleAsync()
		return hp.finish(goldenWidth, goldenHeight)
	})
	assertGolden(t, "sessions_view", captureView(t, app))
}

// newHeadlessAppSessions is the sessions-golden world: the stub RPC server
// serves two sessions with a STABLE relative-time cell (last_activity 40
// days in the past lands in the "Jan 2" absolute-date branch of
// formatRelativeTime — accepted documented hazard, see golden_test.go).
func newHeadlessAppSessions(t *testing.T, w, h int) *headlessProgram {
	t.Helper()
	hp := newHeadlessApp(t, w, h)
	old := time.Now().Add(-40 * 24 * time.Hour).Format(time.RFC3339)
	sessions := []types.Session{
		{ID: "sess-alpha", Name: "alpha", Description: "alpha session", CreatedAt: old, LastActivity: old},
		{ID: "sess-beta", Name: "beta", Description: "beta session", CreatedAt: old, LastActivity: old},
	}
	raw, err := json.Marshal(map[string]any{"sessions": sessions})
	if err != nil {
		t.Fatalf("marshal sessions: %v", err)
	}
	hp.stub.setResult("session.list", json.RawMessage(raw))
	return hp
}

func TestGoldenTasksView(t *testing.T) {
	app := retryWithFreshApp(t, "write the gold", func(t *testing.T) *App {
		hp := newHeadlessAppTasks(t, goldenWidth, goldenHeight)
		switchView(t, hp, "t")
		settleAsync()
		return hp.finish(goldenWidth, goldenHeight)
	})
	assertGolden(t, "tasks_view", captureView(t, app))
}

// newHeadlessAppTasks is the tasks-golden world: the stub serves one
// completed task. formatTimeAgo renders the LAST 8 CHARS of the timestamp
// (documented quirk), so the fixture uses a FIXED non-parseable stamp —
// formatTimeAgo never parses, it only slices: constant in, constant out.
func newHeadlessAppTasks(t *testing.T, w, h int) *headlessProgram {
	t.Helper()
	hp := newHeadlessApp(t, w, h)
	const fixedStamp = "golden-fixed"
	tasks := []types.TaskExtended{
		{Task: types.Task{
			ID:            "task-1",
			Name:          "write the golden fixture",
			State:         "completed",
			TotalJobs:     2,
			CompletedJobs: 2,
			CreatedAt:     fixedStamp,
			UpdatedAt:     fixedStamp,
		}},
	}
	raw, err := json.Marshal(map[string]any{"tasks": tasks})
	if err != nil {
		t.Fatalf("marshal tasks: %v", err)
	}
	hp.stub.setResult("task.list_extended", json.RawMessage(raw))
	return hp
}

func TestGoldenPaletteOpen(t *testing.T) {
	app := retryWithFreshApp(t, "command palette", func(t *testing.T) *App {
		hp := newHeadlessApp(t, goldenWidth, goldenHeight)
		hp.send(keyPress('x', tea.ModCtrl))
		hp.settle(goldenWidth, goldenHeight)
		return hp.finish(goldenWidth, goldenHeight)
	})
	if app.activeModal != ModalCommandPalette {
		t.Fatalf("palette did not open on ctrl+x (activeModal=%v)", app.activeModal)
	}
	assertGolden(t, "palette_open", captureView(t, app))
}

func TestGoldenStatusBar(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	// renderStatusBar is a pure function of app state; the fixture
	// captures just the bar. resetStatusApp clears the transient
	// "resumed session:" line (wall-clock, see strip rules).
	app := hp.finish(goldenWidth, goldenHeight)
	resetStatusApp(app)
	assertGolden(t, "status_bar", app.renderStatusBar())
}

// TestGoldenDeterministicRepeat is the leaf self-check: capturing the
// same scripted state twice must produce byte-identical normalized
// output (no wall-clock or map-iteration leakage into View()).
func TestGoldenDeterministicRepeat(t *testing.T) {
	app := retryWithFreshApp(t, "alpha session", func(t *testing.T) *App {
		hp := newHeadlessAppSessions(t, goldenWidth, goldenHeight)
		switchView(t, hp, "s")
		settleAsync()
		return hp.finish(goldenWidth, goldenHeight)
	})
	first := captureView(t, app)
	second := captureView(t, app)
	if first != second {
		t.Errorf("View() not deterministic across captures:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// TestGoldenStatusMessageClear documents the strip rule: a status
// message set through Update carries a tea.Tick; after the scripted
// flow, resetStatus clears the line so the golden shows the steady-state
// bar, not the transient message.
func TestGoldenStatusMessageClear(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	// Simulate what the ctrl+c hint does (statusMessage + time.Now()):
	// direct field writes are safe only after finish (loop stopped).
	app := hp.finish(goldenWidth, goldenHeight)
	app.statusMessage = "transient status"
	app.statusMessageTime = time.Now()
	resetStatusApp(app)
	view := app.View().Content
	if strings.Contains(view, "transient status") {
		t.Errorf("status message still visible after resetStatus:\n%s", view)
	}
}
