package rpc

import (
	"context"
	"strings"
	"testing"
)

func TestDelegateStatusDisabledEnvelope(t *testing.T) {
	h := NewDelegateChildrenHandler(DelegateChildrenSeam{})
	res, err := h.handleStatus(context.Background(), []byte(`{"query":"sess-1"}`))
	if err != nil {
		t.Fatalf("disabled posture must answer, not error: %v", err)
	}
	v, ok := res.(DelegateStatusResult)
	if !ok {
		t.Fatalf("result type = %T, want DelegateStatusResult", res)
	}
	if v.Children == nil || len(v.Children) != 0 {
		t.Errorf("children must be an empty list, got %+v", v.Children)
	}
	if v.Note == "" {
		t.Error("disabled posture must carry an explanatory note")
	}
}

func TestDelegateStatusEmptyQueryNamedError(t *testing.T) {
	h := NewDelegateChildrenHandler(DelegateChildrenSeam{})
	if _, err := h.handleStatus(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("empty query must be a named error")
	} else if !strings.Contains(err.Error(), "query is required") {
		t.Errorf("error text = %q, want it to name the required query", err.Error())
	}
}

func TestDelegateStatusFiltersBySessionAgentChild(t *testing.T) {
	seam := DelegateChildrenSeam{Children: func() []DelegateChildRow {
		return []DelegateChildRow{
			NewDelegateChildRow("delegate-a1", "coder", "sess-1", "spawn", "running", "m1", "", "", false, ""),
			NewDelegateChildRow("delegate-a2", "writer", "sess-1", "acp", "completed", "m2", "p", "2026-11-08T00:00:00Z", true, ""),
			NewDelegateChildRow("delegate-a3", "coder", "sess-2", "spawn", "failed", "", "", "", false, "boom"),
			NewDelegateChildRow("delegate-a4", "analyst", "sess-3", "spawn", "completed", "", "", "", true, ""),
		}
	}}
	h := NewDelegateChildrenHandler(seam)

	// By session id.
	res, err := h.handleStatus(context.Background(), []byte(`{"query":"sess-1"}`))
	if err != nil {
		t.Fatalf("status by session: %v", err)
	}
	v := res.(DelegateStatusResult)
	if len(v.Children) != 2 {
		t.Fatalf("children for sess-1 = %d, want 2", len(v.Children))
	}
	if v.Children[0].ChildID != "delegate-a1" || v.Children[1].ChildID != "delegate-a2" {
		t.Errorf("unexpected rows: %+v", v.Children)
	}
	if !v.Children[1].HasResult || v.Children[1].CreatedAt == "" {
		t.Errorf("completed row lost result presence/created_at: %+v", v.Children[1])
	}

	// By agent id (matches across sessions).
	res, _ = h.handleStatus(context.Background(), []byte(`{"query":"coder"}`))
	v = res.(DelegateStatusResult)
	if len(v.Children) != 2 {
		t.Fatalf("children for coder = %d, want 2", len(v.Children))
	}

	// By child id.
	res, _ = h.handleStatus(context.Background(), []byte(`{"query":"delegate-a4"}`))
	v = res.(DelegateStatusResult)
	if len(v.Children) != 1 || v.Children[0].AgentID != "analyst" {
		t.Fatalf("children for delegate-a4 = %+v, want the analyst row", v.Children)
	}

	// Unknown query: empty list + note, never an error.
	res, err = h.handleStatus(context.Background(), []byte(`{"query":"sess-nope"}`))
	if err != nil {
		t.Fatalf("unknown query must not error: %v", err)
	}
	v = res.(DelegateStatusResult)
	if len(v.Children) != 0 || v.Note == "" {
		t.Errorf("unknown query = %+v, want empty list + note", v)
	}
}

func TestDelegateStatusErrorTextBounded(t *testing.T) {
	long := strings.Repeat("x", 1000)
	row := NewDelegateChildRow("delegate-b1", "coder", "sess-9", "spawn", "failed", "", "", "", false, long)
	if len(row.Error) >= 1000 {
		t.Errorf("error text not bounded: %d chars", len(row.Error))
	}
	if !strings.HasSuffix(row.Error, " ...") {
		t.Errorf("bounded error must carry a truncation marker, got %q", row.Error)
	}
}

func TestDelegateStatusRegister(t *testing.T) {
	h := NewDelegateChildrenHandler(DelegateChildrenSeam{Children: func() []DelegateChildRow {
		return nil
	}})
	s := New(&Config{}, nil, nil)
	h.RegisterDelegateStatusMethods(s)
	if _, err := s.CallMethod(context.Background(), "delegate.status", []byte(`{"query":"sess-1"}`)); err != nil {
		t.Fatalf("delegate.status not registered: %v", err)
	}
}
