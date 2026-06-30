# Client Config PATCH Merge-Patch Route Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an RFC 7396 JSON merge-patch endpoint `PATCH /api/v1/config/client` that atomically merges a partial update into `~/.meept/client.json5` and returns the merged result, then wire the Flutter and TUI verbosity cycle to persist across restarts.

**Architecture:** The backend reads the on-disk JSON5 fresh, standardizes it to JSON via `hujson.Standardize`, unmarshals into `map[string]any`, applies the existing `deepMerge` (now exported as `DeepMerge`), and writes back atomically (temp file + `os.Rename`). Flutter's `setClientConfig` switches from POST to PATCH and `verbosityProvider.cycle()` fire-and-forgets a merge-patch on each cycle. The TUI's Ctrl+V handler does the same via a direct disk-write helper mirroring the existing load path.

**Tech Stack:** Go 1.22+ method-pattern `ServeMux`, `github.com/tailscale/hujson`, `encoding/json`, Flutter Riverpod, `package:dio`, Dart async.

---

## File Structure

**Backend (Go):**
- Modify: `internal/config/merger.go` — rename `deepMerge` → `DeepMerge` (export)
- Modify: `internal/config/merger_test.go` — update test call sites from `deepMerge` → `DeepMerge`
- Modify: `internal/comm/http/config_service.go` — add `PatchClientConfig(patch map[string]any) (map[string]any, error)`
- Modify: `internal/comm/http/server.go` — register `PATCH /api/v1/config/client` route + add `handleClientConfigPatch`
- Modify: `internal/comm/http/server_test.go` — add handler tests
- Create: `internal/comm/http/config_service_test.go` — add `ConfigService.PatchClientConfig` tests (file-scoped fixture)

**Flutter (Dart):**
- Modify: `ui/flutter_ui/lib/services/sdk_client.dart:1180-1200` — switch `setClientConfig` from POST to PATCH; update docstring
- Modify: `ui/flutter_ui/lib/providers/verbosity_provider.dart` — make `VerbosityNotifier` accept an optional `Ref` so `cycle()` can persist via fire-and-forget
- Modify: `ui/flutter_ui/test/providers/verbosity_provider_test.dart` — assert `setClientConfig` is invoked on cycle (with a stub)

**TUI (Go):**
- Modify: `internal/tui/config.go` — add `persistVerbosity(path, level string) error` helper
- Modify: `internal/tui/app.go:725-730` — call `persistVerbosity` on Ctrl+V cycle (goroutine)

**Docs:**
- Modify: `docs/reference/http-api.md` — add PATCH row + example
- Modify: `docs/workflows/flutter_gui.md` — note verbosity now persists

---

## Phase 1: Backend foundation

### Task 1.1: Export `deepMerge` as `DeepMerge`

The existing `deepMerge` in `internal/config/merger.go:239-261` already implements RFC 7396 semantics (null deletes key, objects recurse, scalars/arrays replace). Exporting it avoids duplicating the logic. This is a pure rename — no behavior change.

**Files:**
- Modify: `internal/config/merger.go:191, 233-261` (definition + one internal caller)
- Modify: `internal/config/merger_test.go:278, 306, 326, 347, 368, 397` (six test call sites)

- [ ] **Step 1: Rename the function definition and update the doc comment**

In `internal/config/merger.go`, locate the function at line 239:

```go
// DeepMerge returns a new map that merges src on top of dst:
//   - Object values are recursively merged.
//   - Array and scalar values from src replace the corresponding dst value.
//   - A JSON null in src deletes the corresponding key from dst.
//
// dst is not mutated; the returned map shares sub-maps only at unmodified keys.
func DeepMerge(dst, src map[string]any) map[string]any {
```

(Rename `deepMerge` → `DeepMerge`. Body unchanged.)

Update the recursive call at line 253:

```go
					out[k] = DeepMerge(dvMap, svMap)
```

Update the single internal caller at line 191:

```go
	merged = DeepMerge(merged, srcObj)
```

- [ ] **Step 2: Update test call sites**

In `internal/config/merger_test.go`, replace every `deepMerge(` with `DeepMerge(`. Six call sites at lines 278, 306, 326, 347, 368, 397. The test function names (`TestDeepMerge_*`) already use the capitalized form and don't need changes.

- [ ] **Step 3: Build and run tests**

Run: `go build ./internal/config/...`
Expected: builds clean.

Run: `go test ./internal/config/... -run TestDeepMerge -v`
Expected: all 6 deepMerge tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/config/merger.go internal/config/merger_test.go
git commit -m "refactor(config): export DeepMerge for HTTP merge-patch

Rename package-private deepMerge to exported DeepMerge so the
HTTP layer can reuse the RFC 7396 merge semantics. Pure rename,
no behavior change. All 6 deepMerge tests still pass."
```

---

### Task 1.2: Add `ConfigService.PatchClientConfig`

Add the core merge-patch method on `ConfigService`. It reads `client.json5` fresh from disk, standardizes JSON5 → JSON, unmarshals into a map, deep-merges the patch, writes back atomically (temp + rename), and returns the merged map. This mirrors the atomic-write pattern in `SaveOrchestratorConfig` at `config_service.go:509-561`.

**Files:**
- Modify: `internal/config/merger.go` (already exported in Task 1.1)
- Modify: `internal/comm/http/config_service.go` — append new method
- Create: `internal/comm/http/config_service_test.go` — TDD test file

- [ ] **Step 1: Write the failing test**

Create `internal/comm/http/config_service_test.go`:

```go
package http

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPatchClientConfig_MergesNestedKey verifies a patch is deep-merged
// onto the on-disk client.json5.
func TestPatchClientConfig_MergesNestedKey(t *testing.T) {
	dir := t.TempDir()
	cs := &ConfigService{meeptDir: dir}

	// Seed an existing client.json5 with JSON5 (comment + unquoted keys).
	seed := `{
  // client config
  theme: "system",
  chat: {
    verbosity: "normal",
    scroll_speed: 3
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "client.json5"), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	patch := map[string]any{
		"chat": map[string]any{
			"verbosity": "verbose",
		},
	}

	merged, err := cs.PatchClientConfig(patch)
	if err != nil {
		t.Fatalf("PatchClientConfig: %v", err)
	}

	// Returned map reflects the merge.
	chat, ok := merged["chat"].(map[string]any)
	if !ok {
		t.Fatalf("expected chat map, got %T", merged["chat"])
	}
	if chat["verbosity"] != "verbose" {
		t.Errorf("verbosity = %v, want verbose", chat["verbosity"])
	}
	if chat["scroll_speed"] != float64(3) {
		t.Errorf("scroll_speed not preserved: got %v", chat["scroll_speed"])
	}
	if merged["theme"] != "system" {
		t.Errorf("theme not preserved: got %v", merged["theme"])
	}

	// On-disk file is valid JSON (comments stripped by Standardize).
	reread, err := os.ReadFile(filepath.Join(dir, "client.json5"))
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(reread, &onDisk); err != nil {
		t.Fatalf("on-disk file is not valid JSON: %v\n%s", err, reread)
	}
	chat2, _ := onDisk["chat"].(map[string]any)
	if chat2["verbosity"] != "verbose" {
		t.Errorf("on-disk verbosity = %v, want verbose", chat2["verbosity"])
	}
}

// TestPatchClientConfig_NullDeletesKey verifies RFC 7396 null-deletes-key.
func TestPatchClientConfig_NullDeletesKey(t *testing.T) {
	dir := t.TempDir()
	cs := &ConfigService{meeptDir: dir}

	seed := `{"keep": 1, "drop": "x"}`
	if err := os.WriteFile(filepath.Join(dir, "client.json5"), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	patch := map[string]any{"drop": nil}
	merged, err := cs.PatchClientConfig(patch)
	if err != nil {
		t.Fatalf("PatchClientConfig: %v", err)
	}
	if _, exists := merged["drop"]; exists {
		t.Errorf("expected 'drop' deleted by null patch")
	}
	if merged["keep"] != float64(1) {
		t.Errorf("expected 'keep' preserved, got %v", merged["keep"])
	}
}

// TestPatchClientConfig_CreatesFileWhenMissing verifies the file is
// created from the patch when client.json5 doesn't yet exist.
func TestPatchClientConfig_CreatesFileWhenMissing(t *testing.T) {
	dir := t.TempDir()
	cs := &ConfigService{meeptDir: dir}

	patch := map[string]any{"chat": map[string]any{"verbosity": "quiet"}}
	merged, err := cs.PatchClientConfig(patch)
	if err != nil {
		t.Fatalf("PatchClientConfig: %v", err)
	}
	chat, _ := merged["chat"].(map[string]any)
	if chat["verbosity"] != "quiet" {
		t.Errorf("got %v, want quiet", chat["verbosity"])
	}

	// File exists on disk now.
	if _, err := os.Stat(filepath.Join(dir, "client.json5")); err != nil {
		t.Errorf("expected file created: %v", err)
	}
}

// TestPatchClientConfig_InvalidJSON5 verifies an unparseable existing
// file surfaces an error rather than silently corrupting state.
func TestPatchClientConfig_InvalidJSON5(t *testing.T) {
	dir := t.TempDir()
	cs := &ConfigService{meeptDir: dir}

	bad := []byte("{not valid json5")
	if err := os.WriteFile(filepath.Join(dir, "client.json5"), bad, 0o600); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	_, err := cs.PatchClientConfig(map[string]any{"x": 1})
	if err == nil {
		t.Fatal("expected error parsing invalid JSON5, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/comm/http/ -run TestPatchClientConfig -v`
Expected: FAIL with compile error (`cs.PatchClientConfig undefined`).

- [ ] **Step 3: Implement `PatchClientConfig`**

Append to `internal/comm/http/config_service.go` (after `SaveOrchestratorConfig`, around line 561):

```go
// PatchClientConfig applies an RFC 7396 JSON merge-patch to the on-disk
// client.json5. It reads the file fresh, standardizes JSON5 → JSON via
// hujson, unmarshals into map[string]any, deep-merges the patch, and
// writes back atomically (temp file + rename). The merged map is
// returned (as plain JSON data — comments from the source file are
// stripped by Standardize). If client.json5 does not exist, it is
// created from the patch alone.
//
// Merge semantics (RFC 7396):
//   - A null value in patch deletes the corresponding key in the target.
//   - Object values are merged recursively.
//   - Arrays and scalars from patch replace the target value.
func (s *ConfigService) PatchClientConfig(patch map[string]any) (map[string]any, error) {
	path := s.getClientConfigPath()

	// Read existing content if present, otherwise start from an empty object.
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read client config: %w", err)
	}
	if os.IsNotExist(err) {
		existing = []byte("{}")
	}

	// Standardize JSON5 (strip comments, trailing commas, quote keys).
	stdJSON, err := hujson.Standardize(existing)
	if err != nil {
		return nil, fmt.Errorf("failed to parse client.json5: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(stdJSON, &root); err != nil {
		return nil, fmt.Errorf("failed to unmarshal client.json5: %w", err)
	}
	if root == nil {
		root = map[string]any{}
	}

	merged := configCli.DeepMerge(root, patch)

	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged client config: %w", err)
	}
	out = append(out, '\n')

	// Atomic write: temp file + rename. Mirrors SaveOrchestratorConfig.
	tmpPath := path + ".tmp"
	//nolint:gosec // user config file; restrictive perms intended
	if err := os.WriteFile(tmpPath, out, 0o600); err != nil {
		return nil, fmt.Errorf("failed to write client config temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if removeErr := os.Remove(tmpPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return nil, fmt.Errorf("failed to rename client config into place (cleanup also failed: %v): %w", removeErr, err)
		}
		return nil, fmt.Errorf("failed to rename client config into place: %w", err)
	}

	return merged, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/comm/http/ -run TestPatchClientConfig -v`
Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/comm/http/config_service.go internal/comm/http/config_service_test.go
git commit -m "feat(http): add ConfigService.PatchClientConfig merge-patch

RFC 7396 JSON merge-patch for ~/.meept/client.json5: reads on-disk,
standardizes JSON5 to JSON via hujson, deep-merges the patch, writes
back atomically (temp + rename). Returns the merged map. 4 tests
covering nested merge, null-delete, file-create, and invalid JSON5."
```

---

### Task 1.3: Add HTTP handler `handleClientConfigPatch` + register route

Wire the merge-patch behind a Go 1.22 method-pattern route. Unlike the strict-field PATCH precedent (`handleSessionArchive`), merge-patch must accept arbitrary keys (the whole point), so the decoder uses a plain `map[string]any`.

**Files:**
- Modify: `internal/comm/http/server.go:942` (route registration block) and end of `handleSaveClientConfig` block (around line 1449) for the new handler
- Modify: `internal/comm/http/server_test.go` — add handler tests

- [ ] **Step 1: Write the failing handler tests**

Append to `internal/comm/http/server_test.go`:

```go
// TestHandleClientConfigPatch_MergesAndReturns tests the full handler
// path: request body is decoded, PatchClientConfig is invoked, and the
// merged map is returned as JSON.
func TestHandleClientConfigPatch_MergesAndReturns(t *testing.T) {
	dir := t.TempDir()
	cs := &ConfigService{meeptDir: dir}
	server := NewServer(ServerConfig{}, cs, nil, nil, nil, nil)

	// Seed an existing client.json5.
	seed := `{"chat":{"verbosity":"normal"}}`
	if err := os.WriteFile(filepath.Join(dir, "client.json5"), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	body := `{"chat":{"verbosity":"verbose"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/config/client", strings.NewReader(body))
	w := httptest.NewRecorder()

	server.handleClientConfigPatch(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	chat, ok := result["chat"].(map[string]any)
	if !ok {
		t.Fatalf("expected chat map, got %T", result["chat"])
	}
	if chat["verbosity"] != "verbose" {
		t.Errorf("verbosity = %v, want verbose", chat["verbosity"])
	}
}

// TestHandleClientConfigPatch_NoConfigService verifies the 503 path.
func TestHandleClientConfigPatch_NoConfigService(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/config/client", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	server.handleClientConfigPatch(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

// TestHandleClientConfigPatch_InvalidBody verifies malformed JSON is
// rejected with 400.
func TestHandleClientConfigPatch_InvalidBody(t *testing.T) {
	dir := t.TempDir()
	cs := &ConfigService{meeptDir: dir}
	server := NewServer(ServerConfig{}, cs, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/config/client", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	server.handleClientConfigPatch(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestHandleClientConfigPatch_NonObjectBody verifies a JSON non-object
// (array, string) is rejected.
func TestHandleClientConfigPatch_NonObjectBody(t *testing.T) {
	dir := t.TempDir()
	cs := &ConfigService{meeptDir: dir}
	server := NewServer(ServerConfig{}, cs, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/config/client", strings.NewReader(`["not","an","object"]`))
	w := httptest.NewRecorder()

	server.handleClientConfigPatch(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (non-object body)", resp.StatusCode, http.StatusBadRequest)
	}
}
```

If the test file does not already import `"os"` and `"path/filepath"`, add them to the import block.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/comm/http/ -run TestHandleClientConfigPatch -v`
Expected: FAIL with compile error (`server.handleClientConfigPatch undefined`).

- [ ] **Step 3: Register the route**

In `internal/comm/http/server.go`, locate the client config routes at line 941-942. Add the PATCH route immediately after:

```go
	mux.HandleFunc("GET /api/v1/config/client", s.handleGetClientConfig)
	mux.HandleFunc("POST /api/v1/config/client", s.handleSaveClientConfig)
	mux.HandleFunc("PATCH /api/v1/config/client", s.handleClientConfigPatch)
```

- [ ] **Step 4: Add the handler**

Insert immediately after `handleSaveClientConfig` (around line 1449):

```go
// handleClientConfigPatch handles PATCH /api/v1/config/client.
//
// Body: any JSON object — merged into client.json5 per RFC 7396
// (null deletes key, objects recurse, scalars/arrays replace).
// Response: 200 with the merged config as JSON.
func (s *Server) handleClientConfigPatch(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}

	// Decode into map[string]any. Unlike strict-field handlers, merge-patch
	// MUST accept arbitrary keys — DisallowUnknownFields is intentionally
	// not set. Use MaxBytesReader via readJSON-style pattern.
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	// Reject non-object payloads (json.Decode into map succeeds for some
	// edge cases but the resulting map is nil for `null`, `[]`, etc.).
	if patch == nil {
		s.writeError(w, http.StatusBadRequest, "request body must be a JSON object")
		return
	}

	merged, err := s.configService.PatchClientConfig(patch)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, merged)
}
```

- [ ] **Step 5: Run handler tests**

Run: `go test ./internal/comm/http/ -run TestHandleClientConfigPatch -v`
Expected: all 4 tests PASS.

- [ ] **Step 6: Run the full http package test suite to check for regressions**

Run: `go test ./internal/comm/http/...`
Expected: all tests PASS (no new failures).

- [ ] **Step 7: Commit**

```bash
git add internal/comm/http/server.go internal/comm/http/server_test.go
git commit -m "feat(http): add PATCH /api/v1/config/client merge-patch route

Registers a Go 1.22 method-pattern PATCH route and adds
handleClientConfigPatch. Decodes the body into map[string]any (unknown
fields allowed), invokes ConfigService.PatchClientConfig, and returns
the merged config as JSON. 4 handler tests covering success, 503, 400
malformed, and 400 non-object body."
```

---

## Phase 2: Flutter wiring

### Task 2.1: Switch `setClientConfig` from POST to PATCH

The Flutter `SdkClient.setClientConfig` is already written but uses POST (the footgun — POST expects `{"content": <full-string>}`, so a partial map silently truncates). Switch it to PATCH and update the docstring to remove the WARNING block.

**Files:**
- Modify: `ui/flutter_ui/lib/services/sdk_client.dart:1180-1200`

- [ ] **Step 1: Update `setClientConfig`**

In `ui/flutter_ui/lib/services/sdk_client.dart`, replace lines 1180-1200 (the `setClientConfig` method and its docstring) with:

```dart
  /// PATCH /api/v1/config/client — merge-patch a partial update into the
  /// client config. Follows RFC 7396: null deletes a key, objects recurse,
  /// scalars/arrays replace. Returns the merged config as JSON.
  ///
  /// Example: `setClientConfig({'chat': {'verbosity': 'verbose'}})`.
  Future<void> setClientConfig(Map<String, dynamic> patch) async {
    await _patch('/api/v1/config/client', body: patch);
  }
```

Also remove the `// ignore: unused_element` line above the method (it will now be used by Task 2.2).

- [ ] **Step 2: Verify the Flutter analyzer picks up the change**

Run: `cd ui/flutter_ui && flutter analyze lib/services/sdk_client.dart`
Expected: no errors or warnings related to `setClientConfig`.

- [ ] **Step 3: Commit**

```bash
git add ui/flutter_ui/lib/services/sdk_client.dart
git commit -m "feat(flutter): switch setClientConfig from POST to PATCH

SdkClient.setClientConfig now hits PATCH /api/v1/config/client (RFC
7396 merge-patch) instead of POST (which expected full-file content
and would silently truncate partial maps). Removes the WARNING
docstring and the unused_element suppression."
```

---

### Task 2.2: Wire `verbosityProvider.cycle()` to persist via fire-and-forget PATCH

Make `VerbosityNotifier` Riverpod-aware so `cycle()` fire-and-forgets a `setClientConfig` call after updating state. The persistence call runs in the background; failures are logged but never revert UI state.

**Files:**
- Modify: `ui/flutter_ui/lib/providers/verbosity_provider.dart`
- Modify: `ui/flutter_ui/test/providers/verbosity_provider_test.dart`

- [ ] **Step 1: Write the failing test**

Update `ui/flutter_ui/test/providers/verbosity_provider_test.dart` to add a persistence test. Replace the file contents with:

```dart
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/providers/verbosity_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// Stand-in SdkClient that records setClientConfig invocations.
class _RecordingClient implements SdkClient {
  List<Map<String, dynamic>> patches = [];

  @override
  Future<void> setClientConfig(Map<String, dynamic> patch) async {
    patches.add(patch);
  }

  // The remaining SdkClient surface is unused by these tests; throws keep
  // us honest if a test strays into real network calls. Implementations
  // are stubbed with UnimplementedError rather than `throw` so the test
  // file compiles cleanly against the evolving interface.
  @override
  dynamic noSuchMethod(Invocation invocation) =>
      throw UnimplementedError('SdkClient.${invocation.memberName} not stubbed');
}

void main() {
  group('verbosityProvider', () {
    test('initial value defaults to normal (1)', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);
      expect(container.read(verbosityProvider), 1);
    });

    test('cycle rotates 1 -> 2 -> 0 -> 1', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);

      container.read(verbosityProvider.notifier).cycle();
      expect(container.read(verbosityProvider), 2);

      container.read(verbosityProvider.notifier).cycle();
      expect(container.read(verbosityProvider), 0);

      container.read(verbosityProvider.notifier).cycle();
      expect(container.read(verbosityProvider), 1);
    });

    test('VerbosityLevel.name returns correct strings', () {
      expect(VerbosityLevel.name(VerbosityLevel.quiet), 'quiet');
      expect(VerbosityLevel.name(VerbosityLevel.normal), 'normal');
      expect(VerbosityLevel.name(VerbosityLevel.verbose), 'verbose');
    });

    test('shouldEmitAgentEvent drops events with tier > current verbosity', () {
      expect(shouldEmitAgentEvent(currentVerbosity: 1, eventTier: 0), isTrue);
      expect(shouldEmitAgentEvent(currentVerbosity: 1, eventTier: 1), isTrue);
      expect(shouldEmitAgentEvent(currentVerbosity: 1, eventTier: 2), isFalse);
    });

    test('cycle persists new verbosity via setClientConfig (fire-and-forget)',
        () async {
      final client = _RecordingClient();
      final container = ProviderContainer(
        overrides: [
          sdkClientProvider.overrideWithValue(client),
        ],
      );
      addTearDown(container.dispose);

      container.read(verbosityProvider.notifier).cycle();
      // Fire-and-forget runs in a microtask; pump to let it land.
      await Future<void>.delayed(Duration.zero);

      expect(container.read(verbosityProvider), 2);
      expect(client.patches.length, 1);
      expect(client.patches.last, {'chat': {'verbosity': 'verbose'}});
    });
  });
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd ui/flutter_ui && flutter test test/providers/verbosity_provider_test.dart`
Expected: FAIL — `sdkClientProvider` undefined (not yet wired into `VerbosityNotifier`).

- [ ] **Step 3: Update `VerbosityNotifier` to persist on cycle**

Replace the contents of `ui/flutter_ui/lib/providers/verbosity_provider.dart`:

```dart
import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/sdk_client.dart';

/// Verbosity levels mirror the TUI's VerbosityLevel enum
/// (internal/tui/app.go:52-58). Values:
///   0 = quiet   — only high-level completion events
///   1 = normal  — tool results + agent completions (default)
///   2 = verbose — everything including tool starts
class VerbosityLevel {
  const VerbosityLevel._();
  static const int quiet = 0;
  static const int normal = 1;
  static const int verbose = 2;

  static String name(int level) {
    switch (level) {
      case quiet:
        return 'quiet';
      case verbose:
        return 'verbose';
      default:
        return 'normal';
    }
  }
}

/// Current verbosity level. cycle() updates state and fire-and-forgets a
/// merge-patch to client.json5 so the value persists across restarts.
class VerbosityNotifier extends StateNotifier<int> {
  final SdkClient? _client;

  VerbosityNotifier(this._client) : super(VerbosityLevel.normal);

  /// Cycle 0 -> 1 -> 2 -> 0. Matches TUI Ctrl+V (app.go:727).
  /// After updating state, fire-and-forget a setClientConfig call to
  /// persist the new level. Failures are debug-logged only — UI state
  /// is never reverted on persistence failure.
  void cycle() {
    state = (state + 1) % 3;
    final client = _client;
    if (client == null) return;
    final name = VerbosityLevel.name(state);
    // Fire-and-forget; never await in cycle (callers expect sync UI update).
    Future<void>.microtask(() async {
      try {
        await client.setClientConfig({'chat': {'verbosity': name}});
      } catch (e) {
        debugPrint('verbosityProvider: persist failed: $e');
      }
    });
  }
}

/// Provides the singleton [SdkClient] used by [VerbosityNotifier] for
/// persistence. Tests override this with a stub.
final sdkClientProvider = Provider<SdkClient?>((ref) => null);

final verbosityProvider =
    StateNotifierProvider<VerbosityNotifier, int>((ref) {
  return VerbosityNotifier(ref.watch(sdkClientProvider));
});

/// Pure predicate used by ChatNotifier to filter agent events by tier.
/// Mirrors TUI app.go:1347: `if tier <= a.verbosity`.
bool shouldEmitAgentEvent(
    {required int currentVerbosity, required int eventTier}) {
  return eventTier <= currentVerbosity;
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ui/flutter_ui && flutter test test/providers/verbosity_provider_test.dart`
Expected: all 5 tests PASS (including the new persistence test).

- [ ] **Step 5: Wire `sdkClientProvider` to the real `SdkClient` instance**

Find where the app initializes the live `SdkClient` instance (search for `SdkClient(` or `Dio`): Run `grep -n "SdkClient(" ui/flutter_ui/lib/main.dart ui/flutter_ui/lib/app.dart 2>/dev/null`. The real provider override goes wherever the app bootstraps providers — typically `main.dart` or a `ProviderScope(overrides: ...)` at the root.

If `SdkClient` is already provided as a singleton (e.g., via an existing provider), update `sdkClientProvider` to delegate to it. If not, the bootstrap creates it:

```dart
// In main.dart or app.dart, inside the ProviderScope overrides:
final sdkClient = SdkClient(/* existing args */);
ProviderScope(
  overrides: [
    sdkClientProvider.overrideWithValue(sdkClient),
    // ...existing overrides
  ],
  child: const MeeptApp(),
)
```

If you cannot find the bootstrap site, run `grep -rn "ProviderScope" ui/flutter_ui/lib` to locate the root override list.

- [ ] **Step 6: Verify the analyzer and full provider test suite pass**

Run: `cd ui/flutter_ui && flutter analyze lib/providers/verbosity_provider.dart`
Expected: no errors.

Run: `cd ui/flutter_ui && flutter test test/providers/`
Expected: all provider tests PASS.

- [ ] **Step 7: Commit**

```bash
git add ui/flutter_ui/lib/providers/verbosity_provider.dart ui/flutter_ui/test/providers/verbosity_provider_test.dart
git commit -m "feat(flutter): persist verbosity on cycle via PATCH merge-patch

VerbosityNotifier now accepts an optional SdkClient and fire-and-forgets
a setClientConfig({'chat': {'verbosity': name}}) call on each cycle.
Failures are debug-logged only — UI state never reverts. Adds a new
sdkClientProvider so tests can inject a recording stub. 5 tests pass."
```

---

## Phase 3: TUI wiring

### Task 3.1: Add `persistVerbosity` helper in `internal/tui/config.go`

The TUI loads `client.json5` via `LoadClientConfig` (which reads project-local then home dir). Persisting the verbosity level requires writing to the SAME path the load succeeded from. The helper mirrors the HTTP layer's pattern: read → standardize → unmarshal → mutate → atomic write.

**Files:**
- Modify: `internal/tui/config.go` — append helper near `LoadClientConfig`
- Create: `internal/tui/config_persist_test.go` — TDD test

- [ ] **Step 1: Write the failing test**

Create `internal/tui/config_persist_test.go`:

```go
package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPersistVerbosity_UpdatesExistingFile verifies the helper writes
// chat.verbosity into the on-disk client.json5 without clobbering other keys.
func TestPersistVerbosity_UpdatesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.json5")

	seed := `{
  // comment
  chat: {
    verbosity: "normal",
    scroll_speed: 3
  },
  theme: "monokai"
}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := persistVerbosity(path, "verbose"); err != nil {
		t.Fatalf("persistVerbosity: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	// File must be valid JSON (comments stripped by Standardize).
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("on-disk file not valid JSON: %v\n%s", err, raw)
	}
	chat, _ := got["chat"].(map[string]any)
	if chat["verbosity"] != "verbose" {
		t.Errorf("verbosity = %v, want verbose", chat["verbosity"])
	}
	if chat["scroll_speed"] != float64(3) {
		t.Errorf("scroll_speed not preserved: %v", chat["scroll_speed"])
	}
	if got["theme"] != "monokai" {
		t.Errorf("theme not preserved: %v", got["theme"])
	}
}

// TestPersistVerbosity_CreatesFileWhenMissing verifies the helper
// bootstraps a minimal client.json5 when none exists.
func TestPersistVerbosity_CreatesFileWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.json5")

	if err := persistVerbosity(path, "quiet"); err != nil {
		t.Fatalf("persistVerbosity: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("on-disk file not valid JSON: %v\n%s", err, raw)
	}
	chat, _ := got["chat"].(map[string]any)
	if chat["verbosity"] != "quiet" {
		t.Errorf("verbosity = %v, want quiet", chat["verbosity"])
	}
}

// TestPersistVerbosity_InvalidJSON5 verifies an unparseable existing
// file surfaces an error.
func TestPersistVerbosity_InvalidJSON5(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.json5")

	if err := os.WriteFile(path, []byte("{bad json5"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := persistVerbosity(path, "normal"); err == nil {
		t.Fatal("expected error for invalid JSON5, got nil")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestPersistVerbosity -v`
Expected: FAIL with compile error (`persistVerbosity undefined`).

- [ ] **Step 3: Implement `persistVerbosity`**

Append to `internal/tui/config.go` (after `checkClientConfigDefaults`, around line 310):

```go
// persistVerbosity writes chat.verbosity=<level> into the client.json5
// at path. The file is read, standardized (JSON5 → JSON via hujson),
// unmarshaled, mutated at chat.verbosity, and written back atomically.
// If path does not exist, a minimal file is created.
//
// This helper is invoked on the TUI's Ctrl+V verbosity cycle so the
// value survives restarts, mirroring the Flutter PATCH /api/v1/config/
// client path. Failure is non-fatal — callers should log and continue.
func persistVerbosity(path, level string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read client config: %w", err)
	}
	if os.IsNotExist(err) {
		existing = []byte("{}")
	}

	stdJSON, err := hujson.Standardize(existing)
	if err != nil {
		return fmt.Errorf("parse client.json5: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(stdJSON, &root); err != nil {
		return fmt.Errorf("unmarshal client.json5: %w", err)
	}
	if root == nil {
		root = map[string]any{}
	}

	chat, _ := root["chat"].(map[string]any)
	if chat == nil {
		chat = map[string]any{}
	}
	chat["verbosity"] = level
	root["chat"] = chat

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal client config: %w", err)
	}
	out = append(out, '\n')

	tmpPath := path + ".tmp"
	//nolint:gosec // user config file; restrictive perms intended
	if err := os.WriteFile(tmpPath, out, 0o600); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
```

Also ensure the `fmt` import is present in `internal/tui/config.go` (it likely already is; verify by running `go build ./internal/tui/` in Step 4).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run TestPersistVerbosity -v`
Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/config.go internal/tui/config_persist_test.go
git commit -m "feat(tui): add persistVerbosity helper for client.json5

Reads client.json5, standardizes JSON5 → JSON, mutates chat.verbosity,
writes back atomically (temp + rename). Used by the Ctrl+V cycle to
persist verbosity across restarts. 3 tests covering existing-file
update, file-create, and invalid-JSON5 error path."
```

---

### Task 3.2: Wire Ctrl+V handler to call `persistVerbosity` (fire-and-forget)

The TUI's Ctrl+V handler at `app.go:725-730` currently only updates in-memory state. Wire it to also persist via the new helper, using the same path `LoadClientConfig` resolved. The persistence call runs in a goroutine and failures are logged.

**Files:**
- Modify: `internal/tui/app.go:725-730` (Ctrl+V handler) and `app.go:231` (capture resolved path)

- [ ] **Step 1: Capture the resolved client config path on the App struct**

The existing `LoadClientConfig` at `app.go:231` returns a config but not the path it was loaded from. We need the path to persist back to the same location.

In `internal/tui/app.go`, locate the `App` struct (around line 124) and add a field:

```go
type App struct {
	// ... existing fields ...
	clientConfig     *ClientConfig
	clientConfigPath string  // resolved path to client.json5 (home or project-local)
	// ... existing fields ...
}
```

Then locate `NewApp` (around line 231) where `clientConfig, _ := LoadClientConfig()` is called. Replace with:

```go
	clientConfig, clientConfigPath := LoadClientConfigPath()
```

(`LoadClientConfigPath` is added in Step 2 below.)

- [ ] **Step 2: Add `LoadClientConfigPath` wrapper**

In `internal/tui/config.go`, append a variant of `LoadClientConfig` that also returns the resolved path:

```go
// LoadClientConfigPath mirrors LoadClientConfig but also returns the
// resolved on-disk path of the loaded config (project-local or user-global).
// Returns ("", <defaults>) when no file is found; in that case callers
// should write to the user-global path (home/.meept/client.json5) when
// persisting.
func LoadClientConfigPath() (*ClientConfig, string) {
	localPath := ".meept/client.json5"
	if cfg, err := loadConfigFile(localPath); err == nil {
		return cfg, localPath
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		homePath := filepath.Join(homeDir, ".meept", "client.json5")
		if cfg, err := loadConfigFile(homePath); err == nil {
			return cfg, homePath
		}
		// No file found — return the home path so persists have a target.
		return DefaultClientConfig(), homePath
	}

	slog.Warn("client config: no config file found, using all defaults")
	return DefaultClientConfig(), ".meept/client.json5"
}
```

Also update `LoadClientConfig` to delegate to `LoadClientConfigPath` for DRY:

```go
func LoadClientConfig() (*ClientConfig, error) {
	cfg, _ := LoadClientConfigPath()
	return cfg, nil
}
```

(The signature is preserved for backward compat with existing call sites.)

- [ ] **Step 3: Update `NewApp` to store the path**

In `NewApp` (around line 261), the `clientConfig: clientConfig` line becomes:

```go
		clientConfig:     clientConfig,
		clientConfigPath: clientConfigPath,
```

- [ ] **Step 4: Update the Ctrl+V handler to persist**

Locate the Ctrl+V handler at `app.go:725-730`. Replace it with:

```go
		// Check for Ctrl+V: cycle verbosity level
		if msg.String() == "ctrl+v" {
			a.verbosity = (a.verbosity + 1) % 3
			a.statusMessage = fmt.Sprintf("verbosity: %s", a.verbosity)
			a.statusMessageTime = time.Now()
			// Update in-memory config and persist fire-and-forget so the
			// value survives restarts. Failures are logged but never revert
			// the UI state (per plan: best-effort persistence).
			if a.clientConfig != nil {
				a.clientConfig.Chat.Verbosity = a.verbosity.String()
			}
			level := a.verbosity.String()
			path := a.clientConfigPath
			go func() {
				if err := persistVerbosity(path, level); err != nil {
					slog.Warn("client config: failed to persist verbosity",
						"path", path, "error", err)
				}
			}()
			return a, nil
		}
```

- [ ] **Step 5: Build and run TUI tests**

Run: `go build ./internal/tui/`
Expected: builds clean.

Run: `go test ./internal/tui/ -run TestPersistVerbosity -v`
Expected: 3 PASS (from Task 3.1).

Run: `go test ./internal/tui/`
Expected: all tests PASS (no new failures from the `LoadClientConfigPath` refactor — `LoadClientConfig` retains its signature).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/config.go
git commit -m "feat(tui): persist verbosity on Ctrl+V cycle

Ctrl+V now updates in-memory state AND fire-and-forgets a disk write
to client.json5 so the verbosity level survives restarts. Adds
LoadClientConfigPath to capture which file the config loaded from
(project-local or user-global). Update in-memory clientConfig too so
subsequent saves are consistent. Persistence failures are logged."
```

---

## Phase 4: Docs + cleanup

### Task 4.1: Update `docs/reference/http-api.md`

Add the PATCH endpoint to the Configuration table and a usage example.

**Files:**
- Modify: `docs/reference/http-api.md:480-492` (Configuration table) and a new example block

- [ ] **Step 1: Add the PATCH row to the table**

In `docs/reference/http-api.md`, locate the Configuration table (around line 480-492). Insert the PATCH row between the GET and POST rows:

```markdown
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/config/client` | Get client config |
| PATCH | `/api/v1/config/client` | Merge-patch client config (RFC 7396) |
| POST | `/api/v1/config/client` | Save client config (full replace) |
| GET | `/api/v1/config/models` | Get models config |
| POST | `/api/v1/config/models` | Save models config |
| GET | `/api/v1/config/menubar` | Get menubar config |
| POST | `/api/v1/config/menubar` | Save menubar config |
| POST | `/api/v1/config/normalize` | Normalize JSON5 config content |
| GET | `/api/v1/config/agents` | List agents |
| GET | `/api/v1/config/agents/{id}` | Get agent config |
| POST | `/api/v1/config/agents/{id}` | Save agent |
| DELETE | `/api/v1/config/agents/{id}` | Delete agent |
```

- [ ] **Step 2: Add an example block**

Append to the Configuration examples section (after the existing normalize example, around line 540):

```markdown
**Patch Client Config (RFC 7396 merge-patch):**
```bash
curl -X PATCH http://localhost:8081/api/v1/config/client \
  -H "Content-Type: application/json" \
  -d '{"chat":{"verbosity":"verbose"}}'
```
Returns the merged config as JSON. `null` deletes a key, objects recurse, scalars/arrays replace.
```

- [ ] **Step 3: Commit**

```bash
git add docs/reference/http-api.md
git commit -m "docs: add PATCH /api/v1/config/client to http-api reference"
```

---

### Task 4.2: Update `docs/workflows/flutter_gui.md`

Note that verbosity now persists across restarts via the merge-patch endpoint.

**Files:**
- Modify: `docs/workflows/flutter_gui.md`

- [ ] **Step 1: Locate the verbosity section**

Run: `grep -n "verbosity\|Ctrl.?V\|PATCH" docs/workflows/flutter_gui.md`

If a verbosity section exists, append the persistence note. If not, add a brief paragraph in the keyboard shortcuts or status bar section.

- [ ] **Step 2: Append the note**

Add the following paragraph (place near the existing verbosity mention, or in a "State persistence" subsection at the end of the relevant section):

```markdown
Verbosity level (Ctrl+V cycle) persists across app restarts. Each
cycle fire-and-forgets a `PATCH /api/v1/config/client` merge-patch
with `{"chat":{"verbosity":"<level>"}}` so the value lands in
`~/.meept/client.json5`. The UI state updates immediately; persistence
is best-effort — if the daemon is unreachable, the cycle still applies
for the current session.
```

- [ ] **Step 3: Commit**

```bash
git add docs/workflows/flutter_gui.md
git commit -m "docs(flutter): note verbosity persistence across restarts"
```

---

### Task 4.3: Final verification commit (if needed)

This step captures any final adjustments. If all prior commits landed cleanly, this is a no-op.

- [ ] **Step 1: Build everything**

Run: `go build ./...`
Expected: builds clean.

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no new warnings.

- [ ] **Step 2: Run targeted tests**

Run: `go test ./internal/config/ ./internal/comm/http/ ./internal/tui/ -v`
Expected: all PASS.

Run: `cd ui/flutter_ui && flutter test test/providers/verbosity_provider_test.dart`
Expected: PASS.

- [ ] **Step 3: Final commit (only if there are uncommitted changes)**

```bash
git status
# If clean: done.
# If not clean:
git add -A
git commit -m "chore: final cleanup for client config merge-patch"
```

---

## Self-review

**Spec coverage:**

1. **PATCH route reads on-disk fresh** — Task 1.2 `PatchClientConfig` does `os.ReadFile(path)` (no caching). ✅
2. **Standardize JSON5 → JSON via hujson** — Task 1.2 calls `hujson.Standardize`. ✅
3. **Unmarshal into `map[string]any`** — Task 1.2 does this. ✅
4. **Deep-merge with RFC 7396 semantics** — Task 1.2 calls exported `DeepMerge` (Task 1.1). Null-deletes-key covered by `TestPatchClientConfig_NullDeletesKey`. ✅
5. **Atomic write (temp + rename)** — Task 1.2 uses the temp+rename pattern from `SaveOrchestratorConfig`. ✅
6. **Returns merged config as JSON** — Task 1.2 returns `merged`, Task 1.3 handler responds with `s.writeJSON(w, 200, merged)`. ✅
7. **Flutter `setClientConfig` uses PATCH instead of POST** — Task 2.1 switches to `_patch`. ✅
8. **Flutter verbosity cycle persists** — Task 2.2 wires `VerbosityNotifier.cycle` to fire-and-forget PATCH. ✅
9. **TUI verbosity persists** — Task 3.1 + 3.2 add `persistVerbosity` and call it from the Ctrl+V handler. ✅
10. **Tests: HTTP handler, deepMerge export, Flutter integration** — Task 1.1 covers merge tests, Task 1.2 covers `PatchClientConfig` (4 tests), Task 1.3 covers handler (4 tests), Task 2.2 covers Flutter persistence. ✅
11. **Docs: http-api.md + flutter_gui.md** — Task 4.1 + 4.2. ✅
12. **RFC 7396 null-deletes verified** — `TestPatchClientConfig_NullDeletesKey` + existing `TestDeepMerge_NullDeletesKey`. ✅
13. **Fire-and-forget on both surfaces** — Task 2.2 uses `Future<void>.microtask`, Task 3.2 uses `go func()`. ✅
14. **Failure modes don't revert UI state** — Task 2.2 try/catch debug-logs; Task 3.2 logs via `slog.Warn`. ✅

**Placeholder scan:** No "TBD", "implement here", "add error handling", "similar to Task N" placeholders. Step 5 of Task 2.2 has conditional branches ("If you cannot find...") with concrete `grep` commands — this is acceptable because it points to a deterministic search, not a hand-wave.

**Type consistency:**
- `DeepMerge(dst, src map[string]any) map[string]any` — used in Task 1.1, 1.2.
- `ConfigService.PatchClientConfig(patch map[string]any) (map[string]any, error)` — defined Task 1.2, used Task 1.3.
- `handleClientConfigPatch(w http.ResponseWriter, r *http.Request)` — defined Task 1.3, registered in same task.
- `persistVerbosity(path, level string) error` — defined Task 3.1, used Task 3.2.
- `LoadClientConfigPath() (*ClientConfig, string)` — defined Task 3.2 Step 2, used Task 3.2 Step 1.
- Flutter `setClientConfig(Map<String, dynamic> patch)` — defined Task 2.1, invoked Task 2.2.
- `sdkClientProvider` — defined Task 2.2, overridden in test bootstrap (Step 1) and real app bootstrap (Step 5).
- `clientConfigPath` (App field) — declared Task 3.2 Step 1, assigned Step 3, read Step 4.

All signatures align across tasks.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-06-29-client-config-patch.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints for review.

**Which approach?**
