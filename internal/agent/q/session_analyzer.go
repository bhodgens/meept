package q

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/memory/memvid"
)

// SessionAnalyzer parses and analyzes completed session transcripts.
type SessionAnalyzer struct {
	memvidClient *memvid.Client
	logger       *slog.Logger
	config       SessionAnalyzerConfig
}

// SessionAnalyzerConfig holds configuration for the SessionAnalyzer.
type SessionAnalyzerConfig struct {
	SessionIdleTriggerHours int
}

// NewSessionAnalyzer creates a new SessionAnalyzer.
func NewSessionAnalyzer(client *memvid.Client, logger *slog.Logger, config SessionAnalyzerConfig) *SessionAnalyzer {
	return &SessionAnalyzer{
		memvidClient: client,
		logger:       logger,
		config:       config,
	}
}

// AnalyzeSession analyzes a single session and returns the analysis.
func (a *SessionAnalyzer) AnalyzeSession(ctx context.Context, sessionID string) (*SessionAnalysis, error) {
	// Fetch session metadata from memvid
	sessionMemories, err := a.memvidClient.Search(ctx, fmt.Sprintf("session:%s metadata", sessionID), 10)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch session metadata: %w", err)
	}

	if len(sessionMemories) == 0 {
		return nil, fmt.Errorf("no session data found for ID: %s", sessionID)
	}

	// Parse session data
	sessionData, err := a.parseSessionData(sessionMemories)
	if err != nil {
		return nil, fmt.Errorf("failed to parse session data: %w", err)
	}

	// Fetch conversation transcript
	messages, err := a.fetchTranscript(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch transcript: %w", err)
	}

	// Compute analysis metrics
	analysis := a.computeAnalysis(sessionData, messages)
	analysis.SessionID = sessionID

	return analysis, nil
}

// AnalyzeMultipleSessions analyzes multiple sessions and returns analyses.
func (a *SessionAnalyzer) AnalyzeMultipleSessions(ctx context.Context, sessionIDs []string) ([]*SessionAnalysis, error) {
	analyses := make([]*SessionAnalysis, 0, len(sessionIDs))

	for _, sessionID := range sessionIDs {
		analysis, err := a.AnalyzeSession(ctx, sessionID)
		if err != nil {
			a.logger.Warn("failed to analyze session", "session_id", sessionID, "error", err)
			continue
		}
		analyses = append(analyses, analysis)
	}

	return analyses, nil
}

// parseSessionData parses session metadata from memvid memories.
func (a *SessionAnalyzer) parseSessionData(memories []memvid.MemoryResult) (*SessionData, error) {
	if len(memories) == 0 {
		return nil, fmt.Errorf("no memories to parse")
	}

	data := &SessionData{
		Metrics: SessionMetrics{},
	}

	for _, mem := range memories {
		// Parse metadata JSON to extract session info
		// This is simplified - in production, you'd unmarshal the JSON properly
		if zone := mem.Memory.Zone; zone == "sessions" {
			// Extract intents, agent_id, outcome from metadata
			if intents, ok := mem.Memory.Metadata["intents"].([]string); ok {
				data.Intents = intents
			}
			if agentID, ok := mem.Memory.Metadata["agent_id"].(string); ok {
				data.AgentID = agentID
			}
			if outcome, ok := mem.Memory.Metadata["outcome"].(string); ok {
				data.Outcome = outcome
			}
			if startTime, ok := mem.Memory.Metadata["start_time"].(string); ok {
				if t, err := time.Parse(time.RFC3339, startTime); err == nil {
					data.StartTime = t
				}
			}
			if endTime, ok := mem.Memory.Metadata["end_time"].(string); ok {
				if t, err := time.Parse(time.RFC3339, endTime); err == nil {
					data.EndTime = t
				}
			}
			if duration, ok := mem.Memory.Metadata["duration_seconds"].(float64); ok {
				data.Metrics.Duration = time.Duration(duration) * time.Second
			}
			if iterations, ok := mem.Memory.Metadata["iterations"].(float64); ok {
				data.Metrics.Iterations = int(iterations)
			}
			if tokenUsage, ok := mem.Memory.Metadata["token_usage"].(float64); ok {
				data.Metrics.TokenUsage = int(tokenUsage)
			}
			if toolCalls, ok := mem.Memory.Metadata["tool_calls"].(float64); ok {
				data.Metrics.ToolCalls = int(toolCalls)
			}
			if agentSwitches, ok := mem.Memory.Metadata["agent_switches"].(float64); ok {
				data.Metrics.AgentSwitches = int(agentSwitches)
			}
			if errors, ok := mem.Memory.Metadata["errors"].(float64); ok {
				data.Metrics.Errors = int(errors)
			}
			if revisions, ok := mem.Memory.Metadata["revisions"].(float64); ok {
				data.Metrics.Revisions = int(revisions)
			}
		}
	}

	data.SessionID = memories[0].Memory.ID

	return data, nil
}

// fetchTranscript fetches the conversation transcript for a session.
func (a *SessionAnalyzer) fetchTranscript(ctx context.Context, sessionID string) ([]Message, error) {
	// Search for conversation messages in memvid
	transcriptMemories, err := a.memvidClient.Search(ctx, fmt.Sprintf("session:%s transcript", sessionID), 100)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch transcript: %w", err)
	}

	messages := make([]Message, 0, len(transcriptMemories))
	for _, mem := range transcriptMemories {
		msg, err := a.parseMessage(mem)
		if err != nil {
			continue
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// parseMessage parses a message from memvid memory.
func (a *SessionAnalyzer) parseMessage(mem memvid.MemoryResult) (Message, error) {
	msg := Message{
		Content: mem.Memory.Content,
	}

	// Extract role from metadata
	if role, ok := mem.Memory.Metadata["role"].(string); ok {
		msg.Role = role
	}

	// Parse timestamp if available
	if ts, ok := mem.Memory.Metadata["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			msg.Timestamp = t
		}
	}

	// Parse tool calls if present
	if toolCallsRaw, ok := mem.Memory.Metadata["tool_calls"].([]interface{}); ok {
		for _, tc := range toolCallsRaw {
			if tcMap, ok := tc.(map[string]interface{}); ok {
				toolCall := ToolCall{
					Name:      getString(tcMap, "name"),
					Arguments: getString(tcMap, "arguments"),
					Result:    getString(tcMap, "result"),
					Success:   getBool(tcMap, "success"),
				}
				msg.ToolCalls = append(msg.ToolCalls, toolCall)
			}
		}
	}

	return msg, nil
}

// computeAnalysis computes analysis metrics from session data and messages.
func (a *SessionAnalyzer) computeAnalysis(data *SessionData, messages []Message) *SessionAnalysis {
	analysis := &SessionAnalysis{
		StartTime:      data.StartTime,
		EndTime:        data.EndTime,
		Duration:       data.Metrics.Duration,
		IterationCount: data.Metrics.Iterations,
		AgentSwitches:  data.Metrics.AgentSwitches,
		RevisionCycles: data.Metrics.Revisions,
		TokenUsage:     data.Metrics.TokenUsage,
		Intents:        data.Intents,
		Outcome:        data.Outcome,
		AgentID:        data.AgentID,
		AnomalyFlags:   make([]string, 0),
	}

	// Compute tool call records from messages
	analysis.ToolCalls = a.extractToolCalls(messages)

	// Compute difficulty score
	analysis.DifficultyScore = a.computeDifficultyScore(data, messages)

	// Detect anomalies
	analysis.AnomalyFlags = a.detectAnomalies(analysis)

	return analysis
}

// extractToolCalls extracts tool call records from messages.
func (a *SessionAnalyzer) extractToolCalls(messages []Message) []ToolCallRecord {
	records := make([]ToolCallRecord, 0)

	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			records = append(records, ToolCallRecord{
				ToolName: tc.Name,
				Success:  tc.Success,
			})
		}
	}

	return records
}

// computeDifficultyScore computes a difficulty score (0.0-1.0) based on session metrics.
func (a *SessionAnalyzer) computeDifficultyScore(data *SessionData, messages []Message) float64 {
	score := 0.0

	// Factor 1: Duration (longer sessions tend to be more difficult)
	if data.Metrics.Duration > 30*time.Minute {
		score += 0.2
	} else if data.Metrics.Duration > 10*time.Minute {
		score += 0.1
	}

	// Factor 2: Iterations (more iterations indicate more difficulty)
	if data.Metrics.Iterations > 20 {
		score += 0.3
	} else if data.Metrics.Iterations > 10 {
		score += 0.15
	}

	// Factor 3: Revisions (indicates implementation issues)
	if data.Metrics.Revisions > 5 {
		score += 0.25
	} else if data.Metrics.Revisions > 2 {
		score += 0.1
	}

	// Factor 4: Errors (indicates problems)
	if data.Metrics.Errors > 3 {
		score += 0.25
	} else if data.Metrics.Errors > 1 {
		score += 0.1
	}

	// Factor 5: Agent switches (thrashing indicates difficulty)
	if data.Metrics.AgentSwitches > 3 {
		score += 0.2
	} else if data.Metrics.AgentSwitches > 1 {
		score += 0.1
	}

	// Cap at 1.0
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// detectAnomalies detects anomalies in the session analysis.
func (a *SessionAnalyzer) detectAnomalies(analysis *SessionAnalysis) []string {
	flags := make([]string, 0)

	// Long duration anomaly (> 60 minutes)
	if analysis.Duration > 60*time.Minute {
		flags = append(flags, "long_duration")
	}

	// High iterations anomaly (> 25 iterations)
	if analysis.IterationCount > 25 {
		flags = append(flags, "high_iterations")
	}

	// Agent thrashing anomaly (> 4 switches)
	if analysis.AgentSwitches > 4 {
		flags = append(flags, "agent_thrashing")
	}

	// High revision cycles anomaly (> 5 revisions)
	if analysis.RevisionCycles > 5 {
		flags = append(flags, "high_revisions")
	}

	return flags
}

// Helper functions for parsing metadata

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}
