package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

// TestProposalQueue_MarkStatus_MissingID verifies that MarkApplied/MarkSkipped
// return an error (rather than silently succeeding) when the given proposal ID
// is not found in the queue.
func TestProposalQueue_MarkStatus_MissingID(t *testing.T) {
	tmp := t.TempDir()
	q := newProposalQueue(filepath.Join(tmp, "improvements.md"))
	// Empty queue — no proposals appended.
	if err := q.MarkApplied("nonexistent-id"); err == nil {
		t.Errorf("MarkApplied on empty queue returned nil error; want error")
	}
	if err := q.MarkSkipped("nonexistent-id"); err == nil {
		t.Errorf("MarkSkipped on empty queue returned nil error; want error")
	}

	// Populate one proposal, then try to mark a different ID that doesn't exist.
	p := ReflectionProposal{Type: "agent_prompt", Target: "x", Change: "y", Confidence: 0.7, Source: "test"}
	if err := q.Append(p); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := q.MarkApplied("wrong-id"); err == nil {
		t.Errorf("MarkApplied with wrong ID returned nil error; want error")
	}
	// Verify the existing proposal is still pending (file was rewritten unchanged).
	pending, _ := q.ListPending()
	if len(pending) != 1 {
		t.Errorf("after wrong-ID mark, pending = %d; want 1 (unchanged)", len(pending))
	}
}

// TestProposalQueue_AppendMarkStatusConcurrency verifies that concurrent
// Append + MarkApplied calls do not lose data. Before the mutex fix,
// markStatus's os.WriteFile could truncate a proposal that Append just wrote
// (TOCTOU race). This test fires Append and MarkApplied concurrently and
// verifies no proposals are lost.
func TestProposalQueue_AppendMarkStatusConcurrency(t *testing.T) {
	tmp := t.TempDir()
	q := newProposalQueue(filepath.Join(tmp, "improvements.md"))

	// Pre-populate with 5 proposals so MarkApplied has something to mark.
	var preIDs []string
	for i := 0; i < 5; i++ {
		p := ReflectionProposal{
			Type: "skill_create", Target: "x",
			Change: "y", Justification: "z", Confidence: 0.5, Source: "pre",
		}
		if err := q.Append(p); err != nil {
			t.Fatalf("pre-Append %d: %v", i, err)
		}
		pending, _ := q.ListPending()
		preIDs = append(preIDs, pending[i].ID)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	// Half the goroutines append new proposals; half mark existing ones applied.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := ReflectionProposal{
				Type: "skill_create", Target: "x",
				Change: "y", Justification: "z", Confidence: 0.5, Source: "concurrent",
			}
			if err := q.Append(p); err != nil {
				errCh <- fmt.Errorf("Append: %w", err)
			}
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := preIDs[idx%len(preIDs)]
			// Ignore error — proposal may already be marked by another goroutine.
			_ = q.MarkApplied(id)
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}

	// Re-read all proposals (pending + non-pending) and count total.
	// We expect 5 pre-populated + 10 concurrent = 15 total. Data loss
	// from the race would result in fewer than 15.
	data, err := os.ReadFile(filepath.Join(tmp, "improvements.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	all := parseProposals(string(data))
	// Count all entries that appear in the file with a valid ID.
	totalWithID := 0
	for _, p := range all {
		if p.ID != "" {
			totalWithID++
		}
	}
	if totalWithID < 15 {
		t.Errorf("data loss detected: expected at least 15 proposals with IDs, got %d", totalWithID)
	}
}

// TestProposalQueue_ParseMultiLineChange verifies that parseProposals
// correctly reconstructs multi-line Proposed change content. The Append method
// indents continuation lines with 2 spaces; parseProposals must un-indent
// and rejoin with newlines.
func TestProposalQueue_ParseMultiLineChange(t *testing.T) {
	tmp := t.TempDir()
	q := newProposalQueue(filepath.Join(tmp, "improvements.md"))

	multiLineChange := "# my skill\nbody line 1\nbody line 2"
	p := ReflectionProposal{
		Type:          "skill_create",
		Target:        ".meept/skills/x/SKILL.md",
		Change:        multiLineChange,
		Justification: "because",
		Confidence:    0.8,
		Source:        "turn:s1",
	}
	if err := q.Append(p); err != nil {
		t.Fatalf("Append: %v", err)
	}
	pending, err := q.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("got %d pending; want 1", len(pending))
	}
	if pending[0].Change != multiLineChange {
		t.Errorf("Change mismatch:\n  got:  %q\n  want: %q", pending[0].Change, multiLineChange)
	}
}

// TestIsSafeTargetPath verifies that path traversal and absolute paths are
// rejected by isSafeTargetPath.
func TestIsSafeTargetPath(t *testing.T) {
	cases := []struct {
		target string
		safe   bool
	}{
		{".meept/skills/x/SKILL.md", true},
		{"config/prompts/test.md", true},
		{"CLAUDE.md", true},
		{"relative/path/file.md", true},

		{"/etc/passwd", false},
		{"/absolute/path.md", false},
		{"..", false},
		{"../etc/passwd", false},
		{"../../.ssh/authorized_keys", false},
		{"foo/../../bar", false},
	}
	for _, c := range cases {
		got := isSafeTargetPath(c.target)
		if got != c.safe {
			t.Errorf("isSafeTargetPath(%q) = %v; want %v", c.target, got, c.safe)
		}
	}
}

// TestProposalQueue_AppendAtomicity verifies that concurrent Append calls
// don't corrupt the markdown file. The single-Write + O_APPEND pattern
// is atomic on POSIX, so the file should remain parsable.
func TestProposalQueue_AppendConcurrency(t *testing.T) {
	tmp := t.TempDir()
	q := newProposalQueue(filepath.Join(tmp, "improvements.md"))
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			p := ReflectionProposal{
				Type:          "skill_create",
				Target:        "x",
				Change:        "y",
				Justification: "z",
				Confidence:    0.5,
				Source:        "test",
			}
			done <- q.Append(p)
		}(i)
	}
	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	pending, err := q.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 10 {
		t.Errorf("after 10 concurrent Appends, pending = %d; want 10", len(pending))
	}
}
