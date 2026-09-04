# Skill Parser List-Field Tolerance - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** The skill metadata parser accepts comma-separated string scalars for list fields, and Claude-source parse warnings dedupe so repeated scans stay quiet.
- **Dependencies:** none
- **Estimated Context:** 25K
- **Audit references:** finding F8 (12,760 identical "Failed to parse Claude skill file" warnings in one day; root cause: `allowed-tools: Read, Grep, Glob, Bash, Agent` scalar → []string type error)

## Goal

Claude-format skills use comma-separated scalars for list fields
(`allowed-tools: Read, Grep, Glob, Bash, Agent`). gopkg.in/yaml rejects a
string→[]string decode, so ParseSkillText fails with ErrInvalidYAML and
source_claude.go logs a WARN — for every skill, on every discovery scan,
thousands of times per day. This leaf makes list fields accept both real
YAML lists and comma-separated scalars, and demotes repeat warnings.

## Context

Parse pipeline: source_claude.go loadAndAdapt (line 136) → ParseSkillFile
(internal/skills/parser.go:141) → ParseSkillText (parser.go:159) →
parseMetadata (parser.go:288) → yaml.Unmarshal into SkillMetadata
(internal/skills/models.go:140). List-typed fields: Requires, Tags,
Examples, AllowedTools, Triggers, MCPServers (leave MCPServers strict —
objects, not scalars). Warning emission: source_claude.go:115.

Key files to understand before implementing:
- internal/skills/models.go:140-164 - SkillMetadata yaml tags
- internal/skills/parser.go:288-306 - parseMetadata (two-pass: primary + alt-name struct)
- internal/skills/source_claude.go:95-125 - discovery loop + warning site
- internal/skills/parser_test.go (if present) - test conventions

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/skills/models.go
//   Requires/Tags/Examples/AllowedTools/Triggers accept BOTH:
//     allowed-tools: [Read, Grep]         (YAML list, unchanged)
//     allowed-tools: Read, Grep, Glob     (comma-separated scalar, NEW)
//   via custom UnmarshalYAML on a stringList type (or equivalent
//   normalization in parseMetadata — implementer's choice, keep one).
// internal/skills/source_claude.go
//   Parse-failure warns dedupe per path within a ClaudeSource instance:
//   first occurrence WARN, subsequent occurrences Debug. Map guarded by
//   mutex (scans can run concurrently).
// Owner: 08. Consumers: none (leaf-independent).
```

### What This Leaf Consumes

```
// gopkg.in/yaml.v3 (already a dependency of internal/skills)
```

## Tasks

### Task 1: comma-separated scalar tolerance

**Objective:** List fields decode from comma-separated scalars.

**Files:**
- Modify: `internal/skills/models.go` (add stringList type + UnmarshalYAML;
  swap field types Requires/Tags/Examples/AllowedTools/Triggers to stringList)
  OR `internal/skills/parser.go` (pre-decode normalization) — pick ONE,
  document in Deviations. stringList must remain assignment-compatible
  with []string consumers: define `type stringList []string` with
  `UnmarshalYAML(value *yaml.Node) error` that accepts a sequence node or
  a scalar node (split on ',' + TrimSpace each; drop empties).
- Test: `internal/skills/parser_test.go` (or models_test.go where field
  parsing is tested)

**Step 1: Write failing test**

```go
func TestSkillMetadata_CommaSeparatedListFields(t *testing.T) {
	fm := "name: security-audit\n" +
		"description: audit\n" +
		"allowed-tools: Read, Grep, Glob, Bash, Agent\n" +
		"triggers:\n  - \"/security-audit\"\n  - \"security scan\"\n"
	var meta SkillMetadata
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"Read", "Grep", "Glob", "Bash", "Agent"}
	if !reflect.DeepEqual([]string(meta.AllowedTools), want) {
		t.Errorf("AllowedTools = %v, want %v", meta.AllowedTools, want)
	}
	if !reflect.DeepEqual([]string(meta.Triggers), []string{"/security-audit", "security scan"}) {
		t.Errorf("Triggers (list form) = %v", meta.Triggers)
	}
}
```

**Step 2: verify failure** — `go test -p 2 ./internal/skills/ -run TestSkillMetadata_CommaSeparatedListFields -v` → FAIL (cannot unmarshal string into []string).

**Step 3: implement** the chosen mechanism.

**Step 4: verify pass** — same run → PASS; also run the whole package:
`go test -p 2 ./internal/skills/` → PASS.

### Task 2: end-to-end parse of a Claude skill

**Objective:** A skill file with scalar allowed-tools parses via ParseSkillText without error.

**Files:**
- Test: `internal/skills/parser_test.go`

**Step 1: Write failing test** — inline text mirroring
~/.claude/skills/security-audit/SKILL.md frontmatter (name, block-scalar
description with `|`, triggers list, comma allowed-tools); assert
ParseSkillText returns a skill with Name "security-audit" and 5 AllowedTools.

**Step 2: verify failure → implement nothing new (Task 1 fix should make it pass) → verify pass.**
If it fails, fix Task 1's implementation, not the test.

### Task 3: warning dedupe in ClaudeSource

**Objective:** Repeat parse failures for the same path log WARN once, then Debug.

**Files:**
- Modify: `internal/skills/source_claude.go` (ClaudeSource struct + warning site 114-120)
- Test: `internal/skills/source_claude_test.go` (or nearest existing file)

**Step 1: Write failing test** — build ClaudeSource with a logger writing
to a buffer (check how source_claude tests construct it; if none, add a
small one); scan a temp dir containing one bad skill twice; assert the
buffer contains exactly one WARN-level line and >=1 Debug-level line for
the path.

**Step 2: verify failure** → FAIL (two WARN lines).

**Step 3: implement** — add `warnedMu sync.Mutex; warned map[string]struct{}`
to ClaudeSource (initialize lazily or in the constructor — check how
ClaudeSource is constructed in the daemon and keep nil-safety); at the
warning site: first time `logger.Warn(...)`, otherwise `logger.Debug(...)`.

**Step 4: verify pass** — `go test -p 2 ./internal/skills/ -run 'Claude' -v` → PASS.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or documented below)
- [ ] No scope creep
- [ ] MCPServers field left strict
- [ ] Full `go test -p 2 ./internal/skills/` passes

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Both scalar and list forms decode for the five list fields
- [ ] Empty segments dropped, whitespace trimmed
- [ ] Warning dedupe is per-path, mutex-guarded, nil-safe
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Do NOT edit files under ~/.claude/skills — they are another tool's data;
  meept must tolerate them.
- stringList in models.go is preferable (keeps parseMetadata two-pass
  structure); if the alt-name pass (parser.go:298-306) conflicts, normalize
  THERE and keep []string fields — either is contract-satisfying.
- The unused-stringList-method lint: ensure the UnmarshalYAML method is
  referenced by the yaml package decode path (it will be, via the type).
