package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// TestDualStore_LocalWriteGoesToGossipDB verifies that a local StoreMemory
// call for a memory carrying source_node metadata from a peer node lands in
// the gossip DB, not the local DB. This exercises the routing logic that
// prevents remote-sourced memories from being persisted as "own" data.
func TestDualStore_LocalWriteGoesToGossipDB(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	store, err := memory.NewDualStore(tmp, "node-local", newTestLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer store.Close()

	peerNode := "node-peer"
	memID := id.Generate("mem-")
	mem := &memory.Memory{
		ID:        memID,
		Type:      memory.MemoryTypeEpisodic,
		Category:  "remote",
		Content:   "from peer",
		CreatedAt: time.Now().UTC(),
		Metadata:  map[string]any{"source_node": peerNode},
	}

	ctx := context.Background()
	// StoreMemory with source_node metadata pointing to a peer should route
	// to the gossip DB (treated as remote-origin via metadata inspection).
	if err := store.StoreMemory(ctx, mem); err != nil {
		t.Fatalf("StoreMemory: %v", err)
	}

	// The memory should be in gossip DB.
	var gossipCount int
	if err := store.GossipDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memories WHERE id = ?`, memID,
	).Scan(&gossipCount); err != nil {
		t.Fatalf("query gossip memories: %v", err)
	}
	if gossipCount != 1 {
		t.Errorf("gossip DB count = %d, want 1", gossipCount)
	}

	// The memory should NOT be in the local DB.
	var localCount int
	if err := store.LocalDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memories WHERE id = ?`, memID,
	).Scan(&localCount); err != nil {
		t.Fatalf("query local memories: %v", err)
	}
	if localCount != 0 {
		t.Errorf("local DB count = %d, want 0 (remote-origin memory must not be in local DB)", localCount)
	}
}

// TestDualStore_RemoteWriteStaysInGossipDB verifies that StoreRemoteMemory
// only writes to the gossip DB and never touches the local DB.
func TestDualStore_RemoteWriteStaysInGossipDB(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	store, err := memory.NewDualStore(tmp, "node-local", newTestLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer store.Close()

	memID := id.Generate("mem-")
	mem := &memory.Memory{
		ID:        memID,
		Type:      memory.MemoryTypeTask,
		Category:  "peer-data",
		Content:   "remote content",
		CreatedAt: time.Now().UTC(),
	}
	ctx := context.Background()
	if err := store.StoreRemoteMemory(ctx, mem, "node-peer"); err != nil {
		t.Fatalf("StoreRemoteMemory: %v", err)
	}

	var gossipCount int
	if err := store.GossipDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memories WHERE id = ?`, memID,
	).Scan(&gossipCount); err != nil {
		t.Fatalf("query gossip: %v", err)
	}
	if gossipCount != 1 {
		t.Errorf("gossip DB count = %d, want 1", gossipCount)
	}

	var localCount int
	if err := store.LocalDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memories WHERE id = ?`, memID,
	).Scan(&localCount); err != nil {
		t.Fatalf("query local: %v", err)
	}
	if localCount != 0 {
		t.Errorf("local DB count = %d, want 0", localCount)
	}
}

// TestDualStore_MergedReadLocalWins inserts the same memory ID into both DBs
// and verifies that GetMemories returns the local version (dedup, local wins).
func TestDualStore_MergedReadLocalWins(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	store, err := memory.NewDualStore(tmp, "node-local", newTestLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer store.Close()

	memID := id.Generate("dup-")

	// Insert into local DB directly with content "LOCAL".
	localMem := &memory.Memory{
		ID:        memID,
		Type:      memory.MemoryTypeEpisodic,
		Category:  "shared",
		Content:   "LOCAL",
		CreatedAt: time.Now().UTC(),
	}
	ctx := context.Background()
	if err := store.StoreMemory(ctx, localMem); err != nil {
		t.Fatalf("StoreMemory local: %v", err)
	}

	// Insert into gossip DB directly with content "GOSSIP".
	gossipMem := &memory.Memory{
		ID:        memID,
		Type:      memory.MemoryTypeEpisodic,
		Category:  "shared",
		Content:   "GOSSIP",
		CreatedAt: time.Now().UTC(),
	}
	if err := store.StoreRemoteMemory(ctx, gossipMem, "node-peer"); err != nil {
		t.Fatalf("StoreRemoteMemory: %v", err)
	}

	results, err := store.GetMemories(ctx, &memory.MemoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	// Find the entry for memID.
	var found *memory.MemoryResult
	for i := range results {
		if results[i].Memory.ID == memID {
			found = &results[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("memory %s not in merged results (count=%d)", memID, len(results))
	}
	if found.Memory.Content != "LOCAL" {
		t.Errorf("merged content = %q, want %q (local should win)", found.Memory.Content, "LOCAL")
	}
}

// countingPublisher is a GossipPublisher that counts PublishClusterEvent calls.
type countingPublisher struct {
	count    int32
	lastType atomic.Value // ClusterEventType
}

func (c *countingPublisher) PublishClusterEvent(eventType models.ClusterEventType, payload any) error {
	atomic.AddInt32(&c.count, 1)
	c.lastType.Store(eventType)

	// Validate that payload is JSON-marshalable (mirrors real publisher contract).
	if _, err := json.Marshal(payload); err != nil {
		return err
	}
	return nil
}

// TestDualStore_SessionStorePublishesTurn verifies that the PublishTurn
// adapter (used by the session store) writes to local.db AND publishes a
// SESSION_TURN gossip event when a publisher is wired.
func TestDualStore_SessionStorePublishesTurn(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	store, err := memory.NewDualStore(tmp, "node-local", newTestLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer store.Close()

	pub := &countingPublisher{}
	store.SetGossipPublisher(pub)

	turnID := id.Generate("turn-")
	sessionID := id.Generate("sess-")
	ts := time.Now().UTC()

	if err := store.PublishTurn(sessionID, turnID, "user", "hello", ts); err != nil {
		t.Fatalf("PublishTurn: %v", err)
	}

	// Wait briefly for the non-blocking goroutine in publishTurnGossip.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&pub.count) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := atomic.LoadInt32(&pub.count); got != 1 {
		t.Errorf("publish count = %d, want 1", got)
	}

	lastType, _ := pub.lastType.Load().(models.ClusterEventType)
	if lastType != models.EventTypeSessionTurn {
		t.Errorf("last event type = %q, want %q", lastType, models.EventTypeSessionTurn)
	}

	// Verify the turn landed in local.db.
	ctx := context.Background()
	var localTurnCount int
	if err := store.LocalDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM turns WHERE turn_id = ?`, turnID,
	).Scan(&localTurnCount); err != nil {
		t.Fatalf("query local turns: %v", err)
	}
	if localTurnCount != 1 {
		t.Errorf("local turns count = %d, want 1", localTurnCount)
	}
}

// TestDualStore_GetSessionMergesBoth inserts sessions into both DBs and
// verifies GetSessions returns both, deduplicated local-wins for a shared ID.
func TestDualStore_GetSessionMergesBoth(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	store, err := memory.NewDualStore(tmp, "node-local", newTestLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Local-only session.
	localSessID := id.Generate("local-sess-")
	localSess := &memory.Session{
		ID:           localSessID,
		Name:         "local",
		ConversationID: id.Generate("conv-"),
		CreatedAt:    time.Now().UTC(),
		LastActivity: time.Now().UTC(),
	}
	if err := store.StoreSession(ctx, localSess); err != nil {
		t.Fatalf("StoreSession local: %v", err)
	}

	// Remote session via StoreRemoteSession.
	remoteSessID := id.Generate("remote-sess-")
	remoteSess := &memory.Session{
		ID:           remoteSessID,
		Name:         "remote",
		ConversationID: id.Generate("conv-"),
		CreatedAt:    time.Now().UTC(),
		LastActivity: time.Now().UTC(),
	}
	if err := store.StoreRemoteSession(ctx, remoteSess, "node-peer"); err != nil {
		t.Fatalf("StoreRemoteSession: %v", err)
	}

	// Shared ID: insert into local first, then gossip. Local should win.
	sharedID := id.Generate("shared-sess-")
	sharedLocal := &memory.Session{
		ID:           sharedID,
		Name:         "local-version",
		ConversationID: id.Generate("conv-"),
		CreatedAt:    time.Now().UTC(),
		LastActivity: time.Now().UTC(),
	}
	if err := store.StoreSession(ctx, sharedLocal); err != nil {
		t.Fatalf("StoreSession shared local: %v", err)
	}
	sharedRemote := &memory.Session{
		ID:           sharedID,
		Name:         "remote-version",
		ConversationID: id.Generate("conv-"),
		CreatedAt:    time.Now().UTC(),
		LastActivity: time.Now().UTC(),
	}
	if err := store.StoreRemoteSession(ctx, sharedRemote, "node-peer"); err != nil {
		t.Fatalf("StoreRemoteSession shared: %v", err)
	}

	sessions, err := store.GetSessions(ctx)
	if err != nil {
		t.Fatalf("GetSessions: %v", err)
	}

	// Expect 3 unique sessions (local-only, remote-only, shared).
	if len(sessions) != 3 {
		t.Fatalf("session count = %d, want 3", len(sessions))
	}

	byID := make(map[string]*memory.Session, len(sessions))
	for i := range sessions {
		byID[sessions[i].ID] = sessions[i]
	}

	if _, ok := byID[localSessID]; !ok {
		t.Errorf("local session %s missing from merged result", localSessID)
	}
	if _, ok := byID[remoteSessID]; !ok {
		t.Errorf("remote session %s missing from merged result", remoteSessID)
	}
	shared, ok := byID[sharedID]
	if !ok {
		t.Fatalf("shared session %s missing from merged result", sharedID)
	}
	// Local wins → the Name should reflect the local version.
	if shared.Name != "local-version" {
		t.Errorf("shared session Name = %q, want %q (local should win)", shared.Name, "local-version")
	}
}

// echoBlockingPublisher is a GossipPublisher that blocks publish calls so the
// test can detect whether StoreRemoteTurn inadvertently re-publishes. If it
// did, an infinite loop would form (StoreRemoteTurn → publish → handler →
// StoreRemoteTurn). We don't test for the loop directly; we test that
// StoreRemoteTurn does NOT invoke the publisher at all.
type noopPublisher struct{}

func (noopPublisher) PublishClusterEvent(models.ClusterEventType, any) error { return nil }

// TestDualStore_StoreRemoteTurn_NoEcho verifies that calling StoreRemoteTurn
// (the gossip-handler code path for peer-originated turns) does NOT trigger
// another gossip publish. This is the echo-guard that prevents infinite loops
// in the gossip propagation path.
func TestDualStore_StoreRemoteTurn_NoEcho(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	store, err := memory.NewDualStore(tmp, "node-local", newTestLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer store.Close()

	pub := &countingPublisher{}
	store.SetGossipPublisher(pub)

	turnID := id.Generate("turn-")
	turn := &memory.Turn{
		TurnID:    turnID,
		SessionID: id.Generate("sess-"),
		Role:      "user",
		Content:   "remote turn",
		Timestamp: time.Now().UTC(),
	}
	ctx := context.Background()
	if err := store.StoreRemoteTurn(ctx, turn, "node-peer"); err != nil {
		t.Fatalf("StoreRemoteTurn: %v", err)
	}

	// StoreRemoteTurn should NOT invoke the publisher (no echo).
	// Give a brief grace period in case of goroutine scheduling.
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&pub.count); got != 0 {
		t.Errorf("publish count = %d, want 0 (StoreRemoteTurn must not echo-publish)", got)
	}

	// Verify the turn landed in gossip DB.
	var gossipTurnCount int
	if err := store.GossipDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM turns WHERE turn_id = ?`, turnID,
	).Scan(&gossipTurnCount); err != nil {
		t.Fatalf("query gossip turns: %v", err)
	}
	if gossipTurnCount != 1 {
		t.Errorf("gossip turns count = %d, want 1", gossipTurnCount)
	}
}

// Compile-time: ensure the test's noop publisher satisfies the interface.
var _ memory.GossipPublisher = noopPublisher{}

// guard against unused import warnings if the test file is trimmed.
var _ = io.Discard
var _ = slog.Default
var _ = filepath.Join
