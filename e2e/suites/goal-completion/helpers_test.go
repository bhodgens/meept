//go:build e2e

// Shared helpers for the goal-completion suite: raw RPC over the sandbox
// Unix socket (length-prefixed JSON-RPC framing), a restart primitive that
// re-boots the daemon against the SAME sandbox home (GoalLoops register only
// at boot, so employees created via RPC need a restart before agents.trigger
// reaches the loop path), and read-only sqlite readers for the plans and
// employee-goal stores.
package goalcompletion

import (
	"bufio"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"

	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon)
)

// ---------------------------------------------------------------------------
// RPC
// ---------------------------------------------------------------------------

// rpcCall dials a fresh connection per call and performs one JSON-RPC
// request over the scratch daemon's Unix socket, using the daemon's framed
// wire protocol ("<length>\n<payload>").
func rpcCall(t *testing.T, s *harness.Stack, method string, params any) (json.RawMessage, error) {
	t.Helper()
	conn, err := net.DialTimeout("unix", s.SocketPath, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	if err := conn.SetDeadline(time.Now().Add(60 * time.Second)); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(conn, "%d\n", len(payload)); err != nil {
		return nil, fmt.Errorf("write frame length: %w", err)
	}
	if _, err := conn.Write(payload); err != nil {
		return nil, fmt.Errorf("write frame payload: %w", err)
	}

	reader := bufio.NewReader(conn)
	lengthLine, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read frame length: %w", err)
	}
	length, err := strconv.Atoi(strings.TrimSpace(lengthLine))
	if err != nil {
		return nil, fmt.Errorf("invalid frame length: %w", err)
	}
	respData := make([]byte, length)
	if _, err := io.ReadFull(reader, respData); err != nil {
		return nil, fmt.Errorf("read frame payload: %w", err)
	}

	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", method, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s: [%d] %s", method, resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// rpcResult runs one RPC call and returns the unwrapped result object,
// failing the test on transport or RPC-error envelopes.
func rpcResult(t *testing.T, s *harness.Stack, method string, params any) map[string]any {
	t.Helper()
	raw, err := rpcCall(t, s, method, params)
	if err != nil {
		t.Fatalf("rpc %s: %v", method, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("rpc %s: result is not an object: %v (%s)", method, err, raw)
	}
	return out
}

// str fetches a string field from a decoded RPC result map.
func str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// ---------------------------------------------------------------------------
// Restart primitive
// ---------------------------------------------------------------------------

// stopDaemon sends SIGTERM and waits for exit (SIGKILL after 10s).
func stopDaemon(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _, _ = proc.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = proc.Kill()
		<-done
	}
	_ = os.Remove(sSocketPath)
}

// sSocketPath mirrors the socket path of the stack under test; set by
// restartDaemon before stopDaemon runs.
var sSocketPath string

// bootDaemon spawns a fresh scratch daemon against the stack's sandbox and
// waits for /health. Same flags and env as harness.Daemon.boot.
func bootDaemon(t *testing.T, s *harness.Stack) {
	t.Helper()

	bin := harness.DaemonPath(t)
	logPath := filepath.Join(s.Work, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open daemon log: %v", err)
	}
	defer logFile.Close()

	cmd := exec.Command(bin,
		"-c", filepath.Join(s.MeeptHome, "meept.json5"),
		"-d", s.StateDir,
		"-s", s.SocketPath,
	)
	cmd.Dir = s.Work
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = s.Env()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}

	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
			ForceAttemptHTTP2: false,
		},
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		resp, err := client.Get(s.HTTPBaseURL() + "/health") //nolint:noctx // bounded poll in test harness
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("re-booted daemon not healthy within 60s; log tail:\n%s", s.Daemon.LogTail())
		}
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatalf("re-booted daemon died during boot; log tail:\n%s", s.Daemon.LogTail())
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// restartDaemon stops the harness-booted daemon and re-boots a fresh daemon
// process against the SAME sandbox home (same meept.json5, state dir, socket
// path, and daemon.log — appended, not truncated). The returned func stops
// the re-booted daemon via the pidfile; the harness's own cleanup remains a
// safe no-op afterwards.
func restartDaemon(t *testing.T, s *harness.Stack) func() {
	t.Helper()

	sSocketPath = s.SocketPath
	oldPID := s.Daemon.Pid()
	if oldPID == 0 {
		t.Fatal("harness daemon pid not recorded; nothing to restart")
	}
	stopDaemon(t, oldPID)

	// Wait for the old process to release the socket path before re-binding.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(s.SocketPath); os.IsNotExist(err) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	bootDaemon(t, s)

	return func() {
		data, err := os.ReadFile(filepath.Join(s.StateDir, "meept.pid"))
		if err != nil {
			return
		}
		var pid int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err == nil {
			stopDaemon(t, pid)
		}
	}
}

// ---------------------------------------------------------------------------
// Employee definitions
// ---------------------------------------------------------------------------

// employeeDefinition is an agents.create payload satisfying every hire-path
// validation (modeled on the tui-flows fixture): a trigger, a constitution
// with requires_approval, valid risk ceiling, and escalates_to for tier 2+.
func employeeDefinition(id, purpose string, tier int, escalationMarker string) map[string]any {
	constitution := map[string]any{
		"purpose":       purpose,
		"role":          "e2e goal-completion employee",
		"charter":       "Complete the assigned goal. Never delete data.",
		"autonomy_tier": tierName(tier),
		"escalates_to":  []string{"user"},
		"constraints": map[string]any{
			"tools_allowed": []string{},
			"risk_ceiling":  "medium",
			"never":         []string{"delete production data"},
		},
		"amendment_policy": map[string]any{
			"self_propose_allowed": false,
			"requires_approval":    true,
			"frozen_fields":        []string{},
		},
		// The tier-3 fulfil cycle runs an escalating round (pending plan)
		// and an autonomous round in the same test; the default cap of 1
		// concurrent plan would skip the second round.
		"max_active_plans": 3,
		"version":          1,
		"authored_by":      "user",
	}
	if escalationMarker != "" {
		constitution["constraints"].(map[string]any)["escalation_triggers"] = []map[string]any{
			{
				"on":     "action",
				"match":  escalationMarker,
				"reason": "e2e escalation probe",
			},
		}
	}
	return map[string]any{
		"id":      id,
		"name":    id,
		"prompt":  "You are " + id + ", an e2e test employee.",
		"enabled": true,
		"triggers": []map[string]any{
			{"type": "cron", "schedule": "0 4 * * *", "enabled": false},
		},
		"constitution": constitution,
	}
}

// tierName maps an AutonomyTier int to its canonical wire name.
func tierName(tier int) string {
	switch tier {
	case 3:
		return "tier_3_autonomous"
	case 2:
		return "tier_2_propose"
	default:
		return "tier_1_reactive"
	}
}

// hireEmployee creates an employee through the REAL agents.create RPC.
func hireEmployee(t *testing.T, s *harness.Stack, id string, tier int, escalationMarker string) {
	t.Helper()
	created := rpcResult(t, s, "agents.create", employeeDefinition(id, "e2e goal-completion probe: "+id, tier, escalationMarker))
	if got := str(created, "id"); got != id {
		t.Fatalf("agents.create returned id %q, want %q: %v", got, id, created)
	}
}

// createGoal creates a goal through the REAL agents.goals.create RPC and
// returns the goal's id.
func createGoal(t *testing.T, s *harness.Stack, employeeID, title, mandate, gateCommand string) string {
	t.Helper()
	spec := map[string]any{
		"employee_id": employeeID,
		"title":       title,
		"mandate":     mandate,
		"source":      "user",
	}
	if gateCommand != "" {
		spec["gate"] = map[string]any{"command": gateCommand, "timeout_seconds": 60}
	}
	created := rpcResult(t, s, "agents.goals.create", spec)
	goal, _ := created["goal"].(map[string]any)
	if goal == nil {
		t.Fatalf("agents.goals.create returned no goal object: %v", created)
	}
	gid := str(goal, "id")
	if gid == "" {
		t.Fatalf("agents.goals.create returned a goal with no id: %v", created)
	}
	return gid
}

// goalListByID fetches agents.goals.list and returns the goal JSON object
// with the given id (nil when absent).
func goalListByID(t *testing.T, s *harness.Stack, employeeID, goalID string) map[string]any {
	t.Helper()
	list := rpcResult(t, s, "agents.goals.list", map[string]any{"employee_id": employeeID})
	goals, _ := list["goals"].([]any)
	for _, g := range goals {
		goal, ok := g.(map[string]any)
		if !ok {
			continue
		}
		if str(goal, "id") == goalID {
			return goal
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Read-only sqlite readers
// ---------------------------------------------------------------------------

// openRO opens a sqlite database read-only with WAL + busy timeout so a
// concurrent daemon writer never blocks the test reader.
func openRO(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("ping %s: %v", path, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// PlanGoalRow mirrors one row of the plans table in <state_dir>/plans.db.
type PlanGoalRow struct {
	ID     string
	Title  string
	GoalID string
	State  string
}

// Plans reads every plan row (id, title, goal_id, state) ordered by creation.
func Plans(t *testing.T, plansDB string) []PlanGoalRow {
	t.Helper()
	db := openRO(t, plansDB)
	rows, err := db.Query(`SELECT id, COALESCE(title,''), COALESCE(goal_id,''), COALESCE(state,'') FROM plans ORDER BY created_at`)
	if err != nil {
		t.Fatalf("query plans: %v", err)
	}
	defer rows.Close()
	var out []PlanGoalRow
	for rows.Next() {
		var r PlanGoalRow
		if err := rows.Scan(&r.ID, &r.Title, &r.GoalID, &r.State); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plans: %v", err)
	}
	return out
}

// GoalHealthRow mirrors the health-relevant columns of employee_goals in
// <state_dir>/employees/goals.db.
type GoalHealthRow struct {
	ID     string
	Title  string
	State  string
	Health string
}

// Goals reads every employee_goals row from the stack's goal store. When
// bots are enabled the goal store shares the bots database (the
// employee_goals FOREIGN KEY on bot_definitions), so try the shared bots.db
// first and fall back to the standalone employees/goals.db.
func Goals(t *testing.T, s *harness.Stack) []GoalHealthRow {
	t.Helper()
	goalsDB := filepath.Join(s.MeeptHome, "bots", "bots.db")
	if _, err := os.Stat(goalsDB); err != nil {
		goalsDB = GoalsDBPath(s)
	}
	db := openRO(t, goalsDB)
	rows, err := db.Query(`SELECT id, COALESCE(title,''), COALESCE(state,''), COALESCE(health,'') FROM employee_goals ORDER BY created_at`)
	if err != nil {
		t.Fatalf("query employee_goals: %v", err)
	}
	defer rows.Close()
	var out []GoalHealthRow
	for rows.Next() {
		var r GoalHealthRow
		if err := rows.Scan(&r.ID, &r.Title, &r.State, &r.Health); err != nil {
			t.Fatalf("scan goal row: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate employee_goals: %v", err)
	}
	return out
}

// GoalsDBPath is the employee goal store path (<state_dir>/employees/goals.db).
func GoalsDBPath(s *harness.Stack) string {
	return filepath.Join(s.StateDir, "employees", "goals.db")
}

// PlansDBPath is the plan store path (<state_dir>/plans.db).
func PlansDBPath(s *harness.Stack) string {
	return filepath.Join(s.StateDir, "plans.db")
}

// jsonMarshal is a test-local convenience for predicate checks over request
// bodies.
func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// daemonLog reads the full daemon log (harness boots the daemon with its
// combined stdout/stderr captured there; restarts append).
func daemonLog(s *harness.Stack) string {
	data, err := os.ReadFile(s.Daemon.LogPath())
	if err != nil {
		return ""
	}
	return string(data)
}

// assessReply builds an ASSESS LLM reply with one candidate.
func assessReply(title, description, prompt string) string {
	return fmt.Sprintf(`{"candidates":[{"title":%q,"description":%q,"prompt":%q}]}`, title, description, prompt)
}

// reflectReply builds a REFLECT LLM reply assessing the goal healthy.
func reflectReply() string {
	return `{"health":"healthy","reasoning":"the execution satisfied the mandate"}`
}

// goalStack starts a scratch daemon with the employee goal layer fully
// operational: bots enabled (the goal store's employee_goals table declares
// a FOREIGN KEY on bot_definitions, so it must share the bots database —
// the production "employees layer over bots" wiring) and the goal-completion
// gate kill switch on. Extra overlays merge on top; later keys win.
func goalStack(t *testing.T, extra map[string]any) *harness.Stack {
	t.Helper()
	overlay := map[string]any{
		"bots.enabled":                    true,
		"employees.defaults.gate.enabled": true,
	}
	for k, v := range extra {
		overlay[k] = v
	}
	return harness.Start(t, harness.WithConfigOverlay(overlay))
}
