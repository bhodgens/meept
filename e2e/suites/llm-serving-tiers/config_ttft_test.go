//go:build e2e

// Config + store-shape scenarios (leaf 05 T4, over leaves 01/03/04):
// models.json5 --cache-reuse loader compatibility (leaf 03), the live
// daemon's llm_calls ledger shape WITH the ttft_ms value assertions
// (TIER-2: the column is live via migration v9 and the leaf-04 capture
// paths, so the previously-deferred assertions are landed), and the
// llm.tiers / llm.cache.semantic config-acceptance posture.
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
// be ACCEPTED; the daemon-side wiring now exists but the harness stubs
// leave both tiers inert for these turns - embedder never resolves against
// the stub topology), plus the short failure policy the chat path needs
// for fast stub failure rotation.
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

// TestLLMCallsLedgerTTFTOnStreamingTurn (T4b, the leaf's Integration item
// 4 — now ASSERTED, no longer deferred): a turn whose LLM calls ride the
// daemon's STREAMING path lands llm_calls rows carrying ttft_ms >= 0.
// Which call streams: a tool-bearing turn's executor requests go through
// chatWithFailoverRaw with a delta callback (loop.go hands EVERY chat turn
// streamOnDelta), the manager fans to the client's ChatWithDeltaCallback,
// and the harness FakeLLM answers "stream": true bodies with SSE deltas —
// leaf 04's first-delta capture arms per attempt and stamps the measured
// wait. Non-streaming calls in the same ledger (classifier etc.) carry
// the -1 sentinel; that half is T4c's test.
func TestLLMCallsLedgerTTFTOnStreamingTurn(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(ttftSuiteOverlay()))
	harness.WaitFor(t, 60*time.Second, "the daemon to become healthy", func() bool {
		return s.HealthOK()
	})
	s.RegisterProject(t, "e2e-project")
	sess := s.CreateSession(t, "tiers", s.ProjectDir)
	// Script a tool-call turn: the executor step's requests stream through
	// the fake's writeSSE lane (body["stream"]=true) — the same lane the
	// TTFT capture arms on. The plain-text turn ALSO reaches that lane,
	// but the tool-bearing shape is the deterministic one (classifier and
	// planner calls stay non-streaming; the executor call is the turn's
	// core LLM work and always arrives with stream=true).
	artifact := filepath.Join(s.ProjectDir, "ttft-probe.txt")
	s.Fake.SetPostToolText("Created ttft-probe.txt at " + artifact + ".")
	s.Fake.EnqueueFileWrite("call-ttft", artifact, "ttft")
	if reply := s.ChatTurn(t, sess, "Create a file named ttft-probe.txt containing ttft", 120*time.Second); reply == "" {
		t.Fatal("chat turn returned empty reply")
	}

	// metrics.db lives in the daemon data dir (store.go NewStore wiring).
	// The turn is async and recordUsageStore writes from a goroutine, so
	// poll the ledger until a measured (ttft_ms >= 0) row lands — polling
	// only EXTENDS the wait; the assertions below stay exact.
	dbPath := filepath.Join(s.StateDir, "metrics.db")
	var measuredRow *llmCallsRow
	harness.WaitFor(t, 30*time.Second, "a measured (ttft_ms >= 0) llm_calls row", func() bool {
		for _, r := range tierSuiteLLMCallsRows(t, dbPath, 50) {
			if r.TTFTMS >= 0 {
				row := r
				measuredRow = &row
				return true
			}
		}
		return false
	})
	if measuredRow == nil {
		t.Fatal("no llm_calls row carries ttft_ms >= 0 after a streaming chat turn")
	}
	m := *measuredRow
	// Column-contract pins on the measured row (leaf 04 + the trace
	// shape the ttft_ms write sits alongside):
	if m.Provider == "" || m.ModelID == "" {
		t.Fatalf("measured llm_calls row missing provider/model_id: %+v", m)
	}
	if m.LatencyMS < 0 {
		t.Fatalf("measured llm_calls row latency_ms = %d, want >= 0", m.LatencyMS)
	}
	if m.TTFTMS > m.LatencyMS {
		t.Fatalf("measured row id %d: ttft_ms (%d) > latency_ms (%d): first token cannot precede call end", m.ID, m.TTFTMS, m.LatencyMS)
	}
}

// TestLLMCallsLedgerNonStreamingSentinel (T4c): the ledger's -1 sentinel is
// observable on the live daemon. The daemon's intent classifier dials the
// fake LLM on the NON-streaming path (LLMClassifier.chatWithFailover uses
// plain Chat, no delta callback -> leaf 04 arms no clock -> ttft_ms -1),
// so a turn that routes through classification leaves at least one
// sentinel row. Newest-row-==-1 is NOT asserted (streaming executor rows
// race the classifier row); the assertion is the SENTINEL VALUE ITSELF
// landing in the column exactly as the contract states, alongside the
// measured rows from the same turn.
func TestLLMCallsLedgerNonStreamingSentinel(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(ttftSuiteOverlay()))
	harness.WaitFor(t, 60*time.Second, "the daemon to become healthy", func() bool {
		return s.HealthOK()
	})
	s.RegisterProject(t, "e2e-project")
	sess := s.CreateSession(t, "tiers-ns", s.ProjectDir)
	if reply := s.ChatTurn(t, sess, "just say hi", 120*time.Second); reply == "" {
		t.Fatal("chat turn returned empty reply")
	}

	dbPath := filepath.Join(s.StateDir, "metrics.db")
	var rows []llmCallsRow
	harness.WaitFor(t, 30*time.Second, "llm_calls rows from the turn", func() bool {
		rows = tierSuiteLLMCallsRows(t, dbPath, 50)
		return len(rows) > 0
	})
	sentinel := 0
	measured := 0
	for _, r := range rows {
		switch {
		case r.TTFTMS == -1:
			sentinel++
		case r.TTFTMS >= 0:
			measured++
		default:
			t.Fatalf("row id %d carries ttft_ms %d: outside the contract domain {-1} U [0, inf)", r.ID, r.TTFTMS)
		}
	}
	if sentinel == 0 {
		t.Fatalf("no ttft_ms == -1 row after a turn that routes through the non-streaming classifier path (rows: %+v)", rows)
	}
}

// TestConfigTierSemanticKeysAccepted (leaf 01/02 config acceptance at the
// daemon boundary): the llm.tiers.* and llm.cache.semantic.* keys parse
// and the daemon boots cleanly with them enabled. The wiring seam landed
// (issue #82 arm-up: applyTierPolicyToProviderManager arms llm.tiers;
// buildSemanticCacheTier runs at client construction), so this now pins
// both the config contract AND the armed posture: with
// llm.cache.semantic.enabled=true and no embedder configured, the daemon
// logs the degraded-off Warn line ("semantic cache disabled: no
// embedder") and still boots clean - the degraded-never-boot-fails
// guarantee of wiring-followups-final leaf 05 proven on the live daemon.
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
	// Wiring-armed proof: the semantic construction site RAN and degraded
	// to exact-only (the harness sets no embedder_alias and no
	// memory.embeddings provider), never failing boot.
	if !strings.Contains(log, "semantic cache disabled: no embedder") {
		t.Fatalf("daemon boot log missing the semantic-cache degraded Warn line - the leaf-05 construction site did not run:\n%s", log)
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

// llmCallsRow is one llm_calls row (the subset leaf 05 pins, plus ttft_ms
// for the TIER-2 value assertions).
type llmCallsRow struct {
	ID        int64
	Provider  string
	ModelID   string
	LatencyMS int64
	TTFTMS    int64
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
		COALESCE(latency_ms,0), COALESCE(ttft_ms,-1), COALESCE(trace_id,''), COALESCE(span_id,'')
		FROM llm_calls ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		t.Fatalf("query llm_calls: %v", err)
	}
	defer rows.Close()
	var out []llmCallsRow
	for rows.Next() {
		var r llmCallsRow
		if err := rows.Scan(&r.ID, &r.Provider, &r.ModelID, &r.LatencyMS, &r.TTFTMS, &r.TraceID, &r.SpanID); err != nil {
			t.Fatalf("scan llm_calls: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate llm_calls: %v", err)
	}
	return out
}
