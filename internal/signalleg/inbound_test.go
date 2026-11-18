package signalleg

// Tests for inbound Signal message routing (leaf 11b T3). They pin:
//
//   - Dedupe: a replayed (sourceUuid, timestamp) envelope routes once.
//   - Session binding: the Binding seam fires with number
//     "signal:<sourceUuid>" (never a call row - a message is not a call).
//   - Body injection: the message body reaches the session as prompt
//     material exactly once per distinct envelope.
//   - Attachments surface by path only.

import (
	"context"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/signal"
)

// fakeBinder is the Binding seam fake: it records resolve/bind calls and
// feeds bodies through a TurnRunner-shaped callback.
type fakeBinder struct {
	mu       sync.Mutex
	binds    []bindCall
	sessions map[string]signalSession // name -> handle
}

type bindCall struct {
	number     string
	employeeID string
	prompt     string
}

func (b *fakeBinder) ResolveSession(number, employeeID string) (signalSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessions == nil {
		b.sessions = map[string]signalSession{}
	}
	name := "signal-msg:" + employeeID + ":" + number
	if sess, ok := b.sessions[name]; ok {
		return sess, nil
	}
	sess := signalSession{ID: "sess-" + number, ConversationID: "conv-" + number}
	b.sessions[name] = sess
	return sess, nil
}

// BindMessage records one routed message (the seam RouteInbound drives).
func (b *fakeBinder) BindMessage(ctx context.Context, number, employeeID, prompt string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.binds = append(b.binds, bindCall{number: number, employeeID: employeeID, prompt: prompt})
	return nil
}

func (b *fakeBinder) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.binds)
}

func (b *fakeBinder) last() bindCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.binds[len(b.binds)-1]
}

func (b *fakeBinder) prompts() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.binds))
	for _, c := range b.binds {
		out = append(out, c.prompt)
	}
	return out
}

// newInboundLeg builds a minimal Leg for RouteInbound tests. It relies on
// leg.go (leaf 11a) for the Leg type; the deps here are only the pieces
// inbound routing consumes.
func newInboundLeg(binder *fakeBinder) *Leg {
	return &Leg{
		deps: Deps{
			Binding: binder,
			Logger:  discardLogger(),
		},
		log: discardLogger(),
	}
}

// TestRouteInboundDedupe pins the canonical rule: the same envelope
// twice routes the body ONCE.
func TestRouteInboundDedupe(t *testing.T) {
	binder := &fakeBinder{}
	l := newInboundLeg(binder)

	msg := signal.DataMessage{
		SourceUUID: "dddd4444-2222-3333-4444-555555555555",
		SourceName: "Signal Contact",
		Body:       "ping from signal",
		Timestamp:  1731000000999,
	}
	l.RouteInbound(msg)
	l.RouteInbound(msg) // replayed envelope

	if got := binder.count(); got != 1 {
		t.Fatalf("binding fired %d times, want 1 (replayed envelope must dedupe)", got)
	}
}

// TestRouteInboundBindsSignalNumber pins the session identity: the bind
// number is "signal:" + sourceUuid (never E.164, never a call row).
func TestRouteInboundBindsSignalNumber(t *testing.T) {
	binder := &fakeBinder{}
	l := newInboundLeg(binder)

	l.RouteInbound(signal.DataMessage{
		SourceUUID: "eeee5555-2222-3333-4444-555555555555",
		Body:       "hello",
		Timestamp:  1731000001000,
	})

	call := binder.last()
	if call.number != "signal:eeee5555-2222-3333-4444-555555555555" {
		t.Errorf("bind number = %q, want %q", call.number, "signal:eeee5555-2222-3333-4444-555555555555")
	}
}

// TestRouteInboundBodyInjected pins prompt material: the message body
// reaches the session prompt verbatim.
func TestRouteInboundBodyInjected(t *testing.T) {
	binder := &fakeBinder{}
	l := newInboundLeg(binder)

	l.RouteInbound(signal.DataMessage{
		SourceUUID: "ffff6666-2222-3333-4444-555555555555",
		Body:       "please reschedule my dentist appointment",
		Timestamp:  1731000001001,
	})

	prompts := binder.prompts()
	if len(prompts) != 1 || prompts[0] != "please reschedule my dentist appointment" {
		t.Errorf("prompts = %v, want exactly the message body", prompts)
	}
}

// TestRouteInboundDistinctEnvelopesRoute pins the dedupe key: two
// distinct timestamps from the same uuid both route; two uuids at the
// same timestamp both route.
func TestRouteInboundDistinctEnvelopesRoute(t *testing.T) {
	binder := &fakeBinder{}
	l := newInboundLeg(binder)
	const uuid = "aaaa7777-2222-3333-4444-555555555555"

	l.RouteInbound(signal.DataMessage{SourceUUID: uuid, Body: "first", Timestamp: 100})
	l.RouteInbound(signal.DataMessage{SourceUUID: uuid, Body: "second", Timestamp: 200})
	l.RouteInbound(signal.DataMessage{SourceUUID: "bbbb8888-2222-3333-4444-555555555555", Body: "third", Timestamp: 100})

	if got := binder.count(); got != 3 {
		t.Fatalf("binding fired %d times, want 3 distinct routes", got)
	}
	prompts := binder.prompts()
	for i, want := range []string{"first", "second", "third"} {
		if prompts[i] != want {
			t.Errorf("prompt %d = %q, want %q", i, prompts[i], want)
		}
	}
}

// TestRouteInboundEmptyBodySkipped pins the hygiene rule: an empty-body
// envelope (attachment-only) does not inject empty prompt material, but
// still binds the session (attachments arrive as context lines).
func TestRouteInboundEmptyBodySkipped(t *testing.T) {
	binder := &fakeBinder{}
	l := newInboundLeg(binder)

	l.RouteInbound(signal.DataMessage{
		SourceUUID:  "cccc9999-2222-3333-4444-555555555555",
		Body:        "",
		Attachments: []string{"/tmp/signal/attachment-1.jpg"},
		Timestamp:   1731000001002,
	})

	call := binder.last()
	if call.prompt == "" {
		t.Errorf("attachment-only message injected an empty prompt; want the attachment path as context")
	}
	if !contains(call.prompt, "/tmp/signal/attachment-1.jpg") {
		t.Errorf("prompt %q missing attachment path", call.prompt)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
