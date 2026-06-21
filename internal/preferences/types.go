package preferences

import "time"

// ParsedInstruction represents a user instruction extracted from natural language.
type ParsedInstruction struct {
	Trigger    TriggerConfig
	Action     ActionConfig
	Scope      string
	Priority   string
	RawInput   string
	Confidence float64
	CreatedAt  time.Time
}

// TriggerConfig holds the parsed trigger configuration.
type TriggerConfig struct {
	Type       string            // "cron", "post_hook", "event", "intent", "git"
	Pattern    string
	Conditions map[string]string
}

// ActionConfig holds the parsed action configuration.
type ActionConfig struct {
	Tool    string
	Args    map[string]any
	AgentID string
}
