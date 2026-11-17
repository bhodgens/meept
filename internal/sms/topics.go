package sms

// Bus topics for the SMS lane (leaf 03/01, contract §5): the daemon
// arming glue publishes typed payloads on ingest (inbound message) and
// on terminal DLR resolution (state changed); the dispatch tree and #132
// consume them. Topic NAMES are wire contract — pin exactly, the strings
// below are what subscribers attach to.
//
// Pattern: internal/bus/topic.go typed topics (NewTopic/PublishT/
// SubscribeT). Raw publishes to the same string name decode into these
// payloads (interop rule in topic.go's NewTopic doc), so observability
// tooling can republish raw JSON on the same lane as the daemon's typed
// publishes.

import (
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// InboundMessage is the fan-out payload for a received SMS: normalized
// E.164 endpoints, the text, the provider event id (for traceability
// back to sms_events_raw), and the provider timestamp.
type InboundMessage struct {
	EventID string    `json:"event_id"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Text    string    `json:"text"`
	At      time.Time `json:"at"`
}

// InboundTopic carries every accepted inbound SMS after it has been
// durably appended to sms_events_raw.
var InboundTopic = bus.NewTopic[InboundMessage]("sms.inbound.received")

// StateChanged is the terminal-DLR fan-out payload: provider message id
// plus the resolved status and error text (empty on success).
type StateChanged struct {
	ProviderMessageID string `json:"provider_message_id"`
	Status            string `json:"status"`
	Err               string `json:"err"`
}

// StateTopic carries every sms_message_state write that passed the
// monotonic guard (out-of-order writes are never published).
var StateTopic = bus.NewTopic[StateChanged]("sms.state.changed")
