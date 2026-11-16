package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// mockMessageStore captures Enqueue/DrainInbox/MarkRead behaviour.
type mockMessageStore struct {
	queued  []*AgentMessage
	drained map[string][]*AgentMessage // recipient -> drained messages
	readIDs []string
	nextID  int
	enqErr  error
}

func (m *mockMessageStore) Enqueue(msg *AgentMessage) error {
	if m.enqErr != nil {
		return m.enqErr
	}
	if len(msg.Body) > MaxAgentMessageBody {
		return errors.New("body too large")
	}
	m.nextID++
	msg.ID = "msg-test-" + strconv.Itoa(m.nextID)
	msg.State = "queued"
	m.queued = append(m.queued, msg)
	return nil
}

func (m *mockMessageStore) DrainInbox(to string, limit int) ([]*AgentMessage, error) {
	var out []*AgentMessage
	var remaining []*AgentMessage
	for _, q := range m.queued {
		if q.To == to && len(out) < limit {
			q.State = "delivered"
			out = append(out, q)
		} else {
			remaining = append(remaining, q)
		}
	}
	m.queued = remaining
	if m.drained == nil {
		m.drained = map[string][]*AgentMessage{}
	}
	m.drained[to] = append(m.drained[to], out...)
	return out, nil
}

func (m *mockMessageStore) MarkRead(ids []string) error {
	m.readIDs = append(m.readIDs, ids...)
	return nil
}

func TestSendAgentMessageTool_ImplementsTool(t *testing.T) {
	var _ interface {
		Name() string
		Category() string
		Description() string
	} = (*SendAgentMessageTool)(nil)
}

func TestSendAgentMessageTool_UnknownRecipientListsTargets(t *testing.T) {
	store := &mockMessageStore{}
	tool := NewSendAgentMessageTool(store,
		func(id string) bool { return id == "coder" || id == "planner" },
		func() []string { return []string{"orchestrator", "coder", "planner"} },
		nil,
	)
	res, err := tool.Execute(context.Background(), map[string]any{
		"to": "ghost", "message": "hi",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr, ok := res.(*tools.ToolResult)
	if !ok || tr.Success {
		t.Fatalf("expected error ToolResult; got %T", res)
	}
	if !strings.Contains(tr.Error, "unknown recipient") || !strings.Contains(tr.Error, "coder, planner") {
		t.Fatalf("error should list valid targets; got %q", tr.Error)
	}
}

func TestSendAgentMessageTool_SendsReceipt(t *testing.T) {
	store := &mockMessageStore{}
	tool := NewSendAgentMessageTool(store,
		func(id string) bool { return id == "coder" },
		func() []string { return []string{"coder"} },
		func(ctx context.Context) string { return "orchestrator" },
	)
	res, err := tool.Execute(context.Background(), map[string]any{
		"to": "coder", "message": "please review",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	receipt, ok := res.(SendMessageResult)
	if !ok {
		t.Fatalf("result type %T; want SendMessageResult", res)
	}
	if receipt.ID == "" || receipt.State != "queued" || receipt.To != "coder" {
		t.Fatalf("receipt mismatch: %+v", receipt)
	}
	if len(store.queued) != 1 || store.queued[0].From != "orchestrator" || store.queued[0].Body != "please review" {
		t.Fatalf("enqueue mismatch: %+v", store.queued)
	}
}

func TestInboxTool_DrainsAndMarksRead(t *testing.T) {
	store := &mockMessageStore{}
	bus := &mockHandoffBus{}
	tool := NewInboxTool(store, func(ctx context.Context) string { return "coder" }, bus, "test")
	// Pre-queue two messages for coder.
	_, _ = tool.Execute(context.Background(), nil) // empty inbox first
	sender := NewSendAgentMessageTool(store, func(string) bool { return true }, nil, nil)
	for _, body := range []string{"one", "two"} {
		if _, err := sender.Execute(context.Background(), map[string]any{"to": "coder", "message": body}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	inbox, ok := res.(InboxResult)
	if !ok {
		t.Fatalf("result type %T", res)
	}
	if inbox.Count != 2 || len(inbox.Messages) != 2 {
		t.Fatalf("inbox: %+v", inbox)
	}
	if inbox.Messages[0].Body != "one" || inbox.Messages[1].Body != "two" {
		t.Fatalf("FIFO order violated: %q %q", inbox.Messages[0].Body, inbox.Messages[1].Body)
	}
	if len(store.readIDs) != 2 {
		t.Fatalf("mark read ids: %v", store.readIDs)
	}
	// Second poll is empty (no re-delivery).
	res2, _ := tool.Execute(context.Background(), nil)
	if res2.(InboxResult).Count != 0 {
		t.Fatalf("second drain should be empty")
	}
	// Bus delivered events published.
	if n := len(bus.events); n == 0 {
		t.Fatal("expected agent.message.delivered bus events")
	}
}

// TestSendAgentMessage_Structured pins F7 (inbox leaf 03): the optional
// "structured" object param composes an AgentInboxBody JSON envelope as
// the stored body, while the receipt stays byte-identical to the legacy
// send path. RED until the structured param lands.
func TestSendAgentMessage_Structured(t *testing.T) {
	store := &mockMessageStore{}
	tool := NewSendAgentMessageTool(store,
		func(id string) bool { return id == "bob" },
		func() []string { return []string{"bob"} },
		func(ctx context.Context) string { return "orchestrator" },
	)

	t.Run("parameters declare optional structured object", func(t *testing.T) {
		params := tool.Parameters()
		prop, ok := params.Properties["structured"]
		if !ok {
			t.Fatalf("Parameters() missing optional %q property; have keys %v", "structured", propKeys(params.Properties))
		}
		if prop.Type != schemaTypeObject {
			t.Fatalf("structured property type = %q; want object", prop.Type)
		}
		for _, r := range params.Required {
			if r == "structured" {
				t.Fatal("structured must be optional, not required")
			}
		}
	})

	t.Run("send with structured stores inbox-body envelope", func(t *testing.T) {
		structured := map[string]any{
			"tool_targets": []any{"shell"},
			"file_paths":   []any{},
			"plan_entries": []any{"03-01"},
		}
		res, err := tool.Execute(context.Background(), map[string]any{
			"to":         "bob",
			"message":    "run the shell check",
			"structured": structured,
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		receipt, ok := res.(SendMessageResult)
		if !ok {
			t.Fatalf("result type %T; want SendMessageResult", res)
		}
		if receipt.ID == "" || receipt.State != "queued" || receipt.To != "bob" {
			t.Fatalf("receipt must be unchanged by structured; got %+v", receipt)
		}
		if len(store.queued) != 1 {
			t.Fatalf("queued %d messages; want 1", len(store.queued))
		}

		// The stored body must decode as the AgentInboxBody envelope
		// (local mirror: builtin cannot import internal/employee).
		var envelope struct {
			Body       string `json:"body"`
			Structured *struct {
				ToolTargets []string `json:"tool_targets"`
				FilePaths   []string `json:"file_paths"`
				PlanEntries []string `json:"plan_entries"`
			} `json:"structured"`
		}
		if err := json.Unmarshal([]byte(store.queued[0].Body), &envelope); err != nil {
			t.Fatalf("stored body is not an AgentInboxBody envelope: %v (body %q)", err, store.queued[0].Body)
		}
		if envelope.Body != "run the shell check" {
			t.Fatalf("envelope body = %q; want %q", envelope.Body, "run the shell check")
		}
		if envelope.Structured == nil {
			t.Fatalf("stored envelope lost structured; body %q", store.queued[0].Body)
		}
		if len(envelope.Structured.ToolTargets) != 1 || envelope.Structured.ToolTargets[0] != "shell" {
			t.Fatalf("ToolTargets = %v; want [shell]", envelope.Structured.ToolTargets)
		}
		if len(envelope.Structured.PlanEntries) != 1 || envelope.Structured.PlanEntries[0] != "03-01" {
			t.Fatalf("PlanEntries = %v; want [03-01]", envelope.Structured.PlanEntries)
		}
	})

	t.Run("send without structured keeps legacy raw body", func(t *testing.T) {
		if _, err := tool.Execute(context.Background(), map[string]any{
			"to": "bob", "message": "plain hello",
		}); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if len(store.queued) != 2 {
			t.Fatalf("queued %d messages; want 2", len(store.queued))
		}
		body := store.queued[1].Body
		if body != "plain hello" {
			t.Fatalf("legacy send body = %q; want verbatim %q", body, "plain hello")
		}
	})
}

// propKeys keeps the missing-property failure readable.
func propKeys(m map[string]llm.ParameterProperty) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestEmployeeMessageStoreSatisfiesToolInterface(t *testing.T) {
	// The employee.MessageStore → tool-layer adapter lives in
	// internal/daemon (import-cycle constraint: builtin tests cannot
	// import internal/employee). The store's own surface is covered by
	// internal/employee/messaging_test.go; the adapter is exercised in
	// internal/daemon wiring tests.
	t.Skip("adapter covered in internal/daemon; see agent_message.go docs")
}
