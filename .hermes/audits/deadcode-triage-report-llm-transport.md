# Deadcode Triage Report — scope `llm-transport`

**Date:** 2026-11-09 · **Scope:** internal/llm, internal/transport, internal/sharedclient · **Flagged funcs:** 142 (from `.hermes/audits/deadcode-triage-scope-llm-transport.txt`)
**Method:** per file-cluster — production constructor grep over `cmd/`, `internal/` (tests and `.hermes/worktrees/` excluded; worktrees are untracked), receiver/name-qualified caller verification to eliminate collisions, intra-file reference scan, `git log --follow` producer attribution, `docs/plans/` citation grep.

## Class counts (verified)

| Class | Funcs |
|---|---|
| WIRED-FILE-AUX | 42 |
| UNWIRED-FEATURE | 89 |
| RTA-IMPRECISION | 11 |
| **Total** | **142** ✓ (40 aux files + 2 aux from mixed prompt.go, 13+16+8+2+45+5 unwired, 1+3+7 imprecision) |

## Rows per file-cluster

| File | Count | Class | Evidence | Disposition |
|---|---|---|---|---|
| internal/llm/broker.go | 13 | UNWIRED-FEATURE | `NewModelBroker` has **zero** non-test callers (6 test call sites only). Producer a6637678 (2026-04-04, "agent robustness framework phases 1-6"). Plan citation: docs/plans/quota-reset-resilience/04-direct-call-wait.md:163-167 — "**the broker is dead in production**"; production path is Resolver + turn parking. Intra-cluster refs only (newChatterFor@broker.go:85,99; isRetryableError@189,249). | DELETE-CANDIDATE |
| internal/llm/model_picker.go | 16 | UNWIRED-FEATURE | `NewModelPicker`/`RunModelPicker` zero callers anywhere (prod or test). Producer 35995a6e (2026-04-22, "meept models CLI command") — that CLI no longer exists (`cmd/meept/model.go` is the GGUF store CLI; analytics `models` is unrelated). No docs/plans/ claim — **UNCLAIMED** (docs/workflows/models-cli.md:69,96 still advertises a "TUI Model Picker", stale doc). | DELETE-CANDIDATE (+ stale models-cli.md fix) |
| internal/llm/task_summarizer.go | 8 | UNWIRED-FEATURE | `NewTaskSummarizer` zero prod callers (16 test sites); Summarize* zero callers anywhere. Producer acddcb54 (2026-07-12, "add task summarization capability"). Plan citation: docs/plans/20261007-reasoning-history-strip/01-strip-export.md owns the file for "regex move only" — **UNCLAIMED** as a feature. `StripThinking` already lives exported in reasoning_strip.go:27 (used agent/conversation.go:490); the private copy here is residual. extractTitle/parseHandoffSections/serializeMessages are intra-cluster only. | DELETE-CANDIDATE |
| internal/llm/resolver_direct.go | 5 | UNWIRED-FEATURE | `newQuotaWaitChatter` sole caller is broker.go:472 (unwired broker). Producer 49640b9d/bc8afada (2026-08-31, quota-reset-resilience leaves 01-07,10). Plan citation: docs/plans/quota-reset-resilience/04-direct-call-wait.md:163-167 — documented deviation: decorator "wired only via ModelBroker.ChatterForModel… **remains for the broker/test path**" — **DEFERRED-BY-PLAN**. | BASELINE (plan-protected; only deletable together with broker.go) |
| internal/transport/sdk_client.go | 45 | UNWIRED-FEATURE | `NewSDKClient` zero callers anywhere. **Supersession verified:** `transport.New` (internal/transport/client.go:118-139) dispatches only `"http"` → NewHTTPClient (:134) and `"rpc"/"unix"/"socket"` → NewRPCClient (:136) — **no `"sdk"` case**. Both consumers use it: cmd/meept/main.go:304 and cmd/meept-lite/main.go:47 (`transport.New(cfg)`, default Transport `"rpc"` per DefaultConfig client.go:109). Sole importer of the `sdk/go` module (go.mod:13,156 replace). Producer b4b7725b (2026-06-16, "OpenAPI SDK integration for Go TUI"); plans docs/plans/openapi-sdk-migration.md, openapi-sdk-migration-complete.md, openapi-sdk-cleanup.md (cleanup Task 1 wanted the *legacy* client deleted in favor of SDK — the reverse happened). callAPI's 38 intra-file refs are intra-cluster. | DELETE-CANDIDATE (whole file + sdk/go module decision) |
| internal/sharedclient/claude_commands.go | 2 | UNWIRED-FEATURE | `discoverClaudeCommands` zero callers; `claudeCommandsPath` self-refs only. Claude-compat discovery was folded inline into slash.go DiscoverCustomCommands (slash.go:280-281 loads `~/.claude/commands` via loadCommandsFromDir directly). Producer cc76a076 (2026-06-18, "Claude compatibility and /skill"); last touched by deadcode sweep 9edc4c85. Residual duplicate — **UNCLAIMED** remainder. | DELETE-CANDIDATE |
| internal/llm/anthropic.go | 1 | RTA-IMPRECISION | `isAnthropicRoute` has a verified textual caller: internal/llm/broker.go:114 (real call, not a collision) — transitively dead only because broker itself is unwired. anthropic.go is wired: NewAnthropicClient at broker.go:133, provider_manager.go:450, internal/skills/executor.go:31. | IMPRECISION-BASELINE (dies with broker.go) |
| internal/sharedclient/prompt.go | 5 | RTA-IMPRECISION (3) + WIRED-FILE-AUX (2) | File wired via **cmd/meept-lite** (a separate binary — likely outside the RTA binary roots): `NewPromptRenderer` cmd/meept-lite/tui.go:69, `Render` tui.go:767, `RenderInput` tui.go:771 → 3 funcs IMPRECISION. `Height` (def:30) and `SessionName` (def:25, getter; callers use SetSessionName) have zero callers → 2 AUX. | IMPRECISION-BASELINE ×3; BASELINE ×2 |
| internal/sharedclient/slash.go | 7 | RTA-IMPRECISION | Verified textual callers, not collisions: `GetCustomCommand` internal/comm/http/api_templates.go:39, `RenderTemplate` :46, `DiscoverCustomCommands` :61. **Caveat:** that TemplatesService (HandleInvoke/HandleList, api_templates.go:26,54) is **never registered** — the live routes are server.go:1780-1782 → `services.Templates` (different service, internal/services/templates_service.go, built service.go:153). So these callers are transitively dead; `IsCustomCommand`/`CustomCommandNames`/`loadCommandsFromDir`/`parseCommandFile` have no live callers (cache populated only by the dead Discover path or tests). Core slash parsing (ParseSlash/BuiltinCommands) IS wired (internal/tui/slash.go re-export). Custom-command discovery is an unwired sub-feature: **UNCLAIMED** end-to-end. | IMPRECISION-BASELINE (reclassify DELETE after removing unrouted api_templates.go) |
| internal/llm/client.go | 5 | WIRED-FILE-AUX | File wired: `llm.NewClient` internal/daemon/components.go:1250, internal/agent/loop.go:5079, cmd/meept-classifier-test/main.go:53. Flagged = aux: `parseResponse` (superseded by parseResponseWithTools, internal/llm/AGENTS.md:34), EffectiveTraceID:1596, PriorityOf:1809, RawGrammarOf:1854, ToolChoiceOf:1841 — zero prod callers, no collisions. | BASELINE |
| internal/llm/context_compactor.go | 3 | WIRED-FILE-AUX | File wired: NewContextCompactor internal/agent/loop.go:2863. DefaultCompactorConfig test-only; isSplitTurn:481, getCompactionPrompt:708 zero prod callers. | BASELINE |
| internal/llm/context_discovery.go | 1 | WIRED-FILE-AUX | File wired: NewContextDiscovery internal/daemon/components.go:1508 (opt-in gate `cfg.LLM.ContextDiscovery.Enabled` — the config field, checked :1507, not the method). `Enabled()` method def:101 zero callers. | BASELINE |
| internal/llm/credentials.go | 1 | WIRED-FILE-AUX | File wired: NewCredentialStore internal/services/model_service.go:50 (`.Get` used :112,226). `List` def:126 zero callers. | BASELINE |
| internal/llm/errors.go | 5 | WIRED-FILE-AUX | File wired (chat-path error classification). `IsRateLimitErrorMessage` explicitly deprecated — internal/agent/tactical.go:2900-2906 SA1019 suppression comment calls errcls.IsRateLimitErrorMessage instead; AsRateLimitError/AsThrottleGiveUpError/BackoffWithJitter/ClassificationUserGuidance zero prod callers. | BASELINE |
| internal/llm/errors_refusal.go | 1 | WIRED-FILE-AUX | File wired (RefusalError on all parse sites, producer 25213f35). AttachUsageToRefusal:154 zero prod callers. | BASELINE |
| internal/llm/errors_tier.go | 2 | WIRED-FILE-AUX | File wired: TierFloorError constructed tier_policy.go:159; e2e pins errors.Is(ErrTierFloor). AsTierFloorError:82/IsTierFloorError:72 zero prod callers — call sites use the ErrTierFloor sentinel directly. Producer 21b05e71 (llm-serving-tiers leaf 01, issue 82). | BASELINE |
| internal/llm/lfm_loader.go | 1 | WIRED-FILE-AUX | File wired: NewLFMLoader internal/daemon/components.go:2109. hasAdapterArtifacts:322 zero callers (diagnostic; wraps applyRuntimeLoRA). | BASELINE |
| internal/llm/models_catalog.go | 2 | WIRED-FILE-AUX | File wired: CatalogSnapshot() called internal/llm/pricing_sync.go:139. GetAllCatalogModels:279 zero callers; GetModelsForProvider:255 sole textual caller is unwired model_picker.go:128 (transitively dead). | BASELINE (GetModelsForProvider dies with model_picker.go) |
| internal/llm/modelstore.go | 1 | WIRED-FILE-AUX | File wired: OpenModelStore cmd/meept/model.go:43. OpenModelStoreForTesting:53 is the named injectable test seam (zero prod callers, used by tests). | BASELINE |
| internal/llm/pacing.go | 1 | WIRED-FILE-AUX | File wired: SetPacer(llm.NewAdaptivePacer…) internal/daemon/components.go:1286. `interval()`:251 is a documented "test seam and diagnostics" accessor (zero callers; the `st.interval` field is separate and live). | BASELINE |
| internal/llm/provider_registry.go | 3 | WIRED-FILE-AUX | File wired: GetProviderByID used internal/daemon/components.go:1521,1531. GetProviderByEnvVar:269, ListProviders:279, ProvidersFromConfig:298 zero prod callers. | BASELINE |
| internal/llm/reasoning.go | 1 | WIRED-FILE-AUX | File wired: AgentReasoningConfig in internal/agent/loop.go:1083, spec.go:58, internal/agents/models.go:279. ResolveReasoning:231 (precedence-chain helper) zero prod callers — precedence resolved elsewhere. | BASELINE |
| internal/llm/runtime_config.go | 5 | WIRED-FILE-AUX | File wired: RuntimeConfig/RuntimeLifecycleConfig in cmd/meept/runtime.go:483, cmd/meept/doctor.go:398, internal/signal/supervisor.go:120, internal/voice/childproc.go:371. Flagged = aux helpers: SupportedRuntimes:454, IsSupportedRuntime:459, ParseRuntimeType:613, FormatPIDFilePath:630, ContainsSupportedRuntime:643 — zero prod callers. | BASELINE |
| internal/llm/runtime_sweep.go | 1 | WIRED-FILE-AUX | File wired: `SweepStaleSpawnRecordsWithAges` internal/daemon/orphan.go:89,110. Plain `SweepStaleSpawnRecords`:1001 superseded by the -WithAges variant, zero prod callers. | BASELINE |
| internal/llm/slot_gate.go | 2 | WIRED-FILE-AUX | File wired: newSlotGate internal/llm/client.go:599,620. heldCount:170, waiterCount:178 zero callers (diagnostics). Producer b125e22b (tree 04 leaf 03). | BASELINE |
| internal/llm/teardown.go | 2 | WIRED-FILE-AUX | File wired: NewTeardownCoordinator internal/daemon/components.go:1498; TeardownCoordinator.OnSessionEvent called components.go:772,777 (live path). Flagged are the two unused adapter shims: `CoordinatorHook`/`TeardownHookFunc` have **zero constructions** anywhere (daemon uses its own teardownStoreHook, components.go:745, registered :3102). | BASELINE (shims deletable) |
| internal/llm/token_cache.go | 1 | WIRED-FILE-AUX | File wired: NewTokenCacheCoordinatorWithMetrics internal/daemon/components.go:1221. Non-metrics `NewTokenCacheCoordinator`:437 zero prod callers (5 test sites). | BASELINE |
| internal/llm/content_parts.go | 1 | WIRED-FILE-AUX | Type wired (llm.ContentPart across internal/agent — budget_resume.go:31, dispatcher.go:951, loop.go:3102, …). HasImageParts:69 zero callers. | BASELINE |
| internal/transport/http_client.go | 1 | WIRED-FILE-AUX | File wired: NewHTTPClient internal/transport/client.go:134 (the live `"http"` transport). buildTLSConfig:145 is documented "Exposed primarily for tests" — zero prod callers; prod path goes through applyPinning (:116). | BASELINE |

## Disposition totals (funcs)

| Disposition | Funcs |
|---|---|
| BASELINE (incl. IMPRECISION-BASELINE) | 53 (41 aux + 11 imprecision + 5 resolver_direct plan-protected... see note) |
| DELETE-CANDIDATE | 84 (broker 13, model_picker 16, task_summarizer 8, claude_commands 2, sdk_client 45) |
| BASELINE (unwired-but-plan-protected) | 5 (resolver_direct quotaWaitChatter) |

Note: 42 (WIRED-FILE-AUX) + 11 (RTA-IMPRECISION) = 53 sit in files that are production-wired → BASELINE/IMPRECISION-BASELINE. 89 UNWIRED funcs split 84 DELETE-CANDIDATE / 5 BASELINE (resolver_direct is DEFERRED-BY-PLAN: quota-reset-resilience master.md + 04-direct-call-wait.md explicitly retain the decorator for the broker/test path).

## Key findings

1. **sdk_client.go (45 funcs) is fully superseded.** The OpenAPI SDK client was never wired into `transport.New`; cmd/meept and cmd/meept-lite both construct via `transport.New` → `rpcAdapter`/`httpClient`. The sdk/go module's only Go importer is sdk_client.go itself. The cleanup plan's direction (delete legacy, keep SDK) was inverted in practice. Deleting sdk_client.go makes the `sdk/go` replace directive orphaned (separate decision).
2. **broker.go (13) + resolver_direct.go (5) are coupled.** The broker is documented dead in production (quota-reset-resilience plan); the quotaWaitChatter decorator is plan-protected only as the broker/test path. Delete together or keep together.
3. **model_picker.go (16) and task_summarizer.go (8) are UNCLAIMED** — no docs/plans/ entry claims their wiring; their producing CLIs/features were removed or never wired.
4. **RTA imprecision is concentrated in internal/sharedclient (8 of 11)** — `cmd/meept-lite` is a separate binary; funcs used only from it (PromptRenderer.Render family) read as unreachable unless lite is an RTA root. The remaining 3 (anthropic.go isAnthropicRoute, slash.go api_templates-backed trio) have textual callers that are transitively dead (unwired broker / unrouted HTTP handlers).
5. **Adjacent finding (out of scope):** internal/comm/http/api_templates.go's TemplatesService handlers (HandleInvoke/HandleList) are constructed nowhere and registered nowhere — the live templates routes call `services.Templates`. That file is itself dead code.

## Wave-1 disposition (2026-11-09, delete wave, scope internal/llm + internal/transport)

Mandatory HEAD re-grep performed before every deletion; all report rows re-verified.

| Row | Action | Evidence at HEAD |
|---|---|---|
| internal/transport/sdk_client.go (45 funcs) | DELETED (c92203c7) | `NewSDKClient` zero callers; `transport.New` (client.go:118-139) dispatches only http/rpc — confirmed. Sole Go importer of `sdk/go` — confirmed. **go.mod require `github.com/caimlas/meept/sdk/go` removed via `go mod tidy`** (in the same commit); the `replace ./sdk/go` line and the sdk/ tree remain on disk — module deletion deferred (openapi-sdk-* plan docs still claim it). |
| internal/llm/broker.go + broker_routing_test.go (13 + 2) | DELETED (6b7d1177, 6591b358) | `NewModelBroker`/`ChatterForModel` zero non-test callers; plan 04-direct-call-wait.md:163-167 declares broker dead. Note: a concurrent process restored broker.go + the interface.go assertion between the first `git rm` and its commit — re-deleted in 6591b358; assertion cleanup 2c7b2e71, stale comment b2177a98. |
| internal/llm/model_picker.go (16) | DELETED (f88a2152) | UNCLAIMED, zero callers incl. tests — confirmed. Stale models-cli.md picker section removed in same wave (697552e3). |
| internal/llm/task_summarizer.go + tests (8) | DELETED (f88a2152) | UNCLAIMED confirmed; three stale comment references updated (2cfbe7b4). |
| internal/llm/resolver_direct.go | KEPT (BASELINE/HOLD) | quotaWaitChatter orphaned as expected — but the same file hosts LIVE `QuotaWaitConfig` + `ConfigFromSchema` (resolver.go:206 SetQuotaConfig; daemon components.go:1335). File stays whole. |
| internal/llm/anthropic.go isAnthropicRoute (RTA) | RESOLVED, not imprecision | its only caller WAS broker.go:114 — died with the broker; function itself remains wired-tested (broker_routing_test.go moved to anthropic_test.go coverage). |
| internal/sharedclient/* | UNTOUCHED (HOLD, meept-lite imprecision) | per instructions. |

Test-only dependents updated: TestBroker_InjectsMetricsAndTimeout (anthropic_test.go), 5× TestChatterForModelQuota_* (resolver_direct_test.go), TestModelBroker_ChatterForModel_Empty + type-assertion repoint (context_compactor_config_test.go), TestIsRetryableError_OverflowFalse (errors_overflow_test.go — helper lived only in broker.go), interface.go `_ Chatter = (*ModelBroker)(nil)` assertion.

**Gate results (own):** `go build ./...` green for scope packages (`go build ./internal/llm/... ./internal/transport/...` OK); `go test -count=1 ./internal/llm ./internal/transport` ok (llm ~40s, transport ~0.7s). Full-tree `go build ./...` transiently red from SIBLING waves mid-run (daemon/components.go referencing deleted agent.ApplyPreset — agent-wave concern, not llm/transport; services/vet churn from comm-http fixer) — resolved to green for scope by end of wave; full-tree state at wave close owned by the orchestrator. `go mod tidy` run: only removes the dead sdk/go require (chroma/pflag churn observed mid-run came from sibling edits and was left alone).

**Imprecision records:**
- resolver_direct.go "UNWIRED-FEATURE (5)" is an undercount: the file also carries the live QuotaWaitConfig translation seam — future sweeps must treat the whole file as wired.
- broker_routing_test.go was misfiled under the broker cluster; it tests isAnthropicRoute/URL routing (live anthropic.go code) and was preserved in anthropic_test.go rather than deleted with the broker.
- Commits used --no-verify (shared-tree hooks run sibling waves' staged WIP; own gates run explicitly and green, disclosed per protocol).

Commit list (concern order): c92203c7 (sdk), 6b7d1177+6591b358+2c7b2e71+b2177a98 (broker), f88a2152+697552e3+2cfbe7b4 (picker/summarizer), + generated-docs regen commit.
