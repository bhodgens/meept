package agent

// Park/resume/give-up bus events (tree 03 leaf 04 Task 1, DECISIONS.md D9):
// throttle parking reuses the EXISTING "agent.quota_wait" lifecycle topic —
// the episode tracker's topic — extended with class-carrying payloads. These
// tests pin the payload keys surfaces and the WS classifier consume:
//
//	park:     {agent_id, to: quota_wait, reason: throttle_wait, class, resume_at, model_id, provider_id}
//	resume:   {agent_id, to: running, reason: throttle_resumed, class, waited, session_id}
//	give_up:  {agent_id, reason: throttle_give_up, waited, model_id, provider_id}
//
// No new topic is introduced, so the WS "agent.quota" prefix match keeps
// classifying every park event as agent_progress (Task 2 pins that).
//
// The bus is drained via a real subscription; waits are select-with-deadline,
// never sleeps.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

// wantClass is the wire value the events carry for the throttle class
// (parkClassString's vocabulary; see parked_turn.go).
const wantClass = "throttle"

// parkEventSpy subscribes to agent.quota_wait BEFORE the event under test is
// published (bus delivery targets subscribers present at publish time) and
// collects the delivered payloads until the deadline elapses.
type parkEventSpy struct {
	sub *bus.Subscriber
}

func newParkEventSpy(b *bus.MessageBus) *parkEventSpy {
	return &parkEventSpy{sub: b.Subscribe("park-events-test", "agent.quota_wait")}
}

// collect decodes every message delivered since subscribe and returns the
// payload maps. Waits up to deadline for the async channel delivery — a
// bounded wait, never an unbounded one.
func (s *parkEventSpy) collect(t *testing.T, deadline time.Duration) []map[string]any {
	t.Helper()
	defer func() { /* sub unsubscribed by the test via stop */ }()
	var got []map[string]any
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for {
		select {
		case msg := <-s.sub.Channel:
			payload := map[string]any{}
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("agent.quota_wait payload unmarshal: %v (raw %s)", err, msg.Payload)
			}
			got = append(got, payload)
		case <-timer.C:
			return got
		}
	}
}

func (s *parkEventSpy) stop(b *bus.MessageBus) {
	b.Unsubscribe(s.sub)
}

// newParkEventsLoop mirrors newThrottleParkLoop but exposes the bus.
func newParkEventsLoop(t *testing.T, maxWait time.Duration) (*AgentLoop, *bus.MessageBus, *fakeNowFunc) {
	t.Helper()
	throttleFailurePolicyForTests(t, 20*time.Millisecond)
	b := bus.New(nil, testLogger())
	loop := NewAgentLoop("sess-park-events", t.TempDir(),
		WithMessageBus(b),
		WithModelRef(testClassifierAlias),
	)
	loop.security = security.NewPermissionChecker(security.Config{})
	clock := &fakeNowFunc{now: time.Now()}
	parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, maxWait)
	parker.nowFunc = clock.nowFn
	parker.SetParkEventBus(b)
	loop.SetClock(clock.nowFn)
	loop.SetTurnParker(parker)
	return loop, b, clock
}

// TestParkTurnEvents_ThrottleParkPayload: parking a throttled turn emits ONE
// agent.quota_wait event carrying the contract keys, on the EXISTING topic.
func TestParkTurnEvents_ThrottleParkPayload(t *testing.T) {
	loop, b, clock := newParkEventsLoop(t, time.Hour)
	spy := newParkEventSpy(b)
	defer spy.stop(b)
	terr := &llm.ThrottleBackoffError{
		ProviderID: "p1",
		ModelID:    "m1",
		// ≥1s out: RFC3339 resume_at is second-truncated, so a sub-second
		// schedule can collide with the truncated park instant in the
		// strictly-after assertion below.
		RetryAt: clock.now.Add(1500 * time.Millisecond),
		Attempt: 0,
	}

	parked, giveUp := loop.parkThrottledTurn(context.Background(), terr)
	if !parked || giveUp != nil {
		t.Fatalf("parkThrottledTurn = (%v, %v), want (true, nil)", parked, giveUp)
	}

	events := spy.collect(t, 500*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("park events = %d, want 1 (%+v)", len(events), events)
	}
	ev := events[0]
	if agentID, ok := ev["agent_id"].(string); !ok || agentID != loop.agentID {
		t.Errorf("agent_id = %v, want %q", ev["agent_id"], loop.agentID)
	}
	if to, ok := ev["to"].(string); !ok || to != "quota_wait" {
		t.Errorf("to = %v, want quota_wait", ev["to"])
	}
	if reason, ok := ev["reason"].(string); !ok || reason != "throttle_wait" {
		t.Errorf("reason = %v, want throttle_wait", ev["reason"])
	}
	if class, ok := ev["class"].(string); !ok || class != wantClass {
		t.Errorf("class = %v, want %q", ev["class"], wantClass)
	}
	resumeAtStr, ok := ev["resume_at"].(string)
	if !ok || resumeAtStr == "" {
		t.Fatalf("resume_at = %v, want an RFC3339 string", ev["resume_at"])
	}
	resumeAt, err := time.Parse(time.RFC3339, resumeAtStr)
	if err != nil {
		t.Errorf("resume_at %q not RFC3339: %v", resumeAtStr, err)
	} else if !resumeAt.After(clock.now.Truncate(time.Second)) {
		// RFC3339 truncates to seconds; compare against the truncated
		// park instant so a sub-second schedule still reads as future.
		t.Errorf("resume_at %v not after park instant", resumeAt)
	}
	if modelID, ok := ev["model_id"].(string); !ok || modelID != "m1" {
		t.Errorf("model_id = %v, want m1", ev["model_id"])
	}
}

// TestParkTurnEvents_ThrottleGiveUpPayload: a MaxWait give-up emits the
// throttle_give_up event (waited in Go duration string; to stays empty — a
// give-up is a failure surface, not a parked-state change).
func TestParkTurnEvents_ThrottleGiveUpPayload(t *testing.T) {
	loop, b, clock := newParkEventsLoop(t, time.Millisecond)
	spy := newParkEventSpy(b)
	defer spy.stop(b)
	terr := &llm.ThrottleBackoffError{
		ProviderID: "p1",
		ModelID:    "m1",
		RetryAt:    clock.now.Add(30 * time.Second),
		Attempt:    0,
	}

	parked, giveUp := loop.parkThrottledTurn(context.Background(), terr)
	if parked || giveUp == nil {
		t.Fatalf("parkThrottledTurn = (%v, %v), want (false, giveUp)", parked, giveUp)
	}

	events := spy.collect(t, 500*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("give-up events = %d, want 1 (%+v)", len(events), events)
	}
	ev := events[0]
	if agentID, ok := ev["agent_id"].(string); !ok || agentID != loop.agentID {
		t.Errorf("agent_id = %v, want %q", ev["agent_id"], loop.agentID)
	}
	if reason, ok := ev["reason"].(string); !ok || reason != "throttle_give_up" {
		t.Errorf("reason = %v, want throttle_give_up", ev["reason"])
	}
	waitedStr, ok := ev["waited"].(string)
	if !ok || waitedStr == "" {
		t.Fatalf("waited = %v, want a duration string", ev["waited"])
	}
	waited, err := time.ParseDuration(waitedStr)
	if err != nil {
		t.Errorf("waited %q not a duration: %v", waitedStr, err)
	} else if waited < 25*time.Second {
		t.Errorf("waited = %v, want ≥ the refused wait", waited)
	}
	if to, ok := ev["to"].(string); ok && to == "quota_wait" {
		t.Errorf("give-up event must not claim the parked state, to = %q", to)
	}
}

// TestParkTurnEvents_ThrottleResumePayload: a resumed throttle turn emits the
// throttle_resumed event with the waited duration from the stored park time.
func TestParkTurnEvents_ThrottleResumePayload(t *testing.T) {
	throttleFailurePolicyForTests(t, time.Second)
	b := bus.New(nil, testLogger())
	spy := newParkEventSpy(b)
	defer spy.stop(b)
	chatter := &throttleScriptChatter{resp: &llm.Response{Content: "ok", FinishReason: "stop"}}
	loop := NewAgentLoop("sess-park-resume", t.TempDir(),
		WithMessageBus(b),
		WithLLMChatter(chatter),
	)
	loop.security = security.NewPermissionChecker(security.Config{})

	rec, err := throttleTurnToRecord(throttleParkedTurn{
		Message:        "hello",
		ConversationID: "c-park-resume",
		SessionID:      "s-park-resume",
		ParkedAt:       time.Now().Add(-30 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wire a parker (the loop's resume path reads its clock through the
	// loop; the parker itself only needs to exist for the emit guard).
	parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, time.Hour)
	parker.SetParkEventBus(b)
	loop.SetTurnParker(parker)

	loop.resumeThrottledTurn(context.Background(), rec)

	events := spy.collect(t, 500*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("resume events = %d, want 1 (%+v)", len(events), events)
	}
	ev := events[0]
	if agentID, ok := ev["agent_id"].(string); !ok || agentID != loop.agentID {
		t.Errorf("agent_id = %v, want %q", ev["agent_id"], loop.agentID)
	}
	if to, ok := ev["to"].(string); !ok || to != "running" {
		t.Errorf("to = %v, want running", ev["to"])
	}
	if reason, ok := ev["reason"].(string); !ok || reason != "throttle_resumed" {
		t.Errorf("reason = %v, want throttle_resumed", ev["reason"])
	}
	if class, ok := ev["class"].(string); !ok || class != wantClass {
		t.Errorf("class = %v, want %q", ev["class"], wantClass)
	}
	waitedStr, ok := ev["waited"].(string)
	if !ok || waitedStr == "" {
		t.Fatalf("waited = %v, want a duration string", ev["waited"])
	}
	waited, err := time.ParseDuration(waitedStr)
	if err != nil {
		t.Errorf("waited %q not a duration: %v", waitedStr, err)
	} else if waited < 25*time.Second {
		t.Errorf("waited = %v, want ≥ 30s minus scheduling drift", waited)
	}
	if sessionID, ok := ev["session_id"].(string); !ok || sessionID != "s-park-resume" {
		t.Errorf("session_id = %v, want s-park-resume", ev["session_id"])
	}
}
