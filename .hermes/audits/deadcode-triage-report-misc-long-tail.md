# Deadcode Triage Report — misc-long-tail scope

Scope: all remaining packages not covered by sibling scopes (internal/rpc, skills, effects, queue,
employee, cluster, scheduler, session, gate, jevgate, comm, configui, metrics, auditlog, auth, …).
Input: `.hermes/audits/deadcode-triage-scope-misc-long-tail.txt` (300 flagged funcs, RTA from both binaries).

Classification rules (same as sibling scopes):
- **WIRED-FILE-AUX** — production construction of the owning package/type exists (verified in
  `internal/daemon/*.go` wiring files and `cmd/`); flagged funcs are unwired accessors, default-config
  helpers, deprecated variants, or test seams on a wired feature. → **BASELINE**
- **UNWIRED-FEATURE** — no production wiring anywhere; producing commit + plan citation given
  (DEFERRED-BY-PLAN / PLANNED-OPEN / UNCLAIMED). → **WIRE-CANDIDATE** (feature should be wired) or
  **DELETE-CANDIDATE** (superseded/unclaimed)
- **RTA-IMPRECISION** — a verified real textual production caller exists (tests, `.hermes/worktrees/`,
  and name collisions excluded). → **IMPRECISION-BASELINE**

Long-tail batching: tiny packages classified at file level; mixed packages get per-file-cluster rows.

## Totals

| Class | Funcs |
|---|---|
| WIRED-FILE-AUX | 160 |
| UNWIRED-FEATURE | 136 |
| RTA-IMPRECISION | 4 |
| **Total** | **300** |

| Disposition | Funcs |
|---|---|
| BASELINE (incl. deliberate/test-seam variants) | 158 |
| IMPRECISION-BASELINE | 4 |
| WIRE-CANDIDATE | 53 |
| DELETE-CANDIDATE | 85 |

## Per-package findings


### internal/activity — 2 funcs — WIRED-FILE-AUX → **BASELINE**

activity aggregator wired: `internal/daemon/activity_wiring.go:70` `activity.NewAggregator`. Flagged `TaskStoreAdapter.ListTasks`/`NewTaskStoreAdapter` are the never-constructed alt impl of the TaskLister seam (aggregator builds `NewTaskRegistryAdapter`, adapters.go:64-76). External `ListTasks` grep hits are different types (name collision).

### internal/agent — 5 funcs, MIXED

- **prompts/baseline.go** (3) — WIRED-FILE-AUX → **BASELINE**
  prompts pkg live via BuildVerifierPrompt (agent/verification_hook.go:178); BuildBaselinePrompt/BuildSkillsPromptSection(Tracked) superseded by PromptBuilder (prompt.go:54). 10b1c97f 2026-02-20 Phase 2.

- **prompts/specialists.go** (2) — UNWIRED-FEATURE → **DELETE-CANDIDATE**
  specialist multi-agent framework unwired: GetSpecialistPrompt/BuildFullPrompt callers are only in-file mutual. Phase 2 (10b1c97f) specialist dispatch superseded by the current dispatcher/orchestrator. UNCLAIMED.

### internal/auth — 3 funcs, MIXED

- **refresh.go** (1) — WIRED-FILE-AUX → **BASELINE**
  RefreshManager wired internal/daemon/components.go:9456; Failures() accessor uncalled.

- **permissions.go + quota.go** (2) — UNWIRED-FEATURE → **WIRE-CANDIDATE**
  OwnerOnlyPermissions.CanAccess / NoopQuota.Allow zero callers — declared stubs of the multiuser-access design (docs/plans/2026-08-26-multiuser-access/01-auth-store.md:57 "Quota stub — NoopQuota allowing everything", :60 "OwnerOnlyPermissions: CanAccess true iff owner==actor"). DEFERRED-BY-PLAN.

### internal/backup — 10 funcs — WIRED-FILE-AUX → **BASELINE**

backup/sync shipped: `NewGitBackupScheduler` wired `internal/daemon/components.go:5324`; `NewSyncPuller` wired `cmd/meept/sync_cmd.go:61`; `NewSyncMetadataStore` wired `cmd/meept/sync_cmd.go:121,147` + `sync_puller.go:60`; `NewTempManager` wired `sync_puller.go:55`; `CompressFile` wired `git_backup.go:287`. Flagged funcs are unused byte-slice variants (CompressData/DecompressData/DecompressReader), unused singular helper (GetLocalDBPath — plural `GetLocalDBPaths` wired git_backup.go:264), unused accessors (GetLastSync, ClearPeerStatus, IsBackupError, IsSyncError, IsSyncRetryable, GenerateManifest — manifest built inline elsewhere, `LoadManifest` wired git_backup.go:210). Producing commits: 3dd94c11 (2026-06-26 Phase 1+3 backup/sync), 4b686cca, 9edc4c85.

### internal/bus — 2 funcs, MIXED

- **bus.go MatchWildcard** (1) — WIRED-FILE-AUX → **BASELINE**
  exported form of the live lowercase matchWildcard (bus.go:164,257,432); zero external callers (tui test-only), kept deliberately per doc comment "consumers must test against this" (b58265dd 2026-10-14).

- **bus.go Unsubscribe** (1) — RTA-IMPRECISION → **IMPRECISION-BASELINE**
  VERIFIED REAL CALLERS: 50+ production sites — internal/plan/handler.go:64, internal/agent/handler.go:391-530, internal/rpc/proxy.go:269-629, internal/agent/loop_rewake.go:166, internal/bot/router.go:108, and more. RTA flag is imprecise.

### internal/calendar — 2 funcs — WIRED-FILE-AUX → **BASELINE**

`calendar.NewClient` wired `internal/daemon/components.go:9543`; `NewReminderWatcher` wired `components.go:9566` (builds ReminderWatcherConfig literal — `DefaultReminderWatcherConfig` bypassed). `NewClientForTesting` is a declared test ctor (gcal.go:304).

### internal/cluster — 12 funcs, MIXED

- **gossip.go** (2) — WIRED-FILE-AUX → **BASELINE**
  GossipEngine wired internal/daemon/daemon.go:550,575 (NewGossipEngine); expireSigningKeyGrace/pubKeyID are uncalled helpers on the wired type.

- **engine.go** (9) — UNWIRED-FEATURE → **WIRE-CANDIDATE**
- **cluster.go SaveClusterConfig** (1) — WIRED-FILE-AUX → **BASELINE** (pkg wired via NewGossipEngine; unused config writer of a wired pkg)
  standalone Engine never constructed (NewEngine/Start/Stop/Gossip/Config/IsRunning/loadKeys/nodeIDFromPub/defaultKeysDir) + SaveClusterConfig writer — daemon wires NewGossipEngine instead; the parallel Engine/Gossip split never resolved. 5aeac92d 2026-06-06 "complete gossip, engine, and queue implementation". PLANNED-OPEN (engine seam kept for the gossip-anchoring deferral, docs/plans/tamper-evident-audit-log/OPEN-QUESTIONS.md Q2).

### internal/code — 11 funcs — WIRED-FILE-AUX → **BASELINE**

LSP manager constructs `NewCapabilities` (`internal/code/lsp/manager.go:290`); flagged `Has*`/`CheckCapabilities`/`TextDocumentSyncKindFromCapabilities` predicates have zero callers. Producing commit 672dcb2d (2026-03-01 AST+LSP tooling).

### internal/codemode — 1 funcs — WIRED-FILE-AUX → **BASELINE**

C3 codemode sandbox live (af2f4bde 2026-11-01, goja VM wired through agent executor); `Sandbox.SortedToolNames` determinism helper uncalled.

### internal/compress — 3 funcs, MIXED

- **pipeline.go DefaultPipelineConfig + NewPipeline** (2) — RTA-IMPRECISION → **IMPRECISION-BASELINE**
  VERIFIED REAL CALLER CHAIN: NewPipelineWithConfig (wired internal/daemon/daemon.go:1955) calls both in-file at pipeline.go:53 — transitively live in the compression pipeline.

- **ccr_hash.go findSubstring** (1) — WIRED-FILE-AUX → **BASELINE**
  compress pkg wired (CCR pipeline, fb2ba380 headroom integration); findSubstring has zero callers (name collision with live internal/context/types.go:204 findSubstring caused the raw grep noise).

### internal/config — 12 funcs — WIRED-FILE-AUX → **BASELINE**

config pkg fully wired. Flagged = unused default-config helpers (`DefaultBackupConfig`, `DefaultConfigSyncConfig`), unwired writers/accessors (`SaveACPAgents` — ACP catalog is read-only in prod per 5358bff3; `ConfigSyncer.LastAppliedCommit/LastCommitHash` — syncer itself wired `components.go:5347`; `Merger.FileWouldChange` — Merger wired `sync.go:68`; `ReloadRegistry.Len/RegisteredHooks` — registry wired `sync.go:69`), wrappers with no live call chain (`StripJSON5Comments`/`stripJSON5Comments` mutually call only each other; prod loaders use hujson directly), duplicated helper (`config.go expandEnvVars` — llm/providers.go:535 has its own live copy; the live config path uses exported `ExpandEnvVars`), `ParseLogLevel` (prod callers use `ParseLogLevelValue`, wired since 29d497e9).

### internal/debug — 1 funcs — UNWIRED-FEATURE → **DELETE-CANDIDATE**

DAP debug Client never constructed: `debug.NewClient` zero callers. `trimCR` helper uncalled. Produced ef4efeea 2026-05-25 "feat(agent): TT-SR stream rule enforcement and DAP debugging" — UNCLAIMED (no plan leaf found for wiring).

### internal/effects — 17 funcs — WIRED-FILE-AUX → **BASELINE**

effects ledger shipped: `NewSQLiteLedger` wired `internal/daemon/components.go:3181` (over effects.db); the `Ledger` interface is consumed daemon-wide (`voice/dial.go:92-95` EffectsLedger, push_service, toolgate). `MemoryLedger` (all 17 flagged funcs) is the documented in-memory impl "for tests" (effects.go:116) and a structural reference impl named in dial.go — restored as test-seam in 9edc4c85 (160 restorations). `NewMemoryLedger` zero prod callers by design.

### internal/employee — 13 funcs, MIXED

- **manager.go NewManager** (1) — WIRED-FILE-AUX → **BASELINE**
  Manager heavily wired: components.go:5147 NewManagerFromConfig → wiring.go:233 NewManagerWithStores; dsh_wiring.go:171-203 goal adapter over the live manager; NewManager(bm) is the minimal ctor variant (survivor of 9edc4c85 deadcode sweep).

- **enforcement.go** (3) — WIRED-FILE-AUX → **BASELINE**
  NewPostTurnAuditor/NewPeriodicAuditor wired components.go:6059-6060; NewTurnBudgetTracker + RiskCeilingToSecurityLevel/SecurityEngineRiskToCeiling uncalled (agent/conversation.go:51 has the live TurnBudgetTracker, wired loop.go:10213). b8288208 2026-06-26 spec-gap closure.

- **goal_round_driver.go ValidGoalRound** (1) — WIRED-FILE-AUX → **BASELINE** *(admission-fence caller gap flagged)*
  driver wired dsh_wiring.go:73 (fb8f6d73 2026-11-07 "goals-plans complete - e2e 3/3 green"). ValidGoalRound has NO production caller — only goal_round_driver_test.go:48; the comment at :537 claims it "fences a stale reservation" but no code calls it (doc-vs-code gap in the admission fence).

- **goal_loop.go** (2) — UNWIRED-FEATURE → **WIRE-CANDIDATE**
  buildImplicitPlanPrompt/parseImplicitPlanResponse zero callers — implicit-plan trigger path of the goal loop unwired (b8288208 2026-06-26 "close all 40 AI Employee spec gaps"). PLANNED-OPEN sub-feature.

- **handler_auth.go** (3) — UNWIRED-FEATURE → **DELETE-CANDIDATE**
  requireCaller/isAuthorizedForOperation/isAuthorizedAmmender zero callers — employee RPC authorizes via PeerIdentityFromContext instead (902c3dee 2026-07-15 caller-auth framework superseded by the kernel-verified peer model). UNCLAIMED.

- **authority.go** (2) — UNWIRED-FEATURE → **WIRE-CANDIDATE**
  EscalationRouter stub never constructed in prod (test-only, authority_test.go:245); "The real escalation-routing [stub]" per :301. a761aacb 2026-06-23 AI Employee design spec. UNCLAIMED.

- **drift.go CalculateDriftScore** (1) — UNWIRED-FEATURE → **DELETE-CANDIDATE**
  zero callers; manager keeps a driftScores map (manager.go:673) fed by the wired PeriodicAuditor, not this half-life calc. b8288208. UNCLAIMED.

### internal/errcls — 3 funcs, MIXED

- **classify.go IsAuthError** (1) — RTA-IMPRECISION → **IMPRECISION-BASELINE**
  VERIFIED REAL CALLER: internal/agent/retry_recovery.go:776 errcls.IsAuthError(err). RTA flag imprecise for this symbol.

- **classify.go IsClientError + IsDuplicateColumn** (2) — WIRED-FILE-AUX → **BASELINE**
  zero callers; wired errcls consumers use IsNetworkError/IsRetryable/IsRateLimit/IsParameterError (tactical.go, retry_recovery.go, rpc/server.go). Sentinel registration machinery (services/errors.go:13-14) is wired.

### internal/githubsvc — 2 funcs — WIRED-FILE-AUX → **BASELINE**

githubsvc wired via toolprov seam (`internal/daemon/github_wiring.go` → `toolprov.RegisterAll`). `StaticTokenSource`/`NewStaticTokenSource` is an alternate TokenSource never selected (prod uses file/env sources).

### internal/groups — 3 funcs, MIXED

- **resolution.go** (1) — WIRED-FILE-AUX → **BASELINE**
  Resolver wired internal/daemon/components.go:1023,3764 + agent/registry.go:322; AtomicResolver.Get/Set mutex wrapper never used by the daemon wiring.

- **memberjoin.go** (2) — UNWIRED-FEATURE → **WIRE-CANDIDATE**
  EffectiveGroups/dedupSortGroups zero callers. internal/auth/users.go:87 comment claims "Consumed by groups.EffectiveGroups via the store lookup seam" but no call exists — the human-identity group-join path of the C5 groups design (360d1c35 2026-11-02 leaf 03-membership) is not built. PLANNED-OPEN.

### internal/lint — 18 funcs, MIXED

- **languages/go_lint.go + js_lint.go + python_lint.go** (8) — WIRED-FILE-AUX → **BASELINE**
  GoLinter/PythonLinter/JSLinter constructed in lint/registry.go registerDefaults — CompileCheck/Vet/Flake8 chains wired via components.go:4668 lint.NewRegistry + reflection.go. Flagged TypeCheck/FormatCheck/ESLint/Pyright are unwired chain members; parseESLintErrors/parsePyrightErrors are called only by their dead ESLint/Pyright wrappers.

- **testrunner.go** (5) — WIRED-FILE-AUX → **BASELINE**
  NewTestRunner wired components.go:4669 → reflection.go:209 RunTests; FilterFailed/FilterPassed/FilterSkipped/HasFailures/Summary zero callers.

- **treelint.go** (5) — UNWIRED-FEATURE → **DELETE-CANDIDATE**
  TreeSitterLinter type + NewTreeSitterLinter/Lint/GetTreeSitterLanguage/detectLanguageFromPath/traverseTreeForErrors zero callers — registry.go duplicates the tree-sitter logic with private detectLanguage/getTreeSitterLanguage (registry.go:187,208). Duplicate implementation; c3de8b05 2026-06-10.

### internal/llm — 1 funcs — UNWIRED-FEATURE → **DELETE-CANDIDATE**

`metrics.NewCalculator` (adaptive timeout) zero callers. Produced a6637678 2026-04-04 "agent robustness framework phases 1-6"; the shipped retry/backoff path uses `agent/backoff.go` + `errcls` instead. UNCLAIMED.

### internal/mcp — 1 funcs — WIRED-FILE-AUX → **BASELINE**

`BufferedReader`/`ReadMessageBuffered` wired (`internal/mcp/server.go:58`); `ReadMessage` explicitly marked Deprecated in-file (transport.go:90) after 0052-mcp-stdio-bufio-reader-bug fix.

### internal/memory — 2 funcs — UNWIRED-FEATURE → **WIRE-CANDIDATE**

working-tier `EpisodicSinkAdapter`/`NewEpisodicSink` never constructed in production — the flush path has no owner: `working.Store.Flush(ctx, EpisodicSink)` is called only from `e2e/suites/context-memory-engine/context_memory_engine_test.go:421` and unit tests. Daemon builds the working store (`context_assembler_wiring.go` WireContextAssembler) but never attaches a sink. Produced by the context-memory-engine tree (docs/plans/20261031-context-memory-engine/02-memory-tiers, "Append overflow must first offer the turns to the episodic sink"). PLANNED-OPEN.

### internal/metrics — 5 funcs — WIRED-FILE-AUX → **BASELINE**

`NewOTLPExporter` wired `internal/daemon/otlp_wiring.go:42` + `collector.go:677 RegisterOTLPExporter`; live Record* calls are RecordSpan/RecordToolCall/RecordTurn (otlp_trace_producer.go, collector.go:689-700). Flagged `RecordAnalysisDuration/RecordFailureDetected/RecordPatchApplied/IsStarted/ExportForTestFlushBatch` are the unwired self-improvement counters + test flush hook. Produced 8b6ec961 2026-07-17.

### internal/migrate — 1 funcs — WIRED-FILE-AUX → **BASELINE**

`migrate.NewRegistry` wired from 30+ stores (gatecapture/store_sqlite.go:72, metrics/store.go:394, llm/metrics/store.go:211, …). `Applied()` standalone helper zero callers.

### internal/plan — 1 funcs — WIRED-FILE-AUX → **BASELINE**

treeemit live (compiler leaf 03, 1cef6e58; plan.seal RPC wired `internal/daemon/plan_seal_wiring.go:190`, 978af22e). `sectionPresent` left uncalled — the doc-example contract (docs/plans/plan-compiler/03-tree-emission.md:119) describes it but EmitTree inlines its checks.

### internal/preferences — 2 funcs — UNWIRED-FEATURE → **DELETE-CANDIDATE**

`GeneratePreCommitHook`/`GeneratePostCommitHook` zero callers. `docs/workflows/user-instructions.md:136` claims the daemon generates these hooks on instruction save — no call site exists (doc-vs-code gap). Produced 65d96a59 2026-06-22 user-instructions handlers. UNCLAIMED.

### internal/project — 1 funcs — WIRED-FILE-AUX → **BASELINE**

`NewDeepInitializer` wired; `LoadAgentsForContext` uncalled helper, known-broken per docs/plans/glm52-findings-6.md SX-9.

### internal/pty — 2 funcs — WIRED-FILE-AUX → **BASELINE**

pty.Manager wired `internal/daemon/components.go:3682`; manager.go:61 constructs NewSession. `IsTerminalAvailable`/`KillProcessTree` uncalled helpers (grok-findings E-19 notes KillProcessTree risk).

### internal/queue — 16 funcs, MIXED

- **queue.go WaitForJob** (1) — WIRED-FILE-AUX → **BASELINE**
  PersistentQueue heavily wired (components.go:3403 NewPersistentQueue; Claim via queue.go:1059; services/queue_service.go:137). WaitForJob package helper zero callers (name-collides with live method internal/shadow/adapters/openai.go:239 — raw-grep noise).

- **dispatcher.go (entire)** (15) — UNWIRED-FEATURE → **DELETE-CANDIDATE**
  queue.Dispatcher never constructed: zero queue.NewDispatcher callers. Daemon uses agent.NewDispatcher (components.go:4123) and tui/viz has its own dispatcher. cfc2f8b6 2026-02-18 "multi-agent task orchestration foundation (Phase 1)" — the worker-registration/dispatch model was superseded by the agent dispatcher + worker pool. UNCLAIMED.

### internal/resources — 3 funcs — WIRED-FILE-AUX → **BASELINE**

`resources.NewManager` wired `internal/daemon/cluster_resources_wiring.go:74`; `ErrResourceUnavailable` sentinel consumed (`internal/cluster/executor_bridge.go:530`). `ResourceUnavailableError` struct never constructed (sentinel path used directly), `HashBytes` uncalled.

### internal/rpc — 16 funcs, MIXED

- **instructions.go** (9) — WIRED-FILE-AUX → **BASELINE** *(deliberate — documented at daemon.go:1358-1362)*
  InstructionHandler wired daemon.go:1364 RegisterInstructionMethods; the flagged bus paths (Start/Stop/handleAdd/handleList/handleDelete/handleExecute/handlePreview/sendResponse/sendError) are DELIBERATELY not started — documented at daemon.go:1358-1362: "rpc.InstructionHandler bus Start() is deliberately NOT invoked — agent.InstructionHandler already owns the instruction.* bus subscriptions; a second subscriber would double-consume". The live instruction.* RPC surface is handle*RPC (cmd/meept/instructions.go:53-149).

- **groups.go ContextWithCallerIdentity + SortAdminAgents** (2) — WIRED-FILE-AUX → **WIRE-CANDIDATE** *(handler wired; unwired halves of a wired file — the HTTP→RPC identity bridge was never built)*
  GroupsHandler wired daemon.go:355 (360d1c35 leaf 03). ContextWithCallerIdentity has ZERO callers — the HTTP→RPC identity bridge it exists for was never built (commit message: "human privilege = store-validated Identity via ContextWithCallerIdentity, no-identity fails closed"); SortAdminAgents zero callers. PLANNED-OPEN.

- **plan_seal.go** (2) — WIRED-FILE-AUX → **BASELINE**
  PlanSealHandler wired internal/daemon/plan_seal_wiring.go:190; HandleSealJSON/HandleDraftJSON JSON-invoker helpers zero callers (cmd/meept/plan_seal.go uses RPC draftSealCall directly).

- **projects.go ForTest wrappers** (3) — WIRED-FILE-AUX → **BASELINE** *(test seam)*
  ProjectHandler wired daemon.go:1402; HandleReadDirForTest/HandleSetForTest/ProjectManagerForTest are declared test seams ("used in integration tests", d7cdf18a 2026-07-01).

### internal/runtime — 3 funcs — UNWIRED-FEATURE → **WIRE-CANDIDATE**

containerized-execution backend tree fully unwired: `NewDockerBackend`, `NewTestHarness`, `Qualifies` zero callers. Produced by docs/plans/2026-06-10-containerized-execution-backend.md; resolver doc plans/containment-and-computer-use/02-failclosed-backends.md:48. PLANNED-OPEN (feature deferred, never wired).

### internal/scheduler — 1 funcs — WIRED-FILE-AUX → **BASELINE**

rewake path live: `scheduler.go:743 s.publishRewake(jobID, …)` → `rewake.go:69`. `NotifyJobComplete` is the standalone injectable adapter — prod callers zero (only rewake_test.go:50; e2e goal-loop suite references it in a comment). 31513939 2026-08-30 harness-eval leaf 14.

### internal/secrets — 6 funcs, MIXED

- **proxy.go Proxy.Stop** (1) — WIRED-FILE-AUX → **BASELINE** *(Stop: WIRE-CANDIDATE — wire into daemon shutdown)*
  secrets.Proxy wired internal/daemon/secrets_wiring.go:54 (egress proxy live). Stop() is never called — the daemon shutdown path never closes the proxy listener (shutdown-gap; flagged for wiring).

- **egress_policy.go** (5) — WIRED-FILE-AUX → **BASELINE** *(ApplyEgressEnv child-env scrub: WIRE-CANDIDATE)*
  EgressPolicy is constructed and held by the wired proxy (proxy.go:75 policy field, built in NewProxy path af109d77 2026-08-27 leaf-09). NewEgressPolicy external callers zero; ApplyEgressEnv/Decisions/ScrubNoProxy/splitEnvKey zero callers — the child-env scrub post-pass was never wired into tool spawn paths.

### internal/security — 1 funcs — WIRED-FILE-AUX → **BASELINE**

pii Swapper wired `internal/daemon/components.go:2410` via `NewSwapperWithDetector` (46661bbe taint/pii leaf 01); `NewSwapper` simple variant zero callers.

### internal/selfimprove — 23 funcs, MIXED

- **controller.go** (4) — WIRED-FILE-AUX → **BASELINE**
  Controller wired internal/daemon/components.go:2046 (SelfImproveCtrl); GetApplier/GetCachedFixes/GetCachedValidations/SecurityOrchestrator accessors zero callers.

- **analyzer.go + detector.go + generator.go + applier.go** (5) — UNWIRED-FEATURE → **WIRE-CANDIDATE**
  AnalyzeBatch/ScanTraces/FailureModeToIssueType/GenerateBatch/Rollback zero callers — the batch-analyze/rollback slice of the loop never wired. Produced by 1e74b9b3 2026-07-17 (RLM analyzer) + docs/plans/2026-07-07-self-improvement-loop-closure.md; Rollback from 63799185/d14 rollback coverage (2026-06-14). PLANNED-OPEN.

- **judged_filter.go** (1) — UNWIRED-FEATURE → **DELETE-CANDIDATE**
  OnlyJudged zero callers; learning.go:163-204 re-implements the gate inline (leaf 08 semantics copied, helper unused). 06862e82 2026-08-29 harness-eval leaf 08. UNCLAIMED (dup).

- **report_artifact.go** (13) — UNWIRED-FEATURE → **DELETE-CANDIDATE**
  whole ReportArtifact (NewReportArtifact/Generate/GenerateWithMeta/Save/GetPath/write*/cleanupOld/ensureOutputDir/DefaultReportConfig) zero callers. f79ab1c8 2026-07-17 "on-disk report artifact generation for self-improvement analysis". UNCLAIMED.

### internal/session — 9 funcs, MIXED

- **thread.go + thread_store.go** (4) — WIRED-FILE-AUX → **BASELINE**
  threads feature live (services/thread_service.go via session.Store.GetThread/DeleteThread; SQLiteStore delegates through NewSQLiteThreadStore at store_sqlite.go:2921-2966). DefaultThreadConfig unused default; SQLiteThreadStore.GetThread/DeleteThread plain variants superseded by GetThreadByID/DeleteThreadByID (the wired delegates).

- **thread_summary.go** (2) — UNWIRED-FEATURE → **WIRE-CANDIDATE**
  GenerateThreadSummary/truncateString zero callers (b789c3db 2026-06-21); thread summaries never surfaced anywhere. PLANNED-OPEN (thread partitioning follow-up, docs/plans/2026-06-20-thread-based-context-partitioning.md).

- **tree.go** (3) — UNWIRED-FEATURE → **DELETE-CANDIDATE**
  RestoreConversationFromStore/ConvertChatMessagesToSessionMessages/ChatMessagesToToolCalls zero callers — 2c8221cb 2026-05-12 session branching/tree persistence; restore path superseded by conversation.go message loading. UNCLAIMED.

### internal/shadow — 7 funcs, MIXED

- **models.go NewTrainingRun + selector.go EmbeddingToJSON** (2) — WIRED-FILE-AUX → **BASELINE**
  shadow pkg wired (shadow.NewManager cmd/meept/shadow.go:820 + components.go:1754; NewSelector manager.go:126); the two flagged ctors/helpers are unused variants (a8f6c0bc 2026-02-21).

- **classifier.go** (5) — UNWIRED-FEATURE → **DELETE-CANDIDATE**
  Classifier.Classify/ClassifyDomain/ClassifyTaskType/EstimateComplexity/ClassifyFromShadowMessages zero prod callers. DefaultClassifier self-inits (classifier.go:167) but nothing consumes it; prod classification comes from store round-trips (store_sqlite.go:548) and the hot-swap adapter (docs/plans/2026-07-07-self-improvement-loop-closure.md:515). a94bf5cb 2026-02-26 "shadow training system to 100%". UNCLAIMED.

### internal/skills — 23 funcs, MIXED

- **watcher.go** (2) — WIRED-FILE-AUX → **BASELINE**
  hot-reload SkillWatcher wired internal/daemon/components.go:9791-9840 (contract B3, 04-tool-surface leaf 03); ctor self-arms (watcher.go:84 "immediately arms it") so Start is optional and Close uncalled — ccccb25c 2026-11-07.

- **mcp_runtime.go** (2) — WIRED-FILE-AUX → **BASELINE**
  NewMCPRuntime wired executor.go:304,522 (skill MCP lifecycle); ClientTools/HasServers accessors zero callers.

- **source_claude.go** (1) — WIRED-FILE-AUX → **BASELINE**
  NewClaudeSource wired (Claude skills discovery tier, a376b3f3/a4a92c42); NewClaudeSourceWithPath test-injection variant zero callers.

- **lifecycle/evolver_scheduler.go** (1) — WIRED-FILE-AUX → **BASELINE**
  NewEvolverSchedulerWithRunOnStart wired components_wiki.go:212; bare NewEvolverScheduler zero callers (kept as convenience ctor).

- **skill_mcp_tool.go** (10) — UNWIRED-FEATURE → **WIRE-CANDIDATE**
  whole SkillMCPTool (NewSkillMCPTool + 8 methods + stripServerPrefix) zero callers — Cluster H "wire MCP lifecycle, add SkillMCPTool" (11b46804 2026-06-06) never reached a construction site: skill-embedded MCP servers get NO tool exposure today. PLANNED-OPEN.

- **lifecycle/evolver_plan.go** (5) — UNWIRED-FEATURE → **WIRE-CANDIDATE**
  IsEvolverPlan/EvolverPlanAction/EvolverPlanProposalID(+File variants) zero callers — the provenance read-back accessors from leaf 01 (287e1817 evolver plan sink + provenance) were superseded by the StampEvolverPlan write path (evolver.go:725) with no reader. PLANNED-OPEN (verifier gating leaf 04 may need them).

- **adapter.go IsClaudeSkillPath** (1) — UNWIRED-FEATURE → **DELETE-CANDIDATE**
  zero callers (a376b3f3 2026-06-06 Claude-format detection helper). UNCLAIMED.

- **lifecycle/types.go ParseOutcome** (1) — UNWIRED-FEATURE → **DELETE-CANDIDATE**
  zero callers (30a12f11 2026-06-24 skill evolution closed-loop; outcome parsing never wired). UNCLAIMED.

### internal/task — 5 funcs — UNWIRED-FEATURE → **DELETE-CANDIDATE**

checklist system never wired: `GetChecklistTemplate` + 4 template getters zero callers. Produced 3b37b3c7 2026-06-10 "implement checklist system with Ralph Loop integration" — the Ralph Loop consumer was never wired. UNCLAIMED.

### internal/templates — 1 funcs — WIRED-FILE-AUX → **BASELINE**

templates pkg live via RPC (templates.list/get/invoke/clear, cmd/meept/templates.go; 9db7948b prompt-template system). `ParseTemplateMetadataOnly` uncalled helper.

### internal/tools — 3 funcs — MIXED

- **mcp/compression.go + mcp/protocol.go** (2) — WIRED-FILE-AUX → **BASELINE**
- **workdir_ctx.go IsNoWorkingDir** (1) — UNWIRED-FEATURE → **WIRE-CANDIDATE**

tools/mcp compression handler wired `internal/daemon/daemon.go:2007` (`NewCompressionHandler`); `DefaultCompressionConfig` uncalled (daemon passes explicit cfg). protocol.go `Deserialize` uncalled (client.go:385 uses live `NewRequest`). workdir_ctx `IsNoWorkingDir` — documented detection surface (internal/agent/AGENTS.md:57) but ZERO production callers exist: doc-vs-code gap, the TUI/GUI never special-case ErrNoWorkingDir.

### internal/tui — 24 funcs, MIXED

- **components/sparkline.go** (8) — WIRED-FILE-AUX → **BASELINE**
  Sparkline wired sidebar.go:171-173 (queue/workers/agents); MinMaxSparkline + MultiSparkline families (8 funcs) zero callers. 24e15a05 2026-02-25.

- **components/fuzzy.go** (1) — WIRED-FILE-AUX → **BASELINE**
  NewFuzzyMatcher wired modal.go:1138; HighlightMatch zero callers.

- **vim/keymap.go** (3) — WIRED-FILE-AUX → **BASELINE**
  vim pkg wired (models/chat.go:372 vim.NewState); DefaultKeymap/DefaultVimConfig/HelpText zero callers — State does not consume DefaultKeymap.

- **viz/colors.go Current** (1) — WIRED-FILE-AUX → **BASELINE**
  viz pkg wired (app.go:29, sidebar, thread_indicator, cmd/animation); Current() PaletteSnapshot accessor zero callers — agents_panel uses tui palette.Current (different type; name collision noise). 660227b4 2026-08-24 unified theming L2.

- **render/syntax.go** (7) — UNWIRED-FEATURE → **DELETE-CANDIDATE**
  whole SyntaxHighlighter (NewSyntaxHighlighter(WithStyle)/Highlight/getLexer/DetectLanguage/ExtractCodeBlocks/SupportedLanguages) zero callers — the TUI markdown path uses glamour (render/markdown.go:20, chat.go:369). 0e271771 2026-02-20. Superseded implementation.

- **modals/project_prompt.go** (4) — UNWIRED-FEATURE → **WIRE-CANDIDATE**
  ProjectPromptModal (NewProjectPromptModal/Init/Update/View) zero callers — the TUI project-binding confirmation was never surfaced; GUI HAS the equivalent (ui/flutter_ui/lib/dialogs/project_prompt_dialog.dart, providers/session_project_provider.dart) → TUI/GUI parity violation. 7506ee09 2026-07-09 "project prompt modals".

### internal/validator — 5 funcs — UNWIRED-FEATURE → **DELETE-CANDIDATE**

validator pkg never wired: `NewTaskValidator`, `NewStepValidator` (+validateClaim/validateEvidence), `NewFilesystemValidatorWithConfig` zero callers; validator/manager.go itself has no constructor callers. Produced 15e4342f 2026-04-24 "close determinism audit gaps — evidence flow, validators". UNCLAIMED.

### internal/voice — 2 funcs — UNWIRED-FEATURE → **WIRE-CANDIDATE**

local-leg barge-in seam never invoked: `LocalSpliceFor`/`runLocalSplice.HandleInterrupt` appear ONLY in comments (`internal/voice/local.go:44,467`) — the C1-interrupt glue the comments describe ("leaf 07 entry point: the C1 interrupt frame path resolves the live splice by ccid and calls this") has no call site. Local leg itself is live per internal/voice/AGENTS.md; PLANNED-OPEN.

### pkg/constants — 1 funcs — UNWIRED-FEATURE → **DELETE-CANDIDATE**

`DefaultDevAPIKey` returns the legacy hardcoded key; docstring claims backward-compat for "the Swift menubar app that references the constant by name" — rg over menubar/ finds zero references. d0733f3d 2026-07-12 grok-findings remediation (S7-5/S5). UNCLAIMED compat shim.

### pkg/sqlite — 12 funcs — MIXED

- **fts5.go CreateFTS5Table + 10 others** (11) — UNWIRED-FEATURE → **DELETE-CANDIDATE**
- **pool.go DefaultPoolConfig** (1) — WIRED-FILE-AUX → **BASELINE** (Pool wired via sqlite.NewPool: internal/llm/token_cache_l2.go:107, internal/project/store.go:37)

FTS5Query builder + CreateFTS5Table/CreateFTS5Triggers/Highlight/Rank/Snippet zero callers — FTS stores hand-roll their SQL (session SQLiteFTSStore with LIKE-fallback). Produced ~5b5227f3 (2026-05-14 lint sweep). UNCLAIMED helper library.

### pkg/tlsutil — 2 funcs — UNWIRED-FEATURE → **DELETE-CANDIDATE**

`PinningVerifier.VerifyConnection/PinTransport` never constructed — fingerprint pinning shipped via the LoadExpectedFingerprint/SaveFingerprint path instead (`internal/comm/http/security.go:90-92`, transport/client.go:150). a046bc9e 2026-05-31 security hardening; superseded implementation. UNCLAIMED.

### theme — 1 funcs — WIRED-FILE-AUX → **BASELINE**

`theme.Parse` wired at 4 call sites (tui/palette.go:85, tui/viz/colors.go:38, components/palette/bridge.go:26, models/palette_bridge.go:30). `MustParse` unwired variant.


## Verification notes

- All production-wiring citations were grepped live (excludes `*_test.go`, `.hermes/worktrees/`, `docs/`, `plans/`, `*.md`).
- The 4 RTA-IMPRECISION entries each have a named, verified call site above.
- Counts: 160 WIRED-FILE-AUX + 136 UNWIRED-FEATURE + 4 RTA-IMPRECISION = 300 = scope-file line count (verified).
- Doc-vs-code gaps surfaced (not acted on, out of scope): internal/preferences/git_hooks.go (docs claim daemon writes hooks),
  internal/employee/goal_round_driver.go ValidGoalRound fence comment, internal/auth/users.go:87 EffectiveGroups seam comment,
  internal/tools/workdir_ctx.go IsNoWorkingDir detection surface, internal/rpc/groups.go ContextWithCallerIdentity bridge,
  internal/secrets/proxy.go Stop never wired into shutdown.

## Wave-1 disposition (DELETE sweep, 2026-11-09)

Re-grep at HEAD (prod callers only; tests/worktrees excluded) CORRECTED four
rows before any deletion — kept as imprecision, deleted the rest:

- **internal/queue dispatcher.go** — deleted whole file (16 funcs, 2cfbe7b4).
  Zero NewDispatcher callers; daemon uses agent.NewDispatcher
  (components.go:4123). queue pkg wired members still compile+test green.
- **pkg/sqlite FTS5 (11)** — PARTIAL delete (3d510c41). Re-grep found LIVE
  callers: SanitizeQueryOpPrefixes/SanitizeQueryOp/SanitizeQuery
  (memory/task.go:190-192, episodic.go:209-211, knowledge/search.go:57),
  BM25 (knowledge/search.go:70), NormalizeRank (ftstore.go:699,
  knowledge/search.go:83). Deleted only the builder/DDL/HTML half:
  FTS5Query chain API, Highlight, Snippet, Rank, CreateFTS5Table,
  CreateFTS5Triggers. KEPT-IMPRECISION: report's "11 funcs zero callers"
  was wrong for the sanitize/rank family.
- **internal/validator (5)** — PARTIAL delete (34f04c44). Re-grep: manager
  IS wired (components.go:4454 → tactical.go:1833 ValidateStep) — the
  report's "manager.go itself has no constructor callers" was wrong.
  Deleted only task.go (TaskValidator) + interface.go StepValidator
  claim-evidence orchestrator (+ their tests). NewFilesystemValidatorWithConfig
  kept as the config seam of a wired type.
- **internal/session tree.go (3)** — PARTIAL delete (a0a6d22b). Re-grep:
  AssembleBranch + LoadToolCallsForMessages (+ in-file helpers) ARE LIVE
  via branch.go:229-238 (BranchManager wired components.go:3286). Deleted
  only RestoreConversationFromStore + ConvertChatMessagesToSessionMessages +
  ChatMessagesToToolCalls (+ their tests).
- **internal/debug** — NOT DELETED; KEPT-IMPRECISION. Report's "NewClient
  zero callers" was wrong: manager.go:102/204 call it, Manager is wired
  (components.go:9391 → builtin debug tool registry.Register). Whole debug
  pkg stays.
- **internal/shadow classifier.go (5)** — deleted whole file (e84abdaa);
  verified zero prod AND test callers (agent *_classifier_test hits are
  different types). ClassificationResult died with the file; Domain/
  TaskType/Complexity (models.go) stay — live via agent/loop.go:8565 +
  components.go:7735.
- **skills DELETE rows** — IsClaudeSkillPath + lifecycle ParseOutcome
  deleted (ca2512b7). Outcome.String KEPT (live: usage.go:201,
  evolver_wiki.go:152). SkillMCPTool + evolver_plan accessors are Wave-2
  WIRE holds, untouched.
- **prompts/specialists.go** — deleted (28580cf8) + its orphaned
  planner_prompt_pin_test.go (86405206/#53 pin anchored ONLY the deleted
  const; live planner path never consumed it).
- **employee** — drift.go deleted (04ad8507 + test cases 46d4813f);
  handler_auth.go dead HALF deleted (6ad31b9d): requireCaller/
  isAuthorizedForOperation/isAuthorizedAmmender had zero callers, but
  CallerFromContext/ContextWithCaller/resolveCaller/presentedAPIKey/
  isAuthorizedApprover ARE LIVE (handler.go plan approval + api_handlers.go
  injectCallerIntoContext, /api/v1/agents/* wired daemon.go:1654) — KEPT.
- **llm metrics adaptive** — deleted whole chain (a7366653): Calculator +
  client/anthropic timeoutCalc seams + options + 4 nil-guarded call sites
  + tests. Zero construction sites anywhere; Store stays (live).
- **task checklist templates** — deleted (7b523466); NOT plan-claimed (no
  plan references the template funcs; hold-list condition not met). Live
  Checklist/ChecklistItem types stay (ralph_loop.go validateChecklists).
- **preferences git_hooks.go** — deleted (ec72b73c). Doc-vs-code gap:
  docs/workflows/user-instructions.md:136-137 claims the daemon generates
  these hooks — needs a docs correction follow-up.
- **tui/render syntax.go** — deleted whole file (ed15341a); glamour path is
  live. chroma stays as glamour indirect dep.
- **lint treelint.go** — deleted (a81c8a39); registry.go has its own
  private duplicate (live).
- **pkg/tlsutil PinningVerifier + pkg/constants DefaultDevAPIKey** —
  deleted (dc8560e5, amended: defaultDevAPIKeyLegacy const also removed —
  its only reader was the deleted func; menubar keeps its own Swift literal,
  comm/http hardcodes its migration string).

HOLDS honored (untouched): selfimprove report_artifact.go, skills
SkillMCPTool, tui modals/project_prompt.go, internal/runtime docker/harness.

Commits (chronological): 2cfbe7b4 queue, 34f04c44 validator, 3f923667
selfimprove/judged_filter, ca2512b7 skills, e8ad4707→28580cf8 prompts,
04ad8507+46d4813f+6ad31b9d employee, a81c8a39 lint, ed15341a tui/render,
3d510c41 pkg/sqlite, dc8560e5 pkg tlsutil+constants, ec72b73c preferences,
7b523466 task, a0a6d22b session/tree, e84abdaa shadow, a7366653 llm.
Ancillary: 47e586c6 fixes an unrelated agent's broken in-flight
reasoning_strip.go regexp that blocked the llm package gates.

Gates: go build on every touched package green; go test -count=1 green on
all 16 touched packages. Full `go build ./...` currently fails in
internal/daemon (undefined agent.ApplyPreset) — NOT this wave: a sibling
agent's uncommitted deletion of internal/agent/team_presets.go; components.go
consumers need their in-flight commit to land. Test failures in
internal/voice (TestChildProcParentDeath) are likewise outside this wave's
files. e2e manifest.json path_map: no package was emptied, no manifest edit
needed; e2e-affected should still be re-run over the wave's diff.

## Wave-2 disposition (scope: skills SkillMCPTool + TUI project_prompt)

Executed 2026-11-09. Commit 3b3eba0c (TUI project prompt). UNRESOLVED:
SkillMCPTool. Gates: go build ./..., go vet ./internal/tui, go test
-count=1 ./internal/tui + ./internal/skills, gofmt clean on touched files.

| Report row | Disposition | Evidence |
|---|---|---|
| `modals/project_prompt.go` (WIRE-CANDIDATE, TUI/GUI parity) | WIRED | ProjectPromptModal had zero callers while the Flutter GUI has the full flow (ProjectPromptDialog + SessionProjectChecker on session activation). Commit 3b3eba0c: modal made embeddable (Show/Hide/IsVisible + SessionDefaultPath; esc now emits ProjectDismissedMsg instead of tea.Quit — an embedded App modal must not tear down the program); App wiring follows the sibling-modal patterns (ModalProjectPrompt appended last to the enum, Update message cases for OpenProjectPromptMsg/ProjectConfirmedMsg/ProjectPickRequestedMsg/ProjectDismissedMsg, handleModalKey + renderModalOverlay cases, ctrl+g binding). Parity trigger: SessionSwitchMsg runs sessionNeedsProjectPrompt (mirror of the GUI needsProjectPrompt) and emits the prompt for unbound sessions; accept binds via the new RPCClient.SetProjectPath (project.set path param, the GUI accept flow); pick opens the existing projectPicker; decline/dismiss proceed unbound. Update-path pins: project_prompt_app_test.go (open-message activates modal, accept issues project.set, esc dismisses not quits, unbound switch emits the prompt, bound stays silent). |
| `skill_mcp_tool.go` Cluster-H (WIRE-CANDIDATE) | UNRESOLVED-INTENT — STOPPED, untouched | Producing commit 11b46804 (2026-06-06, "Cluster H — wire MCP lifecycle, add SkillMCPTool") marks ALL success criteria [x] COMPLETE in docs/20260606-cluster-h-skill-system-evolution.md and docs/superpowers/plans/2026-06-06-cluster-h-skill-system-evolution.md; the plan names no registration seam. The intent text ("Add server tools to agent's tool surface for this task only") conflicts with every candidate seam: (a) boot-time registration (toolprov provider / registerBuiltinTools) contradicts the per-skill-execution, scoped, ephemeral server lifecycle the same plan mandates; (b) per-execution exposure requires a tool-call loop in skills.Executor's LLM call, which does not exist (Execute makes a bare chatter.Chat with no tool surface). ClientTools() (the documented "callers constructing SkillMCPTool instances" seam) has zero callers — no intended construction site was ever written down. Wiring would require inventing an architectural seam the plan never specified; per the wire-wave rule this is recorded UNRESOLVED-INTENT and left for an owner decision (either retire SkillMCPTool + MCPRuntime.ClientTools as superseded, or author a leaf that adds the skill-execution tool loop). |

## Wave-3 disposition (final small fixes, 2026-11-10)

All misc-long-tail doc-vs-code rows closed, the two deferred code moves
done, plus two pre-existing red gates fixed in passing. Executed per
concern, explicit paths. Gates at close: go build ./... green, go vet
./... clean, go test -count=1 -p 2 green on every touched package
(tui, tui/models, tui/modals, services, employee, auth, tools,
tools/builtin, rpc, secrets, daemon, comm/http, integration);
gofmt clean on touched files.

| Concern | Disposition | Commit |
|---|---|---|
| 1. tui/models/turn.go test helpers in production | MOVED. The whole test-helper export block (TestTurnTerminalMsg/TurnRouter aliases, NewTestTurnRouter*/NewManualTimerSource, TestAwaitTurnCmd, Register*/RegisterWithSession, DeliverTestTerminal/Progress, FireTimers/ArmedTimers) plus the noop/manual timer sources left turn.go for turn_test_helpers_test.go. Enabler: the await-command contract tests moved from package tui (deleted chat_async_test.go) into models/turn_await_test.go — they exercise only router+awaitTurnCmd, and in-package tests let every helper stay unexported (a test file in another package cannot see them, which is what had forced the exported surface into production). SubmitChat wire-format tests stay in package tui. In-package tests updated to unexported names. chat_turn.go gained the missing EOF newline (gofmt). | f9f77f07 |
| 2. ServiceRegistry Start/Stop design tension | DOC. Git intent (c8097250) shows the DESIGN NOTE was itself the fix — the container-not-lifecycle rationale is correct and stays. The tension was zero callers (wave-1 keep row). Added a STATUS note on Start (mirrored by Stop) marking them deliberately-unwired API surface with the real boot/shutdown paths named; no deletion. | 8f7612f3 |
| 3a. git_hooks doc references | DOC. Removed the dead Git Hooks section from docs/workflows/user-instructions.md (generator deleted in ec72b73c; the daemon never wrote hooks). Regenerated docs/generated/llms-readme-full.txt (was stale since 2026-10-15 — picked up six weeks of other doc landings too; only intended delta is the git-hooks removal). | e82603f9 |
| 3b. ValidGoalRound fence comment | DOC (comment fix preferred; grep: zero production callers). The fence doc now says available-but-unused API surface; the reserve-path comment no longer claims the driver calls it. | b97c5c6d |
| 3c. auth/users.go:87 EffectiveGroups comment | DOC (grep: zero production callers; the human-identity join path was never built). User.Groups field comment corrected to documented-but-unwired, field persists to avoid a future store migration. | 250ca97f |
| 3d. tools.IsNoWorkingDir zero consumers | DOC, no 1-line wire exists. The sentinel-detection contract targets CLIENT surfaces; TUI/GUI render the sentinel's message text verbatim and no RPC/comm layer sees raw tool errors to special-case — a classification wire is not 1 line. STATUS note records the contract as available API surface with first-caller guidance. | b3fb33fb |
| 3e. ContextWithCallerIdentity unbuilt bridge | DOC. Code kept; one STATUS note at the seam stating the HTTP bridge was never built and naming the wiring site (HTTP auth middleware after auth.Store.Validate). | 0c5ccb3e |
| 3f. secrets.Proxy.Stop unwired | WIRED. Proxy IS constructed (wireSecretsProxy, [secrets.proxy] enabled). stopComponents now calls SecretsProxy.Stop (nil-guarded on disabled boots) with the same single-teardown-seam posture as wave-2's quota_notifier Cleanup. | 75a2fa50 |
| 3g. DefaultDevAPIKey menubar-compat doc claim | DOC. Canonical docs/reference+workflows had NO stale reference (rg-verified; only historical docs/plans mention it, untouched as history). The one live stale claim was docs/20260618-checkreview.md's G101 note (#if DEBUG-gated constant) — corrected to describe what remains: the legacy migration-detection constant + the menubar's own Swift LegacyPublicDevAPIKey, both compare-only; accessor deleted in dc8560e5. | f743795b |
| (passing fix) tui/modals project-prompt esc pin | TEST FIX, pre-existing red: wave-2 (3b3eba0c) intentionally changed esc from tea.Quit to ProjectDismissedMsg but left the 13e08238-era tea.QuitMsg assertion — ./internal/tui/modals was red before this wave. Pin updated to the committed behavior (dismissal message + modal hidden). | 07345651 |
| (passing fix) integration containment vs Server.Addr removal | TEST FIX, pre-existing red: containment_test.go still called srv.Addr() — the production accessor wave-1 deleted (14e189b0) — so ./internal/integration and go vet ./... were red since then. The wave-1 TestAddr shim is _test.go-invisible cross-package; fixed by pre-picking a free port (net.Listen :0 → Close) and binding the server to it, no accessor re-added. | 80763bc5 |

Notes for the next baseline pass: turn.go's exported test surface is gone
from the production build (re-grep TestAwaitTurnCmd/RegisterWithSession/
DeliverTestTerminal/turnRouter.FireTimers now hits only _test.go files);
llms-readme-full.txt freshness now rides the regenerated 2026-11-10
timestamp.

Adjacent observations (not acted on, out of scope): the secrets proxy
Stop wiring has no dedicated test pin (behavior covered by
internal/secrets proxy tests + the daemon shutdown tests); e2e
manifest.json needed no edit (no package emptied).
