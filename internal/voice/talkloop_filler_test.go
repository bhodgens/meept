package voice

// talkloop_filler_test.go - delayed-filler + turn-telemetry pins (leaf
// 02, dead-air defect 2026-11-10): filler must NOT fire for a turn that
// answers inside the grace window, MUST fire once when it does not, and a
// cancelled draft must never emit zombie filler after the grace elapses.

import (
	"log/slog"
	"strings"
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
