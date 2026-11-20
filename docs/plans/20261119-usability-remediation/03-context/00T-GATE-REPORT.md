# 03-CONTEXT TEST-GATE - Completion Report

Branch: plan/usability-remediation (worktree
.hermes/worktrees/usability-remediation, base HEAD 76b1c0f7).
Leaf: 00T-gate.md tasks T1-T6. TESTS ONLY - no implementation code,
no existing test weakened, no commit, no git add.

## 1. Files touched

Created (tests only):
- internal/rpc/chat_submit_validate_test.go (T1: TestUnknownIDRejected,
  TestBothIDMismatch; C4 submit validation, F-ORCH-22).
- internal/agent/handler_history_test.go (T2: TestConvOnlyHistoryRoundtrip,
  TestConvOnlyHistoryFK787Reproduction; C4 conv-only durability, F-ORCH-29).
- internal/session/messages_toolcalls_test.go (T3: TestIncludeToolCalls;
  C4 include_tool_calls read surface + byte-identical legacy default).
- internal/agent/recall_state_gate_test.go (T4: TestRecallFailedState,
  TestRecallTaskSet; F-ORCH-16a/b, F-NEW-7).
- internal/agent/recall_fact_test.go (T5: TestRecallFactToMemory,
  TestStepPromptFidelity; F-ORCH-24, F-NEW-9, I5 scope control).
- e2e/suites/session-storage/conv_only_durability_test.go (T6:
  TestConvOnlyDialogDurability, TestSubmitValidationOverWire; F9).
- e2e/suites/naive-user-chat/recall_fidelity_test.go (T6:
  TestRecallFailedStateFidelity, TestRecallTaskSetCoverage,
  TestRecallFactToMemoryLive; F10).

Modified:
- e2e/manifest.json (+64 lines, 0 deletions): scenarios
  session-storage-03/04, naive-user-chat-06/07/08; path_map additions
  (internal/rpc/ + internal/agent/ -> session-storage;
  e2e/suites/naive-user-chat key). No existing row altered.

## 2. RED output per task (current HEAD, -count=1)

T1 - go test ./internal/rpc/ -run 'TestUnknownIDRejected|TestBothIDMismatch'
NOTE: the rpc package test build is currently blocked by the SIBLING
01-safety gate file task_approval_reject_test.go (references
TaskApprovalHandler.RejectFunc, not yet implemented by 01-safety/01).
RED proven via go -overlay substituting ONLY that sibling file with an
empty package-rpc file (sibling file untouched on disk; overlay artifacts
in /tmp/t1-gate-proof/). Once 01-safety/01 lands RejectFunc the plain
verify command runs unchanged.
    --- FAIL: TestUnknownIDRejected/unknown_session_id_(F-ORCH-22_F3)
        accepted = true, want false ... ack = map[accepted:true
        conversation_id:conv-3624317d63483d7d ...] (phantom conv fabricated)
    --- FAIL: TestUnknownIDRejected/unknown_conversation_id_(F-ORCH-22_F4)
        accepted = true (conv-doesnotexist999 accepted)
    --- FAIL: TestBothIDMismatch/session_A_paired_with_session_B's_conversation
        mismatched both-ids pair accepted as submitted (conversation_id =
        conv-gate-b); C4 requires correction to the owning conversation or
        rejection
    --- FAIL: TestBothIDMismatch/session_A_paired_with_an_unknown_conversation
    --- PASS: TestUnknownIDRejected/known_session_id_still_accepted (control)
    --- PASS: TestBothIDMismatch/consistent_pair_preserved (control)
    --- PASS: TestBothIDMismatch/legitimate_thread_id_accepted (control)

T2 - go test ./internal/agent/ -run TestConvOnlyHistoryRoundtrip
    --- FAIL: .../conv-only_exchange_persists_user_and_assistant_rows
        conv-only history lost: want user+assistant rows, got 0 (F-ORCH-29).
        Direct store write error on current code: failed to insert message:
        constraint failed: FOREIGN KEY constraint failed (787)
    --- FAIL: .../concurrent_conv-only_submits_are_race-safe
        want 12 rows, got 0
    --- PASS: .../control:_session-backed_conversation_roundtrips
Mutation discipline (00T Notes): TestConvOnlyHistoryFK787Reproduction
reproduces the ACTUAL error text on current code:
    RED (F-ORCH-29): conv-only exchange rejected by the sessions foreign
    key: failed to insert message: constraint failed: FOREIGN KEY
    constraint failed (787)
Post-fix the same test's success branch passes (write lands + readable).

T3 - go test ./internal/session/ -run TestIncludeToolCalls
    --- FAIL: .../include_tool_calls=true_joins_rows_by_message_id
        RED: include_tool_calls=true returned 0 tool calls on the assistant
        row, want 2 (messages.get omits tool calls today)
    --- PASS: .../default_false_is_byte-identical_legacy (legacy pin green
        today; absent param == explicit false, no tool_calls key anywhere)
Engine-side precondition green: GetToolCallsForMessages returns both rows
(the gap is the RPC surface, not storage).

T4 - go test ./internal/agent/ -run 'TestRecallFailedState|TestRecallTaskSet'
    --- FAIL: TestRecallFailedState/failed (also cancelled, rejected)
        RED (F-ORCH-16a): terminal failed task misreported as in-progress:
        "Prior task: \"change hello.txt\" - status: failed ... The work
        above was still in progress; its stored result may not reflect the
        final state."
    --- FAIL: TestRecallTaskSet
        RED (F-ORCH-16b/F-NEW-7): plural artifact answer omits the session's
        hello.txt artifact (last-task-only digest): "Prior task: \"what is
        the capital of France\" - status: completed ... Paris."
    --- PASS: TestRecallFailedState/completed_unchanged (control)
    --- PASS: TestRecallFailedState/non-terminal_keeps_in-progress (control)

T5 - go test ./internal/agent/ -run 'TestRecallFactToMemory|TestStepPromptFidelity'
    --- FAIL: TestRecallFactToMemory
        RED (F-ORCH-24): fact question created task
        "task-20261120052136...-0001" (What was the codeword I told you
        earlier in this session?) - it must route to the memory_search
        lane, never task_get decomposition
        (controls green: scoped memory search sees ZEBRA-4417 for session
        A; I5 control green: foreign/unbound session B does NOT recall A's
        project-scoped fact)
    --- FAIL: TestStepPromptFidelity
        RED (F-NEW-9): orchestrator step lost the explicit file path
        "/tmp/usability-rig-p3/definitely-missing-9f3k2.txt"; the executor
        would answer with a clarification question for info already in the
        prompt. (Real quick_plan path; planner prompt carried the path -
        control green - the persisted step dropped it. Loss seam logged:
        extractSummary truncates the 198-char prompt before the path.)

T6 - e2e (harness stack, fake LLM):
go test -tags e2e ./e2e/suites/session-storage/ -run
'TestConvOnlyDialogDurability|TestSubmitValidationOverWire'
    --- FAIL: TestConvOnlyDialogDurability
        harness: timed out after 15s waiting for conv-only history rows
        (turn completed; exchange lost to FK 787 - F-ORCH-29)
    --- FAIL: TestSubmitValidationOverWire/unknown_session_id_rejected
        RED (F-ORCH-22): unknown session_id accepted, phantom conv
        fabricated: map[accepted:true conversation_id:conv-6d2deaa5...]
    --- FAIL: .../unknown_conversation_id_rejected
    --- FAIL: .../mismatched_both-ids_corrected_or_rejected
        RED (C4): mismatched both-ids pair accepted as submitted
    --- PASS: .../known_session_accepted, .../consistent_pair_accepted
go test -tags e2e ./e2e/suites/naive-user-chat/ -run
'TestRecallFailedStateFidelity|TestRecallTaskSetCoverage|TestRecallFactToMemoryLive'
    --- FAIL: TestRecallFailedStateFidelity
        RED (F-ORCH-16a): failed task misreported as still in progress
        (live doomed file_write task finalizes failed; recall reply carries
        "The work above was still in progress")
    --- FAIL: TestRecallFactToMemoryLive
        RED (F-ORCH-24): fact answer does not carry the planted codeword
        ZEBRA-4417: "## task completed: what was the codeword ... ### steps:
        1. [+] do the scripted step ... completed in 1s" (quickplan task
        machinery instead of the memory lane)
    --- PASS: TestRecallTaskSetCoverage (precondition pin, see Deviations)
Manifest completeness: go test -tags e2e ./e2e/suites/manifest/ -> ok
(all 5 new scenario rows registered, path-covered, suites reachable).

## 3. Deviations

- T4 file name: the leaf names internal/agent/recall_answer_test.go,
  which ALREADY EXISTS (option-3 pins). Gate tests live in the sibling
  recall_state_gate_test.go so no existing test is touched; the leaf's
  -run regex matches by test name and is unaffected.
- T1 verify command: blocked at HEAD by the sibling 01-safety gate's
  compile error (RejectFunc undefined). RED recorded via go -overlay
  (sibling file untouched). Runs plain once 01-safety/01 lands.
- T6 naive-user-chat runs used -vet=off: the sibling 04-clutter gate file
  clutter_gate_test.go has a pre-existing vet error (non-constant Fatalf
  format string) that fails the package vet step. Not touched here.
- T6 TestRecallTaskSetCoverage is a live PRECONDITION pin (asserts the
  durable session task set holds hello.txt + the unrelated newer task and
  records the observed plural reply). A deterministic RED assertion on the
  answer text is not achievable with the fake LLM (the loop's conversation
  history echoes T1's "Created hello.txt", letting a fake reply pass for
  the wrong reason); the deterministic RED gate for task-set coverage is
  the unit TestRecallTaskSet (isolated dispatcher, no history echo).
- T2 control keys the exchange on the session PRIMARY id (production
  persistID shape, handler.go:1290); the FK references sessions(id), so a
  conversation-id key would fail even for real sessions. Gate arms use
  conv-only ids exactly as the bench/GUI submits do.
- No `_ = ` added lines; ASCII-only in all new files; gofmt clean;
  Go 1.27.3. Only tracked-file change is e2e/manifest.json (additive).

## 4. Branch matrix coverage

- F9 (session validation + history): T1, T2, T3 unit + session-storage-03/04
  e2e. RED reasons: unknown ids accepted / FK 787 / tool calls omitted.
- F10 (recall digest): T4, T5 unit + naive-user-chat-06/07/08 e2e. RED
  reasons: failed->in-progress, last-task-only, task_get decomposition,
  step path loss.

DO NOT COMMIT honored - orchestrator handles git after review.