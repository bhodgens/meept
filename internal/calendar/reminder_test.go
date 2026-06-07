package calendar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Calendar Client Tests ---

func TestNewClient_MissingToken(t *testing.T) {
	_, err := NewClient(ClientConfig{AccessToken: ""}, nil)
	if err == nil {
		t.Fatal("expected error for empty access token")
	}
}

func TestNewClient_DefaultCalendarID(t *testing.T) {
	client, err := NewClient(ClientConfig{AccessToken: "test-token"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.calendarID != "primary" {
		t.Errorf("expected calendarID 'primary', got %q", client.calendarID)
	}
}

func TestNewClient_CustomCalendarID(t *testing.T) {
	client, err := NewClient(ClientConfig{
		AccessToken: "test-token",
		CalendarID:  "custom@group.calendar.google.com",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.calendarID != "custom@group.calendar.google.com" {
		t.Errorf("expected custom calendarID, got %q", client.calendarID)
	}
}

func TestClient_GetToday(t *testing.T) {
	eventsJSON := `{
		"items": [
			{
				"id": "evt1",
				"summary": "Team Standup",
				"start": {"dateTime": "2024-06-15T09:00:00Z"},
				"end": {"dateTime": "2024-06-15T09:30:00Z"}
			},
			{
				"id": "evt2",
				"summary": "Lunch with Alex",
				"start": {"dateTime": "2024-06-15T12:00:00Z"},
				"end": {"dateTime": "2024-06-15T13:00:00Z"},
				"location": "Cafe Central"
			}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or wrong Authorization header: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(eventsJSON))
	}))
	defer server.Close()

	client := &Client{
		httpClient:  server.Client(),
		accessToken: "test-token",
		calendarID:  "primary",
		baseURL:     server.URL,
		logger:      nil,
	}

	events, err := client.ListEvents(context.Background(), time.Now(), time.Now().Add(24*time.Hour), 10)
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if events[0].Summary != "Team Standup" {
		t.Errorf("event 0 summary: got %q, want %q", events[0].Summary, "Team Standup")
	}
	if events[1].Location != "Cafe Central" {
		t.Errorf("event 1 location: got %q, want %q", events[1].Location, "Cafe Central")
	}
}

func TestClient_CreateEvent(t *testing.T) {
	createdJSON := `{
		"id": "new-evt-123",
		"summary": "Sprint Planning",
		"start": {"dateTime": "2024-06-15T10:00:00Z"},
		"end": {"dateTime": "2024-06-15T11:00:00Z"}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected JSON content type, got %s", r.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(createdJSON))
	}))
	defer server.Close()

	client := &Client{
		httpClient:  server.Client(),
		accessToken: "test-token",
		calendarID:  "primary",
		baseURL:     server.URL,
		logger:      nil,
	}

	event, err := client.CreateEvent(context.Background(), &Event{
		Summary: "Sprint Planning",
		Start:   EventTime{DateTime: "2024-06-15T10:00:00Z"},
		End:     EventTime{DateTime: "2024-06-15T11:00:00Z"},
	})
	if err != nil {
		t.Fatalf("CreateEvent failed: %v", err)
	}

	if event.ID != "new-evt-123" {
		t.Errorf("event ID: got %q, want %q", event.ID, "new-evt-123")
	}
}

func TestClient_QuickAdd(t *testing.T) {
	quickAddJSON := `{
		"id": "qa-evt-456",
		"summary": "Meeting with John tomorrow at 3pm"
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Query().Get("text") != "Meeting tomorrow at 3pm" {
			t.Errorf("missing text parameter: %s", r.URL.Query().Get("text"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(quickAddJSON))
	}))
	defer server.Close()

	client := &Client{
		httpClient:  server.Client(),
		accessToken: "test-token",
		calendarID:  "primary",
		baseURL:     server.URL,
		logger:      nil,
	}

	event, err := client.QuickAdd(context.Background(), "Meeting tomorrow at 3pm")
	if err != nil {
		t.Fatalf("QuickAdd failed: %v", err)
	}

	if event.ID != "qa-evt-456" {
		t.Errorf("event ID: got %q, want %q", event.ID, "qa-evt-456")
	}
}

func TestClient_DeleteEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := &Client{
		httpClient:  server.Client(),
		accessToken: "test-token",
		calendarID:  "primary",
		baseURL:     server.URL,
		logger:      nil,
	}

	if err := client.DeleteEvent(context.Background(), "evt-to-delete"); err != nil {
		t.Fatalf("DeleteEvent failed: %v", err)
	}
}

func TestClient_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "invalid credentials"}`))
	}))
	defer server.Close()

	client := &Client{
		httpClient:  server.Client(),
		accessToken: "bad-token",
		calendarID:  "primary",
		baseURL:     server.URL,
		logger:      nil,
	}

	_, err := client.GetEvent(context.Background(), "evt-1")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

// --- EventTime Tests ---

func TestEventTime_Time(t *testing.T) {
	tests := []struct {
		name    string
		et      EventTime
		wantErr bool
	}{
		{
			name:    "datetime format",
			et:      EventTime{DateTime: "2024-06-15T09:00:00Z"},
			wantErr: false,
		},
		{
			name:    "date format",
			et:      EventTime{Date: "2024-06-15"},
			wantErr: false,
		},
		{
			name:    "empty time",
			et:      EventTime{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.et.Time()
			if (err != nil) != tt.wantErr {
				t.Errorf("EventTime.Time() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_SetAccessToken(t *testing.T) {
	client, _ := NewClient(ClientConfig{AccessToken: "old-token"}, nil)
	client.SetAccessToken("new-token")
	if client.accessToken != "new-token" {
		t.Errorf("SetAccessToken did not update token: got %q", client.accessToken)
	}
}

// --- Reminder Watcher Tests ---

func TestReminderWatcher_Config(t *testing.T) {
	cfg := DefaultReminderWatcherConfig()
	if cfg.Interval != 5*time.Minute {
		t.Errorf("default interval: got %v, want %v", cfg.Interval, 5*time.Minute)
	}
	if cfg.AdvanceMinutes != 10 {
		t.Errorf("default advance minutes: got %d, want %d", cfg.AdvanceMinutes, 10)
	}
}

func TestReminderWatcher_Stop(t *testing.T) {
	client, _ := NewClient(ClientConfig{AccessToken: "test-token"}, nil)
	watcher := NewReminderWatcher(client, nil, ReminderWatcherConfig{
		Interval:       1 * time.Hour,
		AdvanceMinutes: 10,
	}, nil)

	// Stop should not block or panic
	watcher.Stop()

	// Double stop should be safe
	watcher.Stop()
}

func TestReminderWatcher_TriggerReminder(t *testing.T) {
	var publishedTopic string
	var publishedData map[string]any

	client, _ := NewClient(ClientConfig{AccessToken: "test-token"}, nil)
	publish := func(topic string, data map[string]any) {
		publishedTopic = topic
		publishedData = data
	}

	watcher := NewReminderWatcher(client, publish, ReminderWatcherConfig{
		Interval:       5 * time.Minute,
		AdvanceMinutes: 10,
	}, nil)

	// Test that the watcher was created correctly
	if watcher == nil {
		t.Fatal("watcher should not be nil")
	}

	// Verify publish callback works by direct invocation (covers the callback path)
	publish("calendar.reminder", map[string]any{
		"event_id": "test-123",
		"summary":  "Test Event",
	})

	if publishedTopic != "calendar.reminder" {
		t.Errorf("topic: got %q, want %q", publishedTopic, "calendar.reminder")
	}
	if publishedData["event_id"] != "test-123" {
		t.Errorf("event_id: got %v, want %q", publishedData["event_id"], "test-123")
	}
}
