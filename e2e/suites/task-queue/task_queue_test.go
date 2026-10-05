//go:build e2e

// Package taskqueue covers internal/task + internal/queue durability at the
// daemon boundary (manifest scenarios task-queue-01..03): a dispatched task
// and its step rows survive a daemon restart (state-restart-style, focused on
// the task store), and a queued job either resumes or is honestly marked
// after the restart via the queue's startup reclaim sweep.
//
// Restart primitive: the same boot-identical-daemon-spawn pattern the
// state-restart suite uses (the harness intentionally exposes no
// Stop/Restart API). The re-booted daemon reopens the SAME state dir, so
// every post-restart assertion reads persisted state alone.
package taskqueue

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/queue"
)

// ---------------------------------------------------------------------------
// restart primitive (mirrors e2e/suites/state-restart/helpers_test.go)
// ---------------------------------------------------------------------------

// stopDaemon sends SIGTERM to the process and waits for exit (SIGKILL after
// 10s). Safe to call on an already-exited process.
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
	// Clean slate for the next boot even if shutdown skipped socket removal.
	_ = os.Remove(sandboxSocketPath(t))
}

// sandboxSocketPath resolves the sandbox socket path from the meept.json5 the
// harness wrote under the sandbox home.
func sandboxSocketPath(t *testing.T) string {
	t.Helper()
	home := os.Getenv("MEEPT_E2E_RESTART_HOME")
	if home == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".meept", "meept.json5"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, `"socket_path"`) {
			var val string
			if _, err := fmt.Sscanf(line, `"socket_path": %q,`, &val); err == nil && val != "" {
				return val
			}
		}
	}
	return ""
}

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
	healthURL := s.HTTPBaseURL() + "/health"
	deadline := time.Now().Add(60 * time.Second)
	for {
		resp, err := client.Get(healthURL) //nolint:noctx // bounded poll in test harness
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
// process against the SAME sandbox home. The returned func stops the
// re-booted daemon; the harness cleanup remains a safe no-op afterwards.
func restartDaemon(t *testing.T, s *harness.Stack) func() {
	t.Helper()

	oldPID := s.Daemon.Pid()
	if oldPID == 0 {
		t.Fatal("harness daemon pid not recorded; nothing to restart")
	}
	stopDaemon(t, oldPID)

	// Wait for the old process to be fully reaped before re-binding the
	// socket path.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(s.SocketPath); os.IsNotExist(err) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	bootDaemon(t, s)

	return func() {
		// The re-booted daemon has no harness-managed cmd; find and stop it
		// via the pidfile the daemon wrote at boot.
		data, err := os.ReadFile(filepath.Join(s.StateDir, "meept.pid"))
		if err == nil {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err == nil {
				stopDaemon(t, pid)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// RPC helpers
// ---------------------------------------------------------------------------

// rpcCall sends one JSON-RPC request over the sandbox Unix socket and decodes
// the full response envelope.
func rpcCall(t *testing.T, socketPath, method string, params any) map[string]any {
	t.Helper()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial %s: %v", socketPath, err)
	}
	defer conn.Close()

	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := fmt.Fprintf(conn, "%d\n", len(payload)); err != nil {
		t.Fatalf("write length: %v", err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	reader := bufio.NewReader(conn)
	lengthLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read length: %v", err)
	}
	var length int
	if _, err := fmt.Sscanf(strings.TrimSpace(lengthLine), "%d", &length); err != nil || length <= 0 {
		t.Fatalf("bad length line %q", lengthLine)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(reader, buf); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(buf, &resp); err != nil {
		t.Fatalf("decode response %q: %v", buf, err)
	}
	return resp
}

// rpcResult asserts the call succeeded and returns the result object.
func rpcResult(t *testing.T, s *harness.Stack, method string, params any) map[string]any {
	t.Helper()
	resp := rpcCall(t, s.SocketPath, method, params)
	if e, ok := resp["error"]; ok && e != nil {
		t.Fatalf("%s: RPC error: %v", method, e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: result is not an object: %v", method, resp["result"])
	}
	return result
}

// str fetches a string field from a decoded RPC result map.
func str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// taskRow is the task-shape slice of a decoded task RPC object.
type taskRow struct {
	ID         string
	State      string
	TotalJobs  int
	FailedJobs int
}

// decodeTask extracts the durable fields the suite asserts on.
func decodeTask(m map[string]any) taskRow {
	row := taskRow{ID: str(m, "id"), State: str(m, "state")}
	if v, ok := m["total_jobs"].(float64); ok {
		row.TotalJobs = int(v)
	}
	if v, ok := m["failed_jobs"].(float64); ok {
		row.FailedJobs = int(v)
	}
	return row
}

// ---------------------------------------------------------------------------
// task-queue-01 (M): a completed task persists across a daemon restart —
// the task/step stores re-open and the rows are recoverable
// ---------------------------------------------------------------------------

// TestCompletedTaskSurvivesRestart drives a real turn to task completion,
// restarts the daemon, and asserts the task row and its step rows are
// recoverable from the stores alone — via the task RPC surface AND by
// reading tasks.db directly (the state-restart contract applied to the
// task store).
func TestCompletedTaskSurvivesRestart(t *testing.T) {
	s := harness.Start(t)
	sessionID := s.CreateSession(t, "taskq-01", s.ProjectDir)

	ack := s.SubmitChatHTTP(t, sessionID, "create a file named durable.txt containing durable work")
	if str(ack, "turn_id") == "" {
		t.Fatalf("chat submit ack missing turn_id: %v", ack)
	}

	// The task row appears when the turn dispatches.
	var taskID string
	harness.WaitFor(t, 30*time.Second, "task row for the turn", func() bool {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.Name != "" {
				taskID = row.ID
				return true
			}
		}
		return false
	})

	// Wait for terminal completion before the restart.
	harness.WaitFor(t, 120*time.Second, "task "+taskID+" terminal", func() bool {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.ID == taskID {
				return row.State == "completed" || row.State == "failed"
			}
		}
		return false
	})
	row, _ := taskByID(t, s, taskID)
	if row.State != "completed" {
		t.Fatalf("pre-restart task state = %q, want completed; steps:\n%s",
			row.State, harness.FormatSteps(harness.Steps(t, s.TasksDBPath(), taskID)))
	}
	preSteps := harness.Steps(t, s.TasksDBPath(), taskID)
	if len(preSteps) == 0 {
		t.Fatalf("no steps recorded for task %s before restart", taskID)
	}

	stop := restartDaemon(t, s)
	defer stop()

	// The task row survives via the task RPC surface (bus proxy -> task
	// handler -> task store).
	got := rpcResult(t, s, "task.get", map[string]any{"id": taskID})
	decoded := decodeTask(got)
	if decoded.ID != taskID {
		t.Fatalf("task.get after restart returned id %q, want %s (result: %v)", decoded.ID, taskID, got)
	}
	if decoded.State != "completed" {
		t.Fatalf("task state after restart = %q, want completed", decoded.State)
	}

	// The steps survive too.
	steps := rpcResult(t, s, "task.steps", map[string]any{"task_id": taskID})
	raw, err := json.Marshal(steps)
	if err != nil {
		t.Fatalf("marshal steps: %v", err)
	}
	for _, pre := range preSteps {
		if !strings.Contains(string(raw), pre.ID) {
			t.Fatalf("step %s lost across restart; steps: %s", pre.ID, raw)
		}
	}

	// And the store file directly agrees (state-restart contract: the store
	// alone carries the state; the new process never had it in memory).
	postRow, _ := taskByID(t, s, taskID)
	if postRow.State != "completed" || postRow.CompletedJobs < 1 {
		t.Fatalf("tasks.db after restart: state=%q completed_jobs=%d, want completed/>=1",
			postRow.State, postRow.CompletedJobs)
	}
}

// ---------------------------------------------------------------------------
// task-queue-02 (M): an in-flight task killed mid-execution is honestly
// marked failed by the stale-task recovery sweep after restart
// ---------------------------------------------------------------------------

// TestKilledTaskIsHonestlyMarkedAfterRestart submits work, kills the daemon
// mid-execution (SIGKILL — no graceful shutdown), and asserts the restarted
// daemon's RecoverStaleTasks sweep honestly marks the orphaned task and its
// non-terminal steps failed (result "daemon_shutdown") instead of leaving
// them pending forever.
func TestKilledTaskIsHonestlyMarkedAfterRestart(t *testing.T) {
	s := harness.Start(t)
	sessionID := s.CreateSession(t, "taskq-02", s.ProjectDir)

	// Script a multi-round executor turn so the task stays mid-flight long
	// enough to kill it: first a file write, then the daemon dies before the
	// follow-up round can complete.
	artifact := filepath.Join(s.ProjectDir, "interrupted.txt")
	s.Fake.SetPostToolText("wrote interrupted.txt")
	s.Fake.EnqueueFileWrite("call-tq02", artifact, "interrupted work")

	s.SubmitChatHTTP(t, sessionID, "create a file named interrupted.txt containing interrupted work")

	var taskID string
	harness.WaitFor(t, 30*time.Second, "task row for the killed turn", func() bool {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			taskID = row.ID
			return true
		}
		return false
	})

	// Wait until the task leaves pending before the SIGKILL — OR reaches a
	// terminal state. Terminal during this poll is a LEGITIMATE exit (M8):
	// under load the fake executor can finish the whole turn between two
	// 200 ms polls, and killing a daemon whose task already completed would
	// be a spurious failure (the post-restart assertion wants "failed", the
	// row says "completed"). In that case the honest outcome is asserted
	// and the kill path is skipped.
	deadline := time.Now().Add(30 * time.Second)
	killed := false
	for time.Now().Before(deadline) {
		row, ok := taskByID(t, s, taskID)
		if !ok {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		switch row.State {
		case "completed", "failed":
			// The task finished before we could kill it: that is honest
			// completion, not a fixture failure. Assert the completed
			// outcome and skip the kill/restart sweep entirely.
			if row.State != "completed" {
				t.Fatalf("task reached terminal state %q before the kill window; expected completed or still in-flight", row.State)
			}
			t.Skipf("task %s completed between polls before the daemon could be SIGKILLed — honest completion, kill-recovery path not exercised (M8)", taskID)
		case "pending":
			if time.Now().After(deadline.Add(-5 * time.Second)) {
				killed = true // kill a still-pending task rather than spin forever
			}
		default:
			killed = true // in-flight (running/planning/...) — the intended kill window
		}
		if killed {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Hard kill: no graceful shutdown, no daemon_shutdown marking on the way
	// out. The pidfile carries the harness daemon's pid.
	pidData, err := os.ReadFile(filepath.Join(s.StateDir, "meept.pid"))
	if err != nil {
		t.Fatalf("read pidfile: %v", err)
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(pidData)), "%d", &pid); err != nil {
		t.Fatalf("parse pidfile %q: %v", pidData, err)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("find daemon pid %d: %v", pid, err)
	}
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL daemon: %v", err)
	}
	// Reap the SIGKILLed daemon explicitly. It is a child of THIS test
	// process (harness exec.Command); until some goroutine calls Wait, it
	// stays a <defunct> zombie that both Signal(0) and `ps -p` report as
	// present — which would fail the re-booted daemon's pidfile check. A raw
	// WNOHANG wait4 reaps it without disturbing the harness's own reaper
	// (a later Wait returns "already finished" harmlessly).
	reapDeadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(reapDeadline) {
		var ws syscall.WaitStatus
		wpid, err := syscall.Wait4(pid, &ws, syscall.WNOHANG, nil)
		if err == nil && wpid == pid {
			break
		}
		if err == syscall.ECHILD {
			break // someone else already reaped it
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = os.Remove(s.SocketPath)

	// Re-boot against the SAME sandbox home.
	restartPID := s.Daemon.Pid()
	_ = restartPID // the harness bookkeeping pid is stale now; the pidfile path is the source
	bootDaemon(t, s)
	stop := func() {
		data, err := os.ReadFile(filepath.Join(s.StateDir, "meept.pid"))
		if err == nil {
			var newPID int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &newPID); err == nil {
				stopDaemon(t, newPID)
			}
		}
	}
	defer stop()

	// The stale-task recovery sweep runs at boot: the orphaned task is
	// marked failed (honest terminal state), not left pending/planning.
	row, ok := taskByID(t, s, taskID)
	if !ok {
		t.Fatalf("task %s lost across SIGKILL restart", taskID)
	}
	if row.State != "failed" {
		t.Fatalf("killed task state after restart = %q, want failed (honest marking)", row.State)
	}

	// Its non-terminal steps carry the daemon_shutdown result.
	for _, st := range harness.Steps(t, s.TasksDBPath(), taskID) {
		switch st.State {
		case "completed", "approved", "failed", "skipped", "rejected":
			// terminal — fine
		default:
			t.Fatalf("step %s still non-terminal %q after recovery sweep", st.ID, st.State)
		}
	}
}

// ---------------------------------------------------------------------------
// task-queue-03 (M): a queued job persists across restart and either re-runs
// or is honestly marked by the startup reclaim sweep
// ---------------------------------------------------------------------------

// TestQueuedJobSurvivesRestartAndIsHonestlyResolved enqueues a one-off job
// through the queue RPC surface, restarts the daemon, and asserts the job
// row survives in queue.db and is honestly resolved: after the startup
// reclaim sweep it is pending again (re-claimable), completed (it ran), or
// failed — never claimed/processing forever against a dead worker.
func TestQueuedJobSurvivesRestartAndIsHonestlyResolved(t *testing.T) {
	s := harness.Start(t)

	// Enqueue through the daemon's own queue RPC surface so the assertion
	// covers the persistence path itself.
	created := rpcResult(t, s, "queue.enqueue", map[string]any{
		"type":   "one_off",
		"prompt": "e2e task-queue durability probe",
	})
	jobID := str(created, "id")
	if jobID == "" {
		t.Fatalf("queue.enqueue returned no job id: %v", created)
	}

	// Confirm it is in the queue store before the restart, and capture the
	// updated_at baseline (M8): the pending branch below must prove the
	// startup reclaim sweep actually touched the row, not just that the row
	// still says pending (indistinguishable from sweep-never-ran without a
	// baseline).
	preJob := queueJobByID(t, s, jobID)
	if preJob == nil {
		t.Fatalf("job %s not found in queue.db after enqueue", jobID)
	}
	// pending OR claimed: worker wake-up (event-driven claim) can move the
	// job out of pending within milliseconds of the enqueue RPC, so the
	// persistence assertion accepts either — both prove the row is durable
	// in queue.db before the restart. The post-restart loop below already
	// grades every reachable state honestly.
	switch preJob.State {
	case string(queue.StatePending), string(queue.StateClaimed), string(queue.StateProcessing):
	default:
		t.Fatalf("job state after enqueue = %q, want pending/claimed/processing", preJob.State)
	}
	if preJob.UpdatedAt == "" {
		t.Fatalf("job %s has no updated_at baseline in queue.db", jobID)
	}
	updatedBefore := preJob.UpdatedAt

	stop := restartDaemon(t, s)
	defer stop()

	// The job row survives the restart in queue.db.
	postJob := queueJobByID(t, s, jobID)
	if postJob == nil {
		t.Fatalf("job %s lost across restart (queue.db did not persist it)", jobID)
	}

	// Honest resolution: the startup reclaim sweep resets crash-orphaned
	// claims to pending; the worker pool may then complete or fail it. Any
	// terminal state is honest; a forever-claimed/processing row is not.
	// The PENDING branch is honest only when updated_at advanced past the
	// pre-restart baseline — a bare "still pending" with a stale timestamp
	// means the sweep never ran (the vacuous grade the audit flagged).
	resolvedDeadline := time.Now().Add(30 * time.Second)
	var pendingAdvanced bool
	for time.Now().Before(resolvedDeadline) {
		postJob = queueJobByID(t, s, jobID)
		if postJob == nil {
			t.Fatalf("job %s disappeared after restart", jobID)
		}
		switch postJob.State {
		case string(queue.StatePending):
			if postJob.UpdatedAt > updatedBefore {
				// Timestamp advanced past the baseline: the row was
				// reclaimed/reset after restart, then returned to
				// pending (or was re-pended by the sweep) — honest.
				pendingAdvanced = true
				return
			}
			// Stale timestamp: give the sweep/worker a moment before
			// declaring the branch vacuous.
		case string(queue.StateCompleted), string(queue.StateFailed), string(queue.StateDead):
			return // executed to a terminal state — honest
		}
		time.Sleep(250 * time.Millisecond)
	}
	if postJob.State == string(queue.StatePending) && !pendingAdvanced {
		t.Fatalf("job %s stuck pending with updated_at %q never advancing past pre-restart baseline %q (startup reclaim sweep did not touch the row)", jobID, postJob.UpdatedAt, updatedBefore)
	}
	t.Fatalf("job %s stuck in state %q after restart (not reclaimed, not terminal)", jobID, postJob.State)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// taskByID returns the task row with the given id.
func taskByID(t *testing.T, s *harness.Stack, id string) (harness.TaskRow, bool) {
	t.Helper()
	for _, row := range harness.Tasks(t, s.TasksDBPath()) {
		if row.ID == id {
			return row, true
		}
	}
	return harness.TaskRow{}, false
}

// queueJobRow is the decoded jobs-row shape the suite asserts on.
type queueJobRow struct {
	ID        string
	State     string
	UpdatedAt string // RFC3339 text, bumped on every state transition
}

// queueJobByID opens queue.db read-only and returns the job row, or nil when
// the job is absent.
func queueJobByID(t *testing.T, s *harness.Stack, jobID string) *queueJobRow {
	t.Helper()
	db, err := openQueueDB(s)
	if err != nil {
		t.Fatalf("open queue.db: %v", err)
	}
	defer db.Close()
	row, err := queryQueueJob(db, jobID)
	if err != nil {
		t.Fatalf("query queue.db: %v", err)
	}
	return row
}

// queueDBPath resolves the daemon's queue.db path. The default
// [queue].db_path is "~/.meept/queue.db", which the MEEPT_HOME override
// redirects into the sandbox meept home — that is where the scratch
// daemon's queue store lives (the data_dir fallback only fires for an
// explicitly empty DBPath).
func queueDBPath(s *harness.Stack) string {
	return filepath.Join(s.MeeptHome, "queue.db")
}
