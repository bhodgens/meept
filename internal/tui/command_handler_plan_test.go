package tui

// 05-navigation TEST-GATE T2 (leaf 00T-gate.md, matrix F12a; master C1/C6).
//
// Pins the /plan fix and the dropped-event notice:
//   - /plan must forward the command text to chat.submit (the dispatcher's
//     working /plan route, internal/agent/dispatcher.go) and must NEVER
//     call session.set_mode, which is UNREGISTERED daemon-side (the TUI
//     command today dies with method-not-found; W20/N5).
//   - bus.poll's in-band dropped counter (C6: dropped>0 stays in-band)
//     must reach the user as a system notice, once per drop episode,
//     instead of being discarded by the poll decoder (events.go).
//
// RED today: executePlan calls session.set_mode and never chat.submit;
// EventStreamDataMsg has no dropped field so the counter is discarded.
// Reflection keeps the dropped-field assertion compiling at HEAD.
//
// The gate uses its own call-recording RPC stub (the shared rpcStub in
// headless_test.go records nothing); the App harness itself is reused.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// gateStubCall is one recorded RPC request.
type gateStubCall struct {
	method string
	params map[string]any
}

// gateCallStub is a unix-socket JSON-RPC stub that RECORDS every call
// (method + params) and answers from a canned result map. Unknown
// methods answer {"ok":true}, same as the shared headless stub.
type gateCallStub struct {
	listener   net.Listener
	socketPath string
	mu         sync.Mutex
	results    map[string]any
	calls      []gateStubCall
}

func newGateCallStub(t *testing.T) *gateCallStub {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("gate stub: listen: %v", err)
	}
	s := &gateCallStub{listener: l, socketPath: path, results: map[string]any{}}
	go s.acceptLoop()
	t.Cleanup(func() {
		l.Close()
		os.RemoveAll(dir)
	})
	return s
}

func (s *gateCallStub) setResult(method string, result any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[method] = result
}

func (s *gateCallStub) recorded() []gateStubCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]gateStubCall, len(s.calls))
	copy(out, s.calls)
	return out
}

func (s *gateCallStub) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.serve(conn)
	}
}

func (s *gateCallStub) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		var length int
		if _, err := fmt.Fscanf(r, "%d\n", &length); err != nil {
			return
		}
		buf := make([]byte, length)
		if _, err := readFull(r, buf); err != nil {
			return
		}
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(buf, &req); err != nil {
			return
		}
		var params map[string]any
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return
			}
		}
		s.mu.Lock()
		s.calls = append(s.calls, gateStubCall{method: req.Method, params: params})
		result := s.results[req.Method]
		s.mu.Unlock()
		if result == nil {
			result = map[string]any{"ok": true}
		}
		respData, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		if err != nil {
			return
		}
		fmt.Fprintf(conn, "%d\n", len(respData))
		if _, err := conn.Write(respData); err != nil {
			return
		}
	}
}

// gateStubCalled reports whether the stub saw the RPC method at all.
func gateStubCalled(s *gateCallStub, method string) bool {
	for _, c := range s.recorded() {
		if c.method == method {
			return true
		}
	}
	return false
}

// gateSubmitMessage returns the message param of the first chat.submit.
func gateSubmitMessage(s *gateCallStub) (string, bool) {
	for _, c := range s.recorded() {
		if c.method == "chat.submit" {
			msg, _ := c.params["message"].(string)
			return msg, true
		}
	}
	return "", false
}

// gatePlanStubResults seeds the canned answers a /plan forward needs.
func gatePlanStubResults(s *gateCallStub, turnID string) {
	s.setResult("session.list", map[string]any{"sessions": []any{}})
	s.setResult("task.list_extended", map[string]any{"tasks": []any{}})
	s.setResult("queue.stats", map[string]any{"by_state": map[string]int{}})
	s.setResult("queue.jobs", map[string]any{"jobs": []any{}})
	s.setResult("plan.list_by_session", map[string]any{"plans": []any{}})
	s.setResult("chat.submit", map[string]any{
		"turn_id":         turnID,
		"conversation_id": "conv-plan",
		"session_id":      "sess-plan",
		"accepted":        true,
		"note":            "accepted; result arrives via turn.terminal",
	})
}

// TestPlanSlashForwards pins the /plan route through the REAL command
// handler + REAL RPC client: the command forwards its text to
// chat.submit (dispatcher handles /plan), never session.set_mode.
func TestPlanSlashForwards(t *testing.T) {
	stub := newGateCallStub(t)
	gatePlanStubResults(stub, "turn-plan-1")
	client := NewRPCClient(stub.socketPath)
	if err := client.Connect(); err != nil {
		t.Fatalf("connect gate stub: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	h := NewCommandHandler(client)
	res := h.executeSync(&SlashCommand{Name: "plan", Args: []string{"design", "the", "quartz", "migration"}})
	if res == nil {
		t.Fatal("executeSync(/plan) returned nil")
	}
	if res.IsError {
		t.Errorf("/plan returned an error result (session.set_mode is unregistered daemon-side): %q", res.Output)
	}

	if gateStubCalled(stub, "session.set_mode") {
		t.Error("/plan still calls session.set_mode - unregistered daemon-side; must forward to chat.submit (dispatcher route)")
	}
	msg, ok := gateSubmitMessage(stub)
	if !ok {
		t.Fatalf("/plan never forwarded to chat.submit (working dispatcher route unused; W20); calls=%v", stub.recorded())
	}
	if !strings.HasPrefix(strings.TrimSpace(msg), "/plan") {
		t.Errorf("chat.submit message %q does not carry the /plan command text (dispatcher /plan route keys on the prefix)", msg)
	}
	if !strings.Contains(msg, "quartz") {
		t.Errorf("chat.submit message %q lost the /plan arguments", msg)
	}
}

// TestPlanSlashBareToggleForwards pins the bare /plan (no args) arm: it
// too forwards through chat.submit so the daemon-side plan-mode toggle
// (PlanModeController.HandlePlanCommand) owns the semantics.
func TestPlanSlashBareToggleForwards(t *testing.T) {
	stub := newGateCallStub(t)
	gatePlanStubResults(stub, "turn-plan-2")
	client := NewRPCClient(stub.socketPath)
	if err := client.Connect(); err != nil {
		t.Fatalf("connect gate stub: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	h := NewCommandHandler(client)
	res := h.executeSync(&SlashCommand{Name: "plan"})
	if res != nil && res.IsError {
		t.Errorf("bare /plan returned an error result: %q", res.Output)
	}
	if gateStubCalled(stub, "session.set_mode") {
		t.Error("bare /plan still calls the unregistered session.set_mode")
	}
	msg, ok := gateSubmitMessage(stub)
	if !ok {
		t.Fatalf("bare /plan never forwarded to chat.submit; calls=%v", stub.recorded())
	}
	if strings.TrimSpace(msg) != "/plan" {
		t.Errorf("bare /plan forwarded message %q, want exactly '/plan'", msg)
	}
}

// gateDroppedField reads a dropped-counter field off EventStreamDataMsg
// through reflection: no such field exists at HEAD (the poll decoder
// discards the in-band counter), which is the RED reason.
func gateDroppedField(msg EventStreamDataMsg) (int64, bool) {
	f := reflect.ValueOf(msg).FieldByName("Dropped")
	if !f.IsValid() {
		return 0, false
	}
	switch f.Kind() {
	case reflect.Int, reflect.Int64:
		return f.Int(), true
	default:
		return 0, false
	}
}

// TestDroppedNotice pins C6 surfacing end-to-end through the real poll
// decoder and the real App Update path: a poll response with dropped>0
// produces exactly one system notice per drop episode in the chat
// transcript (never silent loss, never per-poll spam).
func TestDroppedNotice(t *testing.T) {
	hp := newHeadlessApp(t, 100, 30)
	hp.stub.setResult("bus.subscribe", map[string]any{"subscription_id": "sub-gate"})
	hp.stub.setResult("bus.poll", map[string]any{
		"events":   []any{},
		"next_seq": int64(1),
		"dropped":  int64(3),
	})

	es := NewEventStream(hp.app.rpc, &EventStreamConfig{
		Topics:       []string{"turn.terminal"},
		BufferSize:   8,
		PollInterval: time.Hour, // no autonomous ticks; the test drives Poll
	})
	if cmd := es.Start(); cmd != nil {
		// The returned cmd only ARMS an hour-long tick; never execute it.
		_ = cmd
	}
	t.Cleanup(es.Stop)

	pollMsg := es.Poll()()
	data, ok := pollMsg.(EventStreamDataMsg)
	if !ok {
		t.Fatalf("Poll returned %T, want EventStreamDataMsg", pollMsg)
	}

	// 1. The in-band counter must survive the decoder (C6).
	dropped, hasField := gateDroppedField(data)
	if !hasField {
		t.Errorf("EventStreamDataMsg carries no dropped-counter field - bus.poll's in-band dropped counter is discarded (events.go decoder; C6)")
	} else if dropped != 3 {
		t.Errorf("dropped counter = %d, want 3 (the daemon reported dropped=3)", dropped)
	}

	// 2. Feed the REAL decoded message through the App event loop; the
	// transcript must gain one system notice mentioning the loss.
	hp.send(data)
	hp.settle(100, 30)
	app := hp.finish(100, 30)

	notices := 0
	for _, m := range app.chat.GetMessages() {
		if m.Role == "system" && strings.Contains(strings.ToLower(m.Content), "dropped") {
			notices++
		}
	}
	if notices == 0 {
		t.Errorf("dropped>0 produced no system notice in the chat transcript (silent event loss; C6/W26)")
	}
	if notices > 1 {
		t.Errorf("dropped notice fired %d times for one episode (must be once per episode)", notices)
	}
}
