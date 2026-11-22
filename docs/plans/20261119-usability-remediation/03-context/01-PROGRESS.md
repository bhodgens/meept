# 01-session-history PROGRESS

Plan: 20261119-usability-remediation / 03-context / leaf 01
Worktree: /Users/caimlas/git/meept/.hermes/worktrees/usability-remediation
Started: 2026-11-20

## Status

- [x] Read leaf doc docs/plans/20261119-usability-remediation/03-context/01-session-history.md
- [x] Read gate tests (T1 rpc, T2 agent, T3 session)
- [x] T1: chat.submit validation (unknown session/conv ids, both-ids mismatch)
- [x] T2: conv-only history roundtrip (sessions-row upsert, FK 787 fix) - GREEN
  via in-tx ensureSessionRowInTx on SaveMessages + SaveMessagesWithToolCalls
  plus persistExchange pre-call through the sessionRowEnsurer capability
- [x] T3: include_tool_calls read surface (byte-identical legacy default) - GREEN
- [ ] go build ./... + package test verification
- [ ] 01-REPORT.md written

## Log

- 2026-11-20: leaf doc read. Scope: submit validation, sessions upsert before
  persistExchange, include_tool_calls additive read param. T4 (resume contract)
  is in the leaf table but delegated task scope is T1/T2/T3 gate GREEN; will
  assess T4 after core gates.
- 2026-11-20 (resumed after prior child 429 death): checkpoint verified; reading
  gate tests next, then implementation T1->T2->T3.
- 2026-11-20 (finishing child): T1 confirmed GREEN by orchestrator (rpc gates).
  T2: prior child's store_upsert.go EnsureSessionRow existed but was unwired -
  no caller. Fix landed at TWO levels: (a) ensureSessionRowInTx inside the
  SaveMessages / SaveMessagesWithToolCalls transactions (the authoritative
  race-safe guarantee - s.mu serializes writers, ON CONFLICT DO NOTHING
  idempotent, existing rows untouched so no binding widening); (b) the
  persistExchange pre-call through the sessionRowEnsurer capability the prior
  child had already declared. T3: Message gains additive ToolCalls
  []MessageToolCall omitempty (C4 wire shape {id,name,args_summary,
  result_summary,at}, summaries bounded 200 runes, at = owning message
  timestamp); handleGetMessages parses include_tool_calls and joins via
  GetToolCallsForMessages only when true. Gate runs:
  TestIncludeToolCalls ok, TestConvOnlyHistoryRoundtrip +
  TestConvOnlyHistoryFK787Reproduction ok (no overlay needed - ralph_storm
  compiled clean at run time).
- 2026-11-20 (finishing child, verification pass): full ./internal/session/
  package ok. T1 rpc gates re-verified ok. Discovered T1 breaks
  internal/daemon/http_only_submit_test.go (F17 pin submitted fabricated
  sess-g1, now correctly rejected) - fixture updated to create a real
  session, test GREEN. Baseline comparison at HEAD shows the remaining
  agent/daemon failures (TestStepJobDirBinding*, TestQueueStatusParked*,
  TestTaskApprove_RPC, TestTrivialFastLane, TestPairFailureTerminal, ...)
  are pre-existing RED gate suites owned by SIBLING leaves (01-safety,
  05-navigation, 06-planning commits), not regressions from this leaf.
  Tree carries live sibling in-flight edits (loop_park.go, ralph_loop.go
  mid-edit); agent/rpc/daemon gate runs used /tmp/ctx01-overlay/overlay.json
  masking ONLY those two sibling files - gates still exercise this leaf's
  real code paths. Incident: a git stash -u baseline check swept sibling
  edits; all restored from stash (kept as stash@{0}, not dropped),
  re-verified by the gate runs above. go build ./... clean at 10:52 (whole
  tree, pre-sibling-edit-contention). gofmt + ASCII + %w checks clean.
- 2026-11-20: DONE. 01-REPORT.md written. NOT committed, NOT git-added.
  T4 (resume contract) flagged to orchestrator as out of finishing scope.