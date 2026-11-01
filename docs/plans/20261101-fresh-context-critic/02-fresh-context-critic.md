# Fresh-Context Evidence Critic - Implementation Leaf

> For the implementing agent: Implement ALL tasks below. Do NOT commit.
> Do NOT git add. Do NOT call SpawnVerifier. Do NOT edit C10 files.
> Re-activate only e2e/suites/plan-critic/fresh_critic_test.go.staged.

## Meta

- Parent: master.md
- Scope: When fresh_critic_enabled is on, run round 1 in a new child loop that sees only the draft and PlanCritiqueInput.
- Dependencies: 01 REVIEWED (flag names). Setter may land here even if 01's daemon wire is absent.
- Estimated Context: 55K
- Concurrency Group: B

## Goal

Replace the critic call inside the TierComplex loop when the flag is on. Flag off keeps runCriticOnce on the planner loop, byte-identical.

The child is not the verifier. It has no tools. It does not load verifier.go.

## Context

Today runCriticOnce (plan_critique_loop.go:600) calls runPlannerChat on plannerLoop. That loop holds the draft conversation.

Build the child with NewAgentLoop, an empty tool registry, and SystemPromptOveride set to the evidence-critic prompt. Pass one user message: draft markdown plus PlanCritiqueInput.Render(). Copy the isolation idea from BuildSpawnContext IsolationArtifactOnly (internal/agent/isolation.go:86). Do not call SpawnVerifier (verification_hook.go:436).

SetCritiqueChatter (plan_critique_loop.go:534) remains the planner-side chatter. SetCriticChatter is the optional round-1 model. Nil means the planner chatter is allowed for round 1 only. The hostile pass must not use this fallback. That rule is leaf 03.

## Verified Anchors

- internal/agent/plan_critique_loop.go:345 - critique prompt build call
- internal/agent/plan_critique_loop.go:363 - clean-round break
- internal/agent/plan_critique_loop.go:534 - SetCritiqueChatter
- internal/agent/plan_critique_loop.go:577 - buildCriticPrompt
- internal/agent/plan_critique_loop.go:600 - runCriticOnce
- internal/agent/isolation.go:86 - IsolationArtifactOnly keeps Transcript empty
- internal/agent/verification_hook.go:344 - verifierAllowedTools includes shell_execute; the plan critic must not copy that allowlist

## Interface Contracts (From Parent)

### What This Leaf Exposes

- C2 setters SetFreshCriticEnabled and SetCriticChatter
- C3 runIsolatedCritic
- C4 paraphrase envelope, parsed only on the fresh-critic path

### What This Leaf Consumes

- C1 flag name fresh_critic_enabled (leaf 01 owns the struct field; this leaf reads it only via the setter, not by importing a new config helper)
- C3 and C4 from master.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add SetFreshCriticEnabled and keep flag-off path on runPlannerChat | Modify: internal/agent/plan_critique_loop.go; Create: internal/agent/plan_critic_child.go | `go test -p 2 ./internal/agent/ -count=1 -run 'TestFreshCritic\|Critique'` | Flag on still uses plannerLoop | Flag off: critic prompt hits the planner chatter. Flag on: planner chatter does not receive the critic system prompt | Given FreshCriticEnabled false; When a critique round runs; Then runCriticOnce still calls the planner loop |
| T2 | Implement runIsolatedCritic with empty tools and empty starting history | Create: internal/agent/plan_critic_child.go | same, plus re-activated e2e test TestFreshCriticChildHasNoTranscript | Child missing | Child message list length is 1 (the user prompt) before the call returns; tool list length is 0 | Given flag on; When round 1 runs; Then the child registry lists no tools and the conversation had no prior messages |
| T3 | Require C4 paraphrase on the fresh path; empty paraphrase becomes one blocking objection | Modify: plan_critic_child.go parse path | TestEmptyParaphraseIsBlocking | Parser accepts objections-only as a full fresh-path success | Fresh path with `{"objections":[]}` and no paraphrase yields one blocking objection and does not seal clean | Given a non-empty draft and a fresh-path reply with an empty paraphrase; When parsed; Then blocking length is 1 |

Re-activate fresh_critic_test.go.staged in this leaf's working tree (`mv` to `.go`) so you can run it. Do not weaken assertions. Do not re-activate the other staged files.

Prompt rules for the child system prompt:

- State that the reader has no earlier conversation.
- Demand the C4 JSON object and nothing else.
- Do not copy text from internal/agent/prompts/verifier.go. This pass is an evidence critic, not the hostile pass.

## Self-Verification Checklist

- [ ] Flag off does not construct a child loop
- [ ] Flag on child has no tools and no inherited transcript
- [ ] SpawnVerifier is not called (grep the new file)
- [ ] C10 files untouched
- [ ] Only fresh_critic_test.go was re-activated

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] C3 invariants hold in plan_critic_child.go
- [ ] verifier.go and verification_hook.go absent from the diff
- [ ] Flag-off test passes
- [ ] Paraphrase gap is blocking, not advisory

Output: APPROVED or specific gaps with file + line references.

## Notes

Do not edit config/agents/verifier/AGENT.md, internal/agent/prompts/verifier.go, internal/agent/verification_hook.go, or planner-reviewer / skeptic / planner / architect AGENT.md files.
