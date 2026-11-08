package lifecycle

// bughunt 2026-11-07 agent-fixes F-G pin: an evolver proposal against a
// skill whose on-disk frontmatter declares `origin: user` must be SKIPPED
// at processProposal time — before the verifier LLM call, before any apply,
// before any sidecar rewrite. The config-tools F-4 guard made overwrite a
// PERMANENT refusal class; without this the retry loop re-proposed,
// re-refused, and re-wrote the candidate sidecar every cycle forever.

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/skills"
)

func TestEvolverSkipsUserLockedSkill(t *testing.T) {
	skillName := "locked-skill"
	userContent := "---\nname: locked-skill\ndescription: d\norigin: user\n---\n\nuser authored body\n"

	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	if err := writer.WriteSkillWithOrigin(skillName, userContent, OriginUser); err != nil {
		t.Fatalf("seed user skill: %v", err)
	}

	registry := skills.NewRegistry()
	parsed, err := skills.ParseSkillFile(filepath.Join(dir, skillName, "SKILL.md"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	registry.Register(parsed)

	usage := newStubUsageTracker()
	usage.SetStats(skillName, &UsageStats{
		SkillName: skillName, InjectCount: 10, PositiveCount: 5, Effectiveness: 0.5,
	})

	// Refusing verifier: if the guard failed to skip BEFORE the verifier,
	// this call would reject and the report would say Rejected=1 — the
	// distinguishing shape between "skipped (user-locked)" and "verifier
	// ran". The LLM chatter counts calls too: pass A must not even spend
	// the refine prompt... it does (proposal generation precedes the
	// gate), so assert on the REPORT, not the call count.
	verifier := newTestVerifier(true)
	cfg := defaultEvolverConfig()
	cfg.AutoApply = true

	evolver := NewEvolver(usage, nil, writer, registry, nil, verifier, nil, nil, cfg, slog.Default())
	// Direct processProposal with a hand-built refine (deterministic; no
	// LLM generation needed for the pin).
	proposal := EvolutionProposal{
		Action:           ProposalRefine,
		SkillName:        skillName,
		Rationale:        "test",
		CandidateContent: "---\nname: locked-skill\ndescription: d\norigin: evolver\n---\n\nevolved body\n",
	}
	report := &EvolutionReport{}
	evolver.processProposal(context.Background(), report, proposal, "", "improve_skill", nil)

	if report.Skipped != 1 {
		t.Fatalf("user-locked proposal must skip (Skipped=1), report: %+v", report)
	}
	if report.Refined != 0 || report.Rejected != 0 || report.Planned != 0 {
		t.Fatalf("user-locked proposal must not reach verifier/apply: %+v", report)
	}
	if len(report.Details) != 1 || !strings.Contains(report.Details[0].Rationale, "user-locked") {
		t.Fatalf("skip must be recorded with the reason: %+v", report.Details)
	}

	// On-disk file untouched; NO sidecar churn this cycle (the F-F writer
	// path was never reached — skip precedes apply).
	got, err := writer.ReadSkill(skillName)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != userContent {
		t.Fatalf("user-locked skill altered: %q", got)
	}
}
