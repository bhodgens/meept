package builtin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/memory"
)

func newFactSearchTestManager(t *testing.T, withStore bool) *memory.Manager {
	t.Helper()
	mgr := memory.NewManager(memory.ManagerConfig{})
	if withStore {
		if _, err := mgr.OpenFactStore(filepath.Join(t.TempDir(), "facts.db")); err != nil {
			t.Fatalf("OpenFactStore: %v", err)
		}
	}
	return mgr
}

func TestMemoryFactSearch_NilStoreEmptyNotError(t *testing.T) {
	mgr := newFactSearchTestManager(t, false)
	tool := NewMemoryFactSearchTool(mgr)
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("nil store must not error: %v", err)
	}
	out, ok := res.(MemoryFactSearchResult)
	if !ok {
		t.Fatalf("result type = %T", res)
	}
	if !out.Success || out.Count != 0 || out.Facts == nil {
		t.Fatalf("empty result wrong: %+v", out)
	}
	if out.Message != "no fact store open" {
		t.Fatalf("message = %q", out.Message)
	}
}

func TestMemoryFactSearch_QueryAndKindFilter(t *testing.T) {
	mgr := newFactSearchTestManager(t, true)
	store := mgr.GetFactStore()
	if store == nil {
		t.Fatal("store nil after OpenFactStore")
	}
	ctx := context.Background()
	for _, f := range []memory.MemoryFact{
		{Kind: memory.FactPreference, Key: "seat", Value: "window seat"},
		{Kind: memory.FactRestriction, Key: "dietary", Value: "vegetarian"},
	} {
		if err := store.Upsert(ctx, f); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	tool := NewMemoryFactSearchTool(mgr)

	// Query hit.
	res, err := tool.Execute(ctx, map[string]any{"query": "window"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	out := res.(MemoryFactSearchResult)
	if out.Count != 1 || out.Facts[0].Key != "seat" {
		t.Fatalf("query result: %+v", out)
	}

	// Kind filter.
	res, err = tool.Execute(ctx, map[string]any{"kind": "restriction"})
	if err != nil {
		t.Fatalf("kind: %v", err)
	}
	out = res.(MemoryFactSearchResult)
	if out.Count != 1 || out.Facts[0].Kind != memory.FactRestriction {
		t.Fatalf("kind result: %+v", out)
	}

	// No match: empty list, not error.
	res, err = tool.Execute(ctx, map[string]any{"query": "zzz-nothing"})
	if err != nil {
		t.Fatalf("no-match: %v", err)
	}
	out = res.(MemoryFactSearchResult)
	if out.Count != 0 || out.Facts == nil {
		t.Fatalf("no-match result: %+v", out)
	}
}

func TestMemoryFactSearch_ResultIsJSONShaped(t *testing.T) {
	mgr := newFactSearchTestManager(t, true)
	if err := mgr.GetFactStore().Upsert(context.Background(),
		memory.MemoryFact{Kind: memory.FactAccount, Key: "united", Value: "123"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	tool := NewMemoryFactSearchTool(mgr)
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Envelope must survive JSON round-trip (model-facing shape).
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back MemoryFactSearchResult
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Count != 1 || back.Facts[0].Value != "123" {
		t.Fatalf("round-trip result: %+v", back)
	}
}
