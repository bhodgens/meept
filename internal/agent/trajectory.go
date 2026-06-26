package agent

import (
	"encoding/json"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// ReflectionTrajectory captures the rich execution trace of a single agent
// turn for the immediate self-reflection system. This is distinct from the
// legacy LearningPipeline Trajectory type (loop.go) which remains in use by
// the learning adapter; this richer type is consumed by ReflectionCollector
// to render reflection/turn.md.
type ReflectionTrajectory struct {
	UserInput     string                   `json:"user_input"`
	Steps         []ReflectionTrajectoryStep `json:"steps"`
	FinalResponse string                   `json:"final_response"`
	SessionID     string                   `json:"session_id"`
	AgentID       string                   `json:"agent_id"`
	Outcome       string                   `json:"outcome"` // success|partial|failure
	Duration      time.Duration            `json:"duration"`
}

// ReflectionTrajectoryStep describes one step in the reflection execution trace.
type ReflectionTrajectoryStep struct {
	Kind       string `json:"kind"` // assistant_message|tool_call|tool_result|error
	Content    string `json:"content"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolResult string `json:"tool_result,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	RetryOf    string `json:"retry_of,omitempty"`
}

// JSON serializes the trajectory to JSON for template injection.
func (t ReflectionTrajectory) JSON() ([]byte, error) {
	return json.Marshal(t)
}

// buildTrajectory assembles a ReflectionTrajectory from a conversation.
// Truncation: assistant messages 1000 chars, tool results 500 chars,
// errors 300 chars. Trajectory capped at 50 steps. A nil conversation
// yields an empty trajectory (header fields still populated).
func buildTrajectory(conv *Conversation, sessionID, agentID, userInput, outcome string, duration time.Duration) ReflectionTrajectory {
	traj := ReflectionTrajectory{
		UserInput: userInput,
		SessionID: sessionID,
		AgentID:   agentID,
		Outcome:   outcome,
		Duration:  duration,
	}
	if conv == nil {
		return traj
	}
	messages := conv.GetMessages()
	for _, m := range messages {
		if len(traj.Steps) >= 50 {
			break
		}
		switch string(m.Role) {
		case "assistant":
			traj.Steps = append(traj.Steps, ReflectionTrajectoryStep{
				Kind:    "assistant_message",
				Content: truncStr(m.Content, 1000),
			})
		case "tool":
			ts := ReflectionTrajectoryStep{
				Kind:       "tool_result",
				ToolName:   m.Name,
				ToolResult: truncStr(m.Content, 500),
			}
			if m.IsToolError {
				ts.Kind = "error"
				ts.ErrorCode = truncStr(m.Content, 300)
			}
			traj.Steps = append(traj.Steps, ts)
		}
	}
	return traj
}

// Compile-time assertion that llm.ChatMessage is used.
var _ = func(m llm.ChatMessage) bool { return m.Role != "" }
