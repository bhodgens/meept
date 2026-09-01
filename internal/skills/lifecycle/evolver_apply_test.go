package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/skills"
)

// ---------------------------------------------------------------------------
// Test infrastructure for ApplyApprovedPlan (leaf 03, approval actuator)
// ---------------------------------------------------------------------------

// applyLogCapture is a slog.Handler that captures emitted messages so the
// audit-line tests can assert the exact
// "applied evolver plan <file> action=<a> proposal=<id> result=<ok|err>"
// line. Attrs/groups are dropped — only the message text is recorded.
type applyLogCapture struct {
	mu    sync.Mutex
	lines []string
}

func (c *applyLogCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *applyLogCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.Level >= slog.LevelInfo {
		c.lines = append(c.lines, r.Message)
	}
	return nil
}

func (c *applyLogCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *applyLogCapture) WithGroup(string) slog.Handler      { return c }

// infoLines returns a snapshot of the captured Info-level messages.
func (c *applyLogCapture) infoLines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.lines))
	copy(out, c.lines)
	return out
}

// auditLines returns only the captured evolver audit lines.
func (c *applyLogCapture) auditLines() []string {
	var out []string
	for _, l := range c.infoLines() {
		if strings.HasPrefix(l, "applied evolver plan ") {
			out = append(out, l)
		}
	}
	return out
}

// applyPlanSlug mirrors the plan writer's slug rule closely enough for
// fixture filenames: lowercase, non-alphanumerics collapsed to hyphens.
func applyPlanSlug(title string) string {
	var b strings.Builder
	lastHyphen := true
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// writeApplyPlanFixture writes a plan.md-format fixture into dir and returns
// its path. extraMeta lines are appended inside the ## Meta section (used for
// the evolver provenance stamp); candidate is optional text appended under
// ## Summary (used as the refine/create candidate content).
func writeApplyPlanFixture(t *testing.T, dir, title string, extraMeta []string, candidate string) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "# Plan: %s\n\n## Meta\n\n- plan_id: plan-apply-fixture\n- created: 2026-09-01\n- status: approved\n", title)
	for _, l := range extraMeta {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n## Summary\n\nApply-plan fixture rationale.\n")
	if candidate != "" {
		b.WriteString("\nCandidate content:\n" + candidate + "\n")
	}
	b.WriteString("\n## Notes\n")
	path := filepath.Join(dir, applyPlanSlug(title)+".md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write plan fixture: %v", err)
	}
	return path
}

// newApplyTestEvolver constructs an Evolver whose writer is rooted at the
// returned skillsDir and whose logger writes into the given capture. The LLM
// client is nil (never used by ApplyApprovedPlan — dispatch happens without
// another verifier pass) and the plan manager is nil (not used by the
// actuator).
func newApplyTestEvolver(t *testing.T, skillsDir string, capture *applyLogCapture) *Evolver {
	t.Helper()
	writer := NewWriter(skillsDir, slog.New(capture))
	evolver := NewEvolver(
		newStubUsageTracker(), nil, writer, skills.NewRegistry(), nil,
		NewVerifier(nil, slog.New(capture)), nil, nil,
		defaultEvolverConfig(), slog.New(capture),
	)
	return evolver
}

// assertAuditLine asserts exactly one audit line was emitted and its shape is
// `applied evolver plan <file> action=<a> proposal=<id> result=<r>`.
func assertAuditLine(t *testing.T, capture *applyLogCapture, planPath, action, proposalID, result string) {
	t.Helper()
	lines := capture.auditLines()
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 audit line, got %d: %q", len(lines), lines)
	}
	want := fmt.Sprintf("applied evolver plan %s action=%s proposal=%s result=%s",
		filepath.Base(planPath), action, proposalID, result)
	if lines[0] != want {
		t.Fatalf("audit line mismatch:\n got: %q\nwant: %q", lines[0], want)
	}
}

// ---------------------------------------------------------------------------
// TestApplyApprovedPlan: dispatch matrix for the approval actuator
// ---------------------------------------------------------------------------

func TestApplyApprovedPlan(t *testing.T) {
	t.Run("rejects non-evolver plan without touching skills", func(t *testing.T) {
		fixtureHome := t.TempDir()
		t.Setenv("HOME", fixtureHome)
		t.Setenv("USERPROFILE", fixtureHome)

		skillsDir := filepath.Join(t.TempDir(), "skills")
		capture := &applyLogCapture{}
		evolver := newApplyTestEvolver(t, skillsDir, capture)

		// Human-authored plan: no evolver provenance lines.
		planPath := writeApplyPlanFixture(t, t.TempDir(), "Human plan for something", nil, "")

		err := evolver.ApplyApprovedPlan(planPath)
		if err == nil {
			t.Fatal("expected rejection of a plan without evolver provenance, got nil")
		}
		if !strings.Contains(err.Error(), "skill-evolver") {
			t.Errorf("error should name the expected origin, got: %v", err)
		}

		// Nothing applied: the skills dir was never created/written.
		if entries, err := os.ReadDir(skillsDir); err == nil && len(entries) > 0 {
			t.Errorf("skills dir unexpectedly populated: %v", entries)
		}
		// No audit line for a rejected plan.
		if lines := capture.auditLines(); len(lines) != 0 {
			t.Errorf("rejected plan must not emit an audit line, got: %q", lines)
		}
	})

	t.Run("archive action archives skill in non-default tier", func(t *testing.T) {
		fixtureHome := t.TempDir()
		t.Setenv("HOME", fixtureHome)
		t.Setenv("USERPROFILE", fixtureHome)

		skillsDir := filepath.Join(t.TempDir(), "skills")
		claudeTier := filepath.Join(fixtureHome, ".claude", "skills")
		writeTierFixtureSkill(t, claudeTier, "apply-archive-skill")

		capture := &applyLogCapture{}
		evolver := newApplyTestEvolver(t, skillsDir, capture)

		title := "Skill evolution: archive apply-archive-skill"
		planPath := writeApplyPlanFixture(t, t.TempDir(), title, []string{
			"- origin: skill-evolver",
			"- proposal_id: evo-archive-apply-archive-skill-000001",
			"- action: archive",
		}, "")

		if err := evolver.ApplyApprovedPlan(planPath); err != nil {
			t.Fatalf("ApplyApprovedPlan: %v", err)
		}

		// Leaf 02 semantics: the skill is gone from its tier, the archive
		// landed under the Writer root's .archived sibling.
		if _, err := os.Stat(filepath.Join(claudeTier, "apply-archive-skill")); !os.IsNotExist(err) {
			t.Errorf("skill still present in tier after apply (err=%v)", err)
		}
		archived := filepath.Join(skillsDir+".archived", "apply-archive-skill", "SKILL.md")
		if _, err := os.Stat(archived); err != nil {
			t.Errorf("archived skill missing at %s: %v", archived, err)
		}

		assertAuditLine(t, capture, planPath, "archive", "evo-archive-apply-archive-skill-000001", "ok")

		// Durable idempotency marker written onto the plan file itself.
		data, err := os.ReadFile(planPath) //nolint:gosec // test fixture path
		if err != nil {
			t.Fatalf("read applied plan: %v", err)
		}
		if !strings.Contains(string(data), "\n- applied: ") {
			t.Errorf("applied marker missing from plan file:\n%s", data)
		}
	})

	t.Run("refine action rewrites skill content", func(t *testing.T) {
		fixtureHome := t.TempDir()
		t.Setenv("HOME", fixtureHome)
		t.Setenv("USERPROFILE", fixtureHome)

		skillsDir := filepath.Join(t.TempDir(), "skills")
		capture := &applyLogCapture{}
		evolver := newApplyTestEvolver(t, skillsDir, capture)

		const candidate = "---\nname: apply-refine-skill\ndescription: refined by approval actuator\n---\n\n## Refined body\n\nnew content"
		title := "Skill evolution: improve apply-refine-skill"
		planPath := writeApplyPlanFixture(t, t.TempDir(), title, []string{
			"- origin: skill-evolver",
			"- proposal_id: evo-improve-apply-refine-skill-000002",
			"- action: improve",
		}, candidate)

		if err := evolver.ApplyApprovedPlan(planPath); err != nil {
			t.Fatalf("ApplyApprovedPlan: %v", err)
		}

		written, err := os.ReadFile(filepath.Join(skillsDir, "apply-refine-skill", "SKILL.md")) //nolint:gosec // test fixture
		if err != nil {
			t.Fatalf("refined skill missing: %v", err)
		}
		if string(written) != candidate {
			t.Errorf("skill content mismatch:\n got: %q\nwant: %q", string(written), candidate)
		}

		assertAuditLine(t, capture, planPath, "improve", "evo-improve-apply-refine-skill-000002", "ok")
	})

	t.Run("create action creates new skill", func(t *testing.T) {
		fixtureHome := t.TempDir()
		t.Setenv("HOME", fixtureHome)
		t.Setenv("USERPROFILE", fixtureHome)

		skillsDir := filepath.Join(t.TempDir(), "skills")
		capture := &applyLogCapture{}
		evolver := newApplyTestEvolver(t, skillsDir, capture)

		const candidate = "---\nname: apply-create-skill\ndescription: created by approval actuator\n---\n\n## New skill body"
		title := "Skill evolution: create apply-create-skill"
		planPath := writeApplyPlanFixture(t, t.TempDir(), title, []string{
			"- origin: skill-evolver",
			"- proposal_id: evo-create-apply-create-skill-000003",
			"- action: create",
		}, candidate)

		if err := evolver.ApplyApprovedPlan(planPath); err != nil {
			t.Fatalf("ApplyApprovedPlan: %v", err)
		}

		written, err := os.ReadFile(filepath.Join(skillsDir, "apply-create-skill", "SKILL.md")) //nolint:gosec // test fixture
		if err != nil {
			t.Fatalf("created skill missing: %v", err)
		}
		if string(written) != candidate {
			t.Errorf("skill content mismatch:\n got: %q\nwant: %q", string(written), candidate)
		}

		assertAuditLine(t, capture, planPath, "create", "evo-create-apply-create-skill-000003", "ok")
	})

	t.Run("already applied proposal is a no-op", func(t *testing.T) {
		fixtureHome := t.TempDir()
		t.Setenv("HOME", fixtureHome)
		t.Setenv("USERPROFILE", fixtureHome)

		skillsDir := filepath.Join(t.TempDir(), "skills")
		claudeTier := filepath.Join(fixtureHome, ".claude", "skills")
		writeTierFixtureSkill(t, claudeTier, "apply-archive-skill")

		capture := &applyLogCapture{}
		evolver := newApplyTestEvolver(t, skillsDir, capture)

		title := "Skill evolution: archive apply-archive-skill"
		planPath := writeApplyPlanFixture(t, t.TempDir(), title, []string{
			"- origin: skill-evolver",
			"- proposal_id: evo-archive-apply-archive-skill-000001",
			"- action: archive",
		}, "")

		if err := evolver.ApplyApprovedPlan(planPath); err != nil {
			t.Fatalf("first ApplyApprovedPlan: %v", err)
		}
		before, err := os.ReadFile(planPath) //nolint:gosec // test fixture path
		if err != nil {
			t.Fatalf("read plan after first apply: %v", err)
		}

		// Second approval of the same plan (retry / repeat click): no-op.
		if err := evolver.ApplyApprovedPlan(planPath); err != nil {
			t.Fatalf("second ApplyApprovedPlan must be a no-op with nil error, got: %v", err)
		}

		if lines := capture.auditLines(); len(lines) != 1 {
			t.Errorf("no-op must not emit a second audit line, got %d: %q", len(lines), lines)
		}
		after, err := os.ReadFile(planPath) //nolint:gosec // test fixture path
		if err != nil {
			t.Fatalf("read plan after second apply: %v", err)
		}
		if string(before) != string(after) {
			t.Errorf("no-op rewrote the plan file")
		}
		// The archive still exists exactly once (no double application).
		archived := filepath.Join(skillsDir+".archived", "apply-archive-skill", "SKILL.md")
		if _, err := os.Stat(archived); err != nil {
			t.Errorf("archived skill missing after no-op: %v", err)
		}
	})

	t.Run("missing plan file errors naming the path", func(t *testing.T) {
		fixtureHome := t.TempDir()
		t.Setenv("HOME", fixtureHome)
		t.Setenv("USERPROFILE", fixtureHome)

		skillsDir := filepath.Join(t.TempDir(), "skills")
		capture := &applyLogCapture{}
		evolver := newApplyTestEvolver(t, skillsDir, capture)

		missing := filepath.Join(t.TempDir(), "no-such-plan.md")
		err := evolver.ApplyApprovedPlan(missing)
		if err == nil {
			t.Fatal("expected error for missing plan file, got nil")
		}
		if !strings.Contains(err.Error(), "no-such-plan.md") {
			t.Errorf("error should name the missing path, got: %v", err)
		}
		if lines := capture.auditLines(); len(lines) != 0 {
			t.Errorf("unreadable plan must not emit an audit line, got: %q", lines)
		}
	})

	t.Run("apply failure emits audit line with result=err", func(t *testing.T) {
		fixtureHome := t.TempDir()
		t.Setenv("HOME", fixtureHome)
		t.Setenv("USERPROFILE", fixtureHome)

		skillsDir := filepath.Join(t.TempDir(), "skills")
		capture := &applyLogCapture{}
		evolver := newApplyTestEvolver(t, skillsDir, capture)

		// Evolver plan whose skill exists in no tier → ArchiveSkill fails.
		title := "Skill evolution: archive apply-ghost-skill"
		planPath := writeApplyPlanFixture(t, t.TempDir(), title, []string{
			"- origin: skill-evolver",
			"- proposal_id: evo-archive-apply-ghost-skill-000004",
			"- action: archive",
		}, "")

		err := evolver.ApplyApprovedPlan(planPath)
		if err == nil {
			t.Fatal("expected error applying an archive for a nonexistent skill, got nil")
		}

		assertAuditLine(t, capture, planPath, "archive", "evo-archive-apply-ghost-skill-000004", "err")
	})

	t.Run("content action without candidate content is rejected", func(t *testing.T) {
		fixtureHome := t.TempDir()
		t.Setenv("HOME", fixtureHome)
		t.Setenv("USERPROFILE", fixtureHome)

		skillsDir := filepath.Join(t.TempDir(), "skills")
		capture := &applyLogCapture{}
		evolver := newApplyTestEvolver(t, skillsDir, capture)

		// A create plan with NO "Candidate content:" section: applying it
		// would write an empty skill, so the actuator must refuse (it never
		// invents creation inputs).
		title := "Skill evolution: create apply-empty-skill"
		planPath := writeApplyPlanFixture(t, t.TempDir(), title, []string{
			"- origin: skill-evolver",
			"- proposal_id: evo-create-apply-empty-skill-000005",
			"- action: create",
		}, "")

		err := evolver.ApplyApprovedPlan(planPath)
		if err == nil {
			t.Fatal("expected rejection of a create plan without candidate content, got nil")
		}
		if !strings.Contains(err.Error(), "candidate content") {
			t.Errorf("error should name the missing candidate content, got: %v", err)
		}
		// No skill dir was created.
		if _, err := os.Stat(filepath.Join(skillsDir, "apply-empty-skill")); !os.IsNotExist(err) {
			t.Errorf("empty skill was written (err=%v)", err)
		}
		assertAuditLine(t, capture, planPath, "create", "evo-create-apply-empty-skill-000005", "err")
	})
}
