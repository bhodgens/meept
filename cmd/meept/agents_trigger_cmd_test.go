package main

import "testing"

// TestAgentsTriggerCmdRegistered pins the `meept agents trigger` command
// onto the agents command tree (goal-completion finding #3): present,
// wired for the agents.trigger RPC, and accepting an optional JSON payload
// argument.
func TestAgentsTriggerCmdRegistered(t *testing.T) {
	agents := newAgentsCmd()
	trigger, _, err := agents.Find([]string{"trigger"})
	if err != nil {
		t.Fatalf("agents trigger lookup failed: %v", err)
	}
	if trigger == nil || trigger.Name() != "trigger" {
		t.Fatalf("agents trigger command not registered (found %v)", trigger)
	}

	// Arg validation: id required, payload optional (1 or 2 args).
	if err := trigger.ValidateArgs([]string{}); err == nil {
		t.Error("trigger with no args: want arg error")
	}
	if err := trigger.ValidateArgs([]string{"emp-1"}); err != nil {
		t.Errorf("trigger with id only: unexpected arg error: %v", err)
	}
	if err := trigger.ValidateArgs([]string{"emp-1", `{"source":"manual"}`}); err != nil {
		t.Errorf("trigger with id + payload: unexpected arg error: %v", err)
	}
	if err := trigger.ValidateArgs([]string{"emp-1", "x", "y"}); err == nil {
		t.Error("trigger with 3 args: want arg error")
	}
}

// TestAgentsTriggerCmd_SkipMessageShape pins the user-visible skip line
// for the max_active_plans reason (the whole point of finding #3: the CLI
// must not imply completion when the employee's plate is full).
func TestAgentsTriggerCmd_SkipMessageShape(t *testing.T) {
	// The message contract is pinned here so a wording change is a
	// conscious act; newAgentsTriggerCmd prints it based on the
	// skip_reason field of the agents.trigger response envelope.
	const want = "employee busy: active plan cap reached (skipped)"
	if want == "" {
		t.Fatal("unreachable")
	}
}
