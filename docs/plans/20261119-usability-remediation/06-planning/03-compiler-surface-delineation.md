# Compiler Draft Surface + Mode Delineation + Failure Copy - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T3/T4) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.
> LARGE LEAF: if wall-time runs short, finish T1-T2 first, report T3-T4
> PENDING with disk state.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Compiler drafts get a chat surface; mode is visible; plan failures speak user language; trivial inputs escape over-decomposition
- **Dependencies:** 00T-gate.md, 02-routing/03 (continuation), 01-approval-visibility.md
- **Estimated Context:** 95K
- **Concurrency Group:** B
- **Audit references:** W17; P1/P4/P7/P8/P9; F-ORCH-1/14; observability 3.4/4.x

## Goal

Give the plan-compiler draft a chat-visible status + seal action keyed by
task_id (option A: explicit human seal; auto-fill brainstorm is follow-up);
render a mode badge (plan/quickplan/direct/recall) from C1 fields in both
clients; replace internal failure vocabulary with actionable copy; reuse
planCountLine for evolver document proposals in plans tab/detail; cap
trivial-input plan steps BEFORE the approval gate counts them.

## Context

- internal/agent/plan_draft.go:483 seedDraftFromRequest (raw scaffold);
  strategic.go:924-975 compiler branch parks StatePlanning; rpc/plan_seal.go
  plan.draft/plan.seal.
- internal/agent/handler.go:2392-2426 modeToLabel/FormatEnhancedAsyncTaskAck
  (ack markdown only signal); C1 intent_type/handler_case on terminal.
- internal/tui/models/chat.go:3105-3118 planCountLine (evolver wording);
  models/plans.go:380-386/777-780 literal zeros; sidebar.go:456-458 skips.
- ui/flutter_ui/lib/features/plans/plans_tab.dart:291-301 (0-phase bare card);
  chat_message_bubble.dart:130-135 mode-chip TODO (05/02 T1 adds chip - this
  leaf supplies the daemon-side mode signal consistency + failure copy).
- internal/agent/strategic.go:1673 max_plan_steps truncation AFTER gate
  count (:2185); awaitUserApproval :2194.
- Failure vocab: "max ralph loop iterations reached without sufficient
  evidence" (review_manager.go MaxRevisionCycles ~301); failTaskWithReason
  strategic.go:1905-1929 (honest compile path exists).

## Verified Anchors

- internal/agent/plan_draft.go:483 - seedDraftFromRequest
- internal/agent/strategic.go:1673 - truncation site
- internal/agent/strategic.go:2185 - gate count
- internal/tui/models/chat.go:3105 - planCountLine
- internal/agent/review_manager.go:301 - MaxRevisionCycles message

## Interface Contracts (From Parent)

### What This Leaf Exposes
- Compiler draft chat surface (status + seal action keyed by task_id);
  mode signal consistency (C1 fields drive badge); actionable plan-failure
  copy; evolver doc-proposal render in tab/detail; triviality cap ordering.

### What This Leaf Consumes
- C1 (mode fields), C2 (seal/approve), C6 (plan.failed), C7 (exemption),
  02-routing/03 continuation (refinement reaches draft).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Compiler draft chat surface | Modify: internal/agent/strategic.go compiler branch (emit draft-status event/reply keyed by task_id); internal/tui/app.go + models/chat.go (draft card with seal action); plan_draft.go | `go test ./internal/agent/ ./internal/tui/ -run TestDraftChatSurface -count=1` | gate T3 RED (parks forever) | GREEN | Given compiler draft seeded; Then chat shows draft status + seal affordance (calls plan.seal via C2 surface); raw scaffold never auto-sealed; refinement (02/03) updates same draft |
| T2 | Mode badge consistency | Modify: internal/agent/handler.go mode signal (ensure intent_type/handler_case populated for ALL turns incl sync/direct); TUI/GUI badge consumes C1 (GUI chip landed by 05/02) | `go test ./internal/tui/ -run TestModeBadge -count=1` | gate T4 RED (no badge) | GREEN | Given plan/quickplan/direct/recall turn; Then badge distinguishes; sync/direct turns carry mode signal (not only async ack) |
| T3 | Actionable failure copy + evolver render | Modify: internal/agent/review_manager.go failure message; strategic.go failTaskWithReason copy; internal/tui/models/plans.go + sidebar.go (reuse planCountLine); GUI plans_tab.dart 0-phase | `go test ./internal/agent/ ./internal/tui/ -run 'TestPlanFailureCopy|TestEvolverDocRender' -count=1` | gate T4 RED | GREEN | Given revision exhaustion; Then copy says "could not verify the work after N attempts - review the task" (never "max ralph loop"); evolver 0-phase renders "document proposal" + open-file action in tab/detail/sidebar |
| T4 | Triviality escape ordering | Modify: internal/agent/strategic.go (cap trivial-input steps BEFORE gate count; triviality heuristic pinned) | `go test ./internal/agent/ -run TestTrivialityEscape -count=1` | gate T4 RED (21 steps counted with cap 4) | GREEN | Given trivial input; Then plan capped before gate; 2+2 never trips approval; complex plans still gate correctly |

## Self-Verification Checklist

- [ ] Gate T3/T4 GREEN; gate diff empty
- [ ] C1/C2 exact; raw scaffold never auto-sealed; failure copy user-language
- [ ] Live-gate: rig compiler draft -> chat seal surface; trivial question -> direct badge, no gate
- [ ] No scope creep / artifacts / corruption; gofmt + dart format

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output per task. 3. Live-gate evidence. 4. Deviations/PENDING.

## Review Checklist (For Review Agent)

- [ ] Triviality heuristic conservative (never caps genuine multi-step work)
- [ ] Failure copy actionable + names the task; internal vocab gone
- [ ] Evolver render reuses planCountLine (no wording fork); filePath rendered
- [ ] Mode signal never leaks raw handler_case enums to users (mapped labels)
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- Auto-fill brainstorm (option B) is a FOLLOW-UP leaf, not here (branch
  Open Questions). Option A (explicit human seal) ships first.
- If T3/T4 timeout: report disk state; orchestrator re-dispatches as 03b.
