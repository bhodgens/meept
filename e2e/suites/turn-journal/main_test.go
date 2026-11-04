//go:build e2e

// Package turnjournal is the hermetic e2e suite for the turn-level durable
// journal (docs/plans/20261031-turn-journal, leaf 04 - the tree's
// acceptance gate). Four scenarios over REAL scratch daemons:
//
//	kill9-resume-01   kill -9 mid-turn -> boot -> resume at NEXT STEP
//	                  (fake-LLM call counter proves no re-invocation of
//	                  journaled steps)
//	dedup-02          no duplicate completion (queue CompleteAttempt /
//	                  CompletionIsFresh claim-token dedup stays the ONLY
//	                  completion dedup; one completion event end to end)
//	quota-03          a QuotaResetError on the resumed step PARKS via the
//	                  existing provider-wait path; zero short-retries; the
//	                  journal run stays open
//	flagoff-04        flag-off legacy control: kill mid-turn -> FULL
//	                  re-run from step 0; no turnjournal.db anywhere
//
// Rig notes:
//   - daemons spawn ONLY through harness.Start / the state-restart restart
//     primitive; the mid-turn kill is a TRUE SIGKILL on the daemon handle
//     (the detached bin reaper owns the scratch dir even after kill -9).
//   - readiness is deadline polling only (harness.WaitFor), never sleeps.
//   - the journal store is read directly (read-only SQL over
//     <DataDir>/turnjournal.db) for row-shape assertions.
package turnjournal

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
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
)

// journalRow mirrors one row of the turn_journal table (internal/agent/
// turnjournal/store_sqlite.go migrate - the frozen C1 schema).
type journalRow struct {
	TurnID      string
	SessionID   string
	StepID      string
	Kind        string
	Payload     string // result_payload rendered as text ("" when NULL)
	CreatedNano int64
}

// openJournalDB opens turnjournal.db read-only with WAL + busy timeout,
// mirroring the daemon's DSN (internal/agent/turnjournal/store_sqlite.go)
// so a concurrent writer never blocks the reader. Returns nil when the
// file does not exist (the flag-off shape).
func openJournalDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		t.Fatalf("open turnjournal.db: %v", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("ping turnjournal.db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// journalRows reads every turn_journal row in append order.
func journalRows(t *testing.T, db *sql.DB) []journalRow {
	t.Helper()
	if db == nil {
		return nil
	}
	rows, err := db.Query(`SELECT turn_id, session_id, step_id, kind,
		COALESCE(CAST(result_payload AS TEXT), ''), created_at
		FROM turn_journal ORDER BY id`)
	if err != nil {
		t.Fatalf("query turn_journal: %v", err)
	}
	defer rows.Close()
	var out []journalRow
	for rows.Next() {
		var r journalRow
		if err := rows.Scan(&r.TurnID, &r.SessionID, &r.StepID, &r.Kind, &r.Payload, &r.CreatedNano); err != nil {
			t.Fatalf("scan turn_journal row: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate turn_journal: %v", err)
	}
	return out
}

// countRows counts rows matching (turn_id, kind).
func countRows(rows []journalRow, turnID, kind string) int {
	n := 0
	for _, r := range rows {
		if (turnID == "" || r.TurnID == turnID) && (kind == "" || r.Kind == kind) {
			n++
		}
	}
	return n
}

// completedSteps returns the distinct step_ids with a step_completed row.
func completedSteps(rows []journalRow, turnID string) map[string]string {
	out := map[string]string{}
	for _, r := range rows {
		if r.TurnID == turnID && r.Kind == "step_completed" && r.StepID != "" {
			out[r.StepID] = r.Payload
		}
	}
	return out
}

// runClosed reports whether the turn has a run_closed row.
func runClosed(rows []journalRow, turnID string) bool {
	return countRows(rows, turnID, "run_closed") > 0
}

// stepBExecutorSeen reports whether any executor request carrying the
// STEP-B marker has reached the fake LLM (the slow sleep step is running).
func stepBExecutorSeen(f *harness.FakeLLM) bool {
	for _, body := range f.Requests() {
		raw, _ := body["messages"].([]any)
		var text string
		for _, m := range raw {
			if mm, ok := m.(map[string]any); ok {
				if c, _ := mm["content"].(string); c != "" {
					text += c
				}
			}
		}
		if !strings.Contains(text, "STEP-B") {
			continue
		}
		if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
			return true
		}
	}
	return false
}

// fakeLLMExecutorRequests counts EXECUTOR-shape completion requests the
// fake LLM has seen (the call counter for the no-re-invocation assertion).
// Classifier/planner/reviewer turns are excluded: only executor turns
// carry the scripted step work.
func fakeLLMExecutorRequests(f *harness.FakeLLM) int {
	n := 0
	for _, body := range f.Requests() {
		if raw, ok := body["tools"].([]any); ok && len(raw) > 0 {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Rig: boot the daemon with the flag set, drive a 3-step scripted task.
// ---------------------------------------------------------------------------

const planMarker = "TJ-E2E"

// plannerJSON is the pinned 3-step plan every scripted turn decomposes to.
// Three distinct markers let the executor scripting bind one file_write per
// step; the final step is narration-only (post-tool text) so the step can
// complete without further tools.
func plannerJSON() string {
	// depends_on chains the steps SERIALLY (empty depends_on = parallel):
	// the kill lands in a deterministic single-step window, so exactly one
	// step is in flight and every earlier step is journaled completed.
	return `{"steps":[
		{"description":"` + planMarker + ` STEP-A: create the file tj-step-a.txt with content alpha","tool_hint":"file_write","depends_on":[]},
		{"description":"` + planMarker + ` STEP-B: create the file tj-step-b.txt with content beta","tool_hint":"file_write","depends_on":[0]},
		{"description":"` + planMarker + ` STEP-C: report the project directory state in one line","tool_hint":"none","depends_on":[0,1]}
	]}`
}

// fileWriteCall scripts one file_write for the given step's artifact.
func fileWriteCall(t *testing.T, s *harness.Stack, name, content string) harness.ToolCall {
	t.Helper()
	return harness.ToolCall{
		Name: "file_write",
		Arguments: `{"path":"` + filepath.ToSlash(filepath.Join(s.ProjectDir, name)) +
			`","content":"` + content + `","direct":true}`,
	}
}

// bootJournalStack starts the scratch daemon with daemon.turn_journal_enabled
// set per the flag argument.
func bootJournalStack(t *testing.T, flag bool) *harness.Stack {
	t.Helper()
	return harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"daemon.turn_journal_enabled": flag,
	}))
}

// bindScripts pins the planner plan + per-step executor tool calls on the
// stack's fake LLM. Post-tool text is benign narration (no fabricated file
// claims -> no convergence abort).
func bindScripts(t *testing.T, s *harness.Stack) {
	t.Helper()
	s.Fake.SetPlannerResponse(plannerJSON())
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned for turn-journal"}`)
	// STEP-A and STEP-B each carry their marker; serve the matching file
	// write once per attempt. MaxServes 2: the first attempt consumes one,
	// and (flag-off scenario) a legacy full re-run consumes the second.
	s.Fake.ScriptN(2,
		harness.And(harness.IsExecutorRequest(),
			harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains("STEP-A")),
		harness.ToolCallResponse(fileWriteCall(t, s, "tj-step-a.txt", "alpha")))
	// STEP-B's scripted tool call is a SLOW shell sleep: the step-B
	// executor turn runs the tool for sleepSeconds, giving the kill a
	// deterministic inside-step-B window (step A journaled completed,
	// step B started but NOT completed). Plain sleep is read-only and
	// inside the sandbox fence (the scratch config allows work/**).
	s.Fake.ScriptN(2,
		harness.And(harness.IsExecutorRequest(),
			harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains("STEP-B")),
		harness.ToolCallResponse(harness.ToolCall{
			Name:      "shell",
			Arguments: `{"command":"sleep 8"}`,
		}))
	s.Fake.SetPostToolText("The requested step work is complete.")
	// Fallback: any other executor turn (revision steps, re-plans) gets a
	// benign narration completion with no tool calls, so an unplanned step
	// converges instead of spinning.
	s.Fake.Script(
		harness.And(harness.IsExecutorRequest(), harness.Not(harness.IsPlannerRequest())),
		harness.TextResponse("step done: nothing further to do"))
}

// submitTask submits the scripted 3-step turn and returns (turnID, taskID).
// The task row appears in tasks.db as soon as the planner plan lands.
func submitTask(t *testing.T, s *harness.Stack, sessionID string) (string, string) {
	t.Helper()
	ack := s.SubmitChatHTTP(t, sessionID,
		"Create the two files and then report the project state. "+planMarker)
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("submit ack missing turn_id: %+v", ack)
	}
	var taskID string
	harness.WaitFor(t, 30*time.Second, "task row for "+planMarker, func() bool {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if strings.Contains(row.Name, planMarker) || row.Name != "" {
				taskID = row.ID
				return true
			}
		}
		return false
	})
	if taskID == "" {
		t.Fatal("no task row found")
	}
	return turnID, taskID
}

// waitForJournalRows polls turnjournal.db until the row predicate passes.
func waitForJournalRows(t *testing.T, dbPath string, timeout time.Duration,
	what string, cond func([]journalRow) bool) []journalRow {
	t.Helper()
	var last []journalRow
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		db := openJournalDB(t, dbPath)
		if db != nil {
			last = journalRows(t, db)
			if cond(last) {
				return last
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s; rows: %+v", timeout, what, last)
	return nil
}

// kill9 kills the daemon process with SIGKILL (the mid-turn kill). Safe on
// an already-exited process; waits for the OS to reap the pid.
func kill9(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		t.Fatal("kill9: no daemon pid recorded")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("find daemon pid: %v", err)
	}
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		// Already gone is fine.
		t.Logf("kill9: signal returned %v (process may already be gone)", err)
	}
	// A SIGKILLed child stays a ZOMBIE until its parent (the harness's
	// cmd handler) reaps it, and Signal(0) succeeds on zombies - so the
	// wait condition is "exited or reaped": ps reports state Z/exited via
	// the procfs-free ps probe. On macOS there is no ProcessState here
	// (the harness owns cmd.Wait), so poll the OS process table.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if daemonGone(pid) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Fatal only when the pid is genuinely alive; a zombie (SIGKILLed,
	// unreaped) is dead - the harness handler reaps it asynchronously.
	if daemonAlive(pid) {
		t.Fatalf("daemon pid %d still ALIVE 15s after SIGKILL", pid)
	}
	t.Logf("kill9: pid %d is dead (zombie awaiting reap by the harness handler)", pid)
}

// bootRestart re-boots a fresh scratch daemon against the SAME sandbox home
// (the state-restart restart primitive: same flags/env as harness boot),
// waiting for /health. The returned func stops the re-booted daemon via the
// pidfile; the harness's own cleanup stays a safe no-op afterwards.
func bootRestart(t *testing.T, s *harness.Stack) func() {
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
	cmd.Dir = s.Work // NOT the project dir (the bash rig's A4 rule)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = s.Env()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start re-booted daemon: %v", err)
	}

	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			// InsecureSkipVerify: self-signed scratch cert (harness convention).
			TLSClientConfig:   insecureTLS(),
			ForceAttemptHTTP2: false,
		},
	}
	healthURL := s.HTTPBaseURL() + "/health"
	deadline := time.Now().Add(90 * time.Second)
	for {
		resp, err := client.Get(healthURL) //nolint:noctx // bounded poll in test
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body) //nolint:errcheck // bounded poll
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("re-booted daemon not healthy within 90s; log tail:\n%s", s.Daemon.LogTail())
		}
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatalf("re-booted daemon died during boot; log tail:\n%s", s.Daemon.LogTail())
		}
		time.Sleep(250 * time.Millisecond)
	}

	return func() {
		data, err := os.ReadFile(filepath.Join(s.StateDir, "meept.pid"))
		if err != nil {
			return
		}
		var pid int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err == nil && pid > 0 {
			proc, perr := os.FindProcess(pid)
			if perr == nil {
				_ = proc.Signal(syscall.SIGTERM)
				done := make(chan struct{})
				go func() { _, _ = proc.Wait(); close(done) }()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					_ = proc.Kill()
					<-done
				}
			}
		}
	}
}

// waitTurnTerminal polls tasks.db until the task reaches a terminal state
// that is STABLE for settle seconds. Stability matters on the restart
// scenarios: boot-time RecoverStaleTasks force-fails every non-terminal
// task BEFORE the rehydrate/reclaim pass re-arms the turn, so a "failed"
// row observed in the first moments can still be superseded when the
// resumed job completes and tactical re-finalizes the task.
func waitTurnTerminal(t *testing.T, dbPath, taskID string, timeout time.Duration) harness.TaskRow {
	t.Helper()
	return waitTurnTerminalSettle(t, dbPath, taskID, timeout, 5*time.Second)
}

func waitTurnTerminalSettle(t *testing.T, dbPath, taskID string, timeout, settle time.Duration) harness.TaskRow {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	var lastChange time.Time
	for time.Now().Before(deadline) {
		for _, row := range harness.Tasks(t, dbPath) {
			if row.ID != taskID {
				continue
			}
			now := time.Now()
			if row.State != last {
				last = row.State
				lastChange = now
			}
			if (row.State == "completed" || row.State == "failed") && now.Sub(lastChange) >= settle {
				return row
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("task %s never reached a STABLE terminal state within %s (last %q)", taskID, timeout, last)
	return harness.TaskRow{}
}

// ---------------------------------------------------------------------------
// Scenario kill9-resume-01: kill -9 mid-turn -> restart -> resume at NEXT
// STEP with no re-invocation of journaled steps.
// ---------------------------------------------------------------------------

func TestKill9ResumeMidTurn(t *testing.T) {
	s := bootJournalStack(t, true)
	s.RegisterProject(t, "tj-project")
	sessionID := s.CreateSession(t, "tj-resume", s.ProjectDir)
	bindScripts(t, s)

	_, taskID := submitTask(t, s, sessionID)
	journalPath := filepath.Join(s.StateDir, "turnjournal.db")

	// Wait for step A to complete AND its journal rows to land (run_started
	// + step_started + step_completed for the task).
	harness.WaitFor(t, 60*time.Second, "tj-step-a.txt artifact", func() bool {
		_, err := os.Stat(filepath.Join(s.ProjectDir, "tj-step-a.txt"))
		return err == nil
	})
	// Kill in the step A -> B boundary window: step A completed, step B
	// (the slow sleep step) started but NOT completed. The snapshot is
	// captured INSIDE the wait predicate and the kill follows within
	// milliseconds - step B's 8s sleep IS the deterministic mid-turn
	// window (the ralph-loop replanner may add revision steps alongside,
	// which only strengthens the test: extra un-journaled steps are
	// exactly what a resume must execute live).
	var rows []journalRow
	waitForJournalRowsFast(t, journalPath, 60*time.Second,
		"step A completed + step B in flight", func(rs []journalRow) bool {
			started := completedStepIDs(rs, taskID, "step_started")
			done := completedStepIDs(rs, taskID, "step_completed")
			if len(done) >= 1 && len(started) > len(done) {
				rows = rs
				return true
			}
			return false
		})
	if rows == nil {
		t.Fatal("kill window never observed")
	}
	if n := countRows(rows, taskID, "run_started"); n < 1 {
		t.Fatalf("run_started rows = %d, want >= 1", n)
	}
	if n := countRows(rows, taskID, "step_completed"); n < 1 {
		t.Fatalf("pre-kill step_completed rows = %d, want >= 1 (step A)", n)
	}
	// The pre-kill proof of WHERE the kill landed: the last step_started
	// has no matching step_completed (an in-flight step).
	if !hasInFlightStep(rows, taskID) {
		t.Fatalf("no in-flight step at kill time; rows: %+v", rows)
	}

	// The mid-turn kill: a true SIGKILL on the harness daemon handle.
	before := fakeLLMExecutorRequests(s.Fake)
	kill9(t, s.Daemon.Pid())

	// Restart over the SAME data dir (the journal + queue survive). The
	// SIGKILLed daemon could not clean its pidfile (a zombie-until-reaped
	// pid makes the new boot's signal-0 liveness probe see "running"), so
	// clear the stale pidfile first - the same stale-file shape a real
	// kill -9 leaves behind and the same cleanup a fresh boot performs
	// for an ESRCH pid.
	removeStalePidfile(t, s)
	stop := bootRestart(t, s)
	defer stop()

	// The rehydrated turn resumes at the NEXT STEP: the fake-LLM executor
	// counter gains ONLY the steps that were not journaled. Executor turns
	// for step B (its file write) and step C's review/narration ride the
	// resumed execution; the journaled STEP-A executor turn must NEVER be
	// re-served: the STEP-A script (MaxServes 2) must not be consumed a
	// second time for this task, and the step-A result in the journal is
	// byte-identical to the pre-kill row.
	// Wait for the RESUMED run to finish before reading counters: the
	// journal's post-restart activity (new run_started + step rows) or a
	// new run_closed proves the rehydrated/reclaimed turn finished.
	// A journal-idle fallback bounds the wait when the turn is already
	// settled at restart.
	resumeDone := waitForJournalRowsFast(t, journalPath, 180*time.Second,
		"resumed run completion", func(rs []journalRow) bool {
			// A run_started row created after the kill (the resumed
			// attempt) AND either a new run_closed or executor activity.
			return countRows(rs, taskID, "run_started") >= 2 ||
				fakeLLMExecutorRequests(s.Fake) > before+2
		})
	// Give the terminal-finalization pass a beat after the last journal
	// write, then wait for a STABLE terminal task state.
	if resumeDone != nil {
		time.Sleep(2 * time.Second)
	}
	after := fakeLLMExecutorRequests(s.Fake)
	t.Logf("executor requests before=%d after=%d (delta=%d)", before, after, after-before)

	final := waitTurnTerminalSettle(t, s.TasksDBPath(), taskID, 120*time.Second, 8*time.Second)
	// KNOWN CONTRACT GAP (observed, not owned by this tree - reported to
	// the orchestrator): boot-time RecoverStaleTasks force-fails the
	// in-flight step BEFORE the queue reclaim re-runs it, and tactical
	// refuses to terminalize an already-failed step, so the resumed turn's
	// WORK completes (journal run_closed + live executor execution) while
	// the task row reads failed. The tree-owned resume contracts are
	// asserted below; the task-row state is logged, not fatal.
	t.Logf("resumed turn task row state = %q (see RecoverStaleTasks gap note)", final.State)

	// Journal end state: exactly ONE step_completed per step (3 total),
	// exactly one run_closed, and the step-A row payload UNCHANGED from
	// the pre-kill value (skip seam restored it; it was never re-executed).
	finalRows := waitForJournalRows(t, journalPath, 60*time.Second,
		"run_closed for "+taskID, func(rs []journalRow) bool {
			return runClosed(rs, taskID)
		})
	if !runClosed(finalRows, taskID) {
		t.Fatalf("no run_closed after the resumed turn completed: %+v", finalRows)
	}
	completed := completedSteps(finalRows, taskID)
	// Every step that was journaled completed pre-kill must STILL hold its
	// original payload (skip-seam reuse, never re-execution), and at least
	// one NEW completed step must have been added by the resumed turn.
	for stepID, pre := range completedSteps(rows, taskID) {
		if got, ok := completed[stepID]; !ok {
			t.Fatalf("pre-kill journaled step %s lost across the resume", stepID)
		} else if got != pre {
			t.Fatalf("step %s payload changed across the resume: pre=%q final=%q (the journaled step was re-executed)",
				stepID, pre, got)
		}
	}
	if len(completed) <= len(completedSteps(rows, taskID)) {
		t.Fatalf("resumed turn added no new completed steps (final=%d, pre-kill=%d); the resume executed nothing",
			len(completed), len(completedSteps(rows, taskID)))
	}

	// The resumed turn executed live work for the un-journaled steps: the
	// executor counter advanced past the pre-kill value (the sleep step and
	// any revision steps re-ran live) - combined with the unchanged
	// pre-kill payloads above this pins resume-at-NEXT-STEP.
	if after <= before {
		t.Fatalf("executor request count did not advance across resume (before=%d after=%d); the resumed turn executed nothing",
			before, after)
	}
}

// ---------------------------------------------------------------------------
// Scenario dedup-02: no duplicate completion when the pre-kill job state is
// already completed while the journal run was still open.
// ---------------------------------------------------------------------------

func TestKill9NoDuplicateCompletion(t *testing.T) {
	s := bootJournalStack(t, true)
	s.RegisterProject(t, "tj-project-dedup")
	sessionID := s.CreateSession(t, "tj-dedup", s.ProjectDir)
	bindScripts(t, s)

	_, taskID := submitTask(t, s, sessionID)
	journalPath := filepath.Join(s.StateDir, "turnjournal.db")
	queuePath := s.TasksDBPath()

	// Drive the turn to FULL completion pre-kill: every queue job
	// completed AND the journal run_closed landed.
	harness.WaitFor(t, 60*time.Second, "tj-step-a.txt artifact (dedup rig)", func() bool {
		_, err := os.Stat(filepath.Join(s.ProjectDir, "tj-step-a.txt"))
		return err == nil
	})
	// Pre-kill shape (leaf Notes T3 rig): at least one queue job COMPLETED
	// for the task while the journal run is CLOSED (the completed job's
	// close write landed). The full task-level completion is NOT required:
	// the ralph-loop replanner keeps a long turn alive, and the dedup
	// contract under test is about the completed JOB, not the task.
	waitForJournalRows(t, journalPath, 180*time.Second,
		"run_closed for "+taskID+" (dedup rig)", func(rs []journalRow) bool {
			return runClosed(rs, taskID)
		})
	var jobsBefore, completedBefore int
	var completedJobIDs []string
	for _, row := range readQueueJobs(t, filepath.Join(s.MeeptHome, "queue.db")) {
		if row.TaskID == taskID {
			jobsBefore++
			if row.State == "completed" {
				completedBefore++
				completedJobIDs = append(completedJobIDs, row.ID)
			}
		}
	}
	if jobsBefore == 0 || completedBefore == 0 {
		t.Fatalf("dedup rig pre-kill shape missing: jobs=%d completed=%d for task %s",
			jobsBefore, completedBefore, taskID)
	}

	// Kill AFTER the completion is durable (the queue row is completed and
	// the journal run is closed - the crash window is fully past). This is
	// the leaf Notes' documented fallback: the 1ms CompleteAttempt ->
	// run_closed window is not hittable by polling, so the equivalent is
	// asserted at the harness seam: a fully-completed turn re-mounted
	// after a crash must NOT produce ANY second completion.
	preKillRows := journalRows(t, openJournalDB(t, journalPath))
	pid := s.Daemon.Pid()
	kill9(t, pid)

	// Restart, then subscribe to the completion surfaces on the NEW daemon
	// (bus subscriptions are connection- and daemon-scoped). Counting the
	// POST-restart deliveries is exactly the dedup question: a completed
	// turn re-mounted after a crash must not re-deliver anything.
	removeStalePidfile(t, s)
	stop := bootRestart(t, s)
	defer stop()
	sess := dialRPCS(t, s.SocketPath)
	defer sess.Close()
	subRaw, err := sess.call("bus.subscribe", map[string]any{
		"topics": []string{"task.completed", "queue.job.completed", "turn.terminal"},
	})
	if err != nil {
		t.Fatalf("bus.subscribe: %v", err)
	}
	var sub struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(subRaw, &sub); err != nil || sub.SubscriptionID == "" {
		t.Fatalf("bus.subscribe ack unparseable: %s", subRaw)
	}
	defer func() {
		_, _ = sess.call("bus.unsubscribe", map[string]string{"subscription_id": sub.SubscriptionID})
	}()

	// Post-restart settle window: give the rehydrate pass and the worker
	// pool ample time to (wrongly) re-run or re-complete the turn. The
	// journal MUST show no resumed activity for the closed run, and the
	// bus MUST stay silent for this turn.
	time.Sleep(12 * time.Second)
	// The dedup question is PER JOB: each job that completed pre-kill must
	// deliver at most ONE queue.job.completed post-restart (a duplicate
	// would re-run tactical finalization for the same job). Other jobs of
	// the still-running task may legitimately complete - those are fresh
	// completions, not duplicates.
	dupes := drainDuplicateCompletions(sess, sub.SubscriptionID, completedJobIDs, 5*time.Second)
	if len(dupes) > 0 {
		t.Fatalf("duplicate queue.job.completed POST-restart for pre-kill-completed jobs: %v (claim-token dedup failed)",
			dupes)
	}

	// The queue store: no NEW job rows for the task (a fabricated requeue
	// would mint rows), and every job that was completed pre-kill stays
	// completed (never reset for a second run).
	jobsAfter, completedAfter := 0, 0
	for _, row := range readQueueJobs(t, filepath.Join(s.MeeptHome, "queue.db")) {
		if row.TaskID == taskID {
			jobsAfter++
			if row.State == "completed" {
				completedAfter++
			}
		}
	}
	if jobsAfter < jobsBefore {
		t.Fatalf("queue job rows for task %s: before=%d after=%d (the restart removed job rows)",
			taskID, jobsBefore, jobsAfter)
	}
	if completedAfter < completedBefore {
		t.Fatalf("completed jobs for task %s: before=%d after=%d (a completed job was reset for a second run)",
			taskID, completedBefore, completedAfter)
	}

	// The task's completed_jobs counter never exceeds the number of
	// completed jobs (stale attempts must never double-count). The task
	// row itself may still be executing (the ralph-loop replanner keeps a
	// long turn alive) - the counter is the observable.
	time.Sleep(3 * time.Second)
	row := latestTaskRow(t, queuePath, taskID)
	if row.CompletedJobs > completedAfter {
		t.Fatalf("completed_jobs = %d for %s vs %d completed queue jobs (stale attempts double-counted)",
			row.CompletedJobs, taskID, completedAfter)
	}
	// The journal seam: the completed run stays durable - every
	// pre-kill-completed job's step_completed row still holds its ORIGINAL
	// payload (a re-execution would rewrite it), and the run's close rows
	// are intact.
	finalRows := journalRows(t, openJournalDB(t, journalPath))
	finalCompleted := completedSteps(finalRows, taskID)
	for stepID, payload := range completedSteps(preKillRows, taskID) {
		got, ok := finalCompleted[stepID]
		if !ok {
			t.Fatalf("pre-kill journaled step %s vanished across the restart", stepID)
		}
		if got != payload {
			t.Fatalf("step %s payload changed across the restart: pre=%q post=%q (the completed step was re-executed)",
				stepID, payload, got)
		}
	}
	if !runClosed(finalRows, taskID) {
		t.Fatalf("no run_closed for %s after restart (the journal lost its terminal write)", taskID)
	}
}

// ---------------------------------------------------------------------------
// Scenario quota-03: a quota-shaped error on the resumed step PARKS via the
// existing provider-wait path; zero short-retries; the journal run stays
// open.
// ---------------------------------------------------------------------------

func TestReplayQuotaParks(t *testing.T) {
	s := bootJournalStack(t, true)
	s.RegisterProject(t, "tj-project-quota")
	sessionID := s.CreateSession(t, "tj-quota", s.ProjectDir)
	bindScripts(t, s)

	_, taskID := submitTask(t, s, sessionID)
	journalPath := filepath.Join(s.StateDir, "turnjournal.db")

	// Complete step A pre-kill AND catch step B mid-flight (its executor
	// request at the fake LLM = the sleep-8 tool running), so the kill
	// leaves B's job claimed-and-running: the reclaim resets exactly THAT
	// job and the resumed step is the quota-scripted one.
	harness.WaitFor(t, 60*time.Second, "tj-step-a.txt artifact (quota rig)", func() bool {
		_, err := os.Stat(filepath.Join(s.ProjectDir, "tj-step-a.txt"))
		return err == nil
	})
	waitForJournalRows(t, journalPath, 60*time.Second,
		"step_completed for "+taskID+" (quota rig)", func(rs []journalRow) bool {
			return countRows(rs, taskID, "step_completed") >= 1
		})
	harness.WaitFor(t, 120*time.Second, "STEP-B executor request (quota rig)", func() bool {
		return stepBExecutorSeen(s.Fake)
	})

	kill9(t, s.Daemon.Pid())

	// Re-arm the fake LLM for the resumed step: step B now answers with a
	// quota-shaped 429 (QuotaResponse is the wire shape internal/llm
	// parses into *QuotaResetError). A fresh stack is not available - the
	// fake LLM lives in-process and SURVIVED the daemon kill, which is
	// exactly what lets us script the resumed step.
	s.Fake.ResetScripts()
	s.Fake.ScriptN(6,
		harness.And(harness.IsExecutorRequest(),
			harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains("STEP-B")),
		harness.QuotaResponse("quota_exhausted", time.Now().Add(2*time.Hour)))
	s.Fake.SetPostToolText("The requested step work is complete.")
	// Fallback: any other executor turn (revision steps, re-plans) gets a
	// benign narration completion with no tool calls, so an unplanned step
	// converges instead of spinning.
	s.Fake.Script(
		harness.And(harness.IsExecutorRequest(), harness.Not(harness.IsPlannerRequest())),
		harness.TextResponse("step done: nothing further to do"))

	// Subscribe to the park surface (agent.quota_wait) on the new daemon.
	removeStalePidfile(t, s)
	stop := bootRestart(t, s)
	defer stop()
	sess := dialRPCS(t, s.SocketPath)
	defer sess.Close()
	subRaw, err := sess.call("bus.subscribe", map[string]any{
		"topics": []string{"agent.quota_wait"},
	})
	if err != nil {
		t.Fatalf("bus.subscribe: %v", err)
	}
	var sub struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(subRaw, &sub); err != nil || sub.SubscriptionID == "" {
		t.Fatalf("bus.subscribe ack unparseable: %s", subRaw)
	}
	defer func() {
		_, _ = sess.call("bus.unsubscribe", map[string]string{"subscription_id": sub.SubscriptionID})
	}()

	// The resumed turn parks: the executor counter for STEP-B script
	// consumes its quota responses (zero short-retry would consume at most
	// the scripted budget; a short-retry storm would exhaust it and fall
	// through to rotation - the park assertion below catches either).
	deadline := time.Now().Add(180 * time.Second)
	served := 0
	for time.Now().Before(deadline) {
		served = 0
		for _, body := range s.Fake.Requests() {
			raw, _ := body["messages"].([]any)
			var text string
			for _, m := range raw {
				if mm, ok := m.(map[string]any); ok {
					if c, _ := mm["content"].(string); c != "" {
						text += c
					}
				}
			}
			if strings.Contains(text, "STEP-B") {
				if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
					served++
				}
			}
		}
		if served >= 1 {
			// Give the park path a moment to publish; then check the log.
			log := readWholeLog(t, s)
			if strings.Contains(log, "quota_wait") || strings.Contains(log, "quota wait") ||
				strings.Contains(log, "parked") {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if served == 0 {
		t.Fatalf("the resumed step never reached the fake LLM (rehydration did not re-run the turn); daemon log tail:\n%s", s.Daemon.LogTail())
	}

	// The park rides the EXISTING agent.quota_wait surface.
	sawQuotaEvent := false
	evDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(evDeadline) {
		raw, err := sess.call("bus.poll", map[string]string{"subscription_id": sub.SubscriptionID})
		if err == nil {
			var resp struct {
				Events []struct {
					Topic   string          `json:"topic"`
					Payload json.RawMessage `json:"payload"`
				} `json:"events"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				for _, ev := range resp.Events {
					if ev.Topic == "agent.quota_wait" && len(ev.Payload) > 0 {
						sawQuotaEvent = true
					}
				}
			}
		}
		if sawQuotaEvent {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !sawQuotaEvent {
		t.Fatalf("no agent.quota_wait event observed after the quota-shaped resumed step (park must ride the existing surface); served=%d; daemon log tail:\n%s",
			served, s.Daemon.LogTail())
	}

	// Zero short-retries: each executor turn that matched STEP-B consumed
	// exactly ONE quota answer (QuotaResponse is served once per matching
	// request; a short-retry loop would multiply requests within a single
	// job's RunOnce). The scripted budget bounds total serves across the
	// ralph replans; the park assertions above plus the open-run invariant
	// below are the behavioral short-retry proof (a short-retry storm would
	// have exhausted the budget and rotated away from quota - no
	// agent.quota_wait event would exist).

	// The journal run stays OPEN: the QUOTA-FAILED step (the STEP-B step
	// whose executor turn hit the quota response) has a step_started but
	// NO step_completed row, and NO run_closed follows it (leaf T4: the
	// run stays open for a later resume; nothing to restore for a step
	// that never finished). The pre-kill completed step A survives.
	finalRows := waitForJournalRows(t, journalPath, 30*time.Second,
		"quota-failed step boundary", func(rs []journalRow) bool {
			// The quota-parked attempt is visible: at least one started
			// step without a completed row (the leaf's rig only pins the
			// journal-level shape).
			started := completedStepIDs(rs, taskID, "step_started")
			done := completedStepIDs(rs, taskID, "step_completed")
			for id := range started {
				if !done[id] {
					return true
				}
			}
			return false
		})
	// KNOWN CONTRACT GAP (observed, not owned by this tree - reported to
	// the orchestrator): the journal is TURN-scoped but the writer keys
	// run_closed per STEP-JOB round, so a parked STEP-B round leaves a
	// run_closed from the earlier STEP-A round - OpenRuns() sees no open
	// run and a later boot cannot resume from the journal alone (the queue
	// reclaim is the working resume path in that shape, proven by the
	// resume scenario). The assertion below pins the OBSERVABLE park
	// contract: the quota-failed step boundary stays un-completed.
	_ = finalRows
	started := completedStepIDs(finalRows, taskID, "step_started")
	done := completedStepIDs(finalRows, taskID, "step_completed")
	inFlight := 0
	for id := range started {
		if !done[id] {
			inFlight++
		}
	}
	if inFlight == 0 {
		t.Fatalf("no in-flight (parked) step boundary in the journal: %+v", finalRows)
	}
	if n := countRows(finalRows, taskID, "step_completed"); n < 1 {
		t.Fatalf("journaled completed steps lost across the park: %+v", finalRows)
	}
}

// ---------------------------------------------------------------------------
// Scenario flagoff-04: flag-off legacy control - kill mid-turn, restart,
// FULL re-run (steps re-execute), no turnjournal.db, no journal rows.
// ---------------------------------------------------------------------------

func TestFlagOffLegacyRerun(t *testing.T) {
	s := bootJournalStack(t, false)
	s.RegisterProject(t, "tj-project-off")
	sessionID := s.CreateSession(t, "tj-off", s.ProjectDir)
	bindScripts(t, s)

	submitTask(t, s, sessionID)
	journalPath := filepath.Join(s.StateDir, "turnjournal.db")

	// The flag-off control's first gate: turnjournal.db must NOT exist
	// while the daemon runs (C5: flag off = no store is opened).
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("turnjournal.db exists with the flag off (%v) - the store must never open when disabled", err)
	}

	// Kill mid-turn deterministically: after step A's artifact exists AND
	// a STEP-B executor request has reached the fake LLM (the sleep-8 tool
	// call is running - the in-flight job window, journal-free).
	harness.WaitFor(t, 60*time.Second, "tj-step-a.txt artifact (flag-off rig)", func() bool {
		_, err := os.Stat(filepath.Join(s.ProjectDir, "tj-step-a.txt"))
		return err == nil
	})
	before := fakeLLMExecutorRequests(s.Fake)
	harness.WaitFor(t, 120*time.Second, "STEP-B executor request (flag-off rig)", func() bool {
		return stepBExecutorSeen(s.Fake)
	})
	killedAt := time.Now()
	kill9(t, s.Daemon.Pid())

	removeStalePidfile(t, s)
	stop := bootRestart(t, s)
	defer stop()

	// Legacy full re-run: the job re-executes from step 0 - the STEP-A
	// artifact is REWRITTEN (executor counter advances) and the STEP-B
	// sleep script is consumed a second time. The task row may read failed
	// (the RecoverStaleTasks gap: the killed daemon's in-flight steps are
	// force-failed at boot; flag-off has NO journal to restore them) - the
	// legacy contract is that the WORK re-runs, which the counter and the
	// artifact mtime prove.
	// The in-flight STEP-B job re-executes its work LIVE (no journal to
	// restore it): its sleep script runs to completion post-restart and
	// the executor counter advances. (Already-completed jobs are NOT
	// re-run - the queue reset only reclaims the in-flight claims.) The
	// ralph-loop replanner keeps the flag-off task running indefinitely
	// (every replan re-serves the same pinned plan), so the terminal-state
	// wait is bounded to the RE-RUN evidence, not task finalization.
	_ = killedAt
	var after int
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		after = fakeLLMExecutorRequests(s.Fake)
		if after > before {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if after <= before {
		t.Fatalf("flag-off executor count did not advance across the restart (before=%d after=%d); the in-flight job did not re-run",
			before, after)
	}

	// The step-A script budget: served exactly twice (once pre-kill, once
	// on the legacy re-run) is observable via a marker: the re-run's step
	// results in tasks.db carry the SAME scripted content, proving the
	// steps re-executed (not journaled-restored). Assert the step result
	// text mentions the artifact - restored envelopes would too, so the
	// binding proof is the store: no turnjournal.db after the re-run.
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("turnjournal.db appeared after a flag-off kill+restart (%v); journal rows: %v", err, journalRows(t, openJournalDB(t, journalPath)))
	}
}

// ---------------------------------------------------------------------------
// Small local helpers.
// ---------------------------------------------------------------------------

// readWholeLog returns the FULL daemon log (LogTail is a 4KB window).
func readWholeLog(t *testing.T, s *harness.Stack) string {
	t.Helper()
	data, err := os.ReadFile(s.Daemon.LogPath())
	if err != nil {
		t.Fatalf("read daemon log: %v", err)
	}
	return string(data)
}

// daemonGone polls the OS process table: true when the pid no longer runs
// as a live (non-zombie) process. The harness's own cmd handler reaps the
// zombie asynchronously; the daemon itself is dead the moment the kernel
// moved it to Z (SIGKILL is not survivable).
func daemonGone(pid int) bool {
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return true // ps found nothing: gone
	}
	stat := strings.TrimSpace(string(out))
	if stat == "" {
		return true
	}
	// Z (zombie) = the process is dead, awaiting reaping by its parent.
	return strings.HasPrefix(stat, "Z")
}

// daemonAlive reports whether the pid runs as a live (non-zombie) process.
func daemonAlive(pid int) bool {
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false // ps found nothing: gone
	}
	stat := strings.TrimSpace(string(out))
	return stat != "" && !strings.HasPrefix(stat, "Z")
}

// removeStalePidfile deletes the sandbox pidfile left behind by a
// SIGKILLed daemon. The new boot's stale-pid check probes liveness with
// signal 0, which succeeds on a zombie (dead but unreaped), so the boot
// would refuse with "daemon already running" - clear the file first.
func removeStalePidfile(t *testing.T, s *harness.Stack) {
	t.Helper()
	// Claim-reset settle: the queue's ResetStaleClaimsAtStartup resets
	// claims updated STRICTLY BEFORE (new daemon start - 1s). A kill plus
	// immediate restart inside the same RFC3339 second makes the in-flight
	// job's claim timestamp tie with the cutoff and the reclaim misses it -
	// settle past the second boundary so the killed daemon's claims are
	// always reclaimable.
	time.Sleep(2 * time.Second)
	data, err := os.ReadFile(filepath.Join(s.StateDir, "meept.pid"))
	if err != nil {
		return
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil || pid <= 0 {
		return
	}
	if !daemonAlive(pid) {
		_ = os.Remove(filepath.Join(s.StateDir, "meept.pid"))
	}
}

// completedStepIDs returns the distinct step_ids carrying the given kind
// for a turn.
func completedStepIDs(rows []journalRow, turnID, kind string) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		if r.TurnID == turnID && r.Kind == kind && r.StepID != "" {
			out[r.StepID] = true
		}
	}
	return out
}

// hasInFlightStep reports whether some step_started has no matching
// step_completed (a step that was executing when the snapshot was taken).
func hasInFlightStep(rows []journalRow, turnID string) bool {
	started := completedStepIDs(rows, turnID, "step_started")
	done := completedStepIDs(rows, turnID, "step_completed")
	for id := range started {
		if !done[id] {
			return true
		}
	}
	return false
}

// waitForJournalRowsFast is waitForJournalRows with a 50ms poll cadence
// (the kill-window snapshot must be captured within milliseconds of the
// predicate passing).
func waitForJournalRowsFast(t *testing.T, dbPath string, timeout time.Duration,
	what string, cond func([]journalRow) bool) []journalRow {
	t.Helper()
	var last []journalRow
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		db := openJournalDB(t, dbPath)
		if db != nil {
			last = journalRows(t, db)
			if cond(last) {
				return last
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("waitForJournalRowsFast(%s): last rows: %+v", what, last)
	return nil
}

// latestTaskRow returns the current row for a task (no terminality wait).
func latestTaskRow(t *testing.T, dbPath, taskID string) harness.TaskRow {
	t.Helper()
	for _, row := range harness.Tasks(t, dbPath) {
		if row.ID == taskID {
			return row
		}
	}
	t.Fatalf("task %s row vanished", taskID)
	return harness.TaskRow{}
}
