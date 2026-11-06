package voice

// Talk loop tests (leaf 14 T3/T4 + the master Test Gate greeting/filler and
// pipelining rows). Order-sensitive assertions run through order recorders
// so the pins survive reordering of unrelated code.

import (
	"context"
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
		for _, tx := range sink.sayTexts() {
			if tx == "I cannot confirm that yet." {
				return true
			}
		}
		return false
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
		for _, tx := range sink.sayTexts() {
			if tx == "I cannot confirm that yet." {
				return true
			}
		}
		return false
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
		loop.Run(context.Background(), Instance{CallControlID: "cc-silence"})
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
		for _, tx := range sink.sayTexts() {
			if tx == "Beta." {
				return true
			}
		}
		return false
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
		loop.Run(context.Background(), Instance{CallControlID: "cc-m5"})
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
		for _, tx := range sink.sayTexts() {
			if tx == "Back online." {
				return true
			}
		}
		return false
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
