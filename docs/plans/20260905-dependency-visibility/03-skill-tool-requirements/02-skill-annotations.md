# leaf 03-skill-tool-requirements/02 — annotate bundled skills + docs

## DISPATCH INSTRUCTION

You are an implementation agent. Make focused doc edits. **Do NOT
commit. Do NOT run `git add`.**

- Parent: docs/plans/20260905-dependency-visibility/03-skill-tool-requirements/orchestrator.md
- Scope STRICTLY: config/skills/*/SKILL.md (frontmatter additions only)
  + docs/workflows/skills.md (one section). No Go files.
- Dependencies: leaf 01 COMPLETE (key `requires-tools` is parseable).
- Estimated context: ~25K.

## Task 1: inventory tool dependencies per skill

Read every SKILL.md under config/skills/ (14 skills). For each, decide:
does this skill's body reference MCP-server tools (server.tool names)
or rely on specific builtin tools being present?

Known ground truth (verify against bodies, don't trust this list):
- web-browsing: body escalates to `obscura.browser_navigate` /
  `obscura.browser_snapshot` → requires-tools: [obscura.browser_navigate]
- computer-use: allowed-tools list is all cua-driver.* → requires-tools:
  [cua-driver.capture] (one representative tool is enough — the server
  being up makes all of them available; annotate the FIRST tool the
  skill's flow uses)
- others: most are pure-reasoning (requires: [reasoning] only). If a
  body references a concrete tool at its core, annotate; if it merely
  mentions a tool as an option, leave unannotated. Record your per-skill
  verdict (annotated/not + why) in the report — the orchestrator uses
  this as the review diff.

## Task 2: annotate

Insert into each chosen SKILL.md frontmatter, after the `requires:` key:

```yaml
requires-tools:
  - <server.tool>
```

Kebab key exact. No other frontmatter churn. Do not touch bodies.

## Task 3: document

docs/workflows/skills.md — new `### Tool requirements` section (near the
existing frontmatter documentation wherever that lives in the file; read
first): what the key is, matching semantics (bare = builtin, server.tool
= mcp), failure behavior (routed error telling the user to run
`meept doctor`), and that skills without the key are unaffected.

## Task 4: verify

```
grep -c "requires-tools:" config/skills/*/SKILL.md   # only the annotated ones > 0
go test -p 2 ./internal/skills/ ./internal/context/ -count=1 -timeout 180s   # all SKILL.md still parse
```

## Self-Verification Checklist

- [ ] web-browsing + computer-use annotated; verdicts for all 14 recorded
- [ ] Key spelling kebab-case; placement after requires:
- [ ] skills.md section accurate to Contract F behavior
- [ ] All skills still parse (tests green); no body edits

## Review Checklist (for orchestrator)

- [ ] Per-skill verdict list sane (no mass-annotation of reasoning skills)
- [ ] Diff = N frontmatter insertions + one docs section

Do NOT commit.
