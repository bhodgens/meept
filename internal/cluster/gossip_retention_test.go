package cluster

import (
	"database/sql"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	_ "github.com/mattn/go-sqlite3"
)

// TestRetentionSweep verifies that the cluster_events retention sweep
// deletes rows older than EventTableRetention and keeps fresh ones.
func TestRetentionSweep(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/test.db"
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	defer db.Close()

	// Create cluster_events table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS cluster_events (
			event_id TEXT PRIMARY KEY,
			node_id TEXT,
			event_type TEXT,
			timestamp INTEGER,
			vector_clock TEXT,
			payload TEXT,
			signature TEXT,
			received_at INTEGER
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	b := bus.New(nil, discardLogger())
	cfg := &Config{
		NodeID: "node-retention",
		Gossip: GossipConfig{
			HeartbeatInterval:   time.Hour,
			PeerTimeout:         time.Hour,
			EventRetention:      time.Hour,
			EventTableRetention: 5 * time.Minute, // 5 minutes for test
		},
		Security: SecurityConfig{RequireNodeSignatures: false},
	}

	engine := NewGossipEngine(cfg, "node-retention", b, discardLogger(), WithDatabase(db))

	// Insert a "fresh" event (received_at = now)
	freshTime := time.Now().UnixNano()
	_, err = db.Exec(`
		INSERT INTO cluster_events (event_id, node_id, event_type, timestamp, vector_clock, payload, signature, received_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "fresh-event", "node-a", "session_turn", freshTime, "{}", "{}", "", freshTime)
	if err != nil {
		t.Fatalf("insert fresh: %v", err)
	}

	// Insert an "old" event (received_at = now - 10 minutes)
	oldTime := time.Now().UnixNano() - int64(10*time.Minute)
	_, err = db.Exec(`
		INSERT INTO cluster_events (event_id, node_id, event_type, timestamp, vector_clock, payload, signature, received_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "old-event", "node-b", "session_turn", oldTime, "{}", "{}", "", oldTime)
	if err != nil {
		t.Fatalf("insert old: %v", err)
	}

	// Verify both rows exist before sweep
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM cluster_events").Scan(&count)
	if err != nil {
		t.Fatalf("count before sweep: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows before sweep, got %d", count)
	}

	// Run the retention sweep
	engine.sweepClusterEventsRetention()

	// Verify only the fresh row remains
	err = db.QueryRow("SELECT COUNT(*) FROM cluster_events").Scan(&count)
	if err != nil {
		t.Fatalf("count after sweep: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row after sweep, got %d", count)
	}

	// Verify the remaining row is the fresh one
	var eventID string
	err = db.QueryRow("SELECT event_id FROM cluster_events").Scan(&eventID)
	if err != nil {
		t.Fatalf("select event_id: %v", err)
	}
	if eventID != "fresh-event" {
		t.Errorf("expected 'fresh-event' to remain, got %q", eventID)
	}
}
