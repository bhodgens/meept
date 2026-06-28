package memory

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// TestManager_Store_PublishesGossip_WhenDualStoreSet verifies that storing a
// memory through Manager.Store mirrors the write to the DualStore, which in
// turn publishes a MEMORY_STORED gossip event when a publisher is configured.
// This satisfies T3.6's literal requirement: Manager writes route through
// DualStore when present.
func TestManager_Store_PublishesGossip_WhenDualStoreSet(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	// Create and wire a DualStore in a separate subdir of the test temp dir.
	dsDir := t.TempDir()
	ds, err := NewDualStore(dsDir, "test-local-node", testLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	pub := &mockGossipPublisher{}
	ds.SetGossipPublisher(pub)
	mgr.SetDualStore(ds)

	ctx := context.Background()
	mem := Memory{
		Content:  "gossip mirror test content",
		Type:     MemoryTypeEpisodic,
		Category: "conversation",
	}
	id, err := mgr.Store(ctx, mem)
	if err != nil {
		t.Fatalf("Manager.Store: %v", err)
	}
	if id == "" {
		t.Fatal("Manager.Store returned empty ID")
	}

	// publishMemoryGossip runs in a goroutine — give it a moment.
	// The publish happens inside DualStore.publishMemoryGossip via `go func()`.
	waitFor(t, func() bool {
		return atomic.LoadInt64(&pub.eventCount) > 0
	}, "expected at least one gossip event after Manager.Store")

	last, ok := pub.getLastEvent()
	if !ok {
		t.Fatal("expected a published gossip event")
	}
	if last.eventType != models.EventTypeMemoryStored {
		t.Errorf("event type = %s, want %s", last.eventType, models.EventTypeMemoryStored)
	}
	payload, ok := last.payload.(models.MemoryStoredPayload)
	if !ok {
		t.Fatalf("payload type = %T, want models.MemoryStoredPayload", last.payload)
	}
	if payload.ID != id {
		t.Errorf("payload.ID = %q, want %q", payload.ID, id)
	}
	if payload.Content != mem.Content {
		t.Errorf("payload.Content = %q, want %q", payload.Content, mem.Content)
	}

	// The DualStore local.db should also have the mirrored row.
	local, _, err := ds.GetMemoryCountByOwner(ctx)
	if err != nil {
		t.Fatalf("GetMemoryCountByOwner: %v", err)
	}
	if local != 1 {
		t.Errorf("DualStore local count = %d, want 1 (mirrored row)", local)
	}
}

// TestManager_GetMemories_MergesDualStoreResults verifies that GetRecent
// includes remote-only memories from the DualStore's gossip.db in addition
// to local episodic/task rows. Local entries win on duplicate IDs.
func TestManager_GetMemories_MergesDualStoreResults(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	ctx := context.Background()

	// Store one memory locally (no DualStore yet).
	localMem := Memory{
		Content:  "local-only memory",
		Type:     MemoryTypeEpisodic,
		Category: "conversation",
	}
	localID, err := mgr.Store(ctx, localMem)
	if err != nil {
		t.Fatalf("Store local: %v", err)
	}

	// Now wire a DualStore with a remote-only memory in gossip.db.
	dsDir := t.TempDir()
	ds, err := NewDualStore(dsDir, "test-local-node", testLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	remoteMem := &Memory{
		ID:        id.Generate("mem-remote-"),
		Type:      MemoryTypeEpisodic,
		Category:  "conversation",
		Content:   "remote gossip-only memory",
		CreatedAt: time.Now().UTC(),
		AgentID:   "peer-agent",
	}
	if err := ds.StoreRemoteMemory(ctx, remoteMem, "peer-node-1"); err != nil {
		t.Fatalf("StoreRemoteMemory: %v", err)
	}
	mgr.SetDualStore(ds)

	// GetRecent should return both the local and remote memories.
	results, err := mgr.GetRecent(ctx, 50)
	if err != nil {
		t.Fatalf("GetRecent: %v", err)
	}

	var sawLocal, sawRemote bool
	for _, r := range results {
		if r.Memory.ID == localID {
			sawLocal = true
		}
		if r.Memory.ID == remoteMem.ID {
			sawRemote = true
		}
	}
	if !sawLocal {
		t.Error("expected local memory in GetRecent results, not found")
	}
	if !sawRemote {
		t.Error("expected remote (gossip) memory in GetRecent results, not found")
	}
}

// TestManager_Store_NoDualStore_Noop verifies that with no DualStore wired,
// Manager.Store behaves normally (no panic, no gossip publish attempt).
func TestManager_Store_NoDualStore_Noop(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	// No SetDualStore call — dualStore is nil.
	ctx := context.Background()
	mem := Memory{
		Content:  "no dualstore test",
		Type:     MemoryTypeEpisodic,
		Category: "conversation",
	}
	id, err := mgr.Store(ctx, mem)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	// Sanity: the row is in the local episodic backend.
	results, err := mgr.GetRecent(ctx, 10)
	if err != nil {
		t.Fatalf("GetRecent: %v", err)
	}
	var found bool
	for _, r := range results {
		if r.Memory.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Error("stored memory not found in GetRecent results")
	}
}

// TestManager_GetMemories_NoDualStore_Noop verifies GetRecent works
// normally without a DualStore (no panic).
func TestManager_GetMemories_NoDualStore_Noop(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	ctx := context.Background()
	if _, err := mgr.Store(ctx, Memory{
		Content:  "plain memory",
		Type:     MemoryTypeTask,
		Category: DomainGeneral,
	}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	results, err := mgr.GetRecent(ctx, 10)
	if err != nil {
		t.Fatalf("GetRecent: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result from GetRecent without DualStore")
	}
}

// TestManager_Store_DualStoreSetButMemoryIsRemote_NoEcho verifies that when
// a memory's metadata carries a foreign "source_node", the Manager does NOT
// mirror it to the DualStore. This prevents gossip echo loops where a memory
// ingested from a peer would be re-published back to the cluster.
func TestManager_Store_DualStoreSetButMemoryIsRemote_NoEcho(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	dsDir := t.TempDir()
	ds, err := NewDualStore(dsDir, "test-local-node", testLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	pub := &mockGossipPublisher{}
	ds.SetGossipPublisher(pub)
	mgr.SetDualStore(ds)

	ctx := context.Background()
	// Simulate a memory that originated from a peer node. The Manager's
	// storeViaSQLite path still writes it to the local episodic_memories.db
	// (so the agent has a unified view), but the DualStore mirror is skipped.
	mem := Memory{
		Content:  "remote-origin memory echoed through agent loop",
		Type:     MemoryTypeEpisodic,
		Category: "conversation",
		Metadata: map[string]any{
			"source_node": "peer-node-42",
		},
	}
	storedID, err := mgr.Store(ctx, mem)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if storedID == "" {
		t.Fatal("expected non-empty ID")
	}

	// Allow any pending goroutines to flush, then assert no gossip events.
	time.Sleep(100 * time.Millisecond)
	if count := atomic.LoadInt64(&pub.eventCount); count != 0 {
		t.Errorf("expected 0 gossip events for remote-origin memory, got %d", count)
	}

	// Sanity: the DualStore local.db should NOT contain this row (mirror skipped).
	local, _, err := ds.GetMemoryCountByOwner(ctx)
	if err != nil {
		t.Fatalf("GetMemoryCountByOwner: %v", err)
	}
	if local != 0 {
		t.Errorf("DualStore local count = %d, want 0 (no mirror for remote-origin)", local)
	}
}

// waitFor polls cond up to ~2s, failing the test with msg if it never holds.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}
