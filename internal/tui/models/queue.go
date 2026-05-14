package models

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// QueueModel is the model for the job queue view.
type QueueModel struct {
	rpc         QueueRPCClient
	jobs        []types.QueueJob
	stats       *types.QueueStatsResponse
	table       table.Model
	selectedJob *types.QueueJob
	width       int
	height      int
	loading     bool
	err         error
	showingHelp bool
	filterState string // Filter by job state
}

// QueueRPCClient interface for the queue model.
type QueueRPCClient interface {
	GetQueueStats() (*types.QueueStatsResponse, error)
	ListQueueJobs(state string, limit int) (*types.QueueJobListResponse, error)
	RetryQueueJob(jobID string) error
	IsConnected() bool
}

// NewQueueModel creates a new queue model.
func NewQueueModel(rpc QueueRPCClient) *QueueModel {
	columns := []table.Column{
		{Title: "ID", Width: 20},
		{Title: "Type", Width: 12},
		{Title: "Priority", Width: 10},
		{Title: "State", Width: 12},
		{Title: "Task", Width: 20},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color("#06B6D4"))

	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#06B6D4")).
		Bold(true)

	t.SetStyles(s)

	return &QueueModel{
		rpc:         rpc,
		table:       t,
		filterState: "pending", // Default to pending jobs
	}
}

// SetSize updates the model dimensions.
func (m *QueueModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Update table dimensions
	tableHeight := max(
		// Account for stats panel, detail panel and padding
		height-16, 5)
	m.table.SetHeight(tableHeight)

	// Update column widths based on available space
	remaining := width - 54 // ID(20) + type(12) + priority(10) + state(12)
	taskWidth := max(remaining, 10)
	m.table.SetColumns([]table.Column{
		{Title: "ID", Width: 20},
		{Title: "Type", Width: 12},
		{Title: "Priority", Width: 10},
		{Title: "State", Width: 12},
		{Title: "Task", Width: taskWidth},
	})
}

// QueueUpdateMsg carries the queue data update.
type QueueUpdateMsg struct {
	Stats *types.QueueStatsResponse
	Jobs  []types.QueueJob
	Err   error
}

// Init initializes the queue model.
func (m *QueueModel) Init() tea.Cmd {
	return m.fetchQueueData
}

func (m *QueueModel) fetchQueueData() tea.Msg {
	var jobs []types.QueueJob

	// Get queue stats
	stats, err := m.rpc.GetQueueStats()
	if err != nil {
		return QueueUpdateMsg{Err: err}
	}

	// Get jobs list
	jobsResp, err := m.rpc.ListQueueJobs(m.filterState, 50)
	if err != nil {
		return QueueUpdateMsg{Stats: stats, Err: err}
	}
	jobs = jobsResp.Jobs

	return QueueUpdateMsg{Stats: stats, Jobs: jobs}
}

// Update handles messages for the queue view.
func (m *QueueModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case QueueUpdateMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return nil
		}
		m.err = nil
		m.stats = msg.Stats
		m.jobs = msg.Jobs
		m.updateTable()
		return nil

	case tea.KeyPressMsg:
		if m.showingHelp {
			m.showingHelp = false
			return nil
		}

		switch msg.String() {
		case "r":
			// Refresh
			m.loading = true
			return m.fetchQueueData

		case "?":
			m.showingHelp = true
			return nil

		case "enter":
			// Select job for detail view
			if len(m.jobs) > 0 {
				idx := m.table.Cursor()
				if idx >= 0 && idx < len(m.jobs) {
					m.selectedJob = &m.jobs[idx]
				}
			}
			return nil

		case "esc":
			m.selectedJob = nil
			return nil

		case "R":
			// Retry selected job
			if m.selectedJob != nil {
				jobID := m.selectedJob.ID
				return func() tea.Msg {
					err := m.rpc.RetryQueueJob(jobID)
					if err != nil {
						return QueueUpdateMsg{Err: err}
					}
					// Refresh after retry
					return m.fetchQueueData()
				}
			}
			return nil

		case "p":
			// Filter to pending
			m.filterState = "pending"
			m.loading = true
			return m.fetchQueueData

		case "f":
			// Filter to failed
			m.filterState = "failed"
			m.loading = true
			return m.fetchQueueData

		case "c":
			// Filter to completed
			m.filterState = "completed"
			m.loading = true
			return m.fetchQueueData

		case "a":
			// Show all (no filter)
			m.filterState = ""
			m.loading = true
			return m.fetchQueueData

		case "up", "down", "j", "k":
			// Let table handle navigation
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			// Update selected job as we navigate
			if len(m.jobs) > 0 {
				idx := m.table.Cursor()
				if idx >= 0 && idx < len(m.jobs) {
					m.selectedJob = &m.jobs[idx]
				}
			}
			return cmd
		}
	}

	// Pass other messages to table
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return cmd
}

func (m *QueueModel) updateTable() {
	rows := make([]table.Row, len(m.jobs))

	priorityNames := map[int]string{
		1: "low",
		2: "normal",
		3: "high",
		4: "urgent",
	}

	for i, job := range m.jobs {
		priority := priorityNames[job.Priority]
		if priority == "" {
			priority = fmt.Sprintf("%d", job.Priority)
		}

		taskID := job.TaskID
		if taskID == "" {
			taskID = "-"
		}

		id := job.ID
		if len(id) > 20 {
			id = id[:17] + "..."
		}

		rows[i] = table.Row{
			id,
			types.TruncateString(job.Type, 12),
			priority,
			job.State,
			types.TruncateString(taskID, 20),
		}
	}
	m.table.SetRows(rows)
}

// View renders the queue view.
func (m *QueueModel) View() string {
	if m.showingHelp {
		return m.renderHelp()
	}

	if m.loading && len(m.jobs) == 0 && m.stats == nil {
		return m.renderLoading()
	}

	if m.err != nil && len(m.jobs) == 0 {
		return m.renderError()
	}

	var b strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#06B6D4")).
		MarginBottom(1)

	b.WriteString(titleStyle.Render("Job Queue"))
	b.WriteString("\n\n")

	// Stats panel
	b.WriteString(m.renderStatsPanel())
	b.WriteString("\n")

	// Filter indicator
	filterStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280"))
	filterLabel := "All Jobs"
	if m.filterState != "" {
		filterLabel = fmt.Sprintf("Filter: %s", m.filterState)
	}
	b.WriteString(filterStyle.Render(filterLabel))
	b.WriteString("\n\n")

	// Table
	tableStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151"))

	b.WriteString(tableStyle.Render(m.table.View()))
	b.WriteString("\n")

	// Detail panel
	if m.selectedJob != nil {
		b.WriteString(m.renderJobDetail())
	}

	// Help hint
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		MarginTop(1)

	b.WriteString(hintStyle.Render("r: refresh | p/f/c/a: filter | R: retry | ?: help"))

	return b.String()
}

func (m *QueueModel) renderStatsPanel() string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(0, 2).
		Width(m.width - 4)

	if m.stats == nil {
		return panelStyle.Render(lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("Loading statistics..."))
	}

	// Build stats line
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280"))

	pendingStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F59E0B")).
		Bold(true)

	processingStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#06B6D4")).
		Bold(true)

	completedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#10B981")).
		Bold(true)

	failedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#EF4444")).
		Bold(true)

	var parts []string

	pending := m.stats.ByState["pending"]
	processing := m.stats.ByState["processing"]
	completed := m.stats.ByState["completed"]
	failed := m.stats.ByState["failed"]

	parts = append(parts, labelStyle.Render("Pending: ")+pendingStyle.Render(fmt.Sprintf("%d", pending)), labelStyle.Render("Processing: ")+processingStyle.Render(fmt.Sprintf("%d", processing)), labelStyle.Render("Completed: ")+completedStyle.Render(fmt.Sprintf("%d", completed)), labelStyle.Render("Failed: ")+failedStyle.Render(fmt.Sprintf("%d", failed)))

	if m.stats.DeadCount > 0 {
		deadStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#DC2626")).
			Bold(true)
		parts = append(parts, labelStyle.Render("Dead: ")+deadStyle.Render(fmt.Sprintf("%d", m.stats.DeadCount)))
	}

	return panelStyle.Render(strings.Join(parts, "  │  "))
}

func (m *QueueModel) renderLoading() string {
	style := lipgloss.NewStyle().
		Width(m.width-4).
		Align(lipgloss.Center).
		Padding(4, 0)

	return style.Render("Loading queue data...")
}

func (m *QueueModel) renderError() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#EF4444")).
		Padding(1, 2).
		Width(m.width - 4)

	return style.Render(
		lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true).Render("Error") +
			"\n\n" +
			fmt.Sprintf("%v", m.err) +
			"\n\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("Press 'r' to refresh"),
	)
}

func (m *QueueModel) renderJobDetail() string {
	job := m.selectedJob

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#06B6D4")).
		Padding(0, 2).
		Width(m.width - 4)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#06B6D4"))

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Width(14)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	// State color
	stateColor := "#6B7280"
	switch job.State {
	case "pending":
		stateColor = "#F59E0B"
	case "processing", "claimed":
		stateColor = "#06B6D4"
	case "completed":
		stateColor = "#10B981"
	case "failed":
		stateColor = "#EF4444"
	case "dead":
		stateColor = "#DC2626"
	}
	stateStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(stateColor)).
		Bold(true)

	priorityNames := map[int]string{
		1: "low",
		2: "normal",
		3: "high",
		4: "urgent",
	}
	priority := priorityNames[job.Priority]
	if priority == "" {
		priority = fmt.Sprintf("%d", job.Priority)
	}

	content := titleStyle.Render("Job Detail") + "\n"
	content += labelStyle.Render("ID:") + valueStyle.Render(job.ID) + "\n"
	content += labelStyle.Render("Type:") + valueStyle.Render(job.Type) + "\n"
	content += labelStyle.Render("State:") + stateStyle.Render(job.State) + "\n"
	content += labelStyle.Render("Priority:") + valueStyle.Render(priority) + "\n"

	if job.TaskID != "" {
		content += labelStyle.Render("Task:") + valueStyle.Render(job.TaskID) + "\n"
	}

	return panelStyle.Render(content)
}

func (m *QueueModel) renderHelp() string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#06B6D4")).
		Padding(2, 4).
		Width(m.width - 4)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#06B6D4")).
		MarginBottom(1)

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F59E0B")).
		Bold(true).
		Width(12)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	content := titleStyle.Render("Queue View Help") + "\n\n"
	content += keyStyle.Render("up/k") + descStyle.Render("Move cursor up") + "\n"
	content += keyStyle.Render("down/j") + descStyle.Render("Move cursor down") + "\n"
	content += keyStyle.Render("enter") + descStyle.Render("Select job for detail") + "\n"
	content += keyStyle.Render("esc") + descStyle.Render("Clear selection") + "\n"
	content += keyStyle.Render("r") + descStyle.Render("Refresh data") + "\n"
	content += keyStyle.Render("R") + descStyle.Render("Retry selected failed job") + "\n"
	content += "\n"
	content += titleStyle.Render("Filters") + "\n"
	content += keyStyle.Render("p") + descStyle.Render("Show pending jobs") + "\n"
	content += keyStyle.Render("f") + descStyle.Render("Show failed jobs") + "\n"
	content += keyStyle.Render("c") + descStyle.Render("Show completed jobs") + "\n"
	content += keyStyle.Render("a") + descStyle.Render("Show all jobs") + "\n"
	content += "\n"
	content += lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("Press any key to close")

	return panelStyle.Render(content)
}
