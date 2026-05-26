package builtin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caimlas/meept/internal/calendar"
)

// newTestCalendarClient creates a calendar client pointing at a test server.
func newTestCalendarClient(handler http.HandlerFunc) (*calendar.Client, *httptest.Server) {
	server := httptest.NewServer(handler)
	client := calendar.NewClientForTesting(server.URL, "test-token", "primary")
	return client, server
}

func TestCalendarListTool_Name(t *testing.T) {
	tool := NewCalendarListTool(nil)
	if tool.Name() != "calendar_list" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "calendar_list")
	}
}

func TestCalendarListTool_Description(t *testing.T) {
	tool := NewCalendarListTool(nil)
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestCalendarListTool_Parameters(t *testing.T) {
	tool := NewCalendarListTool(nil)
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters Type = %q, want %q", params.Type, "object")
	}
	if _, ok := params.Properties["start"]; !ok {
		t.Error("Parameters missing 'start' property")
	}
	if _, ok := params.Properties["end"]; !ok {
		t.Error("Parameters missing 'end' property")
	}
}

func TestCalendarListTool_Execute(t *testing.T) {
	eventsJSON := `{
		"items": [
			{"id": "1", "summary": "Standup", "start": {"dateTime": "2024-06-15T09:00:00Z"}, "end": {"dateTime": "2024-06-15T09:30:00Z"}}
		]
	}`

	client, server := newTestCalendarClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(eventsJSON))
	})
	defer server.Close()

	tool := NewCalendarListTool(client)
	result, err := tool.Execute(context.Background(), map[string]any{
		"start":       "2024-06-15T00:00:00Z",
		"end":         "2024-06-15T23:59:59Z",
		"max_results": float64(10),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Result should be a *tools.ToolResult
	resultJSON, _ := json.Marshal(result)
	if !contains(string(resultJSON), "Standup") {
		t.Errorf("result should contain 'Standup': %s", string(resultJSON))
	}
}

func TestCalendarListTool_Execute_InvalidStart(t *testing.T) {
	tool := NewCalendarListTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"start": "not-a-date",
		"end":   "2024-06-15T23:59:59Z",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	resultJSON, _ := json.Marshal(result)
	if !contains(string(resultJSON), "invalid start time") {
		t.Errorf("should report invalid start time: %s", string(resultJSON))
	}
}

func TestCalendarListTool_Execute_MissingEnd(t *testing.T) {
	tool := NewCalendarListTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"start": "2024-06-15T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	resultJSON, _ := json.Marshal(result)
	if !contains(string(resultJSON), "invalid end time") {
		t.Errorf("should report invalid end time: %s", string(resultJSON))
	}
}

func TestCalendarCreateTool_Execute(t *testing.T) {
	createdJSON := `{
		"id": "new-123",
		"summary": "Sprint Planning",
		"start": {"dateTime": "2024-06-15T10:00:00Z"},
		"end": {"dateTime": "2024-06-15T11:00:00Z"}
	}`

	client, server := newTestCalendarClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(createdJSON))
	})
	defer server.Close()

	tool := NewCalendarCreateTool(client)
	result, err := tool.Execute(context.Background(), map[string]any{
		"summary":     "Sprint Planning",
		"start":       "2024-06-15T10:00:00Z",
		"end":         "2024-06-15T11:00:00Z",
		"description": "Weekly sprint planning",
		"location":    "Room 4",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	resultJSON, _ := json.Marshal(result)
	if !contains(string(resultJSON), "Created event") {
		t.Errorf("result should mention created event: %s", string(resultJSON))
	}
	if !contains(string(resultJSON), "new-123") {
		t.Errorf("result should contain event ID: %s", string(resultJSON))
	}
}

func TestCalendarCreateTool_Execute_MissingSummary(t *testing.T) {
	tool := NewCalendarCreateTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"start": "2024-06-15T10:00:00Z",
		"end":   "2024-06-15T11:00:00Z",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	resultJSON, _ := json.Marshal(result)
	if !contains(string(resultJSON), "summary is required") {
		t.Errorf("should report missing summary: %s", string(resultJSON))
	}
}

func TestCalendarQuickAddTool_Execute(t *testing.T) {
	quickJSON := `{"id": "qa-789", "summary": "Meeting with John"}`

	client, server := newTestCalendarClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(quickJSON))
	})
	defer server.Close()

	tool := NewCalendarQuickAddTool(client)
	result, err := tool.Execute(context.Background(), map[string]any{
		"text": "Meeting with John tomorrow at 3pm",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	resultJSON, _ := json.Marshal(result)
	if !contains(string(resultJSON), "Created event") {
		t.Errorf("result should mention created event: %s", string(resultJSON))
	}
}

func TestCalendarQuickAddTool_Execute_EmptyText(t *testing.T) {
	tool := NewCalendarQuickAddTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"text": "",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	resultJSON, _ := json.Marshal(result)
	if !contains(string(resultJSON), "text is required") {
		t.Errorf("should report missing text: %s", string(resultJSON))
	}
}

func TestCalendarTodayTool_Execute(t *testing.T) {
	eventsJSON := `{
		"items": [
			{"id": "1", "summary": "Morning Meeting", "start": {"dateTime": "2024-06-15T09:00:00Z"}, "end": {"dateTime": "2024-06-15T09:30:00Z"}},
			{"id": "2", "summary": "Lunch", "start": {"dateTime": "2024-06-15T12:00:00Z"}, "end": {"dateTime": "2024-06-15T13:00:00Z"}, "location": "Cafe"}
		]
	}`

	client, server := newTestCalendarClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(eventsJSON))
	})
	defer server.Close()

	tool := NewCalendarTodayTool(client)
	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	resultJSON, _ := json.Marshal(result)
	if !contains(string(resultJSON), "Morning Meeting") {
		t.Errorf("result should contain events: %s", string(resultJSON))
	}
	if !contains(string(resultJSON), "2 event") {
		t.Errorf("result should show 2 events: %s", string(resultJSON))
	}
}

func TestCalendarTodayTool_Execute_NoEvents(t *testing.T) {
	client, server := newTestCalendarClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items": []}`))
	})
	defer server.Close()

	tool := NewCalendarTodayTool(client)
	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	resultJSON, _ := json.Marshal(result)
	if !contains(string(resultJSON), "No events scheduled") {
		t.Errorf("should report no events: %s", string(resultJSON))
	}
}

func TestCalendarTools_Interface(t *testing.T) {
	// Verify all tools implement the tools.Tool interface by checking non-nil creation
	var client *calendar.Client // nil is ok for interface check

	tools := []any{
		NewCalendarListTool(client),
		NewCalendarCreateTool(client),
		NewCalendarQuickAddTool(client),
		NewCalendarTodayTool(client),
	}

	for i, tool := range tools {
		if tool == nil {
			t.Errorf("tool %d is nil", i)
		}
	}
}

func TestFormatCalendarEvents_Empty(t *testing.T) {
	result := formatCalendarEvents([]calendar.Event{})
	if !contains(result, "0 event") {
		t.Errorf("should report 0 events: %s", result)
	}
}

func TestFormatCalendarEvents_WithLocation(t *testing.T) {
	events := []calendar.Event{
		{
			Summary:  "Lunch",
			Location: "Cafe",
			Start:    calendar.EventTime{DateTime: "2024-06-15T12:00:00Z"},
		},
	}
	result := formatCalendarEvents(events)
	if !contains(result, "Lunch") {
		t.Errorf("should contain event summary: %s", result)
	}
	if !contains(result, "Cafe") {
		t.Errorf("should contain event location: %s", result)
	}
}

func TestCalendarGetString(t *testing.T) {
	args := map[string]any{
		"key": "value",
		"num": float64(42),
	}

	if got := calendarGetString(args, "key"); got != "value" {
		t.Errorf("got %q, want %q", got, "value")
	}
	if got := calendarGetString(args, "missing"); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
	if got := calendarGetString(args, "num"); got != "" {
		t.Errorf("got %q, want empty string for non-string", got)
	}
}

// contains checks if s contains substr (simple substring check).
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
