# Skill root resolution for writer/archive - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Discovery-tier resolution for skill paths, wired into the
  Writer/Versioner so archive and refine operate inside the tier that
  actually contains the skill.
- **Dependencies:** none
- **Estimated Context:** 70K
- **Concurrency Group:** A

## Goal

Discovery spans five tiers — `.meept/skills` (project), `~/.meept/skills`
(user), `~/.config/meept/skills` (system), `~/.hermes/skills` (hermes), and
`~/.claude/skills` (ClaudeSource; internal/skills/discovery.go:36-50, :28).
The daemon constructs Writer+Versioner at
`filepath.Join(cfg.Daemon.DataDir, "skills")` = `~/.meept/skills`
(internal/daemon/components.go:1039-1040) — **a directory that does not
exist on this machine.** The evolver-managed skills actually live in
`~/.claude/skills/` (189 dirs). `Writer.ArchiveSkill`
(internal/skills/lifecycle/writer.go:309) would fail with "skill not found."
Fix root resolution at the source: the Writer resolves each skill's actual
discovery tier and edits/moves within it. No per-call-site special cases.

## Context

Verified facts (2026-08-31, do not re-derive):

- `internal/skills/discovery.go:36-50` — the tier list; `:28` — ClaudeSource
  for `~/.claude/skills`.
- `internal/daemon/components.go:1039-1040` — Writer+Versioner constructed
  at `cfg.Daemon.DataDir + "/skills"`.
- `internal/skills/lifecycle/writer.go:309` — `ArchiveSkill`.
- Reality: `~/.meept/skills` does not exist; `~/.claude/skills` holds the
  189 managed skill dirs.

Key files to understand before implementing:

- `internal/skills/discovery.go` — Discoverer API, source types, precedence
  order, the per-skill metadata it returns (does a discovery result already
  carry its absolute path or source? if yes, reuse it — smallest possible
  change).
- `internal/skills/lifecycle/writer.go` — Writer struct/constructor, how it
  joins its root, ArchiveSkill's move semantics, refine/edit paths.
- `internal/daemon/components.go` around :1030-1050 — construction order
  and what's available there (discovery instance? config tiers?).
- The evolver's calls into Writer (internal/skills/lifecycle/evolver.go) —
  what identity (name? path?) it passes today.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/skills/discovery.go (or a sibling file in the package) — tier
// lookup consistent with discovery precedence:
//   ResolveTierPath(skillName) -> (tierRoot, skillPath, source, error)
//   - same tiers, same precedence as discovery
//   - error when the skill exists in no tier
// (Adapt receiver/name to the actual Discoverer API during implementation;
// state the final signature in your report.)

// internal/skills/lifecycle/writer.go — Writer (and Versioner, constructed
// together at components.go:1039-1040) resolves the tier before any
// read/write/move. ArchiveSkill keeps its current archive semantics and
// naming; ONLY the root resolution changes: operate inside the tier that
// actually contains the skill (e.g. ~/.claude/skills/<name> on this
// machine), never the fixed ~/.meept/skills root.
```

### What This Leaf Consumes

```
// internal/skills/discovery.go — tier definitions + precedence (:28, :36-50)
// internal/skills/lifecycle/writer.go — Writer root usage + ArchiveSkill (:309)
// internal/daemon/components.go:1030-1050 — construction path
```

## Tasks

### Task 1: ResolveTierPath on discovery

**Objective:** A lookup that returns the tier actually holding a skill.

**Files:**
- Modify: internal/skills/discovery.go (or sibling file)
- Test: `internal/skills/discovery_test.go` (or sibling `_test.go`)

**Step 1: Write failing test**

Use temp-dir HOME fixtures ONLY (never the developer's home):

- Skill present only in the claude tier → resolved there.
- Skill present in both user and claude tiers → precedence decides
  (same precedence discovery uses — read it, don't guess).
- Skill in no tier → error wrapping a sentinel (e.g. ErrSkillNotFound)
  with the skill name.
- Resolution is stable across repeated calls (pure lookup).

**Step 2: Run test to verify failure**
Run: `go test ./internal/skills/ -run TestResolveTierPath -v`
Expected: FAIL (undefined)

**Step 3: Write minimal implementation**

If discovery results already carry absolute paths/sources, derive the
lookup from that — do not duplicate the tier list a second time.

**Step 4: Run test to verify pass**
Run: `go test ./internal/skills/ -run TestResolveTierPath -v`
Expected: PASS

### Task 2: Writer resolves the tier

**Objective:** ArchiveSkill (and the refine/edit path) operate inside the
resolved tier.

**Files:**
- Modify: internal/skills/lifecycle/writer.go
- Test: `internal/skills/lifecycle/writer_test.go` (or sibling)

**Step 1: Write failing test**

Fixture HOME with a skill in a NON-default tier:

- ArchiveSkill archives a skill living in `~/.claude/skills/<name>` —
  succeeds, archive output lands per Writer's existing naming/location
  semantics, and `~/.meept/skills` is never created or consulted.
- ArchiveSkill on a skill in NO tier → clean error naming the skill.
- Refine/edit path (locate the Writer's write/refine method): edits land in
  the resolved tier file, not a path under DataDir.

**Step 2: Run test to verify failure**

**Step 3: Write minimal implementation**

Resolution happens once per operation at the top (not per join). Keep
ArchiveSkill's public signature stable unless the tier context must flow in
— if a signature change is unavoidable, keep it additive (options/field)
and note the deviation.

**Step 4: Run test to verify pass**
Run: `go test ./internal/skills/lifecycle/ -run TestWriterTier -v`
Expected: PASS

### Task 3: Daemon construction unchanged but honest

**Objective:** components.go keeps constructing Writer/Versioner, and the
hard-coded root stops being load-bearing.

**Files:**
- Modify: internal/daemon/components.go only if the Writer constructor
  needs the discovery instance/config tiers passed in
- Test: covered by Task 2 tests + a construction smoke assertion if cheap

**Step 1: Write failing test (conditional)**

If the Writer gains a dependency (discovery or tier resolver), assert the
daemon construction path supplies it. If no signature change was needed,
skip this task and say so in deviations.

**Step 2/3/4: implement and verify per the TDD loop**

Run: `go build ./... && go test ./internal/daemon/ -run TestComponents -v`

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep — only what the tasks specify
- [ ] No hard-coded `~/.meept/skills` or `~/.claude/skills` literals remain
      in product code (grep to confirm)
- [ ] Tier precedence matches discovery exactly (no second source of truth)
- [ ] Fixture tests use temp HOMEs — the developer's real `~/.claude/skills`
      (189 live dirs) is never touched

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match (tier resolution consistent with discovery
      precedence; Writer operates in-tier)
- [ ] Existing discovery tests green and unmodified
- [ ] Existing Writer/Versioner behavior for skills that DO live in the
      default tier unchanged (regression tests green)
- [ ] No duplicated tier list; resolution derives from discovery's data
- [ ] No scope creep beyond specified tasks

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Clean-fix rule (AGENTS.md): fix root resolution at the source (this
  leaf), not with per-tier conditionals at archive call sites. If you feel
  the urge to add `if tier == claude` anywhere outside resolution, stop and
  restructure.
- The archive destination layout stays whatever Writer does today — this
  leaf fixes WHERE the skill is found, not the archive layout. If the
  archive path encoding is welded to the DataDir root and cannot be
  parameterized cleanly, STOP and escalate (per master.md Open Questions).
- Discovery precedence is the single source of truth for ordering. Writer
  must never outrank discovery on where a skill "really" lives.
