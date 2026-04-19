package viz

import (
	"math"

	"github.com/charmbracelet/x/ansi"
)

// RobotState represents the current state of a robot agent.
type RobotState int

const (
	RobotIdle RobotState = iota
	RobotMovingToCenter
	RobotWorking
	RobotTaskComplete
	RobotDispatchingSubtask
	RobotCarrying
	RobotFailed
	RobotProblems
)

// Robot dimensions in Braille dots (1x1 character = 2x4 dots)
const (
	RobotWidth  = 2
	RobotHeight = 4
)

// Robot represents an animated robot agent in the visualization.
type Robot struct {
	ID           string
	AgentID      string
	Position     Point   // Current position in Braille coordinates
	HomePosition Point   // Rest position (corner)
	TargetPos    Point   // Animation target
	State        RobotState
	Progress     float64 // 0.0-1.0 for work progress fill

	// Animation state
	bouncePhase  float64 // Phase for bounce animation (0 to 2π)
	haloPhase    float64 // Phase for pulsing halo (0 to 2π)
	blinkOn      bool    // For blink animations (X, !)
	moveProgress float64 // 0.0-1.0 for position interpolation
}

// NewRobot creates a new robot at the given position.
func NewRobot(id, agentID string, homePos Point) *Robot {
	return &Robot{
		ID:           id,
		AgentID:      agentID,
		Position:     homePos,
		HomePosition: homePos,
		TargetPos:    homePos,
		State:        RobotIdle,
		Progress:     0,
		bouncePhase:  0,
		haloPhase:    0,
		blinkOn:      true,
	}
}

// SetState updates the robot state and resets animation phases.
func (r *Robot) SetState(state RobotState) {
	if r.State != state {
		r.State = state
		r.bouncePhase = 0
		r.haloPhase = 0
		r.Progress = 0
	}
}

// MoveTo starts an animation to the target position.
func (r *Robot) MoveTo(target Point) {
	r.TargetPos = target
	r.moveProgress = 0
}

// MoveToHome starts an animation back to the home position.
func (r *Robot) MoveToHome() {
	r.MoveTo(r.HomePosition)
}

// Update advances the robot's animation state by one frame.
func (r *Robot) Update() {
	// Advance bounce phase (complete cycle every ~30 frames at 12 FPS = ~2.5 sec)
	r.bouncePhase += 0.21
	if r.bouncePhase > 2*math.Pi {
		r.bouncePhase -= 2 * math.Pi
	}

	// Advance halo phase (faster pulse)
	r.haloPhase += 0.15
	if r.haloPhase > 2*math.Pi {
		r.haloPhase -= 2 * math.Pi
	}

	// Toggle blink every ~6 frames
	if int(r.bouncePhase*10)%12 < 6 {
		r.blinkOn = true
	} else {
		r.blinkOn = false
	}

	// Update position interpolation
	if r.moveProgress < 1.0 {
		r.moveProgress += 0.08 // ~12 frames to complete move
		if r.moveProgress > 1.0 {
			r.moveProgress = 1.0
		}

		// Interpolate position
		startX := float64(r.Position.X)
		startY := float64(r.Position.Y)
		targetX := float64(r.TargetPos.X)
		targetY := float64(r.TargetPos.Y)

		// Ease-out interpolation
		t := easeOutQuad(r.moveProgress)
		r.Position.X = int(startX + (targetX-startX)*t)
		r.Position.Y = int(startY + (targetY-startY)*t)
	}
}

// ApplyBounce returns the vertical scale factor for bounce animation.
func (r *Robot) ApplyBounce() float64 {
	switch r.State {
	case RobotMovingToCenter, RobotTaskComplete:
		// Bouncing robots compress vertically
		return 1.0 - 0.3*math.Sin(r.bouncePhase)
	default:
		return 1.0
	}
}

// GetColor returns the primary color for this robot based on state.
func (r *Robot) GetColor() ansi.Color {
	switch r.State {
	case RobotIdle:
		return ColorIdle
	case RobotMovingToCenter:
		return ColorIdle
	case RobotWorking:
		// Interpolate from idle to working color based on progress
		return ColorWorking
	case RobotTaskComplete:
		return ColorSuccess
	case RobotDispatchingSubtask:
		return ColorMuted
	case RobotCarrying:
		return ColorCarrying
	case RobotFailed, RobotProblems:
		return ColorError
	default:
		return ColorIdle
	}
}

// Draw renders the robot onto the canvas at its current position.
func (r *Robot) Draw(c *Canvas) {
	x := r.Position.X
	y := r.Position.Y

	color := r.GetColor()

	switch r.State {
	case RobotWorking:
		// Progressive fill from bottom to top
		r.drawWithFill(c, x, y, RobotHeight, color)
	case RobotFailed:
		// Draw body with blinking effect
		if r.blinkOn {
			r.drawBody(c, x, y, RobotHeight, ColorError)
		} else {
			r.drawBody(c, x, y, RobotHeight, ColorMuted)
		}
	case RobotProblems:
		// Draw body with blinking effect
		if r.blinkOn {
			r.drawBody(c, x, y, RobotHeight, ColorError)
		} else {
			r.drawBody(c, x, y, RobotHeight, color)
		}
	default:
		// Normal body
		r.drawBody(c, x, y, RobotHeight, color)
	}
}

// drawBody draws the basic robot shape.
func (r *Robot) drawBody(c *Canvas, x, y, height int, color ansi.Color) {
	// Simple filled rectangle for small robot (2x4 dots = 1 char)
	for py := 0; py < height; py++ {
		for px := 0; px < RobotWidth; px++ {
			c.SetPixel(x+px, y+py, true)
			c.SetColorAtPixel(x+px, y+py, color)
		}
	}
}

// drawWithFill draws the robot with a progress fill from bottom to top.
func (r *Robot) drawWithFill(c *Canvas, x, y, height int, color ansi.Color) {
	fillHeight := int(float64(height) * r.Progress)

	for py := 0; py < height; py++ {
		for px := 0; px < RobotWidth; px++ {
			c.SetPixel(x+px, y+py, true)

			// Fill from bottom: if py >= (height - fillHeight), use working color
			if py >= height-fillHeight {
				c.SetColorAtPixel(x+px, y+py, ColorWorking)
			} else {
				c.SetColorAtPixel(x+px, y+py, ColorIdle)
			}
		}
	}
}

// drawX draws an X over the robot body.
func (r *Robot) drawX(c *Canvas, x, y, height int, color ansi.Color) {
	// Draw X in the center of the body
	centerX := x + RobotWidth/2
	centerY := y + height/2

	// Small X pattern
	for i := -1; i <= 1; i++ {
		c.SetPixel(centerX+i, centerY+i, true)
		c.SetColorAtPixel(centerX+i, centerY+i, color)
		c.SetPixel(centerX+i, centerY-i, true)
		c.SetColorAtPixel(centerX+i, centerY-i, color)
	}
}

// drawExclamation draws a ! symbol in the robot body.
func (r *Robot) drawExclamation(c *Canvas, x, y, height int, color ansi.Color) {
	centerX := x + RobotWidth/2
	centerY := y + height/2

	// Exclamation mark: vertical line + dot below
	for i := -2; i <= 0; i++ {
		c.SetPixel(centerX, centerY+i, true)
		c.SetColorAtPixel(centerX, centerY+i, color)
	}
	c.SetPixel(centerX, centerY+2, true)
	c.SetColorAtPixel(centerX, centerY+2, color)
}

// DrawHalo draws a pulsing arc above the robot head.
func (r *Robot) DrawHalo(c *Canvas) {
	x := r.Position.X + RobotWidth/2
	y := r.Position.Y - 2 // Above head

	// Pulsing intensity
	intensity := 0.5 + 0.5*math.Sin(r.haloPhase)

	if intensity > 0.3 {
		// Draw a small arc above the head
		radius := 3
		// Arc from roughly -π/4 to -3π/4 (top arc)
		startAngle := -2.35 // -3π/4
		endAngle := -0.78   // -π/4

		c.DrawArc(x, y, radius, startAngle, endAngle, ColorCarrying)
	}
}

// easeOutQuad provides smooth deceleration for animations.
func easeOutQuad(t float64) float64 {
	return 1 - (1-t)*(1-t)
}
