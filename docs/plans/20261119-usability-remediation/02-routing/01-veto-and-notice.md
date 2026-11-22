# Git-Veto Widening + Routing Notice + Hint Fit - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T1/T2) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Biased git verdicts vetoed regardless of input shape; degradation disclosed; research hints fit capabilities
- **Dependencies:** 00T-gate.md
- **Estimated Context:** 70K
- **Concurrency Group:** A
- **Audit references:** W8, W22; F-ORCH-2/20; audit-routing R1/R2/R4/R6

## Goal

Drop the leading-imperative requirement from the git-verb agreement veto
(the lexical bias is shape-independent); populate ClassificationNotice on
every fallback lane; validate planner tool_hint against step capability fit.

## Context

- internal/agent/dispatcher.go ~1775-1796 - veto condition: IntentGit &&
  !inputContainsGitVerb && hasLeadingImperativeVerb. F41 branch order:
  recall matchers FIRST - keep it.
- internal/agent/dispatcher.go:1870-1982 - classifyIntent fallback lanes
  (llm_empty_fallback_chat, keyword, semantic, heuristic, fallback).
- internal/agent/dispatcher.go:231-234 - ClassificationNotice field;
  handler.go:1049/1258 read it (dead today).
- internal/llm/errors.go:591-606 - ClassificationUserGuidance(err) helper.
- internal/agent/strategic.go - planner hint acceptance;
  internal/config/tool_hints.go:55-80 - hint->agent table;
  config/agents/*/AGENT.md - capability lists (researcher HAS web_fetch+file_write).
- internal/agent/llm_classifier.go:585-591 - cooldown arming (notice on
  cooldown path too).

## Verified Anchors

- internal/agent/dispatcher.go:1777 - hasLeadingImperativeVerb in veto
- internal/agent/dispatcher.go:231 - ClassificationNotice
- internal/llm/errors.go:591 - ClassificationUserGuidance
- internal/config/tool_hints.go:62 - research->researcher mapping

## Interface Contracts (From Parent)

### What This Leaf Exposes
- C1 routing_notice key (declare in schemas/ws_events.schema.json).
- routing.warning telemetry on hint mismatch (existing telemetry seam).

### What This Leaf Consumes
- C1 (../master.md).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Widen veto | Modify: internal/agent/dispatcher.go veto condition (~1775-1796) | `go test ./internal/agent/ -run TestGitVetoShapeIndependent -count=1` | gate T1 RED | GREEN | Given git verdict + no git verb on ANY shape; Then vetoed; genuine git verb inputs untouched; recall priority preserved |
| T2 | Hint capability fit | Modify: internal/agent/strategic.go hint acceptance (validate vs agent tool lists) | `go test ./internal/agent/ -run TestHintCapabilityFit -count=1` | gate T2 RED | GREEN | Given research-shaped step hint=code; Then remapped to researcher or mismatch warning emitted; never silent coder-without-web_fetch |
| T3 | Notice population | Modify: internal/agent/dispatcher.go fallback lanes + llm_classifier cooldown path; handler.go notice->reply/terminal wiring; schemas/ws_events.schema.json | `go test ./internal/agent/ -run TestNoticePopulated -count=1` | gate T2 RED (zero writers) | GREEN | Given classifier failure/cooldown; Then notice set from ClassificationUserGuidance, rendered in terminal routing_notice + brief reply suffix |

## Self-Verification Checklist

- [ ] Gate T1/T2 GREEN; gate diff empty
- [ ] Frozen p1/p6 prompts route coder on rig (live-gate); regression routing tasks green
- [ ] C1 routing_notice declared; no over-veto of genuine git prompts
- [ ] No scope creep / artifacts / corruption

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Live-gate evidence. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] F41 branch order intact (recall first)
- [ ] Notice text user-language, brief, never raw error dumps
- [ ] Hint remap uses the authoritative table (no drift)
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- Classifier PROMPT is not changed here (dispatcher-side logic) - GEPA gate
  not required. If a future leaf changes the prompt, run tools/gepa-tune --run.
