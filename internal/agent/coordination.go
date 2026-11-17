package agent

import (
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/comm/wsclass"
)

// AgentTurnState is the lifecycle state of an agent's turn, published on
// TopicTurnState so sibling/child agents can observe each other's activity.
type AgentTurnState string

const (
	TurnStateRunning        AgentTurnState = "running"
	TurnStateIdle           AgentTurnState = "idle"
	TurnStateRequiresAction AgentTurnState = "requires_action"
)

// StopReason explains why a turn left the running state.
type StopReason string

const (
	StopReasonEndTurn       StopReason = "end_turn"
	StopReasonCancelled     StopReason = "cancelled"
	StopReasonMaxTokens     StopReason = "max_tokens"
	StopReasonRefusal       StopReason = "refusal"
	StopReasonMaxIterations StopReason = "max_iterations"
	StopReasonError         StopReason = "error"
)

// TurnStateEvent is the payload published on TopicTurnState whenever an
// agent's turn state changes. It models TurnTerminalEvent's wire shape
// (internal/agent/handler.go) and classifies as WSProgress.
//
// Schema note (Q2 ratified 2026-11-16, see docs/plans/20261115-inter-agent-coordination/master.md):
// the SAME struct carries both the plain delegate/ACP shape (phase absent) and
// the pair-phase shape (phase = actor_turn|reviewer_turn|completed|failed).
// `phase` is OPTIONAL — consumers must not require it and must tolerate its
// absence; pair consumers discriminate on it, never on struct identity.
// Both shapes decode without error (pinned by TestTurnStateEvent_BothShapesDecode).
type TurnStateEvent struct {
	AgentID    string         `json:"agent_id"`
	SessionID  string         `json:"session_id,omitempty"`
	ChildID    string         `json:"child_id,omitempty"`
	State      AgentTurnState `json:"state"`
	StopReason StopReason     `json:"stop_reason,omitempty"`
	Phase      string         `json:"phase,omitempty"`
	Timestamp  time.Time      `json:"timestamp"`
}

// WSClass marks this event for the WS relay's progress lane.
func (TurnStateEvent) WSClass() wsclass.WSClass { return wsclass.WSProgress }

// TopicTurnState carries TurnStateEvent publishes on the "agent.turn.state" bus topic.
var TopicTurnState = bus.NewTopic[TurnStateEvent]("agent.turn.state")
