package agent

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

func TestNewPairManager(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	if pm.ActiveSessionCount() != 0 {
		t.Errorf("expected 0 active sessions, got %d", pm.ActiveSessionCount())
	}
}

func TestPairManager_CreateSession(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-1", "implement auth", "coder", "planner", 3)

	if session == nil {
		t.Fatal("expected non-nil session")
	}
	if session.TaskID != "task-1" {
		t.Errorf("expected task_id 'task-1', got %q", session.TaskID)
	}
	if pm.ActiveSessionCount() != 1 {
		t.Errorf("expected 1 active session, got %d", pm.ActiveSessionCount())
	}

	// Retrieve it back
	got, ok := pm.GetSession(session.ID)
	if !ok {
		t.Fatal("expected to find session by ID")
	}
	if got.ID != session.ID {
		t.Errorf("expected session ID %q, got %q", session.ID, got.ID)
	}
}

func TestPairManager_CreateSession_DefaultMaxRounds(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-1", "spec", "coder", "planner", 0)
	if session.MaxRounds != DefaultPairMaxRounds {
		t.Errorf("expected default max rounds %d, got %d", DefaultPairMaxRounds, session.MaxRounds)
	}

	session2 := pm.CreateSession("task-2", "spec", "coder", "planner", -1)
	if session2.MaxRounds != DefaultPairMaxRounds {
		t.Errorf("expected default max rounds %d for negative input, got %d", DefaultPairMaxRounds, session2.MaxRounds)
	}
}

func TestPairManager_GetSession_NotFound(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	_, ok := pm.GetSession("nonexistent")
	if ok {
		t.Error("should not find nonexistent session")
	}
}

func TestPairManager_GetSessionByTask(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-42", "spec", "coder", "planner", 3)

	got, ok := pm.GetSessionByTask("task-42")
	if !ok {
		t.Fatal("expected to find session by task ID")
	}
	if got.ID != session.ID {
		t.Errorf("expected session ID %q, got %q", session.ID, got.ID)
	}

	_, ok = pm.GetSessionByTask("task-nonexistent")
	if ok {
		t.Error("should not find session for nonexistent task")
	}

	// Terminal sessions should not be returned
	session.MarkConverged()
	_, ok = pm.GetSessionByTask("task-42")
	if ok {
		t.Error("should not find terminal session by task")
	}
}

func TestPairManager_GetSessionByStep(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-1", "spec", "coder", "planner", 3)
	session.AddStepID("step-alpha")

	got, ok := pm.GetSessionByStep("step-alpha")
	if !ok {
		t.Fatal("expected to find session by step ID")
	}
	if got.ID != session.ID {
		t.Errorf("expected session ID %q, got %q", session.ID, got.ID)
	}

	_, ok = pm.GetSessionByStep("step-other")
	if ok {
		t.Error("should not find session for unowned step")
	}
}

func TestPairManager_RemoveSession(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-1", "spec", "coder", "planner", 3)
	pm.RemoveSession(session.ID)

	_, ok := pm.GetSession(session.ID)
	if ok {
		t.Error("should not find removed session")
	}
	if pm.ActiveSessionCount() != 0 {
		t.Errorf("expected 0 active sessions after removal, got %d", pm.ActiveSessionCount())
	}
}

func TestPairManager_ListSessions(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	s1 := pm.CreateSession("task-1", "spec", "coder", "planner", 3)
	s2 := pm.CreateSession("task-2", "spec", "coder", "planner", 3)

	all := pm.ListSessions(false)
	if len(all) != 2 {
		t.Fatalf("expected 2 total sessions, got %d", len(all))
	}

	active := pm.ListSessions(true)
	if len(active) != 2 {
		t.Fatalf("expected 2 active sessions, got %d", len(active))
	}

	s1.MarkConverged()
	active = pm.ListSessions(true)
	if len(active) != 1 {
		t.Errorf("expected 1 active session after convergence, got %d", len(active))
	}
	_ = s2 // use variable
}

func TestPairManager_parseReviewOutput(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	tests := []struct {
		name         string
		output       string
		wantApproved bool
	}{
		{
			name:         "explicit approval",
			output:       "The implementation looks good. All requirements met.",
			wantApproved: true,
		},
		{
			name:         "lgtm",
			output:       "LGTM, ship it",
			wantApproved: true,
		},
		{
			name:         "rejection with issues",
			output:       "Missing error handling\nNo test coverage for edge cases",
			wantApproved: false,
		},
		{
			name:         "structured JSON approved",
			output:       `{"status": "approved", "feedback": "all good", "confidence": 0.95}`,
			wantApproved: true,
		},
		{
			name:         "structured JSON rejected",
			output:       `{"status": "rejected", "feedback": "needs work", "issues": ["no tests"], "confidence": 0.8}`,
			wantApproved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pm.parseReviewOutput(tt.output)
			if tt.wantApproved && result.Status != ReviewApproved {
				t.Errorf("expected approved, got %q", result.Status)
			}
			if !tt.wantApproved && result.Status == ReviewApproved {
				t.Errorf("expected non-approved, got approved")
			}
		})
	}
}

func TestPairManager_parseReviewOutput_JSONCodeBlock(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	output := "Here is my review:\n```json\n{\"status\": \"approved\", \"feedback\": \"good\", \"confidence\": 0.9}\n```"
	result := pm.parseReviewOutput(output)
	if result.Status != ReviewApproved {
		t.Errorf("expected approved from JSON code block, got %q", result.Status)
	}
	if result.Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %f", result.Confidence)
	}
}

func TestPairManager_updateCriteria(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-1", "spec", "coder", "planner", 3)
	session.SetCriteria([]string{"write tests", "handle errors"})

	// Rejected review should not change criteria
	pm.updateCriteria(session, &ReviewResult{Status: ReviewRejected})
	if len(session.Context.AcceptedCriteria) != 0 {
		t.Error("rejected review should not move criteria to accepted")
	}
	if len(session.Context.PendingCriteria) != 2 {
		t.Error("pending criteria should remain unchanged after rejection")
	}

	// Approved review should move all pending to accepted
	pm.updateCriteria(session, &ReviewResult{Status: ReviewApproved})
	if len(session.Context.AcceptedCriteria) != 2 {
		t.Errorf("expected 2 accepted criteria, got %d", len(session.Context.AcceptedCriteria))
	}
	if len(session.Context.PendingCriteria) != 0 {
		t.Errorf("expected 0 pending criteria, got %d", len(session.Context.PendingCriteria))
	}
}

func TestPairManager_updateCriteria_NeedsInfo(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-1", "spec", "coder", "planner", 3)
	session.SetCriteria([]string{"write tests"})

	// Needs-info review should not change criteria
	pm.updateCriteria(session, &ReviewResult{Status: ReviewNeedsInfo})
	if len(session.Context.AcceptedCriteria) != 0 {
		t.Error("needs_info review should not move criteria to accepted")
	}
	if len(session.Context.PendingCriteria) != 1 {
		t.Errorf("expected 1 pending criteria, got %d", len(session.Context.PendingCriteria))
	}
}

func TestPairManager_RunRound_SessionNotFound(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	_, err := pm.RunRound(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestPairManager_RunRound_TerminalSession(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-1", "spec", "coder", "planner", 3)
	session.MarkConverged()

	_, err := pm.RunRound(context.Background(), session.ID)
	if err == nil {
		t.Fatal("expected error for terminal session")
	}
}

func TestPairManager_RunRound_NoRegistry(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-1", "spec", "coder", "planner", 3)

	_, err := pm.RunRound(context.Background(), session.ID)
	if err == nil {
		t.Fatal("expected error when registry is not configured")
	}
	// Session should be marked failed
	if session.State != PairSessionFailed {
		t.Errorf("expected session state failed, got %q", session.State)
	}
}

func TestPairManager_RunAllRounds_SessionNotFound(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	_, err := pm.RunAllRounds(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestPairManager_RunAllRounds_AlreadyTerminal(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-1", "spec", "coder", "planner", 3)
	session.MarkConverged()

	result, err := pm.RunAllRounds(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != session.ID {
		t.Errorf("expected session ID %q, got %q", session.ID, result.ID)
	}
}

func TestPairManager_runAgent_NoRegistry(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	_, err := pm.runAgent(context.Background(), "coder", "hello", "conv-1")
	if err == nil {
		t.Fatal("expected error when registry is not configured")
	}
}

func TestPairManager_publishEvent_NoBus(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	// Should not panic with nil bus
	pm.publishEvent("pair.test", map[string]any{"key": "value"})
}

func TestPairManager_finalizeTask_NoTaskStore(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-1", "spec", "coder", "planner", 3)

	// Should not panic with nil task store
	pm.finalizeTask(context.Background(), session, true)
}

func TestExtractIssueLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "empty", input: "", want: 0},
		{name: "single line", input: "missing tests", want: 1},
		{name: "multi line", input: "missing tests\nno error handling\n", want: 2},
		{name: "skip headers", input: "# Review\nmissing tests\nno errors", want: 2},
		{name: "capped at 5", input: "a\nb\nc\nd\ne\nf\ng", want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractIssueLines(tt.input)
			if len(got) != tt.want {
				t.Errorf("expected %d issues, got %d", tt.want, len(got))
			}
		})
	}
}

func TestContainsAny(t *testing.T) {
	if !containsAny("hello world", []string{"world"}) {
		t.Error("should find 'world' in 'hello world'")
	}
	if containsAny("hello world", []string{"xyz"}) {
		t.Error("should not find 'xyz' in 'hello world'")
	}
	if !containsAny("hello world", []string{"xyz", "world"}) {
		t.Error("should find 'world' when checking multiple substrings")
	}
	if !containsAny("approved", []string{"approved"}) {
		t.Error("should find exact match")
	}
}

func TestToLower(t *testing.T) {
	if toLower("Hello") != "hello" {
		t.Error("toLower should lowercase strings")
	}
	if toLower("already") != "already" {
		t.Error("toLower should preserve already-lowercase strings")
	}
}

func TestParseReviewJSON(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantStatus ReviewStatus
		wantErr    bool
	}{
		{
			name:       "valid JSON",
			input:      `{"status": "approved", "feedback": "good", "confidence": 0.9}`,
			wantStatus: ReviewApproved,
			wantErr:    false,
		},
		{
			name:       "valid JSON in code block",
			input:      "```json\n{\"status\": \"rejected\", \"feedback\": \"bad\", \"confidence\": 0.5}\n```",
			wantStatus: ReviewRejected,
			wantErr:    false,
		},
		{
			name:    "no JSON",
			input:   "just plain text",
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   "{not valid json}",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &ReviewResult{}
			err := parseReviewJSON(tt.input, result)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != tt.wantStatus {
				t.Errorf("expected status %q, got %q", tt.wantStatus, result.Status)
			}
		})
	}
}

func TestPairManager_BusEvents(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	sub := msgBus.Subscribe("test-pair-events", "pair.*")
	defer msgBus.Unsubscribe(sub)

	pm := NewPairManager(PairManagerConfig{
		Bus:    msgBus,
		Logger: slog.Default(),
	})

	_ = pm.CreateSession("task-1", "spec", "coder", "planner", 3)

	// Drain any creation events
	drainBusMessages(sub)

	pm.publishEvent("pair.test_event", map[string]any{
		KeyTaskID: "task-1",
	})

	select {
	case msg := <-sub.Channel:
		if msg.Topic != "pair.test_event" {
			t.Errorf("expected topic 'pair.test_event', got %q", msg.Topic)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for bus event")
	}
}

func TestPairManager_BusEvents_NilBus(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	// Should not panic
	pm.publishEvent("pair.test", map[string]any{"key": "value"})
}

func TestPairManager_ActiveSessionCount_MultipleStates(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	s1 := pm.CreateSession("task-1", "spec", "coder", "planner", 3)
	s2 := pm.CreateSession("task-2", "spec", "coder", "planner", 3)
	_ = pm.CreateSession("task-3", "spec", "coder", "planner", 3)

	if pm.ActiveSessionCount() != 3 {
		t.Errorf("expected 3 active sessions, got %d", pm.ActiveSessionCount())
	}

	s1.MarkConverged()
	if pm.ActiveSessionCount() != 2 {
		t.Errorf("expected 2 active sessions after first convergence, got %d", pm.ActiveSessionCount())
	}

	s2.MarkFailed()
	if pm.ActiveSessionCount() != 1 {
		t.Errorf("expected 1 active session after failure, got %d", pm.ActiveSessionCount())
	}
}

func drainBusMessages(sub *bus.Subscriber) {
	for {
		select {
		case <-sub.Channel:
		default:
			return
		}
	}
}
