package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/agent"
)

func TestReflectionService_ListPending_Empty(t *testing.T) {
	tmp := t.TempDir()
	svc := NewReflectionService(filepath.Join(tmp, "improvements.md"))

	pending, err := svc.ListPending()
	if err != nil {
		t.Fatalf("ListPending returned error: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending proposals, got %d", len(pending))
	}
}

func TestReflectionService_ListPending(t *testing.T) {
	tmp := t.TempDir()
	queuePath := filepath.Join(tmp, "improvements.md")
	queue := agent.NewExternalProposalQueue(queuePath)

	proposal := agent.ReflectionProposal{
		Type:          "skill_create",
		Target:        ".meept/skills/test/SKILL.md",
		Change:        "add a new skill",
		Justification: "improves coverage",
		Confidence:    0.85,
		Source:        "turn:abc123",
	}
	if err := queue.Append(proposal); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}

	svc := NewReflectionService(queuePath)
	pending, err := svc.ListPending()
	if err != nil {
		t.Fatalf("ListPending returned error: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending proposal, got %d", len(pending))
	}

	got := pending[0]
	if got.Type != "skill_create" {
		t.Errorf("Type = %q, want %q", got.Type, "skill_create")
	}
	if got.Target != ".meept/skills/test/SKILL.md" {
		t.Errorf("Target = %q, want %q", got.Target, ".meept/skills/test/SKILL.md")
	}
	if got.Confidence != 0.85 {
		t.Errorf("Confidence = %f, want 0.85", got.Confidence)
	}
	if got.Source != "turn:abc123" {
		t.Errorf("Source = %q, want %q", got.Source, "turn:abc123")
	}
	if got.ID == "" {
		t.Error("expected non-empty ID")
	}
	if got.Status != "pending" {
		t.Errorf("Status = %q, want %q", got.Status, "pending")
	}
}

func TestReflectionService_Apply(t *testing.T) {
	tmp := t.TempDir()
	queuePath := filepath.Join(tmp, "improvements.md")
	queue := agent.NewExternalProposalQueue(queuePath)

	proposal := agent.ReflectionProposal{
		Type:   "agent_prompt",
		Target: "config/agents/coder/AGENT.md",
		Change: "update system prompt",
	}
	if err := queue.Append(proposal); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}

	svc := NewReflectionService(queuePath)
	pending, err := svc.ListPending()
	if err != nil {
		t.Fatalf("ListPending returned error: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending proposal before Apply, got %d", len(pending))
	}

	id := pending[0].ID
	if err := svc.Apply(id); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	pendingAfter, err := svc.ListPending()
	if err != nil {
		t.Fatalf("ListPending after Apply returned error: %v", err)
	}
	if len(pendingAfter) != 0 {
		t.Errorf("expected 0 pending proposals after Apply, got %d", len(pendingAfter))
	}
}

func TestReflectionService_Skip(t *testing.T) {
	tmp := t.TempDir()
	queuePath := filepath.Join(tmp, "improvements.md")
	queue := agent.NewExternalProposalQueue(queuePath)

	proposal := agent.ReflectionProposal{
		Type:   "project_instruction",
		Target: "CLAUDE.md",
		Change: "add coding rule",
	}
	if err := queue.Append(proposal); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}

	svc := NewReflectionService(queuePath)
	pending, err := svc.ListPending()
	if err != nil {
		t.Fatalf("ListPending returned error: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending proposal before Skip, got %d", len(pending))
	}

	id := pending[0].ID
	if err := svc.Skip(id); err != nil {
		t.Fatalf("Skip returned error: %v", err)
	}

	pendingAfter, err := svc.ListPending()
	if err != nil {
		t.Fatalf("ListPending after Skip returned error: %v", err)
	}
	if len(pendingAfter) != 0 {
		t.Errorf("expected 0 pending proposals after Skip, got %d", len(pendingAfter))
	}
}

func TestReflectionService_Remember(t *testing.T) {
	tmp := t.TempDir()
	queuePath := filepath.Join(tmp, "improvements.md")
	svc := NewReflectionService(queuePath)

	if err := svc.Remember("CLAUDE.md", "new rule", "because"); err != nil {
		t.Fatalf("Remember returned error: %v", err)
	}

	pending, err := svc.ListPending()
	if err != nil {
		t.Fatalf("ListPending returned error: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending proposal, got %d", len(pending))
	}

	got := pending[0]
	if got.Type != "project_instruction" {
		t.Errorf("Type = %q, want %q", got.Type, "project_instruction")
	}
	if got.Target != "CLAUDE.md" {
		t.Errorf("Target = %q, want %q", got.Target, "CLAUDE.md")
	}
	if got.Confidence != 0.9 {
		t.Errorf("Confidence = %f, want 0.9", got.Confidence)
	}
	if got.Source != "manual:http" {
		t.Errorf("Source = %q, want %q", got.Source, "manual:http")
	}
	if got.Change != "new rule" {
		t.Errorf("Change = %q, want %q", got.Change, "new rule")
	}
	if got.Justification != "because" {
		t.Errorf("Justification = %q, want %q", got.Justification, "because")
	}
}

func TestReflectionService_Remember_InferType(t *testing.T) {
	cases := []struct {
		target   string
		wantType string
	}{
		{"x/SKILL.md", "skill_create"},
		{".meept/skills/y/SKILL.md", "skill_create"},
		{"config/agents/coder/AGENT.md", "agent_prompt"},
		{"CLAUDE.md", "project_instruction"},
		{"config/prompts/tools/bash.md", "prompt_component"},
		{"random/path.md", "prompt_component"},
	}

	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			tmp := t.TempDir()
			queuePath := filepath.Join(tmp, "improvements.md")
			svc := NewReflectionService(queuePath)

			if err := svc.Remember(tc.target, "change", "justification"); err != nil {
				t.Fatalf("Remember returned error: %v", err)
			}

			pending, err := svc.ListPending()
			if err != nil {
				t.Fatalf("ListPending returned error: %v", err)
			}
			if len(pending) != 1 {
				t.Fatalf("expected 1 pending proposal, got %d", len(pending))
			}
			if pending[0].Type != tc.wantType {
				t.Errorf("Type = %q, want %q", pending[0].Type, tc.wantType)
			}
			if pending[0].Target != tc.target {
				t.Errorf("Target = %q, want %q", pending[0].Target, tc.target)
			}
		})
	}
}

func TestReflectionService_DefaultQueuePath(t *testing.T) {
	// NewReflectionService("") should default to ".meept/improvements.md".
	// Exercise Append + ListPending in a temp CWD to avoid polluting the
	// real project directory.
	tmp := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWd)
	}()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	svc := NewReflectionService("")
	if err := svc.Remember("CLAUDE.md", "test rule", "testing default path"); err != nil {
		t.Fatalf("Remember returned error: %v", err)
	}

	// The file should have been created at the default path relative to CWD.
	if _, err := os.Stat(".meept/improvements.md"); err != nil {
		t.Fatalf("expected default queue file at .meept/improvements.md: %v", err)
	}

	pending, err := svc.ListPending()
	if err != nil {
		t.Fatalf("ListPending returned error: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("expected 1 pending proposal, got %d", len(pending))
	}
}
