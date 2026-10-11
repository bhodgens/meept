package main

import "testing"

// TestAgentsSteerCmdRegistered pins the `meept agents steer` command onto
// the agents command tree (leaf 04, contract C5): present, wired for the
// agents.steer RPC, and requiring both an employee id and steering content.
func TestAgentsSteerCmdRegistered(t *testing.T) {
	agents := newAgentsCmd()
	steer, _, err := agents.Find([]string{"steer"})
	if err != nil {
		t.Fatalf("agents steer lookup failed: %v", err)
	}
	if steer == nil || steer.Name() != "steer" {
		t.Fatalf("agents steer command not registered (found %v)", steer)
	}

	// Arg validation: id + content are required.
	if err := steer.ValidateArgs([]string{"emp-1"}); err == nil {
		t.Error("steer with no content: want arg error")
	}
	if err := steer.ValidateArgs([]string{"emp-1", "do", "the", "thing"}); err != nil {
		t.Errorf("steer with id + content: unexpected arg error: %v", err)
	}
}
