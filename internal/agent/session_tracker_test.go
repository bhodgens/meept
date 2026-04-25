package agent

import (
	"sync"
	"testing"
	"time"
)

func TestRecordIntent(t *testing.T) {
	tracker := NewSessionTracker(30 * time.Minute)

	intent := &Intent{Type: "code", Confidence: 0.8, AgentType: "coder"}
	tracker.RecordIntent("session1", intent, "coder")

	intent2 := &Intent{Type: "debug", Confidence: 0.7, AgentType: "debugger"}
	tracker.RecordIntent("session1", intent2, "debugger")

	state := tracker.GetSession("session1")
	if state == nil {
		t.Fatal("GetSession() returned nil")
	}
	if state.TotalRequests != 2 {
		t.Errorf("TotalRequests = %d, want 2", state.TotalRequests)
	}
	if len(state.IntentHistory) != 2 {
		t.Errorf("len(IntentHistory) = %d, want 2", len(state.IntentHistory))
	}
}

func TestRecordIntentHistoryCap(t *testing.T) {
	tracker := NewSessionTracker(30 * time.Minute)

	for i := 0; i < 25; i++ {
		intent := &Intent{Type: "chat", Confidence: 0.5, AgentType: "chat"}
		tracker.RecordIntent("session1", intent, "chat")
	}

	state := tracker.GetSession("session1")
	if state == nil {
		t.Fatal("GetSession() returned nil")
	}
	if len(state.IntentHistory) > 20 {
		t.Errorf("len(IntentHistory) = %d, want <= 20", len(state.IntentHistory))
	}
	if state.TotalRequests != 25 {
		t.Errorf("TotalRequests = %d, want 25", state.TotalRequests)
	}
}

func TestGetDominantIntent(t *testing.T) {
	tracker := NewSessionTracker(30 * time.Minute)

	tracker.RecordIntent("s1", &Intent{Type: "code", AgentType: "coder"}, "coder")
	tracker.RecordIntent("s1", &Intent{Type: "code", AgentType: "coder"}, "coder")
	tracker.RecordIntent("s1", &Intent{Type: "chat", AgentType: "chat"}, "chat")

	got := tracker.GetDominantIntent("s1")
	if got != "code" {
		t.Errorf("GetDominantIntent() = %q, want %q", got, "code")
	}
}

func TestGetDominantIntentEmpty(t *testing.T) {
	tracker := NewSessionTracker(30 * time.Minute)
	got := tracker.GetDominantIntent("nonexistent")
	if got != "" {
		t.Errorf("GetDominantIntent() = %q, want empty", got)
	}
}

func TestGetLastIntent(t *testing.T) {
	tracker := NewSessionTracker(30 * time.Minute)

	tracker.RecordIntent("s1", &Intent{Type: "code", AgentType: "coder"}, "coder")
	tracker.RecordIntent("s1", &Intent{Type: "debug", AgentType: "debugger"}, "debugger")

	last := tracker.GetLastIntent("s1")
	if last == nil {
		t.Fatal("GetLastIntent() returned nil")
	}
	if last.Type != "debug" {
		t.Errorf("GetLastIntent().Type = %q, want %q", last.Type, "debug")
	}
}

func TestGetLastIntentEmpty(t *testing.T) {
	tracker := NewSessionTracker(30 * time.Minute)
	got := tracker.GetLastIntent("nonexistent")
	if got != nil {
		t.Errorf("GetLastIntent() = %v, want nil", got)
	}
}

func TestGetLastAgent(t *testing.T) {
	tracker := NewSessionTracker(30 * time.Minute)

	tracker.RecordIntent("s1", &Intent{Type: "code", AgentType: "coder"}, "coder")
	tracker.RecordIntent("s1", &Intent{Type: "debug", AgentType: "debugger"}, "debugger")

	got := tracker.GetLastAgent("s1")
	if got != "debugger" {
		t.Errorf("GetLastAgent() = %q, want %q", got, "debugger")
	}
}

func TestGetLastAgentEmpty(t *testing.T) {
	tracker := NewSessionTracker(30 * time.Minute)
	got := tracker.GetLastAgent("nonexistent")
	if got != "" {
		t.Errorf("GetLastAgent() = %q, want empty", got)
	}
}

func TestGetIntentCounts(t *testing.T) {
	tracker := NewSessionTracker(30 * time.Minute)

	tracker.RecordIntent("s1", &Intent{Type: "code", AgentType: "coder"}, "coder")
	tracker.RecordIntent("s1", &Intent{Type: "code", AgentType: "coder"}, "coder")
	tracker.RecordIntent("s1", &Intent{Type: "chat", AgentType: "chat"}, "chat")

	counts := tracker.GetIntentCounts("s1")
	if counts["code"] != 2 {
		t.Errorf("counts[\"code\"] = %d, want 2", counts["code"])
	}
	if counts["chat"] != 1 {
		t.Errorf("counts[\"chat\"] = %d, want 1", counts["chat"])
	}
}

func TestGetIntentCountsEmpty(t *testing.T) {
	tracker := NewSessionTracker(30 * time.Minute)
	counts := tracker.GetIntentCounts("nonexistent")
	if len(counts) != 0 {
		t.Errorf("len(counts) = %d, want 0", len(counts))
	}
}

func TestCleanup(t *testing.T) {
	tracker := NewSessionTracker(100 * time.Millisecond)

	tracker.RecordIntent("s1", &Intent{Type: "chat", AgentType: "chat"}, "chat")

	time.Sleep(200 * time.Millisecond)

	tracker.Cleanup()

	state := tracker.GetSession("s1")
	if state != nil {
		t.Error("GetSession() should return nil after cleanup")
	}
}

func TestConcurrentAccess(t *testing.T) {
	tracker := NewSessionTracker(30 * time.Minute)
	var wg sync.WaitGroup

	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				intent := &Intent{Type: "code", Confidence: float64(n) * 0.01, AgentType: "coder"}
				tracker.RecordIntent("session1", intent, "coder")
			}
		}(i)
	}

	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tracker.GetDominantIntent("session1")
				tracker.GetLastIntent("session1")
				tracker.GetLastAgent("session1")
				tracker.GetIntentCounts("session1")
			}
		}()
	}

	wg.Wait()
}
