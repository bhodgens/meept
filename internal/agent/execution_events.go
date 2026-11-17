package agent

import (
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/comm/wsclass"
)

// ExecutionResultEvent is the shared wire payload for bot and employee
// execution results, published on TopicBotResult ("bot.result") and
// TopicEmployeeResult ("employee.result"). The SAME struct serves both
// tiers: the topic name is the sole tier discriminator and classifies as
// WSProgress.
//
// Schema note (issue #140): there is deliberately NO `kind` field and no
// ExecutionKind type — per operator decision the wire shape is shared and
// untagged, and callers discriminate on the topic name, never on an
// in-payload discriminator. Status is a plain string carrying
// completed|failed|skipped.
type ExecutionResultEvent struct {
	AgentID    string     `json:"agent_id"`
	Status     string     `json:"status"`
	Output     string     `json:"output,omitempty"`
	TokensUsed int        `json:"tokens_used"`
	Error      string     `json:"error,omitempty"`
	DurationMS int64      `json:"duration_ms"`
	StopReason StopReason `json:"stop_reason,omitempty"`
	TriggerRef string     `json:"trigger_ref,omitempty"`
	SessionID  string     `json:"session_id,omitempty"`
	Timestamp  time.Time  `json:"timestamp"`
}

// WSClass marks this event for the WS relay's progress lane.
func (ExecutionResultEvent) WSClass() wsclass.WSClass { return wsclass.WSProgress }

// TopicBotResult carries ExecutionResultEvent publishes on the
// "bot.result" bus topic. The topic name is the tier discriminator (issue
// #140); the payload carries no kind field.
var TopicBotResult = bus.NewTopic[ExecutionResultEvent]("bot.result")

// TopicEmployeeResult carries ExecutionResultEvent publishes on the
// "employee.result" bus topic. It shares the exact ExecutionResultEvent
// wire shape with TopicBotResult (issue #140).
var TopicEmployeeResult = bus.NewTopic[ExecutionResultEvent]("employee.result")
