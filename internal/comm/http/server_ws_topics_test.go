package http

import (
	"os"
	"strings"
	"testing"
)

// Pin for the scopes-2 audit HIGH (2026-09-18): the bus wildcard '*' matches
// single-segment topics only (bus.matchWildcard compares segment counts), so
// the WS relay's "*" subscription never sees turn.terminal. WithWebSocket's
// topics slice must carry "turn.*" explicitly or HTTP/WS clients (Flutter
// GUI) never receive turn lifecycle relays and hang in pending forever.
func TestWSRelayTopicsIncludeTurn(t *testing.T) {
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	if !strings.Contains(string(src), `"turn.*"`) {
		t.Fatal(`WithWebSocket topics slice is missing "turn.*": turn.terminal relays never reach WS clients`)
	}
}
