# 04-plan-mode - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. This branch runs a tests-first gate —
> the failing tests ALREADY EXIST (00T-tests.md, goals-plans e2e
> `plan_mode_test.go` G4) and are committed — your job is to make them pass; do
> NOT delete, skip, weaken, or rewrite any gate test (report suspected-wrong
> gate tests to the orchestrator instead). Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built, files
> touched, and any deviations.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Per-session plan-mode collaboration toggle + `/plan` command +
  model-facing `exit_plan_mode` present-for-review tool that drives the
  existing ApprovePlan gate. Plan-mode is a session collaboration state; it is
  DISTINCT from goal-mode (master C4 consumer note).
- **Dependencies:** none (uses today's session store + existing plan gate; does
  NOT depend on branch 03 C5)
- **Estimated Context:** 35K
- **Concurrency Group:** A (parallel with 01-goal-model.md)
- **Audit references:** /tmp/dsh-audit/goal-workflow-plan.md Feature 2
  (plan-mode session toggle + exit_plan_mode present-for-review)

## Goal

Meept already has the durability half of plan approval — human CLI/RPC/TUI
approve/reject/confirm over a durable plan store
(`internal/plan/manager.go` ApprovePlan/RejectPlan/ConfirmPlan). What it
lacks (audit Feature 2, GAP partial) is the persistent per-session plan MODE:
a toggle that (a) injects a plan-policy prompt section while the model
explores/designs before executing, and (b) lets the model present its markdown
plan for an inline **Approve / Keep planning** decision inside the turn, whose
approval lands the plan on the existing durable gate so the orchestrator still
executes it. This leaf adds that collaboration mode without rewriting the plan
store.

## Context

- **File budget (hard): THREE files of change max.** All plan-mode behavior —
  the per-session `plan_mode` toggle, the plan:policy prompt injection, the
  `exit_plan_mode` tool, and the `/plan` command surface — is owned by ONE new
  file: `internal/tools/builtin/plan_mode.go` [NEW] (a `PlanModeController`
  struct with a per-session in-memory map, persisted/restored via an existing
  session metadata accessor, plus the `/plan` command handler and tool
  registration via the builtin registrar seam). A second file is the session
  store additive `plan_mode` column accessor ONLY if no existing session
  metadata field can carry it — the implementer reads
  `internal/session/store*.go` to decide (prefer reusing an existing per-session
  metadata/flag mechanism; add a column only if none exists and document it).
  A third file (optional) is the CLI/TUI command surface for `/plan` if it
  cannot ride the existing command registry.
- **Plan-policy prompt section:** inject a plan:policy guidance section into
  the model prompt while plan_mode is active, via the existing system-prompt
  injection seam (`internal/agent/context_injector.go`; explore the current
  seam, do not open a second copy).
- **Present-for-review:** `exit_plan_mode` presents the model's markdown plan
  (must start with a `#` heading) as an inline user question with options
  **Approve** / **Keep planning** (reuse the ask/user-question channel —
  `internal/tools/builtin/ask.go`). On **Approve**: exit plan_mode, then land
  the plan on the existing plan gate (`PlanManager.ApprovePlan`, or create a
  plan via the manager's CreatePlan then approve it). On **Keep planning**:
  the feedback goes back to the model (revise + present again); plan_mode stays
  active.
- **/plan command:** toggle plan mode on/off; non-'off' message/attachments
  steer to the agent while in plan mode (mirror dsh `:232-278`). TUI + GUI
  parity (root AGENTS.md) requires documenting the surface; a CLI `meept`
  plan-mode toggle is the minimal user-facing hedge.
- Repo rule: NEW feature tests live in the e2e tier (00T G4 covers plan-mode);
  do not add standalone unit test files for this feature.

## Verified Anchors

- internal/plan/manager.go:381-430 — `ApprovePlan(ctx, planID, sessionID, by string)`
- internal/plan/manager.go:431-470 — `RejectPlan(ctx, planID, sessionID, by, reason)`
- internal/plan/manager.go:509-560 — `ConfirmPlan(ctx, planID, sessionID, by)`
- internal/tools/builtin/ask.go — user-question / ask tool channel (re-read live signature before coding)
- internal/agent/context_injector.go — system-prompt/context injection seam (re-read: prompt section append)
- internal/session store — per-session row persistence (return/store path to add plan_mode if needed)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// Per-session plan-mode toggle + model tool. Contract C4d verbatim:
// Persistent state: session.plan_mode bool (today's session store or existing
//   per-session metadata; NOT a branch-03 dependency).
// /plan command: toggles plan_mode; non-'off' message/attachments steer to the
//   agent while active (TUI/GUI/CLI surface documented per parity rule).
// While active, a deployment-owned plan:policy prompt section is injected.
// Model tool:
//   exit_plan_mode
//     params: plan(string, req) markdown starting with a # heading
//     presents Approve / Keep planning inline; on Approve leaves plan_mode and
//       lands the plan on PlanManager.ApprovePlan (or CreatePlan+ApprovePlan);
//       on Keep planning returns the user's feedback to the model to revise.
//     only available while plan_mode is active (progressively disclosed).
// Goal-mode vs plan-mode remain DISTINCT (master C4): entering plan_mode does
//   not touch the goal, and the round driver (leaf 03) does not run in
//   plan-mode.
```

### What This Leaf Consumes

- `internal/plan.PlanManager` `ApprovePlan`/`RejectPlan`/`ConfirmPlan` (existing; verified).
- Session store / per-session metadata for the `plan_mode` bool (existing; additive column only if no metadata seam).
- User-question/ask channel for the inline Approve/Keep-planning decision (existing).

## Tasks

| Task ID | Objective | Files (≤3) | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-----------|----------------|------------|------------|-----------------|
| T1 | Implement `PlanModeController` in ONE new file: per-session `plan_mode` bool (default off, persisted/restored via existing session metadata or an additive column), `/plan` command handler (toggle + non-'off' steer), and the plan-policy prompt-injection hook that fires only while active | Create: `internal/tools/builtin/plan_mode.go`; Modify: registrar (register controller + tool); [optional] session store column file ONLY if no metadata seam exists | `go test -p 2 -tags e2e ./e2e/suites/goals-plans/ -run PlanMode -v` (gate G4) | FAIL: no plan_mode state / no /plan command | PASS: a fresh session reads plan_mode=false; `/plan` flips it on, `/plan off` clears it; the plan:policy section is present only while active | Given a fresh session; When `/plan read this`; Then plan_mode activates, the agent receives the message, and the plan:policy section is injected |
| T2 | Implement the `exit_plan_mode` model tool in the same file: validates markdown has a `#` heading; presents Approve / Keep-planning via the ask channel; on Keep-planning returns the user's feedback for revise; only available while plan_mode active (progressive disclosure) | Modify: `internal/tools/builtin/plan_mode.go` | gate G4 same | FAIL: exit_plan_mode not registered / no inline review | PASS: calling it with a plan and approving exits plan_mode; keep-planning stays active and returns feedback; non-`#` plan rejected | Given plan_mode active + a valid markdown plan; When the user keeps planning; Then plan_mode stays active and the model receives the feedback to revise |
| T3 | On Approve, land the plan on the existing durable gate (PlanManager.ApprovePlan or CreatePlan+ApprovePlan) and surface it for the orchestrator execution path; TUI + GUI surface documented | Modify: `internal/tools/builtin/plan_mode.go` | gate G4 full | FAIL: approved plan not persisted/approved | PASS: after approval the durable plan reaches an approved state via the existing gate; `meept plans` shows it; plan_mode reads off | Given an approved exit_plan_mode call; When the plan gate is consulted; Then the plan is StateApproved and plan_mode reads off |

## Self-Verification Checklist

- [ ] All tasks implemented and gate G4 passing (RED -> GREEN)
- [ ] Interface contract C4d satisfied exactly (permission/params/naming)
- [ ] File budget respected: ≤3 files changed (one new file owns the feature)
- [ ] Per-session persistence additive; no dependency on branch 03 C5
- [ ] plan-mode distinct from goal-mode — no interaction with leaf 03 or the goal tools
- [ ] User-facing surface documented (TUI + GUI parity or explicit deviation)
- [ ] No scope creep — no goal changes, no round-driver work here

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path (list must stay ≤3)
2. **Verify command + output:** `go test -p 2 -tags e2e ./e2e/suites/goals-plans/ -run PlanMode -v` last 20 lines; `gofmt -l` clean
3. **Deviations:** none / list with rationale (surface choice, plan-landing path, session-persistence seam)

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate G4 untouched (git diff empty)
- [ ] Contract C4d matches exactly; /plan + exit_plan_mode per spec
- [ ] ≤3 files changed; plan-mode owned by one new file, not scattered
- [ ] Session persistence is additive; no branch-03 dependency crept in
- [ ] TUI + GUI parity satisfied or documented deviation (root AGENTS.md)
- [ ] Conventions: no ignored errors, predictable-ID, no scope creep, no debug/line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- dsh source to re-read: `/tmp/deepseek-harness/packages/plan/plan-mode/src/index.ts`
  — the `/plan` command (:232-278), `exit_plan_mode` (:280-375, the `#` heading
  check :307, the Approve/Keep-planning options :313-333, feedback throw
  :354-356), and the progressive disclosure `when` (:375). Port the semantics;
  meept's ask channel and plan gate replace dsh's `userQuestions` + projection.
- `internal/tools/builtin/ask.go` live signature MUST be read before coding the
  inline review — the anchor is the re-read point, not the final say.
- If branch 03 (C5 versioned session log) becomes available during execution,
  this leaf MAY re-point plan_mode persistence to it as a dated addendum — it
  never blocks or is blocked by 03.
