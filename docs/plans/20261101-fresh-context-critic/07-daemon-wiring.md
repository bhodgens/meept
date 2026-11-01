# Daemon Wiring and Model Slots - Implementation Leaf

> For the implementing agent: Implement ALL tasks below. Do NOT commit.
> Do NOT git add. Do NOT edit C10 files.
> Re-activate only e2e/suites/plan-critic/wiring_test.go.staged.
> This leaf also adds the e2e manifest rows. Merge them. Do not replace the file.

## Meta

- Parent: master.md
- Scope: Wire the new setters and the two model slots. Document the flags. Run the full parked suite green.
- Dependencies: 01, 02, 03, and 06 REVIEWED
- Estimated Context: 45K
- Concurrency Group: E

## Goal

Connect config to the setters the earlier leaves added. Resolve critic_model and hostile_critic_model the same way planner_model is resolved. Empty hostile_critic_model stays a nil chatter so leaf 03 skips the pass.

Update the plan-compiler section of docs/workflows/agent-orchestration.md with the flag names, the skip rule, and the forbidden-file rule in one short subsection.

## Context

Daemon already calls SetSelfSealEnabled and SetCritiqueMaxRounds at internal/daemon/daemon.go:1919-1920. Add the new setter calls next to that site. Nil-guard each call.

planner_model client construction is internal/daemon/components.go:1351. Copy that shape for the two new slots. An unresolvable slot is nil, not a boot error. Do not fail F-D8 because a critic slot is empty.

config/models.json5:63 is the commented planner_model example. Add commented critic_model and hostile_critic_model lines beside it. Do not enable them.

## Verified Anchors

- internal/daemon/daemon.go:1919 - SetSelfSealEnabled call
- internal/daemon/daemon.go:1920 - SetCritiqueMaxRounds call
- internal/daemon/components.go:1351 - planner_model client construction
- config/models.json5:59-63 - planner_model comment block
- internal/daemon/components_planner_model_test.go:21 - nil-when-unset pin to mirror

## Interface Contracts (From Parent)

### What This Leaf Exposes

- C9 slots wired
- Daemon calls SetFreshCriticEnabled, SetHostileCriticEnabled, SetInterviewOptionGates, SetCriticChatter, SetHostileCriticChatter
- e2e manifest suite `plan-critic` and path_map entries for the new files
- docs/workflows/agent-orchestration.md subsection

### What This Leaf Consumes

- C1, C2, C9 from master.md
- Setters from leaves 02, 03, and 06

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Wire setters beside the self-seal site. Default config leaves all three flags false | Modify: internal/daemon/daemon.go | `go test -p 2 ./internal/daemon/ -count=1 -run 'PlannerModel\|Critic'` | Flags ignored at construction | Boot with empty plans config does not enable fresh or hostile critic | Given default PlansConfig; When the planner is constructed; Then FreshCriticEnabled and HostileCriticEnabled are false |
| T2 | Resolve the two slots like planner_model. Nil when empty or unresolvable | Modify: internal/daemon/components.go; Modify: config/models.json5 comments only; Modify: components_planner_model_test.go or a sibling test in that file | same | hostile slot empty still builds the planner client as the hostile chatter | Empty hostile_critic_model yields nil hostile chatter | Given models.json5 with no hostile_critic_model; When components build; Then hostile chatter is nil and planner chatter is unchanged |
| T3 | Add manifest suite and path_map without dropping sibling rows. Re-activate wiring_test.go.staged. Document the flags | Modify: e2e/manifest.json (add only); Modify: docs/workflows/agent-orchestration.md; restore wiring_test.go | `go test -p 2 -tags e2e ./e2e/suites/plan-critic/ -count=1` | Suite not selected or test fails | Suite passes; manifest still contains the pre-existing suite names | Given the manifest after the edit; When parsed; Then suites contains plan-critic and the previous suite count is not lower |

path_map keys to add, each pointing at suite name `plan-critic`:

- internal/agent/plan_critic_child.go
- internal/agent/plan_critic_hostile.go
- e2e/suites/plan-critic

Do not remove existing path_map entries for internal/agent or internal/plan. Add the new suite beside them. If a sibling has uncommitted manifest edits, merge. A lost suites[] entry fails TestEveryMappedSuiteHasScenarios.

Doc subsection must state:

- fresh_critic_enabled and hostile_critic_enabled default false
- hostile pass skips when the model matches the planner or the slot is empty
- the implementation verifier (config/agents/verifier) is not the plan critic and is not modified by this feature

## Self-Verification Checklist

- [ ] Default boot does not enable the new flags
- [ ] Empty hostile slot does not reuse the planner chatter
- [ ] Manifest merge did not drop suites
- [ ] Full plan-critic e2e suite passes
- [ ] No C10 file in the diff
- [ ] models.json5 examples stay commented

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] C9 slot names match exactly
- [ ] Setter calls sit next to daemon.go:1919
- [ ] Forbidden files absent from the diff
- [ ] Workflow doc names the skip rule
- [ ] e2e suite green with MEEPT_SKIP_E2E unset

Output: APPROVED or specific gaps with file + line references.

## Notes

Do not edit config/agents/verifier/AGENT.md, internal/agent/prompts/verifier.go, internal/agent/verification_hook.go, or the planner, architect, planner-reviewer, or skeptic AGENT.md files.

This is the integration leaf. After review, the orchestrator runs the Integration Test Plan in master.md.
