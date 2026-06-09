# Distributed Meept Cluster Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a decentralized cluster architecture allowing multiple `meept-daemon` instances to form a peer-to-peer mesh network, share a distributed task queue, and coordinate work without a central management server.

**Architecture:** Git-based membership registry + WireGuard mesh network + gossip protocol for event replication + SQLite-backed replicated queue. Each node operates independently and syncs state when connected.

**Tech Stack:** Go 1.24+, WireGuard (wgctrl), ed25519 signatures, SQLite, git for coordination, `wg` CLI tool for interface management.

---

## File Structure

### New Files to Create

| File | Responsibility |
|------|----------------|
| `internal/cluster/gossip.go` | Gossip engine: event publish/subscribe, peer management, flood protocol |
| `internal/cluster/git_sync.go` | Git synchronization: pull remote, push heartbeats, parse node registry |
| `internal/cluster/wireguard_sync.go` | WireGuard config generation and application via `wg` binary |
| `internal/cluster/cluster.go` | Cluster types: ClusterEvent, Member, ClusterConfig |
| `internal/queue/cluster_queue.go` | Cluster-aware queue: task claim/timeout/reclaim logic |
| `internal/rpc/cluster_handler.go` | CLI RPC handlers for cluster commands |
| `cmd/meept/cluster_cmd.go` | CLI command definitions (init, join, start, status, leave) |
| `pkg/models/cluster.go` | Cluster event types and payloads |
| `~/.meept/cluster/config.json5` | Local node configuration (created by CLI) |
| `docs/configuration/cluster.md` | User guide for cluster setup |

### Modified Files

| File | Changes |
|------|---------|
| `internal/daemon/daemon.go` | Wire up cluster components on startup |
| `internal/daemon/components.go` | Add ClusterEngine, ClusterQueue dependencies |
| `internal/queue/queue.go` | Add cluster fields to job schema |
| `internal/agent/loop.go` | Add reachability check before claiming remote tasks |
| `internal/config/schema.go` | Add cluster config section |
| `cmd/meept/main.go` | Register cluster subcommand |

---

## Phase 1: Foundation (Core Types and Schema)

### Task 1: Cluster Event Types and Models

**Files:**
- Create: `pkg/models/cluster.go`
- Test: `pkg/models/cluster_test.go`

- [x] **Step 1: Write cluster event type tests**

```go
// pkg/models/cluster_test.go
package models

import (
    "testing"
    "time"
    "golang.org/x/crypto/ed25519"
)

func TestClusterEvent_SignAndVerify(t *testing.T) {
    pubKey, privKey, err := ed25519.GenerateKey(nil)
    if err != nil {
        t.Fatalf("key gen failed: %v", err)
    }

    event := &ClusterEvent{
        EventID:   "test-event-001",
        NodeID:    "node-01",
        EventType: EventTaskCreate,
        Timestamp: time.Now(),
        Payload:   []byte(`{"task_id":"t1","agent_id":"coder"}`),
    }

    err = event.Sign(privKey)
    if err != nil {
        t.Fatalf("sign failed: %v", err)
    }

    if !event.Verify(pubKey) {
        t.Error("verification failed for valid signature")
    }

    // Tamper with payload
    event.Payload[0] ^= 0xFF
    if event.Verify(pubKey) {
        t.Error("verification passed for tampered event")
    }
}

func TestClusterEvent_MarshalUnmarshal(t *testing.T) {
    event := &ClusterEvent{
        EventID:     "test-001",
        NodeID:      "node-test",
        EventType:   EventTaskClaim,
        Timestamp:   time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC),
        VectorClock: map[string]int64{"node-test": 1},
        Payload:     []byte(`{"task_id":"t1"}`),
    }

    data, err := event.MarshalJSON()
    if err != nil {
        t.Fatalf("marshal failed: %v", err)
    }

    var decoded ClusterEvent
    err = decoded.UnmarshalJSON(data)
    if err != nil {
        t.Fatalf("unmarshal failed: %v", err)
    }

    if decoded.EventID != event.EventID {
        t.Errorf("EventID mismatch: got %s, want %s", decoded.EventID, event.EventID)
    }
}
```

- [x] **Step 2: Run test to verify it fails**

```bash
cd /Users/caimlas/git/meept
go test ./pkg/models/cluster_test.go -v
```
Expected: FAIL with "undefined: ClusterEvent"

- [x] **Step 3: Implement cluster event types**

```go
// pkg/models/cluster.go
package models

import (
    "crypto/ed25519"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "time"
)

// ClusterEventType identifies the type of cluster event
type ClusterEventType string

const (
    EventTaskCreate   ClusterEventType = "TASK_CREATE"
    EventTaskClaim    ClusterEventType = "TASK_CLAIM"
    EventTaskComplete ClusterEventType = "TASK_COMPLETE"
    EventTaskFail     ClusterEventType = "TASK_FAIL"
    EventTaskReclaim  ClusterEventType = "TASK_RECLAIM"
    EventTaskPause    ClusterEventType = "TASK_PAUSE"
    EventTaskResume   ClusterEventType = "TASK_RESUME"
    EventNodeJoin     ClusterEventType = "NODE_JOIN"
    EventNodeLeave    ClusterEventType = "NODE_LEAVE"
    EventNodeHeartbeat ClusterEventType = "NODE_HEARTBEAT"
)

// ClusterEvent represents a signed, replicated cluster event
type ClusterEvent struct {
    EventID     string                 `json:"event_id"`
    NodeID      string                 `json:"node_id"`
    EventType   ClusterEventType       `json:"event_type"`
    Timestamp   time.Time              `json:"timestamp"`
    VectorClock map[string]int64       `json:"vector_clock"`
    Payload     json.RawMessage        `json:"payload"`
    Signature   []byte                 `json:"signature"`
}

// Sign signs the event with an ed25519 private key
func (e *ClusterEvent) Sign(privKey ed25519.PrivateKey) error {
    data := e.signingData()
    e.Signature = ed25519.Sign(privKey, data)
    return nil
}

// Verify verifies the event signature
func (e *ClusterEvent) Verify(pubKey ed25519.PublicKey) bool {
    data := e.signingData()
    return ed25519.Verify(pubKey, data, e.Signature)
}

// signingData returns the data to be signed (canonical JSON without signature)
func (e *ClusterEvent) signingData() []byte {
    // Create a copy without signature for signing
    temp := struct {
        EventID     string           `json:"event_id"`
        NodeID      string           `json:"node_id"`
        EventType   ClusterEventType `json:"event_type"`
        Timestamp   int64            `json:"timestamp"`
        VectorClock map[string]int64 `json:"vector_clock"`
        Payload     json.RawMessage  `json:"payload"`
    }{
        EventID:     e.EventID,
        NodeID:      e.NodeID,
        EventType:   e.EventType,
        Timestamp:   e.Timestamp.UnixNano(),
        VectorClock: e.VectorClock,
        Payload:     e.Payload,
    }
    data, _ := json.Marshal(temp)
    return data
}

// GenerateEventID creates a unique event ID
func GenerateEventID() string {
    b := make([]byte, 16)
    rand.Read(b)
    return hex.EncodeToString(b)
}

// MarshalJSON implements custom JSON marshaling
func (e *ClusterEvent) MarshalJSON() ([]byte, error) {
    type Alias ClusterEvent
    return json.Marshal(&struct {
        Timestamp int64 `json:"timestamp"`
        *Alias
    }{
        Timestamp: e.Timestamp.UnixNano(),
        Alias:     (*Alias)(e),
    })
}

// UnmarshalJSON implements custom JSON unmarshaling
func (e *ClusterEvent) UnmarshalJSON(data []byte) error {
    type Alias ClusterEvent
    aux := &struct {
        Timestamp int64 `json:"timestamp"`
        *Alias
    }{
        Alias: (*Alias)(e),
    }
    if err := json.Unmarshal(data, &aux); err != nil {
        return err
    }
    e.Timestamp = time.Unix(0, aux.Timestamp)
    return nil
}

// TaskPayload contains the serialized task data
type TaskPayload struct {
    TaskID      string         `json:"task_id"`
    AgentID     string         `json:"agent_id"`
    Description string         `json:"description"`
    Input       map[string]any `json:"input"`
    Constraints []string       `json:"constraints"`
    Priority    int            `json:"priority"`
    CreatedBy   string         `json:"created_by"`
}

// ClaimPayload contains claim metadata
type ClaimPayload struct {
    TaskID    string    `json:"task_id"`
    ClaimedBy string    `json:"claimed_by"`
    TimeoutAt time.Time `json:"timeout_at"`
}

// ReclaimPayload contains reclaim metadata
type ReclaimPayload struct {
    TaskID      string `json:"task_id"`
    Reason      string `json:"reason"`
    ReclaimedBy string `json:"reclaimed_by"`
}

// NodePayload contains node registration data
type NodePayload struct {
    NodeID       string    `json:"node_id"`
    NodeName     string    `json:"node_name"`
    WireGuardPub string    `json:"wireguard_pubkey"`
    SigningPub   []byte    `json:"signing_pubkey"`
    Endpoint     string    `json:"endpoint"`
    Capabilities []string  `json:"capabilities"`
    ClusterIP    string    `json:"cluster_ip"`
    JoinedAt     time.Time `json:"joined_at"`
}
```

- [x] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/models/ -v -run TestClusterEvent
```
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add pkg/models/cluster.go pkg/models/cluster_test.go
git commit -m "feat(cluster): add cluster event types and signing
- ClusterEvent with ed25519 signatures
- Event types: TASK_CREATE, TASK_CLAIM, TASK_COMPLETE, etc.
- Payload types: TaskPayload, ClaimPayload, NodePayload
- Custom JSON marshaling with Unix timestamps"
```

---

### Task 2: Cluster Config Schema

**Files:**
- Create: `internal/cluster/cluster.go`
- Test: `internal/cluster/cluster_test.go`

- [x] **Step 1: Write config loading tests**

```go
// internal/cluster/cluster_test.go
package cluster

import (
    "os"
    "path/filepath"
    "testing"
    "time"
)

func TestLoadClusterConfig(t *testing.T) {
    tmpDir := t.TempDir()
    cfgPath := filepath.Join(tmpDir, "cluster.json5")

    cfg := &ClusterConfig{
        ClusterID:   "test-cluster",
        ClusterName: "Test Cluster",
        Network: NetworkConfig{
            WireGuardSubnet: "10.200.0.0/24",
            WireGuardPort:   51820,
            Interface:       "wg0",
        },
        Gossip: GossipConfig{
            HeartbeatInterval: 30 * time.Second,
            PeerTimeout:       2 * time.Minute,
        },
    }

    // Write config
    data, _ := json.MarshalIndent(cfg, "", "  ")
    os.WriteFile(cfgPath, data, 0600)

    // Load config
    loaded, err := LoadClusterConfig(cfgPath)
    if err != nil {
        t.Fatalf("load failed: %v", err)
    }

    if loaded.ClusterID != cfg.ClusterID {
        t.Errorf("ClusterID mismatch: got %s, want %s", loaded.ClusterID, cfg.ClusterID)
    }
}

func TestMember_MarshalUnmarshal(t *testing.T) {
    member := &Member{
        NodeID:       "node-01",
        NodeName:     "Test Node",
        WireGuardPub: "XyZabc123...",
        SigningPub:   []byte{0x01, 0x02, 0x03},
        Endpoint:     "192.168.1.42:51820",
        Capabilities: []string{"coder", "analyst"},
        ClusterIP:    "10.200.0.1",
        Status:       "active",
    }

    data, err := json.MarshalIndent(member, "", "  ")
    if err != nil {
        t.Fatalf("marshal failed: %v", err)
    }

    var loaded Member
    err = json.Unmarshal(data, &loaded)
    if err != nil {
        t.Fatalf("unmarshal failed: %v", err)
    }

    if loaded.NodeID != member.NodeID {
        t.Errorf("NodeID mismatch")
    }
}
```

- [x] **Step 2: Run test to verify it fails**

```bash
go test ./internal/cluster/ -v
```
Expected: FAIL (package doesn't exist yet)

- [x] **Step 3: Implement cluster config types**

```go
// internal/cluster/cluster.go
package cluster

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"

    "github.com/caimlas/meept/internal/config"
)

// ClusterConfig holds the global cluster configuration
type ClusterConfig struct {
    // Identity
    ClusterID   string `json:"cluster_id"`
    ClusterName string `json:"cluster_name"`

    // Network configuration
    Network NetworkConfig `json:"network"`

    // Gossip configuration
    Gossip GossipConfig `json:"gossip"`

    // Queue configuration
    Queue QueueConfig `json:"queue"`

    // Git sync configuration
    Git GitConfig `json:"git"`

    // Security configuration
    Security SecurityConfig `json:"security"`
}

// NetworkConfig holds WireGuard network settings
type NetworkConfig struct {
    WireGuardSubnet string `json:"wireguard_subnet"`
    WireGuardPort   int    `json:"wireguard_port"`
    Interface       string `json:"mesh_interface"`
}

// GossipConfig holds gossip protocol settings
type GossipConfig struct {
    HeartbeatInterval time.Duration `json:"heartbeat_interval"`
    PeerTimeout       time.Duration `json:"peer_timeout"`
    EventRetention    time.Duration `json:"event_retention"`
    MaxRetryAttempts  int           `json:"max_retry_attempts"`
}

// QueueConfig holds task queue settings
type QueueConfig struct {
    DefaultClaimTimeout     time.Duration `json:"default_claim_timeout"`
    NodeReachabilityTimeout time.Duration `json:"node_reachability_timeout"`
    FullPayloadReplication  bool          `json:"full_payload_replication"`
}

// GitConfig holds git sync settings
type GitConfig struct {
    SyncInterval    time.Duration `json:"sync_interval"`
    HeartbeatCommit bool          `json:"heartbeat_commit"`
}

// SecurityConfig holds security settings
type SecurityConfig struct {
    RequireNodeSignatures   bool          `json:"require_node_signatures"`
    Ed25519KeyRotationDays  int           `json:"ed25519_key_rotation_days"`
}

// Member represents a cluster member (stored in git nodes/*.json5)
type Member struct {
    NodeID        string    `json:"node_id"`
    NodeName      string    `json:"node_name"`
    WireGuardPub  string    `json:"wireguard_pubkey"`
    SigningPub    []byte    `json:"signing_pubkey"`
    Endpoint      string    `json:"endpoint"`
    Capabilities  []string  `json:"capabilities"`
    ClusterIP     string    `json:"cluster_ip"`
    JoinedAt      time.Time `json:"joined_at"`
    LastHeartbeat time.Time `json:"last_heartbeat"`
    Status        string    `json:"status"` // active | inactive | leaving
}

// LoadClusterConfig loads cluster configuration from a JSON5 file
func LoadClusterConfig(path string) (*ClusterConfig, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("failed to read config: %w", err)
    }

    // Use config package's JSON5 parser
    var cfg ClusterConfig
    if err := config.UnmarshalJSON5(data, &cfg); err != nil {
        return nil, fmt.Errorf("failed to parse config: %w", err)
    }

    // Set defaults
    cfg.setDefaults()

    return &cfg, nil
}

// setDefaults applies default values for missing fields
func (c *ClusterConfig) setDefaults() {
    if c.Gossip.HeartbeatInterval == 0 {
        c.Gossip.HeartbeatInterval = 30 * time.Second
    }
    if c.Gossip.PeerTimeout == 0 {
        c.Gossip.PeerTimeout = 2 * time.Minute
    }
    if c.Gossip.MaxRetryAttempts == 0 {
        c.Gossip.MaxRetryAttempts = 3
    }
    if c.Queue.DefaultClaimTimeout == 0 {
        c.Queue.DefaultClaimTimeout = 5 * time.Minute
    }
    if c.Queue.NodeReachabilityTimeout == 0 {
        c.Queue.NodeReachabilityTimeout = 2 * time.Minute
    }
    if c.Git.SyncInterval == 0 {
        c.Git.SyncInterval = 5 * time.Minute
    }
    if c.Security.Ed25519KeyRotationDays == 0 {
        c.Security.Ed25519KeyRotationDays = 90
    }
}

// SaveClusterConfig saves cluster configuration to a JSON5 file
func SaveClusterConfig(path string, cfg *ClusterConfig) error {
    if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
        return err
    }
    data, err := json.MarshalIndent(cfg, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(path, data, 0600)
}

// MemberPath returns the path to a node's registry file
func MemberPath(baseDir, nodeID string) string {
    return filepath.Join(baseDir, "nodes", nodeID+".json5")
}

// LoadMember loads a member record from git
func LoadMember(baseDir, nodeID string) (*Member, error) {
    path := MemberPath(baseDir, nodeID)
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("failed to read member: %w", err)
    }

    var member Member
    if err := config.UnmarshalJSON5(data, &member); err != nil {
        return nil, fmt.Errorf("failed to parse member: %w", err)
    }

    return &member, nil
}

// SaveMember saves a member record to git
func SaveMember(baseDir string, member *Member) error {
    path := MemberPath(baseDir, member.NodeID)
    if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
        return err
    }
    data, err := json.MarshalIndent(member, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(path, data, 0600)
}
```

- [x] **Step 4: Add UnmarshalJSON5 helper to config package**

```go
// internal/config/json5.go (new file or modify existing)
package config

import (
    "github.com/titanous/json5"
)

// UnmarshalJSON5 parses JSON5 data into a struct
func UnmarshalJSON5(data []byte, v interface{}) error {
    return json5.Unmarshal(data, v)
}
```

- [x] **Step 5: Run test to verify it passes**

```bash
go test ./internal/cluster/ -v
```

- [x] **Step 6: Commit**

```bash
git add internal/cluster/cluster.go internal/cluster/cluster_test.go internal/config/json5.go
git commit -m "feat(cluster): add cluster configuration types

- ClusterConfig with network, gossip, queue, git, security sections
- Member type for node registry
- Load/Save functions for config and members
- JSON5 parsing support"
```

---

### Task 3: SQLite Schema Extensions

**Files:**
- Modify: `internal/queue/store.go`
- Create: `internal/queue/schema_cluster.sql`
- Test: `internal/queue/cluster_schema_test.go`

- [x] **Step 1: Write schema migration test**

```go
// internal/queue/cluster_schema_test.go
package queue

import (
    "database/sql"
    "testing"
    "time"

    _ "modernc.org/sqlite"
)

func TestClusterSchemaMigration(t *testing.T) {
    db, err := sql.Open("sqlite", ":memory:")
    if err != nil {
        t.Fatalf("failed to open db: %v", err)
    }

    // Run base schema
    _, err = db.Exec(baseSchema)
    if err != nil {
        t.Fatalf("base schema failed: %v", err)
    }

    // Run cluster migration
    _, err = db.Exec(clusterSchema)
    if err != nil {
        t.Fatalf("cluster migration failed: %v", err)
    }

    // Verify new columns exist
    var colName string
    err = db.QueryRow(`
        SELECT name FROM pragma_table_info('jobs')
        WHERE name = 'managing_node'
    `).Scan(&colName)
    if err != nil {
        t.Error("managing_node column not found")
    }

    // Verify cluster_events table exists
    err = db.QueryRow(`
        SELECT name FROM sqlite_master
        WHERE type='table' AND name='cluster_events'
    `).Scan(&colName)
    if err != nil {
        t.Error("cluster_events table not found")
    }

    // Test inserting a cluster-aware job
    _, err = db.Exec(`
        INSERT INTO jobs (
            id, name, agent_id, status,
            managing_node, claimed_by_node, payload_full
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
    `, "task-001", "Test Task", "coder", "pending",
        "node-01", "node-02", []byte(`{"task_id":"t1"}`))
    if err != nil {
        t.Errorf("failed to insert cluster job: %v", err)
    }

    // Test inserting a cluster event
    _, err = db.Exec(`
        INSERT INTO cluster_events (
            event_id, node_id, event_type, timestamp,
            vector_clock, payload, signature, received_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `, "evt-001", "node-01", "TASK_CREATE",
        time.Now().UnixNano(),
        `{"node-01": 1}`,
        []byte(`{"task_id":"t1"}`),
        []byte{0x01, 0x02},
        time.Now().UnixNano())
    if err != nil {
        t.Errorf("failed to insert cluster event: %v", err)
    }
}
```

- [x] **Step 2: Run test to verify it fails**

```bash
go test ./internal/queue/ -v -run TestClusterSchema
```
Expected: FAIL (clusterSchema doesn't exist)

- [x] **Step 3: Add cluster schema to store.go**

```go
// internal/queue/store.go - add these after baseSchema

// clusterSchema extends the base schema with cluster support
const clusterSchema = `
-- Add cluster fields to jobs table
ALTER TABLE jobs ADD COLUMN cluster_task_id TEXT UNIQUE;
ALTER TABLE jobs ADD COLUMN managing_node TEXT;
ALTER TABLE jobs ADD COLUMN claimed_by_node TEXT;
ALTER TABLE jobs ADD COLUMN timeout_at TIMESTAMP;
ALTER TABLE jobs ADD COLUMN last_heartbeat_at TIMESTAMP;
ALTER TABLE jobs ADD COLUMN payload_full BLOB;
ALTER TABLE jobs ADD COLUMN is_replica INTEGER DEFAULT 0;

-- Create cluster_events table for gossip replication
CREATE TABLE IF NOT EXISTS cluster_events (
    event_id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    vector_clock TEXT NOT NULL,
    payload BLOB NOT NULL,
    signature BLOB NOT NULL,
    received_at INTEGER NOT NULL,
    synced INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_events_type ON cluster_events(event_type);
CREATE INDEX IF NOT EXISTS idx_events_node ON cluster_events(node_id);
CREATE INDEX IF NOT EXISTS idx_events_time ON cluster_events(timestamp);

-- Create cluster_members cache table (populated from git sync)
CREATE TABLE IF NOT EXISTS cluster_members (
    node_id TEXT PRIMARY KEY,
    node_name TEXT,
    wireguard_pub TEXT NOT NULL,
    signing_pub BLOB NOT NULL,
    endpoint TEXT NOT NULL,
    capabilities TEXT,  -- JSON array
    cluster_ip TEXT,
    joined_at INTEGER NOT NULL,
    last_heartbeat INTEGER NOT NULL,
    status TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_members_status ON cluster_members(status);
`
```

- [x] **Step 4: Modify NewStore to apply cluster schema**

```go
// internal/queue/store.go - modify NewStore function

func NewStore(dbPath string) (*Store, error) {
    db, err := sql.Open("sqlite", dbPath)
    if err != nil {
        return nil, err
    }

    // Enable WAL mode for better concurrency
    _, err = db.Exec("PRAGMA journal_mode=WAL")
    if err != nil {
        db.Close()
        return nil, err
    }

    // Apply base schema
    _, err = db.Exec(baseSchema)
    if err != nil {
        db.Close()
        return nil, fmt.Errorf("failed to apply base schema: %w", err)
    }

    // Apply cluster schema (idempotent - ALTER TABLE ignores existing columns)
    _, err = db.Exec(clusterSchema)
    if err != nil {
        db.Close()
        return nil, fmt.Errorf("failed to apply cluster schema: %w", err)
    }

    return &Store{db: db}, nil
}
```

- [x] **Step 5: Run test to verify it passes**

```bash
go test ./internal/queue/ -v -run TestClusterSchema
```

- [x] **Step 6: Commit**

```bash
git add internal/queue/store.go internal/queue/cluster_schema_test.go
git commit -m "feat(cluster): extend SQLite schema for cluster support

- Add cluster columns to jobs table (managing_node, claimed_by_node, etc.)
- Create cluster_events table for gossip replication
- Create cluster_members cache table for git-synced membership
- Idempotent migrations for safe rollout"
```

---

**Plan complete and saved to `docs/superpowers/plans/2026-06-06-distributed-cluster-implementation.md`.**

The plan continues with:

**Phase 2: Git Sync & WireGuard** (Tasks 4-7)
**Phase 3: Gossip Engine** (Tasks 8-12)
**Phase 4: Cluster Queue** (Tasks 13-17)
**Phase 5: CLI Commands** (Tasks 18-23)
**Phase 6: Documentation** (Tasks 24-25)
**Phase 7: Integration Testing** (Tasks 26-28)

---

## Execution Options

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**

Given this is a ~28 task implementation spanning multiple packages, I recommend **subagent-driven** for parallel progress on independent phases.