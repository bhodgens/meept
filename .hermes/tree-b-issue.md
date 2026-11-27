## Summary

Make the Hermes hierarchical-plan workflow the default way meept goes from a plan to working code. This changes meept's execution model. It is not a new surface and it is not the model-rating work.

Meept already plans in phases, flattens phases into steps, gates phases on artifact availability, and runs a reviewer agent per step. It lacks the hierarchical-plan discipline: test-first gates, leaf-size limits, isolated worktrees, in-session orchestrator review, frozen contracts, requirement traceability, tracking tables, and a compliance scan.

## Motivation

This comes from the review of a post about grading an agent's work. The request is broader than grading: the workflow that the Hermes hierarchical-plan skill suite manifests should be part of how meept plans through implementation, by default.

The review agent must be a first-class part of the planner's regimen, not an optional extra. The orchestrator must make sure review happens, scoring happens, and the implementation agent then passes or fails under the current criteria.

## Scope: what "full implementation" means

1. **Plan schema** - add `execution_profile`, leaf kind, requirement ID, owned file list, and do-not-commit flag to plan and step records.
2. **Planner regimen** - emit Hermes-shaped leaves with frozen interface contracts. A code review agent is automatically included in every plan's review regimen. The planner schedules it; it is not opt-in.
3. **Test-first gate** - `00T` test leaves are validated before implementation leaves dispatch. A phase cannot start its implementation while its test gate is unverified.
4. **Leaf sizing** - max 3 files, one language, roughly 25 minutes per leaf. Enforce in planner output, not in prose guidance.
5. **Isolation worktree** - one worktree per plan at `.hermes/worktrees/<tree-name>` on branch `plan/<tree-name>`. Main stays untouched until final merge.
6. **Orchestrator review in-session** - the orchestrator reviews each leaf. The reviewer agent supplies grading. The implementation agent passes or fails under the current step criteria.
7. **Compliance gate** - run the structural compliance scan before a tree commits or dispatches. A tree that does not pass does not execute.
8. **Tracking and traceability** - per-leaf status from PENDING through TESTS_VALIDATED, IN_PROGRESS, REVIEWED, COMPLETE, with commit hashes and requirement IDs resolving to the master contract.
9. **Default flip** - land as a profile first, then flip the default when the hermetic e2e suites pass.

## Review and grading order

The order is fixed:

1. The reviewer agent reviews the step's work.
2. The reviewer agent grades the work using the hierarchical-planning rubrics.
3. The implementation step passes or fails under the current criteria.

The reviewer is a code review role agent. It is part of the planner's regimen automatically. The orchestrator is responsible for making sure the review and the grading both happen before the pass/fail decision.

## Relationship to #168 (Tree A, model rating)

These are separate trees with one shared seam.

| | Tree B (this issue) | Tree A (#168) |
|---|---|---|
| Question | How does meept execute a plan? | Which model did bad work, what kind, and who reviewed it? |
| Input | A user goal | A completed review verdict |
| Output | Leaves, gates, worktrees, review passes | Dashboard, CLI, HTTP endpoint |
| Changes | Planner, steps, gates, review manager, worktree resolver | Metrics store, query, HTTP, Flutter, menubar, CLI |
| Owns | Workflow enforcement | Rating taxonomy and retention |

Shared seam: a frozen rubric and review-verdict contract. Tree B's reviewer agent emits verdicts. Tree A stores and displays them. Neither tree edits the other's files.

## Differentiation from adjacent work

- **#61** (tool-driven plan decomposer) - changes planner output shape and step verification. Does not make the hierarchical workflow default.
- **#28** (older trees fail compliance scanner) - fixes scanner false positives on documents. Does not add a runtime compliance gate.
- **#168** (model rating) - records and displays grade data. Does not change how plans execute.

## Open questions

These need answers before the plan tree is authored.

1. **Default flip timing** - land as an opt-in profile and flip in the same tree after e2e passes, or flip in the first commit? Recommendation: same tree, final leaf, gated on e2e.
2. **Reviewer identity** - does the dashboard and gate need the reviewer agent id, the reviewer model id, or both? Recommendation: both, resolved through the existing model precedence chain.
3. **Review lane mechanism** - extend `ReviewManager` with an orchestrator lane, or add a separate in-session review path? Recommendation: extend `ReviewManager`, preserve its fail-closed behavior.
4. **Worktree ownership** - who creates and removes the plan worktree when a daemon owns the lifecycle? Recommendation: the plan manager, with crash recovery through a lease, like the synthesis lease.
5. **Rubric source of truth** - which tree owns the frozen rubric IDs that both trees reference? Recommendation: Tree B owns the workflow rubric IDs; Tree A owns the rating/severity IDs; a shared document maps them.

## Effort estimate

30 to 47 working days for one engineer. The largest costs are the planner output changes, the worktree lifecycle with crash recovery, and the in-session review lane. The default flip adds regression work across every existing plan e2e suite.

## Plan-tree requirements

This issue will itself be implemented through the hierarchical-planning skill suite:

- Test-gate leaves before implementation leaves, one per implementation leaf.
- Leaves of 3 or fewer files, one language per leaf.
- Isolation worktree on branch `plan/<tree-name>`.
- Frozen interface contracts in `master.md`.
- Orchestrator reviews in-session; leaf agents do not commit.
- `check_template_compliance.py <tree> --strict-leaves` must print `ALL TREES COMPLIANT: True` before commit.
- New e2e suite and `e2e/manifest.json` row in the same commit.
- Any new bus topic requires regenerated connectivity graphs in the same commit.
- AGENTS.md and `docs/workflows/` updates in the same commit.

## Acceptance criteria

1. A plan can be created with `execution_profile: hermes_tree`.
2. The planner emits leaves with requirement IDs, owned files, and frozen contracts.
3. A code review agent is automatically part of the plan's review regimen.
4. Test gates validate before implementation leaves dispatch.
5. Each leaf runs in the plan worktree; main is untouched until final merge.
6. The orchestrator reviews each leaf in-session and records status with a commit hash.
7. The reviewer agent grades work with the hierarchical-planning rubrics.
8. The implementation step passes or fails under the current criteria after review and scoring.
9. The compliance gate blocks a tree that fails the structural scan.
10. The default profile is `hermes_tree` after the e2e suites pass, with legacy behavior available by opt-out.
11. Hermetic e2e suites cover the full path from goal to merged work.
