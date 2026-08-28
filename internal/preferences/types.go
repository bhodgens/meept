package preferences

import "time"

// ParsedInstruction represents a user instruction extracted from natural language.
type ParsedInstruction struct {
	Trigger    TriggerConfig `json:"trigger"`
	Action     ActionConfig  `json:"action"`
	Scope      string        `json:"scope"`
	Priority   string        `json:"priority"`
	RawInput   string        `json:"raw_input"`
	Confidence float64       `json:"confidence"`
	CreatedAt  time.Time     `json:"created_at"`
}

// TriggerConfig holds the parsed trigger configuration.
type TriggerConfig struct {
	Type       string            `json:"type"` // "cron", "post_hook", "event", "intent", "git"
	Pattern    string            `json:"pattern"`
	Conditions map[string]string `json:"conditions,omitempty"`
}

// ActionConfig holds the parsed action configuration.
type ActionConfig struct {
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args,omitempty"`
	AgentID string         `json:"agent_id,omitempty"`
}
