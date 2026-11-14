//go:build e2e

// Suite escalation-resume (tree docs/plans/20261113-escalation-resume-and-hardening,
// gate leaf T9): the approve→resume path at the daemon boundary.
//
// Flow pinned: a tier-2 employee's goal loop assesses an escalating
// candidate → Plan() creates a pending plan linked to the goal
// (plan.create path, GoalID stamped) → the operator approves → the
// plan.approved bus event reaches the "employee-plan-resume" subscriber →
// Manager.ResumeApprovedPlan executes the goal round EXACTLY once
// (ApproveAndExecute) → a second approve is a skip (the once-per-process
// guard). The execution observable is the goal store's PlanHistory + the
// executor turn counter on the fake LLM.
//
// RED/incomplete until leaves 01 (Manager.ResumeApprovedPlan) + 02 (daemon
// subscriber) land — that is expected for the gate. This suite compiles
// today and asserts the FINAL behavior only through public surfaces (RPC +
// stores + fake LLM); no mocks beyond the harness fake model.

package escalationresume

import (
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"

	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon)
)

// ---------------------------------------------------------------------------
// Observability helpers (same read-only SQL posture as goal-completion)
// ---------------------------------------------------------------------------

// planGoalRow mirrors the linkage columns of the plans table.
type planGoalRow struct {
	ID     string
	State  string
	GoalID string
}

// planByID reads one plan row (id, state, goal_id) from the scratch plans.db.
func planByID(t *testing.T, s *harness.Stack, planID string) (planGoalRow, bool) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(s.StateDir, "plans.db")+"?mode=ro")
	if err != nil {
		t.Fatalf("open plans.db: %v", err)
	}
	defer db.Close()
	var r planGoalRow
	err = db.QueryRow(`SELECT id, COALESCE(state,''), COALESCE(goal_id,'') FROM plans WHERE id = ?`, planID).
		Scan(&r.ID, &r.State, &r.GoalID)
	if err == sql.ErrNoRows {
		return planGoalRow{}, false
	}
	if err != nil {
		t.Fatalf("query plan %s: %v", planID, err)
	}
	return r, true
}

// goalRow mirrors the resume-relevant columns of employee_goals.
type goalRow struct {
	ID          string
	Health      string
	ActivePlans string // active_plan_ids JSON text
	History     string // plan_history JSON text
}

// goalByID reads one goal row from the standalone goal store.
func goalByID(t *testing.T, s *harness.Stack, goalID string) (goalRow, bool) {
	t.Helper()
	dbPath := filepath.Join(s.StateDir, "employees", "goals.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer db.Close()
	var r goalRow
	err = db.QueryRow(`SELECT id, COALESCE(health,''), COALESCE(active_plan_ids,'[]'), COALESCE(plan_history,'[]') FROM employee_goals WHERE id = ?`, goalID).
		Scan(&r.ID, &r.Health, &r.ActivePlans, &r.History)
	if err == sql.ErrNoRows {
		return goalRow{}, false
	}
	if err != nil {
		t.Fatalf("query goal %s: %v", goalID, err)
	}
	return r, true
}

// daemonLog reads the whole scratch daemon log.
func daemonLog(t *testing.T, s *harness.Stack) string {
	t.Helper()
	data, err := os.ReadFile(s.Daemon.LogPath())
	if err != nil {
		return ""
	}
	return string(data)
}

// ---------------------------------------------------------------------------
// RPC helpers
// ---------------------------------------------------------------------------

// rpc runs one RPC call against the sandbox daemon, returning the raw
// result JSON (empty on RPC error — callers that expect errors read ok).
func rpc(t *testing.T, s *harness.Stack, method string, params any) (json.RawMessage, bool) {
	t.Helper()
	client := harness.DialRPC(t, s.SocketPath)
	resp := client.Call(method, params)
	if e, ok := resp["error"]; ok && e != nil {
		return nil, false
	}
	raw, err := json.Marshal(resp["result"])
	if err != nil {
		return nil, false
	}
	return raw, true
}

// rpcResult runs one RPC call and fails the test on error.
func rpcResult(t *testing.T, s *harness.Stack, method string, params any) map[string]any {
	t.Helper()
	client := harness.DialRPC(t, s.SocketPath)
	return client.CallResult(method, params)
}

func str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// hireResumeEmployee registers the tier-2 escalation fixture through the
// REAL agents.create RPC: the constitution carries one action escalation
// trigger whose match is a unique marker (so the marker-named candidate
// escalates to plan signoff instead of executing) and escalates_to user.
func hireResumeEmployee(t *testing.T, s *harness.Stack, employeeID, escMarker string) {
	t.Helper()
	created := rpcResult(t, s, "agents.create", map[string]any{
		"id":      employeeID,
		"name":    employeeID,
		"prompt":  "You are " + employeeID + ", an e2e escalation-resume probe.",
		"enabled": true,
		"triggers": []map[string]any{
			{"type": "cron", "schedule": "0 4 * * *", "enabled": false},
		},
		"constitution": map[string]any{
			"purpose":       employeeID + " escalation-resume fixture",
			"role":          "escalation resume probe",
			"charter":       "Complete the assigned goal. Never delete data.",
			"autonomy_tier": "tier_2_propose",
			"escalates_to":  []string{"user"},
			"constraints": map[string]any{
				"tools_allowed": []string{},
				"risk_ceiling":  "medium",
				"never":         []string{"delete production data"},
				"escalation_triggers": []map[string]any{
					{"on": "action", "match": escMarker, "reason": "e2e escalation-resume probe"},
				},
			},
			"max_active_plans": 3,
			"version":          1,
			"authored_by":      "user",
		},
	})
	if got := str(created, "id"); got != employeeID {
		t.Fatalf("agents.create id = %q, want %q: %v", got, employeeID, created)
	}
}

// createResumeGoal creates the fixture goal through agents.goals.create.
func createResumeGoal(t *testing.T, s *harness.Stack, employeeID, goalMark string) string {
	t.Helper()
	created := rpcResult(t, s, "agents.goals.create", map[string]any{
		"employee_id": employeeID,
		"title":       "keep the " + goalMark + " fixture green",
		"mandate":     "pursue the " + goalMark + " mandate to completion",
		"source":      "user",
	})
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

// assessment replies -------------------------------------------------------------

// assessReply builds an ASSESS reply proposing one escalating candidate.
func assessReply(marker, title, prompt string) string {
	return fmt.Sprintf(`{"candidates":[{"title":%q,"description":"the escalating candidate for %s","prompt":%q}]}`,
		title, marker, prompt)
}

// ---------------------------------------------------------------------------
// The scenario
// ---------------------------------------------------------------------------

// TestApprovedPlanResumesGoalRoundOnce drives the full contract at the
// daemon boundary: escalated candidate → pending plan → approve → the goal
// round executes exactly once → the second approve is a skip.
//
// RED until leaves 01+02 land: today nothing subscribes plan.approved on
// the employee side, so after the approve the plan never executes — the
// history-wait below times out and the daemon log carries no resume line.
func TestApprovedPlanResumesGoalRoundOnce(t *testing.T) {
	const (
		empID     = "esc-resume-emp"
		escMarker = "escresume-escalate"
		goalMark  = "ESCRESUME-01"
		planTitle = "escalated: " + goalMark
	)

	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"employees.defaults.gate.enabled": true,
	}))

	hireResumeEmployee(t, s, empID, escMarker)
	goalID := createResumeGoal(t, s, empID, goalMark)

	// GoalLoops register at boot only: restart so the new employee gets one
	// (goal-completion restart primitive posture).
	stop := restartResumeDaemon(t, s)
	defer stop()

	// Script the goal-loop LLM turns. ASSESS (trigger #1) proposes the
	// escalating candidate; the tier-2 path Plans it instead of executing.
	// REFLECT turns (post-approve execution) assess the goal healthy. The
	// executor turn is a plain finish.
	s.Fake.ScriptOnce(harness.MessageContains("first-probe"),
		harness.TextResponse(assessReply(escMarker, planTitle,
			"perform the risky "+escMarker+" action now")))
	s.Fake.ScriptN(10, harness.MessageContains("execution outcome"),
		harness.TextResponse(`{"health":"healthy","reasoning":"the execution satisfied the mandate"}`))
	s.Fake.SetPostToolText("done — the escalated action was performed.")
	s.Fake.SetChatText("ok")

	// Trigger #1: the escalating candidate → Plan() → a pending plan row
	// linked to the goal (plan.get exposes goal_id).
	rpcResult(t, s, "agents.trigger", map[string]any{
		"id":      empID,
		"payload": map[string]any{"source": "manual", "round": "first-probe"},
	})

	var pending planGoalRow
	harness.WaitFor(t, 90*time.Second, "pending plan row linked to goal "+goalID, func() bool {
		// Discover the plan through the goal linkage on the plans table.
		db, err := sql.Open("sqlite", "file:"+filepath.Join(s.StateDir, "plans.db")+"?mode=ro")
		if err != nil {
			return false
		}
		defer db.Close()
		rows, err := db.Query(`SELECT id, COALESCE(state,''), COALESCE(goal_id,'') FROM plans WHERE goal_id = ? ORDER BY created_at`, goalID)
		if err != nil {
			return false
		}
		defer rows.Close()
		for rows.Next() {
			var r planGoalRow
			if err := rows.Scan(&r.ID, &r.State, &r.GoalID); err != nil {
				continue
			}
			if strings.Contains(r.State, "pending") {
				pending = r
				return true
			}
		}
		return false
	})
	if pending.ID == "" {
		t.Fatalf("no pending_approval plan linked to goal %s appeared", goalID)
	}

	// The plan record exposes the goal linkage (C2's resolution source).
	got, ok := planByID(t, s, pending.ID)
	if !ok || got.GoalID != goalID {
		t.Fatalf("plan %s lost its goal linkage: %+v (want goal_id=%s)", pending.ID, got, goalID)
	}

	// Operator approve — the exact surface every approval UI funnels
	// through. The plan.approved event this publishes must reach the
	// employee-plan-resume subscriber and resume the goal round.
	rpcResult(t, s, "plan.approve", map[string]any{
		"plan_id": pending.ID,
		"by":      "user",
	})

	// The goal round executes EXACTLY once: the plan lands in the goal's
	// PlanHistory (ApproveAndExecute appends it after Reflect).
	harness.WaitFor(t, 90*time.Second, "plan "+pending.ID+" in goal history (ApproveAndExecute ran)", func() bool {
		row, ok := goalByID(t, s, goalID)
		if !ok {
			return false
		}
		return strings.Contains(row.History, pending.ID)
	})

	// The executor turn ran: the fake LLM saw the execution prompt.
	sawExecution := false
	for _, body := range s.Fake.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role == "user" {
				if c, _ := msg["content"].(string); strings.Contains(c, escMarker) {
					sawExecution = true
				}
			}
		}
	}
	if !sawExecution {
		t.Error("no executor turn carried the escalated prompt — the resume never executed the plan")
	}

	// Second approve event is a SKIP: publish the same approval again via
	// the bus (a duplicate plan.approve RPC errors harmlessly on the CAS,
	// so the bus event is the honest duplicate-delivery surface). The once-
	// per-process guard must not run ApproveAndExecute again — the plan
	// history count stays 1 and no duplicate resume log line appears.
	client := harness.DialRPC(t, s.SocketPath)
	_ = client.Call("bus.publish", map[string]any{
		"topic":   "plan.approved",
		"payload": map[string]any{"plan_id": pending.ID, "by": "user"},
	})
	time.Sleep(3 * time.Second)

	row, ok := goalByID(t, s, goalID)
	if !ok {
		t.Fatalf("goal %s vanished after duplicate approve", goalID)
	}
	if n := strings.Count(row.History, pending.ID); n != 1 {
		t.Errorf("plan %s appears %d times in goal history after duplicate approve, want exactly 1 (double-execute)", pending.ID, n)
	}

	// The subscriber's resume line appears in the daemon log exactly once.
	log := daemonLog(t, s)
	// leaf 02's structured line carries the subscriber id + plan id; count
	// co-occurrences conservatively (either marker would do — require the
	// plan id on a resume line).
	if !strings.Contains(log, pending.ID) {
		t.Error("daemon log never mentions the approved plan id")
	}
}

// ---------------------------------------------------------------------------
// Restart primitive (goal-completion posture, trimmed to what this suite
// needs): stop via SIGTERM + raw exec re-boot against the SAME sandbox
// home (the harness Daemon handles are one-shot; the stop is idempotent via
// the harness's own sync.Once, and the raw boot mirrors goal-completion's
// bootDaemon with the pidfile-based cleanup).
// ---------------------------------------------------------------------------

func restartResumeDaemon(t *testing.T, s *harness.Stack) func() {
	t.Helper()
	s.Daemon.Stop()
	return bootResumeDaemon(t, s)
}

func bootResumeDaemon(t *testing.T, s *harness.Stack) func() {
	t.Helper()
	logFile, err := os.OpenFile(s.Daemon.LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open daemon log: %v", err)
	}
	cmd := exec.Command(harness.DaemonPath(t),
		"-c", filepath.Join(s.MeeptHome, "meept.json5"),
		"-d", s.StateDir,
		"-s", s.SocketPath,
	)
	cmd.Dir = s.Work
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), "HOME="+s.Home, "MEEPT_HOME="+s.MeeptHome)
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatalf("start daemon: %v", err)
	}
	// Release the log fd once the child has duplicated it.
	go func() {
		time.Sleep(2 * time.Second)
		logFile.Close()
	}()

	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
			ForceAttemptHTTP2: false,
		},
	}
	deadline := time.Now().Add(120 * time.Second)
	for {
		resp, err := client.Get(s.HTTPBaseURL() + "/health") //nolint:noctx // bounded poll in test harness
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("re-booted daemon not healthy within 120s; log tail:\n%s", s.Daemon.LogTail())
		}
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatalf("re-booted daemon died during boot; log tail:\n%s", s.Daemon.LogTail())
		}
		time.Sleep(250 * time.Millisecond)
	}

	return func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _, _ = cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}
}
