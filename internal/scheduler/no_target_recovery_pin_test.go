package scheduler

// M-7 (bughunt 2026-11-14 round 2) pin: the no-target-session guard must
// run BEFORE the F2 already-delivered reconciliation. A job whose
// AgentConfig.TargetSessionID was removed after its receipts went stuck
// used to get those rows flipped to delivered with the "already delivered"
// note — a false audit trail, because nothing was ever deliverable to a
// job with no target session. The guard must own the skip: the stuck rows
// stay pending/in_flight and the Warn fires.

import (
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

// TestRecoveryKeepsStuckRowsOnTargetlessJob pins M-7: a stuck receipt on a
// job WITHOUT a target session survives a recovery pass untouched — no
// delivered flip, no reconciliation note, and no chat.request publish.
func TestRecoveryKeepsStuckRowsOnTargetlessJob(t *testing.T) {
	msgBus := bus.New(nil, nil)
	s, err := NewScheduler(config.SchedulerConfig{Enabled: true, Timezone: "UTC"}, msgBus, WithDataDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	// The job CARRIES stuck receipts (a legacy session-pinned job whose
	// TargetSessionID was later removed) but now has AgentConfig == nil
	// shape: use a job with AgentConfig present but empty TargetSessionID —
	// the guard's exact condition.
	if _, err := s.ScheduleConfig(JobConfig{
		ID:       "job-m7",
		Name:     "M7 Job",
		Type:     JobTypeAgent,
		Schedule: "@yearly",
		Enabled:  true,
		AgentConfig: &AgentJobConfig{
			Prompt:          "m7",
			TargetSessionID: "", // removed after the rows went stuck
		},
	}); err != nil {
		t.Fatalf("ScheduleConfig: %v", err)
	}

	now := time.Now().UTC()
	// Both stuck statuses, plus a DELIVERED twin so the old code path
	// (reconciliation BEFORE the guard) would have flipped them with the
	// false "already delivered" note.
	stuckPending := ScheduleDeliveryReceipt{
		ScheduleID:      "job-m7",
		ScheduledAt:     now,
		OccurrenceAt:    now,
		MessageID:       "turn-m7-never-deliverable",
		TargetSessionID: "sess-m7-gone",
		Status:          DeliveryPending,
	}
	stuckInFlight := stuckPending
	stuckInFlight.Status = DeliveryInFlight
	if err := s.recordDelivery("job-m7", stuckPending); err != nil {
		t.Fatalf("recordDelivery(pending): %v", err)
	}
	if err := s.recordDelivery("job-m7", stuckInFlight); err != nil {
		t.Fatalf("recordDelivery(in_flight): %v", err)
	}

	sub := msgBus.Subscribe("m7-count", "chat.request")
	defer msgBus.Unsubscribe(sub)

	if err := s.recoverInFlightDeliveries(); err != nil {
		t.Fatalf("recoverInFlightDeliveries: %v", err)
	}

	// Zero publishes.
	if counts := drainChatRequests(t, sub, 300*time.Millisecond); len(counts) != 0 {
		t.Fatalf("target-less job published %v on recovery — nothing is deliverable without a target session", counts)
	}

	jc, ok := s.Store().Get("job-m7")
	if !ok {
		t.Fatal("job-m7 vanished from store")
	}
	for _, r := range jc.DeliveryHistory.Records {
		if r.MessageID != "turn-m7-never-deliverable" {
			continue
		}
		if r.Status != DeliveryPending && r.Status != DeliveryInFlight {
			t.Errorf("stuck row for %s was flipped to %s by recovery on a target-less job — the no-target-session guard must precede the F2 reconciliation (M-7)", r.MessageID, r.Status)
		}
		if r.RecoveryNote != "" {
			t.Errorf("stuck row for %s carries a false RecoveryNote %q — nothing was ever deliverable (M-7)", r.MessageID, r.RecoveryNote)
		}
	}

	// Second pass: idempotent, still untouched, still zero publishes.
	if err := s.recoverInFlightDeliveries(); err != nil {
		t.Fatalf("recoverInFlightDeliveries(2nd): %v", err)
	}
	if counts := drainChatRequests(t, sub, 300*time.Millisecond); len(counts) != 0 {
		t.Errorf("second recovery pass published %v for a target-less job", counts)
	}
}
