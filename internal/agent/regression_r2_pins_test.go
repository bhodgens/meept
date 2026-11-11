package agent

// Regression-review R2 wave pin tests (2026-11-11).
//
//   R2-2: the C3 resume guards treated ('' ,nil) from RunOnceWithParts as a
//         re-park and deferred delivery forever. But the loop can also return
//         that shape when it ABSORBED an LLM failure (no park row anywhere) —
//         no response, no terminal, no park: the client hung permanently. The
//         guards now disambiguate by PARKED-RECORD PRESENCE:
//           (a) genuine re-park (park row for this conversation)  -> deferred;
//           (b) blank/absorbed shape with NO park row             -> failed
//               terminal emitted (no hang).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
)

// r22ParkerFor builds a loop + chat handler wired with a bare parker (same
// shape the C3 pins use) and returns the handler, parker, and bus subs.
func r22ResumeFixture(t *testing.T, tag string, chatter llm.Chatter) (*ChatHandler, *TurnParker, *bus.Subscriber, *bus.Subscriber) {
	t.Helper()
	msgBus := bus.New(nil, slogDiscardLogger())
	loop := NewAgentLoop("r22-"+tag+"-loop", "/tmp", WithLLMChatter(chatter))
	loop.security = nil
	parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, 24*time.Hour)
	parker.SetPollInterval(10 * time.Millisecond)
	loop.SetTurnParker(parker)
	h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())
	respSub := msgBus.Subscribe("r22-"+tag+"-resp", "chat.response")
	t.Cleanup(func() { msgBus.Unsubscribe(respSub) })
	termSub := msgBus.Subscribe("r22-"+tag+"-term", "turn.terminal")
	t.Cleanup(func() { msgBus.Unsubscribe(termSub) })
	return h, parker, respSub, termSub
}

// r22DrainChannel drains a subscription for up to d, returning the payloads.
func r22DrainChannel(sub *bus.Subscriber, d time.Duration) []string {
	var got []string
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		select {
		case msg := <-sub.Channel:
			got = append(got, string(msg.Payload))
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	return got
}

// TestR2_2a_QuotaResumeGenuineReparkDefers pins R2-2a: a quota resume whose
// turn re-parked mid-flight (throttle parker holds a row for the
// conversation) is DEFERRED — no chat.response, no turn.terminal.
func TestR2_2a_QuotaResumeGenuineReparkDefers(t *testing.T) {
	throttleErr := &llm.ThrottleBackoffError{ProviderID: "p1", ModelID: "m1"}
	h, parker, respSub, termSub := r22ResumeFixture(t, "qp", &errChatter{err: throttleErr})
	throttleFailurePolicyForTests(t, time.Second)

	h.resumeQuotaParkedTurn(context.Background(), QuotaParkedTurn{
		SessionID:      "sess-r22qp",
		ConversationID: "conv-r22qp",
		Message:        "finish it",
		TurnID:         "turn-r22qp",
	})

	// Discriminator: a park row for THIS conversation exists.
	if !parker.HasPendingForConversation("conv-r22qp") {
		t.Fatal("no parked record for the conversation — fixture did not produce a genuine re-park")
	}
	if got := parker.Pending(); got != 1 {
		t.Fatalf("pending on the throttle parker = %d, want 1 (genuine re-park)", got)
	}
	for _, sub := range []*bus.Subscriber{respSub, termSub} {
		if got := r22DrainChannel(sub, 250*time.Millisecond); len(got) > 0 {
			t.Fatalf("genuine re-park delivered %d event(s), want 0 (deferred): %v", len(got), got)
		}
	}
}

// TestR2_2a_BudgetResumeGenuineReparkDefers pins R2-2a for the budget twin.
func TestR2_2a_BudgetResumeGenuineReparkDefers(t *testing.T) {
	throttleErr := &llm.ThrottleBackoffError{ProviderID: "p1", ModelID: "m1"}
	h, parker, respSub, termSub := r22ResumeFixture(t, "qb", &errChatter{err: throttleErr})
	throttleFailurePolicyForTests(t, time.Second)

	h.resumeParkedTurn(context.Background(), ParkedTurn{
		SessionID:      "sess-r22qb",
		ConversationID: "conv-r22qb",
		TurnID:         "turn-r22qb",
		Message:        "finish it",
	})

	if !parker.HasPendingForConversation("conv-r22qb") {
		t.Fatal("no parked record for the conversation — fixture did not produce a genuine re-park")
	}
	for _, sub := range []*bus.Subscriber{respSub, termSub} {
		if got := r22DrainChannel(sub, 250*time.Millisecond); len(got) > 0 {
			t.Fatalf("genuine re-park delivered %d event(s), want 0 (deferred): %v", len(got), got)
		}
	}
}

// TestR2_2b_QuotaResumeAbsorbedShapeTerminalizes pins R2-2b: a blank/absorbed
// ("", nil) shape with NO park row must NOT hang the client — a failed
// turn.terminal and an error chat.response are emitted.
func TestR2_2b_QuotaResumeAbsorbedShapeTerminalizes(t *testing.T) {
	// Whitespace-only terminal completion: the loop returns a blank reply
	// with nil error and nothing parked — the pre-fix hang shape.
	chatter := newMockChatter(&llm.Response{
		Content:      "   ",
		FinishReason: "stop",
		Usage:        llm.TokenUsage{TotalTokens: 3},
	})
	h, parker, respSub, termSub := r22ResumeFixture(t, "qa", chatter)

	h.resumeQuotaParkedTurn(context.Background(), QuotaParkedTurn{
		SessionID:      "sess-r22qa",
		ConversationID: "conv-r22qa",
		Message:        "finish it",
		TurnID:         "turn-r22qa",
	})

	// Nothing is parked: the shape is NOT a re-park.
	if parker.HasPendingForConversation("conv-r22qa") {
		t.Fatal("unexpected park row — fixture must exercise the absorbed (no-park) shape")
	}
	// No hang: the failed terminal fires under the acked turn id.
	terms := r22DrainChannel(termSub, time.Second)
	if len(terms) == 0 {
		t.Fatal("absorbed shape emitted no turn.terminal — the awaiter hangs forever (R2-2 regression)")
	}
	if !strings.Contains(terms[0], `"status":"failed"`) {
		t.Errorf("terminal status = %q, want failed", terms[0])
	}
	if !strings.Contains(terms[0], `"turn_id":"turn-r22qa"`) {
		t.Errorf("terminal missing the acked turn id: %s", terms[0])
	}
	// User-visible error response shipped too.
	resps := r22DrainChannel(respSub, 250*time.Millisecond)
	if len(resps) == 0 || !strings.Contains(resps[0], "Auto-resume failed") {
		t.Errorf("absorbed shape shipped no error chat.response: %v", resps)
	}
}

// TestR2_2b_BudgetResumeAbsorbedShapeTerminalizes pins R2-2b for the budget
// twin (budget_resume HandlerCase).
func TestR2_2b_BudgetResumeAbsorbedShapeTerminalizes(t *testing.T) {
	chatter := newMockChatter(&llm.Response{
		Content:      "   ",
		FinishReason: "stop",
		Usage:        llm.TokenUsage{TotalTokens: 3},
	})
	h, parker, _, termSub := r22ResumeFixture(t, "zb", chatter)

	h.resumeParkedTurn(context.Background(), ParkedTurn{
		SessionID:      "sess-r22zb",
		ConversationID: "conv-r22zb",
		TurnID:         "turn-r22zb",
		Message:        "finish it",
	})

	if parker.HasPendingForConversation("conv-r22zb") {
		t.Fatal("unexpected park row — fixture must exercise the absorbed (no-park) shape")
	}
	terms := r22DrainChannel(termSub, time.Second)
	if len(terms) == 0 {
		t.Fatal("absorbed shape emitted no turn.terminal — the awaiter hangs forever (R2-2 regression)")
	}
	if !strings.Contains(terms[0], `"status":"failed"`) {
		t.Errorf("terminal status = %q, want failed", terms[0])
	}
	if !strings.Contains(terms[0], `"handler_case":"budget_resume"`) {
		t.Errorf("terminal handler_case missing: %s", terms[0])
	}
}

// TestTurnParkerHasPendingForConversation pins the probe itself.
func TestTurnParkerHasPendingForConversation(t *testing.T) {
	p := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, 24*time.Hour)
	if p.HasPendingForConversation("anything") {
		t.Fatal("empty parker reported a pending record")
	}
	if !p.Park(ParkedTurnRecord{
		ConversationID: "conv-x",
		Class:          llm.FailureThrottle,
		ResumeAt:       time.Now().Add(time.Hour),
	}) {
		t.Fatal("park refused")
	}
	if !p.HasPendingForConversation("conv-x") {
		t.Error("parker missed the parked record for its own conversation")
	}
	if p.HasPendingForConversation("conv-other") {
		t.Error("probe matched a different conversation — the discriminator must be per-conversation")
	}
	if p.HasPendingForConversation("") {
		t.Error("empty conversation id must never match (fail toward terminalizing)")
	}
}
