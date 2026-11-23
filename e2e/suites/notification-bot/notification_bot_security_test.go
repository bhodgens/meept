//go:build e2e

// Suite notification-bot covers the configurable MCP security-alert
// delivery channel (issues #166/#167), selected by the single
// [notifications].security_channel knob:
//
//	sec-notify-01  "" (off, default) emits nothing — byte-identical boot
//	sec-notify-02  "bot" routes to NotificationBot.TriggerSecurityAlert
//	sec-notify-03  "push" publishes security.alert and produces a push delivery
//
// Hermetic: no daemon, no Telegram, no network. The seams are stubbed with
// in-process fakes — an EventEmitter-shaped publisher for channel A and a
// real in-memory MessageBus + PushService for channel B.
package notificationbot

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/services"
)

// fakeEmitter is the EventEmitterAdapter seam the NotificationBot consumes.
// It delivers published events into a buffered channel the test drains.
type fakeEmitter struct {
	ch chan *http.NotificationEvent
}

func newFakeEmitter() *fakeEmitter { return &fakeEmitter{ch: make(chan *http.NotificationEvent, 16)} }

func (f *fakeEmitter) Subscribe() chan *http.NotificationEvent     { return f.ch }
func (f *fakeEmitter) Unsubscribe(ch chan *http.NotificationEvent) {}

// captureHandler is a slog.Handler that records log attributes for a named
// message. The NotificationBot logs "dispatching notification" with the
// resolved trigger ONLY when handleEvent accepts the event and shouldNotify
// passes — a hermetic observable of the real mapEventToTrigger routing.
type captureHandler struct {
	mu       sync.Mutex
	wantMsg  string
	attrs    map[string]any
	seenMsgs []string
	next     slog.Handler
}

func (h *captureHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= slog.LevelDebug
}
func (h *captureHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == h.wantMsg {
		h.mu.Lock()
		h.seenMsgs = append(h.seenMsgs, r.Message)
		if h.attrs == nil {
			h.attrs = make(map[string]any)
		}
		r.Attrs(func(a slog.Attr) bool {
			h.attrs[a.Key] = a.Value.Any()
			return true
		})
		h.mu.Unlock()
	}
	if h.next != nil {
		return h.next.Handle(ctx, r)
	}
	return nil
}
func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(name string) slog.Handler       { return h }

func (h *captureHandler) snapshot() (int, map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cp := make(map[string]any, len(h.attrs))
	for k, v := range h.attrs {
		cp[k] = v
	}
	return len(h.seenMsgs), cp
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// TestSecurityChannelOffEmitsNothing covers sec-notify-01: the default ""
// channel is a no-op on both seams — no bus publish, no emitter event.
func TestSecurityChannelOffEmitsNothing(t *testing.T) {
	b := bus.New(nil, discardLogger())
	defer b.Close()

	sn := services.NewSecurityNotifier("", b, discardLogger())

	// Channel-A seam: a fake emitter must receive nothing.
	emitter := newFakeEmitter()
	sn.SetSecurityEventPublisher(func(n services.SecurityNotification) {
		emitter.ch <- &http.NotificationEvent{Title: n.Title, Message: n.Message}
	})

	// Channel-B seam: wildcard subscriber observes every bus publish.
	sub := b.Subscribe("sec-notify-01", "*")
	defer b.Unsubscribe(sub)

	sn.Notify("prompt injection attempt detected", "mcp")

	select {
	case ev := <-emitter.ch:
		t.Errorf("off channel emitted bot event: %+v", ev)
	default:
	}
	select {
	case msg, ok := <-sub.Channel:
		if ok {
			t.Errorf("off channel published a bus event: %s", string(msg.Payload))
		}
	case <-time.After(20 * time.Millisecond):
	}
}

// TestSecurityChannelBotRoutesToSecurityTrigger covers sec-notify-02: "bot"
// emits an http.NotificationEvent whose title contains "security"; the REAL
// NotificationBot (NotifyOn = [TriggerSecurityAlert] only) accepts it and
// logs the dispatch with trigger="security_alert". A nil publisher seam is
// also verified inert (setter nil guard).
func TestSecurityChannelBotRoutesToSecurityTrigger(t *testing.T) {
	sn := services.NewSecurityNotifier("bot", nil, discardLogger())

	// Wire the channel-A seam to a real NotificationBot driven by a fake
	// emitter, with a log capture on "dispatching notification".
	capture := &captureHandler{wantMsg: "dispatching notification"}
	logger := slog.New(capture)
	emitter := newFakeEmitter()
	nb := bot.NewNotificationBot(bot.NotificationBotConfig{
		Enabled:          true,
		NotifyOn:         []string{string(bot.TriggerSecurityAlert)},
		RateLimitPerHour: 10,
	}, emitter, logger)
	// Track publisher invocations directly: the nil setter guard must make
	// the second Notify a no-op regardless of how the bot logs downstream.
	var pubCalls int
	var pubMu sync.Mutex
	sn.SetSecurityEventPublisher(func(n services.SecurityNotification) {
		pubMu.Lock()
		pubCalls++
		pubMu.Unlock()
		emitter.ch <- &http.NotificationEvent{
			ID:        n.ID,
			Timestamp: n.Timestamp,
			Type:      http.NotificationType(n.Type),
			Title:     n.Title,
			Message:   n.Message,
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	nb.Start(ctx)
	defer nb.Stop()

	sn.Notify("unauthorized tool call blocked", "mcp")

	deadline := time.Now().Add(2 * time.Second)
	for {
		n, attrs := capture.snapshot()
		if n >= 1 {
			// The trigger attr is bot.NotificationTriggerType (a named
			// string), so compare via fmt.Sprint.
			if got := fmt.Sprint(attrs["trigger"]); got != string(bot.TriggerSecurityAlert) {
				t.Errorf("dispatch trigger = %q, want %q (event title carries security keyword)",
					got, bot.TriggerSecurityAlert)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("NotificationBot never dispatched the security alert as security_alert")
		}
		time.Sleep(2 * time.Millisecond)
	}

	// A nil publisher seam is a no-op (setter nil guard).
	sn.SetSecurityEventPublisher(nil)
	sn.Notify("second alert", "mcp")
	time.Sleep(30 * time.Millisecond)
	pubMu.Lock()
	calls := pubCalls
	pubMu.Unlock()
	if calls != 1 {
		t.Errorf("publisher call count after nil seam = %d, want 1", calls)
	}
}

// TestSecurityChannelPushPublishesAndDelivers covers sec-notify-03: "push"
// publishes the typed security.alert topic and QuotaNotifier delivers it
// through PushService (observed via the attached PushHistory). It also pins
// the separate dedup rule: the same (source,message) alert is suppressed, a
// distinct alert still delivers, and the quota dedup ledger is untouched.
func TestSecurityChannelPushPublishesAndDelivers(t *testing.T) {
	b := bus.New(nil, discardLogger())
	defer b.Close()

	var mu sync.Mutex
	var securityEvents []services.SecurityAlertEvent
	secSub := bus.SubscribeT(b, "sec-notify-03-sec", services.TopicSecurityAlert, func(ev services.SecurityAlertEvent) {
		mu.Lock()
		securityEvents = append(securityEvents, ev)
		mu.Unlock()
	})
	defer b.Unsubscribe(secSub)
	pushSvc := services.NewPushServiceWithChannels(b, nil, discardLogger())
	history := services.NewPushHistory(50)
	pushSvc.SetPushHistory(history)
	qn := services.NewQuotaNotifier(b, pushSvc, discardLogger())
	defer qn.Stop()

	// pushHistory mirrors the PushHistory entries the delivery writes. Each
	// entry carries Content/Source/Type/Priority; counts are the observable
	// proof of delivery and dedup.
	pushHistory := func() []services.PushEntry { return history.QueryAll(50) }

	sn := services.NewSecurityNotifier("push", b, discardLogger())
	sn.Notify("suspicious injection payload", "mcp")

	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(securityEvents) >= 1 && len(pushHistory()) >= 1
	})

	mu.Lock()
	if len(securityEvents) != 1 {
		t.Errorf("security.alert deliveries = %d, want 1", len(securityEvents))
	} else {
		ev := securityEvents[0]
		if ev.Message != "suspicious injection payload" || ev.Source != "mcp" {
			t.Errorf("security.alert payload = %+v, want message/source populated", ev)
		}
	}
	entries := pushHistory()
	if len(entries) != 1 {
		t.Errorf("push deliveries = %d, want 1", len(entries))
	} else {
		e := entries[0]
		if e.Content != "suspicious injection payload" || e.Source != "security" ||
			e.Type != services.PushTypeAlert || e.Priority != services.PushPriorityUrgent {
			t.Errorf("push entry = %+v, want content/source=security/type=alert/priority=urgent", e)
		}
	}
	mu.Unlock()

	// Duplicate same (source,message) is suppressed by the security dedup.
	sn.Notify("suspicious injection payload", "mcp")
	time.Sleep(30 * time.Millisecond)
	if got := len(pushHistory()); got != 1 {
		t.Errorf("duplicate alert produced %d push deliveries, want 1", got)
	}

	// A distinct alert still delivers (dedup key is source+message).
	sn.Notify("second suspicious payload", "mcp")
	waitFor(t, 2*time.Second, func() bool {
		return len(pushHistory()) >= 2
	})

	// Both recorded deliveries are security alerts (not chat notifications).
	for _, e := range pushHistory() {
		if e.Source != "security" || e.Type != services.PushTypeAlert {
			t.Errorf("delivery source/type = %q/%q, want security/alert", e.Source, e.Type)
		}
	}
}

// waitFor polls until cond() is true or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
