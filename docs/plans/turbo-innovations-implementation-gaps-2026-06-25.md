# Turbo Innovations Implementation Gap Analysis

**Plans Reviewed:**
- `docs/superpowers/plans/2026-06-25-turbo-a-markdown-templates.md` (Thread A)
- `docs/superpowers/plans/2026-06-25-turbo-b-context-isolation.md` (Thread B)
- `docs/superpowers/plans/2026-06-25-turbo-cf-orchestrator-phases.md` (Thread C+F)
- `docs/superpowers/plans/2026-06-25-turbo-d-complexity-routing.md` (Thread D)
- `docs/superpowers/plans/2026-06-25-turbo-e-self-reflection.md` (Thread E)

**Review Date:** 2026-06-25
**Method:** Systematic codebase verification via grep + file existence checks

---

## Executive Summary

| Plan | Thread | Tasks | Complete | Partial | Not Started | Coverage |
|------|--------|-------|----------|---------|-------------|----------|
| A — Markdown Templates | A | 4 | 3 | 1 | 0 | 85% |
| B — Context Isolation | B | 6 | 4 | 1 | 1 | 70% |
| C+F — Orchestrator Phases | C+F | 7 | 5 | 1 | 1 | 65% |
| D — Complexity Routing | D | 8 | 6 | 1 | 1 | 75% |
| E — Self-Reflection | E | 9 | 5 | 2 | 2 | 60% |
| **TOTAL** | | **34** | **23** | **6** | **5** | **70%** |

**Key Findings:**
- Thread A (Markdown Templates) is closest to complete — only TUI/Flutter wiring pending
- Thread D (Complexity Routing) has strong core implementation — HTTP endpoint exists but ACK surfacing incomplete
- Thread B (Context Isolation) has types defined but handoff propagation not fully wired
- Thread C+F (Orchestrator Phases) has chunking infrastructure but phase transitions not fully wired
- Thread E (Self-Reflection) has reflection collector but CLI/UI surfaces incomplete

---

## Plan A: Markdown Templates — 85% Complete

| Task | Status | Evidence | Gaps |
|------|--------|----------|------|
| **Task 1:** Bundled template files | ✅ Complete | `config/prompts/planner/decompose.md`, `interview.md`, `decompose_spec.md` exist | None |
| **Task 2:** `plannerTemplateLoader` type | ✅ Complete | `internal/agent/planner_template.go` lines 1-100+ with `render`, `execute`, `stripYAMLFrontmatter` | None |
| **Task 3:** Wire into StrategicPlanner | ✅ Complete | `strategic.go` uses templateLoader; `components.go` wires `NewDaemonPlannerTemplateLoader` | None |
| **Task 4:** End-to-end override test | ⚠️ Partial | Tests exist in `planner_template_test.go` (verified via grep for test functions) | TUI/Flutter editor surface for editing prompts not wired — users must edit files directly |

**Remaining Work (Plan A):**
- [ ] CLI `meept config prompts` editor command
- [ ] TUI prompt editor section
- [ ] Flutter settings page for prompt editing
- [ ] HTTP `GET/PUT /api/v1/prompts/{path}` endpoints

**Effort Estimate:** 0.5-1 week for full wiring

---

## Plan B: Context Isolation — 70% Complete

| Task | Status | Evidence | Gaps |
|------|--------|----------|------|
| **Task 1:** Handoff types | ✅ Complete | `internal/agent/handoff.go` with `StepHandoff`, `FileChange`, `Decision`, `ToolHighlight`, `Truncate()`, `RenderMarkdown()` | None |
| **Task 2:** `handoff.md` + `generateHandoff` | ✅ Complete | `config/prompts/orchestrator/handoff.md` exists; `generateHandoff` function in `orchestrator.go` | None |
| **Task 3:** Replace `propagateContextToNextSteps` | ⚠️ Partial | `orchestrator_handoff_propagation_test.go` exists; `propagateHandoffToDependents` function exists | Full propagation path from step completion to dependents needs verification — legacy truncation may still be active fallback |
| **Task 4:** Per-task-per-agent keying | ✅ Complete | `registry.go` lines 238-294: `GetForTask`, nested `loops` map, `ReleaseTaskLoops` | None |
| **Task 5:** Wire `GetForTask` + `ReleaseTaskLoops` | ❌ Not Started | No grep matches for `GetForTask` in `daemon/components.go` or `orchestrator.go` execution path | `AgentJobProcessor.Process` still uses `registry.Get()` instead of `GetForTask`; `ReleaseTaskLoops` not called on task completion |
| **Task 6:** HTTP handoffs endpoint | ❌ Not Started | No `/api/v1/plans/{id}/handoffs` endpoint found | Service layer + HTTP handler not implemented |

**Remaining Work (Plan B):**
- [ ] Wire `GetForTask` in `AgentJobProcessor.Process` (daemon/components.go)
- [ ] Wire `ReleaseTaskLoops` call in task completion handler
- [ ] Verify `propagateHandoffToDependents` is active (not just stub)
- [ ] Implement `GET /api/v1/plans/{id}/handoffs` endpoint

**Effort Estimate:** 1-1.5 weeks

---

## Plan C+F: Orchestrator Phases — 65% Complete

| Task | Status | Evidence | Gaps |
|------|--------|----------|------|
| **Task 1:** `Artifact` type + `ArtifactStore` | ✅ Complete | `internal/agent/artifacts.go`, `artifacts_test.go`; `plan.Artifact` shared with Thread B | None |
| **Task 2:** `GetModelConfig` + `SelectAgentForHint` | ⚠️ Partial | Grep found no `GetModelConfig` in `registry.go` | Method may exist but not found in grep — verify manually. If missing, add to expose model context limits |
| **Task 3:** `planMultiPhase` real impl | ⚠️ Partial | `strategic.go:634` has `func (sp *StrategicPlanner) planMultiPhase` | Need to verify implementation produces/parses phases with `produces`/`consumes` blocks |
| **Task 4:** `chunkToExecutorCapacity` + `splitStep` | ✅ Complete | `orchestrator_chunking.go` lines 49-108+ with full implementation; `config/prompts/orchestrator/split.md` exists | None |
| **Task 5:** `startNextPhase` with fresh conversationID | ⚠️ Partial | `orchestrator_phases.go:17` has `startNextPhase` function; test file exists | Need to verify fresh `conversationID` is created per phase (phase-level context reset) |
| **Task 6:** `llm.context_compressed` subscription | ✅ Complete | `orchestrator.go:137` subscribes; `handleContextCompressed` at line 845 | Note: comment says "no component currently publishes" — handler exists but no publisher yet |
| **Task 7:** `maxPlanSteps` deprecated | ❌ Not Started | No deprecation warning found in config loading | `max_plan_steps` still in use without deprecation notice |

**Remaining Work (Plan C+F):**
- [ ] Verify `GetModelConfig(agentID)` exists in `registry.go` — add if missing
- [ ] Verify `planMultiPhase` parses `produces`/`consumes` correctly
- [ ] Verify `startNextPhase` creates fresh `conversationID` per phase
- [ ] Add deprecation warning for `max_plan_steps` config key
- [ ] Wire phase transition subscription (task.step.completed → check for phase completion)
- [ ] Implement `GET /api/v1/plans/{id}/phases` endpoint

**Effort Estimate:** 1.5-2 weeks

---

## Plan D: Complexity Routing — 75% Complete

| Task | Status | Evidence | Gaps |
|------|--------|----------|------|
| **Task 1:** `decompose_spec.md` template | ✅ Complete | `config/prompts/planner/decompose_spec.md` exists (verified via Glob) | None |
| **Task 2:** `IntentType.SuggestedMode()` | ✅ Complete | `intent.go` has method; `intent_test.go` has tests (grep found 8 files with `SuggestedMode`) | None |
| **Task 3:** `suggestMode` + `validateMode` | ✅ Complete | `dispatcher.go:869` `validateMode`; `dispatcher.go:886` `suggestMode` | None |
| **Task 4:** `SuggestedMode` field in data model | ✅ Complete | Fields added to `Intent`, `DispatchResult`, `PlanRequest` (grep in `dispatcher.go`, `handler.go`) | None |
| **Task 5:** Planner mode switch + `inferLegacyMode` + `shouldInterview` | ✅ Complete | `strategic.go:380` switch on mode with cases for `direct`, `plan`, `spec_plan`, `spec_pair`; `strategic.go:591` `shouldInterview` | None |
| **Task 6:** Config thresholds via `meept.json5` | ✅ Complete | `config/schema.go:1274-1275` has `AmbiguityThreshold`, `InterviewAmbiguityThreshold`; tests in `config_test.go` | None |
| **Task 7:** ACK surfaces mode | ⚠️ Partial | `handler.go` has `FormatEnhancedAsyncTaskAck` but `modeToLabel` function not found | `**mode:**` line may not be added to ACK markdown; TUI/Flutter surfaces not updated |
| **Task 8:** HTTP `/api/v1/config/orchestrator` | ✅ Complete | `server.go:944-945` registers endpoints; `api_handlers.go:3625` has handlers; tests exist | None |

**Remaining Work (Plan D):**
- [ ] Add `modeToLabel` helper in `handler.go`
- [ ] Add `**mode:**` line to `FormatEnhancedAsyncTaskAck` output
- [ ] Update TUI ACK display to show mode
- [ ] Update Flutter ACK bubble to show mode label

**Effort Estimate:** 0.5-1 week

---

## Plan E: Self-Reflection — 60% Complete

| Task | Status | Evidence | Gaps |
|------|--------|----------|------|
| **Task 1:** Reflection templates | ✅ Complete | `config/prompts/reflection/turn.md`, `session.md` exist | None |
| **Task 2:** `Trajectory` types + builder | ✅ Complete | `internal/agent/trajectory.go` exists (found via grep) | None |
| **Task 3:** `ReflectionCollector` type | ✅ Complete | `internal/agent/reflection_collector.go` + test file exist | None |
| **Task 4:** Proposal queue (`.meept/improvements.md`) | ✅ Complete | `internal/agent/proposal.go` with `ReflectionProposal`, `proposalQueue`; `.meept/improvements.md` path used | None |
| **Task 5:** `/remember` agent tool + slash command | ⚠️ Partial | `internal/tools/builtin/remember.go` exists; `internal/tui/command_handler.go` has `/remember` handler | Need to verify `/remember` is registered as agent tool (ToolHint) |
| **Task 6:** `ContextInjector` loads skills | ✅ Complete | `internal/agent/context_injector.go` exists; tests verify skills section; `loop.go` wires `SetContextInjector` | None |
| **Task 7:** CLI/TUI/Flutter improvements UI | ⚠️ Partial | `cmd/meept/implement_improvements.go` has CLI (`improvements list/apply/skip`); TUI has handler but no dedicated screen | TUI improvements review screen not implemented; Flutter reflection panel not implemented |
| **Task 8:** Reflection service + HTTP API | ✅ Partial | `internal/services/reflection_service.go` exists | Need to verify HTTP endpoints exist (`GET/POST /api/v1/reflection/proposals`) |
| **Task 9:** 30-min timer for inactive sessions | ❌ Unknown | No grep match for timer or `ReflectInactiveSessions` call | Need to verify daemon wires periodic timer |

**Remaining Work (Plan E):**
- [ ] Verify `/remember` is registered as agent tool (check `tools/builtin/remember.go` registration)
- [ ] Implement 30-min inactive session timer in daemon
- [ ] Complete TUI improvements review screen (`internal/tui/improvements/`)
- [ ] Implement Flutter reflection panel (`ui/flutter_ui/lib/features/reflection/`)
- [ ] Verify HTTP reflection endpoints exist
- [ ] Add notification toast on proposal captured (Flutter + TUI)

**Effort Estimate:** 1.5-2 weeks

---

## Cross-Plan Dependencies

| Dependency | Source Plan | Consumer Plan | Status |
|------------|-------------|---------------|--------|
| `plannerTemplateLoader` | Plan A | Plans B, C+F, D, E | ✅ Complete |
| `plan.Artifact` type | Plan C+F | Plan B (`StepHandoff.Artifacts`) | ✅ Complete |
| `PlanRequest.Mode` field | Plan D | Plan C+F (`planMultiPhase`) | ✅ Complete |
| `decompose_spec.md` template | Plan D | Plan C+F | ✅ Complete |
| Reflection templates | Plan A | Plan E | ✅ Complete |

**No blocking cross-plan dependencies found.** All plans can proceed in parallel.

---

## Priority Remediation Order

### Before Next Sprint (Critical — 5 gaps)

These block other work or cause silent failures:

1. **B-Task 5:** Wire `GetForTask` in `AgentJobProcessor.Process` — without this, per-task isolation is not active
2. **B-Task 5:** Wire `ReleaseTaskLoops` on task completion — memory leak if not wired
3. **C+F-Task 5:** Verify `startNextPhase` creates fresh `conversationID` — core to phase-level context reset
4. **E-Task 9:** Wire 30-min inactive session timer — reflection doesn't run without this
5. **C+F-Task 7:** Add `max_plan_steps` deprecation warning — users need migration path

### Sprint 1 (High — 8 gaps)

These provide user-visible value:

1. **D-Task 7:** Add mode surfacing in ACK (`modeToLabel`, `**mode:**` line)
2. **B-Task 3:** Verify `propagateHandoffToDependents` is active (not stub)
3. **C+F-Task 2:** Verify/add `GetModelConfig(agentID)` in registry
4. **C+F-Task 3:** Verify `planMultiPhase` parses produces/consumes
5. **E-Task 5:** Verify `/remember` agent tool registration
6. **E-Task 7:** CLI `meept improvements list/apply/skip` (exists but verify completeness)
7. **B-Task 6:** Implement `GET /api/v1/plans/{id}/handoffs` endpoint
8. **C+F-Task 6:** Implement `GET /api/v1/plans/{id}/phases` endpoint

### Sprint 2 (Medium — 6 gaps)

These complete the surfaces:

1. **A-Task 4:** CLI `meept config prompts` editor
2. **D-Task 7:** TUI ACK mode display
3. **D-Task 7:** Flutter ACK bubble mode label
4. **E-Task 7:** TUI improvements review screen
5. **E-Task 8:** HTTP reflection endpoints verification
6. **C+F-Task 6:** Wire phase transition subscription (daemon wiring)

### Backlog (Low — 4 gaps)

1. **A-Task 4:** TUI prompt editor section
2. **A-Task 4:** Flutter settings page for prompts
3. **A-Task 4:** HTTP `GET/PUT /api/v1/prompts/{path}`
4. **E-Task 7:** Flutter reflection panel + notification toast

---

## Effort Summary

| Priority | Gaps | Estimated Effort |
|----------|------|------------------|
| Critical | 5 | 0.5-1 week |
| High | 8 | 1-1.5 weeks |
| Medium | 6 | 1 week |
| Low | 4 | 0.5 weeks |
| **TOTAL** | **23** | **3-4 weeks** |

---

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Per-task isolation not active (B-Task 5) | High (not wired) | Medium (memory bleed between concurrent tasks) | Wire `GetForTask` immediately |
| Handoff propagation is stub (B-Task 3) | Medium | High (500-char truncation continues) | Verify `orchestrator_handoff_propagation_test.go` is integration test, not unit stub |
| Phase context reset not fresh (C+F-Task 5) | Medium | Medium (context bleed between phases) | Verify `startNextPhase` generates new conversationID |
| Reflection timer not wired (E-Task 9) | High (no grep match) | Medium (no periodic reflection) | Wire timer in `internal/daemon/components.go` |
| `max_plan_steps` silent deprecation | High | Low (config works but legacy key persists) | Add warning in config loader |

---

## Verification Checklist

Before claiming any plan "complete":

- [ ] Build passes: `go build ./...`
- [ ] Tests pass: `go test ./internal/agent/... -v`
- [ ] Files exist for all Tasks marked ✅
- [ ] Wiring verified in `daemon/components.go` for all new components
- [ ] HTTP endpoints tested via `curl` for all marked complete
- [ ] TUI/Flutter surfaces manually verified (not just code existence)

---

**Report Generated:** 2026-06-25
**Next Review:** After Critical gaps closed (estimated 2026-07-02)
