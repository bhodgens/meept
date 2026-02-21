package viz

import (
	"testing"
)

func TestNewDispatchViz(t *testing.T) {
	v := NewDispatchViz(30)

	if v.Width() != 30 {
		t.Errorf("Width() = %d, want 30", v.Width())
	}
	if v.Height() <= 0 {
		t.Errorf("Height() = %d, expected > 0", v.Height())
	}
	if v.canvas == nil {
		t.Error("canvas should be initialized")
	}
	if v.dispatcher == nil {
		t.Error("dispatcher should be initialized")
	}
	if len(v.robots) != 4 {
		t.Errorf("len(robots) = %d, want 4", len(v.robots))
	}
}

func TestDispatchVizSetSize(t *testing.T) {
	v := NewDispatchViz(30)

	// Set robot states before resize
	v.robots[0].SetState(RobotWorking)
	v.robots[0].Progress = 0.5

	// Resize
	v.SetSize(40)

	if v.Width() != 40 {
		t.Errorf("Width() = %d, want 40", v.Width())
	}

	// State should be preserved
	if v.robots[0].State != RobotWorking {
		t.Error("Robot state should be preserved after resize")
	}
	if v.robots[0].Progress != 0.5 {
		t.Errorf("Robot progress = %v, want 0.5", v.robots[0].Progress)
	}
}

func TestDispatchVizInit(t *testing.T) {
	v := NewDispatchViz(30)
	cmd := v.Init()

	if cmd == nil {
		t.Error("Init() should return a tick command")
	}
}

func TestDispatchVizUpdate(t *testing.T) {
	v := NewDispatchViz(30)

	// Update with tick message
	cmd := v.Update(VizTickMsg{})

	if cmd == nil {
		t.Error("Update(VizTickMsg) should return next tick command")
	}

	// Frame should increment
	if v.frame != 1 {
		t.Errorf("frame = %d, want 1 after Update", v.frame)
	}
}

func TestDispatchVizView(t *testing.T) {
	v := NewDispatchViz(30)

	output := v.View()

	if output == "" {
		t.Error("View() should not return empty string")
	}

	// Should contain Braille characters
	hasBraille := false
	for _, r := range output {
		if r >= '\u2800' && r <= '\u28FF' {
			hasBraille = true
			break
		}
	}

	if !hasBraille {
		t.Error("View() output should contain Braille characters")
	}
}

func TestDispatchVizSyncWithData(t *testing.T) {
	v := NewDispatchViz(30)

	agents := []AgentActivityData{
		{AgentID: "agent-1", State: "reasoning", Progress: 0.5},
		{AgentID: "agent-2", State: "tool_exec", Progress: 0.3},
	}

	workers := []WorkerData{
		{ID: "worker-1", State: "idle"},
	}

	v.SyncWithData(agents, workers)

	// Check that robot states were updated
	if v.robots[0].State != RobotWorking {
		t.Errorf("Robot 0 state = %v, want RobotWorking", v.robots[0].State)
	}
	if v.robots[1].State != RobotCarrying {
		t.Errorf("Robot 1 state = %v, want RobotCarrying", v.robots[1].State)
	}
}

func TestDispatchVizSetRobotState(t *testing.T) {
	v := NewDispatchViz(30)

	v.SetRobotState(0, RobotWorking)
	if v.robots[0].State != RobotWorking {
		t.Errorf("Robot 0 state = %v, want RobotWorking", v.robots[0].State)
	}

	// Out of bounds should not panic
	v.SetRobotState(-1, RobotWorking)
	v.SetRobotState(100, RobotWorking)
}

func TestDispatchVizSetRobotProgress(t *testing.T) {
	v := NewDispatchViz(30)

	v.SetRobotProgress(0, 0.75)
	if v.robots[0].Progress != 0.75 {
		t.Errorf("Robot 0 progress = %v, want 0.75", v.robots[0].Progress)
	}

	// Out of bounds should not panic
	v.SetRobotProgress(-1, 0.5)
	v.SetRobotProgress(100, 0.5)
}

func TestMapAgentState(t *testing.T) {
	tests := []struct {
		input    string
		expected RobotState
	}{
		{"reasoning", RobotWorking},
		{"tool_exec", RobotCarrying},
		{"waiting", RobotIdle},
		{"complete", RobotTaskComplete},
		{"done", RobotTaskComplete},
		{"error", RobotFailed},
		{"failed", RobotFailed},
		{"unknown", RobotIdle},
	}

	for _, tt := range tests {
		result := mapAgentState(tt.input)
		if result != tt.expected {
			t.Errorf("mapAgentState(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestMapWorkerState(t *testing.T) {
	tests := []struct {
		input    string
		expected RobotState
	}{
		{"idle", RobotIdle},
		{"claiming", RobotMovingToCenter},
		{"processing", RobotWorking},
		{"error", RobotFailed},
		{"unknown", RobotIdle},
	}

	for _, tt := range tests {
		result := mapWorkerState(tt.input)
		if result != tt.expected {
			t.Errorf("mapWorkerState(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestDispatcher(t *testing.T) {
	center := Point{20, 15}
	d := NewDispatcher(center)

	if d.Position != center {
		t.Errorf("Position = %v, want %v", d.Position, center)
	}
	if d.HomePosition != center {
		t.Errorf("HomePosition = %v, want %v", d.HomePosition, center)
	}
	if d.Moving {
		t.Error("Dispatcher should not be moving initially")
	}
}

func TestDispatcherMoveTo(t *testing.T) {
	d := NewDispatcher(Point{20, 15})
	target := Point{30, 25}

	d.MoveTo(target)

	if !d.Moving {
		t.Error("Dispatcher should be moving after MoveTo")
	}
	if d.TargetPos != target {
		t.Errorf("TargetPos = %v, want %v", d.TargetPos, target)
	}
}

func TestDispatcherUpdate(t *testing.T) {
	d := NewDispatcher(Point{20, 15})
	target := Point{30, 25}

	d.MoveTo(target)

	// Update several times
	for i := 0; i < 20; i++ {
		d.Update()
	}

	// Should have reached target
	if d.Moving {
		t.Error("Dispatcher should stop moving after reaching target")
	}
	if d.Position != target {
		t.Errorf("Position = %v, want %v after reaching target", d.Position, target)
	}
}

func TestDispatcherDraw(t *testing.T) {
	c := NewCanvas(30, 15)
	d := NewDispatcher(Point{10, 10})

	// Should not panic
	d.Draw(c)

	// Check that some pixels were set
	hasPixels := false
	for y := 10; y < 10+DispatcherHeight; y++ {
		for x := 10; x < 10+DispatcherWidth; x++ {
			if c.GetPixel(x, y) {
				hasPixels = true
				break
			}
		}
		if hasPixels {
			break
		}
	}

	if !hasPixels {
		t.Error("Dispatcher Draw() should set some pixels")
	}
}

func TestDispatcherCenterPosition(t *testing.T) {
	d := NewDispatcher(Point{10, 10})
	center := d.CenterPosition()

	expectedCenter := Point{
		X: 10 + DispatcherWidth/2,
		Y: 10 + DispatcherHeight/2,
	}

	if center != expectedCenter {
		t.Errorf("CenterPosition() = %v, want %v", center, expectedCenter)
	}
}
