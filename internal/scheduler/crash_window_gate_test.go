package scheduler

// Crash-window gate tests (plan 20261109-statemachine-remediation, leaf
// 02-scheduler/00T, audit M-5 / scheduler F5). Pins contracts C-S1 + the
// advance-ordering half of the orchestrator:
//
//   - TestAdvanceLastWakeAfterDelivery: executeJob must NOT advance the
//     persisted last-wake watermark when job.Execute fails. Today
//     scheduler.go advances lastWake BEFORE Execute (claim -> advance ->
//     execute), so a failed tick silently removes the occurrence from the
//     post-crash catch-up window's reach -> RED.
//   - TestRecoverInFlightWalksHistory: recovery must walk the FULL
//     DeliveryHistory and re-deliver EVERY pending/in_flight receipt.
//     Today recoverInFlightDeliveries inspects cfg.LastDelivery only, so a
//     stuck earlier receipt hidden behind a later delivered receipt is
//     never re-delivered -> RED.
//
// These are gate tests: they must fail for missing-implementation reasons
// until leaf 01-crash-window.md lands. No production code lives here.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

// gateJob is the package's fake-executable Job for the crash-window gate:
// Execute returns the injected error (or nil) and records invocations.
type gateJob struct {
	id       string
	schedule string
	execErr  error
	runs     int
}

func (j *gateJob) ID() string       { return j.id }
func (j *gateJob) Name() string     { return "crash-window gate job" }
func (j *gateJob) Schedule() string { return j.schedule }
func (j *gateJob) Type() JobType    { return JobTypeShell }
func (j *gateJob) Config() JobConfig {
	return JobConfig{
		ID:       j.id,
		Name:     j.Name(),
		Type:     JobTypeShell,
		Schedule: j.schedule,
		Enabled:  true,
	}
}
func (j *gateJob) Execute(_ context.Context) error {
	j.runs++
	return j.execErr
}

func newGateScheduler(t *testing.T) (*Scheduler, *bus.MessageBus) {
	t.Helper()
	msgBus := bus.New(nil, nil)
	s, err := NewScheduler(config.SchedulerConfig{Enabled: true, Timezone: "UTC"},
		msgBus, WithDataDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	return s, msgBus
}

// TestAdvanceLastWakeAfterDelivery pins the Q4-default ordering: the
// last-wake watermark moves forward only after a successful delivery, so a
// failed (or crash-interrupted) tick stays inside the catch-up window.
func TestAdvanceLastWakeAfterDelivery(t *testing.T) {
	fakeErr := errors.New("injected execute failure")

	cases := []struct {
		name     string
		execErr  error
		wantAdvc bool
	}{
		{"failed execute leaves watermark", fakeErr, false},
		{"successful execute advances watermark", nil, true},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newGateScheduler(t)
			ctx := context.Background()

			// Anchor the watermark well before the tick we are about to
			// deliver so the advance (if any) is observable.
			base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
			if err := s.SetLastWake(base); err != nil {
				t.Fatalf("SetLastWake(base): %v", err)
			}

			tick := base.Add(time.Duration(90+i) * time.Minute).Truncate(time.Second)
			job := &gateJob{id: "crash-window-advance", schedule: "@every 1h", execErr: tc.execErr}

			// Drive the scheduled-fire path directly with an explicit tick
			// (what wrapJob does on a cron fire; explicitTick=true is the
			// gate condition around advanceLastWake in executeJob).
			s.executeJob(ctx, job, ExecutionOptions{Tick: tick})

			if job.runs != 1 {
				t.Fatalf("Execute runs = %d, want 1", job.runs)
			}

			got, err := s.GetLastWake()
			if err != nil {
				t.Fatalf("GetLastWake: %v", err)
			}
			if tc.wantAdvc {
				if !got.Equal(tick) {
					t.Errorf("lastWake after SUCCESSFUL delivery = %v, want advanced to tick %v", got, tick)
				}
				return
			}
			// M-5 gate: a failed tick must NOT consume the catch-up window.
			if !got.Equal(base) {
				t.Errorf("lastWake advanced to %v after FAILED Execute (want unchanged %v): "+
					"a crash/failure between claim and delivery silently drops the occurrence "+
					"from ProcessMissedTicks — advance must happen only after successful delivery",
					got, base)
			}
		})
	}
}

// TestRecoverInFlightWalksHistory pins C-S1: recovery walks the whole
// DeliveryHistory, re-delivering every pending/in_flight receipt exactly
// once (same MessageID, turn_id-dedupe idempotent), and never touching a
// delivered receipt. Today recovery reads cfg.LastDelivery only, so the
// stuck earlier receipt hidden behind a later delivered receipt is lost.
func TestRecoverInFlightWalksHistory(t *testing.T) {
	s, msgBus := newGateScheduler(t)

	if _, err := s.ScheduleConfig(JobConfig{
		ID:       "job-walk-history",
		Name:     "Walk History Job",
		Type:     JobTypeAgent,
		Schedule: "@yearly",
		Enabled:  true,
		AgentConfig: &AgentJobConfig{
			Prompt:          "walk the history",
			TargetSessionID: "sess-walk-1",
		},
	}); err != nil {
		t.Fatalf("ScheduleConfig: %v", err)
	}

	now := time.Now().UTC()

	// Receipt A: an earlier occurrence stuck in_flight (crash between
	// publish and session ack).
	stuckA := ScheduleDeliveryReceipt{
		ScheduleID:      "job-walk-history",
		ScheduledAt:     now.Add(-2 * time.Hour),
		OccurrenceAt:    now.Add(-2 * time.Hour),
		MessageID:       "turn-stuck-a",
		TargetSessionID: "sess-walk-1",
		Prompt:          "walk the history",
		Status:          DeliveryInFlight,
	}
	// Receipt B: a LATER occurrence that reached delivered — it mirrors
	// into LastDelivery, which is all the current recovery implementation
	// inspects, hiding stuck receipt A.
	deliveredB := ScheduleDeliveryReceipt{
		ScheduleID:      "job-walk-history",
		ScheduledAt:     now.Add(-time.Hour),
		OccurrenceAt:    now.Add(-time.Hour),
		DeliveredAt:     now.Add(-time.Hour),
		MessageID:       "turn-delivered-b",
		TargetSessionID: "sess-walk-1",
		Prompt:          "walk the history",
		Status:          DeliveryDelivered,
	}
	if err := s.recordDelivery("job-walk-history", stuckA); err != nil {
		t.Fatalf("recordDelivery(stuckA): %v", err)
	}
	if err := s.recordDelivery("job-walk-history", deliveredB); err != nil {
		t.Fatalf("recordDelivery(deliveredB): %v", err)
	}

	// Sanity: the fixture really is the interleaved shape the leaf
	// requires — two receipts in history, newest mirror already delivered.
	jc, ok := s.Store().Get("job-walk-history")
	if !ok {
		t.Fatal("job-walk-history not in store")
	}
	if jc.DeliveryHistory == nil || len(jc.DeliveryHistory.Records) != 2 {
		t.Fatalf("DeliveryHistory len = %v, want 2 records (fixture build failed)",
			jc.DeliveryHistory)
	}
	if jc.LastDelivery == nil || jc.LastDelivery.Status != DeliveryDelivered {
		t.Fatalf("LastDelivery = %+v, want the delivered receipt B (fixture build failed)", jc.LastDelivery)
	}

	// Recording fake: count chat.request deliveries per turn_id so we can
	// assert exactly-one for the stuck receipt and zero for delivered B.
	sub := msgBus.Subscribe("crash-window-gate", "chat.request")
	defer msgBus.Unsubscribe(sub)

	if err := s.recoverInFlightDeliveries(); err != nil {
		t.Fatalf("recoverInFlightDeliveries: %v", err)
	}

	counts := map[string]int{}
	deadline := time.After(2 * time.Second)
collect:
	for {
		// Drain whatever arrived; a short idle timeout bounds the wait for
		// any over-delivery too.
		select {
		case msg, okRecv := <-sub.Channel:
			if !okRecv {
				break collect
			}
			var payload map[string]any
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			turnID, _ := payload["turn_id"].(string)
			counts[turnID]++
		case <-time.After(150 * time.Millisecond):
			break collect
		case <-deadline:
			break collect
		}
	}

	// C-S1 gate: EVERY pending/in_flight receipt is re-delivered exactly
	// once with its same MessageID.
	if counts["turn-stuck-a"] != 1 {
		t.Errorf("stuck receipt A (in_flight, hidden behind newer delivered B): re-delivered %d time(s), want exactly 1 — "+
			"recovery must walk DeliveryHistory, not inspect LastDelivery only", counts["turn-stuck-a"])
	}
	// No duplicate-delivery regression and the delivered occurrence is
	// never re-sent.
	if n := counts["turn-delivered-b"]; n != 0 {
		t.Errorf("delivered receipt B re-delivered %d time(s), want 0", n)
	}
	for turnID, n := range counts {
		if n > 1 {
			t.Errorf("turn_id %q delivered %d times in one recovery run, want <=1", turnID, n)
		}
	}

	// After a successful recovery run the stuck receipt must have moved to
	// delivered in the persisted history.
	jcAfter, ok := s.Store().Get("job-walk-history")
	if !ok {
		t.Fatal("job-walk-history vanished from store")
	}
	foundA := false
	if jcAfter.DeliveryHistory != nil {
		for _, r := range jcAfter.DeliveryHistory.Records {
			if r.MessageID == "turn-stuck-a" && r.Status == DeliveryDelivered {
				foundA = true
			}
		}
	}
	if !foundA {
		t.Errorf("receipt A not persisted as delivered after recovery (history: %+v)", jcAfter.DeliveryHistory)
	}
}
