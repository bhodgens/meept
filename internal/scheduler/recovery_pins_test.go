package scheduler

// Bughunt 2026-11-12 fix-wave pin tests (findings F1-F5, audit HEAD 6fb6a300).
//
//   F1 (HIGH, infinite re-delivery): a recovery re-delivery must reconcile
//       the original stuck receipt row IN PLACE, so two consecutive
//       recoverInFlightDeliveries runs deliver ONCE total.
//   F2 (HIGH, dedupe-that-isn't): a stuck MessageID that already has a
//       DELIVERED receipt in history must be skipped BEFORE re-publishing —
//       chat.request (internal/agent) registers the turn with no
//       prior-completion check, so re-publishing re-executes a completed
//       turn.
//   F3 (MED, corrupt-file wipe): a jobs.json parse failure at NewStore must
//       quarantine the file to jobs.json.corrupt-<ts> BEFORE any Add can
//       persist, so the corrupt file survives a construction-time
//       Schedule+Add.
//   F4 (MED, cap evicts stuck rows): DeliveryHistory.Append's count bound
//       must evict delivered rows first; stuck rows only as a last resort.
//   F5 (MED, panic kills daemon post-claim): RunNow and dispatchMissed
//       goroutines recover panics (cron.Recover covers only cron fires).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

// drainChatRequests collects turn_id payloads from a chat.request
// subscription until the channel goes quiet.
func drainChatRequests(t *testing.T, sub *bus.Subscriber, idle time.Duration) map[string]int {
	t.Helper()
	counts := map[string]int{}
	deadline := time.After(5 * time.Second)
collect:
	for {
		select {
		case msg, ok := <-sub.Channel:
			if !ok {
				break collect
			}
			var payload map[string]any
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			turnID, _ := payload["turn_id"].(string)
			counts[turnID]++
		case <-time.After(idle):
			break collect
		case <-deadline:
			break collect
		}
	}
	return counts
}

// TestRecoverSkipsAlreadyDeliveredMessageID pins F2: a stuck receipt whose
// MessageID already has a DELIVERED row in history is never re-published;
// its stuck row is reconciled to delivered with an audit note instead.
func TestRecoverSkipsAlreadyDeliveredMessageID(t *testing.T) {
	msgBus := bus.New(nil, nil)
	tmpDir := t.TempDir()
	s, err := NewScheduler(config.SchedulerConfig{Enabled: true, Timezone: "UTC"}, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if _, err := s.ScheduleConfig(JobConfig{
		ID:       "job-f2",
		Name:     "F2 Job",
		Type:     JobTypeAgent,
		Schedule: "@yearly",
		Enabled:  true,
		AgentConfig: &AgentJobConfig{
			Prompt:          "f2",
			TargetSessionID: "sess-f2",
		},
	}); err != nil {
		t.Fatalf("ScheduleConfig: %v", err)
	}

	now := time.Now().UTC()
	// The turn was delivered (crash AFTER the delivered receipt write left a
	// stale pending row behind, or a partial-write shape). A pending row and
	// a delivered row share the MessageID.
	pending := ScheduleDeliveryReceipt{
		ScheduleID:      "job-f2",
		ScheduledAt:     now,
		OccurrenceAt:    now,
		MessageID:       "turn-f2-completed",
		TargetSessionID: "sess-f2",
		Status:          DeliveryPending,
	}
	delivered := pending
	delivered.Status = DeliveryDelivered
	delivered.DeliveredAt = now.Add(time.Minute)
	if err := s.recordDelivery("job-f2", pending); err != nil {
		t.Fatalf("recordDelivery(pending): %v", err)
	}
	if err := s.recordDelivery("job-f2", delivered); err != nil {
		t.Fatalf("recordDelivery(delivered): %v", err)
	}

	sub := msgBus.Subscribe("f2-count", "chat.request")
	defer msgBus.Unsubscribe(sub)

	if err := s.recoverInFlightDeliveries(); err != nil {
		t.Fatalf("recoverInFlightDeliveries: %v", err)
	}
	counts := drainChatRequests(t, sub, 300*time.Millisecond)
	if n := counts["turn-f2-completed"]; n != 0 {
		t.Fatalf("completed turn re-published %d time(s), want 0 — chat.request has no prior-completion dedupe, this re-executes the turn (F2)", n)
	}

	// The stuck row must be reconciled (no longer pending/in_flight).
	jc, ok := s.Store().Get("job-f2")
	if !ok {
		t.Fatal("job-f2 vanished from store")
	}
	stuckLeft := 0
	for _, r := range jc.DeliveryHistory.Records {
		if r.MessageID == "turn-f2-completed" &&
			(r.Status == DeliveryPending || r.Status == DeliveryInFlight) {
			stuckLeft++
		}
	}
	if stuckLeft != 0 {
		t.Errorf("%d stuck row(s) for turn-f2-completed remain after recovery — every restart would re-evaluate them (F2)", stuckLeft)
	}

	// Second pass: nothing stuck, still zero deliveries.
	if err := s.recoverInFlightDeliveries(); err != nil {
		t.Fatalf("recoverInFlightDeliveries(2nd): %v", err)
	}
	if n := drainChatRequests(t, sub, 300*time.Millisecond)["turn-f2-completed"]; n != 0 {
		t.Errorf("second recovery pass re-published completed turn %d time(s), want 0", n)
	}
}

// TestTwoRecoveryPassesDeliverOnce pins F1: a stuck receipt WITHOUT a prior
// delivered row is re-delivered exactly once across two consecutive
// recovery runs — the first run reconciles the stuck row in place so the
// second run has nothing left to recover.
func TestTwoRecoveryPassesDeliverOnce(t *testing.T) {
	msgBus := bus.New(nil, nil)
	tmpDir := t.TempDir()
	s, err := NewScheduler(config.SchedulerConfig{Enabled: true, Timezone: "UTC"}, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if _, err := s.ScheduleConfig(JobConfig{
		ID:       "job-f1",
		Name:     "F1 Job",
		Type:     JobTypeAgent,
		Schedule: "@yearly",
		Enabled:  true,
		AgentConfig: &AgentJobConfig{
			Prompt:          "f1",
			TargetSessionID: "sess-f1",
		},
	}); err != nil {
		t.Fatalf("ScheduleConfig: %v", err)
	}
	now := time.Now().UTC()
	stuck := ScheduleDeliveryReceipt{
		ScheduleID:      "job-f1",
		ScheduledAt:     now,
		OccurrenceAt:    now,
		MessageID:       "turn-f1-once",
		TargetSessionID: "sess-f1",
		Status:          DeliveryInFlight,
	}
	if err := s.recordDelivery("job-f1", stuck); err != nil {
		t.Fatalf("recordDelivery(stuck): %v", err)
	}

	sub := msgBus.Subscribe("f1-count", "chat.request")
	defer msgBus.Unsubscribe(sub)

	if err := s.recoverInFlightDeliveries(); err != nil {
		t.Fatalf("recoverInFlightDeliveries: %v", err)
	}
	first := drainChatRequests(t, sub, 400*time.Millisecond)

	if err := s.recoverInFlightDeliveries(); err != nil {
		t.Fatalf("recoverInFlightDeliveries(2nd): %v", err)
	}
	second := drainChatRequests(t, sub, 400*time.Millisecond)

	total := first["turn-f1-once"] + second["turn-f1-once"]
	if total != 1 {
		t.Fatalf("two consecutive recovery runs delivered turn-f1-once %d time(s) (pass1=%d pass2=%d), want exactly 1 — the stuck row was never reconciled (F1)", total, first["turn-f1-once"], second["turn-f1-once"])
	}

	// The original stuck row carries the recovery audit note.
	jc, ok := s.Store().Get("job-f1")
	if !ok {
		t.Fatal("job-f1 vanished from store")
	}
	reconciled := false
	for _, r := range jc.DeliveryHistory.Records {
		if r.MessageID == "turn-f1-once" && r.Status == DeliveryDelivered && r.RecoveryNote != "" {
			reconciled = true
		}
	}
	if !reconciled {
		t.Error("no reconciled delivered row with a RecoveryNote found for turn-f1-once")
	}
}

// TestCorruptJobsFileQuarantinedBeforeAdd pins F3: a jobs.json that fails
// to parse is quarantined to jobs.json.corrupt-<ts> at NewStore time, and
// a construction-time Schedule+Add cannot wipe it.
func TestCorruptJobsFileQuarantinedBeforeAdd(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "jobs.json")
	corrupt := []byte(`{"version":1,"jobs":[{"id":"job-trapped","na`)
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatalf("write corrupt jobs.json: %v", err)
	}

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Construction-time Schedule+Add (the daemon's NewComponents pattern).
	cfg := JobConfig{
		ID:       "job-new",
		Name:     "Construction Job",
		Type:     JobTypeShell,
		Schedule: "@yearly",
		Enabled:  true,
		ShellConfig: &ShellJobConfig{
			Command: "true",
		},
	}
	if err := store.Add(cfg); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// The corrupt file must have survived as a quarantine copy.
	matches, err := filepath.Glob(path + ".corrupt-*")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no jobs.json.corrupt-<ts> quarantine copy found — the unparsed file was silently destroyed by Add (F3)")
	}
	for _, m := range matches {
		data, readErr := os.ReadFile(m)
		if readErr != nil {
			t.Fatalf("read quarantine %s: %v", m, readErr)
		}
		if string(data) != string(corrupt) {
			t.Errorf("quarantine copy %s content drifted from the corrupt original", m)
		}
	}

	// The live path must no longer hold the corrupt content (it was moved
	// aside), so Load no longer fails on it.
	if data, readErr := os.ReadFile(path); readErr == nil && string(data) == string(corrupt) {
		t.Error("corrupt content still live at jobs.json — quarantine must move it out of the write path")
	}
}

// TestAppendEvictsTerminalBeforeStuck pins F4: when the count bound
// overflows, the oldest DELIVERED rows are evicted first and stuck rows
// survive.
func TestAppendEvictsTerminalBeforeStuck(t *testing.T) {
	h := NewDeliveryHistory()
	now := time.Now().UTC()
	// Fill the cap with delivered rows.
	for i := 0; i < MaxDeliveryHistoryRecords; i++ {
		h.Append(ScheduleDeliveryReceipt{
			ScheduleID:  "job-f4",
			ScheduledAt: now.Add(time.Duration(i) * time.Minute),
			DeliveredAt: now.Add(time.Duration(i) * time.Minute),
			MessageID:   fmt.Sprintf("turn-f4-delivered-%03d", i),
			Status:      DeliveryDelivered,
		})
	}
	// Two stuck rows overflow the cap.
	h.Append(ScheduleDeliveryReceipt{
		ScheduleID: "job-f4", ScheduledAt: now, MessageID: "turn-f4-stuck-a", Status: DeliveryPending,
	})
	h.Append(ScheduleDeliveryReceipt{
		ScheduleID: "job-f4", ScheduledAt: now, MessageID: "turn-f4-stuck-b", Status: DeliveryInFlight,
	})

	if len(h.Records) > MaxDeliveryHistoryRecords {
		t.Fatalf("history len = %d, want <= %d", len(h.Records), MaxDeliveryHistoryRecords)
	}
	kept := map[string]bool{}
	for _, r := range h.Records {
		kept[r.MessageID] = true
	}
	for _, mid := range []string{"turn-f4-stuck-a", "turn-f4-stuck-b"} {
		if !kept[mid] {
			t.Errorf("stuck receipt %s evicted while delivered rows remained — recovery loses the occurrence (F4)", mid)
		}
	}
	if len(h.LastEvictedStuckIDs) != 0 {
		t.Errorf("LastEvictedStuckIDs = %v, want empty (terminal eviction was enough)", h.LastEvictedStuckIDs)
	}
	// The oldest delivered row must be the eviction victim.
	if kept["turn-f4-delivered-000"] {
		t.Error("oldest delivered row survived while the cap overflowed — terminal rows must be evicted first")
	}
}

// TestAppendLastResortStuckEvictionWarns pins F4's last-resort branch: when
// NO terminal rows remain, the oldest stuck rows are evicted and named in
// LastEvictedStuckIDs (recordDelivery Warns for each).
func TestAppendLastResortStuckEvictionWarns(t *testing.T) {
	h := NewDeliveryHistory()
	now := time.Now().UTC()
	ids := make([]string, 0, MaxDeliveryHistoryRecords+1)
	for i := 0; i < MaxDeliveryHistoryRecords+1; i++ {
		mid := fmt.Sprintf("turn-f4-allstuck-%03d", i)
		ids = append(ids, mid)
		h.Append(ScheduleDeliveryReceipt{
			ScheduleID:  "job-f4b",
			ScheduledAt: now.Add(time.Duration(i) * time.Minute),
			MessageID:   mid,
			Status:      DeliveryPending,
		})
	}
	if len(h.Records) > MaxDeliveryHistoryRecords {
		t.Fatalf("history len = %d, want <= %d", len(h.Records), MaxDeliveryHistoryRecords)
	}
	if len(h.LastEvictedStuckIDs) == 0 {
		t.Fatal("last-resort stuck eviction not flagged in LastEvictedStuckIDs — the lost occurrence is silent (F4)")
	}
	if !strings.Contains(h.LastEvictedStuckIDs[0], "turn-f4-allstuck-") {
		t.Errorf("evicted stuck IDs name the wrong messages: %v", h.LastEvictedStuckIDs)
	}
	// The NEWEST stuck rows must be the survivors.
	last := h.Records[len(h.Records)-1]
	if last.MessageID != ids[len(ids)-1] {
		t.Errorf("newest stuck row %s not kept; last record = %s", ids[len(ids)-1], last.MessageID)
	}
}

// panicJob is a Job whose Execute panics (F5).
type panicJob struct {
	id string
}

func (j *panicJob) ID() string       { return j.id }
func (j *panicJob) Name() string     { return "panic job" }
func (j *panicJob) Schedule() string { return "@yearly" }
func (j *panicJob) Type() JobType    { return JobTypeShell }
func (j *panicJob) Config() JobConfig {
	return JobConfig{
		ID: j.id, Name: j.Name(), Type: JobTypeShell, Schedule: j.Schedule(), Enabled: true,
		ShellConfig: &ShellJobConfig{Command: "true"},
	}
}
func (j *panicJob) Execute(_ context.Context) error {
	panic("injected job panic")
}

// waitJobIdle polls until the job is no longer marked running.
func waitJobIdle(t *testing.T, s *Scheduler, jobID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		running := s.runningJobs[jobID]
		s.mu.RUnlock()
		if !running {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job still marked running after 5s")
}

// TestRunNowPanicRecovered pins F5 (RunNow): a panicking job must not crash
// the test process; the goroutine recovers, logs, and the scheduler keeps
// running.
func TestRunNowPanicRecovered(t *testing.T) {
	msgBus := bus.New(nil, nil)
	s, err := NewScheduler(config.SchedulerConfig{Enabled: true, Timezone: "UTC"}, msgBus, WithDataDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	job := &panicJob{id: "job-panic-runnow"}
	if _, err := s.Schedule(job); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	if err := s.RunNow(job.ID()); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	waitJobIdle(t, s, job.ID())

	// Reaching here without the test process dying IS the pin: the panic
	// was recovered inside the goroutine instead of killing the daemon
	// after the tick was claimed (F5).
	if !s.Running() {
		t.Error("scheduler stopped after a job panic — recovery must not take the scheduler down")
	}
}

// TestDispatchMissedPanicRecovered pins F5 (dispatchMissed): a panicking
// job on the missed-tick path must be recovered — the tick was already
// claimed, so an unrecovered panic would lose the occurrence with the
// daemon.
func TestDispatchMissedPanicRecovered(t *testing.T) {
	msgBus := bus.New(nil, nil)
	s, err := NewScheduler(config.SchedulerConfig{Enabled: true, Timezone: "UTC"}, msgBus, WithDataDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	job := &panicJob{id: "job-panic-missed"}
	if _, err := s.Schedule(job); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	tick := time.Now().UTC().Add(-time.Minute)
	s.dispatchMissed(job, tick, 0)
	waitJobIdle(t, s, job.ID())

	if !s.Running() {
		t.Error("scheduler stopped after a missed-tick job panic — recovery must not take the scheduler down")
	}
}
