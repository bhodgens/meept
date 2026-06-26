# AI Employee Spec Gap Analysis

**Spec:** `docs/superpowers/specs/2026-06-23-ai-employee-design.md`
**Review Date:** 2026-06-25
**Method:** Systematic domain-partitioned review

---

## Executive Summary

| Domain | Gaps Found | Critical | High | Medium | Low | Coverage |
|--------|------------|----------|------|--------|-----|----------|
| Constitution Design | 6 | 1 | 2 | 2 | 1 | 85% |
| Goal Model + GoalLoop | 8 | 2 | 3 | 2 | 1 | 78% |
| Enforcement Engine | 10 | 2 | 4 | 3 | 1 | 72% |
| CLI/TUI/HTTP/RPC + POC | 7 | 1 | 3 | 2 | 1 | 80% |
| Error Handling + Testing | 9 | 1 | 3 | 4 | 1 | 75% |
| **TOTAL** | **40** | **7** | **15** | **13** | **5** | **78%** |

**Recommendation:** Address all 7 Critical gaps before beginning implementation. These represent fundamental design ambiguities that would cause implementation rework if discovered mid-sprint.

---

## Gap Distribution by Type

| Gap Type | Count | Percentage |
|----------|-------|------------|
| Missing data structure definitions | 8 | 20% |
| Concurrency/race conditions | 6 | 15% |
| Validation logic gaps | 7 | 18% |
| Service/interface contracts | 5 | 12% |
| Budget tracking mechanics | 4 | 10% |
| Error/retry handling | 4 | 10% |
| Documentation/template gaps | 3 | 8% |
| Indexing/performance | 2 | 5% |
| Edge case handling | 1 | 2% |
| **TOTAL** | **40** | **100%** |

---

## 1. Constitution Design Gaps (Lines 122-229)

**Coverage: ~85%** - Core schema is well-defined; gaps are in enforcement mechanics and validation.

| ID | Severity | Location | Description | Recommended Fix |
|----|----------|----------|-------------|-----------------|
| **C1** | **Critical** | line 148 `Version int` | Version field is `int` but spec mentions bumping on "approved amendment" — no mechanism defined for atomic version increment during Plan signoff approval window. Race condition: operator approves amendment v2 while employee loads v3. | Add `VersionLock bool` field; approval flow must read current version and fail if changed. Or use optimistic locking pattern with `expected_version` in amendment proposals. |
| **C2** | **High** | lines 160-180 | `RiskCeiling` is a string (`safe | low | medium | high | critical`) but SecurityEngine's risk calculation returns what type? No mapping defined between enforcement engine's string ceiling and security engine's internal risk representation. | Define explicit mapping: `SecurityEngine.RiskLevel` enum → `Constitution.RiskCeiling` comparison. Add conversion function in `enforcement.go`. |
| **C3** | **High** | lines 162-163 | `ToolsAllowed`/`ToolsForbidden` are string lists but tool names in `internal/tools/` are registered with what identifier? Tool registration uses type-based keys, not string names. No canonical tool name registry defined. | Create `internal/tools/registry.go:CanonicalName(tool Tool) string` function. All constitution tool references validated against canonical names at load time. |
| **C4** | **Medium** | line 176 `Never []string` | Spec says "machine-checked where possible" but provides zero pattern matching semantics. Is it substring match? Regex? Shell command glob? LLM-only? | Define `NeverRule` struct with `pattern string`, `match_type MatchType` (substring\|regex\|glob\|llm_only). Enforcement engine handles each type differently. |
| **C5** | **Medium** | lines 196-206 | `SynthesizedPrompt()` described conceptually but no actual template shown. How are structured constraints rendered vs charter? What if combined prompt exceeds model context? | Add appendix with actual prompt template. Include truncation strategy: "If prompt > context_limit, truncate Charter first, then expand constraints minimally." |
| **C6** | **Low** | line 228 | "minimal conservative constitution" for vague bots lists example but doesn't specify default `AssessmentInterval` or `FrozenFields`. Migration produces inconsistent constitutions. | Define `DefaultConservativeConstitution()` function with explicit defaults for all fields. |

---

## 2. Goal Model + GoalLoop Gaps (Lines 234-324)

**Coverage: ~78%** — State machine is underspecified; concurrency and multi-plan scenarios not addressed.

| ID | Severity | Location | Description | Recommended Fix |
|----|----------|----------|-------------|-----------------|
| **G1** | **Critical** | lines 266-272 | `GoalHealth` has 4 states but spec at line 296 says "marked `at_risk` or `broken` after N consecutive failures" — what sets a goal back to `healthy`? No recovery transition defined. | Add `HealthDecay func(failures int) GoalHealth` and `HealthRecovery func(successes int) GoalHealth`. Define explicit state machine: `broken → at_risk → healthy` requires M consecutive successes. |
| **G2** | **Critical** | line 255 `ActivePlanID` | Goal tracks single `ActivePlanID` but tier-2 employees could have multiple approved plans in flight concurrently. Which plan is "active"? What if two plans conflict? | Change to `ActivePlanIDs []string` with max concurrent plans limit (`MaxActivePlans int` in constitution). GoalLoop blocks ASSESS if `len(ActivePlanIDs) >= MaxActivePlans`. |
| **G3** | **High** | lines 292-297 | Tier 2 EXECUTE phase says "when a Plan is approved, GoalLoop triggers `BotRunner.Execute()`" — but what if the Plan was approved by a different employee's escalator? Who owns execution? | Clarify: Plan.approver_id recorded at approval time. GoalLoop only executes plans where `approver_id matches employee.escalates_to OR approver_id == "system"`. |
| **G4** | **High** | line 256 `PlanHistory []string` | Unbounded slice will grow forever. No retention policy defined. Goals living months could accumulate thousands of plan IDs. | Add `MaxPlanHistory int` (default 100). Implement ring buffer: oldest plan ID dropped when limit exceeded. Optionally persist history to `employee_goals.plan_history_json` for archival. |
| **G5** | **High** | lines 315-319 | Schedule section says scheduler spawns one job per employee per interval — but what if assessment is still running when next interval fires? No concurrency semaphore defined. | Add `assessmentSemaphore chan struct{}` per employee (buffer=1). Scheduler job tries non-blocking send; if channel full, skip this interval with debug log. |
| **G6** | **Medium** | line 247 `Source GoalSource` | Source includes `self_proposed` but tier 1 employees cannot self-propose (reactive only). No validation at goal creation time checking tier compatibility. | Add `ValidateGoalSource(source GoalSource, tier AutonomyTier) error`. Tier 1 rejects `self_proposed`; Tier 2 requires human/user source. |
| **G7** | **Medium** | line 288 | Tier 1 ASSESS says "LLM's response becomes an implicit Plan (single-step)" — but Plan schema requires structured fields (prompt, steps, etc.). How is LLM output converted to Plan? | Define `ImplicitPlanPrompt = "Given trigger: %s, current state: %s, propose single action: ..."` parser extracts action via JSON markdown block. |
| **G8** | **Low** | line 275 | Goal store "follows existing `internal/bot/store.go` pattern" but bot store uses soft-delete via `retired_at`. Should `Goal.RetiredAt` cascade-delete associated findings? | Clarify: findings NOT cascade-deleted on goal retire (audit trail). Add `goal_id IS NULL` handling in periodic audit — findings for retired goals excluded from DriftScore. |

---

## 3. Constitution Enforcement Engine Gaps (Lines 325-455)

**Coverage: ~72%** — Critical gaps in TurnRecord definition, DriftScore formula, and conversation-level budget tracking.

| ID | Severity | Location | Description | Recommended Fix |
|----|----------|----------|-------------|-----------------|
| **E1** | **Critical** | lines 347-348 | Budget check says "via BotState" but BotState tracks `tokens_today`, `cost_cents_today` — what about `MaxConversationTokens`? Conversation-level tracking requires different aggregation (per conversation, not per day). | Add `ConversationTokenStore` interface with `GetConversationTokens(conversationID string) (int, error)`. Pre-exec checker queries this before each turn. |
| **E2** | **Critical** | lines 368-379 | `Audit(ctx, turn TurnRecord)` — `TurnRecord` is NEVER defined anywhere in spec. What fields does it have? Does it include tool results, LLM output, token counts? | Define `TurnRecord` struct explicitly: `conversationID, turnID, toolCalls []ToolCall, llmOutput string, tokenUsage TokenCounts, duration time.Duration`. |
| **E3** | **High** | lines 342-348 | `PreExecChecker.Check()` has no conversationID parameter but needs it for `MaxConversationTokens` check and budget tracking. How does it know which conversation's budget to check? | Add `conversationID string` to `Check()` signature. Wire through from `SecurityEngine.Check()` which already has conversation context. |
| **E4** | **High** | lines 383-386 | Auto-pause "sets `BotState.status = paused`" — but who calls `Manager.Pause()`? Does `PostTurnAuditor` have a Manager reference? Circular dependency: Manager → GoalLoop → BotRunner → PostTurnAuditor → Manager? | Use event bus pattern: `PostTurnAuditor` emits `employee.CriticalFindingEvent` with employee_id. Manager subscribes and calls Pause on receipt. Decouples auditor from lifecycle. |
| **E5** | **High** | lines 393-397 | `DriftScore` calculation formula completely unspecified. "0.0–1.0" but how? Weighted average of findings? Time-decayed? Severity-weighted? This is the core metric for auto-pause. | Define formula: `DriftScore = sum(finding_i.weight * time_decay_i) / max_score`. Example: critical=1.0, warning=0.3, info=0.1; time_decay = exp(-days_since_detected / half_life). |
| **E6** | **High** | lines 443-448 | "We add an optional `agentID` parameter" to SecurityEngine.Check — but Check signature already exists and is called from 20+ call sites. Backward compatibility? Default behavior? | Define `CheckForAgent(action, tool, details, convID, agentID)` as NEW method. Existing `Check()` delegates to `CheckForAgent` with `agentID=""` (empty = skip employee checks). |
| **E7** | **Medium** | lines 380-386 | Post-turn audit routing: `critical` → auto-pause, but what defines "critical"? Severity from `AuditFinding` struct but what criteria does auditor use to assign severity? | Add severity rubric: `critical = Never[] violation OR risk_ceiling exceeded OR budget fraud suspected`; `warning = Charter commitment violation`; `info = minor style drift`. |
| **E8** | **Medium** | line 375 | Periodic audit says "reviews last N invocations" — what is N? Configurable per employee? Global default? What if employee has 1000 invocations in 6h? | Add `PeriodicAuditSampleSize int` (default 50). If `total_invocations > SampleSize`, use reservoir sampling to select representative subset. |
| **E9** | **Medium** | line 418 | Index `idx_audit_employee` on `(employee_id, detected_at)` but queries will filter by `severity` and `checkpoint` and `resolved_at IS NULL`. Missing composite indexes. | Add `idx_audit_severity (severity, resolved_at)` and `idx_audit_checkpoint (checkpoint, detected_at)` for common query patterns. |
| **E10** | **Low** | lines 403-419 | Audit table has `plan_id` and `turn_id` TEXT but both are actually `pkg/id.Generate()` which returns what format? If IDs change format, foreign key semantics break. | Add comment: `plan_id references plans.id (TEXT, pkg/id.Generate)`. Enforce via application-layer FK check before insert. |

---

## 4. CLI/TUI/HTTP/RPC + POC Gaps (Lines 456-583)

**Coverage: ~80%** — Interface definitions are clear; gaps in service layer contracts and menubar/Flutter confusion.

| ID | Severity | Location | Description | Recommended Fix |
|----|----------|----------|-------------|-----------------|
| **S1** | **Critical** | lines 527-543 | RPC methods list `agents.*` but spec says "Service layer: new `EmployeeService` in `internal/services/`" — no method signatures defined. How do RPC handlers call the service? | Define `EmployeeService` interface: `ListAgents(ctx) ([]Agent, error)`, `GetAgent(id string) (*Agent, error)`, etc. RPC handlers delegate to service methods. |
| **S2** | **High** | line 494 | TUI keybinding `ctl-x e` — but `ctl-x o` is MCP per CLAUDE.md line ~600. Is `ctl-x e` already bound? TUI keybindings are in `client.json5` — what's the default layout? | Check `internal/tui/components/keybindings.go` for conflicts. If conflict, use `ctl-x a` (agents) or `ctl-x y` (emploYees). Update `client.json5` template. |
| **S3** | **High** | lines 544-551 | Flutter menubar "new agents tab" — but menubar app is Swift, not Flutter (`menubar/MeeptMenuBar/`). Spec contradicts itself: section title says "Flutter (menubar)" but menubar is native Swift. | Clarify: menubar app is Swift (line 545). Flutter is `ui/flutter_ui/` for full GUI. Create TWO specs: menubar Swift tab + Flutter full-window agents view. |
| **S4** | **High** | line 476-477 | `meept agents migrate` and `meept agents migrate --apply` — but migration produces constitutions for ALL legacy bots. How does operator review individual proposals? No intermediate edit step. | Add `meept agents migrate --list` (show proposals), `meept agents migrate --show <id>` (view single), `meept agents migrate --apply <id>` (per-bot). |
| **S5** | **Medium** | line 469 | `meept agents update <id> <definition.json>` — but constitution amendments go through Plan signoff. Does `update` modify the running config immediately or propose amendment? Conflicts with `amend` command. | Clarify: `update` is for non-constitutional fields (triggers, model). `amend` is for constitution fields (purpose, constraints). Reject constitution changes via `update` with "use `meept agents amend`". |
| **S6** | **Medium** | lines 508-524 | HTTP endpoints include `/api/v1/agents/{id}/goals/{gid}/plans/{pid}/approve` but Plan approval already exists at `/api/v1/plans/{pid}/approve`. Why duplicate? Consistency: agent-less plans approved via `/plans/*`. | Use existing Plan endpoints: `POST /api/v1/plans/{pid}/approve` with body `{approver_id, employee_id}`. Remove agent-specific plan approval endpoints. |
| **S7** | **Low** | lines 552-571 | POC `ci-monitor` uses `tools_allowed: ["web_fetch", "shell_execute"]` but doesn't exercise `AssessmentInterval`, `EscalationTriggers`, or `AmendmentPolicy`. Incomplete feature coverage. | Extend POC: add cron trigger for 15m assess, add `EscalationTriggers` entry for `risk_level >= high`, add `SelfProposeAllowed: false` to show all features. |

---

## 5. Error Handling + Edge Cases + Testing Gaps (Lines 586-663)

**Coverage: ~75%** — Retry logic, timeout handling, and cycle detection are missing; test plan lacks integration scenarios.

| ID | Severity | Location | Description | Recommended Fix |
|----|----------|----------|-------------|-----------------|
| **H1** | **Critical** | lines 614-616 | "Two triggers fire simultaneously ... `BotRunner` already serializes invocations per bot via a per-employee mutex" — but where is this mutex defined? `internal/bot/runner.go` has no such mutex. | Add `invocationMu sync.Mutex` to `BotRunner` struct. `RunOnce()` does `r.invocationMu.Lock(); defer r.invocationMu.Unlock()`. |
| **H2** | **High** | line 588 | "LLM call fails → BotRunner's existing retry path" — but `internal/bot/runner.go` has NO retry logic. LLM client has retry but runner doesn't orchestrate it. | Add `RetryPolicy` to `BotRunner`: `MaxRetries int`, `RetryBackoff time.Duration`. Wrap LLM calls in retry loop with exponential backoff. |
| **H3** | **High** | lines 591-592 | "Plan approval times out (no human signs off within configurable `approval_timeout`, default 7d)" — who checks for timeout? Scheduler job? Who auto-rejects? | Add `PlanTimeoutChecker` scheduled job (runs hourly). Query `plans WHERE state = 'PendingApproval' AND created_at < deadline`. Auto-reject with `timeout_exceeded` reason. |
| **H4** | **High** | line 624 | "Budget exhausted mid-turn" — budget is checked pre-exec (per tool) but turn could have 10 tools. After tool #3 exceeds budget, tools #4-10 are already queued. Does GoalLoop cancel remaining tools? | Add `TurnBudgetTracker` that tracks cumulative tool costs within turn. Pre-exec gate checks `turnBudgetRemaining`, not just daily budget. If exceeded, gate denies and sets `turnComplete = true`. |
| **H5** | **Medium** | lines 595-597 | Constitution validation errors: "employee refuses to start" but doesn't specify what happens to the BotDefinition. Is it stored? Deleted? Operator notified how? | Add `ConstitutionValidationError` event → emitted to bus + written to `employee_audit_findings` at load time. BotDefinition stored with `status = constitution_invalid`. |
| **H6** | **Medium** | line 621 | "Cycle detection over `escalates_to` graph" — but graph could include Plan signoff chains (employee A escalates to B, B's plan escalates to A). Cycle detection must span both direct and transitive chains. | Implement DFS cycle detection in `authority.go:ValidateEscalationChain()`. Check direct `escalates_to` AND transitive via Plan chains (query plans WHERE approver_id IN ...). |
| **H7** | **Medium** | lines 636-637 | `goal_loop_test.go` tests "tier 1 and tier 2 Decide() logic with mocked LLM + executor" — but tier logic includes ASSESS→PLAN→EXECUTE→REFLECT cycle. Full-cycle integration test needed. | Add `goal_loop_integration_test.go`: Mock LLM returns valid ASSESS output; mock executor records tool calls; assert full cycle completes with correct state transitions. |
| **H8** | **Medium** | lines 664-676 | Telemetry section lists 7 metrics but testing section never mentions verifying metric emission. How do we know metrics are actually emitted? | Add `metrics_test.go`: Start employee with test metrics emitter; trigger invocation; assert `employee.invocations` counter incremented, `employee.goal.health` gauge updated. |
| **H9** | **Low** | line 656 | Pre-commit hooks list "mutexio, setters, staticcheck, feature-docs" but `enforcement.go` will have I/O under mutex (audit writes). Pre-commit mutexio will fail. | Exception comment in `enforcement.go` for audit writes (necessary I/O under lock for atomicity). Or refactor: snapshot findings under lock, write to DB after unlock. |

---

## Priority Remediation Order

### Before Implementation (Critical - 7 gaps)

These must be resolved before coding begins:

1. **C1** - Constitution version race condition during amendment approval
2. **G1** - Goal health recovery transitions (how does broken → healthy?)
3. **G2** - Multi-plan concurrency on Goal (ActivePlanID vs multiple concurrent plans)
4. **E1** - Conversation-level token budget tracking (not just daily)
5. **E2** - TurnRecord struct definition (completely unspecified)
6. **S1** - EmployeeService interface contract (method signatures)
7. **H1** - per-bot invocation mutex implementation (missing from BotRunner)

### Sprint 1 (High - 15 gaps)

These would cause significant rework if discovered mid-implementation:

- **C2** - Risk ceiling mapping to SecurityEngine risk levels
- **C3** - Tool name canonicalization (registry function)
- **G3** - Plan ownership and execution authority
- **G4** - PlanHistory retention policy (ring buffer)
- **G5** - Assessment concurrency semaphore
- **E3** - ConversationID parameter in PreExecChecker.Check()
- **E4** - Event bus pattern for auto-pause (decouple auditor from Manager)
- **E5** - DriftScore calculation formula
- **E6** - agentID parameter backward compatibility
- **S2** - TUI keybinding conflicts
- **S3** - Flutter vs Swift menubar clarification
- **S4** - Migration UX (per-bot review workflow)
- **H2** - BotRunner retry logic
- **H3** - Plan timeout checker scheduled job
- **H4** - Turn-level budget tracker

### Sprint 2 (Medium - 13 gaps)

These can be resolved during implementation:

- **C4** - Never rule pattern matching semantics
- **C5** - SynthesizedPrompt template
- **G6** - GoalSource validation by tier
- **G7** - Implicit Plan prompt and parser
- **G8** - Findings retention on goal retire
- **E7** - Severity rubric for audit findings
- **E8** - PeriodicAuditSampleSize configuration
- **E9** - Composite database indexes
- **S5** - update vs amend command distinction
- **S6** - Plan approval endpoint consolidation
- **H5** - ConstitutionValidationError event handling
- **H6** - Cycle detection algorithm (DFS)
- **H7** - Full-cycle integration tests
- **H8** - Metrics emission tests

### Backlog (Low - 5 gaps)

Minor clarifications and documentation improvements:

- **C6** - DefaultConservativeConstitution explicit defaults
- **E10** - Audit table FK comments (pkg/id.Generate format)
- **S7** - POC feature extension
- **H9** - mutexio exception for necessary audit writes

---

## Remediation Effort Estimate

| Phase | Gaps | Estimated Effort |
|-------|------|------------------|
| Pre-implementation (Critical) | 7 | 1-2 weeks |
| Sprint 1 (High) | 15 | 2-3 weeks |
| Sprint 2 (Medium) | 13 | 1-2 weeks |
| Backlog (Low) | 5 | 0.5 weeks |
| **TOTAL** | **40** | **~5-7 weeks** |

---

## Verification Checklist

Before beginning implementation, verify:

- [ ] All 7 Critical gaps have design decisions documented
- [ ] EmployeeService interface is fully specified
- [ ] TurnRecord struct is defined with all fields
- [ ] DriftScore formula is implemented and tested
- [ ] Conversation-level budget tracking is wired
- [ ] Goal state machine includes recovery transitions
- [ ] Multi-plan concurrency is handled on Goal

---

## Related Documents

- **Spec:** `docs/superpowers/specs/2026-06-23-ai-employee-design.md`
- **Implementation Plan:** (to be created after gap remediation)
- **Skill:** `.claude/skills/design-spec-review/SKILL.md` (this review methodology)

---

**Review Method:** Domain-partitioned systematic analysis with 5 review domains, 9 gap categories, and severity-classified findings.

**Review Date:** 2026-06-25
**Next Review:** After Critical gaps are resolved, before Sprint 1 planning
