# leaf 03-skill-tool-requirements/01 — requires-tools parse + executor check

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task with TDD. **Do NOT
commit. Do NOT run `git add`.** Write code, run tests, report results.

- Parent: docs/plans/20260905-dependency-visibility/03-skill-tool-requirements/orchestrator.md
- Scope STRICTLY: internal/context/skill_parser.go + skill_parser_test.go,
  internal/skills/models.go, internal/skills/index.go,
  internal/skills/executor.go + executor_test.go. No other files.
- Dependencies: none (Wave 1).
- Estimated context: ~50K.

## Contract F (verbatim from parent master — parse/executor half)

```go
// internal/context/skill_parser.go — extend skillFrontmatter:
    RequiresTools []string `yaml:"requires-tools"`
// surfaced as Skill.RequiresTools []string (models.go + index.go gain the field)
```

Executor check (gated on the existing validatePrerequisites flag,
internal/skills/executor.go:204 — no new config knob):

```go
// internal/skills/executor.go
// ToolAvailabilityFunc reports whether a named tool is currently
// registered (bare name) or provided by an MCP server (server.tool).
type ToolAvailabilityFunc func(toolName string) bool

func (e *Executor) SetToolAvailability(fn ToolAvailabilityFunc) // typed-nil guard per AGENTS.md
```

Failure (before execution, only when RequiresTools non-empty and the
checker is set): ExecutorError, message exactly
`skill <name> requires unavailable tool(s): <missing-list> — run 'meept doctor' to diagnose`
(join multiple missing with ", ").

## Tasks

### Task 1: read first

internal/skills/executor.go fully (the validatePrerequisites block ~:199-210
shows where your check goes), internal/context/skill_parser.go
(skillFrontmatter ~:110), models.go Skill struct, index.go entry struct.
Also grep how index entries are serialized (JSON tags) to mirror the new
field.

### Task 2: TDD parser (skill_parser_test.go)

- frontmatter with requires-tools: [web_fetch, cua-driver.capture] →
  Skill.RequiresTools both entries, order preserved.
- absent → nil/empty (existing skills unaffected).
- malformed (requires-tools: "web_fetch" single string) → yaml unmarshal
  error or graceful string-wrapping; pick yaml's behavior, assert it,
  report what you chose.

### Task 3: TDD executor (executor_test.go)

Seam-injected ToolAvailabilityFunc; table:
1. no requires-tools → executes, checker never called (count invocations).
2. requires-tools all available → executes.
3. one missing → ExecutorError, message EXACT per contract (missing only,
   not all).
4. multiple missing → both listed, ", "-joined.
5. checker nil + requires-tools set → check skipped (backward compat:
   executor constructed without the setter must not regress existing
   skill execution).
6. validatePrerequisites disabled → check skipped even when checker set.
7. typed-nil guard: SetToolAvailability(nil) after setting a real fn →
   real fn retained.

### Task 4: implement

- parser field + models field (+ index.go JSON tag `requires_tools,omitempty`
  — verify index serialization test patterns and mirror).
- executor: SetToolAvailability with typed-nil guard; availability check
  placed inside the existing validatePrerequisites gate, BEFORE
  CheckPrerequisites or after (read the flow; place so a missing tool
  fails before any prerequisite side effects; justify placement in report).

### Task 5: verify

```
go build ./internal/...
go vet ./internal/skills/ ./internal/context/
go test -p 2 ./internal/skills/ ./internal/context/ -count=1 -timeout 180s
gofmt -l internal/skills/ internal/context/
```

## Self-Verification Checklist

- [ ] Key spelling `requires-tools` exact (kebab) in yaml tag
- [ ] Message text byte-verbatim per contract incl. em-dash
- [ ] All 7 executor cases + 3 parser cases pass
- [ ] Zero behavior change for skills without requires-tools
- [ ] gofmt/vet clean; no TODOs; no line-number prefixes

## Review Checklist (for orchestrator)

- [ ] Gating inside existing validatePrerequisites (no new knob)
- [ ] Typed-nil guard tested
- [ ] index.go field serialized consistently with existing fields

Do NOT commit.
