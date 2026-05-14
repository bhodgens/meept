// Package components provides reusable TUI components.
package components

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Sparkline renders a simple sparkline visualization using block characters.
// Characters used: ▁▂▃▄▅▆▇█ (8 levels from lowest to highest)
type Sparkline struct {
	data     []int
	maxValue int // Max value for scaling (0 = auto-scale)
	width    int // Width in characters
	label    string
	style    lipgloss.Style
}

// SparklineChars are the block characters for sparkline levels (lowest to highest).
var SparklineChars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// NewSparkline creates a new sparkline component.
func NewSparkline(label string, width int) *Sparkline {
	return &Sparkline{
		data:     make([]int, 0),
		maxValue: 0,
		width:    width,
		label:    label,
		style:    lipgloss.NewStyle(),
	}
}

// SetData sets the sparkline data points.
func (s *Sparkline) SetData(data []int) {
	s.data = data
}

// AddPoint adds a data point, maintaining window size.
func (s *Sparkline) AddPoint(value int) {
	s.data = append(s.data, value)
	// Keep only the last 'width' points
	dataWidth := s.width
	if s.label != "" {
		// Reserve space for label
		dataWidth = s.width - len(s.label) - 2
	}
	if dataWidth < 1 {
		dataWidth = 1
	}
	if len(s.data) > dataWidth {
		s.data = s.data[len(s.data)-dataWidth:]
	}
}

// SetMaxValue sets the maximum value for scaling.
// If 0, auto-scale based on current data max.
func (s *Sparkline) SetMaxValue(maxVal int) {
	s.maxValue = maxVal
}

// SetLabel sets the sparkline label.
func (s *Sparkline) SetLabel(label string) {
	s.label = label
}

// SetWidth sets the total width including label.
func (s *Sparkline) SetWidth(width int) {
	s.width = width
}

// SetStyle sets the rendering style.
func (s *Sparkline) SetStyle(style lipgloss.Style) {
	s.style = style
}

// View renders the sparkline to a string.
func (s *Sparkline) View() string {
	if len(s.data) == 0 {
		return s.renderEmpty()
	}

	// Calculate data width (total width minus label space)
	dataWidth := s.width
	labelStr := ""
	if s.label != "" {
		labelStr = s.label + ": "
		dataWidth = s.width - len(labelStr)
	}
	if dataWidth < 1 {
		dataWidth = 1
	}

	// Find max value for scaling
	maxVal := s.maxValue
	if maxVal == 0 {
		for _, v := range s.data {
			if v > maxVal {
				maxVal = v
			}
		}
	}
	if maxVal == 0 {
		maxVal = 1 // Prevent division by zero
	}

	// Build sparkline
	var sb strings.Builder
	sb.WriteString(labelStr)

	// Get the most recent data points that fit
	data := s.data
	if len(data) > dataWidth {
		data = data[len(data)-dataWidth:]
	}

	// Render each data point
	for _, v := range data {
		// Scale to 0-7 range (8 levels)
		level := max(min((v*7)/maxVal, 7), 0)
		sb.WriteRune(SparklineChars[level])
	}

	// Pad with spaces if data is shorter than width
	padding := dataWidth - len(data)
	if padding > 0 {
		sb.WriteString(strings.Repeat(" ", padding))
	}

	return s.style.Render(sb.String())
}

func (s *Sparkline) renderEmpty() string {
	var sb strings.Builder
	if s.label != "" {
		sb.WriteString(s.label)
		sb.WriteString(": ")
	}
	// Fill with lowest block to show empty state
	dataWidth := s.width
	if s.label != "" {
		dataWidth = s.width - len(s.label) - 2
	}
	if dataWidth < 1 {
		dataWidth = 1
	}
	sb.WriteString(strings.Repeat(string(SparklineChars[0]), dataWidth))
	return s.style.Render(sb.String())
}

// MinMaxSparkline shows min/max values alongside the sparkline.
type MinMaxSparkline struct {
	*Sparkline
	showMinMax bool
}

// NewMinMaxSparkline creates a sparkline with min/max value display.
func NewMinMaxSparkline(label string, width int) *MinMaxSparkline {
	return &MinMaxSparkline{
		Sparkline:  NewSparkline(label, width),
		showMinMax: true,
	}
}

// View renders the sparkline with min/max indicators.
func (s *MinMaxSparkline) View() string {
	if !s.showMinMax || len(s.data) == 0 {
		return s.Sparkline.View()
	}

	// Find min/max
	minVal, maxVal := s.data[0], s.data[0]
	for _, v := range s.data {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	// Reduce sparkline width to fit min/max
	originalWidth := s.width
	s.width = originalWidth - 8 // Reserve space for " [0-99]"
	sparkline := s.Sparkline.View()
	s.width = originalWidth

	// Append range indicator
	rangeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	return sparkline + rangeStyle.Render(" [") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Render(itoa(minVal)) +
		rangeStyle.Render("-") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Render(itoa(maxVal)) +
		rangeStyle.Render("]")
}

// itoa converts int to string (simple helper to avoid strconv import for small ints).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}

	digits := make([]byte, 0, 10)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}

	// Reverse
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}

// MultiSparkline displays multiple sparklines stacked vertically.
type MultiSparkline struct {
	sparklines []*Sparkline
	width      int
}

// NewMultiSparkline creates a container for multiple sparklines.
func NewMultiSparkline(width int) *MultiSparkline {
	return &MultiSparkline{
		sparklines: make([]*Sparkline, 0),
		width:      width,
	}
}

// Add adds a sparkline to the container.
func (m *MultiSparkline) Add(label string) *Sparkline {
	s := NewSparkline(label, m.width)
	m.sparklines = append(m.sparklines, s)
	return s
}

// SetWidth updates width for all sparklines.
func (m *MultiSparkline) SetWidth(width int) {
	m.width = width
	for _, s := range m.sparklines {
		s.SetWidth(width)
	}
}

// View renders all sparklines stacked.
func (m *MultiSparkline) View() string {
	lines := make([]string, len(m.sparklines))
	for i, s := range m.sparklines {
		lines[i] = s.View()
	}
	return strings.Join(lines, "\n")
}

// Get returns a sparkline by index.
func (m *MultiSparkline) Get(index int) *Sparkline {
	if index >= 0 && index < len(m.sparklines) {
		return m.sparklines[index]
	}
	return nil
}

// Count returns the number of sparklines.
func (m *MultiSparkline) Count() int {
	return len(m.sparklines)
}
