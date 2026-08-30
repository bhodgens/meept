package tui

import (
	"encoding/json"
	"testing"
)

func TestStrMap(t *testing.T) {
	m := map[string]any{"name": "test", "count": 42}
	if strMap(m, "name") != "test" {
		t.Errorf("strMap(name) = %q, want test", strMap(m, "name"))
	}
	if strMap(m, "missing") != "" {
		t.Errorf("strMap(missing) = %q, want empty", strMap(m, "missing"))
	}
}

func TestBoolMap(t *testing.T) {
	m := map[string]any{"ok": true, "fail": false}
	if !boolMap(m, "ok") {
		t.Error("boolMap(ok) = false, want true")
	}
	if boolMap(m, "fail") {
		t.Error("boolMap(fail) = true, want false")
	}
}

func TestIntMap(t *testing.T) {
	m := map[string]any{"n": float64(42)}
	if intMap(m, "n") != 42 {
		t.Errorf("intMap(n) = %d, want 42", intMap(m, "n"))
	}
}

func TestEvalListPayload(t *testing.T) {
	runs := []map[string]any{
		{"id": "r1", "kind": "pass_k", "passed": true},
	}
	raw, _ := json.Marshal(map[string]any{"runs": runs})
	got, err := evalListPayload(raw)
	if err != nil {
		t.Fatalf("evalListPayload: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0]["id"] != "r1" {
		t.Errorf("got[0][id] = %v, want r1", got[0]["id"])
	}
}

func TestEvalListPayload_Error(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"error": "boom"})
	_, err := evalListPayload(raw)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestEvalListPayload_Empty(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"runs": []any{}})
	got, err := evalListPayload(raw)
	if err != nil {
		t.Fatalf("evalListPayload: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0", len(got))
	}
}
