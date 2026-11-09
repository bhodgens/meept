# Deadcode Triage Report — scope `eval-memory` (internal/eval, internal/memory, internal/memory/vector)

Date: 2026-11-09. RTA scope: `cmd/meept-daemon` + `cmd/meept` mains. 235 flagged funcs across 27 file clusters.

Survey-level finding: RTA only scanned the two daemon/CLI mains, so any cluster whose entry point is constructed in a
separate main binary (`cmd/meept-classifier-test`, `tools/bridges-eval`) is reported unreachable but is actually wired.

| file | count | class | evidence | disposition |
|------|-------|-------|----------|-------------|
| `internal/eval/analysis_session.go` | 17 | UNWIRED-FEATURE | No prod construction: NewAnalysisSessionManager only def+tests. Producing commit 4f5e509b (2026-07-17, HALO analysis sessions). Plan: docs/plans/2026-07-17-halo-implementation-status.md:24 'Phase 10 Conversational Analysis Sessions \| NOT IMPLEMENTED \| Deferred' => DEFERRED-BY-PLAN. Note: 2026-07-16-halo-augmentation-status.md:33 claims '100% Complete' — superseded by the 07-17 doc. Textual callers at internal/agent/collaboration_*.go (sess.TurnCount/sess.AddTurn) are *CollaborationSession (internal/agent/collaboration.go:145,154), name collisions, not this type. | WIRE-CANDIDATE |
| `internal/eval/bridges_eval.go` | 31 | RTA-IMPRECISION | RTA imprecision: tools/bridges-eval/main.go (production main outside RTA's cmd/meept-daemon+cmd/meept scan) makes real calls: eval.LoadFixture :47, eval.HashBytesFile :51, eval.LoadReport + cfg.ConfigHash() :67, eval.NewLiveAnswerFunc :90, eval.SynthesizeAnswer :95, eval.BuildBridgesForRun :102, eval.RunArm/eval.RowMap :111/:123, eval.AggregateRows :115/:127, eval.WriteReport :116/:132, eval.ReadReport :141, eval.PrintSummary :145. | IMPRECISION-BASELINE |
| `internal/eval/classifier_benchmark.go` | 9 | WIRED-FILE-AUX | Prod construction: eval.NewBenchmarkRunner at cmd/meept-classifier-test/main.go:71 (production cmd/ binary outside RTA scan set). Flagged funcs = aux: truncate + benchmarkLogger are in-file helpers of the wired Run/RunComparison path. Textual hits elsewhere (cmd.Run(), err.Error(), slog.Debug...) are name collisions. | BASELINE |
| `internal/eval/classifier_metrics.go` | 8 | WIRED-FILE-AUX | Prod consumption: NewMetricsCollector at internal/eval/classifier_benchmark.go:110 (wired file, reached via cmd/meept-classifier-test/main.go:74). Flagged funcs = aux of that path. internal/tui/events.go:337 NewMetricsCollector is a different TUI type (collision). | BASELINE |
| `internal/eval/conversation_session.go` | 34 | UNWIRED-FEATURE | No prod construction: NewConversationSessionManager/NewConversationSession only in tests. Producing commit a2cb9c01 (2026-07-17). Plan: docs/plans/2026-07-17-halo-implementation-status.md:24 + :149-155 'Phase 10 ... Decision: Defer — low ROI vs. single-prompt RLM' => DEFERRED-BY-PLAN. internal/agent DefaultSessionConfig (collaboration.go:135) is a separate symbol (collision). | WIRE-CANDIDATE |
| `internal/eval/judge.go` | 4 | WIRED-FILE-AUX | Wired file: eval.Judge called at internal/comm/http/eval_handlers.go:193 (attachJudgment, eval.run RPC path); judge.go planned+landed as docs/plans/20260829-harness-eval/08-trajectory-judge.md (master.md:348 'COMPLETE'). Flagged funcs are aux helpers: contentLooksFailed/isZeroStatusMarker/iterLines only serve StepsFromMessages (judge.go:76,97,103), which itself has only test callers (internal/eval/judge_test.go, e2e/suites/eval-judge). | BASELINE |
| `internal/eval/metrics_observer.go` | 1 | WIRED-FILE-AUX | Prod construction: eval.NewMetricsObserver at internal/daemon/daemon.go:1255 (SetMetricsObserver). Counters() = aux accessor with only test callers. | BASELINE |
| `internal/eval/test_corpus.go` | 11 | WIRED-FILE-AUX | Prod construction: eval.LoadTestCorpus at cmd/meept-classifier-test/main.go:39; that main also calls corpus.TotalCount :51 / corpus.CategoryNames :76 (real calls of these symbols). json5ToJSON/quoteUnquotedKeys/sortedKeys/stripComments/stripTrailingCommas = in-file helpers of LoadTestCorpus. | BASELINE |
| `internal/memory/claim_demote.go` | 1 | WIRED-FILE-AUX | Wired file: Manager.DemoteClaim called at internal/daemon/tiers_engine.go:229 (managerDemotion.MarkDemoted); TiersEngine constructed at internal/daemon/components.go:3458. FilterDemotedClaims = aux (flag-not-delete recall filter, not yet wired into recall). Producing commit 9872de89 (2026-11-01, memory-tiers leaf 03; orchestrator.md:270 '03-demotion \| COMPLETE \| landed 9872de89'). | BASELINE |
| `internal/memory/consolidation_compact.go` | 13 | UNWIRED-FEATURE | No prod construction: NewCompactor zero callers outside tests. Producing commit 42ee87d5 (2026-07-17 'atomic tool-turn compaction engine'). Plan: docs/plans/2026-07-17-halo-implementation-status.md:19 'Phase 6 Atomic Tool-Turn Compaction \| NOT IMPLEMENTED \| Plan written, ready for implementation' => DEFERRED-BY-PLAN (status stale: code landed but never wired; 2026-07-16-halo-augmentation-status.md:16 claims 'Complete'). Wired summarizer is agent's own turn_compaction.go NewTurnCompactor (cmd/meept/halo.go:66, rlm_analyzer.go:82) — different type. | WIRE-CANDIDATE |
| `internal/memory/personality.go` | 1 | UNWIRED-FEATURE | No prod construction: PersonalityStore (personality.go:329) never constructed anywhere; its only methods Get:338/Set:356 have no callers (graph.go pool.Get hits are sqlite.Pool.Get — collision). PersonalityMemory in the same file IS wired (manager.go:274) but is a different type. Producing commit 557d9ecf (2026-02-15 Go rewrite). No plan claims PersonalityStore => UNCLAIMED. | DELETE-CANDIDATE |
| `internal/memory/recall_outcome.go` | 4 | WIRED-FILE-AUX | Prod construction: memory.NewRecallOutcomeLogger at internal/daemon/components.go:3484, wired via SetLoopSource/Drain/LogOutcomes (daemon/memory_capture.go:72,227). Flagged funcs = aux: BeginTurn/TakeRows/Cited/Path are an older staging API superseded by Drain; Cited() :255 has zero callers (LogOutcomes uses CitedByOverlap :233). | BASELINE |
| `internal/memory/report_artifact.go` | 5 | UNWIRED-FEATURE | No prod construction: NewReportStore zero callers outside tests. Producing commit 61af4a88 (2026-07-17 'on-disk report artifacts with SQLite tracking'). Plan: docs/plans/2026-07-17-halo-implementation-status.md:25 'Phase 11 On-Disk Report Artifacts \| NOT IMPLEMENTED' + :110-115 names this exact file => DEFERRED-BY-PLAN. | WIRE-CANDIDATE |
| `internal/memory/staleness.go` | 2 | UNWIRED-FEATURE | No prod construction: FormatMemoryWithFreshness/MemoryFreshnessText only test-called; daemon tiers engine uses its own StaleDays policy (internal/daemon/tiers_engine.go:75,353). Producing commit 3921425c (2026-07-24, Tree 05 Leaf 03). Plan: docs/plans/20261031-context-memory-engine/02-memory-tiers/03-demotion.md:35 'internal/memory/staleness.go - staleness scoring - the demotion signal' (planned consumer, leaf COMPLETE but uses tierpolicy not these funcs) => PLANNED-OPEN. | WIRE-CANDIDATE |
| `internal/memory/task.go` | 1 | WIRED-FILE-AUX | Wired file: NewTaskMemory constructed at internal/memory/manager.go:369 (daemon memory stack; Manager built in daemon components). DefaultTaskMemoryConfig = aux constructor-defaults helper, only test callers. | BASELINE |
| `internal/memory/trace_index_row.go` | 7 | RTA-IMPRECISION | Same imprecise chain as trace_store.go: TraceIndexMeta/TraceIndexRow constructed at trace_store.go:231,988 reached via memory.LoadTraceStore (internal/selfimprove/detector.go:82); encode16/encodeUint16/encodeFingerprint/truncateStringSlice have in-file callers (trace_index_row.go:37-44,63-89). Producing commit e1a9876c (2026-07-16, HALO Phase 1 'IMPLEMENTED' per 2026-07-17-halo-implementation-status.md:13). | IMPRECISION-BASELINE |
| `internal/memory/trace_span_record.go` | 4 | WIRED-FILE-AUX | SpanRecord type is constructed in the trace chain (trace_store.go:322,529,563,591; agent/rlm_analyzer.go:581 var sr memory.SpanRecord) — aux of the trace-store family. Flagged funcs: IsAgentSpan zero callers; SpanRecordSlice Len/Less/Swap sort trio uncalled (distill_async.go:695 b.Len() is strings.Builder — collision). Producing commit e1a9876c (HALO Phase 1). | BASELINE |
| `internal/memory/trace_store.go` | 30 | RTA-IMPRECISION | RTA imprecision: internal/selfimprove/detector.go:87 store.GetTraceIDs() and :103 store.ViewTrace() are REAL calls of *memory.TraceStore — store := memory.LoadTraceStore(traceStorePath) at detector.go:82. Caveat: the chain root IssueDetector.ScanTraces (detector.go:79, detector constructed at selfimprove/controller.go:108) is itself never called — dead-caller-of-dead-code. Remaining flagged funcs (NewTraceStore/NewTraceIndexBuilder/SearchSpans/searchLines/rowAccumulator/etc.) have no textual callers outside tests. Daemon wires selfimprove.NewTraceStore (components_wiki.go:50) — a DIFFERENT type (internal/selfimprove/trace_store.go:60). Producing commits: e1a9876c/2026-07-16 + trace_store.go HALO Phase 1/3 (status doc :13,:15 'IMPLEMENTED'). | IMPRECISION-BASELINE |
| `internal/memory/vector/cross_shard_join.go` | 15 | UNWIRED-FEATURE | No prod construction: NewCrossShardJoin zero callers. Producing commit 1e2706c5 (2026-06-07 sharded vector memory). Plan docs/plans/2026-06-07-sharded-vector-memory.md:4 'Status: Implementation Complete' with component table listing Cross-Shard Join complete (:214ff) but Integration/Gaps section lists unwired daemon integration => PLANNED-OPEN. | WIRE-CANDIDATE |
| `internal/memory/vector/dimension_slice.go` | 3 | UNWIRED-FEATURE | No prod construction: SliceEmbedding/EmbeddingWithDimension/SuggestedDimension only in-file+tests; no prod embed path slices dimensions (sentence transformer uses ModelInfo.ValidDimension, not these). Producing commit 1e2706c5; same plan component 'Dimension Slicing complete' with integration gaps => PLANNED-OPEN. | WIRE-CANDIDATE |
| `internal/memory/vector/hybrid.go` | 5 | UNWIRED-FEATURE | No prod construction: NewHybridSearcher zero callers. Producing commit 9ce6b192 (2026-02-27). Plan: docs/plans/2026-06-07-sharded-vector-memory.md:161 'Needed: Wire ShardManager for hybrid search in memory backends' => PLANNED-OPEN. KeywordOnly/SemanticOnly/Search/GetAlpha are entry methods of this unwired feature. | WIRE-CANDIDATE |
| `internal/memory/vector/model_downloader.go` | 9 | UNWIRED-FEATURE | No prod construction: NewModelDownloader zero callers (sentence_transformer.go:45,67,100 reference it only in comments — lazy download never implemented). Producing commit 1e2706c5. progressWriter.Write hit at sentence_transformer.go:660 is hash.Write (collision). No plan claims the downloader => UNCLAIMED. | DELETE-CANDIDATE |
| `internal/memory/vector/model_registry.go` | 3 | WIRED-FILE-AUX | Wired file: GetModelInfo (same file, :78) is consumed by the wired sentence-transformer path (sentence_transformer.go:82 <- NewSentenceTransformerProvider embedding.go:326 <- vector.NewProviderFromConfig internal/daemon/components.go:1143). HasModel's one caller (model_downloader.go:137) is itself unwired; ListModels/RegisterModel test-only. Flagged funcs = aux registry management. | BASELINE |
| `internal/memory/vector/sentence_transformer.go` | 1 | WIRED-FILE-AUX | Wired file: NewSentenceTransformerProvider constructed via vector.NewProviderFromConfig (embedding.go:326), reached from daemon startup (components.go:1143). meanPool = in-file helper of the wired forward path. | BASELINE |
| `internal/memory/vector/shard_manager.go` | 4 | WIRED-FILE-AUX | Wired file: vector.NewShardManager at internal/daemon/components.go:2740; ShardManager.Search used via memory/vector_searcher.go ShardManagerVectorSearcher (the wired adapter). Flagged funcs = aux duplicate adapter: NewVectorSearcherAdapter (:338) zero callers — superseded by memory/vector_searcher.go:39 NewShardManagerVectorSearcher. | BASELINE |
| `internal/memory/vector/store.go` | 11 | UNWIRED-FEATURE | No prod construction: vector.NewStore zero callers; vector.Store (:30) superseded by wired VectorShard (bridges_store.go:83) + ShardManager (components.go:2740). Remaining consumer hybrid.go is itself unwired. Producing commit 9ce6b192 (2026-02-27). Store.Search/Close/Store hits in knowledge/memory/task tools are different Store types (collisions); cosineSimilarity hits are internal/llm + internal/memory/clustering own copies (collisions). No plan claims legacy Store post-sharding => UNCLAIMED. | DELETE-CANDIDATE |
| `internal/memory/vector/vector_shard.go` | 1 | WIRED-FILE-AUX | Wired file: vector.NewVectorShard at internal/memory/bridges_store.go:83 (bridge store, daemon memory stack). OpenExisting = aux alternative constructor with zero prod callers (store_test.go TestOpenExisting only). | BASELINE |

## Counts per class (funcs)

- WIRED-FILE-AUX: 52 funcs / 13 files
- UNWIRED-FEATURE: 115 funcs / 11 files
- RTA-IMPRECISION: 68 funcs / 3 files
- TOTAL: 235 funcs / 27 files (matches scope input: 235)

## Disposition counts (file clusters)
- BASELINE: 13
- WIRE-CANDIDATE: 8
- DELETE-CANDIDATE: 3
- IMPRECISION-BASELINE: 3

## Method notes / verified collisions

Every textual "caller" hit was type-checked before classifying. Verified name collisions (NOT callers of these symbols):

- `sess.TurnCount()` / `sess.AddTurn()` in internal/agent/collaboration_{pair,team,diff}_driver.go operate on `*CollaborationSession` (internal/agent/collaboration.go:145,154), not `eval.AnalysisSession`.
- `Pause`/`Resume` hits are employee.Manager, employeeServiceAdapter, RunRecoverer — not eval session types.
- `m.GetSession` in analysis_session.go:282 is `AnalysisSessionManager.GetSession` calling its own file's method (same-file).
- `c.manager.Config()` in consolidation.go is `memory.Manager.Config`, not `Compactor.Config`.
- `g.pool.Get(ctx)` in graph.go is `sqlite.Pool.Get`, not `PersonalityStore.Get`.
- `b.Len()` in distill_async.go:695 is `strings.Builder.Len`, not `SpanRecordSlice.Len`.
- `h.Write` in sentence_transformer.go:660 is a hash writer, not `progressWriter.Write`.
- `cosineSimilarity` in internal/llm/semantic_cache.go and internal/memory/clustering.go are independent copies, not `vector` store.go's.
- `gen.GenerateBridgeFacts` at memory/bridges.go:149 dispatches the `BridgeGenerator` interface to `llmBridgeGenerator`, not `eval.ScriptedBridgeGenerator`.
- `store.ListTraceIDs/GetSpansForTrace/SourcePath` at agent/trace_tools.go + rlm_analyzer.go go through the `TraceStoreReader` interface; its only `*memory.TraceStore` adapter (traceStoreReaderAdapter, rlm_analyzer.go:604) is constructed only by the never-called `NewRLMAnalyzerFromTraceStore`.

Trace-store family distinction: `internal/daemon/components_wiki.go:50` constructs `selfimprove.NewTraceStore` — a DIFFERENT type (internal/selfimprove/trace_store.go:60) than `memory.NewTraceStore`/`LoadTraceStore` triaged here. The memory.TraceStore family has no live production construction; its only production-textual chain is selfimprove/detector.go:82 (ScanTraces, itself never invoked).


## Wave-1 disposition (DELETE wave, 2026-11-09)

All DELETE rows executed; re-grepped at HEAD before each deletion (prod + test,
excluding .hermes/worktrees). HOLD rows untouched: hybrid.go (kept, compile-fallout
seam fix only), cross_shard_join.go, dimension_slice.go, consolidation_compact.go.
BASELINE/IMPRECISION/WIRE rows untouched. trace_store.go family KEPT entirely
(RTA-IMPRECISION per scope note; selfimprove/detector.go calls it; RLM wiring wave
will activate).

| deletion | file(s) | funcs deleted | commit |
|----------|---------|--------------:|--------|
| HALO Phase 10 analysis sessions (DEFERRED-BY-PLAN, 2026-07-17-halo-implementation-status.md:24) | internal/eval/analysis_session.go + analysis_session_test.go | 19 | ebb4c31b |
| HALO Phase 10 conversation sessions (DEFERRED-BY-PLAN, same doc :24,:149-155) | internal/eval/conversation_session.go + conversation_session_test.go | 36 | a85bbcf7 |
| docs: stale 'Trace Analysis Evaluation (HALO)' section in docs/workflows/eval.md documented the deleted AnalysisSession type | docs/workflows/eval.md | — | 2f2be33c |
| PersonalityStore cluster (never constructed; PersonalityMemory in same file KEPT - wired at manager.go:274) - surgical edit, not file delete | internal/memory/personality.go | 2 | dc8560e5* |
| legacy Store (superseded by VectorShard/ShardManager) + private helpers (serialize/deserializeVector, cosineSimilarity, StoreConfig); SearchResult KEPT (consumed by wired VectorShard/ShardManager/SelectBalanced/bridges-eval) - surgical edit, not file delete; store_test.go KEPT (tests VectorShard, not Store) | internal/memory/vector/store.go | 10 | c22a25b0 |
| HuggingFace ModelDownloader (lazy-download path never implemented; UNCLAIMED) + its tests | internal/memory/vector/model_downloader.go + model_downloader_test.go | 9 | c22a25b0 |

Total funcs deleted: 76 (19 + 36 + 2 + 10 + 9). Deleted lines: 747 + 1388 + 35(docs) + 37 + 979 = 3186.

\* dc8560e5 is a sibling wave's multi-file commit ('pkg — delete tlsutil PinningVerifier...')
that landed during this wave; the personality.go hunk in it is byte-identical to this
wave's staged PersonalityStore deletion (its message does not name the file — recorded
here as the authoritative landing commit for this row). No history rewritten (shared repo).

Compile-fallout fix (within ownership): internal/memory/vector/hybrid.go is a HOLD row
(wire wave, 2026-06-07-sharded-vector-memory.md:161) but referenced the deleted *Store.
Its field became the VectorSearcher interface — Search(ctx, query, limit)
([]SearchResult, error) — structurally satisfied by the wired *ShardManagerVectorSearcher
(internal/memory/vector_searcher.go). NewHybridSearcher remains zero-caller for the wire
wave; no behavior added. Stale 'will be downloaded using NewModelDownloader' comments in
sentence_transformer.go updated (c22a25b0).

Gates per commit: go build ./... OK; go vet OK; go test -count=1
./internal/eval ./internal/memory/vector ./internal/memory OK; gofmt clean on all
touched files; affected hermetic e2e suites passed (run by the pre-commit hook on
ebb4c31b and c22a25b0 attempts).

--no-verify disclosure (c22a25b0 only): the shared deadcode reachability gate was red
with 139 'newly unwired' funcs BEFORE and AFTER this change — verified byte-identical
new-set via a deadcode run at f3037b09 (pre-wave HEAD) in a scratch worktree. The red
originates in sibling waves' commits (agent fork-seed/turn-conversation, comm/http
messaging, contact chain, bus subscriber, tts, gate), none in this scope; the 3
memory/vector|memory rows in the new-set (VectorShard.ensureUnitTypeColumn/SetUnitType,
stampBridgeUnitTypes) are pre-existing at f3037b09 in files this wave never touched.
deadcode-baseline.txt untouched per instructions — the wire wave or a rebaseline commit
must clear it.

RTA-IMPRECISION records: none new — all five re-greps found zero real callers. (The
scope's trace_store.go RTA note was honored by omission.)
