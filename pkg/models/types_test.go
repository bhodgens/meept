package models

import (
	"encoding/json"
	"testing"
)

func TestBusMessageSourceClient(t *testing.T) {
	msg := &BusMessage{
		ID:           "test-1",
		Type:         MessageTypeRequest,
		Source:       "chat-handler",
		SourceClient: "tui",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BusMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SourceClient != "tui" {
		t.Errorf("SourceClient = %q, want %q", decoded.SourceClient, "tui")
	}
}

func TestBusMessageSourceClientOmitEmpty(t *testing.T) {
	msg := &BusMessage{
		ID:     "test-2",
		Type:   MessageTypeRequest,
		Source: "chat-handler",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// source_client should be absent when empty (omitempty)
	if string(data) != "" {
		var m map[string]any
		json.Unmarshal(data, &m)
		if _, exists := m["source_client"]; exists {
			t.Error("source_client should be omitted when empty")
		}
	}
}
