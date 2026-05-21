package menus

import (
	"fmt"

	"github.com/caimlas/meept/internal/transport"
	"github.com/caimlas/meept/internal/tui/types"
	"github.com/nsf/termbox-go"
)

// TasksMenu provides task viewing menu functionality.
type TasksMenu struct {
	modal     *Modal
	client    transport.Client
	tasks     []types.TaskExtended
	onSelect  func(*types.Task)
	onDismiss func()
}

// NewTasksMenu creates a new tasks menu.
func NewTasksMenu(client transport.Client) *TasksMenu {
	m := &TasksMenu{client: client}

	items := []ModalItem{
		{Key: "a", Label: "all tasks", Hint: "show all"},
		{Key: "p", Label: "pending only", Hint: "filter pending"},
		{Key: "r", Label: "running only", Hint: "filter running"},
		{Key: "c", Label: "completed only", Hint: "filter completed"},
	}

	m.modal = NewModal("tasks", items)
	m.modal.onSelect = m.handleSelect

	return m
}

// Show displays the tasks menu and loads tasks.
func (m *TasksMenu) Show() {
	// Load tasks
	resp, err := m.client.ListTasksExtended()
	if err == nil {
		m.tasks = resp.Tasks

		// Update modal with task list
		items := []ModalItem{
			{Key: "a", Label: "all tasks", Hint: fmt.Sprintf("%d total", len(m.tasks))},
		}

		pending := 0
		running := 0
		completed := 0

		for i, t := range m.tasks {
			if i >= 9 {
				break
			}
			key := fmt.Sprintf("%d", i+1)
			state := t.State
			name := t.Name
			if name == "" {
				name = t.Description
			}
			if len(name) > 30 {
				name = name[:27] + "..."
			}

			switch state {
			case "pending":
				pending++
			case "running":
				running++
			case "completed":
				completed++
			}

			items = append(items, ModalItem{
				Key:   key,
				Label: fmt.Sprintf("%s (%s)", name, state),
				Hint:  fmt.Sprintf("%d/%d", t.CompletedJobs, t.TotalJobs),
			})
		}

		// Add filter options
		items = append(items,
			ModalItem{Key: "p", Label: "pending", Hint: fmt.Sprintf("%d", pending)},
			ModalItem{Key: "r", Label: "running", Hint: fmt.Sprintf("%d", running)},
			ModalItem{Key: "c", Label: "completed", Hint: fmt.Sprintf("%d", completed)},
		)

		m.modal.items = items
		m.modal.height = len(items) + 4
	}

	m.modal.Show()
}

// Hide dismisses the tasks menu.
func (m *TasksMenu) Hide() {
	m.modal.Hide()
}

// IsVisible returns whether the menu is visible.
func (m *TasksMenu) IsVisible() bool {
	return m.modal.IsVisible()
}

// HandleKey processes keyboard input.
func (m *TasksMenu) HandleKey(ch rune, key termbox.Key) bool {
	return m.modal.HandleKey(ch, key)
}

// Render draws the menu.
func (m *TasksMenu) Render() {
	m.modal.Render()
}

func (m *TasksMenu) handleSelect(idx int) {
	if idx >= len(m.modal.items) {
		return
	}

	item := m.modal.items[idx]

	switch item.Key {
	case "a", "p", "r", "c":
		// Filter options - just dismiss, filtering would be done elsewhere
		m.modal.Hide()

	default:
		// Task selection
		for i, t := range m.tasks {
			if fmt.Sprintf("%d", i+1) == item.Key {
				if m.onSelect != nil {
					m.onSelect(&t.Task)
				}
				m.modal.Hide()
				return
			}
		}
	}
}

// SetCallbacks sets the callback functions.
func (m *TasksMenu) SetCallbacks(onSelect func(*types.Task), onDismiss func()) {
	m.onSelect = onSelect
	m.onDismiss = onDismiss
}
