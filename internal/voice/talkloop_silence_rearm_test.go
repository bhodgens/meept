package voice

// Silence-clock re-arm pins (bughunt 2026-11-12, defect 7).
//
// Contract: the silence window is SUSPENDED from final-prompt dispatch to
// the draft DRAIN. A cancelled draft (barge-in / supersede) IS a drain for
// that purpose — cancelPipeline drops t.cur and bumps the generation but
// never touched the timers, so the suspension had no end: silenceC stayed
// nil forever, and only max_call_minutes could end the call.
//
// These pins run at ms scale via silenceWindowUnit (same discipline as the
// other silence-path files: no t.Parallel anywhere in this path).

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// turnEnds collects the call's end reason without a buffered channel the
// loop could block on, and signals waiters through a channel.
type turnEnds struct {
	mu     sync.Mutex
	reason string
	ch     chan string
}

func newTurnEnds() *turnEnds { return &turnEnds{ch: make(chan string, 1)} }

func (q *turnEnds) fn(ccid, reason string) {
	q.mu.Lock()
	q.reason = reason
	q.mu.Unlock()
	select {
	case q.ch <- reason:
	default:
	}
}

func (q *turnEnds) get() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.reason
}

func (q *turnEnds) wait(t *testing.T, d time.Duration) string {
	t.Helper()
	select {
	case r := <-q.ch:
		return r
	case <-time.After(d):
		t.Fatalf("call never ended (reason so far = %q)", q.get())
		return ""
	}
}

// countSay counts how many say frames carry exactly text.
func countSay(sink *recordSink, text string) int {
	n := 0
	for _, tx := range sink.sayTexts() {
		if tx == text {
			n++
		}
	}
	return n
}

// bargeInLoop builds a loop with a BLOCKED turn (gatedTurns never released)
// so the interrupt lands while a draft is genuinely in flight, then the
// draft dies without a drain: exactly the production shape of defect 7.
func bargeInLoop(t *testing.T, sink *recordSink, turns TurnRunner, ends *turnEnds, turnTimeout int) *TalkLoop {
	t.Helper()
	return NewTalkLoop(TalkDeps{
		Supervisor: &cannedSupervisor{},
		Turns:      turns,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       sink.Send,
		OnEnd:      ends.fn,
		Logger:     slog.New(&captureHandler{}),
	}, TalkLoopConfig{
		GreetingLine:              "Welcome.",
		SuppressGreeting:          true,
		SilencePromptSeconds:      20,
		SilencePromptLine:         "Are you still there?",
		SilenceHangupAfterSeconds: 20,
		TurnTimeoutSeconds:        turnTimeout,
	})
}

// dispatchFinal pushes a final prompt frame (last:true) into the loop.
func dispatchFinal(loop *TalkLoop, ccid, text string) {
	last := true
	loop.frames <- Frame{CallID: ccid, Event: EventPrompt, Text: text, Last: &last}
}

// TestTalkLoopSilenceRearmsAfterBargeIn is the defect-7 regression pin:
// dispatch a prompt (silence window suspended), barge in mid-turn so the
// draft is cancelled with NO drain, then let the clock advance. The nudge
// MUST fire: a cancelled draft ends the suspension exactly like a drained
// one. Pre-fix, silenceC was nil forever and only the max-call timer could
// end the call.
func TestTalkLoopSilenceRearmsAfterBargeIn(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	turns := &gatedTurns{reply: "Never spoken.", gate: make(chan struct{})}
	ends := newTurnEnds()
	loop := bargeInLoop(t, sink, turns, ends, 0)

	const ccid = "cc-barge-silence"
	cancel, done := runLoop(t, loop, ccid)
	defer func() { cancel(); <-done }()

	dispatchFinal(loop, ccid, "tell me something")

	waitFor(t, 2*time.Second, "turn never started", func() bool {
		turns.mu.Lock()
		defer turns.mu.Unlock()
		return turns.starts > 0
	})
	loop.frames <- Frame{CallID: ccid, Event: EventInterrupt}

	// The clock must restart from the cancel: the nudge fires S after the
	// barge-in, with the draft long dead.
	waitFor(t, 2*time.Second, "silence nudge never fired after barge-in", func() bool {
		return countSay(sink, "Are you still there?") > 0
	})
}

// TestTalkLoopSilenceHangupAfterBargeIn pins the terminal half: the H
// window re-arms too, so a caller who barges in and then says nothing is
// hung up on instead of holding the line until max_call_minutes.
func TestTalkLoopSilenceHangupAfterBargeIn(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	turns := &gatedTurns{reply: "Never spoken.", gate: make(chan struct{})}
	ends := newTurnEnds()
	loop := bargeInLoop(t, sink, turns, ends, 0)

	const ccid = "cc-barge-silence-hangup"
	cancel, done := runLoop(t, loop, ccid)
	defer func() { cancel(); <-done }()

	dispatchFinal(loop, ccid, "tell me something")
	waitFor(t, 2*time.Second, "turn never started", func() bool {
		turns.mu.Lock()
		defer turns.mu.Unlock()
		return turns.starts > 0
	})
	loop.frames <- Frame{CallID: ccid, Event: EventInterrupt}

	if r := ends.wait(t, 2*time.Second); !strings.Contains(r, "silence") {
		t.Fatalf("end reason = %q, want a silence-named reason", r)
	}
}

// TestTalkLoopSilenceRearmsAfterSupersede pins the supersede half: a newer
// prompt cancels the older draft, the new turn runs and drains normally,
// and the silence window re-arms from THAT drain (never firing between the
// supersede and the new dispatch).
func TestTalkLoopSilenceRearmsAfterSupersede(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	turns := &cannedTurns{reply: "Answered."}
	ends := newTurnEnds()
	loop := bargeInLoop(t, sink, turns, ends, 0)

	const ccid = "cc-supersede-silence"
	cancel, done := runLoop(t, loop, ccid)
	defer func() { cancel(); <-done }()

	dispatchFinal(loop, ccid, "first question")
	waitFor(t, 2*time.Second, "first turn never started", func() bool {
		turns.mu.Lock()
		defer turns.mu.Unlock()
		return turns.starts > 0
	})
	dispatchFinal(loop, ccid, "second question")

	// The drain of the SECOND draft re-arms the clock; dead air after it
	// must reach the nudge (the cancelled first draft must not have left
	// the clock stopped).
	waitFor(t, 2*time.Second, "silence nudge never fired after supersede+drain", func() bool {
		return countSay(sink, "Are you still there?") > 0
	})
	if got := strings.Count(strings.Join(sink.sayTexts(), "|"), "Are you still there?"); got != 1 {
		t.Errorf("nudge spoken %d times, want exactly 1", got)
	}
}

// TestTalkLoopSupersedeDoesNotNudgeWhileNewDraftInFlight pins the guard on
// the defect-7 re-arm: the cancel signal from the superseded draft must NOT
// restart the clock while the NEWER draft is still generating (that would
// un-suspend the turn-aware suspension mid-generation). The nudge may only
// come from the newer draft's own drain.
func TestTalkLoopSupersedeDoesNotNudgeWhileNewDraftInFlight(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	turns := &secondCallBlocksTurns{
		reply:   "Answered.",
		release: make(chan struct{}),
	}
	ends := newTurnEnds()
	loop := bargeInLoop(t, sink, turns, ends, 0)

	const ccid = "cc-supersede-suspended"
	cancel, done := runLoop(t, loop, ccid)
	defer func() { cancel(); <-done }()

	dispatchFinal(loop, ccid, "question that answers fast")
	waitFor(t, 2*time.Second, "first turn never finished draining", func() bool {
		turns.mu.Lock()
		defer turns.mu.Unlock()
		return turns.starts >= 1 && loop.curPipeline() == nil
	})

	// Dispatch a prompt whose draft BLOCKS, then a second prompt that
	// supersedes the blocked draft and spawns another blocked draft. The
	// supersede's cancel signal must not re-arm while the newer draft
	// lives.
	dispatchFinal(loop, ccid, "second question blocks")
	waitFor(t, 2*time.Second, "second turn never started", func() bool {
		turns.mu.Lock()
		defer turns.mu.Unlock()
		return turns.starts >= 2
	})
	dispatchFinal(loop, ccid, "third question supersedes")
	waitFor(t, 2*time.Second, "third turn never started", func() bool {
		turns.mu.Lock()
		defer turns.mu.Unlock()
		return turns.starts >= 3
	})

	// Far past S (20ms at the ms seam) with a live draft: the nudge must
	// NOT have fired — the clock is suspended for the in-flight turn.
	time.Sleep(150 * time.Millisecond)
	if n := countSay(sink, "Are you still there?"); n != 0 {
		t.Fatalf("silence nudge fired %d times while the superseding draft was in flight", n)
	}

	// Release the live draft: its normal drain re-arms the clock and the
	// nudge fires exactly once.
	close(turns.release)
	waitFor(t, 2*time.Second, "silence nudge never fired after the final drain", func() bool {
		return countSay(sink, "Are you still there?") > 0
	})
	if got := strings.Count(strings.Join(sink.sayTexts(), "|"), "Are you still there?"); got != 1 {
		t.Errorf("nudge spoken %d times, want exactly 1", got)
	}
}

// secondCallBlocksTurns answers the first call instantly and blocks every
// later call on release (so a supersede lands while the newer draft is
// genuinely mid-generation).
type secondCallBlocksTurns struct {
	mu      sync.Mutex
	reply   string
	starts  int
	release chan struct{}
}

func (s *secondCallBlocksTurns) Run(ctx context.Context, _ SessionHandle, _ string) (string, error) {
	s.mu.Lock()
	s.starts++
	n := s.starts
	s.mu.Unlock()
	if n == 1 {
		return s.reply, nil
	}
	select {
	case <-s.release:
		return s.reply, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// TestTalkLoopSilenceStaysSuspendedWhileTurnInFlight is the counter-pin:
// the fix must NOT re-arm while a live draft is still running. A long but
// finite turn holds the clock suspended; only its drain restarts the
// window, so the nudge can never fire mid-generation.
func TestTalkLoopSilenceStaysSuspendedWhileTurnInFlight(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	turns := &slowTurns{cannedTurns{reply: "Finally here."}, 300 * time.Millisecond}
	ends := newTurnEnds()
	loop := bargeInLoop(t, sink, turns, ends, 0)

	const ccid = "cc-suspend"
	cancel, done := runLoop(t, loop, ccid)
	defer func() { cancel(); <-done }()

	dispatchFinal(loop, ccid, "slow one")

	// While the turn is in flight the clock is suspended: well past S the
	// nudge must NOT have been spoken.
	time.Sleep(120 * time.Millisecond)
	if n := countSay(sink, "Are you still there?"); n != 0 {
		t.Fatalf("silence nudge fired %d times while a turn was in flight", n)
	}

	// The drain re-arms: dead air after the reply ends still reaches the
	// nudge (this also proves the drain branch is the re-arm point).
	waitFor(t, 3*time.Second, "silence nudge never fired after the drain", func() bool {
		return countSay(sink, "Are you still there?") > 0
	})
}
