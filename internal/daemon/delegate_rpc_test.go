package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/rpc"
)

// TestDelegateStatusWiringLiveRegistry: the daemon adapter maps real
// DelegateManager records onto the wire rows and the handler filters them.
func TestDelegateStatusWiringLiveRegistry(t *testing.T) {
	mgr := agent.NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })
	res1, err := mgr.Spawn(context.Background(), agent.DelegateRequest{
		AgentID: "coder", Message: "do a thing", Mode: "spawn", Model: "m1",
	}, "sess-live")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	_, _ = mgr.Spawn(context.Background(), agent.DelegateRequest{
		AgentID: "writer", Message: "other", Mode: "spawn",
	}, "sess-other")
	mgr.SettleForTest(res1.ChildID, agent.ChildStateFailed, "", "child exploded badly "+strings.Repeat("x", 500))

	components := &Components{DelegateManager: mgr}
	srv := rpc.New(&rpc.Config{SocketPath: ""}, nil, nil)
	registerDelegateStatusRPC(components, srv, nil)

	raw := callDelegateStatus(t, srv, "sess-live")
	var out rpc.DelegateStatusResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Children) != 1 {
		t.Fatalf("children for sess-live = %d, want 1 (%s)", len(out.Children), raw)
	}
	row := out.Children[0]
	if row.ChildID != res1.ChildID || row.AgentID != "coder" || row.State != "failed" {
		t.Errorf("row projection wrong: %+v", row)
	}
	if row.Error == "" || len(row.Error) >= 500 {
		t.Errorf("row error must be present and bounded: %q", row.Error)
	}
	if row.CreatedAt == "" {
		if ts := time.Since(time.Time{}); ts == 0 {
			t.Log("no created_at on record")
		}
	}

	// Agent-id query crosses sessions.
	raw = callDelegateStatus(t, srv, "coder")
	_ = json.Unmarshal(raw, &out)
	if len(out.Children) != 1 || out.Children[0].AgentID != "coder" {
		t.Errorf("agent query = %+v, want the coder row", out.Children)
	}

	// Unregistered parent: empty + note.
	raw = callDelegateStatus(t, srv, "sess-none")
	_ = json.Unmarshal(raw, &out)
	if len(out.Children) != 0 || out.Note == "" {
		t.Errorf("unknown parent = %+v, want empty + note", out)
	}
}

// TestDelegateStatusWiringNilComponents: no daemon wiring -> the disabled
// envelope, never "method not found".
func TestDelegateStatusWiringNilComponents(t *testing.T) {
	srv := rpc.New(&rpc.Config{SocketPath: ""}, nil, nil)
	registerDelegateStatusRPC(nil, srv, nil)
	raw := callDelegateStatus(t, srv, "sess-x")
	var out rpc.DelegateStatusResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Children == nil || out.Note == "" {
		t.Errorf("nil wiring = %+v, want empty children + note", out)
	}
}

// callDelegateStatus invokes delegate.status in-process.
func callDelegateStatus(t *testing.T, srv *rpc.Server, query string) json.RawMessage {
	t.Helper()
	res, err := srv.CallMethod(context.Background(), "delegate.status", []byte(`{"query":"`+query+`"}`))
	if err != nil {
		t.Fatalf("delegate.status: %v", err)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
