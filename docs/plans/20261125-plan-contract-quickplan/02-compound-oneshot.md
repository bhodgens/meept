# Leaf 02 — Compound intents ride the quickplan lane (oneshot)

Contract: master.md C-4 + C-2 (compound half). Decision D4.

## Deliverables

1. dispatcher: IntentCompound / IsCompound inputs route through the
   quick_plan branch (post-leaf-01 shape). spec_pair dispatch lane in
   strategic.go (shouldUseFlow "spec_pair" case ~:1365) no longer
   reached from compound intent; keep pair-session as an EXECUTOR
   concern (steps may still link pair sessions inside synthesized
   tasks — verify, do not assume).
2. Delete the dead veto wiring: req.AutoSeal set at handler.go ~:2158
   (BLOCKED: handler.go carries sibling uncommitted edits — coordinate;
   if blocked at merge time, land the dispatcher half and leave the
   deletion as a listed follow-up), PlanAutoSealFromIntent + its regexes
   (strategic.go ~:2470-2500) move to leaf 03's adjudication replacement.
3. Ack parity: compound turns render the same brief-plan ack as
   quick_plan.

## Files owned

internal/agent/dispatcher.go (compound branch), internal/agent/
strategic.go (spec_pair reachability + regex deletion — regex deletion
ONLY after leaf 03's replacement lands),
internal/agent/dispatcher_compound_oneshot_test.go (new).

## Pins

- "plan X then implement it" -> one quickplan contract execution: plan
  row completes via rollup; no spec_pair short-circuit; no orphan task.
- Existing compound tests (routeCompoundWithModel, pair-session e2e)
  triage: those asserting the dispatch lane get repointed to assert the
  plan-contract lane; agent-level pair-session executor pins stay green
  byte-identically.
- Conditional phrasing ("...only after I approve") -> pending_approval
  via leaf 03's attribute (pin depends on 03; stub-flag it if 03 not
  landed).

## Gates

full agent suite; e2e/suites/plan-lifecycle scenario addition; gofmt.
ONE commit naming H6/D4.
