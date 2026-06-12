# Review Gaps — Research & Design Investigation

> **For Claude:** This is a research plan, not an implementation plan. Each task produces findings and a recommended approach. Do NOT write code — only investigate and document.

**Goal:** Investigate 5 gaps that need design decisions before fixing. Each task produces: (a) findings from code investigation, (b) a recommended approach, (c) estimated fix complexity. No code changes until the user approves each recommended approach.

**Context:** These issues were identified by 7 parallel review agents during the full codebase review (2026-06-12). They require understanding intent, evaluating trade-offs, or profiling before a fix can be designed.

---

## Task 1: Investigate Reflection Loop Single-Iteration Behavior

**Severity:** MEDIUM
**Files to investigate:**
- `internal/agent/reflection.go` — loop body (lines 85-164)
- `internal/agent/orchestrator.go` — caller of reflection
- Any tests in `internal/agent/reflection_test.go`

**Problem:** The reflection loop iterates up to `MaxReflections` times, but every code path inside the loop returns before reaching the bottom of the for-loop body. If linters find errors → request fix and return. If linters pass but tests fail → request fix and return. If both pass → return with `Fixed = true`. There is no path that continues to the next iteration.

**Investigation questions:**
1. Was multi-iteration reflection ever implemented, or was `MaxReflections` aspirational?
2. What would a second iteration do differently? Would it re-run linters on the fixed code and then ask the LLM again?
3. Is there a risk of infinite loops if multi-iteration is enabled (LLM fix introduces new errors → loop forever)?
4. Do the callers (orchestrator) expect `PendingFix` to be applied and then re-entered?
5. What does the reflection flow look like end-to-end: who calls `Reflect`, who applies the `PendingFix`, who calls `Reflect` again?

**Deliverable:** A section documenting the current flow, whether multi-iteration makes sense, and a recommended approach (enable multi-iteration with a circuit breaker, or remove `MaxReflections` config and document single-iteration as intentional).

---

## Task 2: Investigate parseFixResponse Indiscriminate File Targeting

**Severity:** MEDIUM
**Files to investigate:**
- `internal/agent/reflection.go` — `parseFixResponse` (lines 295-319) and `applyFix` caller
- `internal/agent/orchestrator.go` — how `FixAttempt.Files` is consumed

**Problem:** `parseFixResponse` returns a `FixAttempt` with `targetFiles` set to ALL original files, regardless of whether the LLM response addresses those files. The file-reference check loop is a no-op (`continue` does nothing). When `applyFix` iterates over `fix.Files`, it writes the same content to every file.

**Investigation questions:**
1. How does `applyFix` actually use `FixAttempt.Files`? Does it write the same `FixText` to each file, or does it try to extract per-file edits?
2. What's the actual failure mode? Does it overwrite file contents with the full LLM response text?
3. Could we parse the LLM response for per-file code blocks (````filepath\n...code...````) and map them to the correct files?
4. Or should we just filter `targetFiles` to only include files the LLM response actually references?
5. What format does the LLM actually return fixes in? Read the prompt in `formatLintFixRequest` and `formatTestFixRequest`.

**Deliverable:** A section documenting the current fix application flow, what the LLM actually returns, and a recommended parsing strategy.

---

## Task 3: Investigate Security Hooks — Intended Enforcement Model

**Severity:** MEDIUM
**Files to investigate:**
- `internal/agent/security_hooks.go` — `checkFilePermission`, `checkNetworkPermission` (lines 84-132)
- `internal/security/` — existing `FenceChecker`, `PermissionChecker`, `Orchestrator`
- `internal/agent/security_hooks.go` — `scanShellCommand` (the one hook that actually works)

**Problem:** `checkFilePermission` and `checkNetworkPermission` log that they perform checks but always return `BlockResult{}` (not blocked). They don't validate path boundaries or check URL allowlists. Only `scanShellCommand` performs actual checks via Tirith.

**Investigation questions:**
1. What is the intended security model? Should file ops be restricted to project worktree boundaries? Should network ops be restricted to specific domains?
2. How does `FenceChecker` (already wired) relate to these hooks? Is there overlap?
3. What does `SecurityOrchestrator.Check()` already cover? Are these hooks redundant?
4. If they're meant to be real checks, what policy should they enforce? Read any security docs in `docs/` for clues.
5. Should these hooks be removed (redundant with FenceChecker) or implemented (defense-in-depth)?

**Deliverable:** A section documenting the existing security layers, any overlap, and a recommendation (remove redundant hooks, implement real checks, or document as intentional placeholders).

---

## Task 4: Investigate Streaming Parser Tool Call Delta Handling

**Severity:** MEDIUM
**Files to investigate:**
- `internal/llm/client.go` — `ChatWithDeltaCallback` streaming chunk parser (lines 843-1014)
- `internal/llm/anthropic.go` — Anthropic streaming parser
- OpenAI streaming protocol documentation for `delta.tool_calls` format

**Problem:** The streaming chunk struct only extracts `delta.content`. It does not parse `delta.tool_calls` from the SSE stream. When the model returns tool calls in streaming mode, they are silently dropped. The returned `Response` has empty `ToolCalls` and zero `Usage`.

**Investigation questions:**
1. What does the OpenAI streaming protocol send for tool calls? (SSE chunks with `delta.tool_calls[].function.name` and `delta.tool_calls[].function.arguments` deltas)
2. What does the Anthropic streaming protocol send? (`content_block_start`, `content_block_delta` with `tool_use` type)
3. Is the streaming parser used for agentic workflows (tool calls required), or only for display (text-only OK)?
4. What's the scope of the fix — just parse the deltas, or also accumulate them into complete tool calls?
5. Are there tests for the streaming parser that would need updating?

**Deliverable:** A section documenting the streaming protocol formats, which code paths use streaming, and a recommended implementation approach with estimated complexity.

---

## Task 5: Investigate TokenCache Growth and Eviction Strategy

**Severity:** LOW
**Files to investigate:**
- `internal/llm/tokenizer.go` — `TokenCache` struct (lines 88-118)
- Callers in `internal/llm/context_firewall.go` and `internal/llm/context_compactor.go`

**Problem:** `TokenCache` uses `sync.Map` with no eviction. Every unique string passed to `CountTokens` is cached forever. In long sessions with diverse inputs, this grows without bound.

**Investigation questions:**
1. What's the typical cache key size? (full message text? truncated hash?)
2. What's the value size? (just an int — 8 bytes)
3. What's the practical growth rate? Count calls per request to estimate entries per hour.
4. Is the cache even effective? What's the hit rate — are the same strings counted repeatedly?
5. What eviction strategies fit? Options:
   - **LRU with max size** (e.g., 10K entries, evict oldest)
   - **TTL-based** (expire after 5 minutes)
   - **Periodic purge** (clear entire cache every N minutes)
   - **Remove entirely** (if hit rate is low, the cache may not be worth the complexity)

**Deliverable:** A section documenting the cache's actual usage pattern, estimated memory footprint, and a recommended strategy with rationale.
