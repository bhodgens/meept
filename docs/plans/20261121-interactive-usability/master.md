# Master — Interactive Usability Remediation (2026-11-21)

## Meta

- Tree: 20261121-interactive-usability
- Source report: `INTERACTIVE-USABILITY-20261121.md` (repo root). Audit
  detail: `/tmp/meept-usability/*-audit.md` (4 reports; copy into this
  tree's companion dir if /tmp is at risk).
- Backend: direct dispatch (isolation worktree per the
  hierarchical-planning skill).
- Execution precondition: `git worktree add .hermes/worktrees/usability
  -b plan/20261121-interactive-usability` BEFORE wave 1; every leaf brief
  names that absolute path as cwd.

## Goal and Architecture Overview

Close every P0/P1/P2 finding from the 2026-11-21 interactive-usability
analysis. Four themes:

1. Identity: canonicalize (session_id, conversation_id, thread id) once
   at the chat boundary (leaf 01) so steering, progress delivery, and
   cross-surface context work from every client.
2. Honesty: false completions become failures or labeled replies
   (leaf 02); invisible states (plan.failed, interview questions,
   degraded classifier, mode) reach every surface (leaves 03, 04, 05).
3. Degrade-not-die: planner empty-plan fallback, user-language error
   mapping, unknown-schedule quota note (leaf 06).
4. Clutter and parity: payload-boundary scaffolding strip, GUI verbosity
   filter, TUI/GUI parity gaps (leaves 07, 08); CLI affordances
   (leaf 09); plan-lifecycle hardening (leaf 10).

Leaves are ordered by dispatch wave; letters mark concurrency groups
(FILE-DISJOINT within a group).

## Interface Contracts

C-1 Canonical chat id: leaf 01 exports
  `Dispatcher.ResolveActiveQueue(id string) (ActiveQueue, bool)` and
  normalizes handler.go submit to always resolve via
  `session.ResolveThreadConversationID(sessionID, GetByConversationID, Get)`
  before LinkSession / queue registration / progress publication.
  Consumers: leaves 03, 04 (subscribe/relay fixes assume canonical ids).

C-2 Completion honesty: leaf 02 changes NO exported types; it tightens
  the unbacked-claim guard (loop.go) and review gate (review.go) to fail
  steps whose claimed file effects have zero backing tool executions, and
  marks steps errored when the step's primary tool call was
  permission-denied. Leaves 03/05 render the existing
  `[unverified: no tools were executed]` label unchanged.

C-3 Session-visible planner pauses: leaf 04 adds one persisted shape:
  interview questions written as an assistant session message with
  metadata key `planner_interview` + task_id; the answer path matches the
  next user message in the same conversation to the waiting planner.
  No new bus topic (audit FI guidance: reuse chat surfaces).

C-4 User-language error funnel: leaf 06 exports
  `UserFacingError(err error) string` from internal/agent (mapping table:
  IsContextTooSmall, TierFloor, ErrAllModelsQuotaBlocked,
  ErrEmptyResponse, generic). Terminal funnel calls it before composing
  the failed-turn reply.

C-5 Stripped payloads: leaf 07 applies StripPromptScaffolding /
  StripClaimsEvidenceOrProse at the task/step RPC + HTTP response
  boundary (internal/rpc/task.go, internal/comm/http api handlers).
  Clients make zero changes for the strip itself.

C-6 Plan defaults: leaf 10 sets RequireApproval=true in
  NormalizePlansDefaults (internal/config/schema.go); doc update to
  docs/agents/opt-in-defaults.md rides the same commit.

All leaves: e2e policy applies (hermetic e2e tier, no new unit-test files
for new features — internal/agent/AGENTS.md and root AGENTS.md). TUI/GUI
parity rule: every client-facing leaf states both surfaces.

## Child Index

| Leaf | Group | Title | Est. ctx | Depends |
|------|-------|-------|----------|---------|
| 01-identity-canonicalization.md | A | Canonical chat identity + steer/followup dual-resolution | 90K | - |
| 02-completion-honesty.md | A | False-completion guards (unbacked claims, denied tools) | 85K | - |
| 03-plan-failure-visibility.md | B | plan.failed + task.planned mode in TUI chat pump + GUI cards | 70K | 01 |
| 04-interview-questions-surfacing.md | B | Planner interview questions as session messages + answer path | 75K | 01 |
| 05-degraded-signals.md | B | Classifier-degraded note + meta.classification_method footer | 60K | 01 |
| 06-graceful-errors.md | B | Empty-plan fallback + UserFacingError funnel + quota note | 80K | - |
| 07-scaffolding-payload-strip.md | C | Strip scaffolding at RPC/HTTP payload boundary | 65K | - |
| 08-gui-tui-parity.md | C | GUI verbosity filter, mid-turn errors, TUI truncation/keys | 85K | 07 |
| 09-cli-affordances.md | C | /plan subcommands, --session create, plans list --state | 70K | - |
| 10-plan-lifecycle-hardening.md | C | require_approval default, zero-phase guard, evolver reclaimer, stalled sweep | 90K | - |

Groups: A and B-waves serialize on internal/agent/handler.go and
dispatcher.go (01 owns those files first). C-group leaves are
file-disjoint from A/B and from each other.

## Dispatch Protocol

0. Test Gate: each branch dispatches its gate tests FIRST. This tree is
   flat (no branches); the master holds the gate. Gate leaves (one per
   implementation leaf, `NN-gate-*.md` naming is NOT used — instead each
   leaf's Tasks section starts with RED-gate test tasks) — see Test Gate
   section. Status: TESTS_IN_PROGRESS -> TESTS_VALIDATED per leaf before
   its implementation tasks run.
1. Dispatch implementation agents via delegate_task with the leaf doc as
   context; briefs name the worktree path, pinned contracts (C-1..C-6),
   and: "Do NOT commit. Do NOT run git add. Write code, run tests,
   report results only."
2. Orchestrator (main model) reviews in-session: read diffs, verify
   contracts, run scoped tests, grep for artifacts.
3. Re-dispatch with feedback on gaps (max 3 iterations).
4. Orchestrator commits per leaf with explicit pathspecs (repo rule:
   stage explicit files; sibling sessions share the tree).
5. Update the Completion Tracking Table after every transition.

Wave order: wave 1 = group A (01, 02) + 06 + 07 (all file-disjoint).
wave 2 = group B (03, 04, 05) after 01 lands. wave 3 = 08, 09, 10.

## Coding Conventions

- Go: gofmt, repo style; no `--no-verify` commits; error handling per
  docs/agents/coding-practices.md (capture-and-log, never `_ =`).
- TUI text lowercase (repo UI rule). No em-dashes in new files
  (pre-commit ascii gate).
- e2e-first policy: new feature tests go in `e2e/suites/<name>/` with
  manifest + path_map entries in the same commit.
- Shared-tree discipline: leaves never `git add` directories; explicit
  file lists only.
- Every leaf greps its target symbols at start (verify-or-apply rule:
  a sibling tree may have landed overlapping fixes).

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 01 identity | PENDING | |
| 02 completion honesty | PENDING | |
| 03 plan failure visibility | PENDING | |
| 04 interview questions | PENDING | |
| 05 degraded signals | PENDING | |
| 06 graceful errors | PENDING | |
| 07 scaffolding strip | PENDING | |
| 08 gui/tui parity | PENDING | |
| 09 cli affordances | PENDING | |
| 10 plan lifecycle | PENDING | |

## Test Gate

Coverage matrix (feature -> e2e suite -> status). Gate tests are authored
as the FIRST tasks inside each leaf and must be seen RED before
implementation; the orchestrator validates each leaf's gate before its
implementation tasks run.

| Feature | Gate test (e2e suite) | Status |
|---------|----------------------|--------|
| Steer by primary session id | e2e/suites/chat-identity/ | PENDING |
| Unbacked claim fails step | e2e/suites/completion-honesty/ | PENDING |
| plan.failed renders in chat | e2e/suites/plan-visibility/ | PENDING |
| Interview question visible as message | e2e/suites/planner-interview/ | PENDING |
| Empty-plan falls back, no error exit | e2e/suites/graceful-errors/ | PENDING |
| Step descriptions stripped in RPC | e2e/suites/payload-strip/ | PENDING |
| GUI verbosity drops high-tier frames | widget test in ui/flutter_ui | PENDING |
| require_approval defaults true | config unit pin | PENDING |

## Integration Test Plan

After all leaves REVIEWED, in the worktree:
1. `go build ./... && go vet` scoped to touched packages.
2. `make e2e-fast-area AREA=<each new suite>` for all suites above.
3. `make graphs` + `make graphs-check` (bus topic changes: task.planned
   consumers, plan.failed subscribers).
4. Live gate: scratch-rig boot, one steer probe from CLI by primary
   session id, one multi-turn probe (codeword recall), one plan-failure
   probe; assert artifacts and honest replies.
5. Flutter: `flutter analyze` + widget tests for the verbosity filter.
6. TUI/GUI parity checklist sign-off per repo rule.

## Review Checklist

- Every leaf diff compiles; scoped tests green; no artifacts/TODOs.
- Contracts C-1..C-6 verified at the code level (grep the exported
  symbols; three-site rule for seam consumers).
- No leaf committed .go with --no-verify; deadcode gate clean.
- Parity: each client-facing change names both surfaces.
- Report findings P0-1..P2-11 each map to a leaf (traceability below).

## Requirement Traceability

P0-1 -> 06; P0-2 -> 04; P0-3 -> 01; P0-4 -> 02; P0-5 -> 03;
P1-1 -> 01; P1-2 -> 07; P1-3 -> 08; P1-4 -> 03; P1-5 -> 09;
P1-6 -> 10; P1-7 -> 01; P2-1 -> 09; P2-2 -> 08; P2-3 -> 08;
P2-4 -> 05; P2-5 -> 06; P2-6 -> 06; P2-7 -> 08; P2-8 -> 08;
P2-9 -> 10; P2-10 -> 08+09; P2-11 -> 09. FI items are out of tree scope
(recorded in the report for separate investigation).

## Open Questions

- None blocking. FI-1 (classifier starvation on single-runtime rigs)
  affects test design, not leaf scope: gate suites must use the
  multi-runtime hermetic harness, not a single-model scratch rig.
