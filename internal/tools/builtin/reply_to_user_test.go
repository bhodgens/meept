package builtin

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

func TestReplyToUser_Ack(t *testing.T) {
	tool := NewReplyToUserTool(func(text string) error { return nil })

	res, err := tool.Execute(context.Background(), map[string]any{"text": "found the bug"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	tr, ok := res.(*tools.ToolResult)
	if !ok || !tr.Success {
		t.Fatalf("Execute result = %#v, want success ToolResult", res)
	}
	reply, ok := tr.Result.(ReplyResult)
	if !ok || !reply.Ack {
		t.Errorf("result body = %#v, want acked ReplyResult", tr.Result)
	}
	if !tool.Notified() {
		t.Error("Notified must be true after successful delivery")
	}
}

func TestReplyToUser_RequiresText(t *testing.T) {
	tool := NewReplyToUserTool(func(string) error { return nil })
	for _, args := range []map[string]any{
		{},
		{"text": ""},
		{"text": "   \n\t"},
		{"text": 42}, // wrong type yields empty string
	} {
		res, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute(%v) returned error: %v", args, err)
		}
		tr, ok := res.(*tools.ToolResult)
		if !ok || tr.Success || tr.Error == "" {
			t.Errorf("Execute(%v) = %#v, want error ToolResult", args, res)
		}
	}
}

func TestReplyToUser_NilCarrierErrorResult(t *testing.T) {
	tool := NewReplyToUserTool(nil)
	res, err := tool.Execute(context.Background(), map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	tr, ok := res.(*tools.ToolResult)
	if !ok || tr.Success {
		t.Fatalf("nil carrier should yield error ToolResult, got %#v", res)
	}
	if tool.Notified() {
		t.Error("Notified must stay false when no carrier is configured")
	}
}

func TestReplyToUser_IsolatedChildCarrierError(t *testing.T) {
	// C4: the loop's carrier returns this exact error for isolated
	// children; the tool must surface it as a hard error (not a silent
	// success), so the model learns it cannot reach the user.
	tool := NewReplyToUserTool(func(string) error {
		return errors.New("isolated child cannot speak to user")
	})
	_, err := tool.Execute(context.Background(), map[string]any{"text": "leak"})
	if err == nil || !strings.Contains(err.Error(), "isolated child cannot speak to user") {
		t.Errorf("Execute error = %v, want wrapped isolated-child C4 error", err)
	}
	if tool.Notified() {
		t.Error("Notified must stay false for a rejected delivery")
	}
}

func TestReplyToUser_CarrierErrorPropagates(t *testing.T) {
	sentinel := errors.New("router down")
	tool := NewReplyToUserTool(func(string) error { return sentinel })

	_, err := tool.Execute(context.Background(), map[string]any{"text": "hi"})
	if !errors.Is(err, sentinel) {
		t.Errorf("Execute error = %v, want wrapped %v", err, sentinel)
	}
	if tool.Notified() {
		t.Error("Notified must stay false after a failed delivery")
	}
}

func TestReplyToUser_NotifiedLifecycle(t *testing.T) {
	tool := NewReplyToUserTool(func(string) error { return nil })

	if tool.Notified() {
		t.Fatal("new tool must not be marked notified")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"text": "a"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !tool.Notified() {
		t.Error("Notified must be true after a successful delivery")
	}
	tool.ResetNotified()
	if tool.Notified() {
		t.Error("ResetNotified must clear the flag")
	}
}

func TestReplyToUser_SetReplyFuncTypedNilGuard(t *testing.T) {
	calls := 0
	var mu sync.Mutex
	tool := NewReplyToUserTool(func(string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	})

	tool.SetReplyFunc(nil) // must be ignored
	if _, err := tool.Execute(context.Background(), map[string]any{"text": "x"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("carrier calls = %d, want 1 (original carrier retained)", calls)
	}
}

func TestReplyToUser_Metadata(t *testing.T) {
	tool := NewReplyToUserTool(nil)
	if tool.Name() != "reply_to_user" {
		t.Errorf("Name = %q, want reply_to_user", tool.Name())
	}
	params := tool.Parameters()
	found := false
	for _, req := range params.Required {
		if req == "text" {
			found = true
		}
	}
	if !found {
		t.Error("text must be a required parameter")
	}
	if _, ok := params.Properties["text"]; !ok {
		t.Error("text property must be declared")
	}
}
