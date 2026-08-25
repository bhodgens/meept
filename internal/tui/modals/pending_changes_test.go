package modals

import (
	"errors"
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// runMsgSync executes a tea.Cmd synchronously and returns its message.
func runMsgSync(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil command")
	}
	return cmd()
}

// fakeChangesAPI implements PendingChangeAPI for modal tests.
type fakeChangesAPI struct {
	changes     []PendingChange
	listErr     error
	accepted    []string
	rejected    []string
	acceptErr   error
	rejectErr   error
	listCalls   int
	acceptCalls int
	rejectCalls int
}

func (f *fakeChangesAPI) ListPendingChanges(sessionID string) ([]PendingChange, error) {
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.changes, nil
}

func (f *fakeChangesAPI) AcceptPendingChange(id string) error {
	f.acceptCalls++
	if f.acceptErr != nil {
		return f.acceptErr
	}
	f.accepted = append(f.accepted, id)
	return nil
}

func (f *fakeChangesAPI) RejectPendingChange(id string) error {
	f.rejectCalls++
	if f.rejectErr != nil {
		return f.rejectErr
	}
	f.rejected = append(f.rejected, id)
	return nil
}

func testChanges(n int) []PendingChange {
	changes := make([]PendingChange, 0, n)
	for i := range n {
		changes = append(changes, PendingChange{
			ID:        "stage-" + string(rune('a'+i)),
			FilePath:  "/tmp/file" + string(rune('a'+i)) + ".txt",
			Diff:      "@@ -1 +1 @@\n-old line\n+new line",
			CreatedAt: "2026-08-25T00:00:00Z",
		})
	}
	return changes
}

func TestPendingChangesModal_RendersAllItems(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{"single change", 1},
		{"three changes", 3},
		{"scrolls beyond max visible", 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeChangesAPI{changes: testChanges(tt.count)}
			m := NewPendingChangesModal(api)
			// Install the list the way App.Update does after the fetch.
			changes, listErr := api.ListPendingChanges("s")
			if listErr != nil {
				t.Fatal(listErr)
			}
			m.SetChanges(changes)
			_ = m.Show("sess-1")

			view := m.View(120, 40)
			wantVisible := min(tt.count, m.maxVisible)
			for i := range wantVisible {
				id := api.changes[i].ID
				if !strings.Contains(view, id) {
					t.Errorf("view missing change %q", id)
				}
			}
			if tt.count > m.maxVisible && !strings.Contains(view, "showing") {
				t.Error("expected scroll indicator for long lists")
			}
		})
	}
}

func TestPendingChangesModal_EmptyListRendersEmptyState(t *testing.T) {
	m := NewPendingChangesModal(&fakeChangesAPI{})
	_ = m.Show("sess-1")
	m.SetChanges(nil)

	view := m.View(120, 40)
	if !strings.Contains(view, "no pending changes") {
		t.Errorf("view = %q, want empty state text", view)
	}
}

func TestPendingChangesModal_Navigation(t *testing.T) {
	api := &fakeChangesAPI{changes: testChanges(3)}
	m := NewPendingChangesModal(api)
	m.SetChanges(api.changes)
	_ = m.Show("sess-1")

	if got := m.SelectedIndex(); got != 0 {
		t.Fatalf("initial selection = %d, want 0", got)
	}
	m.HandleKey("j")
	m.HandleKey("down")
	if got := m.SelectedIndex(); got != 2 {
		t.Fatalf("after j+down selection = %d, want 2", got)
	}
	// Clamped at the end.
	m.HandleKey("j")
	if got := m.SelectedIndex(); got != 2 {
		t.Fatalf("selection past end = %d, want 2", got)
	}
	m.HandleKey("k")
	if got := m.SelectedIndex(); got != 1 {
		t.Fatalf("after k selection = %d, want 1", got)
	}
}

func TestPendingChangesModal_AcceptCallsAPI(t *testing.T) {
	api := &fakeChangesAPI{changes: testChanges(2)}
	m := NewPendingChangesModal(api)
	m.SetChanges(api.changes)
	_ = m.Show("sess-1")

	cmd := m.HandleKey("a")
	if cmd == nil {
		t.Fatal("accept returned nil command")
	}
	runMsgSync(t, cmd)
	if len(api.accepted) != 1 || api.accepted[0] != api.changes[0].ID {
		t.Errorf("accepted = %v, want [%s]", api.accepted, api.changes[0].ID)
	}
}

func TestPendingChangesModal_RejectCallsAPI(t *testing.T) {
	api := &fakeChangesAPI{changes: testChanges(2)}
	m := NewPendingChangesModal(api)
	m.SetChanges(api.changes)
	_ = m.Show("sess-1")
	m.HandleKey("j") // select second change

	cmd := m.HandleKey("r")
	if cmd == nil {
		t.Fatal("reject returned nil command")
	}
	runMsgSync(t, cmd)
	if len(api.rejected) != 1 || api.rejected[0] != api.changes[1].ID {
		t.Errorf("rejected = %v, want [%s]", api.rejected, api.changes[1].ID)
	}
}

func TestPendingChangesModal_AcceptErrorPropagates(t *testing.T) {
	api := &fakeChangesAPI{changes: testChanges(1), acceptErr: errors.New("file changed since staging")}
	m := NewPendingChangesModal(api)
	m.SetChanges(api.changes)
	_ = m.Show("sess-1")

	cmd := m.HandleKey("a")
	msg := runMsgSync(t, cmd)
	result, ok := msg.(PendingChangeActionResultMsg)
	if !ok {
		t.Fatalf("msg type = %T, want PendingChangeActionResultMsg", msg)
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "file changed since staging") {
		t.Errorf("err = %v, want drift message", result.Err)
	}
	if result.Action != "accept" {
		t.Errorf("action = %q, want accept", result.Action)
	}
}

func TestPendingChangesModal_ActionsOnEmptyListAreNoOps(t *testing.T) {
	api := &fakeChangesAPI{}
	m := NewPendingChangesModal(api)
	m.SetChanges(nil)
	_ = m.Show("sess-1")

	if cmd := m.HandleKey("a"); cmd != nil {
		t.Error("accept on empty list should return nil command")
	}
	if cmd := m.HandleKey("r"); cmd != nil {
		t.Error("reject on empty list should return nil command")
	}
	if cmd := m.HandleKey("v"); cmd != nil {
		t.Error("view on empty list should return nil command")
	}
	if m.InDiffMode() {
		t.Error("diff mode must not activate on empty list")
	}
}

func TestPendingChangesModal_DiffMode(t *testing.T) {
	api := &fakeChangesAPI{changes: testChanges(1)}
	m := NewPendingChangesModal(api)
	m.SetChanges(api.changes)
	_ = m.Show("sess-1")

	m.HandleKey("v")
	if !m.InDiffMode() {
		t.Fatal("v did not enter diff mode")
	}
	view := m.View(120, 40)
	if !strings.Contains(view, "+new line") {
		t.Errorf("diff view missing diff body: %q", view)
	}
	// v again returns to the list.
	m.HandleKey("v")
	if m.InDiffMode() {
		t.Error("second v did not leave diff mode")
	}
	// esc leaves diff mode first without closing the modal.
	m.HandleKey("v")
	m.HandleKey("esc")
	if m.InDiffMode() {
		t.Error("esc did not leave diff mode")
	}
	if !m.IsVisible() {
		t.Error("esc from diff mode closed the modal")
	}
	// esc again closes.
	m.HandleKey("esc")
	if m.IsVisible() {
		t.Error("second esc did not close the modal")
	}
}

func TestPendingChangesModal_EscCloses(t *testing.T) {
	api := &fakeChangesAPI{changes: testChanges(1)}
	m := NewPendingChangesModal(api)
	m.SetChanges(api.changes)
	_ = m.Show("sess-1")

	m.HandleKey("esc")
	if m.IsVisible() {
		t.Error("esc did not close the modal")
	}
}

func TestPendingChangesModal_FetchCount(t *testing.T) {
	api := &fakeChangesAPI{changes: testChanges(3)}
	m := NewPendingChangesModal(api)

	msg := runMsgSync(t, m.FetchCount("sess-1"))
	count, ok := msg.(PendingChangesCountMsg)
	if !ok {
		t.Fatalf("msg type = %T, want PendingChangesCountMsg", msg)
	}
	if count.Count != 3 {
		t.Errorf("count = %d, want 3", count.Count)
	}
}

func TestPendingChangesModal_NilAPIDegrades(t *testing.T) {
	m := NewPendingChangesModal(nil)
	msg := runMsgSync(t, m.FetchCount("sess-1"))
	if count, ok := msg.(PendingChangesCountMsg); !ok || count.Count != 0 {
		t.Errorf("nil api count = %+v, want 0", msg)
	}
}

func TestPendingChangesModal_LowercaseStrings(t *testing.T) {
	api := &fakeChangesAPI{changes: testChanges(1)}
	m := NewPendingChangesModal(api)
	m.SetChanges(api.changes)
	_ = m.Show("sess-1")

	view := m.View(120, 40)
	for _, line := range strings.Split(view, "\n") {
		// Skip decoration and diff content markers; check that no line
		// carries an uppercase alphabetic character (lowercase mandate).
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "─") || strings.HasPrefix(trimmed, "▸") {
			continue
		}
		if strings.Contains(trimmed, "/tmp/file") {
			continue // file paths are data, not UI strings
		}
		for _, r := range trimmed {
			if unicode.IsUpper(r) {
				t.Errorf("uppercase character in ui string: %q", trimmed)
				break
			}
		}
	}
}
