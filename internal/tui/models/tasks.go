package models

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/caimlas/meept/internal/tui/types"
)

// TasksModel is the model for the tasks view.
type TasksModel struct {
	rpc          TasksRPCClient
	jobs         []types.Job
	table        table.Model
	selectedJob  *types.Job
	width        int
	height       int
	loading      bool
	err          error
	showingHelp  bool
}

// TasksRPCClient interface for the tasks model.
type TasksRPCClient interface {
	ListJobs() (*types.JobListResponse, error)
	IsConnected() bool
}

// NewTasksModel creates a new tasks model.
func NewTasksModel(rpc TasksRPCClient) *TasksModel {
	columns := []table.Column{
		{Title: "Name", Width: 20},
		{Title: "Schedule", Width: 20},
		{Title: "Next Run", Width: 20},
		{Title: "Status", Width: 10},
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
		Foreground(lipgloss.Color("#7C3AED"))

	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#7C3AED")).
		Bold(true)

	t.SetStyles(s)

	return &TasksModel{
		rpc:   rpc,
		table: t,
	}
}

// SetSize updates the model dimensions.
func (m *TasksModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Update table dimensions
	tableHeight := height - 12 // Account for detail panel and padding
	if tableHeight < 5 {
		tableHeight = 5
	}
	m.table.SetHeight(tableHeight)

	// Update column widths based on available space
	colWidth := (width - 20) / 4
	if colWidth < 10 {
		colWidth = 10
	}
	m.table.SetColumns([]table.Column{
		{Title: "Name", Width: colWidth},
		{Title: "Schedule", Width: colWidth},
		{Title: "Next Run", Width: colWidth},
		{Title: "Status", Width: 10},
	})
}

// JobsUpdateMsg carries the jobs update.
type JobsUpdateMsg struct {
	Jobs []types.Job
	Err  error
}

// Init initializes the tasks model.
func (m *TasksModel) Init() tea.Cmd {
	return m.fetchJobs
}

func (m *TasksModel) fetchJobs() tea.Msg {
	resp, err := m.rpc.ListJobs()
	if err != nil {
		return JobsUpdateMsg{Err: err}
	}
	return JobsUpdateMsg{Jobs: resp.Jobs}
}

// Update handles messages for the tasks view.
func (m *TasksModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case JobsUpdateMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return nil
		}
		m.err = nil
		m.jobs = msg.Jobs
		m.updateTable()
		return nil

	case tea.KeyMsg:
		if m.showingHelp {
			m.showingHelp = false
			return nil
		}

		switch msg.String() {
		case "r":
			// Refresh
			m.loading = true
			return m.fetchJobs

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

func (m *TasksModel) updateTable() {
	rows := make([]table.Row, len(m.jobs))
	for i, job := range m.jobs {
		status := "active"
		if job.Paused {
			status = "paused"
		}

		schedule := job.Schedule
		if schedule == "" {
			schedule = job.Trigger
		}
		if schedule == "" {
			schedule = "n/a"
		}

		nextRun := job.NextRunTime
		if nextRun == "" {
			nextRun = "n/a"
		}

		name := job.Name
		if name == "" {
			name = job.ID
		}

		rows[i] = table.Row{
			types.TruncateString(name, 20),
			types.TruncateString(schedule, 20),
			types.TruncateString(nextRun, 20),
			status,
		}
	}
	m.table.SetRows(rows)
}

// View renders the tasks view.
func (m *TasksModel) View() string {
	if m.showingHelp {
		return m.renderHelp()
	}

	if m.loading && len(m.jobs) == 0 {
		return m.renderLoading()
	}

	if m.err != nil && len(m.jobs) == 0 {
		return m.renderError()
	}

	var b strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED")).
		MarginBottom(1)

	b.WriteString(titleStyle.Render("Scheduled Jobs"))
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
	} else {
		b.WriteString(m.renderEmptyDetail())
	}

	// Help hint
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		MarginTop(1)

	b.WriteString(hintStyle.Render("r: refresh | enter: select | ?: help"))

	return b.String()
}

func (m *TasksModel) renderLoading() string {
	style := lipgloss.NewStyle().
		Width(m.width - 4).
		Align(lipgloss.Center).
		Padding(4, 0)

	return style.Render("Loading jobs...")
}

func (m *TasksModel) renderError() string {
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

func (m *TasksModel) renderJobDetail() string {
	job := m.selectedJob

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7C3AED")).
		Padding(1, 2).
		Width(m.width - 4)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED"))

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Width(14)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	statusColor := "#10B981" // Green
	if job.Paused {
		statusColor = "#F59E0B" // Amber
	}
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(statusColor)).
		Bold(true)

	status := "active"
	if job.Paused {
		status = "paused"
	}

	content := titleStyle.Render("Job Detail") + "\n\n"

	name := job.Name
	if name == "" {
		name = job.ID
	}
	content += labelStyle.Render("Name:") + valueStyle.Render(name) + "\n"
	content += labelStyle.Render("ID:") + valueStyle.Render(job.ID) + "\n"
	content += labelStyle.Render("Status:") + statusStyle.Render(status) + "\n"

	schedule := job.Schedule
	if schedule == "" {
		schedule = job.Trigger
	}
	if schedule == "" {
		schedule = "n/a"
	}
	content += labelStyle.Render("Schedule:") + valueStyle.Render(schedule) + "\n"

	nextRun := job.NextRunTime
	if nextRun == "" {
		nextRun = "n/a"
	}
	content += labelStyle.Render("Next Run:") + valueStyle.Render(nextRun) + "\n"

	lastResult := job.LastResult
	if lastResult == "" {
		lastResult = "n/a"
	}
	content += labelStyle.Render("Last Result:") + valueStyle.Render(lastResult) + "\n"

	if job.Action != "" {
		content += labelStyle.Render("Action:") + valueStyle.Render(job.Action) + "\n"
	}

	return panelStyle.Render(content)
}

func (m *TasksModel) renderEmptyDetail() string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(1, 2).
		Width(m.width - 4)

	content := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Italic(true).
		Render("Select a job to view details")

	return panelStyle.Render(content)
}

func (m *TasksModel) renderHelp() string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7C3AED")).
		Padding(2, 4).
		Width(m.width - 4)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED")).
		MarginBottom(1)

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F59E0B")).
		Bold(true).
		Width(12)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	content := titleStyle.Render("Tasks View Help") + "\n\n"
	content += keyStyle.Render("up/k") + descStyle.Render("Move cursor up") + "\n"
	content += keyStyle.Render("down/j") + descStyle.Render("Move cursor down") + "\n"
	content += keyStyle.Render("enter") + descStyle.Render("Select job for detail") + "\n"
	content += keyStyle.Render("esc") + descStyle.Render("Clear selection") + "\n"
	content += keyStyle.Render("r") + descStyle.Render("Refresh job list") + "\n"
	content += keyStyle.Render("?") + descStyle.Render("Toggle this help") + "\n"
	content += "\n"
	content += lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("Press any key to close")

	return panelStyle.Render(content)
}
