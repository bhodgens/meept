//go:build e2e

// Suite extension (issue #160 follow-up): durable execution-run history.
// Scenario 05 proves the stateful guarantee the live WS relay cannot give:
// a real bot round is persisted server-side, and the run is still queryable
// via agents.executionRuns AFTER a full daemon restart (the SQLite
// projection survives; a live-only surface would lose it). Also pins the
// agents.executionHistogram shape (8 cells) and the HTTP bridge route.
package executionobservability

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// execRunRow mirrors the persisted-run wire shape (activity.ExecutionRun).
type execRunRow struct {
	ID         int64  `json:"id"`
	Tier       string `json:"tier"`
	AgentID    string `json:"agent_id"`
	Status     string `json:"status"`
	TokensUsed int    `json:"tokens_used"`
	DurationMS int64  `json:"duration_ms"`
	TriggerRef string `json:"trigger_ref"`
	CreatedAt  string `json:"created_at"`
}

func waitForRPCSocket(t *testing.T, s *harness.Stack) {
	t.Helper()
	// bootDaemon returns on HTTP /health, which can answer a beat before
	// the unix RPC socket is bound (client transports come up during boot).
	// Probe with a raw dial (harness.DialRPC fatals on failure) until the
	// socket accepts connections.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", s.SocketPath, time.Second)
		if err == nil {
			if closeErr := conn.Close(); closeErr != nil {
				t.Logf("rpc probe close: %v", closeErr)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("rpc socket %s not ready within 30s after restart", s.SocketPath)
}

func listExecutionRuns(t *testing.T, s *harness.Stack, agentID string) ([]execRunRow, float64) {
	t.Helper()
	params := map[string]any{"limit": 100}
	if agentID != "" {
		params["agent_id"] = agentID
	}
	res := harness.DialRPC(t, s.SocketPath).CallResult("agents.executionRuns", params)
	raw, err := json.Marshal(res["runs"])
	if err != nil {
		t.Fatalf("marshal runs: %v", err)
	}
	var rows []execRunRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode runs: %v (%s)", err, raw)
	}
	total, _ := res["total"].(float64)
	return rows, total
}

func waitForPersistedRun(t *testing.T, s *harness.Stack, agentID, what string) execRunRow {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		rows, _ := listExecutionRuns(t, s, agentID)
		for _, r := range rows {
			if r.AgentID == agentID && r.Status != "" {
				return r
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("execution-history: no persisted run for %s arrived (%s)", agentID, what)
	return execRunRow{}
}

// TestExecutionObservability_PersistedHistory (scenario 05) drives a real
// bot bus-trigger round, waits for the durable projection to record it,
// restarts the daemon, and asserts the SAME run is still returned by
// agents.executionRuns — the reconnect/restart guarantee. It also pins the
// histogram envelope (8 cells) for that agent.
func TestExecutionObservability_PersistedHistory(t *testing.T) {
	s := obsStack(t)
	hireEntity(t, s, "obs-hist-bot", "obs.hist.trigger")
	stop := restartDaemon(t, s)
	defer stop()

	scriptRound(s, "OBS-HIST-ROUND")
	publishTrigger(t, s, "obs.hist.trigger", map[string]any{"marker": "OBS-HIST-ROUND"})

	// The projector appends asynchronously off the bus; poll the RPC.
	run := waitForPersistedRun(t, s, "obs-hist-bot", "bot round projection")
	if run.Tier != "bot" {
		t.Errorf("persisted tier = %q, want bot (topic-name discriminator)", run.Tier)
	}
	if run.Status != "completed" && run.Status != "failed" {
		t.Errorf("persisted status = %q, want completed|failed", run.Status)
	}
	if run.CreatedAt == "" {
		t.Error("persisted run carries no created_at")
	}
	firstID := run.ID

	// Full daemon restart: the live WS surface would lose the run; the
	// durable store must keep it.
	stop2 := restartDaemon(t, s)
	defer stop2()
	waitForRPCSocket(t, s)

	rows, total := listExecutionRuns(t, s, "obs-hist-bot")
	if total < 1 || len(rows) < 1 {
		t.Fatalf("after restart: total=%v len=%d, want the persisted run to survive", total, len(rows))
	}
	var found bool
	for _, r := range rows {
		if r.ID == firstID && r.AgentID == "obs-hist-bot" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("after restart: run id=%d not in %v — history did not survive the restart", firstID, rows)
	}

	// Histogram envelope: exactly 8 cells for the agent.
	hist := harness.DialRPC(t, s.SocketPath).CallResult("agents.executionHistogram", map[string]any{
		"agent_id": "obs-hist-bot",
	})
	cells, ok := hist["cells"].([]any)
	if !ok || len(cells) != 8 {
		t.Fatalf("histogram cells = %v (%T), want 8-element array", hist["cells"], hist["cells"])
	}
}
