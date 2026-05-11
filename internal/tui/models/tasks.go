package models

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// TasksModel is the model for the tasks view.
type TasksModel struct {
	rpc             TasksRPCClient
	jobs            []types.Job
	tasks           []types.TaskExtended
	table           table.Model
	selectedJob     *types.Job
	selectedTask    *types.TaskExtended
	width           int
	height          int
	loading         bool
	err             error
	showingHelp     bool
	showingDetail   bool          // Task detail modal
	viewMode        TaskViewMode  // jobs vs tasks
	filter          TaskFilter
	currentAgentID  string // Current agent ID for FilterMine (for agent-mode clients)
	currentSessionID string // Current session ID for FilterMine (for TUI clients)
	expandedTaskIDs map[string]bool // Track which parent tasks are expanded
	taskChildren    map[string][]types.TaskExtended // Map parent ID to children
}

// TaskViewMode selects between jobs, tasks, and lineage view.
type TaskViewMode int

const (
	ViewModeJobs TaskViewMode = iota
	ViewModeTasks
	ViewModeLineage
)

// TaskFilter defines filter options.
type TaskFilter int

const (
	FilterAll TaskFilter = iota
	FilterActive
	FilterMine
	FilterCompleted
	FilterFailed
)

// TasksRPCClient interface for the tasks model.
type TasksRPCClient interface {
	ListJobs() (*types.JobListResponse, error)
	ListTasksExtended() (*types.TaskExtendedListResponse, error)
	ListTaskSteps(taskID string) (*types.TaskStepsResponse, error)
	IsConnected() bool
}

// NewTasksModel creates a new tasks model.
func NewTasksModel(rpc TasksRPCClient) *TasksModel {
	// Initialize with task columns (7) since default viewMode is ViewModeTasks.
	// SetSize() will adjust column widths; setJobsColumns() switches to 4 job columns.
	columns := []table.Column{
		{Title: "Name", Width: 20},
		{Title: "State", Width: 8},
		{Title: "Agent", Width: 12},
		{Title: "Steps", Width: 7},
		{Title: "Progress", Width: 12},
		{Title: "Memory", Width: 10},
		{Title: "Updated", Width: 10},
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
		rpc:             rpc,
		table:           t,
		viewMode:        ViewModeTasks, // Default to tasks view
		filter:          FilterAll,
		expandedTaskIDs: make(map[string]bool),
		taskChildren:    make(map[string][]types.TaskExtended),
	}
}

// SetViewMode switches between jobs and tasks view.
func (m *TasksModel) SetViewMode(mode TaskViewMode) {
	m.viewMode = mode
	m.loading = true
}

// SetFilter sets the task filter.
func (m *TasksModel) SetFilter(filter TaskFilter) {
	m.filter = filter
	if m.viewMode == ViewModeTasks {
		m.updateTasksTable()
	}
}

// SetCurrentAgent sets the current agent ID for FilterMine filtering.
// This is used when the client is an agent (e.g., in a multi-agent setup).
func (m *TasksModel) SetCurrentAgent(agentID string) {
	m.currentAgentID = agentID
}

// SetCurrentSession sets the current session ID for FilterMine filtering.
// This is used by the TUI to filter tasks linked to the current session.
func (m *TasksModel) SetCurrentSession(sessionID string) {
	m.currentSessionID = sessionID
}

// buildTaskTree builds parent-child relationships from tasks.
func (m *TasksModel) buildTaskTree(tasks []types.TaskExtended) {
	m.taskChildren = make(map[string][]types.TaskExtended)
	for _, task := range tasks {
		if task.InheritedFrom != "" {
			m.taskChildren[task.InheritedFrom] = append(m.taskChildren[task.InheritedFrom], task)
		}
	}
}

// isTaskCollapsible checks if a task has children (subtasks).
func (m *TasksModel) isTaskCollapsible(taskID string) bool {
	children, ok := m.taskChildren[taskID]
	return ok && len(children) > 0
}

// isTaskExpanded checks if a parent task is currently expanded.
func (m *TasksModel) isTaskExpanded(taskID string) bool {
	return m.expandedTaskIDs[taskID]
}

// toggleTaskExpanded toggles the expanded state of a parent task.
func (m *TasksModel) toggleTaskExpanded(taskID string) {
	m.expandedTaskIDs[taskID] = !m.expandedTaskIDs[taskID]
}

// buildFlatListWithChildren builds a flat list for the table, respecting expanded/collapsed state.
// Parent tasks appear first, followed by their subtasks if expanded.
func (m *TasksModel) buildFlatListWithChildren(tasks []types.TaskExtended) []types.TaskExtended {
	var result []types.TaskExtended

	// Build a map of taskID to task for quick lookup
	taskMap := make(map[string]types.TaskExtended)
	for _, task := range tasks {
		taskMap[task.ID] = task
	}

	// First pass: add all parent tasks (tasks with no InheritedFrom)
	var parents []types.TaskExtended
	for _, task := range tasks {
		if task.InheritedFrom == "" {
			parents = append(parents, task)
		}
	}

	// Second pass: build result with parents and their children (if expanded)
	for _, parent := range parents {
		result = append(result, parent)

		// Add children if parent is expanded
		if m.isTaskExpanded(parent.ID) {
			children := m.taskChildren[parent.ID]
			result = append(result, children...)
		}
	}

	return result
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

	// Update column widths based on view mode
	if m.viewMode == ViewModeTasks {
		m.setTasksColumns()
	} else {
		m.setJobsColumns()
	}
}

func (m *TasksModel) setJobsColumns() {
	// Clear rows before changing columns to prevent panic from row/column mismatch
	m.table.SetRows([]table.Row{})

	colWidth := (m.width - 20) / 4
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

func (m *TasksModel) setTasksColumns() {
	// Clear rows before changing columns to prevent panic from row/column mismatch
	m.table.SetRows([]table.Row{})

	// Task view columns: Name | State | Agent | Steps | Progress | Memory | Updated
	available := m.width - 10 // borders/padding
	nameW := available * 22 / 100
	stateW := 8
	agentW := 12
	stepsW := 7
	progressW := 12
	memoryW := 10
	updatedW := 10

	if nameW < 15 {
		nameW = 15
	}

	m.table.SetColumns([]table.Column{
		{Title: "Name", Width: nameW},
		{Title: "State", Width: stateW},
		{Title: "Agent", Width: agentW},
		{Title: "Steps", Width: stepsW},
		{Title: "Progress", Width: progressW},
		{Title: "Memory", Width: memoryW},
		{Title: "Updated", Width: updatedW},
	})
}

// JobsUpdateMsg carries the jobs update.
type JobsUpdateMsg struct {
	Jobs []types.Job
	Err  error
}

// TasksUpdateMsg carries the tasks update.
type TasksUpdateMsg struct {
	Tasks []types.TaskExtended
	Err   error
}

// Init initializes the tasks model.
func (m *TasksModel) Init() tea.Cmd {
	if m.viewMode == ViewModeTasks {
		return m.fetchTasks
	}
	return m.fetchJobs
}

func (m *TasksModel) fetchJobs() tea.Msg {
	resp, err := m.rpc.ListJobs()
	if err != nil {
		return JobsUpdateMsg{Err: err}
	}
	return JobsUpdateMsg{Jobs: resp.Jobs}
}

func (m *TasksModel) fetchTasks() tea.Msg {
	resp, err := m.rpc.ListTasksExtended()
	if err != nil {
		return TasksUpdateMsg{Err: err}
	}
	return TasksUpdateMsg{Tasks: resp.Tasks}
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

	case TasksUpdateMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return nil
		}
		m.err = nil
		m.tasks = msg.Tasks
		m.buildTaskTree(msg.Tasks)
		m.updateTasksTable()
		return nil

	case tea.KeyPressMsg:
		// Handle detail modal first
		if m.showingDetail {
			if msg.String() == "esc" || msg.String() == "q" {
				m.showingDetail = false
				return nil
			}
			// Other keys in modal could be handled here
			return nil
		}

		if m.showingHelp {
			m.showingHelp = false
			return nil
		}

		switch msg.String() {
		case "r":
			// Refresh
			m.loading = true
			if m.viewMode == ViewModeTasks {
				return m.fetchTasks
			}
			return m.fetchJobs

		case "?":
			m.showingHelp = true
			return nil

		case "tab":
			// Cycle view mode: tasks -> jobs -> lineage -> tasks
			switch m.viewMode {
			case ViewModeTasks:
				m.viewMode = ViewModeJobs
				m.setJobsColumns()
				m.loading = true
				return m.fetchJobs
			case ViewModeJobs:
				m.viewMode = ViewModeLineage
				m.loading = true
				return m.fetchTasks
			default: // ViewModeLineage
				m.viewMode = ViewModeTasks
				m.setTasksColumns()
				m.loading = true
				return m.fetchTasks
			}

		case "t":
			// Quick toggle lineage view
			if m.viewMode == ViewModeLineage {
				m.viewMode = ViewModeTasks
				m.setTasksColumns()
				m.loading = true
				return m.fetchTasks
			}
			m.viewMode = ViewModeLineage
			m.loading = true
			return m.fetchTasks

		case "f":
			// Cycle through filters
			m.filter = (m.filter + 1) % 5
			if m.viewMode == ViewModeTasks {
				m.updateTasksTable()
			}
			return nil

		case "enter":
			// Open detail modal
			if m.viewMode == ViewModeTasks {
				if len(m.tasks) > 0 {
					idx := m.table.Cursor()
					filtered := m.filterTasks()
					if idx >= 0 && idx < len(filtered) {
						m.selectedTask = &filtered[idx]
						m.showingDetail = true
					}
				}
			} else {
				if len(m.jobs) > 0 {
					idx := m.table.Cursor()
					if idx >= 0 && idx < len(m.jobs) {
						m.selectedJob = &m.jobs[idx]
					}
				}
			}
			return nil

		case "esc":
			m.selectedJob = nil
			m.selectedTask = nil
			m.showingDetail = false
			return nil

		case "left":
			// Collapse expanded parent task
			if m.viewMode == ViewModeTasks {
				filtered := m.filterTasks()
				idx := m.table.Cursor()
				if idx >= 0 && idx < len(filtered) {
					task := filtered[idx]
					// Only collapse if this task has children and is expanded
					if m.isTaskCollapsible(task.ID) && m.isTaskExpanded(task.ID) {
						m.toggleTaskExpanded(task.ID)
						m.updateTasksTable()
					}
				}
			}
			return nil

		case "right":
			// Expand collapsed parent task
			if m.viewMode == ViewModeTasks {
				filtered := m.filterTasks()
				idx := m.table.Cursor()
				if idx >= 0 && idx < len(filtered) {
					task := filtered[idx]
					// Only expand if this task has children and is collapsed
					if m.isTaskCollapsible(task.ID) && !m.isTaskExpanded(task.ID) {
						m.toggleTaskExpanded(task.ID)
						m.updateTasksTable()
					}
				}
			}
			return nil

		case "up", "down", "j", "k":
			// Let table handle navigation
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			// Update selection as we navigate
			idx := m.table.Cursor()
			if m.viewMode == ViewModeTasks {
				filtered := m.filterTasks()
				if idx >= 0 && idx < len(filtered) {
					m.selectedTask = &filtered[idx]
				}
			} else if idx >= 0 && idx < len(m.jobs) {
				m.selectedJob = &m.jobs[idx]
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
	if len(rows) > 0 {
		m.table.GotoTop()
	}
}

func (m *TasksModel) filterTasks() []types.TaskExtended {
	var filtered []types.TaskExtended

	for _, t := range m.tasks {
		include := false
		switch m.filter {
		case FilterAll:
			include = true
		case FilterActive:
			if t.State == "executing" || t.State == "planning" || t.State == "pending" {
				include = true
			}
		case FilterCompleted:
			if t.State == "completed" {
				include = true
			}
		case FilterFailed:
			if t.State == "failed" || t.State == "cancelled" {
				include = true
			}
		case FilterMine:
			// Filter by session (for TUI) or agent (for agent-mode clients).
			// Priority: session > agent > fallback to all assigned tasks.
			switch {
			case m.currentSessionID != "":
				// Check if current session is linked to this task
				for _, linkedSess := range t.LinkedSessions {
					if linkedSess == m.currentSessionID {
						include = true
						break
					}
				}
			case m.currentAgentID != "":
				if t.AssignedAgent == m.currentAgentID {
					include = true
				}
			case t.AssignedAgent != "":
				// Fallback: show all assigned tasks
				include = true
			}
		default:
			include = true
		}

		if include {
			filtered = append(filtered, t)
		}
	}

	// Build hierarchical list with children
	m.buildTaskTree(filtered)
	return m.buildFlatListWithChildren(filtered)
}

func (m *TasksModel) updateTasksTable() {
	tasks := m.filterTasks()
	rows := make([]table.Row, len(tasks))

	for i, task := range tasks {
		// State with icon
		stateIcon := m.getStateIcon(task.State)

		// Agent name (truncated)
		agent := task.AssignedAgent
		if agent == "" {
			agent = "-"
		}

		// Steps column: completed/total from step data
		stepsStr := "-"
		if len(task.Steps) > 0 {
			completedSteps := 0
			for _, s := range task.Steps {
				if s.State == "completed" {
					completedSteps++
				}
			}
			stepsStr = fmt.Sprintf("%d/%d", completedSteps, len(task.Steps))
		} else if task.TotalJobs > 0 {
			stepsStr = fmt.Sprintf("%d/%d", task.CompletedJobs, task.TotalJobs)
		}

		// Progress bar
		progress := m.renderProgressBar(task.CompletedJobs, task.TotalJobs, 8)

		// Memory indicators: ⚡refs ⬅inherited
		memRefs := len(task.MemoryRefs)
		inherited := 0
		if task.InheritedFrom != "" {
			inherited = 1 // Simplified; could count actual memories
		}
		memory := fmt.Sprintf("⚡%d⬅%d", memRefs, inherited)

		// Updated time
		updated := m.formatTimeAgo(task.UpdatedAt)

		// Name with indentation and expand/collapse icons
		name := task.Name
		if name == "" {
			name = types.TruncateString(task.ID, 15)
		}

		// Add tree indicators
		if task.InheritedFrom != "" {
			// This is a subtask - indent with tree character
			name = "  └─ " + name
		} else if m.isTaskCollapsible(task.ID) {
			// This is a parent task with children
			if m.isTaskExpanded(task.ID) {
				name = "▼ " + name
			} else {
				name = "▶ " + name
			}
		}

		rows[i] = table.Row{
			types.TruncateString(name, 20),
			stateIcon,
			types.TruncateString(agent, 10),
			stepsStr,
			progress,
			memory,
			updated,
		}
	}
	m.table.SetRows(rows)
	if len(rows) > 0 {
		m.table.GotoTop()
	}
}

func (m *TasksModel) getStateIcon(state string) string {
	switch state {
	case "pending":
		return "○ pend"
	case "planning":
		return "◐ plan"
	case "executing":
		return "● exec"
	case "testing":
		return "◑ test"
	case "completed":
		return "✓ done"
	case "failed":
		return "✗ fail"
	case "cancelled":
		return "⊘ stop"
	default:
		return "? " + types.TruncateString(state, 4)
	}
}

func (m *TasksModel) renderProgressBar(completed, total, width int) string {
	if total == 0 {
		return strings.Repeat("░", width)
	}

	filledWidth := (completed * width) / total
	emptyWidth := width - filledWidth

	if filledWidth > width {
		filledWidth = width
		emptyWidth = 0
	}

	bar := strings.Repeat("█", filledWidth) + strings.Repeat("░", emptyWidth)
	return fmt.Sprintf("%s %d/%d", bar, completed, total)
}

func (m *TasksModel) formatTimeAgo(timestamp string) string {
	// Simplified time formatting
	// In production, parse the timestamp and calculate relative time
	if timestamp == "" {
		return "n/a"
	}
	// Just return last few chars for now
	if len(timestamp) > 5 {
		return types.TruncateString(timestamp[len(timestamp)-8:], 8)
	}
	return timestamp
}

// View renders the tasks view.
func (m *TasksModel) View() string {
	// Task detail modal overlay
	if m.showingDetail && m.selectedTask != nil {
		return m.renderTaskDetailModal()
	}

	if m.showingHelp {
		return m.renderHelp()
	}

	// Lineage view has its own renderer
	if m.viewMode == ViewModeLineage {
		return m.renderLineageView()
	}

	// Check for loading/error based on view mode
	isEmpty := (m.viewMode == ViewModeTasks && len(m.tasks) == 0) ||
		(m.viewMode == ViewModeJobs && len(m.jobs) == 0)

	if m.loading && isEmpty {
		return m.renderLoading()
	}

	if m.err != nil && isEmpty {
		return m.renderError()
	}

	var b strings.Builder

	// Header with view mode toggle and filter
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Table
	tableStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151"))

	b.WriteString(tableStyle.Render(m.table.View()))
	b.WriteString("\n")

	// Detail panel (preview, not full modal)
	switch {
	case m.viewMode == ViewModeTasks && m.selectedTask != nil:
		b.WriteString(m.renderTaskPreview())
	case m.viewMode == ViewModeJobs && m.selectedJob != nil:
		b.WriteString(m.renderJobDetail())
	default:
		b.WriteString(m.renderEmptyDetail())
	}

	// Help hint
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		MarginTop(1)

	if m.viewMode == ViewModeTasks {
		b.WriteString(hintStyle.Render("r: refresh | tab: jobs view | t: lineage | f: filter | <-/->: collapse/expand | enter: details | ?: help"))
	} else {
		b.WriteString(hintStyle.Render("r: refresh | tab: lineage view | enter: select | ?: help"))
	}

	return b.String()
}

func (m *TasksModel) renderHeader() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED"))

	modeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Background(lipgloss.Color("#1F2937")).
		Padding(0, 1)

	activeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#7C3AED")).
		Bold(true).
		Padding(0, 1)

	filterStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F59E0B")).
		Padding(0, 1)

	var title string
	var tabs string

	switch {
	case m.viewMode == ViewModeTasks:
		title = titleStyle.Render("Tasks")
		tabs = activeStyle.Render("Tasks") + " " + modeStyle.Render("Jobs") + " " + modeStyle.Render("Lineage")
	case m.viewMode == ViewModeLineage:
		title = titleStyle.Render("Task Lineage")
		tabs = modeStyle.Render("Tasks") + " " + modeStyle.Render("Jobs") + " " + activeStyle.Render("Lineage")
	default:
		title = titleStyle.Render("Scheduled Jobs")
		tabs = modeStyle.Render("Tasks") + " " + activeStyle.Render("Jobs") + " " + modeStyle.Render("Lineage")
	}

	// Filter indicator
	filterText := ""
	if m.viewMode == ViewModeTasks {
		switch m.filter {
		case FilterAll:
			filterText = filterStyle.Render("[All]")
		case FilterActive:
			filterText = filterStyle.Render("[Active]")
		case FilterMine:
			filterText = filterStyle.Render("[Mine]")
		case FilterCompleted:
			filterText = filterStyle.Render("[Completed]")
		case FilterFailed:
			filterText = filterStyle.Render("[Failed]")
		}
	}

	// Layout: Title [tabs] [filter]
	header := lipgloss.JoinHorizontal(
		lipgloss.Left,
		title,
		"  ",
		tabs,
		"  ",
		filterText,
	)

	return header
}

func (m *TasksModel) renderTaskPreview() string {
	task := m.selectedTask
	if task == nil {
		return m.renderEmptyDetail()
	}

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7C3AED")).
		Padding(0, 1).
		Width(m.width - 4)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Width(12)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	memStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F59E0B"))

	// Quick preview
	var content strings.Builder

	content.WriteString(labelStyle.Render("ID:"))
	content.WriteString(valueStyle.Render(types.TruncateString(task.ID, 40)))
	content.WriteString("\n")

	if task.Description != "" {
		content.WriteString(labelStyle.Render("Desc:"))
		content.WriteString(valueStyle.Render(types.TruncateString(task.Description, 50)))
		content.WriteString("\n")
	}

	// Memory context summary
	memRefs := len(task.MemoryRefs)
	createdMems := len(task.CreatedMemories)
	inherited := ""
	if task.InheritedFrom != "" {
		inherited = fmt.Sprintf("from %s", types.TruncateString(task.InheritedFrom, 20))
	}

	content.WriteString(labelStyle.Render("Memory:"))
	content.WriteString(memStyle.Render(fmt.Sprintf("⚡%d refs  📝%d created  %s", memRefs, createdMems, inherited)))
	content.WriteString("\n")

	// Stats: models used, error count
	if len(task.ModelsUsed) > 0 {
		models := strings.Join(task.ModelsUsed, ", ")
		if len(models) > 40 {
			models = models[:37] + "..."
		}
		content.WriteString(labelStyle.Render("Models:"))
		content.WriteString(valueStyle.Render(models))
		content.WriteString("\n")
	}

	if task.ErrorCount > 0 {
		content.WriteString(labelStyle.Render("Errors:"))
		content.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Render(fmt.Sprintf("%d", task.ErrorCount)))
		content.WriteString("\n")
	}

	// Child tasks with progress
	if len(task.ChildTasks) > 0 {
		content.WriteString(labelStyle.Render("Children:"))
		for i, child := range task.ChildTasks {
			prefix := "├─"
			if i == len(task.ChildTasks)-1 {
				prefix = "└─"
			}
			percent := child.Progress()
			name := types.TruncateString(child.Name, 18)
			content.WriteString(fmt.Sprintf("    %s %s (%.0f%%)\n", prefix, name, percent))
		}
	}

	return panelStyle.Render(content.String())
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

func (m *TasksModel) renderTaskDetailModal() string {
	task := m.selectedTask
	if task == nil {
		return ""
	}

	// Modal style - centered overlay
	modalWidth := m.width - 8
	if modalWidth > 80 {
		modalWidth = 80
	}

	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("#7C3AED")).
		Padding(1, 2).
		Width(modalWidth)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED")).
		MarginBottom(1)

	sectionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F59E0B")).
		Bold(true).
		MarginTop(1)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Width(14)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	statusColor := m.getStateColor(task.State)
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(statusColor)).
		Bold(true)

	var content strings.Builder

	// Title
	name := task.Name
	if name == "" {
		name = task.ID
	}
	content.WriteString(titleStyle.Render("Task: " + types.TruncateString(name, 50)))
	content.WriteString("\n\n")

	// Basic info
	content.WriteString(labelStyle.Render("ID:"))
	content.WriteString(valueStyle.Render(task.ID))
	content.WriteString("\n")

	content.WriteString(labelStyle.Render("State:"))
	content.WriteString(statusStyle.Render(m.getStateIcon(task.State)))
	content.WriteString("\n")

	if task.AssignedAgent != "" {
		content.WriteString(labelStyle.Render("Agent:"))
		content.WriteString(valueStyle.Render(task.AssignedAgent))
		content.WriteString("\n")
	}

	content.WriteString(labelStyle.Render("Created:"))
	content.WriteString(valueStyle.Render(task.CreatedAt))
	content.WriteString("\n")

	content.WriteString(labelStyle.Render("Updated:"))
	content.WriteString(valueStyle.Render(task.UpdatedAt))
	content.WriteString("\n\n")

	// Progress section
	content.WriteString(labelStyle.Render("Progress:"))
	progress := m.renderProgressBar(task.CompletedJobs, task.TotalJobs, 20)
	percent := float64(0)
	if task.TotalJobs > 0 {
		percent = float64(task.CompletedJobs) / float64(task.TotalJobs) * 100
	}
	content.WriteString(valueStyle.Render(fmt.Sprintf("%s (%.0f%%)", progress, percent)))
	content.WriteString("\n")

	completedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))
	failedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444"))
	pendingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))

	pending := task.TotalJobs - task.CompletedJobs - task.FailedJobs
	if pending < 0 {
		pending = 0
	}

	content.WriteString("              ")
	content.WriteString(completedStyle.Render(fmt.Sprintf("✓ %d completed", task.CompletedJobs)))
	content.WriteString("  ")
	content.WriteString(pendingStyle.Render(fmt.Sprintf("○ %d pending", pending)))
	content.WriteString("  ")
	content.WriteString(failedStyle.Render(fmt.Sprintf("✗ %d failed", task.FailedJobs)))
	content.WriteString("\n")

	// Steps section
	if len(task.Steps) > 0 {
		content.WriteString("\n")
		content.WriteString(sectionStyle.Render("─── Steps ───"))
		content.WriteString("\n")

		agentStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#06B6D4")).
			Bold(true)

		for _, step := range task.Steps {
			// Line 1: seq. [agent] description  state_icon state_label
			stepIcon := m.getStepStateIcon(step.State)
			stepLabel := m.getStepStateLabel(step.State)
			stepColor := m.getStepStateColor(step.State)
			stepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(stepColor))

			agentLabel := ""
			if step.AgentID != "" {
				agentLabel = agentStyle.Render(fmt.Sprintf("[%s]", step.AgentID))
			}

			desc := step.Description
			maxDescLen := modalWidth - 30
			if maxDescLen < 20 {
				maxDescLen = 20
			}
			if len(desc) > maxDescLen {
				desc = desc[:maxDescLen-3] + "..."
			}

			revisionBadge := ""
			if step.RevisionCount > 0 {
				revisionBadge = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#F59E0B")).
					Render(fmt.Sprintf(" (rev %d)", step.RevisionCount))
			}

			content.WriteString(fmt.Sprintf(" %d. %s %s%s  %s",
				step.Sequence,
				agentLabel,
				valueStyle.Render(desc),
				revisionBadge,
				stepStyle.Render(stepIcon+" "+stepLabel),
			))
			content.WriteString("\n")

			// Line 2: progress bar  percent%  (blocked indicator)
			stepPercent := m.getStepPercent(step.State)
			barWidth := 20
			filled := int(stepPercent / 100 * float64(barWidth))
			empty := barWidth - filled
			if filled > barWidth {
				filled = barWidth
				empty = 0
			}
			bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

			blockedIndicator := ""
			if step.State == "pending" && len(step.DependsOn) > 0 {
				blockedIndicator = pendingStyle.Render("  (blocked)")
			}

			content.WriteString(fmt.Sprintf("    %s %3.0f%%%s",
				stepStyle.Render(bar),
				stepPercent,
				blockedIndicator,
			))
			content.WriteString("\n")
		}
	}

	// Memory Context section
	content.WriteString("\n")
	content.WriteString(sectionStyle.Render("─── Memory Context ───"))
	content.WriteString("\n")

	if task.InheritedFrom != "" {
		content.WriteString(labelStyle.Render("Inherited:"))
		content.WriteString(valueStyle.Render(task.InheritedFrom))
		content.WriteString("\n")
	}

	if len(task.MemoryRefs) > 0 {
		content.WriteString(labelStyle.Render("Memory refs:"))
		refs := strings.Join(task.MemoryRefs, ", ")
		if len(refs) > 50 {
			refs = refs[:47] + "..."
		}
		content.WriteString(valueStyle.Render(refs))
		content.WriteString("\n")
	}

	if task.ContextQuery != "" {
		content.WriteString(labelStyle.Render("Query:"))
		content.WriteString(valueStyle.Render(fmt.Sprintf("\"%s\"", task.ContextQuery)))
		content.WriteString("\n")
	}

	if len(task.CreatedMemories) > 0 {
		content.WriteString(labelStyle.Render("Created:"))
		mems := strings.Join(task.CreatedMemories, ", ")
		if len(mems) > 50 {
			mems = mems[:47] + "..."
		}
		content.WriteString(valueStyle.Render(mems))
		content.WriteString("\n")
	}

	// Linked Sessions section
	if len(task.LinkedSessions) > 0 {
		content.WriteString("\n")
		content.WriteString(sectionStyle.Render("─── Linked Sessions ───"))
		content.WriteString("\n")
		for _, sess := range task.LinkedSessions {
			content.WriteString("  • ")
			content.WriteString(valueStyle.Render(sess))
			content.WriteString("\n")
		}
	}

	// Footer
	content.WriteString("\n")
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Italic(true)
	content.WriteString(footerStyle.Render("[Esc/q] close"))

	return modalStyle.Render(content.String())
}

func (m *TasksModel) getStateColor(state string) string {
	switch state {
	case "pending":
		return "#6B7280" // Gray
	case "planning":
		return "#F59E0B" // Amber
	case "executing":
		return "#3B82F6" // Blue
	case "testing":
		return "#8B5CF6" // Purple
	case "completed":
		return "#10B981" // Green
	case "failed":
		return "#EF4444" // Red
	case "cancelled":
		return "#6B7280" // Gray
	default:
		return "#6B7280"
	}
}

func (m *TasksModel) getStepStateIcon(state string) string {
	switch state {
	case "pending":
		return "○"
	case "ready":
		return "◌"
	case "scheduled":
		return "◐"
	case "running":
		return "●"
	case "reviewing":
		return "🔍"
	case "approved":
		return "✔"
	case "rejected":
		return "✎"
	case "completed":
		return "✓"
	case "failed":
		return "✗"
	case "skipped":
		return "⊘"
	default:
		return "?"
	}
}

func (m *TasksModel) getStepStateLabel(state string) string {
	switch state {
	case "pending":
		return "pend"
	case "ready":
		return "ready"
	case "scheduled":
		return "sched"
	case "running":
		return "exec"
	case "reviewing":
		return "rev"
	case "approved":
		return "ok"
	case "rejected":
		return "fix"
	case "completed":
		return "done"
	case "failed":
		return "fail"
	case "skipped":
		return "skip"
	default:
		return state
	}
}

func (m *TasksModel) getStepStateColor(state string) string {
	switch state {
	case "pending":
		return "#6B7280"
	case "ready":
		return "#F59E0B"
	case "scheduled":
		return "#F59E0B"
	case "running":
		return "#3B82F6"
	case "reviewing":
		return "#8B5CF6"
	case "approved":
		return "#10B981"
	case "rejected":
		return "#F59E0B"
	case "completed":
		return "#10B981"
	case "failed":
		return "#EF4444"
	case "skipped":
		return "#6B7280"
	default:
		return "#6B7280"
	}
}

func (m *TasksModel) getStepPercent(state string) float64 {
	switch state {
	case "completed", "approved":
		return 100
	case "running", "reviewing":
		return 50
	case "failed", "rejected":
		return 100
	default:
		return 0
	}
}

// renderLineageView renders the task lineage tree view.
func (m *TasksModel) renderLineageView() string {
	// Fetch tasks if empty
	if len(m.tasks) == 0 {
		if m.loading {
			return m.renderLoading()
		}
		return m.renderError()
	}

	var b strings.Builder

	// Header
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED"))

	modeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Background(lipgloss.Color("#1F2937")).
		Padding(0, 1)

	activeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#7C3AED")).
		Bold(true).
		Padding(0, 1)

	tabs := modeStyle.Render("Tasks") + " " + modeStyle.Render("Jobs") + " " + activeStyle.Render("Lineage")
	b.WriteString(titleStyle.Render("Task Lineage"))
	b.WriteString("  ")
	b.WriteString(tabs)
	b.WriteString("\n\n")

	// Build the tree from tasks
	// Root tasks have no InheritedFrom, children reference their parent
	parentTasks := make(map[string][]types.TaskExtended) // parent ID -> children
	rootTasks := make([]types.TaskExtended, 0)
	taskMap := make(map[string]types.TaskExtended)

	for _, task := range m.tasks {
		taskMap[task.ID] = task
		if task.InheritedFrom == "" {
			rootTasks = append(rootTasks, task)
		} else {
			parentTasks[task.InheritedFrom] = append(parentTasks[task.InheritedFrom], task)
		}
	}

	// Render tree
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7C3AED")).
		Padding(1, 2).
		Width(m.width - 4)

	if len(rootTasks) == 0 {
		b.WriteString(panelStyle.Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true).Render("No tasks with lineage information"),
		))
	} else {
		var treeContent strings.Builder
		for i, root := range rootTasks {
			if i > 0 {
				treeContent.WriteString("\n")
			}
			m.renderTaskNode(&treeContent, root, parentTasks, "", true, i == len(rootTasks)-1)
		}
		b.WriteString(panelStyle.Render(treeContent.String()))
	}

	// Footer hints
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		MarginTop(1)
	b.WriteString(hintStyle.Render("tab: tasks view | t: toggle | r: refresh | enter: details | ?: help"))

	return b.String()
}

// renderTaskNode renders a single task node in the lineage tree.
func (m *TasksModel) renderTaskNode(b *strings.Builder, task types.TaskExtended, parentTasks map[string][]types.TaskExtended, prefix string, isRoot, isLast bool) {
	stateIcon := m.getStateIcon(task.State)
	stateColor := m.getStateColor(task.State)
	stateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(stateColor))

	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB"))
	memStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))

	// Task name
	name := task.Name
	if name == "" {
		name = types.TruncateString(task.ID, 20)
	}
	name = fmt.Sprintf("%q", name)

	// Connector for non-root nodes
	connector := ""
	if !isRoot {
		if isLast {
			connector = prefix + "└── "
		} else {
			connector = prefix + "├── "
		}
	}

	// Render task line
	b.WriteString(connector)
	b.WriteString(stateStyle.Render(stateIcon))
	b.WriteString(" ")
	b.WriteString(nameStyle.Render(name))
	b.WriteString(" ")
	b.WriteString(stateStyle.Render(task.State))

	// Show progress if applicable
	if task.TotalJobs > 0 {
		percent := float64(task.CompletedJobs) / float64(task.TotalJobs) * 100
		b.WriteString(mutedStyle.Render(fmt.Sprintf(" (%.0f%%)", percent)))
	}
	b.WriteString("\n")

	// Memory info line
	memRefs := len(task.MemoryRefs)
	createdMems := len(task.CreatedMemories)
	if memRefs > 0 || createdMems > 0 || task.InheritedFrom != "" {
		childPrefix := prefix + "│   "
		if isRoot {
			childPrefix = ""
		} else if isLast {
			childPrefix = prefix + "    "
		}

		var memParts []string
		if task.InheritedFrom != "" {
			parent := types.TruncateString(task.InheritedFrom, 20)
			memParts = append(memParts, mutedStyle.Render("inherited from: ")+memStyle.Render(parent))
		}
		if memRefs > 0 {
			refs := strings.Join(task.MemoryRefs, ", ")
			if len(refs) > 30 {
				refs = refs[:27] + "..."
			}
			memParts = append(memParts, memStyle.Render(fmt.Sprintf("refs: %s", refs)))
		}
		if createdMems > 0 {
			refs := strings.Join(task.CreatedMemories, ", ")
			if len(refs) > 30 {
				refs = refs[:27] + "..."
			}
			memParts = append(memParts, memStyle.Render(fmt.Sprintf("created: %s", refs)))
		}

		if len(memParts) > 0 {
			b.WriteString(childPrefix + strings.Join(memParts, " | ") + "\n")
		}
	}

	// Render children
	children := parentTasks[task.ID]
	childPrefix := prefix + "│   "
	if isRoot {
		childPrefix = "│   "
	} else if isLast {
		childPrefix = prefix + "    "
	}

	for j, child := range children {
		m.renderTaskNode(b, child, parentTasks, childPrefix, false, j == len(children)-1)
	}
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

	sectionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F59E0B")).
		Bold(true).
		MarginTop(1)

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F59E0B")).
		Bold(true).
		Width(12)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	content := titleStyle.Render("Tasks View Help") + "\n\n"

	content += sectionStyle.Render("Navigation") + "\n"
	content += keyStyle.Render("up/k") + descStyle.Render("Move cursor up") + "\n"
	content += keyStyle.Render("down/j") + descStyle.Render("Move cursor down") + "\n"
	content += keyStyle.Render("enter") + descStyle.Render("Open task detail modal") + "\n"
	content += keyStyle.Render("esc") + descStyle.Render("Close modal / clear selection") + "\n"

	content += "\n" + sectionStyle.Render("View Controls") + "\n"
	content += keyStyle.Render("tab") + descStyle.Render("Cycle Tasks/Jobs/Lineage views") + "\n"
	content += keyStyle.Render("t") + descStyle.Render("Toggle lineage view") + "\n"
	content += keyStyle.Render("f") + descStyle.Render("Cycle through filters (All/Active/Mine/Done/Failed)") + "\n"
	content += keyStyle.Render("r") + descStyle.Render("Refresh data") + "\n"
	content += keyStyle.Render("←/→") + descStyle.Render("Collapse/expand task tree") + "\n"
	content += keyStyle.Render("?") + descStyle.Render("Toggle this help") + "\n"

	content += "\n" + sectionStyle.Render("Memory Indicators") + "\n"
	content += keyStyle.Render("⚡N") + descStyle.Render("N memory references") + "\n"
	content += keyStyle.Render("⬅N") + descStyle.Render("N inherited memories") + "\n"
	content += keyStyle.Render("📝N") + descStyle.Render("N memories created") + "\n"

	content += "\n" + sectionStyle.Render("State Icons") + "\n"
	content += keyStyle.Render("○") + descStyle.Render("Pending") + "\n"
	content += keyStyle.Render("◐") + descStyle.Render("Planning") + "\n"
	content += keyStyle.Render("●") + descStyle.Render("Executing") + "\n"
	content += keyStyle.Render("✓") + descStyle.Render("Completed") + "\n"
	content += keyStyle.Render("✗") + descStyle.Render("Failed") + "\n"

	content += "\n"
	content += lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("Press any key to close")

	return panelStyle.Render(content)
}
