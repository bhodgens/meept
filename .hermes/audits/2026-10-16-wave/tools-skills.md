# internal/tools, internal/mcp, internal/skills, internal/selfimprove, internal/acp -- bughunt 2026-10-16

## Findings

### 1. MCP Client: Missing timeout on `Start()` / initialize handshake

- **Location:** `internal/tools/mcp/transport/stdio.go:74-92`
- **Mechanism:** `StdioTransport.Start()` creates a detached context via `context.WithCancel(context.Background())` but the initialize handshake in `Client.Connect()` uses the caller's context. If the caller's context has a short timeout (e.g., 30s from an RPC handler), the initialize request may timeout before the subprocess fully starts. The transport's `Send()` has a timeout (`t.config.TimeoutMS`), but `Start()` itself has no protection against a hung subprocess startup.
- **Severity:** HIGH
- **Test/Probe:** Call `Connect()` with a 1ms timeout context; observe whether the transport hangs or returns an error.
- **Evidence:** SOURCE (code review)
- **Not covered:** Whether `http.go` (MCP HTTP transport) has similar timeout gaps.

### 2. MCP Client: Process leak on server crash (incomplete cleanup)

- **Location:** `internal/tools/mcp/transport/stdio.go:315-396` (Close())
- **Mechanism:** When `Close()` is called, it kills the subprocess and waits for relay/stderr goroutines. However, if the subprocess crashes *without* `Close()` being called (e.g., segfault, OOM kill), the transport's `running` flag stays `true` and the goroutines leak. The `relayStdout()` and `drainStderr()` goroutines only exit when `running` is false or the pipe closes -- but a crashed subprocess may leave pipes in an undefined state.
- **Severity:** MEDIUM
- **Test/Probe:** Kill the MCP server subprocess directly (e.g., `kill -9 <pid>`); verify that the transport's goroutines don't leak (check with `pprof` or race detector).
- **Evidence:** SOURCE (code review)
- **Not covered:** HTTP transport (`http.go`) cleanup behavior on server crash.

### 3. Skills: `requires-tools` gating can be bypassed via nil checker

- **Location:** `internal/skills/executor.go:241-245`
- **Mechanism:** `checkRequiredTools()` returns early if `e.toolAvailability == nil`. A skill with `requires-tools: [web_fetch]` in its frontmatter will skip the tool availability check entirely if the executor's `toolAvailability` checker is nil. This is intentional per the test `executor_requires_tools_test.go:129` ("nil checker with requires-tools skips check"), but it means a skill can declare hard dependencies that are never enforced.
- **Severity:** MEDIUM
- **Test/Probe:** Create a skill with `requires-tools: [definitely.missing]` and run it with a nil checker; verify the skill executes without error.
- **Evidence:** SOURCE (code review + test file)
- **Not covered:** Whether this bypass is documented as expected behavior in SKILL.md or AGENTS.md.

### 4. Builtin Tools: Path traversal guard is incomplete for `file://` URIs

- **Location:** `internal/tools/mcp/client.go:299-305`
- **Mechanism:** The security fix for resource URIs only blocks `file://` URIs containing `/etc/` or `/../`. It does *not* block `file:///home/`, `file://var/`, or absolute paths like `file:///etc/passwd` (the `/etc/` check would catch that, but `file:///etc/passwd` with triple slash is handled). However, it does NOT block `file://.` or `file://..` or other relative path traversals that don't contain `/etc/`. A malicious MCP server could return `file://../../secret.txt` which would pass the check.
- **Severity:** HIGH
- **Test/Probe:** Send an MCP tool call that returns a resource with URI `file://../../etc/passwd`; verify whether the client blocks it.
- **Evidence:** SOURCE (code review)
- **Not covered:** Whether the `ssrf.go` file has additional URI validation that complements this.

### 5. Selfimprove: TODO marker detection is a FALSE ALARM (not 6 TODOs)

- **Location:** `internal/selfimprove/detector.go:323`
- **Mechanism:** The detector's `ScanCode()` method searches for `// TODO:` patterns. The grep shows only ONE TODO pattern definition (line 323), not 6 TODO markers in the file. The detector itself doesn't contain 6 TODO markers -- it's a pattern-matching tool that *finds* TODOs in other files. The task's premise of "6 TODO markers at detector.go" appears to be a misreading.
- **Severity:** LOW (informational)
- **Test/Probe:** `grep -n 'TODO' internal/selfimprove/detector.go` returns only line 323 (the pattern definition).
- **Evidence:** LIVE (grep output)
- **Not covered:** Whether other files in `internal/selfimprove/` contain TODOs that the detector would find.

## Checked and clean

- **internal/tools/registry.go:** Tool registration uses a simple map; duplicates are logged with `Warn` but not prevented. No duplicate registration *bugs* found -- the behavior is intentional (last-wins).
- **internal/tools/mcp/protocol.go:** Basic JSON-RPC parsing; no obvious injection vulnerabilities.
- **internal/skills/parser.go:** Frontmatter parsing uses `yaml.v3`; no obvious parse asymmetry between `ParseSkillMetadataOnly` and `ParseSkillText` -- both handle missing frontmatter leniently.
- **internal/acp/:** Package exists but is small; no critical issues identified in scope.
- **MCP client response size limiting:** The `maxMCPToolResponseSize` (1MB) cap and block count limit (1000) are in place -- this addresses the unbounded output concern partially.

## Residual / not covered

- Full audit of all builtin tool registrations (which tools are registered where, and whether any are missing from the dispatch switch).
- Detailed review of `internal/tools/mcp/manager.go` for server lifecycle management.
- Skills `add` vs `list` frontmatter parse asymmetry -- the parser seems consistent, but the `add` command's validation path wasn't traced.
- `internal/tools/builtin/` shell tool command injection risks (if any `shell` tool exists).

gates_run: `go vet ./internal/tools/... ./internal/mcp/... ./internal/skills/... ./internal/selfimprove/... ./internal/acp/...` (passed, no errors)
