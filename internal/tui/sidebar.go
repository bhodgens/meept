package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/caimlas/meept/internal/tui/components"
	"github.com/caimlas/meept/internal/tui/models"
	"github.com/caimlas/meept/internal/tui/types"
	"github.com/caimlas/meept/internal/tui/viz"
)

// SidebarPanel represents a collapsible panel in the sidebar.
type SidebarPanel int

const (
	PanelStatus SidebarPanel = iota
	PanelAgentActivity
	PanelWorkers
	PanelTasks
	PanelMemory
	PanelMetrics
	PanelActivityFeed
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
	selectedPanel SidebarPanel // For keyboard navigation

	// Panel header Y positions for click detection
	panelHeaderY map[SidebarPanel]int

	// Cached data for panels
	statusData        *SidebarStatusData
	agentActivityData []SidebarAgentActivity
	tasksData         []SidebarTaskItem
	memoryData        []SidebarMemoryItem
	workersData       []SidebarWorkerItem

	// Metrics data for sparklines
	metricsCollector *MetricsCollector
	queueSparkline   *components.Sparkline
	workersSparkline *components.Sparkline
	agentsSparkline  *components.Sparkline

	// Activity feed data
	eventStream  *EventStream
	activityFeed []ActivityFeedItem

	// Dispatch visualization
	viz              *viz.DispatchViz
	animationEnabled bool

	// Loading/error state
	loading bool
	err     error
}

// ActivityFeedItem represents a single item in the activity feed.
type ActivityFeedItem struct {
	Timestamp time.Time
	Topic     string
	Summary   string
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
func NewSidebarModel(rpc *RPCClient, styles *Styles, animationEnabled bool) *SidebarModel {
	s := &SidebarModel{
		rpc:              rpc,
		styles:           styles,
		expandedPanel:    PanelStatus,
		selectedPanel:    PanelStatus,
		visible:          true, // Visible by default
		animationEnabled: animationEnabled,
		activityFeed:     make([]ActivityFeedItem, 0),
		panelHeaderY:     make(map[SidebarPanel]int),
	}
	if animationEnabled {
		s.viz = viz.NewDispatchViz(30) // Default width
	}

	// Initialize sparklines
	s.queueSparkline = components.NewSparkline("queue", 20)
	s.workersSparkline = components.NewSparkline("workers", 20)
	s.agentsSparkline = components.NewSparkline("agents", 20)

	// Initialize metrics collector
	s.metricsCollector = NewMetricsCollector(rpc, 30)

	// Initialize event stream
	s.eventStream = NewEventStream(rpc, nil)

	return s
}

// SetSize updates the sidebar dimensions.
func (s *SidebarModel) SetSize(width, height int) {
	s.width = width
	s.height = height
	// Update viz width to match sidebar content area
	// Account for: border (2) + padding (2) + small margin (2) = 6
	if s.viz != nil && width > 8 {
		s.viz.SetSize(width - 6)
	}
	// Update sparkline widths
	sparklineWidth := width - 14 // Account for label + padding
	if sparklineWidth < 5 {
		sparklineWidth = 5
	}
	if s.queueSparkline != nil {
		s.queueSparkline.SetWidth(sparklineWidth)
	}
	if s.workersSparkline != nil {
		s.workersSparkline.SetWidth(sparklineWidth)
	}
	if s.agentsSparkline != nil {
		s.agentsSparkline.SetWidth(sparklineWidth)
	}
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

// HandleClick processes a mouse click at the given relative coordinates.
// Returns a tea.Cmd if an action should be taken.
func (s *SidebarModel) HandleClick(x, y int) tea.Cmd {
	// Check if click is on a panel header
	for panel, headerY := range s.panelHeaderY {
		// Headers are typically 1 line tall, check if y is within that line
		if y == headerY {
			s.expandedPanel = panel
			s.selectedPanel = panel
			return nil
		}
	}
	return nil
}

// SidebarRefreshTick signals time for sidebar data refresh.
type SidebarRefreshTick struct{}

// Init initializes the sidebar.
func (s *SidebarModel) Init() tea.Cmd {
	if !s.visible {
		return nil
	}
	// Initialize data refresh, periodic tick, and optionally visualization tick
	cmds := []tea.Cmd{s.refreshData(), s.scheduleRefresh()}
	if s.animationEnabled && s.viz != nil {
		cmds = append(cmds, s.viz.Init())
	}
	// Start metrics collector
	if s.metricsCollector != nil {
		cmds = append(cmds, s.metricsCollector.Start())
	}
	// Start event stream
	if s.eventStream != nil {
		cmds = append(cmds, s.eventStream.Start())
	}
	return tea.Batch(cmds...)
}

// scheduleRefresh schedules the next periodic refresh.
func (s *SidebarModel) scheduleRefresh() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return SidebarRefreshTick{}
	})
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

		// Fetch recent memories
		var memories []SidebarMemoryItem
		if s.rpc.IsConnected() {
			if memResp, err := s.rpc.GetRecentMemories(5); err == nil {
				items := memResp.GetItems()
				for _, m := range items {
					preview := m.Content
					if len(preview) > 50 {
						preview = preview[:47] + "..."
					}
					memories = append(memories, SidebarMemoryItem{
						ID:      m.ID,
						Type:    m.GetType(),
						Preview: preview,
						Created: m.CreatedAt,
					})
				}
				// Set actual memory count from fetched memories
				status.MemoryCount = len(items)
			}
		}

		return SidebarDataMsg{
			Status:        status,
			AgentActivity: agentActivity,
			Workers:       workers,
			Tasks:         tasks,
			Memory:        memories,
		}
	}
}

// SidebarFocusChatMsg signals that focus should return to chat.
type SidebarFocusChatMsg struct{}

// Update handles messages for the sidebar.
func (s *SidebarModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case SidebarRefreshTick:
		// Periodic refresh - only if visible
		if s.visible {
			return tea.Batch(s.refreshData(), s.scheduleRefresh())
		}
		return s.scheduleRefresh() // Keep scheduling even if not visible

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

		// Sync visualization with data
		s.syncVizWithData()
		return nil

	case viz.VizTickMsg:
		// Forward tick to visualization and return next tick command
		if s.animationEnabled && s.viz != nil && s.visible {
			return s.viz.Update(msg)
		}
		return nil

	case MetricsTickMsg:
		// Forward to metrics collector
		if s.metricsCollector != nil && s.visible {
			return s.metricsCollector.Update(msg)
		}
		return nil

	case MetricsDataMsg:
		// Update sparklines with new metrics
		if s.metricsCollector != nil {
			cmd := s.metricsCollector.Update(msg)
			s.updateSparklines()
			return cmd
		}
		return nil

	case EventStreamTickMsg:
		// Forward to event stream
		if s.eventStream != nil && s.visible {
			return s.eventStream.Update(msg)
		}
		return nil

	case EventStreamDataMsg:
		// Update activity feed with new events
		if s.eventStream != nil {
			s.eventStream.Update(msg)
			s.updateActivityFeed()
		}
		// DEBUG: Log event count to stderr
		if len(msg.Events) > 0 {
			fmt.Fprintf(os.Stderr, "[DEBUG] Received %d events\n", len(msg.Events))
			for _, e := range msg.Events {
				fmt.Fprintf(os.Stderr, "[DEBUG]   Topic: %s\n", e.Topic)
			}
		}
		// Check for progress events and forward to chat
		// Collect all commands - don't return early on first match
		var cmds []tea.Cmd
		for _, e := range msg.Events {
			// DEBUG: Show exact topic comparison
			if e.Topic == "agent.progress" {
				fmt.Fprintf(os.Stderr, "[DEBUG] MATCH: topic '%s' == 'agent.progress'\n", e.Topic)
			}
			switch e.Topic {
			case "agent.progress":
				fmt.Fprintf(os.Stderr, "[DEBUG] Handling agent.progress event, payload type: %T\n", e.Payload)
				cmds = append(cmds, s.handleProgressEvent(e))
			case "llm.tokens.used":
				cmds = append(cmds, s.handleTokenEvent(e))
			case "conversation.reset":
				cmds = append(cmds, s.handleContextResetEvent(e))
			case "worker.state_changed":
				cmds = append(cmds, s.handleWorkerStateEvent(e))
			}
		}
		if len(cmds) > 0 {
			return tea.Batch(cmds...)
		}
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
			// Move selection up
			if s.selectedPanel > PanelStatus {
				s.selectedPanel--
			}
			return nil
		case "down", "j":
			// Move selection down
			if s.selectedPanel < PanelActivityFeed {
				s.selectedPanel++
			}
			return nil
		case "right", "enter", "l":
			// Expand selected panel
			s.expandedPanel = s.selectedPanel
			return nil
		case "left", "h":
			// Collapse current panel (go back to no expansion by selecting status)
			// Actually just cycle focus back
			s.focused = false
			return func() tea.Msg { return SidebarFocusChatMsg{} }
		}
	}

	return nil
}

// updateSparklines updates sparklines with metrics collector data.
func (s *SidebarModel) updateSparklines() {
	if s.metricsCollector == nil {
		return
	}

	// Update queue sparkline
	queueData := s.metricsCollector.QueueDepthHistory()
	s.queueSparkline.SetData(queueData)

	// Update workers sparkline
	workersData := s.metricsCollector.WorkersBusyHistory()
	s.workersSparkline.SetData(workersData)

	// Update agents sparkline
	agentsData := s.metricsCollector.AgentsActiveHistory()
	s.agentsSparkline.SetData(agentsData)
}

// updateActivityFeed updates the activity feed with recent events.
func (s *SidebarModel) updateActivityFeed() {
	if s.eventStream == nil {
		return
	}

	// Get recent events
	events := s.eventStream.RecentEvents(10)

	// Convert to activity feed items
	s.activityFeed = make([]ActivityFeedItem, len(events))
	for i, e := range events {
		// Summarize the event
		summary := summarizeEvent(e.Topic, e.Payload)
		s.activityFeed[i] = ActivityFeedItem{
			Timestamp: e.Timestamp,
			Topic:     e.Topic,
			Summary:   summary,
		}
	}
}

// summarizeEvent creates a brief summary of a bus event.
func summarizeEvent(topic string, payload any) string {
	// Extract topic suffix for display
	parts := strings.Split(topic, ".")
	action := parts[len(parts)-1]

	// Try to extract key info from payload
	if payloadMap, ok := payload.(map[string]any); ok {
		if status, ok := payloadMap["status"].(string); ok {
			return action + " - " + status
		}
		if state, ok := payloadMap["state"].(string); ok {
			return action + " - " + state
		}
		if id, ok := payloadMap["id"].(string); ok {
			if len(id) > 8 {
				id = id[:8]
			}
			return action + " " + id
		}
	}

	return action
}

// handleProgressEvent converts an agent.progress bus event to a ProgressUpdateMsg.
func (s *SidebarModel) handleProgressEvent(e BusEvent) tea.Cmd {
	return func() tea.Msg {
		fmt.Fprintf(os.Stderr, "[DEBUG] handleProgressEvent: payload type=%T\n", e.Payload)
		payloadMap, ok := e.Payload.(map[string]any)
		if !ok {
			fmt.Fprintf(os.Stderr, "[DEBUG] handleProgressEvent: payload is NOT map[string]any, returning nil\n")
			return nil
		}

		var agentID, stage, currentTool string
		var percent float64
		var tokenCount float64

		// Support both field naming conventions
		if v, ok := payloadMap["agent_id"].(string); ok {
			agentID = v
		} else if v, ok := payloadMap["conversation_id"].(string); ok {
			agentID = v
		}
		if v, ok := payloadMap["stage"].(string); ok {
			stage = v
		}
		if v, ok := payloadMap["detail"].(string); ok {
			currentTool = v
		}
		if v, ok := payloadMap["percent"].(float64); ok {
			percent = v
		} else if iteration, ok := payloadMap["iteration"].(float64); ok {
			// Estimate percent from iteration (assume max 10 iterations)
			percent = iteration * 10.0
			if percent > 100 {
				percent = 100
			}
		}
		if v, ok := payloadMap["token_count"].(float64); ok {
			tokenCount = v
		}

		fmt.Fprintf(os.Stderr, "[DEBUG] handleProgressEvent: returning ProgressUpdateMsg{AgentID:%s, Stage:%s, Percent:%.0f, Tokens:%d, Tool:%s}\n",
			agentID, stage, percent, int(tokenCount), currentTool)

		return models.ProgressUpdateMsg{
			AgentID:     agentID,
			Stage:       stage,
			Percent:     percent,
			TokensUsed:  int(tokenCount),
			CurrentTool: currentTool,
		}
	}
}

// handleTokenEvent converts an llm.tokens.used event to a ProgressUpdateMsg.
func (s *SidebarModel) handleTokenEvent(e BusEvent) tea.Cmd {
	return func() tea.Msg {
		payloadMap, ok := e.Payload.(map[string]any)
		if !ok {
			return nil
		}

		var totalTokens float64
		if v, ok := payloadMap["total_tokens"].(float64); ok {
			totalTokens = v
		}

		// Just update the token count, preserve other progress state
		return models.ProgressUpdateMsg{
			TokensUsed: int(totalTokens),
		}
	}
}

// handleContextResetEvent converts a conversation.reset event to a ProgressUpdateMsg.
func (s *SidebarModel) handleContextResetEvent(e BusEvent) tea.Cmd {
	return func() tea.Msg {
		payloadMap, ok := e.Payload.(map[string]any)
		if !ok {
			return nil
		}

		var resetCount int
		if v, ok := payloadMap["messages_removed"].(float64); ok {
			resetCount = int(v)
		}

		return models.ProgressUpdateMsg{
			ContextResets: resetCount,
		}
	}
}

// handleWorkerStateEvent converts a worker.state_changed event to a ProgressUpdateMsg.
func (s *SidebarModel) handleWorkerStateEvent(e BusEvent) tea.Cmd {
	return func() tea.Msg {
		payloadMap, ok := e.Payload.(map[string]any)
		if !ok {
			return nil
		}

		var currentTool string
		if v, ok := payloadMap["current_tool"].(string); ok {
			currentTool = v
		}

		return models.ProgressUpdateMsg{
			CurrentTool: currentTool,
		}
	}
}

// syncVizWithData synchronizes the visualization with current agent/worker data.
func (s *SidebarModel) syncVizWithData() {
	if !s.animationEnabled || s.viz == nil {
		return
	}

	// Convert agent activity to viz data
	var agents []viz.AgentActivityData
	for _, a := range s.agentActivityData {
		agents = append(agents, viz.AgentActivityData{
			AgentID:   a.AgentID,
			AgentName: a.AgentName,
			State:     a.State,
			Progress:  float64(a.Iteration) / float64(a.MaxIter),
		})
	}

	// Convert workers to viz data
	var workers []viz.WorkerData
	for _, w := range s.workersData {
		workers = append(workers, viz.WorkerData{
			ID:           w.ID,
			State:        w.State,
			CurrentJobID: w.CurrentJobID,
		})
	}

	s.viz.SyncWithData(agents, workers)
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
		MaxHeight(innerHeight).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		Width(s.width - 6).
		Align(lipgloss.Center)

	b.WriteString(titleStyle.Render("sidebar"))
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
	b.WriteString("\n")
	b.WriteString(s.renderMetricsPanel())
	b.WriteString("\n")
	b.WriteString(s.renderActivityFeedPanel())

	// Calculate space used by panels
	panelsContent := b.String()
	panelsLines := strings.Count(panelsContent, "\n") + 1

	// Calculate viz height (approximately square based on width)
	vizHeight := 0
	if s.animationEnabled && s.viz != nil {
		vizHeight = s.viz.Height()
	}

	// Calculate remaining space for viz
	remainingLines := innerHeight - panelsLines

	// Render visualization at bottom if we have space and animation is enabled
	if s.animationEnabled && s.viz != nil && remainingLines >= vizHeight {
		// Add spacing before viz
		spacingBeforeViz := remainingLines - vizHeight
		if spacingBeforeViz > 0 {
			b.WriteString(strings.Repeat("\n", spacingBeforeViz))
		}
		b.WriteString(s.viz.View())
	} else if remainingLines > 0 {
		// Just add spacing if no room for viz
		b.WriteString(strings.Repeat("\n", remainingLines))
	}

	return containerStyle.Render(b.String())
}

func (s *SidebarModel) renderPanelHeader(title string, panel SidebarPanel) string {
	icon := "▸"
	if s.expandedPanel == panel {
		icon = "▾"
	}

	// Selection indicator for keyboard navigation
	selectionIndicator := " "
	if s.focused && s.selectedPanel == panel {
		selectionIndicator = ">"
	}

	style := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Bold(s.expandedPanel == panel)

	if s.expandedPanel == panel {
		style = style.Foreground(ColorAccent)
	}

	// Highlight selected panel when sidebar is focused
	if s.focused && s.selectedPanel == panel {
		style = style.Background(lipgloss.Color("#374151"))
	}

	return style.Render(fmt.Sprintf("%s%s %s", selectionIndicator, icon, title))
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

func (s *SidebarModel) renderMetricsPanel() string {
	var b strings.Builder

	b.WriteString(s.renderPanelHeader("Metrics", PanelMetrics))
	b.WriteString("\n")

	if s.expandedPanel == PanelMetrics {
		// Sparkline style
		sparkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#06B6D4"))
		labelStyle := lipgloss.NewStyle().
			Foreground(ColorMuted).
			Width(10)

		// Queue depth sparkline
		s.queueSparkline.SetStyle(sparkStyle)
		b.WriteString("  ")
		b.WriteString(labelStyle.Render("queue:"))
		b.WriteString(s.queueSparkline.View())
		b.WriteString("\n")

		// Workers busy sparkline
		workerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))
		s.workersSparkline.SetStyle(workerStyle)
		b.WriteString("  ")
		b.WriteString(labelStyle.Render("workers:"))
		b.WriteString(s.workersSparkline.View())
		b.WriteString("\n")

		// Active agents sparkline
		agentStyle := lipgloss.NewStyle().Foreground(ColorAccent)
		s.agentsSparkline.SetStyle(agentStyle)
		b.WriteString("  ")
		b.WriteString(labelStyle.Render("agents:"))
		b.WriteString(s.agentsSparkline.View())
		b.WriteString("\n")

		// Current values
		if snapshot := s.metricsCollector.LatestSnapshot(); snapshot != nil {
			valueStyle := lipgloss.NewStyle().Foreground(ColorForeground)
			b.WriteString("  ")
			b.WriteString(s.styles.Muted.Render("current: "))
			b.WriteString(valueStyle.Render(fmt.Sprintf("q:%d w:%d a:%d",
				snapshot.QueueDepth,
				snapshot.WorkersBusy,
				snapshot.AgentsActive,
			)))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (s *SidebarModel) renderActivityFeedPanel() string {
	var b strings.Builder

	b.WriteString(s.renderPanelHeader("Activity", PanelActivityFeed))
	b.WriteString("\n")

	if s.expandedPanel == PanelActivityFeed {
		if len(s.activityFeed) == 0 {
			b.WriteString(s.styles.Muted.Render("  No recent activity"))
			b.WriteString("\n")
		} else {
			for i, item := range s.activityFeed {
				if i >= 8 { // Limit display to 8 items
					break
				}

				// Format timestamp
				timeStr := item.Timestamp.Format("15:04:05")

				// Topic color based on category
				topicStyle := s.styles.Muted
				topicParts := strings.Split(item.Topic, ".")
				if len(topicParts) > 0 {
					switch topicParts[0] {
					case "agent":
						topicStyle = lipgloss.NewStyle().Foreground(ColorAccent)
					case "task":
						topicStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B5CF6"))
					case "queue":
						topicStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#06B6D4"))
					case "worker":
						topicStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))
					case "memory":
						topicStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#EC4899"))
					}
				}

				// Truncate summary
				summary := item.Summary
				maxSummaryLen := s.width - 18 // Account for timestamp and spacing
				if maxSummaryLen < 5 {
					maxSummaryLen = 5
				}
				if len(summary) > maxSummaryLen {
					summary = summary[:maxSummaryLen-3] + "..."
				}

				b.WriteString(fmt.Sprintf("  %s %s",
					s.styles.Muted.Render(timeStr),
					topicStyle.Render(summary),
				))
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}
