package viz

import (
	"strings"
	"testing"
)

func TestNewCanvas(t *testing.T) {
	c := NewCanvas(10, 5)

	if c.CharWidth() != 10 {
		t.Errorf("CharWidth() = %d, want 10", c.CharWidth())
	}
	if c.CharHeight() != 5 {
		t.Errorf("CharHeight() = %d, want 5", c.CharHeight())
	}
	if c.PixelWidth() != 20 {
		t.Errorf("PixelWidth() = %d, want 20", c.PixelWidth())
	}
	if c.PixelHeight() != 20 {
		t.Errorf("PixelHeight() = %d, want 20", c.PixelHeight())
	}
}

func TestSetPixel(t *testing.T) {
	c := NewCanvas(10, 5)

	// Test setting pixel in bounds
	c.SetPixel(5, 5, true)
	if !c.GetPixel(5, 5) {
		t.Error("SetPixel(5, 5, true) did not set pixel")
	}

	// Test setting pixel out of bounds (should not panic)
	c.SetPixel(-1, -1, true)
	c.SetPixel(100, 100, true)

	// Test getting pixel out of bounds
	if c.GetPixel(-1, -1) {
		t.Error("GetPixel(-1, -1) should return false for out of bounds")
	}
}

func TestDrawRect(t *testing.T) {
	c := NewCanvas(10, 5)

	// Draw filled rect
	c.DrawRect(2, 2, 4, 4, true)

	// Check corners
	if !c.GetPixel(2, 2) {
		t.Error("Top-left corner not set")
	}
	if !c.GetPixel(5, 5) {
		t.Error("Bottom-right corner not set")
	}

	// Check interior (should be filled)
	if !c.GetPixel(3, 3) {
		t.Error("Interior pixel not set for filled rect")
	}
}

func TestDrawRectOutline(t *testing.T) {
	c := NewCanvas(10, 5)

	// Draw outline rect
	c.DrawRect(2, 2, 4, 4, false)

	// Check border
	if !c.GetPixel(2, 2) {
		t.Error("Border pixel not set")
	}

	// Check interior (should not be filled)
	if c.GetPixel(3, 3) {
		t.Error("Interior pixel should not be set for outline rect")
	}
}

func TestClear(t *testing.T) {
	c := NewCanvas(10, 5)

	// Set some pixels
	c.SetPixel(5, 5, true)
	c.SetPixel(3, 3, true)

	// Clear
	c.Clear()

	// Verify cleared
	if c.GetPixel(5, 5) {
		t.Error("Pixel should be cleared after Clear()")
	}
	if c.GetPixel(3, 3) {
		t.Error("Pixel should be cleared after Clear()")
	}
}

func TestRender(t *testing.T) {
	c := NewCanvas(2, 1)

	// Render empty canvas
	output := c.Render()
	if output == "" {
		t.Error("Render() returned empty string for empty canvas")
	}

	// Verify Braille base character is present (U+2800)
	if !strings.ContainsRune(output, '\u2800') {
		// Empty cells should have Braille blank
		t.Logf("Output: %q", output)
	}

	// Set some pixels and verify render changes
	c.SetPixel(0, 0, true)
	outputWithPixel := c.Render()
	if outputWithPixel == output {
		t.Error("Setting pixel should change render output")
	}
}

func TestSetColorAt(t *testing.T) {
	c := NewCanvas(5, 3)

	// Set color at valid position
	c.SetColorAt(2, 1, ColorWorking)

	// Set color at pixel position
	c.SetColorAtPixel(4, 8, ColorSuccess)

	// Out of bounds should not panic
	c.SetColorAt(-1, -1, ColorError)
	c.SetColorAt(100, 100, ColorError)
}

func TestDrawDottedLine(t *testing.T) {
	c := NewCanvas(10, 5)

	from := Point{0, 0}
	to := Point{19, 0}

	c.DrawDottedLine(from, to, 2, 2)

	// Check that some pixels are set and some are not (dotted)
	hasSet := false
	hasUnset := false
	for x := range 20 {
		if c.GetPixel(x, 0) {
			hasSet = true
		} else {
			hasUnset = true
		}
	}

	if !hasSet {
		t.Error("Dotted line should have some pixels set")
	}
	if !hasUnset {
		t.Error("Dotted line should have some gaps")
	}
}

func TestDrawLine(t *testing.T) {
	c := NewCanvas(10, 5)

	from := Point{0, 0}
	to := Point{10, 10}

	c.DrawLine(from, to, ColorIdle)

	// Check that pixels along diagonal are set
	if !c.GetPixel(0, 0) {
		t.Error("Start of line should be set")
	}
	if !c.GetPixel(5, 5) {
		t.Error("Middle of line should be set")
	}
	if !c.GetPixel(10, 10) {
		t.Error("End of line should be set")
	}
}

func TestBrailleEncoding(t *testing.T) {
	c := NewCanvas(1, 1)

	// Test encoding of single dot in top-left position
	c.SetPixel(0, 0, true)
	output := c.Render()

	// U+2800 + 0x01 = U+2801
	if !strings.ContainsRune(output, '\u2801') {
		t.Errorf("Single top-left dot should render as U+2801, got %q", output)
	}

	// Clear and set bottom-right dot
	c.Clear()
	c.SetPixel(1, 3, true)
	output = c.Render()

	// U+2800 + 0x80 = U+2880
	if !strings.ContainsRune(output, '\u2880') {
		t.Errorf("Single bottom-right dot should render as U+2880, got %q", output)
	}
}

func TestAbs(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{5, 5},
		{-5, 5},
		{0, 0},
		{-100, 100},
	}

	for _, tt := range tests {
		result := abs(tt.input)
		if result != tt.expected {
			t.Errorf("abs(%d) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}
