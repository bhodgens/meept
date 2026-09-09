# Leaf 04 — Documentation: routing reference + workflow doc

## DISPATCH INSTRUCTION

> **Implementing agent:** implement ALL tasks below. Do NOT commit.
> Verify cross-references resolve to existing files; confirm no broken
> relative links; ASCII only. Never feed read_file output into write_file.

- **Parent:** docs/plans/quickplan-mode/master.md
- **Scope:** user-facing routing documentation for quickplan across the
  docs tree
- **Dependencies:** 01, 02 (documents what was built)
- **Estimated context:** ~35K

## Tasks

### Task 1: Update docs/workflows/intent-routing.md

The file exists (authored during the classifier campaign) with a
`quickplan` row in the decision table and `quickplan` as the final
fall-through. Bring it to match the IMPLEMENTED behavior:

1. In the routing pipeline diagram, confirm the final fall-through line
   reads `quickplan (clarify → plan → execute)` — it already does;
   verify the surrounding prose states the clarify-first behavior
   explicitly (ambiguity gate runs before execution; clarification
   answers resume quickplan mode).
2. Add a dedicated `## QuickPlan` section after the decision table:
   - What it is: plan, clarify-if-ambiguous up front, then execute
     autonomously to completion. No approval pauses after clarification.
   - The cue rule: a quickplan classification requires orchestration
     evidence (subagents, task lists, waves/leaves, plan.md references,
     correction clauses, multi-step sequences). Without it, the message
     falls to the LLM chain. Rationale: quickplan-vs-code is a session-
     state judgment; the orchestrator makes it at execution time using
     active-plan/task context.
   - Three worked examples: "review the daemon for bugs and correct them
     as you find them" (quickplan), "review the json files for
     completeness" (review — verdict only), "add pagination to the API"
     (code).
3. Verify the research/analyze/search rows match: `research` = open
   survey; `analyze` = judgment against testable criteria; `search` =
   locate references.

### Task 2: Update docs/workflows/classifier-prefilter.md

Add a quickplan section: the class exists in the centroid index; the
prefilter's quickplan direct-route requires the orchestration cue
(QuickPlanCuePattern); without the cue a quickplan vote falls through to
the LLM chain. Reference internal/agent/quickplan_cue.go. Note the
centroid store rebuild command. If the file documents the k=5-unanimity
head, add a one-line note that the centroid-margin champion from the
classifier campaign is the tracked M4 wiring candidate — do NOT describe
it as shipped.

### Task 3: AGENTS.md review (required by repo policy)

Read AGENTS.md. If the dispatcher behavior change (final fallback →
quickplan, new intent type) invalidates any statement, update AGENTS.md
in the same change. Most likely: nothing needed (AGENTS.md does not
document the intent list) — record the check as done either way in the
leaf report.

### Task 4: Cross-reference sweep

```bash
grep -rn "falls? back to chat\|fallback to chat" docs/ --include="*.md" | grep -v generated
```

Any user-facing doc still claiming plain-chat final fallback gets a
one-line correction to quickplan (with clarify-first). Internal/history
docs (plans/, audits) are NOT touched.

## Self-Verification Checklist

- [ ] intent-routing.md QuickPlan section present; three examples correct
- [ ] classifier-prefilter.md cue-guard section present
- [ ] AGENTS.md reviewed (updated or explicitly unchanged)
- [ ] Fallback-correction sweep done; only user-facing docs touched
- [ ] Every relative link resolves; ASCII only; no corruption markers

## Review Checklist (orchestrator)

- [ ] Docs match the IMPLEMENTED contracts (not the design aspiration)
- [ ] quickplan clarify-first behavior stated explicitly
- [ ] Cue rule documented with rationale
- [ ] No private data (adjudication corpus stays untracked/unreferenced
      by path)
