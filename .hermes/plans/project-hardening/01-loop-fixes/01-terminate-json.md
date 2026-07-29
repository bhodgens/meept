# Leaf: buildTerminateResponse — Format Non-String Results as Natural Language

DISPATCH INSTRUCTION: Implement this leaf using TDD. Do NOT commit. Do NOT run git add. Write code, run tests, report results only.

## Parent
`01-loop-fixes/orchestrator.md`

## Scope
`internal/agent/loop.go` `buildTerminateResponse` function (~line 5686).

## Problem
`buildTerminateResponse` at line 5698 calls `json.Marshal(r.Result)` for non-string results, dumping raw JSON to the user. This affects `PlatformAgentsTool` and `PlatformToolsTool` (both still have `TerminateHint = true`).

## Tasks

### Task 1: Rewrite buildTerminateResponse formatting
File: `internal/agent/loop.go` (~line 5686-5708)

Replace the raw `json.Marshal` path with a formatter that converts structured data to readable text:

```go
func (l *AgentLoop) buildTerminateResponse(results []*ExecutionResult) string {
    var parts []string
    for _, r := range results {
        if r == nil || !r.Success {
            continue
        }
        if s, ok := r.Result.(string); ok {
            parts = append(parts, s)
            continue
        }
        // Format structured results as readable text instead of raw JSON.
        parts = append(parts, formatToolResult(r.Result))
    }
    if len(parts) == 0 {
        return "done"
    }
    return strings.Join(parts, "\n")
}
```

### Task 2: Add formatToolResult helper
File: `internal/agent/loop.go` (new function, near buildTerminateResponse)

```go
// formatToolResult converts a structured tool result (map[string]any, etc.)
// into a human-readable summary. This prevents raw JSON from reaching the
// user when a terminating tool returns structured data.
func formatToolResult(result any) string {
    // If the result is a map with a "status" key, it's likely a status object.
    if m, ok := result.(map[string]any); ok {
        // Platform agents/tools return arrays of names/descriptions.
        if agents, ok := m["agents"].([]any); ok {
            var names []string
            for _, a := range agents {
                if am, ok := a.(map[string]any); ok {
                    if name, ok := am["name"].(string); ok {
                        names = append(names, name)
                    }
                }
            }
            if len(names) > 0 {
                return fmt.Sprintf("available agents: %s", strings.Join(names, ", "))
            }
        }
        if tools, ok := m["tools"].([]any); ok {
            var names []string
            for _, t := range tools {
                if tm, ok := t.(map[string]any); ok {
                    if name, ok := tm["name"].(string); ok {
                        names = append(names, name)
                    }
                }
            }
            if len(names) > 0 {
                return fmt.Sprintf("available tools: %s", strings.Join(names, ", "))
            }
        }
    }
    // Fallback: JSON-encode but indented for readability.
    data, err := json.MarshalIndent(result, "", "  ")
    if err != nil {
        return fmt.Sprintf("%v", result)
    }
    return string(data)
}
```

### Task 3: Test
File: `internal/agent/loop_test.go` (or existing test file)

Add a test that calls `buildTerminateResponse` with:
- A string result (should pass through)
- A map[string]any result with "agents" key (should format as "available agents: ...")
- A nil/failed result (should be skipped)
- All-failed results (should return "done")

## Self-Verification Checklist
- [ ] `go build ./internal/agent/...` compiles
- [ ] `go test ./internal/agent/...` passes
- [ ] No raw JSON in buildTerminateResponse output for map results
- [ ] String results pass through unchanged
