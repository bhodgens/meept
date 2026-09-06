# orchestrator.md — 03-skill-tool-requirements branch

## Goal

Deliver Contract F from the parent master: skills declare
`requires-tools` in frontmatter; the executor checks availability before
execution and fails with a routed message. Implements workstream 3.

## Architecture Overview

Two leaves:

- 01-frontmatter-parse: parser extension (internal/context/skill_parser.go),
  Skill models (internal/skills/models.go, index.go), executor check
  (internal/skills/executor.go), tests. Wave 1.
- 02-skill-annotations: annotate bundled SKILL.md files that depend on
  tools + docs/workflows/skills.md section. Wave 2.

## Interface Contracts

### Contract F (verbatim from parent master — parse/executor half)

```go
// internal/context/skill_parser.go — extend skillFrontmatter:
    RequiresTools []string `yaml:"requires-tools"`
// surfaced as Skill.RequiresTools []string (models.go + index.go gain the field)
```

Semantics: each entry is a full registered tool name, server-qualified
(`cua-driver.capture`) or bare-builtin (`web_fetch`). Availability =
tool is in the live registry (or MCP manager AllTools for
server-qualified). Executor (internal/skills/executor.go) checks BEFORE
execution when `validatePrerequisites` is enabled; failure returns
ExecutorError with message:
`skill <name> requires unavailable tool(s): <list> — run 'meept doctor' to diagnose`
(lowercase per UI convention).

### Cross-leaf seam

The availability check must be injectable so leaf 01 tests don't need a
live registry:

```go
// internal/skills/executor.go
// ToolAvailabilityFunc reports whether a named tool is currently
// registered (bare name) or provided by an MCP server (server.tool).
type ToolAvailabilityFunc func(toolName string) bool

func (e *Executor) SetToolAvailability(fn ToolAvailabilityFunc) // typed-nil guard per AGENTS.md
```

Leaf 02 only writes SKILL.md annotations — it consumes nothing from the
Go code; the seam is the `requires-tools:` YAML key itself. Both leaves
must use the exact key spelling `requires-tools` (kebab, matching the
contract comment) — a spelling mismatch is an integration failure.

## Child Index

| Doc | Scope | Est. context | Dependencies |
|-----|-------|--------------|--------------|
| 01-frontmatter-parse.md | parser + models + executor check + tests | ~50K | none |
| 02-skill-annotations.md | SKILL.md + skills.md docs | ~25K | 01 |

Wave 1: 01. Wave 2: 02.

## Dispatch Protocol

Per parent master.md.

## Coding Conventions

Per parent master.md. Extra: the availability check runs only when
`validatePrerequisites` is enabled (the existing executor gate at
internal/skills/executor.go:204) — do NOT add a second config knob.
Skills with empty RequiresTools skip the check entirely (zero cost for
existing skills).

## Completion Tracking Table

| Doc | Status | Notes |
|-----|--------|-------|
| 01-frontmatter-parse.md | PENDING | |
| 02-skill-annotations.md | PENDING | blocked by 01 |

## Review Checklist (branch)

- [ ] Contract F verbatim: key spelling, message text, gating condition
- [ ] Typed-nil guard on SetToolAvailability
- [ ] All 14 bundled SKILL.md files still parse (existing tests green)
- [ ] Annotations only on skills whose tools can actually be unavailable
      (web-browsing → obscura.browser_*; computer-use → cua-driver.*;
      others reviewed and either annotated or justified in the leaf)
- [ ] go vet clean; no TODOs

## Integration Test Plan

`go test -p 2 ./internal/skills/ ./internal/context/ -count=1`; manual:
executor with a stub availability func returning false for a skill with
requires-tools → ExecutorError message matches Contract F exactly. Mark
branch COMPLETE in parent master.md after both leaves.
