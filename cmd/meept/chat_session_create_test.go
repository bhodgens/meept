package main

import (
	"strings"
	"testing"
)

// 09-cli-affordances TEST-GATE (task 4): `meept chat --session <name>`
// must NOT auto-create a missing session (typo safety); the error text
// must point at the new --session-create flag, and --session-create must
// create the named session before send. RED before chat.go grows the
// flag.
//
// The session lookup goes through session.get over the transport client,
// so a scripted fake proves both behaviors at the exact boundary.

func chatSessionFlagsClient(t *testing.T, sessionExists bool) *fakeAsyncClient {
	t.Helper()
	f := &fakeAsyncClient{responses: map[string]func(params any) (any, error){}}
	f.setResponse("session.get", func(_ any) (any, error) {
		if sessionExists {
			return map[string]any{"id": "sess-target", "name": "sess-target"}, nil
		}
		return map[string]any{"error": "session not found"}, nil
	})
	f.setResponse("session.create", func(params any) (any, error) {
		m, _ := params.(map[string]any)
		name, _ := m["name"].(string)
		return map[string]any{"id": name, "name": name}, nil
	})
	f.setResponse("bus.subscribe", func(_ any) (any, error) {
		return map[string]any{"subscription_id": "sub-test-1"}, nil
	})
	f.setResponse("chat.submit", func(_ any) (any, error) {
		return f.ackResult(), nil
	})
	f.setResponse("bus.poll", func(_ any) (any, error) {
		return terminalEvent("turn-123", "completed", "ok", "", 10), nil
	})
	return f
}

// TestChatMissingSessionErrorMentionsFlag pins the typo-safety error: a
// missing --session target fails with a message that names
// --session-create, and no session.create ever fires without the flag.
func TestChatMissingSessionErrorMentionsFlag(t *testing.T) {
	f := chatSessionFlagsClient(t, false)
	chatSessionID = "typo-sess"
	chatSessionCreate = false
	t.Cleanup(func() { chatSessionID = ""; chatSessionCreate = false })

	err := chatWithSession(f, "typo-sess", "hello")
	if err == nil {
		t.Fatal("09-gate: missing --session target must fail, got nil error")
	}
	if !strings.Contains(err.Error(), "--session-create") {
		t.Fatalf("09-gate: missing-session error must suggest --session-create, got: %v", err)
	}
	for _, m := range f.order {
		if m == "session.create" {
			t.Fatal("09-gate: session.create fired without --session-create (typo auto-create)")
		}
	}
}

// TestChatSessionCreateCreatesBeforeSend pins the flag path: with
// --session-create set, a missing session is created via session.create
// carrying the requested name, and the turn rides the new session id.
func TestChatSessionCreateCreatesBeforeSend(t *testing.T) {
	f := chatSessionFlagsClient(t, false)
	chatSessionID = "fresh-sess"
	chatSessionCreate = true
	t.Cleanup(func() { chatSessionID = ""; chatSessionCreate = false })

	if err := chatWithSession(f, "fresh-sess", "hello"); err != nil {
		t.Fatalf("09-gate: chatWithSession with --session-create failed: %v", err)
	}

	var creates int
	for _, m := range f.order {
		if m == "session.create" {
			creates++
		}
	}
	if creates != 1 {
		t.Fatalf("09-gate: session.create calls = %d, want exactly 1 (order: %v)", creates, f.order)
	}
	// The fake records only method names, so run a second scripted pass
	// that captures the create params to assert the requested name rides
	// the create call.
	f2 := chatSessionFlagsClient(t, false)
	var captured map[string]any
	f2.setResponse("session.create", func(params any) (any, error) {
		m, _ := params.(map[string]any)
		captured = m
		name, _ := m["name"].(string)
		return map[string]any{"id": name, "name": name}, nil
	})
	if err := chatWithSession(f2, "fresh-sess", "hello"); err != nil {
		t.Fatalf("09-gate: capture pass failed: %v", err)
	}
	if captured == nil {
		t.Fatal("09-gate: session.create never fired on the flag path")
	}
	if name, _ := captured["name"].(string); name != "fresh-sess" {
		t.Errorf("09-gate: session.create name = %v, want fresh-sess", captured["name"])
	}
}
