// Package lite provides a lightweight TUI for meept-lite.
package lite

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Stage constants for agent status.
const (
	StageIdle      = "idle"
	StageThinking  = "thinking"
	StageExecuting = "executing"
)

// TickMsg signals the agent status to update its animation.
type TickMsg time.Time

// ProgressUpdateMsg carries progress updates from the event stream.
// This mirrors the structure from internal/tui/models/chat.go.
type ProgressUpdateMsg struct {
	AgentID       string
	Stage         string
	Percent       float64
	CurrentTool   string
	TokensUsed    int
	ContextResets int
}

// toolIcon returns an icon for the given tool name.
func toolIcon(tool string) string {
	toolLower := strings.ToLower(tool)

	switch {
	case strings.Contains(toolLower, "shell") || strings.Contains(toolLower, "bash") || strings.Contains(toolLower, "exec"):
		return "[cmd]"
	case strings.Contains(toolLower, "search") || strings.Contains(toolLower, "grep") || strings.Contains(toolLower, "find"):
		return "[search]"
	case strings.Contains(toolLower, "file") || strings.Contains(toolLower, "read") || strings.Contains(toolLower, "write"):
		return "[file]"
	case strings.Contains(toolLower, "web") || strings.Contains(toolLower, "http") || strings.Contains(toolLower, "fetch"):
		return "[web]"
	case strings.Contains(toolLower, "git"):
		return "[git]"
	default:
		return "[tool]"
	}
}

// AgentStatus displays the current agent state below the prompt.
type AgentStatus struct {
	visible     bool
	stage       string    // "thinking", "executing", "idle"
	currentTool string    // e.g., "shell command", "file read"
	startTime   time.Time // when the current stage started
	width       int
	dotCount    int // for animated dots (0-3)

	// Styling
	boxStyle    lipgloss.Style
	stageStyle  lipgloss.Style
	toolStyle   lipgloss.Style
	timerStyle  lipgloss.Style
	mutedStyle  lipgloss.Style
}

// NewAgentStatus creates a new AgentStatus component.
func NewAgentStatus() *AgentStatus {
	return &AgentStatus{
		visible:   false,
		stage:     StageIdle,
		startTime: time.Now(),
		width:     80,
		dotCount:  0,

		// Muted background styling
		boxStyle: lipgloss.NewStyle().
			Background(lipgloss.Color("#1a1a2e")).
			Foreground(lipgloss.Color("#a0a0a0")).
			Padding(0, 1),
		stageStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f0a500")).
			Bold(true),
		toolStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00d9ff")),
		timerStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#808080")),
		mutedStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#606060")),
	}
}

// SetSize updates the component width.
func (a *AgentStatus) SetSize(width int) {
	a.width = width
}

// Update handles tea messages and returns commands.
func (a *AgentStatus) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case TickMsg:
		if a.visible && a.stage != StageIdle {
			// Cycle the dot animation (0 -> 1 -> 2 -> 3 -> 0)
			a.dotCount = (a.dotCount + 1) % 4
			return a.tick()
		}

	case ProgressUpdateMsg:
		return a.handleProgressUpdate(msg)
	}

	return nil
}

// handleProgressUpdate processes a progress update message.
func (a *AgentStatus) handleProgressUpdate(msg ProgressUpdateMsg) tea.Cmd {
	// Map the stage from the message
	switch strings.ToLower(msg.Stage) {
	case "thinking", "planning", "processing":
		if a.stage != StageThinking {
			a.stage = StageThinking
			a.startTime = time.Now()
			a.dotCount = 0
		}
		a.visible = true
		a.currentTool = ""

	case "executing", "running", "tool":
		if a.stage != StageExecuting || a.currentTool != msg.CurrentTool {
			a.stage = StageExecuting
			if a.currentTool != msg.CurrentTool {
				a.startTime = time.Now()
			}
			a.dotCount = 0
		}
		a.visible = true
		a.currentTool = msg.CurrentTool

	case "complete", "done", "finished", "idle":
		a.Clear()
		return nil

	default:
		// Unknown stage - if we have a tool, show executing
		if msg.CurrentTool != "" {
			a.stage = StageExecuting
			a.currentTool = msg.CurrentTool
			a.visible = true
		} else if msg.Stage != "" {
			// Some other stage, treat as thinking
			a.stage = StageThinking
			a.visible = true
		}
	}

	// Start ticking if we became visible
	if a.visible && a.stage != StageIdle {
		return a.tick()
	}

	return nil
}

// View renders the agent status bar.
func (a *AgentStatus) View() string {
	if !a.visible || a.stage == StageIdle {
		return ""
	}

	// Calculate elapsed time
	elapsed := time.Since(a.startTime)
	elapsedStr := fmt.Sprintf("(%.1fs)", elapsed.Seconds())

	// Build animated dots
	dots := strings.Repeat(".", a.dotCount+1)
	padding := strings.Repeat(" ", 3-a.dotCount)

	// Build the content based on stage
	var leftContent, rightContent string

	switch a.stage {
	case StageThinking:
		leftContent = a.stageStyle.Render("thinking" + dots + padding)
		leftContent += " " + a.timerStyle.Render(elapsedStr)

	case StageExecuting:
		leftContent = a.stageStyle.Render("executing" + dots + padding)
		leftContent += " " + a.timerStyle.Render(elapsedStr)

		if a.currentTool != "" {
			icon := toolIcon(a.currentTool)
			rightContent = a.toolStyle.Render(icon + " " + a.currentTool)
		}
	}

	// Calculate widths for layout
	innerWidth := a.width - 4 // Account for box padding and borders

	// Build the status line
	var statusLine string
	if rightContent != "" {
		// Two-column layout with separator
		leftLen := lipgloss.Width(leftContent)
		rightLen := lipgloss.Width(rightContent)
		separator := a.mutedStyle.Render(" | ")
		separatorLen := lipgloss.Width(separator)

		// Calculate available space
		availableSpace := innerWidth - leftLen - separatorLen - rightLen
		if availableSpace < 0 {
			// Truncate right content if needed
			maxRightLen := innerWidth - leftLen - separatorLen - 3
			if maxRightLen > 0 && rightLen > maxRightLen {
				rightContent = rightContent[:maxRightLen] + "..."
			}
		}

		statusLine = leftContent + separator + rightContent
	} else {
		statusLine = leftContent
	}

	// Create the full-width box
	boxContent := a.boxStyle.Width(innerWidth).Render(statusLine)

	// Add top and bottom borders
	borderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#404040"))

	topBorder := borderStyle.Render("+" + strings.Repeat("-", innerWidth+2) + "+")
	bottomBorder := borderStyle.Render("+" + strings.Repeat("-", innerWidth+2) + "+")

	// Wrap content with left/right borders
	leftBorder := borderStyle.Render("|")
	rightBorder := borderStyle.Render("|")

	contentLine := leftBorder + " " + boxContent + " " + rightBorder

	return topBorder + "\n" + contentLine + "\n" + bottomBorder
}

// SetStage updates the current stage and detail.
func (a *AgentStatus) SetStage(stage, detail string) {
	oldStage := a.stage

	switch strings.ToLower(stage) {
	case "thinking":
		a.stage = StageThinking
		a.currentTool = ""
	case "executing":
		a.stage = StageExecuting
		a.currentTool = detail
	case "idle":
		a.stage = StageIdle
		a.currentTool = ""
		a.visible = false
		return
	default:
		a.stage = stage
		a.currentTool = detail
	}

	// Reset timer when stage changes
	if oldStage != a.stage {
		a.startTime = time.Now()
		a.dotCount = 0
	}

	a.visible = true
}

// Clear resets the agent status to hidden/idle state.
func (a *AgentStatus) Clear() {
	a.visible = false
	a.stage = StageIdle
	a.currentTool = ""
	a.dotCount = 0
}

// IsActive returns whether the status bar is currently visible.
func (a *AgentStatus) IsActive() bool {
	return a.visible && a.stage != StageIdle
}

// tick returns a command that sends a TickMsg after a delay.
func (a *AgentStatus) tick() tea.Cmd {
	return tea.Tick(400*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// StartThinking is a convenience method to start the thinking animation.
func (a *AgentStatus) StartThinking() tea.Cmd {
	a.SetStage(StageThinking, "")
	return a.tick()
}

// StartExecuting is a convenience method to start executing with a tool.
func (a *AgentStatus) StartExecuting(tool string) tea.Cmd {
	a.SetStage(StageExecuting, tool)
	return a.tick()
}
