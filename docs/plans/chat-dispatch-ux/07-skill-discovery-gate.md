# Skill Discovery Domain Gate - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** A domain-agreement gate stops generic verb/noun overlap from surfacing irrelevant skills (e.g. go-linter-save-hook-revert for a water-reminder request).
- **Dependencies:** 05 (both edit internal/agent/loop.go area — dispatch only after 05 is committed)
- **Estimated Context:** 25K
- **Audit references:** observation O1 (discovery matched go-linter-save-hook-revert 0.72 to a water-reminder ask); extends commit e0d08e2f stopword work

## Goal

discoverRelevantSkills (internal/agent/loop.go:2877) ranks skills by
keyword confidence; generic tokens ("save", "fix", "improve", "file",
"error") overlap across unrelated domains, so threshold 0.5 alone lets
wrong-domain skills inject into prompts and restrict tool lists. This leaf
adds a post-rank domain check: at least one non-stopword domain token from
the query must appear in the skill's name, tags, or description. Pure
pass-through when the gate has no domain tokens to compare.

## Context

The CapabilityIndex match API: ci.MatchWithThreshold(input, minConfidence, 3)
returns []*DiscoveredSkill-ish matches with Entry.Name, Entry.Tags,
Entry.Description, Confidence (loop.go:2886). Stopword handling landed in
commit e0d08e2f ("stopword generic tool verbs/nouns in keyword extraction")
— locate its word list (internal/agent or internal/skills; grep the commit)
and REUSE it; do not fork a second list.

Key files to understand before implementing:
- internal/agent/loop.go:2877-2930 - discoverRelevantSkills
- commit e0d08e2f - stopword list location and shape (`git show e0d08e2f --stat`)
- internal/agent/cache.go / capability index types - Entry fields

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent (new file internal/agent/skill_gate.go allowed)
//   func domainAgrees(query string, entry SkillEntryView) bool
//     - extract query tokens (lowercase, split non-alnum, drop stopwords,
//       drop len<4) -> domainTokens
//     - no domainTokens (e.g. "hi", "help me") -> true (pass-through)
//     - true when ANY domainToken appears (substring, case-insensitive)
//       in entry.Name OR entry.Tags OR entry.Description
//   discoverRelevantSkills filters its returned matches with domainAgrees
//   BEFORE threshold slicing; if all matches fail the gate, return empty
//   (record the low-match as today's no-match path does).
// Owner: 07. Consumers: 10 (e2e: water-reminder ask discovers no linter skill).
```

### What This Leaf Consumes

```
// stopword list from e0d08e2f (same package or internal/skills — locate)
// CapabilityIndex match entry fields (Name/Tags/Description)
```

## Tasks

### Task 1: domainAgrees function

**Objective:** Pure function + table tests.

**Files:**
- Create: `internal/agent/skill_gate.go`
- Test: `internal/agent/skill_gate_test.go`

**Step 1: Write failing test**

```go
func TestDomainAgrees(t *testing.T) {
	linter := SkillEntryView{Name: "go-linter-save-hook-revert",
		Tags: []string{"go", "linting", "hooks"},
		Description: "revert linter save hooks"}
	water := SkillEntryView{Name: "python-project-local-setup",
		Tags: []string{"python", "venv"},
		Description: "set up python projects for local testing"}

	tests := []struct {
		name  string
		query string
		entry SkillEntryView
		want  bool
	}{
		{"water ask vs linter skill", "remind me to drink water every 30 minutes while working", linter, false},
		{"linter ask vs linter skill", "the linter save hook reverts my go changes", linter, true},
		{"python ask vs python skill", "set up my python project for testing", water, true},
		{"generic greeting passes through", "hello there", linter, true},
		{"stopword-only query passes", "fix this for me please", water, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := domainAgrees(tc.query, tc.entry); got != tc.want {
				t.Errorf("domainAgrees(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}
```

**Step 2: verify failure** — `go test -p 2 ./internal/agent/ -run TestDomainAgrees -v` → FAIL (undefined).

**Step 3: implement** per contract. Reuse the e0d08e2f stopword list;
add domain-flavored tokens to the drop list ONLY if absent (e.g. "please",
"every") — keep changes minimal and documented.

**Step 4: verify pass** — same run → PASS.

### Task 2: wire into discoverRelevantSkills

**Objective:** Filter matches through domainAgrees before returning.

**Files:**
- Modify: `internal/agent/loop.go:2877-2930` (post-MatchWithThreshold)
- Test: `internal/agent/loop_test.go` (or skill_gate_test.go with a fake
  CapabilityIndex — check how existing tests fake the index; grep
  `MatchWithThreshold` in _test files)

**Step 1: Write failing test** — fake index returns [linter-skill(0.8),
water-skill(0.6)] for the water query; discoverRelevantSkills returns only
the water skill (or empty if the fake only has the linter skill).

**Step 2: verify failure → implement (filter slice with domainAgrees) →
verify pass.** Keep the low-match recording path intact when the filter
empties the set.

**Step 3: run** `go test -p 2 ./internal/agent/ -run 'DomainAgrees|DiscoverRelevant' -v` → PASS.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or documented below)
- [ ] No scope creep
- [ ] Threshold semantics unchanged (gate is additional, not a replacement)
- [ ] Skill-restricted tool filtering (loop.go:2313) still functions for
      gated-in skills

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Stopword list REUSED (single source), not forked
- [ ] Pass-through when no domain tokens (short/generic queries unaffected)
- [ ] Filter placed before threshold slicing; low-match recording intact
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The 0.5 default threshold and SkillDiscoveryThreshold config stay as-is.
- Substring match (not token equality) so "water" matches
  "drink-water-hydrate" style names; false-positive risk is bounded by the
  len>=4 floor.
- If the stopword list lives in internal/skills, pass it in or duplicate a
  minimal local reference — no import cycle (internal/agent must not import
  internal/skills if that would cycle; check go.mod layout and pick the
  acyclic direction; document the choice).
