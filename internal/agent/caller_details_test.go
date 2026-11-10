package agent

// Test gate for plan 20261110-caller-authz, leaf 02-enforcement/00T (§5-§6).
//
// §5 pins the executor details injection: when callerid.From(ctx) reports a
// stamped caller, Executor.checkPermission's details map gains EXACTLY the
// two presence-only keys caller_approved / caller_known (strconv.FormatBool
// values). When absent, the keys are NOT added — a non-voice turn's details
// map length must not change (byte-identical gate input, §2 twin).
//
// Identity NEVER flows further: Number/Name must not appear in details at
// all (display-surface redaction rule — details reach prompts/logs).
//
// §6 pins deny propagation posture: the pre-exec deny's Reason string
// reaches ExecutionResult.Error as "permission denied: <Reason>" — the
// tool-result the model sees. Covered HERE in the agent package (Execute ->
// results[].Error), matching the existing deny-propagation posture
// (executor_dep_test.go's "permission denied" assertions); the employee
// package covers the caller-specific Reason CONSTRUCTION in
// caller_gate_test.go §1. Rationale for the choice: internal/employee
// imports internal/agent, so agent cannot import employee to reuse its
// gate; the frozen deny text is mirrored below with a cross-reference.
//
// IMPL SHAPE (pinned by the gate, same contract as employee caller_gate_test):
// Execute threads ctx into checkPermission; checkPermission calls
// callerid.From(ctx) and injects the keys after the existing agent_id
// injection (~executor.go:1981). No new exported symbols needed here — this
// file is runtime-RED until the injection lands.

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/callerid"
	"github.com/caimlas/meept/pkg/security"
)

// Mirrors the frozen constants in internal/employee/caller_gate_test.go
// (plan 20261110-caller-authz master.md §Interface Contracts). If either
// drifts, both packages' gates must fail together.
const (
	callerDeniedTextForAgent = "This caller is not approved for that action. Take a message instead."
	callerDenyReasonForAgent = "caller_not_approved: " + callerDeniedTextForAgent
)

// capturePreExec records every Check invocation's details map and returns a
// configurable decision.
type capturePreExec struct {
	mu      sync.Mutex
	calls   []map[string]string
	allowed bool
	reason  string
}

func (c *capturePreExec) Check(action, toolName string, details map[string]string) security.PreExecDecision {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Copy — the checker runs before later stages may mutate.
	cp := make(map[string]string, len(details))
	for k, v := range details {
		cp[k] = v
	}
	c.calls = append(c.calls, cp)
	return security.PreExecDecision{Allowed: c.allowed, Reason: c.reason}
}

func (c *capturePreExec) last() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.calls) == 0 {
		return nil
	}
	return c.calls[len(c.calls)-1]
}

// newCallerTestExecutor builds an executor with an allowed file_read mock
// plus a registered pre-exec capture for agent id "emp-caller".
func newCallerTestExecutor(t *testing.T, cap *capturePreExec) *Executor {
	t.Helper()
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_read", "read", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"content": "data"}, nil
	}))
	pc := security.NewPermissionChecker(security.Config{})
	pc.SetPreExecChecker("emp-caller", cap)
	return NewExecutor(registry, pc, WithExecutorAgentID("emp-caller"))
}

// ---------------------------------------------------------------------------
// §5 — details injection, presence-only.
// ---------------------------------------------------------------------------

func TestCheckPermission_CallerDetailsPresenceOnly(t *testing.T) {
	cases := []struct {
		name     string
		caller   *callerid.Info
		wantKeys bool
	}{
		{
			name:     "approved caller -> two keys",
			caller:   &callerid.Info{Number: "+15551230000", Name: "Ada", Approved: true, Known: true},
			wantKeys: true,
		},
		{
			name:     "unapproved known caller -> two keys",
			caller:   &callerid.Info{Number: "+15551230000", Name: "Ada", Approved: false, Known: true},
			wantKeys: true,
		},
		{
			name:     "zero-Info unresolved caller -> two keys",
			caller:   &callerid.Info{},
			wantKeys: true,
		},
		{
			name:     "no caller stamped -> keys absent",
			caller:   nil,
			wantKeys: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cap := &capturePreExec{allowed: true}
			e := newCallerTestExecutor(t, cap)

			ctx := context.Background()
			if tc.caller != nil {
				ctx = callerid.With(ctx, *tc.caller)
			}
			res := e.Execute(ctx, makeToolCall("c1", "file_read", `{"file":"x"}`))
			if res == nil {
				t.Fatal("nil result")
			}
			details := cap.last()
			if details == nil {
				t.Fatal("pre-exec checker never invoked — caller keys cannot be asserted")
			}

			_, hasApproved := details["caller_approved"]
			_, hasKnown := details["caller_known"]
			if hasApproved != tc.wantKeys || hasKnown != tc.wantKeys {
				t.Fatalf("caller_approved=%v caller_known=%v present, want both present=%v (details=%v)",
					hasApproved, hasKnown, tc.wantKeys, details)
			}
			if tc.wantKeys {
				// Both keys together, never one alone.
				if hasApproved != hasKnown {
					t.Errorf("keys must be injected as a pair")
				}
				// Values are presence-only booleans — the approved bit must
				// round-trip so step 1c can read it.
				wantApproved := strings.EqualFold(details["caller_approved"], "true")
				if wantApproved != tc.caller.Approved {
					t.Errorf("caller_approved = %q, want strconv.FormatBool(%v)", details["caller_approved"], tc.caller.Approved)
				}
				wantKnown := strings.EqualFold(details["caller_known"], "true")
				if wantKnown != tc.caller.Known {
					t.Errorf("caller_known = %q, want strconv.FormatBool(%v)", details["caller_known"], tc.caller.Known)
				}
				// Identity leakage guard: number/name must NEVER reach
				// details (prompts + logs see this map).
				for _, v := range details {
					if strings.Contains(v, "+15551230000") || strings.Contains(v, "Ada") {
						t.Errorf("caller identity leaked into details: %q", v)
					}
				}
			}
		})
	}
}

// TestCheckPermission_NoCallerMapLengthGolden pins the map-length contract:
// without a caller stamp, details has exactly args keys + tool_name +
// agent_id — the caller keys add nothing.
func TestCheckPermission_NoCallerMapLengthGolden(t *testing.T) {
	cap := &capturePreExec{allowed: true}
	e := newCallerTestExecutor(t, cap)

	res := e.Execute(context.Background(), makeToolCall("c1", "file_read", `{"file":"x","mode":"r"}`))
	if res == nil {
		t.Fatal("nil result")
	}
	details := cap.last()
	if details == nil {
		t.Fatal("pre-exec checker never invoked")
	}
	// 2 arg keys + tool_name + agent_id = 4. Any caller key injection on a
	// no-caller turn would grow this.
	if len(details) != 4 {
		t.Errorf("details len = %d, want 4 (no caller -> no extra keys): %v", len(details), details)
	}

	// With the same call + stamped caller, exactly two more keys.
	cap2 := &capturePreExec{allowed: true}
	e2 := newCallerTestExecutor(t, cap2)
	ctx := callerid.With(context.Background(), callerid.Info{Number: "+1", Approved: true, Known: true})
	e2.Execute(ctx, makeToolCall("c2", "file_read", `{"file":"x","mode":"r"}`))
	d2 := cap2.last()
	if d2 == nil {
		t.Fatal("pre-exec checker never invoked (caller case)")
	}
	if len(d2) != len(details)+2 {
		t.Errorf("details len with caller = %d, want %d (base %d + 2 caller keys): %v",
			len(d2), len(details)+2, len(details), d2)
	}
}

// ---------------------------------------------------------------------------
// §6 — deny propagation: the caller deny reason+text reach the tool-result
// error the model sees.
// ---------------------------------------------------------------------------

func TestExecute_CallerDenyReachesModelError(t *testing.T) {
	cap := &capturePreExec{allowed: false, reason: callerDenyReasonForAgent}
	e := newCallerTestExecutor(t, cap)

	ctx := callerid.With(context.Background(), callerid.Info{Number: "+15550000000"})
	res := e.Execute(ctx, makeToolCall("c1", "file_read", `{"file":"x"}`))
	if res == nil {
		t.Fatal("nil result")
	}
	if res.Success {
		t.Fatalf("denied call reported success: %+v", res)
	}
	// Existing deny-propagation posture (executor_dep_test.go): the
	// ExecutionResult.Error carries "permission denied: <reason>".
	if !strings.Contains(res.Error, "permission denied") {
		t.Errorf("Error = %q, want 'permission denied' prefix", res.Error)
	}
	if !strings.Contains(res.Error, "caller_not_approved") {
		t.Errorf("Error = %q, want reason token caller_not_approved", res.Error)
	}
	if !strings.Contains(res.Error, callerDeniedTextForAgent) {
		t.Errorf("Error = %q, want pinned model-facing text verbatim: %q", res.Error, callerDeniedTextForAgent)
	}
	// The denial must not leak the caller number into the model-visible
	// error either.
	if strings.Contains(res.Error, "+15550000000") {
		t.Errorf("Error leaked caller number: %q", res.Error)
	}
}
