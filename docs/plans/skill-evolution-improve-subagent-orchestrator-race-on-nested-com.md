# Plan: Skill evolution: improve subagent-orchestrator-race-on-nested-completion

## Meta

- plan_id: plan-20260830204304-0001
- created: 2026-08-30
- status: planning

## Summary

The skill body is currently empty, so its 5 injections (1 positive, 2 negative, effectiveness 0.20) had no guidance to shape behavior. The successful traces reveal the intended pattern: when orchestrating a decomposed plan, the coordinator must drive all steps to completion in a single exchange, immediately dispatching dependent steps each time a nested subagent completes ('racing on nested completion'), passing prior-step results forward, and verifying state-mutating steps. Codifying that pattern — plus the anti-patterns that likely caused the negative outcomes (stopping after the first subagent returns, sequential dispatch of parallelizable steps, missing context propagation, unverified writes) — should raise effectiveness.

Candidate content:
# Subagent Orchestrator — Race on Nested Completion

## Purpose
When executing a plan by delegating steps to subagents, do not pause between steps. Every time a nested agent completes, immediately race ahead: ingest its result, dispatch every step whose dependencies are now satisfied, and keep going until the plan is fully resolved — all within a single exchange.

## When this applies
- You receive a task decomposed into steps (your own plan or an upstream planner's JSON plan with `description`, `tool_hint`, `depends_on`).
- You are the coordinator spawning or invoking subagents for individual steps.
- A subagent returns a completion payload (job_id, response, evidence).

## Core rules

1. **Single-exchange resolution.** Resolve the entire delegated plan before returning to the user. Never end your turn with "step 1 done, awaiting continuation" — that fragments the workflow and hands control back prematurely.

2. **Race on every nested completion.** The moment a subagent result arrives:
   - Parse its status and evidence (claims, file evidence, hashes).
   - Mark the step done or failed.
   - Recompute the dependency graph and immediately dispatch all newly unblocked steps — in parallel where their `depends_on` allows.

3. **Dispatch early.** At the start of the exchange, fire every step with empty `depends_on` in parallel rather than sequentially.

4. **Verify state-mutating steps.** For steps that write files, commit, or otherwise mutate state, ensure the result is verified (existence, size, content, hash) — either by the executor or a dedicated follow-up verification step. If verification fails, re-run the step once with the failure evidence before reporting failure.

5. **Propagate context.** When dispatching a dependent step, include the results/evidence of its dependencies in the step prompt ("Results from Prior Steps"). Subagents have no shared memory of prior jobs unless you provide it.

6. **Map hints to agents.** Translate the plan's `tool_hint` to the correct available agent using the dynamic agent list when provided (code→coder, debug→debugger, analyze→analyst, git→committer, research→researcher, plan→decompose further, chat→self).

7. **Terminal report only at the end.** Emit the structured completion JSON (status, accomplished, not_done, issues, observations, evidence) once — after all steps have completed or irrecoverably failed. Include per-step outcomes and concrete evidence (file paths, sizes, hashes, content).

## Failure handling
- A failed step: retry once with clarified instructions that include the failure evidence.
- An irrecoverably failed dependency: mark downstream steps as blocked, report `status: "partial"` with issues, and populate `suggested_next_agent`.
- Never silently drop a step: every step in the plan must end as done, failed, or blocked in the final report.

## Anti-patterns (known causes of prior negative outcomes)
- Returning to the user after the first subagent completes instead of continuing the plan.
- Dispatching independent steps sequentially instead of in parallel.
- Omitting prior-step results when prompting a dependent subagent.
- Ending the exchange after a write with no verification of the artifact.
- Producing the terminal JSON report before all steps are resolved.


## Notes

