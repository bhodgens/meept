//go:build e2e

// Package scheduler — F13a/F13b branch e2e rows (05-schedule/00T-tests.md T3).
// Timing-kind schedule fires on schedule (one-shot `at` against the real
// robfigAtSchedule registration path), session delivery receipts record on a
// TargetSessionID job, and a daemon restart re-delivers a stuck in-flight
// receipt with the SAME MessageID (at-least-once, idempotent via the
// chat.request turn_id).
//
// Evidence surfaces: the durable jobs.json delivery receipts
// (ScheduleDeliveryReceipt: status/target_session_id/message_id/bounded
// history), the fake LLM's received completion requests (the turn actually
// reached the model boundary — the fake server lives in the test process and
// survives a daemon restart), and the daemon log ("recovering in-flight
// delivery"). The RPC bus.subscribe stream is deliberately NOT used: its
// collector dropped events under concurrent-daemon load in this suite.
//
// Restart primitive mirrors goal-completion/helpers_test.go: the harness has
// no exported Stop/Restart API, so the suite spawns the scratch daemon
// binary itself (same flags/env as harness.Daemon.boot) and stops the
// re-booted daemon via the state-dir pidfile.
package scheduler

import (
	"crypto/tls"
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
)

// ---------------------------------------------------------------------------
// restart primitive (same shape as goal-completion/helpers_test.go)
// ---------------------------------------------------------------------------

// stopDaemonByPid SIGTERMs the pid and waits for exit (SIGKILL after 10s).
func stopDaemonByPid(t *testing.T, pid int) {
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
}

// bootScratchDaemon spawns a fresh daemon against the SAME sandbox and
// waits for /health. Same flags/env as harness.Daemon.boot.
func bootScratchDaemon(t *testing.T, s *harness.Stack) {
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

// restartDaemon stops the harness daemon and re-boots against the SAME
// sandbox home. The returned func stops the re-booted daemon (pidfile).
func restartDaemon(t *testing.T, s *harness.Stack) func() {
	t.Helper()

	oldPID := s.Daemon.Pid()
	if oldPID == 0 {
		t.Fatal("harness daemon pid not recorded; nothing to restart")
	}
	stopDaemonByPid(t, oldPID)

	// The old process must release the socket path before re-binding.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(s.SocketPath); os.IsNotExist(err) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	bootScratchDaemon(t, s)

	return func() {
		data, err := os.ReadFile(filepath.Join(s.StateDir, "meept.pid"))
		if err != nil {
			return
		}
		var pid int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err == nil {
			stopDaemonByPid(t, pid)
		}
	}
}

// ---------------------------------------------------------------------------
// jobs.json full-shape helpers (receipt assertions)
// ---------------------------------------------------------------------------

// receiptShape is the ScheduleDeliveryReceipt JSON the suite asserts on.
type receiptShape struct {
	ScheduleID      string    `json:"schedule_id"`
	ScheduledAt     time.Time `json:"scheduled_at"`
	DeliveredAt     time.Time `json:"delivered_at"`
	MessageID       string    `json:"message_id"`
	TargetSessionID string    `json:"target_session_id"`
	Prompt          string    `json:"prompt"`
	Status          string    `json:"status"`
}

// deliveryHist is the bounded DeliveryHistory in the persisted JobConfig.
type deliveryHist struct {
	Records []receiptShape `json:"records"`
}

// fullJob mirrors the persisted JobConfig fields the receipt tests need.
type fullJob struct {
	ID              string         `json:"id"`
	LastDelivery    *receiptShape  `json:"last_delivery"`
	DeliveryHistory *deliveryHist  `json:"delivery_history"`
	Timing          map[string]any `json:"timing"`
	TargetSession   string         `json:"-"`
}

// readPersistedJobsFull decodes jobs.json including the delivery fields.
func readPersistedJobsFull(t *testing.T, s *harness.Stack) map[string]fullJob {
	t.Helper()
	data, err := os.ReadFile(jobsDBPath(s))
	if err != nil {
		t.Fatalf("read jobs.json: %v", err)
	}
	var doc struct {
		Jobs []fullJob `json:"jobs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode jobs.json: %v", err)
	}
	out := make(map[string]fullJob, len(doc.Jobs))
	for _, j := range doc.Jobs {
		out[j.ID] = j
	}
	return out
}

// rewindReceiptToInFlight rewires the job's last_delivery back to in_flight
// in jobs.json — exactly the state a daemon dies in between claim and
// session ack (the crash simulation for restart recovery).
func rewindReceiptToInFlight(t *testing.T, s *harness.Stack, jobID, messageID string) {
	t.Helper()
	data, err := os.ReadFile(jobsDBPath(s))
	if err != nil {
		t.Fatalf("read jobs.json for rewind: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode jobs.json for rewind: %v", err)
	}
	jobs, _ := doc["jobs"].([]any)
	found := false
	for _, raw := range jobs {
		job, ok := raw.(map[string]any)
		if !ok || job["id"] != jobID {
			continue
		}
		ld, ok := job["last_delivery"].(map[string]any)
		if !ok {
			t.Fatalf("job %s has no last_delivery to rewind", jobID)
		}
		if ld["message_id"] != messageID {
			t.Fatalf("receipt message_id %v != expected %q", ld["message_id"], messageID)
		}
		ld["status"] = "in_flight"
		delete(ld, "delivered_at")
		// A real crash rewinds the HISTORY row too: recordDelivery persists
		// the history append and the LastDelivery mirror in one store.Update
		// per transition, so killing the daemon before the delivered-write
		// leaves BOTH in_flight. Without this rewind the F2 already-delivered
		// guard (receipts.go hasDeliveredReceiptForMessageID) sees the still-
		// delivered history row and correctly refuses to re-deliver.
		if hist, ok := job["delivery_history"].(map[string]any); ok {
			recs, ok := hist["records"].([]any)
			if !ok {
				t.Fatalf("job %s delivery_history.records has wrong shape", jobID)
			}
			histHit := false
			for _, rr := range recs {
				rec, ok := rr.(map[string]any)
				if !ok || rec["message_id"] != messageID {
					continue
				}
				rec["status"] = "in_flight"
				delete(rec, "delivered_at")
				histHit = true
			}
			if !histHit {
				t.Fatalf("job %s history has no record for %s", jobID, messageID)
			}
		}
		found = true
	}
	if !found {
		t.Fatalf("job %s not found in jobs.json for rewind", jobID)
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("encode jobs.json for rewind: %v", err)
	}
	if err := os.WriteFile(jobsDBPath(s), out, 0o600); err != nil {
		t.Fatalf("write rewound jobs.json: %v", err)
	}
}

// fakeRequestsContaining reports whether the fake LLM received at least n
// completion requests whose user-message content contains the needle. The
// fake server lives in the test process, so its request log spans a daemon
// restart — the LLM-boundary proof of (re-)delivery.
func fakeRequestsContaining(f *harness.FakeLLM, needle string, n int) bool {
	count := 0
	for _, body := range f.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role == "user" {
				if c, _ := msg["content"].(string); strings.Contains(c, needle) {
					count++
					break
				}
			}
		}
		if count >= n {
			return true
		}
	}
	return count >= n
}

// addSessionPinnedAgentJob adds an agent job with a timing spec pinned to a
// target session — the F13 shape shared by all three scenarios.
func addSessionPinnedAgentJob(t *testing.T, s *harness.Stack, id, timing, prompt, sessionID string) string {
	t.Helper()
	res := rpc(t, s, "scheduler.add_job", map[string]any{
		"id":      id,
		"name":    "e2e " + id,
		"type":    "agent",
		"timing":  timing,
		"enabled": true,
		"agent_config": map[string]any{
			"prompt":            prompt,
			"target_session_id": sessionID,
		},
	})
	jobID := str(res, "job_id")
	if jobID == "" {
		t.Fatalf("scheduler.add_job returned no job_id: %v", res)
	}
	if res["next_run"] == nil {
		t.Fatalf("timing job has no next_run: %v", res)
	}
	return jobID
}

// ---------------------------------------------------------------------------
// scheduler-04 (F13a): a timing-kind schedule fires on schedule
// ---------------------------------------------------------------------------

// TestTimingKindScheduleFires adds an AGENT job with a semantic timing spec
// (one-shot "at <RFC3339>" ~75s out) through scheduler.add_job and proves
// the timing engine fires it at the scheduled instant: the delivery reaches
// the model boundary (fake LLM saw the prompt) and the receipt lands in
// jobs.json delivered, targeting the pinned session. The one-shot kind
// exercises the robfigAtSchedule registration path (no cron expr).
func TestTimingKindScheduleFires(t *testing.T) {
	s := harness.Start(t)

	// The chat path must answer without tools: plain finish.
	s.Fake.SetChatText("timing-kind fired ok")

	sessionID := s.CreateSession(t, "timing-inbox", s.ProjectDir)

	// One-shot ~50s out: the smallest margin that stays safely outside the
	// add_job→fire round-trip (the fire can only come from the engine)
	// while keeping the scenario under a minute of wall clock.
	fireInstant := time.Now().UTC().Add(50 * time.Second).Truncate(time.Second)
	jobID := addSessionPinnedAgentJob(t, s, "e2e-timing-f13a",
		"at "+fireInstant.Format(time.RFC3339), "timing-kind fire probe", sessionID)

	// The job is listed (the timing admission path, not raw cron).
	listRes := rpc(t, s, "scheduler.list_jobs", map[string]any{})
	rawList, err := json.Marshal(listRes)
	if err != nil {
		t.Fatalf("marshal list_jobs: %v", err)
	}
	if !strings.Contains(string(rawList), jobID) {
		t.Fatalf("scheduler.list_jobs missing %s: %s", jobID, rawList)
	}

	// The timing spec persisted (semantic Timing field, kind "at").
	harness.WaitFor(t, 10*time.Second, "timing spec in jobs.json", func() bool {
		job, ok := readPersistedJobsFull(t, s)[jobID]
		return ok && job.Timing != nil && job.Timing["kind"] == "at"
	})

	// Wait for the scheduled fire: the turn reaches the model boundary.
	deadline := time.Until(fireInstant) + 20*time.Second
	fireDeadline := time.Now().Add(deadline)
	for time.Now().Before(fireDeadline) {
		if fakeRequestsContaining(s.Fake, "timing-kind fire probe", 1) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !fakeRequestsContaining(s.Fake, "timing-kind fire probe", 1) {
		t.Fatalf("timing job never fired within %s (no completion request at the model boundary); daemon log tail:\n%s",
			deadline, s.Daemon.LogTail())
	}

	// The receipt landed in jobs.json: delivered, pinned to the session.
	harness.WaitFor(t, 15*time.Second, "delivery receipt delivered in jobs.json", func() bool {
		job, ok := readPersistedJobsFull(t, s)[jobID]
		return ok && job.LastDelivery != nil &&
			job.LastDelivery.Status == "delivered" &&
			job.DeliveryHistory != nil && len(job.DeliveryHistory.Records) >= 1
	})
	job := readPersistedJobsFull(t, s)[jobID]
	if job.LastDelivery.TargetSessionID != sessionID {
		t.Fatalf("receipt target_session_id = %q, want %q", job.LastDelivery.TargetSessionID, sessionID)
	}
	if job.LastDelivery.MessageID == "" {
		t.Fatal("receipt message_id is empty")
	}
}

// ---------------------------------------------------------------------------
// scheduler-05 (F13b): session delivery receipt recorded (receipts.go path)
// ---------------------------------------------------------------------------

// TestSessionDeliveryReceiptRecorded drives the receipts path: an agent job
// pinned to a target session fires (RunNow), the receipt transitions to
// delivered in jobs.json with the session id, and the bounded delivery
// history records the occurrence with message_id == the delivered turn id
// (the idempotency key for re-delivery).
func TestSessionDeliveryReceiptRecorded(t *testing.T) {
	s := harness.Start(t)
	s.Fake.SetChatText("receipt recorded ok")

	sessionID := s.CreateSession(t, "receipt-inbox", s.ProjectDir)

	// A far-future one-shot: no scheduled fire during the test; the
	// delivery comes from RunNow. The instant must sit INSIDE the
	// one-shot scheduling horizon (validateOneShotHorizon, 2h default)
	// while remaining beyond any realistic test window — +90m.
	fireInstant := time.Now().UTC().Add(90 * time.Minute).Truncate(time.Second)
	jobID := addSessionPinnedAgentJob(t, s, "e2e-receipt-f13b",
		"at "+fireInstant.Format(time.RFC3339), "receipt probe", sessionID)

	// No receipt before the fire.
	if job, ok := readPersistedJobsFull(t, s)[jobID]; ok && job.LastDelivery != nil {
		t.Fatalf("receipt recorded before any delivery: %+v", job.LastDelivery)
	}

	// Fire now: RunNow executes the session-pinned delivery path
	// (recordDelivery pending -> in_flight -> delivered around the
	// chat.request publish).
	rpc(t, s, "scheduler.run_job", map[string]any{"job_id": jobID})

	// The turn reached the model boundary with the pinned prompt.
	harness.WaitFor(t, 30*time.Second, "delivered turn at the model boundary", func() bool {
		return fakeRequestsContaining(s.Fake, "receipt probe", 1)
	})

	// The receipt lands delivered with the session id and the history
	// carries the occurrence.
	var last receiptShape
	harness.WaitFor(t, 30*time.Second, "delivery receipt delivered in jobs.json", func() bool {
		job, ok := readPersistedJobsFull(t, s)[jobID]
		if !ok || job.LastDelivery == nil {
			return false
		}
		last = *job.LastDelivery
		return job.LastDelivery.Status == "delivered" &&
			job.DeliveryHistory != nil && len(job.DeliveryHistory.Records) >= 1
	})
	if last.TargetSessionID != sessionID {
		t.Fatalf("receipt target_session_id = %q, want %q", last.TargetSessionID, sessionID)
	}
	if last.MessageID == "" {
		t.Fatal("receipt message_id is empty")
	}
	if last.ScheduledAt.IsZero() {
		t.Fatal("receipt scheduled_at is zero")
	}

	// The history record carries the SAME message_id (idempotent
	// re-delivery key) and the prompt.
	rec := jobReceiptHistory(t, s, jobID)
	if len(rec) < 1 {
		t.Fatal("delivery history is empty")
	}
	if rec[0].MessageID != last.MessageID {
		t.Fatalf("history message_id %q != last_delivery message_id %q", rec[0].MessageID, last.MessageID)
	}
	if rec[0].Prompt != "receipt probe" {
		t.Fatalf("history prompt = %q, want the job prompt", rec[0].Prompt)
	}
}

// jobReceiptHistory returns the job's recorded delivery history (or nil).
func jobReceiptHistory(t *testing.T, s *harness.Stack, jobID string) []receiptShape {
	t.Helper()
	job, ok := readPersistedJobsFull(t, s)[jobID]
	if !ok || job.DeliveryHistory == nil {
		return nil
	}
	return job.DeliveryHistory.Records
}

// ---------------------------------------------------------------------------
// scheduler-06 (F13b): restart re-delivers a stuck in-flight receipt
// ---------------------------------------------------------------------------

// TestRestartRedeliversInFlightReceipt crashes a delivery (the in-flight
// receipt persists in jobs.json), kills the daemon, re-boots against the
// SAME sandbox, and proves recoverInFlightDeliveries re-delivered with the
// SAME MessageID: the recovery log names the message id, a SECOND completion
// request carrying the prompt reaches the model boundary, and the receipt
// row is delivered again.
func TestRestartRedeliversInFlightReceipt(t *testing.T) {
	s := harness.Start(t)
	s.Fake.SetChatText("recovery redelivered ok")

	sessionID := s.CreateSession(t, "recovery-inbox", s.ProjectDir)

	// The instant must sit INSIDE the one-shot scheduling horizon
	// (validateOneShotHorizon, 2h default) while remaining beyond any
	// realistic test window — +90m; the deliveries come from RunNow and
	// the crash-recovery redelivery, never from the scheduled instant.
	fireInstant := time.Now().UTC().Add(90 * time.Minute).Truncate(time.Second)
	jobID := addSessionPinnedAgentJob(t, s, "e2e-redelivery-f13b",
		"at "+fireInstant.Format(time.RFC3339), "redelivery probe", sessionID)

	// Fire once: the receipt is recorded delivered and the turn lands.
	rpc(t, s, "scheduler.run_job", map[string]any{"job_id": jobID})
	var firstDelivered receiptShape
	harness.WaitFor(t, 30*time.Second, "first delivery receipt delivered", func() bool {
		job, ok := readPersistedJobsFull(t, s)[jobID]
		if !ok || job.LastDelivery == nil {
			return false
		}
		firstDelivered = *job.LastDelivery
		return job.LastDelivery.Status == "delivered"
	})
	firstMessageID := firstDelivered.MessageID
	if firstMessageID == "" {
		t.Fatal("first delivery receipt has no message_id")
	}
	harness.WaitFor(t, 30*time.Second, "first turn at the model boundary", func() bool {
		return fakeRequestsContaining(s.Fake, "redelivery probe", 1)
	})

	// Simulate the crash: rewind the receipt to in_flight in jobs.json.
	rewindReceiptToInFlight(t, s, jobID, firstMessageID)

	// Kill the daemon and re-boot against the SAME sandbox home.
	stop := restartDaemon(t, s)
	defer stop()

	// Recovery must re-deliver with the SAME MessageID: the recovery log
	// names the message id...
	logDeadline := time.Now().Add(45 * time.Second)
	recoveryLogged := false
	for time.Now().Before(logDeadline) {
		// Read the FULL log file (not LogTail): the re-booted daemon's
		// memory-capture retry spam can push the recovery line out of a
		// bounded tail window under a loaded suite run.
		if data, err := os.ReadFile(filepath.Join(s.Work, "daemon.log")); err == nil {
			if strings.Contains(string(data), "recovering in-flight delivery") &&
				strings.Contains(string(data), firstMessageID) {
				recoveryLogged = true
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !recoveryLogged {
		t.Fatalf("restart recovery never logged the in-flight re-delivery of %s; log tail:\n%s",
			firstMessageID, s.Daemon.LogTail())
	}

	// ...a SECOND completion request with the prompt reaches the model
	// boundary (at-least-once re-delivery)...
	harness.WaitFor(t, 30*time.Second, "re-delivered turn at the model boundary", func() bool {
		return fakeRequestsContaining(s.Fake, "redelivery probe", 2)
	})

	// ...and the receipt row is delivered again.
	harness.WaitFor(t, 15*time.Second, "receipt back to delivered after recovery", func() bool {
		job, ok := readPersistedJobsFull(t, s)[jobID]
		return ok && job.LastDelivery != nil && job.LastDelivery.Status == "delivered"
	})
	job := readPersistedJobsFull(t, s)[jobID]
	if job.LastDelivery.MessageID != firstMessageID {
		t.Fatalf("recovered receipt message_id %q != original %q (re-delivery must be idempotent)",
			job.LastDelivery.MessageID, firstMessageID)
	}
}
