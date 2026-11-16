package agent

// turnstate.go hosts the shared turn-state emission helpers for the
// agent-coordination plane (plan
// docs/plans/20261115-inter-agent-coordination, branch 02-acp-emit leaf
// 02-01). Every delegate child and pair session publishes TurnStateEvent
// edges on TopicTurnState so sibling/parent agents can observe each other's
// activity without polling; the vocabulary (AgentTurnState, StopReason,
// TurnStateEvent, TopicTurnState) lives in coordination.go.
//
// Two transport shapes are supported BY DESIGN:
//   - a *bus.MessageBus holder (PairOrchestrator, later ACP) publishes the
//     typed event directly via bus.PublishT on TopicTurnState;
//   - DelegateManager holds NO bus — it owns only the generic
//     `publisher func(topic string, payload any)` seam (delegate.go, wired
//     in dsh_wiring.go). Its emissions route through that same seam, passing
//     the TurnStateEvent struct so the JSON marshalling happens in the
//     publisher adapter exactly as it does for agent.delegate.result.

import (
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// publishTurnState publishes ev on TopicTurnState via bus.PublishT. The
// timestamp defaults to now (UTC) when zero so every event carries a
// usable clock stamp even when the producer did not set one. A nil bus is
// a no-op (unwired surfaces stay byte-identical).
func publishTurnState(b *bus.MessageBus, ev TurnStateEvent) {
	if b == nil {
		return
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	bus.PublishT(b, TopicTurnState, "agent", ev)
}

// emitTurnStatePublisher publishes ev through the delegate publisher seam
// (the same func(topic, payload) that carries agent.delegate.result). The
// TurnStateEvent struct is passed through unmarshalled — the publisher
// adapter owns the wire encoding. A nil publisher is a no-op.
func emitTurnStatePublisher(publisher func(topic string, payload any), ev TurnStateEvent) {
	if publisher == nil {
		return
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	publisher(TopicTurnState.Name, ev)
}
