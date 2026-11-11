package scheduler

// Bughunt 2026-11-10 fix-wave pin tests (findings B8 + B2).
//
//   B8: DeliveryHistory.Append must EXEMPT pending/in_flight receipts from
//       the age prune — recoverInFlightDeliveries recovers FROM those rows,
//       so pruning a stuck receipt older than retention permanently lost
//       the tick.
//   B2: a claimed tick with NO delivery receipt (crash between ClaimTick
//       and the first receipt write) must be re-delivered by startup
//       recovery, exactly once per pass.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

// TestAppendExemptsStuckReceiptsFromAgePrune pins B8: a pending receipt
// older than DeliveryHistoryRetentionDays survives Append; a delivered
// receipt past retention is still pruned.
func TestAppendExemptsStuckReceiptsFromAgePrune(t *testing.T) {
	old := time.Now().UTC().AddDate(0, 0, -(DeliveryHistoryRetentionDays + 5))
	fresh := time.Now().UTC()

	stuckOld := ScheduleDeliveryReceipt{
		ScheduleID: "job-b8", ScheduledAt: old, OccurrenceAt: old,
		MessageID: "turn-stuck-old", Status: DeliveryPending,
	}
	deliveredOld := ScheduleDeliveryReceipt{
		ScheduleID: "job-b8", ScheduledAt: old, OccurrenceAt: old,
		DeliveredAt: old, MessageID: "turn-delivered-old", Status: DeliveryDelivered,
	}
	inFlightOld := ScheduleDeliveryReceipt{
		ScheduleID: "job-b8", ScheduledAt: old, OccurrenceAt: old,
		MessageID: "turn-inflight-old", Status: DeliveryInFlight,
	}
	freshDelivered := ScheduleDeliveryReceipt{
		ScheduleID: "job-b8", ScheduledAt: fresh, OccurrenceAt: fresh,
		DeliveredAt: fresh, MessageID: "turn-fresh", Status: DeliveryDelivered,
	}

	h := NewDeliveryHistory()
	h.Append(stuckOld)
	h.Append(deliveredOld)
	h.Append(inFlightOld)
	h.Append(freshDelivered)

	got := map[string]bool{}
	for _, r := range h.Records {
		got[r.MessageID] = true
	}
	if !got["turn-stuck-old"] {
		t.Error("pending receipt older than retention was pruned — recovery loses the tick (B8)")
	}
	if !got["turn-inflight-old"] {
		t.Error("in_flight receipt older than retention was pruned — recovery loses the tick (B8)")
	}
	if got["turn-delivered-old"] {
		t.Error("delivered receipt past retention was kept — age retention must still bound delivered history")
	}
	if !h.EarlierRecordsPruned {
		t.Error("pruning the delivered record must set EarlierRecordsPruned")
	}
	if !got["turn-fresh"] {
		t.Error("fresh delivered receipt must survive")
	}
}

// TestRecoverClaimedTickWithoutReceipt pins B2: a claim row with no receipt
// (the crash window between ClaimTick and the first receipt write) is
// re-delivered exactly once by recovery, and a claim WITH a receipt is not
// double-delivered by this sweep.
func TestRecoverClaimedTickWithoutReceipt(t *testing.T) {
	msgBus := bus.New(nil, nil)
	tmpDir := t.TempDir()
	s, err := NewScheduler(config.SchedulerConfig{Enabled: true, Timezone: "UTC"}, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	defer func() { _ = s.Stop(context.Background()) }()

	// Job A: claimed tick, NO receipt (the crash window).
	// Job B: claimed tick WITH a delivered receipt (must not re-fire).
	for _, id := range []string{"job-b2-noReceipt", "job-b2-withReceipt"} {
		if _, err := s.ScheduleConfig(JobConfig{
			ID:       id,
			Name:     id,
			Type:     JobTypeAgent,
			Schedule: "@yearly",
			Enabled:  true,
			AgentConfig: &AgentJobConfig{
				Prompt:          "ping",
				TargetSessionID: "sess-b2-" + id,
			},
		}); err != nil {
			t.Fatalf("ScheduleConfig(%s): %v", id, err)
		}
	}

	tick := time.Now().UTC().Add(-time.Minute)
	for _, id := range []string{"job-b2-noReceipt", "job-b2-withReceipt"} {
		if claimed, err := s.ClaimTick(id, tick); err != nil || !claimed {
			t.Fatalf("ClaimTick(%s): claimed=%v err=%v", id, claimed, err)
		}
	}
	cfgB, _ := s.store.Get("job-b2-withReceipt")
	if err := s.recordDelivery("job-b2-withReceipt", ScheduleDeliveryReceipt{
		ScheduleID:      "job-b2-withReceipt",
		ScheduledAt:     tick,
		OccurrenceAt:    tick,
		DeliveredAt:     tick.Add(time.Second),
		MessageID:       "turn-b2-delivered",
		TargetSessionID: "sess-b2",
		Status:          DeliveryDelivered,
	}); err != nil {
		t.Fatalf("recordDelivery: %v", err)
	}
	_ = cfgB

	// Count chat.request deliveries while recovery runs. The bus closes
	// sub.Channel on Unsubscribe, so the drain must tolerate a closed
	// channel instead of panicking on it.
	sub := msgBus.Subscribe("b2-count", "chat.request")

	var mu sync.Mutex
	gotTurns := map[string]int{}
	go func() {
		for msg := range sub.Channel {
			if strings.Contains(string(msg.Payload), "job-b2") {
				mu.Lock()
				gotTurns[string(msg.Payload)]++
				mu.Unlock()
			}
		}
	}()
	defer func() {
		msgBus.Unsubscribe(sub)
		time.Sleep(1500 * time.Millisecond)
	}()

	if err := s.recoverClaimedTicksWithoutReceipt(); err != nil {
		t.Fatalf("recoverClaimedTicksWithoutReceipt: %v", err)
	}
	// One recovery pass must dispatch the no-receipt tick exactly once.
	// dispatchMissed runs async; give Execute a moment, then drain.
	time.Sleep(500 * time.Millisecond)

	// Idempotence within a pass: a second call sees the same claim (still
	// no receipt for the in-flight delivery until Execute completes) — the
	// at-least-once guarantee is per-pass, and this pass re-delivers once.
	// The pin here is that the sweep found and dispatched the claim at all.
	claims, err := s.claims.ListClaimsSince(tick.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListClaimsSince: %v", err)
	}
	if len(claims) != 2 {
		t.Fatalf("claims visible to recovery = %d, want 2", len(claims))
	}
	// The with-receipt claim is filtered out by hasReceiptForTick.
	cfgBNow, _ := s.store.Get("job-b2-withReceipt")
	if !hasReceiptForTick(cfgBNow, tick) {
		t.Fatal("hasReceiptForTick failed to match a delivered receipt — the sweep would double-deliver it")
	}

	time.Sleep(1500 * time.Millisecond)
	mu.Lock()
	total := 0
	noReceiptDeliveries := 0
	for payload, n := range gotTurns {
		total += n
		if strings.Contains(payload, "job-b2-noReceipt") {
			noReceiptDeliveries += n
		}
	}
	mu.Unlock()
	if total == 0 {
		t.Fatal("recovery delivered nothing — the claimed tick was lost (B2)")
	}
	if noReceiptDeliveries != 1 {
		t.Errorf("no-receipt claim delivered %d times in one pass, want exactly 1", noReceiptDeliveries)
	}
}
