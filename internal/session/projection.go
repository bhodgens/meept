package session

// Session projection registry (contract S2, .hermes/contracts/03-session-storage.md;
// leaf 03-session-storage/02-session-projection).
//
// Ported semantics from dsh session-projection/src/index.ts:
//   - a ProjectionDefinition is a PURE fold (Init/Apply/optional View)
//     registered on a registry the framework DRIVES forward over every
//     committed session message (OnAppend, post-commit),
//   - Apply returns the SAME state reference when the unit is uninterested
//     (zero downstream work),
//   - load-bearing whole-value rule: fold state is the complete post-change
//     value, never a bare delta,
//   - cache rows carry a state version; a ver mismatch DISCARDS the row at
//     read time (never migrates it) and the key refolds from Init,
//   - checkpointed values are detached (serialized), never the live cell ref.
//
// The registry never performs I/O under its own mutex: folds happen under
// the lock, cache writes and listener notifications happen after release.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
)

// SessionProjectionState is the opaque fold state of one projection unit.
// Units own their concrete type; the registry only stores and passes the
// reference through.
type SessionProjectionState = any

// SessionVersionedMeta is the session context handed to Init.
type SessionVersionedMeta struct {
	SessionID     string
	SchemaVersion int
}

// ProjectionDefinition is one registered fold unit. K is the key type;
// the registry stores keys as their fmt.Sprint rendering.
type ProjectionDefinition[K comparable] struct {
	Key          K
	StateVersion int // bump to invalidate persisted rows; discard, NEVER migrate
	Init         func(SessionVersionedMeta) SessionProjectionState
	Apply        func(state SessionProjectionState, msg Message) SessionProjectionState // same-ref when uninterested
	View         func(state SessionProjectionState) any                                 // optional; must be plain JSON
}

// projectionUnit is the type-erased form the registry stores.
type projectionUnit struct {
	key          string
	stateVersion int
	init         func(SessionVersionedMeta) SessionProjectionState
	apply        func(SessionProjectionState, Message) SessionProjectionState
	view         func(SessionProjectionState) any
}

// projAdapter is satisfied by every ProjectionDefinition[K] instantiation,
// letting Register(def any) erase the key type without reflection.
type projAdapter interface {
	projectionUnit() projectionUnit
}

func (d ProjectionDefinition[K]) projectionUnit() projectionUnit {
	init := d.Init
	if init == nil {
		init = func(SessionVersionedMeta) SessionProjectionState { return nil }
	}
	apply := d.Apply
	if apply == nil {
		apply = func(state SessionProjectionState, _ Message) SessionProjectionState { return state }
	}
	return projectionUnit{
		key:          fmt.Sprint(d.Key),
		stateVersion: d.StateVersion,
		init:         init,
		apply:        apply,
		view:         d.View,
	}
}

func (u projectionUnit) render(state SessionProjectionState) any {
	if u.view != nil {
		return u.view(state)
	}
	return state
}

// SessionProjectionHandle is returned by Register; Dispose unregisters the
// unit (live state is dropped; persisted cache rows are left for ver-discard).
type SessionProjectionHandle struct {
	Key     string
	Dispose func()
}

// SessionProjectionSnapshot is one read of projected values. Values holds
// the rendered fold state per key. Live and Init states are the unit's own
// type; a ver-matching CACHED row is served as json.RawMessage (the
// whole-value rule keeps the serialized form complete — rehydration is the
// consumer's job). Seqs carries the per-key watermark.
type SessionProjectionSnapshot struct {
	Values map[string]any
	Seqs   map[string]int64
}

// projectionChange is one post-fold notification payload.
type projectionChange struct {
	key   string
	value any
	seq   int64
}

// SessionProjectionRegistry drives registered fold units over committed
// session messages and serves projected snapshots. Safe for concurrent use.
type SessionProjectionRegistry struct {
	mu     sync.RWMutex
	units  map[string]projectionUnit
	states map[string]map[string]SessionProjectionState // sessionID -> key -> state
	seqs   map[string]map[string]int64                  // sessionID -> key -> watermark
	cache  *SessionProjectionCache

	listenersMu  sync.Mutex
	listeners    map[int]func(sessionID, key string, value any, seq int64)
	nextListener int

	// versionGuard, when set, reports a session's stored schema version.
	// Snapshot/OnAppend refuse (ErrSessionFormatNewer) or skip folding for
	// sessions stamped ahead of this build (contract S1 consumer rule).
	versionGuard func(sessionID string) (int, error)
}

// NewSessionProjectionRegistry builds a registry over the watermark cache.
// A nil cache disables persistence (folds stay in-memory only).
func NewSessionProjectionRegistry(cache *SessionProjectionCache) *SessionProjectionRegistry {
	return &SessionProjectionRegistry{
		units:     make(map[string]projectionUnit),
		states:    make(map[string]map[string]SessionProjectionState),
		seqs:      make(map[string]map[string]int64),
		cache:     cache,
		listeners: make(map[int]func(sessionID, key string, value any, seq int64)),
	}
}

// SetVersionGuard wires the S1 schema-version check (leaf task T4). The
// guard runs OUTSIDE the registry lock (it may hit the store).
func (r *SessionProjectionRegistry) SetVersionGuard(guard func(sessionID string) (int, error)) {
	if guard == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.versionGuard = guard
}

// Register adds one fold unit. A duplicate key replaces the unit and drops
// its live state (persisted rows are handled by ver-discard at read).
func (r *SessionProjectionRegistry) Register(def any) (SessionProjectionHandle, error) {
	adapter, ok := def.(projAdapter)
	if !ok {
		return SessionProjectionHandle{}, fmt.Errorf("session: Register expects a ProjectionDefinition[K], got %T", def)
	}
	unit := adapter.projectionUnit()

	r.mu.Lock()
	r.units[unit.key] = unit
	for sid, byKey := range r.states {
		delete(byKey, unit.key)
		if len(byKey) == 0 {
			delete(r.states, sid)
		}
	}
	r.mu.Unlock()

	return SessionProjectionHandle{
		Key: unit.key,
		Dispose: func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if cur, ok := r.units[unit.key]; ok && cur.stateVersion == unit.stateVersion {
				delete(r.units, unit.key)
			}
		},
	}, nil
}

// OnAppend eagerly drives every registered unit over one committed message
// (leaf task T2: called post-commit, outside the store lock, best-effort —
// a fold panic or cache failure never fails the save).
func (r *SessionProjectionRegistry) OnAppend(sessionID string, msg Message) {
	if r.refuseNewer(sessionID) {
		return
	}

	var changes []projectionChange
	var rows map[string]ProjectionCheckpointRow

	func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Debug("session projection: fold panic (non-fatal)",
					"session_id", sessionID, "panic", rec)
			}
		}()

		r.mu.Lock()
		defer r.mu.Unlock()
		if len(r.units) == 0 {
			return
		}
		if r.states[sessionID] == nil {
			r.states[sessionID] = make(map[string]SessionProjectionState)
		}
		if r.seqs[sessionID] == nil {
			r.seqs[sessionID] = make(map[string]int64)
		}
		meta := SessionVersionedMeta{SessionID: sessionID, SchemaVersion: SessionFormatVersion}
		for key, unit := range r.units {
			state, ok := r.states[sessionID][key]
			if !ok {
				state = unit.init(meta)
			}
			state = unit.apply(state, msg)
			r.states[sessionID][key] = state
			r.seqs[sessionID][key]++
			seq := r.seqs[sessionID][key]
			changes = append(changes, projectionChange{key: key, value: unit.render(state), seq: seq})
			if rows == nil {
				rows = make(map[string]ProjectionCheckpointRow, len(r.units))
			}
			val, err := json.Marshal(unit.render(state))
			if err != nil {
				slog.Debug("session projection: checkpoint marshal failed (non-fatal)",
					"session_id", sessionID, "key", key, "error", err)
				continue
			}
			rows[key] = ProjectionCheckpointRow{
				Key: key,
				Ver: unit.stateVersion,
				Seq: seq,
				Val: string(val),
			}
		}
	}()

	// Cache write + listener notification: I/O outside the registry lock.
	if r.cache != nil && len(rows) > 0 {
		if err := r.cache.Checkpoint(sessionID, rows); err != nil {
			slog.Debug("session projection: checkpoint failed (non-fatal)",
				"session_id", sessionID, "error", err)
		}
	}
	r.notify(sessionID, changes)
}

func (r *SessionProjectionRegistry) notify(sessionID string, changes []projectionChange) {
	if len(changes) == 0 {
		return
	}
	r.listenersMu.Lock()
	listeners := make([]func(string, string, any, int64), 0, len(r.listeners))
	for _, fn := range r.listeners {
		listeners = append(listeners, fn)
	}
	r.listenersMu.Unlock()
	for _, c := range changes {
		for _, fn := range listeners {
			fn(sessionID, c.key, c.value, c.seq)
		}
	}
}

// refuseNewer reports whether the session's stored format version is ahead
// of this build (S1 guard). Guard errors are treated as non-refusal
// (best-effort path) and logged.
func (r *SessionProjectionRegistry) refuseNewer(sessionID string) bool {
	r.mu.RLock()
	guard := r.versionGuard
	r.mu.RUnlock()
	if guard == nil {
		return false
	}
	version, err := guard(sessionID)
	if err != nil {
		slog.Debug("session projection: version guard failed (non-fatal)",
			"session_id", sessionID, "error", err)
		return false
	}
	return version > SessionFormatVersion
}

// Snapshot reads projected values for one session. Keys empty = every
// registered unit. Read order per key: live fold state; else a cache row
// whose ver matches the unit's StateVersion (served as json.RawMessage —
// whole-value, detached); else (missing row OR ver mismatch = DISCARD,
// never migrate) a fresh Init state. Refuses with ErrSessionFormatNewer
// when the session row is stamped ahead of this build (leaf task T4).
func (r *SessionProjectionRegistry) Snapshot(sessionID string, keys ...string) (SessionProjectionSnapshot, error) {
	if r.refuseNewer(sessionID) {
		return SessionProjectionSnapshot{}, fmt.Errorf("%w: session %s", ErrSessionFormatNewer, sessionID)
	}

	// Live-state read under the lock; cache reads happen after release.
	r.mu.RLock()
	units := make(map[string]projectionUnit, len(r.units))
	for k, u := range r.units {
		units[k] = u
	}
	live := make(map[string]SessionProjectionState, len(units))
	liveSeqs := make(map[string]int64, len(units))
	for k := range units {
		if st, ok := r.states[sessionID][k]; ok {
			live[k] = st
			liveSeqs[k] = r.seqs[sessionID][k]
		}
	}
	meta := SessionVersionedMeta{SessionID: sessionID, SchemaVersion: SessionFormatVersion}
	initStates := make(map[string]SessionProjectionState, len(units))
	for k, u := range units {
		if _, ok := live[k]; !ok {
			initStates[k] = u.init(meta)
		}
	}
	r.mu.RUnlock()

	wanted := keys
	if len(wanted) == 0 {
		wanted = make([]string, 0, len(units))
		for k := range units {
			wanted = append(wanted, k)
		}
	}

	snap := SessionProjectionSnapshot{
		Values: make(map[string]any, len(wanted)),
		Seqs:   make(map[string]int64, len(wanted)),
	}

	var cached map[string]ProjectionCheckpointRow
	for _, key := range wanted {
		unit, ok := units[key]
		if !ok {
			continue // unknown key: omit rather than error (registry is open)
		}
		if st, isLive := live[key]; isLive {
			snap.Values[key] = unit.render(st)
			snap.Seqs[key] = liveSeqs[key]
			continue
		}
		// No live state: consult the cache (I/O outside the lock).
		if cached == nil && r.cache != nil {
			rows, err := r.cache.LoadSession(sessionID)
			if err != nil {
				slog.Debug("session projection: cache load failed (non-fatal)",
					"session_id", sessionID, "error", err)
			} else {
				cached = rows
			}
			if cached == nil {
				cached = map[string]ProjectionCheckpointRow{}
			}
		}
		if row, hit := cached[key]; hit && row.Ver == unit.stateVersion {
			snap.Values[key] = json.RawMessage(row.Val)
			snap.Seqs[key] = row.Seq
			continue
		}
		// Missing row or ver mismatch: DISCARD (never migrate) and serve
		// the fresh Init state.
		snap.Values[key] = unit.render(initStates[key])
		snap.Seqs[key] = 0
	}
	return snap, nil
}

// OnChanged subscribes to post-fold notifications. The returned dispose
// func unsubscribes. Listeners fire outside the registry lock.
func (r *SessionProjectionRegistry) OnChanged(listener func(sessionID, key string, value any, seq int64)) (dispose func()) {
	if listener == nil {
		return func() {}
	}
	r.listenersMu.Lock()
	id := r.nextListener
	r.nextListener++
	r.listeners[id] = listener
	r.listenersMu.Unlock()
	return func() {
		r.listenersMu.Lock()
		defer r.listenersMu.Unlock()
		delete(r.listeners, id)
	}
}
