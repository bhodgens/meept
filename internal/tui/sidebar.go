package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/components"
	"github.com/caimlas/meept/internal/tui/models"
	"github.com/caimlas/meept/internal/tui/types"
	"github.com/caimlas/meept/internal/tui/viz"
	"github.com/caimlas/meept/internal/version"
)

// SidebarPanel represents a collapsible panel in the sidebar.
type SidebarPanel int

const (
	PanelStatus SidebarPanel = iota
	PanelAgentActivity
	PanelWorkers
	PanelTasks
	PanelPlan
	PanelMemory
	PanelMetrics
	PanelActivityFeed
)

// SidebarModel is the model for the expandable sidebar.
type SidebarModel struct {
	width          int
	height         int
	visible        bool
	focused        bool
	styles         *Styles
	rpc            *RPCClient
	expandedPanels map[SidebarPanel]bool // Multiple panels can be expanded
	selectedPanel  SidebarPanel          // For keyboard navigation

	// Panel header Y positions for click detection
	panelHeaderY map[SidebarPanel]int

	// Cached data for panels
	statusData        *SidebarStatusData
	agentActivityData []SidebarAgentActivity
	tasksData         []SidebarTaskItem
	memoryData        []SidebarMemoryItem
	workersData       []SidebarWorkerItem

	// Plan phase panel data
	planView *components.PlanView

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
	ID            string
	Title         string
	Status        string
	AgentID       string
	CompletedJobs int
	TotalJobs     int
	Created       string
	CurrentStep   string // Description of the currently executing step
	TokenUsage    int
	ReviewStatus  string // Current review status: "reviewing", "revision #N", "approved", "rejected"
	RevisionCount int    // Number of revision cycles for current step
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
// eventRPC is a separate RPC client for event stream polling, so it doesn't
// block on the main client's mutex during long-running Chat calls.
func NewSidebarModel(rpc, eventRPC *RPCClient, styles *Styles, animationEnabled bool) *SidebarModel {
	s := &SidebarModel{
		rpc:           rpc,
		styles:        styles,
		selectedPanel: PanelStatus,
		visible:       true, // Visible by default
		// All panels expanded by default
		expandedPanels: map[SidebarPanel]bool{
			PanelStatus:        true,
			PanelAgentActivity: true,
			PanelTasks:         true,
			PanelPlan:          false, // collapsed by default; expands when plan data arrives
			PanelMemory:        true,
			PanelMetrics:       true,
			PanelActivityFeed:  true,
		},
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

	// Initialize plan view widget (rendered in the Plan sidebar panel;
	// populated by refreshData from plan.list and updated on task.planned
	// events so users can see active plan phases alongside tasks).
	s.planView = components.NewPlanView(components.PlanViewConfig{
		Title: "active plan",
	})

	// Initialize metrics collector
	s.metricsCollector = NewMetricsCollector(rpc, 30)

	// Initialize event stream with dedicated RPC client to avoid blocking
	// on the main client's callMu during long-running Chat calls
	esRPC := eventRPC
	if esRPC == nil {
		esRPC = rpc // Fallback to shared client
	}
	s.eventStream = NewEventStream(esRPC, nil)

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
	sparklineWidth := max(width-14, 5) // Account for label + padding
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
		if y == headerY {
			// Toggle the clicked panel's expansion state
			s.expandedPanels[panel] = !s.expandedPanels[panel]
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

// Cleanup stops background goroutines (event stream, metrics collector).
// Should be called on app shutdown before tea.Quit.
func (s *SidebarModel) Cleanup() {
	if s.eventStream != nil {
		s.eventStream.Stop()
	}
	if s.metricsCollector != nil {
		s.metricsCollector.Stop()
	}
}
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
	PlanPhases    []components.PhaseRow
	PlanTitle     string
	Err           error
}

func (s *SidebarModel) refreshData() tea.Cmd {
	return func() tea.Msg {
		// Fetch status data - start assuming disconnected
		status := &SidebarStatusData{
			DaemonRunning: false,
		}

		var tasks []SidebarTaskItem
		var workers []SidebarWorkerItem
		var agentActivity []SidebarAgentActivity

		if s.rpc.IsConnected() {
			// Try to get status info
			if statusResp, err := s.rpc.Status(); err == nil {
				// Only mark as running if Status() succeeds
				status.DaemonRunning = true
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

			// Fetch active agent workers for agent activity panel
			if workersResp, err := s.rpc.ListWorkers(); err == nil {
				for _, w := range workersResp.Workers {
					// Create agent activity entry for active workers
					if w.State == StateProcessing || w.State == "executing_tool" {
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
				}
			}

			// Fetch tasks from task registry with progress data
			if taskResp, err := s.rpc.ListTasksExtended(); err == nil {
				for _, t := range taskResp.Tasks {
					taskStatus := t.State
					title := t.Name
					if title == "" {
						title = t.ID
					}
					tasks = append(tasks, SidebarTaskItem{
						ID:            t.ID,
						Title:         title,
						Status:        taskStatus,
						AgentID:       t.AssignedAgent,
						CompletedJobs: t.CompletedJobs,
						TotalJobs:     t.TotalJobs,
						Created:       t.CreatedAt,
						TokenUsage:    t.TokenUsage,
					})
				}
				status.PendingTasks = 0
				for _, t := range taskResp.Tasks {
					if t.State == "pending" || t.State == StatePlanning || t.State == StateExecuting {
						status.PendingTasks++
					}
				}
			} else {
				// Fallback: fetch pending task count
				if taskResp, err := s.rpc.ListTasks("pending", 100); err == nil {
					status.PendingTasks = len(taskResp.Tasks)
				}
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

		// Fetch the most recent plan with phases so the sidebar's Plan panel
		// can render its phase breakdown. We fetch up to 10 recent plans and
		// pick the first one that has phases populated. This runs every
		// refresh tick (2s) which is cheap because plan.list is a SQLite read.
		var planPhases []components.PhaseRow
		var planTitle string
		if s.rpc.IsConnected() {
			if raw, err := s.rpc.Call("plan.list", map[string]any{"limit": 10}); err == nil {
				var resp struct {
					Plans []types.PlanExtended `json:"plans"`
				}
				if json.Unmarshal(raw, &resp) == nil {
					for _, p := range resp.Plans {
						if len(p.Phases) == 0 {
							continue
						}
						planTitle = p.Title
						if planTitle == "" {
							planTitle = p.ID
						}
						for _, ph := range p.Phases {
							planPhases = append(planPhases, components.PhaseRow{
								Name:           ph.Name,
								State:          ph.State,
								Sequence:       ph.Sequence,
								TotalSteps:     ph.TotalSteps,
								CompletedSteps: ph.CompletedSteps,
							})
						}
						break
					}
				}
			}
		}

		return SidebarDataMsg{
			Status:        status,
			AgentActivity: agentActivity,
			Workers:       workers,
			Tasks:         tasks,
			Memory:        memories,
			PlanPhases:    planPhases,
			PlanTitle:     planTitle,
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
		// Update the plan panel: replace phases and auto-expand if non-empty.
		if s.planView != nil {
			if msg.PlanTitle != "" {
				s.planView.SetTitle(msg.PlanTitle)
			}
			s.planView.SetPhases(msg.PlanPhases)
			if len(msg.PlanPhases) > 0 {
				s.expandedPanels[PanelPlan] = true
			}
		}

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
		// Forward to event stream - always poll regardless of sidebar visibility
		// because progress events need to reach the chat model
		if s.eventStream != nil {
			return s.eventStream.Update(msg)
		}
		return nil

	case EventStreamDataMsg:
		// Update activity feed with new events
		if s.eventStream != nil {
			s.eventStream.Update(msg)
			s.updateActivityFeed()
		}
		var cmds []tea.Cmd
		for _, e := range msg.Events {
			switch e.Topic {
			case "agent.progress":
				cmds = append(cmds, s.handleProgressEvent(e))
			case "llm.tokens.used":
				cmds = append(cmds, s.handleTokenEvent(e))
			case "conversation.reset":
				cmds = append(cmds, s.handleContextResetEvent(e))
			case "worker.started", "worker.completed", "worker.error", "worker.state_changed":
				// Worker lifecycle + progress events: refresh data so the viz
				// picks up the new worker state immediately instead of
				// waiting for the next periodic poll tick.
				cmds = append(cmds, s.refreshData())
			case "task.progress":
				s.handleTaskProgressEvent(e)
				cmds = append(cmds, s.refreshData())
			case "task.step_completed":
				s.handleStepCompletedEvent(e)
			case "step.review_completed", "task.review_completed":
				s.handleReviewCompletedEvent(e)
				cmds = append(cmds, s.refreshData())
			case "task.planned", "task.completed", EventTaskFailed:
				cmds = append(cmds, s.refreshData())

			// DispatchViz: bridge typed agent events to the visualization.
			// These arrive on "agent.event.<type>" topics via the daemon's
			// EventEmitter bridgeToBus. The payload is the full AgentEvent
			// JSON (agent_id, data, etc.) deserialized as map[string]any.
			case "agent.event.turn_start":
				s.handleVizTurnStart(e)
			case "agent.event.turn_end":
				s.handleVizTurnEnd(e)
			case "agent.event.tool_execution_start":
				s.handleVizToolExecStart(e)
			case "agent.event.tool_execution_update":
				s.handleVizToolExecUpdate(e)
			case "agent.event.tool_execution_end":
				s.handleVizToolExecEnd(e)
			}
		}
		if len(cmds) > 0 {
			return tea.Batch(cmds...)
		}
		return nil

	case tea.KeyPressMsg:
		if !s.visible || !s.focused {
			return nil
		}
		switch msg.String() {
		case KeyTab:
			// Cycle focus back to chat
			s.focused = false
			return func() tea.Msg { return SidebarFocusChatMsg{} }
		case "up", "k":
			// Move selection up, skipping PanelWorkers (now just a counter)
			if s.selectedPanel > PanelStatus {
				s.selectedPanel--
				if s.selectedPanel == PanelWorkers {
					s.selectedPanel--
				}
			}
			return nil
		case KeyDown, "j":
			// Move selection down, skipping PanelWorkers (now just a counter)
			if s.selectedPanel < PanelActivityFeed {
				s.selectedPanel++
				if s.selectedPanel == PanelWorkers {
					s.selectedPanel++
				}
			}
			return nil
		case KeyRight, KeyEnter, "l":
			// Toggle selected panel expansion
			s.expandedPanels[s.selectedPanel] = !s.expandedPanels[s.selectedPanel]
			return nil
		case KeyLeft, "h":
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
		payloadMap, ok := e.Payload.(map[string]any)
		if !ok {
			return nil
		}

		var agentID, stage, currentTool string
		var percent float64
		var tokenCount float64

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
			percent = iteration * 10.0
			if percent > 100 {
				percent = 100
			}
		}
		if v, ok := payloadMap["token_count"].(float64); ok {
			tokenCount = v
		}

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

// handleTaskProgressEvent updates the current step for a task from progress events.
func (s *SidebarModel) handleTaskProgressEvent(e BusEvent) {
	payloadMap, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	taskID, _ := payloadMap["task_id"].(string)
	if taskID == "" {
		return
	}

	// Extract chat_visible flag - sidebar updates happen regardless,
	// but activity feed only gets chat_visible events
	chatVisible := true // Default to visible for backwards compatibility
	if cv, ok := payloadMap["chat_visible"].(bool); ok {
		chatVisible = cv
	}

	// If NOT chat visible, skip sidebar/task updates (sidebar-only event)
	if !chatVisible {
		return
	}

	currentStep, _ := payloadMap["current_step"].(string)

	// Update the matching task in tasksData
	for i := range s.tasksData {
		if s.tasksData[i].ID != taskID {
			continue
		}
		if currentStep != "" {
			s.tasksData[i].CurrentStep = currentStep
		}
		// Update job counts from event data (source of truth)
		if completed, ok := payloadMap["completed_jobs"].(float64); ok {
			s.tasksData[i].CompletedJobs = int(completed)
		}
		if total, ok := payloadMap["total_jobs"].(float64); ok {
			s.tasksData[i].TotalJobs = int(total)
		}
		// Update token usage from event data
		if tokenUsage, ok := payloadMap["token_usage"].(float64); ok {
			s.tasksData[i].TokenUsage = int(tokenUsage)
		}
		break
	}
}

// handleStepCompletedEvent updates task data when a step completes.
func (s *SidebarModel) handleStepCompletedEvent(e BusEvent) {
	payloadMap, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	taskID, _ := payloadMap["task_id"].(string)
	if taskID == "" {
		return
	}

	// Clear current step since it just completed (will be updated with next progress event)
	// Note: We don't increment CompletedJobs here to avoid drift from actual state.
	// The next task.progress event will provide accurate counts.
	for i := range s.tasksData {
		if s.tasksData[i].ID == taskID {
			s.tasksData[i].CurrentStep = ""
			break
		}
	}
}

// handleReviewCompletedEvent updates task data when a review completes.
func (s *SidebarModel) handleReviewCompletedEvent(e BusEvent) {
	payloadMap, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	taskID, _ := payloadMap["task_id"].(string)
	if taskID == "" {
		return
	}

	reviewStatus, _ := payloadMap["status"].(string)
	revisionCount := 0
	if rc, ok := payloadMap["revision_count"].(float64); ok {
		revisionCount = int(rc)
	}

	for i := range s.tasksData {
		if s.tasksData[i].ID != taskID {
			continue
		}
		switch reviewStatus {
		case "rejected":
			if revisionCount > 0 {
				s.tasksData[i].ReviewStatus = fmt.Sprintf("revision #%d", revisionCount)
			} else {
				s.tasksData[i].ReviewStatus = "rejected"
			}
		case "needs_info":
			s.tasksData[i].ReviewStatus = "reviewing..."
		case "approved":
			s.tasksData[i].ReviewStatus = "approved"
			s.tasksData[i].RevisionCount = 0
		default:
			s.tasksData[i].ReviewStatus = "reviewing..."
		}
		s.tasksData[i].RevisionCount = revisionCount
		break
	}
}

// --- DispatchViz typed event handlers ---
// These bridge agent.event.* bus topics to pushActivity calls on the
// DispatchViz model, mirroring the daemon-side RegisterEventListeners
// logic. The payload is the full AgentEvent JSON envelope with a nested
// "data" object.

// vizAgentInfo extracts agent_id and the nested data map from a typed
// agent event bus payload. Returns nil if the payload can't be decoded.
func vizAgentInfo(payload any) (agentID string, data map[string]any) {
	pm, ok := payload.(map[string]any)
	if !ok {
		return "", nil
	}
	id, _ := pm["agent_id"].(string)
	d, _ := pm["data"].(map[string]any)
	return id, d
}

func (s *SidebarModel) handleVizTurnStart(e BusEvent) {
	if s.viz == nil {
		return
	}
	agentID, _ := vizAgentInfo(e.Payload)
	if agentID == "" {
		return
	}
	s.viz.PushActivity(agentID, "reasoning", 0)
}

func (s *SidebarModel) handleVizTurnEnd(e BusEvent) {
	if s.viz == nil {
		return
	}
	agentID, data := vizAgentInfo(e.Payload)
	if agentID == "" {
		return
	}
	state := "waiting"
	if data != nil {
		if hadTools, ok := data["had_tool_calls"].(bool); ok && hadTools {
			state = "tool_exec"
		}
	}
	s.viz.PushActivity(agentID, state, 1.0)
}

func (s *SidebarModel) handleVizToolExecStart(e BusEvent) {
	if s.viz == nil {
		return
	}
	agentID, _ := vizAgentInfo(e.Payload)
	if agentID == "" {
		return
	}
	s.viz.PushActivity(agentID, "tool_exec", 0)
}

func (s *SidebarModel) handleVizToolExecUpdate(e BusEvent) {
	if s.viz == nil {
		return
	}
	agentID, data := vizAgentInfo(e.Payload)
	if agentID == "" || data == nil {
		return
	}
	progress := 0.5 // default mid-progress
	if status, ok := data["status"].(string); ok {
		switch status {
		case "running", "in_progress":
			progress = 0.3
		case "nearly_done", "finishing":
			progress = 0.8
		case "streaming":
			progress = 0.6
		}
	}
	s.viz.PushActivity(agentID, "tool_exec", progress)
}

func (s *SidebarModel) handleVizToolExecEnd(e BusEvent) {
	if s.viz == nil {
		return
	}
	agentID, data := vizAgentInfo(e.Payload)
	if agentID == "" || data == nil {
		return
	}
	success, _ := data["success"].(bool)
	if !success {
		s.viz.PushActivity(agentID, "error", 1.0)
		return
	}
	s.viz.PushActivity(agentID, "reasoning", 1.0)
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

	// Sidebar container style with focus-dependent border
	borderColor := ColorBorder
	if s.focused {
		borderColor = ColorPrimary
	}

	// Height is the total visual height including border (2 lines for top+bottom)
	innerHeight := max(s.height-2, 1)

	contentWidth := s.width - 4 // Account for border (2) + padding (2)

	// Calculate viz height FIRST - this is fixed at the bottom
	vizHeight := 0
	vizContent := ""
	if s.animationEnabled && s.viz != nil {
		vizHeight = s.viz.Height()
		vizContent = s.viz.View()
	}

	// Build header (1 line: version banner with orange bg, black text)
	// Version is based on git commit date
	versionStr := "meept " + version.String()
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#F97316")). // Orange
		Width(contentWidth).
		Align(lipgloss.Center)

	header := titleStyle.Render(versionStr)
	headerLines := 1

	// Calculate available height for scrollable panel content
	// innerHeight - headerLines - vizHeight - 1 (blank before viz)
	availableForPanels := max(innerHeight-headerLines-vizHeight-5, 3) // -1 header, -1 blank before viz, -4 bottom padding

	// Render all panel content
	var panelContent strings.Builder
	panelContent.WriteString(s.renderStatusPanel())
	panelContent.WriteString(s.renderAgentActivityPanel())
	panelContent.WriteString(s.renderWorkersPanel())
	panelContent.WriteString(s.renderTasksPanel())
	panelContent.WriteString(s.renderPlanPanel())
	panelContent.WriteString(s.renderMemoryPanel())
	panelContent.WriteString(s.renderMetricsPanel())
	panelContent.WriteString(s.renderActivityFeedPanel())

	// Truncate panels to fit
	panelStr := panelContent.String()
	panelLines := strings.Split(strings.TrimRight(panelStr, "\n"), "\n")
	if len(panelLines) > availableForPanels {
		panelLines = panelLines[:availableForPanels-1]
		panelLines = append(panelLines, s.styles.Muted.Render("  ..."))
	}

	// Pad panels to fill available space (so viz stays at bottom)
	for len(panelLines) < availableForPanels {
		panelLines = append(panelLines, "")
	}
	panels := strings.Join(panelLines, "\n")

	// Build panel header Y positions for click detection
	// Click Y is relative to sidebar content area (lipgloss handles border offset)
	s.panelHeaderY = make(map[SidebarPanel]int)
	panelNames := map[string]SidebarPanel{
		"Status":         PanelStatus,
		"Agent Activity": PanelAgentActivity,
		"Tasks":          PanelTasks,
		"Plan":           PanelPlan,
		"Recent Memory":  PanelMemory,
		"Metrics":        PanelMetrics,
		"Activity":       PanelActivityFeed,
	}
	// Panel lines start at Y=0 relative to content area
	panelStartY := 0
	for i, line := range panelLines {
		// Panel headers contain ▸ or ▾ followed by panel name
		for name, panel := range panelNames {
			if strings.Contains(line, name) && (strings.Contains(line, "▸") || strings.Contains(line, "▾")) {
				s.panelHeaderY[panel] = panelStartY + i
				break
			}
		}
	}

	// Compose final content: header + panels + blank + viz
	var content strings.Builder
	content.WriteString(header)
	content.WriteString("\n") // newline after header (starts panels on next line)
	content.WriteString(panels)
	if vizHeight > 0 {
		// Blank line before viz: panels already ends with content (no trailing \n),
		// so we need \n to end panels line, then empty string, then \n to start viz.
		content.WriteString("\n\n") // ends panel line + blank line
		content.WriteString(vizContent)
		content.WriteString("\n\n\n\n") // 4 lines bottom padding so viz isn't cut off
	}

	// Constrain the inner content to exactly innerHeight rows of the sidebar
	// content width. Panels that wrap to multiple visual rows at narrow
	// widths would otherwise push the sidebar taller than the chat view and
	// introduce blank padding rows beneath the chat input in JoinHorizontal.
	innerStyle := lipgloss.NewStyle().
		Width(contentWidth).
		Height(innerHeight).
		MaxHeight(innerHeight)
	innerRendered := innerStyle.Render(content.String())

	// Apply border + padding wrapper. Content is already exactly innerHeight
	// rows so the wrapped total is innerHeight + 2 (top/bottom border) =
	// s.height, matching chat.View() and preventing vertical mismatch.
	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)

	return containerStyle.Render(innerRendered)
}

func (s *SidebarModel) renderPanelHeader(title string, panel SidebarPanel) string {
	isExpanded := s.expandedPanels[panel]
	icon := "▸"
	if isExpanded {
		icon = "▾"
	}

	// Selection indicator for keyboard navigation
	selectionIndicator := " "
	if s.focused && s.selectedPanel == panel {
		selectionIndicator = ">"
	}

	style := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Bold(isExpanded)

	if isExpanded {
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

	if s.expandedPanels[PanelStatus] {
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

	if s.expandedPanels[PanelAgentActivity] {
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

				fmt.Fprintf(&b, "  %s %s %s",
					stateStyle.Render(stateIcon),
					s.styles.Paragraph.Render(agentName),
					s.styles.Muted.Render(progress))
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

					fmt.Fprintf(&b, "    %s %s %s",
						s.styles.Muted.Render(toolIcon),
						toolStyle.Render(toolState),
						s.styles.Paragraph.Render(toolName))
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

	// Count workers by state
	var idle, busy, errored int
	for _, w := range s.workersData {
		switch w.State {
		case "idle":
			idle++
		case "claiming", StateProcessing:
			busy++
		case "error":
			errored++
		}
	}

	total := len(s.workersData)

	// Compact single-line display: "Workers: 3 (2 busy, 1 idle)"
	labelStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	valueStyle := lipgloss.NewStyle().Foreground(ColorForeground)

	b.WriteString(labelStyle.Render("  workers: "))
	if total == 0 {
		b.WriteString(s.styles.Muted.Render("none"))
	} else {
		b.WriteString(valueStyle.Render(fmt.Sprintf("%d", total)))
		b.WriteString(s.styles.Muted.Render(" ("))
		if busy > 0 {
			b.WriteString(s.styles.StatusRunning.Render(fmt.Sprintf("%d busy", busy)))
			if idle > 0 || errored > 0 {
				b.WriteString(s.styles.Muted.Render(", "))
			}
		}
		if idle > 0 {
			b.WriteString(s.styles.Muted.Render(fmt.Sprintf("%d idle", idle)))
			if errored > 0 {
				b.WriteString(s.styles.Muted.Render(", "))
			}
		}
		if errored > 0 {
			b.WriteString(s.styles.Error.Render(fmt.Sprintf("%d err", errored)))
		}
		b.WriteString(s.styles.Muted.Render(")"))
	}
	b.WriteString("\n")

	return b.String()
}

func (s *SidebarModel) renderTasksPanel() string {
	var b strings.Builder

	b.WriteString(s.renderPanelHeader("Tasks", PanelTasks))
	b.WriteString("\n")

	if s.expandedPanels[PanelTasks] {
		if len(s.tasksData) == 0 {
			b.WriteString(s.styles.Muted.Render("  No active tasks"))
			b.WriteString("\n")
		} else {
			for i, task := range s.tasksData {
				if i >= 4 { // Limit to 4 tasks (8 lines: 2 lines per task)
					remaining := len(s.tasksData) - 4
					b.WriteString(s.styles.Muted.Render(fmt.Sprintf("  +%d more...", remaining)))
					b.WriteString("\n")
					break
				}

				// Line 1: state_icon task_name [agent]
				statusIcon := "○"
				statusStyle := s.styles.Muted
				switch task.Status {
				case "pending":
					statusIcon = "○"
					statusStyle = s.styles.Muted
				case StatePlanning:
					statusIcon = "◐"
					statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
				case StateExecuting:
					statusIcon = "●"
					statusStyle = s.styles.StatusRunning
				case "testing":
					statusIcon = "◑"
					statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B5CF6"))
				case "completed":
					statusIcon = "✓"
					statusStyle = s.styles.Success
				case "failed":
					statusIcon = "✗"
					statusStyle = s.styles.Error
				case "cancelled":
					statusIcon = "⊘"
					statusStyle = s.styles.Muted
				}

				agentLabel := ""
				if task.AgentID != "" {
					agentLabel = s.styles.Muted.Render(fmt.Sprintf(" [%s]", task.AgentID))
				}

				title := task.Title
				maxLen := max(s.width-16, 8)
				if len(title) > maxLen {
					title = title[:maxLen-3] + "..."
				}

				fmt.Fprintf(&b, "  %s %s%s",
					statusStyle.Render(statusIcon),
					s.styles.Paragraph.Render(title),
					agentLabel)
				b.WriteString("\n")

				// Line 2: progress bar completed/total
				barWidth := min(max(s.width-14, 4), 12)

				if task.TotalJobs > 0 {
					filled := (task.CompletedJobs * barWidth) / task.TotalJobs
					empty := barWidth - filled
					if filled > barWidth {
						filled = barWidth
						empty = 0
					}
					bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
					fmt.Fprintf(&b, "    %s %d/%d",
						statusStyle.Render(bar),
						task.CompletedJobs,
						task.TotalJobs)
				} else {
					bar := strings.Repeat("░", barWidth)
					fmt.Fprintf(&b, "    %s", s.styles.Muted.Render(bar))
				}
				b.WriteString("\n")

				// Line 3 (optional): current step when executing
				if (task.Status == StateExecuting || task.Status == StatePlanning) && task.CurrentStep != "" {
					stepMaxLen := max(s.width-10, 8)
					stepDesc := task.CurrentStep
					if len(stepDesc) > stepMaxLen {
						stepDesc = stepDesc[:stepMaxLen-3] + "..."
					}
					fmt.Fprintf(&b, "    -> %s\n", s.styles.Muted.Render(stepDesc))
				}

				// Line 3b (optional): review status when reviewing/rejected
				if task.ReviewStatus != "" && task.ReviewStatus != "approved" {
					reviewStyle := s.styles.Muted
					reviewIcon := "..."
					switch {
					case task.ReviewStatus == "reviewing...":
						reviewIcon = "..."
						reviewStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAmber))
					case strings.HasPrefix(task.ReviewStatus, "revision #"):
						reviewIcon = task.ReviewStatus
						reviewStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAmber))
					case task.ReviewStatus == "rejected":
						reviewIcon = "!"
						reviewStyle = s.styles.Error
					}
					fmt.Fprintf(&b, "    %s %s\n",
						reviewStyle.Render(reviewIcon),
						s.styles.Muted.Render("review"))
				}

				// Line 4 (optional): token usage when > 0
				if task.TokenUsage > 0 {
					fmt.Fprintf(&b, "    %s\n", s.styles.Muted.Render(formatTokenCount(task.TokenUsage)+" tok"))
				}
			}
		}
	}

	return b.String()
}

// renderPlanPanel renders the active plan's phases via the PlanView widget.
// The panel appears between Tasks and Recent Memory; it auto-expands when a
// non-empty phase list arrives (via SidebarDataMsg or SetPlanPhases) and shows
// "no active plan" when there is nothing to display.
func (s *SidebarModel) renderPlanPanel() string {
	var b strings.Builder

	b.WriteString(s.renderPanelHeader("Plan", PanelPlan))
	b.WriteString("\n")

	if s.expandedPanels[PanelPlan] {
		if s.planView == nil {
			b.WriteString(s.styles.Muted.Render("  no active plan"))
			b.WriteString("\n")
			return b.String()
		}
		// Indent each rendered line by two spaces so the widget aligns with
		// the other sidebar panels. PlanView.Render returns a string with no
		// leading indentation; we add it here to keep the widget itself
		// layout-agnostic.
		rendered := s.planView.Render()
		for _, line := range strings.Split(rendered, "\n") {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	return b.String()
}

// SetPlanPhases replaces the phase data shown in the Plan panel and expands
// the panel so the new data is visible. Pass nil/empty to clear it.
func (s *SidebarModel) SetPlanPhases(phases []components.PhaseRow) {
	if s.planView == nil {
		return
	}
	s.planView.SetPhases(phases)
	if len(phases) > 0 {
		s.expandedPanels[PanelPlan] = true
	}
}

func (s *SidebarModel) renderMemoryPanel() string {
	var b strings.Builder

	b.WriteString(s.renderPanelHeader("Recent Memory", PanelMemory))
	b.WriteString("\n")

	if s.expandedPanels[PanelMemory] {
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

				fmt.Fprintf(&b, "  %s %s",
					typeStyle.Render(fmt.Sprintf("[%s]", mem.Type)),
					s.styles.Paragraph.Render(preview))
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

	if s.expandedPanels[PanelMetrics] {
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

	if s.expandedPanels[PanelActivityFeed] {
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
				maxSummaryLen := max(s.width-18, 5) // Account for timestamp and spacing
				if len(summary) > maxSummaryLen {
					summary = summary[:maxSummaryLen-3] + "..."
				}

				fmt.Fprintf(&b, "  %s %s",
					s.styles.Muted.Render(timeStr),
					topicStyle.Render(summary))
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

// formatTokenCount formats a token count for compact display.
// >= 1,000,000 -> "1.2M", >= 1,000 -> "1.5K", otherwise -> "42"
func formatTokenCount(count int) string {
	if count >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(count)/1_000_000)
	}
	if count >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(count)/1_000)
	}
	return fmt.Sprintf("%d", count)
}
