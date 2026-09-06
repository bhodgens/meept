# Linked Skill Assets - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files — explore with search_files or terminal cat.
> After writing a file, do NOT read it back to verify — write once and
> stop. After completing, report what you built, what files you touched,
> and any deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Skills gain `Dir` + `LinkedAssets`: the read-side exposes
  helper scripts/references/templates stored next to SKILL.md, and the
  executor injects the skill directory into execution context so agents
  can run those helpers via shell_execute.
- **Dependencies:** none
- **Estimated Context:** 45K
- **Concurrency Group:** A

## Goal

Hermes skills ship helper scripts (`scripts/fetch_transcript.py`) that
SKILL.md bodies reference via a SKILL_DIR convention. Meept's parser
reads only SKILL.md today; anything beside it is invisible. This leaf
makes directory-layout skills self-describing: discovery records the
skill dir and its linked assets; the executor tells the agent where
they are; existing tools (shell_execute) run them under existing risk
classification. This is what makes a shipped `learn-from-video` skill
able to carry its own helper script — and generally lets skills be
packages, not single files.

## Context

`internal/skills/discovery.go` scans tier directories
(`FileSource.scanTier` -> `loadSkillFile`): directory layout is
`<tier>/<name>/SKILL.md`, flat layout is `<tier>/<name>.md`. The
`Skill` struct (internal/skills/models.go:29) has Name/Description/
Requires/Tags/Examples/Body/Path/Priority. The executor
(internal/skills/executor.go `Execute`/`ExecuteWithMessages`) builds
the model input from `skill.Body`, translating Hermes tool references
(hermes_compat.go `TranslateToolReferences`).

Key files:
- internal/skills/models.go — Skill struct to extend
- internal/skills/discovery.go — scanTier/loadSkillFile population point
- internal/skills/parser.go — parsed metadata flow
- internal/skills/executor.go — context injection point
- internal/skills/discovery_test.go — fixture layout to extend

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/skills/models.go
package skills

type LinkedAsset struct {
    Name    string `json:"name"`    // basename
    RelPath string `json:"rel_path"` // "scripts/x.py" (slash-separated)
    Kind    string `json:"kind"`    // "script"|"reference"|"template"|"asset"
}

// Added to Skill:
//   Dir          string        `json:"dir,omitempty"`
//   LinkedAssets []LinkedAsset `json:"linked_assets,omitempty"`
// Dir = directory containing SKILL.md ("" for flat layout).

// internal/skills/executor.go: when skill.Dir != "", the composed
// execution context contains a line:
//   skill_dir: <Dir>
// and every "SKILL_DIR" literal in skill.Body is replaced with <Dir>
// (after Hermes tool-reference translation, same order as body
// processing today).
```

### What This Leaf Consumes

Nothing from siblings. Pure internal/skills package changes.

## Tasks

### Task 1: Struct fields + kind classification

**Objective:** Add LinkedAsset + the two Skill fields; implement
`classifyAsset(relPath) string`.

**Files:**
- Modify: `internal/skills/models.go`
- Test: `internal/skills/models_test.go` (create if absent — check
  with search_files first)

**Step 1: Write failing test**

```go
func TestLinkedAssetClassification(t *testing.T) {
    // scripts/x.py -> script; references/x.md -> reference;
    // templates/x.tmpl -> template; assets/x.png -> asset;
    // other/x.txt -> "" (ignored); SKILL.md itself -> "".
}
```

**Step 2: Run test to verify failure**
Run: `go test ./internal/skills/ -run TestLinkedAsset -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

Struct + `classifyAsset(relPath string) string` keyed on the first
path segment; anything outside the four known dirs returns "".

**Step 4: Run test to verify pass**
Expected: PASS

### Task 2: Discovery populates Dir + LinkedAssets

**Objective:** Directory-layout skills get Dir and a sorted
LinkedAssets scan; flat skills keep both empty.

**Files:**
- Modify: `internal/skills/discovery.go` (scanTier/loadSkillFile)
- Test: `internal/skills/discovery_test.go` (extend fixtures)

**Step 1: Write failing test**

Extend the existing directory-layout fixture (discovery_test.go
builds temp tier dirs — read it and reuse its helper):
- skill dir with SKILL.md + scripts/a.py + scripts/b.sh +
  references/notes.md + assets/logo.png + random/ignored.txt
- assert: skill.Dir == the temp skill dir; LinkedAssets has 4 entries;
  RelPaths slash-separated; list sorted by RelPath; random/ignored.txt
  absent.
- flat-layout fixture: Dir == "" and LinkedAssets nil.

**Step 2: Run test to verify failure**
Run: `go test ./internal/skills/ -run TestDiscovery -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

In the directory-layout branch (where SKILL.md is found inside a named
dir): set Dir to that dir; walk ONE level of subdirs (do not recurse
deeper — scripts/refs/templates/assets only), classify, append,
sort. Symlinks: skip (walk with d.Type() check). Errors reading a
subdir: log at debug (discovery has a logger on FileSource), skip the
subdir, never fail discovery.

**Step 4: Run test to verify pass**
Expected: PASS

### Task 3: Registry/Index passthrough

**Objective:** Registry and SkillIndex preserve the new fields (verify;
fix only if they strip or copy structurally).

**Files:**
- Modify: `internal/skills/registry.go` and/or `index.go` ONLY if
  they do field-by-field copies (read first; if they pass *Skill
  pointers through, no change needed)
- Test: extend `internal/skills/registry_test.go`

**Step 1: Write failing test**

```go
func TestRegistry_PreservesLinkedAssets(t *testing.T) {
    // Register a skill with Dir+LinkedAssets set; Get(name) returns
    // both intact.
}
```

**Step 2: Run test to verify failure**
Expected: FAIL only if a copy drops fields; if PASS with zero changes,
record that in Deviations ("registry passes *Skill through; no change
required") — that is a legitimate outcome of this task.

**Step 3: Write minimal implementation** (if needed)

**Step 4: Run test to verify pass**
Expected: PASS

### Task 4: Executor context injection

**Objective:** skill_dir line + SKILL_DIR substitution in the
execution input.

**Files:**
- Modify: `internal/skills/executor.go`
- Test: `internal/skills/executor_test.go` (extend; read existing
  fake-chatter pattern first)

**Step 1: Write failing test**

```go
func TestExecutor_InjectsSkillDir(t *testing.T) {
    // Executor with a fake Chatter (executor_test.go has the pattern)
    // executing a skill whose Dir=tempdir and whose Body contains
    // "run python3 $SKILL_DIR/scripts/x.py".
    // Assert the captured user/system input contains
    // "skill_dir: <tempdir>" and the SKILL_DIR literal was replaced
    // with tempdir. Flat skill: neither line nor substitution appears.
}
```

**Step 2: Run test to verify failure**
Run: `go test ./internal/skills/ -run TestExecutor_InjectsSkillDir -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

In Execute (and ExecuteWithMessages — both paths), after
`execBody := e.toolMapper.TranslateToolReferences(skill.Body)`:
if skill.Dir != "", strings.ReplaceAll(execBody, "SKILL_DIR",
skill.Dir) and prepend the `skill_dir: <Dir>` context line where the
executor already composes preamble content (read how it builds the
input messages around line 284 and follow the established shape).

**Step 4: Run test to verify pass**
Expected: PASS

### Task 5: gofmt + package green

gofmt on touched files; `go build ./internal/skills/... && go test
./internal/skills/ -count=1` — pass.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep - only what the tasks specify
- [ ] gofmt clean; discovery never fails on unreadable asset dirs

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (field names, JSON tags,
      executor line format `skill_dir: <Dir>`)
- [ ] Flat-layout skills byte-identical behavior (Dir "" -> no
      injection, no substitution)
- [ ] No bugs, no security issues (symlinks skipped; no recursion
      into arbitrary depths; JSON tags stable)
- [ ] No scope creep beyond specified tasks (NO writes to skill dirs;
      NO new tools; NO hermes_compat changes beyond the documented
      substitution ordering)

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- JSON tags matter: skills.list/skills.get RPC serialize Skill
  directly; `dir`/`linked_assets` become visible to CLI/TUI consumers
  automatically. Keep tags snake_case per existing fields.
- The executor substitution is plain-text ReplaceAll; skill bodies that
  mention SKILL_DIR in prose get substituted too — acceptable (matches
  Hermes semantics) and covered by the test.
- Do NOT touch internal/skills/lifecycle (the Writer creates files but
  linking assets into NEW skills is out of scope — a create-tool
  follow-up can add asset support later; note it in leaf 05 docs).
