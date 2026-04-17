# Meept — Bugs & Gaps Audit

**Date**: 2026-04-16
**Scope**: `cmd/`, `internal/` (excluding `archive/` legacy Python and third-party vendored code)
**Nature**: Diagnostic. Each entry cites `file:line` and is verified against current source. This document describes what exists today; remediation is out of scope.

---

## Remediation status (2026-04-17)

37 of 38 findings have been remediated in a single remediation pass. Scope reductions are explicitly called out.

| Severity | Original | Resolved | Deferred |
|----------|---------:|---------:|---------:|
| CRITICAL |        2 |        2 |        0 |
| HIGH     |        9 |        9 |        0 |
| MEDIUM   |       19 |       18 |        1 |
| LOW      |        8 |        8 |        0 |
| **Total** |      **38** |     **37** |      **1** |

**Deferred:**
- **#28 — MEDIUM — security override matching**: the three-strategy cascade is retained. Tightening to strict glob/exact evaluation would silently alter behaviour for any deployed overrides; migration must be opt-in.

**Scope reductions baked into the remediation:**
- **#16 MergeRelated** — renamed/documented rather than re-implemented with embeddings (full semantic clustering is a feature, not a bug).
- **#32 cancelTask** — implemented as a state flip via a new `task.cancel` RPC; interruption of in-flight work is out of scope.
- **#33 Skills registry lazy loading** — TODO replaced with an explanatory comment; the hot path is already covered by `SkillIndex`/`SkillLoader`.

**Pre-existing, unrelated failures surfaced during verification** (NOT caused by the remediation, flagged for follow-up):
- `internal/tui/tui_teatest_test.go` — both teatest cases time out on 2026-04-16 base.
- `internal/tools/builtin/tool_web_search_test.go` — `parseDuckDuckGoHTML` panics because the regex uses `(?=...)` lookahead, unsupported by Go's RE2.
- `internal/lite` test build under `-race` SIGSEGVs in `cmd/link`: Go 1.26.1 toolchain bug on arm64.
- `tests/integration` MCP tests time out — external MCP servers not available in CI.

---

## Executive summary

| Severity | Count |
|----------|------:|
| CRITICAL | 2 |
| HIGH     | 9 |
| MEDIUM   | 19 |
| LOW      | 8 |
| **Total entries** | **38** |

Findings are bucketed as: `bug` (wrong/broken), `stub` (unimplemented), `partial` (incomplete relative to surrounding feature), `unwired` (declared + set but never read), `silent-error` (error suppressed with `_`), `security` (fail-open or weak check), `test-gap` (removed/skipped coverage).

**Top risks**

1. **Fail-open security in path rule checks** (`internal/security/engine.go:449`) — DB errors default to `allow`.
2. **Rollback loses directory path** (`internal/selfimprove/applier.go:186`) — rollback target is project root, not original file location; silent data corruption risk.
3. **`publishStatus` is a no-op** (`internal/selfimprove/controller.go:359`) — 7 callers publish nothing; upstream observability is dark.
4. **AnthropicClient metrics/timeout wiring is dead** (`internal/llm/anthropic.go:43-85`, `internal/llm/broker.go:125`) — setters exist, fields never read; adaptive timeouts and latency metrics only apply to OpenAI-compat path.
5. **Circuit breaker does not count validator/applier failures** (`internal/selfimprove/controller.go:220, 256`) — `continue` paths bypass `recordFailure`.
6. **Self-improve state persistence is a stub** (`internal/selfimprove/controller.go:392-407`) — `loadState` deserializes JSON then discards it.
7. **8 code-intel tool constructors `panic` on nil manager** — daemon-crash risk if dependency injection misfires.

---

## Methodology

- Evidence gathered by directly reading referenced files and by `Grep`/`Glob` sweeps over `internal/` and `cmd/`.
- Five parallel exploration subagents covered: phase-1 re-verification, deep-scan of session/task/shadow/context/tui, orphan/wiring analysis, LLM/agent deep-dive, tools/skills/security deep-dive.
- Spot-checks: 12 claims were verified against current source by the orchestrator before inclusion.
- Excluded: `archive/` (legacy Python), vendored deps, style-only issues, perf profiling beyond what the code already flags with TODOs.

Each entry follows the schema:

```
### Title
- File: path:line
- Severity
- Class
- Evidence excerpt
- Observed: current behaviour
- Gap: what is missing or intended
```

---

## CRITICAL

### 1. Fail-open on DB error in `checkPath`
- **File**: `internal/security/engine.go:449-450`
- **Severity**: CRITICAL
- **Class**: security
- **Evidence**:
  ```go
  if err != nil {
      e.logger.Error("Failed to query allow path rules", "error", err)
      return nil
  }
  ```
- **Observed**: When the SQLite query for path allow-rules fails, `checkPath` returns `nil`, which callers treat as "no decision → allow".
- **Gap**: Security-critical path should fail closed on query error. Expected: return a deny `Decision` or surface the error.

### 2. Rollback writes to project root, not original directory
- **File**: `internal/selfimprove/applier.go:186-189`
- **Severity**: CRITICAL
- **Class**: partial (data-loss risk)
- **Evidence**:
  ```go
  originalPath := strings.TrimSuffix(applied.BackupPath, ".backup")
  originalPath = filepath.Join(a.projectRoot, filepath.Base(originalPath))
  ```
- **Observed**: `filepath.Base` strips directory components. A backup of `internal/pkg/foo.go` rolls back to `<projectRoot>/foo.go`, leaving the real file unrestored and creating a stray file at the root.
- **Gap**: Needs to preserve the original relative path (stored in the `AppliedFix` record or encoded in the backup filename). Current code comment already admits: *"For now, we'll use a convention"*.

---

## HIGH

### 3. `publishStatus` is an empty no-op with 7 callers
- **File**: `internal/selfimprove/controller.go:359-365`
- **Severity**: HIGH
- **Class**: stub
- **Evidence**:
  ```go
  func (c *Controller) publishStatus(phase string, data any) {
      if c.bus == nil {
          return
      }
      // Publish status update to bus
      // Implementation depends on bus interface
  }
  ```
- **Observed**: Body is empty; seven call sites (lines 115, 126, 145, 176, 214, 237, 272) trigger zero bus messages.
- **Gap**: Intended to emit status events for UI/scheduler observers. Nothing is published today.

### 4. AnthropicClient `metricsStore` / `timeoutCalc` fields never read
- **File**: `internal/llm/anthropic.go:43-44, 72-85`
- **Severity**: HIGH
- **Class**: unwired
- **Evidence**:
  ```go
  metricsStore  *metrics.Store
  timeoutCalc   *metrics.Calculator

  func WithAnthropicMetricsStore(store *metrics.Store) AnthropicClientOption { ... }
  func WithAnthropicTimeoutCalculator(calc *metrics.Calculator) AnthropicClientOption { ... }
  ```
- **Observed**: Setters exist; fields are populated by broker config but never consulted anywhere in `anthropic.go`. The sibling `Client` in `internal/llm/client.go:153-159, 494` actively uses both.
- **Gap**: Anthropic path has no adaptive timeout and no metrics recording. Broker options are silently discarded for this provider.

### 5. Broker defers metrics/timeout injection entirely
- **File**: `internal/llm/broker.go:125-127`
- **Severity**: HIGH
- **Class**: deferred-phase
- **Evidence**:
  ```go
  // TODO: Inject metrics and timeout calculator options
  // (deferred to Phase 6 when Client/AnthropicClient support these options)
  return chatter
  ```
- **Observed**: `newChatterFor` builds a Chatter without calling `WithMetricsStore` / `WithTimeoutCalculator` / `WithAnthropicMetricsStore`, etc. Phase 6 never landed.
- **Gap**: Even where Client supports the options, the broker does not propagate them; metrics and adaptive timeouts are untested from the broker entry point.

### 6. Circuit breaker bypassed in validation and application phases
- **File**: `internal/selfimprove/controller.go:220, 256`
- **Severity**: HIGH
- **Class**: bug
- **Evidence**:
  ```go
  // line 220 (validation)
  if err != nil { continue }
  // line 256 (application)
  if err != nil { continue }
  ```
- **Observed**: The analysis phase (lines 161, 197) correctly calls `c.recordFailure(...)` before `continue`, but validation and application skip that call. Consecutive failures in those phases never trip the circuit breaker.
- **Gap**: All error-return paths in the controller loop should record failures symmetrically.

### 7. Self-improve `loadState` is a stub
- **File**: `internal/selfimprove/controller.go:392-407`
- **Severity**: HIGH
- **Class**: stub
- **Evidence**:
  ```go
  var state map[string]json.RawMessage
  if err := json.Unmarshal(data, &state); err != nil { return err }
  // Load each component (simplified - would need proper deserialization)
  c.logger.Info("loaded state from disk")
  return nil
  ```
- **Observed**: State is deserialized into `state` then discarded. `c.issues`, `c.analyses`, cycle history are never populated on restart. Companion `saveState` at line 372 does write real data.
- **Gap**: After daemon restart, the self-improve subsystem forgets everything, but the log line falsely claims state was loaded.

### 8. `task.Store.List` returns partial results on scan error and skips `rows.Err()`
- **File**: `internal/task/store.go:283-292`
- **Severity**: HIGH
- **Class**: bug
- **Evidence**:
  ```go
  for rows.Next() {
      task, err := s.scanTaskRows(rows)
      if err != nil {
          s.logger.Error("Failed to scan task", "error", err)
          continue
      }
      tasks = append(tasks, task)
  }
  return tasks, nil
  ```
- **Observed**: Scan failures are logged but the function continues; iteration errors from `rows.Err()` are never checked. Partial lists are returned as successful.
- **Gap**: Caller cannot distinguish a healthy empty set from a partially-failed query. Needs `if err := rows.Err(); err != nil { return nil, err }` and a decision about scan-error propagation.

### 9. `task.Store.ListActive` additionally swallows scan errors without logging
- **File**: `internal/task/store.go:310-319`
- **Severity**: HIGH
- **Class**: silent-error
- **Evidence**:
  ```go
  for rows.Next() {
      task, err := s.scanTaskRows(rows)
      if err != nil { continue }
      tasks = append(tasks, task)
  }
  return tasks, nil
  ```
- **Observed**: Same shape as `List` but without even a log line, and still no `rows.Err()` check.
- **Gap**: Same as #8; additionally, failures are completely invisible.

### 10. `session.SQLiteStore.Create` returns `nil` on DB error
- **File**: `internal/session/store_sqlite.go:124-127`
- **Severity**: HIGH
- **Class**: bug
- **Evidence**:
  ```go
  if err != nil {
      s.logger.Error("Failed to create session", "error", err)
      return nil
  }
  ```
- **Observed**: Function signature is `*Session`; failures log and return nil, indistinguishable from success paths to callers that do not null-check.
- **Gap**: Either signature must change to `(*Session, error)` or callers must universally nil-check. Today several callers assume a non-nil return.

### 11. `session.SQLiteStore.List` returns `nil` on query error
- **File**: `internal/session/store_sqlite.go:225-227`
- **Severity**: HIGH
- **Class**: bug
- **Evidence**:
  ```go
  if err != nil {
      s.logger.Error("Failed to list sessions", "error", err)
      return nil
  }
  ```
- **Observed**: A DB error and an empty table are indistinguishable at the call site.
- **Gap**: Function should return `([]*Session, error)`.

---

## MEDIUM

### 12. Eight code-intel tool constructors panic on nil manager
- **Files**:
  - `internal/code/tools/ast_parse.go:21`
  - `internal/code/tools/ast_query.go:20`
  - `internal/code/tools/ast_symbols.go:20`
  - `internal/code/tools/lsp_definition.go:21`
  - `internal/code/tools/lsp_diagnostics.go:26`
  - `internal/code/tools/lsp_hover.go:21`
  - `internal/code/tools/lsp_references.go:21`
  - `internal/code/tools/lsp_symbols.go:21`
- **Severity**: MEDIUM
- **Class**: bug (idiom violation)
- **Evidence** (representative):
  ```go
  panic("ast.ParserManager cannot be nil")
  // or
  panic("lsp.Manager cannot be nil")
  ```
- **Observed**: A nil dependency crashes the daemon instead of returning an error during wiring.
- **Gap**: Idiomatic Go constructors return `(T, error)`; current style turns a wiring bug into a full daemon panic.

### 13. Memory store constructors panic on FTS init failure
- **Files**: `internal/memory/episodic.go:95`, `internal/memory/task.go:107`
- **Severity**: MEDIUM
- **Class**: bug (initialization fragility)
- **Evidence**:
  ```go
  panic(fmt.Sprintf("failed to create episodic store: %v", err))
  // and
  panic(fmt.Sprintf("failed to create task store: %v", err))
  ```
- **Observed**: FTS5 index failure (disk full, permissions, schema drift) crashes the daemon at startup with a panic rather than a graceful startup error.
- **Gap**: Propagate the error so daemon can report a clean failure or degrade.

### 14. Permission-override usage-count UPDATE silently suppresses errors
- **File**: `internal/security/engine.go:541-545`
- **Severity**: MEDIUM
- **Class**: silent-error
- **Evidence**:
  ```go
  _, _ = e.db.Exec(`
      UPDATE permission_overrides
      SET usage_count = usage_count + 1,
          updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
      WHERE id = ?`, id)
  ```
- **Observed**: Both return values are discarded. A failed write leaves the audit counter stale with no log entry.
- **Gap**: At minimum, log the error; ideally emit an audit event on failure.

### 15. Async metrics record uses `context.Background()` and swallows the error
- **File**: `internal/llm/client.go:494`
- **Severity**: MEDIUM
- **Class**: silent-error
- **Evidence**:
  ```go
  go func() {
      ...
      _ = c.metricsStore.Record(context.Background(), record)
  }()
  ```
- **Observed**: Detached from request context (never cancellable) and any failure to persist metrics is discarded.
- **Gap**: Either log/account the record error, or drop metrics during shutdown signalling; `context.Background()` prevents the store from draining.

### 16. `MergeRelated` is a date-grouping placeholder
- **File**: `internal/memory/consolidation.go:298-301`
- **Severity**: MEDIUM
- **Class**: partial
- **Evidence**:
  ```go
  func (c *Consolidator) MergeRelated(ctx context.Context, memories []MemoryResult) ([]Summary, error) {
      // For now, fall back to date-based grouping
      // A future version could use embeddings or keyword clustering
      return c.summarizeByDate(memories), nil
  }
  ```
- **Observed**: Name implies semantic merge; implementation is strictly date-bucketing.
- **Gap**: Either rename to reflect capability or implement embedding/keyword clustering.

### 17. `clawskills` zip extraction relies only on prefix check
- **File**: `internal/clawskills/installer.go:237-245`
- **Severity**: MEDIUM
- **Class**: security
- **Evidence**:
  ```go
  name := filepath.Clean(file.Name)
  if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
      i.logger.Warn("skipping suspicious path in zip", "path", file.Name)
      continue
  }
  targetPath := filepath.Join(targetDir, name)
  ```
- **Observed**: `filepath.Clean` canonicalises, but some entries can still escape `targetDir` via symlinks or exotic relative paths; no resolved-path containment check.
- **Gap**: After constructing `targetPath`, verify `strings.HasPrefix(absTarget, absTargetDir+string(filepath.Separator))` before writing.

### 18. Shadow store migrations ignore ALL errors, not just "column exists"
- **File**: `internal/shadow/store_sqlite.go:162-170, 744-757`
- **Severity**: MEDIUM
- **Class**: silent-error
- **Evidence**:
  ```go
  _, err = s.db.Exec(`ALTER TABLE shadow_records ADD COLUMN exported_at TEXT;`)
  _ = err
  _, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_shadow_records_exported ...`)
  ```
  ```go
  _, err := s.db.Exec(`ALTER TABLE fewshot_examples ADD COLUMN tags TEXT DEFAULT '';`)
  _ = err
  _, err = s.db.Exec(`ALTER TABLE fewshot_examples ADD COLUMN last_used_at TEXT;`)
  _ = err
  ```
- **Observed**: Comment says "Ignore if already exists", but the code ignores every error class (permissions, disk full, schema mismatch).
- **Gap**: Match on "duplicate column" and propagate other errors.

### 19. Filesystem tool `listDirect` silently returns `nil, false` on read error
- **File**: `internal/tools/builtin/filesystem.go:387-391`
- **Severity**: MEDIUM
- **Class**: silent-error
- **Evidence**:
  ```go
  dirEntries, err := os.ReadDir(dir)
  if err != nil {
      return nil, false
  }
  ```
- **Observed**: Permission-denied, missing, or transient I/O errors look indistinguishable from an empty directory to callers.
- **Gap**: Return the error or at least log it; caller currently cannot differentiate.

### 20. Filesystem tool `listRecursive` swallows WalkDir errors
- **File**: `internal/tools/builtin/filesystem.go:426-429`
- **Severity**: MEDIUM
- **Class**: silent-error
- **Evidence**:
  ```go
  err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
      if err != nil {
          return nil // Skip errors (permission denied, etc.)
      }
  ```
- **Observed**: Denied subtrees are invisibly skipped, giving an apparently-complete listing.
- **Gap**: At least accumulate partial errors and surface them alongside entries.

### 21. MCP client returns error result marked "successful"
- **File**: `internal/tools/mcp/client.go:214, 219`
- **Severity**: MEDIUM
- **Class**: silent-error
- **Evidence**:
  ```go
  resp, err := c.request(ctx, "tools/call", params)
  if err != nil {
      return tools.NewErrorResult(err.Error()), nil
  }
  result, err := ExtractResult[*CallToolResult](resp)
  if err != nil {
      return tools.NewErrorResult(err.Error()), nil
  }
  ```
- **Observed**: The Go `error` return is `nil` even when the RPC failed; the calling tool registry treats this as a successful tool execution with an error-text body.
- **Gap**: Either return the underlying error, or document and test that all callers inspect `result.IsError()`.

### 22. Context firewall silently falls back to unsummarized when summarization fails
- **File**: `internal/llm/context_firewall.go:233-241`
- **Severity**: MEDIUM
- **Class**: silent-error
- **Evidence**:
  ```go
  summarized, err := f.summarizeOldHistory(ctx, result)
  if err != nil {
      f.logger.Debug("summarization failed", "error", err)
      // Continue without summarization
  } else {
      result = summarized
  }
  ```
- **Observed**: Debug-level log only; caller continues with potentially over-budget messages and no escalation.
- **Gap**: At least warn-level log with metrics; callers need a way to know summarization failed before sending to LLM.

### 23. Context firewall `dropOldContext` discards history without a signal
- **File**: `internal/llm/context_firewall.go:247-284`
- **Severity**: MEDIUM
- **Class**: partial
- **Evidence**: Function keeps system prompt + last 2 messages; everything else is discarded silently when hard cap hit.
- **Observed**: No event, metric, or warning is emitted when the firewall truncates. Truncation is undetectable downstream.
- **Gap**: Emit a dropped-message count and summary event; this is destructive behaviour with no feedback channel.

### 24. Unsafe git args from user-controlled fields in `createCommit`
- **File**: `internal/selfimprove/applier.go:276, 283-286`
- **Severity**: MEDIUM
- **Class**: security
- **Evidence**:
  ```go
  cmd := exec.Command("git", "add", fix.FilePath)
  ...
  message := fmt.Sprintf("fix(selfimprove): %s\n\nFix ID: %s\nRisk: %s",
      fix.Description, fix.ID, fix.Risk)
  cmd = exec.Command("git", "commit", "-m", message)
  ```
- **Observed**: `fix.FilePath` is passed directly — if it begins with `-` or contains `..`, git interprets it as a flag or escapes the repo. `fix.Description` is embedded in the commit message without sanitisation.
- **Gap**: Prefix `--` before `fix.FilePath`, and validate path is within `projectRoot`. Commit message is lower risk but should be sanitised.

### 25. Artifact scanner uses `fmt.Printf` instead of the project logger
- **File**: `internal/context/artifact_scanner.go:139, 178`
- **Severity**: MEDIUM
- **Class**: partial
- **Evidence**:
  ```go
  fmt.Printf("Warning: failed to parse skill file %s: %v\n", path, err)
  ```
- **Observed**: Bypasses the `log/slog` pipeline; warnings cannot be redirected or filtered.
- **Gap**: Use the package's slog logger; consistency with the rest of the codebase.

### 26. Session `scanSessionRows` discards `json.Unmarshal` errors and `RowsAffected` error
- **File**: `internal/session/store_sqlite.go:206-207, 266-267, 286`
- **Severity**: MEDIUM
- **Class**: silent-error
- **Evidence**:
  ```go
  json.Unmarshal([]byte(attachedJSON), &session.AttachedClients)
  json.Unmarshal([]byte(workersJSON), &session.WorkerIDs)
  ...
  rows, _ := result.RowsAffected()
  ```
- **Observed**: Corrupt JSON in the DB yields an empty slice and no warning. The `RowsAffected` error path is blanked.
- **Gap**: Log and possibly flag the row as corrupt.

### 27. `session.UpdateActivity` has no error return
- **File**: `internal/session/store_sqlite.go:336-343`
- **Severity**: MEDIUM
- **Class**: partial
- **Evidence**: Signature `UpdateActivity(sessionID string)` with no return.
- **Observed**: Any DB update failure is invisible; caller cannot retry.
- **Gap**: Change signature to return `error`.

### 28. Override decision pattern-match logic overlaps multiple strategies
- **File**: `internal/security/engine.go:512-538`
- **Severity**: MEDIUM
- **Class**: bug
- **Evidence**: Combines JSON-substring match, `filepath.Match` over each value, and trimmed substring match in a nested chain; also discards `json.Marshal` and `filepath.Match` errors with `_`.
- **Observed**: The precedence between the three strategies is not explicit; a value matching only by substring-of-trimmed-pattern still returns true.
- **Gap**: The intent is unclear and has security implications — any one strategy permitting yields an override. Needs tightening and tests.

### 29. `isolated metricsStore.Record` context detach in `ChatWithProgress`
- **File**: `internal/llm/client.go` async recording (see #15). Noted separately because the same pattern appears on the non-streaming path.
- **Severity**: MEDIUM
- **Class**: silent-error
- **Evidence**: Identical `_ = c.metricsStore.Record(context.Background(), record)` style.
- **Observed**: Duplicated silent-error pattern.
- **Gap**: Central helper with logging; consistent context handling.

### 30. `TacticalScheduler.handleReviewResult` duplicates `ReviewManager.HandleReviewResult`
- **File**: `internal/agent/tactical.go:307-357`, `internal/agent/review_manager.go:249-287`
- **Severity**: MEDIUM
- **Class**: partial
- **Evidence**: Two functions with overlapping switch on `result.Status`.
- **Observed**: Tactical scheduler fetches a review via `ReviewManager.ReviewStep` then reimplements the post-processing. Future changes risk divergence.
- **Gap**: Delegate to `ReviewManager.HandleReviewResult`.

---

## LOW

### 31. Status handler hardcodes `uptime_seconds: 0.0`
- **File**: `internal/rpc/server.go:307`
- **Severity**: LOW
- **Class**: partial
- **Evidence**:
  ```go
  "uptime_seconds":     0.0, // TODO: track actual uptime
  ```
- **Observed**: `startTime` is tracked elsewhere; the status payload always reports zero.
- **Gap**: Compute `time.Since(startTime).Seconds()`.

### 32. Task `cancelTask` returns "not yet implemented"
- **File**: `internal/lite/tasks.go:115`
- **Severity**: LOW
- **Class**: stub
- **Evidence**:
  ```go
  // TODO: Implement when task.cancel RPC method is available
  return fmt.Errorf("task cancellation not yet implemented")
  ```
- **Observed**: Depends on an RPC method that has not shipped.
- **Gap**: User-facing cancellation is unavailable.

### 33. Skills registry eagerly loads all bodies
- **File**: `internal/daemon/components.go:1719`
- **Severity**: LOW
- **Class**: partial (perf)
- **Evidence**:
  ```go
  // TODO: Consider making registry also use lazy loading
  ```
- **Observed**: All skill bodies read at startup; cold-start cost scales with `~/.meept/clawskills` size.
- **Gap**: Lazy-load pattern exists elsewhere; apply to registry.

### 34. Vim-mode `0` key is a declared stub
- **File**: `internal/tui/vim/mode.go:156`
- **Severity**: LOW
- **Class**: stub
- **Evidence**:
  ```go
  if s.Count == 0 && key == "0" {
      // 0 without count means move to start of line (not implemented here)
      return Action{Type: ActionNone}, false
  }
  ```
- **Observed**: Pressing `0` in vim-mode is an explicit no-op.
- **Gap**: Either implement or document the omission outside a `// not implemented here` comment.

### 35. `NewReviewManager` has no callers outside its own package tests
- **File**: `internal/agent/review_manager.go:38`
- **Severity**: LOW
- **Class**: unwired (orphan)
- **Evidence**: Repo-wide grep for `NewReviewManager(` returns only the definition line (plus the preceding docstring line).
- **Observed**: The type has methods, but no production callsite constructs one. `TacticalScheduler` uses a `*ReviewManager` field (see #30) but it is populated through a setter that itself is never invoked from `cmd/` or `internal/daemon/components.go`.
- **Gap**: Either wire it (e.g., from daemon components) or remove.

### 36. `TestDispatcher_FallbackChain` removed, not rewritten
- **File**: `internal/agent/llm_classifier_test.go:343-345`
- **Severity**: LOW
- **Class**: test-gap
- **Evidence**:
  ```go
  // TestDispatcher_FallbackChain removed: it relied on a non-existent function-field
  // layout for *LLMClassifier.Classify and has been dead/non-building.
  // TODO: rewrite using the classifierFn interface if fallback-chain coverage is desired.
  ```
- **Observed**: Coverage of classifier fallback behaviour is absent.
- **Gap**: Rewrite using the new interface, or accept the coverage loss deliberately.

### 37. Shell-tool risk default is coarse
- **File**: `internal/tools/builtin/shell.go:274-276`
- **Severity**: LOW
- **Class**: partial
- **Evidence**: `return RiskHigh` for any unknown command.
- **Observed**: Legitimate domain-specific CLIs trigger HIGH-risk prompts.
- **Gap**: Allow a user-configurable allowlist, or add a MEDIUM default.

### 38. LLM classifier timeout is a hardcoded constant
- **File**: `internal/agent/llm_classifier.go:15`
- **Severity**: LOW
- **Class**: stub
- **Evidence**:
  ```go
  const (
      classifierTimeout = 5 * time.Second
  )
  ```
- **Observed**: Cannot be tuned per deployment or per classifier model.
- **Gap**: Surface on `LLMClassifierConfig`.

---

## Test gaps

| File:Line | Gap |
|-----------|-----|
| `internal/agent/llm_classifier_test.go:343-345` | `TestDispatcher_FallbackChain` removed, no replacement (see #36) |
| `tests/integration/mcp_test.go:54, 143, 216, 277, 322, 406, 496, 545` | Eight MCP integration tests gated behind `testing.Short()` → skipped in default CI runs |

---

## Appendix A: verified TODO/FIXME inventory

Actionable TODO/FIXME comments in production Go files (`internal/` + `cmd/`). Excludes matches inside `_test.go` files, regex pattern-definitions inside detectors, and the prompt-sanitizer's example strings.

| File:Line | Class | Description |
|-----------|-------|-------------|
| `internal/llm/broker.go:125` | deferred-feature | Inject metrics and timeout calculator options (Phase 6 deferred) |
| `internal/lite/tasks.go:115` | deferred-feature | Implement task cancellation RPC method |
| `internal/daemon/components.go:1719` | cleanup | Consider making registry also use lazy loading |
| `internal/rpc/server.go:307` | perf | Track actual daemon uptime |
| `internal/selfimprove/controller.go:362` | deferred-feature | Bus publish implementation (body empty) |
| `internal/selfimprove/applier.go:186` | partial | "For now, we'll use a convention" — backup path reconstruction |
| `internal/selfimprove/controller.go:404` | stub | State deserialization simplified, never applied |
| `internal/memory/consolidation.go:299` | partial | Date-grouping fallback; future embedding clustering |
| `internal/agent/llm_classifier_test.go:345` | test | Rewrite fallback-chain test using classifierFn interface |

A broader `grep -n 'TODO\|FIXME\|HACK'` across `internal/` and `cmd/` returns additional matches in pattern-definition strings (e.g., the self-improve *detector* intentionally searches for the words "TODO" and "FIXME" in source files, so many grep hits are data, not defects). Those are excluded here.

---

## Appendix B: claims that were investigated and refuted

These were raised during Phase-1 intake but did not survive verification. They are recorded here so that future audits do not relitigate:

- `internal/scheduler/jobs.go:388` — nil `j.bus` panic risk. **Refuted**: `ReminderJob` constructors validate inputs; the bus pointer is always non-nil at Execute time.
- `internal/comm/web/server.go` — handler stubs / 501 responses. **Refuted**: every handler is wired; missing optional services return a structured "not configured" payload.
- `internal/scheduler/rpc.go` — bare `return nil` swallowing results. **Refuted**: all RPC handlers populate structured maps or typed results.
- `internal/calendar/gcal.go` — write ops missing. **Refuted**: `CreateEvent`, `UpdateEvent`, `DeleteEvent` are all fully implemented with proper HTTP methods.
- `internal/memory/manager.go` prefetch cache — **Refuted**: cache is populated by `StartPrefetchService` + `SetPrefetchCallback`, and read by `GetCachedPrefetch`.
- `internal/clawskills/installer.go` partial-extract cleanup — **Confirmed present** (`os.RemoveAll(skillPath)` on verification failure at line 125); the weakness instead is in the zip entry-path check (see #17).
- `internal/selfimprove/learning.go` LearningPipeline wiring — **Refuted**: wired via `LearningConsolidatorAdapter` in `internal/daemon/components.go:243-253, 681-686`.
- `internal/comm/telegram/bot.go` allowed-users enforcement — **Refuted**: `Bot.isAllowed` is called on every incoming message at `internal/comm/telegram/bot.go:199-206`.
