# OpenAPI SDK Cleanup - Complete

**Date**: 2026-06-16
**Last Updated**: 2026-06-16 (Task 5 completed)
**Status**: ✅ Complete

## Overview

Completed all cleanup tasks and fixes from the OpenAPI SDK migration connectivity analysis.

## Tasks Completed

### Task 2: Health Endpoint Path Consistency ✅

**Files Modified**:
- `internal/transport/sdk_client.go` (lines 44, 60)

**Change**: Standardized health endpoint paths to use `/api/v1/health` instead of `/health`.

```go
// BEFORE
resp, err := c.http.Get(c.baseURL + "/health")

// AFTER
resp, err := c.http.Get(c.baseURL + "/api/v1/health")
```

**Why**: Consistency with REST API convention (`/api/v1/*` prefix).

---

### Task 3: mustJSON Helper Panic Fix ✅

**Files Modified**:
- `internal/transport/sdk_client.go` (Chat method, removed mustJSON function)

**Change**: Replaced panic-on-error `mustJSON()` helper with proper error handling.

```go
// BEFORE
func mustJSON(v any) []byte {
    data, err := json.Marshal(v)
    if err != nil {
        panic(err)
    }
    return data
}

// AFTER (inline in Chat method)
reqBody, err := json.Marshal(req)
if err != nil {
    return "", fmt.Errorf("marshal chat request: %w", err)
}
```

**Why**: Go conventions prefer returning errors over panicking for recoverable errors.

---

### Task 4: WebSocket Origin Header Scheme ✅

**Files Modified**:
- `ui/flutter_ui/lib/services/websocket_service.dart` (line 237)

**Change**: Fixed Origin header to use `https://` scheme for `wss://` connections.

```dart
// BEFORE
'Origin': 'http://localhost:$_port',

// AFTER
'Origin': 'https://localhost:$_port',
```

**Why**: Browsers may reject WebSocket connections where the Origin scheme doesn't match the connection scheme. Using `https://` for `wss://` connections is the correct pairing.

---

### Task 5: WebSocket Auth Logic Duplication ✅

**Files Modified**:
- `internal/comm/http/auth.go` - Added exported `ExtractKeyFromRequest()` function
- `internal/comm/http/server.go` - WebSocket handler now calls shared function

**Change**: Refactored 30+ lines of duplicated auth logic to use shared `ExtractKeyFromRequest()` function.

```go
// BEFORE (server.go - 30+ lines duplicated)
authHeader := r.Header.Get("Authorization")
if authHeader == "" {
    token := r.URL.Query().Get("token")
    // ... 15 lines of Bearer prefix parsing ...
}

// AFTER (server.go - 5 lines)
token := ExtractKeyFromRequest(r)
if token == "" {
    s.writeError(w, http.StatusUnauthorized, "unauthorized: missing API token")
    return
}
```

**Benefits**:
- All auth paths (REST, WebSocket, MCP) now use identical extraction logic
- Sec-WebSocket-Protocol header support now consistent across all handlers
- Security gap closed: WebSocket handler now supports all 3 auth methods
- Maintenance burden reduced: new auth methods added in one place

---

## Verification

```bash
# Go build
$ go build ./...
✅ Success

# Go tests
$ go test ./internal/transport/...
ok  github.com/caimlas/meept/internal/transport 0.342s

$ go test ./internal/comm/http/...
ok  github.com/caimlas/meept/internal/comm/http 36.5s

# Flutter analyze (pre-existing errors in sdk_client.dart unrelated to these changes)
$ cd ui/flutter_ui && flutter analyze
✅ websocket_service.dart passes analysis
```

---

## Files Changed

| File | Changes |
|------|---------|
| `internal/transport/sdk_client.go` | Fixed health paths (2), replaced mustJSON with error handling |
| `ui/flutter_ui/lib/services/websocket_service.dart` | Fixed Origin header scheme |
| `internal/comm/http/auth.go` | Added exported `ExtractKeyFromRequest()` |
| `internal/comm/http/server.go` | Refactored WebSocket auth to use shared function |
| `docs/plans/openapi-sdk-cleanup-complete.md` | This document |

---

## Impact Summary

- **Go code**: More idiomatic error handling, consistent API paths, deduplicated auth logic
- **Flutter code**: Correct WebSocket Origin header for secure connections
- **Security**: All auth paths now support identical authentication methods
- **No breaking changes**: All fixes are internal improvements

---

## Commits

1. `79d1a0b3` - fix: OpenAPI SDK connectivity fixes and cleanup
2. `5c1d9a1c` - refactor(http): deduplicate WebSocket auth logic
