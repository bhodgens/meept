// Package wsclass defines the WSClass enum and the WSClassified marker
// interface used by the WebSocket relay (internal/comm/http) to classify
// bus events into frontend event types. Payloads that reach the relay
// implement WSClassified; classification prefers the marker over the
// legacy topic-prefix table (see server.go's legacy fallback comment).
//
// wsclass imports NOTHING from internal/ — the import direction is
// agent -> wsclass only.
package wsclass

// WSClass enumerates the six frontend event types produced by the WS
// relay's classification (formerly the topic-prefix switch in
// transformBusEventToWS).
type WSClass int

const (
	WSChatMessage   WSClass = iota // chat_message
	WSProgress                     // agent_progress
	WSMetricsUpdate                // metrics_update
	WSJobUpdate                    // job_update
	WSPlanUpdate                   // plan_update
	WSEvent                        // generic "event" (the old default)
)

// WSClassified is implemented by bus event payloads that reach the
// WebSocket relay. Classification prefers the marker over the legacy
// topic-prefix table.
type WSClassified interface {
	WSClass() WSClass
}

// String maps a WSClass to its exact wire string (the "type" field the
// frontend receives). This switch is the single mapping site between the
// enum and wire strings; the exhaustive linter guards its coverage.
func (c WSClass) String() string {
	switch c {
	case WSChatMessage:
		return "chat_message"
	case WSProgress:
		return "agent_progress"
	case WSMetricsUpdate:
		return "metrics_update"
	case WSJobUpdate:
		return "job_update"
	case WSPlanUpdate:
		return "plan_update"
	case WSEvent:
		return "event"
	default:
		return "event"
	}
}
