package agent

import (
	"path/filepath"
	"testing"
)

func TestProposalQueue_AppendAndList(t *testing.T) {
	tmp := t.TempDir()
	q := newProposalQueue(filepath.Join(tmp, "improvements.md"))
	p1 := ReflectionProposal{
		Type:          "skill_create",
		Target:        ".meept/skills/x/SKILL.md",
		Change:        "content",
		Justification: "because",
		Confidence:    0.8,
		Source:        "turn:s1",
	}
	if err := q.Append(p1); err != nil {
		t.Fatalf("Append: %v", err)
	}
	pending, err := q.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("got %d pending; want 1", len(pending))
	}
	if pending[0].Target != ".meept/skills/x/SKILL.md" {
		t.Errorf("target = %q", pending[0].Target)
	}
	if pending[0].ID == "" {
		t.Errorf("ID was not assigned")
	}
	if pending[0].Status != "pending" {
		t.Errorf("status = %q; want pending", pending[0].Status)
	}
}

func TestProposalQueue_MarkApplied(t *testing.T) {
	tmp := t.TempDir()
	q := newProposalQueue(filepath.Join(tmp, "improvements.md"))
	p := ReflectionProposal{Type: "agent_prompt", Target: "x", Change: "y", Confidence: 0.7, Source: "test"}
	if err := q.Append(p); err != nil {
		t.Fatalf("Append: %v", err)
	}
	pending, _ := q.ListPending()
	if len(pending) != 1 {
		t.Fatalf("pre: pending = %d; want 1", len(pending))
	}
	if err := q.MarkApplied(pending[0].ID); err != nil {
		t.Fatalf("MarkApplied: %v", err)
	}
	pending2, _ := q.ListPending()
	if len(pending2) != 0 {
		t.Errorf("after MarkApplied, pending = %d; want 0", len(pending2))
	}
}

func TestProposalQueue_MarkSkipped(t *testing.T) {
	tmp := t.TempDir()
	q := newProposalQueue(filepath.Join(tmp, "improvements.md"))
	p := ReflectionProposal{Type: "agent_prompt", Target: "x", Change: "y", Confidence: 0.7, Source: "test"}
	q.Append(p)
	pending, _ := q.ListPending()
	if err := q.MarkSkipped(pending[0].ID); err != nil {
		t.Fatalf("MarkSkipped: %v", err)
	}
	pending2, _ := q.ListPending()
	if len(pending2) != 0 {
		t.Errorf("after MarkSkipped, pending = %d; want 0", len(pending2))
	}
}

func TestProposalQueue_Authorization(t *testing.T) {
	cases := []struct {
		target string
		want   bool // true = always propose-only
	}{
		{"config/agents/coder/AGENT.md", true},
		{"CLAUDE.md", true},
		{"config/prompts/tools/bash.md", true},
		{".meept/skills/auto/foo/SKILL.md", false}, // auto-writable
		{".meept/skills/x/SKILL.md", false},        // propose-only but not "always"
	}
	for _, c := range cases {
		got := isAlwaysProposeOnly(c.target)
		if got != c.want {
			t.Errorf("isAlwaysProposeOnly(%q) = %v; want %v", c.target, got, c.want)
		}
	}
}

func TestProposalQueue_EmptyListPending(t *testing.T) {
	tmp := t.TempDir()
	q := newProposalQueue(filepath.Join(tmp, "nodir", "improvements.md"))
	pending, err := q.ListPending()
	if err != nil {
		t.Fatalf("ListPending on missing file: %v", err)
	}
	if pending != nil {
		t.Errorf("got %v; want nil", pending)
	}
}
