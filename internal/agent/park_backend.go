package agent

import (
	"context"
	"time"
)

// parkBackend is the C-A1 persistence seam for TurnParker (tree
// 20261110-parked-turns-as-jobs leaf 01). The parker's storage traffic —
// Park Save, drain Delete, re-arm LoadOwned, plus an Arm hook fired at
// Start — routes through ONE backend when one is wired via SetBackend.
// A backend REPLACES the store path, never mirrors it: with a backend set
// and a store also wired (the park-key plumbing still needs the store for
// key derivation), Save/Delete/LoadOwned go to the backend ONLY.
//
// The signature is frozen by park_queue_gate_test.go (TestParkBackendSeam-
// Wrapped pins it): LoadOwned takes NO now parameter (backends prune on
// their own clock), and the give-up/attempt-cap logic stays in the parker
// — a backend never routes give-ups, it only stores rows.
//
// The DEFAULT path (no SetBackend, or SetBackend(nil)) does NOT go through
// a backend object at all: it keeps the pre-seam parkPersistence code path
// byte-identically (the gate pins "without SetBackend the parker keeps the
// store path byte-identically"). sqliteParkBackend below is the equivalent
// wrapper for consumers that opt into the seam.
type parkBackend interface {
	// Save persists one parked record (owner already stamped by the
	// parker; the record is passed verbatim per the C-A1 pin).
	Save(ctx context.Context, rec ParkedTurnRecord) error
	// Delete removes a record by kind + persistence key, mirroring
	// ParkPersistence.Delete. The parker calls it BEFORE the resume
	// callback (at-most-once resume), including for give-ups.
	Delete(ctx context.Context, kind ParkKind, key string) error
	// LoadOwned returns exactly the records Save stamped with owner and
	// prunes the caller-visible expired rows (empty owner = no filter,
	// same convention as the parker's C-K1 stamping). No now param: the
	// backend prunes on its own clock.
	LoadOwned(ctx context.Context, kind ParkKind, owner string) ([]ParkedTurnRecord, error)
	// Arm starts whatever drain/claim loop the backend owns. Called once
	// from TurnParker.Start after re-arm.
	Arm(ctx context.Context)
}

// keyedParkBackend is the OPTIONAL capability seam (interface-at-consumer,
// the same regime as OwnerScopedPersistence) a parkBackend may implement so
// the parker's persistence key — the identity the delete/re-arm paths
// resolve rows by — survives the C-A1 Save/Load boundary, which carries no
// key argument. Backends that implement it get byte-identical key semantics
// with the store path: SaveKeyed receives the parker's key, LoadOwnedKeyed
// returns survivors WITH their keys so drain deletes hit exactly the rows
// re-armed. A backend that does NOT implement it falls back to the plain
// Save/LoadOwned; re-armed records then carry empty keys, so the parker
// cannot Delete them on drain — such backends must make resurrection
// idempotent on their own (leaf 02's queue backend owns that decision).
type keyedParkBackend interface {
	parkBackend
	// SaveKeyed mirrors ParkPersistence.Save exactly (kind-scoped,
	// parker-derived key).
	SaveKeyed(ctx context.Context, kind ParkKind, key string, rec ParkedTurnRecord) error
	// LoadOwnedKeyed mirrors the parker's re-arm load: owner-filtered
	// when owner != "" (and the store supports LoadOwned), kind-only
	// sticky-filter Load otherwise; prunes expired at now; returns
	// records with their park keys.
	LoadOwnedKeyed(ctx context.Context, kind ParkKind, owner string, now time.Time) ([]ParkedTurnRecord, []string, error)
}

// sqliteParkBackend is the C-A1 default-implementation wrapper: it holds
// the SAME ParkPersistence the parker would use unwired and forwards every
// call to the SAME store methods with the SAME semantics — no logic change,
// this leaf is pure refactor insurance that makes leaf 02 additive.
// Construction goes through the parker's SetBackend callers; the owner
// lives on each record (stamped by the parker), not on the wrapper.
type sqliteParkBackend struct {
	store ParkPersistence
	kind  ParkKind
	// nowFn overrides time.Now for the plain-path clock (test seam, same
	// regime as TurnParker.nowFunc; nil means time.Now).
	nowFn func() time.Time
}

var (
	_ parkBackend      = (*sqliteParkBackend)(nil)
	_ keyedParkBackend = (*sqliteParkBackend)(nil)
)

func (b *sqliteParkBackend) now() time.Time {
	if b.nowFn != nil {
		return b.nowFn()
	}
	return time.Now()
}

// Save is the plain C-A1 entry: with no key argument on the interface, the
// wrapper derives the chat-style default key (class|session|wall-clock
// nanos — the same formula as parkKeyForNewRecord) on its own clock. The
// parker never reaches this method (it prefers SaveKeyed); it exists for
// stand-alone backend users.
func (b *sqliteParkBackend) Save(ctx context.Context, rec ParkedTurnRecord) error {
	if b == nil || b.store == nil {
		return nil
	}
	key := parkClassString(rec.Class) + "|" + rec.SessionID + "|" + b.now().UTC().Format(chatPersistenceKeyTimeFormat)
	return b.store.Save(ctx, b.kind, key, rec)
}

// SaveKeyed forwards to ParkPersistence.Save verbatim — same kind scoping,
// same parker-derived key, same store upsert semantics.
func (b *sqliteParkBackend) SaveKeyed(ctx context.Context, kind ParkKind, key string, rec ParkedTurnRecord) error {
	if b == nil || b.store == nil {
		return nil
	}
	return b.store.Save(ctx, kind, key, rec)
}

// Delete forwards to ParkPersistence.Delete verbatim (idempotent absent-row
// semantics unchanged).
func (b *sqliteParkBackend) Delete(ctx context.Context, kind ParkKind, key string) error {
	if b == nil || b.store == nil {
		return nil
	}
	return b.store.Delete(ctx, kind, key)
}

// LoadOwned is the plain C-A1 entry (no now param): prune on the wrapper's
// own clock and drop the keys (the interface returns records only).
func (b *sqliteParkBackend) LoadOwned(ctx context.Context, kind ParkKind, owner string) ([]ParkedTurnRecord, error) {
	records, _, err := b.LoadOwnedKeyed(ctx, kind, owner, b.now())
	return records, err
}

// LoadOwnedKeyed reproduces the parker's re-arm load branch exactly:
// owner != "" + OwnerScopedPersistence store -> LoadOwned; anything else
// (empty owner, or a store without the capability) -> the legacy kind-only
// Load, which keeps honoring the sticky SetOwnerFilter test seam. Prune-on-
// load (expired rows deleted, non-owner expired rows untouched) lives in
// the store and is unchanged.
func (b *sqliteParkBackend) LoadOwnedKeyed(ctx context.Context, kind ParkKind, owner string, now time.Time) ([]ParkedTurnRecord, []string, error) {
	if b == nil || b.store == nil {
		return nil, nil, nil
	}
	if owner != "" {
		if osp, ok := b.store.(OwnerScopedPersistence); ok {
			return osp.LoadOwned(ctx, kind, owner, now)
		}
	}
	return b.store.Load(ctx, kind, now)
}

// Arm is a documented no-op: the sqlite store owns no drain loop — the
// TurnParker's existing polling goroutine (drainDue) keeps running on the
// poll interval and re-drains the in-memory queue, so there is nothing to
// start. (A queue-backed implementation uses Arm to launch its claim/wake
// loop; the parker-side drain stays exactly where it is.)
func (b *sqliteParkBackend) Arm(context.Context) {}
