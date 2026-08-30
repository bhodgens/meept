package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/bus"
)

// fakeSpeakPublisher records every Deliver for assertions and is safe for
// concurrent use (the loop may deliver from background goroutines).
type fakeSpeakPublisher struct {
	mu    sync.Mutex
	calls []speakCall
	err   error // returned to every call when non-nil
}

type speakCall struct {
	kind           SpeakKind
	text           string
	sessionID      string
	conversationID string
}

func (f *fakeSpeakPublisher) publish(kind SpeakKind, text, sessionID, conversationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, speakCall{kind, text, sessionID, conversationID})
	return f.err
}

func (f *fakeSpeakPublisher) recorded() []speakCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]speakCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// TestClassifyRun_Matrix covers the full attachment×isolation matrix (C3/C4).
// C4 is fail-closed: isolated=true always yields SpeakParent, even when
// sessionAttached is (contradictorily) true.
func TestClassifyRun_Matrix(t *testing.T) {
	cases := []struct {
		name          string
		sessionAttach bool
		isolatedChild bool
		want          SpeakKind
	}{
		{"attached+not isolated=bubble", true, false, SpeakSession},
		{"attached+isolated=parent (C4 fail-closed)", true, true, SpeakParent},
		{"detached+not isolated=notify", false, false, SpeakNotify},
		{"detached+isolated=parent", false, true, SpeakParent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyRun(tc.sessionAttach, tc.isolatedChild)
			if got != tc.want {
				t.Errorf("ClassifyRun(%v, %v) = %s, want %s",
					tc.sessionAttach, tc.isolatedChild, got, tc.want)
			}
		})
	}
}

func TestSpeakKindString(t *testing.T) {
	for k, want := range map[SpeakKind]string{
		SpeakSession:  "session",
		SpeakNotify:   "notify",
		SpeakParent:   "parent",
		SpeakKind(99): "unknown",
	} {
		if got := k.String(); got != want {
			t.Errorf("SpeakKind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

// TestDeliver_RoutesByKind verifies the publisher receives the kind plus
// session/conversation IDs intact for each SpeakKind.
func TestDeliver_RoutesByKind(t *testing.T) {
	fake := &fakeSpeakPublisher{}
	router := NewSpeakRouter(fake.publish)

	cases := []struct {
		kind SpeakKind
	}{
		{SpeakSession}, {SpeakNotify}, {SpeakParent},
	}
	for _, tc := range cases {
		err := router.Deliver(context.Background(), tc.kind, "hello", "sess-1", "conv-1")
		if err != nil {
			t.Fatalf("Deliver(%s) returned error: %v", tc.kind, err)
		}
	}
	calls := fake.recorded()
	if len(calls) != 3 {
		t.Fatalf("publisher called %d times, want 3", len(calls))
	}
	for i, tc := range cases {
		got := calls[i]
		if got.kind != tc.kind {
			t.Errorf("call %d kind = %s, want %s", i, got.kind, tc.kind)
		}
		if got.text != "hello" || got.sessionID != "sess-1" || got.conversationID != "conv-1" {
			t.Errorf("call %d = %+v, want text=hello session=sess-1 conversation=conv-1", i, got)
		}
	}
}

// TestDeliver_EmptyTextNoOp verifies the C3 rule: empty (and
// whitespace-only) text is a no-op returning nil — a detached run with
// nothing to say must not notify.
func TestDeliver_EmptyTextNoOp(t *testing.T) {
	fake := &fakeSpeakPublisher{}
	router := NewSpeakRouter(fake.publish)

	for _, text := range []string{"", "   ", "\n\t "} {
		for _, kind := range []SpeakKind{SpeakSession, SpeakNotify, SpeakParent} {
			if err := router.Deliver(context.Background(), kind, text, "s", "c"); err != nil {
				t.Fatalf("Deliver(%s, %q) = %v, want nil", kind, text, err)
			}
		}
	}
	if calls := fake.recorded(); len(calls) != 0 {
		t.Errorf("empty text must not reach the publisher, got %d calls", len(calls))
	}
}

// TestDeliver_NilRouterSafe verifies Deliver on a nil *SpeakRouter is a
// no-op returning nil (defensive; call sites nil-guard anyway).
func TestDeliver_NilRouterSafe(t *testing.T) {
	var router *SpeakRouter
	if err := router.Deliver(context.Background(), SpeakNotify, "hi", "s", "c"); err != nil {
		t.Errorf("nil router Deliver = %v, want nil", err)
	}
}

// TestDeliver_NilPublisherNoOp verifies a router built without a publisher
// degrades to a logged no-op, not a panic.
func TestDeliver_NilPublisherNoOp(t *testing.T) {
	router := NewSpeakRouter(nil)
	if err := router.Deliver(context.Background(), SpeakNotify, "hi", "s", "c"); err != nil {
		t.Errorf("nil publisher Deliver = %v, want nil", err)
	}
}

// TestDeliver_PublisherErrorWrapped verifies publisher errors propagate to
// the caller (which logs them best-effort; the turn must not fail hard).
func TestDeliver_PublisherErrorWrapped(t *testing.T) {
	sentinel := errors.New("bus down")
	fake := &fakeSpeakPublisher{err: sentinel}
	router := NewSpeakRouter(fake.publish)

	err := router.Deliver(context.Background(), SpeakNotify, "hi", "s", "c")
	if !errors.Is(err, sentinel) {
		t.Errorf("Deliver error = %v, want wrapped %v", err, sentinel)
	}
}

// TestDeliverIsolatedNotify_SilentDrop documents C4: an isolated child's
// notify is silently dropped (nil error, publisher never called).
func TestDeliverIsolatedNotify_SilentDrop(t *testing.T) {
	fake := &fakeSpeakPublisher{}
	router := NewSpeakRouter(fake.publish)

	if err := router.DeliverIsolatedNotify(context.Background(), "leak", "s", "c"); err != nil {
		t.Errorf("DeliverIsolatedNotify = %v, want nil", err)
	}
	if calls := fake.recorded(); len(calls) != 0 {
		t.Errorf("isolated child notify reached publisher %d times, want 0 (C4)", len(calls))
	}
}

// TestSetPublisher_TypedNilGuard verifies the typed-nil setter guard: a nil
// function never replaces an existing publisher (ask.go pattern).
func TestSetPublisher_TypedNilGuard(t *testing.T) {
	fake := &fakeSpeakPublisher{}
	router := NewSpeakRouter(fake.publish)

	router.SetPublisher(nil) // must be ignored
	router.SetPublisher(fake.publish)

	if err := router.Deliver(context.Background(), SpeakNotify, "hi", "s", "c"); err != nil {
		t.Fatalf("Deliver = %v", err)
	}
	if calls := fake.recorded(); len(calls) != 1 {
		t.Errorf("publisher calls = %d, want 1 (original publisher retained)", len(calls))
	}
}

// TestBusSpeakPublisher_KindRouting verifies the production bus publisher:
// SpeakSession and SpeakParent are no-ops (bubble path unchanged; no user
// surface for parents), only SpeakNotify publishes employee.notify with the
// session_id/conversation_id/text payload.
func TestBusSpeakPublisher_KindRouting(t *testing.T) {
	t.Setenv("TZ", "UTC") // deterministic timestamps in payloads
	testBus := bus.New(nil, nil)
	defer testBus.Close()

	// Subscribe BEFORE publishing: bus.Publish is not a queue for late
	// subscribers — messages published before Subscribe are not replayed.
	sub := testBus.Subscribe("test-notify-drain", SpeakTopicNotify)

	pub := BusSpeakPublisher(testBus, "test-source")

	// Session and Parent: no publish.
	if err := pub(SpeakSession, "bubble text", "s", "c"); err != nil {
		t.Errorf("SpeakSession publish = %v, want nil", err)
	}
	if err := pub(SpeakParent, "parent report", "s", "c"); err != nil {
		t.Errorf("SpeakParent publish = %v, want nil", err)
	}
	// Notify: publishes.
	if err := pub(SpeakNotify, "goal done", "sess-9", "conv-9"); err != nil {
		t.Fatalf("SpeakNotify publish = %v", err)
	}

	select {
	case msg := <-sub.Channel:
		var payload NotifyPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("payload unmarshal: %v", err)
		}
		if payload.Text != "goal done" || payload.SessionID != "sess-9" || payload.ConversationID != "conv-9" {
			t.Errorf("payload = %+v, want text=goal done session=sess-9 conversation=conv-9", payload)
		}
	default:
		t.Fatal("employee.notify message not published")
	}
}

// TestBusSpeakPublisher_NilBusSafe verifies a nil bus yields a no-op
// publisher (never panics).
func TestBusSpeakPublisher_NilBusSafe(t *testing.T) {
	pub := BusSpeakPublisher(nil, "test")
	if err := pub(SpeakNotify, "hi", "s", "c"); err != nil {
		t.Errorf("nil bus publish = %v, want nil", err)
	}
}
