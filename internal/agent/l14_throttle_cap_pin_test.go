package agent

// L14 pins (bughunt 2026-11-24): pre-fix throttle rows persisted BEFORE the
// cap landed (the encode-side stamp of DefaultThrottleMaxAttempts=5 covers
// NEW parks only) carry MaxAttempts=0, so the drain-side give-up condition
// (turn.MaxAttempts > 0 && turn.Attempt >= turn.MaxAttempts, parked_turn.go
// drainDue) never fires and the row replays an always-blocked endpoint
// forever — the exact silent-forever turn the cap exists to kill, via the
// legacy-residue half of the same bug class.
//
// The fix is migration-at-load in TurnParker.reArm: a class=throttle record
// re-armed with MaxAttempts==0 gets the cap stamped (Debug-logged once per
// row), so the EXISTING M-4-park machinery terminalizes it at the cap.
// Class-gated: quota rows keep their own lane semantics byte-identically,
// and none/unknown-class rows are never reinterpreted (C-K1 discipline).
//
// The give-up TERMINAL consumer (ChatHandler.giveUpThrottleParkedTurn in the
// sibling-dirty handler.go) is NOT asserted here — these pins assert the
// parker-side lane it subscribes to: the SetGiveUpFunc callback plus the
// throttle_give_up event on agent.quota_wait (exactly what
// parker.SetGiveUpFunc(h.giveUpThrottleParkedTurn) consumes at HEAD).

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
)

// l14ThrottlePayload is a minimal class=throttle TurnPayload carrying the
// turn identity the real give-up terminal lane needs (turn_id rides the
// payload, C-K2).
const l14ThrottlePayload = `{"message":"hello","conversation_id":"conv-l14","turn_id":"turn-l14"}`

func newL14StoreParker(t *testing.T, store ParkPersistence, owner string, resume func(context.Context, ParkedTurnRecord)) *TurnParker {
	t.Helper()
	p := NewTurnParker(newParkStoreTestLogger(), resume, time.Hour)
	p.SetPollInterval(10 * time.Millisecond)
	p.SetParkPersistence(store, ParkKindChat)
	p.SetPersistenceOwner(owner)
	return p
}

// --- Pin (i): a legacy throttle row (MaxAttempts=0) loads -> stamped -> the
// drain gives up at the cap instead of replaying forever, observable on
// BOTH existing lanes: the SetGiveUpFunc callback (the handler's terminal
// seam) and the throttle_give_up event on agent.quota_wait.
func TestL14_LegacyThrottleRowStampedAtLoad_GivesUpAtCap(t *testing.T) {
	store := newTestParkStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	// Seed the exact pre-fix shape: class=throttle, Attempt already past
	// the cap's boundary (what a multi-hour endpoint-block re-park streak
	// accumulates), MaxAttempts=0 (persisted before the cap existed).
	seed := ParkedTurnRecord{
		SessionID:      "sess-l14",
		ConversationID: "conv-l14",
		AgentID:        "emp-l14",
		Class:          llm.FailureThrottle,
		ResumeAt:       time.Now().Add(30 * time.Millisecond),
		Attempt:        5,
		MaxAttempts:    0,
		TurnPayload:    []byte(l14ThrottlePayload),
		Owner:          ParkOwnerThrottle,
	}
	if err := store.Save(ctx, ParkKindChat, "l14-throttle", seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	b := bus.New(nil, newParkStoreTestLogger())
	spy := newParkEventSpy(b)

	var mu sync.Mutex
	var resumed []ParkedTurnRecord
	var gaveUp []ParkedTurnRecord
	p := newL14StoreParker(t, store, ParkOwnerThrottle, func(_ context.Context, rec ParkedTurnRecord) {
		mu.Lock()
		resumed = append(resumed, rec)
		mu.Unlock()
	})
	p.SetParkEventBus(b)
	p.SetGiveUpFunc(func(_ context.Context, rec ParkedTurnRecord) {
		mu.Lock()
		gaveUp = append(gaveUp, rec)
		mu.Unlock()
	})
	p.Start(ctx)
	defer p.Stop()

	waitUntil(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(gaveUp) == 1
	})

	mu.Lock()
	g := gaveUp[0]
	resumeCount := len(resumed)
	mu.Unlock()

	// The stamped record reached the give-up callback (the handler's
	// terminal lane) with the load-time cap visible on the record.
	if g.Class != llm.FailureThrottle {
		t.Errorf("give-up class = %v, want FailureThrottle", g.Class)
	}
	if g.MaxAttempts != DefaultLegacyThrottleMaxAttempts {
		t.Errorf("give-up MaxAttempts = %d, want the load-time stamp (%d)", g.MaxAttempts, DefaultLegacyThrottleMaxAttempts)
	}
	if g.Attempt < g.MaxAttempts {
		t.Errorf("give-up Attempt = %d, want >= stamped cap", g.Attempt)
	}
	// The stamp closes the replay hole: the record NEVER reaches the resume
	// callback (no LLM replay past the cap).
	if resumeCount != 0 {
		t.Errorf("resume callback ran %d times, want 0 (capped at load-time stamp)", resumeCount)
	}
	// Row deleted at the drain (at-most-once) — it cannot resurrect and
	// replay forever on the next boot.
	waitUntil(t, 2*time.Second, func() bool {
		return store.persistedRowCount(t, ParkKindChat) == 0
	})

	// The give-up event rides the existing agent.quota_wait lane with the
	// throttle vocabulary (identical to what capped NEW parks emit).
	events := spy.collect(t, 300*time.Millisecond)
	b.Unsubscribe(spy.sub)
	var sawGiveUp bool
	for _, ev := range events {
		if reason, _ := ev["reason"].(string); reason == ReasonThrottleGiveUp {
			sawGiveUp = true
			if class, _ := ev["class"].(string); class != "throttle" {
				t.Errorf("give-up event class = %v, want throttle", class)
			}
		}
	}
	if !sawGiveUp {
		t.Errorf("no %s event on agent.quota_wait (%d events collected)", ReasonThrottleGiveUp, len(events))
	}
}

// --- Pin (ii): a quota-class row with MaxAttempts=0 is NOT stamped. The
// quota lane has its own give-up semantics (giveUpQuotaParkedTurn) and its
// own cap stamped at the quota park site; a legacy quota row keeps
// byte-identical unlimited-replay behaviour through load -> drain.
func TestL14_LegacyQuotaRowNotStamped_ByteIdenticalReplay(t *testing.T) {
	store := newTestParkStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	seed := ParkedTurnRecord{
		SessionID:      "sess-l14q",
		ConversationID: "conv-l14q",
		AgentID:        "emp-l14q",
		Class:          llm.FailureQuota,
		ResumeAt:       time.Now().Add(30 * time.Millisecond),
		Attempt:        5,
		MaxAttempts:    0,
		TurnPayload:    []byte(`{"message":"q","conversation_id":"conv-l14q"}`),
		Owner:          ParkOwnerQuotaWatcher,
	}
	if err := store.Save(ctx, ParkKindChat, "l14-quota", seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var mu sync.Mutex
	var resumed []ParkedTurnRecord
	var gaveUp int
	p := newL14StoreParker(t, store, ParkOwnerQuotaWatcher, func(_ context.Context, rec ParkedTurnRecord) {
		mu.Lock()
		resumed = append(resumed, rec)
		mu.Unlock()
	})
	p.SetGiveUpFunc(func(context.Context, ParkedTurnRecord) {
		mu.Lock()
		gaveUp++
		mu.Unlock()
	})
	p.Start(ctx)
	defer p.Stop()

	waitUntil(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(resumed) == 1
	})
	mu.Lock()
	rec := resumed[0]
	giveUpCount := gaveUp
	mu.Unlock()

	if rec.Class != llm.FailureQuota {
		t.Fatalf("resumed class = %v, want quota", rec.Class)
	}
	if rec.MaxAttempts != 0 {
		t.Errorf("quota row MaxAttempts = %d after load, want 0 (NO stamp — quota lane keeps byte-identical replay)", rec.MaxAttempts)
	}
	if giveUpCount != 0 {
		t.Errorf("give-up fired %d times for a quota row, want 0 (unlimited replay preserved)", giveUpCount)
	}
}

// --- Pin (ii-b): the stamp is class-gated and non-destructive at the source:
// a throttle row that ALREADY carries a cap keeps its own value (no
// override), and a class=none row with MaxAttempts=0 is never stamped (not a
// throttle park — C-K1: reArm must not fabricate cap behaviour for foreign
// rows). Both rows sit beyond the resume window: reArm queues them without
// draining, so the in-memory queue (same package) is the observation point.
func TestL14_StampIsClassGatedAndNonDestructive(t *testing.T) {
	store := newTestParkStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	if err := store.Save(ctx, ParkKindChat, "l14-capped", ParkedTurnRecord{
		SessionID:   "sess-l14c",
		Class:       llm.FailureThrottle,
		ResumeAt:    time.Now().Add(time.Hour),
		Attempt:     1,
		MaxAttempts: 2,
		TurnPayload: []byte(l14ThrottlePayload),
		Owner:       ParkOwnerThrottle,
	}); err != nil {
		t.Fatalf("seed capped: %v", err)
	}
	if err := store.Save(ctx, ParkKindChat, "l14-none", ParkedTurnRecord{
		SessionID:   "sess-l14n",
		Class:       llm.FailureNone,
		ResumeAt:    time.Now().Add(time.Hour),
		Attempt:     0,
		MaxAttempts: 0,
		TurnPayload: []byte(`{"kind":"budget"}`),
		Owner:       ParkOwnerThrottle,
	}); err != nil {
		t.Fatalf("seed none: %v", err)
	}

	p := newL14StoreParker(t, store, ParkOwnerThrottle, func(context.Context, ParkedTurnRecord) {})
	p.Start(ctx)
	defer p.Stop()

	waitUntil(t, 2*time.Second, func() bool { return p.Pending() == 2 })

	p.mu.Lock()
	queue := append([]ParkedTurnRecord(nil), p.parked...)
	p.mu.Unlock()

	bySess := map[string]ParkedTurnRecord{}
	for _, rec := range queue {
		bySess[rec.SessionID] = rec
	}
	if capped := bySess["sess-l14c"]; capped.MaxAttempts != 2 {
		t.Errorf("already-capped throttle row MaxAttempts = %d, want 2 (stamp must not override an existing cap)", capped.MaxAttempts)
	}
	if none := bySess["sess-l14n"]; none.MaxAttempts != 0 {
		t.Errorf("class=none row MaxAttempts = %d, want 0 (never stamped — C-K1: no cap behaviour fabricated for foreign rows)", none.MaxAttempts)
	}
	// reArm must not delete live rows.
	if n := store.persistedRowCount(t, ParkKindChat); n != 2 {
		t.Errorf("persisted rows = %d, want 2 (reArm must not delete live rows)", n)
	}
}
