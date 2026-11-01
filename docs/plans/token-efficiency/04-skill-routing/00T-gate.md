# Branch 04 Test Gate - Skill Listing Tests - Test Leaf

> **For the implementing agent:** Write TESTS ONLY, no implementation. RED
> for missing-implementation reasons against CURRENT exported surfaces
> (BuildSystemPrompt output assertions compile today). Do NOT commit.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** failing tests pinning C4: no-cap listing, line shape, hidden
  skills, stability
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** solo (gate)

## Goal

Pin the post-fix behavior: with >5 cached visible skills, ALL appear in the
prompt; each line matches C4 shape; a disable-model-invocation skill does not
appear; two builds in the same episode are byte-identical.

## Context

- internal/agent/context_injector.go:54 BuildSystemPrompt(ctx, base) - the
  surface under test (exported; tests compile today and fail RED on content).
- internal/skills discovery: frontmatter parse site for the new key
  (internal/skills/discovery.go:235 area; models.go:67 AllowedTools neighbor).
- Existing tests in internal/agent/ for injector setup patterns.

## Verified Anchors

- internal/agent/context_injector.go:54 - BuildSystemPrompt
- internal/agent/context_injector.go:15 - maxRelevantSkills = 5 (const under removal)
- internal/skills/models.go:67 - frontmatter model area

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/agent (existing test file for injector, or context_injector_test.go
// if it exists - bug-fix class allows unit tests in existing files)
func TestSkillsListingHasNoCap(t *testing.T)          // 8 cached skills -> 8 lines
func TestSkillsListingLineShape(t *testing.T)         // matches C4 exactly
func TestSkillsListingHidesModelInvocationDisabled(t *testing.T)
func TestSkillsListingStableWithinEpisode(t *testing.T) // byte-identical twice
```

### What This Leaf Consumes

- C4: `- <name>: <description> [load: file_read <absolute SKILL.md path>]`

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | No-cap test | Modify/Create: internal/agent/context_injector_test.go (create only if absent; else use the existing injector test file) | `go test -p 2 -run TestSkillsListing ./internal/agent/` | current cap 5 | all visible skills listed | Given 8 cached skills; When BuildSystemPrompt; Then 8 listing lines |
| T2 | Line shape | same | same | current shape differs (`- **name**: desc`) | C4 exact | Given one skill; When listed; Then line matches regex `^- [a-z0-9-]+: .+ \[load: file_read /.+/SKILL\.md\]$` |
| T3 | Hidden skill | same (+ internal/skills test for frontmatter parse) | `go test -p 2 -run TestSkills ./internal/agent/ ./internal/skills/` | key not parsed | hidden from listing | Given frontmatter disable-model-invocation: true; When listed; Then absent |
| T4 | Stability | same | same | ordering may vary (MatchAll scores) | byte-identical | Given fixed cache; When built twice; Then equal strings |

## Self-Verification Checklist

- [ ] Tests compile against CURRENT code and fail RED on assertions
- [ ] No implementation changes; no weakened assertions
- [ ] Deterministic fixtures (t.TempDir skill trees)

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. RED output tails per test; 3. deviations.

## Review Checklist (For Review Agent)

- [ ] All four RED for behavioral reasons
- [ ] C4 regex in T2 matches ../master.md contract verbatim
