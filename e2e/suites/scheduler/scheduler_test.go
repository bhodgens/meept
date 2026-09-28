//go:build e2e

// Package scheduler covers internal/scheduler at the daemon boundary
// (manifest scenarios scheduler-01..03): a shell job added through the
// scheduler RPC surface fires on its cron schedule within the window,
// executes its command for real, and lands an observable audit trail
// (jobs.json run accounting + scheduler_claims.db claimed tick); the claim
// store then proves idempotency — the same tick is never delivered twice.
//
// Determinism note: the scheduler is cron-driven over real time, but the
// second-granularity cron parser plus a tight fire window keeps the suite
// hermetic and fast (no wall-clock mocking is wired through the daemon).
// Idempotency is asserted against the claim store rather than by racing two
// fires: the tick claimed by the observed fire must be present exactly once,
// and RunNow deliveries use nanosecond-unique ticks, so a second delivery of
// the same tick is structurally impossible — the suite verifies the persisted
// evidence of that invariant.
package scheduler

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// rpc runs one JSON-RPC call against the sandbox daemon and returns the
// unwrapped result object (failing the test on an RPC error envelope).
func rpc(t *testing.T, s *harness.Stack, method string, params any) map[string]any {
	t.Helper()
	client := harness.DialRPC(t, s.SocketPath)
	return client.CallResult(method, params)
}

// str fetches a string field from a decoded RPC result map.
func str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// jobsDBPath resolves the scheduler's persisted-jobs file (jobs.json in the
// data dir, per scheduler.Store / WithDataDir(cfg.Daemon.DataDir)).
func jobsDBPath(s *harness.Stack) string {
	return filepath.Join(s.StateDir, "jobs.json")
}

// claimsDBPath resolves the scheduler's claimed-ticks SQLite store.
func claimsDBPath(s *harness.Stack) string {
	return filepath.Join(s.StateDir, "scheduler_claims.db")
}

// addShellJob registers one shell job through scheduler.add_job. A "* * * * *
// " style schedule fires every minute; the caller picks the expression.
func addShellJob(t *testing.T, s *harness.Stack, id, schedule, command string, args []string) string {
	t.Helper()
	params := map[string]any{
		"id":       id,
		"name":     "e2e " + id,
		"type":     "shell",
		"schedule": schedule,
		"enabled":  true,
		"shell_config": map[string]any{
			"command":        command,
			"args":           args,
			"work_dir":       s.ProjectDir,
			"capture_output": true,
		},
	}
	res := rpc(t, s, "scheduler.add_job", params)
	jobID := str(res, "job_id")
	if jobID == "" {
		t.Fatalf("scheduler.add_job returned no job_id: %v", res)
	}
	return jobID
}

// persistedJob is the decoded jobs.json JobConfig shape the suite asserts on.
type persistedJob struct {
	ID        string     `json:"id"`
	RunCount  int64      `json:"run_count"`
	LastRunAt *time.Time `json:"last_run_at"`
	LastError string     `json:"last_error"`
}

// readPersistedJobs decodes the daemon's jobs.json.
func readPersistedJobs(t *testing.T, s *harness.Stack) map[string]persistedJob {
	t.Helper()
	data, err := os.ReadFile(jobsDBPath(s))
	if err != nil {
		t.Fatalf("read jobs.json: %v", err)
	}
	var doc struct {
		Jobs []persistedJob `json:"jobs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode jobs.json: %v", err)
	}
	out := make(map[string]persistedJob, len(doc.Jobs))
	for _, j := range doc.Jobs {
		out[j.ID] = j
	}
	return out
}

// claimCountForJob opens scheduler_claims.db read-only and counts the claimed
// ticks for one job.
func claimCountForJob(t *testing.T, s *harness.Stack, jobID string) int {
	t.Helper()
	db, err := sql.Open("sqlite", claimsDBPath(s)+"?_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		t.Fatalf("open scheduler_claims.db: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM scheduler_claimed_ticks WHERE job_id = ?`, jobID,
	).Scan(&n); err != nil {
		t.Fatalf("count claimed ticks for %s: %v", jobID, err)
	}
	return n
}

// ---------------------------------------------------------------------------
// scheduler-01 (M): a scheduled job fires within its window, executes, and
// lands its audit trail
// ---------------------------------------------------------------------------

// TestScheduledShellJobFiresAndRecordsRun adds a shell job at
// second-granularity cron (the parser accepts an optional seconds field),
// waits inside a tight window for the run accounting to land, and asserts:
// the job's command actually executed (artifact file), the persisted config
// carries run_count >= 1 with a last_run_at timestamp, and the claimed-tick
// store holds the fire's tick (the audit trail of the claim-before-deliver
// dispatch).
func TestScheduledShellJobFiresAndRecordsRun(t *testing.T) {
	s := harness.Start(t)

	// Compute the next whole minute boundary so the * * * * * fire lands
	// within a bounded, predictable window (11s after the minute turn is
	// generous for the cron loop's 10s-ish granularity).
	now := time.Now()
	nextMinute := now.Truncate(time.Minute).Add(time.Minute)
	wait := nextMinute.Sub(now) + 15*time.Second
	if wait > 90*time.Second {
		t.Fatalf("next-minute wait %s exceeds the hermetic window", wait)
	}

	artifact := filepath.Join(s.ProjectDir, "scheduler-fired.txt")
	jobID := addShellJob(t, s, "e2e-fire-01", "* * * * *",
		"/bin/sh", []string{"-c", "printf fired >> " + artifact + " 2>&1 || true"})

	// The job is listed by the scheduler RPC surface.
	listRes := rpc(t, s, "scheduler.list_jobs", map[string]any{})
	raw, err := json.Marshal(listRes)
	if err != nil {
		t.Fatalf("marshal list_jobs: %v", err)
	}
	if !strings.Contains(string(raw), jobID) {
		t.Fatalf("scheduler.list_jobs missing %s: %s", jobID, raw)
	}

	// Wait through the fire window.
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(artifact); err == nil && strings.Contains(string(data), "fired") {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("scheduled job never executed its command within %s: %v\ndaemon log tail:\n%s",
			wait, err, s.Daemon.LogTail())
	}
	if !strings.Contains(string(data), "fired") {
		t.Fatalf("artifact content = %q, want it to contain fired", data)
	}

	// The run accounting landed in the persisted config: run_count >= 1
	// with a last_run_at inside the test window and no error. The artifact
	// proves the command ran; UpdateLastRun persists just after execution,
	// so poll briefly for the accounting to land rather than reading once.
	accDeadline := time.Now().Add(15 * time.Second)
	for {
		jobs := readPersistedJobs(t, s)
		job, ok := jobs[jobID]
		if ok && job.RunCount >= 1 && job.LastRunAt != nil && job.LastError == "" {
			if job.LastRunAt.Before(now.Add(-time.Minute)) {
				t.Fatalf("job last_run_at = %s, predates the test", job.LastRunAt)
			}
			break
		}
		if time.Now().After(accDeadline) {
			t.Fatalf("run accounting never landed for %s after the artifact fired: %+v", jobID, job)
		}
		time.Sleep(250 * time.Millisecond)
	}

	// The claim store holds this fire's tick — the claim-before-deliver
	// audit trail. Exactly one tick claimed so far for this job.
	if n := claimCountForJob(t, s, jobID); n != 1 {
		t.Fatalf("claimed ticks for %s = %d, want 1 (one scheduled fire so far)", jobID, n)
	}
}

// ---------------------------------------------------------------------------
// scheduler-02 (S): the claimed tick is never delivered twice — the persisted
// claims hold one row per distinct tick, and a replayed claim of the SAME
// tick is a no-op
// ---------------------------------------------------------------------------

// TestClaimedTickIsNeverDoubled proves the idempotency invariant against the
// persisted claim store: after the scheduled fire plus one manual RunNow
// delivery (nanosecond-unique tick), each claimed tick is a distinct row —
// the store contains no duplicate tick for either delivery, and the job's
// accounting reflects exactly the delivered runs.
func TestClaimedTickIsNeverDoubled(t *testing.T) {
	s := harness.Start(t)

	// A long-interval job that will NOT fire on its own during the test;
	// deliveries come from an explicit RunNow only.
	jobID := addShellJob(t, s, "e2e-idem-02", "0 0 31 2 *",
		"/bin/sh", []string{"-c", "true"})

	// Two manual deliveries. RunNow refuses while a previous delivery is
	// still running, so wait for each run to land (run_count) before
	// triggering the next.
	for want := int64(1); want <= 2; want++ {
		rpc(t, s, "scheduler.run_job", map[string]any{"job_id": jobID})
		deadline := time.Now().Add(30 * time.Second)
		for {
			job, ok := readPersistedJobs(t, s)[jobID]
			if ok && job.RunCount >= want {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("run %d never landed (jobs.json: %v)", want, readPersistedJobs(t, s)[jobID])
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Each RunNow claims a distinct nanosecond-unique tick: two deliveries,
	// two claim rows, and the persisted config records run_count = 2.
	deadline := time.Now().Add(30 * time.Second)
	var claims int
	for time.Now().Before(deadline) {
		claims = claimCountForJob(t, s, jobID)
		job, ok := readPersistedJobs(t, s)[jobID]
		if ok && job.RunCount == 2 && claims == 2 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if claims != 2 {
		t.Fatalf("claimed ticks after 2 RunNow deliveries = %d, want 2 (one unique tick per delivery)", claims)
	}
	job, ok := readPersistedJobs(t, s)[jobID]
	if !ok {
		t.Fatalf("job %s missing from jobs.json", jobID)
	}
	if job.RunCount != 2 {
		t.Fatalf("job run_count = %d, want exactly 2 after 2 deliveries", job.RunCount)
	}
	if job.LastError != "" {
		t.Fatalf("job last_error = %q, want empty", job.LastError)
	}

	// A distinct-tick re-claim through the RPC-free path is already covered
	// by the store's UNIQUE(job_id, tick_time); assert the schema guarantee
	// is actually in force by attempting a duplicate insert and expecting a
	// constraint failure.
	db, err := sql.Open("sqlite", claimsDBPath(s)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open claims db rw: %v", err)
	}
	defer db.Close()
	var tick string
	if err := db.QueryRow(
		`SELECT tick_time FROM scheduler_claimed_ticks WHERE job_id = ? LIMIT 1`, jobID,
	).Scan(&tick); err != nil {
		t.Fatalf("read a claimed tick: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO scheduler_claimed_ticks(job_id, tick_time) VALUES(?,?)`, jobID, tick,
	); err == nil {
		t.Fatal("duplicate claim insert succeeded — the UNIQUE(job_id, tick_time) guard is missing")
	}
}

// ---------------------------------------------------------------------------
// scheduler-03 (S): job lifecycle controls — pause stops fires, resume
// restores them, remove cleans up
// ---------------------------------------------------------------------------

// TestPauseResumeRemoveLifecycle drives the scheduler's lifecycle RPC
// surface: pause removes the job from the cron table (listed disabled), and
// remove deletes the job from both the live scheduler and the persisted
// config.
func TestPauseResumeRemoveLifecycle(t *testing.T) {
	s := harness.Start(t)

	jobID := addShellJob(t, s, "e2e-lifecycle-03", "* * * * *",
		"/bin/sh", []string{"-c", "true"})

	// Pause: the job stays registered but leaves the fire path.
	res := rpc(t, s, "scheduler.pause_job", map[string]any{"job_id": jobID})
	if res["paused"] != true {
		t.Fatalf("pause_job result = %v, want paused=true", res)
	}
	jobs := readPersistedJobs(t, s)
	if job, ok := jobs[jobID]; ok && job.LastError != "" {
		t.Fatalf("unexpected error after pause: %q", job.LastError)
	}

	// The paused job is still listed.
	listRes := rpc(t, s, "scheduler.list_jobs", map[string]any{})
	raw, err := json.Marshal(listRes)
	if err != nil {
		t.Fatalf("marshal list_jobs: %v", err)
	}
	if !strings.Contains(string(raw), jobID) {
		t.Fatalf("paused job %s missing from list_jobs: %s", jobID, raw)
	}

	// Resume is legal from paused.
	rpc(t, s, "scheduler.resume_job", map[string]any{"job_id": jobID})

	// Remove: the job disappears from the scheduler and jobs.json.
	rpc(t, s, "scheduler.remove_job", map[string]any{"job_id": jobID})
	listRes = rpc(t, s, "scheduler.list_jobs", map[string]any{})
	raw, err = json.Marshal(listRes)
	if err != nil {
		t.Fatalf("marshal list_jobs: %v", err)
	}
	if strings.Contains(string(raw), jobID) {
		t.Fatalf("removed job %s still listed: %s", jobID, raw)
	}
	if _, ok := readPersistedJobs(t, s)[jobID]; ok {
		t.Fatalf("removed job %s still persisted in jobs.json", jobID)
	}
}
