# 04-CLUTTER Test Gate - Completion Report (00T)

> TESTS ONLY. No implementation code. Every gate fails RED for a
> missing-implementation reason. NOT committed, NOT git-added. Worktree:
> .hermes/worktrees/usability-remediation (HEAD 76b1c0f7).

## 1. Files touched

Created (9 test files):

| Task | File | Package |
|------|------|---------|
| T1 | internal/agent/handler_empty_reply_test.go | agent |
| T2 | internal/agent/relay_strip_test.go | agent |
| T3 | internal/agent/review_chat_test.go | agent |
| T4 | internal/agent/orchestrator_sidejob_test.go | agent |
| T4 (firewall) | internal/llm/context_firewall_disclosure_test.go | llm |
| T5 | internal/agent/dispatcher_trivial_test.go | agent |
| T6 | internal/tui/models/chat_dedupe_test.go | models |
| T7 | ui/flutter_ui/test/providers/chat_dedupe_test.dart | Dart |
| T8 | e2e/suites/output-filters/reply_hygiene_test.go | outputfilters (e2e) |
| T8 | e2e/suites/naive-user-chat/clutter_gate_test.go | naiveuserchat (e2e) |

Modified (1):

- e2e/manifest.json - added 5 scenario rows (output-filters-06/07/08,
  naive-user-chat-09/10). Manifest completeness suite passes
  (`go test -tags e2e ./e2e/suites/manifest/` -> ok). NOTE: a sibling
  session concurrently added session-storage rows to the same file; the
  read-modify-write preserved them (4 session-storage scenarios intact,
  429 total scenarios, all ids unique).

No existing test was weakened; no implementation file was touched.

## 2. RED output per task

### T1 - TestEmptyReplySynthesis (F-ORCH-15, frozen p1 turn 1)
`go test ./internal/agent/ -run TestEmptyReplySynthesis -count=1`
```
--- FAIL: TestEmptyReplySynthesis/tool_only_completed_turn_names_written_path
    F-ORCH-15: completed turn shipped an empty reply (frozen p1 shape: reply="");
    the tool ledger recorded a write of .../hello.txt - synthesize from it
--- FAIL: TestEmptyReplySynthesis/reply_source_marks_synthesized
    C1 reply_source missing: TurnTerminalEvent has no string ReplySource field
--- FAIL: TestEmptyReplySynthesis/no_ledger_evidence_never_fabricates_effects
    completed turn shipped an empty reply even with no ledger evidence
--- PASS: TestEmptyReplySynthesis/prose_reply_passes_through_unchanged  (regression)
```
RED reason: route_to_agent completion ships the loop's empty finalResponse
verbatim; C1 reply_source field absent. Drives the REAL route_to_agent path
(scripted tooluse classifier + file_write tool that writes the ledger path +
blank-stop completion). GREEN after 04-clutter/01.

### T2 - TestRelayEnvelopeStrip (F-ORCH-21, frozen p6 envelope)
`go test ./internal/agent/ -run TestRelayEnvelopeStrip -count=1`
```
--- FAIL: best_summary_recovers_prose_from_full_envelope
    raw envelope key "status": / "accomplished": / ... leaked (F-ORCH-21)
--- FAIL: completion_card_recovers_prose_from_truncated_job_evidence
--- FAIL: task_completed_relay_terminal_and_chat_are_clean
    chat.response relay + turn.terminal relay carry raw envelope keys
--- FAIL: stripper_directly_recovers_full_envelope
--- PASS: legacy_claims_evidence_envelope_still_stripped  (regression)
--- PASS: plain_prose_passes_byte_identical  (regression)
```
RED reason: StripClaimsEvidenceOrProse misses the
{status,accomplished,not_done,issues,observations} shape. Fixtures VERBATIM
from p6-positive-control.json events[52].payload.result.response /
evidence[0] and turns[0].terminal.payload.reply. GREEN after 04-clutter/01.

### T3 - TestReviewFeedbackNotChat (F-ORCH-25, frozen p2 frames)
`go test ./internal/agent/ -run TestReviewFeedbackNotChat -count=1`
```
--- FAIL: approved_never_pushed_to_chat
    F-ORCH-25: review verdict "approved" pushed to chat.response as "## review: approved..."
--- FAIL: rejected_never_pushed_to_chat
--- FAIL: needs_info_never_pushed_to_chat
--- FAIL: linked_sessions_receive_no_review_chat
--- PASS: review_event_still_flows_for_task_detail  (regression)
--- PASS: frozen_p2_replies_are_review_chatter  (fixture self-check)
```
RED reason: handleReviewCompleted sendResponse pushes "## review:" to
chat.response for every verdict. Fixtures VERBATIM from p2-isolation.json
events[111]/[56].payload.reply. GREEN after 04-clutter/01.

### T4 - TestSideJobCompletionQuiet (F-ORCH-6) + TestFirewallTruncationDisclosed (F-NEW-8)
`go test ./internal/agent/ -run TestSideJobCompletionQuiet -count=1`
```
--- FAIL: memory_capture_completion_logs_no_error
    F-ORCH-6: side-job completion logged at ERROR: "Failed to handle job completion"
    error=failed to find step for job ...: step not found
--- FAIL: repeated_side_jobs_stay_quiet  (4 ERROR records across 4 side jobs)
--- PASS: side_job_completion_emits_no_chat_event  (regression)
--- PASS: real_step_failure_stays_loud  (regression)
```
`go test ./internal/llm/ -run TestFirewallTruncationDisclosed -count=1`
```
--- FAIL: enabled_firewall_logs_one_warn_per_truncation
    F-NEW-8: one oversized system message produced 2 truncation WARNs, want exactly 1
    [window-independent bound + to fit model window]
--- FAIL: truncation_is_disclosed_never_silent
    F-NEW-8: no truncation disclosure surface on ContextFirewall
--- PASS: inert_path_logs_one_warn_per_truncation  (regression)
--- PASS: small_system_messages_stay_byte_identical  (regression)
```
RED reason: orchestrator logs unmapped side-job completion at ERROR;
firewall double-logs truncation and exposes no disclosure surface.
GREEN after 04-clutter/01.

### T5 - TestTrivialFastLane (F-ORCH-5, frozen 2+2 fixture)
`go test ./internal/agent/ -run TestTrivialFastLane -count=1`
```
--- FAIL: config_surface_exists_default_on
    config.OrchestratorConfig has no TrivialFastLane bool field
--- FAIL: trivial_question_routes_direct_chat_no_task
    F-ORCH-5: "What is 2+2? Answer briefly." classified "quickplan", want direct chat lane
--- FAIL: flag_off_restores_legacy_classification
    no trivial-fast-lane config seam on Dispatcher
--- FAIL: fast_lane_keeps_session_binding_and_sanitization
--- PASS: artifact_verbs_still_task_route  (regression)
```
RED reason: trivial question over-classifies to quickplan and creates a
task; orchestrator.trivial_fast_lane config + dispatcher seam absent.
Fixture VERBATIM from tui-2plus2.json. GREEN after 04-clutter/02.

### T6 - TestTUITaskCardDedupe (notes-server 2.4 / F-ORCH-5 double-render)
`go test ./internal/tui/models/ -run TestTUITaskCardDedupe -count=1`
```
--- FAIL: terminal_then_card_renders_once
    double render: the logical reply rendered 2 times (terminal bubble + task card)
--- FAIL: card_then_terminal_renders_once
--- FAIL: relay_json_card_after_terminal_renders_once
--- FAIL: dedupe_key_carries_task_id_when_present
    turnTerminalMsg has no string TaskID field (05-navigation/01 adds it via C1)
--- PASS: late_terminal_after_stalled_still_renders  (REGRESSION PIN)
--- PASS: distinct_turns_both_render  (regression)
```
RED reason: SuppressTaskCompletedFor is pending-only/conversation-scoped;
once the terminal lands the card renders again. Dedupe contract pinned:
key (turn_id, task_id), whichever-first-wins, late-terminal-after-stalled
never suppressed. GREEN after 04-clutter/02 T2.

### T7 - GUI dedupe robustness (Dart, notes-server 2.4 GUI half)
`cd ui/flutter_ui && flutter test test/providers/chat_dedupe_test.dart`
```
+4 -3: Some tests failed.
Failing:
  chat_message then terminal with interleaved assistant message renders once  (Actual: 2)
  terminal then chat_message redelivery renders once  (Actual: 2)
  chat_message carries turn identity for the dedupe key  (Actual: false)
Passing (regression):
  interleaved system note does not defeat dedupe
  distinct turns with identical content both render
  late terminal after parked notice still renders
  plain terminal without prior chat_message renders once
```
RED reason: _lastAssistantContentEquals is last-assistant-bubble-only; an
interleaved assistant message defeats it (double render); ChatMessage
carries no turn/task identity for the C1 key. GREEN after 04-clutter/02 T3.

### T8 - e2e extensions (manifest rows added same change)
`go test -tags e2e ./e2e/suites/output-filters/ -run TestOutputFilters06... -count=1`
```
--- FAIL: TestOutputFilters06RelayEnvelopeStripped
    F-ORCH-21 relay reply carries raw envelope key "status": (full p6 envelope rendered)
--- FAIL: TestOutputFilters07ReviewFeedbackNotChat
    F-ORCH-25 review feedback pushed to chat.response: "## review: approved..."
--- FAIL: TestOutputFilters08SideJobCompletionQuiet
    F-ORCH-6 side-job completion logged at ERROR: ... failed to find step for job ...
```
`go test -tags e2e ./e2e/suites/naive-user-chat/ -run TestNaiveUserChat09... -count=1`
```
--- FAIL: TestNaiveUserChat09ToolOnlyReplyNonEmpty
    F-ORCH-15 completed turn shipped an empty reply; ledger recorded write of .../hello.txt
--- FAIL: TestNaiveUserChat10TrivialFastLane
    F-ORCH-5 trivial question dispatched task ...; left 1 task row; reply is a
    "## task completed: what is 2+2? ... job job-... completed by agent orchestrator" dump
```
RED reason: scenarios absent until this change; each asserts the gated
defect end-to-end against the fake-LLM harness. GREEN after branch.

## 3. Deviations

1. **T6 verify path.** The leaf's verify command is
   `go test ./internal/tui/ -run TestTUITaskCardDedupe`, but the created
   file `internal/tui/models/chat_dedupe_test.go` is `package models` (it
   drives unexported ChatModel/turnTerminalMsg internals, matching the
   sibling chat_async_test.go). The correct command is
   `go test ./internal/tui/models/ -run TestTUITaskCardDedupe`. The leaf's
   `./internal/tui/` path reports "no tests to run". File location follows
   the leaf's Files column; only the verify path is corrected here.

2. **T4 firewall disclosure surface.** The leaf names C1 routing_notice or
   a brief system note as the disclosure channel but does not fix the
   ContextFirewall API. The gate asserts the STRICTEST reading via
   reflection: a Stats() truncation counter OR a notice accessor must exist
   AND report the truncation. It compiles before 01 lands the seam and
   flips GREEN when either surface appears. 01 owns the exact shape.

3. **T5/T6 config/field seams via reflection.** TrivialFastLane (config),
   the dispatcher fast-lane seam, reply_source (C1), and
   turnTerminalMsg.TaskID do not exist yet. The gates assert them through
   reflection so the package compiles today and the assertion (not a
   compile error) is the RED. This follows the spec-first-test-leaf
   runnability floor. No t.Skip used.

4. **T7 reverse-order redelivery.** The terminal-then-chat_message case
   feeds turn_id/task_id on the chat_message frame defensively (the wire
   does not carry them today; 05-navigation/01 adds them, 02 reads them).
   This pins the C1 contract without requiring a server change owned by
   another leaf. The primary interleaved-assistant case needs no such
   assumption and is RED on the current last-bubble dedupe alone.

5. **`_, _ = w.Write` in two httptest classifier servers.** Matches the
   established sibling convention (classifier_llm_gate_test.go:43,
   dispatcher_task_gate_test.go:92) under errcheck. These are
   `_, _ =` (two blanks), not `_ = ` single-assignment lines.

6. **Concurrent manifest edit.** A sibling session added session-storage
   scenario rows to e2e/manifest.json while this gate was being written.
   The read-modify-write preserved them; completeness suite passes with
   both sets present.

## 4. Verification summary

- All 9 test files gofmt-clean; Dart file `dart format` clean (0 changed).
- No implementation code; no existing test weakened.
- Regression suites green: internal/agent (strip/terminal/persist/chunking),
  internal/llm (TestContextFirewall*), internal/tui/models (TestChatModel_*),
  GUI chat_submit_flow/chat_retry/chat_pagination (13 pass).
- Manifest completeness suite passes; F11a/F11b matrix covered
  (F11a: T1-T4 + output-filters-06/07/08 + naive-user-chat-09;
   F11b: T5-T7 + naive-user-chat-10).
- Dedupe pins cover late-terminal-after-stalled (T6) and
  late-terminal-after-parked (T7) - both PASS today, must survive the fix.
- Fixtures VERBATIM from rig probes (p1 turn 1, p6 events[52]/turn 1,
  p2 events[111]/[56], tui-2plus2) - no rewording.

**DO NOT COMMIT.** Orchestrator handles git after review.