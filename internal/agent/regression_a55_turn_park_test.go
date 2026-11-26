package agent

// regression_a55_turn_park_test.go — bughunt 11-25 A5-5 pins (MEDIUM,
// sa-8 patch). HasPendingForConversation is CONVERSATION-scoped: a park
// row belonging to a DIFFERENT turn of the same conversation (e.g. an
// unrelated throttle park that outlived this resume) made an absorbed-
// failure ("", nil) resume read as a genuine mid-flight re-park, and the
// C3 guard deferred delivery forever — no turn.terminal, the awaiting
// client hangs (the R2-2 hang class resurrected across turns).
//
// The fix: TurnParker.HasPendingForTurn(turnID, conversationID) matches
// the resumed turn's own identity (payload turn_id) and falls back to the
// conversation test only for LEGACY rows that carry no turn_id. The three
// resume guards (handler.go quota/budget, loop_park.go throttle) now call
// it with the resume's TurnID.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
)

// a55ParkForeignTurn parks a quota record for turn A on conversation C
// through the REAL encode path (payload carries turn_id), and returns the
// record so the fixture can assert the shape.
func a55ParkForeignTurn(t *testing.T, parker *TurnParker, convID, turnA string) {
	t.Helper()
	rec, err := quotaTurnToRecord(QuotaParkedTurn{
		SessionID:      "sess-a55",
		ConversationID: convID,
		Message:        "someone else's turn",
		TurnID:         turnA,
		UnblockAt:      time.Now().Add(time.Hour),
		ParkedAt:       time.Now(),
	})
	if err != nil {
		t.Fatalf("encode foreign park: %v", err)
	}
	if !parker.Park(rec) {
		t.Fatal("park refused")
	}
}

// TestA55_QuotaResumeForeignTurnParkTerminalizes is the defect pin: turn
// A sits parked on conversation C; turn B (SAME conversation) resumes
// through the absorbed ("", nil) shape. Old shape: B defers forever (the
// conversation probe saw A's row). Fixed shape: the turn-scoped probe
// ignores A's row (different turn_id, not a legacy row) and B
// terminalizes failed honestly.
func TestA55_QuotaResumeForeignTurnParkTerminalizes(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	loop := NewAgentLoop("a55-loop", "/tmp", WithLLMChatter(newMockChatter(&llm.Response{
		Content:      "   ", // absorbed shape: blank completion, nil error
		FinishReason: "stop",
		Usage:        llm.TokenUsage{TotalTokens: 3},
	})))
	loop.security = nil
	parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, 24*time.Hour)
	parker.SetPollInterval(10 * time.Millisecond)
	loop.SetTurnParker(parker)
	h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())
	termSub := msgBus.Subscribe("a55-term", "turn.terminal")
	defer msgBus.Unsubscribe(termSub)
	respSub := msgBus.Subscribe("a55-resp", "chat.response")
	defer msgBus.Unsubscribe(respSub)

	const conv = "conv-a55-shared"
	a55ParkForeignTurn(t, parker, conv, "turn-a55-A")

	// Discriminator preconditions: the conversation probe (old shape)
	// WOULD defer, the turn probe does not. If this drifts the pin below
	// is vacuous.
	if !parker.HasPendingForConversation(conv) {
		t.Fatal("fixture: no conversation-scoped row — pin would be vacuous (old shape already terminalizes)")
	}
	if parker.HasPendingForTurn("turn-a55-B", conv) {
		t.Fatal("fixture: turn-scoped probe still matches a foreign turn's row — HasPendingForTurn broken")
	}

	h.resumeQuotaParkedTurn(context.Background(), QuotaParkedTurn{
		SessionID:      "sess-a55",
		ConversationID: conv,
		Message:        "finish it",
		TurnID:         "turn-a55-B", // DIFFERENT turn than the parked row
	})

	terms := r22DrainChannel(termSub, time.Second)
	if len(terms) == 0 {
		t.Fatal("absorbed resume deferred forever behind a FOREIGN turn's park row — A5-5 hang regression (no turn.terminal)")
	}
	if !strings.Contains(terms[0], `"status":"failed"`) || !strings.Contains(terms[0], `"turn_id":"turn-a55-B"`) {
		t.Errorf("absorbed resume terminal = %s, want failed terminal under turn-a55-B", terms[0])
	}
	resps := r22DrainChannel(respSub, 250*time.Millisecond)
	if len(resps) == 0 || !strings.Contains(resps[0], "Auto-resume failed") {
		t.Errorf("absorbed resume shipped no error chat.response: %v", resps)
	}
}

// TestA55_GenuineReparkStillDefers pins the preserved half: a re-park of
// the SAME turn (mid-flight re-throttle carrying the same TurnID) still
// defers delivery — turn-scoped matching must not break R2-2a. Uses the
// probe directly at the parker level (the same-call identity case) plus
// the legacy-row fallback case.
func TestA55_GenuineReparkStillDefers(t *testing.T) {
	parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, 24*time.Hour)
	parker.SetPollInterval(time.Hour)

	// Same-turn row: matches by turn id.
	a55ParkForeignTurn(t, parker, "conv-a55-g1", "turn-a55-SAME")
	if !parker.HasPendingForTurn("turn-a55-SAME", "conv-a55-g1") {
		t.Fatal("same-turn park row not matched — a genuine re-park would false-terminalize (R2-2 regression)")
	}

	// Legacy row (payload WITHOUT turn_id): falls back to conversation
	// scope, so pre-C-K2 rows keep deferring (byte-identical old shape).
	legacyPayload, err := json.Marshal(map[string]any{
		"message":         "legacy park",
		"conversation_id": "conv-a55-g2",
		"provider_id":     "p1",
		"credential_key":  "k1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !parker.Park(ParkedTurnRecord{
		ConversationID: "conv-a55-g2",
		Class:          llm.FailureQuota,
		ResumeAt:       time.Now().Add(time.Hour),
		TurnPayload:    legacyPayload,
	}) {
		t.Fatal("legacy park refused")
	}
	if !parker.HasPendingForTurn("turn-a55-new", "conv-a55-g2") {
		t.Fatal("legacy row (no turn_id) must fall back to the conversation probe — pre-C-K2 rows would false-terminalize a real re-park")
	}
	// Different conversation, different turn: no match, no defer.
	if parker.HasPendingForTurn("turn-a55-new", "conv-a55-other") {
		t.Fatal("row from another conversation matched an unrelated turn")
	}
}
