package shadow

import (
	"context"
	"testing"
)

func TestEvalGate_PassesAboveThreshold(t *testing.T) {
	gate := NewEvalGate(0.7)
	run := &TrainingRun{EvalScore: 0.85, RecordsUsed: 100}
	if err := gate.Check(context.Background(), run); err != nil {
		t.Errorf("expected pass, got %v", err)
	}
}

func TestEvalGate_FailsBelowThreshold(t *testing.T) {
	gate := NewEvalGate(0.7)
	run := &TrainingRun{EvalScore: 0.4, RecordsUsed: 100}
	if err := gate.Check(context.Background(), run); err == nil {
		t.Errorf("expected failure, got pass")
	}
}

func TestEvalGate_DisabledWhenThresholdZero(t *testing.T) {
	gate := NewEvalGate(0.0)
	run := &TrainingRun{EvalScore: 0.0, RecordsUsed: 1}
	if err := gate.Check(context.Background(), run); err != nil {
		t.Errorf("disabled gate should always pass, got %v", err)
	}
}

func TestEvalGate_FailsOnInsufficientRecords(t *testing.T) {
	gate := NewEvalGate(0.7)
	run := &TrainingRun{EvalScore: 0.95, RecordsUsed: 5}
	if err := gate.Check(context.Background(), run); err == nil {
		t.Errorf("expected failure on insufficient records, got pass")
	}
}
