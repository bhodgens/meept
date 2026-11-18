package agent

// Parked-turn gate tests (statemachine-remediation tree 04 leaf 00T).
// TESTS ONLY — these pin the TARGET contracts C-K1/C-K2/C-K3 and are
// EXPECTED TO FAIL RED (compile: undefined new symbols; behavior where the
// symbols exist but semantics are not yet implemented). Implementation
// leaves 01..04 must land these GREEN without editing this file
// ("Gate tests GREEN, untouched").
//
// Findings pinned:
//   C1 (CRITICAL): two parkers (quota watcher via SetParkPersistence
//       forwarding ParkKindChat; throttle parker wired with
//       SetParkPersistence(store, ParkKindChat) in daemon/components.go)
//       persist under the SAME kind and store Load is kind-scoped only —
//       a restart re-arms BOTH rows into BOTH parkers → double execution.
//       Killers: TestLoadReturnsOnlyOwnedRows + TestRestartRearmSingleOwner.
//   M-12: gate parks strand on plan rejection  → GateParker.ReleaseByKey.
//   M-13: throttle resume discards the result  → delivery parity (persist +
//       push + turn.terminal) with throttledTurnPayload.TurnID.
//   M-4-park: unbounded quota re-park          → Attempt/MaxAttempts + give-up.
//   F5: budget park persistence + events.
//   F6: parked-turn detection via explicit marker (nil state machine).
//   F8: throttle dispatch stash shared across turns → per-turn stash.
//   F9: ParkGate re-raise appends a duplicate record → replaces.
//   strict decode: malformed/foreign payloads into the quota resume path
//       must DROP (never misroute — the overlap that makes C1 dangerous).
//
// Target symbols referenced (RED until leaves 01/03/04 land):
//   ParkedTurnRecord.Owner; (*TurnParker).SetPersistenceOwner;
//   (*SQLiteParkStore).SetOwnerFilter; (*QuotaResumeWatcher).SetPersistenceOwner;
//   QuotaParkedTurn.Attempt/.MaxAttempts + DefaultQuotaMaxAttempts;
//   throttleParkedTurn.TurnID; (*GateParker).ReleaseByKey(gateID, reason)
//   (bool, error); (*BudgetResumeWatcher).SetParkPersistence /
//   .SetPersistenceOwner / .SetParkEventBus.
//
// Rig reuse: fakeNowFunc/testClock/waitUntil (parked_turn_test.go),
// fakePersistence/newTestParkStore/persistedRowCount (park_store_sqlite_test.go),
// countingResume/gateData/newGateTestParker (gate_park_test.go),
// parkEventSpy (park_turn_events_test.go), scriptedToggleChatter +
// throttleFailurePolicyForTests (loop_throttle_*_test.go).

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/security"
)

// --- helpers ----------------------------------------------------------------

// exchangeSpySaver records every persistExchange write (SessionMessageSaver).
type exchangeSpySaver struct {
	mu    sync.Mutex
	saved [][]session.Message
}

func (s *exchangeSpySaver) SaveMessages(sessionID string, msgs []session.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved = append(s.saved, msgs)
	return nil
}

func (s *exchangeSpySaver) batches() [][]session.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]session.Message, len(s.saved))
	copy(out, s.saved)
	return out
}

// decodePayloadMap decodes a bus payload into a generic map (test-side key
// inspection without coupling to struct internals).
func decodePayloadMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("payload unmarshal: %v (raw %s)", err, raw)
	}
	return m
}

// quotaGateRecord builds a quota chat row payload for store/parker seeding.
func quotaGateRecord(sessionID string, resumeAt time.Time) ParkedTurnRecord {
	return ParkedTurnRecord{
		ConversationID: "conv-" + sessionID,
		SessionID:      sessionID,
		AgentID:        "gate-test-agent",
		Class:          llm.FailureQuota,
		ResumeAt:       resumeAt,
		TurnPayload: json.RawMessage(`{
			"message":"q",
			"conversation_id":"conv-` + sessionID + `",
			"provider_id":"p1",
			"credential_key":"k1",
			"source_client":"tui",
			"parked_at":"` + resumeAt.Add(-time.Hour).Format(time.RFC3339) + `"
		}`),
	}
}

// throttleGateRecord builds a throttle chat row payload for store/parker
// seeding. NOTE the overlapping quota keys (message/conversation_id/
// provider_id/credential_key/source_client/parked_at) — the exact shape
// that decodes "successfully" into quotaTurnPayload and makes C1 dangerous.
func throttleGateRecord(sessionID string, resumeAt time.Time) ParkedTurnRecord {
	return ParkedTurnRecord{
		ConversationID: "conv-" + sessionID,
		SessionID:      sessionID,
		AgentID:        "gate-test-agent",
		Class:          llm.FailureThrottle,
		ResumeAt:       resumeAt,
		TurnPayload: json.RawMessage(`{
			"message":"t",
			"conversation_id":"conv-` + sessionID + `",
			"provider_id":"p1",
			"credential_key":"k1",
			"source_client":"cli",
			"model_id":"m1",
			"parked_at":"` + resumeAt.Add(-time.Hour).Format(time.RFC3339) + `"
		}`),
	}
}

func ownersOf(recs []ParkedTurnRecord) []string {
	var out []string
	for _, r := range recs {
		out = append(out, r.Owner)
	}
	return out
}

func firstParked(t *testing.T, p *TurnParker) ParkedTurnRecord {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.parked) == 0 {
		t.Fatal("parker has no parked records")
	}
	return p.parked[0]
}

// newOwnerStoreParker opens a parker over the shared parks.db file wired
// with ParkKindChat persistence + a persistence owner (C-K1 wiring shape).
func newOwnerStoreParker(t *testing.T, dbPath, owner string, resume func(context.Context, ParkedTurnRecord)) *TurnParker {
	t.Helper()
	store, err := NewSQLiteParkStore(dbPath, newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("open store handle: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	p := NewTurnParker(newParkStoreTestLogger(), resume, time.Hour)
	p.SetPollInterval(10 * time.Millisecond)
	p.SetParkPersistence(store, ParkKindChat)
	p.SetPersistenceOwner(owner)
	return p
}

// newDeliveryRig wires loop + chat handler + class-router parker exactly
// like newThrottleResumeLoop (loop_throttle_resume_test.go) but with the
// handler on a REAL bus + an exchange-spy saver, so the M-13 delivery
// targets (persistExchange / client push / turn.terminal) are observable.
func newDeliveryRig(t *testing.T, chatter llm.Chatter) (*AgentLoop, *ChatHandler, *TurnParker, *testClock, *bus.MessageBus, *exchangeSpySaver) {
	t.Helper()
	throttleFailurePolicyForTests(t, time.Second)
	b := bus.New(nil, testLogger())
	loop := NewAgentLoop("sess-park-owner-gate", t.TempDir(),
		WithMessageBus(b),
		WithLLMChatter(chatter),
	)
	loop.security = security.NewPermissionChecker(security.Config{})
	handler := NewChatHandler(loop, nil, b, testLogger())
	saver := &exchangeSpySaver{}
	handler.SetMessageSaver(saver)
	clock := newTestClock(time.Now())
	parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, 24*time.Hour)
	parker.SetPollInterval(10 * time.Millisecond)
	parker.nowFunc = clock.nowFn
	parker.testDrainGate = clock.WaitUntil
	handler.SetThrottleParker(parker)
	loop.SetClock(clock.nowFn)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	parker.Start(ctx)
	return loop, handler, parker, clock, b, saver
}

// --- 1. TestLoadReturnsOnlyOwnedRows (C-K1, kills C1) ------------------------

// A store Save must persist the record's Owner; Load with a SetOwnerFilter
// must return ONLY that owner's rows; two parkers wired with distinct
// owners against the same store load kind=chat DISJOINTLY.
func TestLoadReturnsOnlyOwnedRows(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "parks.db")
	store, err := NewSQLiteParkStore(dbPath, newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// Two rows under the SAME kind (chat), owned by different parkers —
	// the daemon's C1 shape (quota watcher + throttle parker share one
	// store and one kind).
	resumeAt := time.Now().Add(time.Hour)
	quotaRec := quotaGateRecord("s-quota", resumeAt)
	thrRec := throttleGateRecord("s-throttle", resumeAt)
	quotaRec.Owner = "quota-watcher"
	thrRec.Owner = "throttle"
	if err := store.Save(ctx, ParkKindChat, "k-quota", quotaRec); err != nil {
		t.Fatalf("save quota row: %v", err)
	}
	if err := store.Save(ctx, ParkKindChat, "k-throttle", thrRec); err != nil {
		t.Fatalf("save throttle row: %v", err)
	}

	// Unfiltered Load: both rows, owners round-trip.
	recs, _, err := store.Load(ctx, ParkKindChat, time.Now())
	if err != nil {
		t.Fatalf("load unfiltered: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("unfiltered load = %d rows, want 2", len(recs))
	}
	byOwner := map[string]bool{}
	for _, r := range recs {
		byOwner[r.Owner] = true
	}
	if !byOwner["quota-watcher"] || !byOwner["throttle"] {
		t.Errorf("owner column did not round-trip: owners %v (want quota-watcher + throttle)", ownersOf(recs))
	}

	// Owner-filtered Load: each owner view is exactly its own rows.
	store.SetOwnerFilter("quota-watcher")
	recs, _, err = store.Load(ctx, ParkKindChat, time.Now())
	if err != nil {
		t.Fatalf("load quota-watcher: %v", err)
	}
	if len(recs) != 1 || recs[0].Owner != "quota-watcher" {
		t.Fatalf("filtered load = %d rows owners %v, want exactly 1 quota-watcher row", len(recs), ownersOf(recs))
	}
	store.SetOwnerFilter("throttle")
	recs, _, err = store.Load(ctx, ParkKindChat, time.Now())
	if err != nil {
		t.Fatalf("load throttle: %v", err)
	}
	if len(recs) != 1 || recs[0].Owner != "throttle" {
		t.Fatalf("filtered load = %d rows owners %v, want exactly 1 throttle row", len(recs), ownersOf(recs))
	}

	// Parker-level view: two fresh parkers with distinct owners over the
	// same file re-arm disjointly (kind=chat for both, like the daemon).
	pq := newOwnerStoreParker(t, dbPath, "quota-watcher", func(context.Context, ParkedTurnRecord) {})
	pt := newOwnerStoreParker(t, dbPath, "throttle", func(context.Context, ParkedTurnRecord) {})
	pq.Start(ctx)
	defer pq.Stop()
	pt.Start(ctx)
	defer pt.Stop()
	if got := pq.Pending(); got != 1 {
		t.Errorf("quota-watcher parker re-armed %d rows, want 1 (only its own)", got)
	}
	if got := pt.Pending(); got != 1 {
		t.Errorf("throttle parker re-armed %d rows, want 1 (only its own)", got)
	}
	if rec := firstParked(t, pq); rec.Class != llm.FailureQuota {
		t.Errorf("quota-watcher parker holds class %v, want quota", rec.Class)
	}
	if rec := firstParked(t, pt); rec.Class != llm.FailureThrottle {
		t.Errorf("throttle parker holds class %v, want throttle", rec.Class)
	}
}

// --- 2. TestRestartRearmSingleOwner (C1 killer, restart shape) ---------------

// One throttle row + one quota row persisted under ParkKindChat through the
// parkers' own Save path; after a simulated restart, fresh parkers with
// DISTINCT owners re-arm and each resume callback runs EXACTLY ONCE for its
// own row (callbacks counted per parker). At HEAD both parkers load both
// rows (Load is kind-scoped only) → each callback fires twice.
func TestRestartRearmSingleOwner(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "parks.db")
	store, err := NewSQLiteParkStore(dbPath, newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	quotaCB := atomic.Int32{}
	thrCB := atomic.Int32{}

	// Run 1: two parkers with distinct owners persist one row each via
	// Park (Save stamps the owner). Far-future schedule keeps both rows
	// on disk past Stop.
	base := time.Now()
	pq := NewTurnParker(newParkStoreTestLogger(), func(context.Context, ParkedTurnRecord) { quotaCB.Add(1) }, 2*time.Hour)
	pq.SetParkPersistence(store, ParkKindChat)
	pq.SetPersistenceOwner("quota-watcher")
	pq.nowFunc = func() time.Time { return base }
	pt := NewTurnParker(newParkStoreTestLogger(), func(context.Context, ParkedTurnRecord) { thrCB.Add(1) }, 2*time.Hour)
	pt.SetParkPersistence(store, ParkKindChat)
	pt.SetPersistenceOwner("throttle")
	pt.nowFunc = func() time.Time { return base }

	if !pq.Park(quotaGateRecord("s-q1", base.Add(90*time.Minute))) {
		t.Fatal("quota park refused")
	}
	if !pt.Park(throttleGateRecord("s-t1", base.Add(90*time.Minute))) {
		t.Fatal("throttle park refused")
	}
	if n := store.persistedRowCount(t, ParkKindChat); n != 2 {
		t.Fatalf("persisted chat rows = %d, want 2", n)
	}
	pq.Stop()
	pt.Stop()
	store.Close()

	// Run 2 (restart): fresh parkers, distinct owners, over the same file
	// and the same clock base — re-arm partitions the two rows, and
	// advancing past both resume windows drains each row exactly once, by
	// its owner only.
	clock := newTestClock(base)
	// The quota parker must NOT see the throttle row and vice versa.
	dueQ := NewTurnParker(newParkStoreTestLogger(), func(context.Context, ParkedTurnRecord) { quotaCB.Add(1) }, 2*time.Hour)
	sq, err := NewSQLiteParkStore(dbPath, newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer sq.Close()
	dueQ.SetParkPersistence(sq, ParkKindChat)
	dueQ.SetPersistenceOwner("quota-watcher")
	dueQ.SetPollInterval(10 * time.Millisecond)
	dueQ.nowFunc = clock.nowFn
	dueQ.testDrainGate = clock.WaitUntil

	dueT := NewTurnParker(newParkStoreTestLogger(), func(context.Context, ParkedTurnRecord) { thrCB.Add(1) }, 2*time.Hour)
	stt, err := NewSQLiteParkStore(dbPath, newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer stt.Close()
	dueT.SetParkPersistence(stt, ParkKindChat)
	dueT.SetPersistenceOwner("throttle")
	dueT.SetPollInterval(10 * time.Millisecond)
	dueT.nowFunc = clock.nowFn
	dueT.testDrainGate = clock.WaitUntil

	dueQ.Start(context.Background())
	dueT.Start(context.Background())
	defer dueQ.Stop()
	defer dueT.Stop()

	if got := dueQ.Pending(); got != 1 {
		t.Errorf("restart re-arm: quota parker holds %d rows, want 1 (its own only)", got)
	}
	if got := dueT.Pending(); got != 1 {
		t.Errorf("restart re-arm: throttle parker holds %d rows, want 1 (its own only)", got)
	}

	// Advance past both resume windows (scheduled at base+90m or earlier
	// after the MaxWait soft-stop); each owner drains ONLY its own row.
	clock.advance(2*time.Hour + time.Second)
	clock.proceed()
	waitUntil(t, 2*time.Second, func() bool {
		return quotaCB.Load()+thrCB.Load() >= 2
	})
	time.Sleep(150 * time.Millisecond) // let any (wrong) cross-resume surface
	if got := quotaCB.Load(); got != 1 {
		t.Errorf("quota-watcher resume callbacks = %d, want exactly 1 (its own row; C1 cross-feed = 2)", got)
	}
	if got := thrCB.Load(); got != 1 {
		t.Errorf("throttle resume callbacks = %d, want exactly 1 (its own row; C1 cross-feed = 2)", got)
	}

	// Both rows gone from disk after their drains (no resurrection).
	check, err := NewSQLiteParkStore(dbPath, newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("reopen check: %v", err)
	}
	defer check.Close()
	if n := check.persistedRowCount(t, ParkKindChat); n != 0 {
		t.Errorf("rows left after both drains = %d, want 0", n)
	}
}

// --- 3. TestThrottleResumeDeliversResult (M-13, C-K2) ------------------------

// throttledTurnPayload carries TurnID; after a parked throttle turn resumes,
// the result is delivered the way the quota resume delivers (handler
// resumeQuotaParkedTurn parity): exchange persisted + client push
// (chat_message) + turn.terminal under the parked TurnID, exactly once.
func TestThrottleResumeDeliversResult(t *testing.T) {
	chatter := &scriptedToggleChatter{reply: "throttle resume reply"}
	_, handler, parker, clock, b, saver := newDeliveryRig(t, chatter)

	pushSub := b.Subscribe("gate-chat-message", "chat_message")
	termSub := b.Subscribe("gate-turn-terminal", "turn.terminal")
	defer b.Unsubscribe(pushSub)
	defer b.Unsubscribe(termSub)

	// Encode the park THROUGH the payload builder: TurnID must ride the
	// payload (C-K2) and survive the round-trip.
	rec, err := throttleTurnToRecord(throttleParkedTurn{
		Message:        "hello throttled",
		ConversationID: "conv-deliver",
		SessionID:      "s-deliver",
		AgentID:        handler.loop.agentID,
		ModelID:        "m1",
		ProviderID:     "p1",
		TurnID:         "turn-deliver-1",
		ParkedAt:       clock.nowFn(),
	})
	if err != nil {
		t.Fatalf("throttleTurnToRecord: %v", err)
	}
	if !strings.Contains(string(rec.TurnPayload), `"turn_id":"turn-deliver-1"`) {
		t.Fatalf("payload missing turn_id key: %s", rec.TurnPayload)
	}
	round, err := recordToThrottleTurn(rec)
	if err != nil {
		t.Fatalf("recordToThrottleTurn: %v", err)
	}
	if round.TurnID != "turn-deliver-1" {
		t.Errorf("decoded TurnID = %q, want turn-deliver-1", round.TurnID)
	}

	rec.ResumeAt = clock.nowFn().Add(20 * time.Millisecond)
	if !parker.Park(rec) {
		t.Fatal("throttle park refused")
	}
	clock.advance(50 * time.Millisecond)
	clock.proceed()

	// (a) Exchange persisted with the resumed reply.
	waitUntil(t, 3*time.Second, func() bool {
		return len(saver.batches()) >= 1
	})
	var exchangeAssist string
	for _, batch := range saver.batches() {
		for _, m := range batch {
			if m.Role == "assistant" && m.Content != "" {
				exchangeAssist = m.Content
			}
		}
	}
	if exchangeAssist != "throttle resume reply" {
		t.Errorf("persistExchange assistant reply = %q, want the resumed reply (batches=%d)", exchangeAssist, len(saver.batches()))
	}

	// (b) Client push: chat_message carries the assistant reply.
	var pushed string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case msg := <-pushSub.Channel:
			m := decodePayloadMap(t, msg.Payload)
			if role, _ := m["role"].(string); role == "assistant" {
				pushed, _ = m["content"].(string)
			}
		case <-time.After(50 * time.Millisecond):
		}
		if pushed != "" {
			break
		}
	}
	if pushed == "" {
		t.Error("resumed throttle turn pushed no assistant chat_message to the client")
	}

	// (c) turn.terminal under the parked TurnID, exactly once.
	var term TurnTerminalEvent
	gotTerminal := false
	deadline2 := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline2) && !gotTerminal {
		select {
		case msg := <-termSub.Channel:
			var ev TurnTerminalEvent
			if err := json.Unmarshal(msg.Payload, &ev); err != nil {
				t.Fatalf("unmarshal turn.terminal: %v", err)
			}
			term = ev
			gotTerminal = true
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !gotTerminal {
		t.Fatal("resumed throttle turn emitted no turn.terminal event")
	}
	if term.TurnID != "turn-deliver-1" {
		t.Errorf("terminal TurnID = %q, want turn-deliver-1", term.TurnID)
	}
	if term.Status != "completed" {
		t.Errorf("terminal status = %q, want completed", term.Status)
	}
	if term.ConversationID != "conv-deliver" {
		t.Errorf("terminal conversation_id = %q, want conv-deliver", term.ConversationID)
	}
	select {
	case msg := <-termSub.Channel:
		t.Errorf("second turn.terminal emitted for one resume: %s", msg.Payload)
	case <-time.After(300 * time.Millisecond):
	}
}

// --- 4. TestThrottleResumeLegacyRow (C-K2 tolerance) -------------------------

// A payload WITHOUT turn_id (row persisted before C-K2) resumes without
// crash; the client delivery/terminal is skipped (legacy rows stay silent,
// warn logged), and the H3 marker still suppresses a duplicate user-message
// re-add.
func TestThrottleResumeLegacyRow(t *testing.T) {
	chatter := &scriptedToggleChatter{reply: "legacy reply"}
	loop, _, parker, clock, b, _ := newDeliveryRig(t, chatter)

	termSub := b.Subscribe("gate-legacy-terminal", "turn.terminal")
	pushSub := b.Subscribe("gate-legacy-push", "chat_message")
	defer b.Unsubscribe(termSub)
	defer b.Unsubscribe(pushSub)

	// Seed the conversation with the ORIGINAL user message (what the
	// parked attempt added) so a duplicate re-add is observable.
	conv := loop.SessionConversation("conv-legacy")
	conv.AddUserMessage("legacy hello")

	legacy := json.RawMessage(`{"message":"legacy hello","conversation_id":"conv-legacy","provider_id":"p1"}`)
	rec := ParkedTurnRecord{
		ConversationID: "conv-legacy",
		SessionID:      "s-legacy",
		AgentID:        "gate-test-agent",
		Class:          llm.FailureThrottle,
		ResumeAt:       clock.nowFn().Add(20 * time.Millisecond),
		TurnPayload:    legacy,
	}
	if !parker.Park(rec) {
		t.Fatal("legacy park refused")
	}
	clock.advance(50 * time.Millisecond)
	clock.proceed()

	// The turn re-ran without a panic.
	waitUntil(t, 3*time.Second, func() bool { return chatter.callCount() >= 1 })

	// No terminal / no client push for a legacy row (delivery skipped).
	select {
	case msg := <-termSub.Channel:
		t.Errorf("legacy row (no TurnID) emitted turn.terminal: %s", msg.Payload)
	case <-time.After(300 * time.Millisecond):
	}
	select {
	case msg := <-pushSub.Channel:
		t.Errorf("legacy row (no TurnID) pushed to the client: %s", msg.Payload)
	case <-time.After(300 * time.Millisecond):
	}

	// H3: the user message was not re-added.
	userAdds := 0
	for _, m := range conv.GetMessages() {
		if m.Role == llm.RoleUser && m.Content == "legacy hello" {
			userAdds++
		}
	}
	if userAdds != 1 {
		t.Errorf("user message count = %d, want 1 (no duplicate re-add)", userAdds)
	}
}

// --- 5. TestRejectReleasesGatePark (M-12, C-K3) ------------------------------

// GateParker.ReleaseByKey(gateID, reason) deletes the park row + the
// in-memory record and emits a lifecycle event carrying the release reason;
// a second call returns false; releasing an unknown gate is a no-op false.
func TestRejectReleasesGatePark(t *testing.T) {
	store := newTestParkStore(t)
	defer store.Close()
	b := bus.New(nil, testLogger())
	spy := newParkEventSpy(b)
	defer spy.stop(b)

	var resumed countingResume
	p := newGateTestParker(t, resumed.add)
	p.SetParkPersistence(store, ParkKindGate)
	p.SetPersistenceOwner("gate")
	p.SetParkEventBus(b)
	gp := NewGateParker(parkedTurnTestLogger(), p)

	if !gp.ParkGate(gateData("plan-reject")) {
		t.Fatal("ParkGate refused")
	}
	if n := store.persistedRowCount(t, ParkKindGate); n != 1 {
		t.Fatalf("gate rows before reject = %d, want 1", n)
	}

	// Release on plan rejection: row deleted + memory cleared + event.
	ok, err := gp.ReleaseByKey("plan-reject", "rejected")
	if err != nil {
		t.Fatalf("ReleaseByKey error: %v", err)
	}
	if !ok {
		t.Fatal("ReleaseByKey returned false for a parked gate")
	}
	if n := store.persistedRowCount(t, ParkKindGate); n != 0 {
		t.Errorf("gate row survived release: %d rows, want 0", n)
	}
	if n := gp.PendingGates(); n != 0 {
		t.Errorf("PendingGates after release = %d, want 0 (M-12 strand)", n)
	}
	// Release must NOT run the resume callback — rejection discards the
	// parked re-entry, it does not re-drive it.
	time.Sleep(150 * time.Millisecond)
	if n := resumed.count(); n != 0 {
		t.Errorf("release ran %d resume callbacks, want 0 (release discards)", n)
	}

	// Second release finds nothing: false, not an error.
	ok2, err2 := gp.ReleaseByKey("plan-reject", "rejected")
	if err2 != nil {
		t.Errorf("second ReleaseByKey error: %v, want nil (idempotent)", err2)
	}
	if ok2 {
		t.Error("second ReleaseByKey returned true, want false")
	}
	// Unknown gate id: no-op false (re-drive safety).
	if ok3, _ := gp.ReleaseByKey("never-parked", "cancelled"); ok3 {
		t.Error("ReleaseByKey of an unparked gate returned true")
	}

	// Event observability: the release publishes on the quota_wait topic
	// shape with a reason derived from the release reason.
	events := spy.collect(t, 500*time.Millisecond)
	if len(events) == 0 {
		t.Fatal("ReleaseByKey emitted no park-lifecycle event")
	}
	seenReject := false
	for _, ev := range events {
		if r, _ := ev["reason"].(string); strings.Contains(strings.ToLower(r), "reject") {
			seenReject = true
		}
	}
	if !seenReject {
		t.Errorf("no release event carried the rejection reason: %+v", events)
	}
}

// --- 6. TestQuotaReparkAttemptCap (M-4-park) ---------------------------------

// quotaTurnToRecord carries Attempt/MaxAttempts (default cap
// DefaultQuotaMaxAttempts); the parker gives up at the cap: NO resume
// callback (no further LLM call), the row is deleted, and a give-up event
// surfaces.
func TestQuotaReparkAttemptCap(t *testing.T) {
	// Encode-side contract: quotaTurnToRecord sets the cap and preserves
	// attempt growth across re-parks.
	turn := QuotaParkedTurn{
		SessionID:      "s-cap",
		ConversationID: "conv-cap",
		Message:        "hi",
		ProviderID:     "p1",
		CredentialKey:  "k1",
		UnblockAt:      time.Now().Add(time.Hour),
	}
	rec, err := quotaTurnToRecord(turn)
	if err != nil {
		t.Fatalf("quotaTurnToRecord: %v", err)
	}
	if rec.MaxAttempts != DefaultQuotaMaxAttempts {
		t.Errorf("default MaxAttempts = %d, want DefaultQuotaMaxAttempts (%d)", rec.MaxAttempts, DefaultQuotaMaxAttempts)
	}
	repark := turn
	repark.Attempt = rec.Attempt + 1
	rec2, err := quotaTurnToRecord(repark)
	if err != nil {
		t.Fatalf("re-park encode: %v", err)
	}
	if rec2.Attempt != rec.Attempt+1 {
		t.Errorf("re-park Attempt = %d, want %d (growth across re-parks)", rec2.Attempt, rec.Attempt+1)
	}
	if rec2.MaxAttempts != rec.MaxAttempts {
		t.Errorf("re-park MaxAttempts = %d, want it preserved (%d)", rec2.MaxAttempts, rec.MaxAttempts)
	}

	// Drain-side contract: a record at the cap must NOT run the resume
	// callback (no LLM replay); its row is deleted and a give-up event
	// surfaces on agent.quota_wait.
	store := newTestParkStore(t)
	defer store.Close()
	b := bus.New(nil, testLogger())
	spy := newParkEventSpy(b)
	defer spy.stop(b)

	llmCalls := atomic.Int32{}
	p := NewTurnParker(parkedTurnTestLogger(), func(context.Context, ParkedTurnRecord) { llmCalls.Add(1) }, time.Hour)
	p.SetPollInterval(10 * time.Millisecond)
	p.SetParkPersistence(store, ParkKindChat)
	p.SetPersistenceOwner("quota-watcher")
	p.SetParkEventBus(b)
	ctx := context.Background()
	p.Start(ctx)
	defer p.Stop()

	capped := quotaGateRecord("s-cap", time.Now().Add(30*time.Millisecond))
	capped.Attempt = DefaultQuotaMaxAttempts
	capped.MaxAttempts = DefaultQuotaMaxAttempts
	if !p.Park(capped) {
		t.Fatal("capped park refused")
	}
	// Past the resume window: give-up, not resume.
	time.Sleep(500 * time.Millisecond)
	if got := llmCalls.Load(); got != 0 {
		t.Errorf("resume ran %d LLM replays at the attempt cap, want 0 (give-up)", got)
	}
	if n := p.Pending(); n != 0 {
		t.Errorf("pending after cap give-up = %d, want 0 (record abandoned)", n)
	}
	if n := store.persistedRowCount(t, ParkKindChat); n != 0 {
		t.Errorf("capped row survived give-up: %d rows, want 0 (row deleted)", n)
	}
	events := spy.collect(t, 500*time.Millisecond)
	gaveUp := false
	for _, ev := range events {
		if r, _ := ev["reason"].(string); strings.Contains(strings.ToLower(r), "give_up") {
			gaveUp = true
		}
	}
	if !gaveUp {
		t.Errorf("cap give-up emitted no give-up-reason event: %+v", events)
	}
}

// --- 7. TestBudgetParkPersists (F5) -------------------------------------------

// BudgetResumeWatcher.SetParkPersistence(store, ParkKindChat) + owner
// "budget": the row survives a restart, is resumed by the BUDGET watcher
// once its window clears, and is NEVER loaded by the quota parker.
func TestBudgetParkPersists(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "parks.db")
	ctx := context.Background()

	exceeded := llm.NewBudget(llm.BudgetConfig{HourlyLimit: 1, Aggressiveness: 1.0}, nil)
	exceeded.RecordUsage(llm.TokenUsage{TotalTokens: 100})

	// Run 1: budget watcher parks with persistence + owner "budget".
	w1 := NewBudgetResumeWatcher(exceeded, newParkStoreTestLogger(), func(context.Context, ParkedTurn) {
		t.Error("budget resume fired before the restart")
	})
	st1, err := NewSQLiteParkStore(dbPath, newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	w1.SetParkPersistence(st1, ParkKindChat)
	w1.SetPersistenceOwner("budget")
	if !w1.Park(ParkedTurn{
		SessionID:      "s-budget",
		ConversationID: "conv-budget",
		Message:        "budget hello",
	}) {
		t.Fatal("budget park refused")
	}
	w1.Stop() // must NOT drop the row (persistence wired)
	st1.Close()

	// The row is on disk under kind=chat with owner=budget.
	check, err := NewSQLiteParkStore(dbPath, newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	recs, _, err := check.Load(ctx, ParkKindChat, time.Now())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(recs) != 1 || recs[0].Owner != "budget" {
		t.Fatalf("budget row after restart = %+v (owners %v), want exactly 1 row owner 'budget'", recs, ownersOf(recs))
	}
	check.Close()

	// Run 2: a fresh budget watcher over the same DB with a CLEARED budget
	// re-arms and resumes the row exactly once.
	clearBudget := llm.NewBudget(llm.BudgetConfig{HourlyLimit: 100000}, nil)
	resumed := make(chan ParkedTurn, 4)
	st2, err := NewSQLiteParkStore(dbPath, newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	w2 := NewBudgetResumeWatcher(clearBudget, newParkStoreTestLogger(), func(_ context.Context, turn ParkedTurn) {
		resumed <- turn
	})
	w2.SetParkPersistence(st2, ParkKindChat)
	w2.SetPersistenceOwner("budget")
	w2.pollInterval = 20 * time.Millisecond
	w2.Start(ctx)
	defer w2.Stop()

	select {
	case got := <-resumed:
		if got.SessionID != "s-budget" {
			t.Errorf("budget resume re-ran the wrong turn: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("budget watcher did not resume the persisted row after restart")
	}
	select {
	case extra := <-resumed:
		t.Errorf("budget row resumed more than once: %+v", extra)
	case <-time.After(300 * time.Millisecond):
	}

	// Cross-owner isolation: seed a future-dated budget row; a quota
	// parker (owner "quota-watcher") over the same file must not re-arm
	// or resume it.
	seed, err := NewSQLiteParkStore(dbPath, newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("reopen seed: %v", err)
	}
	if err := seed.Save(ctx, ParkKindChat, "budget|s-b2|t1", ParkedTurnRecord{
		SessionID:   "s-b2",
		AgentID:     "a",
		Class:       llm.FailureThrottle,
		ResumeAt:    time.Now().Add(time.Hour),
		Owner:       "budget",
		TurnPayload: json.RawMessage(`{"message":"b2"}`),
	}); err != nil {
		t.Fatalf("seed budget row: %v", err)
	}
	seed.Close()

	quotaResumes := atomic.Int32{}
	qp := newOwnerStoreParker(t, dbPath, "quota-watcher", func(context.Context, ParkedTurnRecord) { quotaResumes.Add(1) })
	qp.Start(ctx)
	defer qp.Stop()
	time.Sleep(300 * time.Millisecond)
	if got := quotaResumes.Load(); got != 0 {
		t.Errorf("quota parker re-armed/resumed %d budget rows, want 0 (owner isolation)", got)
	}
	if n := qp.Pending(); n != 0 {
		t.Errorf("quota parker holds %d budget rows after re-arm, want 0", n)
	}
}

// --- 8. TestBudgetParkEmitsEvent (F5 observability) ---------------------------

// Budget park + resume publish on the agent.quota_wait topic shape with
// class "budget" (reuses the parkEventSpy real-bus rig).
func TestBudgetParkEmitsEvent(t *testing.T) {
	b := bus.New(nil, testLogger())
	spy := newParkEventSpy(b)
	defer spy.stop(b)

	exceeded := llm.NewBudget(llm.BudgetConfig{HourlyLimit: 1, Aggressiveness: 1.0}, nil)
	exceeded.RecordUsage(llm.TokenUsage{TotalTokens: 100})

	resumed := make(chan struct{}, 4)
	w := NewBudgetResumeWatcher(exceeded, newParkStoreTestLogger(), func(context.Context, ParkedTurn) {
		resumed <- struct{}{}
	})
	w.SetParkEventBus(b)
	w.pollInterval = 20 * time.Millisecond
	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	if !w.Park(ParkedTurn{SessionID: "s-bev", ConversationID: "c-bev", Message: "hi"}) {
		t.Fatal("budget park refused")
	}
	// Clear the budget (window swap, same technique as budget_resume_test.go).
	w.mu.Lock()
	w.budget = llm.NewBudget(llm.BudgetConfig{HourlyLimit: 100000}, nil)
	w.mu.Unlock()

	select {
	case <-resumed:
	case <-time.After(3 * time.Second):
		t.Fatal("budget watcher did not resume after the budget cleared")
	}

	events := spy.collect(t, 500*time.Millisecond)
	var parkEv, resumeEv map[string]any
	for _, ev := range events {
		if class, _ := ev["class"].(string); class != "budget" {
			continue
		}
		switch to, _ := ev["to"].(string); to {
		case "quota_wait":
			parkEv = ev
		case "running":
			resumeEv = ev
		}
	}
	if parkEv == nil {
		t.Errorf("no budget-class park event (to=quota_wait) on agent.quota_wait: %+v", events)
	}
	if resumeEv == nil {
		t.Errorf("no budget-class resume event (to=running) on agent.quota_wait: %+v", events)
	}
	if parkEv != nil {
		// unblock_at is the wire key both surfaces already read — the
		// budget park must carry it (no payload reshape rule).
		if ua, _ := parkEv["unblock_at"].(string); ua == "" {
			t.Errorf("budget park event missing unblock_at: %+v", parkEv)
		}
	}
}

// --- 9. TestThrottleStashPerTurn (F8) -----------------------------------------

// Overlapping throttle captures must park their OWN payloads: the dispatch
// stash is per-turn, not one shared slot the second entering turn
// overwrites. At HEAD parkThrottledTurn captures whatever l.throttleTurnCtx
// holds — turn A parks with turn B's dispatch.
func TestThrottleStashPerTurn(t *testing.T) {
	throttleFailurePolicyForTests(t, 20*time.Millisecond)
	SetDefaultBackoffOverride(BackoffConfig{BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond, MaxAttempts: 2})
	t.Cleanup(clearDefaultBackoffOverride)

	throttleErr := &llm.ThrottleBackoffError{ProviderID: "p1", ModelID: "m1", RetryAt: time.Now().Add(50 * time.Millisecond)}
	gateA := make(chan struct{})
	gateB := make(chan struct{})
	chatter := &gateChatter{gates: []chan struct{}{gateA, gateB}, errs: []error{throttleErr, throttleErr}}

	loop := NewAgentLoop("sess-stash", t.TempDir(),
		WithMessageBus(bus.New(nil, testLogger())),
		WithLLMChatter(chatter),
	)
	loop.security = security.NewPermissionChecker(security.Config{})
	clock := &fakeNowFunc{now: time.Now()}
	parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, time.Hour)
	parker.nowFunc = clock.nowFn
	loop.SetClock(clock.nowFn)
	loop.SetTurnParker(parker) // parker NOT started: parks stay parked

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = loop.RunOnceWithParts(context.Background(), "message-A", nil, "conv-A")
	}()
	// Let A reach the (blocked) LLM call so its dispatch is stashed.
	waitUntil(t, 2*time.Second, func() bool { return chatter.enteredCount() >= 1 })
	go func() {
		defer wg.Done()
		_, _ = loop.RunOnceWithParts(context.Background(), "message-B", nil, "conv-B")
	}()
	waitUntil(t, 2*time.Second, func() bool { return chatter.enteredCount() >= 2 })

	close(gateA) // A throttles → parks while B's dispatch owns a shared stash
	waitUntil(t, 2*time.Second, func() bool { return parker.Pending() >= 1 })
	close(gateB) // B throttles → parks its own dispatch
	wg.Wait()

	if got := parker.Pending(); got != 2 {
		t.Fatalf("parked records = %d, want 2 (both turns park)", got)
	}
	parker.mu.Lock()
	recs := append([]ParkedTurnRecord(nil), parker.parked...)
	parker.mu.Unlock()

	messages := map[string]int{}
	convs := map[string]int{}
	for _, rec := range recs {
		if len(rec.TurnPayload) == 0 {
			t.Errorf("a parked throttle record carries NO payload — the shared stash was cleared by the first turn's exit (F8 cross-capture)")
			continue
		}
		turn, err := recordToThrottleTurn(rec)
		if err != nil {
			t.Fatalf("decode parked payload: %v", err)
		}
		messages[turn.Message]++
		convs[rec.ConversationID]++
	}
	if messages["message-A"] != 1 || messages["message-B"] != 1 {
		t.Errorf("overlapping turns did not park their OWN payloads: got %v (want one message-A + one message-B)", messages)
	}
	if convs["conv-A"] != 1 || convs["conv-B"] != 1 {
		t.Errorf("parked records did not carry their own conversation ids: got %v", convs)
	}
}

// gateChatter blocks each call on its gate (nil gate = no blocking), then
// fails with the scripted error (or answers). enteredCount counts calls
// already inside Chat.
type gateChatter struct {
	gates   []chan struct{}
	errs    []error
	mu      sync.Mutex
	entered int
}

func (c *gateChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	c.mu.Lock()
	i := c.entered
	c.entered++
	c.mu.Unlock()
	if i < len(c.gates) {
		<-c.gates[i]
	}
	if i < len(c.errs) && c.errs[i] != nil {
		return nil, c.errs[i]
	}
	return &llm.Response{Content: "done", FinishReason: "stop"}, nil
}

func (c *gateChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *gateChatter) Config() *llm.ModelConfig { return &llm.ModelConfig{ModelID: "gate-chatter"} }

func (c *gateChatter) enteredCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entered
}

// --- 10. TestReRaiseReplacesNotAppends (F9) ------------------------------------

// Re-raising the same gate must REPLACE the queued in-memory record
// (at-most-one per key). The store row already upserts on the key, but at
// HEAD ParkGate APPENDS a second memory record, so ResumeByKey pops one and
// a stale copy lingers — a second approval would double-resume.
func TestReRaiseReplacesNotAppends(t *testing.T) {
	store := &fakePersistence{}
	var resumed countingResume
	p := newGateTestParker(t, resumed.add)
	p.SetParkPersistence(store, ParkKindGate)
	p.SetPersistenceOwner("gate")
	gp := NewGateParker(parkedTurnTestLogger(), p)

	if !gp.ParkGate(gateData("plan-rr")) {
		t.Fatal("first ParkGate refused")
	}
	if !gp.ParkGate(gateData("plan-rr")) {
		t.Fatal("re-raise ParkGate refused")
	}
	if n := gp.PendingGates(); n != 1 {
		t.Fatalf("PendingGates after re-raise = %d, want 1 (replace, not append — F9)", n)
	}
	// Persistence: also exactly one row under the gate key.
	loaded, _, err := store.Load(context.Background(), ParkKindGate, time.Now())
	if err != nil {
		t.Fatalf("store load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("persisted gate rows after re-raise = %d, want 1", len(loaded))
	}

	ctx := context.Background()
	if !gp.ResumeByKey(ctx, "plan", "plan-rr") {
		t.Fatal("ResumeByKey found nothing after re-raise")
	}
	// The leftover copy (HEAD shape) must be gone: a second resume finds
	// nothing.
	if gp.ResumeByKey(ctx, "plan", "plan-rr") {
		t.Error("duplicate in-memory record survived the re-raise (second ResumeByKey popped it) — F9")
	}
	waitUntil(t, 2*time.Second, func() bool { return resumed.count() == 1 })
	if n := resumed.count(); n != 1 {
		t.Errorf("resume callbacks = %d, want exactly 1", n)
	}
	if n := gp.PendingGates(); n != 0 {
		t.Errorf("PendingGates after resume = %d, want 0 (no leftover copy)", n)
	}
}

// --- 11. TestParkedDetectionWithoutStateMachine (F6) ---------------------------

// A parked turn must be detected via the explicit per-turn marker (set by
// parkThrottledTurn), not by reading the OPTIONAL state machine. With a nil
// stateMachine the parked turn still skips the post-turn success pipeline:
// no blank assistant message is appended to the conversation, and the turn
// does not masquerade as a completed (empty) reply.
func TestParkedDetectionWithoutStateMachine(t *testing.T) {
	throttleFailurePolicyForTests(t, 20*time.Millisecond)
	SetDefaultBackoffOverride(BackoffConfig{BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond, MaxAttempts: 2})
	t.Cleanup(clearDefaultBackoffOverride)

	throttleErr := &llm.ThrottleBackoffError{ProviderID: "p1", ModelID: "m1", RetryAt: time.Now().Add(50 * time.Millisecond)}
	chatter := &gateChatter{errs: []error{throttleErr}}
	loop := NewAgentLoop("sess-nilsm", t.TempDir(),
		WithMessageBus(bus.New(nil, testLogger())),
		WithLLMChatter(chatter),
	)
	loop.security = security.NewPermissionChecker(security.Config{})
	clock := &fakeNowFunc{now: time.Now()}
	parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, time.Hour)
	parker.nowFunc = clock.nowFn
	loop.SetClock(clock.nowFn)
	loop.SetTurnParker(parker)

	// F6 target: kill the optional state machine — detection must not
	// depend on it.
	loop.stateMachine = nil

	reply, err := loop.RunOnceWithParts(context.Background(), "hello", nil, "conv-nilsm")
	if reply != "" {
		t.Errorf("parked turn reply = %q, want empty", reply)
	}
	if err != nil && !errors.Is(err, ErrTurnParked) {
		t.Errorf("parked turn surfaced %v, want ErrTurnParked (or nil on the old contract)", err)
	}
	if got := parker.Pending(); got != 1 {
		t.Fatalf("parked records = %d, want 1 (the turn parked with a nil state machine)", got)
	}
	// Detection via the marker skips the success pipeline: no blank
	// assistant message polluted the history (H2's defect class).
	conv := loop.SessionConversation("conv-nilsm")
	for _, m := range conv.GetMessages() {
		if m.Role == llm.RoleAssistant && strings.TrimSpace(m.Content) == "" {
			t.Error("parked turn with nil stateMachine fell through to the success pipeline (blank assistant message appended) — F6")
		}
	}
}

// --- 12. TestQuotaPayloadStrictDecode (misroute defense) ------------------------

// C1 is dangerous because the quota payload decode is tolerant: foreign
// JSON with overlapping keys (throttle payloads carry message/conversation_id/
// provider_id/credential_key/source_client/parked_at) decodes
// "successfully" into quotaTurnPayload and gets RE-EXECUTED as a chat turn;
// malformed JSON must never resume either. Strict decode: both DROP with a
// warn, only the genuine row resumes.
func TestQuotaPayloadStrictDecode(t *testing.T) {
	var resumed []QuotaParkedTurn
	var mu sync.Mutex
	store := &fakePersistence{}
	w := NewQuotaResumeWatcher(testLogger(), func(_ context.Context, turn QuotaParkedTurn) {
		mu.Lock()
		resumed = append(resumed, turn)
		mu.Unlock()
	}, time.Hour)
	w.SetPersistenceOwner("quota-watcher")
	w.SetParkPersistence(store)
	w.SetPollInterval(10 * time.Millisecond)
	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	// A genuine quota row resumes normally.
	if !w.Park(QuotaParkedTurn{
		SessionID: "s-ok", ConversationID: "c-ok", Message: "real",
		ProviderID: "p1", CredentialKey: "k1",
		UnblockAt: time.Now().Add(30 * time.Millisecond),
	}) {
		t.Fatal("park refused")
	}
	// Foreign (throttle-shaped) and malformed payloads parked as class=quota
	// directly on the inner parker: the resume callback must DROP both.
	foreign := json.RawMessage(`{"message":"throttle payload","parts":[],"conversation_id":"c-f","provider_id":"p1","credential_key":"k1","source_client":"gui","model_id":"m1","parked_at":"2026-11-09T00:00:00Z","budget":"daily"}`)
	malformed := json.RawMessage(`{"message":"broken","provider_id":`)
	now := time.Now()
	if !w.turns.Park(ParkedTurnRecord{
		SessionID: "s-f", ConversationID: "c-f", Class: llm.FailureQuota,
		ResumeAt: now.Add(30 * time.Millisecond), TurnPayload: foreign,
	}) {
		t.Fatal("foreign park refused")
	}
	if !w.turns.Park(ParkedTurnRecord{
		SessionID: "s-m", ConversationID: "c-m", Class: llm.FailureQuota,
		ResumeAt: now.Add(30 * time.Millisecond), TurnPayload: malformed,
	}) {
		t.Fatal("malformed park refused")
	}

	time.Sleep(600 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(resumed) != 1 {
		t.Fatalf("quota watcher resumed %d turns, want exactly 1 (genuine row only; foreign/malformed must DROP, never misroute): %+v", len(resumed), resumed)
	}
	if resumed[0].SessionID != "s-ok" {
		t.Errorf("resumed the wrong row: %+v", resumed[0])
	}
}
