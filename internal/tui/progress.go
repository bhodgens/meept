package tui

import (

	"time"
)


// ProgressState holds current progress for display in chat
type ProgressState struct {
	AgentID       string
	Stage         string
	Percent       float64
	CurrentTool   string
	TokensUsed    int
	ContextResets int
	LastUpdate    time.Time
	// Tool-level streaming progress
	ToolName    string
	ToolMessage string
	ToolPercent int
}




