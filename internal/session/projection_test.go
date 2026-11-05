package session

// G3 gate (03-session-storage/00T-tests.md): projection registry fold-on-append
// + state-version discard. Pins contract S2 (.hermes/contracts/03-session-storage.md):
//   - a registered unit folds a committed message via OnAppend
//   - Apply returns the SAME state reference when uninterested (zero work)
//   - a cache row persisted under an older StateVersion is DISCARDED at read,
//     never migrated (dsh projection-cache spec.ts:24-27)
//
// RED until leaf 02-session-projection lands (symbols undefined today).

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// summaryState is the fold state for the test projection unit.
type summaryState struct {
	MsgCount int    `json:"msg_count"`
	LastRole string `json:"last_role"`
	LastText string `json:"last_text"`
}

func TestProjection_RegistryFoldOnAppend(t *testing.T) {
	dbPath := ":memory:"
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cache, err := NewSessionProjectionCache(db)
	if err != nil {
		t.Fatalf("NewSessionProjectionCache: %v", err)
	}
	reg := NewSessionProjectionRegistry(cache)

	def := ProjectionDefinition[string]{
		Key:          "session-summary",
		StateVersion: 1,
		Init: func(SessionVersionedMeta) SessionProjectionState {
			return &summaryState{}
		},
		Apply: func(state SessionProjectionState, msg Message) SessionProjectionState {
			s, ok := state.(*summaryState)
			if !ok {
				return state // not ours - same reference, zero work
			}
			s.MsgCount++
			s.LastRole = msg.Role
			s.LastText = msg.Content
			return s
		},
		View: func(state SessionProjectionState) any {
			s, _ := state.(*summaryState)
			return s
		},
	}
	if _, err := reg.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	reg.OnAppend("sess-g3", Message{
		ID:        1,
		SessionID: "sess-g3",
		Role:      "user",
		Content:   "hello",
		Timestamp: time.Now(),
		EntryType: "message",
		BranchID:  "main",
	})

	snap, err := reg.Snapshot("sess-g3", "session-summary")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	got, ok := snap.Values["session-summary"].(*summaryState)
	if !ok {
		t.Fatalf("Snapshot value type = %T, want *summaryState", snap.Values["session-summary"])
	}
	if got.MsgCount != 1 || got.LastRole != "user" || got.LastText != "hello" {
		t.Errorf("folded state = %+v, want MsgCount=1 LastRole=user LastText=hello", got)
	}
}

func TestProjection_ApplySameRefWhenUninterested(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cache, err := NewSessionProjectionCache(db)
	if err != nil {
		t.Fatalf("NewSessionProjectionCache: %v", err)
	}
	reg := NewSessionProjectionRegistry(cache)

	calls := 0
	var initState SessionProjectionState
	var returned SessionProjectionState
	def := ProjectionDefinition[string]{
		Key:          "session-summary",
		StateVersion: 1,
		Init: func(SessionVersionedMeta) SessionProjectionState {
			initState = &summaryState{}
			return initState
		},
		Apply: func(state SessionProjectionState, msg Message) SessionProjectionState {
			calls++
			returned = state
			return state // uninterested unit: same reference in, same out
		},
		View: func(state SessionProjectionState) any { return state },
	}
	if _, err := reg.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	reg.OnAppend("sess-sameref", Message{
		ID: 1, SessionID: "sess-sameref", Role: "user", Content: "x",
		Timestamp: time.Now(), EntryType: "message", BranchID: "main",
	})

	if calls != 1 {
		t.Fatalf("Apply calls = %d, want 1", calls)
	}
	// The contract: an uninterested unit's Apply receives and returns the
	// SAME reference Init produced (zero downstream work).
	if returned != initState {
		t.Error("Apply must pass through the same state reference Init produced")
	}
}

func TestProjection_StateVersionDiscardAtRead(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cache, err := NewSessionProjectionCache(db)
	if err != nil {
		t.Fatalf("NewSessionProjectionCache: %v", err)
	}

	// Persist a checkpoint row stamped under state version 1.
	rows := map[string]ProjectionCheckpointRow{
		"session-summary": {
			Key: "session-summary",
			Ver: 1,
			Seq: 5,
			Val: `{"msg_count":5,"last_role":"user","last_text":"old"}`,
		},
	}
	if err := cache.Checkpoint("sess-stale", rows); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	// Register the SAME key at state version 2: the v1 row must be discarded
	// at read (never migrated). A fresh Init state (zero fold) is the only
	// acceptable read result.
	reg := NewSessionProjectionRegistry(cache)
	def := ProjectionDefinition[string]{
		Key:          "session-summary",
		StateVersion: 2,
		Init: func(SessionVersionedMeta) SessionProjectionState {
			return &summaryState{}
		},
		Apply: func(state SessionProjectionState, msg Message) SessionProjectionState {
			return state
		},
		View: func(state SessionProjectionState) any { return state },
	}
	if _, err := reg.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	snap, err := reg.Snapshot("sess-stale", "session-summary")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	got, ok := snap.Values["session-summary"].(*summaryState)
	if !ok {
		t.Fatalf("Snapshot value type = %T, want *summaryState", snap.Values["session-summary"])
	}
	if got.MsgCount != 0 || got.LastText != "" {
		t.Errorf("stale-ver row was migrated instead of discarded: %+v, want zero state", got)
	}
}
