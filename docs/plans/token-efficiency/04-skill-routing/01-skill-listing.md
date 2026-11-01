# Skill Listing Fix - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD; gate tests
> exist and are committed - make them pass. Do NOT delete/skip/weaken gate
> tests. Do NOT commit. Write once, never read back. Explore with
> search_files/terminal, not read_file.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** replace relevance-capped skill injection with the C4 no-cap listing; add disable-model-invocation frontmatter support
- **Dependencies:** 00T VALIDATED
- **Estimated Context:** 75K
- **Concurrency Group:** solo

## Goal

GAP-4 fix: skills listing becomes one deterministic line per visible cached
skill (alphabetical), no cap, no MatchAll(base) signal, no all-cached fallback
branch (moot without matching). New frontmatter key
`disable-model-invocation: true` removes a skill from the listing while
keeping it executable via explicit paths.

## Context

- internal/agent/context_injector.go:15 (const to delete), :54-116
  (BuildSystemPrompt - rewrite the skills section), :121-166
  (relevantSkillNames - simplify to visible+sorted, or delete and inline)
- internal/skills/models.go:67 area - frontmatter model; discovery.go:235 -
  parse site. Follow the existing AllowedTools parsing pattern for the new
  bool key.
- Cache stability: the listing must not depend on per-turn inputs (base text
  removed as a signal - that is the point).
- LazySkillLoader.CachedNames()/Get() (used today at :122,:154) provide name +
  description + path.

## Verified Anchors

- internal/agent/context_injector.go:15 - `const maxRelevantSkills = 5`
- internal/agent/context_injector.go:121 - `func (c *ContextInjector) relevantSkillNames`
- internal/skills/models.go:67 - AllowedTools frontmatter neighbor
- internal/skills/discovery.go:235 - AllowedTools parse site

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/skills models: Skill gains
DisableModelInvocation bool `json:"disable_model_invocation,omitempty"` // frontmatter: disable-model-invocation
// context_injector: listing line (C4, byte-exact):
// "- <name>: <description> [load: file_read <absolute SKILL.md path>]
"
// intro line kept short: "The following skills provide specialized
// instructions. Load a skill's file when the task matches its description."
```

### What This Leaf Consumes

- C4 (../master.md)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Frontmatter key | Modify: internal/skills/models.go, internal/skills/discovery.go | `go test -p 2 ./internal/skills/` | gate RED | key parsed; absent = false; old files unaffected | Given SKILL.md with disable-model-invocation: true; When discovered; Then field true |
| T2 | Listing rewrite | Modify: internal/agent/context_injector.go | `go test -p 2 -run TestSkillsListing ./internal/agent/` | gate RED | C4 lines, alphabetical, no cap, hidden excluded | Given 8 cached (1 hidden); When BuildSystemPrompt; Then 7 C4 lines in alpha order |
| T3 | Dead code removal | Modify: internal/agent/context_injector.go | `go build ./... && go vet ./internal/agent/` | staticcheck U1000 on leftover const | maxRelevantSkills + MatchAll(base) call path deleted; no unused symbols | Given the rewrite; When vetted; Then zero unused |
| T4 | Stability | covered by gate T4 | `go test -p 2 -run TestSkillsListingStable ./internal/agent/` | gate RED | byte-identical builds | Given fixed cache; When built twice; Then equal |
| T5 | Docs | Modify: docs/workflows/skills.md (frontmatter table) | cross-ref grep | n/a | key documented | Given a skill author; When reading docs; Then the key's effect is stated |

## Self-Verification Checklist

- [ ] Gate tests pass untouched; C4 byte-exact
- [ ] No cap remains anywhere in the injection path (grep maxRelevantSkills = 0 hits)
- [ ] Hidden skills still executable via explicit invocation (executor path unchanged)
- [ ] `go test -p 2 ./internal/agent/ ./internal/skills/` green

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. verify tails; 3. deviations.

## Review Checklist (For Review Agent)

- [ ] MatchAll(base) signal fully removed (no dead branches)
- [ ] Alphabetical order (cache-stable, deterministic)
- [ ] Intro text minimal (pi parity - no verbose scaffolding)
- [ ] docs/workflows/skills.md updated same commit set
