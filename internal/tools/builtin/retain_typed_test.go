package builtin

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/memory"
)

func TestRetainClaimTool_NilManager(t *testing.T) {
	tool := NewRetainClaimTool(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{"text": "x"}); err == nil {
		t.Error("expected error for nil manager")
	}
}

func TestRetainClaimTool_MissingText(t *testing.T) {
	tool := NewRetainClaimTool(&memory.Manager{})
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error for missing text")
	}
}

func TestRetainClaimTool_Metadata(t *testing.T) {
	tool := NewRetainClaimTool(nil)
	if tool.Name() != "retain_claim" {
		t.Errorf("Name = %q, want retain_claim", tool.Name())
	}
	if tool.Category() != "memory" {
		t.Errorf("Category = %q, want memory", tool.Category())
	}
	if tool.Description() == "" {
		t.Error("Description must not be empty")
	}
	params := tool.Parameters()
	if params.Type != schemaTypeObject {
		t.Errorf("Parameters.Type = %q, want %q", params.Type, schemaTypeObject)
	}
	if _, ok := params.Properties["text"]; !ok {
		t.Error("Parameters must include 'text' property")
	}
	if _, ok := params.Properties["confidence"]; !ok {
		t.Error("Parameters must include 'confidence' property")
	}
}

func TestRetainDecisionTool_NilManager(t *testing.T) {
	tool := NewRetainDecisionTool(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{"call": "x"}); err == nil {
		t.Error("expected error for nil manager")
	}
}

func TestRetainDecisionTool_MissingCall(t *testing.T) {
	tool := NewRetainDecisionTool(&memory.Manager{})
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error for missing call")
	}
}

func TestRetainDecisionTool_Metadata(t *testing.T) {
	tool := NewRetainDecisionTool(nil)
	if tool.Name() != "retain_decision" {
		t.Errorf("Name = %q, want retain_decision", tool.Name())
	}
	if tool.Category() != "memory" {
		t.Errorf("Category = %q, want memory", tool.Category())
	}
	if tool.Description() == "" {
		t.Error("Description must not be empty")
	}
	params := tool.Parameters()
	if params.Type != schemaTypeObject {
		t.Errorf("Parameters.Type = %q, want %q", params.Type, schemaTypeObject)
	}
	if _, ok := params.Properties["call"]; !ok {
		t.Error("Parameters must include 'call' property")
	}
	if _, ok := params.Properties["expected_outcome"]; !ok {
		t.Error("Parameters must include 'expected_outcome' property")
	}
}

func TestRetainPredictionTool_NilManager(t *testing.T) {
	tool := NewRetainPredictionTool(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{"forecast": "x", "horizon": "2026-12-01T00:00:00Z"}); err == nil {
		t.Error("expected error for nil manager")
	}
}

func TestRetainPredictionTool_MissingForecast(t *testing.T) {
	tool := NewRetainPredictionTool(&memory.Manager{})
	if _, err := tool.Execute(context.Background(), map[string]any{"horizon": "2026-12-01T00:00:00Z"}); err == nil {
		t.Error("expected error for missing forecast")
	}
}

func TestRetainPredictionTool_MissingHorizon(t *testing.T) {
	tool := NewRetainPredictionTool(&memory.Manager{})
	if _, err := tool.Execute(context.Background(), map[string]any{"forecast": "x"}); err == nil {
		t.Error("expected error for missing horizon")
	}
}

func TestRetainPredictionTool_Metadata(t *testing.T) {
	tool := NewRetainPredictionTool(nil)
	if tool.Name() != "retain_prediction" {
		t.Errorf("Name = %q, want retain_prediction", tool.Name())
	}
	if tool.Category() != "memory" {
		t.Errorf("Category = %q, want memory", tool.Category())
	}
	if tool.Description() == "" {
		t.Error("Description must not be empty")
	}
	params := tool.Parameters()
	if params.Type != schemaTypeObject {
		t.Errorf("Parameters.Type = %q, want %q", params.Type, schemaTypeObject)
	}
	if _, ok := params.Properties["forecast"]; !ok {
		t.Error("Parameters must include 'forecast' property")
	}
	if _, ok := params.Properties["horizon"]; !ok {
		t.Error("Parameters must include 'horizon' property")
	}
}

func TestAsStringArg(t *testing.T) {
	if got := asStringArg("literal"); got != "literal" {
		t.Errorf("string passthrough = %q", got)
	}
	if got := asStringArg(42); got != "" {
		t.Errorf("non-string should return empty, got %q", got)
	}
	if got := asStringArg(nil); got != "" {
		t.Errorf("nil should return empty, got %q", got)
	}
}

func TestToStringSlice(t *testing.T) {
	got := toStringSlice([]any{"a", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("string slice = %v", got)
	}
	if got := toStringSlice("not a slice"); len(got) != 0 {
		t.Errorf("non-slice should return empty, got %v", got)
	}
	if got := toStringSlice(nil); len(got) != 0 {
		t.Errorf("nil should return empty, got %v", got)
	}
}
