package viz

import (
	"context"
	"log/slog"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// VizTickMsg signals a visualization frame update.
type VizTickMsg struct{} //nolint:revive // stutter with package name is intentional for API clarity

// --- Typed event types for agent events (mirrors internal/agent/events.go) ---

// AgentEventType identifies a typed agent event.
type AgentEventType string

const (
	AgentEventTurnStart           AgentEventType = "turn_start"
	AgentEventTurnEnd             AgentEventType = "turn_end"
	AgentEventToolExecutionStart  AgentEventType = "tool_execution_start"
	AgentEventToolExecutionUpdate AgentEventType = "tool_execution_update"
	AgentEventToolExecutionEnd    AgentEventType = "tool_execution_end"
)

// AgentEventData is the interface all event payloads implement.
type AgentEventData interface {
	agentEventData()
}

// AgentEvent is the envelope for typed agent events.
type AgentEvent struct {
	Type           AgentEventType `json:"type"`
	Timestamp      time.Time      `json:"timestamp"`
	AgentID        string         `json:"agent_id"`
	ConversationID string         `json:"conversation_id"`
	Iteration      int            `json:"iteration"`
	Data           AgentEventData `json:"data"`
}

// TurnStartData is emitted at the beginning of each loop iteration.
type TurnStartData struct {
	TurnNumber       int `json:"turn_number"`
	TotalTokensSoFar int `json:"total_tokens_so_far"`
	MessagesCount    int `json:"messages_count"`
	ToolCount        int `json:"tool_count"`
}

func (TurnStartData) agentEventData() {}

// TurnEndData is emitted at the end of each loop iteration.
type TurnEndData struct {
	TurnNumber     int    `json:"turn_number"`
	HadToolCalls   bool   `json:"had_tool_calls"`
	ToolCallCount  int    `json:"tool_call_count"`
	ResponseTokens int    `json:"response_tokens"`
	StoppedBy      string `json:"stopped_by"`
}

func (TurnEndData) agentEventData() {}

// ToolExecutionStartData is emitted before a tool is executed.
type ToolExecutionStartData struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Arguments  string `json:"arguments"`
}

func (ToolExecutionStartData) agentEventData() {}

// ToolExecutionUpdateData is emitted during tool execution progress.
type ToolExecutionUpdateData struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Status     string `json:"status"`
	Detail     string `json:"detail"`
}

func (ToolExecutionUpdateData) agentEventData() {}

// ToolExecutionEndData is emitted after a tool execution completes.
type ToolExecutionEndData struct {
	ToolCallID  string        `json:"tool_call_id"`
	ToolName    string        `json:"tool_name"`
	Success     bool          `json:"success"`
	Result      string        `json:"result"`
	Error       string        `json:"error,omitempty"`
	Cached      bool          `json:"cached"`
	Duration    time.Duration `json:"duration"`
	Blocked     bool          `json:"blocked"`
	BlockReason string        `json:"block_reason,omitempty"`
}

func (ToolExecutionEndData) agentEventData() {}

// TypedEventEmitter is the interface satisfied by agent.EventEmitter.
// Defined here to avoid an import cycle.
type TypedEventEmitter interface {
	On(eventType AgentEventType, name string, listener func(ctx context.Context, event AgentEvent))
	OnAsync(eventType AgentEventType, name string, listener func(ctx context.Context, event AgentEvent))
}

// AgentActivityData represents agent data for visualization sync.
type AgentActivityData struct {
	AgentID   string
	AgentName string
	State     string // reasoning, tool_exec, waiting
	Progress  float64
}

// WorkerData represents worker data for visualization sync.
type WorkerData struct {
	ID           string
	State        string // idle, claiming, processing, error
	CurrentJobID string
}

// DispatchViz is the main visualization model showing robots around a central dispatcher.
type DispatchViz struct {
	width      int // Width in characters
	height     int // Height in characters (square)
	canvas     *Canvas
	robots     []*Robot
	dispatcher *Dispatcher
	center     Point
	frame      int
	fps        int

	// pendingEventsMu guards the pending event state written by sync listeners.
	pendingEventsMu sync.Mutex
	// pendingActivity holds agent activity updates that have arrived via typed
	// events since the last Update tick. They are flushed into robot states
	// during Update so that rendering stays on the Bubble Tea tick cadence.
	pendingActivity map[string]*agentActivityEntry
	logger          *slog.Logger
}

// agentActivityEntry tracks the latest activity for a single agent.
type agentActivityEntry struct {
	agentID  string
	state    string // reasoning, tool_exec, waiting, complete, error
	progress float64
}

// NewDispatchViz creates a new dispatch visualization with the given width.
// Height is calculated to give more vertical space for the animation.
func NewDispatchViz(width int) *DispatchViz {
	// Calculate height - visualization (compact, width/5 ratio)
	height := max(width/5, 4)

	v := &DispatchViz{
		width:           width,
		height:          height,
		fps:             12,
		pendingActivity: make(map[string]*agentActivityEntry),
		logger:          slog.Default().With("component", "dispatch-viz"),
	}

	v.initCanvas()
	return v
}

// initCanvas initializes the canvas and entities based on current dimensions.
func (v *DispatchViz) initCanvas() {
	innerWidth := v.width
	innerHeight := v.height
	if innerWidth < 4 {
		innerWidth = 4
	}
	if innerHeight < 2 {
		innerHeight = 2
	}

	v.canvas = NewCanvas(innerWidth, innerHeight)

	// Calculate positions
	pixelWidth := v.canvas.PixelWidth()
	pixelHeight := v.canvas.PixelHeight()

	// Center position for dispatcher
	v.center = Point{
		X: (pixelWidth - DispatcherWidth) / 2,
		Y: (pixelHeight - DispatcherHeight) / 2,
	}

	v.dispatcher = NewDispatcher(v.center)

	// Robot positions in corners
	// Leave margin of 1 pixel from edge for compact layout
	margin := 1
	positions := []Point{
		{margin, margin}, // Top-left
		{pixelWidth - RobotWidth - margin, margin},                             // Top-right
		{margin, pixelHeight - RobotHeight - margin},                           // Bottom-left
		{pixelWidth - RobotWidth - margin, pixelHeight - RobotHeight - margin}, // Bottom-right
	}

	v.robots = make([]*Robot, 4)
	agentIDs := []string{"agent-1", "agent-2", "agent-3", "agent-4"}
	for i, pos := range positions {
		v.robots[i] = NewRobot(agentIDs[i], agentIDs[i], pos)
	}
}

// SetSize updates the visualization dimensions.
func (v *DispatchViz) SetSize(width int) {
	if width == v.width {
		return
	}

	v.width = width
	v.height = max(width/5, 4)

	// Preserve robot states before reinitializing
	oldStates := make([]RobotState, len(v.robots))
	oldProgress := make([]float64, len(v.robots))
	for i, r := range v.robots {
		if r != nil {
			oldStates[i] = r.State
			oldProgress[i] = r.Progress
		}
	}

	v.initCanvas()

	// Restore states
	for i, r := range v.robots {
		if r != nil && i < len(oldStates) {
			r.State = oldStates[i]
			r.Progress = oldProgress[i]
		}
	}
}

// Init returns the command to start the animation tick.
func (v *DispatchViz) Init() tea.Cmd {
	return v.tickCmd()
}

// tickCmd returns a command that sends a tick after the frame delay.
func (v *DispatchViz) tickCmd() tea.Cmd {
	return tea.Tick(time.Second/time.Duration(v.fps), func(t time.Time) tea.Msg {
		return VizTickMsg{}
	})
}

// Update handles messages and returns the next tick command.
func (v *DispatchViz) Update(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(VizTickMsg); ok {
		v.frame++

		// Flush any pending typed-event activity into robot states.
		v.flushPendingActivity()

		// Update all entities
		for _, r := range v.robots {
			r.Update()
		}
		v.dispatcher.Update()

		// Return next tick
		return v.tickCmd()
	}
	return nil
}

// SyncWithData synchronizes robot states with actual agent/worker data.
func (v *DispatchViz) SyncWithData(agents []AgentActivityData, workers []WorkerData) {
	// Map agent states to robots
	for i, r := range v.robots {
		switch {
		case i < len(agents):
			agent := agents[i]
			r.AgentID = agent.AgentID

			// Map agent state to robot state
			newState := mapAgentState(agent.State)
			if r.State != newState {
				r.SetState(newState)
				switch newState {
				case RobotMovingToCenter, RobotDispatchingSubtask:
					// Move toward center
					r.MoveTo(v.center)
				case RobotTaskComplete:
					// Move back home
					r.MoveToHome()
				}
			}
			r.Progress = agent.Progress
		case i < len(workers):
			worker := workers[i-len(agents)]
			r.AgentID = worker.ID

			// Map worker state to robot state
			newState := mapWorkerState(worker.State)
			if r.State != newState {
				r.SetState(newState)
				if newState == RobotMovingToCenter {
					r.MoveTo(v.center)
				}
			}
		default:
			if r.State != RobotIdle {
				// No data for this robot, set to idle
				r.SetState(RobotIdle)
				r.MoveToHome()
			}
		}
	}
}

// flushPendingActivity drains pending typed-event activity and applies it to
// the matching robots. Called once per animation tick inside Update so that
// visual state stays consistent with the Bubble Tea update cadence.
func (v *DispatchViz) flushPendingActivity() {
	v.pendingEventsMu.Lock()
	pending := v.pendingActivity
	v.pendingActivity = make(map[string]*agentActivityEntry, len(pending))
	v.pendingEventsMu.Unlock()

	if len(pending) == 0 {
		return
	}

	// Build a slice of AgentActivityData from pending entries for reuse
	// with the existing SyncWithData path.
	activities := make([]AgentActivityData, 0, len(pending))
	for _, entry := range pending {
		activities = append(activities, AgentActivityData{
			AgentID:  entry.agentID,
			State:    entry.state,
			Progress: entry.progress,
		})
	}

	// Match activities to robots by agent ID.
	for _, r := range v.robots {
		for _, act := range activities {
			if r.AgentID == act.AgentID {
				newState := mapAgentState(act.State)
				if r.State != newState {
					r.SetState(newState)
					switch newState {
					case RobotMovingToCenter, RobotDispatchingSubtask:
						r.MoveTo(v.center)
					case RobotTaskComplete, RobotIdle:
						r.MoveToHome()
					}
				}
				r.Progress = act.Progress
				break
			}
		}
	}
}

// pushActivity records a pending activity update from a typed event.
func (v *DispatchViz) pushActivity(agentID, state string, progress float64) {
	v.pendingEventsMu.Lock()
	v.pendingActivity[agentID] = &agentActivityEntry{
		agentID:  agentID,
		state:    state,
		progress: progress,
	}
	v.pendingEventsMu.Unlock()
}

// RegisterEventListeners subscribes the visualization to typed agent events via
// an EventEmitter. Sync listeners are used so that state mutations are visible
// before the next animation tick renders.
//
// The existing SyncWithData path continues to work for callers that push data
// externally (e.g., from RPC polling). Typed events are additive.
func (v *DispatchViz) RegisterEventListeners(emitter TypedEventEmitter) {
	// Turn start: agent begins reasoning.
	emitter.On(AgentEventTurnStart, "viz.turn-start",
		func(ctx context.Context, event AgentEvent) {
			v.pushActivity(event.AgentID, "reasoning", 0)
		},
	)

	// Turn end: agent finished one iteration.
	emitter.On(AgentEventTurnEnd, "viz.turn-end",
		func(ctx context.Context, event AgentEvent) {
			data, ok := event.Data.(TurnEndData)
			if !ok {
				return
			}
			// If the turn had tool calls, transition to tool_exec briefly;
			// otherwise the agent is completing (will be overridden by next
			// turn_start or session_end).
			state := "waiting"
			if data.HadToolCalls {
				state = "tool_exec"
			}
			v.pushActivity(event.AgentID, state, 1.0)
		},
	)

	// Tool execution start: agent is executing a tool.
	emitter.On(AgentEventToolExecutionStart, "viz.tool-start",
		func(ctx context.Context, event AgentEvent) {
			v.pushActivity(event.AgentID, "tool_exec", 0)
		},
	)

	// Tool execution update: progress during tool execution.
	emitter.On(AgentEventToolExecutionUpdate, "viz.tool-update",
		func(ctx context.Context, event AgentEvent) {
			data, ok := event.Data.(ToolExecutionUpdateData)
			if !ok {
				return
			}
			progress := 0.5 // Default mid-progress for generic updates
			switch data.Status {
			case "running", "in_progress":
				progress = 0.3
			case "nearly_done", "finishing":
				progress = 0.8
			case "streaming":
				progress = 0.6
			}
			v.pushActivity(event.AgentID, "tool_exec", progress)
		},
	)

	// Tool execution end: tool completed (success or failure).
	emitter.On(AgentEventToolExecutionEnd, "viz.tool-end",
		func(ctx context.Context, event AgentEvent) {
			data, ok := event.Data.(ToolExecutionEndData)
			if !ok {
				return
			}
			if !data.Success {
				v.pushActivity(event.AgentID, "error", 1.0)
				return
			}
			// Tool done, agent transitions back to reasoning for the next turn.
			v.pushActivity(event.AgentID, "reasoning", 1.0)
		},
	)
}

// mapAgentState converts an agent state string to a RobotState.
func mapAgentState(state string) RobotState {
	switch state {
	case "reasoning":
		return RobotWorking
	case "tool_exec":
		return RobotCarrying
	case "waiting":
		return RobotIdle
	case "complete", "done":
		return RobotTaskComplete
	case "error", "failed":
		return RobotFailed
	default:
		return RobotIdle
	}
}

// mapWorkerState converts a worker state string to a RobotState.
func mapWorkerState(state string) RobotState {
	switch state {
	case "idle":
		return RobotIdle
	case "claiming":
		return RobotMovingToCenter
	case "processing":
		return RobotWorking
	case "error":
		return RobotFailed
	default:
		return RobotIdle
	}
}

// View renders the visualization to a string.
func (v *DispatchViz) View() string {
	// Clear canvas
	v.canvas.Clear()

	// Draw dotted lines from robots to center
	dispatcherCenter := v.dispatcher.CenterPosition()
	for _, r := range v.robots {
		robotCenter := Point{
			X: r.Position.X + RobotWidth/2,
			Y: r.Position.Y + RobotHeight/2,
		}
		v.canvas.DrawDottedLine(robotCenter, dispatcherCenter, 2, 3)
	}

	// Draw dispatcher block
	v.dispatcher.Draw(v.canvas)

	// Draw robots
	for _, r := range v.robots {
		r.Draw(v.canvas)
	}

	// Render canvas content
	return v.canvas.Render()
}

// SetRobotState manually sets a robot's state (for testing/demo).
func (v *DispatchViz) SetRobotState(index int, state RobotState) {
	if index >= 0 && index < len(v.robots) {
		v.robots[index].SetState(state)
		switch state {
		case RobotMovingToCenter, RobotDispatchingSubtask:
			v.robots[index].MoveTo(v.center)
		case RobotTaskComplete, RobotIdle:
			v.robots[index].MoveToHome()
		}
	}
}

// SetRobotProgress sets the progress for a working robot.
func (v *DispatchViz) SetRobotProgress(index int, progress float64) {
	if index >= 0 && index < len(v.robots) {
		v.robots[index].Progress = progress
	}
}

// Width returns the visualization width.
func (v *DispatchViz) Width() int {
	return v.width
}

// Height returns the visualization height.
func (v *DispatchViz) Height() int {
	return v.height
}
