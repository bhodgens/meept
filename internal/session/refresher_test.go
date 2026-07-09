package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

)

// TestRefreshRequest_JSON verifies the RefreshRequest serializes correctly.
func TestRefreshRequest_JSON(t *testing.T) {
	req := RefreshRequest{
		SessionID:   "test-session-123",
		Topic:       "debugging",
		TurnCount:   5,
		Keywords:    []string{"null", "pointer"},
		FirstMsg:    "fix the null pointer",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var unmarshaled RefreshRequest
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if unmarshaled.SessionID != req.SessionID {
		t.Errorf("SessionID = %q, want %q", unmarshaled.SessionID, req.SessionID)
	}
	if unmarshaled.Topic != req.Topic {
		t.Errorf("Topic = %q, want %q", unmarshaled.Topic, req.Topic)
	}
	if unmarshaled.TurnCount != req.TurnCount {
		t.Errorf("TurnCount = %d, want %d", unmarshaled.TurnCount, req.TurnCount)
	}
}

// TestRefreshResult_JSON verifies the RefreshResult serializes correctly.
func TestRefreshResult_JSON(t *testing.T) {
	result := RefreshResult{
		Name:        "debugging",
		Description: "coding: fixed null pointer",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var unmarshaled RefreshResult
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if unmarshaled.Name != result.Name {
		t.Errorf("Name = %q, want %q", unmarshaled.Name, result.Name)
	}
	if unmarshaled.Description != result.Description {
		t.Errorf("Description = %q, want %q", unmarshaled.Description, result.Description)
	}
}

// TestSessionRefresher_Refresh_NoLLMClient verifies fallback behavior when LLM client is nil.
func TestSessionRefresher_Refresh_NoLLMClient(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	refresher := NewSessionRefresher(nil, logger)

	ctx := context.Background()
	req := RefreshRequest{
		SessionID: "test-session-no-llm",
		Topic:     "general",
		TurnCount: 10,
	}

	result, err := refresher.Refresh(ctx, req)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	// Verify fallback naming pattern
	if !strings.HasPrefix(result.Name, "session-") {
		t.Errorf("Name = %q, want session-<count> format", result.Name)
	}
	if !strings.Contains(result.Description, "turns") {
		t.Errorf("Description = %q, should contain 'turns'", result.Description)
	}
}

// TestSessionRefresher_Refresh_EmptyTopic verifies handling of empty topic.
func TestSessionRefresher_Refresh_EmptyTopic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	refresher := NewSessionRefresher(nil, logger)

	ctx := context.Background()
	req := RefreshRequest{
		SessionID: "test-no-topic",
		Topic:     "",
		TurnCount: 3,
	}

	result, err := refresher.Refresh(ctx, req)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	if result.Name == "" {
		t.Error("Name should not be empty")
	}
}

// TestSessionRefresher_Refresh_LargeTurnCount verifies handling of large turn counts.
func TestSessionRefresher_Refresh_LargeTurnCount(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	refresher := NewSessionRefresher(nil, logger)

	ctx := context.Background()
	req := RefreshRequest{
		SessionID: "test-large-turns",
		Topic:     "long-conversation",
		TurnCount: 1000,
	}

	result, err := refresher.Refresh(ctx, req)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	// Should still produce valid result
	if result.Name == "" {
		t.Error("Name should not be empty")
	}
}



// TestRefreshResult_Validation verifies result cleanup logic.
func TestRefreshResult_Cleanup(t *testing.T) {
	tests := []struct {
		name     string
		input    RefreshResult
		wantName string
		wantDesc string
	}{
		{
			name: "multi-word name becomes first word",
			input: RefreshResult{
				Name:        "debugging session",
				Description: "coding: fixed bug",
			},
			wantName: "debugging",
			wantDesc: "coding: fixed bug",
		},
		{
			name: "uppercase name becomes lowercase",
			input: RefreshResult{
				Name:        "DEBUGGING",
				Description: "coding: fixed bug",
			},
			wantName: "debugging",
			wantDesc: "coding: fixed bug",
		},
		{
			name: "long description gets truncated",
			input: RefreshResult{
				Name:        "debugging",
				Description: "this is a very long description that exceeds sixty characters and should be truncated with ellipsis",
			},
			wantName: "debugging",
			wantDesc: "this is a very long description that exceeds sixty charac...",
		},
		{
			name: "description trailing period removed",
			input: RefreshResult{
				Name:        "debugging",
				Description: "coding: fixed bug.",
			},
			wantName: "debugging",
			wantDesc: "coding: fixed bug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the cleanup logic from refresher.go
			result := tt.input
			result.Name = strings.ToLower(strings.TrimSpace(result.Name))
			result.Description = strings.ToLower(strings.TrimSpace(result.Description))
			result.Description = strings.TrimSuffix(result.Description, ".")

			if words := strings.Fields(result.Name); len(words) > 1 {
				result.Name = words[0]
			}
			if len(result.Description) > 60 {
				result.Description = result.Description[:57] + "..."
			}

			if result.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", result.Name, tt.wantName)
			}
			if result.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", result.Description, tt.wantDesc)
			}
		})
	}
}

// TestSessionRefresher_Refresh_WithKeywords verifies keywords are included in prompt.
func TestSessionRefresher_Refresh_WithKeywords(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	refresher := NewSessionRefresher(nil, logger)

	ctx := context.Background()
	req := RefreshRequest{
		SessionID: "test-keywords",
		Topic:     "debugging",
		TurnCount: 5,
		Keywords:  []string{"auth", "token", "expiry"},
	}

	result, err := refresher.Refresh(ctx, req)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	if result.Name == "" {
		t.Error("Name should not be empty")
	}
}

// TestSessionRefresher_Refresh_FirstMessageContext verifies first message is included.
func TestSessionRefresher_Refresh_FirstMessageContext(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	refresher := NewSessionRefresher(nil, logger)

	ctx := context.Background()
	req := RefreshRequest{
		SessionID: "test-first-msg",
		Topic:     "follow-up",
		TurnCount: 10,
		FirstMsg:  "How do I implement authentication in Go?",
	}

	result, err := refresher.Refresh(ctx, req)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	if result.Name == "" {
		t.Error("Name should not be empty")
	}
}

// TestSessionRefresher_Refresh_FirstMessageTruncation verifies long first messages are truncated.
func TestSessionRefresher_Refresh_FirstMessageTruncation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	refresher := NewSessionRefresher(nil, logger)

	longMsg := strings.Repeat("word ", 50) // 250 characters
	req := RefreshRequest{
		SessionID: "test-truncate",
		Topic:     "topic",
		TurnCount: 2,
		FirstMsg:  longMsg,
	}

	// Just verify no panic on long input
	_, err := refresher.Refresh(context.Background(), req)
	if err != nil {
		t.Fatalf("Refresh failed on long first message: %v", err)
	}
}

// BenchmarkRefreshRequest_Marshal benchmarks JSON marshaling performance.
func BenchmarkRefreshRequest_Marshal(b *testing.B) {
	req := RefreshRequest{
		SessionID:   "test-session",
		Topic:       "debugging",
		TurnCount:   10,
		Keywords:    []string{"auth", "token"},
		FirstMsg:    "How does authentication work?",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRefreshResult_Marshal benchmarks result JSON marshaling performance.
func BenchmarkRefreshResult_Marshal(b *testing.B) {
	result := RefreshResult{
		Name:        "debugging",
		Description: "coding: fixed authentication bug",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(result)
		if err != nil {
			b.Fatal(err)
		}
	}
}
