package rpc

import (
	"context"
	"encoding/json"
	"testing"
)

// TestHasMethod pins bughunt A1-11: the duplicate-registration probe needs
// a lookup-only check. HasMethod must report registry state WITHOUT
// dispatching the handler (the old CallMethod-based probe executed the
// real handler with nil params on every boot).
func TestHasMethod(t *testing.T) {
	s := New(&Config{SocketPath: ""}, nil, nil)

	if s.HasMethod("activity.query") {
		t.Fatal("HasMethod on empty registry = true, want false")
	}

	called := 0
	s.RegisterHandler("activity.query", func(ctx context.Context, params json.RawMessage) (any, error) {
		called++
		return nil, nil
	})

	if !s.HasMethod("activity.query") {
		t.Fatal("HasMethod after RegisterHandler = false, want true")
	}
	if s.HasMethod("activity.overview") {
		t.Fatal("HasMethod for unregistered method = true, want false")
	}
	// The lookup itself must never execute the handler.
	if called != 0 {
		t.Fatalf("HasMethod dispatched the handler (%d calls), want 0", called)
	}
}
