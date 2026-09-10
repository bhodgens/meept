# QuickPlan Mode - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 4 leaf documents
- **Scope:** Add `quickplan` as a first-class intent + planning mode in the
  meept dispatcher: plan, clarify-if-ambiguous, then execute autonomously
  with no approval pauses. Includes classifier taxonomy, mode plumbing,
  ambiguity interaction, and documentation.

## Goal

Users issue plan-and-do requests ("review the daemon for bugs and correct
them as you find them", "implement the plan using subagents", "commit, push,
then run prodenv/prod"). Today these either route to single intents (losing
the multi-step orchestration) or stop at the plan-approval gate. QuickPlan
adds a third execution style: **clarify-if-ambiguous → orchestrator plans →
execute to completion → report**, with the orchestrator (not the classifier)
owning quickplan-vs-code/git disambiguation using session state at execution
time.

Measured basis (classifier campaign, docs/plans/classifier-iteration):
quickplan-shaped requests are 42% of real traffic (20/48 adjudicated
replay cases); per-message text classification provably cannot resolve
quickplan-vs-code/git (the distinguishing signal is session state — an
approved plan / tracked tasks exist). The classifier therefore routes
quickplan as a 13th intent with an orchestration-cue guard, and the
orchestrator makes the final call at dispatch time.

## Architecture

1. **Classifier layer (Stage-0 + LLM chain):** `quickplan` becomes the
   13th intent type. Stage-0 centroid gate gets quickplan examples in the
   index; the LLM chain prompt documents the class; a lexical
   orchestration-cue regex (subagent/task N/wave/leaf/plan.md/handoff/
   "and correct them"/"then run") guards quickplan predictions — a
   quickplan prediction without cue support falls through to the chain.
2. **Dispatcher layer:** `quickplan` intent → orchestrator agent,
   `SuggestedMode() = "quick_plan"`. The ambiguity gate runs BEFORE
   execution as designed (clarify first). After clarification resolves —
   or if no clarification was needed — the orchestrator plans and
   dispatches sub-tasks to executor agents (coder/debugger/reviewer)
   without an approval pause.
3. **Session-state resolution:** at dispatch time the orchestrator
   receives session context (active plan, tracked tasks) so it can
   override a code/git classification with quickplan when session state
   indicates an active plan-execution flow. This is the piece the
   per-message classifier cannot do (campaign finding, iter-20).
4. **Fall-through:** `quickplan` becomes the final classifier fallback
   (replacing the plain chat fallback at dispatcher.go Step 5), keeping
   the short/simple guard → chat behavior intact.

## Interface Contracts

### Contract 1: Intent type

```go
// internal/agent/intent.go
IntentQuickPlan IntentType = "quickplan"

// SuggestedMode(): case IntentQuickPlan: return "quick_plan"
// DefaultAgent():  case IntentQuickPlan: return "orchestrator"
// Category():      case IntentQuickPlan: return CategoryDefer
// RequiresPlanning(): case IntentQuickPlan: return true
// ShouldCreateTask(): case IntentQuickPlan: return true

// Owner: 01-intent-type.md
// Consumers: 02-dispatcher-routing.md, 03-prefilter-index.md, 04-docs.md
```

### Contract 2: Mode string

```go
// validModes map (dispatcher.go) gains: "quick_plan"
// suggestMode(): case IntentQuickPlan: return "quick_plan" (never
// downgraded to direct by short input — the explicit execution phrasing
// IS the signal)
// Owner: 02-dispatcher-routing.md
// Consumers: internal/plan manager mode switch
```

### Contract 3: Orchestration-cue guard (shared regex)

```go
// internal/agent/quickplan_cue.go
var QuickPlanCuePattern = regexp.MustCompile(`(?i)\b(subagents?|tasks? \d|` +
    `task list|waves?|leaves?|leaf \d|plan\.md|handoff|checklist|in order|` +
    `one at a time|sealed plan|tracking table|as you (find|go)|, then\b|` +
    `and correct them|and fix them|without (asking|stopping)|no check-?ins?|` +
    `just (do|make|apply)|make it happen|to completion|finish the remaining|` +
    `carry on with the plan|execute (the|what)|implement (the|all) plan|` +
    `implement tasks?|work (through|items)|knock out|carry out|` +
    `complete the outstanding)\b`)

// Used by: prefilter vote() (quickplan prediction without cue → no route)
// and by the LLM-chain post-check. Owner: 03-prefilter-index.md.
```

### Contract 4: Ambiguity-gate interaction

```go
// dispatcher.go ambiguity gate (buildClarificationResult) runs BEFORE
// quickplan execution, exactly as today. Clarification resolution
// (ResumeAfterClarification) must re-enter with mode=quick_plan when the
// original pending intent was quickplan — the clarification answer
// completes the plan, it does not downgrade to plain chat.
// Owner: 02-dispatcher-routing.md
```

### Contract 5: Final fallback swap

```go
// dispatcher.go classifyIntent() Step 5 (final fallback) changes:
// Type: IntentQuickPlan, AgentType: orchestrator, Confidence: 0.3,
// Method: "fallback", Summary: "Could not determine intent;
// planning and executing with clarification as needed"
// recordAgent("orchestrator"); recordIntentType("quickplan")
// The short/simple guard BEFORE the chain still routes to chat.
// Owner: 02-dispatcher-routing.md
```

### Contract 6: Session-state override (orchestrator side)

```go
// The orchestrator's planning prompt (template or code-assembled) receives
// a SessionExecutionContext block: active plan (ID/title/state), tracked
// tasks (open count), prior waves in this conversation. The orchestrator
// may reclassify an incoming code/git intent as quickplan when session
// state shows an active plan-execution flow. One-way: session evidence
// upgrades to quickplan; absence never downgrades an explicit quickplan.
// Owner: 02-dispatcher-routing.md (orchestrator prompt assembly)
```

### Contract 7: Prefilter index entries

```json
// ~/.meept/classifier_prefilter_centroids.json (built by
// scripts/build_prefilter_centroids.py) gains quickplan examples.
// The Go prefilter requires NO code change — new classes are data —
// but vote()'s quickplan prediction must pass the cue guard
// (Contract 3) before returning a direct route.
// Owner: 03-prefilter-index.md
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-intent-type.md | leaf | none | ~45K | A |
| 02 | 02-dispatcher-routing.md | leaf | 01 | ~85K | B |
| 03 | 03-prefilter-index.md | leaf | 01, 03(cue regex) | ~50K | B |
| 04 | 04-docs.md | leaf | 01, 02 | ~35K | C |

**Concurrency groups:** 01 alone in A (everything consumes it). 02 and 03
parallel in B (both consume Contract 1/3 definitions; 03 needs the cue
regex which is pinned in Contract 3 — no code dependency). 04 last in C.

## Dispatch Protocol

For each concurrency group, in dependency order:

### Phase 1: Dispatch Concurrency Group A

1. **Read** 01-intent-type.md and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 01-intent-type.md"
   - Context: full leaf text + Contract 1 + coding conventions block +
     current internal/agent/intent.go content INLINED
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests,
     report results only."
   - Include the read_file corruption warning (Pitfall 12)

### Phase 2: Dispatch + Review Concurrency Group B

1. **Read** 02-dispatcher-routing.md, dispatch via `delegate_task`
   (context: leaf text + Contracts 1/2/4/5/6 + current dispatcher.go
   relevant regions inlined: classifyIntent Step 5, suggestMode,
   validModes, buildClarificationResult, ResumeAfterClarification)
2. **Read** 03-prefilter-index.md, dispatch in the SAME batch
   (context: leaf text + Contract 3 + current
   internal/agent/embedding_prefilter.go vote() region inlined)
3. Review each in-session on return (changed files vs leaf spec,
   `go build ./internal/... && go test ./internal/agent/... -short`)
4. Re-dispatch with feedback on gaps (max 3 cycles); commit per-leaf on
   pass: `git commit -- <exact files>` with
   `feat(quickplan): <leaf summary>` messages

### Phase 3: Dispatch Concurrency Group C

1. **Read** 04-docs.md, dispatch (context: leaf text + implemented
   Contracts 1-7 + docs/workflows/intent-routing.md current content)
2. Review (docs leaf: verify cross-references resolve, ASCII, no
   corruption)
3. Commit: `docs(quickplan): routing + workflow documentation`

### Phase 4: Integration Review

1. `go build ./... && go test ./internal/agent/... -p 2 -count=1`
2. Full suite: `make test`
3. Analyzers: `make mutexio && make predid`
4. End-to-end smoke: scratch daemon with `classifier_prefilter.enabled`,
   send a quickplan-shaped message ("using subagents, review the config
   loader for bugs and fix them"), verify: classified quickplan, no
   approval pause, orchestrator plans and executes, report returns.
5. Re-run replay validation: adjudicated 48-case gold replay through the
   live dispatcher path; acceptance = system accuracy ≥ chain-only 86.8%
   with quickplan class active.

## Review Checklist

- [ ] All tasks from each leaf implemented at exact paths
- [ ] Interface Contracts 1-7 satisfied (compile-level for 1-5)
- [ ] quickplan predictions without cue support fall through (unit test)
- [ ] Ambiguity gate fires BEFORE quickplan execution; clarification
      resume preserves mode=quick_plan
- [ ] Final fallback is quickplan; short/simple guard still chat
- [ ] No approval pause between quickplan plan and execution
- [ ] Existing plan/spec_plan/spec_pair modes unchanged
- [ ] Tests: dispatcher routing tests + prefilter cue-guard tests
- [ ] No debug artifacts, no TODOs, no placeholder values
- [ ] No line-number corruption (`grep -rcE '^\s+[0-9]+\|' --include='*.go'`)

## Coding Conventions

- **Language:** Go 1.22+ (meept module), stdlib + existing deps only
- **Naming:** exported PascalCase, unexported camelCase
- **Error handling:** wrap with `%w`, return early, no panic
- **Tests:** table-driven, `_test.go` alongside, `t.Helper()` for builders
- **Formatting:** gofmt before reporting completion
- **Comments:** explain WHY; every new intent constant references the
  adjudication record (docs/plans/classifier-iteration/adjudication rules)
- **Regex:** single compiled package-level `var`, never inline compile
- **Config:** no new config keys unless a leaf explicitly specifies one

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-intent-type.md | COMPLETE | 1 | 4870fc98 — all 5 switches + SemanticIndex + SteeringHeuristic + ShouldDispatchAsync; LLM-whitelist tables deliberately excluded (leaf 02 scope) |
| 02-dispatcher-routing.md | COMPLETE | 1 | d3ace332 — mode plumbing + clarify-resume (PendingMode) + fallback swap + session context; also fixed 2 latent bugs (gate never recorded pending state; analyzer coerced quickplan to 'other'); 13 new tests |
| 03-prefilter-index.md | COMPLETE | 1 | 50208cf6 — cue regex verbatim, vote() guard, centroid builder fixed for iter-19/20 corpus (// comments + cases: layout) |
| 04-docs.md | COMPLETE | 1 | 25bc635b — QuickPlan sections in intent-routing.md + classifier-prefilter.md; AGENTS.md reviewed (no change needed); fallback sweep 0 corrections |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./...` — zero errors
2. `go test ./internal/agent/... -p 2 -count=1` — all pass, including new
   quickplan routing/cue-guard/fallback tests
3. `make test` — full suite green (macOS: -p 2 mandatory)
4. `make mutexio && make predid` — analyzers clean
5. Prefilter smoke: rebuild centroid store with quickplan examples;
   scratch daemon + `meept chat "using subagents, review the config
   loader for bugs and fix them"` → classification_method
   embedding_prefilter or quickplan chain; no clarification loop; no
   approval pause; final report references executed fixes
6. Ambiguity smoke: `meept chat "fix it"` (ambiguous, no cue) →
   clarification questions; answer "the login endpoint 500s on valid
   credentials" → resumes quickplan mode, executes
7. Fallback smoke: gibberish input ≥ short-guard length → final fallback
   quickplan (orchestrator clarifies, then plans)
8. Replay validation: 48-case adjudicated gold replay ≥ 86.8% system
   accuracy (chain floor) with quickplan class active

## Notes

- The classifier campaign's corpus (testdata/eval/classifier-adversarial-
  corpus.json5, 370 cases incl. 44 quickplan anchors from iters 19-20) is
  the training/validation source. The adjudicated 48-case replay corpus is
  UNTRACKED (tools/classifier-eval/replay-gold.local.json5) — leaves must
  NOT reference it in tests; the tracked corpus carries the same shapes.
- Session-state override (Contract 6) is orchestrator-prompt-side only in
  this tree — a deeper session-tracker integration (programmatic task
  state into the classifier) is explicitly out of scope and should be a
  follow-up plan if quickplan traffic demands it.
- The daemon currently at HEAD ships k=5-unanimity prefilter; the campaign
  champion is centroid margin 0.030. Wiring the centroid champion is the
  classifier-iteration campaign's M4 deliverable, NOT this tree's — this
  tree only adds the quickplan class + cue guard to whatever head ships.
- ASCII-only (pre-commit hook). AGENTS.md review required in the docs leaf.
- Parallel-session hazard: dispatcher.go may carry sibling WIP; leaves
  touching it must re-read at dispatch time and pin hunks (Contract 2 and
  5 regions) in their briefs.
