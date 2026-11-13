package voice

// Talk loop tests (leaf 14 T3/T4 + the master Test Gate greeting/filler and
// pipelining rows). Order-sensitive assertions run through order recorders
// so the pins survive reordering of unrelated code.

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordSink captures every frame the loop sends, in order. gate, when
// non-nil, blocks each Send until released (observably slow playback for
// the pipelining pin).
type recordSink struct {
	mu     sync.Mutex
	frames []Frame
	gate   chan struct{}
}

func (r *recordSink) Send(f Frame) error {
	if r.gate != nil {
		select {
		case <-r.gate:
		case <-time.After(5 * time.Second):
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = append(r.frames, f)
	return nil
}

func (r *recordSink) all() []Frame {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Frame, len(r.frames))
	copy(out, r.frames)
	return out
}

func (r *recordSink) says() []Frame {
	var out []Frame
	for _, f := range r.all() {
		if f.Event == EventSay {
			out = append(out, f)
		}
	}
	return out
}

func (r *recordSink) sayTexts() []string {
	var out []string
	for _, f := range r.says() {
		out = append(out, f.Text)
	}
	return out
}

// cannedSupervisor returns fixed verdicts per sentence; it records Check
// start order for the pipelining pin.
type cannedSupervisor struct {
	mu      sync.Mutex
	allowed map[string]bool
	starts  []string
}

func (s *cannedSupervisor) Check(ctx context.Context, callID, sentence string) (Verdict, error) {
	s.mu.Lock()
	s.starts = append(s.starts, sentence)
	s.mu.Unlock()
	allowed, ok := s.allowed[strings.TrimSpace(sentence)]
	if !ok {
		allowed = true
	}
	return Verdict{Sentence: sentence, Allowed: allowed, Reason: "canned"}, nil
}

func (s *cannedSupervisor) startOrder() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.starts))
	copy(out, s.starts)
	return out
}

// cannedTurns returns a fixed reply per prompt and records turn starts.
type cannedTurns struct {
	mu     sync.Mutex
	reply  string
	starts int
}

func (t *cannedTurns) Run(ctx context.Context, sess SessionHandle, prompt string) (string, error) {
	t.mu.Lock()
	t.starts++
	reply := t.reply
	t.mu.Unlock()
	return reply, nil
}

// slowTurns delays its reply (models the employee lookup latency); it
// records the start BEFORE the delay so tests can interrupt mid-lookup.
type slowTurns struct {
	cannedTurns
	delay time.Duration
}

func (t *slowTurns) Run(ctx context.Context, sess SessionHandle, prompt string) (string, error) {
	t.mu.Lock()
	t.starts++
	t.mu.Unlock()
	time.Sleep(t.delay)
	return t.reply, nil
}

// constSession returns the SessionFor seam for tests.
func constSession(sess SessionHandle) func(Instance) SessionHandle {
	return func(Instance) SessionHandle { return sess }
}

// gatedTurns blocks its turn until the gate channel closes, then returns
// the canned reply. Models a slow cloud LLM (30-90s) or a hung one (gate
// never closed); records the start like cannedTurns.
type gatedTurns struct {
	mu     sync.Mutex
	reply  string
	starts int
	gate   chan struct{}
}

func (t *gatedTurns) Run(ctx context.Context, sess SessionHandle, prompt string) (string, error) {
	t.mu.Lock()
	t.starts++
	t.mu.Unlock()
	select {
	case <-t.gate:
		return t.reply, nil
	case <-ctx.Done():
		// Interrupt/teardown while parked: the draft dies silently.
		return "", ctx.Err()
	}
}

func newTestTalkLoop(t *testing.T, sink func(Frame) error, sup Supervisor, turns TurnRunner, cfg TalkLoopConfig) *TalkLoop {
	t.Helper()
	return NewTalkLoop(TalkDeps{
		Supervisor: sup,
		Turns:      turns,
		SessionFor: constSession(SessionHandle{ID: "s-1", ConversationID: "c-1"}),
		Send:       sink,
		OnEnd:      func(ccid, reason string) {},
		Logger:     testLogger(),
	}, cfg)
}

func runLoop(t *testing.T, loop *TalkLoop, ccid string) (context.CancelFunc, chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		if err := loop.Run(ctx, Instance{CallControlID: ccid}); err != nil {
			t.Logf("talk loop ended: %v", err)
		}
		close(done)
	}()
	return cancel, done
}

func waitFor(t *testing.T, deadline time.Duration, what string, cond func() bool) {
	t.Helper()
	at := time.Now().Add(deadline)
	for {
		if cond() {
			return
		}
		if time.Now().After(at) {
			t.Fatalf("%s within %s", what, deadline)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestTalkLoopGreetingAndFiller pins the master Test Gate row: greeting say
// on connect, filler say immediately on each final prompt, both before any
// supervisor work, both fact-free (last=false).
func TestTalkLoopGreetingAndFiller(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "The report is ready."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:  []string{"Hold on, let me check."},
		GreetingLine: "Hello, how can I help?",
	})

	cancel, done := runLoop(t, loop, "cc-greet")
	defer cancel()

	waitFor(t, 2*time.Second, "no greeting say", func() bool {
		says := sink.sayTexts()
		return len(says) >= 1
	})
	if says := sink.sayTexts(); says[0] != "Hello, how can I help?" {
		t.Fatalf("first say = %q, want greeting", says[0])
	}

	last := true
	loop.frames <- Frame{CallID: "cc-greet", Event: EventPrompt, Text: "is it done?", Last: &last}

	waitFor(t, 2*time.Second, "no greeting+filler+reply says", func() bool {
		return len(sink.sayTexts()) >= 3
	})
	texts := sink.sayTexts()
	if texts[1] != "Hold on, let me check." {
		t.Errorf("second say = %q, want filler before the employee reply", texts[1])
	}
	if texts[2] != "The report is ready." {
		t.Errorf("third say = %q, want the employee reply", texts[2])
	}
	cancel()
	<-done
}

// TestTalkLoopNoGreetingWhenUnconfigured: an empty filler list produces no
// greeting (never a hardcoded line).
func TestTalkLoopNoGreetingWhenUnconfigured(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "Done."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{})

	cancel, done := runLoop(t, loop, "cc-nogreet")
	defer cancel()

	time.Sleep(50 * time.Millisecond)
	if says := sink.sayTexts(); len(says) != 0 {
		t.Fatalf("unconfigured greeting produced says %v, want none", says)
	}
	cancel()
	<-done
}

// TestTalkLoopDenyDropsSentence pins T3's Given/When/Then: a two-sentence
// draft with the second sentence denied yields ONLY sentence 1 in say
// frames (the denied sentence NEVER enters a say frame).
func TestTalkLoopDenyDropsSentence(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{allowed: map[string]bool{
		"First allowed sentence.": true,
		"Denied secret value.":    false,
	}}
	turns := &cannedTurns{reply: "First allowed sentence. Denied secret value."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:  []string{"One moment."},
		GreetingLine: "Hello.",
		FallbackLine: "I cannot confirm that yet.",
	})

	cancel, done := runLoop(t, loop, "cc-deny")
	defer cancel()

	last := true
	loop.frames <- Frame{CallID: "cc-deny", Event: EventPrompt, Text: "go", Last: &last}

	waitFor(t, 2*time.Second, "draft says never completed", func() bool {
		return len(sink.sayTexts()) >= 3 // greeting + filler + sentence 1
	})

	joined := strings.Join(sink.sayTexts(), " | ")
	if strings.Contains(joined, "Denied secret value.") {
		t.Errorf("denied sentence reached a say frame: %v", sink.sayTexts())
	}
	if !strings.Contains(joined, "First allowed sentence.") {
		t.Errorf("allowed sentence missing from says: %v", sink.sayTexts())
	}
	if strings.Contains(joined, "I cannot confirm that yet.") {
		t.Errorf("fallback line spoken for a mid-draft deny (deny drops, it does not replace): %v", sink.sayTexts())
	}
	cancel()
	<-done
}

// TestTalkLoopAllDeniedSpeaksFallback pins the C6 fail-closed user
// experience: when every sentence of a draft is denied, the caller hears
// the fallback line instead of silence.
func TestTalkLoopAllDeniedSpeaksFallback(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{allowed: map[string]bool{
		"Secret one.": false,
		"Secret two.": false,
	}}
	turns := &cannedTurns{reply: "Secret one. Secret two."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:  []string{"One moment."},
		GreetingLine: "Hello.",
		FallbackLine: "I cannot confirm that yet.",
	})

	cancel, done := runLoop(t, loop, "cc-alldeny")
	defer cancel()

	last := true
	loop.frames <- Frame{CallID: "cc-alldeny", Event: EventPrompt, Text: "go", Last: &last}

	waitFor(t, 2*time.Second, "fallback line never spoken", func() bool {
		return slices.Contains(sink.sayTexts(), "I cannot confirm that yet.")
	})
	for _, tx := range sink.sayTexts() {
		if strings.Contains(tx, "Secret") {
			t.Errorf("denied sentence %q reached a say frame", tx)
		}
	}
	cancel()
	<-done
}

// TestTalkLoopCheckFailureSpeaksFallback pins C6's failure mode: a
// supervisor Check that ERRORS (timeout/transport) speaks the fallback line
// and the call continues (never quota-parked, never silent).
func TestTalkLoopCheckFailureSpeaksFallback(t *testing.T) {
	sink := &recordSink{}
	// errorSup denies via error: every Check returns a transport error.
	sup := &errorSup{}
	turns := &cannedTurns{reply: "Fact one. Fact two."}
	endReasons := make(chan string, 1)
	loop := NewTalkLoop(TalkDeps{
		Supervisor: sup,
		Turns:      turns,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) { endReasons <- reason },
		Logger:     testLogger(),
	}, TalkLoopConfig{
		FillerLines:  []string{"One moment."},
		GreetingLine: "Hello.",
		FallbackLine: "I cannot confirm that yet.",
	})

	cancel, done := runLoop(t, loop, "cc-supfail")
	defer cancel()

	last := true
	loop.frames <- Frame{CallID: "cc-supfail", Event: EventPrompt, Text: "go", Last: &last}

	waitFor(t, 2*time.Second, "fallback never spoken on check failure", func() bool {
		return slices.Contains(sink.sayTexts(), "I cannot confirm that yet.")
	})
	for _, tx := range sink.sayTexts() {
		if strings.Contains(tx, "Fact") {
			t.Errorf("un-checked fact %q reached a say frame (fail-closed violated)", tx)
		}
	}
	cancel()
	<-done
}

// errorSup fails every check (the C6 transport-error shape).
type errorSup struct{}

func (errorSup) Check(ctx context.Context, callID, sentence string) (Verdict, error) {
	return Verdict{Sentence: sentence}, context.DeadlineExceeded
}

// TestTalkLoopInterruptDropsPipeline pins the barge-in contract at the talk
// loop: an interrupt cancels the in-flight turn, and no pending sentence
// reaches a say frame afterward. The turn is slow so the interrupt lands
// mid-turn.
func TestTalkLoopInterruptDropsPipeline(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &slowTurns{cannedTurns{reply: "One. Two. Three."}, 400 * time.Millisecond}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:  []string{"Working."},
		GreetingLine: "Hi.",
	})

	cancel, done := runLoop(t, loop, "cc-barge")
	defer cancel()

	last := true
	loop.frames <- Frame{CallID: "cc-barge", Event: EventPrompt, Text: "tell me", Last: &last}

	// Wait for the turn to actually start, then barge in mid-lookup.
	waitFor(t, 2*time.Second, "turn never started", func() bool {
		turns.mu.Lock()
		defer turns.mu.Unlock()
		return turns.starts > 0
	})
	loop.frames <- Frame{CallID: "cc-barge", Event: EventInterrupt}

	// Give the (cancelled) pipeline ample time to leak if it would.
	time.Sleep(700 * time.Millisecond)

	for _, tx := range sink.sayTexts() {
		if tx == "One." || tx == "Two." || tx == "Three." {
			t.Errorf("post-interrupt say %q leaked from the dropped pipeline; says=%v", tx, sink.sayTexts())
		}
	}
	cancel()
	<-done
}

// TestTalkLoopPipelining pins T4 with an order recorder: the supervisor
// Check for sentence N+1 STARTS before the say(N) send returns. The sink's
// gate blocks playback sends (but not the greeting/filler, which pass
// through a passthrough window), so a serial loop could never have both
// checks in flight while the first sentence is still unsent.
func TestTalkLoopPipelining(t *testing.T) {
	// gate opens after the greeting + filler (2 frames); playback says
	// then block until the test releases it.
	sink := &recordSink{}
	gated := &gatedSink{inner: sink, allowFirst: 2, gate: make(chan struct{})}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "Alpha. Beta."}
	loop := newTestTalkLoop(t, gated.Send, sup, turns, TalkLoopConfig{
		FillerLines:  []string{"Filler."},
		GreetingLine: "Hey.",
	})

	cancel, done := runLoop(t, loop, "cc-pipe")
	defer cancel()

	last := true
	loop.frames <- Frame{CallID: "cc-pipe", Event: EventPrompt, Text: "go", Last: &last}

	// The pin: BOTH checks started while say(Alpha) is still blocked
	// behind the gate (the checker runs one sentence ahead of playback).
	waitFor(t, 3*time.Second, "checker did not run one sentence ahead of playback", func() bool {
		return len(sup.startOrder()) >= 2
	})

	gated.mu.Lock()
	sent := len(sink.frames)
	gated.mu.Unlock()
	if sent > 2 {
		t.Fatalf("playback advanced past the gate (%d frames) before release", sent)
	}
	close(gated.gate)
	cancel()
	<-done

	starts := sup.startOrder()
	if len(starts) < 2 || starts[0] != "Alpha." || starts[1] != "Beta." {
		t.Fatalf("check order = %v, want [Alpha. Beta.]", starts)
	}
}

// gatedSink passes the first allowFirst sends through and blocks the rest
// until its gate closes.
type gatedSink struct {
	inner      *recordSink
	allowFirst int
	gate       chan struct{}
	mu         sync.Mutex
	passed     int
}

func (g *gatedSink) Send(f Frame) error {
	g.mu.Lock()
	g.passed++
	allow := g.passed <= g.allowFirst
	g.mu.Unlock()
	if !allow {
		select {
		case <-g.gate:
		case <-time.After(5 * time.Second):
		}
	}
	return g.inner.Send(f)
}

// TestTalkLoopMaxDurationContextCancel pins loop termination on external
// teardown: ctx expiry ends the call through OnEnd (the wiring cancels the
// loop on hangup from any other path).
func TestTalkLoopMaxDurationContextCancel(t *testing.T) {
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
	}, TalkLoopConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := loop.Run(ctx, Instance{CallControlID: "cc-max"}); err != nil {
		t.Logf("talk loop ended: %v", err)
	}

	select {
	case r := <-endReasons:
		if r == "" {
			t.Fatal("empty end reason on teardown")
		}
	default:
		t.Fatal("OnEnd never fired on ctx expiry")
	}
}

// TestTalkLoopSilenceHangup pins the silence path: first silence fire
// speaks one filler-class prompt, second fire hangs up with a silence-named
// reason. Distinct greeting text so the prompt count is unambiguous.
func TestTalkLoopSilenceHangup(t *testing.T) {
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
		FillerLines:          []string{"Are you there?"},
		GreetingLine:         "Welcome.",
		SilencePromptSeconds: 1,
	})

	go func() {
		// Run's own error paths log at Debug; nothing to assert here - the
		// test selects on endReasons and does not wait for this goroutine.
		_ = loop.Run(context.Background(), Instance{CallControlID: "cc-silence"})
	}()

	select {
	case r := <-endReasons:
		if !strings.Contains(r, "silence") {
			t.Fatalf("end reason = %q, want a silence-named reason", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("silence hangup never fired")
	}

	prompts := 0
	for _, tx := range sink.sayTexts() {
		if tx == "Are you there?" {
			prompts++
		}
	}
	if prompts != 1 {
		t.Errorf("silence prompt spoken %d times, want exactly 1", prompts)
	}
}

// TestTalkLoopEndFrame pins the child end frame -> named hangup reason ->
// loop exit.
func TestTalkLoopEndFrame(t *testing.T) {
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
	}, TalkLoopConfig{})

	done := make(chan struct{})
	go func() {
		if err := loop.Run(context.Background(), Instance{CallControlID: "cc-end"}); err != nil {
			t.Logf("talk loop ended: %v", err)
		}
		close(done)
	}()

	loop.frames <- Frame{CallID: "cc-end", Event: EventEnd, Reason: "timeout"}

	select {
	case r := <-endReasons:
		if r != "child end: timeout" {
			t.Fatalf("end reason = %q, want \"child end: timeout\"", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("end frame did not terminate the loop")
	}
	select {
	case <-done:
	default:
		t.Fatal("Run did not return after the end frame")
	}
}

// TestTalkLoopPartialPromptsNeverTurn: prompts with last=false (or absent)
// never start an employee turn (C1: only final prompts start a turn).
func TestTalkLoopPartialPromptsNeverTurn(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "would be a leak"}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines: []string{"Filler."},
	})

	cancel, done := runLoop(t, loop, "cc-partial")
	defer cancel()

	notLast := false
	loop.frames <- Frame{CallID: "cc-partial", Event: EventPrompt, Text: "par", Last: &notLast}
	loop.frames <- Frame{CallID: "cc-partial", Event: EventPrompt, Text: "par"}
	time.Sleep(80 * time.Millisecond)

	turns.mu.Lock()
	starts := turns.starts
	turns.mu.Unlock()
	if starts != 0 {
		t.Errorf("partial prompts started %d turns, want 0", starts)
	}
	cancel()
	<-done
}

// lastTrueSays returns the say frames that carry the turn-final marker
// (last:true), whether on a spoken sentence or on the empty-text flush.
func lastTrueSays(frames []Frame) []Frame {
	var out []Frame
	for _, f := range frames {
		if f.Event != EventSay || f.Last == nil || !*f.Last {
			continue
		}
		out = append(out, f)
	}
	return out
}

// M2: a denied FINAL sentence after earlier sentences were spoken used to
// swallow the turn-final say flush - no last:true frame ever went out, so
// the child's turn-taking never saw the turn end. The drain must send the
// empty-text flush exactly once per draft.
func TestTalkLoopDeniedFinalFlushesTurnEnd(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{allowed: map[string]bool{
		"Visible fact.": true,
		"Denied tail.":  false,
	}}
	turns := &cannedTurns{reply: "Visible fact. Denied tail."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines: []string{"One moment."},
	})

	cancel, done := runLoop(t, loop, "cc-m2flush")
	defer cancel()

	last := true
	loop.frames <- Frame{CallID: "cc-m2flush", Event: EventPrompt, Text: "go", Last: &last}

	// The flush (last:true, empty text) appears once the checker closes
	// the verdict channel - deterministic, no timing guess.
	waitFor(t, 2*time.Second, "turn-final flush never sent after a denied final sentence", func() bool {
		for _, f := range lastTrueSays(sink.all()) {
			if f.Text == "" {
				return true
			}
		}
		return false
	})

	// Exactly once: the drain is the only flusher; settle and re-count.
	time.Sleep(150 * time.Millisecond)
	flushes := 0
	for _, f := range lastTrueSays(sink.all()) {
		if f.Text == "" {
			flushes++
		}
	}
	if flushes != 1 {
		t.Errorf("turn-final flush sent %d times, want exactly 1", flushes)
	}
	// The denied sentence still never reaches a say frame.
	for _, tx := range sink.sayTexts() {
		if strings.Contains(tx, "Denied") {
			t.Errorf("denied sentence %q reached a say frame", tx)
		}
	}
	cancel()
	<-done
}

// M2 exact-once companion pin: a normal draft whose final sentence is
// allowed carries the turn-final marker ON the sentence, and the drain
// adds no second flush (exactly one last:true say per draft).
func TestTalkLoopAllowedFinalSingleLastMarker(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "Alpha. Beta."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines: []string{"One moment."},
	})

	cancel, done := runLoop(t, loop, "cc-m2single")
	defer cancel()

	last := true
	loop.frames <- Frame{CallID: "cc-m2single", Event: EventPrompt, Text: "go", Last: &last}

	waitFor(t, 2*time.Second, "final sentence never spoken", func() bool {
		return slices.Contains(sink.sayTexts(), "Beta.")
	})
	time.Sleep(150 * time.Millisecond) // settle: any buggy second flush lands here

	markers := lastTrueSays(sink.all())
	if len(markers) != 1 {
		t.Fatalf("last:true says = %d, want exactly 1 (the final sentence)", len(markers))
	}
	if markers[0].Text != "Beta." {
		t.Errorf("last:true carried by %q, want the final sentence \"Beta.\"", markers[0].Text)
	}
	cancel()
	<-done
}

// M5: the silence timer used a bare Reset without draining a stale fire,
// so the silence path could double-fire (a second prompt, or an immediate
// bogus hangup right after a new prompt). With a real prompt between the
// two windows, the hangup must come only after the SECOND full window.
func TestTalkLoopSilenceTimerNoDoubleFire(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "Back online."}
	endReasons := make(chan string, 1)
	loop := NewTalkLoop(TalkDeps{
		Supervisor: sup,
		Turns:      turns,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) { endReasons <- reason },
		Logger:     testLogger(),
	}, TalkLoopConfig{
		GreetingLine:         "Welcome.", // distinct: the filler text stays unambiguous
		FillerLines:          []string{"Are you there?"},
		SilencePromptSeconds: 1,
	})

	promptCount := func() int {
		n := 0
		for _, tx := range sink.sayTexts() {
			if tx == "Are you there?" {
				n++
			}
		}
		return n
	}

	go func() {
		_ = loop.Run(context.Background(), Instance{CallControlID: "cc-m5"})
	}()

	// Window 1: silence fires once and speaks the prompt (the greeting is
	// the distinct "Welcome." line, so the count is unambiguous).
	waitFor(t, 5*time.Second, "first silence prompt never spoken", func() bool {
		return promptCount() >= 1
	})

	// Caller returns: a final prompt stops the timer and runs a quick
	// turn; the loop then re-arms a fresh window.
	last := true
	loop.frames <- Frame{CallID: "cc-m5", Event: EventPrompt, Text: "hello?", Last: &last}
	waitFor(t, 2*time.Second, "employee reply never spoken", func() bool {
		return slices.Contains(sink.sayTexts(), "Back online.")
	})
	baseline := promptCount()

	// If a stale fire survived, the loop reads it as the SECOND
	// consecutive fire and hangs up within milliseconds of the reply.
	// Healthy behavior: nothing ends the call inside the fresh window.
	select {
	case r := <-endReasons:
		t.Fatalf("early hangup %q right after a fresh prompt (stale silence fire consumed)", r)
	case <-time.After(800 * time.Millisecond):
	}

	// The re-armed window still works: its first fire speaks the prompt
	// once more, and the second consecutive fire ends the call with the
	// silence reason.
	select {
	case r := <-endReasons:
		if !strings.Contains(r, "silence") {
			t.Fatalf("end reason = %q, want a silence-named reason", r)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("re-armed silence window never hung up")
	}
	if got := promptCount(); got != baseline+1 {
		t.Errorf("silence prompt spoken %d times after the reply (baseline %d), want exactly one more", got, baseline)
	}
}

// TestTalkLoopSilenceNotWhileTurnInFlight pins the turn-aware silence
// rule: a final prompt whose employee turn outlives the whole silence
// window must NOT be reaped - the clock is suspended from prompt dispatch
// until the draft drains, then re-arms fresh. Before the fix the call
// died with the silence reason while the reply was still generating.
func TestTalkLoopSilenceNotWhileTurnInFlight(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	// The turn blocks well past the silence window: the reply arrives
	// only when the test releases the gate.
	release := make(chan struct{})
	turns := &gatedTurns{reply: "Finally, back.", gate: release}
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
		FillerLines:          []string{"Hold on."},
		SilencePromptSeconds: 1,
	})

	go func() {
		_ = loop.Run(context.Background(), Instance{CallControlID: "cc-turnaware"})
	}()

	last := true
	loop.frames <- Frame{CallID: "cc-turnaware", Event: EventPrompt, Text: "slow question", Last: &last}

	// Wait well past TWO full silence windows while the turn is in
	// flight. Legacy behavior would hang the call up here with a
	// silence-named reason (first fire: prompt; second fire: hangup).
	select {
	case r := <-endReasons:
		t.Fatalf("call reaped while the employee turn was in flight: %q", r)
	case <-time.After(2200 * time.Millisecond):
	}

	// The greeting ("Welcome.") is the only say so far besides the
	// prompt-dispatch filler ("Hold on."): the silence path shares the
	// filler list, so a second filler means a silence fire spoke while
	// the turn was in flight.
	fillers := 0
	for _, tx := range sink.sayTexts() {
		if tx == "Hold on." {
			fillers++
		}
	}
	if fillers != 1 {
		t.Errorf("filler spoken %d times while the turn was in flight, want exactly 1 (dispatch filler only; silence clock must stay stopped)", fillers)
	}

	// Release the gate: the slow reply lands, the draft drains, and the
	// silence window re-arms FRESH from that moment.
	close(release)
	waitFor(t, 2*time.Second, "slow reply never spoken after release", func() bool {
		return slices.Contains(sink.sayTexts(), "Finally, back.")
	})

	// The call survived the turn, and the re-armed window works: quiet
	// caller now => first fire speaks the prompt, second fire ends the
	// call with the silence reason.
	select {
	case r := <-endReasons:
		if !strings.Contains(r, "silence") {
			t.Fatalf("end reason = %q, want a silence-named reason after the turn drained", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("re-armed silence window never ended the call")
	}
}

// TestTalkLoopTurnTimeoutCap pins the opt-in turn cap: a hung employee LLM
// (turn never returns) is bounded by voice.turn_timeout_seconds - the
// only bound left once the silence clock is suspended for in-flight
// turns. The end reason names the cap.
func TestTalkLoopTurnTimeoutCap(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	// A turn that never finishes: the goroutine parks forever, mirroring
	// a hung LLM request.
	turns := &gatedTurns{reply: "never", gate: make(chan struct{})}
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
		FillerLines:          []string{"Are you there?"},
		SilencePromptSeconds: 1,
		TurnTimeoutSeconds:   1,
	})

	go func() {
		_ = loop.Run(context.Background(), Instance{CallControlID: "cc-turncap"})
	}()

	last := true
	loop.frames <- Frame{CallID: "cc-turncap", Event: EventPrompt, Text: "hello?", Last: &last}

	select {
	case r := <-endReasons:
		if !strings.Contains(r, "turn timeout") {
			t.Fatalf("end reason = %q, want a turn-timeout-named reason", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("turn timeout cap never fired on a hung turn")
	}
}

// ---------------------------------------------------------------------------
// Link half-duplex loop-side pins (turn-taking leaf): the HoldPrompt
// park/replace/flush/drop semantics run against a FAKE hold (no
// coordinator), so the TalkLoop seam contract is pinned independently of
// the link wiring. Pins: nil HoldPrompt = the legacy dispatch path (every
// pre-existing TestTalkLoop* run already builds zero-value deps); a
// denied final parks (no turn starts, kind=prompt_held); a SECOND held
// final REPLACES the pending one (latest question wins); FlushHeld
// releases on the Run goroutine with kind=prompt_released and re-consults
// the gate (a stale flush against a busy peer re-parks); an allowed
// prompt DROPS a stale park; a park alive at loop exit drops with reason
// "loop ended" and never dispatches into the dead call; SuppressGreeting
// opens silent; TurnState fires turn_start at dispatch + turn_final at
// drain.
// ---------------------------------------------------------------------------

// holdTurns counts dispatched turns (parked prompts must never reach it).
type holdTurns struct {
	mu     sync.Mutex
	starts int
	reply  string
}

func (h *holdTurns) Run(_ context.Context, _ SessionHandle, _ string) (string, error) {
	h.mu.Lock()
	h.starts++
	h.mu.Unlock()
	return h.reply, nil
}

func (h *holdTurns) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.starts
}

// fakeHold is the coordinator stand-in: the gate returns the current
// allow flag and counts denials (parks).
type fakeHold struct {
	mu        sync.Mutex
	allowNext bool
	parks     int
}

func (f *fakeHold) hold(_ Instance, _ func()) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	allow := f.allowNext
	if !allow {
		f.parks++
	}
	return allow
}

func (f *fakeHold) allow() {
	f.mu.Lock()
	f.allowNext = true
	f.mu.Unlock()
}

func (f *fakeHold) deny() {
	f.mu.Lock()
	f.allowNext = false
	f.mu.Unlock()
}

func (f *fakeHold) parkCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.parks
}

func newHoldTalkLoop(t *testing.T, sink *recordSink, sup Supervisor, turns TurnRunner, hold func(Instance, func()) bool, logger *slog.Logger, cfg TalkLoopConfig) *TalkLoop {
	t.Helper()
	return NewTalkLoop(TalkDeps{
		Supervisor: sup,
		Turns:      turns,
		SessionFor: constSession(SessionHandle{ID: "s-hold", ConversationID: "c-hold"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     logger,
		HoldPrompt: hold,
	}, cfg)
}

func finalPrompt(ccid, text string) Frame {
	last := true
	return Frame{CallID: ccid, Event: EventPrompt, Text: text, Last: &last}
}

func logContains(buf interface{ String() string }, wants ...string) bool {
	s := buf.String()
	for _, w := range wants {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

// TestTalkLoopHeldPromptParksAndFlushes pins the core park/release: a
// denied final parks (no dispatch, prompt_held logged), FlushHeld
// releases it (prompt_released logged, the turn dispatches with the
// parked text).
func TestTalkLoopHeldPromptParksAndFlushes(t *testing.T) {
	var buf syncBuffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &holdTurns{reply: "The report is ready."}
	hold := &fakeHold{allowNext: true}
	loop := newHoldTalkLoop(t, sink, sup, turns, hold.hold, logger, TalkLoopConfig{
		GreetingLine: "Hello.",
	})

	cancel, done := runLoop(t, loop, "cc-park")
	defer cancel()
	waitFor(t, 2*time.Second, "no greeting", func() bool { return len(sink.sayTexts()) >= 1 })

	hold.deny()
	loop.frames <- finalPrompt("cc-park", "is it done?")
	waitFor(t, 2*time.Second, "prompt_held line", func() bool {
		return logContains(&buf, `kind=prompt_held`, `text_preview="is it done?"`)
	})
	if n := turns.count(); n != 0 {
		t.Fatalf("turn started while held (%d), want 0", n)
	}
	if len(sink.sayTexts()) != 1 {
		t.Fatalf("says after park = %v, want greeting only", sink.sayTexts())
	}

	hold.allow()
	loop.FlushHeld()
	waitFor(t, 2*time.Second, "prompt_released line", func() bool {
		return logContains(&buf, `kind=prompt_released`, `text_preview="is it done?"`)
	})
	waitFor(t, 2*time.Second, "released turn dispatches", func() bool { return turns.count() == 1 })
	waitFor(t, 2*time.Second, "reply spoken", func() bool { return len(sink.sayTexts()) >= 2 })
	if sink.sayTexts()[1] != "The report is ready." {
		t.Fatalf("second say = %q, want the released turn's reply", sink.sayTexts()[1])
	}
	cancel()
	<-done
}

// TestTalkLoopHeldPromptLatestWins pins the one-slot park: a second held
// final REPLACES the pending one (latest question wins) - the release
// dispatches ONLY the newest text and the stale park never runs.
func TestTalkLoopHeldPromptLatestWins(t *testing.T) {
	var buf syncBuffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &holdTurns{reply: "The report is ready."}
	hold := &fakeHold{allowNext: false} // park from the first prompt on
	loop := newHoldTalkLoop(t, sink, sup, turns, hold.hold, logger, TalkLoopConfig{
		GreetingLine: "Hello.",
	})

	cancel, done := runLoop(t, loop, "cc-latest")
	defer cancel()
	waitFor(t, 2*time.Second, "no greeting", func() bool { return len(sink.sayTexts()) >= 1 })

	loop.frames <- finalPrompt("cc-latest", "stale question")
	waitFor(t, 2*time.Second, "stale park", func() bool {
		return logContains(&buf, `kind=prompt_held`, `text_preview="stale question"`)
	})
	loop.frames <- finalPrompt("cc-latest", "newest question")
	waitFor(t, 2*time.Second, "replace park", func() bool {
		return logContains(&buf, `text_preview="newest question"`, `replaced_pending=true`)
	})
	if n := turns.count(); n != 0 {
		t.Fatalf("turn starts = %d while both prompts held, want 0", n)
	}
	if hold.parkCount() != 2 {
		t.Fatalf("hold denials = %d, want 2", hold.parkCount())
	}

	hold.allow()
	loop.FlushHeld()
	waitFor(t, 2*time.Second, "newest released", func() bool {
		return logContains(&buf, `kind=prompt_released`, `text_preview="newest question"`)
	})
	waitFor(t, 2*time.Second, "newest dispatched", func() bool { return turns.count() == 1 })
	time.Sleep(50 * time.Millisecond)
	if n := turns.count(); n != 1 {
		t.Fatalf("turn starts = %d after release, want exactly 1 (the stale park leaked)", n)
	}
	loop.frames <- Frame{CallID: "cc-latest", Event: EventEnd}
	<-done
}

// TestTalkLoopHeldPromptDroppedByNewerDispatch pins the other
// latest-question-wins edge: the coordinator ALLOWS a fresher prompt
// while one is parked - the parked question drops with a named reason
// and never dispatches behind the fresher one.
func TestTalkLoopHeldPromptDroppedByNewerDispatch(t *testing.T) {
	var buf syncBuffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &holdTurns{reply: "The report is ready."}
	hold := &fakeHold{allowNext: false}
	loop := newHoldTalkLoop(t, sink, sup, turns, hold.hold, logger, TalkLoopConfig{
		GreetingLine: "Hello.",
	})

	cancel, done := runLoop(t, loop, "cc-floor")
	defer cancel()
	waitFor(t, 2*time.Second, "no greeting", func() bool { return len(sink.sayTexts()) >= 1 })

	loop.frames <- finalPrompt("cc-floor", "parked question")
	waitFor(t, 2*time.Second, "park", func() bool {
		return logContains(&buf, `text_preview="parked question"`)
	})

	hold.allow() // the fresher prompt takes the floor
	loop.frames <- finalPrompt("cc-floor", "fresher question")
	waitFor(t, 2*time.Second, "fresher dispatch", func() bool { return turns.count() == 1 })
	waitFor(t, 2*time.Second, "stale drop log", func() bool {
		return logContains(&buf, `kind=prompt_dropped`, `reason="newer prompt took the floor"`)
	})
	if hold.parkCount() != 1 {
		t.Fatalf("hold denials = %d, want 1 (the fresher prompt was allowed)", hold.parkCount())
	}

	// A late flush signal (a coordinator still believing a park is
	// pending) finds nothing parked: no dispatch, no release line.
	loop.FlushHeld()
	time.Sleep(50 * time.Millisecond)
	if n := turns.count(); n != 1 {
		t.Fatalf("turn starts = %d after stale flush, want 1", n)
	}
	if strings.Contains(buf.String(), "kind=prompt_released") {
		t.Fatalf("a release line appeared for a dropped park:\n%s", buf.String())
	}
	loop.frames <- Frame{CallID: "cc-floor", Event: EventEnd}
	<-done
}

// TestTalkLoopHeldPromptDroppedAtLoopEnd pins the exit rule: a park
// alive when the call ends is logged and dropped - never dispatched into
// a dead loop.
func TestTalkLoopHeldPromptDroppedAtLoopEnd(t *testing.T) {
	var buf syncBuffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &holdTurns{reply: "never"}
	hold := &fakeHold{allowNext: false}
	loop := newHoldTalkLoop(t, sink, sup, turns, hold.hold, logger, TalkLoopConfig{
		GreetingLine: "Hello.",
	})

	cancel, done := runLoop(t, loop, "cc-exit")
	defer cancel()
	waitFor(t, 2*time.Second, "no greeting", func() bool { return len(sink.sayTexts()) >= 1 })

	loop.frames <- finalPrompt("cc-exit", "parked at hangup")
	waitFor(t, 2*time.Second, "park", func() bool {
		return logContains(&buf, `text_preview="parked at hangup"`)
	})
	loop.frames <- Frame{CallID: "cc-exit", Event: EventEnd}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit on end frame")
	}
	if !logContains(&buf, `kind=prompt_dropped`, `reason="loop ended"`) {
		t.Fatalf("no loop-end drop line:\n%s", buf.String())
	}
	if n := turns.count(); n != 0 {
		t.Fatalf("turn starts = %d, want 0 - parked prompt dispatched into a dead call", n)
	}
}

// TestTalkLoopReleaseRegateReholdsOnBusyPeer pins the flush race: a
// stale flush signal whose re-gate finds the peer busy AGAIN re-parks
// (prompt_reheld) and does not dispatch; the next flush with the gate
// open releases for real.
func TestTalkLoopReleaseRegateReholdsOnBusyPeer(t *testing.T) {
	var buf syncBuffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &holdTurns{reply: "ok."}
	hold := &fakeHold{allowNext: false}
	loop := newHoldTalkLoop(t, sink, sup, turns, hold.hold, logger, TalkLoopConfig{
		GreetingLine: "Hello.",
	})

	cancel, done := runLoop(t, loop, "cc-regate")
	defer cancel()
	waitFor(t, 2*time.Second, "no greeting", func() bool { return len(sink.sayTexts()) >= 1 })

	loop.frames <- finalPrompt("cc-regate", "question under a new turn")
	waitFor(t, 2*time.Second, "park", func() bool { return strings.Contains(buf.String(), "kind=prompt_held") })

	// Flush while the gate still denies: re-gate re-parks.
	loop.FlushHeld()
	waitFor(t, 2*time.Second, "re-held line", func() bool {
		return strings.Contains(buf.String(), "kind=prompt_reheld")
	})
	if n := turns.count(); n != 0 {
		t.Fatalf("turn started on a denied re-gate flush (%d), want 0", n)
	}

	// Gate opens: the re-parked prompt rides the next release.
	hold.allow()
	loop.FlushHeld()
	waitFor(t, 2*time.Second, "dispatched", func() bool { return turns.count() == 1 })
	loop.frames <- Frame{CallID: "cc-regate", Event: EventEnd}
	<-done
}

// TestTalkLoopTurnStateEdges pins the observation seam: turn_start at
// dispatch, turn_final at drain. Nil seam = zero emissions = the legacy
// path (every existing TestTalkLoop* run already proves that shape).
func TestTalkLoopTurnStateEdges(t *testing.T) {
	var mu sync.Mutex
	var states []string
	record := func(ccid string, seq uint64, kind string) {
		mu.Lock()
		states = append(states, ccid+":"+kind)
		mu.Unlock()
	}
	sink := &recordSink{}
	turns := &holdTurns{reply: "Done."}
	loop := NewTalkLoop(TalkDeps{
		Supervisor: &cannedSupervisor{},
		Turns:      turns,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     testLogger(),
		TurnState:  record,
	}, TalkLoopConfig{})

	cancel, done := runLoop(t, loop, "cc-state")
	defer cancel()

	loop.frames <- finalPrompt("cc-state", "status?")
	waitFor(t, 2*time.Second, "turn_final", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(states) >= 2
	})
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if states[0] != "cc-state:turn_start" || states[1] != "cc-state:turn_final" {
		t.Fatalf("turn-state edges = %v, want [turn_start turn_final]", states)
	}
}

// TestTalkLoopSuppressGreeting pins the single-greeting rule: with
// SuppressGreeting the loop opens silent (no say frames at connect), and
// prompts still flow normally.
func TestTalkLoopSuppressGreeting(t *testing.T) {
	sink := &recordSink{}
	turns := &holdTurns{reply: "Answer."}
	loop := newTestTalkLoop(t, sink.Send, &cannedSupervisor{}, turns, TalkLoopConfig{
		GreetingLine:     "Hello, how can I help?",
		FillerLines:      []string{"One moment."},
		SuppressGreeting: true,
	})

	cancel, done := runLoop(t, loop, "cc-silent")
	defer cancel()
	time.Sleep(80 * time.Millisecond)
	if says := sink.sayTexts(); len(says) != 0 {
		t.Fatalf("suppressed-greeting loop said %v at connect, want silence", says)
	}

	loop.frames <- finalPrompt("cc-silent", "question")
	waitFor(t, 2*time.Second, "answer after prompt", func() bool { return len(sink.sayTexts()) >= 1 })
	if got := strings.Join(sink.sayTexts(), " | "); strings.Contains(got, "Hello, how can I help?") {
		t.Fatalf("greeting leaked into a suppressed run: %v", sink.sayTexts())
	}
	cancel()
	<-done
}

// TestSplitFirstSentence pins the fast-first split boundary: the first
// sentence ends at the first terminal '.?!'/newline, everything after is
// the remainder; a single-sentence / punctuationless string returns
// (whole, "").
func TestSplitFirstSentence(t *testing.T) {
	cases := []struct {
		in, first, rest string
	}{
		{"Thanks for waiting. I checked the schedule and tomorrow is free at ten. Does that work?", "Thanks for waiting.", "I checked the schedule and tomorrow is free at ten. Does that work?"},
		{"Turn on the lights", "Turn on the lights", ""},
		{"Hello!", "Hello!", ""},
		{"One sentence. Two sentence.", "One sentence.", "Two sentence."},
		{"", "", ""},
	}
	for _, c := range cases {
		f, r := splitFirstSentence(c.in)
		if f != c.first || r != c.rest {
			t.Errorf("splitFirstSentence(%q) = (%q, %q), want (%q, %q)", c.in, f, r, c.first, c.rest)
		}
	}
}

// TestTalkLoopConnectSentencesSpeaksOneUtterance pins the natural-prosody
// path (supervisor gate off): a multi-sentence draft is spoken as TWO
// connected say frames — the FIRST sentence ships immediately (fast-first-
// audio: it is short, so its TTS starts while the rest generates), then the
// REMAINDER follows as the final connected utterance. This replaces the
// old whole-reply-as-one-frame shape, which waited for the ENTIRE (non-
// streaming) TTS synthesis before any audio — measured 2-5.6s in live call
// local-fbf9a76ef9357454. The per-sentence split is ONLY for the gate; with
// no gate to satisfy there is nothing to check per sentence.
func TestTalkLoopConnectSentencesSpeaksOneUtterance(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{allowed: map[string]bool{}}
	turns := &cannedTurns{reply: "Thanks for waiting. I checked the schedule and tomorrow is free at ten. Does that work?"}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:      []string{"One moment."},
		GreetingLine:     "Hello.",
		ConnectSentences: true,
	})

	cancel, done := runLoop(t, loop, "cc-connect")
	defer cancel()

	loop.frames <- finalPrompt("cc-connect", "question")
	waitFor(t, 2*time.Second, "connected utterance never spoken", func() bool {
		return len(sink.sayTexts()) >= 4 // greeting + filler + first sentence + remainder
	})

	var replyTexts []string
	for _, tx := range sink.sayTexts() {
		if strings.Contains(tx, "Thanks for waiting") || strings.Contains(tx, "I checked the schedule") {
			replyTexts = append(replyTexts, tx)
		}
	}
	// Fast-first: the opening sentence is its own short say, and the
	// remainder is the second (final, connected) say.
	if len(replyTexts) != 2 {
		t.Fatalf("connected reply emitted %d reply frames, want 2 (first sentence fast, remainder connected): %v", len(replyTexts), sink.sayTexts())
	}
	if replyTexts[0] != "Thanks for waiting." {
		t.Fatalf("first reply frame = %q, want the first sentence spoken immediately", replyTexts[0])
	}
	if replyTexts[1] != "I checked the schedule and tomorrow is free at ten. Does that work?" {
		t.Fatalf("remainder reply frame = %q, want the connected remainder", replyTexts[1])
	}
	// The supervisor checked ONE unit (the whole draft once, not per
	// sentence) — the fast-first split is delivery-only.
	if starts := sup.startOrder(); len(starts) != 1 || starts[0] != "Thanks for waiting. I checked the schedule and tomorrow is free at ten. Does that work?" {
		t.Fatalf("supervisor Check calls = %d/%q, want exactly one call for the whole draft", len(starts), starts)
	}
	cancel()
	<-done
}

// TestTalkLoopPerSentenceSpeaksSplit pins the byte-identical old path when
// ConnectSentences is false (supervisor gate on): the split still emits one
// say frame per allowed sentence — the gate needs per-sentence granularity.
func TestTalkLoopPerSentenceSpeaksSplit(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{allowed: map[string]bool{
		"First sentence.":  true,
		"Second sentence.": true,
	}}
	turns := &cannedTurns{reply: "First sentence. Second sentence."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:  []string{"One moment."},
		GreetingLine: "Hello.",
	})

	cancel, done := runLoop(t, loop, "cc-split")
	defer cancel()

	loop.frames <- finalPrompt("cc-split", "question")
	waitFor(t, 2*time.Second, "sentences never spoken", func() bool {
		return len(sink.sayTexts()) >= 4 // greeting + filler + 2 sentence reply
	})

	var replies []string
	for _, tx := range sink.sayTexts() {
		if strings.Contains(tx, "sentence.") {
			replies = append(replies, tx)
		}
	}
	if len(replies) != 2 {
		t.Fatalf("per-sentence reply emitted %d say frames, want 2 (one per allowed sentence): %v", len(replies), sink.sayTexts())
	}
	// The supervisor checked each sentence separately (2 calls).
	if starts := sup.startOrder(); len(starts) != 2 {
		t.Fatalf("supervisor Check calls = %d, want 2 (per-sentence)", len(starts))
	}
	cancel()
	<-done
}

// TestTalkLoopFragmentClarify is the conversational acknowledgment: a
// dispatched prompt that ended in a trailing ellipsis (the caller trailed
// off mid-thought) speaks the configured clarify line and SKIPS the LLM
// turn — a human says "Was that all? I'm not sure I understand you" rather
// than coldly answering the half-broken sentence. Pins voice.fragment_clarify.
func TestTalkLoopFragmentClarify(t *testing.T) {
	sink := &recordSink{}
	turns := &countingTurns{reply: "Unexpected: the LLM must NOT run for a fragment"}
	sup := &cannedSupervisor{allowed: map[string]bool{}}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:      []string{"One moment."},
		GreetingLine:     "Hello.",
		ConnectSentences: true,
		FragmentClarify:  "Was that all? I'm not sure I understand you.",
	})

	cancel, done := runLoop(t, loop, "cc-fragclarify")
	defer cancel()

	loop.frames <- finalPrompt("cc-fragclarify", "I was just wondering if you've got any t...")
	waitFor(t, 2*time.Second, "clarify line never spoken", func() bool {
		return slices.Contains(sink.sayTexts(), "Was that all? I'm not sure I understand you.")
	})
	// The LLM turn must NOT have run for an unresolved fragment.
	if starts := turns.startCount(); starts != 0 {
		t.Fatalf("LLM Runs started for a fragment prompt = %d, want 0 (clarify line replaces the turn)", starts)
	}
	// A single trailing '.' (a completed one-word command) must NOT clarify.
	if isTrailingEllipsis("Stop.") {
		t.Fatalf("isTrailingEllipsis('Stop.') = true, want false (single dot is a completed command)")
	}
	cancel()
	<-done
}

// TestTalkLoopFragmentClarifySkipsComplete turns MUST run normally when
// FragmentClarify is configured but the prompt is a complete sentence (no
// trailing ellipsis) — a real question still reaches the LLM, and a real
// one-word command like "Stop." is never clarified away.
func TestTalkLoopFragmentClarifySkipsComplete(t *testing.T) {
	sink := &recordSink{}
	turns := &countingTurns{reply: "The schedule is free tomorrow at ten."}
	sup := &cannedSupervisor{allowed: map[string]bool{}}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:      []string{"One moment."},
		GreetingLine:     "Hello.",
		ConnectSentences: true,
		FragmentClarify:  "Was that all? I'm not sure I understand you.",
	})

	cancel, done := runLoop(t, loop, "cc-fragcomplete")
	defer cancel()

	loop.frames <- finalPrompt("cc-fragcomplete", "Is the schedule free tomorrow at ten?")
	waitFor(t, 2*time.Second, "complete sentence never answered", func() bool {
		return len(sink.sayTexts()) >= 3 // greeting + filler + connected reply
	})
	if starts := turns.startCount(); starts != 1 {
		t.Fatalf("LLM Runs for a complete prompt = %d, want 1 (complete sentences must answer)", starts)
	}
	joined := strings.Join(sink.sayTexts(), " | ")
	if strings.Contains(joined, "Was that all?") {
		t.Fatalf("clarify line spoken for a complete sentence: %v", sink.sayTexts())
	}
	cancel()
	<-done
}

// TestTalkLoopFragmentClarifyOff preserves byte-equality when
// FragmentClarify is empty (the default): even an ellipsis prompt runs the
// LLM turn exactly as the legacy path did.
func TestTalkLoopFragmentClarifyOff(t *testing.T) {
	sink := &recordSink{}
	turns := &countingTurns{reply: "Let me check that for you."}
	sup := &cannedSupervisor{allowed: map[string]bool{}}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:      []string{"One moment."},
		GreetingLine:     "Hello.",
		ConnectSentences: true, // clarify NOT set
	})

	cancel, done := runLoop(t, loop, "cc-fragoff")
	defer cancel()

	loop.frames <- finalPrompt("cc-fragoff", "I was just wondering if you've got any t...")
	waitFor(t, 2*time.Second, "turn never answered", func() bool {
		return len(sink.sayTexts()) >= 3
	})
	if starts := turns.startCount(); starts != 1 {
		t.Fatalf("LLM Runs with clarify off = %d, want 1 (legacy: fragment still reaches the turn)", starts)
	}
	cancel()
	<-done
}

// countingTurns records how many LLM turns ran (for the skip assertions).
type countingTurns struct {
	mu      sync.Mutex
	reply   string
	started int
}

func (t *countingTurns) Run(ctx context.Context, sess SessionHandle, prompt string) (string, error) {
	t.mu.Lock()
	t.started++
	t.mu.Unlock()
	return t.reply, nil
}

func (t *countingTurns) startCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.started
}
