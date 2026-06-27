package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent"
)

// newTestCommandHandler builds a CommandHandler with a temp-backed proposal
// queue path. Returns the handler and the queue path so tests can seed
// proposals directly via agent.NewExternalProposalQueue.
func newTestCommandHandler(t *testing.T) (*CommandHandler, string) {
	t.Helper()
	queuePath := filepath.Join(t.TempDir(), "improvements.md")
	h := &CommandHandler{}
	return h, queuePath
}

// seedProposals writes the given proposals to the queue path and returns the
// queue for further test use.
func seedProposals(t *testing.T, queuePath string, proposals ...agent.ReflectionProposal) *agent.ProposalQueueExternal {
	t.Helper()
	q := agent.NewExternalProposalQueue(queuePath)
	for _, p := range proposals {
		if err := q.Append(p); err != nil {
			t.Fatalf("Append(%s): %v", p.ID, err)
		}
	}
	return q
}

func TestImprovementsList_EmptyQueue(t *testing.T) {
	h, queuePath := newTestCommandHandler(t)
	queue := agent.NewExternalProposalQueue(queuePath)
	res := h.improvementsList(queue)
	if res.IsError {
		t.Errorf("unexpected error: %s", res.Output)
	}
	if res.Output != "no pending proposals" {
		t.Errorf("Output = %q; want %q", res.Output, "no pending proposals")
	}
}

func TestImprovementsList_ShowsPendingProposals(t *testing.T) {
	h, queuePath := newTestCommandHandler(t)
	seedProposals(t, queuePath,
		agent.ReflectionProposal{
			ID:            "abc123",
			Type:          "project_instruction",
			Target:        "CLAUDE.md",
			Justification: "always run gofmt",
			Confidence:    0.9,
			Source:        "test",
		},
		agent.ReflectionProposal{
			ID:            "def456",
			Type:          "skill",
			Target:        ".meept/skills/auto/x.md",
			Justification: "extract common pattern",
			Confidence:    0.7,
			Source:        "reflection",
		},
	)
	queue := agent.NewExternalProposalQueue(queuePath)
	res := h.improvementsList(queue)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "abc123") || !strings.Contains(res.Output, "def456") {
		t.Errorf("expected both ids in output; got: %s", res.Output)
	}
	if !strings.Contains(res.Output, "CLAUDE.md") {
		t.Errorf("expected target in output; got: %s", res.Output)
	}
}

func TestImprovementsApply_WritesTargetAndMarksApplied(t *testing.T) {
	h, queuePath := newTestCommandHandler(t)

	// Write an existing target file so the apply overwrites it.
	targetPath := filepath.Join(t.TempDir(), "subdir", "auto-skill.md")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("# old content\n"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	seedProposals(t, queuePath, agent.ReflectionProposal{
		ID:            "abc123",
		Type:          "skill",
		Target:        targetPath,
		Change:        "# new content via /implement-improvements apply\n",
		Justification: "auto-extracted pattern",
		Confidence:    0.8,
		Source:        "reflection",
	})
	queue := agent.NewExternalProposalQueue(queuePath)
	res := h.improvementsApply(queue, "abc123")
	if res.IsError {
		t.Fatalf("apply failed: %s", res.Output)
	}
	if !strings.Contains(res.Output, "applied:") {
		t.Errorf("expected applied marker; got: %s", res.Output)
	}

	// Verify the file was overwritten with the proposed content.
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !strings.Contains(string(got), "new content via /implement-improvements") {
		t.Errorf("target file not overwritten; got: %s", string(got))
	}

	// Verify the proposal was marked applied (no longer pending).
	pending, err := queue.ListPending()
	if err != nil {
		t.Fatalf("ListPending after apply: %v", err)
	}
	for _, p := range pending {
		if p.ID == "abc123" {
			t.Errorf("proposal abc123 still pending after apply")
		}
	}
}

func TestImprovementsApply_ProposeOnlyTargetDoesNotWrite(t *testing.T) {
	h, queuePath := newTestCommandHandler(t)

	// CLAUDE.md is always propose-only per agent.IsAlwaysProposeOnly.
	seedProposals(t, queuePath, agent.ReflectionProposal{
		ID:            "propcl",
		Type:          "project_instruction",
		Target:        "CLAUDE.md",
		Change:        "should NOT be written automatically",
		Justification: "rule",
		Confidence:    0.9,
		Source:        "test",
	})
	queue := agent.NewExternalProposalQueue(queuePath)
	res := h.improvementsApply(queue, "propcl")
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "propose-only") {
		t.Errorf("expected propose-only warning; got: %s", res.Output)
	}
	if !strings.Contains(res.Output, "should NOT be written automatically") {
		t.Errorf("expected proposed change echoed for manual review; got: %s", res.Output)
	}
	// File must NOT exist (we never wrote CLAUDE.md).
	if _, err := os.Stat("CLAUDE.md"); !os.IsNotExist(err) {
		t.Errorf("CLAUDE.md was written; expected propose-only echo only")
	}
}

func TestImprovementsApply_UnknownID(t *testing.T) {
	h, queuePath := newTestCommandHandler(t)
	seedProposals(t, queuePath, agent.ReflectionProposal{
		ID: "abc123", Target: "x.md", Change: "x",
	})
	queue := agent.NewExternalProposalQueue(queuePath)
	res := h.improvementsApply(queue, "nonexistent-id")
	if !res.IsError {
		t.Errorf("expected error for unknown id; got: %s", res.Output)
	}
	if !strings.Contains(res.Output, "not found") {
		t.Errorf("error should mention 'not found'; got: %s", res.Output)
	}
}

func TestImprovementsSkip_MarksSkipped(t *testing.T) {
	h, queuePath := newTestCommandHandler(t)
	seedProposals(t, queuePath, agent.ReflectionProposal{
		ID:     "skip1",
		Target: "x.md",
		Change: "x",
	})
	queue := agent.NewExternalProposalQueue(queuePath)
	res := h.improvementsSkip(queue, "skip1")
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "skipped: skip1") {
		t.Errorf("expected 'skipped: skip1'; got: %s", res.Output)
	}
	pending, _ := queue.ListPending()
	for _, p := range pending {
		if p.ID == "skip1" {
			t.Errorf("proposal skip1 still pending after skip")
		}
	}
}

func TestExecuteImplementImprovements_DefaultLists(t *testing.T) {
	// Verifies dispatch with no args routes to "list".
	// We can't easily intercept defaultRememberQueuePath, so we test the
	// dispatch logic indirectly via the subcommand routing.
	h := &CommandHandler{}
	res := h.executeImplementImprovements([]string{})
	if res.IsError {
		t.Errorf("default list should not error; got: %s", res.Output)
	}
	// Will say "no pending proposals" since the .meept/improvements.md file
	// either doesn't exist or has no pending entries in the test CWD.
	if !strings.Contains(res.Output, "proposals") {
		t.Errorf("expected proposals mention in output; got: %s", res.Output)
	}
}

func TestExecuteImplementImprovements_UnknownSubcommand(t *testing.T) {
	h := &CommandHandler{}
	res := h.executeImplementImprovements([]string{"frobnicate"})
	if !res.IsError {
		t.Errorf("expected error for unknown subcommand")
	}
	if !strings.Contains(res.Output, "unknown subcommand") {
		t.Errorf("error should mention unknown subcommand; got: %s", res.Output)
	}
}

func TestExecuteImplementImprovements_ApplyMissingID(t *testing.T) {
	h := &CommandHandler{}
	res := h.executeImplementImprovements([]string{"apply"})
	if !res.IsError {
		t.Errorf("expected error for apply with no id")
	}
	if !strings.Contains(res.Output, "usage:") {
		t.Errorf("expected usage hint; got: %s", res.Output)
	}
}

func TestExecuteImplementImprovements_SkipMissingID(t *testing.T) {
	h := &CommandHandler{}
	res := h.executeImplementImprovements([]string{"skip"})
	if !res.IsError {
		t.Errorf("expected error for skip with no id")
	}
	if !strings.Contains(res.Output, "usage:") {
		t.Errorf("expected usage hint; got: %s", res.Output)
	}
}
