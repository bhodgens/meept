# OpenAPI SDK Migration Plan

**Created**: 2026-06-16
**Status**: Pending approval
**Estimated effort**: 16-24 hours total

## Overview

Migrate meept TUI (Go) and Flutter GUI to use the generated OpenAPI SDK from `sdk/go` and `sdk/dart`, replacing manual HTTP client implementations with type-safe, auto-generated code.

## Prerequisites

- Generated SDKs exist in `sdk/go/` and `sdk/dart/`
- GitHub Actions workflow ready for CI regeneration
- Existing HTTP clients functional in both clients

---

## Phase 1: Fix OpenAPI Spec Duplicates

**Estimated effort**: 4-6 hours
**Subagent**: `openapi-spec-fixer` (needs file write access)

### Problem

The OpenAPI spec at `docs/reference/http-api/openapi.yaml` has 17 paths duplicated with different HTTP methods. Each path should group all methods together:

```yaml
# WRONG - current structure
/api/v1/calendar/events:
  post: {...}
/api/v1/calendar/events:  # DUPLICATE!
  get: {...}

# CORRECT - should be:
/api/v1/calendar/events:
  post: {...}
  get: {...}
```

### Duplicate Paths (26 total occurrences)

| Path | Count | Methods |
|------|-------|---------|
| `/api/v1/calendar/events` | 2 | POST, GET |
| `/api/v1/calendar/events/{id}` | 3 | DELETE, PUT, GET |
| `/api/v1/config/agents/{id}` | 3 | POST, GET, DELETE |
| `/api/v1/config/client` | 2 | GET, POST |
| `/api/v1/config/menubar` | 2 | GET, POST |
| `/api/v1/config/models` | 2 | POST, GET |
| `/api/v1/models/credentials/{provider}` | 3 | POST, GET, DELETE |
| `/api/v1/models/default` | 2 | GET, POST |
| `/api/v1/plans` | 2 | GET, POST |
| `/api/v1/projects` | 2 | GET, POST |
| `/api/v1/projects/{id}` | 2 | GET, DELETE |
| `/api/v1/queue/jobs` | 2 | GET, POST |
| `/api/v1/scheduler/jobs` | 2 | GET, POST |
| `/api/v1/sessions` | 2 | GET, POST |
| `/api/v1/sessions/{id}` | 2 | GET, DELETE |
| `/api/v1/tasks` | 2 | GET, POST |
| `/api/v1/tasks/{id}` | 3 | GET, DELETE, PUT |

### Subagent Instructions

1. **Read the full spec** to understand the correct structure
2. **Merge duplicate paths** - for each path, combine all HTTP methods under a single path key
3. **Validate** by running `make sdk-generate-go` - strict generators should no longer fail
4. **Test** that the Go SDK still generates correctly

### Success Criteria

- `make sdk-generate-go` completes without "duplicate path" errors
- OpenAPI spec is valid YAML with unique path keys
- All 4159 lines consolidate to expected ~200 unique endpoints

---

## Phase 2: Go TUI SDK Integration

**Estimated effort**: 6-8 hours
**Subagent**: `go-sdk-integrator` (needs Go coding + testing)

### Current State

`internal/tui/http_client.go` - 640 lines of manual HTTP calls using `map[string]string` and anonymous structs.

### Target State

Replace with `sdk/go` client wrapper (~200 lines) that:
- Uses generated `APIClient` from `sdk/go`
- Uses generated request/response models
- Implements the `transport.Client` interface

### Subagent Instructions

1. **Read existing client** - `internal/tui/http_client.go` and `internal/transport/client.go`
2. **Create wrapper** - `internal/transport/sdk_client.go`:
   ```go
   type SDKClient struct {
       apiClient *meeptclient.APIClient
       cfg        *meeptclient.Configuration
   }

   func NewSDKClient(baseURL string, timeout time.Duration) *SDKClient {
       cfg := meeptclient.NewConfiguration()
       cfg.Host = extractHost(baseURL)
       cfg.Scheme = extractScheme(baseURL)
       cfg.HTTPClient = &http.Client{Timeout: timeout}
       return &SDKClient{
           apiClient: meeptclient.NewAPIClient(cfg),
           cfg:       cfg,
       }
   }
   ```
3. **Implement all interface methods** using generated SDK calls:
   ```go
   func (c *SDKClient) Chat(message, conversationID string) (string, error) {
       req := meeptclient.NewChatRequest()
       req.SetMessage(message)
       req.SetConversationID(conversationID)

       resp, httpResp, err := c.apiClient.V1API.ApiV1ChatPost(
           context.Background(),
       ).ChatRequest(*req).Execute()

       if err != nil {
           return "", err
       }
       defer httpResp.Body.Close()
       return resp.GetReply(), nil
   }
   ```
4. **Replace usage** - update `internal/transport/client.go:New()` to return `SDKClient` for HTTP transport
5. **Test** - `make build` and `agent-tui ./bin/meept chat`

### Files to Modify

- `internal/transport/sdk_client.go` - NEW
- `internal/transport/client.go` - wire new client
- `internal/tui/http_client.go` - DELETE (or keep for fallback)

### Success Criteria

- `go build ./...` succeeds
- `agent-tui ./bin/meept chat` works with HTTP transport
- All type references use generated models, not maps

---

## Phase 3: Flutter Dart SDK Integration

**Estimated effort**: 6-8 hours
**Subagent**: `flutter-sdk-integrator` (needs Dart/Flutter coding)

### Current State

`ui/flutter_ui/lib/services/api_client.dart` - manual HTTP calls with `http.post()` and JSON serialization.

### Target State

Replace with `sdk/dart` generated client.

### Subagent Instructions

1. **Regenerate Dart SDK** - `make sdk-generate-dart`
2. **Read existing client** - `ui/flutter_ui/lib/services/api_client.dart`
3. **Create wrapper** - `ui/flutter_ui/lib/services/sdk_client.dart`:
   ```dart
   import 'package:meept_client/meept_client.dart';

   class SdkApiClient {
     final MeeptClient _client;

     SdkApiClient(String baseUrl, String? apiKey)
       : _client = MeeptClient(
           baseUrl: baseUrl,
           authentication: apiKey != null
             ? HttpBearerAuth(token: apiKey)
             : null,
         );

     Future<String> chat(String message, String conversationId) async {
       final req = ChatRequest();
       req.message = message;
       req.conversationId = conversationId;

       final resp = await _client.v1API.apiV1ChatPost(chatRequest: req);
       return resp.reply;
     }
   }
   ```
4. **Replace usage** - update widget code to use `SdkApiClient`
5. **Keep WebSocketService** - it handles real-time updates separately

### Files to Modify

- `ui/flutter_ui/lib/services/sdk_client.dart` - NEW
- `ui/flutter_ui/lib/services/api_client.dart` - REPLACE or DELETE
- `ui/flutter_ui/pubspec.yaml` - add generated SDK as local package

### Success Criteria

- `flutter build` succeeds
- Flutter app connects to daemon over HTTP
- Chat, memory query, and task list work

---

## Phase 4: Verification and Cleanup

**Estimated effort**: 2-3 hours
**Subagent**: `integration-verifier`

### Verification Steps

1. **Go tests**: `go test ./internal/transport/... -v`
2. **Flutter tests**: `cd ui/flutter_ui && flutter test`
3. **Integration test**: Start daemon, connect both TUI and Flutter
4. **TypeChat coverage**: Verify all endpoints used by both clients exist in SDK

### Cleanup

1. Remove unused imports
2. Run `go fmt ./...` and `dart format .`
3. Update CLAUDE.md with new SDK usage patterns
4. Update SDK README if needed

### Success Criteria

- All tests pass
- No TODO comments or stubs
- Documentation updated

---

## Rollback Plan

If SDK integration fails:

1. **Revert git commits** - each phase should be a separate commit
2. **Restore backup** - `http_client.go` and `api_client.dart` backed up before deletion
3. **Fallback** - keep using manual clients temporarily

---

## Acceptance Criteria (All Phases)

- [ ] OpenAPI spec has zero duplicate paths
- [ ] `make sdk-generate` succeeds without `--skip-validate-spec`
- [ ] Go TUI builds and runs with SDK client
- [ ] Flutter GUI builds and runs with SDK client
- [ ] All existing endpoints functional
- [ ] Type safety enforced (no `map[string]string` in client code)
- [ ] GitHub Actions regenerates SDKs on spec change

---

## Dependencies

```
Phase 1 (Spec Fix) --> Phase 2 (Go SDK) --> Phase 4 (Verification)
                  \-> Phase 3 (Flutter SDK) -/
```

Phases 2 and 3 can run in parallel after Phase 1 completes.

---

## Subagent Dispatch Template

Use this prompt structure for each phase:

```markdown
You are implementing Phase X of the OpenAPI SDK migration.

CONTEXT:
- Goal: [phase goal]
- Files to read: [list]
- Files to create/modify: [list]

INSTRUCTIONS:
1. [step 1]
2. [step 2]
...

SUCCESS CRITERIA:
- [criterion 1]
- [criterion 2]

CONSTRAINTS:
- Do NOT modify [files]
- Keep [X] compatible with [Y]
- Tests must pass before committing
```
