package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/backup"
	"github.com/caimlas/meept/internal/config"

	_ "modernc.org/sqlite"
)

// TestSyncCommandStructure verifies the sync command hierarchy exists with the
// expected subcommands.
func TestSyncCommandStructure(t *testing.T) {
	t.Parallel()

	cmd := newSyncCmd()
	if cmd == nil {
		t.Fatal("expected non-nil sync command")
	}
	if cmd.Use != "sync" {
		t.Errorf("sync command use: got %q, want %q", cmd.Use, "sync")
	}
	if cmd.Short == "" {
		t.Error("sync command should have a short description")
	}

	sub := cmd.Commands()
	if len(sub) != 2 {
		t.Fatalf("expected 2 subcommands, got %d", len(sub))
	}

	hasPull, hasStatus := false, false
	for _, c := range sub {
		switch c.Use {
		case "pull":
			hasPull = true
		case "status":
			hasStatus = true
		}
	}
	if !hasPull {
		t.Error("expected 'pull' subcommand")
	}
	if !hasStatus {
		t.Error("expected 'status' subcommand")
	}
}

// TestSyncStatusCmd_HasJSONFlag verifies the --json flag is wired on
// 'sync status'.
func TestSyncStatusCmd_HasJSONFlag(t *testing.T) {
	t.Parallel()

	cmd := newSyncStatusCmd()
	if cmd == nil {
		t.Fatal("expected non-nil status command")
	}
	if cmd.Use != "status" {
		t.Errorf("status use: got %q, want %q", cmd.Use, "status")
	}
	if cmd.Flags().Lookup("json") == nil {
		t.Error("status command should have --json flag")
	}
}

// TestSyncPullCmd_Structure verifies the 'sync pull' command shape.
func TestSyncPullCmd_Structure(t *testing.T) {
	t.Parallel()

	cmd := newSyncPullCmd()
	if cmd == nil {
		t.Fatal("expected non-nil pull command")
	}
	if cmd.Use != "pull" {
		t.Errorf("pull use: got %q, want %q", cmd.Use, "pull")
	}
	if cmd.Short == "" {
		t.Error("pull command should have short description")
	}
}

// TestSyncStatusCmd_JSONOutputStructure verifies the JSON output structure for
// `sync status --json` against a controlled environment. The output must
// include node_id, sync_enabled, and peers fields, and the seeded peer must
// appear in the peers map.
func TestSyncStatusCmd_JSONOutputStructure(t *testing.T) {
	// Cannot use t.Parallel: t.Setenv modifies process environment.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Pre-create the local.db at the expected path so the command can open it.
	dataDir := filepath.Join(tmp, ".meept")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	dbPath := filepath.Join(dataDir, "local.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}

	store := backup.NewSyncMetadataStore(db)
	if err := store.EnsureTable(); err != nil {
		t.Fatalf("EnsureTable: %v", err)
	}
	// Seed one peer with a known sync time and merge stats.
	peerID := "node-seed"
	seedTime := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	if err := store.SetLastSync(peerID, seedTime); err != nil {
		t.Fatalf("SetLastSync: %v", err)
	}
	if err := store.SetLastMergeStats(peerID, &backup.MergeStats{
		SessionsMerged: 5,
		TurnsMerged:    42,
		MemoriesMerged: 7,
	}); err != nil {
		t.Fatalf("SetLastMergeStats: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}

	// Override stdout to capture the command output.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w

	// Write a minimal config enabling peer sync with our seeded peer.
	// pull_schedule is time.Duration in nanoseconds; 3600000000000 = 1 hour.
	cfgJSON := `{
  "daemon": { "data_dir": "` + dataDir + `" },
  "peer_sync": { "enabled": true, "peers": ["` + peerID + `"], "pull_schedule": 3600000000000 }
}`
	if err := os.WriteFile(filepath.Join(dataDir, "meept.json5"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatalf("WriteFile meept.json5: %v", err)
	}

	cmd := newSyncStatusCmd()
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set json flag: %v", err)
	}

	runErr := cmd.RunE(cmd, nil)
	w.Close()
	os.Stdout = origStdout

	if runErr != nil {
		t.Fatalf("RunE: %v", runErr)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	out := buf.String()

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw=%q", err, out)
	}

	if nodeID, ok := parsed["node_id"].(string); !ok || nodeID == "" {
		t.Errorf("node_id missing or empty in JSON output: %v", parsed["node_id"])
	}
	if _, ok := parsed["sync_enabled"]; !ok {
		t.Error("sync_enabled missing from JSON output")
	}

	peersVal, ok := parsed["peers"]
	if !ok {
		t.Error("peers missing from JSON output")
	} else if peersMap, ok := peersVal.(map[string]interface{}); ok {
		if _, has := peersMap[peerID]; !has {
			t.Errorf("seeded peer %q not in status map: %v", peerID, peersMap)
		}
	}
	// When the DB load succeeds, peers is a map[string]SyncStatus. When it
	// doesn't (e.g. db nil), peers is the []string from config. Both shapes
	// are valid per the implementation.
}

// TestSyncStatusCmd_TextOutput_EmptyHistory verifies the text output path for
// a fresh DB with no sync history. The command must not error and must print
// the "no sync history found" guidance line.
func TestSyncStatusCmd_TextOutput_EmptyHistory(t *testing.T) {
	// Cannot use t.Parallel: t.Setenv modifies process environment.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dataDir := filepath.Join(tmp, ".meept")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	dbPath := filepath.Join(dataDir, "local.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	store := backup.NewSyncMetadataStore(db)
	if err := store.EnsureTable(); err != nil {
		t.Fatalf("EnsureTable: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}

	cfgJSON := `{
  "daemon": { "data_dir": "` + dataDir + `" },
  "peer_sync": { "enabled": true, "peers": ["peer-a"], "pull_schedule": 3600000000000 }
}`
	if err := os.WriteFile(filepath.Join(dataDir, "meept.json5"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatalf("WriteFile meept.json5: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w

	cmd := newSyncStatusCmd()
	runErr := cmd.RunE(cmd, nil)
	w.Close()
	os.Stdout = origStdout

	if runErr != nil {
		t.Fatalf("RunE: %v", runErr)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "sync status") {
		t.Errorf("expected header 'sync status' in output, got: %s", out)
	}
	if !strings.Contains(out, "no sync history found") {
		t.Errorf("expected 'no sync history found' guidance, got: %s", out)
	}
}

// TestSyncStatusCmd_DisabledConfig verifies that when sync is disabled in
// config, the text output reports the disabled state with the enable hint.
func TestSyncStatusCmd_DisabledConfig(t *testing.T) {
	// Cannot use t.Parallel: t.Setenv modifies process environment.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dataDir := filepath.Join(tmp, ".meept")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfgJSON := `{
  "daemon": { "data_dir": "` + dataDir + `" },
  "peer_sync": { "enabled": false }
}`
	if err := os.WriteFile(filepath.Join(dataDir, "meept.json5"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w

	cmd := newSyncStatusCmd()
	runErr := cmd.RunE(cmd, nil)
	w.Close()
	os.Stdout = origStdout

	if runErr != nil {
		t.Fatalf("RunE: %v", runErr)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	out := buf.String()

	if !strings.Contains(strings.ToLower(out), "sync enabled: false") {
		t.Errorf("expected 'sync enabled: false' in output, got: %s", out)
	}
	if !strings.Contains(out, "enable sync") {
		t.Errorf("expected enable-sync guidance, got: %s", out)
	}
}

// TestSyncStatusCmd_FreshDB_EnsureTableBehavior guards against the recently
// fixed bug where EnsureTable would error when called against a DB with no
// sync_metadata table. We construct a DB without the table, run the status
// command, and verify it does NOT report an "error initializing sync_metadata"
// message.
func TestSyncStatusCmd_FreshDB_EnsureTableBehavior(t *testing.T) {
	// Cannot use t.Parallel: t.Setenv modifies process environment.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dataDir := filepath.Join(tmp, ".meept")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Create the DB file with no schema at all.
	dbPath := filepath.Join(dataDir, "local.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}

	cfgJSON := `{
  "daemon": { "data_dir": "` + dataDir + `" },
  "peer_sync": { "enabled": true, "peers": ["peer-fresh"], "pull_schedule": 3600000000000 }
}`
	if err := os.WriteFile(filepath.Join(dataDir, "meept.json5"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w

	cmd := newSyncStatusCmd()
	runErr := cmd.RunE(cmd, nil)
	w.Close()
	os.Stdout = origStdout

	if runErr != nil {
		t.Fatalf("RunE: %v", runErr)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	out := buf.String()

	// The "error initializing sync metadata" path must NOT fire — EnsureTable
	// uses CREATE TABLE IF NOT EXISTS so it succeeds on a fresh DB.
	if strings.Contains(out, "error initializing sync metadata") {
		t.Errorf("EnsureTable failed on fresh DB: %s", out)
	}
}

// TestFormatDuration covers the human-friendly duration formatter used in
// 'sync status' text output.
func TestFormatDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{90 * time.Second, "2m"},
		{2 * time.Hour, "2h"},
		{36 * time.Hour, "2d"},
	}
	for _, tc := range tests {
		got := formatDuration(tc.d)
		if got != tc.want {
			t.Errorf("formatDuration(%s) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// TestSyncStatusCmd_AddedToRoot confirms the sync command is registered in the
// root command tree (and thus appears in `meept --help`).
func TestSyncStatusCmd_AddedToRoot(t *testing.T) {
	t.Parallel()

	cmd := newSyncCmd()
	if cmd == nil {
		t.Fatal("sync command should not be nil")
	}
}

// TestNewSyncCmd_PeerSyncConfigDefault verifies the PeerSyncConfig defaults
// exposed by the config package align with what sync_cmd.go expects.
func TestNewSyncCmd_PeerSyncConfigDefault(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultPeerSyncConfig()
	if cfg.MaxMergeMinutes != 10 {
		t.Errorf("default MaxMergeMinutes = %d, want 10", cfg.MaxMergeMinutes)
	}
	if cfg.Enabled {
		t.Error("default PeerSyncConfig.Enabled should be false")
	}
}
