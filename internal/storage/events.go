// Package storage declares the record-change event vocabulary for durable
// domain stores (contract S4, .hermes/contracts/03-session-storage.md; leaf
// 03-session-storage/04-storage-events).
//
// Ported semantics from dsh storage-domain/src/events.ts:10-47 — "every
// durable write emits one event after the backend resolves durability,
// carrying the new snapshot and an operation discriminant — never the old
// value." The DomainChanged union {domain,table,key} + put|deleted maps to
// meept's single Store string + Key + Op (master C6 shape
// {store,key,op,value}).
//
// Import direction is ONE-WAY: durable stores (internal/session, ...) import
// internal/storage; this package imports only internal/bus + stdlib and must
// NEVER import a store package (import-cycle gate).
package storage

import (
	"encoding/json"

	"github.com/caimlas/meept/internal/bus"
)

// ChangeOp is the operation discriminant. Closed set: put | delete (T3
// validates on publish; an unknown op is rejected, never emitted).
type ChangeOp string

const (
	// ChangeOpPut marks a durable write; Value carries the NEW snapshot.
	ChangeOpPut ChangeOp = "put"
	// ChangeOpDelete marks a durable removal; Value is absent (tombstone).
	ChangeOpDelete ChangeOp = "delete"
)

// RecordChanged is the storage.record.changed payload (contract S4):
// {store,key,op,value}. On put, Value is the new snapshot (json.RawMessage);
// on delete, Value is absent (tombstone — a diffing consumer keeps its own
// previous snapshot; the old value is NEVER carried, dsh events.ts:10-47).
type RecordChanged struct {
	Store string          `json:"store"`
	Key   string          `json:"key"`
	Op    ChangeOp        `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}

// StorageRecordChangedTopic is the typed bus topic. Name frozen by contract
// S4: "storage.record.changed". Declared package-level per the typed-topic
// precedent (internal/bus/acp_topics.go) so publisher and subscriber share
// the compile-time-checked payload type.
var StorageRecordChangedTopic = bus.NewTopic[RecordChanged]("storage.record.changed")

// ValidOp reports whether op is in the closed put|delete set (T3 closed
// path: an unknown op must be rejected rather than emitted unvalidated).
func ValidOp(op ChangeOp) bool {
	return op == ChangeOpPut || op == ChangeOpDelete
}
