package services

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

func TestQuotaNotifier_ProcessEvent(t *testing.T) {
	b := bus.New(nil, slog.Default())
	defer b.Close()

	pushSvc := NewPushService(nil, b, slog.Default())
	qn := NewQuotaNotifier(b, pushSvc, slog.Default())

	unblockAt := time.Now().Add(3 * time.Hour)
	payload, _ := json.Marshal(map[string]any{
		"agent_id":       "agent-1",
		"provider_id":    "openrouter",
		"model_id":       "claude-3-opus",
		"credential_key": "key-1",
		"escalation":     "",
		"unblock_at":     unblockAt.Format(time.RFC3339),
		"task_count":     2,
	})
	msg := &models.BusMessage{Payload: payload}

	qn.processEvent(msg)

	// Should have recorded the event
	qn.mu.Lock()
	k := key("agent-1", "key-1", "")
	if !qn.seen[k] {
		t.Error("expected event to be marked as seen")
	}
	qn.mu.Unlock()
}

func TestQuotaNotifier_Dedup(t *testing.T) {
	b := bus.New(nil, slog.Default())
	defer b.Close()

	pushSvc := NewPushService(nil, b, slog.Default())
	qn := NewQuotaNotifier(b, pushSvc, slog.Default())

	unblockAt := time.Now().Add(3 * time.Hour)
	payload, _ := json.Marshal(map[string]any{
		"agent_id":       "agent-1",
		"provider_id":    "openrouter",
		"model_id":       "claude-3-opus",
		"credential_key": "key-1",
		"escalation":     "warn",
		"unblock_at":     unblockAt.Format(time.RFC3339),
		"task_count":     1,
	})
	msg := &models.BusMessage{Payload: payload}

	// Process twice - should only notify once
	qn.processEvent(msg)
	qn.processEvent(msg)

	qn.mu.Lock()
	k := key("agent-1", "key-1", "warn")
	if !qn.seen[k] {
		t.Error("expected event to be marked as seen")
	}
	// Should be true (only set once)
	qn.mu.Unlock()
}

func TestQuotaNotifier_FormatMessage(t *testing.T) {
	unblockAt := time.Now().Add(3 * time.Hour)

	tests := []struct {
		escalation string
		wantSubstr string
	}{
		{"", "quota limit reached"},
		{"warn", "still quota-blocked"},
		{"action_recommended", "action recommended"},
		{"blocked", "manual action required"},
		{"quota_cleared", "quota recovered"},
	}

	for _, tt := range tests {
		got := formatMessage("openrouter", "claude-3", unblockAt, tt.escalation, 1)
		if !contains(got, tt.wantSubstr) {
			t.Errorf("formatMessage(%q) = %q, want substring %q", tt.escalation, got, tt.wantSubstr)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestQuotaNotifier_MalformedPayload(t *testing.T) {
	b := bus.New(nil, slog.Default())
	defer b.Close()

	pushSvc := NewPushService(nil, b, slog.Default())
	qn := NewQuotaNotifier(b, pushSvc, slog.Default())

	// Malformed payload - should not panic
	qn.processEvent(nil)
	qn.processEvent(&models.BusMessage{Payload: json.RawMessage("not json")})
	qn.processEvent(&models.BusMessage{})
}

func TestQuotaNotifier_Cleanup(t *testing.T) {
	b := bus.New(nil, slog.Default())
	defer b.Close()

	pushSvc := NewPushService(nil, b, slog.Default())
	qn := NewQuotaNotifier(b, pushSvc, slog.Default())

	// Add more than 100 entries
	for i := 0; i < 150; i++ {
		qn.mu.Lock()
		qn.seen[fmt.Sprintf("key-%d", i)] = true
		qn.mu.Unlock()
	}

	qn.Cleanup()

	qn.mu.Lock()
	defer qn.mu.Unlock()
	if len(qn.seen) != 0 {
		t.Errorf("expected 0 entries after cleanup, got %d", len(qn.seen))
	}
}
