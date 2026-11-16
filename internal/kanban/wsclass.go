package kanban

import "github.com/caimlas/meept/internal/comm/wsclass"

// WSClass markers for the kanban payloads that reach the WebSocket relay
// (kanban-board plan tree, master.md C8; leaf 05/07 'ws-event-class').
// Every kanban.* bus topic — kanban.card.moved (CardMoved),
// kanban.board.changed (BoardChanged), and the five kanban.event.*.state
// topics (StateEvent) — classifies as WSBoard, wire type "board_event",
// relayed like progress and NEVER chat_message (the blank-bubble
// invariant in internal/comm/AGENTS.md). This reading of C8 takes "board
// payloads" as the whole kanban.* bus surface: the StateEvent streams are
// board-relevant transitions of the work items the board projects, so
// they ride WSBoard too.
//
// Placement follows the internal/agent/handler.go:320 precedent (the
// payload-owning package provides the marker); this file is additive —
// human.go, types.go, and events.go stay untouched. wsclass imports
// NOTHING from internal/, so there is no import cycle.
//
// The wire decoders that let the relay consult these markers are
// registered in internal/comm/http/server_wsclass.go (typedPayloadDecoders),
// gated by internal/comm/http/server_wsclass_gate_test.go.

// WSClass implements wsclass.WSClassified: a card move is a board delta.
func (CardMoved) WSClass() wsclass.WSClass { return wsclass.WSBoard }

// WSClass implements wsclass.WSClassified: a board-level change (including
// the C10 wip_warning note payload) is a board delta.
func (BoardChanged) WSClass() wsclass.WSClass { return wsclass.WSBoard }

// WSClass implements wsclass.WSClassified: the five kanban.event.*.state
// topics carry work-item state transitions projected onto the board, so
// they classify as board deltas too (C8 reading above).
func (StateEvent) WSClass() wsclass.WSClass { return wsclass.WSBoard }
