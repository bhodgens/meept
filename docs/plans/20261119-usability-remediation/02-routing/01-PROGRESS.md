# 01-veto-and-notice PROGRESS -- FINAL

Plan: 20261119-usability-remediation / 02-routing / leaf 01
Worktree: /Users/caimlas/git/meept/.hermes/worktrees/usability-remediation
Branch: plan/usability-remediation. NOT COMMITTED.

## Status: IMPLEMENTATION COMPLETE, GATES GREEN (verified in clean window)

- [x] T1 shape-independent git veto: dispatcher.go ~1774 conjunct
      hasLeadingImperativeVerb DROPPED. F41 recall-first order intact;
      genuine git verbs untouched; disabled escape hatch byte-identical.
- [x] T3 ClassificationNotice writers: Intent.Notice field; degradedNotice
      from llm.ClassificationUserGuidance(err) attached on
      llm_empty_fallback_chat / keyword / semantic / heuristic_fallback /
      fallback; copied to DispatchResult.ClassificationNotice in
      ClassifyAndRoute + ResumeAfterClarification. Handler reply-suffix
      readers were already live (write side was the gap).
- [x] Cooldown lane: llm_classifier.go lastFailure stored at arming;
      cooldown-skip error wraps cause with %w (guidance kind survives).
- [x] C1 routing_notice: TurnTerminalEvent.RoutingNotice wired at sync
      terminal funnel; schemas/ws_events.schema.json turn_terminal
      declares routing_notice.
- [x] T2 hint capability fit: strategic.go fitToolHint in parsePlanOutput;
      research-shaped step + hint agent lacking web_fetch => remap via
      config.ToolHintAgent (research->researcher) + routing.warning
      (reason=hint_capability_mismatch). Code-step guard untouched.
- [x] GATE VERIFY (clean window, overlay2.json masks only ralph_storm):
      go test -overlay /tmp/agent-gate-overlay/overlay2.json ./internal/agent/
        -run 'TestGitVetoShapeIndependent|TestNoticePopulated|TestHintCapabilityFit'
        -count=1
      => ok github.com/caimlas/meept/internal/agent 0.746s
      Orchestrator independently verified GREEN.
- [x] Stale non-gate pin resolved: dispatcher_git_veto_test.go
      TestGitVerbAgreementVeto_NonImperativeGitVerdictKept ->
      ...NonImperativeGitVerdictVetoed (asserts widened contract; comment
      documents supersession; config import added). Gate files
      dispatcher_veto_test.go + dispatcher_notice_test.go: git diff EMPTY.
- [x] gofmt applied to all touched files; go build ./... OK at gate time.
- [x] 01-REPORT.md written.

## Final-state caveat (orchestrator-owned)

Post-gate regression sweep could not complete: sibling leaves
(critical-delivery, 02-routing/03 continuation) left the shared tree
transiently compile-RED (internal/bus b.turnOutbox / publishErr;
internal/agent threadConvIDForResult) AFTER my green gate run. Per
orchestrator steer, whole-tree verification happens once siblings land.
My changes compile and pass gates in isolation (verified above); no
sibling code touched.
