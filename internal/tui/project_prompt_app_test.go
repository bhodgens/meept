package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/caimlas/meept/internal/tui/modals"
	"github.com/caimlas/meept/internal/tui/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Project-prompt modal wiring pin: the modal funcs were dead (zero callers);
// these drive the REAL App.Update path — open-message in, modal active — and
// pin the accept/dismiss flows through handleModalKey.
// ---------------------------------------------------------------------------

func newProjectPromptTestApp() *App {
	app := createTestApp()
	app.projectPrompt = modals.NewProjectPromptModal("", "")
	app.sessionRename = NewSessionRenameModal(DefaultStyles())
	app.projectPicker = NewProjectPickerModal(DefaultStyles(), app.rpc)
	// createTestApp leaves sessionMgr nil (NewApp sets it); SessionSwitchMsg
	// writes it. Same fix as headless_test.go.
	app.sessionMgr = sharedclient.NewSessionManager(app.rpc, app.clientConfig.Session.DefaultName)
	return app
}

func TestApp_Update_OpenProjectPromptMsg_ActivatesModal(t *testing.T) {
	app := newProjectPromptTestApp()

	newModel, _ := app.Update(modals.OpenProjectPromptMsg{
		SessionID:   "sess-1",
		DefaultPath: "/home/user/proj",
	})
	newApp := newModel.(*App)

	assert.Equal(t, ModalProjectPrompt, newApp.activeModal,
		"sending the open-message must activate the project-prompt modal")
	require.NotNil(t, newApp.projectPrompt)
	assert.True(t, newApp.projectPrompt.IsVisible())
	assert.Equal(t, "/home/user/proj", newApp.projectPrompt.SessionDefaultPath())

	// The overlay renders the prompt.
	overlay := newApp.renderModalOverlay()
	assert.Contains(t, overlay, "Session has no project bound")
	assert.Contains(t, overlay, "/home/user/proj")
}

func TestApp_Update_AcceptedConfirm_BindsViaProjectSetPath(t *testing.T) {
	app := newProjectPromptTestApp()
	// Arm the modal as if open (open-message path first).
	newModel, _ := app.Update(modals.OpenProjectPromptMsg{SessionID: "sess-1", DefaultPath: "/tmp/proj"})
	app = newModel.(*App)

	// The confirm arrives from the modal's own Update cmd; drive it through
	// App.Update like bubbletea would.
	newModel, cmd := app.Update(modals.ProjectConfirmedMsg{SessionID: "sess-1", Accepted: true})
	app = newModel.(*App)
	require.NotNil(t, cmd, "accept must produce the project.set command")

	msg := cmd()
	setResult, ok := msg.(SetProjectResultMsg)
	require.True(t, ok, "accept must issue the path-based project.set RPC (got %T)", msg)
	// Disconnected test RPC client: the call fails after retries; the
	// contract under test is that accept ISSUES the RPC and surfaces the
	// result through SetProjectResultMsg.
	assert.Error(t, setResult.Err, "a disconnected daemon must surface the RPC failure, not silently pass")
	assert.Equal(t, ModalNone, app.activeModal)
}

func TestApp_Update_DismissedMsg_ClosesModal(t *testing.T) {
	app := newProjectPromptTestApp()
	newModel, _ := app.Update(modals.OpenProjectPromptMsg{SessionID: "sess-1", DefaultPath: "/p"})
	app = newModel.(*App)
	require.Equal(t, ModalProjectPrompt, app.activeModal)

	newModel, _ = app.Update(modals.ProjectDismissedMsg{SessionID: "sess-1"})
	app = newModel.(*App)
	assert.Equal(t, ModalNone, app.activeModal, "dismiss must close the modal")
}

func TestApp_Update_EscThroughModalKey_DismissesNotQuits(t *testing.T) {
	app := newProjectPromptTestApp()
	newModel, _ := app.Update(modals.OpenProjectPromptMsg{SessionID: "sess-1", DefaultPath: "/p"})
	app = newModel.(*App)

	// Send esc through the modal-key dispatch (handleModalKey path).
	newModel, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	app = newModel.(*App)

	assert.Equal(t, ModalNone, app.activeModal)
	assert.False(t, app.projectPrompt.IsVisible())
	if cmd != nil {
		msg := cmd()
		_, dismissed := msg.(modals.ProjectDismissedMsg)
		assert.True(t, dismissed, "esc must dismiss (ProjectDismissedMsg), never quit the program")
	}
}

func TestApp_SessionSwitch_TriggersPromptForUnboundSession(t *testing.T) {
	app := newProjectPromptTestApp()

	unbound := &types.Session{ID: "sess-unbound", Name: "unbound"}
	newModel, _ := app.Update(SessionSwitchMsg{Session: unbound})
	app = newModel.(*App)

	// The switch itself lands; the prompt is delivered as a follow-up cmd
	// (tea.Batch with the status-clear tick).
	newModel, cmd := app.Update(SessionSwitchMsg{Session: unbound})
	app = newModel.(*App)
	require.NotNil(t, cmd, "an unbound session switch must emit the prompt command")
	var openMsg modals.OpenProjectPromptMsg
	var found bool
	drainCmd(cmd, func(m tea.Msg) bool {
		if o, ok := m.(modals.OpenProjectPromptMsg); ok {
			openMsg = o
			found = true
			return true
		}
		return false
	})
	require.True(t, found, "expected OpenProjectPromptMsg inside the batch")
	assert.Equal(t, "sess-unbound", openMsg.SessionID)
	assert.Equal(t, "(no project detected)", openMsg.DefaultPath)

	// And the bound fast path stays silent.
	bound := &types.Session{ID: "sess-bound", ProjectID: "p1"}
	newModel, cmd = app.Update(SessionSwitchMsg{Session: bound})
	_ = newModel
	_ = app
	if cmd != nil {
		if _, isOpen := cmd().(modals.OpenProjectPromptMsg); isOpen {
			t.Fatal("a bound session must not trigger the project prompt")
		}
	}
}

// drainCmd flattens a tea.Cmd (including tea.Batch) and feeds every produced
// msg to visit until one returns true or the batch is exhausted. Test helper
// for asserting inside BatchMsg commands.
func drainCmd(cmd tea.Cmd, visit func(tea.Msg) bool) {
	if cmd == nil {
		return
	}
	msg := cmd()
	switch m := msg.(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, c := range m {
			drainCmd(c, visit)
		}
	default:
		visit(msg)
	}
}

func TestSessionNeedsProjectPrompt_ParityWithGUI(t *testing.T) {
	// Mirrors SessionProjectChecker.needsProjectPrompt: any binding suppresses.
	assert.False(t, sessionNeedsProjectPrompt(&types.Session{ProjectID: "p"}))
	assert.False(t, sessionNeedsProjectPrompt(&types.Session{ProjectPath: "/p"}))
	assert.False(t, sessionNeedsProjectPrompt(&types.Session{
		DetectionContext: &types.DetectionContext{CWD: "/cwd"},
	}))
	assert.True(t, sessionNeedsProjectPrompt(&types.Session{ID: "s"}))
	assert.False(t, sessionNeedsProjectPrompt(nil))
}

// ---------------------------------------------------------------------------
// M1/A6-3 end-to-end pin: a placeholder session (no detection context) that
// presses Enter must NEVER issue the project.set RPC — the daemon's
// CreateOrResolve would mkdir + git-init "(no project detected)/" and mark it
// active. The modal arms No (decline) instead of Yes, so Enter takes the skip
// path; the App's placeholder guard is the last line if a confirm arrives
// anyway.
// ---------------------------------------------------------------------------

func TestApp_ProjectPrompt_PlaceholderEnterIssuesNoProjectSet(t *testing.T) {
	app := newProjectPromptTestApp()

	// Open the prompt exactly as the session-switch wiring does for a
	// session with no detection context: the placeholder path.
	newModel, _ := app.Update(modals.OpenProjectPromptMsg{
		SessionID:   "sess-placeholder",
		DefaultPath: modals.NoProjectPlaceholder,
	})
	app = newModel.(*App)
	require.Equal(t, ModalProjectPrompt, app.activeModal)

	// Enter through the real key dispatch (handleModalKey -> modal.Update).
	// Pre-fix: Enter on the pre-selected Yes emitted Accepted=true and the
	// App fired rpc.SetProjectPath with the literal placeholder.
	newModel, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	app = newModel.(*App)

	if cmd != nil {
		drainCmd(cmd, func(m tea.Msg) bool {
			if pc, ok := m.(modals.ProjectConfirmedMsg); ok && pc.Accepted {
				t.Fatal("Enter on the placeholder must never confirm acceptance")
			}
			return false
		})
		// A decline confirm is the skip path: it must NOT produce the
		// project.set command — drive it through App.Update and assert.
		if pc, ok := cmd().(modals.ProjectConfirmedMsg); ok {
			require.False(t, pc.Accepted, "placeholder Enter must decline (skip path)")
			_, inner := app.Update(pc)
			if inner != nil {
				im := inner()
				if _, isSet := im.(SetProjectResultMsg); isSet {
					t.Fatal("declined placeholder confirm must not issue project.set")
				}
			}
		}
	}

	// App-level guard: even a SYNTHETIC Accepted=true with the placeholder
	// armed (e.g. a future regression bypassing the modal gate) must not
	// reach the RPC — no SetProjectResultMsg command may be produced.
	app2 := newProjectPromptTestApp()
	_, _ = app2.Update(modals.OpenProjectPromptMsg{
		SessionID:   "sess-placeholder",
		DefaultPath: modals.NoProjectPlaceholder,
	})
	_, cmd = app2.Update(modals.ProjectConfirmedMsg{SessionID: "sess-placeholder", Accepted: true})
	if cmd != nil {
		m := cmd()
		if _, isSet := m.(SetProjectResultMsg); isSet {
			t.Fatal("App must refuse project.set with the placeholder path")
		}
	}
}

func TestApp_ProjectPrompt_RealPathEnterStillBinds(t *testing.T) {
	// Non-vacuity: with a real detected cwd, Enter keeps the pre-existing
	// accept path (issues project.set) — the guard only blocks the
	// placeholder.
	app := newProjectPromptTestApp()
	newModel, _ := app.Update(modals.OpenProjectPromptMsg{
		SessionID:   "sess-real",
		DefaultPath: "/tmp/real-proj",
	})
	app = newModel.(*App)
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd, "real-path Enter must still confirm")
	pc, ok := cmd().(modals.ProjectConfirmedMsg)
	require.True(t, ok)
	require.True(t, pc.Accepted, "real cwd Enter must accept (parity with GUI)")
	_, inner := app.Update(pc)
	require.NotNil(t, inner, "accept must produce the project.set command")
	if _, isSet := inner().(SetProjectResultMsg); !isSet {
		t.Fatal("real accept must issue the path-based project.set RPC")
	}
}
