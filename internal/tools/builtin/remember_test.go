package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

func TestRememberTool_Execute_QueuesProposal(t *testing.T) {
	queuePath := filepath.Join(t.TempDir(), "improvements.md")
	tool := NewRememberTool(queuePath)

	args := map[string]any{
		"target":        ".meept/skills/foo/SKILL.md",
		"change":        "# my skill\nbody content",
		"justification": "useful pattern observed during code review",
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	tr, ok := result.(*tools.ToolResult)
	if !ok {
		t.Fatalf("result type %T is not *tools.ToolResult", result)
	}
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}

	rr, ok := tr.Result.(RememberResult)
	if !ok {
		t.Fatalf("expected Result to be RememberResult, got %T", tr.Result)
	}
	if rr.ID == "" {
		t.Error("expected non-empty proposal ID")
	}
	if !rr.Queued {
		t.Error("expected Queued=true")
	}
	if rr.Target != ".meept/skills/foo/SKILL.md" {
		t.Errorf("target mismatch: %q", rr.Target)
	}

	// Verify the queue file was actually written with the expected fields.
	data, err := os.ReadFile(queuePath)
	if err != nil {
		t.Fatalf("failed to read queue file: %v", err)
	}
	body := string(data)
	for _, want := range []string{
		".meept/skills/foo/SKILL.md",
		"manual:/remember",
		"useful pattern observed during code review",
		"skill_create",
		rr.ID, // written into the header by Append
	} {
		if !strings.Contains(body, want) {
			t.Errorf("queue file missing %q\n--- file ---\n%s", want, body)
		}
	}
}

func TestRememberTool_Execute_MissingFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args map[string]any
	}{
		{"empty target", map[string]any{"target": "", "change": "x", "justification": "y"}},
		{"missing target", map[string]any{"change": "x", "justification": "y"}},
		{"empty change", map[string]any{"target": "x", "change": "", "justification": "y"}},
		{"missing change", map[string]any{"target": "x"}},
		{"all empty", map[string]any{}},
		{"nil args", nil},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			queuePath := filepath.Join(t.TempDir(), "improvements.md")
			tool := NewRememberTool(queuePath)

			result, err := tool.Execute(context.Background(), c.args)
			if err != nil {
				t.Fatalf("Execute returned unexpected error: %v", err)
			}
			tr, ok := result.(*tools.ToolResult)
			if !ok {
				t.Fatalf("result type %T is not *tools.ToolResult", result)
			}
			if tr.Success {
				t.Errorf("expected error result for %s, got success: %+v", c.name, tr.Result)
			}
			if !strings.Contains(tr.Error, "required") {
				t.Errorf("error message should mention 'required', got: %q", tr.Error)
			}

			// Queue file should NOT exist when validation failed.
			if _, err := os.Stat(queuePath); err == nil {
				t.Errorf("queue file was created despite validation failure")
			}
		})
	}
}

func TestRememberTool_DefaultsJustification(t *testing.T) {
	queuePath := filepath.Join(t.TempDir(), "improvements.md")
	tool := NewRememberTool(queuePath)

	args := map[string]any{
		"target": "CLAUDE.md",
		"change": "always run gofmt before committing",
		// justification intentionally omitted
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr := result.(*tools.ToolResult)
	if !tr.Success {
		t.Fatalf("unexpected error: %s", tr.Error)
	}

	data, _ := os.ReadFile(queuePath)
	if !strings.Contains(string(data), "(no justification provided)") {
		t.Errorf("expected default justification text in queue, got:\n%s", string(data))
	}
}

func TestRememberTool_TerminateHint(t *testing.T) {
	tool := NewRememberTool(filepath.Join(t.TempDir(), "improvements.md"))
	if !tool.TerminateHint(nil) {
		t.Error("TerminateHint should return true for remember tool")
	}
}

func TestRememberTool_Category(t *testing.T) {
	tool := NewRememberTool(filepath.Join(t.TempDir(), "improvements.md"))
	if got := tool.Category(); got != "agent" {
		t.Errorf("Category = %q; want %q", got, "agent")
	}
}

func TestInferProposalType(t *testing.T) {
	cases := []struct {
		target string
		want   string
	}{
		{".meept/skills/foo/SKILL.md", "skill_create"},
		{"skills/foo/SKILL.md", "skill_create"},
		{".meept/skills/bar/SKILL.md", "skill_create"},
		{"config/agents/coder/AGENT.md", "agent_prompt"},
		{"AGENT.md", "agent_prompt"},
		{"CLAUDE.md", "project_instruction"},
		{"config/prompts/system.md", "prompt_component"},
		{"config/prompts/anything.txt", "prompt_component"},
		{"docs/random.md", "prompt_component"},
		{"", "prompt_component"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.target, func(t *testing.T) {
			t.Parallel()
			got := inferProposalType(c.target)
			if got != c.want {
				t.Errorf("inferProposalType(%q) = %q; want %q", c.target, got, c.want)
			}
		})
	}
}

// TestRememberTool_InterfaceConformance verifies the compile-time interface
// assertions in remember.go actually hold at runtime (catches accidental
// signature drift introduced during refactors).
func TestRememberTool_InterfaceConformance(t *testing.T) {
	tool := NewRememberTool(filepath.Join(t.TempDir(), "improvements.md"))

	var _ tools.Tool = tool
	var _ tools.Categorizer = tool
	var _ tools.TerminatingTool = tool

	if tool.Name() != "remember" {
		t.Errorf("Name = %q; want %q", tool.Name(), "remember")
	}
}
