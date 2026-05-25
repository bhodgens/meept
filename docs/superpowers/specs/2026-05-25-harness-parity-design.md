# Harness Parity Specification

Closing the gap between Meept's orchestration depth and oh-my-pi's tool surface quality.

**Date**: 2026-05-25
**Status**: Draft
**Priority**: High — the edit interface is the single highest-ROI improvement available

---

## Problem Statement

Meept has strong multi-agent orchestration (dispatcher, strategic planner, tactical scheduler, message bus, collaborative review) but a weak tool surface. Oh-my-pi (omp) demonstrates that the tool surface — specifically the edit interface — matters more than model choice. Their hashline edit format improved Grok Code Fast 1 from 6.7% to 68.3% success rate with zero model changes.

Current Meept gaps:
- No incremental edit tool (whole-file overwrite only)
- No content-hash anchoring in file reads
- No secret obfuscation before LLM exposure
- No TT-SR stream rule enforcement
- No structured subagent output validation
- Limited LSP surface (5 ops vs omp's 14)
- No DAP debugging
- Shell execution via `/bin/sh -c` subprocess for every operation
- No prefix cache stabilization
- No tool result override or preview/approval flow

---

## Specification Sections

### 1. Hashline File Edit System

#### 1.1 file_read Enhancement

**Current**: `file_read` returns raw file content with no line numbers or hashes.

**Target**: Every line returned by `file_read` includes a short content hash tag.

**Output format**:
```
1:a3|package main
2:f1|
3:0e|import "fmt"
4:7b|
5:c2|func main() {
6:9d|    fmt.Println("hello")
7:4a|}
```

Format: `LINE_NUMBER:HASH|CONTENT` where `HASH` is a 2-character lowercase bigram.

**Hash algorithm**:
- Use `github.com/cespare/xxhash/v2` (already widely used in Go ecosystem, MIT licensed)
- Compute `xxhash.Sum32String(strings.TrimRight(line, "\r\n \t")) % 647`
- Map to one of 647 pre-computed BPE single-token bigrams (2-letter lowercase combinations that tokenize as single tokens in cl100k/o200k/Claude vocabularies)
- Identical blank lines intentionally collide; line number disambiguates
- Hash is content-only (line index intentionally unused) — anchors stay stable across line shifts from sibling edits
- The 647 bigrams are the complete set of 2-letter lowercase combinations that are single BPE tokens; the 29 missing are rare-letter pairs (q/x/z heavy)

**Parameters** (unchanged):
- `path` (string, required) — absolute or `~`-prefixed path
- `offset` (integer, optional) — 1-based start line
- `limit` (integer, optional) — max lines

**Context expansion**: When offset/limit is provided, add 1 leading context line (if offset > 1) and 3 trailing context lines (if limit is finite). Context lines also get hash tags.

**Read cache**: Maintain a thread-safe LRU cache (30 entries, protected by `sync.RWMutex`) of `{lineNumber -> content}` maps per agent loop. Required for stale-anchor recovery during edits. Keyed by absolute file path.

**Raw mode**: Add `raw` boolean parameter (default false). When true, return content without hash tags (for cases where the content itself is needed, not for editing).

**Files to modify**:
- `internal/tools/builtin/filesystem.go` — `ReadFileTool`
- New: `internal/tools/builtin/hashline.go` — hash computation, line formatting

#### 1.2 file_edit Tool (NEW)

**New tool**: Incremental file editing using hash-anchored line references.

**Parameters**:
- `path` (string, required) — file to edit
- `edits` (array of edit operations, required) — each edit is one of:
  - `{ "op": "replace", "anchor": "LINE:HASH", "end_anchor": "LINE:HASH", "content": "..." }` — replace range
  - `{ "op": "insert_after", "anchor": "LINE:HASH|BOF|EOF", "content": "..." }` — insert after anchor
  - `{ "op": "insert_before", "anchor": "LINE:HASH|BOF|EOF", "content": "..." }` — insert before anchor
  - `{ "op": "delete", "anchor": "LINE:HASH", "end_anchor": "LINE:HASH" }` — delete range

**Anchor format**: `LINE_NUMBER:HASH` (e.g., `42:a3`). Special values: `BOF` (beginning of file), `EOF` (end of file).

**Execution flow**:
1. Read the target file from disk
2. Validate every anchor's hash against actual file content using the same hash function
3. If any anchor mismatches:
   a. Attempt 3-way merge recovery using the read cache snapshot
   b. If recovery fails, return error with fresh hashline-annotated content so the model can re-anchor
4. Apply edits bottom-up (highest line first) so earlier indices stay valid
5. Write the modified content back to disk
6. Return diff summary + SHA256 evidence

**Boundary absorption**: Auto-detect and absorb payload lines that duplicate existing file content at replacement boundaries. Handles the common case where the model includes context lines in its replacement.

**Multi-edit batching**: All edits in one call are validated together before any are applied (atomic).

**Error format** (on hash mismatch):
```
Edit rejected: 2 anchors do not match the current file (marked *).

*42:a3|function hi() {        // stale — actual content differs
 43:er|    return;
 44:0e|}

The edit was NOT applied. Use the updated content below and re-issue.
```

**Files to create**:
- `internal/tools/builtin/file_edit.go` — edit tool implementation
- `internal/tools/builtin/hashline.go` — shared hash computation

**Daemon wiring**: Register `file_edit` in `internal/daemon/components.go`.

#### 1.3 file_write Changes

**No changes to behavior**. `file_write` remains for creating new files and complete overwrites. The system prompt should instruct the model to prefer `file_edit` for modifications and `file_write` only for new files.

When hashline mode is active, `file_write` should strip `LINE:HASH|` prefixes if the model accidentally includes them (defensive parsing).

---

### 2. Tool Surface Expansion

#### 2.1 Current Tool Count

| Category | Tools |
|----------|-------|
| Filesystem | `file_read`, `file_write`, `file_delete`, `list_directory` |
| Shell | `shell` |
| Web | `web_fetch`, `web_search` |
| Memory | `memory_store`, `memory_search`, `memory_get_context`, `memory_get_version`, `memory_get_version_history` |
| Tasks | `task_create`, `task_get`, `task_list`, `task_update` |
| Scheduler | `schedule_create/list/get/delete/pause/resume/run_now`, `cron_create` |
| Platform | `platform_status`, `platform_agents`, `platform_tools`, `delegate_task` |
| Sessions | `session_history` |
| Templates | `template_invoke`, `template_list`, `template_clear` |
| Code Intel | `ast_parse`, `ast_symbols`, `ast_query`, `lsp_goto_definition`, `lsp_find_references`, `lsp_hover`, `lsp_workspace_symbols`, `lsp_diagnostics` |
| Calendar | `calendar_list`, `calendar_create`, `calendar_quick_add`, `calendar_today` |
| **Total** | **~47** (including conditional MCP tools) |

#### 2.2 Proposed New Tools

| Tool | Purpose | Priority |
|------|---------|----------|
| `file_edit` | Hashline incremental editing | P0 |
| `file_find` | Fast file search (glob patterns) | P1 |
| `file_grep` | Content search (regex patterns) | P1 |
| `lsp_rename` | Symbol rename across codebase | P1 |
| `lsp_code_actions` | Quick-fixes, refactors, imports | P2 |
| `lsp_type_definition` | Go to type definition | P2 |
| `lsp_implementation` | Find implementations | P2 |
| `lsp_format` | Format file via LSP | P2 |
| `debug_launch` | Start debug session (DAP) | P3 |
| `debug_breakpoint` | Set/remove breakpoints | P3 |
| `debug_continue` | Continue/step execution | P3 |
| `debug_evaluate` | Evaluate expressions | P3 |
| `debug_inspect` | Stack trace, variables, scopes | P3 |

**Priority levels**: P0 = implement first, P1 = second wave, P2 = third wave, P3 = later.

#### 2.3 file_find and file_grep Tools (P1)

Currently the model must use `shell` with `find` and `grep` commands. Dedicated tools are more reliable and secure.

**file_find parameters**:
- `pattern` (string, required) — glob pattern (e.g., `**/*.go`)
- `path` (string, optional) — search root (default: working directory)
- `type` (string, optional) — `file`, `dir`, `any` (default: `file`)
- `max_results` (integer, optional) — cap results (default: 100)

**file_grep parameters**:
- `pattern` (string, required) — regex pattern
- `path` (string, optional) — search root
- `glob` (string, optional) — file filter (e.g., `*.go`)
- `output_mode` (string, optional) — `content`, `files_with_matches`, `count` (default: `content`)
- `context` (integer, optional) — context lines (default: 2)
- `max_results` (integer, optional) — cap results (default: 50)

Both tools use Go standard library (`filepath.Glob`, `regexp`) — no external dependencies.

Output of `file_grep` in content mode includes hashline tags for edit anchoring.

**Files to create**:
- `internal/tools/builtin/file_find.go`
- `internal/tools/builtin/file_grep.go`

---

### 3. LSP Surface Expansion

#### 3.1 Current LSP Operations (5)

| Tool | LSP Method |
|------|-----------|
| `lsp_goto_definition` | `textDocument/definition` |
| `lsp_find_references` | `textDocument/references` |
| `lsp_hover` | `textDocument/hover` |
| `lsp_workspace_symbols` | `workspace/symbol` |
| `lsp_diagnostics` | `textDocument/publishDiagnostics` |

#### 3.2 Proposed Additional Operations

| Tool | LSP Method | Purpose |
|------|-----------|---------|
| `lsp_rename` | `textDocument/rename` | Rename symbol across codebase |
| `lsp_code_actions` | `textDocument/codeAction` | Quick-fixes, refactors, organize imports |
| `lsp_type_definition` | `textDocument/typeDefinition` | Go to type definition |
| `lsp_implementation` | `textDocument/implementation` | Find implementations of interface |
| `lsp_document_symbols` | `textDocument/documentSymbol` | Document-level symbol hierarchy |
| `lsp_format` | `textDocument/formatting` | Format file via LSP |

**lsp_rename parameters**:
- `file_path` (string, required)
- `line` (integer, required) — 1-indexed
- `character` (integer, required)
- `new_name` (string, required)
- `apply` (boolean, optional, default: true) — whether to apply the edit

**lsp_code_actions parameters**:
- `file_path` (string, required)
- `line` (integer, required)
- `character` (integer, optional)
- `query` (string, optional) — filter by title substring
- `apply` (boolean, optional) — apply the first matching action

**Files to create**:
- `internal/code/tools/lsp_rename.go`
- `internal/code/tools/lsp_code_actions.go`
- `internal/code/tools/lsp_type_definition.go`
- `internal/code/tools/lsp_implementation.go`
- `internal/code/tools/lsp_document_symbols.go`
- `internal/code/tools/lsp_format.go`

#### 3.3 LSP Writethrough on file_write/file_edit

When LSP is active, file writes and edits should:
1. Send `didOpen`/`didChange` to applicable LSP servers
2. Optionally format via `textDocument/formatting`
3. Write to disk
4. Send `didSave`
5. Wait for fresh diagnostics (up to 5s)
6. Return diagnostics summary with the tool result

This creates a feedback loop: the model writes code, immediately sees type errors, and can fix them.

**Configuration**: Add to transport config:
```json5
{
  lsp: {
    format_on_write: true,     // format file after write
    diagnostics_on_write: true, // collect diagnostics after write
    diagnostics_timeout: 5,     // seconds to wait for diagnostics
  }
}
```

---

### 4. DAP Debugging (Phase 3)

#### 4.1 Debug Tool Architecture

A single `debug` tool with action-based dispatch, mirroring omp's design.

**Parameters**:
- `action` (string, required) — one of: `launch`, `attach`, `set_breakpoint`, `remove_breakpoint`, `continue`, `step_over`, `step_in`, `step_out`, `pause`, `evaluate`, `stack_trace`, `threads`, `scopes`, `variables`, `terminate`, `sessions`
- Plus action-specific parameters (file, line, expression, etc.)

**Session management**:
- `DapSessionManager` singleton — one active session at a time
- Session lifecycle: launching → configured → running/stopped → terminated
- Configuration handshake: wait for `initialized` event, send `configurationDone`
- Output capture: ring buffer of stdout/stderr (max 128KB)
- Idle cleanup: terminate after 10 minutes of inactivity

**DAP adapter configuration**: Auto-detect from:
1. Program file extension matching adapter
2. Project root markers (go.mod, Cargo.toml, etc.)
3. Binary availability in `$PATH`

**Built-in adapters** (Go-relevant first):
- `dlv` — Go (`dlv dap`)
- `gdb` — C/C++/Rust (`gdb -i dap`)
- `lldb-dap` — C/C++/ObjC/Swift/Rust
- `debugpy` — Python
- `js-debug-adapter` — JavaScript/TypeScript

**Files to create**:
- `internal/debug/` — new package
  - `manager.go` — DapSessionManager
  - `client.go` — DAP JSON-RPC client
  - `config.go` — adapter auto-detection
  - `transport.go` — stdio and socket transports
- `internal/tools/builtin/debug.go` — debug tool

---

### 5. Context Management Improvements

#### 5.1 Prefix Cache Stabilization

**Current**: System prompt and tool definitions may shift between turns, breaking LLM provider prefix caches.

**Target**: Stabilize the system prompt + tool definition bytes across turns so provider prefix caches hit at maximum rate.

**Implementation**:
- Compute a deterministic byte layout for system prompt + tool definitions at session start
- If tools change mid-session (skill activation, MCP connection), append rather than reorder
- Track byte boundaries for cache hit measurement

**Files to modify**:
- `internal/agent/conversation.go` — append-only context management

#### 5.2 Tool Output Pruning

**Current**: Context compaction summarizes old messages but does not proactively truncate large tool results before compaction.

**Target**: Before compaction, scan for oversized tool results and truncate them.

**Algorithm**:
1. Protect the most recent 40,000 tokens of tool output
2. Never prune `file_read` or `memory_search` results
3. Only prune when savings exceed 20,000 tokens
4. Replace pruned output with `[Output truncated — N tokens saved]`

**Files to modify**:
- `internal/llm/context_compactor.go` — add `pruneToolOutputs()` pre-pass

#### 5.3 Handoff Compaction Strategy

**Current**: Single compaction strategy (structured summary).

**Target**: Add `handoff` strategy that uses the full agent system prompt during summarization to produce a detailed technical handoff document with exact file paths, symbol names, commands, test results, error messages — not abstractions.

**Configuration**:
```json5
{
  compaction: {
    strategy: "structured",    // "structured" | "handoff" | "off"
    threshold_percent: 85,     // compact at 85% of context window
    keep_recent_tokens: 20000, // preserve recent N tokens
  }
}
```

**Files to modify**:
- `internal/llm/context_compactor.go` — add handoff strategy
- `internal/config/schema.go` — add compaction config struct

---

### 6. Secret Handling

#### 6.1 Secret Obfuscator

**Current**: No secret management. Environment variables with sensitive names are visible to the LLM through `shell` output and system prompt injection.

**Target**: Automatic secret detection and obfuscation before sending content to the LLM.

**Secret sources**:
1. **Environment variables**: Scan `os.Environ()` for names matching `/(?:KEY|SECRET|TOKEN|PASSWORD|PASS|AUTH|CREDENTIAL|PRIVATE|OAUTH)(?:_|$)/i` with values >= 8 chars
2. **Config file**: `~/.meept/secrets.json5` with explicit entries

**Secret entry schema**:
```json5
[
  {
    type: "plain",          // "plain" or "regex"
    content: "AKIA...",     // the secret or regex pattern
    mode: "obfuscate",      // "obfuscate" (reversible) or "replace" (one-way)
  }
]
```

**Obfuscation**:
- Replace secrets with deterministic placeholders `#AB12#` (4 alphanumeric chars from xxHash32 of entry index)
- Process in descending length order (longest first) to prevent partial replacement
- Bidirectional map: obfuscate before LLM, deobfuscate on tool arguments

**Replace mode**:
- One-way substitution with same-length deterministic string
- Not reversed on tool arguments — used for secrets that should never appear in LLM output

**Integration points**:
- `TransformContextHook` — obfuscate all user/assistant messages before LLM call
- `AfterToolCallHook` — deobfuscate tool arguments, re-obfuscate tool results
- `BeforeToolCallHook` — deobfuscate tool parameters before execution

**Files to create**:
- `internal/security/secrets.go` — SecretObfuscator
- `internal/agent/security_hooks.go` — add secret obfuscation hooks

**Files to modify**:
- `internal/agent/hooks.go` — wire secret hooks

---

### 7. TT-SR (Time Traveling Stream Rules)

#### 7.1 Concept

Rules that sit dormant until the model goes off-script. Regex matching on streaming LLM output (text, thinking, tool calls). On match: abort the stream mid-token, inject the rule as a system reminder, retry.

#### 7.2 Rule Schema

Rules are loaded from skill files (YAML frontmatter):

```yaml
---
name: no-direct-file-write-for-edits
scope: tool_call
condition: "tool_name:\\s*['\"]?file_write"
interrupt: true
repeat: once
globs: ["**/*"]
---
When editing existing files, use file_edit instead of file_write. file_write should only be used for creating new files.
```

**Rule fields**:
- `name` (string) — unique identifier
- `scope` (string) — `text`, `thinking`, `tool_call`, `any`
- `condition` (string) — regex pattern to match against stream delta
- `interrupt` (boolean) — whether to abort and retry (true) or inject reminder in-band (false)
- `repeat` (string) — `once` (trigger once after injection) or `after-gap:N` (re-trigger after N turns)
- `globs` (string array) — file path filter

#### 7.3 Stream Monitoring

During LLM streaming, for each delta:
1. Append delta to scoped buffer (isolated by source)
2. Check each registered rule:
   - `canTrigger()` — repeat policy check
   - `matchesScope()` — text/thinking/tool matching
   - `matchesCondition()` — regex test

#### 7.4 Interrupt Flow

When a match triggers with interrupting mode:
1. Abort the LLM stream immediately
2. Discard partial assistant message (or keep as context depending on `contextMode`)
3. Inject a hidden system message with the rule content wrapped in `<system-rule-enforcement>` tags
4. Retry generation

For non-interrupting mode:
- Tool-source matches: prepend `<system-reminder>` to the tool result
- Prose-source matches: queue and inject after the assistant message completes

#### 7.5 Persistence

Injected rules are recorded in the session store. On session reload/resume, injected state is restored so rules don't re-trigger on already-handled violations.

**Files to create**:
- `internal/agent/ttsr.go` — TtsrManager, rule matching, stream monitoring
- `internal/agent/ttsr_rules.go` — rule parsing and loading

**Files to modify**:
- `internal/agent/loop.go` — integrate stream monitoring in `reasoningCycle()`
- `internal/agent/hooks.go` — add TTSR hook points

---

### 8. Structured Subagent Output

#### 8.1 Problem

Currently, `delegate_task` returns raw string responses from subagents. There is no schema validation — the parent agent must parse unstructured text.

#### 8.2 Target

Subagents produce structured JSON output validated against a schema. The parent agent receives typed, validated data.

#### 8.3 Implementation

**delegate_task enhancement**:
- Add `output_schema` parameter (JSON Schema object, optional)
- When provided, the subagent is instructed to produce JSON matching the schema
- The subagent's response is validated against the schema before returning
- On validation failure: return error with schema violation details, allow one retry

**Structured response format**:
```json
{
  "success": true,
  "data": { ... },      // validated against output_schema
  "evidence": [...],    // tool evidence from subagent execution
  "tokens_used": 1234   // token consumption tracking
}
```

**Error format**:
```json
{
  "success": false,
  "error": "schema validation failed: /data/files/0: missing required property 'path'",
  "partial_result": { ... }
}
```

**Files to modify**:
- `internal/tools/builtin/platform.go` — `DelegateTaskTool` — add output_schema parameter
- `internal/agent/registry.go` — `RunAgent` — add schema validation wrapper

---

### 9. Tool Interception Enhancements

#### 9.1 Current Hook System

Meept already has five hook points with priority-based execution:
- `BeforeToolCallHook` — can block tool execution
- `AfterToolCallHook` — can override tool results
- `PrepareNextTurnHook` — can inject messages, switch models
- `ShouldStopAfterTurnHook` — can force loop exit
- `TransformContextHook` — can modify messages and tool definitions

#### 9.2 Proposed Enhancements

**Preview/Approval flow (resolve mechanism)**:
- Tools marked as `Deferrable` stage preview actions that require explicit resolution
- The agent loop tracks pending previews and injects a `resolve` tool into the tool definitions
- The model calls `resolve(action: "apply"|"discard", reason: "...")` to accept or reject
- Use cases: `file_edit` preview (show diff before applying), destructive shell commands

**Implementation**:
- Add `Deferrable` interface to tool system: `Preview(args) (PreviewResult, error)`
- Agent loop manages a pending preview queue
- When a preview is pending, inject `resolve` tool into available tools
- On `resolve("apply")`, call `Apply()` on the deferred tool
- On `resolve("discard")`, call `Discard()` and return cancellation message

**Files to modify**:
- `internal/tools/interface.go` — add `Deferrable` interface
- `internal/agent/executor.go` — preview/approval flow
- `internal/agent/loop.go` — inject resolve tool when preview pending

---

### 10. Slash Command Enhancement

#### 10.1 Current Slash Commands

Built-in: `help`, `new`, `clear`, `retry`, `undo`, `usage`, `stop`, `status`, `vim`, `session`, `task`, `cancel`, `amend`, `interrupt`, `tasks`

Non-builtin: treated as skill invocation (`/skill-name args`)

#### 10.2 Proposed Additions

| Command | Purpose |
|---------|---------|
| `/edit` | Open file in system editor |
| `/diff` | Show git diff for current changes |
| `/model` | Switch active model |
| `/compact` | Force context compaction |
| `/plan` | Enter planning mode |
| `/review` | Review current changes |

#### 10.3 File-Based Custom Commands

Support user-defined slash commands as markdown files:

**Discovery paths** (three-tier):
- `.meept/commands/` (project-local)
- `~/.meept/commands/` (user-global)

**Format** (markdown with YAML frontmatter):
```markdown
---
name: "migrate"
description: "Run database migrations"
arguments: ["direction"]
---
Run the following command: `goose $1`
```

**Argument substitution**: `$ARGUMENTS` for all args, `$1`, `$2` for positional args. (The `{{var}}` syntax is not supported — use `$1`, `$2`, etc.)

**Files to modify**:
- `internal/sharedclient/slash.go` — add file-based command discovery
- `cmd/meept/` — CLI command registration

---

## Implementation Priority

| Phase | Items | Impact |
|-------|-------|--------|
| **Phase 1** | Hashline file_read + file_edit | Highest ROI — eliminates the edit bottleneck |
| **Phase 2** | file_find, file_grep, secret obfuscation, tool output pruning | Reliability and security |
| **Phase 3** | LSP expansion (rename, code_actions, format, writethrough) | Code intelligence depth |
| **Phase 4** | Context management (prefix cache, handoff strategy) | Long-session reliability |
| **Phase 5** | TT-SR rules, structured subagent output | Agent quality enforcement |
| **Phase 6** | DAP debugging, preview/approval flow | Advanced debugging |
| **Phase 7** | Slash commands, remaining LSP ops | Polish |

---

## What We Are NOT Doing

These items from the omp analysis are explicitly excluded:

1. **Native Rust modules** — omp uses Rust N-API for in-process grep, bash, AST, token counting. Meept is a Go daemon; the equivalent would be Go native implementations (already using Go stdlib). The performance difference is smaller in Go since there is no V8 overhead.

2. **Internal URL schemes** (pr://, issue://, etc.) — omp's 10 internal protocols for virtual file reads. Not justified for Meept's use case.

3. **ast_edit tool** — omp has tree-sitter structural find-and-replace. Meept has `ast_query` for search; structural edit via `file_edit` with AST-aware context is sufficient.

4. **Swarm extension** — omp's YAML-based DAG orchestration. Meept already has a superior system (strategic planner + tactical scheduler + message bus).

5. **IRC inter-agent chat** — omp's ephemeral messaging between subagents. Meept has the message bus with persistent pub/sub.

6. **Output minimizer** — omp's post-execution shell output compression with 20+ program-specific filters. Interesting but not P0.

7. **Hindsight vector store** — omp's per-project vector memory. Meept uses SQLite FTS5 which is adequate for current scale.

---

## Success Metrics

| Metric | Current | Target |
|--------|---------|--------|
| Edit failure rate | ~100% for non-trivial edits (whole-file overwrite) | < 5% with hashline |
| Tokens per edit (500-line file, 3-line change) | ~8000 output tokens (entire file) | ~100 output tokens (3 hashline operations) |
| Secret exposure to LLM | All env vars visible | Zero secrets in LLM context |
| LSP operations | 5 | 11+ |
| Tool count | ~47 | ~55+ |
| Context window utilization | 80% trigger for compaction | 85% with proactive pruning |
