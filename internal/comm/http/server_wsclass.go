package http

import (
	"encoding/json"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/comm/wsclass"
	"github.com/caimlas/meept/pkg/models"
)

// decodeTurnTerminalWSClass unmarshals a turn.terminal payload into the
// frozen agent.TurnTerminalEvent struct.
func decodeTurnTerminalWSClass(raw json.RawMessage) (wsclass.WSClassified, error) {
	var ev agent.TurnTerminalEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, err
	}
	return ev, nil
}

// decodeACPPermissionWSClass unmarshals an acp.permission.* payload into
// the frozen bus.ACPPermissionEvent struct (acp-human-permission-relay
// leaf 03). The marker classifies the frame as agent_progress — never
// chat_message.
func decodeACPPermissionWSClass(raw json.RawMessage) (wsclass.WSClassified, error) {
	var ev bus.ACPPermissionEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, err
	}
	return ev, nil
}

// typedPayloadDecoders maps topic names to decoders for payloads with
// WSClass markers. Entries exist only for topics migrated to Topic[T]
// (turn.terminal; acp.permission.requested/.resolved). Legacy fallback
// still classifies any topic missing here.
var typedPayloadDecoders = map[string]func(json.RawMessage) (wsclass.WSClassified, error){
	agent.TopicTurnTerminal.Name:         decodeTurnTerminalWSClass,
	bus.TopicACPPermissionRequested.Name: decodeACPPermissionWSClass,
	bus.TopicACPPermissionResolved.Name:  decodeACPPermissionWSClass,
}

// classifyTypedPayload decodes msg's payload into a typed value carrying
// a WSClassified marker, when the topic has an entry in
// typedPayloadDecoders. Returns ok=false on a missing entry or a decode
// failure (the legacy prefix fallback classifies those).
func classifyTypedPayload(msg *models.BusMessage) (wsclass.WSClass, bool) {
	decode, ok := typedPayloadDecoders[msg.Topic]
	if !ok {
		return 0, false
	}
	typed, err := decode(msg.Payload)
	if err != nil {
		return 0, false
	}
	return typed.WSClass(), true
}
