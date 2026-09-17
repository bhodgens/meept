package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/transport"
)

// fakeAsyncClient scripts RPC method responses for submitAndAwait and
// records the global call order so tests can assert subscribe-before-submit.
// All fields are mutex-guarded: several tests swap scripted responses from
// background goroutines while submitAndAwait's poll loop is calling.
type fakeAsyncClient struct {
	transport.Client // nil-embedded; only Call is used (panics loudly if reached)

	mu        sync.Mutex
	responses map[string]func(params any) (any, error)
	order     []string
}

func (f *fakeAsyncClient) Call(method string, params any) (json.RawMessage, error) {
	f.mu.Lock()
	f.order = append(f.order, method)
	fn, ok := f.responses[method]
	f.mu.Unlock()
	if !ok {
		return nil, errors.New("unexpected method: " + method)
	}
	result, err := fn(params)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// setResponse swaps the scripted handler for a method (thread-safe).
func (f *fakeAsyncClient) setResponse(method string, fn func(params any) (any, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[method] = fn
}

func (f *fakeAsyncClient) ackResult() map[string]any {
	return map[string]any{
		"turn_id":         "turn-123",
		"conversation_id": "conv-456",
		"session_id":      "sess-1",
		"accepted":        true,
		"note":            "accepted; result arrives via turn.terminal",
	}
}

func (f *fakeAsyncClient) subscribeParams() map[string]any {
	return map[string]any{"topics": chatSubmitTopics}
}

func (f *fakeAsyncClient) pollParams(since string) map[string]any {
	return map[string]any{
		"subscription_id": "sub-1",
		"since":           since,
	}
}

// terminalEvent builds a turn.terminal poll response carrying one event.
func terminalEvent(turnID, status, reply, errText string, durationMS int64) map[string]any {
	payload := map[string]any{
		"conversation_id": "conv-456",
		"session_id":      "sess-1",
		"turn_id":         turnID,
		"handler_case":    "test",
		"status":          status,
		"reply":           reply,
		"duration_ms":     durationMS,
	}
	if errText != "" {
		payload["error"] = errText
	}
	return map[string]any{
		"events": []map[string]any{
			{"topic": "turn.terminal", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "payload": payload},
		},
	}
}

// newFakeAsyncClient wires a fake whose poll returns scripted events.
func newFakeAsyncClient(pollResult map[string]any, pollErr error) *fakeAsyncClient {
	f := &fakeAsyncClient{responses: map[string]func(params any) (any, error){}}
	f.responses["bus.subscribe"] = func(params any) (any, error) {
		return map[string]any{"subscription_id": "sub-1", "topics": chatSubmitTopics}, nil
	}
	f.responses["bus.poll"] = func(params any) (any, error) {
		if pollErr != nil {
			return nil, pollErr
		}
		if pollResult == nil {
			return map[string]any{"events": []any{}}, nil
		}
		return pollResult, nil
	}
	f.responses["bus.unsubscribe"] = func(params any) (any, error) {
		return map[string]any{"unsubscribed": true}, nil
	}
	f.responses["chat.submit"] = func(params any) (any, error) {
		return f.ackResult(), nil
	}
	return f
}

func testChatOpts(stdout *bytes.Buffer) chatOpts {
	return chatOpts{
		Stdout: stdout,
		Stderr: stdout,
		isTTY:  func() bool { return false },
	}
}

func TestSubmitAndAwait_HappyPath(t *testing.T) {
	var out bytes.Buffer
	f := newFakeAsyncClient(terminalEvent("turn-123", "completed", "all done!", "", 1500), nil)

	res, err := submitAndAwait(context.Background(), f, "do a thing", "sess-1", testChatOpts(&out))
	if err != nil {
		t.Fatalf("submitAndAwait: %v", err)
	}
	if res.Reply != "all done!" {
		t.Errorf("Reply = %q, want %q", res.Reply, "all done!")
	}
	if res.Status != "completed" {
		t.Errorf("Status = %q, want completed", res.Status)
	}
	if res.AckSeconds <= 0 {
		t.Errorf("AckSeconds = %v, want > 0", res.AckSeconds)
	}
	if res.TurnSeconds <= 0 {
		t.Errorf("TurnSeconds = %v, want > 0 (duration_ms 1500 → 1.5)", res.TurnSeconds)
	}

	// Verify the chat.submit params contract in a dedicated test.
}

func TestSubmitAndAwait_SubscribeBeforeSubmit(t *testing.T) {
	var out bytes.Buffer
	f := newFakeAsyncClient(terminalEvent("turn-123", "completed", "done", "", 10), nil)

	if _, err := submitAndAwait(context.Background(), f, "msg", "sess-1", testChatOpts(&out)); err != nil {
		t.Fatalf("submitAndAwait: %v", err)
	}

	var submitIdx, subscribeIdx = -1, -1
	for i, m := range f.order {
		switch m {
		case "bus.subscribe":
			if subscribeIdx == -1 {
				subscribeIdx = i
			}
		case "chat.submit":
			if submitIdx == -1 {
				submitIdx = i
			}
		}
	}
	if subscribeIdx == -1 || submitIdx == -1 {
		t.Fatalf("expected both bus.subscribe and chat.submit calls, order=%v", f.order)
	}
	if subscribeIdx > submitIdx {
		t.Errorf("bus.subscribe (idx %d) must precede chat.submit (idx %d); order=%v",
			subscribeIdx, submitIdx, f.order)
	}
}

func TestSubmitAndAwait_SubscribeTopics(t *testing.T) {
	var out bytes.Buffer
	f := newFakeAsyncClient(terminalEvent("turn-123", "completed", "done", "", 10), nil)
	f.responses["bus.subscribe"] = func(params any) (any, error) {
		p, ok := params.(map[string]any)
		if !ok {
			return nil, errors.New("bad subscribe params type")
		}
		topics, _ := p["topics"].([]string)
		if len(topics) == 0 {
			// tolerate []any encoding
			if anyTopics, ok := p["topics"].([]any); ok {
				for _, tp := range anyTopics {
					if s, ok := tp.(string); ok {
						topics = append(topics, s)
					}
				}
			}
		}
		found := false
		for _, tp := range topics {
			if tp == "turn.terminal" {
				found = true
			}
		}
		if !found {
			return nil, errors.New("bus.subscribe params missing turn.terminal topic")
		}
		return map[string]any{"subscription_id": "sub-1"}, nil
	}

	if _, err := submitAndAwait(context.Background(), f, "msg", "sess-1", testChatOpts(&out)); err != nil {
		t.Fatalf("submitAndAwait: %v", err)
	}
}

func TestSubmitAndAwait_ChatSubmitParams(t *testing.T) {
	var out bytes.Buffer
	f := newFakeAsyncClient(terminalEvent("turn-123", "completed", "done", "", 10), nil)
	f.responses["chat.submit"] = func(params any) (any, error) {
		p, ok := params.(map[string]any)
		if !ok {
			return nil, errors.New("bad chat.submit params type")
		}
		if p["message"] != "hello daemon" {
			return nil, errors.New("chat.submit message not plumbed")
		}
		if p["session_id"] != "sess-42" {
			return nil, errors.New("chat.submit session_id not plumbed")
		}
		if sc, _ := p["source_client"].(string); sc == "" {
			return nil, errors.New("chat.submit missing source_client")
		}
		if _, has := p["conversation_id"]; has {
			return nil, errors.New("chat.submit must NOT send conversation_id (daemon mints it)")
		}
		return f.ackResult(), nil
	}

	if _, err := submitAndAwait(context.Background(), f, "hello daemon", "sess-42", testChatOpts(&out)); err != nil {
		t.Fatalf("submitAndAwait: %v", err)
	}
}

func TestSubmitAndAwait_LivenessStall(t *testing.T) {
	var out bytes.Buffer
	// Polls always return zero events → no progress ever.
	f := newFakeAsyncClient(nil, nil)

	opts := testChatOpts(&out)
	opts.Liveness = 150 * time.Millisecond
	opts.PollInterval = 50 * time.Millisecond

	_, err := submitAndAwait(context.Background(), f, "long task", "sess-1", opts)
	if err == nil {
		t.Fatal("expected stall error, got nil")
	}
	if !strings.Contains(err.Error(), "stalled") {
		t.Errorf("stall error %q missing \"stalled\"", err)
	}
	if !strings.Contains(err.Error(), "meept tasks") {
		t.Errorf("stall error %q missing actionable hint \"meept tasks\"", err)
	}
	if !strings.Contains(err.Error(), "120") == false && opts.Liveness != DefaultLivenessTimeout {
		t.Logf("non-default liveness used: %v", opts.Liveness)
	}
}

func TestSubmitAndAwait_LivenessDefault120s(t *testing.T) {
	if DefaultLivenessTimeout != 120*time.Second {
		t.Errorf("DefaultLivenessTimeout = %v, want 120s", DefaultLivenessTimeout)
	}
}

func TestSubmitAndAwait_FailedTurn(t *testing.T) {
	var out bytes.Buffer
	f := newFakeAsyncClient(terminalEvent("turn-123", "failed", "", "quota exhausted: retry after 10:00", 400), nil)

	res, err := submitAndAwait(context.Background(), f, "msg", "sess-1", testChatOpts(&out))
	if err != nil {
		t.Fatalf("submitAndAwait returned err for a delivered failed turn: %v", err)
	}
	if res.Status != "failed" {
		t.Errorf("Status = %q, want failed", res.Status)
	}
	if !strings.Contains(res.Error, "quota exhausted") {
		t.Errorf("Error = %q, want the daemon's error text", res.Error)
	}

	// The CLI exit path (submitChatTurn) must convert a failed turn into a
	// non-nil error carrying the daemon error text (main prints it on stderr
	// and exits 1).
	f2 := newFakeAsyncClient(terminalEvent("turn-123", "failed", "", "boom", 10), nil)
	if err := submitChatTurn(f2, "msg", "sess-1", testChatOpts(&out)); err == nil {
		t.Fatal("submitChatTurn on failed turn must return an error (non-zero exit)")
	} else if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %q, want daemon error text", err)
	}
}

func TestSubmitAndAwait_RejectedAck(t *testing.T) {
	var out bytes.Buffer
	f := newFakeAsyncClient(nil, nil)
	f.responses["chat.submit"] = func(params any) (any, error) {
		return map[string]any{
			"turn_id":         "",
			"conversation_id": "conv-1",
			"session_id":      "sess-1",
			"accepted":        false,
			"note":            "message is required",
		}, nil
	}

	_, err := submitAndAwait(context.Background(), f, "msg", "sess-1", testChatOpts(&out))
	if err == nil {
		t.Fatal("expected rejection error, got nil")
	}
	if !strings.Contains(err.Error(), "message is required") {
		t.Errorf("rejection error = %q, want the ack note", err)
	}
	// Subscribe-before-submit ordering means a rejected turn has already
	// subscribed; that's harmless and gets cleaned up by bus.unsubscribe.
	// The real assertion: a rejected turn must NOT enter the await loop
	// (no bus.poll after the rejection).
	subscribed := false
	for _, m := range f.order {
		if m == "bus.subscribe" {
			subscribed = true
		}
		if m == "bus.poll" {
			t.Errorf("rejected turn must not poll; order=%v", f.order)
		}
	}
	if !subscribed {
		t.Errorf("expected bus.subscribe before chat.submit; order=%v", f.order)
	}
}

func TestSubmitAndAwait_AwaitOff(t *testing.T) {
	var out bytes.Buffer
	f := newFakeAsyncClient(nil, nil)
	// Any poll would be a bug: --await=off never waits.
	f.responses["bus.poll"] = func(params any) (any, error) {
		return nil, errors.New("bus.poll must not be called with --await=off")
	}
	f.responses["bus.subscribe"] = func(params any) (any, error) {
		return nil, errors.New("bus.subscribe must not be called with --await=off")
	}

	opts := testChatOpts(&out)
	opts.Await = "off"
	res, err := submitAndAwait(context.Background(), f, "msg", "sess-9", opts)
	if err != nil {
		t.Fatalf("submitAndAwait(await=off): %v", err)
	}

	want := "turn turn-123 accepted; run `meept chat --session sess-9` to follow up"
	if got := strings.TrimSpace(out.String()); got != want {
		t.Errorf("ack line = %q, want %q", got, want)
	}
	if res.Reply != "" {
		t.Errorf("await=off must not produce a Reply, got %q", res.Reply)
	}
	for _, m := range f.order {
		if m == "bus.subscribe" || m == "bus.poll" {
			t.Errorf("--await=off must not touch the bus; saw %s", m)
		}
	}
}

func TestSubmitAndAwait_NoSpinnerOnPipedStdout(t *testing.T) {
	var out bytes.Buffer
	// isTTY=false (testChatOpts) simulates piped stdout.
	f := newFakeAsyncClient(terminalEvent("turn-123", "completed", "done", "", 10), nil)

	if _, err := submitAndAwait(context.Background(), f, "msg", "sess-1", testChatOpts(&out)); err != nil {
		t.Fatalf("submitAndAwait: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("piped stdout must stay clean during await; got %q", out.String())
	}
}

func TestSubmitAndAwait_ProgressLineOnTTY(t *testing.T) {
	var out bytes.Buffer
	f := newFakeAsyncClient(terminalEvent("turn-123", "completed", "done", "", 10), nil)

	opts := testChatOpts(&out)
	tty := true
	opts.isTTY = func() bool { return tty }

	if _, err := submitAndAwait(context.Background(), f, "msg", "sess-1", opts); err != nil {
		t.Fatalf("submitAndAwait: %v", err)
	}
	// A TTY wait may render progress frames and must erase the line before
	// finishing (no stray partial frames left on the row).
	if s := out.String(); !strings.Contains(s, "\r") {
		t.Errorf("TTY progress expected a \\r-updating line; got %q", s)
	}
}

func TestSubmitAndAwait_QuietSuppressesProgress(t *testing.T) {
	var out bytes.Buffer
	f := newFakeAsyncClient(terminalEvent("turn-123", "completed", "done", "", 10), nil)

	opts := testChatOpts(&out)
	opts.Quiet = true
	opts.isTTY = func() bool { return true } // even on a TTY

	if _, err := submitAndAwait(context.Background(), f, "msg", "sess-1", opts); err != nil {
		t.Fatalf("submitAndAwait: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("--quiet must suppress all progress output; got %q", out.String())
	}
}

func TestSubmitAndAwait_TerminalEventAfterLongDelay(t *testing.T) {
	// The old sync path capped at 110s and printed a stub. The new path has
	// NO wall-clock ceiling — a turn whose terminal event arrives after any
	// delay still delivers the real reply. Scripted here with a delay well
	// past the OLD 110s semantic (compressed: we assert the mechanism, not
	// by sleeping minutes — a 300ms-delayed terminal event proves the wait
	// is event-driven, not deadline-driven).
	var out bytes.Buffer
	f := newFakeAsyncClient(nil, nil)

	terminal := terminalEvent("turn-123", "completed", "the REAL long-task reply", "", 115000)
	delayed := make(chan struct{})
	go func() {
		time.Sleep(300 * time.Millisecond)
		f.setResponse("bus.poll", func(params any) (any, error) { return terminal, nil })
		close(delayed)
	}()

	res, err := submitAndAwait(context.Background(), f, "long task", "sess-1", testChatOpts(&out))
	if err != nil {
		t.Fatalf("submitAndAwait: %v", err)
	}
	<-delayed
	if res.Reply != "the REAL long-task reply" {
		t.Errorf("Reply = %q, want the real terminal reply (no 110s stub ceiling)", res.Reply)
	}
	if res.TurnSeconds != 115.0 {
		t.Errorf("TurnSeconds = %v, want 115 (duration_ms 115000 — past the old 110s ceiling)", res.TurnSeconds)
	}
}

func TestSubmitAndAwait_PollFailureFailsFast(t *testing.T) {
	var out bytes.Buffer
	f := newFakeAsyncClient(nil, errors.New("subscription not found: sub-1"))

	opts := testChatOpts(&out)
	opts.PollInterval = 20 * time.Millisecond

	_, err := submitAndAwait(context.Background(), f, "msg", "sess-1", opts)
	if err == nil {
		t.Fatal("persistent poll failure must error, not hang until the stall")
	}
	if !strings.Contains(err.Error(), "bus poll failed") {
		t.Errorf("error = %q, want poll-failure error", err)
	}
}

func TestSubmitAndAwait_IgnoresForeignTurnEvents(t *testing.T) {
	var out bytes.Buffer
	f := newFakeAsyncClient(nil, nil)
	// Poll returns someone else's terminal event first, then ours.
	f.responses["bus.poll"] = func(params any) (any, error) {
		return terminalEvent("turn-other", "completed", "not yours", "", 5), nil
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		f.setResponse("bus.poll", func(params any) (any, error) {
			return terminalEvent("turn-123", "completed", "yours", "", 5), nil
		})
	}()

	res, err := submitAndAwait(context.Background(), f, "msg", "sess-1", testChatOpts(&out))
	if err != nil {
		t.Fatalf("submitAndAwait: %v", err)
	}
	if res.Reply != "yours" {
		t.Errorf("Reply = %q, want the turn-matched event's reply", res.Reply)
	}
}

func TestSubmitAndAwait_UnsubscribeAfterTerminal(t *testing.T) {
	var out bytes.Buffer
	f := newFakeAsyncClient(terminalEvent("turn-123", "completed", "done", "", 10), nil)

	if _, err := submitAndAwait(context.Background(), f, "msg", "sess-1", testChatOpts(&out)); err != nil {
		t.Fatalf("submitAndAwait: %v", err)
	}
	found := false
	for _, m := range f.order {
		if m == "bus.unsubscribe" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected bus.unsubscribe after the turn; order=%v", f.order)
	}
}
