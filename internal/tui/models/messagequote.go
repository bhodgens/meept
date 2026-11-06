// Message-quote affordance (dsh-convergence leaf 04-tool-surface/05).
// Presentational only: QuoteReference builds a `> ` blockquote prefixed with
// a `[msg <id>]` reference for insertion into the chat input; the markdown
// renderer (internal/tui/render/markdown.go, "\n> " detection) already
// displays such blocks as quotes. A quoted reply is an ordinary user
// message whose text happens to start with a `> ` block — no backend or
// persistence changes.
package models

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/caimlas/meept/pkg/id"
)

// QuoteLabel renders the stable reference label for a message id:
// "[msg <id>] ". The id is sanitized (newlines/control characters stripped,
// empty -> "<unknown>") so a hostile id cannot inject blockquote lines into
// the composed reply.
func QuoteLabel(msgID string) string {
	return "[msg " + sanitizeQuoteID(msgID) + "] "
}

// sanitizeQuoteID strips newlines, control characters, and blockquote
// markers from a message id so the reference stays a single safe line that
// the renderer cannot mistake for quote structure. An empty result yields
// "<unknown>".
func sanitizeQuoteID(msgID string) string {
	var b strings.Builder
	for _, r := range msgID {
		if r == '\n' || r == '\r' || r == '>' || unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	s := strings.TrimSpace(b.String())
	if s == "" {
		return "<unknown>"
	}
	return s
}

// QuoteReference returns a blockquote of text prefixed with the message
// reference label, separated from any prior input by a leading newline:
//
//	"\n> [msg msg-12] first\n> second"
//
// Every content line is blockquote-prefixed so the markdown renderer
// (internal/tui/render/markdown.go, "\n> ") displays the whole block as a
// quote. Empty text yields "\n> [msg <id>] ".
func QuoteReference(msgID, text string) string {
	label := QuoteLabel(msgID)
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	lines[0] = label + lines[0]
	var b strings.Builder
	b.WriteString("\n")
	for _, l := range lines {
		fmt.Fprintf(&b, "> %s\n", l)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// QuoteSelected inserts a blockquote reference of the currently selected
// chat message at the input cursor (key: "q" with viewport focus), keeping
// any existing input text. The selection clears and focus returns to the
// input. Without a selection it is a no-op. Returns true when a quote was
// inserted.
func (m *ChatModel) QuoteSelected() bool {
	if m.selectedMsgIdx < 0 || m.selectedMsgIdx >= len(m.messages) {
		return false
	}
	msg := m.messages[m.selectedMsgIdx]
	m.textarea.InsertString(QuoteReference(msg.ID, msg.Content))
	m.textarea.CursorEnd()
	m.selectedMsgIdx = -1
	m.SetFocus(FocusInput)
	m.updateViewport()
	return true
}

// SelectMessage selects the chat message at the given index for keyboard
// interaction (copy/quote). Out-of-range indices clear the selection.
func (m *ChatModel) SelectMessage(idx int) {
	if idx < 0 || idx >= len(m.messages) {
		m.selectedMsgIdx = -1
	} else {
		m.selectedMsgIdx = idx
	}
	m.updateViewport()
}

// AddMessage appends a message to the visible transcript, minting a stable
// quote-reference id for it.
func (m *ChatModel) AddMessage(msg ChatMessage) {
	if msg.ID == "" {
		msg.ID = id.Generate("msg-")
	}
	if msg.State == 0 {
		msg.State = MessageNormal
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	m.messages = append(m.messages, msg)
	if m.findBarVisible && m.findInput.Value() != "" {
		m.recomputeFindMatches()
	} else {
		m.updateViewport()
	}
}

// InputValue returns the current input text (test headless surface).
func (m *ChatModel) InputValue() string {
	return m.textarea.Value()
}

// RenderInputPreview renders the input area as the chat view would show it:
// the textarea view (multi-line content included). Used by headless tests to
// verify composed input, e.g. an inserted message quote.
func (m *ChatModel) RenderInputPreview() string {
	return m.textarea.View()
}
