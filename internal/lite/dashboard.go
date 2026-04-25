// Package lite provides the lightweight TUI components for meept-lite.
package lite

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/caimlas/meept/internal/tui"
)

// DashboardRefreshTick signals time for dashboard data refresh.
type DashboardRefreshTick struct{}

// DashboardData holds the data displayed in the dashboard.
type DashboardData struct {
	ActiveAgents []string // Agent IDs currently active
	PendingTasks int      // Number of pending tasks
	WorkersBusy  int      // Number of busy workers
	WorkersTotal int      // Total number of workers
	MemoryCount  int      // Number of recent memories
}

// Dashboard is a compact 1-line status bar showing background activity.
type Dashboard struct {
	rpc    *tui.RPCClient
	width  int
	data   DashboardData
	err    error
	styles *DashboardStyles
}

// DashboardStyles defines styles for the dashboard.
type DashboardStyles struct {
	Container lipgloss.Style
	Label     lipgloss.Style
	Value     lipgloss.Style
	Separator lipgloss.Style
	Error     lipgloss.Style
}

// NewDashboard creates a new dashboard component.
func NewDashboard(rpc *tui.RPCClient) *Dashboard {
	styles := &DashboardStyles{
		Container: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF")).
			Background(lipgloss.Color("#1F2937")).
			Padding(0, 1),
		Label: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")),
		Value: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E5E7EB")).
			Bold(true),
		Separator: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4B5563")),
		Error: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF4444")),
	}

	return &Dashboard{
		rpc:    rpc,
		width:  80,
		styles: styles,
	}
}

// SetSize updates the dashboard width.
func (d *Dashboard) SetSize(width int) {
	d.width = width
}

// Init initializes the dashboard with periodic refresh.
func (d *Dashboard) Init() tea.Cmd {
	return tea.Batch(d.refreshData(), d.scheduleRefresh())
}

// scheduleRefresh schedules the next periodic refresh.
func (d *Dashboard) scheduleRefresh() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return DashboardRefreshTick{}
	})
}

// DashboardDataMsg carries refreshed dashboard data.
type DashboardDataMsg struct {
	Data DashboardData
	Err  error
}

// refreshData fetches data from the daemon.
func (d *Dashboard) refreshData() tea.Cmd {
	return func() tea.Msg {
		if d.rpc == nil || !d.rpc.IsConnected() {
			return DashboardDataMsg{Err: fmt.Errorf("not connected")}
		}

		data := DashboardData{}

		// Fetch workers
		if poolResp, err := d.rpc.ListPoolWorkers(); err == nil {
			data.WorkersTotal = len(poolResp.Workers)
			for _, w := range poolResp.Workers {
				if w.State == "processing" || w.State == "claiming" {
					data.WorkersBusy++
				}
			}
		} else if workersResp, err := d.rpc.ListWorkers(); err == nil {
			// Fallback to agent workers
			data.WorkersTotal = workersResp.Count
			for _, w := range workersResp.Workers {
				if w.State == "processing" || w.State == "executing_tool" {
					data.WorkersBusy++
					data.ActiveAgents = append(data.ActiveAgents, w.ID)
				}
			}
		}

		// Fetch pending tasks
		if taskResp, err := d.rpc.ListTasks("pending", 100); err == nil {
			data.PendingTasks = len(taskResp.Tasks)
		}

		// Also count executing tasks
		if taskResp, err := d.rpc.ListTasks("executing", 100); err == nil {
			data.PendingTasks += len(taskResp.Tasks)
		}

		// Fetch recent memories
		if memResp, err := d.rpc.GetRecentMemories(1); err == nil {
			items := memResp.GetItems()
			data.MemoryCount = len(items)
			// For actual count, we'd need a separate "count" RPC
			// For now, use recent count as indicator
		}

		return DashboardDataMsg{Data: data}
	}
}

// Update handles messages for the dashboard.
func (d *Dashboard) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case DashboardRefreshTick:
		return tea.Batch(d.refreshData(), d.scheduleRefresh())

	case DashboardDataMsg:
		if msg.Err != nil {
			d.err = msg.Err
		} else {
			d.err = nil
			d.data = msg.Data
		}
		return nil
	}
	return nil
}

// View renders the dashboard as a single line.
func (d *Dashboard) View() string {
	if d.err != nil {
		return d.styles.Container.Width(d.width).Render(
			d.styles.Error.Render("dashboard: disconnected"),
		)
	}

	// Build segments based on available width
	segments := d.buildSegments()

	// Join with separators
	sep := d.styles.Separator.Render(" | ")
	content := strings.Join(segments, sep)

	return d.styles.Container.Width(d.width).Render(content)
}

// buildSegments creates the display segments based on width.
func (d *Dashboard) buildSegments() []string {
	var segments []string

	// Format depends on width: full (>60), compact (40-60), minimal (<40)
	if d.width >= 60 {
		// Full format
		segments = d.buildFullSegments()
	} else if d.width >= 40 {
		// Compact format
		segments = d.buildCompactSegments()
	} else {
		// Minimal format
		segments = d.buildMinimalSegments()
	}

	return segments
}

// buildFullSegments creates full-width display segments.
func (d *Dashboard) buildFullSegments() []string {
	var segments []string

	// Agents
	if len(d.data.ActiveAgents) > 0 {
		agentList := strings.Join(d.data.ActiveAgents, ", ")
		if len(agentList) > 20 {
			agentList = agentList[:17] + "..."
		}
		segments = append(segments, fmt.Sprintf("%s %s (%s)",
			d.styles.Label.Render("agents:"),
			d.styles.Value.Render(fmt.Sprintf("%d", len(d.data.ActiveAgents))),
			agentList,
		))
	} else {
		segments = append(segments, fmt.Sprintf("%s %s",
			d.styles.Label.Render("agents:"),
			d.styles.Value.Render("0"),
		))
	}

	// Tasks
	segments = append(segments, fmt.Sprintf("%s %s",
		d.styles.Label.Render("tasks:"),
		d.styles.Value.Render(fmt.Sprintf("%d", d.data.PendingTasks)),
	))

	// Workers
	segments = append(segments, fmt.Sprintf("%s %s",
		d.styles.Label.Render("workers:"),
		d.styles.Value.Render(fmt.Sprintf("%d/%d", d.data.WorkersBusy, d.data.WorkersTotal)),
	))

	// Memory
	segments = append(segments, fmt.Sprintf("%s %s",
		d.styles.Label.Render("mem:"),
		d.styles.Value.Render(fmt.Sprintf("%d", d.data.MemoryCount)),
	))

	return segments
}

// buildCompactSegments creates compact display segments.
func (d *Dashboard) buildCompactSegments() []string {
	var segments []string

	// Agents (just count)
	segments = append(segments, fmt.Sprintf("a:%s",
		d.styles.Value.Render(fmt.Sprintf("%d", len(d.data.ActiveAgents))),
	))

	// Tasks
	segments = append(segments, fmt.Sprintf("t:%s",
		d.styles.Value.Render(fmt.Sprintf("%d", d.data.PendingTasks)),
	))

	// Workers
	segments = append(segments, fmt.Sprintf("w:%s",
		d.styles.Value.Render(fmt.Sprintf("%d/%d", d.data.WorkersBusy, d.data.WorkersTotal)),
	))

	return segments
}

// buildMinimalSegments creates minimal display segments.
func (d *Dashboard) buildMinimalSegments() []string {
	// Super compact: just show busy workers and tasks
	return []string{
		fmt.Sprintf("%d/%d w", d.data.WorkersBusy, d.data.WorkersTotal),
		fmt.Sprintf("%d t", d.data.PendingTasks),
	}
}

// Height returns the height of the dashboard (always 1).
func (d *Dashboard) Height() int {
	return 1
}
