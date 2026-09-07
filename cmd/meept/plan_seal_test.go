package main

// CLI wiring smoke for the brainstorm draft subcommands (plan-compiler
// leaf 04): the commands exist on the plans tree with the expected Use
// strings and Args validators — no daemon dialing here.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func findSealSub(t *testing.T, cmd *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, sub := range cmd.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	t.Fatalf("plans command missing %q subcommand", name)
	return nil
}

func TestPlansDraftSubcommandsRegistered(t *testing.T) {
	cmd := newPlansCmd()
	want := map[string]string{
		"draft":      "draft <task-id>",
		"show-draft": "show-draft <task-id>",
		"edit-draft": "edit-draft <task-id> [file|-]",
		"seal":       "seal <task-id>",
	}
	for name, use := range want {
		sub := findSealSub(t, cmd, name)
		if sub.Use != use {
			t.Errorf("plans %s Use = %q, want %q", name, sub.Use, use)
		}
	}
}

func TestPlansDraftSubcommandsArgEnforcement(t *testing.T) {
	cmd := newPlansCmd()

	// Wrong arg counts must fail Args validation before any RunE dials
	// the daemon.
	enforcing := []struct {
		name string
		args []string
	}{
		{"draft", nil},
		{"draft", []string{"a", "b"}},
		{"show-draft", nil},
		{"seal", nil},
		{"seal", []string{"a", "b"}},
		{"edit-draft", []string{"a", "b", "c"}},
	}
	for _, c := range enforcing {
		sub := findSealSub(t, cmd, c.name)
		if err := sub.Args(sub, c.args); err == nil {
			t.Errorf("plans %s %v: expected Args error, got nil", c.name, c.args)
		}
	}

	// Correct counts pass Args validation.
	accepting := []struct {
		name string
		args []string
	}{
		{"draft", []string{"task-1"}},
		{"show-draft", []string{"task-1"}},
		{"seal", []string{"task-1"}},
		{"edit-draft", []string{"task-1"}},
		{"edit-draft", []string{"task-1", "-"}},
	}
	for _, c := range accepting {
		sub := findSealSub(t, cmd, c.name)
		if err := sub.Args(sub, c.args); err != nil {
			t.Errorf("plans %s %v: unexpected Args error: %v", c.name, c.args, err)
		}
	}
}

// TestPlansSealHelpListsProblemSemantics pins the user-facing contract in
// the seal command's help: compile problems keep the draft.
func TestPlansSealHelpListsProblemSemantics(t *testing.T) {
	cmd := newPlansCmd()
	seal := findSealSub(t, cmd, "seal")
	var buf bytes.Buffer
	seal.SetOut(&buf)
	if err := seal.Help(); err != nil {
		t.Fatalf("seal Help: %v", err)
	}
	if !strings.Contains(buf.String(), "problems") {
		t.Errorf("seal help should mention compile problems; got:\n%s", buf.String())
	}
}
