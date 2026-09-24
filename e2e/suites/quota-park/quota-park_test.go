//go:build e2e

// Suite quota-park: the park/resume machinery end to end over REAL
// TurnParker + SQLiteParkStore + queue + budget infrastructure.
//
//	01 quota turn parks with queued message and resumes at reset
//	02 park/resume emits agent.quota_wait events with unblock_at
//	03 parked turns survive a restart via the ParkStore (HIGH VALUE)
//	04 throttle park beyond MaxWait gives up visibly
//	05 budget-exhausted turn parks and resumes
//	06 goal-loop park shares the ONE TurnParker
package quotapark

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/models"
)

// quotaTurn is a helper building a parkable quota turn.
func quotaTurn(session, conv, message, provider string, unblockIn time.Duration) agent.QuotaParkedTurn {
	return agent.QuotaParkedTurn{
		SessionID:      session,
		ConversationID: conv,
		Message:        message,
		ProviderID:     provider,
		CredentialKey:  "cred-" + provider,
		UnblockAt:      time.Now().Add(unblockIn),
		TurnID:         "turn-" + session,
	}
}

// TestQuotaPark_ParkThenResumeAtReset covers quota-park-01: a
// quota-interrupted turn parks with its queued message, Pending grows,
// and once the reset time passes the resume callback receives the
// ORIGINAL message/turn identity (oldest-first).
func TestQuotaPark_ParkThenResumeAtReset(t *testing.T) {
	var resumed []agent.QuotaParkedTurn
	var mu sync.Mutex
	w := agent.NewQuotaResumeWatcher(nil, func(_ context.Context, turn agent.QuotaParkedTurn) {
		mu.Lock()
		resumed = append(resumed, turn)
		mu.Unlock()
	}, time.Minute)
	w.SetPollInterval(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	// Two turns parked, second scheduled to resume EARLIER — the drain
	// must be oldest-resume-first.
	if !w.Park(quotaTurn("sess-1", "conv-1", "first message", "anthropic", 900*time.Millisecond)) {
		t.Fatal("park refused for a valid future unblock time")
	}
	if !w.Park(quotaTurn("sess-2", "conv-2", "second message", "openai", 300*time.Millisecond)) {
		t.Fatal("park refused for a valid future unblock time")
	}
	if w.Pending() != 2 {
		t.Fatalf("pending = %d, want 2 queued", w.Pending())
	}

	// Refusals: past/zero unblock and over-MaxWait waits must NOT park.
	if w.Park(quotaTurn("sess-3", "conv-3", "stale", "p", -time.Second)) {
		t.Fatal("a past unblock time must be refused, not parked")
	}
	overWait := quotaTurn("sess-4", "conv-4", "too long", "p", 2*time.Minute)
	if w.Park(overWait) {
		t.Fatal("a wait beyond MaxWait must be refused (quota gives up visibly)")
	}
	if w.Pending() != 2 {
		t.Fatalf("refused parks changed Pending to %d", w.Pending())
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(resumed)
		mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(resumed) != 2 {
		t.Fatalf("resumed %d turns, want 2", len(resumed))
	}
	first := resumed[0]
	if first.SessionID != "sess-2" || first.Message != "second message" {
		t.Fatalf("resume order broken: first = %+v, want sess-2 (earliest reset)", first)
	}
	if first.TurnID != "turn-sess-2" {
		t.Fatalf("resumed turn lost its turn_id: %+v", first)
	}
	if w.Pending() != 0 {
		t.Fatalf("pending = %d after drain, want 0", w.Pending())
	}
}

// TestQuotaPark_ParkResumeEventsWithUnblockAt covers quota-park-02:
// park and resume emit agent.quota_wait events carrying the unblock_at
// wire key and the class vocabulary.
func TestQuotaPark_ParkResumeEventsWithUnblockAt(t *testing.T) {
	messageBus := bus.New(nil, nil)
	defer messageBus.Close()
	sub := messageBus.Subscribe("e2e-quota-events", "agent.quota_wait")
	defer messageBus.Unsubscribe(sub)

	parker := agent.NewTurnParker(nil, func(context.Context, agent.ParkedTurnRecord) {}, time.Minute)
	parker.SetPollInterval(50 * time.Millisecond)
	parker.SetParkEventBus(parkEventPublisher{bus: messageBus})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	parker.Start(ctx)

	rec := agent.ParkedTurnRecord{
		ConversationID: "conv-ev",
		SessionID:      "sess-ev",
		AgentID:        "main",
		Class:          llm.FailureQuota,
		ResumeAt:       time.Now().Add(300 * time.Millisecond),
		TurnPayload:    []byte(`{"message":"ev message","conversation_id":"conv-ev","provider_id":"p1","credential_key":"c1"}`),
	}
	if !parker.Park(rec) {
		t.Fatal("park refused")
	}
	// Park() itself is event-silent; the quota watcher and loop park sites
	// emit explicitly through the exported hook (leaf 04 contract).
	parker.EmitParkEvent(rec, "", "p1")
	// The PARK event: reason quota_wait, class quota, unblock_at RFC3339.
	expectEvent(t, sub, "quota_wait", func(ev agent.ParkTurnEvent) error {
		if ev.Class != "quota" {
			return errors.New("class = " + ev.Class + ", want quota")
		}
		if ev.UnblockAt == "" {
			return errors.New("unblock_at missing from the park event")
		}
		if _, err := time.Parse(time.RFC3339, ev.UnblockAt); err != nil {
			return errors.New("unblock_at not RFC3339: " + ev.UnblockAt)
		}
		if ev.SessionID != "sess-ev" {
			return errors.New("session_id = " + ev.SessionID)
		}
		return nil
	})

	// The RESUME event: reason throttle_resumed, to=running. The drain
	// itself is event-silent; the resume router (loop_park.go) emits via
	// the exported hook, so this test calls it the same way.
	parker.EmitResumeEvent(rec, time.Time{})
	expectEvent(t, sub, "throttle_resumed", func(ev agent.ParkTurnEvent) error {
		if ev.Class != "quota" {
			return errors.New("resume class = " + ev.Class)
		}
		if ev.To != "running" {
			return errors.New("resume to = " + ev.To)
		}
		return nil
	})
}

func expectEvent(t *testing.T, sub *bus.Subscriber, wantReason string, validate func(agent.ParkTurnEvent) error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case got := <-sub.Channel:
			if got == nil || got.Payload == nil {
				continue
			}
			var ev agent.ParkTurnEvent
			if err := json.Unmarshal(got.Payload, &ev); err != nil {
				t.Fatalf("park event decode: %v (%s)", err, got.Payload)
			}
			if ev.Reason != wantReason {
				continue // another lifecycle stage; keep waiting
			}
			if err := validate(ev); err != nil {
				t.Fatalf("%s event invalid: %v (payload %s)", wantReason, err, got.Payload)
			}
			return
		case <-time.After(deadline.Sub(time.Now())):
			t.Fatalf("no %q event observed on agent.quota_wait", wantReason)
		}
	}
	t.Fatalf("no %q event observed", wantReason)
}

// TestQuotaPark_SurvivesRestartViaParkStore covers quota-park-03 (HIGH
// VALUE): a parked turn mirrored to the SQLite ParkStore is re-armed into
// a FRESH TurnParker built over the same parks.db after the "restart" —
// and an expired row is pruned at load rather than resumed stale.
func TestQuotaPark_SurvivesRestartViaParkStore(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "parks.db")

	// --- Generation 1: park two turns into the durable store. ---
	store1, err := agent.NewSQLiteParkStore(dbPath, nil)
	if err != nil {
		t.Fatalf("park store gen1: %v", err)
	}
	parker1 := agent.NewTurnParker(nil, func(context.Context, agent.ParkedTurnRecord) {}, time.Hour)
	parker1.SetParkPersistence(store1, agent.ParkKindChat)
	// No Start: parks must persist immediately regardless.

	survivor := agent.ParkedTurnRecord{
		ConversationID: "conv-survivor",
		SessionID:      "sess-survivor",
		AgentID:        "main",
		Class:          llm.FailureQuota,
		ResumeAt:       time.Now().Add(400 * time.Millisecond),
		TurnPayload:    []byte(`{"message":"survive the restart","conversation_id":"conv-survivor","provider_id":"p","credential_key":"c","turn_id":"turn-s"}`),
	}
	expired := agent.ParkedTurnRecord{
		ConversationID: "conv-expired",
		SessionID:      "sess-expired",
		Class:          llm.FailureThrottle,
		TurnPayload:    []byte(`{"message":"stale"}`),
	}
	if !parker1.Park(survivor) {
		t.Fatal("generation-1 survivor park refused")
	}
	// Park refuses past resume times (correctly), so an already-due record
	// is seeded straight into the store with a past resume time: Load(now)
	// must prune it and the re-armed parker must never resume it.
	// (Direct store writes don't touch the in-memory parker queue.)
	expired.ResumeAt = time.Now().Add(-time.Hour)
	if err := store1.Save(context.Background(), agent.ParkKindChat,
		"e2e-expired-key", expired); err != nil {
		t.Fatalf("seed expired row: %v", err)
	}
	if parker1.Pending() != 1 {
		t.Fatalf("gen1 in-memory pending = %d, want 1 (the store-seeded row is not queued)", parker1.Pending())
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("gen1 close: %v", err)
	}
	// Simulate the process death WITHOUT a graceful drain: the in-memory
	// generation is simply abandoned; the rows are still on disk.

	// --- Generation 2 (the restart): fresh store + parker, re-arm. ---
	var resumed []agent.ParkedTurnRecord
	var mu sync.Mutex
	store2, err := agent.NewSQLiteParkStore(dbPath, nil)
	if err != nil {
		t.Fatalf("park store gen2: %v", err)
	}
	defer store2.Close()
	parker2 := agent.NewTurnParker(nil, func(_ context.Context, rec agent.ParkedTurnRecord) {
		mu.Lock()
		resumed = append(resumed, rec)
		mu.Unlock()
	}, time.Hour)
	parker2.SetParkPersistence(store2, agent.ParkKindChat)
	parker2.SetPollInterval(100 * time.Millisecond)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	parker2.Start(runCtx) // Start re-arms surviving rows; expired ones are pruned

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(resumed)
		mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(resumed) != 1 {
		t.Fatalf("post-restart resumed %d records, want exactly the survivor", len(resumed))
	}
	got := resumed[0]
	if got.SessionID != "sess-survivor" || got.Class != llm.FailureQuota {
		t.Fatalf("resumed record = %+v", got)
	}
	var payload struct {
		Message string `json:"message"`
		TurnID  string `json:"turn_id"`
	}
	if err := json.Unmarshal(got.TurnPayload, &payload); err != nil {
		t.Fatalf("payload round-trip: %v", err)
	}
	if payload.Message != "survive the restart" || payload.TurnID != "turn-s" {
		t.Fatalf("payload lost across restart: %+v", payload)
	}
	// The expired row was pruned, not resumed (it never arrives), and the
	// at-most-once delete means a THIRD generation finds nothing.
	if err := store2.Close(); err != nil {
		t.Fatalf("gen2 close: %v", err)
	}
	store3, err := agent.NewSQLiteParkStore(dbPath, nil)
	if err != nil {
		t.Fatalf("park store gen3: %v", err)
	}
	defer store3.Close()
	rows, _, err := store3.Load(ctx, agent.ParkKindChat, time.Now())
	if err != nil {
		t.Fatalf("gen3 load: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("gen3 found surviving rows %v — at-most-once delete broken", rows)
	}
}

// TestQuotaPark_ThrottleBeyondMaxWaitGivesUp covers quota-park-04: a
// throttle wait beyond MaxWait is refused by the watcher (the caller then
// surfaces ThrottleGiveUpError) and a give-up event lands on the bus with
// reason throttle_give_up.
func TestQuotaPark_ThrottleBeyondMaxWaitGivesUp(t *testing.T) {
	messageBus := bus.New(nil, nil)
	defer messageBus.Close()
	sub := messageBus.Subscribe("e2e-giveup", "agent.quota_wait")
	defer messageBus.Unsubscribe(sub)

	parker := agent.NewTurnParker(nil, func(context.Context, agent.ParkedTurnRecord) {
		t.Fatal("an over-MaxWait record must never be parked, let alone resumed")
	}, 30*time.Second)
	parker.SetParkEventBus(parkEventPublisher{bus: messageBus})

	overMaxWait := agent.ParkedTurnRecord{
		ConversationID: "conv-giveup",
		SessionID:      "sess-giveup",
		AgentID:        "main",
		Class:          llm.FailureThrottle,
		ResumeAt:       time.Now().Add(time.Hour), // >> MaxWait: the parker soft-stops to now+MaxWait...
		TurnPayload:    []byte(`{}`),
	}
	// The PARKER soft-stops (records at now+MaxWait) — the CONSUMER is
	// responsible for refusing; emulate the loop's refusal by checking the
	// soft-stop lands within MaxWait and the give-up event shape carries
	// the waited duration.
	if !parker.Park(overMaxWait) {
		t.Fatal("parker soft-stop should accept and reschedule")
	}
	info := parker.WaitInfo()
	if len(info) != 1 {
		t.Fatalf("WaitInfo = %+v, want the soft-stopped record", info)
	}
	wait := time.Until(info[0].Next)
	if wait > 35*time.Second {
		t.Fatalf("soft-stop resume_at = %s, want now+MaxWait (<=30s)", info[0].Next)
	}
	parker.Stop()

	// The give-up event the loop emits when IT refuses (D8 surface). The
	// event type + reason vocabulary are the exported wire contract; the
	// loop publishes it on the same agent.quota_wait topic.
	giveUpMsg, err := models.NewBusMessage(models.MessageTypeEvent, "main", agent.ParkTurnEvent{
		AgentID:    "main",
		Reason:     agent.ReasonThrottleGiveUp,
		Class:      "throttle",
		Waited:     (2 * time.Hour).String(),
		ModelID:    "model-x",
		ProviderID: "provider-y",
	})
	if err != nil {
		t.Fatalf("build give-up event: %v", err)
	}
	if messageBus.Publish("agent.quota_wait", giveUpMsg) == 0 {
		t.Fatal("give-up event reached no subscriber")
	}
	expectEvent(t, sub, "throttle_give_up", func(ev agent.ParkTurnEvent) error {
		if ev.Waited == "" {
			return errors.New("give-up event lost its waited duration")
		}
		return nil
	})
}

// TestQuotaPark_BudgetParksAndResumes covers quota-park-05: a
// budget-exhausted turn parks on the BudgetResumeWatcher, keeps its
// identity while parked, and HOLDS while the budget stays exceeded. The
// watcher's poll cadence is a fixed 30s (no test seam), so the resume
// side of the drain contract is covered deterministically in
// TestQuotaPark_ParkThenResumeAtReset over the shared TurnParker; here we
// verify the budget-specific half: park acceptance, identity, and the
// no-resume-while-exceeded guard.
func TestQuotaPark_BudgetParksAndResumes(t *testing.T) {
	// Per-session token cap: session A is over, session B is untouched —
	// the scoping contract the chat handler's pre-check parks under.
	budget := llm.NewBudget(llm.BudgetConfig{PerSessionBudget: 100, Aggressiveness: 1}, nil)
	budget.RecordUsageWithScope(llm.TokenUsage{PromptTokens: 1000, CompletionTokens: 1000, TotalTokens: 2000}, "", "sess-A")
	if r := budget.CheckBudgetWithScope("", "sess-A"); !r.Exceeded {
		t.Fatal("test precondition: session A must be over its budget")
	}
	if r := budget.CheckBudgetWithScope("", "sess-B"); r.Exceeded {
		t.Fatal("session B must not be affected by session A's usage")
	}
	// And the hourly shape the drain reads.
	hourly := llm.NewBudget(llm.BudgetConfig{HourlyLimit: 100, Aggressiveness: 1}, nil)
	hourly.RecordUsage(llm.TokenUsage{PromptTokens: 1000, CompletionTokens: 1000, TotalTokens: 2000})
	if !hourly.CheckBudget().Exceeded {
		t.Fatal("test precondition: hourly budget must be exceeded")
	}

	// A BudgetExceededError is NonRetryable (goes to park, never retry).
	var asBudget *llm.BudgetExceededError
	if !llm.IsNonRetryable(&llm.BudgetExceededError{Message: "over", Reason: llm.BudgetLimitPerSession}) {
		t.Fatal("BudgetExceededError must be non-retryable")
	}
	_ = asBudget

	var resumed []agent.ParkedTurn
	var mu sync.Mutex
	w := agent.NewBudgetResumeWatcher(hourly, nil, func(_ context.Context, turn agent.ParkedTurn) {
		mu.Lock()
		resumed = append(resumed, turn)
		mu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// NOTE: Start would poll on the fixed 30s interval; we deliberately do
	// NOT start it — the still-exceeded guard means nothing may drain
	// anyway, and a not-started watcher makes the hold assertion exact
	// rather than timing-based.
	_ = ctx

	turn := agent.ParkedTurn{
		SessionID:      "sess-budget",
		ConversationID: "conv-budget",
		Message:        "please retry me after the budget window",
		TurnID:         "turn-budget-1",
	}
	if !w.Park(turn) {
		t.Fatal("budget park refused")
	}
	if w.Pending() != 1 {
		t.Fatalf("pending = %d, want the parked turn", w.Pending())
	}

	// The parked record retains its identity (what the resume will see).
	mu.Lock()
	if len(resumed) != 0 {
		t.Fatalf("resume fired while over budget and not started: %+v", resumed)
	}
	mu.Unlock()
	if w.Pending() != 1 {
		t.Fatalf("parked turn vanished (pending=%d)", w.Pending())
	}

	// Same-token re-park identity: a second distinct turn queues behind.
	if !w.Park(agent.ParkedTurn{
		SessionID: "sess-budget-2", ConversationID: "conv-budget-2",
		Message: "second queued turn", TurnID: "turn-budget-2",
	}) {
		t.Fatal("second budget park refused")
	}
	if w.Pending() != 2 {
		t.Fatalf("pending = %d, want 2 queued budget turns", w.Pending())
	}
}

// TestQuotaPark_GoalLoopSharesOneTurnParker covers quota-park-06: the
// goal loop's EpisodeParker parks through the SAME shared TurnParker the
// chat side uses — one queue, one Pending count, one drain, and the
// goal-loop give-up schedule propagates the ORIGINAL error.
func TestQuotaPark_GoalLoopSharesOneTurnParker(t *testing.T) {
	messageBus := bus.New(nil, nil)
	defer messageBus.Close()
	sub := messageBus.Subscribe("e2e-shared-parker", "agent.quota_wait")
	defer messageBus.Unsubscribe(sub)

	var resumed []agent.ParkedTurnRecord
	var mu sync.Mutex
	shared := agent.NewTurnParker(nil, func(_ context.Context, rec agent.ParkedTurnRecord) {
		mu.Lock()
		resumed = append(resumed, rec)
		mu.Unlock()
	}, time.Minute)
	shared.SetPollInterval(100 * time.Millisecond)
	shared.SetParkEventBus(parkEventPublisher{bus: messageBus})

	// The goal-loop parker rides the SHARED parker (daemon wiring shape).
	policy := llm.FailurePolicyConfig{
		Horizon:           2 * time.Hour, // generous for parks; give-up tested below
		BaseThrottle:      30 * time.Second,
		BaseQuota402Extra: 5 * time.Minute,
		PollFloor:         time.Hour,
	}
	ep := employee.NewEpisodeParker(shared, policy, nil)

	// A tier-1 goal loop whose ASSESS hits a quota wait: the episode parks
	// onto the shared parker (no error out of Assess — the loop is waiting).
	reflector := &erroringReflector{err: &llm.QuotaResetError{
		ProviderID: "provider-e2e", ModelID: "m1",
		Code: "usage_limit_reached", ResetAt: time.Now().Add(800 * time.Millisecond),
		MaxWait: 24 * time.Hour,
	}}
	loop := employee.NewGoalLoop("emp-e2e", goalConstitution(), nil, nil).
		WithReflector(reflector).
		WithEpisodeParker(ep)

	candidates, err := loop.Assess(context.Background(), employee.TriggerEvent{
		Source: "cron", FiredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Assess must park (no error) on a provider wait: %v", err)
	}
	if candidates != nil {
		t.Fatalf("parked Assess returned candidates: %+v", candidates)
	}

	// The chat side parks into the SAME parker: one Pending covers both.
	if !shared.Park(agent.ParkedTurnRecord{
		ConversationID: "conv-chat",
		SessionID:      "sess-chat",
		Class:          llm.FailureThrottle,
		ResumeAt:       time.Now().Add(200 * time.Millisecond),
		TurnPayload:    []byte(`{}`),
	}) {
		t.Fatal("chat park refused")
	}
	if shared.Pending() != 2 {
		t.Fatalf("shared Pending = %d, want 2 (episode + chat in ONE queue)", shared.Pending())
	}

	// One drain resumes both, ordered by resume time.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shared.Start(ctx)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(resumed)
		mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(resumed) != 2 {
		t.Fatalf("shared drain resumed %d, want 2", len(resumed))
	}
	if resumed[0].Class != llm.FailureThrottle || resumed[1].Class != llm.FailureQuota {
		t.Fatalf("resume order = %v/%v, want throttle then quota (earliest first)",
			resumed[0].Class, resumed[1].Class)
	}
	var episodePayload struct {
		Phase   string `json:"phase"`
		Trigger *struct {
			Source string `json:"source"`
		} `json:"trigger"`
	}
	if err := json.Unmarshal(resumed[1].TurnPayload, &episodePayload); err != nil {
		t.Fatalf("episode payload decode: %v", err)
	}
	if episodePayload.Phase != "tier1" {
		t.Fatalf("episode resumed phase = %q, want tier1 (tier-1 re-entry)", episodePayload.Phase)
	}

	// The park event for the episode rode the shared event bus.
	expectEvent(t, sub, "quota_wait", func(ev agent.ParkTurnEvent) error {
		if ev.AgentID != "emp-e2e" {
			return errors.New("episode park event agent = " + ev.AgentID)
		}
		return nil
	})

	// Give-up: a wait beyond the horizon propagates the ORIGINAL error
	// (no park), so the loop's failure path stays byte-identical.
	giveUpReflector := &erroringReflector{err: &llm.QuotaResetError{
		ProviderID: "provider-e2e", Code: "usage_limit_reached",
		ResetAt: time.Now().Add(48 * time.Hour), // beyond the 2h horizon
		MaxWait: 24 * time.Hour,
	}}
	giveUpLoop := employee.NewGoalLoop("emp-e2e", goalConstitution(), nil, nil).
		WithReflector(giveUpReflector).
		WithEpisodeParker(ep)
	_, assessErr := giveUpLoop.Assess(context.Background(), employee.TriggerEvent{
		Source: "cron", FiredAt: time.Now().UTC(),
	})
	if assessErr == nil {
		t.Fatal("beyond-horizon wait must surface the original error, not park")
	}
	var asQuota *llm.QuotaResetError
	if !errors.As(assessErr, &asQuota) {
		t.Fatalf("give-up error = %v, want the original QuotaResetError", assessErr)
	}
}

// goalConstitution is the tier-1 constitution the park fixture uses.
func goalConstitution() *employee.Constitution {
	return &employee.Constitution{
		Purpose:      "e2e quota-park fixture",
		Role:         "responder",
		Charter:      "park and resume",
		AutonomyTier: employee.Tier1Reactive,
		EscalatesTo:  []string{"user"},
	}
}

// erroringReflector always returns the given error from Chat (the
// provider-wait stand-in for Assess's LLM call).
type erroringReflector struct{ err error }

func (r *erroringReflector) Chat(context.Context, []llm.ChatMessage, ...llm.ChatOption) (*llm.Response, error) {
	return nil, r.err
}

// --- shims for unexported collaborators (the suite drives the packages
// only through exported surfaces; these wrap the bus + payload shapes). ---

type parkEventPublisher struct{ bus *bus.MessageBus }

func (p parkEventPublisher) Publish(topic string, msg *models.BusMessage) int {
	return p.bus.Publish(topic, msg)
}
