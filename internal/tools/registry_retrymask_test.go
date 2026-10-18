package tools

import (
	"context"
	"strings"
	"testing"
)

// TestExecuteWithRetry_PreservesSpecificError pins bughunt 2026-10-15 A9:
// a ToolResult{Success:false, Error:"specific"} returned with err==nil must
// surface the specific error text, never the generic
// "unknown error during tool execution" defensive guard.
//
// Pre-fix shape: the `else if result != nil && result.Error != ""` branch was
// absent, lastErr stayed nil, the guard fired, and the specific reason was
// replaced by the generic string.
func TestExecuteWithRetry_PreservesSpecificError(t *testing.T) {
	r := NewRegistry(nil)
	tool := &mockTool{
		name: "specific_fail_tool",
		execFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return &ToolResult{Success: false, Error: "specific disk quota exceeded"}, nil
		},
	}
	r.Register(tool)

	result, err := r.ExecuteWithRetry(context.Background(), "specific_fail_tool", nil)
	if err != nil {
		t.Fatalf("expected nil transport error, got %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Success {
		t.Fatal("expected Success=false")
	}
	if result.Error == "" || result.Error == "unknown error during tool execution" {
		t.Fatalf("specific error was masked: %q", result.Error)
	}
	if !strings.Contains(result.Error, "disk quota") {
		t.Fatalf("expected specific error to survive, got %q", result.Error)
	}
}
