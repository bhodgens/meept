package tui

import (
	"encoding/json"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestDefaultEventStreamConfig_SubscribesTurnTerminal pins the TUI event
// stream's subscription to the turn.terminal topic (leaf 01-turn-terminal-
// event Task 5). Without the subscription the TUI never sees a turn's
// terminal outcome and the timeout pending-indicator stays dead.
func TestDefaultEventStreamConfig_SubscribesTurnTerminal(t *testing.T) {
	cfg := DefaultEventStreamConfig()
	if !slices.Contains(cfg.Topics, "turn.terminal") {
		t.Errorf("DefaultEventStreamConfig topics %v missing \"turn.terminal\"", cfg.Topics)
	}
}

// TestDefaultEventStreamConfig_SubscribesACPPermissions pins the TUI event
// stream's subscription to the ACP human-permission relay topics
// (acp-human-permission-relay leaf 04, Contract D). The daemon publishes
// acp.permission.requested/.resolved (3-segment topics); without a
// matching wildcard pattern in the subscribe list the TUI never sees a
// relay frame and no permission modal can open. matchWildcard requires
// EQUAL SEGMENT COUNTS (internal/bus/bus.go), so "acp.*" would not match —
// the pattern must be the 3-segment "acp.*.*". It stays pairwise
// non-overlapping with every other pattern (TestTopicPatternsAreNonOverlapping
// invariant).
func TestDefaultEventStreamConfig_SubscribesACPPermissions(t *testing.T) {
	cfg := DefaultEventStreamConfig()
	if !slices.Contains(cfg.Topics, "acp.*.*") {
		t.Errorf("DefaultEventStreamConfig topics %v missing \"acp.*.*\"", cfg.Topics)
	}
}

// TestEventStream_GenerationDropsStaleTick pins P1: a one-shot tea.Tick armed
// before Stop cannot be retracted by Stop, so after a fast Stop->Start the
// stale tick must be rejected by Update rather than re-arming a SECOND poll
// chain. Start advances the generation and stamps it into the tick; Update
// compares against the current generation and drops mismatches.
//
// Exercises the real Start / Stop / Update / schedulePoll path with a
// disconnected RPCClient (subscribe becomes a no-op; no socket needed).
func TestEventStream_GenerationDropsStaleTick(t *testing.T) {
	es := NewEventStream(NewRPCClient("/nonexistent/generation-test.sock"), &EventStreamConfig{
		Topics:       []string{"agent.*"},
		BufferSize:   4,
		PollInterval: time.Millisecond,
	})

	// First run: the armed tick carries generation 1.
	cmd1 := es.Start()
	msg1 := cmd1() // capture once: tea.Tick's Cmd is a blocking one-shot timer
	tick1, ok := msg1.(EventStreamTickMsg)
	if !ok {
		t.Fatalf("Start cmd returned %T, want EventStreamTickMsg", msg1)
	}
	if tick1.generation != 1 {
		t.Fatalf("first tick generation = %d, want 1", tick1.generation)
	}

	// Fast Stop -> Start: the generation advances and a NEW tick carries 2.
	es.Stop()
	cmd2 := es.Start()
	msg2 := cmd2()
	tick2, ok := msg2.(EventStreamTickMsg)
	if !ok {
		t.Fatalf("second Start cmd returned %T, want EventStreamTickMsg", msg2)
	}
	if tick2.generation != 2 {
		t.Fatalf("second tick generation = %d, want 2", tick2.generation)
	}
	defer es.Stop()

	// A tick from the superseded generation arrives while running==true;
	// Update must drop it (nil command), so it cannot re-arm a second chain.
	if got := es.Update(EventStreamTickMsg{generation: 1}); got != nil {
		t.Fatalf("stale-generation tick was accepted (cmd=%T) — poll loop would be doubled", got)
	}
	// A tick stamped with the current generation must still be accepted.
	if got := es.Update(EventStreamTickMsg{generation: 2}); got == nil {
		t.Fatal("current-generation tick was dropped — normal polling broke")
	}
}

// TestEventStream_InFlightPollDedup pins P2's concurrency amplifier: two Poll
// commands started against a slow bus.poll RPC must not both fetch the same
// page. The in-flight guard makes the second Poll return nil immediately;
// the first completes normally and the buffer holds exactly one copy.
//
// Uses the same-package mockServer fake RPCClient seam (rpc_test.go) with a
// blocking bus.poll handler to make the in-flight window deterministic.
func TestEventStream_InFlightPollDedup(t *testing.T) {
	// Build the fake RPC server with the shared short-path helper: t.TempDir()
	// plus a long test name exceeds macOS's 104-byte sun_path limit and
	// net.Listen("unix", ...) fails with EINVAL.
	sockPath := newMockSocketPath(t)
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()

	event := BusEvent{
		Topic:     "agent.action",
		Type:      "event",
		Seq:       42,
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Payload:   map[string]any{"v": "one"},
	}
	respBody, err := json.Marshal(struct {
		Events  []BusEvent `json:"events"`
		NextSeq int64      `json:"next_seq"`
	}{Events: []BusEvent{event}, NextSeq: 43})
	if err != nil {
		t.Fatalf("marshal poll response: %v", err)
	}

	var pollCalls atomic.Int64
	handler := func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "bus.subscribe":
			return map[string]string{"subscription_id": "sub-1"}, nil
		case "bus.poll":
			pollCalls.Add(1)
			enteredOnce.Do(func() { close(entered) })
			<-release // hold the first poll in flight until the test releases it
			return json.RawMessage(respBody), nil
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
		Topics:       []string{"agent.*"},
		BufferSize:   50,
		PollInterval: time.Hour, // no automatic ticks; only explicit Poll below
	})
	es.Start() // subscribe synchronously; sets subscriptionID
	defer es.Stop()

	cmd1 := es.Poll()
	cmd2 := es.Poll()
	res := make(chan any, 2)

	go func() { res <- cmd1() }()
	<-entered // first poll is now in-flight, blocked inside bus.poll
	go func() { res <- cmd2() }()

	// The second poll must return nil at once (in-flight guard), never
	// reaching bus.poll or duplicating the page.
	select {
	case m := <-res:
		if m != nil {
			t.Fatalf("second concurrent Poll returned %T, want nil (in-flight poll should be suppressed)", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second concurrent Poll did not return promptly")
	}

	unblock()
	select {
	case m := <-res:
		dm, ok := m.(EventStreamDataMsg)
		if !ok || len(dm.Events) != 1 {
			t.Fatalf("first Poll returned %#v, want one event", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first Poll did not complete after release")
	}

	if got := es.RecentEvents(10); len(got) != 1 {
		t.Fatalf("buffer has %d events after concurrent Poll, want 1 (duplicate leaked in)", len(got))
	}
	if n := pollCalls.Load(); n != 1 {
		t.Fatalf("bus.poll called %d times, want 1 — the in-flight Poll reached the RPC", n)
	}
}

// serveMockConn accepts one connection and serves length-framed JSON-RPC
// requests using the same wire helpers as rpc_test.go's mockServer. It is a
// local variant so the test can control the socket path length (macOS
// sun_path limit) and inject a blocking handler deterministically.
func serveMockConn(t *testing.T, ln net.Listener, handler func(method string, params json.RawMessage) (any, error)) {
	t.Helper()
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	reader := NewFrameReader(conn)
	writer := NewFrameWriter(conn)
	for {
		req, err := reader.ReadRequest()
		if err != nil {
			return
		}
		result, handlerErr := handler(req.Method, req.Params)
		var resp Response
		if handlerErr != nil {
			resp = Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &RPCError{Code: -32603, Message: handlerErr.Error()},
			}
		} else {
			resultData, err := json.Marshal(result)
			if err != nil {
				return
			}
			resp = Response{JSONRPC: "2.0", ID: req.ID, Result: resultData}
		}
		respData, err := json.Marshal(resp)
		if err != nil {
			return
		}
		if err := writer.WriteFrame(respData); err != nil {
			return
		}
	}
}

// TestEventStream_AddToBufferDedup pins the addToBuffer dedup rules directly:
// a positive Seq is authoritative identity (same seq = duplicate regardless
// of other fields; different seq = distinct even with identical fields), and
// a zero Seq falls back to the full-field signature. A duplicate must NOT
// advance bufferIdx.
func TestEventStream_AddToBufferDedup(t *testing.T) {
	es := &EventStream{buffer: make([]BusEvent, 5), bufferSize: 5}
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	withSeq := BusEvent{Topic: "agent.a", Type: "event", Seq: 7, Timestamp: ts, Payload: map[string]any{"v": 1}}
	es.addToBuffer(withSeq)
	es.addToBuffer(withSeq) // same seq -> duplicate
	if es.bufferIdx != 1 {
		t.Fatalf("duplicate seq advanced bufferIdx to %d, want 1", es.bufferIdx)
	}

	// A different seq is a distinct event even though every other field matches.
	differentSeq := withSeq
	differentSeq.Seq = 8
	es.addToBuffer(differentSeq)
	if es.bufferIdx != 2 {
		t.Fatalf("distinct seq not stored: bufferIdx = %d, want 2", es.bufferIdx)
	}

	// Same seq as the first but otherwise different fields is still a duplicate
	// (seq is authoritative when present).
	sameSeqDifferentFields := BusEvent{Topic: "totally.other", Seq: 7, Timestamp: ts, Payload: map[string]any{"v": 999}}
	es.addToBuffer(sameSeqDifferentFields)
	if es.bufferIdx != 2 {
		t.Fatalf("same-seq event stored as distinct: bufferIdx = %d, want 2", es.bufferIdx)
	}

	// Seq==0 fallback: identical full-field signatures dedupe; a changed
	// payload stays distinct.
	noSeq := BusEvent{Topic: "agent.b", Type: "event", Timestamp: ts, Payload: map[string]any{"k": "v"}}
	es.addToBuffer(noSeq)
	es.addToBuffer(noSeq) // duplicate by full-field signature
	if es.bufferIdx != 3 {
		t.Fatalf("duplicate signature advanced bufferIdx to %d, want 3", es.bufferIdx)
	}
	changed := noSeq
	changed.Payload = map[string]any{"k": "different"}
	es.addToBuffer(changed)
	if es.bufferIdx != 4 {
		t.Fatalf("distinct payload not stored: bufferIdx = %d, want 4", es.bufferIdx)
	}

	if got := es.RecentEvents(10); len(got) != 4 {
		t.Fatalf("buffer holds %d events, want 4 distinct", len(got))
	}
}
