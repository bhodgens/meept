package lifecycle

import (
	"context"
	"fmt"
	"strings"
)

// minCountForGapProposal is the minimum times a low-match query must recur
// before Pass D proposes a new skill for it. Singletons are noise.
const minCountForGapProposal = 5

// GapAnalyzer mines low-match queries and proposes new-skill candidates.
//
// Heuristic-only for first ship: generates a skeleton skill body that the
// verifier + a human (when AutoApply=false) will review. LLM-driven body
// generation is a follow-up.
type GapAnalyzer struct{}

// NewGapAnalyzer constructs an analyzer.
func NewGapAnalyzer() *GapAnalyzer {
	return &GapAnalyzer{}
}

// Propose generates EvolutionProposals for the given low-match queries.
// Queries appearing fewer than minCountForGapProposal times are skipped.
// Duplicate queries are deduped (kept at their max count).
func (a *GapAnalyzer) Propose(_ context.Context, gaps []LowMatchQuery) []EvolutionProposal {
	deduped := dedupeQueries(gaps)

	var proposals []EvolutionProposal
	for _, q := range deduped {
		if q.Count < minCountForGapProposal {
			continue
		}
		skillName := slugifyQuery(q.Query)
		body := generateSkillBody(skillName, q.Query, q.Count, q.BestScore)
		proposals = append(proposals, EvolutionProposal{
			Action: ProposalFillGap,
			SkillName: skillName,
			Rationale: fmt.Sprintf(
				"low-match query recurred %d times (best score %.2f); no existing skill covers it",
				q.Count, q.BestScore,
			),
			CandidateContent: body,
		})
	}
	return proposals
}

// generateSkillBody produces a SKILL.md skeleton for a coverage gap. The
// body is intentionally minimal — the verifier gate plus human review
// (when AutoApply=false) fill in the procedure.
func generateSkillBody(skillName, query string, count int, bestScore float64) string {
	return fmt.Sprintf(`---
name: %s
description: Auto-proposed by Pass D gap analysis (recurred %d times, best match %.2f)
priority: 4
---
# %s

This skill was auto-proposed because the query

    %q

recurred %d times without matching any existing skill above the
capability-index threshold (best score %.2f).

## Procedure

(filled in by review — describe what a competent agent should do when this query arises)

## Triggers

- query: %q
`, skillName, count, bestScore, skillName, query, count, bestScore, query)
}

func dedupeQueries(qs []LowMatchQuery) []LowMatchQuery {
	byQuery := map[string]LowMatchQuery{}
	for _, q := range qs {
		existing, ok := byQuery[q.Query]
		if !ok || q.Count > existing.Count {
			byQuery[q.Query] = q
		}
	}
	out := make([]LowMatchQuery, 0, len(byQuery))
	for _, q := range byQuery {
		out = append(out, q)
	}
	return out
}

func slugifyQuery(q string) string {
	var sb strings.Builder
	prevDash := false
	for _, r := range q {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
			prevDash = false
		case r >= 'A' && r <= 'Z':
			sb.WriteRune(r + ('a' - 'A'))
			prevDash = false
		default:
			if !prevDash && sb.Len() > 0 {
				sb.WriteRune('-')
				prevDash = true
			}
		}
	}
	result := sb.String()
	for len(result) > 0 && result[len(result)-1] == '-' {
		result = result[:len(result)-1]
	}
	if len(result) == 0 {
		return "unnamed-skill"
	}
	return result
}
