package employee

import (
	"context"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
)

// speakCall records one routed delivery.
type speakCall struct {
	kind           agent.SpeakKind
	text           string
	sessionID      string
	conversationID string
}

// recordingSpeakPublisher is a concurrency-safe fake SpeakPublisher.
type recordingSpeakPublisher struct {
	mu    sync.Mutex
	calls []speakCall
}

func (p *recordingSpeakPublisher) publish(kind agent.SpeakKind, text, sessionID, conversationID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, speakCall{kind, text, sessionID, conversationID})
	return nil
}

func (p *recordingSpeakPublisher) recorded() []speakCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]speakCall, len(p.calls))
	copy(out, p.calls)
	return out
}

func TestGoalLoop_Speak_NotifyOnSuccessfulReflect(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"health":"healthy","reasoning":"CI is green"}`)

	pub := &recordingSpeakPublisher{}
	router := agent.NewSpeakRouter(pub.publish)

	loop := NewGoalLoop("emp-speak", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithSpeakRouter(router)
	loop.SetSpeakContext("sess-goal", "conv-goal-round-1")

	result := &bot.BotExecutionResult{
		BotID:      "emp-speak",
		Output:     "nightly backup completed",
		TokensUsed: 50,
		Success:    true,
	}
	if _, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result); err != nil {
		t.Fatalf("Reflect: %v", err)
	}

	calls := pub.recorded()
	if len(calls) != 1 {
		t.Fatalf("notify deliveries = %d, want 1; calls: %+v", len(calls), calls)
	}
	if calls[0].kind != agent.SpeakNotify {
		t.Errorf("kind = %s, want notify", calls[0].kind)
	}
	if calls[0].text != "nightly backup completed" {
		t.Errorf("text = %q, want round output", calls[0].text)
	}
	if calls[0].sessionID != "sess-goal" || calls[0].conversationID != "conv-goal-round-1" {
		t.Errorf("ids = %s/%s, want sess-goal/conv-goal-round-1", calls[0].sessionID, calls[0].conversationID)
	}
}

// TestGoalLoop_Speak_NotifyAlsoOnFailure verifies a failed round's output is
// still surfaced — the operator must learn the round ran and failed.
func TestGoalLoop_Speak_NotifyAlsoOnFailure(t *testing.T) {
	pub := &recordingSpeakPublisher{}
	loop := NewGoalLoop("emp-speak", testTier2Constitution(), nil, nil).
		WithSpeakRouter(agent.NewSpeakRouter(pub.publish))
	loop.SetSpeakContext("sess-goal", "conv-goal-fail")

	result := &bot.BotExecutionResult{BotID: "emp-speak", Success: false, Error: "boom", Output: "deploy failed at step 3"}
	if _, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result); err != nil {
		t.Fatalf("Reflect: %v", err)
	}
	calls := pub.recorded()
	if len(calls) != 1 {
		t.Fatalf("notify deliveries = %d, want 1 on failure path; calls: %+v", len(calls), calls)
	}
	if calls[0].text != "deploy failed at step 3" {
		t.Errorf("text = %q, want round output", calls[0].text)
	}
}

// TestGoalLoop_Speak_DedupWithinRound verifies leaf 11 Task 4 at the GoalLoop
// layer: a tool notify plus the final text yields exactly ONE notify per
// round. The second delivery (final text) is skipped because the round was
// already notified.
func TestGoalLoop_Speak_DedupWithinRound(t *testing.T) {
	pub := &recordingSpeakPublisher{}
	loop := NewGoalLoop("emp-speak", testTier2Constitution(), nil, nil).
		WithSpeakRouter(agent.NewSpeakRouter(pub.publish))
	loop.SetSpeakContext("sess-goal", "conv-goal-dedup")

	// Mid-round: the model called reply_to_user inside the bot, whose loop
	// delivered through the same router.
	loop.notifySpeakRound(context.Background(), "mid-round progress note")

	// End of round: Reflect delivers the final output — must be deduped.
	result := &bot.BotExecutionResult{BotID: "emp-speak", Output: "final text", Success: true}
	if _, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result); err != nil {
		t.Fatalf("Reflect: %v", err)
	}

	calls := pub.recorded()
	if len(calls) != 1 {
		t.Fatalf("notify deliveries = %d, want 1 (tool notify + final text dedup); calls: %+v", len(calls), calls)
	}
	if calls[0].text != "mid-round progress note" {
		t.Errorf("text = %q, want the tool's mid-round notify", calls[0].text)
	}
}

// TestGoalLoop_Speak_DedupResetsPerRound verifies the dedup flag resets on
// SetSpeakContext so the NEXT round notifies again.
func TestGoalLoop_Speak_DedupResetsPerRound(t *testing.T) {
	pub := &recordingSpeakPublisher{}
	loop := NewGoalLoop("emp-speak", testTier2Constitution(), nil, nil).
		WithSpeakRouter(agent.NewSpeakRouter(pub.publish))

	// Round 1: notified.
	loop.SetSpeakContext("sess-goal", "conv-round-1")
	loop.notifySpeakRound(context.Background(), "round 1 output")
	// Round 2: fresh context resets the dedup.
	loop.SetSpeakContext("sess-goal", "conv-round-2")
	loop.notifySpeakRound(context.Background(), "round 2 output")

	calls := pub.recorded()
	if len(calls) != 2 {
		t.Fatalf("notify deliveries = %d, want 2 (one per round); calls: %+v", len(calls), calls)
	}
	if calls[1].conversationID != "conv-round-2" {
		t.Errorf("round-2 conversation = %q, want conv-round-2", calls[1].conversationID)
	}
}

// TestGoalLoop_Speak_NilRouterNoPanic verifies the review-checklist rule:
// GoalLoop is nil-safe when push is missing (log + skip, no panic).
func TestGoalLoop_Speak_NilRouterNoPanic(t *testing.T) {
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-speak", testTier2Constitution(), nil, nil).WithReflector(reflector)
	// No WithSpeakRouter: default nil-safe router.

	result := &bot.BotExecutionResult{BotID: "emp-speak", Output: "out", Success: true}
	if _, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result); err != nil {
		t.Fatalf("Reflect without speak wiring: %v", err)
	}
}

// TestGoalLoop_Speak_EmptyOutputNoNotify verifies the C3 empty-text rule:
// a round with empty output must not notify.
func TestGoalLoop_Speak_EmptyOutputNoNotify(t *testing.T) {
	pub := &recordingSpeakPublisher{}
	loop := NewGoalLoop("emp-speak", testTier2Constitution(), nil, nil).
		WithSpeakRouter(agent.NewSpeakRouter(pub.publish))
	loop.SetSpeakContext("sess-goal", "conv-goal-empty")

	result := &bot.BotExecutionResult{BotID: "emp-speak", Output: "  ", Success: true}
	if _, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result); err != nil {
		t.Fatalf("Reflect: %v", err)
	}
	if calls := pub.recorded(); len(calls) != 0 {
		t.Errorf("notify deliveries = %d, want 0 (empty output)", len(calls))
	}
}

// TestGoalLoop_Speak_TypedNilRouterGuard verifies the typed-nil guard: a nil
// router passed to WithSpeakRouter is ignored (no panic, default retained).
func TestGoalLoop_Speak_TypedNilRouterGuard(t *testing.T) {
	loop := NewGoalLoop("emp-speak", testTier2Constitution(), nil, nil)
	loop.WithSpeakRouter(nil) // must not panic or replace
	result := &bot.BotExecutionResult{BotID: "emp-speak", Output: "out", Success: true}
	if _, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result); err != nil {
		t.Fatalf("Reflect after typed-nil router: %v", err)
	}
}
