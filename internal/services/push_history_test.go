package services

import (
	"testing"
	"time"
)

func TestPushHistory_Record(t *testing.T) {
	h := NewPushHistory(100)
	
	entry := PushEntry{
		ID:        "test-1",
		SessionID: "sess-1",
		Content:   "test message",
		Timestamp: time.Now(),
	}
	
	h.Record(entry)
	
	entries := h.Query("sess-1", 10)
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestPushHistory_QueryAll(t *testing.T) {
	h := NewPushHistory(100)
	
	// Add 10 entries
	for i := 0; i < 10; i++ {
		h.Record(PushEntry{
			ID:        string(rune(i)),
			SessionID: "sess-1",
			Timestamp: time.Now(),
		})
	}
	
	all := h.QueryAll(5)
	if len(all) != 5 {
		t.Errorf("expected 5 entries, got %d", len(all))
	}
}

func TestPushHistory_MaxSize(t *testing.T) {
	h := NewPushHistory(5)
	
	// Add 10 entries
	for i := 0; i < 10; i++ {
		h.Record(PushEntry{
			ID: string(rune(i)),
		})
	}
	
	all := h.QueryAll(100)
	if len(all) != 5 {
		t.Errorf("expected max 5 entries, got %d", len(all))
	}
}

func TestPushHistory_Clear(t *testing.T) {
	h := NewPushHistory(100)
	
	h.Record(PushEntry{ID: "test"})
	h.Clear()
	
	all := h.QueryAll(100)
	if len(all) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(all))
	}
}
