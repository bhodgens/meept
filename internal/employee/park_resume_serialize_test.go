package employee

// Pins for the park-resume path (park.go ResumeGoalEpisode):
//
//   - ResumeSpeaksOnStandingSession pins the standing-speak-session arming:
//     a resume into a FRESH loop (the post-restart shape — speakSessionID
//     empty) must arm the standing chat session BEFORE the phase re-entry,
//     so the resumed round's employee.notify payload carries
//     session_id = emp-sess-<employeeID>. Failing revert: remove the
//     l.SetSpeakContext(EmployeeChatSessionID(l.employeeID), ...) call in
//     ResumeGoalEpisode (park.go) — the notify then carries an empty
//     session_id and the speak push bridge drops it.
//
//   - (Serialization pin: two concurrent resumes for one agent must not run
//     concurrently. NOT PINNED HERE — the fix requires the per-employee
//     invoke mutex, which is reachable only from *Manager methods
//     (acquireInvokeMutex is unexported on Manager and GoalLoop holds no
//     manager reference) or the daemon wiring closure in
//     internal/daemon/components.go. Landed as a reported patch instead per
//     the task's no-manager.go-edit rule.)

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
)

// TestResumeSpeaksOnStandingSession: ResumeGoalEpisode arms the standing
// speak session before the phase re-entry, so a notify published by the
// resumed round carries session_id emp-sess-<agentID> (the speak push
// bridge drops empty-session notifies).
func TestResumeSpeaksOnStandingSession(t *testing.T) {
	parker, _ := newCapturingParker(t, parkTestPolicy())
	_ = parker // phase "reflect" re-enters directly; the parker only satisfies the ResumeGoalEpisode nil-parker guard
	reflector := newStubReflector()
	// Reflect on the resumed turn answers healthy and emits the notify.
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)

	const agentID = "emp-resume-speak"
	pub := &recordingSpeakPublisher{}
	loop := NewGoalLoop(agentID, testTier2Constitution(), nil, slog.New(slog.DiscardHandler)).
		WithReflector(reflector).
		WithSpeakRouter(agent.NewSpeakRouter(pub.publish)).
		WithEpisodeParker(parker)
	// Fresh loop, exactly the post-restart shape: no SetSpeakContext ever
	// ran (manager.go:1797 runs only on a live Trigger round).

	rec := agent.ParkedTurnRecord{
		AgentID:  agentID,
		Class:    llm.FailureQuota,
		ResumeAt: time.Now().UTC(),
		TurnPayload: mustMarshalGoalPayload(t, goalTurnPayload{
			Phase:  "reflect",
			Plan:   &PlanRef{ID: "plan-resume-speak"},
			Result: &bot.BotExecutionResult{BotID: agentID, Success: true, Output: "resumed round finished"},
		}),
	}
	loop.ResumeGoalEpisode(context.Background(), rec)

	t.Logf("reflector calls after resume: %d", reflector.CallCount())
	if got := reflector.CallCount(); got == 0 {
		t.Fatalf("resume did not reach Reflect (reflector calls = 0) — phase re-entry failed before the notify site")
	}
	calls := pub.recorded()
	if len(calls) == 0 {
		t.Fatal("resumed round produced no notify — cannot verify the speak session")
	}
	for _, c := range calls {
		if c.sessionID != EmployeeChatSessionID(agentID) {
			t.Fatalf("resumed notify session_id = %q, want %q (empty or wrong session = the speak push bridge drops it)",
				c.sessionID, EmployeeChatSessionID(agentID))
		}
		if !strings.HasPrefix(c.conversationID, "goal-") {
			t.Errorf("resumed notify conversation_id = %q, want the \"goal-<id>\" conversation", c.conversationID)
		}
	}
}
