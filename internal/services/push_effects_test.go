package services

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/pkg/models"
)

// effectsTestLogger discards output for services effects tests.
func effectsTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// effectsBusCounter counts publishes on the push topics so tests can
// prove the external effect fired (or did not fire).
type effectsBusCounter struct {
	mu    sync.Mutex
	count map[string]int
}

func newEffectsBusCounter() *effectsBusCounter {
	return &effectsBusCounter{count: make(map[string]int)}
}

// subscribeAndCount drains subscriptions for every topic the Push path
// publishes on ("push.notify" and "push.<session>").
func (c *effectsBusCounter) subscribeAndCount(b *bus.MessageBus, topics ...string) []*bus.Subscriber {
	var subs []*bus.Subscriber
	for _, topic := range topics {
		sub := b.Subscribe("effects-test-"+topic, topic)
		subs = append(subs, sub)
		go func(ch chan *models.BusMessage) {
			for range ch {
				c.mu.Lock()
				c.count[topic]++
				c.mu.Unlock()
			}
		}(sub.Channel)
	}
	return subs
}

func (c *effectsBusCounter) total(topic string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count[topic]
}

// pushTestEffectsKey mirrors the production key derivation so tests assert
// the documented contract: EffectKey("push.notify", sessions, source, type,
// content).
func pushTestEffectsKey(req *PushRequest) string {
	sessions := req.SessionIDs
	if sessions == nil {
		sessions = []string{}
	}
	sessionsJSON, _ := json.Marshal(sessions)
	return effects.EffectKey("push.notify", string(sessionsJSON), req.Source, string(req.Type), req.Content)
}

func TestPushService_EffectsIdempotent(t *testing.T) {
	// shared request: identical inputs across calls
	newReq := func() *PushRequest {
		return &PushRequest{
			SessionIDs: []string{"sess-a", "sess-b"},
			Source:     "test",
			Type:       PushTypeAlert,
			Content:    "idempotent hello",
			Priority:   PushPriorityNormal,
		}
	}

	t.Run("first send", func(t *testing.T) {
		b := testBus()
		defer b.Close()
		counter := newEffectsBusCounter()
		subs := counter.subscribeAndCount(b, "push.notify", "push.sess-a", "push.sess-b")
		defer func() {
			for _, s := range subs {
				b.Unsubscribe(s)
			}
		}()

		ledger := effects.NewMemoryLedger()
		s := NewPushService(&fakeStore{}, b, effectsTestLogger())
		s.SetEffectsLedger(ledger)

		res, err := s.Push(context.Background(), newReq())
		if err != nil {
			t.Fatalf("Push: %v", err)
		}
		if res.Delivered != 2 {
			t.Errorf("delivered = %d, want 2", res.Delivered)
		}

		rec, err := ledger.Get(context.Background(), pushTestEffectsKey(newReq()))
		if err != nil {
			t.Fatalf("ledger Get: %v", err)
		}
		if rec.State != effects.StateCompleted {
			t.Errorf("state = %q, want completed", rec.State)
		}
		if rec.Tool != "push.notify" {
			t.Errorf("tool = %q, want push.notify", rec.Tool)
		}
		if rec.ProviderIdempotent {
			t.Error("push must declare ProviderIdempotent=false")
		}

		var receipt struct {
			PushID    string `json:"push_id"`
			Delivered int    `json:"delivered"`
		}
		if err := json.Unmarshal(rec.Receipt, &receipt); err != nil {
			t.Fatalf("receipt unmarshal: %v", err)
		}
		if receipt.PushID == "" {
			t.Error("receipt push_id empty")
		}
		if receipt.Delivered != 2 {
			t.Errorf("receipt delivered = %d, want 2", receipt.Delivered)
		}
	})

	t.Run("duplicate send returns prior receipt without republishing", func(t *testing.T) {
		b := testBus()
		defer b.Close()
		counter := newEffectsBusCounter()
		subs := counter.subscribeAndCount(b, "push.notify", "push.sess-a", "push.sess-b")
		defer func() {
			for _, s := range subs {
				b.Unsubscribe(s)
			}
		}()

		ledger := effects.NewMemoryLedger()
		s := NewPushService(&fakeStore{}, b, effectsTestLogger())
		s.SetEffectsLedger(ledger)

		first, err := s.Push(context.Background(), newReq())
		if err != nil {
			t.Fatalf("first Push: %v", err)
		}

		// wait for the drain goroutines to observe the first send's
		// publishes on BOTH session topics. The per-session Publish is
		// non-blocking bus fan-out; a single-topic check can observe
		// sess-a before sess-b's subscriber drains and race the count
		// assertion below.
		deadline := 100
		for (counter.total("push.sess-a") < 1 || counter.total("push.sess-b") < 1) && deadline > 0 {
			deadline--
			time.Sleep(2 * time.Millisecond)
			if deadline == 0 {
				t.Fatal("first send never published both session topics")
			}
		}

		second, err := s.Push(context.Background(), newReq())
		if err != nil {
			t.Fatalf("duplicate Push: %v", err)
		}
		if second.Delivered != first.Delivered {
			t.Errorf("duplicate delivered = %d, want prior %d (idempotent no-op must be indistinguishable)", second.Delivered, first.Delivered)
		}

		if got := counter.total("push.sess-a") + counter.total("push.sess-b"); got != 2 {
			t.Errorf("session topics published %d times, want 2 (one per session, first send only)", got)
		}

		// the ledger still holds exactly one completed record with the
		// ORIGINAL receipt (not overwritten by the duplicate call)
		rec, err := ledger.Get(context.Background(), pushTestEffectsKey(newReq()))
		if err != nil {
			t.Fatalf("ledger Get: %v", err)
		}
		var receipt struct {
			Delivered int `json:"delivered"`
		}
		if err := json.Unmarshal(rec.Receipt, &receipt); err != nil {
			t.Fatalf("receipt unmarshal: %v", err)
		}
		if receipt.Delivered != 2 {
			t.Errorf("receipt delivered = %d, want 2 (original)", receipt.Delivered)
		}
	})

	t.Run("different content produces a different effect key", func(t *testing.T) {
		b := testBus()
		defer b.Close()

		ledger := effects.NewMemoryLedger()
		s := NewPushService(&fakeStore{}, b, effectsTestLogger())
		s.SetEffectsLedger(ledger)

		reqA := newReq()
		reqB := newReq()
		reqB.Content = "different body"
		if _, err := s.Push(context.Background(), reqA); err != nil {
			t.Fatalf("Push A: %v", err)
		}
		if _, err := s.Push(context.Background(), reqB); err != nil {
			t.Fatalf("Push B: %v", err)
		}
		for _, req := range []*PushRequest{reqA, reqB} {
			rec, err := ledger.Get(context.Background(), pushTestEffectsKey(req))
			if err != nil {
				t.Fatalf("ledger Get: %v", err)
			}
			if rec.State != effects.StateCompleted {
				t.Errorf("state = %q for %q, want completed", rec.State, req.Content)
			}
		}
	})

	t.Run("nil ledger keeps legacy behavior", func(t *testing.T) {
		b := testBus()
		defer b.Close()

		s := NewPushService(&fakeStore{}, b, effectsTestLogger())
		if s.effects != nil {
			t.Fatal("effects field must start nil")
		}

		res, err := s.Push(context.Background(), newReq())
		if err != nil {
			t.Fatalf("Push: %v", err)
		}
		if res.Delivered != 2 {
			t.Errorf("delivered = %d, want 2", res.Delivered)
		}
	})

	t.Run("set effects ledger nil guard", func(t *testing.T) {
		s := NewPushService(&fakeStore{}, testBus(), effectsTestLogger())
		s.SetEffectsLedger(effects.NewMemoryLedger())
		s.SetEffectsLedger(nil) // must be a no-op, not a clobber
		if s.effects == nil {
			t.Error("SetEffectsLedger(nil) must not clear a wired ledger")
		}
	})
}
