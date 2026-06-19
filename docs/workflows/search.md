# Search

## Overview

Cross-scope search over Meept data — session messages, memories, tasks, and plans. Provides both keyword (FTS5) and semantic (vector similarity) retrieval.

## Problem

Without unified search, users must navigate each scope separately. Semantic search enables finding conceptually related content without exact keyword matches.

## Behavior

- **Keyword search** (`POST /api/v1/search`): FTS5 over sessions/tasks/memories/plans with bm25 ranking.
- **Semantic search** (`POST /api/v1/search/semantic`): Vector similarity over session messages (via sqlite-vec) and memories (via HNSW), with keyword fallback for tasks/plans. Falls back to keyword-only mode when no embedding provider is configured.
- **Embedding worker**: Background goroutine embeds unembedded session messages in batches (default 20 per 60s tick) using the memory manager's embedding provider.
- **RPC**: `search.semantic` and `search.keyword` methods for TUI access.
- **TUI**: Press `f` from the sessions panel to open the search view; debounced 250ms queries, scope cycling via `tab`, `enter` navigates to result.
- **Flutter**: `SearchPanel` at `/tools/search` with semantic toggle (default on), mode indicator, and per-result relevance. Press `f` from the sessions tab.

## Configuration

No new config keys — the embedding worker reuses the memory manager's embedding provider. If semantic memory search is configured, semantic session search works automatically.

## Edge Cases

- No embedding provider: `SearchSemantic` returns `mode: "keyword"`; worker does not start.
- sqlite-vec extension unavailable: `SearchMessagesSemantic` returns `ErrSemanticUnavailable`; FTS keyword search still works.
- Result ID format for messages: `"sessionID:msgID"` (e.g., `"abc123:42"`).

---

*Added with Global Semantic Search spec (`docs/superpowers/specs/2026-06-18-global-semantic-search-design.md`).*
