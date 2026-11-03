# 04 Skill Invocation (/name gesture) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The branch gate tests that touch this
> surface are in 00T-tests.md (T3b catalog credence + the e2e hot-reload case);
> the `/name` gesture's own assertions are unit tests you ADD here. Do NOT
> delete, skip, weaken, or rewrite any committed gate test (report
> suspected-wrong gate tests to the orchestrator instead). Do NOT commit - the
> orchestrator handles all git operations after review. Do NOT use read_file
> on existing source - explore with search_files or terminal cat. After
> writing a file, do NOT read it back - write once and stop. Report what you
> built, files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** The explicit `/<skill-name>` direct-invocation gesture: scan the
  user input, look up each matched name in the (now durable) skill catalog /
  loader, and inject the skill body as an explicit "user asked for this skill"
  instruction before the LLM call. Distinct from the existing named-agent
  route (master C9, dsh tool-skill /name).
- **Dependencies:** B3 (durable catalog + loader, from 03-skill-catalog.md),
  B4 (parent orchestrator); gate tests in 00T-tests.md
- **Estimated Context:** 16K
- **Concurrency Group:** A
- **Audit references:** /tmp/dsh-audit/skills-schedule-storage.md (1d /name)

## Goal

Give users a deterministic "run this skill now" affordance that bypasses fuzzy
routing: typing ` /name-of-skill` in a chat message makes that skill's body
appear as explicit instructions the model must follow, with NO possibility of a
fuzzy-keyword miss. Unknown or model-invocable-disabled names stay plain prose;
filesystem paths (`/usr/bin`) and fractions (`5/8`) never match. This is the
explicit slash gesture — the parent does NOT want it routed through
`named_agent_route.go`, which targets a named AGENT, not a skill.

## Context

- `internal/agent/context_injector.go` - where prompt context (incl. the new
  durable catalog from 03) is assembled. The `/name` scan runs at dispatch /
  context-build time over the user's claimed text INPUT ONLY (never tool or
  assistant content — external text must not forge the gesture, dsh 1d).
- `internal/agent/named_agent_route.go:8-24` - the named-AGENT route. It is a
  DISTINCT mechanism; `/name` must not reuse it. Note its bounded regex approach
  as the precedent for where the user-text scanner lives in the agent loop.
- `internal/skills/lazy_loader.go` - `Load(name)` (:73) resolves a skill body;
  `Get` (:214). The gesture "only the loaded-body lookup decides" (dsh): check
  existence + `DisableModelInvocation` on the LOADED definition, not on an
  unparsed token.
- `internal/agent/context_injector.go:115-135 `visibleSkillLines` (the C4 emit)
  + the model-invocability gate (`DisableModelInvocation`) — reuse that gate.

## Verified Anchors

- internal/agent/named_agent_route.go:8-24 - the DISTINCT named-agent route (do not reuse)
- internal/agent/context_injector.go:112-136 - visibleSkillLines + DisableModelInvocation gate
- internal/skills/lazy_loader.go:73/214 - Load / Get (the loaded-body lookup)
- internal/agent/context_injector.go - the turn-start context/injection seam where the gesture fires

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/agent/skill_invocation.go (NEW):
//  var skillGestureRe = regexp.MustCompile(`(^|\s)\/([a-z0-9]+(?:-[a-z0-9]+)*)(?=\s|$)`)
//  func invokedSkillNames(userText string) []string
//      // unique, first-seen order; kebab-case only
//  // At turn-start context build, before the LLM call:
//  // for each name in order:
//  //    skill, ok := loader.Load(name)
//  //    if !ok || skill.DisableModelInvocation { continue }   // stays prose
//  //    inject renderSkillContent(skill) as a user-role instruction block
//  //       ("user asked for this skill: <skill_name>") appended closest to the answer
// func renderSkillContent(s *skills.Skill) string   // reuse existing body-render if present else minimal
```

### What This Leaf Consumes

- B3 (parent, 03) - durable catalog supplies the DISCOVERABLE names; this
  leaf's `Load(name)` resolves the body. B4 (parent) - exact gesture shape
  above. No signature invented beyond B4.
- Consumers: tool-surface e2e (hot-reload case may assert a `/name` follow-on);
  the branch's Review Checklist.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|--------------|
| T1 | skillGestureRe matching | Create internal/agent/skill_invocation.go; Test internal/agent/skill_invocation_test.go | `go test -p 2 ./internal/agent -run SkillInvocation -v` | FAIL: no gesture regex/function | PASS: `/name`, ` use /x-y`, ` /a /b` all match; `/usr/bin`, `5/8`, `foo/bar`, `word/slashend` do not | Given a user message containing `use /review-code please`; When invokedSkillNames scans it; Then it returns ["review-code"] |
| T2 | loaded-body lookup decides | Modify internal/agent/skill_invocation.go | as T1 | FAIL: no lookup gate | PASS: a name with no loaded skill (unknown) or DisableModelInvocation stays prose (no injection, no error); a known user-invocable skill injects its rendered body | Given a user message `/private-skill` where that skill is disable-model-invocation; When the gesture resolves; Then no body is injected and the token remains plain text |
| T3 | Injection placement + source type | Modify internal/agent/skill_invocation.go + wire into the context builder | `go build ./...` + as T1 | FAIL: no injection | PASS: the rendered skill body is appended to the turn context as an explicit user-role block after other injections, closest to the answer, tagged as a direct skill invocation | Given a claimed user message with a known skill; When the context builds; Then a "user asked for this skill: <name>" block with the full body precedes the model's answer material |
| T4 | Durable-catalog names feed the gesture | Modify internal/agent/skill_invocation.go + context_injector.go (03 seam) | as T1 | FAIL: gesture can't see catalog names | PASS: the gesture resolves names against the (03) durable catalog discovery, so a skill that never had a fuzzy hit is still directly invokable by name | Given a skill present in the durable catalog but never cache-loaded; When `/name` is typed; Then Load(name) resolves it from the catalog-backed discovery and the body injects |

**Worked example - T1:**

T1: The gesture regex.
1. Write the test: for `"use /review-code please"` expect `["review-code"]`; assert `/usr/bin` and `5/8` are NOT extracted; assert `/a /b` yields `["a","b"]` (dedup, first-seen order).
2. Implementation: `skillGestureRe = regexp.MustCompile(`(^|\s)\/([a-z0-9]+(?:-[a-z0-9]+)*)(?=\s|$)`)` — the dsh grammar (packages/skill/tool-skill/src/index.ts:429). Group 2 is the name; leading whitespace/start required before `/`; trailing whitespace/end required after.
3. Verify: `go test -p 2 ./internal/agent -run SkillInvocation -v` -> PASS.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (own unit tests + gate catalog tests GREY/GREEN)
- [ ] Interface contract B4 satisfied exactly (gesture regex, loaded-body lookup, injection placement)
- [ ] `/name` is DISTINCT from named_agent_route; no reuse of the named-agent dispatch
- [ ] Scans user text input ONLY (no tool/assistant content)
- [ ] No gate test deleted/weakened; gate-test diff empty
- [ ] No deviations (or documented in the report)
- [ ] No scope creep - this is the gesture only; no new `skill` model tool, no catalog rewrite

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [the verify commands in Tasks; last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing, gate tests untouched
- [ ] B4 matches exactly (signatures, types, paths)
- [ ] Conventions followed; no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption
- [ ] User-only scan (source kind) — renamed to the input seam meept uses, never tool/assistant content

Output: APPROVED or specific gaps with file + line references.

## Notes

- The gesture must NOT intercept `/usr/bin` (single leading slash, no second token) — the regex word-boundary rule already handles it. Verify with hostile inputs in the test table.
- dsh pre-step semantics (packages/skill/tool-skill/src/index.ts:181-208): only customer/user messages scanned; unknown + user-disabled names stay prose; provider disables the gesture for non-user-invocable skills. Port faithfully.
- Injection placement ("agent/pre-step ordering... deterministic last injection before the catalog", dsh:181-208): meept's turn-start context seam is the natural binding point — the catalog from 03 and the /name body both enter there; keep the /name body closest to the answer.
