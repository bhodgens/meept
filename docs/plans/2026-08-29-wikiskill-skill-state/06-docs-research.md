# Docs, README Research Note, AGENTS.md — Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD (docs-only adaptation: verify
> cross-references resolve). Do NOT commit — the orchestrator handles all git
> operations after review. Do NOT use read_file on existing source files to
> then rewrite them — read once, patch precisely.

## Meta

- **Parent:** ./master.md
- **Scope:** Documentation for the wiki/trace/state features; README research
  note listing both papers; AGENTS.md invariant updates; CLI/HTTP reference
  touches where the surfaces already exist.
- **Dependencies:** all (Wave D — last leaf)
- **Estimated Context:** ~35K
- **Research reference:** arXiv:2608.27454 + arXiv:2608.26263. Commit message
  MUST cite both ids.

## Goal

Per repo rules (AGENTS.md Feature Documentation Requirements), the code from
leaves 01-05 needs docs in the same change. This leaf:

1. Adds a "Research references" note to README.md listing both papers with
   ids and what meept adopted.
2. Extends docs/workflows/skills.md with the wiki layer, trace store, and
   state-mode sections (config tables, file layout, guarantees).
3. Updates AGENTS.md: Key Components row stays (selfimprove/skills already
   listed); adds Critical Invariants entries for (a) wiki/trace content must
   never reach inference prompts (WikiSkill §5.1), (b) state-mode is per-skill
   opt-in and never applies to audit/debug histories (SKILL.state §7),
   (c) defaults (wiki on + inert, state off, evolver off).
4. Touches docs/reference/cli.md + docs/reference/http-api.md ONLY where the
   existing surfaces change (they do not: no new CLI verbs, no new endpoints —
   so expect small "no surface change" notes or skip; verify by grep first).

## Context

Key files to understand before implementing:

- README.md — feature table (~294 self-improve row), "Agent and Skill
  Customization" section (~349). The research note goes as a short subsection
  after the comparison table's "Other Key Differentiators" (~75) or beside the
  skills CLI reference (~325) — pick the least intrusive spot and match
  surrounding heading style.
- docs/workflows/skills.md — "Skill Evolution (Closed-Loop)" section at line
  105; the new sections extend THIS file (feature mapping rule:
  internal/skills → docs/workflows/skills.md; internal/selfimprove →
  docs/workflows/self-improvement.md — check that file exists and add the
  trace/wiki ownership there if it does).
- AGENTS.md — Critical Invariants section; follow the existing entry format
  (### heading + short paragraphs).

## Interface Contracts (From Parent)

Docs-only leaf — the contract is content accuracy:

- Every config field name in docs must match internal/config/schema.go as
  merged (wiki.enabled/wiki.dir, state.enabled/state.max_state_chars).
- Wiki layout listing must match master §Architecture (frozen).
- Both arXiv ids appear in README.md and in docs/workflows/skills.md.
- No doc claims a validation-score gate exists (we deliberately did not port
  it); no doc claims wiki content reaches the agent prompt.

## Tasks

### Task 1: README research note

**Objective:** Both papers listed with ids + one line each on what meept adopted.

**Files:**
- Modify: `README.md`

**Steps:** Read the section around the skills CLI reference. Insert a short
"### Research references" (match heading level of neighbors) with:

```markdown
- WikiSkill (arXiv:2608.27454) — persistent wiki + immutable traces feeding
  skill evolution; meept adopts the raw/wiki/skill layering in the evolver.
- SKILL.state (arXiv:2608.26263) — bounded-prompt state-mode skill execution;
  meept adopts opt-in state mode via skill frontmatter `state: true`.
```

Verify: `grep -n "2608.27454\|2608.26263" README.md` finds both.

### Task 2: docs/workflows/skills.md + self-improvement.md sections

**Objective:** Feature docs for wiki, traces, state mode.

**Files:**
- Modify: `docs/workflows/skills.md` (new sections after "Skill Evolution")
- Modify: `docs/workflows/self-improvement.md` IF it exists (grep); else note
  in Deviations.

**Steps:** Add three subsections: "Wiki layer" (layout tree from master,
config table for skills.wiki, restart-survival behavior, skill-impact ledger
semantics), "Trace store" (traces/<date>/<id>.json, failure capture, sampling
constants, nothing here reaches prompts), "State-mode execution" (frontmatter
flag, skills.state config, Σ semantics with null-deletion, reasoning discard,
when NOT to use it). Verify every file path and config key you cite by
grepping the merged code; do not write docs from the plan text alone.

### Task 3: AGENTS.md invariants

**Objective:** Three invariant entries per repo format.

**Files:**
- Modify: `AGENTS.md`

**Steps:** Add under Critical Invariants:

- "### Wiki and traces are evolver-only" — wiki/trace stores must never be
  reachable from ContextInjector, BuildSystemPrompt, or any inference-path
  prompt builder (paper ablation rationale).
- "### State mode is per-skill opt-in" — `state: true` frontmatter + enabled
  config; never on audit/debug/provenance tasks where the history is the
  deliverable.
- "### Wiki/state defaults" — wiki enabled+inert, state disabled, evolver
  disabled; flipping defaults is a product decision, not a code cleanup.

Verify the Key Components table needs no new row (internal/selfimprove and
internal/skills are already listed — confirm by grep, do not add duplicates).

### Task 4: Cross-reference sweep

- `grep -rn "skills.wiki\|skills.state" docs/` — every hit resolves to a real
  config key.
- `grep -rn "2608.27454\|2608.26263" README.md docs/` — ids present in the
  files this tree touched.
- No broken relative links introduced (`docs/workflows/skills.md` links).

## Self-Verification Checklist

- [ ] All files updated at exact paths; headings match neighbors
- [ ] Config keys and file layout match merged code (grepped, not assumed)
- [ ] Both arXiv ids in README.md + docs/workflows/skills.md
- [ ] No doc claims the val-score gate or prompt-side wiki access
- [ ] Cross-reference greps pass

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] README research note present with both ids
- [ ] skills.md sections accurate against merged code (spot-check 3 claims)
- [ ] AGENTS.md invariants present and consistent with merged defaults
- [ ] No contradictions with existing docs (skill evolution section still true)
- [ ] Markdown lint level: headings/nesting consistent with each file

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- README comparison-table claims rules (meept-development skill references)
  apply if you touch the table — prefer NOT to touch it in this leaf; the
  self-improve row is already accurate at the tree's scope.
- Keep the research note to ~6 lines; README is not a bibliography.
