package main

// Integration-style tests for the async chat wiring (async-turn-migration
// leaf 03, Tasks 2 & 3): runChatTurn (oneshot path) and chatWithSession
// (--session path) must go through chat.submit + turn.terminal, never the
// legacy blocking "chat" RPC. The fakeAsyncClient from chat_async_test.go
// is reused; the --await flag plumbing is tested via newChatCmd.

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/transport"
)

// chatWiringFake extends fakeAsyncClient with a call log carrying params so
// wiring tests can assert which RPC methods the chat paths used.
type chatWiringFake struct {
	fakeAsyncClient
	methods []string
	params  []any
}

func (f *chatWiringFake) Call(method string, params any) (json.RawMessage, error) {
	f.methods = append(f.methods, method)
	f.params = append(f.params, params)
	return f.fakeAsyncClient.Call(method, params)
}

func newChatWiringFake(terminal map[string]any) *chatWiringFake {
	f := &chatWiringFake{}
	f.responses = map[string]func(params any) (any, error){}
	f.responses["bus.subscribe"] = func(params any) (any, error) {
		return map[string]any{"subscription_id": "sub-1"}, nil
	}
	f.responses["bus.poll"] = func(params any) (any, error) {
		if terminal == nil {
			return map[string]any{"events": []any{}}, nil
		}
		return terminal, nil
	}
	f.responses["bus.unsubscribe"] = func(params any) (any, error) {
		return map[string]any{"unsubscribed": true}, nil
	}
	f.responses["chat.submit"] = func(params any) (any, error) {
		return f.ackResult(), nil
	}
	// session.get: existing session (chatWithSession validates first).
	f.responses["session.get"] = func(params any) (any, error) {
		return map[string]any{"id": "sess-1", "name": "oneshot_responses"}, nil
	}
	return f
}

func TestRunChatTurn_OneshotPrintsReply(t *testing.T) {
	var out bytes.Buffer
	f := newChatWiringFake(terminalEvent("turn-123", "completed", "the genuine reply for a >110s task", "", 115000))

	// Simulate the default flags.
	oldAwait := chatAwait
	chatAwait = "wait"
	defer func() { chatAwait = oldAwait }()

	reply, err := runChatTurn(f, "do a long task", "sess-1")
	if err != nil {
		t.Fatalf("runChatTurn: %v", err)
	}
	if reply != "the genuine reply for a >110s task" {
		t.Errorf("reply = %q, want the real terminal reply (no 110s stub)", reply)
	}

	// The path must use chat.submit and must NOT use the legacy chat RPC.
	usedSubmit, usedLegacyChat := false, false
	for _, m := range f.methods {
		switch m {
		case "chat.submit":
			usedSubmit = true
		case "chat":
			usedLegacyChat = true
		}
	}
	if !usedSubmit {
		t.Errorf("oneshot path must call chat.submit; methods=%v", f.methods)
	}
	if usedLegacyChat {
		t.Errorf("oneshot path must NEVER call the legacy blocking chat RPC; methods=%v", f.methods)
	}
	_ = out
}

func TestRunChatTurn_AwaitOffReturnsAckNotReply(t *testing.T) {
	var out bytes.Buffer
	f := newChatWiringFake(terminalEvent("turn-123", "completed", "should not wait", "", 10))

	oldAwait := chatAwait
	chatAwait = "off"
	defer func() { chatAwait = oldAwait }()

	opts, err := chatOptsFromFlags()
	if err != nil {
		t.Fatalf("chatOptsFromFlags: %v", err)
	}
	opts.Stdout = &out
	opts.Stderr = &out
	opts.isTTY = func() bool { return false }

	if err := submitChatTurn(f, "msg", "sess-1", opts); err != nil {
		t.Fatalf("submitChatTurn(await=off): %v", err)
	}
	want := "turn turn-123 accepted; run `meept chat --session sess-1` to follow up"
	if got := strings.TrimSpace(out.String()); got != want {
		t.Errorf("ack line = %q, want %q", got, want)
	}
}

func TestRunChatTurn_FailedTurnExitConvention(t *testing.T) {
	f := newChatWiringFake(terminalEvent("turn-123", "failed", "", "model quota exhausted", 50))

	oldAwait := chatAwait
	chatAwait = "wait"
	defer func() { chatAwait = oldAwait }()

	_, err := runChatTurn(f, "msg", "sess-1")
	if err == nil {
		t.Fatal("failed turn must surface as an error (RunE → main prints to stderr, os.Exit(1))")
	}
	if !strings.Contains(err.Error(), "model quota exhausted") {
		t.Errorf("error = %q, want the daemon's error text", err)
	}
}

func TestRunChatTurn_InvalidAwaitFlag(t *testing.T) {
	oldAwait := chatAwait
	chatAwait = "sometimes"
	defer func() { chatAwait = oldAwait }()

	f := newChatWiringFake(nil)
	if _, err := runChatTurn(f, "msg", "sess-1"); err == nil {
		t.Fatal("invalid --await value must error")
	}
}

func TestChatWithSession_UsesAsyncTurn(t *testing.T) {
	f := newChatWiringFake(terminalEvent("turn-123", "completed", "session reply", "", 20))

	oldAwait := chatAwait
	chatAwait = "wait"
	defer func() { chatAwait = oldAwait }()

	opts, _ := chatOptsFromFlags()
	progressSink := &bytes.Buffer{} // progress goes here; reply goes to real stdout
	opts.Stdout = progressSink
	opts.Stderr = progressSink
	opts.isTTY = func() bool { return false }

	// chatWithSession prints the reply via fmt.Println on real stdout —
	// capture it with the package's existing helper (doctor_test.go).
	out := captureStdout(t, func() {
		if err := chatWithSession(f, "sess-1", "hello session"); err != nil {
			t.Errorf("chatWithSession: %v", err)
		}
	})
	if got := strings.TrimSpace(out); got != "session reply" {
		t.Errorf("stdout = %q, want the session turn's reply", got)
	}

	// Session semantics: the session_id must reach chat.submit params, and
	// the subscribe-before-submit ordering must hold here too.
	var submitParams map[string]any
	for i, m := range f.methods {
		if m == "chat.submit" {
			p, ok := f.params[i].(map[string]any)
			if !ok {
				t.Fatalf("chat.submit params type = %T", f.params[i])
			}
			submitParams = p
		}
	}
	if submitParams == nil {
		t.Fatal("chatWithSession never called chat.submit")
	}
	if submitParams["session_id"] != "sess-1" {
		t.Errorf("chat.submit session_id = %v, want sess-1", submitParams["session_id"])
	}
	if _, has := submitParams["conversation_id"]; has {
		t.Errorf("chat.submit must omit conversation_id (daemon generates it)")
	}

	submitIdx, subscribeIdx := -1, -1
	for i, m := range f.methods {
		switch m {
		case "chat.submit":
			if submitIdx == -1 {
				submitIdx = i
			}
		case "bus.subscribe":
			if subscribeIdx == -1 {
				subscribeIdx = i
			}
		}
	}
	if subscribeIdx > submitIdx {
		t.Errorf("session path: bus.subscribe (%d) must precede chat.submit (%d)", subscribeIdx, submitIdx)
	}
}

func TestChatWithSession_SessionValidationStillRuns(t *testing.T) {
	f := newChatWiringFake(nil)
	f.responses["session.get"] = func(params any) (any, error) {
		return nil, errors.New("rpc error: not found")
	}

	if err := chatWithSession(f, "sess-missing", "msg"); err == nil {
		t.Fatal("chatWithSession on a missing session must error before any submit")
	}
	for _, m := range f.methods {
		if m == "chat.submit" {
			t.Error("missing session must never reach chat.submit")
		}
	}
}

// Compile-time: the fake satisfies the transport client contract used here.
var _ transport.Client = (*fakeAsyncClient)(nil)
