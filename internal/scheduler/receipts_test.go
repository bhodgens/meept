package scheduler

// F13b gate (05-schedule/00T-tests.md T2): session delivery inbox + receipts.
// Pins contract L2 (.hermes/contracts/05-schedule.md):
//   - AgentJobConfig.TargetSessionID -> Execute pins payload["session_id"]
//   - receipt status transitions pending -> in_flight -> delivered
//   - DeliveryHistory bounded to MaxDeliveryHistoryRecords (200) and
//     DeliveryHistoryRetentionDays (30) with EarlierRecordsPruned set
//   - recoverInFlightDeliveries (called in Start after loadPersistedJobs)
//     re-delivers stuck pending/in_flight receipts with the SAME MessageID
//     (chat.request turn_id dedupe makes it idempotent)
//
// RED until leaf 02-delivery-inbox lands (symbols undefined today).

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

func TestAgentJob_DeliversToSession(t *testing.T) {
	msgBus := bus.New(nil, nil)

	cfg := JobConfig{
		ID:       "target-session-job",
		Name:     "Target Session Job",
		Type:     JobTypeAgent,
		Schedule: "@yearly",
		Enabled:  true,
		AgentConfig: &AgentJobConfig{
			Prompt:          "deliver me",
			TargetSessionID: "sess-target-1",
		},
	}

	job, err := NewAgentJob(cfg, msgBus)
	if err != nil {
		t.Fatalf("NewAgentJob: %v", err)
	}

	sub := msgBus.Subscribe("f13b-test", "chat.request")
	defer msgBus.Unsubscribe(sub)

	if err := job.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	select {
	case msg := <-sub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		got, ok := payload["session_id"].(string)
		if !ok || got != "sess-target-1" {
			t.Errorf("payload[session_id] = %v (ok=%v), want sess-target-1", payload["session_id"], ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for chat.request")
	}
}

func TestReceiptStatus_Transitions(t *testing.T) {
	tmpDir := t.TempDir()
	msgBus := bus.New(nil, nil)
	cfg := config.SchedulerConfig{Enabled: true, Timezone: "UTC"}

	s, err := NewScheduler(cfg, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	// The job must exist in the store before receipts are recorded.
	if _, err := s.ScheduleConfig(JobConfig{
		ID:       "job-trans",
		Name:     "Transitions Job",
		Type:     JobTypeAgent,
		Schedule: "@yearly",
		Enabled:  true,
		AgentConfig: &AgentJobConfig{
			Prompt:          "ping",
			TargetSessionID: "sess-trans-1",
		},
	}); err != nil {
		t.Fatalf("ScheduleConfig(job-trans): %v", err)
	}

	receipt := ScheduleDeliveryReceipt{
		ScheduleID:      "job-trans",
		ScheduledAt:     time.Now().UTC(),
		OccurrenceAt:    time.Now().UTC(),
		MessageID:       "turn-trans-1",
		TargetSessionID: "sess-trans-1",
		Prompt:          "ping",
		Status:          DeliveryPending,
	}

	// pending -> in_flight -> delivered via recordDelivery.
	if err := s.recordDelivery("job-trans", receipt); err != nil {
		t.Fatalf("recordDelivery(pending): %v", err)
	}
	receipt.Status = DeliveryInFlight
	receipt.DeliveredAt = time.Now().UTC()
	if err := s.recordDelivery("job-trans", receipt); err != nil {
		t.Fatalf("recordDelivery(in_flight): %v", err)
	}
	receipt.Status = DeliveryDelivered
	if err := s.recordDelivery("job-trans", receipt); err != nil {
		t.Fatalf("recordDelivery(delivered): %v", err)
	}

	jc, ok := s.Store().Get("job-trans")
	if !ok {
		t.Fatal("persisted job job-trans not found")
	}
	if jc.LastDelivery == nil {
		t.Fatal("LastDelivery nil after transitions")
	}
	if jc.LastDelivery.Status != DeliveryDelivered {
		t.Errorf("LastDelivery.Status = %q, want %q", jc.LastDelivery.Status, DeliveryDelivered)
	}
	if jc.LastDelivery.MessageID != "turn-trans-1" {
		t.Errorf("MessageID = %q, want turn-trans-1", jc.LastDelivery.MessageID)
	}
}

func TestReceipts_BoundedHistory(t *testing.T) {
	tmpDir := t.TempDir()
	msgBus := bus.New(nil, nil)
	cfg := config.SchedulerConfig{Enabled: true, Timezone: "UTC"}

	s, err := NewScheduler(cfg, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	if _, err := s.ScheduleConfig(JobConfig{
		ID:       "job-hist",
		Name:     "History Job",
		Type:     JobTypeAgent,
		Schedule: "@yearly",
		Enabled:  true,
		AgentConfig: &AgentJobConfig{
			Prompt:          "hist",
			TargetSessionID: "sess-hist",
		},
	}); err != nil {
		t.Fatalf("ScheduleConfig(job-hist): %v", err)
	}

	now := time.Now().UTC()

	// 210 delivered receipts: history must prune to <= 200 and flag it.
	for i := range MaxDeliveryHistoryRecords + 10 {
		r := ScheduleDeliveryReceipt{
			ScheduleID:      "job-hist",
			ScheduledAt:     now,
			OccurrenceAt:    now.Add(time.Duration(i) * time.Minute),
			DeliveredAt:     now.Add(time.Duration(i) * time.Minute),
			MessageID:       "turn-hist-" + string(rune('a'+i%26)) + "-" + time.Duration(i).String(),
			TargetSessionID: "sess-hist",
			Status:          DeliveryDelivered,
		}
		if err := s.recordDelivery("job-hist", r); err != nil {
			t.Fatalf("recordDelivery[%d]: %v", i, err)
		}
	}

	// One receipt older than the retention window: must be pruned too.
	old := ScheduleDeliveryReceipt{
		ScheduleID:      "job-hist",
		ScheduledAt:     now.Add(-time.Duration(DeliveryHistoryRetentionDays+5) * 24 * time.Hour),
		OccurrenceAt:    now.Add(-time.Duration(DeliveryHistoryRetentionDays+5) * 24 * time.Hour),
		DeliveredAt:     now.Add(-time.Duration(DeliveryHistoryRetentionDays+5) * 24 * time.Hour),
		MessageID:       "turn-hist-old",
		TargetSessionID: "sess-hist",
		Status:          DeliveryDelivered,
	}
	if err := s.recordDelivery("job-hist", old); err != nil {
		t.Fatalf("recordDelivery(old): %v", err)
	}

	jc, ok := s.Store().Get("job-hist")
	if !ok {
		t.Fatal("persisted job job-hist not found")
	}
	hist := jc.DeliveryHistory
	if hist == nil {
		t.Fatal("DeliveryHistory nil after bounded recording")
	}
	if len(hist.Records) > MaxDeliveryHistoryRecords {
		t.Errorf("history len = %d, want <= %d", len(hist.Records), MaxDeliveryHistoryRecords)
	}
	if !hist.EarlierRecordsPruned {
		t.Error("EarlierRecordsPruned = false after pruning, want true")
	}
	for _, r := range hist.Records {
		if r.MessageID == "turn-hist-old" {
			t.Error("receipt older than retention window survived pruning")
		}
	}
}

func TestRecoverInFlight_Delivery(t *testing.T) {
	tmpDir := t.TempDir()
	msgBus := bus.New(nil, nil)
	cfg := config.SchedulerConfig{Enabled: true, Timezone: "UTC"}

	// Phase 1: job with a target session exists and a receipt is stuck
	// in_flight (crash after publish, before the session ack).
	s1, err := NewScheduler(cfg, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler(s1): %v", err)
	}
	jobCfg := JobConfig{
		ID:       "job-recover",
		Name:     "Recover Job",
		Type:     JobTypeAgent,
		Schedule: "@yearly",
		Enabled:  true,
		AgentConfig: &AgentJobConfig{
			Prompt:          "redeliver me",
			TargetSessionID: "sess-recover-1",
		},
	}
	if _, err := s1.ScheduleConfig(jobCfg); err != nil {
		t.Fatalf("ScheduleConfig: %v", err)
	}
	stuck := ScheduleDeliveryReceipt{
		ScheduleID:      "job-recover",
		ScheduledAt:     time.Now().UTC(),
		OccurrenceAt:    time.Now().UTC(),
		MessageID:       "turn-recover-1",
		TargetSessionID: "sess-recover-1",
		Prompt:          "redeliver me",
		Status:          DeliveryInFlight,
	}
	if err := s1.recordDelivery("job-recover", stuck); err != nil {
		t.Fatalf("recordDelivery(stuck): %v", err)
	}
	if err := s1.Stop(context.Background()); err != nil {
		t.Fatalf("Stop(s1): %v", err)
	}

	// Phase 2: restart. Start must call recoverInFlightDeliveries after
	// loadPersistedJobs and re-publish the stuck receipt with the SAME
	// MessageID (turn_id dedupe makes re-delivery idempotent).
	msgBus2 := bus.New(nil, nil)
	s2, err := NewScheduler(cfg, msgBus2, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler(s2): %v", err)
	}

	sub := msgBus2.Subscribe("f13b-recover", "chat.request")
	defer msgBus2.Unsubscribe(sub)

	if err := s2.Start(context.Background()); err != nil {
		t.Fatalf("Start(s2): %v", err)
	}
	t.Cleanup(func() { _ = s2.Shutdown(context.Background()) })

	select {
	case msg := <-sub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if sid, _ := payload["session_id"].(string); sid != "sess-recover-1" {
			t.Errorf("re-delivered payload[session_id] = %v, want sess-recover-1", payload["session_id"])
		}
		if mid, _ := payload["turn_id"].(string); mid != "turn-recover-1" {
			t.Errorf("re-delivered payload[turn_id] = %v, want turn-recover-1 (same MessageID)", payload["turn_id"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: recoverInFlightDeliveries did not re-publish the stuck receipt")
	}
}
