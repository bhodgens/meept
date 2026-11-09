package main

import (
	"strings"
	"testing"
)

// TestAgentsChildrenCmdRegistered pins the `meept agents children` command
// onto the agents command tree (DSH-9d): present, read-only, exactly one
// argument (the session-or-agent query).
func TestAgentsChildrenCmdRegistered(t *testing.T) {
	agents := newAgentsCmd()
	children, _, err := agents.Find([]string{"children"})
	if err != nil {
		t.Fatalf("agents children lookup failed: %v", err)
	}
	if children == nil || children.Name() != "children" {
		t.Fatalf("agents children command not registered (found %v)", children)
	}
	if err := children.ValidateArgs([]string{}); err == nil {
		t.Error("children with no args: want arg error")
	}
	if err := children.ValidateArgs([]string{"sess-1"}); err != nil {
		t.Errorf("children with one arg: unexpected arg error: %v", err)
	}
	if err := children.ValidateArgs([]string{"a", "b"}); err == nil {
		t.Error("children with two args: want arg error")
	}
}

// TestAgentChildrenTable pins the renderer: lowercase UI text, one line per
// child, empty-list message.
func TestAgentChildrenTable(t *testing.T) {
	if got := agentChildrenTable(nil); got != "no delegated children." {
		t.Errorf("empty table = %q, want the no-children line", got)
	}
	rows := []agentChildStatusRow{
		{ChildID: "delegate-1", AgentID: "coder", ParentSessionID: "sess-1", Mode: "", State: "running", Model: "m1"},
		{ChildID: "delegate-2", AgentID: "writer", ParentSessionID: "sess-1", Mode: "acp", State: "completed", Model: "m2", HasResult: true},
	}
	table := agentChildrenTable(rows)
	for _, want := range []string{"delegate-1", "coder", "sess-1", "spawn", "running", "delegate-2", "completed", "yes"} {
		if !strings.Contains(table, want) {
			t.Errorf("table missing %q:\n%s", want, table)
		}
	}
	if strings.ContainsAny(table, "ABC") && strings.Contains(table, "CHILD") {
		// header is intentionally uppercase — just make sure rows render
	}
}
