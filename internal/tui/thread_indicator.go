package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/types"
	"github.com/caimlas/meept/internal/tui/viz"
)

// ThreadIndicator displays the current thread and allows switching between threads.
// It works in two modes: compact (showing the active thread label) and list (showing all
// threads with selection). The indicator is designed as a lightweight component that
// reads from the current Session's thread data.
type ThreadIndicator struct {
	threads      []types.Thread
	currentIndex int
	showList     bool
	styles       threadStyles
}

type threadStyles struct {
	active     lipgloss.Style
	inactive   lipgloss.Style
	container  lipgloss.Style
	indicator  lipgloss.Style
	topicLabel lipgloss.Style
	muted      lipgloss.Style
}

func defaultThreadStyles() threadStyles {
	return threadStyles{
		active: lipgloss.NewStyle().
			Foreground(viz.ColorCarrying).
			Bold(true),
		inactive: lipgloss.NewStyle().
			Foreground(viz.ColorMuted),
		container: lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1),
		indicator: lipgloss.NewStyle().
			Foreground(viz.ColorSuccess),
		topicLabel: lipgloss.NewStyle().
			Foreground(viz.ColorWorking),
		muted: lipgloss.NewStyle().
			Foreground(viz.ColorMuted).
			Italic(true),
	}
}

// NewThreadIndicator creates a new thread indicator.
func NewThreadIndicator() *ThreadIndicator {
	return &ThreadIndicator{
		threads:      []types.Thread{},
		currentIndex: 0,
		styles:       defaultThreadStyles(),
	}
}

// Update handles messages for the thread indicator.
func (ti *ThreadIndicator) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.Code {
		case tea.KeyEnter:
			// Toggle list in compact mode, or confirm selection in list mode
			if ti.showList {
				if len(ti.threads) > 0 {
					return ti.switchThreadCmd(ti.threads[ti.currentIndex].ID)
				}
			}
			return nil
		case tea.KeyEsc:
			if ti.showList {
				ti.showList = false
				return nil
			}
		case tea.KeyUp, tea.KeyLeft:
			if ti.showList && len(ti.threads) > 0 {
				if ti.currentIndex > 0 {
					ti.currentIndex--
				}
				return nil
			}
		case tea.KeyDown, tea.KeyRight:
			if ti.showList && len(ti.threads) > 0 {
				if ti.currentIndex < len(ti.threads)-1 {
					ti.currentIndex++
				}
				return nil
			}
		default:
			if msg.Text == "q" && ti.showList {
				ti.showList = false
				return nil
			}
			if msg.Text == "t" || msg.Text == "T" {
				ti.showList = !ti.showList
				return nil
			}
		}
	}
	return nil
}

// View renders the thread indicator.
func (ti *ThreadIndicator) View() string {
	if len(ti.threads) == 0 {
		return ""
	}

	if ti.showList {
		return ti.viewList()
	}

	return ti.viewCompact()
}

// viewCompact renders the single-line thread indicator: "thread: work (3 threads)"
func (ti *ThreadIndicator) viewCompact() string {
	if len(ti.threads) == 0 {
		return ""
	}

	current := ti.threads[ti.currentIndex]
	label := current.TopicLabel
	if label == "" {
		label = "general"
	}

	var b strings.Builder
	b.WriteString(ti.styles.indicator.Render(" \u25cf "))  // bullet
	b.WriteString(ti.styles.topicLabel.Render("thread: " + label))

	if len(ti.threads) > 1 {
		countStr := string(rune('0' + len(ti.threads)))
		if len(ti.threads) > 9 {
			countStr = "9+"
		}
		b.WriteString(ti.styles.muted.Render(" (" + countStr + " threads)"))
	}

	return ti.styles.container.Render(b.String())
}

// viewList renders the expandable thread list.
func (ti *ThreadIndicator) viewList() string {
	var lines []string

	for i, t := range ti.threads {
		prefix := "  "
		style := ti.styles.inactive

		if i == ti.currentIndex {
			prefix = ti.styles.indicator.Render(" \u25cf ")
			style = ti.styles.active
		}

		line := prefix + "thread: " + style.Render(t.TopicLabel)
		lines = append(lines, line)
	}

	// Add hint
	lines = append(lines, "")
	lines = append(lines, ti.styles.muted.Render("enter: switch  t: toggle  esc: close"))

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// SetThreads updates the thread list from session data.
func (ti *ThreadIndicator) SetThreads(session *types.Session) {
	if session == nil || session.Threads == nil {
		ti.threads = []types.Thread{}
		ti.currentIndex = 0
		return
	}

	ti.threads = make([]types.Thread, 0, len(session.Threads))

	for _, t := range session.Threads {
		ti.threads = append(ti.threads, t)
	}

	// Find and select the active thread, defaulting to first
	ti.currentIndex = 0
	for i, t := range ti.threads {
		if t.ID == session.ActiveThreadID {
			ti.currentIndex = i
			break
		}
	}

	// Sort by last activity to show most-recent first
	sortThreadsByActivity(ti.threads)
}

// IsActive returns true if the indicator has any threads.
func (ti *ThreadIndicator) IsActive() bool {
	return len(ti.threads) > 0
}

// switchThreadCmd returns a tea.Cmd that sends a thread switch message.
func (ti *ThreadIndicator) switchThreadCmd(threadID string) tea.Cmd {
	return func() tea.Msg {
		return types.ThreadSwitchMsg{ThreadID: threadID}
	}
}

// sortThreadsByActivity sorts threads in-place, most recently active first.
func sortThreadsByActivity(threads []types.Thread) {
	slices.SortStableFunc(threads, func(a, b types.Thread) int {
		if a.LastActivityAt.Before(b.LastActivityAt) {
			return 1
		}
		if a.LastActivityAt.After(b.LastActivityAt) {
			return -1
		}
		return 0
	})
}

// updateFromData updates the thread indicator from a thread slice and active ID.
// Called when thread data is refreshed after a thread switch.
func (ti *ThreadIndicator) updateFromData(threads []types.Thread, activeID string) {
	ti.threads = make([]types.Thread, 0, len(threads))
	for _, t := range threads {
		ti.threads = append(ti.threads, t)
	}
	ti.currentIndex = 0
	for i, t := range ti.threads {
		if t.ID == activeID {
			ti.currentIndex = i
			break
		}
	}
	sortThreadsByActivity(ti.threads)
}
