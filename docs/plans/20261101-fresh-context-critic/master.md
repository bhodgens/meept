# Fresh-Context Plan Critic - Implementation Orchestrator

> For the executing agent: You are the orchestrator for this tree.
> Dispatch implementation agents, review their work in-session, re-dispatch
> if incomplete, and track completion. Do NOT implement code yourself.
> Full operational detail: skill hierarchical-plan-execution.

## Meta

- Role: Root
- Parent: none (root)
- Children: 8 leaf documents in this directory (flat tree; meept plan trees do not use branch subdirs)
- Scope: Add a fresh-context plan critic and a separate hostile second pass, without editing existing planning or review agents.
- Created: 2026-11-01
- Completed: 2026-11-02
- plan_id: plan-20261101-fresh-context-critic
- Source: operator decisions in the 2026-11-01 EGM comparison session, plus the landed TierComplex critique loop (docs/plans/2026-09-23-tiered-iteration/)

## Goal

The TierComplex critique loop today runs the critic on the same planner loop that wrote the draft (internal/agent/plan_critique_loop.go runCriticOnce, line 600). That shares the author's blind spots.

This tree adds two new passes, both dark by default:

1. A fresh-context evidence critic. It sees only the draft and PlanCritiqueInput. It does not see the planner transcript.
2. A hostile second pass on a different model. It assumes the plan is wrong. It is not a second sample of the same critic.

The existing adversarial verifier stays the post-execution hostile reviewer. This tree does not edit it, retarget it, or route plans through it.

## Architecture

Flag off is byte-identical to today's critique loop. Flag on replaces only the critic call site.

Round 1 (evidence critic) builds a new child AgentLoop. The child has an empty conversation, no tools, and a prompt that contains the draft plus PlanCritiqueInput.Render() only. Its JSON reply must include a paraphrase before objections. An empty paraphrase on a non-empty draft is a synthetic blocking objection.

Round 2 (hostile pass) is a different child, a different prompt, and a different model. It runs only when hostile_critic_model is set and is not the planner model. If the slot is empty or equal to the planner model, the pass is skipped and a metric is recorded. It never falls back onto the planner model. A blocking hit from this pass sends the draft back to the planner revise step. A clean seal requires both passes to return zero blocking objections in the same cycle.

The refine cap stays plans.complex_max_critique_rounds (default 2). That cap counts planner revise cycles. It does not mean "run the same critic twice."

Contest, zero-decision compile, and interview option gates are separate leaves. They do not share files with the verifier.

## Locked constraints

These are operator decisions. A leaf that violates one is NEEDS_FIXES.

- Do not edit config/agents/verifier/AGENT.md.
- Do not edit internal/agent/prompts/verifier.go.
- Do not edit internal/agent/verification_hook.go, and do not call SpawnVerifier from the plan critic.
- Do not edit config/agents/planner/AGENT.md, config/agents/architect/AGENT.md, config/agents/planner-reviewer/AGENT.md, or config/agents/skeptic/AGENT.md.
- Do not send a plan draft into the implementation verifier. That agent runs shell and tests. A plan critic has no tools.
- The hostile pass must use a different model than the planner. Same model is a skip, not a fallback.
- planner-reviewer is not the hostile pass. Its AGENT.md says minor gaps are acceptable. Do not route plans to it.
- Every new flag defaults false and ships dark, same as plans.self_seal_enabled.

## Interface Contracts

### C1: PlansConfig knobs

```go
// Owner: 01-config-knobs.md
// Consumers: 07-daemon-wiring.md
FreshCriticEnabled    bool `json:"fresh_critic_enabled"    toml:"fresh_critic_enabled"`
HostileCriticEnabled  bool `json:"hostile_critic_enabled"  toml:"hostile_critic_enabled"`
InterviewOptionGates  bool `json:"interview_option_gates"  toml:"interview_option_gates"`
```

All three default false. NormalizePlansDefaults does not flip them on. Absent JSON keys stay false.

### C2: Planner setters

```go
// Owner: 02-fresh-context-critic.md except where noted
// Consumers: 07-daemon-wiring.md
func (sp *StrategicPlanner) SetFreshCriticEnabled(enabled bool)
func (sp *StrategicPlanner) SetCriticChatter(ch llm.Chatter) // round 1; nil = planner chatter
func (sp *StrategicPlanner) SetHostileCriticEnabled(enabled bool)      // owner: 03
func (sp *StrategicPlanner) SetHostileCriticChatter(ch llm.Chatter)    // owner: 03; nil = skip
func (sp *StrategicPlanner) SetInterviewOptionGates(enabled bool)      // owner: 06
```

Nil receiver is a no-op, matching SetSelfSealEnabled (plan_critique_loop.go:96).

### C3: Isolated critic child

```go
// Owner: 02-fresh-context-critic.md
// Consumers: 03-hostile-second-pass.md
type planCriticChildRequest struct {
    SystemPrompt string
    UserPrompt   string
    Chatter      llm.Chatter
}

func (sp *StrategicPlanner) runIsolatedCritic(ctx context.Context, req planCriticChildRequest) (string, error)
```

Invariants:

- New AgentLoop. Conversation length is 0 before the single user message.
- Tool registry is empty. No shell_execute, file_write, file_read, or delegate tools.
- Does not call SpawnVerifier. Does not read config/agents/verifier/AGENT.md at runtime.
- UserPrompt is the draft markdown plus PlanCritiqueInput.Render() only. No planner transcript, no session facts, no prior revise reasoning.

### C4: Round-1 reply envelope

```json
{"paraphrase":{"proposes":"...","system_context":"..."},"objections":[{"section":"...","objection":"...","severity":"blocking|advisory"}]}
```

Owner: 02-fresh-context-critic.md. A missing or empty paraphrase on a non-empty draft becomes one synthetic blocking objection. The flag-off path still accepts today's objections-only JSON (parseCriticObjections, plan_critique_loop.go:133).

### C5: Hostile pass skip rule

```text
run hostile pass only when ALL hold:
  HostileCriticEnabled == true
  AND HostileCriticChatter != nil
  AND hostile model ref != planner model ref
else record metric strategic_planner.hostile_critic_skipped
  reason = no_model | same_model | flag_off
  and do not call the planner chatter
```

Owner: 03-hostile-second-pass.md. A clean seal requires round 1 and the hostile pass both blocking-free in the same cycle. If the hostile pass was skipped, outcome is not "clean". Stamp critique_outcome `hostile_skipped` and keep the draft at the human seal in plan mode. Do not self-seal a skipped hostile pass.

### C6: Contest rebuttal

```go
// Owner: 04-contest-path.md
type criticRebuttal struct {
    Section  string `json:"section"`
    Targets  string `json:"targets"`  // objection text being contested
    Evidence string `json:"evidence"` // must quote a PlanCritiqueInput section
    Verdict  string `json:"verdict"`  // accept | contest
}
```

A contest with no quotable evidence is not a contest. It is recorded as contested-unproven and rolled into Known Risks. It is not silently accepted and not silently fixed.

### C7: Zero-decision compile rule

```text
// Owner: 05-zero-decision-compile.md
// A phase fails compile when it has a mutating step and neither produces
// nor consumes a decision artifact from an earlier phase.
mutating tool_hint set: code, shell, file_write, git, commit
error: phase %q changes state but names no decision artifact
```

Extends the existing consume check in internal/plan/compiler.go:125. Does not change file, interface, schema, or test_suite rules.

### C8: Interview option object

```json
{"question":"...","options":[{"mechanism":"...","trade_off":"...","failure_condition":"..."}]}
```

Owner: 06-interview-option-gates.md. At most 3 options. A `recommended` key is a parse error, not a default. Flag off keeps today's `{"questions":["..."]}` shape (strategic.go parseInterviewQuestions, line 495).

### C9: Model slots

```text
// Owner: 07-daemon-wiring.md
// config/models.json5, commented out by default, same shape as planner_model (line 63)
"critic_model": ""          // round 1; empty = planner chatter is allowed
"hostile_critic_model": ""  // round 2; empty = skip (C5), never planner fallback
```

Resolution follows the planner_model client construction in internal/daemon/components.go:1351. A slot that does not resolve is nil chatter, not a hard boot failure.

### C10: Forbidden files

No leaf may modify:

- config/agents/verifier/AGENT.md
- internal/agent/prompts/verifier.go
- internal/agent/verification_hook.go
- config/agents/planner/AGENT.md
- config/agents/architect/AGENT.md
- config/agents/planner-reviewer/AGENT.md
- config/agents/skeptic/AGENT.md

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-plan-critic-gate.md | leaf (test gate) | none | 40K | Phase 0 |
| 01 | 01-config-knobs.md | leaf | 00T TESTS_VALIDATED | 30K | A |
| 05 | 05-zero-decision-compile.md | leaf | 00T TESTS_VALIDATED | 35K | A |
| 06 | 06-interview-option-gates.md | leaf | 00T TESTS_VALIDATED | 40K | A |
| 02 | 02-fresh-context-critic.md | leaf | 01 | 55K | B |
| 03 | 03-hostile-second-pass.md | leaf | 02 | 50K | C |
| 04 | 04-contest-path.md | leaf | 03 | 45K | D |
| 07 | 07-daemon-wiring.md | leaf | 01, 02, 03, 06 | 45K | E |

Same-letter children have no shared files and dispatch together.

File ownership (do not cross):

- 01: internal/config/schema.go, config/meept.json5 comment only if a plans example exists
- 02: internal/agent/plan_critic_child.go, the runCriticOnce branch in plan_critique_loop.go
- 03: internal/agent/plan_critic_hostile.go, the second-pass call in plan_critique_loop.go
- 04: reviseDraft and rebuttal parse in plan_critique_loop.go
- 05: internal/plan/compiler.go and its existing test file only
- 06: internal/agent/strategic.go interview parse only
- 07: internal/daemon/daemon.go, internal/daemon/components.go, config/models.json5, docs/workflows/agent-orchestration.md

02, 03, and 04 share plan_critique_loop.go. They are serial. Never parallel.

## Dispatch Protocol

### Phase 0: Test Gate

Dispatch 00T only. Brief: write tests only, park them as `.staged` (see Coding Conventions). Do NOT commit. Do NOT git add.

The control agent validates in-session that each staged test names the contract it pins (C3-C9) and would fail on today's code for the right reason. Then commit only the `.staged` files and this plan tree. Set 00T to TESTS_VALIDATED. Implementation stays PENDING until that commit lands.

### Phase 1: Concurrency group A

After TESTS_VALIDATED, dispatch 01, 05, and 06 together. Context = leaf text + the contracts that leaf consumes + "Do NOT commit. Do NOT git add."

### Phase 2: Review and commit each child

Review in-session. Run the leaf verify command. Confirm the forbidden-file list has an empty diff. Pass: commit that leaf's pathspec only, status REVIEWED. Gaps: re-dispatch, max 3 cycles.

Each impl commit re-activates only its own `.staged` test in the same commit. The commit must be green. Use MEEPT_SKIP_E2E=1 only while the suite is still parked for later leaves, and say so in the commit body. The integration commit (after 07) runs the suite with the skip unset.

### Phase 3: Groups B, C, D, E

Dispatch in order: 02, then 03, then 04, then 07. 05 and 06 may still be in flight during 02 if their files are disjoint. Do not dispatch 03 until 02 is REVIEWED. Do not dispatch 04 until 03 is REVIEWED. Do not dispatch 07 until 01, 02, 03, and 06 are REVIEWED.

### Phase 4: Integration Review

After all children REVIEWED: `go test -p 2 ./internal/agent/ ./internal/plan/ ./internal/config/ ./internal/daemon/` and the plan-critic e2e suite with no skip. Confirm git diff against the forbidden files is empty. Set children COMPLETE.

## Review Checklist

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created or modified at exact paths
- [ ] Forbidden files (C10) have an empty diff
- [ ] Gate tests for that leaf were re-activated, not rewritten to weaken assertions
- [ ] Tests written and passing
- [ ] Code follows project conventions
- [ ] No scope creep
- [ ] No debug artifacts
- [ ] No line-number corruption (` N|` prefixes) in source files

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- Language: Go, toolchain go1.26.5 (repo pin). Tests: `go test -p 2`.
- New flags default false. Do not change NormalizePlansDefaults to enable them.
- Nil-safe setters. Match SetSelfSealEnabled.
- Errors wrap with fmt.Errorf("%w").
- ASCII only. No em-dash. The pre-commit ascii hook fails on high bytes.
- RED gate tests park as `.staged` and are not compiled until their impl leaf. Proven sequence: meept-development references/red-gate-test-parking.md. A committed failing e2e suite poisons the shared tree.
- Do not `git add` a directory. Explicit pathspecs only. e2e/manifest.json is a hot shared file: add only the plan-critic rows, merge sibling edits, never replace the file.
- Do NOT commit. Do NOT git add. The orchestrator commits after review.

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | "for #2, it should probably be different" | 03-hostile-second-pass.md, contract C5 | hostile skip test: same model does not run |
| R2 | "particularly #1, destructive or modify any of my existing agents" | C10, every leaf Notes | `git diff` empty on forbidden files |
| R3 | "I want to maintain a hostile reviewer" | verifier files untouched; new hostile prompt is a copy of stance, plan-scoped | forbidden-file diff + hostile prompt has no shell tools |
| R4 | Adapt the stronger EGM planning traits: fresh context, paraphrase, zero-decision, contest, unranked options | leaves 02, 04, 05, 06 | per-leaf verify commands |
| R5 | "create the plan tree with your recommendations" | this directory | compliance scan |
| D1 | DEFERRED: EGM five-stage pipeline, six-lens swarm, drift report, bootstrap READMEs | not in this tree | n/a - out of scope |
| D2 | DEFERRED: settled-vs-fixed finding memory across sessions | not in this tree | n/a - contest leaf records one-cycle rebuttals only |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-plan-critic-gate | COMPLETE | 1 | 347ba137 parked; reactivated across 02/03/04/07, all green |
| 01-config-knobs | COMPLETE | 1 | 863624b2 |
| 02-fresh-context-critic | COMPLETE | 1 | dc8f051a (worktree 0241dc07; sibling docs message collision documented in-tree) |
| 03-hostile-second-pass | COMPLETE | 1 | 77734589 |
| 04-contest-path | COMPLETE | 1 | 53e07d22 |
| 05-zero-decision-compile | COMPLETE | 1 | 2a2df9d8 |
| 06-interview-option-gates | COMPLETE | 1 | 238b53c4 |
| 07-daemon-wiring | COMPLETE | 1 | 2d2a8dcb; deadcode -28 net; hostile/contest baseline lines kept (config-gated branch) |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until 00T is TESTS_VALIDATED.

## Integration Test Plan

1. `go test -p 2 ./internal/agent/ -count=1 -run 'Critique|Critic|Interview'`
2. `go test -p 2 ./internal/plan/ -count=1 -run 'Compile|Decision'`
3. `go test -p 2 -tags e2e ./e2e/suites/plan-critic/ -count=1`
4. `git diff -- config/agents/verifier/AGENT.md internal/agent/prompts/verifier.go internal/agent/verification_hook.go config/agents/planner/AGENT.md config/agents/architect/AGENT.md config/agents/planner-reviewer/AGENT.md config/agents/skeptic/AGENT.md` prints nothing.

Expected: all three test commands exit 0. The diff command prints no paths.

## Test Gate

Test leaf: 00T-plan-critic-gate.md. Dispatched in Phase 0. Files land as `.staged` so the gate commit stays green under the meept hook gauntlet.

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|------------|-----------|--------|
| F1 | Flag off keeps planner-loop critic | 02 T1 | n/a - pins live in e2e suite (no new unit file; meept e2e policy) | TestFreshCriticFlagOffDoesNotSpawnChild | PENDING |
| F2 | Flag on child has empty history and no tools | 02 T2 | n/a - same reason | TestFreshCriticChildHasNoTranscript | PENDING |
| F3 | Empty paraphrase is blocking | 02 T3 | n/a | TestEmptyParaphraseIsBlocking | PENDING |
| F4 | Hostile pass skipped when model matches planner | 03 T1 | n/a | TestHostilePassSkipsSameModel | PENDING |
| F5 | Hostile pass uses its own prompt, not verifier.go | 03 T2 | n/a | TestHostilePassDoesNotCallSpawnVerifier | PENDING |
| F6 | Contest without evidence is not applied | 04 T1 | n/a | TestContestWithoutEvidenceNotApplied | PENDING |
| F7 | Mutating phase without decision fails compile | 05 T1 | n/a | TestMutatingPhaseWithoutDecisionFailsCompile | PENDING |
| F8 | Interview flag off keeps string questions | 06 T1 | n/a | TestInterviewFlagOffKeepsStringQuestions | PENDING |
| F9 | Daemon wires flags default false | 07 T1 | n/a | TestCriticFlagsDefaultOff | PENDING |

Statuses: PENDING | DELTA | VALIDATED

Delta log: none yet.

## Open Questions

None blocking. Operator already decided:

- The second pass is a different hostile reader, not two clean rounds of one critic.
- Change 1 must not edit the existing verifier or other planning agents.
- Hostile model slot empty or equal to the planner model means skip, never reuse.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated addenda. Never edit a dispatched leaf mid-execution.

## Notes

Verified anchors at authoring time (2026-11-01):

- runCriticOnce calls runPlannerChat on the planner loop: internal/agent/plan_critique_loop.go:600-605
- Clean-round exit is one round with zero blocking: plan_critique_loop.go:363-366
- SetCritiqueChatter: plan_critique_loop.go:534
- Daemon wires SetSelfSealEnabled and SetCritiqueMaxRounds: internal/daemon/daemon.go:1919-1920
- planner_model slot comment: config/models.json5:63
- planner_model client built only when the slot resolves: internal/daemon/components.go:1351
- SpawnVerifier and verifierAllowedTools: internal/agent/verification_hook.go:344-352 and :436
- Compiler unknown-consume error: internal/plan/compiler.go:125
- Interview question parse: internal/agent/strategic.go:495
