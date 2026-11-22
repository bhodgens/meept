package tui

// 01-safety TEST GATE T6 (00T-gate.md): TUI re-subscribe on lost
// subscription (C6).
//
// bus.poll answers "subscription not found" after a daemon restart (the
// subscription died with the old process / connection). The EventStream
// returns the error as EventStreamDataMsg{Err} and Update ignores it:
// subscribe() is NEVER re-run, so the TUI event feed stays permanently
// dead and turn terminals never render (the reconnect gap that remains
// after d298c74c's generation/inflight/seq fixes).
//
// C6: on a "subscription not found" poll error the stream re-runs
// subscribe() and events flow again on the next poll cycle.
//
// RED today: no re-subscribe happens; the second subscribe call never
// arrives at the mock daemon.

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestResubscribeOnLostSubscription drives the real EventStream against a
// mock daemon whose FIRST subscription is declared lost: poll answers
// "subscription not found" until the client re-subscribes, then serves
// events under the NEW subscription id.
func TestResubscribeOnLostSubscription(t *testing.T) {
	sockPath := filepath.Join(os.TempDir(), fmt.Sprintf("meept-resub-%d.sock", time.Now().UnixNano()))
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	defer os.Remove(sockPath)

	var subscribeCalls atomic.Int64
	var pollCalls atomic.Int64

	event := BusEvent{
		Topic:     "turn.terminal",
		Type:      "event",
		Seq:       1,
		Timestamp: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC),
		Payload:   map[string]any{"status": "completed", "turn_id": "turn-reconnect"},
	}
	respBody, err := json.Marshal(struct {
		Events  []BusEvent `json:"events"`
		NextSeq int64      `json:"next_seq"`
	}{Events: []BusEvent{event}, NextSeq: 2})
	if err != nil {
		t.Fatalf("marshal poll response: %v", err)
	}

	handler := func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "bus.subscribe":
			n := subscribeCalls.Add(1)
			return map[string]string{"subscription_id": fmt.Sprintf("sub-%d", n)}, nil
		case "bus.poll":
			pollCalls.Add(1)
			var req struct {
				SubscriptionID string `json:"subscription_id"`
			}
			if uerr := json.Unmarshal(params, &req); uerr != nil {
				return nil, uerr
			}
			// The first subscription died with the "old daemon": only a
			// RE-subscription (sub-2+) is live and serves events.
			if req.SubscriptionID != "sub-1" {
				return json.RawMessage(respBody), nil
			}
			return nil, fmt.Errorf("subscription not found: %s", req.SubscriptionID)
		default:
			return map[string]string{}, nil
		}
	}
	go serveMockConn(t, listener, handler)

	rpc := NewRPCClient(sockPath)
	if err := rpc.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer rpc.Close()

	es := NewEventStream(rpc, &EventStreamConfig{
		Topics:       []string{"turn.terminal"},
		BufferSize:   10,
		PollInterval: time.Hour, // manual Poll cycles only
	})
	es.Start()
	defer es.Stop()

	if got := subscribeCalls.Load(); got != 1 {
		t.Fatalf("initial subscribe calls = %d, want 1", got)
	}

	// Poll 1: the daemon reports the subscription lost.
	msg1 := es.Poll()()
	dm1, ok := msg1.(EventStreamDataMsg)
	if !ok {
		t.Fatalf("poll 1 returned %T, want EventStreamDataMsg", msg1)
	}
	if dm1.Err == nil || !strings.Contains(dm1.Err.Error(), "subscription not found") {
		t.Fatalf("poll 1 err = %v, want the subscription-not-found error", dm1.Err)
	}

	// The stream must react to the lost subscription: either the Poll
	// error path or Update(EventStreamDataMsg) re-runs subscribe().
	es.Update(dm1)

	// Poll 2 (the next cycle): events must flow again. A correct client
	// re-subscribed after the loss; the current code keeps polling the
	// dead sub-1 forever.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if subscribeCalls.Load() >= 2 {
			break
		}
		es.Update(dm1)
		time.Sleep(20 * time.Millisecond)
	}
	if subscribeCalls.Load() < 2 {
		t.Fatalf("CURRENT DEFECT: poll error 'subscription not found' was swallowed; subscribe() never re-ran (subscribe calls=%d, poll calls=%d) -- the TUI feed stays dead after a daemon restart",
			subscribeCalls.Load(), pollCalls.Load())
	}

	msg2 := es.Poll()()
	dm2, ok := msg2.(EventStreamDataMsg)
	if !ok {
		t.Fatalf("poll 2 returned %T, want EventStreamDataMsg", msg2)
	}
	if dm2.Err != nil {
		t.Fatalf("poll 2 after re-subscribe still errors: %v", dm2.Err)
	}
	if len(dm2.Events) != 1 || dm2.Events[0].Topic != "turn.terminal" {
		t.Fatalf("poll 2 events = %+v, want the turn.terminal event flowing again", dm2.Events)
	}
}
