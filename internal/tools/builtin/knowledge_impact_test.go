package builtin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestKnowledgeImpactTool_Execute covers the impact tool's contract: the
// symbol argument flows through, callers come back (nil normalized to an
// empty array so the JSON envelope never renders "callers": null), errors
// wrap, and a nil closure / empty symbol are honest config/arg errors.
func TestKnowledgeImpactTool_Execute(t *testing.T) {
	t.Run("returns callers", func(t *testing.T) {
		tool := NewKnowledgeImpactTool(func(_ context.Context, symbol string) ([]string, error) {
			if symbol != "alpha.Parse" {
				t.Errorf("symbol = %q, want alpha.Parse", symbol)
			}
			return []string{"alpha.Run", "alpha.Load"}, nil
		})
		res, err := tool.Execute(context.Background(), map[string]any{"symbol": "alpha.Parse"})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		m, ok := res.(map[string]any)
		if !ok {
			t.Fatalf("result type %T, want map", res)
		}
		if m["success"] != true {
			t.Errorf("success = %v, want true", m["success"])
		}
		callers, ok := m["callers"].([]string)
		if !ok || len(callers) != 2 {
			t.Fatalf("callers = %#v, want 2 entries", m["callers"])
		}
		if callers[0] != "alpha.Run" || callers[1] != "alpha.Load" {
			t.Errorf("callers = %v, want [alpha.Run alpha.Load]", callers)
		}
	})

	t.Run("nil callers normalized to empty", func(t *testing.T) {
		tool := NewKnowledgeImpactTool(func(context.Context, string) ([]string, error) {
			return nil, nil
		})
		res, err := tool.Execute(context.Background(), map[string]any{"symbol": "Missing"})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		m := res.(map[string]any)
		callers, ok := m["callers"].([]string)
		if !ok || len(callers) != 0 {
			t.Fatalf("callers = %#v, want empty non-nil", m["callers"])
		}
	})

	t.Run("error wraps", func(t *testing.T) {
		sentinel := errors.New("store closed")
		tool := NewKnowledgeImpactTool(func(context.Context, string) ([]string, error) {
			return nil, sentinel
		})
		_, err := tool.Execute(context.Background(), map[string]any{"symbol": "X"})
		if err == nil || !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want wrapped sentinel", err)
		}
		if !strings.Contains(err.Error(), "knowledge impact") {
			t.Errorf("error %v missing the knowledge impact prefix", err)
		}
	})

	t.Run("nil closure is a config error", func(t *testing.T) {
		tool := NewKnowledgeImpactTool(nil)
		if _, err := tool.Execute(context.Background(), map[string]any{"symbol": "X"}); err == nil {
			t.Fatal("expected an error for a nil impact closure")
		}
	})

	t.Run("empty symbol rejected", func(t *testing.T) {
		tool := NewKnowledgeImpactTool(func(context.Context, string) ([]string, error) {
			return nil, nil
		})
		if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
			t.Fatal("expected an error for an empty symbol")
		}
	})

	t.Run("metadata", func(t *testing.T) {
		tool := NewKnowledgeImpactTool(nil)
		if tool.Name() != "knowledge_impact" {
			t.Errorf("name = %q", tool.Name())
		}
		if !tool.IsReadOnly(nil) || !tool.IsConcurrencySafe(nil) {
			t.Error("impact tool must be read-only and concurrency-safe")
		}
		params := tool.Parameters()
		if _, ok := params.Properties["symbol"]; !ok {
			t.Error("parameters missing symbol property")
		}
	})
}
