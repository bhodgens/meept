package daemon

// Leaf 12 T5: goal hold/release under -race with concurrent hangup; the
// release hook fires on EVERY terminal outcome exactly once; unknown-ccid
// release is a no-op; typed-nil / missing-dep construction fails cleanly.
// The wiring is exercised through fake seams only - no live agent loop,
// bus, or sqlite session store is needed for the hold semantics.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/pkg/models"
)

// holdProgressCapture counts progress publications (bus not needed).
type holdProgressCapture struct {
	mu     sync.Mutex
	events []voice.ProgressEvent
}

func (c *holdProgressCapture) PublishProgress(ev voice.ProgressEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

// newHoldWiring builds a VoiceEmployeeWiring over fakes: a real sqlite
// call store in a temp dir, an in-memory session store, and a throwaway
// template loop (never run - the hold tests never invoke turns).
func newHoldWiring(t *testing.T) (*VoiceEmployeeWiring, *holdProgressCapture) {
	t.Helper()
	logger := testLogger(t)
	sessions := session.NewMemoryStore(logger)
	template := agent.NewAgentLoop("template-voice", t.TempDir())
	mgr := agent.NewManager(agent.ManagerConfig{SessionStore: sessions})
	capture := &holdProgressCapture{}

	w, err := WireVoiceEmployee(VoiceEmployeeDeps{
		Sessions:      sessions,
		LoopManager:   mgr,
		TemplateLoop:  template,
		WorkingDir:    t.TempDir(),
		CallStorePath: filepath.Join(t.TempDir(), "calls.db"),
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("WireVoiceEmployee: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return w, capture
}

func TestVoiceEmployeeConstructionGuards(t *testing.T) {
	logger := testLogger(t)
	sessions := session.NewMemoryStore(logger)
	template := agent.NewAgentLoop("template-voice", t.TempDir())

	if _, err := WireVoiceEmployee(VoiceEmployeeDeps{
		CallStorePath: filepath.Join(t.TempDir(), "calls.db"),
		Logger:        logger,
	}); err == nil {
		t.Fatal("missing loop manager must fail construction")
	}
	if _, err := WireVoiceEmployee(VoiceEmployeeDeps{
		LoopManager:  agent.NewManager(agent.ManagerConfig{}),
		TemplateLoop: template,
		Logger:       logger,
	}); err == nil {
		t.Fatal("missing session store must fail construction")
	}
	if _, err := WireVoiceEmployee(VoiceEmployeeDeps{
		LoopManager:  agent.NewManager(agent.ManagerConfig{}),
		TemplateLoop: template,
		Sessions:     sessions,
		Logger:       logger,
	}); err == nil {
		t.Fatal("missing call store path must fail construction")
	}
}

func TestVoiceEmployeeHoldReleaseOnHangup(t *testing.T) {
	w, _ := newHoldWiring(t)

	// Round entry: take the hold, then learn the ccid (dial path order).
	release := w.AcquireCallHold("cc-h", "emp-hold")

	// The leaf-05 release hook must drop the hold. The direct release
	// handle is single-shot: after the hook already released, calling it
	// must be a no-op, never a double unlock.
	w.ReleaseHook("cc-h", voice.OutcomeHangup)
	done := make(chan struct{})
	go func() {
		release() // single-shot guard makes this a no-op
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("single-shot release handle deadlocked after hook release")
	}
}

func TestVoiceEmployeeHoldBlocksConcurrentAcquire(t *testing.T) {
	w, _ := newHoldWiring(t)

	first := w.AcquireCallHold("cc-1", "emp-b")

	acquired := make(chan struct{})
	go func() {
		second := w.AcquireCallHold("cc-2", "emp-b") // must block until release
		close(acquired)
		second()
	}()

	select {
	case <-acquired:
		t.Fatal("second acquire must block while the call holds the employee")
	case <-time.After(50 * time.Millisecond):
	}

	w.ReleaseHook("cc-1", voice.OutcomeHangup)

	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("second acquire did not unblock after release")
	}
	first()
}

func TestVoiceEmployeeReleaseHookOncePerOutcome(t *testing.T) {
	w, _ := newHoldWiring(t)

	release := w.AcquireCallHold("cc-o", "emp-o")

	// Duplicate terminal outcomes (Telnyx re-sends hangup, plus the
	// no-answer timer racing it): only the first may release.
	w.ReleaseHook("cc-o", voice.OutcomeHangup)
	w.ReleaseHook("cc-o", voice.OutcomeNoAnswer)

	done := make(chan struct{})
	go func() {
		next := w.AcquireCallHold("cc-next", "emp-o")
		close(done)
		next()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("hold was not released exactly once (double-release or missed release)")
	}
	release()
}

func TestVoiceEmployeeReleaseUnknownCallNoop(t *testing.T) {
	w, _ := newHoldWiring(t)
	// Must not panic and must not unlock anything.
	w.ReleaseHook("cc-unknown", voice.OutcomeFailed)
	// A live hold releases directly through the same once-guarded path the
	// hook uses.
	release := w.AcquireCallHold("cc-u", "emp-u")
	if w.ReleaseForCall("cc-unknown") {
		t.Fatal("release for unknown ccid must be a no-op")
	}
	if !w.ReleaseForCall("cc-u") {
		t.Fatal("release for registered call must report released")
	}
	release() // funnels through the consumed ccid: single-shot no-op
}

func TestVoiceEmployeeHoldEveryOutcome(t *testing.T) {
	outcomes := []voice.Outcome{
		voice.OutcomeHangup,
		voice.OutcomeNoAnswer,
		voice.OutcomeFailed,
		voice.OutcomeAnswered,
	}
	for _, oc := range outcomes {
		t.Run(string(oc), func(t *testing.T) {
			w, _ := newHoldWiring(t)
			release := w.AcquireCallHold("cc-"+string(oc), "emp-x")
			w.ReleaseHook("cc-"+string(oc), oc)
			done := make(chan struct{})
			go func() {
				next := w.AcquireCallHold("cc-after", "emp-x")
				close(done)
				next()
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("outcome %s did not release the hold", oc)
			}
			release()
		})
	}
}

func TestVoiceEmployeeTurnPersistExchange(t *testing.T) {
	logger := testLogger(t)
	sessions := session.NewMemoryStore(logger)
	template := agent.NewAgentLoop("template-voice", t.TempDir())
	mgr := agent.NewManager(agent.ManagerConfig{SessionStore: sessions})
	msgBus := bus.New(nil, logger)

	w, err := WireVoiceEmployee(VoiceEmployeeDeps{
		Sessions:      sessions,
		LoopManager:   mgr,
		TemplateLoop:  template,
		WorkingDir:    t.TempDir(),
		Bus:           msgBus,
		CallStorePath: filepath.Join(t.TempDir(), "calls.db"),
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("WireVoiceEmployee: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	// persistExchange writes user+assistant rows even when the loop turn
	// itself fails (persist happens on the reply path only in production;
	// here we exercise the saver directly through a runner).
	runner := &loopTurnRunner{
		mgr:        mgr,
		template:   template,
		workingDir: t.TempDir(),
		sessions:   sessions,
		logger:     logger,
	}
	sess, err := sessions.Create("voice call")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	runner.persistExchange(sess.ID, "hello caller", "hi there")
	msgs, err := sessions.GetMessages(sess.ID, 0, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("persisted %d messages; want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello caller" {
		t.Errorf("msg0 = %s/%q", msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "hi there" {
		t.Errorf("msg1 = %s/%q", msgs[1].Role, msgs[1].Content)
	}
	// Empty reply: only the user row.
	runner.persistExchange(sess.ID, "again", "")
	msgs, err = sessions.GetMessages(sess.ID, 0, 10)
	if err != nil {
		t.Fatalf("GetMessages 2: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("persisted %d messages after empty-reply exchange; want 3", len(msgs))
	}
}

func TestVoiceEmployeeProgressTypedTopic(t *testing.T) {
	logger := testLogger(t)
	sessions := session.NewMemoryStore(logger)
	template := agent.NewAgentLoop("template-voice", t.TempDir())
	mgr := agent.NewManager(agent.ManagerConfig{SessionStore: sessions})
	msgBus := bus.New(nil, logger)

	w, err := WireVoiceEmployee(VoiceEmployeeDeps{
		Sessions:      sessions,
		LoopManager:   mgr,
		TemplateLoop:  template,
		WorkingDir:    t.TempDir(),
		Bus:           msgBus,
		CallStorePath: filepath.Join(t.TempDir(), "calls.db"),
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("WireVoiceEmployee: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	// Subscribe through the typed topic: the compiler enforces the payload.
	got := make(chan voice.ProgressEvent, 8)
	sub := bus.SubscribeT(msgBus, "voice-progress-test", TopicVoiceCallProgress, func(ev voice.ProgressEvent) {
		got <- ev
	})
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })

	ctx := context.Background()
	if _, err := w.Binding.OnCallStart(ctx, "cc-bus", "+15551230010", "emp-bus", voice.DirectionIncoming, voice.DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}
	w.Binding.PublishSay("cc-bus")
	w.Binding.PublishInterrupt("cc-bus")

	deadline := time.After(2 * time.Second)
	kinds := make([]string, 0, 2)
	for len(kinds) < 2 {
		select {
		case ev := <-got:
			kinds = append(kinds, ev.Kind)
		case <-deadline:
			t.Fatalf("got %d progress events; want 2", len(kinds))
		}
	}
	if kinds[0] != voice.ProgressKindSay || kinds[1] != voice.ProgressKindInterrupt {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestVoiceEmployeeHoldRaceConcurrentHangup(t *testing.T) {
	w, _ := newHoldWiring(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			ccid := "cc-race-" + string(rune('a'+k)) //nolint:gosec // G115: k is bounded 0..7 by the range-8 loop, so rune('a'+k) cannot overflow
			emp := "emp-r" + string(rune('a'+k%2))   //nolint:gosec // G115: k%2 is bounded 0..1, cannot overflow int->rune
			release := w.AcquireCallHold(ccid, emp)
			w.ReleaseHook(ccid, voice.OutcomeHangup)
			release()
		}(i)
	}
	wg.Wait()
}

// notifyBusWiring is newHoldWiring with a live bus so the notify relay
// publishes somewhere observable; the returned bus is the wiring's.
func notifyBusWiring(t *testing.T) (*VoiceEmployeeWiring, *bus.MessageBus) {
	t.Helper()
	logger := testLogger(t)
	sessions := session.NewMemoryStore(logger)
	template := agent.NewAgentLoop("template-voice", t.TempDir())
	mgr := agent.NewManager(agent.ManagerConfig{SessionStore: sessions})
	msgBus := bus.New(nil, logger)
	w, err := WireVoiceEmployee(VoiceEmployeeDeps{
		Sessions:      sessions,
		LoopManager:   mgr,
		TemplateLoop:  template,
		WorkingDir:    t.TempDir(),
		Bus:           msgBus,
		CallStorePath: filepath.Join(t.TempDir(), "calls.db"),
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("WireVoiceEmployee: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return w, msgBus
}

// collectNotify decodes every employee.notify payload arriving on sub
// until deadline; a nil channel means the caller never subscribed.
func collectNotify(t *testing.T, sub <-chan *models.BusMessage, deadline time.Duration) []agent.NotifyPayload {
	t.Helper()
	var out []agent.NotifyPayload
	timer := time.After(deadline)
	for {
		select {
		case msg := <-sub:
			if msg == nil || len(msg.Payload) == 0 {
				continue
			}
			var p agent.NotifyPayload
			if err := json.Unmarshal(msg.Payload, &p); err != nil {
				t.Fatalf("bad notify payload: %v", err)
			}
			out = append(out, p)
		case <-timer:
			return out
		}
	}
}

func TestVoiceEmployeeNotifyRelayOnRelease(t *testing.T) {
	w, msgBus := notifyBusWiring(t)
	sub := msgBus.Subscribe("relay-test", agent.SpeakTopicNotify)
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })

	ctx := context.Background()
	if _, err := w.Binding.OnCallStart(ctx, "cc-relay", "+15551230001", "emp-relay", voice.DirectionIncoming, voice.DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}
	now := time.Now().UTC()
	if err := w.store.AppendTranscript(ctx, "cc-relay", voice.TranscriptLine{Role: "user", Text: "please take a message for Alice", At: now}); err != nil {
		t.Fatalf("AppendTranscript: %v", err)
	}
	if err := w.store.AppendTranscript(ctx, "cc-relay", voice.TranscriptLine{Role: "assistant", Text: "I will pass that along.", At: now}); err != nil {
		t.Fatalf("AppendTranscript: %v", err)
	}

	release := w.AcquireCallHold("cc-relay", "emp-relay")
	w.ReleaseHook("cc-relay", voice.OutcomeHangup)
	release()

	payloads := collectNotify(t, sub.Channel, 2*time.Second)
	if len(payloads) != 1 {
		t.Fatalf("got %d notify payloads; want 1", len(payloads))
	}
	p := payloads[0]
	if p.SessionID == "" {
		t.Error("payload session_id is empty")
	}
	for _, want := range []string{"caller: please take a message", "reply: I will pass that along", "emp-relay", "+15551230001", "incoming"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("payload text %q missing %q", p.Text, want)
		}
	}
}

func TestVoiceEmployeeNotifyRelaySkipsEmptyTranscript(t *testing.T) {
	w, msgBus := notifyBusWiring(t)
	sub := msgBus.Subscribe("relay-empty-test", agent.SpeakTopicNotify)
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })

	ctx := context.Background()
	if _, err := w.Binding.OnCallStart(ctx, "cc-empty", "+15551230002", "emp-empty", voice.DirectionOutgoing, voice.DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}

	release := w.AcquireCallHold("cc-empty", "emp-empty")
	w.ReleaseHook("cc-empty", voice.OutcomeNoAnswer)
	release()

	if payloads := collectNotify(t, sub.Channel, 300*time.Millisecond); len(payloads) != 0 {
		t.Fatalf("empty-transcript call notified %d payloads; want 0", len(payloads))
	}
}

func TestVoiceEmployeeNotifyRelayUnknownCallNoop(t *testing.T) {
	w, msgBus := notifyBusWiring(t)
	sub := msgBus.Subscribe("relay-unknown-test", agent.SpeakTopicNotify)
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })

	// Never started, never held: the relay must not panic or notify.
	w.ReleaseHook("cc-never", voice.OutcomeFailed)
	if payloads := collectNotify(t, sub.Channel, 200*time.Millisecond); len(payloads) != 0 {
		t.Fatalf("unknown-ccid release notified %d payloads; want 0", len(payloads))
	}
}

func TestRenderCallRelay(t *testing.T) {
	rec := voice.CallRecord{CallID: "cc-1", Number: "local:desk", EmployeeID: "emp-9", Direction: "incoming"}
	lines := []voice.TranscriptLine{
		{Role: "user", Text: "  hello there  "},
		{Role: "assistant", Text: "hi"},
	}
	got := renderCallRelay(rec, lines, "hangup")
	if !strings.Contains(got, "local leg") || !strings.Contains(got, "caller: hello there") || !strings.Contains(got, "reply: hi") {
		t.Errorf("renderCallRelay = %q", got)
	}

	// Budget: many long lines cap the transcript section.
	long := strings.Repeat("x", 300)
	var many []voice.TranscriptLine
	for range 5 {
		many = append(many, voice.TranscriptLine{Role: "user", Text: long})
	}
	capped := renderCallRelay(rec, many, "hangup")
	if len(capped) > len("call ended (hangup): employee emp-9 | local leg | incoming | local:desk\n")+relayTranscriptBudget+16 {
		t.Errorf("relay not capped: %d chars", len(capped))
	}
	if !strings.HasSuffix(capped, "...") {
		t.Errorf("capped relay missing ellipsis marker: %q", capped)
	}

	// Whitespace-only transcript notifies nothing.
	if got := renderCallRelay(rec, []voice.TranscriptLine{{Role: "user", Text: "   "}}, "hangup"); got != "" {
		t.Errorf("whitespace-only transcript rendered %q; want empty", got)
	}
}
