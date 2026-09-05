package builtin

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
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

// newTemporalTestManager constructs an initialized SQLite-backed memory
// Manager for tool tests, mirroring newVoteTestManager in memory_vote_test.go.
func newTemporalTestManager(t *testing.T) *memory.Manager {
	t.Helper()
	m := memory.NewManager(memory.ManagerConfig{
		Config: config.MemoryConfig{
			DataDir:  t.TempDir(),
			Episodic: config.EpisodicConfig{Enabled: true},
		},
	})
	if err := m.Initialize(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func TestRetainClaimTemporalParams(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		args       map[string]any
		wantErr    bool
		wantKeySet bool // valid_from/valid_to metadata present after store
	}{
		{
			name: "no temporal args = legacy behavior",
			args: map[string]any{"text": "plain claim"},
		},
		{
			name: "valid window",
			args: map[string]any{
				"text":       "bounded",
				"valid_from": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
				"valid_to":   time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
			},
			wantKeySet: true,
		},
		{
			name:    "invalid valid_to errors",
			args:    map[string]any{"text": "x", "valid_to": "next tuesday"},
			wantErr: true,
		},
		{
			name:    "invalid observed_at errors",
			args:    map[string]any{"text": "x", "observed_at": "2026-13-45"},
			wantErr: true,
		},
		{
			name:    "invalid valid_from errors",
			args:    map[string]any{"text": "x", "valid_from": 42},
			wantErr: true,
		},
		{
			name:    "invalid observed_at type errors",
			args:    map[string]any{"text": "x", "observed_at": 7},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mgr := newTemporalTestManager(t)
			tool := NewRetainClaimTool(mgr)
			res, err := tool.Execute(context.Background(), tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got result %#v", res)
				}
				return
			}
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			out, ok := res.(map[string]any)
			if !ok {
				t.Fatalf("unexpected result type %T", res)
			}
			memID, ok := out["memory_id"].(string)
			if !ok || memID == "" {
				t.Fatalf("missing memory_id in result: %#v", out)
			}
			mem, err := mgr.GetByID(context.Background(), memID)
			if err != nil {
				t.Fatalf("load stored memory: %v", err)
			}
			if _, ok := mem.Metadata["valid_from"]; ok != tc.wantKeySet {
				t.Errorf("valid_from presence = %v, want %v", ok, tc.wantKeySet)
			}
			if _, ok := mem.Metadata["valid_to"]; ok != tc.wantKeySet {
				t.Errorf("valid_to presence = %v, want %v", ok, tc.wantKeySet)
			}
			// Legacy case must produce exactly the pre-feature key set.
			if tc.name == "no temporal args = legacy behavior" {
				for _, key := range []string{"valid_from", "valid_to", "observed_at", "rev"} {
					if _, ok := mem.Metadata[key]; ok {
						t.Errorf("legacy claim has unexpected %q key", key)
					}
				}
			}
		})
	}
}

func TestListExpiredClaimsTool(t *testing.T) {
	t.Parallel()
	mgr := newTemporalTestManager(t)
	ctx := context.Background()

	expiredTo := time.Now().UTC().Add(-time.Hour)
	liveTo := time.Now().UTC().Add(time.Hour)
	expiredID, err := mgr.StoreClaim(ctx, memory.Claim{
		Text:    "expired claim",
		Status:  memory.ClaimStatusConfirmed,
		ValidTo: &expiredTo,
	})
	if err != nil {
		t.Fatalf("store expired claim: %v", err)
	}
	if _, err := mgr.StoreClaim(ctx, memory.Claim{
		Text:    "live claim",
		Status:  memory.ClaimStatusConfirmed,
		ValidTo: &liveTo,
	}); err != nil {
		t.Fatalf("store live claim: %v", err)
	}
	if _, err := mgr.StoreClaim(ctx, memory.Claim{Text: "unbounded claim"}); err != nil {
		t.Fatalf("store unbounded claim: %v", err)
	}

	tool := NewListExpiredClaimsTool(mgr)
	if tool.Name() != "list_expired_claims" {
		t.Errorf("Name = %q, want list_expired_claims", tool.Name())
	}
	if tool.Category() != "memory" {
		t.Errorf("Category = %q, want memory", tool.Category())
	}
	if tool.Description() == "" {
		t.Error("Description must not be empty")
	}

	res, err := tool.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	if success, ok := out["success"].(bool); !ok || !success {
		t.Fatalf("success flag = %v, want true", out["success"])
	}
	claims, ok := out["claims"].([]map[string]any)
	if !ok {
		t.Fatalf("claims = %#v, want []map[string]any", out["claims"])
	}
	if len(claims) != 1 {
		t.Fatalf("len(claims) = %d, want 1 (expired only)", len(claims))
	}
	c := claims[0]
	if id, ok := c["memory_id"].(string); !ok || id != expiredID {
		t.Errorf("memory_id = %v, want %q", c["memory_id"], expiredID)
	}
	if _, ok := c["text"].(string); !ok {
		t.Error("text missing or not a string")
	}
	if reason, ok := c["reason"].(string); !ok || reason != "expired" {
		t.Errorf("reason = %v, want expired", c["reason"])
	}
	vt, ok := c["valid_to"].(string)
	if !ok || vt == "" {
		t.Fatalf("valid_to = %v, want non-empty string", c["valid_to"])
	}
	if _, err := time.Parse(time.RFC3339, vt); err != nil {
		t.Errorf("valid_to %q is not RFC3339: %v", vt, err)
	}
	if _, ok := c["rev"].(int64); !ok {
		t.Errorf("rev = %v, want int64", c["rev"])
	}
	if _, ok := c["status"]; !ok {
		t.Error("status missing")
	}

	// limit=0 must not zero out the result (default applies).
	res2, err := tool.Execute(ctx, map[string]any{"limit": float64(0)})
	if err != nil {
		t.Fatalf("execute limit=0: %v", err)
	}
	claims2, ok := res2.(map[string]any)["claims"].([]map[string]any)
	if !ok || len(claims2) != 1 {
		t.Errorf("limit=0 claims = %#v, want 1", claims2)
	}
}

func TestListExpiredClaimsTool_NilManager(t *testing.T) {
	tool := NewListExpiredClaimsTool(nil)
	if _, err := tool.Execute(context.Background(), nil); err == nil {
		t.Error("expected error for nil manager")
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
