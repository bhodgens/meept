# Session

## Overview

Session persistence and conversation tree management (`internal/session/`). Sessions are the top-level container for chat history, branching, tool calls, and embeddings.

## Problem

Multi-client sessions need a unified store supporting conversation trees (branches), compaction, tool-call associations, and content search across both keyword and semantic modes.

## Behavior

- **Store interface** (`store.go`): SQLite and in-memory implementations. Methods cover CRUD, tree operations (leaf message, branches, navigation, fork), tool call persistence, project association, and search.
- **Search**:
  - `SearchMessages(ctx, query, limit)`: FTS5 keyword search with bm25 ranking and snippet generation.
  - `SearchMessagesSemantic(ctx, embedding, limit)`: vec0 KNN query. Returns `ErrSemanticUnavailable` when no embedding index is configured.
  - `StoreEmbedding(ctx, messageID, embedding)`: persists an embedding for a message.
  - `UnembeddedMessages(ctx, limit)`: returns messages without embeddings, for the background worker to process.
- **Embedding worker** (`embedding_worker.go`): batch-embeds unembedded messages using the configured `EmbeddingProvider` (sourced from the memory manager). Default: 20 messages per 60s tick.
- **MessageSearchResult**: unified result type with `MessageID`, `SessionID`, `Role`, `Content`, `Snippet`, `Relevance`, `Timestamp`.

## Configuration

- SQLite store path: `~/.meept/meept.db` (default).
- FTS5 virtual table `session_messages_fts` with `porter unicode61` tokenizer, synced via AFTER INSERT/DELETE/UPDATE triggers.
- vec0 virtual table `session_message_vectors` (768-dim default), populated by the background worker.

## Edge Cases

- vec0 extension missing: migration is non-fatal; semantic search returns `ErrSemanticUnavailable`. Keyword FTS search still works.
- FTS backfill: existing rows are batched into the FTS table (500 rows per batch) on migration.
- MemoryStore (test/ephemeral): semantic methods return `ErrSemanticUnavailable`; keyword search uses substring matching.

## Archive

Sessions can be soft-archived without deleting the underlying messages, branches, or embeddings. Archived sessions stay queryable (FTS5 keyword and vec0 semantic search continue to return hits from archived sessions), but are visually de-emphasized in client UIs.

### Semantics

- The `sessions.archived` SQLite column is a boolean (default `0`).
- `SessionStore.Archive(sessionID string, archived bool) error` flips the flag; no row data is removed. (Note: the store interface takes no `context.Context` — see `internal/session/store.go:91`.)
- Archived sessions still appear in `GET /api/v1/sessions` and the `sessions.list` RPC. Consumers can read the `archived` field and sort/style accordingly.
- Hard delete (`DELETE /api/v1/sessions/{id}`) works on archived and un-archived sessions alike and removes all associated data.

### API

**HTTP:** `PATCH /api/v1/sessions/{id}`

```bash
curl -X PATCH http://localhost:8081/api/v1/sessions/sess-123 \
  -H "Content-Type: application/json" \
  -d '{"archived": true}'
```

The handler is strict: `archived` is a required `*bool` field, the decoder uses `DisallowUnknownFields()`, and omitting `archived` returns `400 Bad Request` with body `{"error":"\"archived\" field is required"}`. On success the response is `204 No Content` with an empty body (no session JSON returned — re-fetch via `GET /api/v1/sessions/{id}` if you need the updated record).

**RPC:** `sessions.archive` with params `{"id": "<id>", "archived": <bool>}`. Returns `{"status": "archived"|"unarchived", "id": "<id>"}`.

### TUI keys

In the sessions view (`internal/tui/models/sessions.go`):

| key | action |
|-----|--------|
| `d` | toggle soft-archive on the selected session (async RPC; status updates after the round-trip) |
| `D` (shift+d) | permanent delete (async RPC; confirms first) |

Archived rows render with a dim-gray per-cell style and an `(archived)` prefix on the title. `sortSessions` uses `sort.SliceStable` so archived sessions sink to the bottom within each designation group — they are not hidden.

Status bar hint: `sessions tab (create: n, archive: d, delete: shift+d)`.

### Flutter UI

The Flutter sessions list (`ui/flutter_ui/lib/features/sessions/sessions_list.dart`) mirrors the TUI semantics with pointer-driven affordances:

- Default tap target uses `Icons.archive_outlined` — a single tap toggles archive.
- Archived tiles wrap in `Opacity(opacity: archived ? 0.5 : 1.0)` to grey them out.
- Long-press opens a context menu (`_showContextMenu`) offering "delete permanently" (hard delete via `DELETE /api/v1/sessions/{id}`).
- `onDoubleTap` on a session tile activates chat (sets `tabActivationProvider = HomeTab.chat` and navigates to `/`), matching the TUI `enter` behavior.

The `Session.archived` field on `ui/flutter_ui/lib/models/api_models.dart` is parsed from the API response's `archived` boolean.

---

*Updated with Global Semantic Search spec and soft-archive feature.*
