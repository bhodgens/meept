package http

import (
	"os"
	"strings"
	"testing"
)

// Pin for the dsh DSH-2 consumer residual: the WS relay's subscription list
// must include "storage.*" or storage.record.changed (2-segment, contract S4
// typed Topic[storage.RecordChanged]) never reaches WS clients — the
// single-segment "*" wildcard matches single-segment topics only
// (bus.matchWildcard compares segment counts), the exact failure class the
// turn.* pin above documents for turn.terminal.
func TestWSRelayTopicsIncludeStorage(t *testing.T) {
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	if !strings.Contains(string(src), `"storage.*"`) {
		t.Fatal(`WithWebSocket topics slice is missing "storage.*": storage.record.changed relays never reach WS clients`)
	}
}

// Pin the classification half: storage.* topics are data-change events and
// must classify as the generic "event" bucket (transformBusEventToWS
// default) — NEVER chat_message (AGENTS.md invariant: only chat_message and
// chat.message.received produce it).
func TestTransformBusEventToWS_StorageRecordChangedIsGenericEvent(t *testing.T) {
	payload := `{"store":"session_messages","key":"session-1","op":"put","value":{}}`
	msg := busMsg("storage.record.changed", payload)
	frame := transformBusEventToWS(msg)
	if frame == nil {
		t.Fatal("transformBusEventToWS returned nil for storage.record.changed")
	}
	if got := frame["type"]; got != "event" {
		t.Fatalf("storage.record.changed type = %v, want event (generic bucket; a chat_message would render a blank bubble)", got)
	}
	if frame["source_topic"] != "storage.record.changed" {
		t.Fatalf("source_topic = %v, want storage.record.changed", frame["source_topic"])
	}
	if frame["store"] != "session_messages" {
		t.Fatalf("payload store field = %v, want session_messages (payload must ride through)", frame["store"])
	}
}
