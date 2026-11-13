# 05 Trigger Wildcard Semantics Pin - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests ALREADY EXIST and are
> committed - your job is to make them pass (the semantics-pin gate test
> already passes today; you add the code comment and verify the pin stays
> green). Do NOT delete, skip, weaken, or rewrite any gate test. Do NOT
> commit - the orchestrator handles all git operations after review. Do NOT
> use read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once and
> stop.

## Meta

- **Parent:** master.md
- **Scope:** Document the exact-match semantics of escalation trigger `match` values and keep the semantics pin green. NO behavior change.
- **Dependencies:** 00T gate tests
- **Estimated Context:** 25K
- **Concurrency Group:** A
- **Audit references:** secretary constitution carries {on: action, match: "*"} — dead trigger; survey proved exact-match (enforcement.go:995)

## Goal

Make the '*' exact-match semantics explicit in code so a future contributor does not (a) assume it is a wildcard and ship config depending on that, or (b) "fix" it into a wildcard and turn every allowlisted employee call into a RequiresPlan livelock (approve -> execute -> re-escalate). The pin test (already committed by 00T) makes any semantic change loud.

## Context

- internal/employee/enforcement.go:990-1016 — matchesTrigger: `case EscalateOnAction: return trig.Match == action` (:995) — EXACT string equality. No wildcard expansion exists anywhere in the file; the ONLY '*' special-case is the tools_forbidden deny wildcard at enforcement.go:667. This asymmetry (one '*' semantics, not the other) is exactly why it must be written down.
- Live config: the secretary constitution carries escalation_triggers [{on:action, match:"voice_call"}, {on:action, match:"*"}] — the '*' row is inert today. Fine: the allowlist does the real gating; the row is documentation-in-config.
- NO behavior change in this leaf. Enforcement code edit = comment only.

## Verified Anchors

- internal/employee/enforcement.go:990-1016 - matchesTrigger - EscalateOnAction exact compare at :995
- internal/employee/enforcement.go:667 - tools_forbidden '*' wildcard - the deliberate asymmetry to cite in the comment
- internal/employee/enforcement.go:766-782 - step-3 trigger loop site - where the comment goes
- internal/employee/enforcement_trigger_test.go - gate pin file (committed by 00T): TestMatchesTrigger_ActionStarIsExactMatch / _ActionExactStillFires

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// Comment contract (C5) at the step-3 loop (enforcement.go:766-782) and on
// matchesTrigger itself:
//   "Escalation trigger match values are EXACT compares (tool name, action
//    string, risk_level token, cost threshold). Unlike tools_forbidden
//    (:667), '*' has NO wildcard meaning here — a trigger {on:action,
//    match:"*"} fires only for a literal action "*". Do not add wildcard
//    expansion without revisiting the allowlist interplay: with a
//    tools_allowed list, wildcard triggers would escalate every allowlisted
//    call (RequiresPlan livelock). Pinned by
//    TestMatchesTrigger_ActionStarIsExactMatch."
```

### What This Leaf Consumes

- Gate pin tests (committed, currently green)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add the exact-match comment at the step-3 trigger loop AND on matchesTrigger itself (cite :667 asymmetry + the livelock caution) | Modify: internal/employee/enforcement.go (comments ONLY at :766-782 and :990-1016) | `go build ./internal/employee/ && go vet ./internal/employee/` | n/a (comment-only) | builds clean | Given a future reader; When they open matchesTrigger; Then exact-match semantics + wildcard caution are stated |
| T2 | Verify the gate pins are green and UNMODIFIED | none | `go test -p 2 ./internal/employee/ -run TestMatchesTrigger_Action -v` + `git diff --stat` on the gate file | pins already green (declared) | GREEN, gate file diff empty | Given committed pins; When run; Then both pass, file untouched by this leaf |
| T3 | Docs note: one line in the constitution/workflow doc (docs/workflows/ or docs/configuration/ page documenting employee constitutions — locate it) stating trigger match values are exact, '*' has no wildcard meaning | Modify: matching docs page | grep -ri "escalation trigger" docs/ | doc silent on match semantics | documented | Given an operator writing triggers; When they read the doc; Then they know '*' is inert |

## Self-Verification Checklist

- [ ] ZERO behavior change — `git diff` shows only comments + docs
- [ ] Comment text covers: exact compare, ':667 asymmetry', livelock caution, pin test name
- [ ] Gate file untouched; pins green
- [ ] `go build ./...` clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Comments only — no code semantics touched
- [ ] Comment includes the pin test name and the livelock rationale
- [ ] Docs page addition is one focused section, not a rewrite

Output: APPROVED or specific gaps with file + line references.

## Notes

- If a future tree DOES want wildcard triggers, it must come with: config validation distinguishing wildcard syntax, a allowlist-interplay analysis, and the livelock guard (C1 once-map helps but does not fully cover re-escalation loops). That is out of scope here by design.
- Operator follow-up (NOT this leaf): consider narrowing the live secretary's '*' trigger row to explicit out-of-charter actions, or deleting the row as dead config.
