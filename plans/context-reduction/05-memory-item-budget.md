# L05 — Whole-Item Memory Budgeting (P5)

**Proposal:** P5. **Depends on:** none. **Files (2):**
- Modify: `internal/agent/loop.go` (~:7163-7168 budget enforcement; recall path :7059, formatting :7559-7563)
- Test: new `internal/agent/loop_memory_budget_test.go` (or extend an existing loop test file)

**Problem:** When memory context exceeds `MaxMemoryContextTokens`
(conversation.go:33, value 2000), the code cuts the assembled string
proportionally: `memoryContext[:truncateLen] + "\n\n...[memory truncated due to
token budget]..."` (loop.go:7163-7168). That can leave a mangled half-memory in
the prompt AND wastes budget (cutting item 7 to 40% of its length spends tokens
on a useless fragment instead of dropping it and keeping item 8 whole).

**Contract:** budget enforcement drops WHOLE lowest-ranked items until the
estimated token count fits. Ranking = the order items were ranked by recall
(top-5 from loop.go:7059, already relevance-ordered). Never touch items that
fit. If the SINGLE largest item alone exceeds the budget, keep its existing
300-rune per-item truncation (loop.go:7562) as the last resort — that path is
already item-shaped.

### Tasks

**Task 1 — failing test:** `TestMemoryBudgetDropsWholeItems`: build a memory
context of 5 items where items 1-3 fit and item 4 pushes over budget; assert
the assembled context contains items 1-3 verbatim, does NOT contain item 4 or a
partial fragment of it, and estimated tokens ≤ budget. Add a second case: one
oversized item alone → 300-rune truncation path fires, not whole-item drop.
Run: `go test -p 2 ./internal/agent/ -run TestMemoryBudgetDropsWholeItems` — FAIL.

**Task 2 — implement:** replace the proportional cut in the budget-enforcement
site with a reverse-order whole-item drop loop over the structured items list.
If the current code path only has the joined string at that point, refactor
minimally: keep items structured until after budgeting, then join. Preserve the
fencing (`<memory-context>` wrapper, loop.go:7178-7186) and the existing
"...[memory truncated due to token budget]..." suffix appended when anything
was dropped (keeps observability).

**Task 3 — neighbors sweep:** check the sibling call sites the audit flagged
(`truncateForMemory(…, 500)` loop.go:7531) for the same proportional-cut
pattern; fix if identical, note if different. Fix the CLASS, not one site.

**Task 4 — full package:** `go test -p 2 ./internal/agent/` green.

**Task 5 — report.** Do NOT commit.

## Self-Verification Checklist
- [ ] No mid-string cut remains on the memory budget path
- [ ] Fencing + truncated-marker suffix preserved
- [ ] Per-item 300-rune truncation unchanged

## Review Checklist (for orchestrator)
- [ ] Item order preserved (relevance ranking intact)
- [ ] Sibling sites fixed or explicitly noted as different
