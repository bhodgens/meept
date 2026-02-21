package viz

// Dispatcher represents the central dispatch entity.
// It moves linearly (no bounce) to collect results from robots.
type Dispatcher struct {
	Position     Point
	HomePosition Point
	TargetPos    Point
	Moving       bool
	moveProgress float64
}

// Dispatcher dimensions in Braille dots (2x1 characters = 4x4 dots)
const (
	DispatcherWidth  = 4
	DispatcherHeight = 4
)

// NewDispatcher creates a new dispatcher at the given center position.
func NewDispatcher(centerPos Point) *Dispatcher {
	return &Dispatcher{
		Position:     centerPos,
		HomePosition: centerPos,
		TargetPos:    centerPos,
		Moving:       false,
		moveProgress: 1.0,
	}
}

// MoveTo starts linear movement to the target position.
func (d *Dispatcher) MoveTo(target Point) {
	if d.Position.X == target.X && d.Position.Y == target.Y {
		return
	}
	d.TargetPos = target
	d.Moving = true
	d.moveProgress = 0
}

// MoveToHome moves the dispatcher back to its home position.
func (d *Dispatcher) MoveToHome() {
	d.MoveTo(d.HomePosition)
}

// Update advances the dispatcher's animation state.
func (d *Dispatcher) Update() {
	if !d.Moving {
		return
	}

	// Linear movement (faster than robots)
	d.moveProgress += 0.12
	if d.moveProgress >= 1.0 {
		d.moveProgress = 1.0
		d.Moving = false
		d.Position = d.TargetPos
		return
	}

	// Linear interpolation
	startX := float64(d.HomePosition.X)
	startY := float64(d.HomePosition.Y)
	if d.moveProgress > 0 {
		// Continue from current position when moving
		startX = float64(d.Position.X)
		startY = float64(d.Position.Y)
	}
	targetX := float64(d.TargetPos.X)
	targetY := float64(d.TargetPos.Y)

	d.Position.X = int(startX + (targetX-startX)*d.moveProgress)
	d.Position.Y = int(startY + (targetY-startY)*d.moveProgress)
}

// Draw renders the dispatcher block onto the canvas.
// The dispatcher is rendered as a distinct central block (3x2 characters = 6x8 dots).
func (d *Dispatcher) Draw(c *Canvas) {
	x := d.Position.X
	y := d.Position.Y

	// Draw a solid block with the dispatcher color
	c.DrawFilledRect(x, y, DispatcherWidth, DispatcherHeight, ColorDispatcher)
}

// CenterPosition returns the center point of the dispatcher for line drawing.
func (d *Dispatcher) CenterPosition() Point {
	return Point{
		X: d.Position.X + DispatcherWidth/2,
		Y: d.Position.Y + DispatcherHeight/2,
	}
}
