package agent

import "github.com/caimlas/meept/internal/bus"

// TopicTurnTerminal is the typed declaration for the turn.terminal bus
// topic (typed-bus-topics leaf 02). Publishers MUST use bus.PublishT with
// this declaration; future subscribers MUST use bus.SubscribeT with it.
// Payload is the frozen TurnTerminalEvent - see its doc comment (CLOSED
// field set).
//
// Placement note: the declaration lives IN package agent (not pkg/models
// or internal/bus) because TurnTerminalEvent lives here and agent already
// imports bus; the reverse imports would be a cycle.
var TopicTurnTerminal = bus.NewTopic[TurnTerminalEvent]("turn.terminal")

// RAW TOPIC (polymorphic payload): "agent.quota_wait" carries QuotaEvent
// (internal/agent/loop.go quota tracker wiring + quota_episode.go episode
// publishers), ParkTurnEvent (internal/agent/parked_turn.go throttle
// park/resume), and a job-level map payload
// (internal/daemon/components.go publishQuotaWait). Do NOT wrap it in
// Topic[T] until the shapes unify - a single T silently zero-fills the
// other shapes' fields. Consumers (services/quota_notifier.go,
// comm/http/server.go WS classifier, tui/app.go) decode to
// map[string]any and discriminate by class/reason keys.
