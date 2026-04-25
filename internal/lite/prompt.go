// Package lite provides a lightweight TUI for meept-lite.
package lite

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
)

// Prompt is a composite component combining StatusBar, Input, and AgentStatus.
// It implements the 2-line prompt design with optional agent status below:
//
//	+-----------------------------------------------------------+
//	| claude-sonnet | 2.3k tokens | $0.02 | 3m42s              |  <- Line 1 (status)
//	+-----------------------------------------------------------+
//	| > _                                                       |  <- Line 2 (input)
//	+-----------------------------------------------------------+
//
//	Agent info displays below prompt when active:
//	+-----------------------------------------------------------+
//	| thinking... (2.3s) | running shell command                |
//	+-----------------------------------------------------------+
type Prompt struct {
	status      *StatusBar
	input       *Input
	agentStatus *AgentStatus
	width       int
}

// NewPrompt creates a new Prompt component with all sub-components initialized.
func NewPrompt() *Prompt {
	return &Prompt{
		status:      NewStatusBar(),
		input:       NewInput(),
		agentStatus: NewAgentStatus(),
		width:       80,
	}
}

// SetSize updates the width of all sub-components.
func (p *Prompt) SetSize(width int) {
	p.width = width
	p.status.SetSize(width)
	p.input.SetSize(width)
	p.agentStatus.SetSize(width)
}

// Update handles tea.Msg and returns a command and whether a message was sent.
// The bool return indicates if the user pressed Enter to send a message.
func (p *Prompt) Update(msg tea.Msg) (tea.Cmd, bool) {
	var cmds []tea.Cmd

	// Handle agent status updates (TickMsg, ProgressUpdateMsg)
	if cmd := p.agentStatus.Update(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Handle status bar updates
	if cmd := p.status.Update(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Handle input and check if message was sent
	inputCmd, sent := p.input.Update(msg)
	if inputCmd != nil {
		cmds = append(cmds, inputCmd)
	}

	// Note: history is added by app.go before prompt.Reset()
	return tea.Batch(cmds...), sent
}

// View renders the complete prompt area:
// Line 1: StatusBar
// Line 2: Input (with optional history indicator)
// Line 3 (optional): AgentStatus when active
func (p *Prompt) View() string {
	var lines []string

	// Status bar (line 1)
	lines = append(lines, p.status.View())

	// Input (line 2) - prepend history indicator if browsing
	inputLine := p.input.View()
	if indicator := p.input.HistoryIndicator(); indicator != "" {
		// Style the indicator with muted color
		indicatorStyled := historyIndicatorStyle.Render(indicator) + " "
		inputLine = indicatorStyled + inputLine
	}
	lines = append(lines, inputLine)

	// Agent status (line 3, only when active)
	if p.agentStatus.IsActive() {
		lines = append(lines, p.agentStatus.View())
	}

	return strings.Join(lines, "\n")
}

// historyIndicatorStyle for the history browsing indicator.
var historyIndicatorStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#6B7280")).
	Italic(true)

// Value returns the current input value.
func (p *Prompt) Value() string {
	return p.input.Value()
}

// SetValue sets the input value.
func (p *Prompt) SetValue(s string) {
	p.input.SetValue(s)
}

// Focus gives focus to the input component.
func (p *Prompt) Focus() {
	p.input.Focus()
}

// Blur removes focus from the input component.
func (p *Prompt) Blur() {
	p.input.Blur()
}

// Height returns the current height of the prompt area.
// Returns 2 when agent status is not active (status bar + input),
// or 3+ when agent status is active (adds agent status line(s)).
func (p *Prompt) Height() int {
	h := 2 // StatusBar (1) + Input baseline (1)

	// Add extra lines if input has multiple lines
	inputLines := p.input.Height()
	if inputLines > 1 {
		h += inputLines - 1
	}

	// Add agent status height when active
	if p.agentStatus.IsActive() {
		// AgentStatus renders as 3 lines (top border, content, bottom border)
		h += 3
	}

	return h
}

// Reset clears the input and stops agent status.
func (p *Prompt) Reset() {
	p.input.Reset()
	p.agentStatus.Clear()
}

// StatusBar returns the underlying StatusBar for direct configuration.
func (p *Prompt) StatusBar() *StatusBar {
	return p.status
}

// AgentStatus returns the underlying AgentStatus for direct configuration.
func (p *Prompt) AgentStatus() *AgentStatus {
	return p.agentStatus
}

// Input returns the underlying Input for direct configuration.
func (p *Prompt) Input() *Input {
	return p.input
}

// SetModel updates the model name in the status bar.
func (p *Prompt) SetModel(name string) {
	p.status.SetModel(name)
}

// SetTokens updates the token counts in the status bar.
func (p *Prompt) SetTokens(used, max int) {
	p.status.SetTokens(used, max)
}

// SetCost updates the cost display in the status bar.
func (p *Prompt) SetCost(cents int) {
	p.status.SetCost(cents)
}

// StartThinking activates the agent status with a thinking animation.
func (p *Prompt) StartThinking() tea.Cmd {
	return p.agentStatus.StartThinking()
}

// StartExecuting activates the agent status with tool execution display.
func (p *Prompt) StartExecuting(tool string) tea.Cmd {
	return p.agentStatus.StartExecuting(tool)
}

// StopAgent clears the agent status display.
func (p *Prompt) StopAgent() {
	p.agentStatus.Clear()
}

// IsFocused returns whether the input component has focus.
func (p *Prompt) IsFocused() bool {
	return p.input.IsFocused()
}

// IsAgentActive returns whether the agent status is currently displayed.
func (p *Prompt) IsAgentActive() bool {
	return p.agentStatus.IsActive()
}
