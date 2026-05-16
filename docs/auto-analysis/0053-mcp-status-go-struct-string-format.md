# MCP meept_status returns raw Go struct string instead of JSON
**Date**: 2026-05-16
**Phase**: 4
**Severity**: low
**Component**: mcp

## Description
The `meept_status` MCP tool returns the daemon's `DaemonStatusResponse` as a Go struct string (e.g., `&{running 42421.615893959 zai/glm-4.7 ...}`) instead of JSON.

## Reproduction
Call `meept_status` via MCP:
```
tools/call: {"name":"meept_status","arguments":{}}
```

Result:
```
&{running 42421.615893959 zai/glm-4.7 zai/glm-4.7 [dev.current_model ...] 68 0 100000 0 ...}
```

## Evidence
`internal/mcp/server.go` line 180:
```go
Result: mustMarshal(map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("%v", result)}}}),
```

The `fmt.Sprintf("%v", result)` produces a Go struct format string for the `*types.DaemonStatusResponse` type.

## Root Cause
`toolStatus` returns `*types.DaemonStatusResponse`. The `handleToolsCall` method at line 180 uses `fmt.Sprintf("%v", result)` to convert the result to a string, which uses Go's `%v` verb for structs.

## Proposed Fix
Marshal the result as JSON instead:
```go
func (s *Server) toolStatus(args map[string]any) (any, error) {
    status, err := s.client.Status()
    if err != nil {
        return nil, err
    }
    data, _ := json.Marshal(status)
    return string(data), nil
}
```

## Classification
[x] Harness bug  [ ] Model quality  [ ] Design gap
