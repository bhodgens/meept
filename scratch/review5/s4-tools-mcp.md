# S4: Tools + MCP Code Review Findings

Round 5 systematic review of tools, MCP, and code intelligence packages.

Scope: `internal/tools/`, `internal/tools/builtin/`, `internal/tools/mcp/`, `internal/mcp/`, `internal/code/ast/`, `internal/code/lsp/`, `internal/code/tools/`

## Critical

### S4-1 WebSearchTool lacks dial-time SSRF protection (DNS rebinding vulnerability)

**File:** `internal/tools/builtin/tool_web_search.go:63-83`
**Severity:** Critical

**Evidence:** `WebSearchTool` constructs its HTTP client with a plain `http.Transport` that has no custom `DialContext`. Compare to `WebFetchTool` (`internal/tools/builtin/web_fetch.go:63-70`) which uses `ssrfDialContext(false)` for dial-time re-validation:

```go
// web_search.go (VULNERABLE)
t.client = &http.Client{
    Timeout: timeout,
    Transport: &http.Transport{
        MaxConnsPerHost: 8,
        // no DialContext — no dial-time SSRF check
    },
    CheckRedirect: func(req *http.Request, via []*http.Request) error {
        ...
        if err := checkURL(req.URL.String()); err != nil { ... }
    },
}

// web_fetch.go (CORRECT)
t.client = &http.Client{
    Transport: &http.Transport{
        MaxConnsPerHost: 8,
        DialContext:     ssrfDialContext(false),  // dial-time re-check
    },
    CheckRedirect: t.checkRedirect,
}
```

**Why it matters:** `checkURL()` in the redirect handler resolves the hostname at call time and validates IPs. But between that resolution and the actual TCP dial (which happens inside `http.Client.Do`), the DNS record can change (DNS rebinding attack). `WebFetchTool` closes this window via `ssrfDialContext`; `WebSearchTool` does not. An attacker controlling a DNS server could make DuckDuckGo redirect to a hostname that first resolves to a public IP (passing `checkURL`) then re-resolves to `169.254.169.254` (cloud metadata) at dial time. Although the search endpoint is fixed to `html.duckduckgo.com`, redirect targets from DuckDuckGo are arbitrary URLs from search results.

**Fix:** Add `DialContext: ssrfDialContext(false)` to the transport in `NewWebSearchTool`.

---

### S4-2 MCP HTTPTransport lacks dial-time SSRF protection

**File:** `internal/tools/mcp/transport/http.go:84-107`
**Severity:** Critical

**Evidence:** The `NewHTTPTransport` function creates an `http.Client` with redirect checking via `checkRedirectURL`, but the underlying `http.Transport` is the default — no custom `DialContext`:

```go
return &HTTPTransport{
    client: &http.Client{
        Timeout: timeout,
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            ...
            if err := checkRedirectURL(req.URL.String()); err != nil { ... }
        },
        // no Transport specified — uses http.DefaultTransport
    },
}
```

Additionally, `checkRedirectURL` (lines 41-66) resolves hostnames using `context.Background()` instead of the request context, so cancellation during DNS resolution is impossible.

**Why it matters:** MCP servers are user-configured (`~/.meept/mcp_servers.json5`). A malicious or compromised MCP server URL or redirect target could exploit DNS rebinding to reach internal services. The redirect check resolves once, but the actual TCP dial uses the default dialer with no re-validation. The `checkRedirectURL` function also duplicates the `isBlockedAddress` logic from `internal/tools/builtin/ssrf.go` rather than importing it, creating a maintenance divergence risk.

**Fix:** Set a custom `Transport` with `DialContext` that re-validates resolved IPs at dial time (equivalent to `ssrfDialContext`). Use the request context for DNS resolution in redirect checks.

---

## High

### S4-3 ASTEditTool and ResolveASTEditTool bypass fence checking on file writes

**File:** `internal/code/tools/ast_edit.go:230`, `internal/code/tools/resolve_ast_edit.go:176`
**Severity:** High

**Evidence:** Both tools write directly to disk via `os.WriteFile(filePath, modifiedSource, 0o644)` with no fence/path validation:

```go
// ast_edit.go:230
if err := os.WriteFile(filePath, modifiedSource, 0o644); err != nil {
    return nil, fmt.Errorf("failed to write modified file: %w", err)
}
```

Neither tool has a `FenceChecker` field, `SetFenceChecker` method, or any path validation beyond what `os.ReadFile`/`os.WriteFile` natively provide. Compare to `FileEditTool` which has `SetFenceChecker` and validates every path, and `ResolveTool` which re-validates at accept time.

**Why it matters:** The agent can use `ast_edit` to write to any path on the filesystem, including `/etc/passwd`, `~/.ssh/authorized_keys`, or files outside the project workspace. The hashline `file_edit` tool is properly fenced, but these AST-based tools are not, creating a fence bypass.

**Fix:** Add `SetFenceChecker(fc FenceChecker)` to both tools (with nil guard per CLAUDE.md), and call `fc.CheckPath(filePath, "write")` before any write operation.

---

### S4-4 lspWriteNotifier absPath helper bypasses fence checking

**File:** `internal/tools/builtin/lsp_writethrough.go:354-367`
**Severity:** High

**Evidence:** The `absPath` function in `lsp_writethrough.go` is a separate copy of `resolvePath` from `filesystem.go` (lines 853-868) that resolves paths to absolute form but performs no fence validation. The `applyFormattingEdits` function (line 350) calls `os.WriteFile(filePath, ...)` on this path without any fence check.

```go
func absPath(path string) (string, error) {
    if strings.HasPrefix(path, "~") { ... }
    abs, err := filepath.Abs(strings.TrimSpace(path))
    ...
    return filepath.Clean(abs), nil
    // no fence check
}
```

The `lspWriteNotifier.NotifyWrite` and `formatFile` methods use `absPath` to resolve paths then pass them to LSP and write to disk. If the LSP server returns formatting edits, `applyFormattingEdits` writes directly to the file.

**Why it matters:** If an LSP server is compromised or returns malicious formatting edits targeting files outside the workspace, the writethrough notifier applies them without fence validation. This is a defense-in-depth gap — the main `WriteFileTool` validates via fence, but the LSP formatting path does not.

**Fix:** Either route through the existing `resolvePath` + fence checker, or add a fence validation step before any write in `applyFormattingEdits`.

---

### S4-5 parseSimpleStatus / parseFileStatus break on file paths with spaces

**File:** `internal/tools/builtin/git_split.go:203-233`, `internal/tools/builtin/git_overview.go:198-215`
**Severity:** High

**Evidence:** Both functions use `strings.Fields(line)` to split git status output, which splits on any whitespace:

```go
// git_split.go:210
parts := strings.Fields(line)
if len(parts) < 2 { continue }
status := parts[0]
filePath := parts[1]  // BUG: only gets first token of path
```

Git `--porcelain` status output uses space as the delimiter between status code and file path, but file paths themselves can contain spaces. A file named `my file.go` would produce status line ` M my file.go`, and `strings.Fields` would split it into `["M", "my", "file.go"]`, capturing only `"my"` as the file path.

The `git_overview.go` variant (line 212-215) has a partial fix for rename tracking (`R` status) using `parts[2]`, but still uses `strings.Fields` for the initial split, so paths with spaces are still broken.

**Why it matters:** Files with spaces in their names will be silently dropped from commit grouping and overview analysis. In projects with such files (common on macOS and Windows), `git_split` will produce incomplete or incorrect commit groups.

**Fix:** Use `git status --porcelain=v1 -z` (NUL-delimited) or parse the fixed-width format (2-char status, space, path) with substring extraction: `status := line[:2]; filePath := line[3:]`.

---

### S4-6 DebugTool lacks fence checking on program/core_file/script_file paths

**File:** `internal/tools/builtin/debug.go:318+`
**Severity:** High

**Evidence:** The `DebugTool.Execute` method handles actions like `load_core`, `launch`, and `script_run` that accept file paths (`core_file`, `program`/`script_file`) from the LLM. These paths are used directly in `os.ReadFile` and debug adapter configuration without any fence validation. The tool struct has no `FenceChecker` field at all.

```go
// load_core action reads core_file with no validation
coreFile, _ := args["core_file"].(string)
// ...used directly with debug manager
```

**Why it matters:** The agent could read core dumps, launch debuggers against arbitrary binaries, or execute scripts from outside the workspace. For `launch` mode, the `program` path could point to any executable on the system. This is a sandbox escape vector.

**Fix:** Add `SetFenceChecker` to `DebugTool` and validate `program`, `core_file`, `script_file`, and `working_dir` paths before use.

---

## Medium

### S4-7 Predictable ID generation using time.Now().UnixNano() across tools

**File:** Multiple files (see below)
**Severity:** Medium

**Evidence:** The grep output shows `time.Now().UnixNano()` used for ID generation in 40+ locations across the codebase. In the tools scope specifically:

- `internal/tools/builtin/platform.go:373` — `generateDelegateID()`
- `internal/tools/builtin/tool_schedule_create.go:119` — job ID
- `internal/tools/builtin/tool_cron_create.go:144` — cron job ID
- `internal/tools/builtin/file_edit.go:381` — session ID fallback
- `internal/tools/builtin/review_tools.go:128` — conversation ID
- `internal/code/tools/ast_edit.go:192` — session ID fallback
- `internal/code/tools/lsp_rename.go:198` — session ID fallback

CLAUDE.md's coding practices say: "use `pkg/id.Generate()`" for predictable IDs. The MEMORY.md notes this as a recurring bug pattern.

**Why it matters:** Two concurrent calls within the same nanosecond produce colliding IDs. While unlikely for single calls, batch operations or high-concurrency scenarios can produce collisions, leading to data corruption or lost updates.

**Fix:** Replace all `fmt.Sprintf("...%d", time.Now().UnixNano())` patterns with `pkg/id.Generate()` or add an atomic counter suffix where the existing pattern provides sufficient uniqueness.

---

### S4-8 CronCreateTool returns both result and error on cron expression build failure

**File:** `internal/tools/builtin/tool_cron_create.go:128-133`
**Severity:** Medium

**Evidence:**

```go
cronExpr, err := t.buildCronExpression(args)
if err != nil {
    return CronCreateResult{
        Success: false,
        Error:   err.Error(),
    }, err  // BUG: returns error alongside result
}
```

Every other error path in this function returns `(result, nil)`. This specific path returns `(result, err)`, which means the caller gets a Go error. The `Registry.Execute` method (registry.go:146-153) treats returned errors differently from error results — it wraps them in `NewErrorResultErr(err)` and logs a warning. The caller may see a double error (the result's Error field plus the returned error).

**Fix:** Change `}, err` to `}, nil` to match all other error paths in the function.

---

### S4-9 mcp.Server handleToolsCall returns errors as successful responses

**File:** `internal/mcp/server.go:172-179`
**Severity:** Medium

**Evidence:** When tool execution returns an error, the server wraps it as a result (not an error), using the MCP content format:

```go
if err != nil {
    return &JSONRPCResponse{
        JSONRPC: "2.0",
        ID:      req.ID,
        Result:  mustMarshal(map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("error: %v", err)}}}),
    }
}
```

This is a valid MCP pattern (`isError` content), but it does not set the `isError` flag in the result. The MCP spec defines `CallToolResult.IsError` to distinguish error content from success content. Clients that check `isError` will treat tool execution failures as successful results.

**Fix:** Include `"isError": true` in the marshaled result, or use the proper `CallToolResult` struct with `IsError: true`.

---

### S4-10 AskTool implements TerminatingTool but TerminateHint returns false

**File:** `internal/tools/builtin/ask.go`
**Severity:** Medium (Low if intentional)

**Evidence:** `AskTool` has a `TerminateHint` method that returns `false`:

```go
func (t *AskTool) TerminateHint(args map[string]any) bool { return false }
```

The `tools.TerminatingTool` interface says: "TerminateHint returns true if the tool's result is a final answer that should be returned to the user without LLM follow-up." The AskTool produces a human response that likely needs LLM follow-up to incorporate into the conversation, so `false` may be correct. However, the tool implements the interface (via the compile-time assertion), which means the executor will call `TerminateHint` on every execution. If the intent was to always allow LLM follow-up, the tool could simply not implement the interface.

**Fix:** Verify this is intentional. If so, add a comment explaining why `false` is correct for AskTool. If not, remove the `TerminatingTool` implementation.

---

### S4-11 MCP HTTPTransport.parseSSEResponse may hang on partial data

**File:** `internal/tools/mcp/transport/http.go:190-226`
**Severity:** Medium

**Evidence:** The SSE parser uses `bufio.NewScanner` with the default buffer size (64KB). If an SSE event data line exceeds the scanner's buffer, `scanner.Scan()` returns false and `scanner.Err()` returns `bufio.ErrTooLong`. The function does not handle this case explicitly — it falls through to the "no response received" error.

Additionally, the parser only looks for `"data: "` prefix (with space). SSE spec allows `"data:"` without space. An MCP server sending `data:{...}` (no space) would be silently skipped.

**Fix:** Use a larger scanner buffer (e.g., `scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)`), and handle both `"data: "` and `"data:"` prefixes (use `strings.CutPrefix(line, "data:")` then trim space).

---

### S4-12 MCP Client.Connect logs c.tools length outside lock

**File:** `internal/tools/mcp/client.go:81-88`
**Severity:** Medium

**Evidence:**

```go
c.connected.Store(true)
c.logger.Info("connected to MCP server",
    "name", c.name,
    "server", initResult.ServerInfo.Name,
    "version", initResult.ServerInfo.Version,
    "tools", len(c.tools),  // reads c.tools without holding c.mu
)
```

The `c.tools` field is protected by `c.mu`. After `refreshTools` sets it under lock (line 123-125), the lock is released. Then `len(c.tools)` is read at line 87 without re-acquiring the lock. In a concurrent context (multiple goroutines calling Connect or refreshTools), this is a data race.

**Fix:** Either snapshot the tool count inside the `refreshTools` locked section, or acquire `c.mu.RLock()` before reading `len(c.tools)`.

---

## Low

### S4-13 file_grep.go formatGrepContent has O(n^2) bubble sort

**File:** `internal/tools/builtin/file_grep.go`
**Severity:** Low

**Evidence:** The `formatGrepContent` function sorts results using a nested loop bubble sort to order by line number. While functionally correct, this is O(n^2) where n is the number of matching lines. For files with many matches (e.g., searching for a common pattern in a large file), this becomes slow.

**Fix:** Use `sort.Slice` with a comparison function.

---

### S4-14 debug.go rawToMap silently swallows unmarshal errors

**File:** `internal/tools/builtin/debug.go`
**Severity:** Low

**Evidence:** The `rawToMap` function catches `json.Unmarshal` errors and falls back to wrapping the raw data in `{"raw": string(data)}`:

```go
if err := json.Unmarshal(data, &result); err != nil {
    return map[string]any{"raw": string(data)}
}
```

This is intentional defensive coding for debug protocol responses that may contain non-JSON data. However, it means malformed debug responses are silently converted to raw strings without any logging, making protocol debugging harder.

**Fix:** Add a `slog.Debug` log when falling back to raw, to aid debugging.

---

### S4-15 MCP Server version mismatch between client and server

**File:** `internal/mcp/server.go:93` vs `internal/tools/mcp/client.go:99`
**Severity:** Low

**Evidence:** The MCP server (`internal/mcp/server.go`) reports version `"0.1.0"` in `handleInitialize`, while the MCP client (`internal/tools/mcp/client.go`) sends version `"0.2.0"` in `ClientInfo`. This version mismatch means the server advertises itself as an older version than the client identifies as.

**Fix:** Align both to the current project version, or derive from a shared constant.

---

### S4-16 MCP StdioTransport stderr drain goroutine may leak if subprocess never exits

**File:** `internal/tools/mcp/transport/stdio.go:129-137`
**Severity:** Low

**Evidence:** The `drainStderr` goroutine loops on `t.stderr.Read(buf)` while `t.running.Load()` is true. The loop condition checks `t.running` at the top but blocks on `Read` inside the loop. If the subprocess keeps running but produces no stderr output, the goroutine blocks indefinitely on `Read`. `Close()` sets `running` to false and closes stdin/process, which eventually causes stderr to return EOF — but only if the process actually exits. The comment at line 122-128 acknowledges this.

This is a known limitation, not a bug per se, but worth documenting as a potential goroutine leak in edge cases (e.g., subprocess that closes stderr but keeps running).

**Fix:** Consider using a separate done channel that Close() signals, and select on it inside the drain loop.

---

### S4-17 orderedMap non-deterministic JSON in hashline_parser.go

**File:** `internal/tools/builtin/hashline_parser.go`
**Severity:** Low

**Evidence:** The `orderedMap` type uses a Go `map[string]string` internally, and its `MarshalJSON` iterates the map. Go maps have non-deterministic iteration order, so the JSON output keys will be in random order across runs. The inline comment admits this: "insertion order is implementation detail." This makes diffs non-reproducible and can cause test flakiness.

**Fix:** Use an ordered map implementation (slice of key-value pairs with a map for lookups) for deterministic JSON output.

---

### S4-18 ExtractJSONFromText first-match bias may miss valid JSON

**File:** `internal/tools/builtin/schema_validation.go:148-186`
**Severity:** Low

**Evidence:** `ExtractJSONFromText` tries three strategies in order: `` ```json `` block, `` ``` `` block, raw parse. If the text contains a `` ```json `` block with invalid JSON followed by a valid `` ``` `` block, the function returns the error from the first attempt and never tries the second. The function silently swallows parse errors from code-block extraction (lines 156, 174).

**Fix:** When the first strategy finds content but fails to parse, try the next strategy before falling through to raw parse.

---

## Summary

- **Critical:** 2 (SSRF DNS rebinding in web_search and MCP HTTP transport)
- **High:** 5 (AST edit fence bypass, LSP writethrough fence bypass, git path splitting, debug tool fence bypass, predictable IDs)
- **Medium:** 5 (cron double error, MCP error handling, AskTool terminate hint, SSE parsing, MCP data race)
- **Low:** 6 (bubble sort, error swallowing, version mismatch, goroutine leak, non-deterministic JSON, JSON extraction bias)
- **Total:** 18 findings
