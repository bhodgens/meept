# Skill Patch Tool - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files — explore with search_files or terminal cat.
> After writing a file, do NOT read it back to verify — write once and
> stop. After completing, report what you built, what files you touched,
> and any deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** A `skills_patch` builtin tool: agent-facing modification
  of EXISTING skills — surgical replace (unique old_string -> new_string)
  or full rewrite — through the lifecycle Writer, with system-tier
  refusal.
- **Dependencies:** 02-skill-create-tool.md (constructor conventions
  and test patterns — mirror them; no shared files, no shared symbols)
- **Estimated Context:** 40K
- **Concurrency Group:** A

## Goal

Skills improve over time — that is the whole point of a skill library.
This leaf adds the modify half of agent-facing authoring: two modes,
never both. `replace` edits the body via a unique-string swap (the
agent's patch primitive is the same discipline as Hermes' patch tool).
`rewrite` replaces the entire SKILL.md content after validation.
Refuses skills discovered in the system tier (immutable), directing
the agent to create a user-tier shadow instead.

## Context

Skill bodies live in the registry after discovery
(`internal/skills/registry.go` — `Get(name)` / `List()`; read it for
the exact accessor). The lifecycle Writer's tier resolution
(`ResolveTierPath`, internal/skills/tier_resolve.go) reports which tier
holds a skill and its on-disk SKILL.md path. The system tier
(`PrioritySystem`) must never be written by agents. Writes go through
`lifecycle.Writer.WriteSkill` (versioned).

Key files:
- internal/skills/registry.go — Get/List accessors
- internal/skills/tier_resolve.go — ResolveTierPath + Source/Priority
- internal/skills/lifecycle/writer.go — WriteSkill
- internal/tools/builtin/skill_create_tool.go — sibling conventions
  (02 must exist in tree history OR follow this doc's stated shapes —
  constructor style is fixed by the master contract: constructor +
  Set* nil guards, table tests, real-writer-into-t.TempDir tests)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: internal/tools/builtin/skill_patch_tool.go
package builtin

func NewSkillPatchTool(registry *skills.Registry, writer *lifecycle.Writer, logger *slog.Logger) *SkillPatchTool

// Name() == "skills_patch"
// Parameters:
//   name       string, required
//   old_string string, required when mode=replace
//   new_string string, required when mode=replace (may be "")
//   content    string, required when mode=rewrite (FULL new SKILL.md)
// Mode inference: content != "" -> rewrite; else replace. If both set:
// error "set either content (rewrite) or old_string/new_string
// (replace), not both".
```

### What This Leaf Consumes

```
// From internal/skills (implemented): Registry.Get(name) returning
// *Skill (body + Path + Priority + Source fields), or the closest
// exported accessor — read registry.go.
// From internal/skills/tier_resolve.go: (*Discovery).ResolveTierPath
// is NOT needed here — the registry Skill already carries
// Path/Priority. Use skill.Priority to detect the system tier.
// From internal/skills/lifecycle: Writer.WriteSkill(name, content).
```

## Tasks

### Task 1: Tool skeleton, schema, mode validation

**Objective:** Struct, constructor, schema; enforce the two-mode rule.

**Files:**
- Create: `internal/tools/builtin/skill_patch_tool.go`
- Test: `internal/tools/builtin/skill_patch_tool_test.go`

**Step 1: Write failing test**

```go
func TestSkillPatch_NameAndSchema(t *testing.T) { /* name == "skills_patch" */ }
func TestSkillPatch_ModeValidation(t *testing.T) {
    // table: name+old_string -> replace; name+content -> rewrite;
    // both old_string and content -> error "not both"; neither -> error;
    // empty name -> error.
}
```

**Step 2: Run test to verify failure**
Run: `go test ./internal/tools/builtin/ -run TestSkillPatch -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

Per contract. `validatePatchArgs(params) (mode, error)`.

**Step 4: Run test to verify pass**
Expected: PASS

### Task 2: Replace mode — unique-match swap on the body

**Objective:** Load the skill, verify old_string occurs exactly once
in the body, swap, write.

**Files:**
- Modify: `internal/tools/builtin/skill_patch_tool.go`
- Test: `internal/tools/builtin/skill_patch_tool_test.go`

**Step 1: Write failing test**

```go
func TestSkillPatch_ReplaceMode(t *testing.T) {
    // Real Discovery over a temp skills dir with one skill
    // (copy the minimal layout skills.Discovery scans — read
    // discovery_test.go for the fixture pattern) OR a minimal
    // constructed Registry if construction is cheap. Real writer into
    // the same temp dir (lifecycle.NewWriter).
    // Execute replace: old="step one", new="step ONE". Assert output
    // mentions the written path; file content contains "step ONE";
    // versioner/history unaffected test skipped (versioner optional).
}
func TestSkillPatch_ReplaceMode_OldStringNotFound(t *testing.T) {
    // old_string absent -> error naming the skill and "not found".
}
func TestSkillPatch_ReplaceMode_OldStringNotUnique(t *testing.T) {
    // old_string twice -> error "found N occurrences; include more
    // context".
}
```

**Step 2: Run test to verify failure**
Expected: FAIL

**Step 3: Write minimal implementation**

`strings.Count(body, old)` — 0 -> not-found error; >1 -> not-unique
error with count; ==1 -> swap via strings.Replace with n=1. New
content = frontmatter (unchanged from disk parse) + patched body.
Write via writer. Registry nil at Execute -> "skills registry
unavailable" error.

**Step 4: Run test to verify pass**
Expected: PASS

### Task 3: Rewrite mode with parser validation

**Objective:** Full-content rewrite validated the same way leaf 02
assembles content (frontmatter + body must parse).

**Files:**
- Modify: `internal/tools/builtin/skill_patch_tool.go`
- Test: `internal/tools/builtin/skill_patch_tool_test.go`

**Step 1: Write failing test**

```go
func TestSkillPatch_RewriteMode(t *testing.T) {
    // content = full SKILL.md with frontmatter. Assert write succeeds
    // and disk content matches exactly.
}
func TestSkillPatch_RewriteMode_InvalidFrontmatter(t *testing.T) {
    // content without a name in frontmatter (or unparseable YAML) ->
    // error from the parser path, nothing written.
}
```

**Step 2: Run test to verify failure**
Expected: FAIL

**Step 3: Write minimal implementation**

Validate content through the same parser path leaf 02 used
(internal/skills parser on a temp file, or name/regex checks if the
parser is not directly exported — mirror leaf 02's choice EXACTLY so
the two tools share validation behavior). Then WriteSkill. The
rewritten name in frontmatter must equal params.name — mismatch ->
error "rewrite content name %q does not match target %q".

**Step 4: Run test to verify pass**
Expected: PASS

### Task 4: System-tier refusal

**Objective:** Never write a skill discovered in the system tier.

**Files:**
- Modify: `internal/tools/builtin/skill_patch_tool.go`
- Test: `internal/tools/builtin/skill_patch_tool_test.go`

**Step 1: Write failing test**

```go
func TestSkillPatch_SystemTierRefused(t *testing.T) {
    // Seed a skill whose discovered Priority == skills.PrioritySystem
    // (fixture: place it in the system tier dir of the temp discovery
    // layout; read discovery.go DefaultTiers for how system tier maps
    // to a directory — for tests, construct the Registry entry with
    // Priority directly if tier dirs are awkward).
    // Execute any mode -> error "system tier skills are read-only;
    // create a user-tier skill named <name> to override" and NO file
    // written.
}
```

**Step 2: Run test to verify failure**
Expected: FAIL

**Step 3: Write minimal implementation**

Check `skill.Priority == skills.PrioritySystem` before any write;
return the refusal error. (User/claude/hermes tiers: allowed — writer
resolves the actual holding tier and writes in place, which is
lifecycle.Writer's documented behavior.)

**Step 4: Run test to verify pass**
Expected: PASS

### Task 5: gofmt + package green

gofmt; `go build ./internal/tools/... ./internal/skills/... && go test
./internal/tools/builtin/ -count=1` — pass.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep - only what the tasks specify
- [ ] gofmt clean; no ignored errors; constructor stores deps possibly-nil

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (signatures, types, file paths)
- [ ] Conventions match leaf 02's shapes (constructor, setters, tests)
- [ ] No bugs, no security issues (no partial writes on validation
      failure; system tier truly unwritten — assert disk state in test)
- [ ] No scope creep beyond specified tasks (NO create; NO delete;
      NO archive integration beyond what Writer already does)

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Patch-on-body-only is deliberate: replace mode never edits
  frontmatter (use rewrite for structural changes). This keeps
  old_string uniqueness meaningful and avoids YAML surgery.
- If the registry's Skill.Body excludes frontmatter (check parser.go —
  Body is "instruction markdown AFTER frontmatter"), the reassembly in
  Task 2 must reconstruct frontmatter from the OTHER parsed fields OR
  re-read the on-disk SKILL.md and splice by index. Re-reading disk
  (skill.Path) and splicing on the second "---" line is the
  lower-risk path; do that.
- Fixture reuse: discovery/registry test fixtures in this repo are
  established (discovery_test.go, registry_test.go). Mirror them
  rather than inventing a third pattern.
