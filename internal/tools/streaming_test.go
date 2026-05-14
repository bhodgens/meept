package tools

import (
	"encoding/json"
	"testing"
)

func TestProgressUpdateJSONSerialization(t *testing.T) {
	t.Run("full update", func(t *testing.T) {
		u := ProgressUpdate{
			Message:       "running go build...",
			Percent:       50,
			PartialResult: json.RawMessage(`{"output_preview":"ok"}`),
			ToolCallID:    "call_abc123",
		}
		data, err := json.Marshal(u)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var parsed ProgressUpdate
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if parsed.Message != "running go build..." {
			t.Errorf("expected message='running go build...', got %q", parsed.Message)
		}
		if parsed.Percent != 50 {
			t.Errorf("expected percent=50, got %d", parsed.Percent)
		}
		if parsed.ToolCallID != "call_abc123" {
			t.Errorf("expected tool_call_id='call_abc123', got %q", parsed.ToolCallID)
		}
	})

	t.Run("minimal update", func(t *testing.T) {
		u := ProgressUpdate{
			Message: "starting...",
			Percent: -1,
		}
		data, err := json.Marshal(u)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if parsed["message"] != "starting..." {
			t.Errorf("expected message, got %v", parsed["message"])
		}
		// percent should be -1 for indeterminate
		if parsed["percent"] != float64(-1) {
			t.Errorf("expected percent=-1, got %v", parsed["percent"])
		}
	})

	t.Run("indeterminate percent", func(t *testing.T) {
		u := ProgressUpdate{
			Message: "working...",
			Percent: -1,
		}
		data, err := json.Marshal(u)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var parsed ProgressUpdate
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if parsed.Percent != -1 {
			t.Errorf("expected percent=-1, got %d", parsed.Percent)
		}
	})
}
