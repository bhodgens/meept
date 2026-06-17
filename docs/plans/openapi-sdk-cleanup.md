# OpenAPI SDK Cleanup Plan

**Date**: 2026-06-16
**Status**: Ready for implementation

## Overview

This plan addresses remaining cleanup tasks and lower-priority observations from the OpenAPI SDK migration connectivity analysis.

## Tasks (Ordered by Priority)

### Task 1: Remove Legacy HTTP Client (High Priority)

**File**: `internal/tui/http_client.go` (642 lines)

**Why**: Replaced by `internal/transport/sdk_client.go`. Keeping it creates confusion about which client to use.

**Steps**:
1. Verify no code imports or references `tui/http_client.go`
2. Delete the file
3. Verify `go build ./...` still passes

**Command**:
```bash
# Check for references
rg "http_client|HTTPClient" internal/

# Remove file
rm internal/tui/http_client.go

# Verify build
go build ./...
```

---

### Task 2: Fix Health Endpoint Path Inconsistency (Low Priority)

**Files**:
- `internal/transport/sdk_client.go` (line 44)
- `internal/tui/http_client.go` (if not deleted)

**Issue**: `SDKClient` uses `/health` while legacy `httpClient` uses `/api/v1/health`.

**Fix**: Standardize on `/api/v1/health` since it follows the REST API convention.

**Steps**:
1. Change `sdk_client.go:44` from `/health` to `/api/v1/health`
2. Verify health endpoint works

---

### Task 3: Fix mustJSON Helper Panic (Low Priority)

**File**: `internal/transport/sdk_client.go` (line 527-533)

**Issue**: `mustJSON()` panics on marshal error instead of returning error.

**Current**:
```go
func mustJSON(v any) []byte {
    data, err := json.Marshal(v)
    if err != nil {
        panic(err)
    }
    return data
}
```

**Fix**: Return error and update callers:
```go
func marshalJSON(v any) ([]byte, error) {
    data, err := json.Marshal(v)
    if err != nil {
        return nil, fmt.Errorf("marshal JSON: %w", err)
    }
    return data, nil
}
```

**Steps**:
1. Rename function to `marshalJSON` and return error
2. Update all callers (Chat method, etc.) to handle error
3. Verify `go build ./...` passes

---

### Task 4: WebSocket Origin Header Scheme (Low Priority)

**File**: `ui/flutter_ui/lib/services/websocket_service.dart`

**Issue**: Uses `http://` scheme for WSS connections. Should use `ws://` or `wss://`.

**Steps**:
1. Find WebSocketService origin header
2. Change scheme to match connection type (wss:// for secure, ws:// for insecure)

---

### Task 5: WebSocket Auth Logic Duplication (Very Low Priority)

**File**: `internal/comm/http/ws_handler.go` (or similar)

**Issue**: Extracts API key from headers inline instead of reusing `auth.go extractKey()`.

**Steps**:
1. Find WebSocket auth extraction
2. Refactor to call `auth.ExtractKey()` or similar shared function

---

## Verification

After all fixes:
```bash
# Go build
go build ./...

# Flutter analyze
cd ui/flutter_ui && flutter analyze

#Run tests
go test ./...
```

## Implementation Notes

- **Task 1 is the highest value** - removes 642 lines of dead code
- **Tasks 2-5 are cosmetic** - don't block functionality, improve code quality
- Consider doing Task 1 only and leaving 2-5 for incremental improvement
