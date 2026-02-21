package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/caimlas/meept/internal/tui/types"
)

// SidebarPanel represents a collapsible panel in the sidebar.
type SidebarPanel int

const (
	PanelStatus SidebarPanel = iota
	PanelAgentActivity
	PanelWorkers
	PanelTasks
	PanelMemory
)

// SidebarModel is the model for the expandable sidebar.
type SidebarModel struct {
	width         int
	height        int
	visible       bool
	focused       bool
	styles        *Styles
	rpc           *RPCClient
	expandedPanel SidebarPanel

	// Cached data for panels
	statusData        *SidebarStatusData
	agentActivityData []SidebarAgentActivity
	tasksData         []SidebarTaskItem
	memoryData        []SidebarMemoryItem
	workersData       []SidebarWorkerItem

	// Loading/error state
	loading bool
	err     error
}

// SidebarStatusData contains daemon status info for the sidebar.
type SidebarStatusData struct {
	DaemonRunning   bool
	Uptime          string
	ConversationCnt int
	MemoryCount     int
	ActiveWorkers   int
	PendingTasks    int
}

// SidebarWorkerItem represents a worker shown in the sidebar.
type SidebarWorkerItem struct {
	ID           string
	State        string
	CurrentJobID string
	Capabilities []string
}

// SidebarTaskItem represents a task shown in the sidebar.
type SidebarTaskItem struct {
	ID      string
	Title   string
	Status  string
	Created string
}

// SidebarMemoryItem represents a recent memory item in the sidebar.
type SidebarMemoryItem struct {
	ID      string
	Type    string
	Preview string
	Created string
}

// SidebarAgentActivity represents active agent execution in the sidebar.
type SidebarAgentActivity struct {
	AgentID    string
	AgentName  string
	Role       string // dispatcher, executor, reviewer
	Iteration  int
	MaxIter    int
	State      string // reasoning, tool_exec, waiting
	ToolCalls  []SidebarToolCall
	MemoryRefs int
	Inherited  int
}

// SidebarToolCall represents a tool call in progress.
type SidebarToolCall struct {
	Name  string
	State string // pending, running, done, error
}

// NewSidebarModel creates a new sidebar model.
func NewSidebarModel(rpc *RPCClient, styles *Styles) *SidebarModel {
	return &SidebarModel{
		rpc:           rpc,
		styles:        styles,
		expandedPanel: PanelStatus,
		visible:       true, // Visible by default
	}
}

// SetSize updates the sidebar dimensions.
func (s *SidebarModel) SetSize(width, height int) {
	s.width = width
	s.height = height
}

// SetVisible shows or hides the sidebar.
func (s *SidebarModel) SetVisible(visible bool) {
	s.visible = visible
}

// IsVisible returns whether the sidebar is visible.
func (s *SidebarModel) IsVisible() bool {
	return s.visible
}

// Toggle switches visibility.
func (s *SidebarModel) Toggle() {
	s.visible = !s.visible
}

// Width returns the sidebar width (0 if hidden).
func (s *SidebarModel) Width() int {
	if !s.visible {
		return 0
	}
	return s.width
}

// SetFocused sets the focus state of the sidebar.
func (s *SidebarModel) SetFocused(focused bool) {
	s.focused = focused
}

// IsFocused returns whether the sidebar has focus.
func (s *SidebarModel) IsFocused() bool {
	return s.focused
}

// Init initializes the sidebar.
func (s *SidebarModel) Init() tea.Cmd {
	if !s.visible {
		return nil
	}
	return s.refreshData()
}

// SidebarDataMsg carries refreshed sidebar data.
type SidebarDataMsg struct {
	Status        *SidebarStatusData
	AgentActivity []SidebarAgentActivity
	Workers       []SidebarWorkerItem
	Tasks         []SidebarTaskItem
	Memory        []SidebarMemoryItem
	Err           error
}

func (s *SidebarModel) refreshData() tea.Cmd {
	return func() tea.Msg {
		// Fetch status data
		status := &SidebarStatusData{
			DaemonRunning: s.rpc.IsConnected(),
		}

		var tasks []SidebarTaskItem
		var workers []SidebarWorkerItem
		var agentActivity []SidebarAgentActivity

		if s.rpc.IsConnected() {
			// Try to get status info
			if statusResp, err := s.rpc.Status(); err == nil {
				status.Uptime = types.FormatUptime(statusResp.UptimeSeconds)
				status.ConversationCnt = statusResp.BusSubscribers // Use bus subscribers as proxy
				status.MemoryCount = statusResp.TokensUsed        // Use tokens as proxy for activity
			}

			// Fetch worker pool stats and workers
			if poolResp, err := s.rpc.ListPoolWorkers(); err == nil {
				for _, w := range poolResp.Workers {
					workers = append(workers, SidebarWorkerItem{
						ID:           w.ID,
						State:        w.State,
						CurrentJobID: w.CurrentJobID,
						Capabilities: w.Capabilities,
					})
				}
				status.ActiveWorkers = len(workers)
			} else {
				// Fallback to old workers API
				if workersResp, err := s.rpc.ListWorkers(); err == nil {
					status.ActiveWorkers = workersResp.Count
				}
			}

			// Fetch active agent workers for agent activity and tasks panel
			if workersResp, err := s.rpc.ListWorkers(); err == nil {
				for _, w := range workersResp.Workers {
					// Create agent activity entry for active workers
					if w.State == "processing" || w.State == "executing_tool" {
						activity := SidebarAgentActivity{
							AgentID:   w.ID,
							AgentName: w.ID, // Use ID as name fallback
							State:     "reasoning",
							Iteration: 1, // Default
							MaxIter:   10,
						}

						if w.State == "executing_tool" {
							activity.State = "tool_exec"
						}

						// Add current tool if executing
						if w.CurrentTool != "" {
							activity.ToolCalls = []SidebarToolCall{
								{
									Name:  w.CurrentTool,
									State: "running",
								},
							}
						}

						agentActivity = append(agentActivity, activity)
					}

					// Also add to tasks for backward compatibility
					taskStatus := "running"
					if w.State == "completed" {
						taskStatus = "completed"
					} else if w.State == "error" {
						taskStatus = "failed"
					}

					title := w.ConversationID
					if w.CurrentTool != "" {
						title = "Tool: " + w.CurrentTool
					}

					tasks = append(tasks, SidebarTaskItem{
						ID:      w.ID,
						Title:   title,
						Status:  taskStatus,
						Created: w.StartTime,
					})
				}
			}

			// Fetch pending task count from task registry
			if taskResp, err := s.rpc.ListTasks("pending", 100); err == nil {
				status.PendingTasks = len(taskResp.Tasks)
			}
		}

		return SidebarDataMsg{
			Status:        status,
			AgentActivity: agentActivity,
			Workers:       workers,
			Tasks:         tasks,
			Memory:        nil, // TODO: Fetch from RPC when available
		}
	}
}

// SidebarFocusChatMsg signals that focus should return to chat.
type SidebarFocusChatMsg struct{}

// Update handles messages for the sidebar.
func (s *SidebarModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case SidebarDataMsg:
		s.loading = false
		if msg.Err != nil {
			s.err = msg.Err
			return nil
		}
		s.err = nil
		s.statusData = msg.Status
		s.agentActivityData = msg.AgentActivity
		s.workersData = msg.Workers
		s.tasksData = msg.Tasks
		s.memoryData = msg.Memory
		return nil

	case tea.KeyMsg:
		if !s.visible || !s.focused {
			return nil
		}
		switch msg.String() {
		case "tab":
			// Cycle focus back to chat
			s.focused = false
			return func() tea.Msg { return SidebarFocusChatMsg{} }
		case "up", "k":
			if s.expandedPanel > 0 {
				s.expandedPanel--
			}
			return nil
		case "down", "j":
			if s.expandedPanel < PanelMemory {
				s.expandedPanel++
			}
			return nil
		}
	}

	return nil
}

// View renders the sidebar.
func (s *SidebarModel) View() string {
	if !s.visible || s.width <= 0 {
		return ""
	}

	var b strings.Builder

	// Sidebar container style with focus-dependent border
	borderColor := ColorBorder
	if s.focused {
		borderColor = ColorPrimary
	}

	// Height is the total visual height including border (2 lines for top+bottom)
	// So inner content height should be s.height - 2
	innerHeight := s.height - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	containerStyle := lipgloss.NewStyle().
		Width(s.width - 2).
		Height(innerHeight).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		Width(s.width - 6).
		Align(lipgloss.Center)

	b.WriteString(titleStyle.Render("Sidebar"))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", s.width-6))
	b.WriteString("\n\n")

	// Render panels
	b.WriteString(s.renderStatusPanel())
	b.WriteString("\n")
	b.WriteString(s.renderAgentActivityPanel())
	b.WriteString("\n")
	b.WriteString(s.renderWorkersPanel())
	b.WriteString("\n")
	b.WriteString(s.renderTasksPanel())
	b.WriteString("\n")
	b.WriteString(s.renderMemoryPanel())

	// Help hint at bottom
	hintStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Width(s.width - 6).
		Align(lipgloss.Center)

	// Calculate remaining space for hint
	content := b.String()
	contentLines := strings.Count(content, "\n")
	remainingLines := s.height - contentLines - 4
	if remainingLines > 1 {
		b.WriteString(strings.Repeat("\n", remainingLines-1))
		hint := "j/k: navigate"
		if s.focused {
			hint = "j/k: navigate | Tab: focus chat"
		}
		b.WriteString(hintStyle.Render(hint))
	}

	return containerStyle.Render(b.String())
}

func (s *SidebarModel) renderPanelHeader(title string, panel SidebarPanel) string {
	icon := "▸"
	if s.expandedPanel == panel {
		icon = "▾"
	}

	style := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Bold(s.expandedPanel == panel)

	if s.expandedPanel == panel {
		style = style.Foreground(ColorAccent)
	}

	return style.Render(fmt.Sprintf("%s %s", icon, title))
}

func (s *SidebarModel) renderStatusPanel() string {
	var b strings.Builder

	b.WriteString(s.renderPanelHeader("Status", PanelStatus))
	b.WriteString("\n")

	if s.expandedPanel == PanelStatus {
		if s.statusData == nil {
			b.WriteString(s.styles.Muted.Render("  Loading..."))
		} else {
			// Connection status
			connStatus := "disconnected"
			connStyle := s.styles.StatusStopped
			if s.statusData.DaemonRunning {
				connStatus = "connected"
				connStyle = s.styles.StatusRunning
			}

			labelStyle := lipgloss.NewStyle().
				Foreground(ColorMuted).
				Width(12)

			valueStyle := lipgloss.NewStyle().
				Foreground(ColorForeground)

			b.WriteString(labelStyle.Render("  Daemon:"))
			b.WriteString(connStyle.Render(connStatus))
			b.WriteString("\n")

			if s.statusData.DaemonRunning {
				if s.statusData.Uptime != "" {
					b.WriteString(labelStyle.Render("  Uptime:"))
					b.WriteString(valueStyle.Render(s.statusData.Uptime))
					b.WriteString("\n")
				}

				b.WriteString(labelStyle.Render("  Agents:"))
				b.WriteString(valueStyle.Render(fmt.Sprintf("%d active", s.statusData.ActiveWorkers)))
				b.WriteString("\n")

				b.WriteString(labelStyle.Render("  Tasks:"))
				b.WriteString(valueStyle.Render(fmt.Sprintf("%d pending", s.statusData.PendingTasks)))
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

func (s *SidebarModel) renderAgentActivityPanel() string {
	var b strings.Builder

	b.WriteString(s.renderPanelHeader("Agent Activity", PanelAgentActivity))
	b.WriteString("\n")

	if s.expandedPanel == PanelAgentActivity {
		if len(s.agentActivityData) == 0 {
			b.WriteString(s.styles.Muted.Render("  No active agents"))
			b.WriteString("\n")
		} else {
			for i, agent := range s.agentActivityData {
				if i >= 3 { // Limit display to 3 agents
					b.WriteString(s.styles.Muted.Render(fmt.Sprintf("  +%d more...", len(s.agentActivityData)-3)))
					b.WriteString("\n")
					break
				}

				// State indicator
				stateIcon := "○"
				stateStyle := s.styles.Muted
				switch agent.State {
				case "reasoning":
					stateIcon = "◐"
					stateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
				case "tool_exec":
					stateIcon = "●"
					stateStyle = s.styles.StatusRunning
				case "waiting":
					stateIcon = "○"
					stateStyle = s.styles.Muted
				}

				// Agent name and iteration
				agentName := agent.AgentName
				if agentName == "" {
					agentName = agent.AgentID
				}
				maxNameLen := s.width - 18
				if len(agentName) > maxNameLen {
					agentName = agentName[:maxNameLen-3] + "..."
				}

				// Progress indicator
				progress := fmt.Sprintf("[%d/%d]", agent.Iteration, agent.MaxIter)

				b.WriteString(fmt.Sprintf("  %s %s %s",
					stateStyle.Render(stateIcon),
					s.styles.Paragraph.Render(agentName),
					s.styles.Muted.Render(progress),
				))
				b.WriteString("\n")

				// Show tool calls if any
				for j, tool := range agent.ToolCalls {
					if j >= 2 { // Max 2 tool calls shown
						b.WriteString(s.styles.Muted.Render(fmt.Sprintf("    +%d more tools...", len(agent.ToolCalls)-2)))
						b.WriteString("\n")
						break
					}

					toolIcon := "├─"
					if j == len(agent.ToolCalls)-1 || j == 1 {
						toolIcon = "└─"
					}

					toolState := "○"
					toolStyle := s.styles.Muted
					switch tool.State {
					case "running":
						toolState = "◐"
						toolStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
					case "done":
						toolState = "✓"
						toolStyle = s.styles.Success
					case "error":
						toolState = "✗"
						toolStyle = s.styles.Error
					}

					toolName := tool.Name
					maxToolLen := s.width - 14
					if len(toolName) > maxToolLen {
						toolName = toolName[:maxToolLen-3] + "..."
					}

					b.WriteString(fmt.Sprintf("    %s %s %s",
						s.styles.Muted.Render(toolIcon),
						toolStyle.Render(toolState),
						s.styles.Paragraph.Render(toolName),
					))
					b.WriteString("\n")
				}

				// Memory context summary
				if agent.MemoryRefs > 0 || agent.Inherited > 0 {
					memInfo := fmt.Sprintf("    refs:%d inherited:%d", agent.MemoryRefs, agent.Inherited)
					b.WriteString(s.styles.Muted.Render(memInfo))
					b.WriteString("\n")
				}
			}
		}
	}

	return b.String()
}

func (s *SidebarModel) renderWorkersPanel() string {
	var b strings.Builder

	b.WriteString(s.renderPanelHeader("Workers", PanelWorkers))
	b.WriteString("\n")

	if s.expandedPanel == PanelWorkers {
		if len(s.workersData) == 0 {
			b.WriteString(s.styles.Muted.Render("  No workers"))
			b.WriteString("\n")
		} else {
			for i, worker := range s.workersData {
				if i >= 6 { // Limit display
					b.WriteString(s.styles.Muted.Render(fmt.Sprintf("  +%d more...", len(s.workersData)-6)))
					b.WriteString("\n")
					break
				}

				// State indicator
				stateIcon := "○"
				stateStyle := s.styles.Muted
				switch worker.State {
				case "idle":
					stateIcon = "○"
					stateStyle = s.styles.Muted
				case "claiming":
					stateIcon = "◐"
					stateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
				case "processing":
					stateIcon = "●"
					stateStyle = s.styles.StatusRunning
				case "error":
					stateIcon = "✗"
					stateStyle = s.styles.Error
				}

				// Worker ID (shortened)
				workerID := worker.ID
				maxIDLen := s.width - 10
				if len(workerID) > maxIDLen {
					workerID = workerID[:maxIDLen-3] + "..."
				}

				b.WriteString(fmt.Sprintf("  %s %s",
					stateStyle.Render(stateIcon),
					s.styles.Paragraph.Render(workerID),
				))
				b.WriteString("\n")

				// Show current job if processing
				if worker.State == "processing" && worker.CurrentJobID != "" {
					jobID := worker.CurrentJobID
					maxJobLen := s.width - 12
					if len(jobID) > maxJobLen {
						jobID = jobID[:maxJobLen-3] + "..."
					}
					b.WriteString(fmt.Sprintf("    %s",
						s.styles.Muted.Render(jobID),
					))
					b.WriteString("\n")
				}
			}
		}
	}

	return b.String()
}

func (s *SidebarModel) renderTasksPanel() string {
	var b strings.Builder

	b.WriteString(s.renderPanelHeader("Tasks", PanelTasks))
	b.WriteString("\n")

	if s.expandedPanel == PanelTasks {
		if len(s.tasksData) == 0 {
			b.WriteString(s.styles.Muted.Render("  No active tasks"))
			b.WriteString("\n")
		} else {
			for i, task := range s.tasksData {
				if i >= 5 { // Limit display
					b.WriteString(s.styles.Muted.Render(fmt.Sprintf("  +%d more...", len(s.tasksData)-5)))
					b.WriteString("\n")
					break
				}

				statusIcon := "○"
				statusStyle := s.styles.Muted
				switch task.Status {
				case "running":
					statusIcon = "●"
					statusStyle = s.styles.StatusRunning
				case "completed":
					statusIcon = "✓"
					statusStyle = s.styles.Success
				case "failed":
					statusIcon = "✗"
					statusStyle = s.styles.Error
				}

				title := task.Title
				maxLen := s.width - 12
				if len(title) > maxLen {
					title = title[:maxLen-3] + "..."
				}

				b.WriteString(fmt.Sprintf("  %s %s",
					statusStyle.Render(statusIcon),
					s.styles.Paragraph.Render(title),
				))
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

func (s *SidebarModel) renderMemoryPanel() string {
	var b strings.Builder

	b.WriteString(s.renderPanelHeader("Recent Memory", PanelMemory))
	b.WriteString("\n")

	if s.expandedPanel == PanelMemory {
		if len(s.memoryData) == 0 {
			b.WriteString(s.styles.Muted.Render("  No recent memories"))
			b.WriteString("\n")
		} else {
			for i, mem := range s.memoryData {
				if i >= 5 { // Limit display
					b.WriteString(s.styles.Muted.Render(fmt.Sprintf("  +%d more...", len(s.memoryData)-5)))
					b.WriteString("\n")
					break
				}

				preview := mem.Preview
				maxLen := s.width - 8
				if len(preview) > maxLen {
					preview = preview[:maxLen-3] + "..."
				}

				typeStyle := s.styles.Muted
				switch mem.Type {
				case "episodic":
					typeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#06B6D4"))
				case "task":
					typeStyle = lipgloss.NewStyle().Foreground(ColorAccent)
				}

				b.WriteString(fmt.Sprintf("  %s %s",
					typeStyle.Render(fmt.Sprintf("[%s]", mem.Type)),
					s.styles.Paragraph.Render(preview),
				))
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}
