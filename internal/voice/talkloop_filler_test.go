package voice

// talkloop_filler_test.go - delayed-filler + turn-telemetry pins (leaf
// 02, dead-air defect 2026-11-10): filler must NOT fire for a turn that
// answers inside the grace window, MUST fire once when it does not, and a
// cancelled draft must never emit zombie filler after the grace elapses.

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTalkLoopFillerSuppressedInsideGrace(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "The report is ready."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines: []string{"Give me a second on that."},
		FillerGrace: time.Hour, // effectively never fires: turn is instant
	})

	cancel, done := runLoop(t, loop, "cc-grace")
	defer cancel()
	waitFor(t, 2*time.Second, "no greeting", func() bool { return len(sink.sayTexts()) >= 1 })

	last := true
	loop.frames <- Frame{CallID: "cc-grace", Event: EventPrompt, Text: "is it done?", Last: &last}
	waitFor(t, 2*time.Second, "no reply", func() bool { return len(sink.sayTexts()) >= 2 })
	texts := sink.sayTexts()
	if texts[1] != "The report is ready." {
		t.Fatalf("second say = %q, want the reply WITHOUT filler inside grace", texts[1])
	}
	// Drain settles; no late filler ever appears.
	time.Sleep(50 * time.Millisecond)
	if n := len(sink.sayTexts()); n != 2 {
		t.Fatalf("says = %d (%v), want exactly greeting+reply - filler leaked", n, sink.sayTexts())
	}
	cancel()
	<-done
}

func TestTalkLoopFillerFiresAfterGrace(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &slowTurns{reply: "Checked. All clear.", delay: 400 * time.Millisecond}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:  []string{"One moment."},
		GreetingLine: "Hello.",
		FillerGrace:  30 * time.Millisecond,
	})

	cancel, done := runLoop(t, loop, "cc-slow")
	defer cancel()
	waitFor(t, 2*time.Second, "no greeting", func() bool { return len(sink.sayTexts()) >= 1 })

	last := true
	loop.frames <- Frame{CallID: "cc-slow", Event: EventPrompt, Text: "status of the deploy?", Last: &last}
	waitFor(t, 2*time.Second, "no delayed filler", func() bool { return len(sink.sayTexts()) >= 2 })
	texts := sink.sayTexts()
	if texts[1] != "One moment." {
		t.Fatalf("second say = %q, want the filler after grace", texts[1])
	}
	// The canned supervisor speaks the reply sentence-by-sentence;
	// everything after the filler must be the reply's sentences.
	waitFor(t, 2*time.Second, "no reply", func() bool {
		return strings.Join(sink.sayTexts()[2:], " ") == "Checked. All clear."
	})
	cancel()
	<-done
}

func TestTalkLoopCancelledDraftNeverEmitsZombieFiller(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &slowTurns{reply: "late", delay: 2 * time.Second}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:  []string{"One moment."},
		GreetingLine: "Hello.",
		FillerGrace:  60 * time.Millisecond,
	})

	cancel, done := runLoop(t, loop, "cc-zombie")
	defer cancel()
	waitFor(t, 2*time.Second, "no greeting", func() bool { return len(sink.sayTexts()) >= 1 })

	last := true
	loop.frames <- Frame{CallID: "cc-zombie", Event: EventPrompt, Text: "long question", Last: &last}
	// Let the draft register, then hang up BEFORE grace elapses.
	time.Sleep(20 * time.Millisecond)
	loop.frames <- Frame{CallID: "cc-zombie", Event: EventEnd}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit on end frame")
	}
	// Past the grace AND past the slow turn's completion: the cancelled
	// draft must never speak filler into a dead call.
	time.Sleep(150 * time.Millisecond)
	for _, s := range sink.sayTexts() {
		if s == "One moment." {
			t.Fatalf("zombie filler after hangup: %v", sink.sayTexts())
		}
	}
}

func TestTalkLoopTurnTelemetryLines(t *testing.T) {
	// syncBuffer, not bytes.Buffer: the loop's Run goroutine keeps
	// logging (turn-final telemetry lands after the last say) while
	// this test reads the buffer - an unsynchronized bytes.Buffer is a
	// data race under -race -count=2 (pre-existing flake, caught while
	// gating the drain-driven floor work).
	var buf syncBuffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "The report is ready."}
	loop := NewTalkLoop(TalkDeps{
		Supervisor: sup,
		Turns:      turns,
		SessionFor: constSession(SessionHandle{ID: "s-1", ConversationID: "c-1"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     logger,
	}, TalkLoopConfig{
		FillerLines: []string{"One moment."},
		FillerGrace: time.Hour, // answers inside grace every time
	})

	cancel, done := runLoop(t, loop, "cc-tel")
	defer cancel()
	waitFor(t, 2*time.Second, "no greeting", func() bool { return len(sink.sayTexts()) >= 1 })
	last := true
	loop.frames <- Frame{CallID: "cc-tel", Event: EventPrompt, Text: "is it done yet please?", Last: &last}
	waitFor(t, 2*time.Second, "no reply", func() bool { return len(sink.sayTexts()) >= 2 })

	logs := buf.String()
	for _, want := range []string{
		`msg="voice talk loop: turn dispatch"`,
		`kind=prompt_dispatch`,
		`text_preview="is it done yet please?"`,
		`words=4`,
		`msg="voice talk loop: turn filler skipped"`,
		`reason="answered inside grace"`,
		`msg="voice talk loop: turn final"`,
		`reply_words=4`,
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("telemetry missing %q\nlog was:\n%s", want, logs)
		}
	}
	cancel()
	<-done
}

// failingSaySink fails the FIRST non-final say carrying wantFailText,
// then succeeds for everything else (greeting, filler, retries). Models
// a transient child-socket write failure on the reply's first sentence.
type failingSaySink struct {
	mu         sync.Mutex
	frames     []Frame
	failText   string
	failedOnce bool
}

func (s *failingSaySink) Send(f Frame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames = append(s.frames, f)
	if f.Event == EventSay && f.Text == s.failText && !s.failedOnce {
		s.failedOnce = true
		return errors.New("simulated say write failure")
	}
	return nil
}

func (s *failingSaySink) sayTexts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, f := range s.frames {
		if f.Event == EventSay {
			out = append(out, f.Text)
		}
	}
	return out
}

func (s *failingSaySink) failed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failedOnce
}

// gatingSupervisor allows the first sentence immediately but blocks the
// second Check until release closes — models the generation/supervision
// gap that keeps a multi-sentence draft LIVE after its first say failed.
type gatingSupervisor struct {
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (s *gatingSupervisor) Check(_ context.Context, _, sentence string) (Verdict, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	if n >= 2 {
		<-s.release
	}
	return Verdict{Sentence: sentence, Allowed: true, Reason: "gated"}, nil
}

// TestTalkLoopSayFailureRearmsFiller pins bughunt 2026-11-19 L5: when an
// allowed sentence's say write fails mid-turn, the rollback must RE-ARM the
// grace timer (stopFillerLocked already stopped it), so filler still covers
// the dead air while the turn keeps generating. Before the fix the latch
// rolled back but no timer remained — permanent dead air. The reply is
// multi-sentence and the second Check is gated so the draft is still live
// when the re-armed grace elapses (a single-sentence instant reply would end
// the draft and legitimately stop the filler at turn-final).
func TestTalkLoopSayFailureRearmsFiller(t *testing.T) {
	sink := &failingSaySink{failText: "First sentence."}
	sup := &gatingSupervisor{release: make(chan struct{})}
	turns := &cannedTurns{reply: "First sentence. Second sentence."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:  []string{"One moment please."},
		GreetingLine: "Hello.",
		FillerGrace:  80 * time.Millisecond,
	})

	cancel, done := runLoop(t, loop, "cc-sayfail")
	defer func() { close(sup.release); cancel(); <-done }()
	waitFor(t, 2*time.Second, "no greeting", func() bool { return len(sink.sayTexts()) >= 1 })

	last := true
	loop.frames <- Frame{CallID: "cc-sayfail", Event: EventPrompt, Text: "give me the two-part status?", Last: &last}

	// The failed say must have been attempted...
	waitFor(t, 2*time.Second, "no failed say attempt", sink.failed)
	// ...and the re-armed grace timer must then speak the filler while the
	// second sentence is still gated in supervision (draft still live).
	waitFor(t, 2*time.Second, "filler never covered the dead air after say failure", func() bool {
		return slices.Contains(sink.sayTexts(), "One moment please.")
	})
	texts := sink.sayTexts()
	if texts[0] != "Hello." {
		t.Fatalf("first say = %q, want the greeting", texts[0])
	}
	fillerCount := 0
	for _, tx := range texts {
		if tx == "One moment please." {
			fillerCount++
		}
	}
	if fillerCount != 1 {
		t.Fatalf("filler spoke %d times, want exactly 1: %v", fillerCount, texts)
	}
}

// TestTalkLoopSayFailureNoZombieFillerAfterCancel pins the re-arm's safety
// boundary: a draft cancelled (barge-in/teardown) after a failed say must
// never emit the re-armed filler — the timer closure re-checks t.cur/gen
// under interruptMu, same discipline as the dispatch-time timer.
func TestTalkLoopSayFailureNoZombieFillerAfterCancel(t *testing.T) {
	sink := &failingSaySink{failText: "First sentence."}
	sup := &gatingSupervisor{release: make(chan struct{})}
	turns := &cannedTurns{reply: "First sentence. Second sentence."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:  []string{"One moment."},
		GreetingLine: "Hello.",
		FillerGrace:  200 * time.Millisecond,
	})

	cancel, done := runLoop(t, loop, "cc-sayfail-cancel")
	defer func() { close(sup.release); cancel(); <-done }()
	waitFor(t, 2*time.Second, "no greeting", func() bool { return len(sink.sayTexts()) >= 1 })

	last := true
	loop.frames <- Frame{CallID: "cc-sayfail-cancel", Event: EventPrompt, Text: "status?", Last: &last}
	waitFor(t, 2*time.Second, "no failed say attempt", sink.failed)

	// Barge-in immediately after the failed say: the draft dies before
	// the re-armed grace window elapses.
	loop.frames <- Frame{CallID: "cc-sayfail-cancel", Event: EventInterrupt}
	time.Sleep(350 * time.Millisecond) // well past the 200ms re-armed grace
	if slices.Contains(sink.sayTexts(), "One moment.") {
		t.Fatalf("zombie filler fired after cancel: %v", sink.sayTexts())
	}
}
