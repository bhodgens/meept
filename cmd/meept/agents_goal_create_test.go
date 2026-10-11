package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestAgentsGoalCreateSubcommandRegistered verifies the goal-create
// subcommand is on the agents tree (leaf 01 Task 6).
func TestAgentsGoalCreateSubcommandRegistered(t *testing.T) {
	cmd := newAgentsCmd()
	var found *cobra.Command
	for _, c := range cmd.Commands() {
		if c.Name() == "goal-create" {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatal("agents command missing \"goal-create\" subcommand")
	}
	if found.Use != "goal-create <employee-id>" {
		t.Errorf("goal-create Use = %q, want %q", found.Use, "goal-create <employee-id>")
	}
	if found.RunE == nil {
		t.Error("goal-create must define RunE")
	}
}
