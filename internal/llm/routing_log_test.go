package llm

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRoutingLogger_ByModel(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "routing_by_model.db")
	logger, err := NewRoutingLogger(dbPath, nil)
	if err != nil {
		t.Fatalf("NewRoutingLogger: %v", err)
	}
	defer logger.Close()

	// Record 3 decisions: 2 for modelA, 1 for modelB.
	decisions := []RoutingDecision{
		{
			RequestID:        "req-a1",
			ChosenModelID:    "modelA",
			ChosenProviderID: "provider1",
			Reason:           "round-robin",
		},
		{
			RequestID:        "req-a2",
			ChosenModelID:    "modelA",
			ChosenProviderID: "provider1",
			Reason:           "explicit",
		},
		{
			RequestID:        "req-b1",
			ChosenModelID:    "modelB",
			ChosenProviderID: "provider2",
			Reason:           "round-robin",
		},
	}
	for _, d := range decisions {
		if err := logger.Record(context.Background(), d); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	// ByModel("modelA") should return 2 entries.
	gotA, err := logger.ByModel(context.Background(), "modelA", 0)
	if err != nil {
		t.Fatalf("ByModel(modelA): %v", err)
	}
	if len(gotA) != 2 {
		t.Errorf("expected 2 records for modelA, got %d", len(gotA))
	}
	for _, d := range gotA {
		if d.ChosenModelID != "modelA" {
			t.Errorf("expected chosen_model_id=modelA, got %q", d.ChosenModelID)
		}
	}

	// ByModel("modelB") should return 1 entry.
	gotB, err := logger.ByModel(context.Background(), "modelB", 0)
	if err != nil {
		t.Fatalf("ByModel(modelB): %v", err)
	}
	if len(gotB) != 1 {
		t.Errorf("expected 1 record for modelB, got %d", len(gotB))
	}
	if gotB[0].RequestID != "req-b1" {
		t.Errorf("expected req-b1, got %q", gotB[0].RequestID)
	}

	// ByModel for nonexistent model should return 0 entries.
	gotNone, err := logger.ByModel(context.Background(), "modelC", 0)
	if err != nil {
		t.Fatalf("ByModel(modelC): %v", err)
	}
	if len(gotNone) != 0 {
		t.Errorf("expected 0 records for modelC, got %d", len(gotNone))
	}
}

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
