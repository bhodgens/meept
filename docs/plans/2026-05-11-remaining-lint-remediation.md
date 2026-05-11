# Plan: Resolve Remaining 3,031 Lint Issues

**Date:** 2026-05-11
**Scope:** 3,031 remaining issues across 11 linters
**Goal:** Reduce lint warnings to near-zero using explicit, reviewable suppressions — no blanket rule hiding

## Design Principle

Every suppression must be visible in code review. We prefer targeted `//nolint` comments with explanations over blanket config disables. The only exceptions are linters with near-100% false-positive rates for this codebase (contextcheck) and config settings that already work correctly (goconst `ignore-tests`).

## Current State

| Linter | Issues | Difficulty | Strategy |
|--------|--------|------------|----------|
| goconst | 1,853 | Easy (mechanical) | Fix `ignore-tests`, extract production constants |
| revive | 552 | Medium (rename params) | Rename unused params, add `//nolint` for stutter |
| gosec | 286 | Medium (security review) | Fix real issues, `//nolint` false positives per-site |
| errcheck | 261 | Medium (add `_ =`) | Check production errors, suppress test warnings |
| contextcheck | 45 | Hard (refactor) | Disable in config (near-100% false positives) |
| intrange | 17 | Easy (mechanical) | Convert for loops to integer range |
| staticcheck | 10 | Easy (mechanical) | Replace WriteString(fmt.Sprintf) with fmt.Fprintf |
| nilerr | 3 | Easy (nolint) | Intentional daemon stale-PID cleanup, add nolint |
| modernize | 2 | Easy | Add omitzero nolint suppressions |
| rowserrcheck | 1 | Easy | Add rows.Err() check |
| prealloc | 1 | Easy | Preallocate slice |

---

## Phase 1: Config Changes (minimal, justified)

Only two config changes. Everything else is handled per-site in code.

### 1a. Disable contextcheck

**Justification:** Near-100% false-positive rate for this codebase. Every warning falls into one of these categories:
- Daemon shutdown paths use `context.Background()` because no request context exists
- Test code uses `context.Background()` because there is no incoming request
- Bus handler goroutines are detached by design — they cannot inherit request context
- RPC cache/stats operations are intentionally context-free (reading local state)
- Scheduler operations use background context for persistent jobs

Passing context through all these paths would require significant refactoring for no functional benefit — the operations don't need cancellation or timeouts.

**Action:** Remove `contextcheck` from the enabled linters list in `.golangci.yml`.

**Issues resolved:** 45

### 1b. Fix goconst `ignore-tests` effectiveness

The config already has `ignore-tests: true` but ~1,700 test-string warnings still appear. Investigate why:
- Some test helpers live in non-`_test.go` files (e.g., `testhelpers.go`)
- Some strings span both test and production code (e.g., `"executor"` used in tests and agent config)

**Action:** If `ignore-tests` is working correctly and the remaining 1,700 are legitimate (non-test files), proceed to Phase 4 to extract the production constants. If the setting is broken, fix it.

**Issues resolved:** Depends on root cause — could be 0 to 1,700

---

## Phase 2: Mechanical Fixes (subagent-friendly)

### 2a. Fix staticcheck (10 issues)

All QF1012 in `internal/agent/q/skill_designer.go` — replace `WriteString(fmt.Sprintf(...))` with `fmt.Fprintf(&buf, ...)` at lines 103, 108, 116, 124, 138, 216, 217, 258, 260, 261.

### 2b. Fix intrange (17 issues)

Convert `for i := 0; i < n; i++` to `for i := range n` in:
- `internal/code/ast/parser.go`: lines 216, 248, 385, 423
- `internal/code/ast/symbols.go`: lines 136, 167, 184, 223, 236, 294, 356, 404, 465, 513, 535, 566, 597

Note: verify each loop body actually uses `i` before converting. If `i` is unused, use `for range n`.

### 2c. Fix errcheck production code (~80 issues)

Add proper error handling for:
- `w.Write()` calls in HTTP handlers → add `_, _ = w.Write(...)` or check error
- `tx.Rollback()` in deferred cleanup → `defer func() { _ = tx.Rollback() }()`
- `s.db.Exec()` in store initialization → check error
- `proc.Signal()` / `proc.Kill()` → check or `_ =` (best-effort signals)
- `os.MkdirAll()` → check error (directory creation should not silently fail)
- `cfg.Validate()` → check error
- `c.saveState()` → check error

### 2d. Fix errcheck test code (~180 issues)

Add `_ =` prefix to all unchecked function calls in test files. This is safe because:
- Test failures are detected via assertions, not return values
- `json.Unmarshal` in test setup failing means the test is broken anyway
- `w.Write()` in test handlers has no consequence

### 2e. Fix revive unused-parameter (~440 issues)

For each unused parameter, rename to `_`:
- If the function is an interface implementation, the parameter must remain but can be `_`
- If the function is standalone and the parameter is truly unused, consider removing it
- For exported functions, removing parameters is a breaking change — use `_` instead

This is the largest single batch. Can be split by package:
- `internal/agent/` (~200 issues)
- `internal/tui/` (~100 issues)
- `internal/config/`, `internal/llm/`, others (~140 issues)

### 2f. Fix revive redefines-builtin-id (~15 issues)

Rename variables that shadow built-in functions:
- `min` → `minVal`, `minTokens`, etc.
- `max` → `maxVal`, `maxTokens`, etc.
- `copy` → `msgCopy`, `src`, etc.
- `cap` → `capacity` (partially done already)

### 2g. Fix small remaining items (4 issues)

- `nilerr` (3): Add `//nolint:nilerr // intentional: stale PID cleanup returns nil to allow startup` to `internal/daemon/daemon.go:462,469,475`
- `modernize` (2): Add `//nolint:modernize // omitzero already applied` to `internal/task/amendment.go:35` and `internal/task/task.go:44`
- `rowserrcheck` (1): Add `rows.Err()` check in `pkg/sqlite/pool.go:260`
- `prealloc` (1): Preallocate `lines` in `internal/selfimprove/detector_test.go:222`

---

## Phase 3: Per-Site gosec Suppressions (visible in code review)

Keep all gosec rules enabled. For each issue, either fix the code or add a `//nolint:gosec // reason` comment. No blanket config excludes.

### 3a. File permissions (G301/G306/G302) — ~90 issues

**Fix sensitive files to use restrictive permissions:**
- `internal/agent/workspace.go:83,167,193` — workspace plan files may contain task data → change to 0600
- `internal/agent/q/q_agent.go:397` — agent state file → change to 0600
- `cmd/meept/main.go:51` — PID file → change to 0600

**Add `//nolint` for standard permissions (config, data, logs are user-readable by design):**
- `internal/agent/workspace.go:68,312` — directory creation for task workspace → `//nolint:gosec // task workspace dirs are user-readable`
- `cmd/gendoc/main.go:229` — doc output directory → `//nolint:gosec // generated docs are public`
- All remaining `os.WriteFile` with 0644 for config/data files

### 3b. Hardcoded credentials false positives (G101) — ~15 issues

These flag field names like `"AccessToken"`, `"api_key"`, `"credentials"` in struct definitions and JSON tags. No actual secrets are hardcoded.

**Action:** Add `//nolint:gosec // field name, not a secret` at each site.

### 3c. Goroutine context (G118) — ~15 issues

**Fix ~5 that should propagate context:**
- `internal/agent/loop.go:937,1477,1616,1878` — goroutines that could accept request context for cancellation

**Add `//nolint` for intentional background goroutines:**
- `internal/agent/dispatcher.go:580` — event publishing goroutine (outlives request)
- Background cleanup workers, metrics recorders

### 3d. SQL string formatting (G201/G202) — 8 issues

These are real injection risks. Fix with parameterized queries:

| File:Line | Issue | Fix |
|-----------|-------|-----|
| `internal/memory/ftstore.go:363` | SQL concatenation | Parameterize query |
| `internal/shadow/store_sqlite.go:263` | SQL concatenation | Parameterize query |
| `internal/shadow/store_sqlite.go:379` | SQL concatenation | Parameterize query |
| `internal/shadow/store_sqlite.go:821` | SQL concatenation | Parameterize query |
| `internal/shadow/store_sqlite.go:893` | SQL concatenation | Parameterize query |
| `internal/shadow/store_sqlite.go:939` | SQL concatenation | Parameterize query |
| `internal/session/store_sqlite.go:165` | SQL formatting | Parameterize query |
| `internal/session/store_sqlite.go:539` | SQL formatting | Parameterize query |

### 3e. Subprocess with tainted input (G204) — 4 issues

| File:Line | Fix |
|-----------|-----|
| `internal/daemon/launchd.go:225,289,307` | Validate plist path is under `~/Library/LaunchAgents/` before exec |
| `internal/selfimprove/validator.go:141` | Validate/sanitize LLM-generated command before exec |

### 3f. Other security issues — 5 issues

| File:Line | Rule | Fix |
|-----------|------|-----|
| `internal/security/tls.go:136` | G402: InsecureSkipVerify | Add `//nolint:gosec // development mode only; prod uses proper TLS` or make configurable |
| `internal/shadow/middleware.go:125` | G404: math/rand | Replace with `crypto/rand` or `math/rand/v2` |
| `internal/agent/q/agent_designer.go:112` | G602: slice out of range | Add bounds check before indexing |
| `cmd/meept/calendar.go:107` | G112: Missing ReadHeaderTimeout | Add `ReadHeaderTimeout: 10 * time.Second` to `http.Server` |
| `internal/selfimprove/detector.go:209` | G122: TOCTOU | Add `//nolint:gosec // WalkDir stat is advisory, not security-critical` or use `os.Stat` directly |

### 3g. Add `//nolint` for revive `exported` stutter (~40 types)

Keep the revive `exported` rule enabled (it catches real naming issues in new code). For the 40 established API types that stutter with their package name, add individual `//nolint:revive` comments:

```go
// AgentConfig holds agent configuration.
//nolint:revive // stutter with package name is intentional for API clarity
type AgentConfig struct { ... }
```

This makes each suppression a conscious, documented decision visible in code review.

---

## Phase 4: goconst Production Code

After Phase 1b reduces test noise, extract constants for the remaining production code:

### 4a. Extract agent ID constants

`internal/config/agents.go` — `"dispatcher"`, `"executor"`, `"analyst"`, `"coder"`, `"debugger"`, `"planner"`, `"committer"`, `"scheduler"` each appear 3-8 times.

Create a constants block:
```go
const (
    AgentDispatcher = "dispatcher"
    AgentExecutor   = "executor"
    // ...
)
```

### 4b. Extract log level constants

`internal/config/schema.go` — `"INFO"`, `"DEBUG"`, `"WARN"`, `"ERROR"` each appear 3-7 times.

Use existing constants or define:
```go
const (
    LogLevelInfo  = "INFO"
    LogLevelDebug = "DEBUG"
    LogLevelWarn  = "WARN"
    LogLevelError = "ERROR"
)
```

### 4c. Extract preset name constants

`internal/config/presets.go` — `"development"`, `"debugging"`, `"planning"`, `"creative"`, `"research"`, `"fast"`, `"detailed"` each appear 3+ times.

---

## Execution Strategy

```
Phase 1 (config) ──→ Run immediately (2 changes in .golangci.yml)
         │
Phase 2 (mechanical) ──→ Run in parallel via subagents:
         │              2a+2b+2g: small batch (1 agent)
         │              2c+2d: errcheck (2 agents: prod + tests)
         │              2e: revive unused-param (3 agents by package)
         │              2f: revive redefines (1 agent)
         │
Phase 3 (gosec per-site) ──→ Run in parallel via subagents:
         │              3a+3b+3c: permissions + false positives (1 agent)
         │              3d+3e+3f: real security fixes (1 agent, needs care)
         │              3g: revive exported nolint comments (1 agent)
         │
Phase 4 (goconst) ──→ Run after Phase 1b reduces noise
```

## Expected Outcome

| Phase | Issues Resolved | Remaining |
|-------|----------------|-----------|
| Start | — | 3,031 |
| Phase 1 (config) | 45 (contextcheck) | 2,986 |
| Phase 2 (mechanical) | ~750 | ~2,236 |
| Phase 3 (gosec + revive exported) | ~286 + ~40 | ~1,910 |
| Phase 4 (goconst prod) | ~150 | ~1,760 |

If Phase 1b fixes the `ignore-tests` issue: remaining drops to ~60 (only goconst production constants, residual revive empty-block/blank-imports, and gosec G115 integer overflow).

If Phase 1b doesn't resolve test noise: remaining ~1,760 are goconst test-string warnings that need either higher `min-occurrences` threshold or test helper refactoring.

## Why This Approach Over Blanket Suppression

| Approach | Pros | Cons |
|----------|------|------|
| **Blanket config disable** (original plan) | Fast, fewer lines changed | Hides real issues, no code review trail, new code inherits suppressions |
| **Per-site `//nolint`** (this plan) | Every suppression is documented and reviewed, new code gets clean warnings, can be audited later | More lines of code, slower to implement |
| **Hybrid** (this plan) | contextcheck disabled in config (justified 100% FP rate), everything else per-site | Best balance of pragmatism and accountability |
