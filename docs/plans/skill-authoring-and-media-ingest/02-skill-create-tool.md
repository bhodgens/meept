# Skill Create Tool - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files — explore with search_files or terminal cat.
> After writing a file, do NOT read it back to verify — write once and
> stop. After completing, report what you built, what files you touched,
> and any deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** A `skills_create` builtin tool: agent-facing creation of
  new SKILL.md files in the user tier through the existing lifecycle
  Writer (versioned, deduped, tier-resolved).
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** A

## Goal

Meept's skill system is read-only from the agent's perspective today.
This leaf adds the create half of agent-facing skill authoring: the
agent supplies name, description, tags, and a markdown body; the tool
assembles frontmatter + body, validates against the parser's rules, and
delegates to `lifecycle.Writer.WriteSkill` — which already handles
tier resolution (new skills land in the user tier), versioning, and
SHA dedup. The tool adds validation and honest errors, nothing more.

## Context

`internal/skills/lifecycle/writer.go` is the only writer:
`NewWriter(skillsDir, logger)` + `WriteSkill(name, content string)
error`, with `SetVersioner`, `SetRegistry`, and tier resolution via
`SetTierResolver` (internal/skills/tier_resolve.go). It is constructed
in the daemon (`internal/daemon/components.go` initializeSkills, with
SetVersioner + SetRegistry wired). The daemon tool-construction path
runs BEFORE skill init (see master Notes) — this leaf's tool takes the
writer via its constructor and must tolerate a nil writer at
construction time, returning a clear "skills writer unavailable" error
from Execute if still nil at call time.

The parser (`internal/skills/parser.go`) owns frontmatter rules
(name required, YAML validity, name-match rules) and Hermes-compat
fields (version, license). Reuse its exported functions for
validation; do not re-implement YAML handling.

Key files:
- internal/skills/lifecycle/writer.go — WriteSkill contract
- internal/skills/parser.go — frontmatter validation
- internal/tools/builtin/web_fetch.go — tool construction pattern
- internal/tools/builtin/setters_test.go — nil-guard test pattern

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: internal/tools/builtin/skill_create_tool.go
package builtin

func NewSkillCreateTool(writer *lifecycle.Writer, logger *slog.Logger) *SkillCreateTool
func (t *SkillCreateTool) SetRegistry(r *skills.Registry) // nil-guarded

// Name() == "skills_create"
// Parameters:
//   name        string, required
//   description string, required
//   body        string, required
//   tags        []string, optional
```

### What This Leaf Consumes

```
// From internal/skills/lifecycle (already implemented):
func NewWriter(skillsDir string, logger *slog.Logger) *Writer
func (w *Writer) WriteSkill(name, content string) error

// From internal/skills (already implemented): parser validation
// functions for frontmatter/name rules — use whatever exported form
// parser.go provides; if no exported validator fits, assemble the
// content and let the tool's own validation enforce: name kebab-case
// (^[a-z0-9][a-z0-9-]*$), description <= 200 chars, body non-empty.
```

## Tasks

### Task 1: Tool skeleton + schema + name validation

**Objective:** Struct, constructor, schema; validate skill names.

**Files:**
- Create: `internal/tools/builtin/skill_create_tool.go`
- Test: `internal/tools/builtin/skill_create_tool_test.go`

**Step 1: Write failing test**

```go
func TestSkillCreate_NameAndSchema(t *testing.T) { /* name == "skills_create"; schema has name/description/body required, tags optional */ }
func TestSkillCreate_ValidateName(t *testing.T) {
    // table: "code-review" ok; "Code Review" reject; "" reject;
    // "-x" reject; "a_b" reject; 64+ chars reject.
}
```

**Step 2: Run test to verify failure**
Run: `go test ./internal/tools/builtin/ -run TestSkillCreate -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

Constructor stores writer (may be nil) + logger. `validateSkillName`.

**Step 4: Run test to verify pass**
Expected: PASS

### Task 2: Content assembly + parser acceptance

**Objective:** Assemble SKILL.md frontmatter + body; the result must
pass the existing skills parser.

**Files:**
- Modify: `internal/tools/builtin/skill_create_tool.go`
- Test: `internal/tools/builtin/skill_create_tool_test.go`

**Step 1: Write failing test**

```go
func TestSkillCreate_AssembleContent(t *testing.T) {
    // name "learn-from-video", desc "one line", tags [media, learning],
    // body "# Learn\n\nsteps...".
    // Assert output starts with "---\n", contains "name: learn-from-video",
    // "description:", "tags:", and the body verbatim after frontmatter.
}
func TestSkillCreate_AssembledContent_ParsesWithSkillsParser(t *testing.T) {
    // Call internal/skills parser (exported Parse function or
    // ParseFile on a temp file) — assert no error and parsed Name matches.
    // If the parser API is not directly callable, write the content to a
    // temp dir under the layout skills.Discovery expects and run a
    // FileSource discovery; assert the skill is found with matching name.
}
```

**Step 2: Run test to verify failure**
Expected: FAIL

**Step 3: Write minimal implementation**

`assembleSkillContent(name, description string, tags []string, body
string) string` — YAML frontmatter (name, description, tags only when
non-empty) + "\n" + body. Keep description a single YAML-safe line
(quote when it contains `:` or leading specials).

**Step 4: Run test to verify pass**
Expected: PASS

### Task 3: Execute path through the Writer

**Objective:** Full Execute: validate, assemble, WriteSkill, return
created path.

**Files:**
- Modify: `internal/tools/builtin/skill_create_tool.go`
- Test: `internal/tools/builtin/skill_create_tool_test.go`

**Step 1: Write failing test**

```go
func TestSkillCreate_Execute_WritesViaLifecycleWriter(t *testing.T) {
    // Construct a real lifecycle.Writer pointed at t.TempDir() (read
    // writer_test.go for the minimum setup — likely just NewWriter).
    // Execute with valid args. Assert: no error; output mentions the
    // created path ~/.noop/<name>/SKILL.md within the temp dir; the file
    // exists on disk with expected content prefix.
}
func TestSkillCreate_Execute_DuplicateRejected(t *testing.T) {
    // Create once OK; create same name again -> error mentioning the
    // existing skill (writer's dedup or a registry check — see Task 4).
}
func TestSkillCreate_Execute_NilWriter(t *testing.T) {
    // nil writer -> error "skills writer unavailable" (not a panic).
}
```

**Step 2: Run test to verify failure**
Expected: FAIL

**Step 3: Write minimal implementation**

Execute: validate name/description/body -> assemble -> writer nil
check -> `writer.WriteSkill(name, content)` -> output: `created skill
"<name>" at <path>` (path from writer resolution; if the writer API
doesn't return a path, compute `<skillsDir>/<name>/SKILL.md` from the
same layout rules writer.go uses — read legacySkillPath/skillPath to
match exactly).

**Step 4: Run test to verify pass**
Expected: PASS

### Task 4: Duplicate detection via registry (SetRegistry)

**Objective:** When a registry is attached, reject creation of a name
that already exists in ANY tier with a helpful message (system tier
exists -> suggest patch or user-tier shadow... NO: same-name shadowing
is the discovery mechanism; a create that would shadow a higher-priority
tier skill must WARN but succeed, stating the shadow).

**Files:**
- Modify: `internal/tools/builtin/skill_create_tool.go`
- Test: `internal/tools/builtin/skill_create_tool_test.go`

**Step 1: Write failing test**

```go
func TestSkillCreate_Execute_ShadowWarnsButSucceeds(t *testing.T) {
    // SetRegistry with a registry containing "existing-skill" (read
    // registry_test.go for the minimal construction pattern; if heavy,
    // use the Discovery-over-tempdir approach instead).
    // Execute creating "existing-skill": success output must contain
    // "shadows" and the tier/source it shadows.
}
```

**Step 2: Run test to verify failure**
Expected: FAIL

**Step 3: Write minimal implementation**

SetRegistry (nil-guarded). On Execute, when registry non-nil, look up
the name (registry Get or List scan — match what registry.go exports);
if found, append shadow note with the found skill's Source/Priority to
the success output. Exact duplicate detection stays the writer's job
(WriteSkill dedups by SHA — verify in writer.go and mirror its
behavior in the error text).

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
- [ ] SetRegistry nil-guarded; no ignored errors; gofmt clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (signatures, types, file paths)
- [ ] Code follows project conventions (naming, error handling, structure)
- [ ] No bugs, no security issues (path traversal via name rejected by
      validateSkillName; no writes outside the skills dir)
- [ ] No scope creep beyond specified tasks (NO patch mode here — that
      is leaf 03; NO RPC handler; NO config changes)

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Writer construction-time nil tolerance is deliberate: daemon ordering
  (tools before skills) means the tool may be built before the writer
  exists. Execute-time nil check is the contract; leaf 05 resolves the
  ordering.
- Do not add confirmation gating inside the tool — risk classification
  (HIGH) and the confirmation flow are the security engine's job
  (wired in leaf 05). The tool stays a pure capability.
- If lifecycle.Writer.WriteSkill does not return the written path,
  prefer computing it from the writer's own path functions (exported
  or mirrored) over inventing layout rules.
