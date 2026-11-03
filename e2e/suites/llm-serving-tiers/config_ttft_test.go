//go:build e2e

// Config + store-shape scenarios (leaf 05 T4, over leaves 01/03/04):
// models.json5 --cache-reuse loader compatibility (leaf 03), the live
// daemon's llm_calls ledger shape, and the llm.tiers / llm.cache.semantic
// config-acceptance posture.
//
// Documented gap (leaf 04 TTFT): the ttft_ms column, first-delta capture,
// and store-write are NOT landed (no TTFTMS anywhere in internal/llm or
// internal/metrics; leaf 04's tasks are unwritten). The contracted
// ttft_ms >= 0 / ttft_ms == -1 assertions are therefore UNASSERTABLE
// today - a query against the nonexistent column would fail for the right
// reason (column missing) but prove nothing about the feature. This file
// pins the observable state that DOES exist: the streaming turn records
// llm_calls rows (the exact table + columns the leaf-04 migration
// extends), the loader accepts --cache-reuse, and the daemon boots with
// the new config keys. The ttft_ms assertions land with leaf 04; the gap
// is flagged to the orchestrator.
package llmservingtiers

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"

	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon)
)

// ttftSuiteOverlay is the meept.json5 overlay shared by the live-daemon
// scenarios: the tier policy and semantic cache keys enabled (config must
// be ACCEPTED - the daemon-side wiring consuming them is a separate gap),
// plus the short failure policy the chat path needs for fast stub failure
// rotation.
func ttftSuiteOverlay() map[string]any {
	return map[string]any{
		"llm.tiers.enabled":                 true,
		"llm.tiers.max_depth":               2,
		"llm.tiers.signal_degraded":         true,
		"llm.cache.semantic.enabled":        true,
		"llm.failure_policy.base_throttle":  "1s",
		"llm.failure_policy.pacing.enabled": false,
		"multiagent.enabled":                true,
	}
}

// TestConfigCacheReuseLoader (T4a): a models.json5 fixture carrying
// --cache-reuse 256 on both llama-server spawn_command entries boots
// through the harness - the loader parses the flag positions without
// error (leaf 03 compatibility, pinned at the daemon-boot boundary rather
// than a raw config-load call).
func TestConfigCacheReuseLoader(t *testing.T) {
	// Count the flags in the REPO's shipped models.json5 (leaf 03 landed
	// them); the harness daemon boots against a seeded copy of the same
	// loader path.
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(repoRoot, "config", "models.json5"))
	if err != nil {
		t.Fatalf("read config/models.json5: %v", err)
	}
	n := strings.Count(string(data), "--cache-reuse")
	if n < 2 {
		t.Fatalf("config/models.json5 carries %d --cache-reuse flags, want at least the 2 llama-server entries from leaf 03", n)
	}
	if strings.Contains(string(data), "fresh cache per call") {
		t.Fatal("stale 'fresh cache per call' comment survived leaf 03's correction")
	}

	// Boot the scratch daemon: the live config-load boundary. Any loader
	// regression on the flag shape fails startup.
	s := harness.Start(t, harness.WithConfigOverlay(ttftSuiteOverlay()))
	harness.WaitFor(t, 60*time.Second, "the daemon to become healthy", func() bool {
		return s.HealthOK()
	})
	log := tierSuiteDaemonLog(s)
	if strings.Contains(log, "config validation") || strings.Contains(log, "failed to load config") {
		t.Fatalf("daemon logged a config error with --cache-reuse fixtures + tier/semantic keys:\n%s", log)
	}
}

// TestLLMCallsLedgerOnStreamingTurn (T4b shape pin): a streaming-capable
// chat turn lands llm_calls rows in metrics.db with the EXACT column set
// leaf 04's migration extends. The ttft_ms >= 0 assertion itself is
// deferred (gap documented in the file header) - leaf 04 is not landed.
func TestLLMCallsLedgerOnStreamingTurn(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(ttftSuiteOverlay()))
	harness.WaitFor(t, 60*time.Second, "the daemon to become healthy", func() bool {
		return s.HealthOK()
	})
	s.RegisterProject(t, "e2e-project")
	sess := s.CreateSession(t, "tiers", s.ProjectDir)
	if reply := s.ChatTurn(t, sess, "hello there", 120*time.Second); reply == "" {
		t.Fatal("chat turn returned empty reply")
	}

	// metrics.db lives in the daemon data dir (store.go NewStore wiring).
	dbPath := filepath.Join(s.StateDir, "metrics.db")
	rows := tierSuiteLLMCallsRows(t, dbPath, 50)
	if len(rows) == 0 {
		t.Fatal("metrics.db has no llm_calls rows after a completed chat turn")
	}
	// Pin the columns the TTFT migration touches: provider/model_id name
	// the serving model, latency_ms is populated, trace_id/span_id exist
	// (the join shape leaf 04 writes ttft_ms alongside).
	newest := rows[0]
	if newest.ModelID == "" || newest.Provider == "" {
		t.Fatalf("newest llm_calls row missing provider/model_id: %+v", newest)
	}
	if newest.LatencyMS <= 0 {
		t.Fatalf("newest llm_calls row latency_ms = %d, want > 0", newest.LatencyMS)
	}
}

// TestConfigTierSemanticKeysAccepted (leaf 01/02 config acceptance at the
// daemon boundary): the llm.tiers.* and llm.cache.semantic.* keys parse
// and the daemon boots cleanly with them enabled - the wiring seam
// (SetTierPolicy call site / semantic client attachment) is a documented
// gap, so this pins the config contract only.
func TestConfigTierSemanticKeysAccepted(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(ttftSuiteOverlay()))
	harness.WaitFor(t, 60*time.Second, "the daemon to become healthy", func() bool {
		return s.HealthOK()
	})
	log := tierSuiteDaemonLog(s)
	for _, bad := range []string{"config validation", "failed to load config", "unknown field"} {
		if strings.Contains(log, bad) {
			t.Fatalf("daemon rejected the tier/semantic config keys (%q in boot log):\n%s", bad, log)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// tierSuiteDaemonLog reads the scratch daemon's full log.
func tierSuiteDaemonLog(s *harness.Stack) string {
	data, err := os.ReadFile(s.Daemon.LogPath())
	if err != nil {
		return ""
	}
	return string(data)
}

// llmCallsRow is one llm_calls row (the subset leaf 05 pins).
type llmCallsRow struct {
	ID        int64
	Provider  string
	ModelID   string
	LatencyMS int64
	TraceID   string
	SpanID    string
}

// tierSuiteLLMCallsRows reads llm_calls newest-first.
func tierSuiteLLMCallsRows(t *testing.T, dbPath string, limit int) []llmCallsRow {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		t.Fatalf("open metrics.db: %v", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	rows, err := db.Query(`SELECT id, COALESCE(provider,''), COALESCE(model_id,''),
		COALESCE(latency_ms,0), COALESCE(trace_id,''), COALESCE(span_id,'')
		FROM llm_calls ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		t.Fatalf("query llm_calls: %v", err)
	}
	defer rows.Close()
	var out []llmCallsRow
	for rows.Next() {
		var r llmCallsRow
		if err := rows.Scan(&r.ID, &r.Provider, &r.ModelID, &r.LatencyMS, &r.TraceID, &r.SpanID); err != nil {
			t.Fatalf("scan llm_calls: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate llm_calls: %v", err)
	}
	return out
}
