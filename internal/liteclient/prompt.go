package liteclient

import "github.com/nsf/termbox-go"

// PromptRenderer renders the meept-lite prompt at the bottom of the screen.
// Prompt format: | meept:session_name #>
// Colors: | orange, meept orange, : white, session-name grey, #> white
type PromptRenderer struct {
	sessionName string
}

// NewPromptRenderer creates a new prompt renderer.
func NewPromptRenderer(sessionName string) *PromptRenderer {
	return &PromptRenderer{
		sessionName: sessionName,
	}
}

// SetSessionName updates the session name in the prompt.
func (p *PromptRenderer) SetSessionName(name string) {
	p.sessionName = name
}

// SessionName returns the current session name.
func (p *PromptRenderer) SessionName() string {
	return p.sessionName
}

// Height returns the height of the prompt (1 line).
func (p *PromptRenderer) Height() int {
	return 1
}

// Render draws the prompt at the bottom of the screen.
// Returns the cursor X position for input.
func (p *PromptRenderer) Render(y int) int {
	// Build the prompt string with colors
	x := 0

	// | orange
	termbox.SetCell(x, y, '|', termbox.Attribute(ColorOrange)|termbox.AttrBold, termbox.Attribute(ColorBackground))
	x++

	// space
	termbox.SetCell(x, y, ' ', 0, termbox.Attribute(ColorBackground))
	x++

	// meept orange
	for _, r := range "meept" {
		termbox.SetCell(x, y, r, termbox.Attribute(ColorOrange)|termbox.AttrBold, termbox.Attribute(ColorBackground))
		x++
	}

	// : white
	termbox.SetCell(x, y, ':', termbox.Attribute(ColorWhite), termbox.Attribute(ColorBackground))
	x++

	// session-name grey
	for _, r := range p.sessionName {
		termbox.SetCell(x, y, r, termbox.Attribute(ColorGrey), termbox.Attribute(ColorBackground))
		x++
	}

	// space
	termbox.SetCell(x, y, ' ', 0, termbox.Attribute(ColorBackground))
	x++

	// #> white
	for _, r := range "#>" {
		termbox.SetCell(x, y, r, termbox.Attribute(ColorWhite)|termbox.AttrBold, termbox.Attribute(ColorBackground))
		x++
	}

	// space after prompt for input cursor
	termbox.SetCell(x, y, ' ', 0, termbox.Attribute(ColorBackground))

	return x // Return the X position after the prompt
}

// RenderInput renders the input text after the prompt.
func (p *PromptRenderer) RenderInput(x, y int, input string, cursorX int) {
	// Clear rest of line
	width, _ := termbox.Size()
	for i := x; i < width; i++ {
		termbox.SetCell(i, y, ' ', 0, termbox.Attribute(ColorBackground))
	}

	// Render input text
	i := 0
	for _, r := range input {
		if x+i >= width {
			break
		}
		termbox.SetCell(x+i, y, r, termbox.Attribute(ColorWhite), termbox.Attribute(ColorBackground))
		i++
	}

	// Draw cursor
	if x+cursorX < width {
		termbox.SetCell(x+cursorX, y, '█', termbox.Attribute(ColorWhite)|termbox.AttrReverse, termbox.Attribute(ColorBackground))
	}
}
