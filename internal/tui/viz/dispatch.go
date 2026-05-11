package viz

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// VizTickMsg signals a visualization frame update.
type VizTickMsg struct{}

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
}

// NewDispatchViz creates a new dispatch visualization with the given width.
// Height is calculated to give more vertical space for the animation.
func NewDispatchViz(width int) *DispatchViz {
	// Calculate height - visualization (compact, width/5 ratio)
	height := max(width/5, 4)

	v := &DispatchViz{
		width:  width,
		height: height,
		fps:    12,
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
		if i < len(agents) {
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
		} else if i < len(workers) {
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
		} else if r.State != RobotIdle {
			// No data for this robot, set to idle
			r.SetState(RobotIdle)
			r.MoveToHome()
		}
	}
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
