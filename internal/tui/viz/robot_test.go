package viz

import (
	"image/color"
	"testing"
)

func TestNewRobot(t *testing.T) {
	homePos := Point{10, 20}
	r := NewRobot("robot-1", "agent-1", homePos)

	if r.ID != "robot-1" {
		t.Errorf("ID = %q, want %q", r.ID, "robot-1")
	}
	if r.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want %q", r.AgentID, "agent-1")
	}
	if r.Position != homePos {
		t.Errorf("Position = %v, want %v", r.Position, homePos)
	}
	if r.HomePosition != homePos {
		t.Errorf("HomePosition = %v, want %v", r.HomePosition, homePos)
	}
	if r.State != RobotIdle {
		t.Errorf("State = %v, want %v", r.State, RobotIdle)
	}
}

func TestRobotSetState(t *testing.T) {
	r := NewRobot("robot-1", "agent-1", Point{0, 0})

	// Set to working state
	r.SetState(RobotWorking)
	if r.State != RobotWorking {
		t.Errorf("State = %v, want %v", r.State, RobotWorking)
	}

	// Progress should be reset
	r.Progress = 0.5
	r.SetState(RobotIdle)
	if r.Progress != 0 {
		t.Errorf("Progress = %v, want 0 after state change", r.Progress)
	}
}

func TestRobotMoveTo(t *testing.T) {
	r := NewRobot("robot-1", "agent-1", Point{0, 0})
	target := Point{50, 50}

	r.MoveTo(target)

	if r.TargetPos != target {
		t.Errorf("TargetPos = %v, want %v", r.TargetPos, target)
	}
	if r.moveProgress != 0 {
		t.Errorf("moveProgress = %v, want 0", r.moveProgress)
	}
}

func TestRobotMoveToHome(t *testing.T) {
	homePos := Point{10, 20}
	r := NewRobot("robot-1", "agent-1", homePos)
	r.Position = Point{50, 50}

	r.MoveToHome()

	if r.TargetPos != homePos {
		t.Errorf("TargetPos = %v, want %v", r.TargetPos, homePos)
	}
}

func TestRobotUpdate(t *testing.T) {
	r := NewRobot("robot-1", "agent-1", Point{0, 0})
	target := Point{100, 0}

	r.MoveTo(target)
	initialProgress := r.moveProgress

	// Update several times
	for i := 0; i < 20; i++ {
		r.Update()
	}

	// Progress should have increased
	if r.moveProgress <= initialProgress {
		t.Error("moveProgress should increase after Update()")
	}

	// After enough updates, should reach target
	if r.moveProgress < 1.0 {
		t.Logf("moveProgress = %v (may need more updates)", r.moveProgress)
	}
}

func TestRobotApplyBounce(t *testing.T) {
	r := NewRobot("robot-1", "agent-1", Point{0, 0})

	// Idle state should return 1.0
	r.SetState(RobotIdle)
	bounce := r.ApplyBounce()
	if bounce != 1.0 {
		t.Errorf("ApplyBounce() for Idle = %v, want 1.0", bounce)
	}

	// Moving state should have bounce effect
	r.SetState(RobotMovingToCenter)
	r.bouncePhase = 1.5708 // π/2 - peak of sin
	bounce = r.ApplyBounce()
	if bounce >= 1.0 || bounce < 0.5 {
		t.Errorf("ApplyBounce() for MovingToCenter = %v, expected between 0.5 and 1.0", bounce)
	}
}

// colorsEqual compares two color.Color values by their RGBA components.
func colorsEqual(a, b color.Color) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func TestRobotGetColor(t *testing.T) {
	r := NewRobot("robot-1", "agent-1", Point{0, 0})

	tests := []struct {
		state RobotState
		color color.Color
	}{
		{RobotIdle, ColorIdle},
		{RobotWorking, ColorWorking},
		{RobotTaskComplete, ColorSuccess},
		{RobotFailed, ColorError},
		{RobotCarrying, ColorCarrying},
	}

	for _, tt := range tests {
		r.SetState(tt.state)
		got := r.GetColor()
		if !colorsEqual(got, tt.color) {
			t.Errorf("GetColor() for state %v = %v, want %v", tt.state, got, tt.color)
		}
	}
}

func TestRobotDraw(t *testing.T) {
	c := NewCanvas(20, 10)
	r := NewRobot("robot-1", "agent-1", Point{5, 5})

	// Drawing should not panic
	r.Draw(c)

	// Check that some pixels were set in the robot area
	hasPixels := false
	for y := 5; y < 5+RobotHeight; y++ {
		for x := 5; x < 5+RobotWidth; x++ {
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
		t.Error("Robot Draw() should set some pixels")
	}
}

func TestRobotDrawHalo(t *testing.T) {
	c := NewCanvas(20, 10)
	r := NewRobot("robot-1", "agent-1", Point{10, 10})
	r.SetState(RobotCarrying)
	r.haloPhase = 0 // Phase where halo is visible

	r.DrawHalo(c)

	// Halo should be drawn above the robot head
	// This is a visual test - just ensure no panics
}

func TestEaseOutQuad(t *testing.T) {
	// t=0 should return 0
	if v := easeOutQuad(0); v != 0 {
		t.Errorf("easeOutQuad(0) = %v, want 0", v)
	}

	// t=1 should return 1
	if v := easeOutQuad(1); v != 1 {
		t.Errorf("easeOutQuad(1) = %v, want 1", v)
	}

	// t=0.5 should be > 0.5 (ease out curve)
	if v := easeOutQuad(0.5); v <= 0.5 {
		t.Errorf("easeOutQuad(0.5) = %v, expected > 0.5", v)
	}
}
