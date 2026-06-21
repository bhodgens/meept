// Package models provides the view models for the TUI.
package models

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/types"
	"github.com/caimlas/meept/internal/tui/viz"
)

// threadIndicatorState holds the minimal state needed for the inline thread
// indicator rendered at the top of the chat view. The full interactive
// ThreadIndicator component lives in the parent tui package
// (internal/tui/thread_indicator.go) and cannot be imported here due to an
// import cycle (tui imports tui/models). This struct mirrors just the data
// needed for display; key-handling and list-toggle are intentionally not
// wired at this layer (see plan Task 8 Step 2: minimal integration).
type threadIndicatorState struct {
	threads      []types.Thread
	activeIndex  int
	active       lipgloss.Style
	inactive     lipgloss.Style
	topicLabel   lipgloss.Style
	muted        lipgloss.Style
	indicator    lipgloss.Style
	container    lipgloss.Style
}

// newThreadIndicatorState returns a threadIndicatorState with default styles.
func newThreadIndicatorState() *threadIndicatorState {
	return &threadIndicatorState{
		threads:     []types.Thread{},
		activeIndex: -1,
		active: lipgloss.NewStyle().
			Foreground(viz.ColorCarrying).
			Bold(true),
		inactive: lipgloss.NewStyle().
			Foreground(viz.ColorMuted),
		topicLabel: lipgloss.NewStyle().
			Foreground(viz.ColorWorking),
		muted: lipgloss.NewStyle().
			Foreground(viz.ColorMuted).
			Italic(true),
		indicator: lipgloss.NewStyle().
			Foreground(viz.ColorSuccess),
		container: lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1),
	}
}

// SetSession updates the thread indicator state from a session.
// Safe to call with nil; clears the thread list.
func (t *threadIndicatorState) SetSession(session *types.Session) {
	if t == nil {
		return
	}
	if session == nil || len(session.Threads) == 0 {
		t.threads = []types.Thread{}
		t.activeIndex = -1
		return
	}

	t.threads = make([]types.Thread, 0, len(session.Threads))
	for _, th := range session.Threads {
		t.threads = append(t.threads, th)
	}
	t.activeIndex = -1
	for i, th := range t.threads {
		if th.ID == session.ActiveThreadID {
			t.activeIndex = i
			break
		}
	}
	if t.activeIndex < 0 {
		t.activeIndex = 0 // default to first if no match
	}
}

// View renders the thread indicator as a single-line compact view.
// Returns "" when no threads are present, so the parent View() can
// unconditionally prepend the output.
func (t *threadIndicatorState) View() string {
	if t == nil || len(t.threads) == 0 {
		return ""
	}

	idx := t.activeIndex
	if idx < 0 || idx >= len(t.threads) {
		idx = 0
	}
	current := t.threads[idx]
	label := current.TopicLabel
	if strings.TrimSpace(label) == "" {
		label = "general"
	}

	var b strings.Builder
	b.WriteString(t.indicator.Render(" \u25cf ")) // bullet
	b.WriteString(t.topicLabel.Render("thread: " + label))

	if n := len(t.threads); n > 1 {
		countStr := "9+"
		if n <= 9 {
			countStr = string(rune('0' + n))
		}
		b.WriteString(t.muted.Render(" (" + countStr + " threads)"))
	}

	return t.container.Render(b.String())
}

// IsActive returns true when the indicator has threads to display.
func (t *threadIndicatorState) IsActive() bool {
	return t != nil && len(t.threads) > 0
}

// ThreadCount returns the number of threads currently tracked.
func (t *threadIndicatorState) ThreadCount() int {
	if t == nil {
		return 0
	}
	return len(t.threads)
}

// ActiveThreadID returns the ID of the currently-selected thread, or "" if none.
func (t *threadIndicatorState) ActiveThreadID() string {
	if t == nil || t.activeIndex < 0 || t.activeIndex >= len(t.threads) {
		return ""
	}
	return t.threads[t.activeIndex].ID
}
