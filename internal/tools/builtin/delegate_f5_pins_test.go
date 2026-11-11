package builtin

// F5 bughunt pin tests (C9, C14, C15).
//
//   - C9: delegate_task's schema exposes route fields (provider/model/
//     reasoning_effort) and executeAsync plumbs them into the spawn request.
//   - C14: SendMessage enforces the parent-of rule; unresolvable caller
//     denies (fail-closed); ListChildren refuses a foreign parent_session_id.
//   - C15: workflow_status scopes a session-owned job's result/error to its
//     owner; foreign session refused; owner ok.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/tools"
)

// C9 pin: the delegate_task tool schema carries the route fields.
func TestDelegateTaskTool_SchemaExposesRouteFields(t *testing.T) {
	tool := NewDelegateTaskTool(nil)
	params := tool.Parameters()
	for _, key := range []string{"provider", "model", "reasoning_effort"} {
		if _, ok := params.Properties[key]; !ok {
			t.Errorf("delegate_task schema missing %q property (C9)", key)
		}
	}
}

// C14 pin: SendMessage denies an unresolvable caller (fail-closed), refuses a
// non-parent, and accepts the parent.
func TestSendMessage_ParentOfEnforcement(t *testing.T) {
	mgr := agent.NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })
	res, err := mgr.Spawn(context.Background(), agent.DelegateRequest{
		AgentID: "w", Message: "m",
	}, "sess-parent")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// Resolver UNBOUND: deny (fail-closed).
	unbound := NewDelegateController(mgr, nil, nil)
	r := flatString(unbound.SendMessage(context.Background(), res.ChildID, "hi"))
	if !strings.Contains(r, "denied") {
		t.Fatalf("unbound resolver SendMessage = %q, want explicit deny", r)
	}

	// Bound resolver, foreign caller: refuse.
	foreign := NewDelegateController(mgr, nil, nil)
	foreign.SetCallerSessionResolver(func(ctx context.Context) string { return "sess-other" })
	r = flatString(foreign.SendMessage(context.Background(), res.ChildID, "hi"))
	if !strings.Contains(r, "unauthorized") {
		t.Fatalf("foreign SendMessage = %q, want unauthorized refusal", r)
	}

	// Bound resolver, the legitimate parent: delivered.
	parent := NewDelegateController(mgr, nil, nil)
	parent.SetCallerSessionResolver(func(ctx context.Context) string { return "sess-parent" })
	got, err := parent.SendMessage(context.Background(), res.ChildID, "hi")
	if err != nil {
		t.Fatalf("parent SendMessage: %v", err)
	}
	if m, ok := got.(map[string]any); !ok || m["message_id"] == nil {
		// The child is not running (unwired rig); a non-running state reply
		// is acceptable but must NOT be an authorization failure.
		if s := flatString(got, err); strings.Contains(s, "unauthorized") || strings.Contains(s, "denied") {
			t.Fatalf("parent SendMessage denied: %s", s)
		}
	}
}

// C14 pin: ListChildren honors only the caller's own session.
func TestListChildren_SessionScoped(t *testing.T) {
	mgr := agent.NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })
	if _, err := mgr.Spawn(context.Background(), agent.DelegateRequest{
		AgentID: "w", Message: "m",
	}, "sess-parent"); err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// Unresolvable caller: deny.
	unbound := NewDelegateController(mgr, nil, nil)
	r := flatString(unbound.ListChildren(context.Background(), ""))
	if !strings.Contains(r, "denied") {
		t.Fatalf("unbound ListChildren = %q, want explicit deny", r)
	}

	// Foreign parent_session_id: refused even though the id is valid.
	foreign := NewDelegateController(mgr, nil, nil)
	foreign.SetCallerSessionResolver(func(ctx context.Context) string { return "sess-other" })
	r = flatString(foreign.ListChildren(context.Background(), "sess-parent"))
	if !strings.Contains(r, "unauthorized") {
		t.Fatalf("foreign ListChildren = %q, want unauthorized refusal", r)
	}

	// The caller's own session: listed.
	parent := NewDelegateController(mgr, nil, nil)
	parent.SetCallerSessionResolver(func(ctx context.Context) string { return "sess-parent" })
	got, err := parent.ListChildren(context.Background(), "")
	if err != nil {
		t.Fatalf("own ListChildren: %v", err)
	}
	if m, ok := got.(map[string]any); !ok || len(m["children"].([]childView)) != 1 {
		t.Fatalf("own ListChildren = %v, want exactly the parent's child", got)
	}
	// Explicit own id also works.
	got, err = parent.ListChildren(context.Background(), "sess-parent")
	if err != nil {
		t.Fatalf("explicit own ListChildren: %v", err)
	}
	if m, ok := got.(map[string]any); !ok || len(m["children"].([]childView)) != 1 {
		t.Fatalf("explicit own ListChildren = %v, want the child list", got)
	}
}

// C15 pin: a session-owned background job's status is owner-only.
func TestWorkflowStatus_OwnerScoped(t *testing.T) {
	c, _ := newStatusTestController(t)
	// Bind a switchable caller-session resolver on the SAME controller that
	// owns the job registry.
	caller := "sess-owner"
	c.SetCallerSessionResolver(func(ctx context.Context) string { return caller })
	eng := newTestEngine(t, &fakeSpawner{})
	jobID := eng.RunID() + "-owned"
	c.registerWorkflowJobForSession(jobID, eng, "sess-owner")

	// Owner session: ok.
	view, err := c.WorkflowStatus(tools.ContextWithSessionID(context.Background(), "sess-owner"), jobID)
	if err != nil {
		t.Fatalf("owner WorkflowStatus: %v", err)
	}
	if _, ok := view.(jobStatusView); !ok {
		t.Fatalf("owner status type = %T (%v), want jobStatusView", view, view)
	}

	// Foreign session: refused.
	caller = "sess-foreign"
	r := flatString(c.WorkflowStatus(tools.ContextWithSessionID(context.Background(), "sess-foreign"), jobID))
	if !strings.Contains(r, "unauthorized") {
		t.Fatalf("foreign WorkflowStatus = %q, want unauthorized refusal", r)
	}

	// Unresolvable caller: refused (fail-closed).
	caller = ""
	r = flatString(c.WorkflowStatus(context.Background(), jobID))
	if !strings.Contains(r, "unauthorized") {
		t.Fatalf("sessionless WorkflowStatus = %q, want unauthorized refusal", r)
	}

	// Ownerless job (no owning session recorded): visible to anyone (the
	// pre-C15 behavior for legacy/test registrations).
	caller = "sess-whatever"
	free := eng.RunID() + "-free"
	c.registerWorkflowJob(free, eng)
	if _, err := c.WorkflowStatus(context.Background(), free); err != nil {
		t.Fatalf("ownerless WorkflowStatus: %v", err)
	}
}

// flatString flattens a tool result (any, error) into a string for assertions.
func flatString(res any, err error) string {
	if err != nil {
		return "go-error: " + err.Error()
	}
	switch v := res.(type) {
	case tools.ToolResult:
		return v.Error
	case *tools.ToolResult:
		if v == nil {
			return ""
		}
		return v.Error
	case string:
		return v
	case map[string]any:
		if e, ok := v["error"].(string); ok {
			return e
		}
		return ""
	default:
		return ""
	}
}
