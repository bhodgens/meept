package http

import (
	"encoding/json"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/comm/wsclass"
	"github.com/caimlas/meept/internal/kanban"
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

// decodeTurnStateWSClass unmarshals an agent.turn.state payload into the
// frozen agent.TurnStateEvent struct (inter-agent-coordination leaf 04-01,
// C6). The marker classifies the frame as agent_progress — never
// chat_message.
func decodeTurnStateWSClass(raw json.RawMessage) (wsclass.WSClassified, error) {
	var ev agent.TurnStateEvent
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

// decodeKanbanCardMovedWSClass unmarshals a kanban.card.moved payload into
// the frozen kanban.CardMoved struct (kanban-board C8, leaf 05/07). The
// marker classifies the frame as board_event — never chat_message.
func decodeKanbanCardMovedWSClass(raw json.RawMessage) (wsclass.WSClassified, error) {
	var ev kanban.CardMoved
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, err
	}
	return ev, nil
}

// decodeKanbanBoardChangedWSClass unmarshals a kanban.board.changed payload
// into the frozen kanban.BoardChanged struct (C2-pinned shape
// {board_id, note?}). Marker: board_event.
func decodeKanbanBoardChangedWSClass(raw json.RawMessage) (wsclass.WSClassified, error) {
	var ev kanban.BoardChanged
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, err
	}
	return ev, nil
}

// decodeKanbanStateEventWSClass unmarshals one of the five
// kanban.event.*.state payloads into the frozen kanban.StateEvent struct
// (C1/C2). Marker: board_event — the work-item transitions the board
// projects are board deltas (C8 reading in internal/kanban/wsclass.go).
func decodeKanbanStateEventWSClass(raw json.RawMessage) (wsclass.WSClassified, error) {
	var ev kanban.StateEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, err
	}
	return ev, nil
}

// typedPayloadDecoders maps topic names to decoders for payloads with
// WSClass markers. Entries exist only for topics migrated to Topic[T]
// (turn.terminal; acp.permission.requested/.resolved; the kanban board
// surface kanban.card.moved, kanban.board.changed and the five
// kanban.event.*.state topics, kanban-board C8 leaf 05/07). Legacy
// fallback still classifies any topic missing here.
var typedPayloadDecoders = map[string]func(json.RawMessage) (wsclass.WSClassified, error){
	agent.TopicTurnTerminal.Name:         decodeTurnTerminalWSClass,
	agent.TopicTurnState.Name:            decodeTurnStateWSClass,
	bus.TopicACPPermissionRequested.Name: decodeACPPermissionWSClass,
	bus.TopicACPPermissionResolved.Name:  decodeACPPermissionWSClass,
	kanban.TopicCardMoved.Name:           decodeKanbanCardMovedWSClass,
	kanban.TopicBoardChanged.Name:        decodeKanbanBoardChangedWSClass,
	kanban.TopicTaskState.Name:           decodeKanbanStateEventWSClass,
	kanban.TopicJobState.Name:            decodeKanbanStateEventWSClass,
	kanban.TopicPlanState.Name:           decodeKanbanStateEventWSClass,
	kanban.TopicStepState.Name:           decodeKanbanStateEventWSClass,
	kanban.TopicGoalState.Name:           decodeKanbanStateEventWSClass,
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
