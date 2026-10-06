package tui

import (
	"testing"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
)

// Phase 3 breadth goldens (leaf 03): queue, memory, plans, search views,
// plus the goldens-only surfaces (sidebar, vim, viz, prompts stay manual
// per the leaf — sidebar gets a golden since it's a pure render).

func TestGoldenQueueView(t *testing.T) {
	hp := newHeadlessAppQueue(t, goldenWidth, goldenHeight)
	switchView(t, hp, "q")
	settleAsync()
	app := hp.finish(goldenWidth, goldenHeight)
	assertGolden(t, "queue_view", captureView(t, app))
}

func TestGoldenMemoryView(t *testing.T) {
	hp := newHeadlessAppMemory(t, goldenWidth, goldenHeight)
	switchView(t, hp, "m")
	settleAsync()
	app := hp.finish(goldenWidth, goldenHeight)
	assertGolden(t, "memory_view", captureView(t, app))
}

func TestGoldenPlansView(t *testing.T) {
	hp := newHeadlessAppPlans(t, goldenWidth, goldenHeight)
	switchView(t, hp, "p")
	settleAsync()
	app := hp.finish(goldenWidth, goldenHeight)
	assertGolden(t, "plans_view", captureView(t, app))
}

func TestGoldenSearchView(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	// Search view: sessions view 'f' opens it (OpenSearchViewMsg) — the
	// real key flow.
	switchView(t, hp, "s")
	hp.send(keyPress('f', 0))
	settleAsync()
	app := hp.finish(goldenWidth, goldenHeight)
	if app.ActiveView() != ViewSearch {
		t.Fatalf("search view did not open via sessions 'f': view=%v", app.ActiveView())
	}
	assertGolden(t, "search_view", captureView(t, app))
}

func TestGoldenSidebar(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()
	// Goldens-only surface: capture the sidebar panel's own render (the
	// full view mixes in the chat transcript, whose welcome bubble races
	// the load — see golden_views_test.go chat_loaded).
	app := hp.finish(goldenWidth, goldenHeight)
	assertGolden(t, "sidebar", app.sidebar.View())
}

// --- stub worlds ---------------------------------------------------------

func newHeadlessAppQueue(t *testing.T, w, h int) *headlessProgram {
	t.Helper()
	hp := newHeadlessApp(t, w, h)
	old := time.Now().Add(-40 * 24 * time.Hour).Format(time.RFC3339)
	setJSONStub(t, hp, "queue.stats", map[string]any{
		"by_state":    map[string]int{"pending": 2, "completed": 5},
		"by_priority": map[string]int{"normal": 7},
	})
	setJSONStub(t, hp, "queue.list", map[string]any{
		"jobs": []types.QueueJob{
			{ID: "job-1", TaskID: "task-1", Type: "one_off", Priority: 2, State: "pending", CreatedAt: old, UpdatedAt: old},
			{ID: "job-2", Type: "project_task", Priority: 3, State: "completed", CreatedAt: old, UpdatedAt: old},
		},
	})
	return hp
}

func newHeadlessAppMemory(t *testing.T, w, h int) *headlessProgram {
	t.Helper()
	hp := newHeadlessApp(t, w, h)
	// The memory view renders the search input + empty state on init; the
	// query path is RPC-driven and covered by its unit tests.
	return hp
}

func newHeadlessAppPlans(t *testing.T, w, h int) *headlessProgram {
	t.Helper()
	hp := newHeadlessApp(t, w, h)
	old := time.Now().Add(-40 * 24 * time.Hour).Format(time.RFC3339)
	setJSONStub(t, hp, "plan.list_by_session", map[string]any{
		"plans": []types.PlanExtended{
			{ID: "plan-1", Title: "rewrite the parser", FilePath: "docs/plans/rewrite.md", State: "draft", CreatedAt: old, UpdatedAt: old, TotalSteps: 4, CompletedSteps: 1},
		},
	})
	return hp
}

// setJSONStub marshals v and pins it as the stub result for method.
func setJSONStub(t *testing.T, hp *headlessProgram, method string, v any) {
	t.Helper()
	hp.stub.setResult(method, v)
}
