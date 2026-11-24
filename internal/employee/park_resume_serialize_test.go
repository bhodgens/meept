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
//   - AcquireInvokeMutexForResume_SharesTheTriggerMutex pins the
//     serialization seam (bughunt 2026-11-21 deferral, landed by the parent):
//     the exported resume accessor must return the SAME mutex
//     triggerViaGoalLoop locks, so a resume queues behind an in-flight
//     trigger round instead of running concurrently. Failing revert: make
//     AcquireInvokeMutexForResume return a fresh &sync.Mutex{} instead of
//     m.acquireInvokeMutex(employeeID).

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

// TestAcquireInvokeMutexForResume_SharesTheTriggerMutex pins the identity of
// the resume serialization seam: the exported accessor must return the same
// mutex instance acquireInvokeMutex (and therefore triggerViaGoalLoop) hands
// out for the same employee. The daemon resume callback locks this across
// ResumeGoalEpisode; a distinct instance would serialize nothing.
func TestAcquireInvokeMutexForResume_SharesTheTriggerMutex(t *testing.T) {
	m := NewManager(nil)
	const id = "emp-resume-mutex"

	trigger := m.acquireInvokeMutex(id)
	resume := m.AcquireInvokeMutexForResume(id)
	if trigger != resume {
		t.Fatal("AcquireInvokeMutexForResume returned a different mutex than acquireInvokeMutex; the resume path would not serialize with trigger rounds")
	}

	// Behavioral check: while the trigger lane holds the mutex, the resume
	// lane's Lock blocks until release. This is the ordering that stops a
	// quota resume from interleaving a live trigger round.
	trigger.Lock()
	resumed := make(chan struct{})
	go func() {
		resume.Lock()
		close(resumed)
	}()
	select {
	case <-resumed:
		t.Fatal("resume acquired the mutex while the trigger lane still held it; serialization is broken")
	case <-time.After(150 * time.Millisecond):
		// Expected: still blocked.
	}
	trigger.Unlock()
	select {
	case <-resumed:
	case <-time.After(2 * time.Second):
		t.Fatal("resume never acquired the mutex after trigger release")
	}
}

// TestRetireDrainsInvokeMutexBeforeDelete pins the bughunt retire-boundary
// fix: Manager.Retire must wait for a parked-resume callback currently
// HOLDING the employee's invoke mutex before dropping the map entry — the
// pre-fix plain delete() pruned the mutex out from under the holder, and a
// same-id re-hire lazily created a NEW mutex, dropping the 0d62dadb
// serialization across the retire boundary. The drain runs OUTSIDE the map
// guard, so other employees are never blocked behind it.
//
// Failing revert: restore `delete(m.invokeMuMap, id)` without the
// Lock+Unlock drain — Retire returns while the holder is still inside its
// critical section and this test fails on the ordering assertion.
func TestRetireDrainsInvokeMutexBeforeDelete(t *testing.T) {
	m, _ := newHirePersistenceManager(t)
	ctx := context.Background()

	const id = "emp-retire-drain"
	// A bot row must exist for Retire's DeleteBot to succeed.
	if err := m.botManager.CreateBot(ctx, bot.BotDefinition{
		ID:      id,
		Name:    "retire drain",
		Prompt:  "p",
		Enabled: true,
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
	}); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}

	// Simulate a parked-resume callback holding the mutex (the exact
	// holder shape AcquireInvokeMutexForResume exposes to the daemon
	// wiring).
	invokeMu := m.AcquireInvokeMutexForResume(id)
	invokeMu.Lock()
	inHolder := make(chan struct{})
	holderDone := make(chan struct{})
	go func() {
		close(inHolder)
		time.Sleep(100 * time.Millisecond) // still inside the critical section
		invokeMu.Unlock()
		close(holderDone)
	}()
	<-inHolder

	retired := make(chan error, 1)
	go func() {
		retired <- m.Retire(ctx, id)
	}()

	// Retire must NOT return while the holder is inside its critical
	// section (the drain waits it out).
	select {
	case err := <-retired:
		t.Fatalf("Retire returned (%v) while a resume callback still held the invoke mutex — dropped-holder regression", err)
	case <-holderDone:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: neither the holder finished nor Retire returned")
	}

	select {
	case err := <-retired:
		if err != nil {
			t.Fatalf("Retire: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Retire never returned after the holder released")
	}

	// Post-retire: the map entry is pruned and a fresh acquisition lazily
	// creates a NEW mutex (the documented re-hire shape).
	fresh := m.AcquireInvokeMutexForResume(id)
	if fresh == invokeMu {
		t.Error("post-retire acquisition returned the retired mutex — the map entry was not pruned")
	}
}
