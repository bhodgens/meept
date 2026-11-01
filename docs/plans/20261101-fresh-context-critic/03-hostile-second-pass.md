# Hostile Second Pass - Implementation Leaf

> For the implementing agent: Implement ALL tasks below. Do NOT commit.
> Do NOT git add. Do NOT call SpawnVerifier. Do NOT edit C10 files.
> Re-activate only e2e/suites/plan-critic/hostile_pass_test.go.staged.

## Meta

- Parent: master.md
- Scope: Add a second critic pass that is a different reader and a different model. It is not two clean rounds of the evidence critic.
- Dependencies: 02 REVIEWED (runIsolatedCritic exists)
- Estimated Context: 50K
- Concurrency Group: C

## Goal

After round 1, when the hostile flag is on and a distinct chatter is set, run one hostile child via runIsolatedCritic. The prompt assumes the plan is wrong. The model must not be the planner model.

If the chatter is nil or the model ref equals the planner model ref, skip the pass. Record `strategic_planner.hostile_critic_skipped` with reason `no_model`, `same_model`, or `flag_off`. Do not send the hostile prompt to the planner chatter.

A seal may be called clean only when round 1 and this pass both have zero blocking objections in the same cycle. A skipped hostile pass is not clean. Stamp critique_outcome `hostile_skipped` and do not self-seal.

## Context

The operator rejected "run the same critic twice." One clean sample from one model is weak. A second sample from the same model shares the same blind spot.

The stance to copy, in new words, into a new file:

- Assume the plan is wrong until the text proves otherwise.
- Do not confirm the plan. Try to find the decision the implementer would still have to make.
- Report blocking and advisory objections. Do not fix the draft.

Do not paste internal/agent/prompts/verifier.go into the new file. That file stays the implementation verifier. Write a plan-scoped prompt. It must not mention shell, tests, or SpawnVerifier.

verifierAllowedTools (verification_hook.go:344) includes shell_execute. Do not reuse that map.

## Verified Anchors

- internal/agent/plan_critique_loop.go:363 - current single clean-round break; this leaf must not treat one evidence-critic clean round as a full clean seal when the hostile flag is on
- internal/agent/prompts/verifier.go:16 - stance source to paraphrase, not to import
- internal/agent/verification_hook.go:436 - SpawnVerifier; must remain uncalled
- config/agents/planner-reviewer/AGENT.md:22 - "Minor gaps are acceptable"; do not route to this agent

## Interface Contracts (From Parent)

### What This Leaf Exposes

- C2 setters SetHostileCriticEnabled and SetHostileCriticChatter
- C5 skip rule and metric
- Exported `PlanCriticSpawnVerifierCalls() int` returning 0 always (the gate test asserts it stays 0)
- internal/agent/plan_critic_hostile.go with the prompt constant and the pass function

### What This Leaf Consumes

- C3 runIsolatedCritic from leaf 02
- C5 from master.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Skip when chatter is nil or model ref equals the planner ref | Create: internal/agent/plan_critic_hostile.go; Modify: internal/agent/plan_critique_loop.go loop body only | `go test -p 2 ./internal/agent/ -count=1 -run 'TestHostilePass\|Critique'` | Same-model call would prove the fallback | Metric reason same_model or no_model; planner chatter message count unchanged | Given HostileCriticEnabled true and chatter nil; When the cycle ends; Then no hostile prompt was sent and outcome is hostile_skipped |
| T2 | Run the hostile child only on a distinct chatter; blocking hit returns to reviseDraft | Modify: plan_critique_loop.go | TestHostilePassSkipsSameModel and TestHostilePassDoesNotCallSpawnVerifier | Pass not wired | Distinct chatter receives the hostile system prompt; SpawnVerifier call count stays 0; a blocking hostile objection prevents clean seal | Given a distinct hostile chatter and one blocking objection; When the cycle runs; Then reviseDraft is invoked and sealed_by is not planner-self |

Cap interaction: plans.complex_max_critique_rounds still limits revise cycles (default 2). The hostile pass is not an extra revise round. It is a reader. A hostile blocking objection consumes one revise cycle, then both readers run again.

Do not edit plan_critic_child.go except to call runIsolatedCritic. If you need a helper, put it in plan_critic_hostile.go.

## Self-Verification Checklist

- [ ] Same-model and nil-chatter paths do not call the planner chatter
- [ ] SpawnVerifier call count is 0
- [ ] verifier.go and verification_hook.go not in the diff
- [ ] planner-reviewer AGENT.md not in the diff
- [ ] Only hostile_pass_test.go was re-activated
- [ ] A skipped hostile pass does not self-seal

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] C5 implemented with the three skip reasons
- [ ] Prompt file does not import or embed verifier.go
- [ ] Clean seal requires both passes
- [ ] No tools on the hostile child

Output: APPROVED or specific gaps with file + line references.

## Notes

Do not edit config/agents/verifier/AGENT.md, internal/agent/prompts/verifier.go, internal/agent/verification_hook.go, or the planner, architect, planner-reviewer, or skeptic AGENT.md files.
