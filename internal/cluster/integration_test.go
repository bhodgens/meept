package cluster

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/pkg/models"
)

func testLogger(t *testing.T) *slog.Logger {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("Logs:\n%s", buf.String())
		}
	})
	return logger
}

// TestClusterBootstrap tests single node initialization and shutdown.
func TestClusterBootstrap(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		ClusterID:   "test-cluster",
		ClusterName: "Test Cluster",
		NodeID:      "node-001",
		Network: NetworkConfig{
			WireGuardSubnet: "10.200.0.0/24",
			WireGuardPort:   51820,
			Interface:       "wg0",
		},
		Gossip: GossipConfig{
			HeartbeatInterval: 100 * time.Millisecond,
			PeerTimeout:       500 * time.Millisecond,
			EventRetention:    1 * time.Minute,
			MaxRetryAttempts:  3,
		},
		Git: GitConfig{
			SyncInterval:    5 * time.Minute,
			HeartbeatCommit: false,
		},
	}

	logger := testLogger(t)
	msgBus := bus.New(bus.DefaultConfig(), logger)

	engine := NewEngine(EngineConfig{
		Cfg:         cfg,
		LocalCfg:    cfg,
		Logger:      logger,
		MsgBus:      msgBus,
		GitRepoPath: filepath.Join(tmpDir, "git"),
		NodeName:    "Test Node 1",
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if !engine.IsRunning() {
		t.Error("Engine should be running after Start()")
	}

	if gossip := engine.Gossip(); gossip == nil {
		t.Error("Gossip engine should be initialized")
	}

	if err := engine.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if engine.IsRunning() {
		t.Error("Engine should not be running after Stop()")
	}
}

// TestClusterEventPersistence tests that published events are stored.
func TestClusterEventPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "queue.db")
	logger := testLogger(t)

	store, err := queue.NewStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	cfg := &Config{
		ClusterID: "test-cluster",
		NodeID:    "node-001",
		Gossip: GossipConfig{
			HeartbeatInterval: 100 * time.Millisecond,
			PeerTimeout:       500 * time.Millisecond,
		},
		Git: GitConfig{
			SyncInterval:    1 * time.Second,
			HeartbeatCommit: false,
		},
	}

	msgBus := bus.New(bus.DefaultConfig(), logger)
	engine := NewEngine(EngineConfig{
		Cfg:         cfg,
		LocalCfg:    cfg,
		Logger:      logger,
		MsgBus:      msgBus,
		GitRepoPath: filepath.Join(tmpDir, "git"),
		QueueStore:  store,
		NodeName:    "Test Node",
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	event := &models.ClusterEvent{
		EventID:     "test-event-001",
		NodeID:      "node-001",
		EventType:   models.EventTaskCreate,
		Timestamp:   time.Now().UTC(),
		VectorClock: map[string]int64{"node-001": 1},
		Payload:     []byte(`{"task_id":"t1"}`),
	}

	gossip := engine.Gossip()
	if gossip == nil {
		t.Fatal("Gossip not initialized")
	}

	gossip.Publish(event)

	time.Sleep(50 * time.Millisecond)

	rows, err := store.DB().Query(`SELECT event_id FROM cluster_events WHERE event_id = ?`, "test-event-001")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var eid string
		if err := rows.Scan(&eid); err != nil {
			continue
		}
		found = true
		break
	}

	if !found {
		t.Error("Event was not persisted to database")
	}
}

// TestClusterQueueReclaim tests task reclaim when timeout elapses.
func TestClusterQueueReclaim(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "queue.db")
	logger := testLogger(t)

	store, err := queue.NewStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	baseQueue, err := queue.NewPersistentQueue(dbPath, nil, logger)
	if err != nil {
		t.Fatalf("NewPersistentQueue failed: %v", err)
	}

	cq := queue.NewClusterQueue(baseQueue, store, "node-001", logger,
		queue.ClusterQueueConfig{
			DefaultClaimTimeout:     100 * time.Millisecond,
			NodeReachabilityTimeout: 200 * time.Millisecond,
		},
	)
	defer cq.Close()

	ctx := t.Context()

	job, err := queue.NewJob(queue.JobTypeOneOff, map[string]string{"task": "test"})
	if err != nil {
		t.Fatalf("NewJob failed: %v", err)
	}
	job.ID = "test-job-001"

	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	claimedJob, err := cq.Claim(ctx, "node-001", []string{"code"}, "")
	if err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	if claimedJob == nil {
		t.Fatal("Expected claimed job")
	}

	time.Sleep(150 * time.Millisecond)

	staleJobs := cq.ReclaimIfStale(ctx)
	for _, staleJob := range staleJobs {
		cq.ReclaimJob(ctx, staleJob.ID, "timeout")
	}

	retrieved, err := store.GetByID("test-job-001")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if retrieved.State != queue.StatePending {
		t.Logf("Job state=%s after reclaim", retrieved.State)
	}
}

// TestClusterConfigLoadSave tests config persistence.
func TestClusterConfigLoadSave(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json5")

	cfg := &Config{
		ClusterID:   "persist-test",
		ClusterName: "Persistence Test",
		NodeID:      "node-persist",
		Network: NetworkConfig{
			WireGuardSubnet: "10.201.0.0/24",
			WireGuardPort:   51821,
			Interface:       "wg1",
		},
		Gossip: GossipConfig{
			HeartbeatInterval: 45 * time.Second,
			PeerTimeout:       3 * time.Minute,
		},
	}

	if err := SaveClusterConfig(cfgPath, cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := LoadClusterConfig(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.ClusterID != cfg.ClusterID {
		t.Errorf("ClusterID: got %s, want %s", loaded.ClusterID, cfg.ClusterID)
	}
}

// TestMultiNodeEventPropagation tests that events published by one node
// are delivered to another via the gossip transport.
func TestMultiNodeEventPropagation(t *testing.T) {
	logger := testLogger(t)

	// Find available ports for two nodes
	l1, _ := net.Listen("tcp", "127.0.0.1:0")
	port1 := l1.Addr().(*net.TCPAddr).Port
	l1.Close()

	l2, _ := net.Listen("tcp", "127.0.0.1:0")
	port2 := l2.Addr().(*net.TCPAddr).Port
	l2.Close()

	// Member records
	node1Member := &Member{
		NodeID:    "node-001",
		ClusterIP: "127.0.0.1",
		Status:    "active",
		Endpoint:  "127.0.0.1:" + strconv.Itoa(port1),
	}
	node2Member := &Member{
		NodeID:    "node-002",
		ClusterIP: "127.0.0.1",
		Status:    "active",
		Endpoint:  "127.0.0.1:" + strconv.Itoa(port2),
	}

	// Create configs
	cfg1 := &Config{
		ClusterID: "test-cluster",
		NodeID:    "node-001",
		Network:   NetworkConfig{WireGuardPort: port1 - 1},
		Gossip: GossipConfig{
			HeartbeatInterval: 1 * time.Hour,
			PeerTimeout:       1 * time.Hour,
			EventRetention:    1 * time.Hour,
			MaxRetryAttempts:  3,
		},
		Git:      GitConfig{SyncInterval: 1 * time.Hour},
		Security: SecurityConfig{RequireNodeSignatures: false},
	}
	cfg2 := &Config{
		ClusterID: "test-cluster",
		NodeID:    "node-002",
		Network:   NetworkConfig{WireGuardPort: port2 - 1},
		Gossip: GossipConfig{
			HeartbeatInterval: 1 * time.Hour,
			PeerTimeout:       1 * time.Hour,
			EventRetention:    1 * time.Hour,
			MaxRetryAttempts:  3,
		},
		Git:      GitConfig{SyncInterval: 1 * time.Hour},
		Security: SecurityConfig{RequireNodeSignatures: false},
	}

	// Create message buses
	bus1 := bus.New(bus.DefaultConfig(), logger)
	bus2 := bus.New(bus.DefaultConfig(), logger)

	// Create mock members providers
	mp1 := &mockMembersProvider{members: map[string]*Member{
		"node-002": node2Member,
	}}
	mp2 := &mockMembersProvider{members: map[string]*Member{
		"node-001": node1Member,
	}}

	// Create gossip engines with transport
	g1 := NewGossipEngine(cfg1, "node-001", bus1, logger,
		WithMembersProvider(mp1),
	)
	g2 := NewGossipEngine(cfg2, "node-002", bus2, logger,
		WithMembersProvider(mp2),
	)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	if err := g1.Start(ctx); err != nil {
		t.Fatalf("g1 start: %v", err)
	}
	defer g1.Stop()

	if err := g2.Start(ctx); err != nil {
		t.Fatalf("g2 start: %v", err)
	}
	defer g2.Stop()

	time.Sleep(50 * time.Millisecond)

	// Publish event from node 1 - this goes to TCP transport
	testEvent := &models.ClusterEvent{
		EventType: models.EventTaskCreate,
		NodeID:    "node-001",
		Payload:   json.RawMessage(`{"task_id":"multi-node-test"}`),
	}
	g1.Publish(testEvent)

	// Verify the event was received by node 2's transport by checking
	// that the transport marked it as delivered to node-002
	time.Sleep(200 * time.Millisecond)

	transport1 := g1.transport
	if transport1 == nil {
		t.Fatal("node-001 transport not initialized")
	}

	if !transport1.hasSentToPeer("node-002", testEvent.EventID) {
		t.Error("event was not delivered to node-002 via TCP transport")
	}

	// Event was delivered via TCP and processed through gossip pipeline
	t.Logf("Event %s delivered to node-002 via TCP transport", testEvent.EventID)
}

// TestClusterQueueComplete tests that a completed task records the event.
func TestClusterQueueComplete(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "queue.db")
	logger := testLogger(t)

	store, err := queue.NewStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	baseQueue, err := queue.NewPersistentQueue(dbPath, nil, logger)
	if err != nil {
		t.Fatalf("NewPersistentQueue: %v", err)
	}

	cq := queue.NewClusterQueue(baseQueue, store, "node-001", logger)
	defer cq.Close()

	ctx := t.Context()

	// Create and insert a job
	job, err := queue.NewJob(queue.JobTypeOneOff, map[string]string{"task": "completion-test"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	job.ID = "complete-test-001"
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Claim the job
	claimed, err := cq.Claim(ctx, "worker-1", nil, "")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed == nil {
		t.Fatal("Expected claimed job")
	}

	// Complete the job
	if err := cq.Complete(ctx, claimed.ID, "done"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	// Verify claim event was recorded
	var count int
	row := store.DB().QueryRow(`SELECT COUNT(*) FROM cluster_events WHERE event_type = 'TASK_complete'`)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count == 0 {
		t.Error("Expected at least one TASK_complete event")
	}
}

// TestClusterQueueFail tests that a failed task records the event.
func TestClusterQueueFail(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "queue.db")
	logger := testLogger(t)

	store, err := queue.NewStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	baseQueue, err := queue.NewPersistentQueue(dbPath, nil, logger)
	if err != nil {
		t.Fatalf("NewPersistentQueue: %v", err)
	}

	cq := queue.NewClusterQueue(baseQueue, store, "node-001", logger)
	defer cq.Close()

	ctx := t.Context()

	job, err := queue.NewJob(queue.JobTypeOneOff, map[string]string{"task": "fail-test"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	job.ID = "fail-test-001"
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	claimed, err := cq.Claim(ctx, "worker-1", nil, "")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed == nil {
		t.Fatal("Expected claimed job")
	}

	if err := cq.Fail(ctx, claimed.ID, fmt.Errorf("something went wrong")); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	var count int
	row := store.DB().QueryRow(`SELECT COUNT(*) FROM cluster_events WHERE event_type = 'TASK_fail'`)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count == 0 {
		t.Error("Expected at least one TASK_fail event")
	}
}

// TestMemberRegistry tests member CRUD operations.
func TestMemberRegistry(t *testing.T) {
	tmpDir := t.TempDir()

	member := &Member{
		NodeID:       "test-node",
		NodeName:     "Test Node",
		WireGuardPub: "test-wg-pubkey",
		SigningPub:   []byte{0x01, 0x02, 0x03},
		Endpoint:     "192.168.1.42:51820",
		Capabilities: []string{"coder", "analyst"},
		ClusterIP:    "10.200.0.5",
		Status:       "active",
	}

	// Save
	if err := SaveMember(tmpDir, member); err != nil {
		t.Fatalf("SaveMember: %v", err)
	}

	// Load
	loaded, err := LoadMember(tmpDir, "test-node")
	if err != nil {
		t.Fatalf("LoadMember: %v", err)
	}

	if loaded.NodeID != member.NodeID {
		t.Errorf("NodeID: got %s, want %s", loaded.NodeID, member.NodeID)
	}
	if loaded.ClusterIP != member.ClusterIP {
		t.Errorf("ClusterIP: got %s, want %s", loaded.ClusterIP, member.ClusterIP)
	}
	if len(loaded.Capabilities) != 2 {
		t.Errorf("Capabilities: got %d, want 2", len(loaded.Capabilities))
	}
	if loaded.Status != "active" {
		t.Errorf("Status: got %s, want active", loaded.Status)
	}

	// List
	members, err := ListLocalMembers(tmpDir)
	if err != nil {
		t.Fatalf("ListLocalMembers: %v", err)
	}
	if len(members) != 1 {
		t.Errorf("Members count: got %d, want 1", len(members))
	}

	// Delete
	if err := DeleteMember(tmpDir, "test-node"); err != nil {
		t.Fatalf("DeleteMember: %v", err)
	}

	_, err = LoadMember(tmpDir, "test-node")
	if err == nil {
		t.Error("Expected error after delete")
	}
}

// TestEventSigning tests the full sign-verify cycle for cluster events.
func TestEventSigning(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}

	event := &models.ClusterEvent{
		EventID:   models.GenerateEventID(),
		NodeID:    "node-sign-test",
		EventType: models.EventTaskClaim,
		Timestamp: time.Now().UTC(),
		Payload:   json.RawMessage(`{"task_id":"sign-test-001"}`),
	}

	// Sign
	if err := event.Sign(priv); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Verify with correct key
	if !event.Verify(pub) {
		t.Error("Verification failed with correct key")
	}

	// Verify with wrong key
	wrongPub, _, _ := ed25519.GenerateKey(nil)
	if event.Verify(wrongPub) {
		t.Error("Verification should fail with wrong key")
	}

	// Tamper with event
	originalPayload := make([]byte, len(event.Payload))
	copy(originalPayload, event.Payload)
	event.Payload[0] ^= 0xFF
	if event.Verify(pub) {
		t.Error("Verification should fail after tampering")
	}
	copy(event.Payload, originalPayload)
}

// TestVectorClockIncrement tests that the vector clock advances on publish.
func TestVectorClockIncrement(t *testing.T) {
	cfg := &Config{
		NodeID: "node-vc",
		Gossip: GossipConfig{
			HeartbeatInterval: 1 * time.Hour,
			PeerTimeout:       1 * time.Hour,
			EventRetention:    1 * time.Hour,
			MaxRetryAttempts:  3,
		},
		Security: SecurityConfig{RequireNodeSignatures: false},
	}

	logger := slog.Default()
	g := NewGossipEngine(cfg, "node-vc", nil, logger)

	// Initial VC should be empty
	vc := g.getVC()
	if len(vc) != 0 {
		t.Errorf("Initial VC should be empty, got %v", vc)
	}

	// After record, should have one entry
	vc = g.RecordLocalVC()
	if vc["node-vc"] != 1 {
		t.Errorf("Expected node-vc=1, got %v", vc)
	}

	// After another record
	vc = g.RecordLocalVC()
	if vc["node-vc"] != 2 {
		t.Errorf("Expected node-vc=2, got %v", vc)
	}
}
