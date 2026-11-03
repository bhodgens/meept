# 01-skill-origin-stamp — implementation leaf (branch 02, contracts C2 + consumes C1)

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist at
> e2e/suites/skills-refresh-origin/ (origin scenario) - make them pass. Do NOT weaken gate
> tests. Do NOT commit / git add.

## Meta

- **Parent:** orchestrator.md
- **Scope:** C2 - Origin type + SkillMetadata.origin + Writer.WriteSkillWithOrigin + notifier
- **Dependencies:** 00T-e2e-tests (TESTS_VALIDATED); branch 1 01-index-refresh-seam REVIEWED (C1)
- **Estimated Context:** 50K
- **GitHub Issue:** https://github.com/bhodgens/meept/issues/104
- **Concurrency Group:** B

## Goal

Every durable skill write stamps an origin (user/agent/evolver/review/unknown) into the
frontmatter, and the Writer emits a post-write change notification that marks the
branch-1 index dirty (C1). Legacy skills read OriginUnknown without migration.

## Context

- Single funnel: lifecycle.Writer.WriteSkill (writer.go:198) is called by the skill tools
 (components.go:7219/7225), the evolver (evolver.go:819), and the CLI (cmd/meept/skills.go).
- Frontmatter: SkillMetadata (models.go:241-266) is the parsed/serialized struct; add Origin.
- C1 (branch 1) provides SkillIndex.MarkDirty() for the notifier to call.

## Verified Anchors

- internal/skills/lifecycle/writer.go:198 - func (w *Writer) WriteSkill(name, content string) error
- internal/skills/models.go:241-266 - type SkillMetadata struct
- internal/skills/lifecycle/evolver.go:819 - e.writer.WriteSkill(proposal.SkillName, ...)
- internal/daemon/components.go:7219,7225 - NewSkillCreateTool / NewSkillPatchTool call sites
- cmd/meept/skills.go (archive/restore = user-origin CLI paths)

## Interface Contracts (From Parent)

### What This Leaf Exposes (C2, exact)

```
type Origin string
const ( OriginUnknown, OriginUser, OriginAgent, OriginEvolver, OriginReview Origin )
func (w *lifecycle.Writer) SetChangeNotifier(fn func(skillName string))
func (w *lifecycle.Writer) WriteSkillWithOrigin(name, content string, o Origin) error
func (w *lifecycle.Writer) WriteSkill(name, content string) error // == WithOrigin(OriginUnknown)
// SkillMetadata gains:
Origin string `yaml:"origin,omitempty"`
```

### What This Leaf Consumes

- C1: `func (i *skills.SkillIndex) MarkDirty()` - the notifier calls it (owned by branch 1).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Define Origin + const set; add Origin to SkillMetadata (yaml origin, omitempty) | Modify: internal/skills/models.go (+ a small origin.go if cleaner) | gate origin suite | RED - undefined | GREEN | compiles; frontmatter round-trips origin |
| T2 | Writer.WriteSkillWithOrigin + SetChangeNotifier; keep WriteSkill as WithOrigin(OriginUnknown) | Modify: internal/skills/lifecycle/writer.go | gate | RED | GREEN | writing via WithOrigin persists frontmatter origin + fires notifier |
| T3 | Stamp every call site: skill tools = agent, CLI = user, evolver = evolver (evolver.go:819 passes OriginEvolver) | Modify: internal/daemon/components.go:7219/7225, cmd/meept/skills.go, internal/skills/lifecycle/evolver.go | gate | RED | GREEN | each path's written skill carries its origin |
| T4 | Evolver curation CONSUMES origin (trust ordering user>evolver>agent>review documented, not gated) - assert evolver writes OriginEvolver | Verify evolver_test | gate | n/a | GREEN | evolver-authored skill has origin 'evolver'; docs note the ordering |

## Self-Verification Checklist

- [ ] C2 exact; Origin enum values as pinned
- [ ] ALL write paths stamp (tools/CLI/evolver); legacy reads 'unknown'
- [ ] notifier wired to C1 MarkDirty (no second dirty seam)
- [ ] gofmt; typed-nil guard on SetChangeNotifier (nil-safe no-op when unset); DO NOT COMMIT

## Review Checklist (For Review Agent)

- [ ] C2 exact; origin byte-round-trips through frontmatter
- [ ] No path left unstamped (grep WriteSkill callers = all migrated to WithOrigin)
- [ ] notifier is nil-safe; e2e origin scenario GREEN

## Notes

- Do not change WriteSkill's signature (backward compat); it delegates to WithOrigin.
