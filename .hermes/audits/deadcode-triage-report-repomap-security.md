# Deadcode Triage Report — scope `repomap-security`

**Scope:** internal/repomap, internal/security, internal/security/taint, internal/code/ast — 106 RTA-flagged funcs
**Input:** `.hermes/audits/deadcode-triage-scope-repomap-security.txt`
**Method:** per FILE-CLUSTER wiring verification (production construction sites + repo-wide textual caller scan excluding tests and `.hermes/worktrees/`, with name-collision checks).

## Class totals

| Class | Funcs |
|---|---|
| WIRED-FILE-AUX | 58 |
| UNWIRED-FEATURE | 48 |
| RTA-IMPRECISION | 0 |
| **Total** | **106** |

Disposition split: BASELINE 43 · DELETE-CANDIDATE 63 · WIRE-CANDIDATE 0 · IMPRECISION-BASELINE 0

## Rows

| file | count | class | evidence | disposition |
|---|---|---|---|---|
| internal/repomap/cache.go | 15 | WIRED-FILE-AUX | Package entry wired: `NewRepoMapGenerator` at internal/daemon/components.go:4741 (`RepoMapGen` field :637), production calls agent/loop.go:8283 + agent/orchestrator.go:1287. Generator builds `TagCache`+`MapCache` directly (generator.go:156,163) — the flagged `CacheManager` (10 methods + `NewCacheManager`/`DefaultCacheConfig`, introduced c3de8b05 2026-06-10) and `RenderCache` (6 funcs) have ZERO production construction anywhere; superseded caching layer. | DELETE-CANDIDATE |
| internal/repomap/extractor.go | 5 | WIRED-FILE-AUX | `NewTagExtractor` constructed at generator.go:136; wired method `ExtractTagsWithContext` called generator.go. Flagged `FilterTagsByKind`/`FindDefinitions`/`FindReferences`/`GetDefinitionsByFile`/`GetReferencesByFile` are query helpers with no callers (only comment/self-decl hits). | BASELINE |
| internal/repomap/fitting.go | 9 | WIRED-FILE-AUX | `FitToBudget` wired at generator.go:102; renderer constructed generator.go:145. Flagged `FitToBudgetSimple`/`FitToBudgetWithStrategy`/`fitProportional`/`fitTopN`/`groupByFileRanked`/`TokenBudgetForFiles`/`AdjustBudgetForConfig`/`DefaultFittingConfig`/`DefaultRenderer.Render` uncalled (renderer built via `RendererConfig` literal, not `DefaultRenderer`). | BASELINE |
| internal/repomap/graph.go | 4 | WIRED-FILE-AUX | RepoGraph wired via ComputeRank path (generator.go:94 → pagerank.go:36). Flagged `SortRankedTags` + `unexportedRankedTags` Len/Less/Swap sort.Interface never invoked (zero `sort.Sort(unexportedRankedTags…)` sites). | BASELINE |
| internal/repomap/pagerank.go | 9 | WIRED-FILE-AUX | `ComputeRank` wired at generator.go:94. Flagged `TopTags`/`FilterByScore`/`GetTagsByFile`/`CombinedPersonalization`/`NormalizePersonalization`/`InversePageRank`/`PageRankWithTeleportation`/`ComputeRankDetailed`/`EccentricityCentrality` are auxiliary API surface, no callers. | BASELINE |
| internal/repomap/renderer.go | 1 | WIRED-FILE-AUX | `NewContextRenderer` constructed at generator.go:145 (wired). Flagged `DefaultRendererConfig` unused — generator builds `RendererConfig` literal (generator.go:~140). | BASELINE |
| internal/code/ast/languages.go | 3 | WIRED-FILE-AUX | Package heavily wired: `ast.NewParserManager` at components.go:9226, repomap/extractor.go:47, repomap/renderer.go:69, project/init_deep.go:79. Flagged `GetLanguageInfo`/`IsSupported`/`SupportedLanguages` have zero callers incl. in-package (introduced 672dcb2d 2026-03-01). | BASELINE |
| internal/code/ast/treectx.go | 11 | UNWIRED-FEATURE | Entire file is an island: `TreeContextWithMarkers` called only by `TreeContextWithMarkersSimple` (also flagged); `DefaultTreeContextOptions` called only within the island (treectx.go:83); agent/reflection.go:429 is a comment, not a call. Zero production callers repo-wide. Producing commit c3de8b05 (2026-06-10). Plan citation: docs/plans/20260609-repomap-pagerank-implementation.md:360-361 planned `ast.TreeContext` integration for repomap rendering and docs/plans/20260609-auto-lint-test-reflection-implementation.md planned it for reflection — both landed WITHOUT it (renderer uses `groupByFileRankedTags` path) → PLANNED-OPEN. | DELETE-CANDIDATE |
| internal/security/agent_boundary.go | 1 | WIRED-FILE-AUX | `InterAgentBoundary` wired: components.go:7692 (`newCollabHandoffBoundary`) → agent.NewHandoffBoundary (components.go:7707) → collaboration drivers at components.go:4567/4575/4583; wired method `InspectHandoffContent` called at collaboration_handoff.go:111. Flagged `InspectHandoff` (composed wrapper) has zero callers — the drivers call the Content variant directly. Plan: docs/plans/20261031-agent-boundary-hardening/01-inter-agent-taint/ (wired leaf 02, commit 1c573f80). | BASELINE |
| internal/security/audit.go | 12 | UNWIRED-FEATURE | `NewAuditLog` has ZERO production construction (only audit_test.go); every `AuditLog.*` method uncalled; security Engine has no AuditLog field (engine.go verified). Distinct from the WIRED `internal/auditlog` tamper-evident chain. Producing commit 557d9ecf (2026-02-15, Go rewrite phases 4-11). No plan claims it (tamper-evident-audit-log plan covers auditlog, not this) → UNCLAIMED. | DELETE-CANDIDATE |
| internal/security/injection.go | 2 | UNWIRED-FEATURE | `CheckBashInjections`/`MaxBashInjectionRisk`: zero callers repo-wide (only self-decls). No constructor; free functions never invoked by Engine/Orchestrator/fence. Producing commit 3921425c (2026-07-24, Tree 05 Leaf 03). No plan section claims wiring of these → UNCLAIMED. | DELETE-CANDIDATE |
| internal/security/instruction_validator.go | 5 | UNWIRED-FEATURE | `NewInstructionValidator` zero production construction; `InstructionValidator.Validate` never called outside the file; all flagged methods (`IsHighRiskCommand`, `IsKnownSafeCommand`, `assessRisk`, `assessShellRisk`) uncalled (preferences/verifier.go has its OWN assessRisk — name collision, different package). Producing commit 65d96a59 (2026-06-22). Plan citation: docs/plans/user-instructions-verification-2026-06-23.md → PLANNED-OPEN (validator shipped but never wired into the instruction path). | DELETE-CANDIDATE |
| internal/security/orchestrator.go | 1 | WIRED-FILE-AUX | Orchestrator wired: components.go:1124 + createSecurityOrchestrator (components.go:7605→7617); Engine wired at components.go:3217. Flagged `DefaultOrchestratorConfig` unused — wiring builds `OrchestratorConfig` literal at :7606. | BASELINE |
| internal/security/prompt_guard.go | 3 | WIRED-FILE-AUX | `PromptGuard` wired via `NewPromptGuard` (orchestrator.go:93), used at orchestrator.go:190/429/437/449. Flagged free functions `IsWithinBoundary`/`ExtractUserInput`/`ExtractToolOutput` have zero callers (referenced only in comments, prompt_guard.go:106/116, orchestrator.go:442). | BASELINE |
| internal/security/seed_rules.go | 1 | WIRED-FILE-AUX | `SeedRules` wired at engine.go:243 (engine constructed components.go:3217). Flagged `CountRules` has zero callers. | BASELINE |
| internal/security/taint/parallel.go | 1 | WIRED-FILE-AUX | `ParallelTaintTracker` wired: `NewParallelTaintTracker` at agent/executor.go:1746 (`trackCombinedTaint`); `RecordTaint`/`GetCombinedTaint` used. Flagged `IsTainted` has zero callers. Plan: docs/plans/2026-07-10-parallel-tool-execution-enhancement.md (commit d353647b). | BASELINE |
| internal/security/taint/patterns.go | 15 | UNWIRED-FEATURE | Entire file island: `NewPatternMatcher`/`PatternMatcher` zero production construction; all detection funcs (`DetectSuspiciousPatterns`, `DetectExfiltration`, taint-`DetectInjection`, `ScoreSuspiciousness`, `SanitizeShellCommand`, `ContainsSecret`, `ExtractURLs`, `HasURL`, `EstimateEntropy`, `IsHighEntropy`, `AddPattern`, `RemovePattern`, `MatchAll`, `detectContextualInjection`) have zero callers — no hits in wired taint.go/segments.go. NOT a collision with the wired `PromptGuard.DetectInjection` (different type). Producing commit 9ce6b192 (2026-02-27). Plan citation: docs/plans/2026-06-11-security-tooling-wiring.md:84-85 lists PatternMatcher as the injection/exfiltration layer to wire; wiring landed for tracker+sinks but PatternMatcher was superseded by PromptGuard.DetectInjection → PLANNED-OPEN. | DELETE-CANDIDATE |
| internal/security/taint/tracker.go | 5 | WIRED-FILE-AUX | `ExtendedTracker` wired: `NewExtendedTracker` at components.go:7634/:7650, `TaintTracker = taint.ExtendedTracker` alias orchestrator.go:69, fed to HandoffBoundary (components.go:4567+). Flagged `DeepCopy` called only by flagged `DeclassifyWithReason` (tracker.go:258, island-internal); `SanitizableTracker` (`NewSanitizableTracker`/`Sanitize`/`DeclassifyWithReason`/`FilterValues`) never constructed — `.Sanitize(` textual hits are the DIFFERENT wired `InputSanitizer`/MCP Sanitizer (collision-checked). | BASELINE |
| internal/security/tls.go | 3 | UNWIRED-FEATURE | `DefaultTLSConfig`/`ServerTLSConfig`/`ClientTLSConfig`: zero callers, zero constructions repo-wide (grep `security.ClientTLS|security.Server|security.TLSConfig` empty). Whole file present since Go migration 63799185 (2026-02-15); no plan claims TLS pinning via this file (TLS pin verification uses scripts/verify-gui-connect.sh against live config instead) → UNCLAIMED. | DELETE-CANDIDATE |

## RTA-IMPRECISION notes

Zero confirmed imprecision. A naive name-level caller scan surfaced 18 funcs with textual "callers" (countLines, getLine, Stats, Clear, Get, Size, Render, Len/Less/Swap, GetStats, FindReferences, Sanitize, DetectInjection, assessRisk/assessShellRisk, IsSupported, SupportedLanguages, InspectHandoff); every one was verified a **name collision**: same-named unexported funcs in other packages (git_validate.go countLines), different receivers (promptGuard.DetectInjection vs taint.DetectInjection; lipgloss .Render; LSP Client.FindReferences; queue .Stats), or substring matches (`rankTargetLine` ⊃ getLine). None survive qualified-call inspection, consistent with the RTA verdicts.

## Verification

- Flag-func count parsed from input: 106 (regex `(\S+) unreachable func: (\S+)`), per-file sum re-checked: repomap 43 + code/ast 14 + security(+taint) 49 = 106.
- Class counts: 58 + 48 + 0 = 106. Disposition counts: 43 + 63 = 106.
- Key wiring cites re-verified in source: components.go:4741/:637/:1124/:3217/:7605/:7617/:7692/:7707/:4567; generator.go:94/:102/:136/:145/:156/:163; loop.go:8283; orchestrator.go:93/:1287; collaboration_handoff.go:111; executor.go:1746; engine.go:243.


## Wave-1 disposition (executed by parent after user approved; scope fixer was blocked on a denied combined command)

| file | funcs | action | commit |
|---|---|---|---|
| internal/repomap/cache.go | 15 | TRIMMED: CacheManager+NewCacheManager+RenderCache+methods deleted; wired CacheConfig/TagCache/MapCache/CachedMap/getFileMtime/DefaultCacheConfig/ValidateCacheConfig kept; wired tests kept (TestGetFileMtime recovered) | 6762e4d6 |
| internal/security/audit.go + test | 12 | DELETED (unwired duplicate of wired internal/auditlog) | a62b78f1 |
| internal/security/injection.go | 2 | DELETED (zero callers repo-wide incl. fuzz test) | a62b78f1 |
| internal/security/instruction_validator.go + test | 5 | DELETED (plan 2026-06-23 verified-complete, no pending wiring claim) | a62b78f1 |
| internal/security/tls.go | 3 | DELETED (zero qualified callers) | a62b78f1 |

Total deleted: 37 funcs. HOLD rows untouched: internal/code/ast/treectx.go,
internal/security/taint/patterns.go (owner review). Gates at close: go build
./... green, vet green, repomap + security suites green.
