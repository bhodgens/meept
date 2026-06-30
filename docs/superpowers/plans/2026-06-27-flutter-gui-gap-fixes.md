# Flutter GUI Gap Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix seven reported gaps in the Flutter GUI (status bar, ctrl-x palette, agent tile sizing, double-click tab activation, click latency, grey-transcript bug, archive-vs-delete) with full cross-surface parity between TUI and Flutter.

**Architecture:** Backend-first additions (archive column, PATCH endpoint, RPC method) land first; then Flutter consumption layers (status bar, palette, providers); then investigation-then-fix for the grey-transcript bug. Each task is independently committable and testable.

**Tech Stack:** Go (daemon, RPC, HTTP, TUI), SQLite (session storage), Flutter/Dart (GUI), Riverpod (state), freezed (models).

**Spec:** `docs/superpowers/specs/2026-06-27-flutter-gui-gap-fixes-design.md`

**Parity rule (CLAUDE.md):** TUI and Flutter GUI features must be kept at parity. Every GUI behavior change has a TUI equivalent and vice versa.

**Implementation note:** Wherever a task references an existing helper (e.g., `newTestStore`, `newTestServer`, `sdkClientProvider`) by name, the engineer must verify the helper exists in that file and reuse it. Do not invent helpers. If a referenced helper doesn't exist, find the closest equivalent used by surrounding tests/code and use that.

---

## File Structure

### Go (backend)
- `internal/session/session.go` — add `Archived` field (modify)
- `internal/session/store_sqlite.go` — add migration + `Archive` method (modify)
- `internal/session/manager.go` — add `ArchiveSession` (modify)
- `internal/session/store_sqlite_test.go` — test Archive + sort order (modify)
- `internal/comm/http/server.go` — register PATCH route (modify)
- `internal/comm/http/api_handlers.go` — add `handleSessionArchive` (modify)
- `internal/comm/http/server_test.go` — test PATCH endpoint (modify)
- `internal/rpc/` — session RPC handler (modify; verify exact filename in Task 1.5 Step 1)

### Flutter (GUI)
- `ui/flutter_ui/lib/models/api_models.dart` — add `archived` field to `Session` (modify); regenerate `.freezed.dart` + `.g.dart`
- `ui/flutter_ui/lib/services/sdk_client.dart` — add `_patch`, `archiveSession`, `getProjectStatus`, `setClientConfig` (modify)
- `ui/flutter_ui/lib/services/session_notifier.dart` — add `archiveSession`/`unarchiveSession` (modify)
- `ui/flutter_ui/lib/providers/verbosity_provider.dart` — new
- `ui/flutter_ui/lib/providers/cached_detail.dart` — new
- `ui/flutter_ui/lib/providers/project_provider.dart` — new
- `ui/flutter_ui/lib/providers/status_message_provider.dart` — new
- `ui/flutter_ui/lib/providers/tab_activation_provider.dart` — new
- `ui/flutter_ui/lib/widgets/status_bar.dart` — new
- `ui/flutter_ui/lib/widgets/command_palette.dart` — new
- `ui/flutter_ui/lib/core/shortcuts.dart` — remove leader-key, add Ctrl+V (modify)
- `ui/flutter_ui/lib/features/home/home_screen.dart` — wire palette, status bar, tab activation (modify)
- `ui/flutter_ui/lib/features/agents/agents_tab.dart` — tile sizing (modify)
- `ui/flutter_ui/lib/features/sessions/sessions_list.dart` — archive icon, double-click fix, greyed rendering (modify)
- `ui/flutter_ui/lib/features/chat/chat_message_list.dart` — placeholder-while-loading refinement (modify, per Gap 6 root-cause)
- `ui/flutter_ui/lib/providers/chat_provider.dart` — session-swap state handling (modify, per Gap 6 root-cause)
- `ui/flutter_ui/test/widgets/status_bar_test.dart` — new
- `ui/flutter_ui/test/widgets/command_palette_test.dart` — new
- `ui/flutter_ui/test/providers/verbosity_provider_test.dart` — new
- `ui/flutter_ui/test/providers/cached_detail_test.dart` — new
- `ui/flutter_ui/test/widgets/agents_tab_test.dart` — new
- `ui/flutter_ui/test/widgets/sessions_list_test.dart` — new
- `ui/flutter_ui/test/widgets/session_archive_test.dart` — new

### TUI
- `internal/tui/types/types.go` — add `Archived` to `Session` (modify)
- `internal/tui/models/sessions.go` — dim archived sessions (modify)
- `internal/tui/app.go` — wire `d`/`D` keys, fix dead status message (modify)

---

## Phase 1: Backend (Archive)

Foundation for Gap 7. No UI changes; all testable in isolation.

### Task 1.1: Add `Archived` field to Go `Session` struct

**Files:**
- Modify: `internal/session/session.go:58-82`

- [ ] **Step 1: Add the field**

In `internal/session/session.go`, locate the `Session` struct (around line 58). Add a new field after `NoFence`:

```go
// Archived indicates the session has been soft-archived. Archived sessions
// are excluded from the default visible set and sort to the bottom of
// listings; their data is preserved.
Archived bool `json:"archived,omitempty"`
```

- [ ] **Step 2: Verify build**

Run: `go build ./internal/session/...`
Expected: exits 0, no output.

- [ ] **Step 3: Commit**

```bash
git add internal/session/session.go
git commit -m "feat(session): add Archived field to Session struct"
```

---

### Task 1.2: Add SQLite migration + `Archive` method

**Files:**
- Modify: `internal/session/store_sqlite.go` (migration list ~lines 114-141, `List` query ~lines 531-554, `Get` query, new `Archive` method)
- Modify: store interface declaration (top of `store_sqlite.go` or sibling `store.go`)
- Test: `internal/session/store_sqlite_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/session/store_sqlite_test.go`. First check what test-helper is used by existing tests in this file (e.g., `newTestStore`, `setupTestStore`, `mustOpenStore`) — use that helper. Do not invent one.

```go
func TestArchiveSession(t *testing.T) {
	store := newTestStore(t) // REPLACE with the helper name used in this file
	defer store.Close()

	s := &session.Session{
		ID:             "test-archive-1",
		Name:           "test archive",
		ConversationID: "conv-archive-1",
		CreatedAt:      time.Now().UTC(),
		LastActivity:   time.Now().UTC(),
	}
	if err := store.Create(s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Archive("test-archive-1", true); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	got, err := store.Get("test-archive-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Archived {
		t.Fatalf("expected Archived=true, got false")
	}

	if err := store.Archive("test-archive-1", false); err != nil {
		t.Fatalf("Archive(false): %v", err)
	}
	got, err = store.Get("test-archive-1")
	if err != nil {
		t.Fatalf("Get after unarchive: %v", err)
	}
	if got.Archived {
		t.Fatalf("expected Archived=false after unarchive, got true")
	}
}

func TestListSortsArchivedToBottom(t *testing.T) {
	store := newTestStore(t) // REPLACE
	defer store.Close()

	now := time.Now().UTC()
	older := now.Add(-2 * time.Hour)
	newer := now.Add(-1 * time.Hour)

	// old-active has older LastActivity than new-archived, but new-archived
	// must still sort BELOW old-active because archived sorts to bottom
	// regardless of activity.
	mustCreate := func(id, name string, last time.Time, archived bool) {
		s := &session.Session{
			ID:             id,
			Name:           name,
			ConversationID: "conv-" + id,
			CreatedAt:      now,
			LastActivity:   last,
		}
		if err := store.Create(s); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
		if archived {
			if err := store.Archive(id, true); err != nil {
				t.Fatalf("Archive %s: %v", id, err)
			}
		}
	}

	mustCreate("old-active", "old active", older, false)
	mustCreate("new-archived", "new archived", newer, true)

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var oldActiveIdx, newArchivedIdx int = -1, -1
	for i, s := range sessions {
		switch s.ID {
		case "old-active":
			oldActiveIdx = i
		case "new-archived":
			newArchivedIdx = i
		}
	}
	if oldActiveIdx < 0 || newArchivedIdx < 0 {
		t.Fatalf("expected both sessions in List result, got %d sessions", len(sessions))
	}
	if oldActiveIdx > newArchivedIdx {
		t.Fatalf("expected old-active (idx %d) BEFORE new-archived (idx %d); archived must sort to bottom",
			oldActiveIdx, newArchivedIdx)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/session/ -run 'TestArchiveSession|TestListSortsArchivedToBottom' -v`
Expected: FAIL — `store.Archive undefined` and/or `unknown field Archived`.

- [ ] **Step 3: Add the migration**

In `internal/session/store_sqlite.go`, find the migration list (the section that runs `ALTER TABLE sessions ADD COLUMN …` statements — around lines 114-141). Add a new migration entry following the exact pattern used by surrounding migrations (including their idempotency check, e.g., checking `sqlite_master`/`PRAGMA table_info` before adding the column):

```go
// Add archived column (soft-archive flag; default 0 = not archived).
{
	Name: "add archived column",
	SQL:  `ALTER TABLE sessions ADD COLUMN archived BOOLEAN DEFAULT 0`,
}
```

- [ ] **Step 4: Update `List` and `Get` queries**

Find `List()` (around line 531) and `Get()`. For both:
- Add `archived` to the SELECT column list.
- Add `&s.Archived` to the `rows.Scan(...)` argument list, matching the SELECT order.
- In `List()`, change `ORDER BY last_activity DESC` to `ORDER BY archived ASC, last_activity DESC`.

Preserve all existing WHERE filters verbatim.

- [ ] **Step 5: Implement `Archive` method**

Add to the SQLite store (adjust receiver type to match other methods in the file):

```go
// Archive sets the archived flag on a session. Pass archived=true to archive,
// false to unarchive. Returns an error if the session doesn't exist or the
// update fails.
func (s *SQLiteStore) Archive(id string, archived bool) error {
	result, err := s.db.Exec("UPDATE sessions SET archived = ? WHERE id = ?", archived, id)
	if err != nil {
		return fmt.Errorf("update archived flag: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("session %q not found", id)
	}
	return nil
}
```

Verify `fmt` is already imported; if not, add it.

- [ ] **Step 6: Add `Archive` to the store interface**

Find the store interface declaration (likely at the top of `store_sqlite.go` or in `internal/session/store.go`). Add:

```go
Archive(id string, archived bool) error
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/session/ -run 'TestArchiveSession|TestListSortsArchivedToBottom' -v`
Expected: PASS for both.

- [ ] **Step 8: Run full store test suite**

Run: `go test ./internal/session/ -v`
Expected: all PASS, no regressions.

- [ ] **Step 9: Commit**

```bash
git add internal/session/store_sqlite.go internal/session/store_sqlite_test.go
# Also stage the interface file if you changed one:
# git add internal/session/store.go
git commit -m "feat(session): add archived column, Archive method, sort-to-bottom"
```

---

### Task 1.3: Add `SessionService.ArchiveSession`

**Correction note:** The original plan referenced a `Manager` type in `internal/session/manager.go`. That type does not exist in this codebase. The intermediate between HTTP/RPC and `session.Store` is `services.SessionService` in `internal/services/session_service.go`. This task is rewritten to add `ArchiveSession` to that service, mirroring `SessionService.DeleteSession` (lines 87-99 of `session_service.go`).

**Files:**
- Modify: `internal/services/session_service.go`
- Modify: `internal/services/session_service_test.go` (or create if missing — check first)

- [ ] **Step 1: Locate the existing session-service tests**

Run: `ls internal/services/session_service_test.go 2>&1; grep -n "func Test.*Delete\|func newTestSessionService\|func setupTestSession" internal/services/session_service_test.go 2>&1`

If a test file exists, mirror its setup pattern. If not, look at `internal/services/session_service_test.go` neighbors (e.g., `push_service_test.go` has `fakeStore`) or `internal/session/store_sqlite_test.go` for store construction patterns. The pattern in this codebase is to use the real SQLite store with `:memory:` for service tests, OR to use a `fakeStore` that implements `session.Store`. Prefer whichever existing tests already do.

- [ ] **Step 2: Write the failing test**

```go
func TestSessionServiceArchiveSession(t *testing.T) {
	store := session.NewMemoryStore(slog.Default())
	svc := NewSessionService(store)

	sess, err := svc.CreateSession(context.Background(), CreateSessionRequest{Name: "to-archive"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := svc.ArchiveSession(context.Background(), ArchiveSessionRequest{ID: sess.ID, Archived: true}); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}

	got, err := svc.GetSession(context.Background(), GetSessionRequest{ID: sess.ID})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !got.Archived {
		t.Fatalf("expected Archived=true, got false")
	}

	// Unarchive round-trip
	if err := svc.ArchiveSession(context.Background(), ArchiveSessionRequest{ID: sess.ID, Archived: false}); err != nil {
		t.Fatalf("ArchiveSession unarchive: %v", err)
	}
	got, _ = svc.GetSession(context.Background(), GetSessionRequest{ID: sess.ID})
	if got.Archived {
		t.Fatalf("expected Archived=false after unarchive, got true")
	}
}

func TestSessionServiceArchiveSession_NotFound(t *testing.T) {
	store := session.NewMemoryStore(slog.Default())
	svc := NewSessionService(store)

	err := svc.ArchiveSession(context.Background(), ArchiveSessionRequest{ID: "nonexistent", Archived: true})
	if err == nil {
		t.Fatalf("expected error for nonexistent session, got nil")
	}
}

func TestSessionServiceArchiveSession_InvalidInput(t *testing.T) {
	store := session.NewMemoryStore(slog.Default())
	svc := NewSessionService(store)

	err := svc.ArchiveSession(context.Background(), ArchiveSessionRequest{ID: "", Archived: true})
	if err == nil {
		t.Fatalf("expected error for empty ID, got nil")
	}
}
```

Adjust to match the real test-file setup conventions (e.g., if existing tests use a fixture helper).

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/services/ -run TestSessionServiceArchiveSession -v`
Expected: FAIL — `svc.ArchiveSession undefined`.

- [ ] **Step 4: Implement `SessionService.ArchiveSession`**

Mirror `DeleteSession` at `internal/services/session_service.go:87-99`:

```go
// ArchiveSessionRequest contains archive parameters.
type ArchiveSessionRequest struct {
	ID       string `json:"id"`
	Archived bool   `json:"archived"`
}

// ArchiveSession sets or clears the archived flag on a session. Archived
// sessions are preserved but sorted to the bottom of the list.
func (s *SessionService) ArchiveSession(ctx context.Context, req ArchiveSessionRequest) error {
	if req.ID == "" {
		return wrapError("session", "ArchiveSession", ErrInvalidInput)
	}
	if s.store == nil {
		return wrapError("session", "ArchiveSession", ErrUnavailable)
	}
	if err := s.store.Archive(req.ID, req.Archived); err != nil {
		// store.Archive returns an error whose message contains "not found"
		// when the session ID does not exist; map to ErrNotFound for
		// consistent HTTP 404 mapping via handleServiceError.
		if strings.Contains(err.Error(), "not found") {
			return wrapError("session", "ArchiveSession", ErrNotFound)
		}
		return wrapError("session", "ArchiveSession", err)
	}
	return nil
}
```

Add `"strings"` to the import block if not already present. Match `wrapError`/`ErrInvalidInput`/`ErrUnavailable`/`ErrNotFound` usage exactly as `DeleteSession` does.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/services/ -run TestSessionServiceArchiveSession -v`
Expected: PASS.

- [ ] **Step 6: Run full services test suite**

Run: `go test ./internal/services/ -v -race`
Expected: all PASS, no race warnings.

- [ ] **Step 7: Commit**

```bash
git add internal/services/session_service.go internal/services/session_service_test.go
git commit -m "feat(services): add SessionService.ArchiveSession"
```

---

### Task 1.4: Add `PATCH /api/v1/sessions/{id}` HTTP endpoint

**Files:**
- Modify: `internal/comm/http/server.go` (route registration ~line 1043-1047)
- Modify: `internal/comm/http/api_handlers.go` (new handler, near `handleSessionDelete`)
- Test: `internal/comm/http/server_test.go`

- [ ] **Step 1: Locate the existing test helper**

Run: `grep -n "func newTestServer\|func setupTestServer" internal/comm/http/server_test.go`

Use whatever helper the existing tests use.

- [ ] **Step 2: Write the failing tests**

Append to `internal/comm/http/server_test.go`:

```go
func TestHandleSessionArchive(t *testing.T) {
	srv, cleanup := newTestServer(t) // REPLACE
	defer cleanup()

	// Create a session via the existing POST /api/v1/sessions endpoint.
	body := strings.NewReader(`{"title":"archive-test"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", body)
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	srv.handleSessionCreate(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", createRR.Code, createRR.Body.String())
	}
	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createRR.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create resp: %v", err)
	}
	if createResp.ID == "" {
		t.Fatal("no id in create response")
	}

	// Archive it.
	archReq := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/sessions/"+createResp.ID,
		strings.NewReader(`{"archived":true}`),
	)
	archReq.Header.Set("Content-Type", "application/json")
	archReq.SetPathValue("id", createResp.ID)
	archRR := httptest.NewRecorder()
	srv.handleSessionArchive(archRR, archReq)
	if archRR.Code != http.StatusNoContent {
		t.Fatalf("archive: expected 204, got %d: %s", archRR.Code, archRR.Body.String())
	}

	// Verify via GET.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+createResp.ID, nil)
	getReq.SetPathValue("id", createResp.ID)
	getRR := httptest.NewRecorder()
	srv.handleSessionGet(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", getRR.Code)
	}
	var getResp map[string]any
	if err := json.Unmarshal(getRR.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("unmarshal get resp: %v", err)
	}
	if got, _ := getResp["archived"].(bool); !got {
		t.Fatalf("expected archived=true in GET response, got %v", getResp["archived"])
	}
}

func TestHandleSessionArchiveRejectsUnknownFields(t *testing.T) {
	srv, cleanup := newTestServer(t) // REPLACE
	defer cleanup()

	body := strings.NewReader(`{"title":"reject-test"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", body)
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	srv.handleSessionCreate(createRR, createReq)
	var createResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(createRR.Body.Bytes(), &createResp)

	badReq := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/sessions/"+createResp.ID,
		strings.NewReader(`{"title":"evil"}`),
	)
	badReq.Header.Set("Content-Type", "application/json")
	badReq.SetPathValue("id", createResp.ID)
	badRR := httptest.NewRecorder()
	srv.handleSessionArchive(badRR, badReq)
	if badRR.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d: %s", badRR.Code, badRR.Body.String())
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/comm/http/ -run 'TestHandleSessionArchive' -v`
Expected: FAIL — `srv.handleSessionArchive undefined`.

- [ ] **Step 4: Register the PATCH route**

In `internal/comm/http/server.go`, in the session routes block (around line 1043-1047), add:

```go
mux.HandleFunc("PATCH /api/v1/sessions/{id}", s.handleSessionArchive)
```

- [ ] **Step 5: Add the handler**

In `internal/comm/http/api_handlers.go`, near `handleSessionDelete`:

```go
// handleSessionArchive handles PATCH /api/v1/sessions/{id}.
//
// Body: {"archived": true|false}
// Response: 204 No Content on success, 400 on malformed body or unknown
// fields, 404 if the session doesn't exist.
func (s *Server) handleSessionArchive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}
	if s.services == nil || s.services.Session == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	// Strict decoding: reject any field that isn't "archived".
	var body struct {
		Archived *bool `json:"archived"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.Archived == nil {
		s.writeError(w, http.StatusBadRequest, `"archived" field is required`)
		return
	}

	if err := s.services.Session.ArchiveSession(r.Context(), services.ArchiveSessionRequest{ID: id, Archived: *body.Archived}); err != nil {
		// ArchiveSession maps store "not found" to services.ErrNotFound already;
		// handleServiceError translates ErrNotFound → 404. Fall back to a
		// substring check only for unexpected error shapes.
		s.handleServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
```

`*bool` is used (not `bool`) so the field's absence is distinguishable from `false`.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/comm/http/ -run 'TestHandleSessionArchive' -v`
Expected: PASS for both.

- [ ] **Step 7: Run the broader HTTP test suite**

Run: `go test ./internal/comm/http/ -v`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/comm/http/server.go internal/comm/http/api_handlers.go internal/comm/http/server_test.go
git commit -m "feat(http): add PATCH /api/v1/sessions/{id} for archive"
```

---

### Task 1.5: Add `sessions.archive` RPC method (for TUI)

**Correction note:** The original plan assumed session RPC handlers live in `internal/rpc/` and consume a `Manager`. Reality: session RPC handlers live in `internal/daemon/session_rpc.go` and consume `*services.SessionService` via closure-based handlers (`handleSessionsDesignated`, `handleSessionDesignatedAcknowledge`). Mirror those exactly.

**Files:**
- Modify: `internal/daemon/session_rpc.go`
- Create or extend: `internal/daemon/session_rpc_test.go` (this file does not exist yet — create it)

- [ ] **Step 1: Read the existing pattern**

Read `internal/daemon/session_rpc.go` in full. Note how `handleSessionDesignatedAcknowledge` works:
- Closure: `func(svc *services.SessionService) rpc.Handler { return func(ctx, params json.RawMessage) (any, error) { ... } }`
- Unmarshal params from `json.RawMessage` into an anonymous struct
- Validate required fields, return `fmt.Errorf("... is required")` for missing
- Call the service method
- Return a `map[string]any` response

- [ ] **Step 2: Write the failing test**

Create `internal/daemon/session_rpc_test.go`. Mirror the closure-based registration pattern. Use `rpc.New` to construct a server, `services.NewSessionService(session.NewMemoryStore(...))` to construct the service:

```go
package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/services"
	"github.com/caimlas/meept/internal/session"
)

func newArchiveRPCTestServer(t *testing.T) (*rpc.Server, *services.SessionService) {
	t.Helper()
	store := session.NewMemoryStore(slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc := services.NewSessionService(store)
	srv := rpc.New(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	registerSessionRPCHandlers(srv, svc)
	return srv, svc
}

func TestRPC_SessionsArchive(t *testing.T) {
	srv, svc := newArchiveRPCTestServer(t)

	created, err := svc.CreateSession(context.Background(), services.CreateSessionRequest{Name: "rpc-archive-test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	params, _ := json.Marshal(map[string]any{"id": created.ID, "archived": true})
	resp, err := srv.CallMethod(context.Background(), "sessions.archive", params)
	if err != nil {
		t.Fatalf("sessions.archive: %v", err)
	}
	respMap, ok := resp.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", resp)
	}
	if got, _ := respMap["status"].(string); got != "archived" {
		t.Fatalf("expected status=archived, got %v", respMap["status"])
	}

	// Verify via the service that the flag persisted.
	got, err := svc.GetSession(context.Background(), services.GetSessionRequest{ID: created.ID})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !got.Archived {
		t.Fatalf("expected Archived=true, got false")
	}
}

func TestRPC_SessionsArchive_MissingID(t *testing.T) {
	srv, _ := newArchiveRPCTestServer(t)

	params, _ := json.Marshal(map[string]any{"archived": true})
	_, err := srv.CallMethod(context.Background(), "sessions.archive", params)
	if err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

func TestRPC_SessionsArchive_NotFound(t *testing.T) {
	srv, _ := newArchiveRPCTestServer(t)

	params, _ := json.Marshal(map[string]any{"id": "nonexistent", "archived": true})
	_, err := srv.CallMethod(context.Background(), "sessions.archive", params)
	if err == nil {
		t.Fatal("expected error for nonexistent session, got nil")
	}
}
```

Adapt the `rpc.New` signature to its real constructor args (verify in `internal/rpc/server.go:81`).

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestRPC_SessionsArchive -v`
Expected: FAIL — no `sessions.archive` handler registered (likely `method not found` error).

- [ ] **Step 4: Add the handler**

Append to `internal/daemon/session_rpc.go`:

```go
// handleSessionArchive sets or clears the archived flag on a session.
// Mirrors handleSessionDesignatedAcknowledge's closure pattern.
func handleSessionArchive(svc *services.SessionService) rpc.Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var req struct {
			ID       string `json:"id"`
			Archived bool   `json:"archived"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if req.ID == "" {
			return nil, fmt.Errorf("id is required")
		}

		if err := svc.ArchiveSession(ctx, services.ArchiveSessionRequest{ID: req.ID, Archived: req.Archived}); err != nil {
			return nil, fmt.Errorf("failed to archive session: %w", err)
		}

		status := "archived"
		if !req.Archived {
			status = "unarchived"
		}
		return map[string]any{
			"status": status,
			"id":     req.ID,
		}, nil
	}
}
```

Then register it inside `registerSessionRPCHandlers`:

```go
server.RegisterHandler("sessions.archive", handleSessionArchive(sessionSvc))
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/daemon/ -run TestRPC_SessionsArchive -v`
Expected: PASS for all 3 tests.

- [ ] **Step 6: Run full daemon test suite (regression)**

Run: `go test ./internal/daemon/ -v -race`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/daemon/session_rpc.go internal/daemon/session_rpc_test.go
git commit -m "feat(rpc): add sessions.archive method"
```

---

## Phase 2: Flutter Foundation (Providers, Models, Client)

### Task 2.1: Add `archived` field to Flutter `Session` model

**Files:**
- Modify: `ui/flutter_ui/lib/models/api_models.dart` (around line 448-486)
- Regenerate: `api_models.freezed.dart`, `api_models.g.dart`

- [ ] **Step 1: Add the field**

In `ui/flutter_ui/lib/models/api_models.dart`, locate the `Session` freezed class. Add a new field:

```dart
@Default(false)
@JsonKey(name: 'archived')
bool archived,
```

Place it after `designation` (or wherever the surrounding fields are ordered).

- [ ] **Step 2: Regenerate freezed/json_serializable**

Run from the `ui/flutter_ui/` directory:

```bash
cd ui/flutter_ui && dart run build_runner build --delete-conflicting-outputs
```

Expected: completes without errors, regenerates `api_models.freezed.dart` and `api_models.g.dart`.

If `build_runner` isn't a dev dependency yet, add it to `pubspec.yaml`. If this fails for unrelated reasons, regenerate by hand-editing the `.freezed.dart` and `.g.dart` files (the pattern is mechanical — copy an existing bool field).

- [ ] **Step 3: Verify it compiles**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no new errors.

- [ ] **Step 4: Commit**

```bash
git add ui/flutter_ui/lib/models/api_models.dart ui/flutter_ui/lib/models/api_models.freezed.dart ui/flutter_ui/lib/models/api_models.g.dart
git commit -m "feat(models): add archived field to Session"
```

---

### Task 2.2: Add `_patch`, `archiveSession`, `getProjectStatus`, `setClientConfig` to `SdkClient`

**Files:**
- Modify: `ui/flutter_ui/lib/services/sdk_client.dart` (~line 200 for helpers, ~466 for session methods, ~916 for projects)

- [ ] **Step 1: Add `_patch` helper**

Find the existing `_get`/`_post`/`_put`/`_delete` helpers (around line 200-241). Add a `_patch` mirroring `_put`:

```dart
Future<Map<String, dynamic>> _patch(String path, {Map<String, dynamic>? body}) async {
  final response = await _send('PATCH', path, body);
  if (response.statusCode >= 400) {
    throw Exception('PATCH $path failed: ${response.statusCode} ${response.body}');
  }
  if (response.body.isEmpty) return {};
  return jsonDecode(response.body) as Map<String, dynamic>;
}
```

Adapt to the actual `_send` helper signature in this file. Look at how `_put` is defined and copy the pattern.

- [ ] **Step 2: Add `archiveSession`**

Near `deleteSession` (~line 466):

```dart
/// PATCH /api/v1/sessions/{id} — set the archived flag.
Future<void> archiveSession(String sessionId, {required bool archived}) async {
  await _patch('/api/v1/sessions/$sessionId', body: {'archived': archived});
}
```

- [ ] **Step 3: Add `getProjectStatus`**

Near `listProjects` (~line 916):

```dart
/// GET /api/v1/projects/{id}/status — current branch + dirty state.
Future<Map<String, dynamic>> getProjectStatus(String projectId) async {
  return _get('/api/v1/projects/$projectId/status');
}
```

- [ ] **Step 4: Add `setClientConfig`**

Near any existing config-related methods:

```dart
/// POST /api/v1/config/client — merge-patch a single field into client config.
/// Example: setClientConfig({'chat': {'verbosity': 'verbose'}})
Future<void> setClientConfig(Map<String, dynamic> patch) async {
  await _post('/api/v1/config/client', body: patch);
}
```

If `_post` doesn't accept the body shape directly, match how other `_post` calls in this file pass JSON bodies (some wrap in `_toJson(...)`).

- [ ] **Step 5: Verify it compiles**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no new errors.

- [ ] **Step 6: Commit**

```bash
git add ui/flutter_ui/lib/services/sdk_client.dart
git commit -m "feat(sdk): add _patch, archiveSession, getProjectStatus, setClientConfig"
```

---

### Task 2.3: Add `archiveSession`/`unarchiveSession` to `SessionNotifier`

**Files:**
- Modify: `ui/flutter_ui/lib/services/session_notifier.dart`

- [ ] **Step 1: Add the methods**

Find `deleteSession` (around line 80). Mirror its structure for archive/unarchive:

```dart
/// Archive a session. Mutates local state to flip the flag and re-sorts
/// the list so archived sessions move to the bottom.
Future<void> archiveSession(String sessionId) async {
  try {
    await sdkClient.archiveSession(sessionId, archived: true);
    state = _withArchiveFlag(state, sessionId, archived: true);
  } catch (e) {
    state = state.copyWith(error: e.toString());
  }
}

/// Unarchive a session. Mutates local state to flip the flag and re-sorts.
Future<void> unarchiveSession(String sessionId) async {
  try {
    await sdkClient.archiveSession(sessionId, archived: false);
    state = _withArchiveFlag(state, sessionId, archived: false);
  } catch (e) {
    state = state.copyWith(error: e.toString());
  }
}

/// Returns a new SessionState with the given session's archived flag
/// flipped, and the list re-sorted (non-archived first, then by
/// lastActivity descending — mirroring the backend's ORDER BY).
SessionState _withArchiveFlag(SessionState current, String sessionId, {required bool archived}) {
  final updated = current.sessions.map((s) {
    if (s.id == sessionId) return s.copyWith(archived: archived);
    return s;
  }).toList();
  updated.sort(_sessionSort);
  return current.copyWith(sessions: updated);
}

/// Comparator: non-archived first, then by lastActivity descending.
int _sessionSort(Session a, Session b) {
  if (a.archived != b.archived) {
    return a.archived ? 1 : -1;
  }
  final aTime = a.lastActivity ?? a.createdAt;
  final bTime = b.lastActivity ?? b.createdAt;
  return bTime.compareTo(aTime);
}
```

If `SessionState` doesn't have a `copyWith`, construct it directly: `SessionState(sessions: updated, isLoading: current.isLoading, error: current.error)`.

- [ ] **Step 2: Verify it compiles**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no new errors.

- [ ] **Step 3: Commit**

```bash
git add ui/flutter_ui/lib/services/session_notifier.dart
git commit -m "feat(session-notifier): add archive/unarchive with local re-sort"
```

---

### Task 2.4: Create `verbosityProvider`

**Files:**
- Create: `ui/flutter_ui/lib/providers/verbosity_provider.dart`
- Test: `ui/flutter_ui/test/providers/verbosity_provider_test.dart`

- [ ] **Step 1: Write the failing test**

```dart
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept/providers/verbosity_provider.dart';

void main() {
  group('verbosityProvider', () {
    test('initial value defaults to normal (1)', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);
      expect(container.read(verbosityProvider), 1);
    });

    test('cycleVerbosity rotates 1 -> 2 -> 0 -> 1', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);

      container.read(verbosityProvider.notifier).cycleVerbosity();
      expect(container.read(verbosityProvider), 2);

      container.read(verbosityProvider.notifier).cycleVerbosity();
      expect(container.read(verbosityProvider), 0);

      container.read(verbosityProvider.notifier).cycleVerbosity();
      expect(container.read(verbosityProvider), 1);
    });

    test('shouldEmitAgentEvent drops events with tier > current verbosity', () {
      expect(shouldEmitAgentEvent(currentVerbosity: 1, eventTier: 0), isTrue);
      expect(shouldEmitAgentEvent(currentVerbosity: 1, eventTier: 1), isTrue);
      expect(shouldEmitAgentEvent(currentVerbosity: 1, eventTier: 2), isFalse);
    });
  });
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ui/flutter_ui && flutter test test/providers/verbosity_provider_test.dart`
Expected: FAIL — file/provider doesn't exist.

- [ ] **Step 3: Create the provider**

```dart
import 'package:flutter_riverpod/flutter_riverpod.dart';

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

/// Current verbosity level. Persisted to client config on change (see
/// the save path in HomeScreen._cycleVerbosity).
final verbosityProvider = StateProvider<int>((ref) => VerbosityLevel.normal);

/// Cycle 0 -> 1 -> 2 -> 0. Matches TUI Ctrl+V (app.go:727).
extension VerbosityCycle on StateController<int> {
  void cycleVerbosity() {
    state = (state + 1) % 3;
  }
}

/// Pure predicate used by ChatNotifier to filter agent events by tier.
/// Mirrors TUI app.go:1347: `if tier <= a.verbosity`.
bool shouldEmitAgentEvent({required int currentVerbosity, required int eventTier}) {
  return eventTier <= currentVerbosity;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ui/flutter_ui && flutter test test/providers/verbosity_provider_test.dart`
Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/flutter_ui/lib/providers/verbosity_provider.dart ui/flutter_ui/test/providers/verbosity_provider_test.dart
git commit -m "feat(provider): add verbosityProvider with cycle + tier filter"
```

---

### Task 2.5: Create `tabActivationProvider`

**Files:**
- Create: `ui/flutter_ui/lib/providers/tab_activation_provider.dart`

- [ ] **Step 1: Create the provider**

```dart
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../features/home/home_screen.dart' show HomeTab;

/// Set by child widgets (e.g. SessionsList) to request that HomeScreen
/// switch to a specific tab. HomeScreen watches this, applies the switch,
/// and clears it back to null. Null = no pending request.
final tabActivationProvider = StateProvider<HomeTab?>((ref) => null);
```

- [ ] **Step 2: Verify it compiles**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no new errors.

- [ ] **Step 3: Commit**

```bash
git add ui/flutter_ui/lib/providers/tab_activation_provider.dart
git commit -m "feat(provider): add tabActivationProvider"
```

---

### Task 2.6: Create `statusMessageProvider`

**Files:**
- Create: `ui/flutter_ui/lib/providers/status_message_provider.dart`

- [ ] **Step 1: Create the provider**

```dart
import 'dart:async';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Transient status message shown by the StatusBar. Auto-clears after
/// 2.5 seconds. Null = no message (status bar renders normal content).
final statusMessageProvider = StateProvider<String?>((ref) => null);

/// Show a transient message. Auto-clears after 2.5 seconds.
void showStatusMessage(WidgetRef ref, String message) {
  ref.read(statusMessageProvider.notifier).state = message;
  Timer(const Duration(milliseconds: 2500), () {
    // Only clear if the same message is still showing (don't clobber a
    // newer message).
    if (ref.read(statusMessageProvider) == message) {
      ref.read(statusMessageProvider.notifier).state = null;
    }
  });
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no new errors.

- [ ] **Step 3: Commit**

```bash
git add ui/flutter_ui/lib/providers/status_message_provider.dart
git commit -m "feat(provider): add statusMessageProvider with auto-clear"
```

---

### Task 2.7: Create `currentProjectProvider`

**Files:**
- Create: `ui/flutter_ui/lib/providers/project_provider.dart`

- [ ] **Step 1: Determine how `SdkClient` is accessed**

Run: `grep -n "SdkClient\|sdkClientProvider\|sdkClient =" ui/flutter_ui/lib/providers/providers.dart ui/flutter_ui/lib/main.dart`

Note the existing wiring (provider? singleton? constructor param?). Use the same pattern below.

- [ ] **Step 2: Create the provider**

```dart
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../services/sdk_client.dart';

/// Mirrors TUI ProjectInfoUpdatedMsg (internal/tui/app.go:559-565).
class CurrentProject {
  final String id;
  final String name;
  final String mode;   // "git" | "local" | ""
  final String branch; // git only
  final bool dirty;    // git only

  const CurrentProject({
    required this.id,
    required this.name,
    required this.mode,
    required this.branch,
    required this.dirty,
  });

  static const empty = CurrentProject(id: '', name: '', mode: '', branch: '', dirty: false);

  bool get isActive => id.isNotEmpty;
}

/// Currently-active project. Loaded on app connect and re-loaded on
/// project switch. Matches TUI app.go:530-554 logic: find entry with
/// status=="active", fetch status for git projects to get branch + dirty.
class CurrentProjectNotifier extends StateNotifier<CurrentProject> {
  final SdkClient _client;
  CurrentProjectNotifier(this._client) : super(CurrentProject.empty);

  Future<void> refresh() async {
    try {
      final projects = await _client.listProjects();
      final active = projects.firstWhere(
        (p) => p['status'] == 'active',
        orElse: () => <String, dynamic>{},
      );
      if (active.isEmpty) {
        state = CurrentProject.empty;
        return;
      }
      final id = active['id'] as String? ?? '';
      final name = active['name'] as String? ?? '';
      final mode = active['mode'] as String? ?? '';

      String branch = '';
      bool dirty = false;
      if (mode == 'git' && id.isNotEmpty) {
        try {
          final status = await _client.getProjectStatus(id);
          branch = status['branch'] as String? ?? '';
          dirty = status['dirty'] as bool? ?? false;
        } catch (_) {
          // Status fetch is best-effort; indicator degrades to name-only.
        }
      }
      state = CurrentProject(id: id, name: name, mode: mode, branch: branch, dirty: dirty);
    } catch (_) {
      state = CurrentProject.empty;
    }
  }
}

// Replace the construction below with the wiring noted in Step 1.
// If a provider like sdkClientProvider exists, use:
//   final currentProjectProvider = StateNotifierProvider<CurrentProjectNotifier, CurrentProject>(
//     (ref) => CurrentProjectNotifier(ref.watch(sdkClientProvider)),
//   );
final currentProjectProvider =
    StateNotifierProvider<CurrentProjectNotifier, CurrentProject>((ref) {
  // TODO: wire SdkClient per Step 1 findings — do not leave this as a raw
  // construction if a provider exists.
  throw UnimplementedError('wire SdkClient per Step 1 findings');
});
```

- [ ] **Step 3: Verify it compiles** (after resolving the TODO per Step 1)

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no new errors.

- [ ] **Step 4: Commit**

```bash
git add ui/flutter_ui/lib/providers/project_provider.dart
git commit -m "feat(provider): add currentProjectProvider (active project + branch/dirty)"
```

---

### Task 2.8: Create `CachedDetailProvider<T>` generic

**Files:**
- Create: `ui/flutter_ui/lib/providers/cached_detail.dart`
- Test: `ui/flutter_ui/test/providers/cached_detail_test.dart`

- [ ] **Step 1: Write the failing test**

```dart
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept/providers/cached_detail.dart';

void main() {
  group('CachedDetail', () {
    test('first read triggers fetch; second read returns cache', () async {
      var fetchCalls = 0;
      String? fetchedId;
      final family = cachedDetailFamily<String>((id) async {
        fetchCalls++;
        fetchedId = id;
        return 'detail-for-$id';
      });

      final container = ProviderContainer();
      addTearDown(container.dispose);

      final first = container.read(family('item-1'));
      expect(first.isLoading, isTrue);
      await Future.delayed(Duration.zero);
      final settled = container.read(family('item-1'));
      expect(settled.hasValue, isTrue);
      expect(settled.value, 'detail-for-item-1');
      expect(fetchedId, 'item-1');
      expect(fetchCalls, 1);

      final again = container.read(family('item-1'));
      expect(again.value, 'detail-for-item-1');
      expect(fetchCalls, 1);
    });

    test('prefetch warms cache without blocking caller', () async {
      var fetchCalls = 0;
      final family = cachedDetailFamily<String>((id) async {
        fetchCalls++;
        await Future.delayed(const Duration(milliseconds: 5));
        return 'warmed-$id';
      });

      final container = ProviderContainer();
      addTearDown(container.dispose);

      container.read(family('pre-1'));
      await Future.delayed(const Duration(milliseconds: 20));
      expect(fetchCalls, 1);

      final cached = container.read(family('pre-1'));
      expect(cached.value, 'warmed-pre-1');
      expect(fetchCalls, 1);
    });
  });
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ui/flutter_ui && flutter test test/providers/cached_detail_test.dart`
Expected: FAIL — file doesn't exist.

- [ ] **Step 3: Create the generic**

Riverpod family providers cache by argument automatically. The key insight: a family of `FutureProvider`s gives us per-id caching for free; we just need to make sure we don't re-fetch on subsequent reads of the same id (Riverpod handles this for us as long as the container isn't disposed).

```dart
import 'package:flutter_riverpod/flutter_riverpod.dart';

typedef DetailFetcher<T> = Future<T> Function(String id);

/// Build a family of per-id AsyncValue providers backed by [fetcher].
/// Riverpod caches by id; subsequent reads of the same id return the
/// cached AsyncValue without re-invoking the fetcher.
///
/// Usage:
///   final sessionDetailFamily = cachedDetailFamily<Session>(
///     (id) async => sdkClient.getSession(id),
///   );
///   final detail = ref.watch(sessionDetailFamily('sess-1'));
CachedDetailFamily<T> cachedDetailFamily<T>(DetailFetcher<T> fetcher) {
  return CachedDetailFamily<T>(fetcher);
}

class CachedDetailFamily<T> {
  final DetailFetcher<T> fetcher;
  late final ProviderListenable<AsyncValue<T>> Function(String id) _provider;

  CachedDetailFamily(this.fetcher) {
    final inner = FutureProvider.family<T, String>((ref, id) async {
      return fetcher(id);
    });
    _provider = inner;
  }

  AsyncValue<T> call() => throw StateError('use family(id) syntax');
  ProviderListenable<AsyncValue<T>> call(String id) => _provider(id);
}
```

If the Riverpod version in `pubspec.yaml` doesn't support `FutureProvider.family` cleanly (e.g., v2 changes), adapt to whatever the codebase already uses for family providers. Check existing family providers via: `grep -rn ".family<" ui/flutter_ui/lib/`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ui/flutter_ui && flutter test test/providers/cached_detail_test.dart`
Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/flutter_ui/lib/providers/cached_detail.dart ui/flutter_ui/test/providers/cached_detail_test.dart
git commit -m "feat(provider): add CachedDetailFamily<T> generic"
```

---

## Phase 3: Flutter Status Bar + Command Palette

Depends on Phase 2.

### Task 3.1: Create `StatusBar` widget

**Files:**
- Create: `ui/flutter_ui/lib/widgets/status_bar.dart`
- Test: `ui/flutter_ui/test/widgets/status_bar_test.dart`

- [ ] **Step 1: Write the failing test**

```dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept/providers/verbosity_provider.dart';
import 'package:meept/providers/status_message_provider.dart';
import 'package:meept/providers/project_provider.dart';
import 'package:meept/widgets/status_bar.dart';

Widget _wrap({required int tab, CurrentProject? project, String? status}) {
  return ProviderScope(
    overrides: [
      verbosityProvider.overrideWith((ref) => 1),
      currentProjectProvider.overrideWith(
        (ref) => CurrentProjectNotifier(_NoopClient()),
      ),
    ],
    child: MaterialApp(home: Scaffold(body: StatusBar(selectedTabIndex: tab))),
  );
}

class _NoopClient implements SdkClient {
  // minimal stub if needed; otherwise omit and use override-only
}

void main() {
  testWidgets('renders verbosity + connection', (tester) async {
    await tester.pumpWidget(_wrap(tab: 0));
    await tester.pump();
    expect(find.textContaining('verbosity'), findsOneWidget);
  });

  testWidgets('renders transient status message when set, hides other parts',
      (tester) async {
    final container = ProviderContainer();
    addTearDown(container.dispose);
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(home: const Scaffold(body: StatusBar(selectedTabIndex: 0))),
      ),
    );
    await tester.pump();
    container.read(statusMessageProvider.notifier).state = 'session archived';
    await tester.pump();
    expect(find.text('session archived'), findsOneWidget);
    expect(find.textContaining('verbosity'), findsNothing);
  });

  testWidgets('keybind hint shows sessions-specific text on sessions tab',
      (tester) async {
    await tester.pumpWidget(_wrap(tab: 1));
    await tester.pump();
    expect(find.textContaining('archive'), findsOneWidget);
  });

  testWidgets('keybind hint shows chat-specific text on chat tab',
      (tester) async {
    await tester.pumpWidget(_wrap(tab: 0));
    await tester.pump();
    expect(find.textContaining('focus'), findsOneWidget);
  });
}
```

Note: the exact provider override syntax depends on the Riverpod version. Adjust as needed.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ui/flutter_ui && flutter test test/widgets/status_bar_test.dart`
Expected: FAIL — StatusBar doesn't exist.

- [ ] **Step 3: Create the widget**

Create `ui/flutter_ui/lib/widgets/status_bar.dart`:

```dart
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';
import '../providers/providers.dart';
import '../providers/verbosity_provider.dart';
import '../providers/status_message_provider.dart';
import '../providers/project_provider.dart';

/// Single-line status bar pinned at the bottom of the HomeScreen.
/// Mirrors TUI renderStatusBar (internal/tui/app.go:2236-2289).
class StatusBar extends ConsumerWidget {
  final int selectedTabIndex;
  const StatusBar({super.key, required this.selectedTabIndex});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final transient = ref.watch(statusMessageProvider);
    if (transient != null) {
      return _bar(child: Text(transient, style: _mutedStyle));
    }

    final parts = <String>[];
    parts.add(_connectionPart(ref));
    final sessionPart = _sessionPart(ref);
    if (sessionPart.isNotEmpty) parts.add(sessionPart);
    parts.add(_keybindHint(selectedTabIndex));
    final projectPart = _projectPart(ref);
    if (projectPart != null) parts.add(projectPart);
    parts.add('verbosity: ${VerbosityLevel.name(ref.watch(verbosityProvider))}');

    return _bar(
      child: Text(
        parts.join(' · '),
        style: _mutedStyle,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
    );
  }

  Widget _bar({required Widget child}) => Container(
        height: 22,
        padding: const EdgeInsets.symmetric(horizontal: 12),
        decoration: BoxDecoration(
          color: CyberpunkColors.blackTransparent(0.7),
          border: Border(top: BorderSide(color: CyberpunkColors.midGray, width: 1)),
        ),
        alignment: Alignment.centerLeft,
        child: child,
      );

  TextStyle get _mutedStyle => CyberpunkTypography.bodySmall.copyWith(
        color: CyberpunkColors.midGray,
        fontFamily: 'SourceCodePro',
        fontSize: 10,
      );

  String _connectionPart(WidgetRef ref) {
    final connected = ref.watch(connectionStateProvider);
    final status = ref.watch(connectionStatusProvider);
    final dot = connected ? '●' : '○';
    return '$dot $status';
  }

  String _sessionPart(WidgetRef ref) {
    final session = ref.watch(activeSessionProvider);
    final name = session?.title;
    if (name == null || name.isEmpty || name == 'default') return '';
    return 'session: ${name.toLowerCase()}';
  }

  String _keybindHint(int tabIndex) {
    switch (tabIndex) {
      case 0:
        return '^k focus · / cmd · ^f find · ^v verbosity';
      case 1:
        return 'dbl-click open · ⌫ archive';
      default:
        return 'j/k navigate · enter select';
    }
  }

  String? _projectPart(WidgetRef ref) {
    final p = ref.watch(currentProjectProvider);
    if (!p.isActive) return null;
    final name = p.name.length > 16 ? '${p.name.substring(0, 13)}...' : p.name;
    if (p.mode == 'git') {
      final branch = p.branch.isNotEmpty ? ' ${p.branch}' : '';
      final dirty = p.dirty ? '*' : '';
      return '[$name$branch$dirty]';
    }
    return '[local:$name]';
  }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ui/flutter_ui && flutter test test/widgets/status_bar_test.dart`
Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/flutter_ui/lib/widgets/status_bar.dart ui/flutter_ui/test/widgets/status_bar_test.dart
git commit -m "feat(widget): add StatusBar with connection/session/keys/project/verbosity"
```

---

### Task 3.2: Create `CommandPalette` widget

**Files:**
- Create: `ui/flutter_ui/lib/widgets/command_palette.dart`
- Test: `ui/flutter_ui/test/widgets/command_palette_test.dart`

- [ ] **Step 1: Write the failing test**

```dart
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept/widgets/command_palette.dart';

void main() {
  testWidgets('shows all 9 items with labels', (tester) async {
    CommandPaletteItem? selected;
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: CommandPalette(
          items: CommandPalette.defaultItems,
          onSelected: (item) => selected = item,
        ),
      ),
    ));
    await tester.pump();
    expect(find.text('chat'), findsOneWidget);
    expect(find.text('sessions'), findsOneWidget);
    expect(find.text('agents'), findsOneWidget);
    expect(find.text('new session'), findsOneWidget);
    expect(find.text('find…'), findsOneWidget);
  });

  testWidgets('arrow down moves selection; enter activates', (tester) async {
    CommandPaletteItem? selected;
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: CommandPalette(
          items: CommandPalette.defaultItems,
          onSelected: (item) => selected = item,
        ),
      ),
    ));
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.pump();
    // Index 1 = sessions.
    expect(selected?.label, 'sessions');
  });

  testWidgets('click activates the tapped item', (tester) async {
    CommandPaletteItem? selected;
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: CommandPalette(
          items: CommandPalette.defaultItems,
          onSelected: (item) => selected = item,
        ),
      ),
    ));
    await tester.pump();
    await tester.tap(find.text('tasks'));
    await tester.pump();
    expect(selected?.label, 'tasks');
  });
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ui/flutter_ui && flutter test test/widgets/command_palette_test.dart`
Expected: FAIL — file doesn't exist.

- [ ] **Step 3: Create the widget**

Create `ui/flutter_ui/lib/widgets/command_palette.dart`:

```dart
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';

class CommandPaletteItem {
  final String keybinding;
  final String label;
  final String description;
  const CommandPaletteItem({
    required this.keybinding,
    required this.label,
    required this.description,
  });
}

class CommandPalette extends StatefulWidget {
  final List<CommandPaletteItem> items;
  final void Function(CommandPaletteItem item) onSelected;

  const CommandPalette({
    super.key,
    required this.items,
    required this.onSelected,
  });

  /// Matches TUI modal.go:192-205, adapted to Flutter surfaces.
  /// Omitted TUI items: queue view, memory view, toggle sidebar (no Flutter route).
  static List<CommandPaletteItem> get defaultItems => const [
    CommandPaletteItem(keybinding: 'c', label: 'chat', description: 'switch to chat view'),
    CommandPaletteItem(keybinding: 's', label: 'sessions', description: 'switch to sessions view'),
    CommandPaletteItem(keybinding: 'p', label: 'plans', description: 'switch to plans view'),
    CommandPaletteItem(keybinding: 't', label: 'tasks', description: 'switch to tasks view'),
    CommandPaletteItem(keybinding: 'a', label: 'agents', description: 'switch to employees view'),
    CommandPaletteItem(keybinding: 'f', label: 'find…', description: 'search sessions and tasks'),
    CommandPaletteItem(keybinding: 'n', label: 'new session', description: 'create a new session'),
    CommandPaletteItem(keybinding: 'e', label: 'edit description', description: 'edit session description'),
    CommandPaletteItem(keybinding: 'o', label: 'projects', description: 'manage projects'),
  ];

  @override
  State<CommandPalette> createState() => _CommandPaletteState();
}

class _CommandPaletteState extends State<CommandPalette> {
  int _selected = 0;
  final _focusNode = FocusNode();

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _focusNode.requestFocus();
    });
  }

  @override
  void dispose() {
    _focusNode.dispose();
    super.dispose();
  }

  KeyEventResult _handleKeyEvent(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent) return KeyEventResult.ignored;
    final key = event.logicalKey;
    if (key == LogicalKeyboardKey.arrowDown) {
      setState(() => _selected = (_selected + 1) % widget.items.length);
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.arrowUp) {
      setState(() => _selected = (_selected - 1 + widget.items.length) % widget.items.length);
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.enter) {
      widget.onSelected(widget.items[_selected]);
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.escape) {
      Navigator.of(context).maybePop();
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  @override
  Widget build(BuildContext context) {
    return Focus(
      focusNode: _focusNode,
      onKeyEvent: _handleKeyEvent,
      child: Container(
        color: CyberpunkColors.darkGray,
        child: ListView.builder(
          itemCount: widget.items.length,
          itemBuilder: (context, index) {
            final item = widget.items[index];
            final isSel = index == _selected;
            return InkWell(
              onTap: () => widget.onSelected(item),
              onHover: (h) => h ? setState(() => _selected = index) : null,
              child: Container(
                color: isSel
                    ? CyberpunkColors.orangePrimary.withValues(alpha: 0.15)
                    : null,
                padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                child: Row(
                  children: [
                    SizedBox(
                      width: 30,
                      child: Text(
                        item.keybinding,
                        style: CyberpunkTypography.bodySmall.copyWith(
                          color: CyberpunkColors.midGray,
                          fontFamily: 'SourceCodePro',
                        ),
                      ),
                    ),
                    SizedBox(
                      width: 130,
                      child: Text(
                        item.label,
                        style: CyberpunkTypography.bodySmall.copyWith(
                          color: isSel
                              ? CyberpunkColors.orangePrimary
                              : CyberpunkColors.greenSuccess,
                          fontFamily: 'SourceCodePro',
                        ),
                      ),
                    ),
                    Expanded(
                      child: Text(
                        item.description,
                        style: CyberpunkTypography.bodySmall.copyWith(
                          color: CyberpunkColors.lightGray,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            );
          },
        ),
      ),
    );
  }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ui/flutter_ui && flutter test test/widgets/command_palette_test.dart`
Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/flutter_ui/lib/widgets/command_palette.dart ui/flutter_ui/test/widgets/command_palette_test.dart
git commit -m "feat(widget): add CommandPalette modal with 9 items"
```

---

### Task 3.3: Replace leader-key with command palette; add Ctrl+V; mount StatusBar; wire tab activation

**Files:**
- Modify: `ui/flutter_ui/lib/core/shortcuts.dart`
- Modify: `ui/flutter_ui/lib/features/home/home_screen.dart`

- [ ] **Step 1: Strip the leader-key state machine from `LeaderKeyController`**

In `ui/flutter_ui/lib/core/shortcuts.dart`, remove:
- `_waiting`, `_enterLeaderMode`, `_exitLeaderMode`, `_timeout`, `isWaiting` fields/methods.
- `handleLeaderSequence` method.
- From `_AppShortcutsState._handleKeyEvent`: the `if (ctrl.isWaiting) { return ctrl.handleLeaderSequence(event); }` branch.
- The slash→`?` mapping in `_logicalKeyToChar` (no longer needed).

Keep the direct-shortcut detection (`_isFocusInputTrigger`, `_isFindTrigger`, `_isGlobalSearchTrigger`, `escape`). Keep `onTabSelected`, `onFocusInput`, `onFind`, `onInSessionFind`, `onGlobalSearch`, `onBranches`, `onShowHelp`, `onNavigate`.

Add new callbacks:
```dart
VoidCallback? onShowCommandPalette;
VoidCallback? onCycleVerbosity;
```

Modify `_isLeaderTrigger` so it now signals "open command palette". The trigger keys stay (Cmd+X mac, Ctrl+X other) but the action changes — when it fires, call `onShowCommandPalette?.call()` instead of entering leader mode.

- [ ] **Step 2: Add Ctrl+V handler**

In `_AppShortcutsState._handleKeyEvent`, add a new trigger check before the existing ones:

```dart
if (_isVerbosityTrigger(event)) {
  widget.controller.onCycleVerbosity?.call();
  return KeyEventResult.handled;
}
```

Add the trigger predicate:

```dart
static bool _isVerbosityTrigger(KeyEvent event) {
  if (event is! KeyDownEvent) return false;
  if (event.logicalKey != LogicalKeyboardKey.keyV) return false;
  // Ctrl+V on ALL platforms (parity with TUI per CLAUDE.md UI conventions).
  return HardwareKeyboard.instance.isControlPressed;
}
```

- [ ] **Step 3: Wire callbacks in `HomeScreen`**

In `ui/flutter_ui/lib/features/home/home_screen.dart`:

3a. Remove the `isWaiting` banner (around lines 442-460).

3b. In `initState`, set the new callbacks (alongside the existing `_leaderController.onTabSelected = …`):

```dart
_leaderController.onShowCommandPalette = _showCommandPalette;
_leaderController.onCycleVerbosity = _cycleVerbosity;
```

3c. Add the methods:

```dart
void _showCommandPalette() {
  showDialog(
    context: context,
    builder: (_) => AlertDialog(
      backgroundColor: CyberpunkColors.darkGray,
      title: Text('command palette', style: CyberpunkTypography.headlineMedium.copyWith(
        color: CyberpunkColors.orangePrimary,
      )),
      contentPadding: const EdgeInsets.symmetric(vertical: 8),
      content: SizedBox(
        width: 480,
        child: CommandPalette(
          items: CommandPalette.defaultItems,
          onSelected: (item) {
            Navigator.of(context).pop();
            _handlePaletteSelection(item);
          },
        ),
      ),
    ),
  );
}

void _handlePaletteSelection(CommandPaletteItem item) {
  switch (item.label) {
    case 'chat':
      setState(() => _selectedTab = HomeTab.chat);
      context.go('/');
      break;
    case 'sessions':
      setState(() => _selectedTab = HomeTab.sessions);
      context.go('/sessions');
      break;
    case 'plans':
      setState(() => _selectedTab = HomeTab.plans);
      context.go('/plans');
      break;
    case 'tasks':
      setState(() => _selectedTab = HomeTab.tasks);
      context.go('/tasks');
      break;
    case 'agents':
      setState(() => _selectedTab = HomeTab.agents);
      context.go('/agents');
      break;
    case 'find…':
      context.goToolSearch();
      break;
    case 'new session':
      setState(() => _selectedTab = HomeTab.sessions);
      context.go('/sessions');
      break;
    case 'edit description':
      setState(() => _selectedTab = HomeTab.sessions);
      context.go('/sessions');
      break;
    case 'projects':
      context.goToolBranches();
      break;
  }
}

void _cycleVerbosity() {
  final notifier = ref.read(verbosityProvider.notifier);
  notifier.state = (notifier.state + 1) % 3;
  final level = notifier.state;
  // Persist via SdkClient — adjust provider name as needed.
  ref.read(sdkClientProvider).setClientConfig(
    {'chat': {'verbosity': VerbosityLevel.name(level)}});
  showStatusMessage(ref, 'verbosity: ${VerbosityLevel.name(level)}');
}
```

If `sdkClientProvider` doesn't exist, find the equivalent way `SdkClient` is accessed in this file (it's used elsewhere — check the pattern). Add the import for `verbosityProvider`, `statusMessageProvider`, `VerbosityLevel`, `showStatusMessage`, `CommandPalette`, `CommandPaletteItem`.

- [ ] **Step 4: Mount the StatusBar**

In `HomeScreen.build`, modify the `Column` (around line 397-439) to insert `StatusBar` after `Expanded(child: _buildTabContent())`:

```dart
Expanded(
  child: _buildTabContent(),
),
StatusBar(selectedTabIndex: _selectedTab.index),
```

- [ ] **Step 5: Apply `tabActivationProvider`**

In `HomeScreen.build`, add a listener:

```dart
ref.listen<HomeTab?>(tabActivationProvider, (prev, next) {
  if (next != null) {
    if (next != _selectedTab) {
      setState(() => _selectedTab = next);
    }
    ref.read(tabActivationProvider.notifier).state = null;
  }
});
```

- [ ] **Step 6: Refresh `currentProjectProvider` on connect**

In `_onConnectionChanged` (around line 267), add after the existing refresh calls:

```dart
ref.read(currentProjectProvider.notifier).refresh();
```

- [ ] **Step 7: Verify it compiles**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no new errors.

- [ ] **Step 8: Manually verify**

Run: `cd ui/flutter_ui && flutter run -d macos` (or appropriate device). Verify:
- Cmd+X / Ctrl+X opens the palette dialog.
- Arrow keys + enter activate items.
- Ctrl+V cycles verbosity; status bar updates.
- Status bar shows at the bottom of the window.

- [ ] **Step 9: Commit**

```bash
git add ui/flutter_ui/lib/core/shortcuts.dart ui/flutter_ui/lib/features/home/home_screen.dart
git commit -m "feat(shortcuts): replace leader-key with command palette; add Ctrl+V verbosity; mount StatusBar; wire tab activation"
```

---

## Phase 4: Flutter Tile/List Fixes (Gaps 3, 4)

### Task 4.1: Resize agent tiles

**Files:**
- Modify: `ui/flutter_ui/lib/features/agents/agents_tab.dart`
- Test: `ui/flutter_ui/test/widgets/agents_tab_test.dart`

- [ ] **Step 1: Locate `agentProvider` and `AgentState` shapes**

Run: `grep -n "agentProvider\|class AgentState\|class AgentNotifier" ui/flutter_ui/lib/providers/providers.dart ui/flutter_ui/lib/services/*.dart`

Note the exact provider type and state shape — the test override must match.

- [ ] **Step 2: Write the failing test**

```dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept/features/agents/agents_tab.dart';
import 'package:meept/models/api_models.dart';
// import agentProvider + AgentState per Step 1 findings

void main() {
  testWidgets('agent tiles are ~150 wide and ~58 tall', (tester) async {
    tester.view.physicalDevicePixelRatio = 1.0;
    tester.view.devicePixelRatio = 1.0;
    tester.view.physicalSize = const Size(800, 600);
    addTearDown(tester.view.resetPhysicalSize);

    final agents = List.generate(
      3,
      (i) => Agent(id: 'agent-$i', name: 'Agent $i', description: 'd'),
    );

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          // Adjust the override shape to match agentProvider's actual type
          // per Step 1 findings.
          agentProvider.overrideWith((ref) => AgentState(agents: agents, isLoading: false)),
        ],
        child: const MaterialApp(home: Scaffold(body: AgentsTab())),
      ),
    );
    await tester.pump();

    final tiles = find.byKey(const ValueKey('agent-tile'));
    expect(tiles.evaluate, isNotEmpty);

    final firstTile = tester.getRect(tiles.first);
    expect(firstTile.width, closeTo(150, 25));
    expect(firstTile.height, closeTo(58, 15));
  });

  testWidgets('widening window shows more tiles per row', (tester) async {
    tester.view.physicalDevicePixelRatio = 1.0;
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);

    final agents = List.generate(
      10,
      (i) => Agent(id: 'agent-$i', name: 'Agent $i', description: 'd'),
    );

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          agentProvider.overrideWith((ref) => AgentState(agents: agents, isLoading: false)),
        ],
        child: const MaterialApp(home: Scaffold(body: AgentsTab())),
      ),
    );

    tester.view.physicalSize = const Size(400, 800);
    await tester.pump();
    final narrowTiles = find.byKey(const ValueKey('agent-tile')).evaluate().map(
      (e) => tester.getRect(find.byWidget(tester.widget(e))),
    ).toList();
    final narrowFirstRowY = narrowTiles.first.top;
    final narrowCols = narrowTiles.where((r) => (r.top - narrowFirstRowY).abs() < 1).length;

    tester.view.physicalSize = const Size(1200, 800);
    await tester.pump();
    final wideTiles = find.byKey(const ValueKey('agent-tile')).evaluate().map(
      (e) => tester.getRect(find.byWidget(tester.widget(e))),
    ).toList();
    final wideFirstRowY = wideTiles.first.top;
    final wideCols = wideTiles.where((r) => (r.top - wideFirstRowY).abs() < 1).length;

    expect(wideCols, greaterThan(narrowCols));
  });
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd ui/flutter_ui && flutter test test/widgets/agents_tab_test.dart`
Expected: FAIL — tile size/column count doesn't match.

- [ ] **Step 4: Update the agents tab**

In `ui/flutter_ui/lib/features/agents/agents_tab.dart`:

4a. Change the grid delegate (around line 99-104):

```dart
gridDelegate: const SliverGridDelegateWithMaxCrossAxisExtent(
  maxCrossAxisExtent: 150,
  crossAxisSpacing: 8,
  mainAxisSpacing: 8,
  childAspectRatio: 2.6,
),
```

4b. Add a `Key` to each tile's `InkWell` (so the test can find them):

```dart
return InkWell(
  key: ValueKey('agent-tile-${agent.id}'),
  ...
);
```

The test uses `find.byKey(const ValueKey('agent-tile'))` which would match a single widget with that exact key — adjust the test to use `find.byKey(const ValueKey('agent-tile-0'))` and so on, OR change the key to a partial-match pattern. Simplest: in the test, query by `find.byIcon(getAgentIcon('agent-0'))` if `getAgentIcon` is exported, or use `find.byType(InkWell)` filtered to the GridView. Use whatever the test framework supports cleanly.

4c. Tighten padding and simplify content in `_buildAgentCard` (around line 118-177):

```dart
child: Container(
  padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),  // was EdgeInsets.all(16)
  decoration: BoxDecoration(
    color: isSelected
        ? CyberpunkColors.orangePrimary.withValues(alpha: 0.1)
        : CyberpunkColors.black,
    border: Border.all(
      color: isSelected ? CyberpunkColors.orangePrimary : CyberpunkColors.midGray,
      width: 1,
    ),
    borderRadius: BorderRadius.circular(8),
  ),
  child: Row(  // single row: icon + name (was Column with icon+name row and id line)
    children: [
      Icon(
        getAgentIcon(agent.id),
        color: isSelected ? CyberpunkColors.orangePrimary : CyberpunkColors.greenSuccess,
        size: 20,  // was 24
      ),
      const SizedBox(width: 8),
      Expanded(
        child: Text(
          agent.name.toLowerCase(),
          style: CyberpunkTypography.bodySmall.copyWith(  // bodySmall, not bodyMedium
            color: isSelected ? CyberpunkColors.orangePrimary : CyberpunkColors.greenSuccess,
            fontFamily: 'SourceCodePro',
          ),
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
      ),
    ],
  ),
),
```

Remove the separate `agent.id` line entirely.

- [ ] **Step 5: Run test to verify it passes**

Run: `cd ui/flutter_ui && flutter test test/widgets/agents_tab_test.dart`
Expected: both tests PASS.

- [ ] **Step 6: Commit**

```bash
git add ui/flutter_ui/lib/features/agents/agents_tab.dart ui/flutter_ui/test/widgets/agents_tab_test.dart
git commit -m "feat(agents): resize tiles to ~150x58 with max-cross-axis-extent"
```

---

### Task 4.2: Fix double-click tab activation (Gap 4)

**Files:**
- Modify: `ui/flutter_ui/lib/features/sessions/sessions_list.dart` (~line 194-199)
- Test: `ui/flutter_ui/test/widgets/sessions_list_test.dart`

- [ ] **Step 1: Write the failing test**

```dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:meept/features/sessions/sessions_list.dart';
import 'package:meept/features/home/home_screen.dart' show HomeTab;
import 'package:meept/models/api_models.dart';
// import sessionProvider, tabActivationProvider, activeSessionProvider

void main() {
  testWidgets('double-tap sets tabActivationProvider to chat', (tester) async {
    final container = ProviderContainer();
    addTearDown(container.dispose);

    final session = Session(
      id: 's1',
      title: 'session one',
      createdAt: DateTime.now(),
      lastActivity: DateTime.now(),
    );

    // Set up the provider state synchronously, bypassing the network fetch.
    container.read(sessionProvider.notifier).setSessionsForTest([session]);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(home: Scaffold(body: SessionsList())),
      ),
    );
    await tester.pump();

    await tester.tap(find.text('session one'));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.tap(find.text('session one'));
    await tester.pump();

    expect(container.read(tabActivationProvider), HomeTab.chat);
    expect(container.read(activeSessionProvider)?.id, 's1');
  });
}
```

If `setSessionsForTest` doesn't exist on `SessionNotifier`, add a test-only method or use a direct state setter:

```dart
// On SessionNotifier (test helper):
void setSessionsForTest(List<Session> sessions) {
  state = state.copyWith(sessions: sessions, isLoading: false, error: null);
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ui/flutter_ui && flutter test test/widgets/sessions_list_test.dart`
Expected: FAIL — `tabActivationProvider` is null (not set).

- [ ] **Step 3: Update the double-tap handler**

In `ui/flutter_ui/lib/features/sessions/sessions_list.dart:194-199`:

```dart
return InkWell(
  key: ValueKey('session-tile-${session.id}'),
  onTap: () => ref.read(activeSessionProvider.notifier).state = session,
  onDoubleTap: () {
    ref.read(activeSessionProvider.notifier).state = session;
    ref.read(tabActivationProvider.notifier).state = HomeTab.chat;  // NEW
    context.go('/');
  },
  ...
);
```

Import `HomeTab` from `home_screen.dart` and `tabActivationProvider` from its new file.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ui/flutter_ui && flutter test test/widgets/sessions_list_test.dart`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/flutter_ui/lib/features/sessions/sessions_list.dart ui/flutter_ui/lib/services/session_notifier.dart ui/flutter_ui/test/widgets/sessions_list_test.dart
git commit -m "fix(sessions): double-click sets tabActivationProvider to chat"
```

---

## Phase 5: Archive UI (Flutter)

Depends on Phases 1 and 2.

### Task 5.1: Trash icon → archive; long-press → delete permanently; greyed archived tiles

**Files:**
- Modify: `ui/flutter_ui/lib/features/sessions/sessions_list.dart`
- Test: `ui/flutter_ui/test/widgets/session_archive_test.dart`

- [ ] **Step 1: Write the failing test**

```dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept/features/sessions/sessions_list.dart';
import 'package:meept/models/api_models.dart';

void main() {
  testWidgets('archived session renders with reduced opacity and sorts after active', (tester) async {
    final container = ProviderContainer();
    addTearDown(container.dispose);

    final archived = Session(
      id: 'arc1', title: 'arc me',
      createdAt: DateTime.now(), lastActivity: DateTime.now(),
      archived: true,
    );
    final active = Session(
      id: 'act1', title: 'active',
      createdAt: DateTime.now(), lastActivity: DateTime.now(),
    );

    container.read(sessionProvider.notifier).setSessionsForTest([active, archived]);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(home: Scaffold(body: SessionsList())),
      ),
    );
    await tester.pump();

    final archivedTile = find.byKey(const ValueKey('session-tile-arc1'));
    expect(archivedTile, findsOneWidget);

    // Active session should appear first (above archived).
    final activeTile = find.byKey(const ValueKey('session-tile-act1'));
    expect(
      tester.getCenter(activeTile).dy,
      lessThan(tester.getCenter(archivedTile).dy),
    );

    // Archived tile should be wrapped in an Opacity widget < 1.0.
    final opacityFinder = find.descendant(
      of: archivedTile,
      matching: find.byType(Opacity),
    );
    expect(opacityFinder, findsOneWidget);
    final opacity = tester.widget<Opacity>(opacityFinder).opacity;
    expect(opacity, lessThan(1.0));
  });

  testWidgets('archive icon tap shows archive confirmation dialog', (tester) async {
    final container = ProviderContainer();
    addTearDown(container.dispose);
    final session = Session(
      id: 'x1', title: 'target',
      createdAt: DateTime.now(), lastActivity: DateTime.now(),
    );
    container.read(sessionProvider.notifier).setSessionsForTest([session]);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(home: Scaffold(body: SessionsList())),
      ),
    );
    await tester.pump();

    await tester.tap(find.byIcon(Icons.archive_outlined));
    await tester.pump();
    expect(find.text('archive session?'), findsOneWidget);
  });
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ui/flutter_ui && flutter test test/widgets/session_archive_test.dart`
Expected: FAIL — archive icon not present; archived sessions not dimmed.

- [ ] **Step 3: Update the session tile**

In `ui/flutter_ui/lib/features/sessions/sessions_list.dart`, modify `_buildSessionTile`:

3a. Wrap the tile in `Opacity` and add the `ValueKey`:

```dart
Widget _buildSessionTile(Session session, bool isSelected) {
  return Opacity(
    opacity: session.archived ? 0.5 : 1.0,
    child: InkWell(
      key: ValueKey('session-tile-${session.id}'),
      onTap: () => ref.read(activeSessionProvider.notifier).state = session,
      onDoubleTap: () {
        ref.read(activeSessionProvider.notifier).state = session;
        ref.read(tabActivationProvider.notifier).state = HomeTab.chat;
        context.go('/');
      },
      onLongPress: () => _showContextMenu(context, session),
      child: Container(
        // existing styling, but tint text grey if archived
        // (existing code stays; the Opacity wrapper handles dimming)
        ...
      ),
    ),
  );
}
```

3b. Change the icon button (around line 237-242) from delete to archive:

```dart
IconButton(
  icon: const Icon(Icons.archive_outlined, size: 16),
  color: CyberpunkColors.orangeDark,
  onPressed: () => _showArchiveConfirmation(session.id, session.title),
),
```

3c. Replace `_showDeleteConfirmation` (or add alongside) with `_showArchiveConfirmation`:

```dart
void _showArchiveConfirmation(String sessionId, String title) {
  showDialog(
    context: context,
    builder: (context) => AlertDialog(
      backgroundColor: CyberpunkColors.darkGray,
      title: const Text('archive session?', style: CyberpunkTypography.headlineMedium),
      content: Text('"$title"', style: CyberpunkTypography.bodyMedium),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('cancel', style: CyberpunkTypography.bodyMedium),
        ),
        FilledButton(
          onPressed: () {
            ref.read(sessionProvider.notifier).archiveSession(sessionId);
            showStatusMessage(ref, 'archived: ${title.toLowerCase()}');
            Navigator.pop(context);
          },
          child: const Text('archive', style: CyberpunkTypography.bodyMedium),
        ),
      ],
    ),
  );
}
```

3d. Add long-press context menu for permanent delete:

```dart
void _showContextMenu(BuildContext context, Session session) {
  showMenu<String>(
    context: context,
    position: const RelativeRect.fromLTRB(0, 0, 0, 0),  // adjust to tap position if available
    items: const [
      PopupMenuItem(value: 'delete', child: Text('delete permanently')),
    ],
  ).then((value) {
    if (value == 'delete') {
      _showDeleteConfirmation(session.id, session.title);
    }
  });
}
```

Keep the existing `_showDeleteConfirmation` for the permanent-delete path. Update its text to "delete permanently?" for clarity.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ui/flutter_ui && flutter test test/widgets/session_archive_test.dart`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/flutter_ui/lib/features/sessions/sessions_list.dart ui/flutter_ui/test/widgets/session_archive_test.dart
git commit -m "feat(sessions): trash->archive, long-press->delete, greyed archived tiles"
```

---

## Phase 6: Cached Detail (Gap 5)

### Task 6.1: Wire cached detail for sessions

**Files:**
- Create: `ui/flutter_ui/lib/providers/session_detail.dart`
- Modify: `ui/flutter_ui/lib/features/sessions/sessions_detail.dart`
- Modify: `ui/flutter_ui/lib/features/home/home_screen.dart` (warm default)

- [ ] **Step 1: Create `sessionDetailFamily`**

Create `ui/flutter_ui/lib/providers/session_detail.dart`:

```dart
import '../models/api_models.dart';
import '../services/sdk_client.dart';
import 'cached_detail.dart';
import 'providers.dart' show sdkClientProvider;

// Verify sdkClientProvider exists in providers.dart; if not, wire the
// SdkClient access per whatever pattern the codebase uses.
final sessionDetailFamily = cachedDetailFamily<Session>((id) async {
  final client = /* SdkClient — per existing pattern */;
  final raw = await client.getSession(id);
  return Session.fromJson(raw);
});
```

If `SdkClient.getSession(id)` doesn't exist (only `getMessages`), add a thin method to `sdk_client.dart`:

```dart
Future<Map<String, dynamic>> getSession(String sessionId) async {
  return _get('/api/v1/sessions/$sessionId');
}
```

- [ ] **Step 2: Use it in the detail pane**

In `ui/flutter_ui/lib/features/sessions/sessions_detail.dart`, replace the direct fetch (if any) with `ref.watch(sessionDetailFamily(sessionId))`. Render a placeholder when loading:

```dart
final detail = ref.watch(sessionDetailFamily(sessionId));
return detail.when(
  data: (session) => /* existing render */,
  loading: () => const Center(child: Text('loading…', style: CyberpunkTypography.bodySmall)),
  error: (e, _) => Center(
    child: Text('error: $e',
        style: CyberpunkTypography.bodySmall.copyWith(color: CyberpunkColors.redAlert)),
  ),
);
```

- [ ] **Step 3: Warm default session on connect**

In `home_screen.dart:_onConnectionChanged`, after the existing refresh calls:

```dart
// Warm the default session detail so the chat tab is never blank on startup.
ref.read(sessionDetailFamily('default'));
```

- [ ] **Step 4: Manual verify**

Click through several sessions rapidly — no spinner should appear on already-visited sessions; first visit to an unvisited session shows brief "loading…".

- [ ] **Step 5: Commit**

```bash
git add ui/flutter_ui/lib/providers/session_detail.dart ui/flutter_ui/lib/features/sessions/sessions_detail.dart ui/flutter_ui/lib/features/home/home_screen.dart
git commit -m "feat(sessions): cache session detail, warm default, placeholder while loading"
```

---

### Task 6.2: Replicate for agents, plans, tasks

**Files:**
- Create: `ui/flutter_ui/lib/providers/agent_detail.dart`, `plan_detail.dart`, `task_detail.dart`
- Modify: each tab's detail widget
- Modify: each tab's `initState` (first-item prefetch)

- [ ] **Step 1: Create detail families**

Mirror Task 6.1 Step 1 for each tab:

```dart
// agent_detail.dart
final agentDetailFamily = cachedDetailFamily<Agent>((id) async {
  final client = /* ... */;
  final raw = await client.getAgent(id);
  return Agent.fromJson(raw);
});
// Similar for plan_detail.dart and task_detail.dart.
```

If `SdkClient.getAgent(id)` / `getPlan(id)` / `getTask(id)` don't exist, add them mirroring `listAgents`/`listPlans`/`listTasks` patterns.

- [ ] **Step 2: Use them in each tab's detail view**

Mirror Task 6.1 Step 2 for each tab's detail pane.

- [ ] **Step 3: Prefetch first item on first tab visit**

In each tab's `initState` (after the list load), add:

```dart
WidgetsBinding.instance.addPostFrameCallback((_) {
  ref.read(<tab>Provider.notifier).load<tab>s().then((_) {
    final items = ref.read(<tab>Provider).<tab>s;
    if (items.isNotEmpty) {
      ref.read(<tab>DetailFamily(items.first.id));  // warm cache
    }
  });
});
```

Replace `<tab>` with `agent`/`plan`/`task` and `<tab>s` with `agents`/`plans`/`tasks` as appropriate.

- [ ] **Step 4: Manual verify**

Click through agents/plans/tasks — observe snappy navigation on second visits.

- [ ] **Step 5: Commit**

```bash
git add ui/flutter_ui/lib/providers/ ui/flutter_ui/lib/features/agents/ ui/flutter_ui/lib/features/plans/ ui/flutter_ui/lib/features/tasks/
git commit -m "feat(cache): detail providers for agents, plans, tasks; first-item prefetch"
```

---

## Phase 7: Grey Transcript Root-Cause + Fix (Gap 6)

### Task 7.1: Reproduce and diagnose

**Files:**
- Test: `ui/flutter_ui/test/features/chat/session_swap_render_test.dart`

- [ ] **Step 1: Write a reproduction test**

The hypothesis: when `activeSessionProvider` changes, `ChatNotifier.loadMessages` clears state to `messages: [], isLoading: true` (`chat_provider.dart:231-235`). `ChatMessageList` then renders `MessagePlaceholder` because `messages.isEmpty` (`chat_message_list.dart:132-133`). During the await window, the user sees a placeholder instead of either (a) a "loading…" indicator that doesn't look like an empty session, or (b) skeleton bubbles.

```dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept/features/chat/chat_message_list.dart';
import 'package:meept/providers/chat_provider.dart';
// imports for mocking SdkClient

void main() {
  testWidgets('session swap shows loading indicator, not empty placeholder',
      (tester) async {
    // Mock setup: SdkClient.getMessages returns session A immediately,
    // returns session B after a 200ms delay.
    final mockClient = _DelayingMockSdkClient(sessionBDelayMs: 200);
    final container = ProviderContainer(overrides: [
      sdkClientProvider.overrideWithValue(mockClient),
    ]);
    addTearDown(container.dispose);

    // Pump ChatMessageList with session A.
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(
          home: Scaffold(body: ChatMessageList(sessionId: 'A')),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.textContaining('A-message'), findsWidgets);

    // Swap to session B.
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(
          home: Scaffold(body: ChatMessageList(sessionId: 'B')),
        ),
      ),
    );
    // Pump just one frame — before B's fetch completes.
    await tester.pump(const Duration(milliseconds: 10));

    // EXPECTATION (after fix): a "loading…" indicator is visible.
    // CURRENT BEHAVIOR (likely): MessagePlaceholder is shown because
    // messages.isEmpty && isLoading is true but the render branch doesn't
    // distinguish "loading empty" from "actually empty".
    expect(find.textContaining('loading'), findsOneWidget);
  });
}
```

- [ ] **Step 2: Run test and capture actual behavior**

Run: `cd ui/flutter_ui && flutter test test/features/chat/session_swap_render_test.dart`

Expected: FAIL. **Capture the actual failure** — what does the UI render during the swap? This is the diagnostic data that determines the fix.

- [ ] **Step 3: Decide fix based on diagnosis**

Three likely outcomes:
- **(a)** `MessagePlaceholder` renders during the loading window because `messages.isEmpty` is checked before `isLoading`. **Fix:** swap the branches — if `isLoading`, show a loading indicator; only show `MessagePlaceholder` if `!isLoading && messages.isEmpty`.
- **(b)** The previous session's messages briefly flash before clearing. **Fix:** add a brief crossfade or ensure the state clear and the loading-indicator render happen in the same frame.
- **(c)** The user-sent messages reappear from optimistic state while assistant messages are absent (matching the "I can see what I sent" symptom). **Fix:** the optimistic-send path shouldn't re-inject messages after a session swap; gate it on `_sessionId` matching the current request.

Document the actual diagnosis in the commit message.

- [ ] **Step 4: Implement the fix**

Apply the fix identified in Step 3. Likely locations:
- `ui/flutter_ui/lib/features/chat/chat_message_list.dart:132-133` (branch order)
- `ui/flutter_ui/lib/providers/chat_provider.dart:231-235` (state shape on swap)

- [ ] **Step 5: Run test to verify it passes**

Run: `cd ui/flutter_ui && flutter test test/features/chat/session_swap_render_test.dart`
Expected: PASS.

- [ ] **Step 6: Run the broader chat test suite**

Run: `cd ui/flutter_ui && flutter test test/features/chat/`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add ui/flutter_ui/lib/features/chat/ ui/flutter_ui/lib/providers/chat_provider.dart ui/flutter_ui/test/features/chat/session_swap_render_test.dart
git commit -m "fix(chat): session-swap loading state (gap 6 root cause: <DIAGNOSIS>)"
```

Replace `<DIAGNOSIS>` with the actual finding from Step 3.

---

## Phase 8: TUI Parity for Archive (Gap 7 TUI portion)

Depends on Phase 1 RPC.

### Task 8.1: Add `Archived` field to TUI Session type

**Files:**
- Modify: `internal/tui/types/types.go:169-186`

- [ ] **Step 1: Add the field**

```go
type Session struct {
	// ... existing fields ...
	Archived bool `json:"archived,omitempty"`
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./internal/tui/...`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/types/types.go
git commit -m "feat(tui-types): add Archived field to Session"
```

---

### Task 8.2: Dim archived sessions in the sessions list

**Files:**
- Modify: `internal/tui/models/sessions.go`

- [ ] **Step 1: Locate the session rendering**

Run: `grep -n "renderItem\|renderSession\|func.*View" internal/tui/models/sessions.go`

Find the function that renders each session row.

- [ ] **Step 2: Apply dim styling for archived sessions**

In the render function, wrap the row's styling:

```go
// Pseudocode — adapt to the actual rendering function:
if session.Archived {
	// Apply dim/muted style. Use the existing styles.Muted or styles.Dim
	// renderer (check internal/tui/styles.go for what's available).
	row = a.styles.Muted.Render(row)
}
```

- [ ] **Step 3: Verify build and manually check**

Run: `go build ./internal/tui/... && ./bin/meept chat` (navigate to sessions view).

- [ ] **Step 4: Commit**

```bash
git add internal/tui/models/sessions.go
git commit -m "feat(tui): dim archived sessions in sessions view"
```

---

### Task 8.3: Wire `d` (archive) and `D` (delete) keys

**Files:**
- Modify: `internal/tui/models/sessions.go` (key handler)
- Modify: `internal/tui/app.go` (~line 717 status message, dispatching)

- [ ] **Step 1: Locate the sessions key handler**

Run: `grep -n "case.*\"n\":\|case.*\"f\":\|case.*\"r\":" internal/tui/models/sessions.go`

Find the key switch in the sessions model's Update.

- [ ] **Step 2: Add `d` key → archive**

In the key handler:

```go
case "d":
	// Archive the selected session.
	selected := m.selectedSession() // adapt to actual selected-session accessor
	if selected == nil {
		return m, nil
	}
	return m, func() tea.Msg {
		// Use the actual RPC client field; this is dispatched from the
		// sessions model, so emit a message that App.Update handles and
		// performs the RPC call there (the model doesn't hold rpc).
		return ArchiveSessionRequestedMsg{SessionID: selected.ID, SessionName: selected.Name}
	}()
```

Define the message type in the same file or in `internal/tui/types/types.go`:

```go
type ArchiveSessionRequestedMsg struct {
	SessionID   string
	SessionName string
}
```

- [ ] **Step 3: Handle the message in App**

In `internal/tui/app.go`, find where similar messages are handled (e.g., where delete or rename messages dispatch the RPC). Add:

```go
case ArchiveSessionRequestedMsg:
	go func() {
		err := a.rpc.Call("sessions.archive", map[string]any{
			"id":       msg.SessionID,
			"archived": true,
		}, nil)
		if err != nil {
			a.statusMessage = fmt.Sprintf("archive failed: %v", err)
		} else {
			a.statusMessage = fmt.Sprintf("archived: %s", msg.SessionName)
		}
		// Trigger a session-list refresh.
		a.refreshSessions()
	}()
	return a, nil
```

Adapt the RPC call signature to match `a.rpc`'s actual interface (look at how `deleteSession` calls RPC at `app.go:1926-1940`).

- [ ] **Step 4: Add `D` (shift+d) → permanent delete**

Find the existing dead `deleteSession` at `app.go:1926-1940` and wire it to `case "D":` in the sessions model. Emit a `DeleteSessionRequestedMsg` and handle it in App by calling the existing delete RPC.

- [ ] **Step 5: Update the status message**

In `internal/tui/app.go:717`, change:

```go
// FROM:
a.statusMessage = "sessions tab (create: n, delete: d)"
// TO:
a.statusMessage = "sessions tab (create: n, archive: d, delete: shift+d)"
```

- [ ] **Step 6: Write tests**

In `internal/tui/models/sessions_test.go` (or wherever session-model tests live), add tests that:
- Pressing `d` emits `ArchiveSessionRequestedMsg`.
- Pressing `D` emits `DeleteSessionRequestedMsg`.

In `internal/tui/app_test.go` (or wherever app tests live), add a test that receiving `ArchiveSessionRequestedMsg` calls `rpc.Call` with `sessions.archive`.

- [ ] **Step 7: Run tests**

Run: `go test ./internal/tui/ -v -race`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/models/sessions.go internal/tui/models/sessions_test.go internal/tui/app.go internal/tui/app_test.go internal/tui/types/types.go
git commit -m "feat(tui): wire d=archive, D=delete; fix dead status message"
```

---

## Phase 9: Documentation

### Task 9.1: Update docs to reflect all changes

**Files:**
- Modify: `docs/workflows/session.md` (archive feature)
- Modify: `docs/workflows/flutter_gui.md` (create if doesn't exist)
- Modify: `docs/reference/http-api.md` (PATCH endpoint)
- Modify: `docs/reference/cli.md` (no CLI changes expected, but verify)

- [ ] **Step 1: Document the archive feature in `docs/workflows/session.md`**

Add a section covering:
- Soft-archive semantics (preserved data, sort-to-bottom, greyed UI).
- API: `PATCH /api/v1/sessions/{id}` with `{"archived": bool}`.
- RPC: `sessions.archive`.
- TUI keys: `d` (archive), `D` (delete permanently).
- Flutter UI: archive icon (default), long-press → "delete permanently".

- [ ] **Step 2: Document the new Flutter surfaces**

Create or update `docs/workflows/flutter_gui.md` with sections for:
- Status bar (what each part means, Ctrl+V cycle).
- Command palette (Cmd/Ctrl+X, items, keyboard navigation).
- Verbosity levels (quiet/normal/verbose, what each shows).
- Agent tile layout.
- Session archive UI.

- [ ] **Step 3: Update `docs/reference/http-api.md`**

Add `PATCH /api/v1/sessions/{id}` to the API reference with request/response schema and examples.

- [ ] **Step 4: Commit**

```bash
git add docs/
git commit -m "docs: archive feature, status bar, palette, verbosity, agent tiles"
```

---

## Self-Review (completed by plan author)

**Spec coverage:**
- Gap 1 (status bar) → Task 3.1, 3.3 ✓
- Gap 1b (verbosity) → Task 2.4, 3.3 ✓
- Gap 2 (palette) → Task 3.2, 3.3 ✓
- Gap 3 (agents) → Task 4.1 ✓
- Gap 4 (double-click) → Task 4.2 ✓
- Gap 5 (cache) → Tasks 2.8, 6.1, 6.2 ✓
- Gap 6 (grey transcript) → Task 7.1 ✓
- Gap 7 (archive) → Tasks 1.1-1.5 (backend), 2.1-2.3 (Flutter model/notifier), 5.1 (Flutter UI), 8.1-8.3 (TUI) ✓

**Placeholder scan:** Some tasks reference helpers (`newTestStore`, `newTestServer`, `newRPCTestHarness`, `sdkClientProvider`) that must be verified at implementation time. Each such reference is annotated with a verification instruction — this is intentional delegation, not a placeholder failure. The plan does not contain "TODO", "TBD", or "fill in details" without guidance.

**Type consistency:** `Archive(id string, archived bool)` is consistent across store interface, SQLite impl, Manager method, HTTP handler (via `*bool` decoded to bool), and RPC handler. `sessionDetailFamily`/`agentDetailFamily`/etc. follow the same pattern. `CommandPalette.defaultItems` labels match the switch in `_handlePaletteSelection`.

**Scope check:** Single plan, 9 phases, ~20 tasks. The work is bounded; no sub-project decomposition needed.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-06-27-flutter-gui-gap-fixes.md`. The user requested subagent execution — proceed with **Subagent-Driven Development** (superpowers:subagent-driven-development). Each phase is a natural dispatch boundary; within phases, tasks can be parallelized where their file sets are disjoint (notably Phase 2 tasks 2.4-2.8 touch disjoint new files).
