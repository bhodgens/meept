# OpenAPI SDK Cleanup - Complete

**Date**: 2026-06-16
**Status**: ✅ Complete

## Overview

Completed lower-priority cleanup tasks and fixes from the OpenAPI SDK migration connectivity analysis.

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

## Tasks Deferred

### Task 1: Remove Legacy HTTP Client

**Status**: Already deleted in previous session

The file `internal/tui/http_client.go` was already removed during the connectivity fix session. The `internal/transport/http_client.go` file is actively used (it's the HTTP-backed transport client, not legacy).

### Task 5: WebSocket Auth Logic Duplication

**Status**: Deferred (very low priority)

The WebSocket handler extracts API keys inline rather than reusing `auth.go`'s `extractKey()` function. This is code duplication but doesn't cause functional issues - both paths work correctly. Can be refactored incrementally.

---

## Verification

```bash
# Go build
$ go build ./...
✅ Success

# Go tests
$ go test ./internal/transport/...
ok  github.com/caimlas/meept/internal/transport 0.342s

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
| `docs/plans/openapi-sdk-cleanup-complete.md` | This document |

---

## Impact Summary

- **Go code**: More idiomatic error handling, consistent API paths
- **Flutter code**: Correct WebSocket Origin header for secure connections
- **No breaking changes**: All fixes are internal improvements
