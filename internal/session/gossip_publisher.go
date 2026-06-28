package session

import "time"

// TurnGossipPublisher broadcasts session turn events to cluster peers.
//
// The session package intentionally defines this interface (rather than
// importing the memory package's DualStore) so that the SQLiteStore can
// emit gossip events without a hard dependency on internal/memory —
// following the "define a small interface in the consumer package" pattern
// documented in CLAUDE.md. The daemon wires memory.DualStore (or any other
// implementation) into the session store via WithTurnGossipPublisher /
// SetTurnGossipPublisher.
//
// PublishTurn is expected to be non-blocking. Implementations that perform
// network I/O should do so asynchronously; callers trust that a nil return
// means the event was queued, not delivered.
type TurnGossipPublisher interface {
	PublishTurn(sessionID, turnID, role, content string, ts time.Time) error
}

// nopTurnGossipPublisher is the zero-value default; it is a no-op so that
// SQLiteStore can call PublishTurn unconditionally when dualStore is wired
// only on nodes running with cluster sync enabled.
type nopTurnGossipPublisher struct{}

func (nopTurnGossipPublisher) PublishTurn(string, string, string, string, time.Time) error {
	return nil
}
