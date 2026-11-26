# Leaf 04 — Surfaces, docs, e2e for the plan-contract quickplan

Contract: consumes C-1..C-4. Wave C (after 01, 02, 03).

## Deliverables

1. Surfaces: quickplan ack renders the brief plan (TUI chat card +
   GUI bubble parity per AGENTS.md rule); plans list shows quickplan
   rows advancing executing->completed (they are real rows now —
   verify existing TUI/GUI render paths need no change; if the ack's
   subtask list shape changed, adjust chat.go/formatTaskCompletedMessage
   consumers' tests, not product logic, unless a real gap).
2. docs/workflows/plans.md: quick_plan section rewritten — plan row is
   the contract, one-or-more tasks, rollup completion, oneshot default,
   approval_requested exception. docs/plans/quickplan-mode/master.md:
   add an AMENDED-BY pointer to this tree's Contract 2 note.
3. e2e/suites/plan-lifecycle new scenarios (manifest entries required —
   run the registry self-tests):
   - dispatched quick_plan: plan row reaches completed; exactly one
     primary task; PruneStalePlans later cancels NOTHING (no zombies).
   - compound "plan X then implement it" same assertions.
   - "plan it but implement only after I approve" -> row parks
     pending_approval; /plan confirm drives execution; completes.
   - adjudication outage (fake LLM error on the veto-suspect slot) ->
     oneshot proceeds, turn never parks.

## Files owned

docs/*, e2e/suites/plan-lifecycle/*, e2e/manifest.json (scenario +
path_map entries), internal/tui test pins if surfaces need assertion
updates ONLY.

## Gates

make e2e-area AREA=plan-lifecycle (or the repo's suite command);
manifest self-tests green; gofmt/docs lint gates. ONE commit naming
M11/H6/M6 + surfaces.
