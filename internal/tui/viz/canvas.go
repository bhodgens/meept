package viz

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Point represents a coordinate in pixel (Braille dot) space.
type Point struct {
	X, Y int
}

// Canvas provides a Braille-based pixel canvas for rendering graphics.
// Each character cell contains a 2x4 grid of dots (U+2800 block).
type Canvas struct {
	charWidth  int                 // Width in characters
	charHeight int                 // Height in characters
	pixels     [][]bool            // Pixel data [y][x] in dot coordinates
	colors     [][]lipgloss.Color  // Per-character color [charY][charX]
}

// Braille dot weights for the 2x4 grid:
//
//	Col 0  Col 1
//	  0      3     weights: 0x01, 0x08
//	  1      4              0x02, 0x10
//	  2      5              0x04, 0x20
//	  6      7              0x40, 0x80
var brailleWeights = [4][2]rune{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

const brailleBase = '\u2800'

// NewCanvas creates a new canvas with the given character dimensions.
// Pixel dimensions are charWidth*2 x charHeight*4.
func NewCanvas(charWidth, charHeight int) *Canvas {
	pixelWidth := charWidth * 2
	pixelHeight := charHeight * 4

	pixels := make([][]bool, pixelHeight)
	for y := range pixels {
		pixels[y] = make([]bool, pixelWidth)
	}

	colors := make([][]lipgloss.Color, charHeight)
	for y := range colors {
		colors[y] = make([]lipgloss.Color, charWidth)
		for x := range colors[y] {
			colors[y][x] = ColorIdle // Default color
		}
	}

	return &Canvas{
		charWidth:  charWidth,
		charHeight: charHeight,
		pixels:     pixels,
		colors:     colors,
	}
}

// PixelWidth returns the width in pixels (dots).
func (c *Canvas) PixelWidth() int {
	return c.charWidth * 2
}

// PixelHeight returns the height in pixels (dots).
func (c *Canvas) PixelHeight() int {
	return c.charHeight * 4
}

// CharWidth returns the width in characters.
func (c *Canvas) CharWidth() int {
	return c.charWidth
}

// CharHeight returns the height in characters.
func (c *Canvas) CharHeight() int {
	return c.charHeight
}

// Clear resets all pixels and colors.
func (c *Canvas) Clear() {
	for y := range c.pixels {
		for x := range c.pixels[y] {
			c.pixels[y][x] = false
		}
	}
	for y := range c.colors {
		for x := range c.colors[y] {
			c.colors[y][x] = ColorIdle
		}
	}
}

// SetPixel sets a single pixel at the given coordinates.
func (c *Canvas) SetPixel(x, y int, on bool) {
	if x < 0 || x >= c.PixelWidth() || y < 0 || y >= c.PixelHeight() {
		return
	}
	c.pixels[y][x] = on
}

// GetPixel returns the state of a pixel.
func (c *Canvas) GetPixel(x, y int) bool {
	if x < 0 || x >= c.PixelWidth() || y < 0 || y >= c.PixelHeight() {
		return false
	}
	return c.pixels[y][x]
}

// SetColorAt sets the color for a character cell.
func (c *Canvas) SetColorAt(charX, charY int, color lipgloss.Color) {
	if charX < 0 || charX >= c.charWidth || charY < 0 || charY >= c.charHeight {
		return
	}
	c.colors[charY][charX] = color
}

// SetColorAtPixel sets the color for the character cell containing the given pixel.
func (c *Canvas) SetColorAtPixel(x, y int, color lipgloss.Color) {
	charX := x / 2
	charY := y / 4
	c.SetColorAt(charX, charY, color)
}

// DrawRect draws a rectangle (filled or outline).
func (c *Canvas) DrawRect(x, y, w, h int, filled bool) {
	for py := y; py < y+h; py++ {
		for px := x; px < x+w; px++ {
			if filled {
				c.SetPixel(px, py, true)
			} else {
				// Only draw border
				if px == x || px == x+w-1 || py == y || py == y+h-1 {
					c.SetPixel(px, py, true)
				}
			}
		}
	}
}

// DrawFilledRect draws a filled rectangle with a specific color.
func (c *Canvas) DrawFilledRect(x, y, w, h int, color lipgloss.Color) {
	c.DrawRect(x, y, w, h, true)
	// Set color for all character cells covered by the rectangle
	startCharX := x / 2
	startCharY := y / 4
	endCharX := (x + w - 1) / 2
	endCharY := (y + h - 1) / 4
	for cy := startCharY; cy <= endCharY; cy++ {
		for cx := startCharX; cx <= endCharX; cx++ {
			c.SetColorAt(cx, cy, color)
		}
	}
}

// DrawDottedLine draws a dotted line between two points.
func (c *Canvas) DrawDottedLine(from, to Point, dashLen, gapLen int) {
	dx := to.X - from.X
	dy := to.Y - from.Y

	// Calculate line length
	length := 0
	if abs(dx) > abs(dy) {
		length = abs(dx)
	} else {
		length = abs(dy)
	}
	if length == 0 {
		return
	}

	// Step along the line
	pattern := dashLen + gapLen
	for i := 0; i <= length; i++ {
		// Check if we're in a dash segment
		if i%pattern < dashLen {
			x := from.X + (dx*i)/length
			y := from.Y + (dy*i)/length
			c.SetPixel(x, y, true)
			c.SetColorAtPixel(x, y, ColorDotLine)
		}
	}
}

// DrawLine draws a solid line between two points using Bresenham's algorithm.
func (c *Canvas) DrawLine(from, to Point, color lipgloss.Color) {
	x0, y0 := from.X, from.Y
	x1, y1 := to.X, to.Y

	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy

	for {
		c.SetPixel(x0, y0, true)
		c.SetColorAtPixel(x0, y0, color)

		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			if x0 == x1 {
				break
			}
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			if y0 == y1 {
				break
			}
			err += dx
			y0 += sy
		}
	}
}

// DrawCircle draws a circle outline.
func (c *Canvas) DrawCircle(cx, cy, r int, color lipgloss.Color) {
	x := r
	y := 0
	err := 0

	for x >= y {
		c.setCirclePoints(cx, cy, x, y, color)
		y++
		err += 1 + 2*y
		if 2*(err-x)+1 > 0 {
			x--
			err += 1 - 2*x
		}
	}
}

func (c *Canvas) setCirclePoints(cx, cy, x, y int, color lipgloss.Color) {
	points := []Point{
		{cx + x, cy + y},
		{cx - x, cy + y},
		{cx + x, cy - y},
		{cx - x, cy - y},
		{cx + y, cy + x},
		{cx - y, cy + x},
		{cx + y, cy - x},
		{cx - y, cy - x},
	}
	for _, p := range points {
		c.SetPixel(p.X, p.Y, true)
		c.SetColorAtPixel(p.X, p.Y, color)
	}
}

// DrawArc draws a partial arc (for halo effect).
func (c *Canvas) DrawArc(cx, cy, r int, startAngle, endAngle float64, color lipgloss.Color) {
	// Simple arc approximation using discrete points
	import_math := func(x float64) float64 { return x }
	_ = import_math // placeholder - we'll use integer math

	// Draw points around the arc using integer approximation
	steps := r * 2
	if steps < 8 {
		steps = 8
	}
	for i := 0; i <= steps; i++ {
		// Map i to angle range
		angle := startAngle + (endAngle-startAngle)*float64(i)/float64(steps)
		// Use lookup table approximation for sin/cos
		px := cx + int(float64(r)*cosApprox(angle))
		py := cy + int(float64(r)*sinApprox(angle))
		c.SetPixel(px, py, true)
		c.SetColorAtPixel(px, py, color)
	}
}

// Simple sin/cos approximations using Taylor series
func sinApprox(x float64) float64 {
	// Normalize to -PI to PI range
	for x > 3.14159 {
		x -= 6.28318
	}
	for x < -3.14159 {
		x += 6.28318
	}
	// Taylor series: sin(x) ≈ x - x³/6 + x⁵/120
	x3 := x * x * x
	x5 := x3 * x * x
	return x - x3/6.0 + x5/120.0
}

func cosApprox(x float64) float64 {
	return sinApprox(x + 1.5708) // cos(x) = sin(x + π/2)
}

// Render converts the canvas to a string of Braille characters with ANSI colors.
func (c *Canvas) Render() string {
	var b strings.Builder

	for charY := 0; charY < c.charHeight; charY++ {
		for charX := 0; charX < c.charWidth; charX++ {
			// Build the Braille character for this cell
			var char rune = brailleBase

			// Map pixels to Braille dots
			basePixelX := charX * 2
			basePixelY := charY * 4

			for dotY := 0; dotY < 4; dotY++ {
				for dotX := 0; dotX < 2; dotX++ {
					px := basePixelX + dotX
					py := basePixelY + dotY
					if py < len(c.pixels) && px < len(c.pixels[py]) && c.pixels[py][px] {
						char += brailleWeights[dotY][dotX]
					}
				}
			}

			// Apply color styling
			color := c.colors[charY][charX]
			style := lipgloss.NewStyle().Foreground(color)
			b.WriteString(style.Render(string(char)))
		}
		if charY < c.charHeight-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
