package builtin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// Decision-model plan tree, leaf 03 (docs/plans/decision-model/03-irreversibility-gate.md):
// pins the DecisionGateHook — fires only when enabled AND the tool is on the
// gate allowlist AND the decision client is available; high confidence allows,
// low confidence denies with a BlockResult (the loop converts the block into a
// phase-1 confirmation-shaped tool result); every client error fails OPEN.
// Client stub pattern reused from internal/llm/decision_client_test.go
// (decisionStubChatter).

// gateStubClient is a decisionAsker test double returning a canned result.
type gateStubClient struct {
	result *llm.DecisionResult
	err    error
	called bool
	// lastQuestion captures the rendered question for shape assertions.
	lastQuestion *llm.DecisionQuestion
}

func (s *gateStubClient) Available() bool { return true }

func (s *gateStubClient) Ask(_ context.Context, q llm.DecisionQuestion) (*llm.DecisionResult, error) {
	s.called = true
	s.lastQuestion = &q
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

// gateTestClient wraps the stub in the hook's client interface.
func gateTestClient(stub *gateStubClient) decisionAsker { return stub }

func gateToolCall(name, argsJSON string) llm.ToolCall {
	return llm.ToolCall{
		ID:       "call-1",
		Type:     "function",
		Function: llm.ToolCallFunction{Name: name, Arguments: argsJSON},
	}
}

// TestDecisionGate_DisabledNeverFires: hook disabled → never fires (zero
// overhead; the client must not even be consulted).
func TestDecisionGate_DisabledNeverFires(t *testing.T) {
	stub := &gateStubClient{result: &llm.DecisionResult{Answer: "yes", Confidence: 1.0}}
	hook := NewDecisionGateHook(gateTestClient(stub), 0.7, nil, nil)
	hook.enabled = false

	got := hook.BeforeToolCall(context.Background(), gateToolCall("shell", `{"command":"rm -rf /tmp/x"}`))
	if got.Block {
		t.Fatalf("disabled hook blocked the call: %+v", got)
	}
	if stub.called {
		t.Fatal("disabled hook consulted the decision client")
	}
}

// TestDecisionGate_NilGateNilClient: a nil hook or nil client must never
// block (typed-nil / unconfigured safety).
func TestDecisionGate_NilGateNilClient(t *testing.T) {
	var nilHook *DecisionGateHook
	if got := nilHook.BeforeToolCall(context.Background(), gateToolCall("shell", `{}`)); got.Block {
		t.Fatal("nil hook blocked the call")
	}
	hook := NewDecisionGateHook(nil, 0.7, nil, nil)
	if got := hook.BeforeToolCall(context.Background(), gateToolCall("shell", `{}`)); got.Block {
		t.Fatal("nil-client hook blocked the call")
	}
}

// TestDecisionGate_ToolNotOnAllowlist: the gate only consults tools on its
// list (default ["shell","file_write"] — verified real registry names).
func TestDecisionGate_ToolNotOnAllowlist(t *testing.T) {
	stub := &gateStubClient{result: &llm.DecisionResult{Answer: "no", Confidence: 0.2}}
	hook := NewDecisionGateHook(gateTestClient(stub), 0.7, nil, nil)

	if got := hook.BeforeToolCall(context.Background(), gateToolCall("file_read", `{"path":"/etc/passwd"}`)); got.Block {
		t.Fatalf("read-only tool blocked: %+v", got)
	}
	if stub.called {
		t.Fatal("client consulted for a non-allowlisted tool")
	}

	// Default allowlist covers the real destructive names.
	for _, name := range []string{"shell", "file_write"} {
		stub := &gateStubClient{result: &llm.DecisionResult{Answer: "no", Confidence: 0.2}}
		hook := NewDecisionGateHook(gateTestClient(stub), 0.0, nil, nil)
		if !hook.gateTools[name] {
			t.Fatalf("default allowlist missing real tool name %q (have %v)", name, hook.gateTools)
		}
	}
}

// TestDecisionGate_ClientErrorFailsOpen: any client error (including
// ErrDecisionUnavailable) → allow + Warn log. The gate never makes things
// worse when the decision layer is down.
func TestDecisionGate_ClientErrorFailsOpen(t *testing.T) {
	cases := []struct {
		name string
		stub *gateStubClient
	}{
		{"transport_error", &gateStubClient{err: errors.New("connection refused")}},
		{"unconfigured", &gateStubClient{err: llm.ErrDecisionUnavailable}},
		{"parse_failure", &gateStubClient{err: llm.ErrDecisionParse}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hook := NewDecisionGateHook(gateTestClient(tc.stub), 0.7, nil, nil)
			hook.SetEnabled(true)
			got := hook.BeforeToolCall(context.Background(), gateToolCall("shell", `{"command":"rm -rf /tmp/x"}`))
			if got.Block {
				t.Fatalf("client error blocked the call (fail-open violated): %+v", got)
			}
		})
	}
}

// TestDecisionGate_HighConfidenceAllows: confidence >= threshold → allow with
// zero modification.
func TestDecisionGate_HighConfidenceAllows(t *testing.T) {
	stub := &gateStubClient{result: &llm.DecisionResult{Answer: "yes", Confidence: 1.0}}
	hook := NewDecisionGateHook(gateTestClient(stub), 0.7, nil, nil)
	hook.SetEnabled(true)

	got := hook.BeforeToolCall(context.Background(), gateToolCall("shell", `{"command":"rm -rf /tmp/x"}`))
	if got.Block {
		t.Fatalf("high-confidence call blocked: %+v", got)
	}
	if !stub.called {
		t.Fatal("client was not consulted")
	}
}

// TestDecisionGate_LowConfidenceDenies: confidence < threshold → the hook's
// deny mechanism (BlockResult.Block=true); the loop turns the block into a
// phase-1 ConfirmationResponse-shaped tool result.
func TestDecisionGate_LowConfidenceDenies(t *testing.T) {
	stub := &gateStubClient{result: &llm.DecisionResult{Answer: "no", Confidence: 0.2}}
	hook := NewDecisionGateHook(gateTestClient(stub), 0.7, nil, nil)
	hook.SetEnabled(true)

	got := hook.BeforeToolCall(context.Background(), gateToolCall("shell", `{"command":"rm -rf /tmp/x"}`))
	if !got.Block {
		t.Fatal("low-confidence call allowed, want deny (escalate to confirmation)")
	}
	if got.Reason == "" {
		t.Fatal("deny reason empty")
	}
}

// TestDecisionGate_ZeroThresholdAlwaysAllows documents the threshold semantics
// pinned in leaf 01 ("0 = treat every answer as below-threshold (escalate)")
// is NOT the gate's reading when overridden: the hook compares confidence
// >= threshold, so a configured 0 threshold means every confident argmax
// answer (Confidence 1.0 argmax floor) passes. The shipped default stays 0 in
// config (opt-in via decision_gate_enabled) and operators raising
// decision_escalation_threshold get escalation behavior.
func TestDecisionGate_ZeroThresholdAlwaysAllows(t *testing.T) {
	stub := &gateStubClient{result: &llm.DecisionResult{Answer: "yes", Confidence: 1.0}}
	hook := NewDecisionGateHook(gateTestClient(stub), 0.0, nil, nil)
	got := hook.BeforeToolCall(context.Background(), gateToolCall("file_write", `{"path":"/a","content":"x"}`))
	if got.Block {
		t.Fatalf("argmax Confidence 1.0 blocked at threshold 0: %+v", got)
	}
}

// TestDecisionGate_QuestionShape: the noul question carries the tool name +
// one-line args summary in the State, the fixed prompt, yes/no options, and
// the configured threshold.
func TestDecisionGate_QuestionShape(t *testing.T) {
	stub := &gateStubClient{result: &llm.DecisionResult{Answer: "yes", Confidence: 1.0}}
	hook := NewDecisionGateHook(gateTestClient(stub), 0.8, nil, nil)
	hook.SetEnabled(true)

	hook.BeforeToolCall(context.Background(), gateToolCall("shell", `{"command":"rm -rf /tmp/x"}`))

	q := stub.lastQuestion
	if q == nil {
		t.Fatal("no question rendered")
	}
	if q.Type != "noul" {
		t.Fatalf("question Type = %q, want %q", q.Type, "noul")
	}
	if q.Prompt != "May this action run?" {
		t.Fatalf("question Prompt = %q", q.Prompt)
	}
	if q.Threshold != 0.8 {
		t.Fatalf("question Threshold = %v, want 0.8", q.Threshold)
	}
	combined := q.State + "\n" + q.Prompt
	for _, want := range []string{"shell", "rm -rf /tmp/x"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("rendered state missing %q; got:\n%s", want, combined)
		}
	}
}
