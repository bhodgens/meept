# 06-planning TEST-GATE progress (durability checkpoint)

Worktree: /Users/caimlas/git/meept/.hermes/worktrees/usability-remediation
Branch base HEAD: 76b1c0f7. Tests only; no commits; no git add.
Sibling compile-RED gate files excluded ONLY via -overlay for verification
(/tmp/gate06-overlay/overlay.json); their on-disk content is untouched.

## T1 - approval card + threshold config + reject-from-card (F13a) - RED
- internal/tui/approval_card_test.go: 5 tests, all RED.
  cmd: go test ./internal/tui/ -run 'TestPendingApprovalCard|TestRejectFromCard|TestApproveFromCard|TestPlansViewTaskGate' -count=1
  last: FAIL github.com/caimlas/meept/internal/tui 1.166s
  reason: orphan task.pending_approval; dead /plan hint; zero Approve/RejectTask callers.
- internal/agent/threshold_config_test.go: 3 tests RED (0/-1 clamped to 5; field absent).
  cmd: go test -overlay /tmp/gate06-overlay/overlay.json ./internal/agent/ -run TestThresholdConfig -count=1
  last: FAIL github.com/caimlas/meept/internal/agent 0.753s

## T2 - GUI approval + seal (F13b) - RED
- ui/flutter_ui/test/features/approval_card_test.dart (5 tests)
- ui/flutter_ui/test/features/seal_surface_test.dart (5 tests)
  cmd: cd ui/flutter_ui && flutter test test/features/approval_card_test.dart test/features/seal_surface_test.dart
  last: 00:00 +0 -10: Some tests failed. EXIT=1
  reason: NoSuchMethodError approveTask/rejectTask/getDraft/sealTask; no card/seal widget.

## T3 - compiler draft chat surface (F13c) - RED
- internal/agent/compiler_surface_test.go: 2 RED (seeded/refinement publish no event)
  + 1 GREEN guard (raw scaffold never auto-sealed - correct, must stay green).
  cmd: go test -overlay ... ./internal/agent/ -run TestDraftChatSurface -count=1
  last: FAIL github.com/caimlas/meept/internal/agent 4.386s

## T4 - mode badge + failure copy + evolver + triviality (F13c) - RED
- internal/agent/plan_failure_copy_test.go: 2 RED (revision/ralph internal vocab)
  + 1 GREEN guard (honest compile path).
- internal/agent/triviality_escape_test.go: 2 RED (trivial trips gate; cap-after-gate)
  + 1 GREEN guard (artifact request still gates).
  cmd: go test -overlay ... ./internal/agent/ -run 'TestPlanFailureCopy|TestTrivialityEscape' -count=1
  last: FAIL github.com/caimlas/meept/internal/agent 1.355s
- internal/tui/mode_badge_test.go: 7 RED (4 mode subtests + 3 evolver renders).
  cmd: go test ./internal/tui/ -run 'TestModeBadge|TestEvolverDocRender' -count=1
  last: FAIL github.com/caimlas/meept/internal/tui 5.902s
- ui/flutter_ui/test/features/mode_chip_test.dart: run in progress (/tmp/gate06-t4-modechip.log).

## T5 - e2e extensions + manifest rows - PENDING

## Notes
- gofmt applied to all Go gate files; dart format to all Dart gate files.
- go vet ./internal/tui/ clean. Agent gate files compile via overlay (sibling
  registry_remap/turn_watchdog_exempt/ralph_storm are compile-RED, not ours).
- Evolver wording pinned to frozen planCountLine(0,0) string (ASCII \u00b7).
- Reject-never-executes (C2) + triviality cap-before-gate + raw-scaffold-never-
  auto-seal are the load-bearing pins; all present.