package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/modals"
	"github.com/caimlas/meept/internal/tui/types"
)

// Project-prompt wiring (TUI/GUI parity): the Flutter GUI prompts for a
// project binding when a session carries none (SessionProjectChecker in
// ui/flutter_ui/lib/providers/session_project_provider.dart). These helpers
// mirror that check and the dialog's default-path label for the TUI modal.

// sessionNeedsProjectPrompt mirrors SessionProjectChecker.needsProjectPrompt:
// a session needs the prompt only when it has NO project binding of any kind
// (project id, project path, or a detected client CWD).
func sessionNeedsProjectPrompt(sess *types.Session) bool {
	if sess == nil {
		return false
	}
	if sess.ProjectPath != "" || sess.ProjectID != "" {
		return false
	}
	if sess.DetectionContext != nil && sess.DetectionContext.CWD != "" {
		return false
	}
	return true
}

// projectPromptDefaultPath renders the dialog's default-path line exactly
// like the GUI: the detected CWD, or the explicit "(no project detected)"
// placeholder when the session carries no detection context.
func projectPromptDefaultPath(sess *types.Session) string {
	if sess != nil && sess.DetectionContext != nil && sess.DetectionContext.CWD != "" {
		return sess.DetectionContext.CWD
	}
	return "(no project detected)"
}

// maybePromptProject returns an OpenProjectPromptMsg command when the session
// needs a project binding, nil otherwise. Called on session switch so the
// parity behavior matches the GUI's _activateSession fast path.
func maybePromptProject(sess *types.Session) tea.Cmd {
	if !sessionNeedsProjectPrompt(sess) {
		return nil
	}
	sessionID := ""
	if sess != nil {
		sessionID = sess.ID
	}
	return func() tea.Msg {
		return modals.OpenProjectPromptMsg{
			SessionID:   sessionID,
			DefaultPath: projectPromptDefaultPath(sess),
		}
	}
}
