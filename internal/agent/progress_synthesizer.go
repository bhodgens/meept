package agent

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
)

// VerbosityLevel controls how much progress detail is surfaced.
type VerbosityLevel int

const (
	// VerbosityQuiet shows only high-level completion events (agent end).
	VerbosityQuiet VerbosityLevel = iota
	// VerbosityNormal shows tool results and agent completions.
	VerbosityNormal
	// VerbosityVerbose shows everything including tool starts and turn boundaries.
	VerbosityVerbose
)

// String returns a human-readable name for the verbosity level.
func (v VerbosityLevel) String() string {
	switch v {
	case VerbosityQuiet:
		return "quiet"
	case VerbosityNormal:
		return "normal"
	case VerbosityVerbose:
		return "verbose"
	default:
		return "unknown"
	}
}

// ParseVerbosityLevel converts a string to a VerbosityLevel.
// Returns VerbosityNormal for unrecognized values.
func ParseVerbosityLevel(s string) VerbosityLevel {
	switch strings.ToLower(s) {
	case "quiet":
		return VerbosityQuiet
	case "normal":
		return VerbosityNormal
	case "verbose":
		return VerbosityVerbose
	default:
		return VerbosityNormal
	}
}

// SynthesizedProgressEvent is a condensed, human-readable progress update
// derived from a raw AgentEvent. Each event is assigned a verbosity tier
// so consumers can filter by desired detail level.
type SynthesizedProgressEvent struct {
	SessionID   string         `json:"session_id"`
	AgentID     string         `json:"agent_id"`
	Tier        VerbosityLevel `json:"tier"`
	Message     string         `json:"message"`
	SourceEvent AgentEventType `json:"source_event"`
	Timestamp   time.Time      `json:"timestamp"`
}

// ProgressSynthesizer converts raw agent events into tiered progress summaries.
type ProgressSynthesizer struct {
	bus    *bus.MessageBus
	client *llm.Client
	logger *slog.Logger
}

// NewProgressSynthesizer creates a new synthesizer. The bus and client
// parameters are optional; they are reserved for future LLM-powered
// summarization of complex events.
func NewProgressSynthesizer(b *bus.MessageBus, client *llm.Client, logger *slog.Logger) *ProgressSynthesizer {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProgressSynthesizer{
		bus:    b,
		client: client,
		logger: logger,
	}
}

// Synthesize converts an AgentEvent into a SynthesizedProgressEvent.
// Returns nil for event types that do not warrant a progress update.
func (ps *ProgressSynthesizer) Synthesize(event AgentEvent) *SynthesizedProgressEvent {
	switch event.Type {
	case AgentEventAgentEnd:
		return ps.synthesizeAgentEnd(event)
	case AgentEventToolExecutionEnd:
		return ps.synthesizeToolEnd(event)
	case AgentEventToolExecutionStart:
		return ps.synthesizeToolStart(event)
	case AgentEventTurnEnd:
		return ps.synthesizeTurnEnd(event)
	default:
		return nil
	}
}

func (ps *ProgressSynthesizer) synthesizeAgentEnd(event AgentEvent) *SynthesizedProgressEvent {
	data, ok := event.Data.(AgentEndData)
	if !ok {
		return nil
	}
	agentID := coalesce(data.AgentID, event.AgentID)
	msg := fmt.Sprintf("%s: completed (%s)", agentID, data.Reason)
	if data.Duration > 0 {
		msg = fmt.Sprintf("%s: %s (%s)", agentID, data.Reason, formatDuration(data.Duration))
	}
	return &SynthesizedProgressEvent{
		SessionID:   event.ConversationID,
		AgentID:     agentID,
		Tier:        VerbosityQuiet,
		Message:     msg,
		SourceEvent: event.Type,
		Timestamp:   event.Timestamp,
	}
}

func (ps *ProgressSynthesizer) synthesizeToolEnd(event AgentEvent) *SynthesizedProgressEvent {
	data, ok := event.Data.(ToolExecutionEndData)
	if !ok {
		return nil
	}
	agentID := event.AgentID

	var status string
	var detail string
	switch {
	case data.Blocked:
		status = "blocked"
		detail = truncate(data.BlockReason, 60)
	case !data.Success:
		status = "failed"
		detail = truncate(data.Error, 60)
	default:
		status = "ok"
		detail = truncate(firstLine(data.Result), 60)
	}

	msg := fmt.Sprintf("%s: %s %s", agentID, status, data.ToolName)
	if data.Duration > 0 {
		msg += fmt.Sprintf(" (%s)", formatDuration(data.Duration))
	}
	if detail != "" {
		msg += ": " + detail
	}

	return &SynthesizedProgressEvent{
		SessionID:   event.ConversationID,
		AgentID:     agentID,
		Tier:        VerbosityNormal,
		Message:     msg,
		SourceEvent: event.Type,
		Timestamp:   event.Timestamp,
	}
}

func (ps *ProgressSynthesizer) synthesizeToolStart(event AgentEvent) *SynthesizedProgressEvent {
	data, ok := event.Data.(ToolExecutionStartData)
	if !ok {
		return nil
	}
	agentID := event.AgentID
	msg := fmt.Sprintf("%s: executing %s", agentID, data.ToolName)

	return &SynthesizedProgressEvent{
		SessionID:   event.ConversationID,
		AgentID:     agentID,
		Tier:        VerbosityVerbose,
		Message:     msg,
		SourceEvent: event.Type,
		Timestamp:   event.Timestamp,
	}
}

func (ps *ProgressSynthesizer) synthesizeTurnEnd(event AgentEvent) *SynthesizedProgressEvent {
	data, ok := event.Data.(TurnEndData)
	if !ok {
		return nil
	}
	agentID := event.AgentID
	msg := fmt.Sprintf("%s: turn %d done (%d tool calls, %d tokens)",
		agentID, data.TurnNumber, data.ToolCallCount, data.ResponseTokens)

	return &SynthesizedProgressEvent{
		SessionID:   event.ConversationID,
		AgentID:     agentID,
		Tier:        VerbosityVerbose,
		Message:     msg,
		SourceEvent: event.Type,
		Timestamp:   event.Timestamp,
	}
}

// formatDuration renders a duration in a compact human-friendly form.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return "0ms"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		if d.Milliseconds()%1000 == 0 {
			return fmt.Sprintf("%ds", int(d.Seconds()))
		}
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
}

// firstLine returns the first non-empty line of s.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// truncate shortens s to at most maxLen runes, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen > 3 {
		return string(runes[:maxLen-3]) + "..."
	}
	return string(runes[:maxLen])
}

// coalesce returns the first non-empty string.
func coalesce(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
