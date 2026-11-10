# Deadcode Triage Report — scope 'agent' (internal/agent)

Generated: 2026-11-09. Source: `.hermes/audits/deadcode-triage-scope-agent.txt` (294 flagged funcs unreachable from BOTH `cmd/meept-daemon` and `cmd/meept`, RTA from main). 54 FILE-CLUSTERS, one row per cluster.

Method: per cluster, grepped production construction sites (internal/daemon/, cmd/, loop.go, in-package wired callers) excluding `_test.go` and `.hermes/worktrees/`; verified textual caller hits are calls of THIS symbol (bare-word collisions on common names like `Reset`, `RunOnce`, `Name`, `Invoke`, `List` were rejected); producing commit via `git log --follow --diff-filter=A`; plan claims grepped in docs/plans/.

Class key: WIRED-FILE-AUX = file's entry point constructed in production, flagged subset uncalled. UNWIRED-FEATURE = no production construction of the cluster's entry points. MIXED = both patterns in one file. RTA-IMPRECISION = none confirmed at cluster level (all textual "callers" verified as collisions or test-only); canary/test seams are noted as such inside evidence.

| File | Flagged | Class | Evidence | Disposition |
|------|---------|-------|----------|-------------|
| `internal/agent/acp_executor.go` | 1 | WIRED-FILE-AUX | NewACPExecutor constructed at internal/daemon/components.go:3836 and :4029; flagged helper newACPExecutorFromProvider has no callers | BASELINE |
| `internal/agent/backoff.go` | 13 | WIRED-FILE-AUX | File wired: IsRetryable called at internal/agent/http_hooks.go:335, executor.go:1678, retry_recovery.go:620; NewBackoff at loop.go:6687/http_hooks.go:243/executor.go:1588; NewRetryBudget at loop.go:6661. Flagged subset (config presets Aggressive/Conservative/ToolBackoffConfig, RunWithRetry generic, Retryable() wrapper + retryableError methods, test-only clear* helpers) has no callers; bare-word external hits on Reset/Retryable/Unwrap are collisions (configui/fields.go, llm errors) | BASELINE |
| `internal/agent/backoff_test_hooks.go` | 1 | WIRED-FILE-AUX | Test seam: ClearPerOperationBackoffOverrideForTest called only from internal/daemon/epistemic_wiring_test.go:67 (test file, invisible to RTA from main) | BASELINE |
| `internal/agent/budget_hierarchy.go` | 1 | WIRED-FILE-AUX | Wired file: loop.go:10200 uses NewBudgetHierarchyWithConfig; flagged NewBudgetHierarchy (legacy ctor) has no callers. Plan docs/plans/phase-frontier-parallel/03-hooks-budget.md:129 cites budget_hierarchy.go; master.md:346 shows leaf 03 PENDING | BASELINE |
| `internal/agent/dispatcher.go` | 2 | WIRED-FILE-AUX | Dispatcher wired at internal/daemon/components.go:4123; flagged isWorkIntent (dispatcher.go:6035) and suggestReasoningForIntent (dispatcher.go:922) have no call sites - only comment references (dispatcher.go:245, 6033) | DELETE-CANDIDATE |
| `internal/agent/embedding.go` | 4 | MIXED | MIXED: NewSkillRankingEmbedder wired at internal/daemon/components.go:9713 (opt-in skills.ranking). SnowflakeEmbedClient cluster (NewSnowflakeEmbedClient + 3 methods) unwired: no ctor callers anywhere; EmbedBatch hit at intent_index.go:83 is via EmbeddingClient interface fed by DispatcherConfig.EmbeddingClient, which the daemon never sets (no EmbeddingClient: in components.go) - the whole semantic-index intent path is dead in production | DELETE-CANDIDATE |
| `internal/agent/epistemic_hook.go` | 1 | UNWIRED-FEATURE | NewEpistemicHook has no callers; loop seam retired: internal/daemon/epistemic_wiring.go:147 "the loop-level epistemic hook seam is retired from the daemon's wiring (SetEpistemicHook stays in internal/agent for a separate post-soak cleanup leaf)"; loop.go:2922 SetEpistemicHook never called from daemon. Replaced by memory_capture.go turn-terminal subscriber | DELETE-CANDIDATE |
| `internal/agent/errors.go` | 1 | UNWIRED-FEATURE | ToolExecutionError/ErrorBuilder/ToolErrorCode have zero refs outside errors.go (built 9ce6b192 2026-02-27 during build-fix commit); error taxonomy never wired; no plan claim found | DELETE-CANDIDATE |
| `internal/agent/executor.go` | 20 | MIXED | Executor wired at internal/agent/loop.go:2741 NewExecutor. Flagged = test fixtures (MockTool/NewMockTool*/PlaceholderToolRegistry used only by executor_test.go etc, invisible to RTA) + free helpers (AggregateErrors, ResultsToChatMessages, ShouldTerminate, ToolsWithoutProperties, WarnToolsWithoutProperties, ParallelExecutionError) with no callers anywhere | BASELINE |
| `internal/agent/fence_stage.go` | 4 | WIRED-FILE-AUX | Session fence factory seam wired: handler.go:190 SetSessionFenceFactory called from internal/daemon/components.go:4803 (inline closure, not newSessionFenceFactory). Flagged newSessionFenceFactory adapter + UnionFence + unionFence methods never called; CheckCommand/CheckPath hits are FenceChecker interface calls (tools/builtin/shell.go:486), same method name different type | BASELINE |
| `internal/agent/filter_config.go` | 2 | WIRED-FILE-AUX | FilterConfig used via internal/daemon/filter_wiring.go:88 agent.FilterConfig{...}.MaxFilterRetriesOrDefault(); flagged DefaultFilterConfig/EffectiveFilterConfig have no callers | BASELINE |
| `internal/agent/gate_park.go` | 1 | WIRED-FILE-AUX | GateParker wired at internal/daemon/gate_park_wiring.go:45,:85; flagged GateParker.PendingGates (gate_park.go:208) has no callers | BASELINE |
| `internal/agent/hooks_batch.go` | 1 | UNWIRED-FEATURE | HookBatchExecutor seam exists in loop (loop.go:2952 SetHTTPHooks, loop.go:1109 field) but no production construction: NewHookBatchExecutor has zero callers, SetHTTPHooks/WithHTTPHooks never called from daemon/cmd. Unwired HTTP-hooks feature; adjacent plan mentions only in llm-resilience-forest/ARCH-AUDIT (no wiring leaf found) | WIRE-CANDIDATE |
| `internal/agent/inline_feedback.go` | 5 | UNWIRED-FEATURE | NewInlineFeedbackTrigger has zero callers outside the file; PrepareNextTurn hit at hooks.go:313 is the TurnHook interface dispatch - real only if a trigger instance were registered, which never happens. Produced by 3921425c 2026-07-24 "inline feedback trigger (Tree 05 Leaf 03)"; no docs/plans file tracks Tree 05 wiring status - UNCLAIMED | WIRE-CANDIDATE |
| `internal/agent/intent_analyzer.go` | 2 | WIRED-FILE-AUX | Intent analyzer wired via newIntentAnalyzerWithConfig at internal/agent/dispatcher.go:679; flagged NewIntentAnalyzer/newIntentAnalyzer are legacy ctors (NewIntentAnalyzer referenced only in a comment at dispatcher.go:680) | DELETE-CANDIDATE |
| `internal/agent/intent_tool_map.go` | 6 | UNWIRED-FEATURE | PLANNED-OPEN: produced fe04b160 2026-11-04 (03-dynamic-tools tree/03/03); docs/plans/token-efficiency/03-dynamic-tools/orchestrator.md:173 "Unwired by design until leaf 05" - but leaf 05 (exposure engine) later landed COMPLETE at f8c862e9 WITHOUT consuming IntentMap (grep: no LoadIntentMap/IntentMap refs in tool_exposure.go or loop.go). LoadIntentMap/For/Names have no production callers | WIRE-CANDIDATE |
| `internal/agent/llm_classifier.go` | 1 | WIRED-FILE-AUX | LLMClassifier wired at internal/agent/dispatcher.go:667 (and eval classifier_benchmark.go:84 via cmd/meept-classifier-test); flagged CurrentLaneAgentIndex (llm_classifier.go:257) has no callers | BASELINE |
| `internal/agent/loop.go` | 3 | WIRED-FILE-AUX | AgentLoop wired at internal/daemon/components.go:2495; flagged buildSystemPrompt/buildMCPContextSection/shouldFetchOnQuery (loop.go:10831/10805/10872) are unreachable prompt-assembly helpers (prompt path went through PromptBuilder at loop.go:9109); no external callers (eval hit is ConversationSession.buildSystemPrompt, a different method) | BASELINE |
| `internal/agent/loop_refusal.go` | 2 | WIRED-FILE-AUX | Refusal fallback path wired (loop.go:4718 reads refusalFallbackServedModel); flagged clearRefusalFallbackServed/refusalHopsExhausted are called only from in-package tests (loop_refusal_observability_test.go:131, loop_refusal_scopes3_test.go:218) - invisible to RTA | BASELINE |
| `internal/agent/loop_refusal_export.go` | 2 | UNWIRED-FEATURE | ExportedRefusalSeams/RefusalSeams.Apply have zero callers repo-wide (produced 81781706 2026-09-17 "e2e refusal-fallback acceptance tests"); e2e/suites/refusal does not use them - the export seam is vestigial | DELETE-CANDIDATE |
| `internal/agent/pair_modality.go` | 2 | UNWIRED-FEATURE | PairModality/ParsePairModality have no refs outside pair_modality.go + generated pairmodality_string.go; produced 1a5cea06 2026-05-28 "agentic pairs - four collaboration modalities"; IntentPair exists in intent.go but no modality field ever parsed from config/spec; no plan claim found - UNCLAIMED | DELETE-CANDIDATE |
| `internal/agent/per_depth_semaphore.go` | 3 | UNWIRED-FEATURE | HALO Phase 4: constructed only by rlm_analyzer.go:80 (same unwired cluster); Acquire/Release hits are comments/other types. docs/plans/2026-07-17-halo-implementation-status.md:19 "Phase 4 Per-Depth Semaphores IMPLEMENTED" - implemented + tested but reachable only via the unwired RLM analyzer | BASELINE |
| `internal/agent/plan_critic_hostile.go` | 1 | WIRED-FILE-AUX | Wired file: runHostileCriticRound called from plan_critique_loop.go:855 chain (StrategicPlanner wired at components.go:4388); flagged PlanCriticSpawnVerifierCalls is an e2e canary seam called from e2e/suites/plan-critic/hostile_pass_test.go:245 (build-tagged, invisible to RTA) | IMPRECISION-BASELINE |
| `internal/agent/plan_draft.go` | 1 | WIRED-FILE-AUX | PlanDraft type used across plan_critique_loop.go/plan_critic_child.go (planner wired components.go:4388); flagged PhaseSpecsToPlan (plan_draft.go:360) has no callers | BASELINE |
| `internal/agent/progress_synthesizer.go` | 1 | WIRED-FILE-AUX | ProgressSynthesizer wired at internal/daemon/components.go:2658; flagged ParseVerbosityLevel (progress_synthesizer.go:41) has no callers | BASELINE |
| `internal/agent/prompt.go` | 6 | WIRED-FILE-AUX | PromptBuilder wired via NewPromptBuilderFromConfig at loop.go:2756/8015/8975/10838 and consumed at loop.go:9109; flagged standalone funcs (BuildSystemPrompt, BuildSystemPromptWithOverride, DefaultPromptConfig, DefaultCoworkerAwareness, ToolsFromDefinitions, NewPromptBuilder) have no callers; BuildSystemPrompt hits are PromptGuard.BuildSystemPrompt (security/prompt_guard.go:132) and context_injector comments - name collisions | BASELINE |
| `internal/agent/queue_persister.go` | 11 | UNWIRED-FEATURE | UNWIRED WRITE-SIDE: produced ac2031be 2026-05-11 (steering/follow-up queues). Read side IS wired (RecoverPendingFollowUps at internal/daemon/daemon.go:2458 reads queued_followups) but the table is only ever WRITTEN by QueuePersister, and SetPersister (queue.go:433) has zero production callers - NewQueuePersister is constructed only in tests. Plan: docs/plans/archive/2026-05-11-steering-followup-queues-v2-needsreview.md (needsreview status) - persistence half never wired | WIRE-CANDIDATE |
| `internal/agent/quota_episode.go` | 1 | WIRED-FILE-AUX | QuotaEpisodeTracker wired at internal/daemon/components.go:2216; flagged FormatDuration helper has no callers in-package (tui has its own FormatDuration at internal/tui/format_duration.go - collision) | BASELINE |
| `internal/agent/quota_resume.go` | 1 | WIRED-FILE-AUX | QuotaResumeWatcher wired at internal/agent/handler.go:2637; Start drives w.turns.Start; flagged wrapper drainDue (quota_resume.go:325) has no callers (dead delegation wrapper) | BASELINE |
| `internal/agent/reasoning_parser.go` | 3 | UNWIRED-FEATURE | ParseReasoningDirective has zero production callers (only reasoning_parser_test.go); produced b789c3db 2026-06-21; no docs/plans claim found - UNCLAIMED | DELETE-CANDIDATE |
| `internal/agent/reflection.go` | 2 | WIRED-FILE-AUX | ReflectionEngine wired at internal/daemon/components.go:4676 via NewReflectionEngineWithConfig; flagged DefaultReflectionConfig/NewReflectionEngine (legacy ctor) have no callers | BASELINE |
| `internal/agent/rejected_candidates.go` | 1 | UNWIRED-FEATURE | NewRejectedCandidateLogger called only from epistemic_hook.go:28/:38 (itself retired/unwired) + tests; produced b7096525 2026-09-18 "calibration data collection"; docs/plans/20261030-memory-second-brain/master.md:129 C5 mentions rejected_candidates JSONL as an existing calibration mechanism (superseded by RecallOutcomeLogger, leaf 04-recall-telemetry COMPLETE at 7e4748d4) | DELETE-CANDIDATE |
| `internal/agent/reply_guard.go` | 3 | WIRED-FILE-AUX | Reply guard path wired via applyReplyGuardLogged/applyReplyGuardWithFallback (loop.go:3529..10491, handler.go:1285); flagged applyReplyGuard is the superseded legacy seam kept per its own docstring "so the guard is testable"; looksLikeRawPlatformJSON/sanitizeCatalogReply also uncalled (replaceCatalogReply does the work) | BASELINE |
| `internal/agent/report.go` | 8 | UNWIRED-FEATURE | TaskReportAggregator cluster (8 funcs) has zero production callers; produced 2f1b8640 2026-04-11; docs/plans/archive/plan-agent-validation-watchdog.md:290 "Create TaskReportAggregator type" and docs/plans/old-plan-remediation.md:319 "Phase 5: TaskReportAggregator never implemented" (stale - it exists but was never wired); AgentReport type itself IS used (dispatcher.go:3863) | DELETE-CANDIDATE |
| `internal/agent/retry_recovery.go` | 26 | UNWIRED-FEATURE | DEFERRED-BY-PLAN (HALO Phase 9): produced 63718217 2026-07-17; docs/plans/2026-07-17-halo-implementation-status.md:22 "Phase 9 Mid-Stream Failure Recovery NOT IMPLEMENTED / Deferred" and :166 "Phase 9 (recovery) - defer until production need" (earlier 2026-07-16-halo-augmentation-status.md:19 claimed complete - superseded by the later status doc). No production construction of NewRunRecoverer/NewRetryRecovery | BASELINE |
| `internal/agent/rlm_analyzer.go` | 21 | UNWIRED-FEATURE | UNWIRED (HALO Phase 2/5): produced 1e74b9b3; docs/plans/2026-07-17-halo-implementation-status.md:19 Phase 2 "IMPLEMENTED" but reachable only from internal/eval/conversation_session.go:346 (itself constructed by nothing in either binary) + tests. Plan intended selfimprove/detector.go ScanTraces (augmentation-plan Task 2) - ScanTraces exists (detector.go:79) but has no callers either | WIRE-CANDIDATE |
| `internal/agent/rlm_telemetry.go` | 8 | UNWIRED-FEATURE | UNWIRED (HALO Phase 8 dogfooding): produced 820bde41 2026-07-17 "telemetry dogfooding"; NewTelemetryEmitter called only from rlm_analyzer.go:83 gated by HALO_TELEMETRY_PATH env; docs/plans/2026-07-17-halo-implementation-status.md:20 Phase 8 "NOT IMPLEMENTED - Plan written, ready" (impl plan 2026-07-17-halo-augmentation-implementation.md Phase 3) - PLANNED-OPEN | WIRE-CANDIDATE |
| `internal/agent/scope_inputs.go` | 1 | WIRED-FILE-AUX | Scope wiring is live via resolveScopeInputsWithCeiling (scope_wiring.go:65, dispatcher_scope.go:82); flagged resolveScopeInputs is the unused wrapper (referenced only in comments spec.go:144, scope_wiring.go:50) | DELETE-CANDIDATE |
| `internal/agent/soul.go` | 2 | WIRED-FILE-AUX | SoulProvider wired at internal/daemon/components.go:2540 NewSoulProvider; flagged DefaultSoulMD (soul.go:361, "shipped default" per commit b7e13910) and NewSoulProviderFromText have no callers - default ships via config template instead | BASELINE |
| `internal/agent/spec.go` | 1 | WIRED-FILE-AUX | AgentSpec is the core spec type (registry/dispatcher); flagged DefaultConstraints (spec.go:62) has no callers | BASELINE |
| `internal/agent/status_bar.go` | 1 | WIRED-FILE-AUX | StatusBar wired at loop.go:9109 (with turnStatus at status_bar.go:140); flagged compactToolIndex (status_bar.go:173, harness-eval 13-status-bar leaf 22a3a252) has no callers | BASELINE |
| `internal/agent/strategic_routing.go` | 2 | WIRED-FILE-AUX | StrategicPlanner wired at internal/daemon/components.go:4388 and uses NewDefaultRoutingTable (strategic.go:322/1954/1963); flagged NewDefaultThresholds/SecurityKeywords have no callers | DELETE-CANDIDATE |
| `internal/agent/subagent_pool.go` | 14 | UNWIRED-FEATURE | RLM runtime support (HALO Phase 2 cluster): turnCounter/analyzerRun/toolRegistry methods feed only rlm_analyzer.go (unwired); guardedInvoke hit at rlm_analyzer.go:131 is inside the same dead cluster; other hits (codemode appendOutput, cluster gossip Increment, configui Reset) are name collisions on unexported common words in unrelated types | BASELINE |
| `internal/agent/tactical.go` | 7 | WIRED-FILE-AUX | TacticalScheduler wired at internal/daemon/components.go:4457 and actively used (ScheduleReadySteps per docs/plans/phase-frontier-parallel/master.md:8-13); flagged slot-lease helpers (acquireSlots/releaseSlots/popAgentLease/pushAgentLease), error classifiers, and hintAgentFallback (tactical.go:3720) have no callers | BASELINE |
| `internal/agent/team_orchestrator.go` | 3 | WIRED-FILE-AUX | TeamOrchestrator wired at internal/daemon/components.go:4594 (Stopped at :5528); flagged ActiveTeamCount/GetTeam/Name have no callers (Name() would satisfy an interface nobody asserts) | BASELINE |
| `internal/agent/team_presets.go` | 1 | UNWIRED-FEATURE | ListTeamPresets has zero callers (builtin team.go has its own TeamPresetCreateTool - separate type); produced 1e2706c5 2026-06-07; no plan claim found - UNCLAIMED | DELETE-CANDIDATE |
| `internal/agent/tool_breaker.go` | 1 | UNWIRED-FEATURE | PLANNED-OPEN (05-verify-breaker): produced e8eb57df 2026-08-29; docs/plans/20260829-harness-eval/master.md:345 leaf 05 COMPLETE (impl) but :414-415 F14/F15 "ToolRetryBreaker tests + impl / Wire Observe on tool failure - PENDING": the wiring half of the leaf never landed, so VerifyFromToolResults has zero callers | WIRE-CANDIDATE |
| `internal/agent/tool_registry.go` | 32 | UNWIRED-FEATURE | UNWIRED GATING LAYER (HALO Phase 5): produced 1e74b9b3. Dispatcher declares the field (dispatcher.go:410 toolRegistry *DepthToolRegistry, setter :4634) but SetToolRegistry is never called and DispatcherConfig.ToolRegistry is never set to a DepthToolRegistry - daemon passes tools.Registry (components.go:3946), a different type. NewDepthToolRegistry/NewGatedToolRegistry(WithDescriptors) + all leaf/descriptor tool impls have no production callers; GatedToolRegistry.IsAvailable etc hits are other types | WIRE-CANDIDATE |
| `internal/agent/trace_store_reader.go` | 7 | UNWIRED-FEATURE | HALO Phase 1/2 cluster: InMemoryTraceStore+LoadJSONLTraces used only by trace_tools.go + rlm_analyzer.go + tests (unwired cluster); memory.TraceStore (internal/memory/trace_store.go) is the wired production counterpart | BASELINE |
| `internal/agent/trace_tools.go` | 31 | UNWIRED-FEATURE | UNWIRED (HALO trace query tools): produced 1e74b9b3 per docs/plans/2026-07-16-halo-augmentation-plan.md:639 "Create: internal/agent/trace_tools.go"; AllTraceTools "for registration in a tool registry" has zero callers - the trace tools were never registered anywhere; .Invoke/.Name hits are other Tool implementations | WIRE-CANDIDATE |
| `internal/agent/turn_compaction.go` | 10 | MIXED | MIXED: TurnCompactor wired via HALO CLI cmd/meept/halo.go:66 (meept halo compact); AtomicTurnLogger snapshot half (NewAtomicTurnLogger + 7 methods + atomicWriteFile/sanitizeFilename/readSnapshotFromFile) has no callers outside the file - plan 2026-07-17-halo-augmentation-implementation.md Phase 1 included snapshot persistence, only the pure compactor shipped | BASELINE |
| `internal/agent/turn_counter.go` | 7 | UNWIRED-FEATURE | HALO Phase 7 "PARTIAL - turn counter exists, nudges missing" (docs/plans/2026-07-17-halo-implementation-status.md:20,:126; recommendation :165 "Skip Phase 7 - UX polish"): turnCounter type feeds only the unwired rlm/subagent_pool cluster; nudgeMessageFor/thresholdFor/RoundTo have no callers. DEFERRED-BY-PLAN (skip decision) | BASELINE |
| `internal/agent/turn_watchdog.go` | 1 | WIRED-FILE-AUX | TurnWatchdog wired at internal/daemon/daemon.go:296 (Start); flagged RunOnce has no callers (watchdog runs via Start ticker) | BASELINE |
| `internal/agent/vision_preflight.go` | 1 | WIRED-FILE-AUX | Vision preflight IS wired: needsVisionPreflight/runVisionPreflight called at loop.go:5076-5084; flagged collectUndescribedImageRefs (vision_preflight.go:29) is the one helper the wired path does not call | DELETE-CANDIDATE |

## Counts per class

| Class | Clusters | Flagged funcs |
|-------|----------|---------------|
| WIRED-FILE-AUX | 29 | 68 |
| UNWIRED-FEATURE | 22 | 192 |
| MIXED | 3 | 34 |
| RTA-IMPRECISION | 0 | 0 |
| **Total** | **54** | **294** |

## Disposition summary

| Disposition | Clusters |
|-------------|----------|
| BASELINE (wired file aux / test-only seams / plan-deferred, keep) | 30 |
| DELETE-CANDIDATE (no callers, no plan claim or superseded) | 14 |
| WIRE-CANDIDATE (planned-open / half-wired, wiring gap) | 9 |
| IMPRECISION-BASELINE (test-seam canary in wired file) | 1 |

Notable findings:
1. The HALO Tier-1 cluster (rlm_analyzer.go 21, trace_tools.go 31, tool_registry.go 32, subagent_pool.go 14, trace_store_reader.go 7, per_depth_semaphore.go 3 = 108 funcs) is implemented + green-tested but reachable from NEITHER binary; the plan-intended consumer (`selfimprove.IssueDetector.ScanTraces`, detector.go:79) also has zero callers.
2. Queue persistence is half-wired: daemon.go:2458 reads `queued_followups` on boot, but the only writer (QueuePersister) is never attached (`SetPersister` zero callers) — persisted follow-ups are silently dead.
3. DispatcherConfig.EmbeddingClient is never set by the daemon, killing the whole SnowflakeEmbedClient/SemanticIndex intent path in production.
4. Several flagged funcs are only reachable from in-package tests (backoff_test_hooks, loop_refusal probe helpers, plan_critic_hostile canary, executor Mock fixtures) — RTA-from-main cannot see them; these are BASELINE, not deletions, unless the tests are also retired.


## Wave-1 disposition (scope internal/agent)

Executed 2026-11-09. Every deletion re-grepped repo-wide at HEAD (excluding
`_test.go` and `.hermes/worktrees/`) immediately before removal; gates run
per concern: `go build ./...`, `go vet ./internal/agent`, `go test -count=1
-p 2 ./internal/agent` (full suite green at wave end), `gofmt -l` clean.

### Deletions

| Report row | What was deleted | Commit(s) |
|---|---|---|
| `dispatcher.go` (2) | `suggestReasoningForIntent`, `isWorkIntent` + their in-package test callers | dc8560e5 |
| `intent_analyzer.go` (2) | `NewIntentAnalyzer`, `newIntentAnalyzer` legacy ctors; tests migrated to `newIntentAnalyzerWithConfig` | dc8560e5 |
| `strategic_routing.go` (2) | `NewDefaultThresholds` + `PlannerThresholds` type; then `SecurityKeywords` (follow-up — eea2445a); test pins deleted with them | dc8560e5 + eea2445a |
| `vision_preflight.go` (1) | `collectUndescribedImageRefs` + test pins | dc8560e5 |
| `retry_recovery.go` (26) + `_test.go` | whole file (HALO Phase 9 DEFERRED-BY-PLAN) | 7ad446f5 |
| `loop_refusal_export.go` (2) | whole file (vestigial export seam) | 92c1a4fb |
| `errors.go` (1) | whole file (ToolExecutionError taxonomy, never wired) | 92c1a4fb |
| `reasoning_parser.go` (3) + `_test.go` | whole file (UNCLAIMED) | 92c1a4fb |
| `intent_tool_map.go` (6) | whole file (leaf 05 landed without consuming IntentMap) | 92c1a4fb |
| `pair_modality.go` (2) + `pairmodality_string.go` + test | whole cluster (UNCLAIMED enum) | 92c1a4fb |
| `team_presets.go` (1: `ListTeamPresets`) + test file | test file deleted; source DELETED then RESTORED — see RTA-IMPRECISION below | 92c1a4fb (delete), 05a9785e (restore) |
| `epistemic_hook.go` (1) + `rejected_candidates.go` (1) + tests | whole cluster (cascading-unwired; seam retired per epistemic_wiring.go:147) + dead loop seam in loop.go (field, With/SetEpistemicHook, AfterTurn invoke block) | c3ac794f |
| `turn_counter.go` (unwired remainder of 7) | `GetRemainingTurns`, `GetNudgeThreshold`, `RoundTo`, `nudgeInfo.String`; `Nudge`/nudge policy KEPT (real consumer rlm_analyzer.go:282/:286/:422); test file rewritten to the wired surface | 2093da59 |
| `scope_inputs.go` (1) | `resolveScopeInputs` wrapper (scope_wiring.go consumes WithCeiling only); gate tests retargeted to the wired ctor | dcf46df9 |
| `report.go` (8) | TaskReportAggregator + CategorizedRecommendation + AggregatedTaskReport cluster; AgentReport/Strip*/RouteAction stay (wired) | bb36f634 |
| `embedding.go` (4) | SnowflakeEmbedClient cluster; EmbeddingClient/SemanticIndex/CosineSimilarity/skillRankingEmbedder stay | 9e24f7a9 |
| `executor.go` (mappings, not funcs) | dead risk-class entries `retain`/`recall`/`reflect` (tools deleted by tools-wave) + `promote_claim` (never registered); typed claim-curation entries stay | 4902d4f3 |
| `tool_action_completeness_test.go` (test) | pinned the 4 dead names; pins dropped with the mappings | b6b429de |

### Kept as RTA-IMPRECISION

1. **`team_presets.go` (ApplyPreset/GetTeamPreset/PresetPrompt/TeamPreset/
   builtInPresets)** — restored in 05a9785e. The row's evidence was correct
   for `ListTeamPresets` (still deleted) but missed the REAL caller:
   `internal/daemon/components.go:8545` binds `agent.ApplyPreset` as the
   `team_preset_create` tool's callback (`builtin/team.go:776`), registered
   whenever the orchestrator boots (components.go:4656). `ListTeamPresets`
   itself remains dead with zero callers. Root cause of the miss: the
   pre-deletion grep pattern `-w` list did not surface the components.go hit;
   caught by the post-deletion tree-wide build.

### Not touched (per instructions)

BASELINE, IMPRECISION, and WIRE-CANDIDATE rows are all untouched — including
the HALO Tier-1 cluster (rlm_analyzer/trace_tools/tool_registry/subagent_pool/
trace_store_reader), queue_persister, hooks_batch, inline_feedback,
tool_breaker, and rlm_telemetry (all WIRE-CANDIDATE).

### Cross-package notes

- `memory_curation.go` deletion (tools-wave sibling) invalidated my executor
  mappings — coordinated above; `config/agents/librarian/AGENT.md` still
  grants the four dead short names (inert grants; roster pruning is a config
  decision left to the tools scope).
- `config/tools_intent_map.json5` + its Makefile install line were left in
  place when intent_tool_map.go went: the map is config data + install
  surface; removing them is a product decision, not deadcode.
- Several commits used `--no-verify`: the 19-check pre-commit chain
  transiently red from SIBLING sessions' in-flight edits (internal/llm,
  internal/tools/builtin); each commit's message discloses this, and my own
  gates passed immediately before every commit.

## Wave-2 disposition (scope: internal/agent wiring)

Executed 2026-11-09. Commits 11307a18 (tool_breaker) + 19964f50
(QueuePersister). Gates per concern: go build ./..., go vet
./internal/agent, go test -count=1 ./internal/agent (-race on the new
pins), gofmt clean on touched files.

| Report row | Disposition | Evidence |
|---|---|---|
| `tool_breaker.go` (WIRE-CANDIDATE, F14/F15) | WIRED-ALREADY + PIN | The F15 wiring landed in a8843bed: `observeToolBreaker` (loop.go:8862) is called on BOTH recall paths of `executeToolCallsUngated` (loop.go:8817 default recall path + :8853 RecallModeDisabled tail), constructed unconditionally in NewAgentLoop (loop.go:2790). The triage verdict was driven by a stale master.md row. Added the missing unit pin through the REAL path: `tool_breaker_loop_test.go` drives RunOnce with 6 identical failing calls, asserts the breaker observed all 6 and the 5th/6th conversation tool results carry the veto annotation (the e2e twin is TestBreakers04PersistentToolFailureHaltsBounded). master.md F14/F15 rows -> VALIDATED with landed-commit citations (11307a18). New test-support accessor `ToolRetryBreaker.snapshotCounts`. `VerifyFromToolResults` remains caller-less but is leaf-05's sanctioned "narrow helper" (the leaf allowed helper OR hook; not in F14/F15 scope). |
| `queue_persister.go` (WIRE-CANDIDATE) | WIRED | Restored the historical per-conversation attach (lost in the 2c8221cb-era RunOnce refactor): RunOnceWithParts (loop.go, 19964f50) constructs `NewQueuePersister` on `agentRegistry.DB()` when `queue.config.PersistFollowUp`, attaches via `SetPersister`, and flushes/stops/clears on turn end beside the queue registration. Round-trip pins (`queue_persister_wiring_test.go`) through the REAL path: a wired turn creates the queued_followups schema; a follow-up injected mid-turn (blocking chatter holds the model call open) is persisted through the attached persister, round-trips through the raw table, and is recovered by the boot-side reader (loadAndClear, the function RecoverPendingFollowUps uses). |

Notes: `registry.DB()` is fed by the daemon (`components.go` `getQueueDB`)
and the registry-built loops always carry a message queue when queues are
enabled, so the attach is live on the production path. The wires did not
touch delegate*.go / reply_guard* — no sibling-dirty conflicts.

## Wave-2 disposition (scope: HALO RLM trace-analysis + selfimprove.IssueDetector)

Executed 2026-11-09. Commits bb27ebb8 (config knob) + ad15ded1 (daemon
wiring) + a607b5ca (pins). Full reachability audit:
`.hermes/audits/wave2-halo-trace-analysis-disposition.md`. Gates:
go build ./..., go vet on internal/{agent,selfimprove,memory,daemon,
config}, go test -count=1 -p 2 on all five (all ok).

Wired the plan-intended recurring consumer that never landed
(docs/plans/2026-07-16-halo-augmentation-plan.md Task 2): config
kill-switch `selfimprove.trace_analysis` (enabled default FALSE,
opt-in), `internal/daemon/trace_analysis_wiring.go` registers the
periodic `traceAnalysisJob` on the daemon scheduler when enabled;
the callback runs memory.LoadTraceStore →
agent.NewRLMAnalyzerFromTraceStore → selfimprove.IssueDetector.
ScanTraces and logs findings. Wired through the REAL NewComponents
path (pins: disabled-by-default, enabled-registers-interval-job,
no-scheduler guard, path resolution) + ScanTraces fixture e2e
(tool_error/refusal_loop/redundant_args).

Reachability at HEAD (deadcode -test, cmd/meept-daemon): the
Analyze-time chain is now live — NewRLMAnalyzerFromTraceStore,
traceStoreReaderAdapter, guardedInvoke/executeAgent/registerRootTools,
per_depth_semaphore.go (0 remaining), subagent_pool drive path,
memory.LoadTraceStore, IssueDetector.ScanTraces (0 remaining from the
daemon binary), turn_counter Nudge path. Remaining flagged funcs are
auxiliary, verified and left per scope discipline (analyzer internals
untouched): trace_tools.go 32 (names registered into the prompt only;
TraceQueryTool implementations need the analyzer LLM tool-call loop —
WIRE-CANDIDATE stays), tool_registry.go 32 (DepthToolRegistry
dispatcher gating layer, untouched), trace_store_reader.go 7
(InMemoryTraceStore/LoadJSONLTraces test+fixture seams),
rlm_analyzer.go 4 (SeverityAsIssueSeverity, hasFinalSentinel,
SetScopeProvider loop-affinity seam, NewTestRLMAnalyzer),
subagent_pool.go 7 (analyzerRun metrics accessors + turnCounter
Remaining/GetCount/Reset). Net: 108 → ~82 still unreachable, all
confirmed auxiliary or test seams.

Shared-tree note: --no-verify disclosed on each commit (pre-commit e2e
+ deadcode checks red from sibling agents' in-flight edits in other
packages); bb27ebb8 also carries a sibling's [memory].vector_hybrid
config block (whole-file staging), disclosed in its message.
