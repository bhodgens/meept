package tui

import (
	"strings"
	"testing"
)

// C3: when the daemon refuses kanban.board.list because the kanban subsystem
// is disabled, the TUI must render the short guidance string instead of the
// raw RPC error. The raw error is "method not found: kanban.board.list"
// (internal/rpc/server.go), which is cryptic to a user.
const wantKanbanDisabledHint = "kanban board is not enabled; set [kanban].enabled=true"

// This reuses fakeKanbanRPC from kanban_view_test.go: setError makes
// kanban.board.list answer with the method-not-found error, then the normal
// Init/drain chain feeds it through kanbanBoardsMsg into the model.
//
// RED today: kanban View() renders "board error: method not found: ..."
// (kanban_view_render.go), so the C3 hint is absent and the raw error is
// present — an assertion failure, not a compile error.
func TestKanbanDisabled_ShowsGuidanceInsteadOfRawError(t *testing.T) {
	f := newFakeKanbanRPC()
	f.setError("kanban.board.list", "method not found: kanban.board.list")

	p := buildPanel(t, f)
	out := p.View()

	if !strings.Contains(out, wantKanbanDisabledHint) {
		t.Errorf("kanban View() is missing the C3 disabled hint %q\n%s", wantKanbanDisabledHint, out)
	}
	if strings.Contains(out, "method not found") {
		t.Errorf("kanban View() still shows the raw method-not-found error instead of guidance\n%s", out)
	}
}

// The guidance must survive the lowercase UI rule and be the error line the
// user sees: assert it appears verbatim (the constant is already lowercase).
func TestKanbanDisabled_HintIsLowercaseAndVerbatim(t *testing.T) {
	f := newFakeKanbanRPC()
	f.setError("kanban.board.list", "method not found: kanban.board.list")
	p := buildPanel(t, f)
	out := p.View()

	if wantKanbanDisabledHint != strings.ToLower(wantKanbanDisabledHint) {
		t.Fatal("test constant must be lowercase per repo UI rule")
	}
	if !strings.Contains(out, wantKanbanDisabledHint) {
		t.Errorf("kanban View() does not contain the verbatim C3 hint\n%s", out)
	}
}
