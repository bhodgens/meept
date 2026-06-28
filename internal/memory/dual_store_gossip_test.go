package memory

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// mockGossipPublisher satisfies GossipPublisher for testing.
type mockGossipPublisher struct {
	mu      sync.Mutex
	events  []publishedEvent
	eventCount int64 // atomic counter for non-locking reads
}

type publishedEvent struct {
	eventType models.ClusterEventType
	payload   any
}

func (m *mockGossipPublisher) PublishClusterEvent(eventType models.ClusterEventType, payload any) error {
	m.mu.Lock()
	m.events = append(m.events, publishedEvent{eventType, payload})
	m.mu.Unlock()
	atomic.AddInt64(&m.eventCount, 1)
	return nil
}

func (m *mockGossipPublisher) getLastEvent() (publishedEvent, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.events) == 0 {
		return publishedEvent{}, false
	}
	return m.events[len(m.events)-1], true
}

func TestDualStore_SetGossipPublisher(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "test-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	pub := &mockGossipPublisher{}
	ds.SetGossipPublisher(pub)
}

func TestDualStore_StoreMemory_PublishesGossip(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "test-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	pub := &mockGossipPublisher{}
	ds.SetGossipPublisher(pub)

	mem := &Memory{
		ID:        "mem-gossip-test",
		Type:      MemoryTypeEpisodic,
		Category:  "gossip_test",
		Content:   "test content for gossip publication",
		CreatedAt: time.Now().UTC(),
		AgentID:   "test-agent",
		SessionID: "test-session",
	}

	err = ds.StoreMemory(context.Background(), mem)
	if err != nil {
		t.Fatalf("StoreMemory: %v", err)
	}

	// Give the goroutine time to publish.
	time.Sleep(50 * time.Millisecond)

	lastEvent, ok := pub.getLastEvent()
	if !ok {
		t.Fatal("expected a published gossip event")
	}
	if lastEvent.eventType != models.EventTypeMemoryStored {
		t.Errorf("event type = %s, want %s", lastEvent.eventType, models.EventTypeMemoryStored)
	}

	payload, ok := lastEvent.payload.(models.MemoryStoredPayload)
	if !ok {
		t.Fatalf("payload type = %T, want models.MemoryStoredPayload", lastEvent.payload)
	}
	if payload.ID != "mem-gossip-test" {
		t.Errorf("payload.ID = %q, want %q", payload.ID, "mem-gossip-test")
	}
	if payload.Type != "episodic" {
		t.Errorf("payload.Type = %q, want %q", payload.Type, "episodic")
	}
}

func TestDualStore_StoreMemory_NoPublisher(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "test-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	// No publisher set — store should still work.
	mem := &Memory{
		ID:        "mem-no-pub",
		Type:      MemoryTypeTask,
		Category:  "category",
		Content:   "test without publisher",
		CreatedAt: time.Now().UTC(),
	}

	err = ds.StoreMemory(context.Background(), mem)
	if err != nil {
		t.Fatalf("StoreMemory with no publisher: %v", err)
	}
}
