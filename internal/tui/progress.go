package tui

import (
	"fmt"
	"strings"
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
}

// Render returns the formatted progress string for display
func (p *ProgressState) Render() string {
	if p == nil {
		return "Sending..."
	}

	var parts []string

	// Agent emoji + name
	if p.AgentID != "" {
		agentDisplay := p.AgentID
		if len(agentDisplay) > 12 {
			agentDisplay = agentDisplay[:12]
		}
		parts = append(parts, fmt.Sprintf("🤖 %s", agentDisplay))
	}

	// Progress bar
	if p.Percent > 0 {
		barWidth := 20
		filled := int(p.Percent / 100 * float64(barWidth))
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		parts = append(parts, fmt.Sprintf("[%s %.0f%%]", bar, p.Percent))
	} else if p.Stage != "" {
		parts = append(parts, p.Stage)
	}

	// Current tool
	if p.CurrentTool != "" {
		parts = append(parts, fmt.Sprintf("→ %s", p.CurrentTool))
	}

	// Tokens
	if p.TokensUsed > 0 {
		parts = append(parts, fmt.Sprintf("📊 %s", formatTokens(p.TokensUsed)))
	}

	// Context reset indicator
	if p.ContextResets > 0 {
		parts = append(parts, fmt.Sprintf("🔄 %d", p.ContextResets))
	}

	if len(parts) == 0 {
		return "Processing..."
	}

	return strings.Join(parts, " │ ")
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

// IsComplete returns true if progress indicates completion
func (p *ProgressState) IsComplete() bool {
	return p != nil && p.Percent >= 100
}

// IsStale returns true if progress hasn't been updated recently
func (p *ProgressState) IsStale() bool {
	return p == nil || time.Since(p.LastUpdate) > 5*time.Minute
}
