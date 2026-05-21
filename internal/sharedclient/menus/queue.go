package menus

import (
	"fmt"

	"github.com/caimlas/meept/internal/transport"
	"github.com/caimlas/meept/internal/tui/types"
	"github.com/nsf/termbox-go"
)

// QueueMenu provides job queue viewing menu functionality.
type QueueMenu struct {
	modal     *Modal
	client    transport.Client
	jobs      []types.QueueJob
	onSelect  func(*types.QueueJob)
	onDismiss func()
}

// NewQueueMenu creates a new queue menu.
func NewQueueMenu(client transport.Client) *QueueMenu {
	m := &QueueMenu{client: client}

	items := []ModalItem{
		{Key: "p", Label: "pending jobs", Hint: "queue"},
		{Key: "r", Label: "running jobs", Hint: "active"},
		{Key: "f", Label: "failed jobs", Hint: "errors"},
		{Key: "c", Label: "completed jobs", Hint: "done"},
	}

	m.modal = NewModal("queue", items)
	m.modal.onSelect = m.handleSelect

	return m
}

// Show displays the queue menu and loads jobs.
func (m *QueueMenu) Show() {
	// Load queue stats first
	stats, err := m.client.GetQueueStats()
	if err != nil {
		// Still show menu but without stats
		m.modal.Show()
		return
	}

	// Extract stats from ByState map
	pending := stats.ByState["pending"]
	running := stats.ByState["running"]
	failed := stats.ByState["failed"]
	completed := stats.ByState["completed"]
	total := pending + running + failed + completed

	// Update modal with stats
	m.modal.items = []ModalItem{
		{Key: "p", Label: "pending", Hint: fmt.Sprintf("%d", pending)},
		{Key: "r", Label: "running", Hint: fmt.Sprintf("%d", running)},
		{Key: "f", Label: "failed", Hint: fmt.Sprintf("%d", failed)},
		{Key: "c", Label: "completed", Hint: fmt.Sprintf("%d", completed)},
		{Key: "t", Label: "total", Hint: fmt.Sprintf("%d", total)},
	}

	// Load pending jobs for display
	resp, err := m.client.ListQueueJobs("pending", 10)
	if err == nil {
		m.jobs = resp.Jobs

		for i, job := range m.jobs {
			if i >= 5 {
				break
			}
			key := fmt.Sprintf("%d", i+1)
			name := job.Type
			if len(name) > 25 {
				name = name[:22] + "..."
			}

			m.modal.items = append(m.modal.items, ModalItem{
				Key:   key,
				Label: name,
				Hint:  fmt.Sprintf("pri:%d", job.Priority),
			})
		}
	}

	m.modal.height = len(m.modal.items) + 4
	m.modal.Show()
}

// Hide dismisses the queue menu.
func (m *QueueMenu) Hide() {
	m.modal.Hide()
}

// IsVisible returns whether the menu is visible.
func (m *QueueMenu) IsVisible() bool {
	return m.modal.IsVisible()
}

// HandleKey processes keyboard input.
func (m *QueueMenu) HandleKey(ch rune, key termbox.Key) bool {
	return m.modal.HandleKey(ch, key)
}

// Render draws the menu.
func (m *QueueMenu) Render() {
	m.modal.Render()
}

func (m *QueueMenu) handleSelect(idx int) {
	if idx >= len(m.modal.items) {
		return
	}

	item := m.modal.items[idx]

	// For now, just dismiss on select - actual filtering would be done elsewhere
	switch item.Key {
	case "p", "r", "f", "c", "t":
		// Filter selection
		m.modal.Hide()

	default:
		// Job selection
		for i := range m.jobs {
			if fmt.Sprintf("%d", i+1) == item.Key {
				if m.onSelect != nil {
					m.onSelect(&m.jobs[i])
				}
				m.modal.Hide()
				return
			}
		}
	}
}

// SetCallbacks sets the callback functions.
func (m *QueueMenu) SetCallbacks(onSelect func(*types.QueueJob), onDismiss func()) {
	m.onSelect = onSelect
	m.onDismiss = onDismiss
}
