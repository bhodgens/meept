# 06-planning/03 Progress - compiler surface delineation (FINAL impl leaf)

## Gate-status sweep (first action, 2026-11-21)

### True RED (implement)
- internal/tui/mode_badge_test.go TestModeBadgeDistinguishesModes (mode badge)
- internal/tui/mode_badge_test.go TestEvolverDocRenderPlansTab / PlansDetail / Sidebar (evolver render)
- internal/agent/compiler_surface_test.go TestDraftChatSurfaceSeeded / TestDraftChatSurfaceRefinement (draft surface)
- internal/agent/plan_failure_copy_test.go TestPlanFailureCopyRevisionExhaustion
- internal/agent/threshold_config_test.go TestThresholdConfig / DefaultKey / ZeroNotClamped (NOT green from siblings)
- internal/agent/triviality_escape_test.go TestTrivialityEscapeNeverGatesTrivialInput / CapBeforeGateOrdering

### Already GREEN (do not touch)
- TestPlanFailureCopyRalphCap (leaf 07 capFailureUserCopy)
- TestTrivialityEscapeHeuristicConservative
- TestDraftChatSurfaceRawScaffoldNeverAutoSealed
- TestPlanFailureCopyHonestCompilePath
- internal/tui/events_resubscribe_test.go TestResubscribeOnLostSubscription (01-safety/03)

## Tasks
- [x] T-threshold: removed 0->5 clamp (strategic.go); config orchestrator.task_approval_step_threshold (default 5, 0 disables); daemon components.go wiring. GREEN
- [x] T-triviality: capTrivialPlanSteps (plan_triviality_cap.go) BEFORE requiresApproval; reuses isTrivialQuestion (04-clutter/02). GREEN
- [x] T-failurecopy: review_manager.go revision-exhaustion copy ("could not verify the work after N attempts - review..."). GREEN
- [x] T-draft: task.draft_status event on SaveDraft (seed + refinement), status/draft_version/sealed/action=seal. GREEN (auto-seal pin stayed GREEN)
- [x] T-badge: modeBadgeLabel/renderModeBadge (chat_nav.go) rendered in updateViewport (chat.go); mapped labels only. GREEN (3 runs)
- [x] T-evolver: plans tab doc-proposal status line + detail scope line + sidebar doc note (DocumentProposalLine reuses planCountLine); refreshData no longer drops 0-phase plans; PlanView SetDocumentNote. GREEN

## Extra fixes required to make gates deterministic (production-honest, not gate edits)
- chat_turn.go DeliverTurnTerminal: synchronous render when the delivery resolves a pending turn (transcript exact immediately after terminal event)
- chat_turn.go/chat.go: session-load freshness guard - loadServerMessages preserves messages created after the load was armed (late load must not erase a just-rendered turn)
- app.go: SessionLoadedMsg.AutoResume marker; stale startup load result dropped when an explicit load bound a different session (mid-turn rebind orphaned transcripts)
- headless_test.go: harness bug - sidebar was left on the dead placeholder RPC client; re-wired to the live stub (mirror of other sub-models)
