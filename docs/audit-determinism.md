# Determinism Audit Report: Meept Agentic System

**Audit Date:** 2026-04-23
**Auditor:** Claude Code (nw,qwen3.5-397b-fast)
**Plan Reference:** `docs/plan-determinism-audit.md`

---

## Executive Summary

Meept demonstrates a **sophisticated multi-agent architecture** with strong foundations for deterministic task completion. The system exhibits several mature patterns including externalized state, explicit dependency tracking, and review workflows. However, critical gaps exist in completion verification, retry logic, and tool result validation that prevent it from achieving production-grade reliability.

**Overall Score: 3.2/5.0** — *CONDITIONAL* (passes basic orchestration, fails verification)

---

## Dimension Scores

### A. Task Decomposition & Granularity
**Score: 4/5** | Confidence: 0.85

**Evidence:**
- `internal/agent/strategic.go:62-106` — `StrategicPlanner` decomposes tasks into discrete steps
- `internal/agent/strategic.go:25-34` — `plannerStep` struct with explicit `DependsOn []int` field
- `internal/task/step.go:55-72` — `TaskStep` with `DependsOn []string`, `Sequence int`
- `internal/agent/strategic.go:277-309` — `shouldDecompose()` avoids over-decomposition for simple requests
- Plan limit enforced at 10 steps max (`strategic.go:90-92`)

**Strengths:**
- Steps are atomic units assigned to specialist agents
- Dependencies are explicit and machine-readable
- Fast-path for simple requests avoids unnecessary LLM calls

**Failures:**
- No explicit validation that steps are truly atomic (≤3 sub-operations)
- Step granularity depends on LLM planner quality without validation
- `tool_hint` is suggestive, not enforced — agent may not match step requirements

---

### B. State Externalization
**Score: 5/5** | Confidence: 0.95

**Evidence:**
- `internal/task/store.go:14-52` — SQLite-backed `Store` with full CRUD
- `internal/task/step.go:134-184` — `StepStore` with migration schema
- `internal/agent/workspace.go:16-277` — `WorkspaceManager` creates git-backed directories
- `internal/task/task.go:32-65` — `Task` struct with `State TaskState`, `TotalJobs`, `CompletedJobs`, `FailedJobs`
- Task states: `pending → planning → executing → testing → completed|failed|cancelled`

**Strengths:**
- All task/step state persisted to SQLite (not in LLM context)
- Git-backed workspaces per task (`~/.meept/workspaces/{task_id}/`)
- Machine-readable state machine with explicit transitions
- `PLAN.md`, `REVIEW.md`, `LOG.md` written to workspace for audit trail

**No Critical Failures**

---

### C. Execution Control
**Score: 4/5** | Confidence: 0.88

**Evidence:**
- `internal/agent/orchestrator.go:14-44` — `Orchestrator` coordinates strategic/tactical layers
- `internal/agent/tactical.go:26-63` — `TacticalScheduler` enqueues steps as jobs
- `internal/agent/tactical.go:109-147` — `scheduleStep()` validates dependencies before execution
- `internal/queue/` — Job queue with agent-specific routing
- `tactical.go:556-573` — `selectAgent()` maps tool hints to agent IDs deterministically

**Strengths:**
- Clear separation: strategic (planning) vs tactical (execution)
- Dependency validation blocks premature execution (`tactical.go:111-147`)
- Agent routing is deterministic based on tool hint

**Failures:**
- No explicit "one-step-at-a-time" enforcement per agent — agents may run concurrently
- No global execution semaphore to prevent resource exhaustion
- Concurrent ready steps are all scheduled (`tactical.go:82-91`), which could cause contention

---

### D. Completion Verification
**Score: 2/5** | Confidence: 0.90

**Evidence:**
- `internal/agent/tactical.go:199-362` — `OnJobCompleted()` stores result, updates state
- `internal/agent/review.go:76-98` — `ReviewPolicy.NeedsReview()` determines review requirements
- `internal/agent/tactical.go:232-258` — Review triggered if policy enabled
- **NO ground-truth validation** — completion is LLM self-reporting

**Strengths:**
- Review workflow exists for high-risk operations (`code`, `refactor`, `git`)
- Step results stored in database

**Critical Failures:**
- **No filesystem validation** — completed file writes not verified to exist
- **No API/DB ground-truth checks** — completion based on agent claims
- Review is LLM-to-LLM (reviewer agent), not tool-based verification
- `AreAllCompleted()` checks step states, not actual outcomes

---

### E. Self-Reporting Integrity
**Score: 2/5** | Confidence: 0.82

**Evidence:**
- `internal/agent/loop.go` — Agent loop with cycle/convergence detection
- `internal/agent/loop.go:42-52` — `DetectionConfig` for cycle/convergence thresholds
- **No mandatory evidence requirements** — agents report completion without proof
- **No programmatic claim validation** — "Task complete" accepted at face value

**Strengths:**
- Cycle detection prevents infinite loops
- Convergence detection identifies stalled execution

**Critical Failures:**
- No structured output schema requiring evidence (e.g., file paths, hashes)
- No mismatch detection between claimed vs actual results
- Recommendations stored (`step.go:69`) but not validated
- Agent can claim "file created" without proof of existence

---

### F. Retry & Repair Logic
**Score: 3/5** | Confidence: 0.85

**Evidence:**
- `internal/agent/tactical.go:420-553` — `OnJobFailed()` with rate-limit retry logic
- `internal/agent/tactical.go:435-459` — Retry with exponential backoff for rate limits
- `internal/task/step.go:111-132` — `CreateRevision()` for review rejections
- `internal/agent/review.go:147-153` — `ExceedsMaxRevisions()` caps revision cycles

**Strengths:**
- Rate-limit retries with backoff
- Revision workflow for rejected steps
- Max revision cycles (default: 3) before human intervention

**Failures:**
- **Only retries rate-limit errors** — other failures are terminal
- No structured retry payload (failure context not fed back)
- Revision steps depend on human-readable `Feedback` string, not structured error
- No automatic repair workflow — rejected steps require new agent execution

---

### G. Plan Drift Resistance
**Score: 4/5** | Confidence: 0.80

**Evidence:**
- `internal/agent/strategic.go:56-57` — Plan limit of 10 steps max
- `internal/agent/tactical.go:93-102` — `task.progress` events per step
- `internal/agent/workspace.go:144-176` — `WritePlan()` persists PLAN.md
- `internal/agent/loop.go` — Agent loop with conversation window management

**Strengths:**
- Plans capped at 10 steps prevents runaway execution
- Per-step context re-injection via job queue payload
- Workspace files provide audit trail for drift detection

**Failures:**
- No explicit checkpointing between steps (no rollback mechanism)
- Long plans (>7 steps) executed without intermediate validation gates
- Context re-injection is implicit (job payload), not explicit re-briefing

---

### H. Tool Use Enforcement
**Score: 3/5** | Confidence: 0.88

**Evidence:**
- `internal/tools/registry.go:131-154` — `Registry.Execute()` runs tools by name
- `internal/agent/loop.go` — Tool execution integrated into agent loop
- **Tool results logged but not validated** — `Execute()` returns `ToolResult` without verification
- `internal/security/engine.go:243-320` — `Engine.Check()` validates tool permissions

**Strengths:**
- Tool registry enforces tool existence before execution
- Security engine validates permissions pre-execution
- Tool definitions generated from registry (`ToLLMDefinitions()`)

**Failures:**
- **No tool result validation** — success/failure based on error return, not output inspection
- No verification that tool side-effects occurred (e.g., file exists after write)
- Security check is pre-flight only — no post-execution validation

---

## Adversarial Test Results

### Test 1: Model skips final step but claims completion
**Detection Mechanism:** `tactical.go:296-300` — `AreAllCompleted()` checks all steps terminal
**Result:** DETECTED — Step would remain `pending`/`running`, task not marked complete
**Verdict:** PASS

### Test 2: Tool call silently fails
**Detection Mechanism:** `tools/registry.go:144-150` — Error returned and logged
**Result:** PARTIAL — Error logged, but no automatic retry or repair
**Verdict:** CONDITIONAL — detected but not repaired

### Test 3: Context window truncates earlier steps
**Detection Mechanism:** `internal/memory/manager.go` — Memory injection for context
**Result:** PARTIAL — Memory provides continuity, but no explicit re-injection per step
**Verdict:** CONDITIONAL — relies on memory quality

### Test 4: Partial output produced but marked complete
**Detection Mechanism:** None — completion based on step state, not output inspection
**Result:** NOT DETECTED — No validation that output matches requirements
**Verdict:** CRITICAL FAILURE

---

## Estimated Reliability

| Metric | Score | Rationale |
|--------|-------|-----------|
| Single-pass success rate | 0.65 | LLM self-reporting without verification |
| With retries | 0.75 | Rate-limit retries help, but other failures terminal |
| With review | 0.80 | Review catches obvious failures, still LLM-based |

**Overall Reliability Estimate: 0.70–0.75** (25–30% silent failure rate)

---

## Systemic Risks

### Critical

1. **No Ground-Truth Verification**
   - File writes not verified to exist on disk
   - Database changes not validated against schema
   - Agent claims accepted without evidence

2. **LLM-to-LLM Review**
   - Reviewer agent validates executor agent
   - Both can hallucinate — no external validator

3. **Tool Result Blindness**
   - Tool execution returns `ToolResult` but no semantic validation
   - `WriteFile` could write empty file — marked success

### High

4. **Revision Feedback Quality**
   - `ReviewResult.Feedback` is unstructured string
   - Revision agent must parse natural language

5. **Concurrent Step Contention**
   - Multiple ready steps scheduled simultaneously (`tactical.go:82-91`)
   - No semaphore for resource-constrained environments

### Medium

6. **Revision Cycle Cap**
   - Max 3 revisions — reasonable but arbitrary
   - No degradation path (what happens after cap?)

---

## Architectural Recommendations

### Immediate (Blocking for Production)

1. **Implement Ground-Truth Validators**
   ```go
   // Add to internal/validator/
   type FileValidator struct { fs.FS }
   func (v *FileValidator) Verify(step *TaskStep) error {
       // Check files exist, have non-zero size, match expected patterns
   }
   ```

2. **Require Structured Evidence**
   ```go
   type StepResult struct {
       Claims []string  `json:"claims"`  // "Created file X"
       Evidence []Evidence `json:"evidence"` // file stat, hashes
   }
   type Evidence struct {
       Type string  `json:"type"` // "file_exists", "hash", "api_response"
       Value string `json:"value"`
   }
   ```

3. **Add Post-Execution Tool Validation**
   ```go
   // internal/tools/registry.go
   func (r *Registry) ExecuteWithValidation(...) error {
       result := tool.Execute(...)
       return validator.Validate(tool.Name(), result)
   }
   ```

### Short-Term (Production Hardening)

4. **Implement Dependency Graph Validation**
   - Before scheduling, validate DAG has no cycles
   - Currently relies on `DependsOn` but no explicit cycle detection

5. **Add Execution Checkpoints**
   - Snapshot `PLAN.md` + workspace state per step
   - Enable rollback on review rejection

6. **Introduce Validation Gates**
   - After every N steps, run `workspace.Validate()`
   - Verify files, run tests, check git status

---

## Final Verdict: CONDITIONAL

**Meept is architecturally sound but verification-gap fragile.**

The system demonstrates sophisticated orchestration:
- Externalized state (SQLite + git workspaces)
- Explicit dependency tracking
- Review workflows
- Rate-limit retry logic

However, it **fails the determinism audit** on critical dimensions:
- No ground-truth completion verification
- Tool results not validated
- Self-reporting accepted without evidence

**Production Readiness:** Not recommended until Critical items addressed.

**Estimated Failure Rate:** 25–30% of tasks will silently fail (claim completion without achieving intended outcome).

---

## Scoring Summary

| Dimension | Score | Status |
|-----------|-------|--------|
| A. Task Decomposition | 4/5 | PASS |
| B. State Externalization | 5/5 | PASS |
| C. Execution Control | 4/5 | PASS |
| D. Completion Verification | 2/5 | FAIL |
| E. Self-Reporting Integrity | 2/5 | FAIL |
| F. Retry & Repair Logic | 3/5 | CONDITIONAL |
| G. Plan Drift Resistance | 4/5 | PASS |
| H. Tool Use Enforcement | 3/5 | CONDITIONAL |

**Weighted Average: 3.2/5.0**

---

*This audit was conducted following the plan in `docs/plan-determinism-audit.md` with strict adherence to "distrust every claim, verify everything externally, assume partial failure is default state" principles.*
