package agent

import (
	"log/slog"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// RoutingTelemetry records routing decisions as bus events.
//
// Fire-and-forget: nil bus = all methods no-op; publish failures are logged
// at Debug and swallowed (telemetry must never fail a step). Topics owned by
// this recorder: routing.decision, routing.hint_miss, routing.handoff_outcome
// (routing.telemetry belongs to the successor-hints leaf — do not publish it).
type RoutingTelemetry struct {
	bus    *bus.MessageBus
	logger *slog.Logger
}

// NewRoutingTelemetry creates a routing telemetry recorder. A nil logger
// falls back to slog.Default(); a nil bus makes every Record* method a no-op.
func NewRoutingTelemetry(bus *bus.MessageBus, logger *slog.Logger) *RoutingTelemetry {
	if logger == nil {
		logger = slog.Default()
	}
	return &RoutingTelemetry{bus: bus, logger: logger}
}

// emit is the shared fire-and-forget core: nil bus/nil receiver = no-op;
// marshal failures are logged at Debug and swallowed so telemetry can never
// fail a routing decision.
func (rt *RoutingTelemetry) emit(topic string, data map[string]any) {
	if rt == nil || rt.bus == nil {
		return
	}
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "tactical-scheduler", data)
	if err != nil {
		rt.logger.Debug("routing telemetry: failed to create bus message", "topic", topic, "error", err)
		return
	}
	rt.bus.Publish(topic, msg)
}

// RecordHintRoute publishes "routing.decision":
// {step_id, agent_id, source} — source ∈ "hint_table"|"explicit"|"chat_fallback".
func (rt *RoutingTelemetry) RecordHintRoute(stepID, agentID, source string) {
	rt.emit("routing.decision", map[string]any{
		"step_id":  stepID,
		"agent_id": agentID,
		"source":   source,
	})
}

// RecordHintMiss publishes "routing.hint_miss":
// {step_id, tool_hint, resolved_to: "chat"}.
func (rt *RoutingTelemetry) RecordHintMiss(stepID, toolHint string) {
	rt.emit("routing.hint_miss", map[string]any{
		"step_id":     stepID,
		"tool_hint":   toolHint,
		"resolved_to": "chat",
	})
}

// RecordHandoff publishes "routing.handoff_outcome":
// {task_id, from_step_id, to_agent_id, accepted}.
func (rt *RoutingTelemetry) RecordHandoff(taskID, fromStepID, toAgentID string, accepted bool) {
	rt.emit("routing.handoff_outcome", map[string]any{
		"task_id":      taskID,
		"from_step_id": fromStepID,
		"to_agent_id":  toAgentID,
		"accepted":     accepted,
	})
}
