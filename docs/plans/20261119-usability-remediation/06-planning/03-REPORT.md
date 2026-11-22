# 03-REPORT - 06-planning/03 compiler-surface-delineation (FINAL impl leaf)

Worktree: .hermes/worktrees/usability-remediation (HEAD 01ece763, uncommitted).
Gate tests untouched (f326aef8); nothing committed per leaf contract.

## 1. Gate sweep (first action)

True RED (12): TestModeBadgeDistinguishesModes, TestEvolverDocRenderPlansTab /
PlansDetail / Sidebar, TestDraftChatSurfaceSeeded / Refinement,
TestPlanFailureCopyRevisionExhaustion, TestThresholdConfig /
DefaultKey / ZeroNotClamped, TestTrivialityEscapeNeverGatesTrivialInput /
CapBeforeGateOrdering.

Already GREEN (7, untouched): TestPlanFailureCopyRalphCap (leaf 07),
TestTrivialityEscapeHeuristicConservative, TestDraftChatSurfaceRawScaffoldNeverAutoSealed,
TestPlanFailureCopyHonestCompilePath, TestResubscribeOnLostSubscription
(01-safety/03), TestPlanFailureCopy* honest path pins.

## 2. Files touched

Agent:
- internal/agent/strategic.go - removed the ApprovalStepThreshold <= 0 -> 5
  clamp (0 = disabled; default lives in config); triviality cap call site
  BEFORE requiresApproval (ordering pin).
- internal/agent/plan_triviality_cap.go (NEW) - capTrivialPlanSteps: trivial
  input (isTrivialQuestion, 04-clutter/02 heuristic) over-decomposed past 1
  step is capped to 1 step pre-gate; conservative, complex work untouched.
- internal/config/schema.go - OrchestratorConfig.TaskApprovalStepThreshold
  (json/toml task_approval_step_threshold), DefaultConfig sets 5.
- internal/daemon/components.go - wires the config field into
  StrategicPlannerConfig.ApprovalStepThreshold.
- internal/agent/review_manager.go - revision-exhaustion copy is now
  user-language: "Could not verify the work after N attempts - please review
  the task ..." (no "revision cycles"/"human intervention").
- internal/agent/plan_draft.go - SaveDraft publishes task.draft_status
  (task_id, status=draft, draft_version, sealed, action=seal) on every save
  (seed AND refinement); sealing stays explicit human action.
- internal/agent/max_revision_guard_test.go, spec_review_integration_test.go -
  regular tests pinned the OLD internal-vocab copy verbatim; updated to the
  leaf contract (attempt count + review next action, vocab asserted absent).

TUI:
- internal/tui/models/chat_nav.go - modeBadgeLabel (C1 intent_type/handler_case
  -> plan/quickplan/direct/recall; raw enum never rendered) + renderModeBadge.
- internal/tui/models/chat.go - badge rendered under assistant bubbles;
  DocumentProposalLine() exports the shared planCountLine(0,0) wording;
  session-load freshness guard (below).
- internal/tui/models/plans.go - doc-proposal status line in the tab view +
  scope line in the detail modal (both reuse planCountLine, no fork).
- internal/tui/sidebar.go - refreshData no longer drops 0-phase plans;
  PlanDocNote carries DocumentProposalLine; panel auto-expands on a doc note.
- internal/tui/components/plan_view.go - SetDocumentNote + title/document
  render for phase-less plans.

Determinism fixes required by the gates (production-honest; gate files NOT edited):
- internal/tui/models/chat_turn.go - DeliverTurnTerminal renders the resolved
  terminal bubble synchronously on the event loop (the delayed await-goroutine
  msg is deduped by turn removal). Transcript exact immediately after the
  terminal event.
- internal/tui/models/chat.go - loadServerMessages preserves messages created
  after the session load was armed (a late load must not erase a just-rendered
  turn; mergeFresherThanSnapshot).
- internal/tui/app.go - SessionLoadedMsg.AutoResume marks startup
  resume/create results; a stale startup result landing after an explicit load
  bound a different session is dropped (mid-turn rebind orphaned transcripts).
- internal/tui/headless_test.go - HARNESS BUG: the sidebar model was left on
  the dead placeholder RPC client in newHeadlessAppSocket; re-wired to the
  live stub (mirrors every other sub-model).
- internal/tui/testdata/golden/*.txt (9 files) - regenerated: the only diff is
  the sidebar panel now rendering its real connected state (previous goldens
  froze the harness artifact "disconnected").

## 3. Verify output

- All leaf gates: 28/28 subtests PASS, 0 FAIL (final run; badge gates also
  GREEN across 3 repeat runs).
- go test ./internal/tui/ -count=1: ok (whole package).
- go test ./internal/agent/ -count=1: only 2 failures, BOTH pre-existing on
  HEAD (A/B scratch worktree at 01ece763 confirmed):
  TestGuardRetry_RetryParksSkipsPostTurnPipeline,
  TestTacticalScheduler_VacuousValidatorPassNotValidated (nil-pointer panic).
  Not in this leaf's scope; flagged for the orchestrator.
- go build ./... : clean. gofmt: clean. Added source lines ASCII-only
  (golden fixtures carry their pre-existing box-drawing glyphs).

## 4. Live-gate evidence

- Compiler draft: Plan (compiler branch) seeds -> task.draft_status observed;
  SaveDraft refinement -> second event, same task, version >= 2; raw scaffold
  SealedHash == "" and no orchestrator.schedule (auto-seal pin GREEN).
- Trivial input "what is 2+2?" with a 21-step over-decomposed planner output,
  MaxPlanSteps=4, threshold=3: no task.pending_approval, task not parked,
  <= 1 step persisted; the same 21-step output for a complex input still gates.
- Mode badge: plan/quickplan/direct/recall terminal turns render
  [plan]/[quickplan]/[direct]/[recall] in the transcript; raw handler_case
  enums never leak (gate-asserted).

## 5. Deviations / PENDING

- Threshold zero-disable semantics required removing the planner clamp; the
  historical default 5 is preserved in DefaultConfig (master Q1 shape).
- GUI (flutter) failure-copy/plans-tab parity is daemon-side-consistent per
  the leaf contract; 05-navigation/02 already landed the GUI chip.
- Two pre-existing internal/agent failures (above) are NOT leaf regressions -
  recommend orchestrator triage before the bench acceptance run.
- NOT committed, per leaf contract.
