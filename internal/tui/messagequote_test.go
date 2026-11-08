package tui

// F12 gate T4 (04-tool-surface/00T-tests.md): message-quotes (TUI headless).
// Given a selected chat message, triggering quote-select must put a
// `> ` blockquote line prefixed with a `[msg <id>]` reference into the input;
// the existing blockquote renderer (internal/tui/render/markdown.go:97, `\n> `)
// then displays it as a quote. E2E: n/a (presentational UI).
//
// RED until leaf 05-message-quotes lands: ChatMessage has no ID field and
// ChatModel has no quote affordance today.
//
// Expected surface pinned by this gate (the leaf conforms or the
// orchestrator runs the delta protocol):
//   - models.ChatMessage gains ID string (stable message identity)
//   - ChatModel gains QuoteSelected() (key: quote-select on the selected
//     message, selectedMsgIdx >= 0)

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tui/models"
)

func TestQuoteSelect_InsertsBlockquoteWithMsgRef(t *testing.T) {
	app := createTestApp()
	m := app.chat

	// Seed two messages; select the second one.
	m.AddMessage(models.ChatMessage{
		ID:      "msg-001",
		Role:    "user",
		Content: "first question",
	})
	m.AddMessage(models.ChatMessage{
		ID:      "msg-002",
		Role:    "assistant",
		Content: "an answer that spans\nmultiple lines",
	})
	m.SelectMessage(1) // selectedMsgIdx = 1

	m.QuoteSelected()

	got := m.InputValue()
	if !strings.Contains(got, "> ") {
		t.Errorf("input %q must contain a '> ' blockquote line", got)
	}
	if !strings.Contains(got, "[msg msg-002]") {
		t.Errorf("input %q must carry the [msg msg-002] reference", got)
	}
	// Every quoted content line must be blockquote-prefixed.
	for line := range strings.SplitSeq(got, "\n") {
		if strings.Contains(line, "answer") || strings.Contains(line, "multiple lines") {
			if !strings.HasPrefix(strings.TrimSpace(line), ">") {
				t.Errorf("quoted line %q is not blockquote-prefixed", line)
			}
		}
	}
}

func TestQuoteSelect_NoSelectionIsNoop(t *testing.T) {
	app := createTestApp()
	m := app.chat
	m.AddMessage(models.ChatMessage{ID: "msg-003", Role: "user", Content: "x"})
	// selectedMsgIdx == -1: quote-select must be a no-op, not a panic.
	m.QuoteSelected()
	if got := m.InputValue(); strings.Contains(got, "> ") {
		t.Errorf("quote-select without a selection modified the input: %q", got)
	}
}

func TestQuoteSelect_RendererDisplaysQuote(t *testing.T) {
	// The existing markdown blockquote renderer must display the quoted
	// input as a quote (render/markdown.go:97 detects "\n> ").
	app := createTestApp()
	m := app.chat
	m.AddMessage(models.ChatMessage{ID: "msg-004", Role: "user", Content: "quoted text"})
	m.SelectMessage(0)
	m.QuoteSelected()

	rendered := m.RenderInputPreview()
	if !strings.Contains(rendered, "quoted text") {
		t.Errorf("rendered input %q lost the quoted content", rendered)
	}
}
