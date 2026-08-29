package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
)

func newVoteTestManager(t *testing.T) *memory.Manager {
	t.Helper()
	m := memory.NewManager(memory.ManagerConfig{
		Config: config.MemoryConfig{
			DataDir:  t.TempDir(),
			Episodic: config.EpisodicConfig{Enabled: true},
			Task:     config.TaskMemoryConfig{Enabled: true},
		},
	})
	if err := m.Initialize(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func TestMemoryVoteTool_BasicAndEvidence(t *testing.T) {
	mgr := newVoteTestManager(t)
	ctx := context.Background()

	id, err := mgr.Store(ctx, memory.Memory{Content: "vote target", Type: memory.MemoryTypeTask})
	if err != nil {
		t.Fatal(err)
	}

	tool := NewMemoryVoteTool(mgr)
	res, err := tool.Execute(ctx, map[string]any{
		"memory_id": id,
		"delta":     float64(1),
		"reason":    "saved an hour of debugging",
	})
	if err != nil {
		t.Fatalf("vote execute: %v", err)
	}
	out, ok := res.(map[string]any)
	if !ok || out["success"] != true {
		t.Fatalf("unexpected result: %#v", res)
	}
	if net, ok := out["net_votes"].(int); !ok || net != 1 {
		t.Fatalf("evidence net_votes = %v, want 1", out["net_votes"])
	}

	// Second vote flips to net 0, third back to +1.
	if _, err := tool.Execute(ctx, map[string]any{"memory_id": id, "delta": float64(-1)}); err != nil {
		t.Fatalf("second vote: %v", err)
	}
	res2, _ := tool.Execute(ctx, map[string]any{"memory_id": id, "delta": float64(1)})
	if got := res2.(map[string]any)["net_votes"]; got != 1 {
		t.Fatalf("net_votes after (+1,-1,+1) = %v, want 1", got)
	}
}

func TestMemoryVoteTool_Validation(t *testing.T) {
	mgr := newVoteTestManager(t)
	ctx := context.Background()
	tool := NewMemoryVoteTool(mgr)

	if _, err := tool.Execute(ctx, map[string]any{"delta": float64(1)}); err == nil {
		t.Error("missing memory_id must error")
	}
	id, _ := mgr.Store(ctx, memory.Memory{Content: "x", Type: memory.MemoryTypeTask})
	if _, err := tool.Execute(ctx, map[string]any{"memory_id": id, "delta": float64(7)}); err == nil {
		t.Error("delta=7 must error")
	}
	if _, err := tool.Execute(ctx, map[string]any{"memory_id": "missing", "delta": float64(-1)}); err == nil {
		t.Error("unknown memory must error")
	}
}

func TestMemoryVoteTool_ReasonCapped(t *testing.T) {
	mgr := newVoteTestManager(t)
	ctx := context.Background()
	id, _ := mgr.Store(ctx, memory.Memory{Content: "cap me", Type: memory.MemoryTypeTask})

	long := strings.Repeat("r", 5000)
	tool := NewMemoryVoteTool(mgr)
	if _, err := tool.Execute(ctx, map[string]any{"memory_id": id, "delta": float64(1), "reason": long}); err != nil {
		t.Fatalf("capped reason vote failed: %v", err)
	}
}

func TestMemoryVoteTool_NilManager(t *testing.T) {
	tool := NewMemoryVoteTool(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{"memory_id": "a", "delta": float64(1)}); err == nil {
		t.Fatal("nil manager must error")
	}
}
