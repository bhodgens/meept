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

---

*Updated with Global Semantic Search spec.*
