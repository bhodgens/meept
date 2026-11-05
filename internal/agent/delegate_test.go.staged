package agent

// T2 gate (01-delegation/00T-tests.md): B1 child-registry type pins.
// Contract .hermes/contracts/01-delegation.md B1:
//   ChildState string ("pending"|"running"|"completed"|"interrupted"|"failed")
//   ChildRecord{ChildID, AgentID, ParentSessionID, Mode, State, Model,
//               Provider, ReasoningEffort, CreatedAt, Result, Err, Descriptor}
//   NewDelegateManager(reg *AgentRegistry, publisher func(string, any),
//                      cancelHook func(string) error) *DelegateManager
//   Spawn / Children / Lookup / Join.
//
// RED until leaf 01-spawn-async lands (symbols undefined).
// Fast unit gate: struct shape + registry transitions only; the live seam
// is the e2e suite (T1/T5/T6/T7).

import (
	"context"
	"testing"
	"time"
)

func TestChildRecord_StateTransitions(t *testing.T) {
	mgr := NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })

	res, err := mgr.Spawn(context.Background(), DelegateRequest{
		AgentID: "worker",
		Message: "say hi",
		Mode:    "spawn",
	}, "sess-parent")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if res.ChildID == "" {
		t.Fatal("async spawn must return a child_id")
	}
	if res.Status != "running" {
		t.Fatalf("Spawn status = %q, want running (async)", res.Status)
	}

	rec, ok := mgr.Lookup(res.ChildID)
	if !ok {
		t.Fatalf("Lookup(%q) not found right after spawn", res.ChildID)
	}
	if rec.State != ChildStateRunning {
		t.Errorf("record state = %q, want running", rec.State)
	}
	if rec.ParentSessionID != "sess-parent" {
		t.Errorf("ParentSessionID = %q, want sess-parent", rec.ParentSessionID)
	}

	// Settle the child through the manager's test seam and re-read.
	mgr.SettleForTest(res.ChildID, ChildStateCompleted, "hi", "")
	rec, _ = mgr.Lookup(res.ChildID)
	if rec.State != ChildStateCompleted {
		t.Errorf("state after settle = %q, want completed", rec.State)
	}
	if rec.Result != "hi" {
		t.Errorf("Result = %q, want hi", rec.Result)
	}
	if rec.Err != "" {
		t.Errorf("Err = %q on completed child, want empty", rec.Err)
	}
}

func TestChildRecord_InterruptedAndFailedStates(t *testing.T) {
	mgr := NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })

	res, err := mgr.Spawn(context.Background(), DelegateRequest{
		AgentID: "worker", Message: "long task",
	}, "sess-parent")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if err := mgr.Cancel(res.ChildID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	rec, _ := mgr.Lookup(res.ChildID)
	if rec.State != ChildStateInterrupted {
		t.Errorf("state after cancel = %q, want interrupted", rec.State)
	}

	res2, err := mgr.Spawn(context.Background(), DelegateRequest{
		AgentID: "worker", Message: "boom",
	}, "sess-parent")
	if err != nil {
		t.Fatalf("Spawn 2: %v", err)
	}
	mgr.SettleForTest(res2.ChildID, ChildStateFailed, "", "provider exploded")
	rec2, _ := mgr.Lookup(res2.ChildID)
	if rec2.State != ChildStateFailed {
		t.Errorf("state = %q, want failed", rec2.State)
	}
	if rec2.Err != "provider exploded" {
		t.Errorf("Err = %q, want provider exploded", rec2.Err)
	}
}

func TestDelegateManager_ChildrenFiltersByParent(t *testing.T) {
	mgr := NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })

	if _, err := mgr.Spawn(context.Background(),
		DelegateRequest{AgentID: "w", Message: "a"}, "sess-a"); err != nil {
		t.Fatalf("spawn a: %v", err)
	}
	if _, err := mgr.Spawn(context.Background(),
		DelegateRequest{AgentID: "w", Message: "b"}, "sess-a"); err != nil {
		t.Fatalf("spawn b: %v", err)
	}
	if _, err := mgr.Spawn(context.Background(),
		DelegateRequest{AgentID: "w", Message: "c"}, "sess-b"); err != nil {
		t.Fatalf("spawn c: %v", err)
	}

	got := mgr.Children("sess-a")
	if len(got) != 2 {
		t.Errorf("Children(sess-a) = %d records, want 2", len(got))
	}
	for _, r := range got {
		if r.ParentSessionID != "sess-a" {
			t.Errorf("foreign record leaked into Children(sess-a): %q", r.ParentSessionID)
		}
	}
	if n := len(mgr.Children("sess-unknown")); n != 0 {
		t.Errorf("Children(unknown parent) = %d, want 0", n)
	}
}

func TestDelegateManager_LookupUnknownChild(t *testing.T) {
	mgr := NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })
	if _, ok := mgr.Lookup("child-does-not-exist"); ok {
		t.Error("Lookup on unknown child returned ok=true")
	}
}

func TestDelegateManager_JoinBoundedWait(t *testing.T) {
	mgr := NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })

	res, err := mgr.Spawn(context.Background(),
		DelegateRequest{AgentID: "w", Message: "settle soon"}, "sess-j")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Settle asynchronously, then Join must return the completed result.
	go func() {
		time.Sleep(50 * time.Millisecond)
		mgr.SettleForTest(res.ChildID, ChildStateCompleted, "done", "")
	}()

	joined, err := mgr.Join(context.Background(), "sess-j", res.ChildID, 5*time.Second)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if joined.Status != "completed" || joined.Response != "done" {
		t.Errorf("Join result = {status:%q response:%q}, want {completed done}",
			joined.Status, joined.Response)
	}

	// Join on an unsettled child with a tiny timeout must NOT hang forever.
	res2, _ := mgr.Spawn(context.Background(),
		DelegateRequest{AgentID: "w", Message: "never settles"}, "sess-j")
	if _, err := mgr.Join(context.Background(), "sess-j", res2.ChildID, 200*time.Millisecond); err == nil {
		t.Error("Join past its timeout returned nil error, want a deadline error")
	}
}
