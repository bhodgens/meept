# Meept weekly bughunt 2026-09-17 — verified findings (provisional)

Scope: 240 commits, September 10-17, cf527d44..e3aa8ebc. HEAD at audit start e3aa8ebc; two sibling commits landed mid-audit (02980908 mcp reply, 4f4c35e3 memory-eval judge lane) and remain unaudited here. Seven read-only auditor reports: /tmp/meept-week-{A..G}.md. Full short Go baseline: 107 packages pass, 0 fail, 0 cached (/tmp/meept-week-20260917-go-tests.log). Flutter: 541 pass (/tmp/meept-week-flutter-tests.log). Python selftests: classifier, e2e-sweep 48, chat harness 4, sidecar 24 all PASS (/tmp/meept-week-python-tests.log). Report-only: no repository fix applied; handler.go verified clean at close. Findings verified in current source by the parent or through /tmp overlay probes; probes are outside the repository.

## Reproduced this session (parent probes in /tmp, current source)

1. HIGH - Task-to-turn correlation missing on both dispatch paths. internal/agent/handler.go:981,1030 call attachTask(result.Task.ID, syncTaskID); signature is attachTask(turnID, taskID string) (internal/agent/handler.go:2144-2149). Async passes the task ID as turn key; sync passes an empty turn ID and empty task. Probe TestWeekParentDispatchAttachment failed for both async and sync before the fix; with the corrected call h.attachTask(req.TurnID, result.Task.ID) placed before the sync/async branch both subtests pass. Without the fix the completion relay mints a fresh turn ID, and turn-scoped clients never receive the task result; reaped events also lose the task_id. Introduced 8b2559ce. Suggested fix: single attachTask(req.TurnID, result.Task.ID) before publishPlanRequest in both paths. Existing tests pass because they call attachTask directly with correct argument order.
2. HIGH - Failed tasks relay as completed. handleTaskCompleted (internal/agent/handler.go:1637-1706) has no status field and always relays status "completed" (1705-1706), while tactical.go:1585-1640 publishes task.completed with status failed for failed steps or validation exhaustion. Probe TestWeekParentFailedCompletionStatus: status "completed" on a failed payload. Clients grade failed work as success. Introduced 5d39f67e; status made honest in dbad424c but the consumer ignores it.
3. HIGH - Optional missing veto artifact panics daemon startup. loadTfidfVeto returns (nil, nil) on a missing file (internal/agent/tfidf_veto.go:52-53); NewEmbeddingPrefilter dereferences it at embedding_prefilter.go:160-163 when cfg.VetoPath is set but the artifact is absent. Probe TestWeekParentMissingVeto: nil pointer dereference. Introduced 511e47fb. Fix shape: guard veto nil before the log line.
4. HIGH - Refusal fallback pin survives the turn. handleRefusal arms SetPersistentModelOverride (internal/agent/loop_refusal.go:118-129); the fresh-turn sweep only clears hook-owned overrides (loop.go:2518-2530), and ClearRefusalFallback is only called on the give-up branch (loop_refusal.go:110). Probe TestWeekAuditRefusalFreshTurn (delegated overlay): override remains local/fallback and the next turn serves the fallback model without disclosure. Introduced 1771980f.
5. HIGH - Global refusal_model lost on session clones and specialists. ConfigSnapshot does not copy globalRefusalModel (internal/agent/loop.go:7592-7601) and registry construction has no global refusal wiring (internal/agent/registry.go has no symbol). Probe TestWeekAuditGlobalRefusalSnapshot: cloned refusalFallbackRef is empty. Introduced 1771980f. Fix shape: extend ConfigSnapshot and registry construction options.
6. MEDIUM - TUI parked event permanently latches the awaiter; later completion is discarded. internal/tui/models/turn.go:134-148 notify() latches done on any status; DeliverTurnTerminal (chat_turn.go:192-208) notifies for parked, and chat_turn.go:75-84 re-arms awaitTurnCmd after parked. Probe TestWeekParentParkedThenCompleted: last result remains parked, completed reply discarded. Introduced d008c5b7b. Severity MEDIUM here because only probe-tested; delegated review also reports a related hot re-arm loop and session-switch misdelivery, unverified by probe.
7. HIGH (eval integrity) - Empty parsed lesson disappears from recall. tools/memory-eval/grade.go:599-601 skips empty principles without incrementing FalseNegatives. Probe (offline, /tmp/meept-week-memory-probe.log): calls=2 tp=1 fn=0 recall=1.00 where 0.50 is expected. A model that drops hard lessons can look perfect. Introduced 280491f6d. Note: sibling WIP 4f4c35e3 modifies the same file; coordinate.
8. MEDIUM (eval integrity) - Scenario grades honest empty-input rejection as FAIL. tools/e2e-sweep/scenarios.py:40-45 wraps missing-required-input with require_tool(count=1) while production json_extract.go:154-155 correctly rejects empty text (json_extract.go confirmed). Probe /tmp/meept-week-scenario-probe.log: grade FAIL on the honest rejection. Introduced 0688a9b0. Correct behavior cannot pass.

## Source-verified, not independently reproduced (delegated reports)

9. HIGH - Review policy bypass for tool-execution claims. review_manager.go:126-132 approves before the heuristic guard (1050-1065) under the default SkipReview policy (review.go:66; daemon wiring components.go:2840-2846). Introduced 3fc9d50a (incomplete fix).
10. HIGH - Validation exhaustion strands dependent steps. tactical.go:1251-1261 fails the step; finalization requires all terminal (1472-1482), and dependents stay pending. Introduced dbad424c (incomplete fix).
11. HIGH - spec_pair setup failure leaves task planning, error only logged (strategic.go:437-441; orchestrator.go:369-375). Introduced b0816ffa (incomplete fix).
12. HIGH - HTTP-200 refusals discard reported usage: parse returns (nil, refusal) before usage recording (client.go:1858-1859 and anthropic.go:1369-1370); budget and metrics undercount. Introduced 99c656ef. tests/refusal_fallback_test.go:465-469 asserts zero primary rows, enshrining the gap.
13. HIGH - ProviderManager treats refusals as provider failures and rotates (provider_manager.go:421-428 default branch; recordFailure 994-1027), contradicting the AGENTS.md one-hop contract. Introduced 99c656ef.
14. MEDIUM - Anthropic streaming refusal reports the configured default model, not the request-selected one (anthropic.go:1697 uses c.config vs effective cfg at 546-549). Introduced 99c656ef.
15. HIGH (parked-turn identity) - Quota/budget park records lack TurnID; resume paths call sendResponse only, never publishTurnTerminal; the funnel then deletes the submitted turn (handler.go:1110-1147,1262-1263,2236-2301,2513-2555). Awaiters never resolve. Introduced 5d39f67e + 4eb9b4f3.
16. HIGH - Same-turn-id retry re-executes after Complete() deletes the idempotency record (turn_registry.go:91-97; chat_submit.go:126-143). Introduced 4eb9b4f3.
17. MEDIUM - /api/v1/chat/submit returns 503 when Unix RPC is disabled because the submitter is constructed only inside the rpcServer gate (daemon.go:286-290,1324-1327). Introduced 4eb9b4f3.
18. MEDIUM - Per-request model selection dropped on async task dispatch: publishPlanRequest omits RequestModel (handler.go:1357-1369) and the prefilter-direct result omits it (dispatcher.go:1066-1075). Introduced 21198aa6.
19. HIGH - GUI and TUI drop terminal events arriving before the ack registers the pending turn (chat_provider.dart:441-445; chat_turn.go:192-198). Introduced c7b051c0f / d008c5b7b.
20. HIGH - Flutter send/steer path rebuilds ChatState without pendingTurns (chat_provider.dart:828-834,912-917), clearing earlier pending tracking. Introduced c7b051c0f.
21. HIGH - TUI completion delivered while a non-chat view is active is discarded or rendered into a newly selected session (app.go:2172-2195; chat_turn.go:87-111; chat.go:2568-2592,1791-1800). Introduced d008c5b7b.
22. HIGH - Flutter Web reconnect exits its retry loop: _diagnoseConnectFailure constructs io.HttpClient outside any kIsWeb guard before its try (websocket_service.dart:238,773-778). Introduced b9ec31b3.
23. HIGH - Cancelled supervised Stop SIGKILLs the wrapper's process group, deletes PID file and spawn record while the runtime survives (runtime_process.go:598-605; supervisor.go:329-347). Introduced f6b5886c.
24. MEDIUM - supervise default defeats auto_stop_on_exit:false: normal daemon exit kills preserved runtimes (runtime_process.go:320-341). Introduced f6b588c.
25. MEDIUM - Start-lock window: empty lock file treated as stale before the owner PID is written (runtime_startlock.go:59-74,131-139), enabling duplicate spawns; release can unlink another holder's lock (144-152). Introduced 2d5bed20.
26. MEDIUM - Doctor reaper deletes a replacement runtime's handles using a stale snapshot (runtime_sweep.go:408-438). Introduced dbc6dce7.
27. MEDIUM - Primary launchd install drops MEEPT_HOME while the controller resolves the overridden home (launchd.go:282-291 vs 316-332). Introduced e98ebefc.
28. MAJOR (labeled) - Procedure distillation forced through lesson grammar; every constrained procedure response fails validation and blocks the queue (distill.go:630-636 vs gbnf.go:454-469 and normalizeDistillPayload 382-393). Introduced c679e1e3.
29. MAJOR (labelled) - Burst detector never observes failed_replan: ResolvePendingOutcome returns only ok/corrected (metrics/store.go:972-1008) while hard failures are marked directly (tactical.go, strategic.go via MarkTaskFailedReplan); default threshold 0.50 unreachable (burst_detector.go:124-128,185-193). Introduced 1da5ca95 / a93e8d39.
30. MEDIUM - memory-eval latency measures headers only (client.go:94-101); failed calls report 0 ms. Probe reproduced 150 ms body vs 0 reported. Introduced 280491f6.
31. MEDIUM - make install ordering not enforced for parallel make (Makefile:347-356; build-gui:982-985; ensure-dev-key.sh:31-39): GUI can embed a dev key that setup then replaces. Introduced b9ec31b3.

## Related but pre-existing (not introduced this week; fix recommended)

- Numeric evidence-ID tolerance added at the wrong boundary: DecodeLesson tolerant, normalizeDistillPayload strict (b4ea06d7 incomplete; strict boundary predates range).
- MCP meept_send cannot deliver async task results and returns the ack (parent trace; server.go:263; tools.go schema; rpc/proxy.go:69). Partly mitigated by 02980908 reply extraction; no turn_id or terminal wait exists.
- TUI stalled liveness timer ignores lastActivity (progress cannot postpone it).
- Flutter noteTurnProgress has no production caller.
- sync-mode watchdog vs 110s sync wait interaction documented in the prior 2026-09-17 report.

## Known routing-repair findings reconciled

- Forced OOD abstention already repaired in 0688a9b0 (routing-repair master stale on this point).
- Undefined classifier ratios, stale chain credit/cache identity, privacy/provenance items remain in the existing routing-repair scope; not duplicated.
- F6 parked-status overloading and sync-flag interactions remain open-ledger from the prior report.

## Baselines and probes

- go test -p 2 -short -count=1 ./...: 107 pass, 0 fail, 27 no-test packages, 0 cached. Short mode; no race detector.
- flutter test --no-pub: 541 pass (VM only; no browser).
- Python: classifier selftest, e2e-sweep 48, chat harness 4, sidecar 24, hardening PASSED.
- Overlay probes (repository untouched): dispatch attachment async+sync PASS with fix, FAIL without; failed-status relay FAIL; missing-veto panic FAIL-as-expected; refusal fresh-turn and snapshot FAIL-as-expected; TUI parked latch FAIL-as-expected; memory-eval recall and latency probes FAIL-as-expected; scenario grading FAIL-as-expected.
- Full -race suite, live model evaluation, browser/native UI runs, and installs were not performed.

## Verification of delegated claims (parent)

Re-read decisive branches for A1/A3/A5, B1-B5, C1-C5 (control flow), D1-D5 (key sites), E1-E5 (call chains), F1-F4, G1-G2. Reproduced 1-8 with probes. A4/A15-A31 accepted on delegated source verification without independent probes. No delegated claim was refuted; severity labels MAJOR normalized to HIGH.

## Coverage ledger (not audited or partial)

- Commits landed mid-audit: 02980908, 4f4c35e3 (plus their follow-ons) are outside the seven reports' scope.
- Queued-turn/reaper and watchdog interleavings traced but not promoted without deterministic tests (report A).
- MCP external server output shapes; live grammar behavior; installs; race stress; browser/native UI; docs generation; CI runs.
- tools/e2e-sweep runner internals beyond grading path; memory-eval main.go mode flags (silent unknown-mode skip noted).
- .githooks pre-commit-build path-comparison weakness pre-dates range (blamed 045a53362) - observed, excluded as not-new.
- Existing tests may have written plan artifacts inside docs/plans trees during test runs; not cleaned.

## Suggested repair order (pending your go-ahead; nothing fixed)

Group 1 (turn identity, highest user impact): 1, 2, 15, 16, 17, 18, 19, 20, 21.
Group 2 (model behavior): 4, 5, 12, 13, 14, 28.
Group 3 (stranding and guards): 3, 9, 10, 11.
Group 4 (runtime lifecycle): 23, 24, 25, 26, 27.
Group 5 (evaluation honesty): 7, 8, 30, 31, 29.
Group 6 (UI polish): 6, 22.


## Fix wave (2026-09-17, "fix all groups" authorized) — ALL 31 findings closed

22 concern-split commits landed on classifier-iteration. Disposition by finding:
F1/F2/F11/F15/F16/F17/F18 → 16882a97, b6208ed2, 5aaba3bf, 41eb0b77, 7f0deb9c
F3/F9/F10/F29 → 921bee47, ce8360d1, 760b3d8b, 8e84113d
F4/F5/F12/F13/F14/F28 → 927b226e, fbedd4cb, 391c869d, 0af403de, 04f27424 (+ AGENTS.md docs 5a31edee)
F23/F24/F25/F26/F27 → 4136c0bc, 827a68d9, 4ff88b70, 198a404d, 74c8ade8
F7/F30/F8/F31 → bf351889, ac16d411, 317e9570, 61f57c7a
F6/F19/F20/F21/F22 → 24f895fc, b4978586, 5ae1c06d

Parent verification at final HEAD: every fix marker grep-verified at its site (F1 old
swapped calls = 0 remaining); all wave pins pass in one consolidated run across
agent/llm/memory/rpc/daemon/tui/tests; go build ./... clean; go vet on wave packages
clean; mutexio clean; python selftests green at final HEAD. Known pre-existing gofmt
drift in 5 test files and tools/memory-eval/{corpus,grade}.go noted; grade.go drift
arrived with 4f4c35e3 (sibling), not this wave.


## Follow-on: tool-boundary-hardening tree (2026-09-19)

The 2026-09-18 e2e churn investigation produced a 4-leaf plan tree
(docs/plans/20260918-tool-boundary-hardening/) executed via hierarchical
plan execution: all 4 leaves COMPLETE first-pass, 7 commits
(d35044a0, 92964672, 1a1a9b8a, 42ae8e06, 9493329e, 7ae53e21, 7bf6d5cb),
42 wave pins green at the merged tree. Fixes: registry-level required-arg
schema gate (invalid_args, task_create whitespace no-op found+fixed),
repeat-identical-error tool breaker (survives replan generations),
plan-vacuity review gate, report-readback compound collapse (F42 kept).
Residual: planner model quality (gh #37 family).


## Live validation campaign close (runs 1-9, 2026-09-18/19/22)

Nine e2e runs across the fix waves. Final attribution, stable across the last
four runs: the harness fixes all behave as pinned under live load — schema
gate rejects (task_create/task_get/list_directory/memory_get_context),
repeat-error breaker containment, plan-vacuity labelling, honest terminals,
zero context-overflow 500s since the 64K bump. The failing leg in every
recent run is the quantized 8B planner/coder output quality (empty plans,
narration-instead-of-tool-calls, run-to-run intent variance chat/compound/
quickplan) plus classifier/planner endpoint timeouts on a machine with
multiple loaded model servers. Runs 5-8 additionally contended with two
orphaned 64K-ctx 8B llama-servers left by --keep runs whose daemons were
killed externally (ppid=1, found and killed 2026-09-22).

Harness finding worth an issue: --keep runs whose daemon is externally
killed leak spawned runtimes (orphan sweep runs only at boot of the same
home); add spawn-record-age-keyed sweep.

Follow-up candidate (not started): widen the report-readback collapse arm
per the intent-shape diagnostic once a healthy-provider run captures the
verdict set; the arm never fired in runs 5-9 because routing never reached
the two-actionable shape (verdict variance upstream).
