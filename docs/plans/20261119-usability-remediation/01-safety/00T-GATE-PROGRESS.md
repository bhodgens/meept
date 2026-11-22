# 00T Gate Progress (durability checkpoint)

Worktree: /Users/caimlas/git/meept/.hermes/worktrees/usability-remediation
Branch: 01-safety TEST-GATE (tests only, RED). Do NOT commit.

| Task | Files | Status | Verify command | Last output line |
|------|-------|--------|----------------|------------------|
| T1 | internal/rpc/task_approval_reject_test.go | RED (undefined RejectFunc) | go test ./internal/rpc/ -run TestReject -count=1 | FAIL github.com/caimlas/meept/internal/rpc [build failed] |
| T2 | internal/agent/handler_rejected_relay_test.go | RED (agent pkg build: undefined new seams; relay test itself pins missing task.rejected subscription) | go test ./internal/agent/ -run TestRejectedRelay -count=1 | FAIL github.com/caimlas/meept/internal/agent [build failed] (undefined ResolveActiveQueue/SetTaskStateFunc/MaxReplanExtensions etc.) |
| T3 | internal/agent/queue_lifecycle_test.go | written; RED via agent batch build | go test ./internal/agent/ -run 'TestFreshQueuePerTurn\|TestCloneQueueIsolation\|TestSteerFullText' -count=1 | build failed (same batch) |
| T4 | internal/agent/registry_remap_test.go | RED (undefined ResolveActiveQueue/ErrAmbiguousQueue) | go test ./internal/agent/ -run TestThreadRemap -count=1 | build failed (undefined symbols) |
| T5 | internal/rpc/proxy_critical_test.go; internal/bus/critical_test.go | RED bus (undefined IsCriticalTopic); rpc behavioral RED pending isolated run | go test ./internal/rpc/ ./internal/bus/ -run 'TestCriticalRing\|TestBlockingPublishers' -count=1 | FAIL github.com/caimlas/meept/internal/bus [build failed] |
| T6 | internal/tui/events_resubscribe_test.go | RED (current defect: subscribe never re-ran) | go test ./internal/tui/ -run TestResubscribe -count=1 | FAIL github.com/caimlas/meept/internal/tui 3.773s |
| T7 | internal/agent/turn_watchdog_exempt_test.go | RED (undefined SetTaskStateFunc/SetTurnLivenessTouch) | go test ./internal/agent/ -run 'TestApprovalWaitNotReaped\|TestSealActivityTouch' -count=1 | build failed (undefined seams) |
| T8 | internal/agent/failure_terminal_test.go; internal/plan/manager_fail_event_test.go | agent RED via batch; plan run in flight | go test ./internal/agent/ ./internal/plan/ -run 'TestPairFailureTerminal\|TestPlanRequestErrorTerminal\|TestSynthesisFailureEvent\|TestParkEmptyReply\|TestPanicRecovery' -count=1 | pending (/tmp/gate-red/t8plan.log) |
| T9 | internal/agent/review_evidence_gate_test.go | written; RED via agent batch (behavioral pin: zero-evidence auto-approve) | go test ./internal/agent/ -run TestFileStepZeroEvidenceGate -count=1 | build failed (same batch) |
| T11 | internal/agent/ralph_storm_test.go | RED (undefined RalphLoopConfig.MaxReplanExtensions) | go test ./internal/agent/ -run 'TestReplanHardCeiling\|TestFailedTaskNeverReplans\|TestFailureRelayedOnce' -count=1 | build failed (undefined MaxReplanExtensions) |
| T12 | internal/services/plan_typednil_test.go; internal/daemon/evolver_store_nil_test.go | runs in flight | go test ./internal/services/ ./internal/daemon/ -run 'TestPlansTypedNil\|TestNilSafeEvolverStore' -count=1 | pending (/tmp/gate-red/t12svc.log, t12daemon.log) |
| T10 | e2e suites + manifest | IN PROGRESS | go test -tags e2e ./e2e/suites/task-approval/... -count=1 | pending |

RED logs: /tmp/gate-red/*.log (last-20-line excerpts go into 00T-GATE-REPORT.md).