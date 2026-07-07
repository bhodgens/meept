package llm

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRoutingLogger_RecordAndQuery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "routing.db")
	logger, err := NewRoutingLogger(dbPath, nil)
	if err != nil {
		t.Fatalf("NewRoutingLogger: %v", err)
	}
	defer logger.Close()

	dec := RoutingDecision{
		RequestID:        "req-1",
		ChosenModelID:    "qwen2.5:7b",
		ChosenProviderID: "ollama",
		Alias:            "default",
		Reason:           "round-robin",
		Skill:            "",
		CandidatesJSON:   `["qwen2.5:7b","glm-4.5:9b"]`,
	}
	if err := logger.Record(context.Background(), dec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := logger.Recent(context.Background(), 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != 1 || got[0].RequestID != "req-1" {
		t.Errorf("expected 1 record with req-1, got %+v", got)
	}
}
