package storage

// Nil-safe change emitter (contract S4, leaf 03-session-storage/04 task T2).

import (
	"log/slog"

	"github.com/caimlas/meept/internal/bus"
)

// ChangeEmitter publishes RecordChanged events on StorageRecordChangedTopic.
// It is nil-safe in BOTH dimensions: a nil *ChangeEmitter receiver no-ops,
// and an emitter constructed over a nil bus no-ops. Publish never blocks the
// caller's commit path: bus.PublishT delivery is non-blocking
// (drop-on-full), and a rejected/undeliverable payload is logged, never
// returned — durability of the record is the store's concern, the event is
// best-effort.
type ChangeEmitter struct {
	bus *bus.MessageBus
}

// NewChangeEmitter wires an emitter to a message bus. A nil bus yields a
// no-op emitter (never nil-dereferences on Publish).
func NewChangeEmitter(b *bus.MessageBus) *ChangeEmitter {
	return &ChangeEmitter{bus: b}
}

// Publish validates the payload (T3 closed path: unknown op is logged and
// skipped, never emitted) and sends it on the typed topic. source names the
// publishing component (bus message provenance). No-op on nil receiver or
// nil bus; never blocks or fails the commit.
func (e *ChangeEmitter) Publish(source string, rc RecordChanged) {
	if e == nil || e.bus == nil {
		return
	}
	if !ValidOp(rc.Op) {
		slog.Warn("storage: rejected record-change event with unknown op",
			"store", rc.Store, "key", rc.Key, "op", string(rc.Op))
		return
	}
	bus.PublishT(e.bus, StorageRecordChangedTopic, source, rc)
}
