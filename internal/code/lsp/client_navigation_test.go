package lsp

// F10 gate T2 client arm (04-tool-surface/00T-tests.md): Client gains
// GotoImplementation alongside GotoDefinition/FindReferences/Hover
// (client.go:282/308/331). Pins contract B2.
//
// RED until leaf 02-lsp-navigation lands (GotoImplementation undefined).
// These tests exercise the request-building seam without a live server:
// GotoImplementation must issue textDocument/implementation with the
// zero-based position it was handed and decode []Location / single
// Location responses like its siblings.

import (
	"context"
	"testing"
)

func TestClient_GotoImplementation_MethodExists(t *testing.T) {
	// Compile-time gate: the method must exist with the sibling signature
	// (ctx, uri, line, char) ([]Location, error). A nil client will panic at
	// runtime, so assert only the method value's existence via the type.
	var c *Client
	fn := c.GotoImplementation // undefined until the leaf lands: RED here
	if fn == nil {
		t.Fatal("GotoImplementation method value is nil")
	}
}

func TestClient_GotoImplementation_NoServerErrorsCleanly(t *testing.T) {
	// Without a connected server the call must return a wrapped error,
	// never panic - same posture as GotoDefinition on a dead client.
	c := &Client{}
	_, err := c.GotoImplementation(context.Background(), "file:///tmp/x.go", 0, 0)
	if err == nil {
		t.Fatal("expected an error from a client with no server connection")
	}
}
