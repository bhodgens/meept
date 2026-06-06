package queue

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
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

	// Apply cluster schema via the idempotent mechanism
	if err := applyClusterSchemaTo(db); err != nil {
		t.Fatalf("cluster migration failed: %v", err)
	}

	// Verify new columns exist on the jobs table
	for _, col := range []string{"managing_node", "claimed_by_node", "timeout_at",
		"last_heartbeat_at", "payload_full", "is_replica"} {
		var colName string
		err = db.QueryRow(`
			SELECT name FROM pragma_table_info('jobs')
			WHERE name = ?`, col).Scan(&colName)
		if err != nil {
			t.Errorf("column %q not found on jobs table", col)
		}
	}

	// Verify cluster_events table exists
	var tableName string
	err = db.QueryRow(`
		SELECT name FROM sqlite_master
		WHERE type='table' AND name='cluster_events'`).Scan(&tableName)
	if err != nil {
		t.Error("cluster_events table not found")
	}

	// Verify cluster_members table exists
	err = db.QueryRow(`
		SELECT name FROM sqlite_master
		WHERE type='table' AND name='cluster_members'`).Scan(&tableName)
	if err != nil {
		t.Error("cluster_members table not found")
	}

	// Test inserting a cluster-aware job
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`
		INSERT INTO jobs (
			id, type, agent_id, state,
			managing_node, claimed_by_node, payload_full, payload,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"task-001", "test", "coder", "pending",
		"node-01", "node-02", []byte(`{"task_id":"t1"}`),
		`{"task_id":"t1"}`,
		now, now)
	if err != nil {
		t.Errorf("failed to insert cluster job: %v", err)
	}

	// Test inserting a cluster event
	_, err = db.Exec(`
		INSERT INTO cluster_events (
			event_id, node_id, event_type, timestamp,
			vector_clock, payload, signature, received_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"evt-001", "node-01", "TASK_CREATE",
		time.Now().UnixNano(),
		`{"node-01": 1}`,
		[]byte(`{"task_id":"t1"}`),
		[]byte{0x01, 0x02},
		time.Now().UnixNano())
	if err != nil {
		t.Errorf("failed to insert cluster event: %v", err)
	}

	// Test inserting a cluster member
	_, err = db.Exec(`
		INSERT INTO cluster_members (
			node_id, node_name, wireguard_pub, signing_pub,
			endpoint, capabilities, cluster_ip,
			joined_at, last_heartbeat, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"node-01", "Test Node", "XyZabc123...",
		[]byte{0xde, 0xad, 0xbe, 0xef},
		"192.168.1.42:51820",
		`["coder","analyst"]`,
		"10.200.0.1",
		time.Now().UnixNano(),
		time.Now().UnixNano(),
		"active")
	if err != nil {
		t.Errorf("failed to insert cluster member: %v", err)
	}
}

func TestClusterSchemaIsIdempotent(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	// Run base schema first
	_, err = db.Exec(baseSchema)
	if err != nil {
		t.Fatalf("base schema failed: %v", err)
	}

	// Running applyClusterSchema twice should not error
	if err := applyClusterSchemaTo(db); err != nil {
		t.Fatalf("first cluster migration failed: %v", err)
	}

	if err := applyClusterSchemaTo(db); err != nil {
		t.Fatalf("second cluster migration should be idempotent, got: %v", err)
	}
}

// applyClusterSchemaTo applies the cluster schema to a raw *sql.DB
// for testing purposes (mimics what Store.applyClusterSchema does).
func applyClusterSchemaTo(db *sql.DB) error {
	// Check which columns already exist.
	var existingCols []string
	rows, err := db.Query(`PRAGMA table_info(jobs)`)
	if err == nil {
		for rows.Next() {
			var (
				cid         int
				name        string
				ctype       string
				notnull     int
				dfltValue   sql.NullString
				pk          int
			)
			if err2 := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err2 == nil {
				existingCols = append(existingCols, name)
			}
		}
		rows.Close()
	}

	// Filter out existing columns from ALTER statements.
	var stmts []string
	for _, col := range clusterColumnNames {
		if has(existingCols, col) {
			continue
		}
		stmts = append(stmts, "ALTER TABLE jobs ADD COLUMN "+col)
	}

	if len(stmts) > 0 {
		_, err = db.Exec(strings.Join(stmts, "; "))
		if err != nil {
			return err
		}
	}

	// CREATE TABLE / IF NOT EXISTS statements are naturally idempotent.
	createStmts := []string{
		`CREATE TABLE IF NOT EXISTS cluster_events (
			event_id TEXT PRIMARY KEY,
			node_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			timestamp INTEGER NOT NULL,
			vector_clock TEXT NOT NULL,
			payload BLOB NOT NULL,
			signature BLOB NOT NULL,
			received_at INTEGER NOT NULL,
			synced INTEGER DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_type ON cluster_events(event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_events_node ON cluster_events(node_id)`,
		`CREATE INDEX IF NOT EXISTS idx_events_time ON cluster_events(timestamp)`,
		`CREATE TABLE IF NOT EXISTS cluster_members (
			node_id TEXT PRIMARY KEY,
			node_name TEXT,
			wireguard_pub TEXT NOT NULL,
			signing_pub BLOB NOT NULL,
			endpoint TEXT NOT NULL,
			capabilities TEXT,
			cluster_ip TEXT,
			joined_at INTEGER NOT NULL,
			last_heartbeat INTEGER NOT NULL,
			status TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_members_status ON cluster_members(status)`,
	}

	for _, stmt := range createStmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}

	return nil
}
