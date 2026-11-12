package voice

// Silence nudge line + S/H window pins (plan 20261111-voice-turn2 leaf 02).
// Operator contract: dead air speaks the dedicated "still there" line, NOT
// a filler answer-pending promise; nudge fires at S =
// silence_prompt_seconds, hangup fires at S+H (H =
// silence_hangup_after_seconds; H<=0 reuses S — the legacy double-window
// byte-compat). Timers run at millisecond scale through the
// silenceWindowUnit seam; no test in this file calls t.Parallel, and no
// silence-path test anywhere in the package does, so the mutation is safe
// (go test runs same-package tests sequentially unless a test opts in).

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureHandler records every slog record for assertions (attr lookup by
// key), with the unit's TextHandler shape kept identical (we read
// Record(), no formatting).
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

// WithAttrs/WithGroup keep the capture simple: the logger is built raw
// (no With paths in the talk loop), so returning the same recorder is
// correct and avoids copying the mutex.
func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *captureHandler) find(msg string) (slog.Record, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message == msg {
			return r, true
		}
	}
	return slog.Record{}, false
}

func attrValue(r slog.Record, key string) (any, bool) {
	var val any
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			val = a.Value.Any()
			found = true
			return false
		}
		return true
	})
	return val, found
}

// withMsSilenceUnit shrinks silenceWindowUnit to milliseconds for the test
// body and restores the second-scale production value via t.Cleanup.
func withMsSilenceUnit(t *testing.T) {
	t.Helper()
	prev := silenceWindowUnit
	silenceWindowUnit = time.Millisecond
	t.Cleanup(func() { silenceWindowUnit = prev })
}

// TestTalkLoopSilenceNudgeUsesConfiguredLine pins (a): the first silence
// fire speaks SilencePromptLine — NOT the filler round-robin — and does
// NOT consume a filler index (the next dispatched turn's filler is still
// filler[0]). The fire emits the INFO telemetry line with the spoken line
// and the real elapsed window (after_s).
func TestTalkLoopSilenceNudgeUsesConfiguredLine(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "Back with an answer."}
	capH := &captureHandler{}
	logger := slog.New(capH)
	endReasons := make(chan string, 1)

	loop := NewTalkLoop(TalkDeps{
		Supervisor: sup,
		Turns:      turns,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) { endReasons <- reason },
		Logger:     logger,
	}, TalkLoopConfig{
		FillerLines:               []string{"Give me a second on that.", "One moment."},
		GreetingLine:              "Welcome.",
		SilencePromptSeconds:      30,
		SilencePromptLine:         "Are you still there?",
		SilenceHangupAfterSeconds: 90,
	})

	cancel, done := runLoop(t, loop, "cc-nudge")
	defer func() { cancel(); <-done }()

	waitFor(t, 2*time.Second, "silence nudge never spoken", func() bool {
		return strings.Contains(strings.Join(sink.sayTexts(), "|"), "Are you still there?")
	})

	says := sink.sayTexts()
	nudges := 0
	for _, tx := range says {
		if tx == "Are you still there?" {
			nudges++
		}
		if tx == "Give me a second on that." || tx == "One moment." {
			t.Errorf("filler-class line %q spoken on the silence path, want only the nudge", tx)
		}
	}
	if nudges != 1 {
		t.Errorf("silence nudge spoken %d times, want exactly 1", nudges)
	}
	// The configured line must NOT consume a filler round-robin index
	// (the legacy chain did; the dedicated line leaves the filler cursor
	// where the greeting/dispatch semantics put it).
	if idx := loop.fillerIdx; idx != 0 {
		t.Errorf("fillerIdx = %d after the nudge, want 0 (no filler consumed)", idx)
	}

	// Telemetry pin: INFO line names the spoken line + after_s elapsed
	// since the re-arm (ms unit => after_s ~0.03, well under 1 s; the
	// pin is presence + sane magnitude, not wall-clock precision).
	rec, ok := capH.find("voice talk loop: silence prompt")
	if !ok {
		t.Fatal("silence prompt telemetry INFO line missing")
	}
	if line, ok := attrValue(rec, "line"); !ok || line != "Are you still there?" {
		t.Errorf("telemetry line attr = %v (ok=%v), want the nudge text", line, ok)
	}
	after, ok := attrValue(rec, "after_s")
	if !ok {
		t.Fatal("telemetry after_s attr missing")
	}
	afterS, isF := after.(float64)
	if !isF {
		t.Fatalf("telemetry after_s attr type = %T, want float64", after)
	}
	if afterS <= 0 || afterS > 5 {
		t.Errorf("telemetry after_s = %v, want a small positive seconds value", afterS)
	}

	// Latch pin: a second consecutive silence fire ends the call instead
	// of nudging twice.
	select {
	case r := <-endReasons:
		if !strings.Contains(r, "silence") {
			t.Fatalf("end reason = %q, want a silence-named reason", r)
		}
		if !strings.Contains(r, "(90s") {
			t.Errorf("end reason = %q, want it to name the H window (90s)", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("silence hangup never fired after the nudge")
	}
	if n := strings.Count(strings.Join(sink.sayTexts(), "|"), "Are you still there?"); n != 1 {
		t.Errorf("silence nudge spoken %d times across two fires, want exactly 1", n)
	}
}

// TestTalkLoopSilenceLegacyFallbackWhenLineEmpty pins (b): SilencePromptLine
// empty => byte-compatible legacy behavior — the filler round-robin line is
// spoken AND a filler index is consumed (the next dispatched prompt's
// filler advances to the next entry).
func TestTalkLoopSilenceLegacyFallbackWhenLineEmpty(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "Answer."}
	loop := NewTalkLoop(TalkDeps{
		Supervisor: sup,
		Turns:      turns,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     testLogger(),
	}, TalkLoopConfig{
		GreetingLine:         "Welcome.", // distinct: the filler text stays unambiguous
		FillerLines:          []string{"Filler one.", "Filler two."},
		SilencePromptSeconds: 30,
		// SilencePromptLine deliberately empty -> legacy chain.
	})

	cancel, done := runLoop(t, loop, "cc-legacy")
	defer func() { cancel(); <-done }()

	// First silence fire speaks filler[0] (index consumed).
	waitFor(t, 2*time.Second, "legacy silence filler never spoken", func() bool {
		return strings.Contains(strings.Join(sink.sayTexts(), "|"), "Filler one.")
	})

	// Caller returns: the dispatched prompt's grace==0 filler must be
	// filler[1] — proof the silence fire consumed index 0 exactly like
	// the legacy code (which called fillerLine() directly).
	last := true
	loop.frames <- Frame{CallID: "cc-legacy", Event: EventPrompt, Text: "hello?", Last: &last}
	waitFor(t, 2*time.Second, "dispatch filler never spoken", func() bool {
		return strings.Contains(strings.Join(sink.sayTexts(), "|"), "Filler two.")
	})
}

// TestTalkLoopSilenceHangupWindowIsH pins (c): with S=30 and H=90
// (millisecond units) the hangup fires S+H after the window armed, not
// 2S — the gap from nudge to hangup is the H window.
func TestTalkLoopSilenceHangupWindowIsH(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "ok."}
	endReasons := make(chan string, 1)
	loop := NewTalkLoop(TalkDeps{
		Supervisor: sup,
		Turns:      turns,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) { endReasons <- reason },
		Logger:     testLogger(),
	}, TalkLoopConfig{
		GreetingLine:              "Welcome.",
		SilencePromptLine:         "Still there?",
		SilencePromptSeconds:      30,
		SilenceHangupAfterSeconds: 90,
	})

	cancel, done := runLoop(t, loop, "cc-sh")
	defer func() { cancel(); <-done }()

	// Wait for the nudge, then time the hangup gap.
	var nudgeAt time.Time
	waitFor(t, 2*time.Second, "nudge never spoken", func() bool {
		if strings.Contains(strings.Join(sink.sayTexts(), "|"), "Still there?") {
			nudgeAt = time.Now()
			return true
		}
		return false
	})

	select {
	case r := <-endReasons:
		gap := time.Since(nudgeAt)
		if !strings.Contains(r, "silence") {
			t.Fatalf("end reason = %q, want silence-named", r)
		}
		// H window = 90 ms. A legacy S+S hangup would land at ~30 ms.
		// Lower bound 70 ms: H minus generous scheduler slack. The
		// 2x-S comparison is only meaningful if S is comfortably
		// under the lower bound — with S=30 the S+S land (~30) and
		// the S+H land (~90) are 3x apart.
		if gap < 70*time.Millisecond {
			t.Errorf("nudge->hangup gap = %v, want >= 70ms (H=90ms window, not the S=30ms legacy window)", gap)
		}
		if gap > 1*time.Second {
			t.Errorf("nudge->hangup gap = %v, want well under 1s (90ms window)", gap)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("silence hangup never fired")
	}
}

// TestTalkLoopSilenceHangupWindowDefaultsToS pins the H<=0 legacy
// equivalence: unset SilenceHangupAfterSeconds re-arms the SAME window
// after the nudge (double-S shape), and the reason names S.
func TestTalkLoopSilenceHangupWindowDefaultsToS(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "ok."}
	endReasons := make(chan string, 1)
	loop := NewTalkLoop(TalkDeps{
		Supervisor: sup,
		Turns:      turns,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) { endReasons <- reason },
		Logger:     testLogger(),
	}, TalkLoopConfig{
		GreetingLine:         "Welcome.",
		SilencePromptLine:    "Still there?",
		SilencePromptSeconds: 40,
		// SilenceHangupAfterSeconds unset -> legacy double-window.
	})

	cancel, done := runLoop(t, loop, "cc-legacy-h")
	defer func() { cancel(); <-done }()

	var nudgeAt time.Time
	waitFor(t, 2*time.Second, "nudge never spoken", func() bool {
		if strings.Contains(strings.Join(sink.sayTexts(), "|"), "Still there?") {
			nudgeAt = time.Now()
			return true
		}
		return false
	})

	select {
	case r := <-endReasons:
		if !strings.Contains(r, "silence (40s") {
			t.Errorf("end reason = %q, want it to name the reused S window (40s)", r)
		}
		// Double-S window (~40 ms) must land well under 70 ms.
		if gap := time.Since(nudgeAt); gap > 70*time.Millisecond {
			t.Errorf("legacy nudge->hangup gap = %v, want the S window (~40ms)", gap)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("silence hangup never fired on the legacy H=0 path")
	}
}
