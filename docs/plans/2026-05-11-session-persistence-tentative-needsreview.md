# Session Persistence and Branching

> **Status:** Tentative -- Needs Review
> **Created:** 2026-05-11
> **Author:** Planning analysis (not yet approved for implementation)

**Goal:** Bridge the gap between Meept's two disconnected session systems (SQLite store and in-memory ConversationStore), add tree-structured conversation branching, and enable session resumption across daemon restarts.

**Inspired by:** Pi Agent's append-only JSONL tree approach, adapted for Meept's SQLite-first architecture.

---

## 1. Problem Statement

Meept has **two independent session-related storage systems** that do not communicate with each other:

1. **`internal/session/store_sqlite.go`** -- A SQLite-backed `session.Store` that persists session metadata and messages to `sessions.db`. It has `sessions` and `session_messages` tables. Messages are saved here by the TUI/RPC clients after each turn, but the agent loop **never reads from this store**.

2. **`internal/agent/conversation.go`** -- An in-memory `ConversationStore` (map with LRU eviction, max 100 entries) that holds the **live** conversation context the agent loop uses for LLM calls. It manages truncation, anchor messages, memory snapshots, and windowed messages. This store **starts empty** after every daemon restart.

### Specific gaps

| Gap | Impact |
|-----|--------|
| No session resumption | After daemon restart, the agent has no memory of prior conversation. The SQLite store has the messages, but nothing loads them back into the ConversationStore. |
| No conversation branching | Messages are stored as a flat list per session. There is no way to explore alternative responses or fork a conversation from a prior point. |
| No conversation forking | Cannot copy a conversation (or subtree) to a new session for parallel exploration. |
| No branch summarization | When a branch is abandoned, there is no mechanism to summarize it for future context. |
| No tree structure | The `session_messages` table uses simple autoincrement IDs with no parent-child relationships. |

### What works today

- The TUI and RPC clients call `session.messages.save` to persist messages to SQLite after each assistant response.
- The session handler correctly routes create/list/get/delete/attach/detach operations via the message bus.
- The `Conversation` type has sophisticated truncation, importance-based compression, windowed message selection, and anchor message support -- all in memory only.
- The `Summarizer` can generate session names and descriptions from the first message.

---

## 2. Current Architecture

### 2.1 SQLite Session Store (`internal/session/`)

**Files:**
- `internal/session/store.go` -- `Store` interface + `Session`/`Message` types + `MemoryStore` implementation
- `internal/session/store_sqlite.go` -- `SQLiteStore` implementation
- `internal/session/session.go` -- `Handler` for message bus RPC routing
- `internal/session/summarizer.go` -- LLM-based session description generator

**Schema:**
```sql
CREATE TABLE sessions (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    conversation_id TEXT UNIQUE NOT NULL,
    created_at      TEXT NOT NULL,
    last_activity   TEXT NOT NULL,
    attached_clients TEXT DEFAULT '[]',
    worker_ids      TEXT DEFAULT '[]',
    description     TEXT DEFAULT ''
);

CREATE TABLE session_messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL,
    role        TEXT NOT NULL,
    content     TEXT NOT NULL,
    timestamp   TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
```

**Message model** (`session.Message`):
```go
type Message struct {
    ID        int64     `json:"id"`
    SessionID string    `json:"session_id"`
    Role      string    `json:"role"`
    Content   string    `json:"content"`
    Timestamp time.Time `json:"timestamp"`
}
```

**Key observations:**
- Messages are flat (no `parent_id`, no branching).
- The `Message` type is simpler than `llm.ChatMessage` -- it lacks `ToolCalls`, `ToolCallID`, `Name`, `SummaryLevel`, and `Critical` fields.
- `SaveMessages` is a batch-insert operation with no tree semantics.
- The `Store` interface has no method for loading messages back into a `Conversation` object.

### 2.2 In-Memory ConversationStore (`internal/agent/conversation.go`)

**Message model** (`llm.ChatMessage`):
```go
type ChatMessage struct {
    Role         Role       `json:"role"`
    Content      string     `json:"content"`
    Name         string     `json:"name,omitempty"`
    ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
    ToolCallID   string     `json:"tool_call_id,omitempty"`
    SummaryLevel int        `json:"-"`
    Critical     bool       `json:"-"`
}
```

**Key observations:**
- The `Conversation` type stores `[]llm.ChatMessage` in memory with parallel `[]MessageClassification` for importance tracking.
- Supports LRU eviction (max 100 conversations in `ConversationStore`).
- Has `Truncate()`, `TruncateByTokens()`, `TruncateByImportance()`, `CompressByImportance()`, `GetWindowedMessages()` -- all operate on the in-memory slice only.
- Has `Clone()` for deep copying a conversation (used for forking).
- No persistence -- evicted conversations are lost.
- No tree structure -- messages are a flat slice.

### 2.3 Data Flow (Current)

```
User sends message via TUI/RPC/HTTP
    |
    v
TUI calls agent.Run() with conversationID
    |
    v
AgentLoop.conversations.Get(conversationID)  <-- creates NEW empty Conversation
    |
    v
AgentLoop runs: adds messages to Conversation, calls LLM, etc.
    |
    v
TUI calls session.messages.save to persist to SQLite  <-- AFTER the fact, for history display
    |
    v
Daemon restarts
    |
    v
ConversationStore is empty again. SQLite has messages but nobody reads them back.
```

The critical disconnect: **step 5 is a one-way write for the TUI session list UI. The agent loop never reads from SQLite.**

---

## 3. Proposed Architecture

### 3.1 High-Level Design

Bridge the gap by making the SQLite store the **source of truth** and having the `ConversationStore` act as a **hot cache** backed by it.

```
                          +-------------------+
                          |   SQLite Store    |  <-- source of truth
                          | (tree-structured  |
                          |  session_messages)|
                          +--------+----------+
                                   |
                    load on startup / restore on access
                                   |
                          +--------v----------+
                          | ConversationStore |  <-- hot cache
                          | (in-memory, LRU)  |
                          +--------+----------+
                                   |
                          used by AgentLoop for LLM calls
                                   |
                          +--------v----------+
                          |    AgentLoop      |
                          | (adds messages,   |
                          |  calls LLM)       |
                          +--------+----------+
                                   |
                    persist after each turn
                                   |
                          +--------v----------+
                          |   SQLite Store    |
                          +-------------------+
```

### 3.2 Tree-Structured Messages

Add `parent_id` to `session_messages` to enable branching:

```sql
ALTER TABLE session_messages ADD COLUMN parent_id INTEGER REFERENCES session_messages(id);
ALTER TABLE session_messages ADD COLUMN entry_type TEXT DEFAULT 'message';
    -- entry_type: 'message' | 'branch_point' | 'compaction' | 'summary'
ALTER TABLE session_messages ADD COLUMN branch_id TEXT DEFAULT 'main';
    -- branch_id identifies which branch a message belongs to
ALTER TABLE session_messages ADD COLUMN model TEXT DEFAULT '';
    -- model used to generate assistant responses (for reproducibility)
```

**Branch navigation** is tracked by a "leaf pointer" -- the ID of the current tip of the conversation. Each session stores its current leaf pointer:

```sql
ALTER TABLE sessions ADD COLUMN leaf_message_id INTEGER;
    -- current position in the conversation tree
```

### 3.3 Session Resumption

On daemon startup or when a `ConversationStore.Get()` misses:
1. Query SQLite for the session's messages along the path from root to `leaf_message_id`.
2. Reconstruct the `[]llm.ChatMessage` slice from the tree path.
3. Populate the in-memory `Conversation` object.
4. Apply any pending compaction entries.

### 3.4 Branch Navigation

When the user navigates to a prior message (e.g., via `/branch <message-id>` in the TUI):
1. Validate the target message exists in the session's tree.
2. If the current leaf has descendants that are not on the new path, generate a summary of the abandoned branch (see 3.5).
3. Insert a `branch_point` entry at the fork.
4. Update `leaf_message_id` to the target.
5. New messages appended after this point create a new branch.

### 3.5 Branch Summarization

When navigating away from a branch:
1. Collect all messages from the fork point to the current leaf.
2. Call the LLM to generate a concise summary.
3. Insert a `summary` entry as a child of the fork point with the abandoned branch's content.
4. This summary is included when assembling context for the new branch.

### 3.6 Session Forking

Create a new session from a point in an existing conversation:
1. Select a source message ID in the source session.
2. Create a new session in SQLite.
3. Copy all messages from root to the selected message into the new session (re-parenting them).
4. Optionally copy summary entries from abandoned branches at the fork point.
5. Set the new session's `leaf_message_id` to the copied message.
6. Return the new session ID to the client.

### 3.7 Compaction in the Tree

Instead of deleting messages, insert `compaction` entries:
1. When context grows too large, identify messages eligible for compression (same as current `CompressByImportance` logic).
2. Generate a summary of the compressed messages via LLM.
3. Insert a `compaction` entry that replaces the compressed range in context assembly.
4. Original messages remain in the tree but are skipped during context assembly.
5. Context assembly walks the tree and replaces runs of messages with their compaction entries.

### 3.8 Tool Call Serialization Sidecar (Optional Enhancement)

Instead of handling tool call JSON encoding/decoding in the main agent loop, delegate to a dedicated sidecar capability:

```
┌─────────────────┐
│   AgentLoop     │
│ (main conversation) │
└────────┬────────┘
         │ serialize/deserialize tool calls
         ▼
┌─────────────────┐
│ Tooling Agent   │ ◄── sidecar capability
│ (or model)      │
│ - JSON encode   │
│ - JSON decode   │
│ - Schema evol.  │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ session.Message │
│ .tool_calls     │
│ (JSON string)   │
└─────────────────┘
```

**Benefits:**
- Keeps main agent loop focused on conversation flow
- Centralizes tool call serialization logic
- Easier to evolve schema (one place to update)
- Can add validation, logging, metrics in one place

**Implementation options:**
1. **Dedicated agent ID** (`tooling`): Register a minimal agent that only handles serialization
2. **Model capability**: Use a fast/cheap model for serialization tasks
3. **Service layer**: Extract to `internal/tools/serialization.go` (simplest, no agent overhead)

**Recommendation:** Start with option 3 (service layer). Promote to agent/sidecar only if the logic grows complex or multiple callers emerge.

**Configuration** (`tooling` section in `meept.json5`):
```json5
tooling: {
  enabled: false,              // Enable sidecar (default: false)
  mode: "service",             // "service" (in-process) or "agent" (sidecar)
  agent_id: "tooling",         // Agent ID when mode is "agent"
  model: "",                   // Model override (empty = default)
  cache_enabled: true,         // Cache serialized tool calls
  cache_max_size: 500,         // Max cache entries
  cache_ttl_minutes: 60,       // Cache TTL
  include_schema: true,        // Include JSON schema in metadata
  validate_on_serialize: false,// Validate against schema
  log_unknown_tools: true,     // Log warnings for unknown tools
}
```

### 3.9 Enhanced Message Model

Extend `session.Message` to carry the full `llm.ChatMessage` data:

```go
type Message struct {
    ID         int64     `json:"id"`
    SessionID  string    `json:"session_id"`
    ParentID   int64     `json:"parent_id,omitempty"`
    Role       string    `json:"role"`
    Content    string    `json:"content"`
    Timestamp  time.Time `json:"timestamp"`
    EntryType  string    `json:"entry_type"`  // "message", "branch_point", "compaction", "summary"
    BranchID   string    `json:"branch_id"`   // "main" or a branch identifier
    Model      string    `json:"model,omitempty"`
    // Fields from llm.ChatMessage that were previously lost
    Name       string    `json:"name,omitempty"`
    ToolCalls  string    `json:"tool_calls,omitempty"` // JSON-encoded
    ToolCallID string    `json:"tool_call_id,omitempty"`
}
```

---

## 4. Pros/Cons Analysis

### 4.1 SQLite vs. JSONL (Pi Agent approach)

| Aspect | Pi Agent (JSONL) | Meept (SQLite) | Verdict |
|--------|-----------------|----------------|---------|
| **Human readability** | Excellent -- each line is a JSON object, viewable in any text editor | Poor -- binary format, requires SQL queries | Pi wins for debugging |
| **Append performance** | Excellent -- just append a line to a file | Good -- INSERT with WAL mode is fast, but has more overhead | Pi wins slightly |
| **Queryability** | Poor -- must scan entire file, no indexing | Excellent -- indexed queries, JOINs, aggregates, pagination | Meept wins significantly |
| **Transactions** | None -- write is atomic per line but multi-entry operations need external coordination | Full ACID transactions, foreign key enforcement | Meept wins significantly |
| **Tree queries** | Must reconstruct tree in memory from all entries | Can use recursive CTEs to walk branches, find ancestors/descendants | Meept wins significantly |
| **Concurrent access** | Must use file locks | Built-in connection pooling, WAL mode allows concurrent reads | Meept wins significantly |
| **Existing infrastructure** | Would need to build from scratch | Already exists, already integrated with session handler, message bus, TUI, HTTP API | Meept wins -- no migration needed |
| **Corruption recovery** | Truncated last line is the worst case | Journal/WAL provides recovery | Roughly equivalent |
| **Storage efficiency** | Redundant -- each entry stores full fields including parent references | Normalized -- can reference by ID, optional fields stored compactly | Meept wins slightly |
| **Portability** | Excellent -- single file, no dependencies | Good -- single file, but requires SQLite library | Roughly equivalent |

**Decision: Stick with SQLite.** The queryability, transaction support, concurrent access, and existing infrastructure far outweigh the readability advantage of JSONL. The tree structure can be modeled with `parent_id` columns and recursive CTEs. Debugging readability can be addressed with a `SELECT` + formatting tool.

### 4.2 Pure In-Memory Tree vs. Hybrid (SQLite + In-Memory Cache)

| Aspect | Pi Agent (in-memory tree from JSONL) | Meept (hybrid) | Verdict |
|--------|-------------------------------------|-----------------|---------|
| **Startup cost** | Must parse entire JSONL file to rebuild tree | Can lazy-load only the active branch from SQLite | Meept wins for large sessions |
| **Memory usage** | Entire tree in memory for active sessions | Only the active branch path in memory | Meept wins for long conversations with many branches |
| **Complexity** | Simpler -- one data structure | More complex -- cache invalidation, persistence coordination | Pi wins |
| **Persistence** | Must flush to JSONL after every mutation | Automatic -- SQLite is the source of truth | Meept wins |
| **Crash safety** | Risk of losing unflushed entries | SQLite WAL ensures durability | Meept wins |
| **Multi-client** | Single-writer model works for CLI | Multiple TUI/HTTP clients can read simultaneously | Meept wins for daemon architecture |

**Decision: Use hybrid approach.** The ConversationStore remains the hot cache for the agent loop's performance-critical path. SQLite is the source of truth for persistence, resumption, and tree queries.

### 4.3 Per-Capability Tradeoffs

#### Tree Structure

| | Pi Agent | Meept (proposed) |
|--|----------|------------------|
| Implementation | Each JSONL entry has `id` + `parentId` | `parent_id` column in `session_messages` |
| Tree reconstruction | Parse entire file, build map of id -> entry | Recursive CTE or lazy path walking |
| Branch creation | Natural -- just set `parentId` to any prior entry | Same -- set `parent_id` to target message |
| Risk | No referential integrity -- orphaned entries possible | Foreign key constraint prevents orphans |

#### Branch Navigation

| | Pi Agent | Meept (proposed) |
|--|----------|------------------|
| Leaf pointer | Tracked in session metadata (in memory) | `leaf_message_id` column in `sessions` table |
| Navigation | Move leaf pointer, continue appending | Same approach, persisted to SQLite |
| Context assembly | Walk from root to leaf | Walk from root to leaf via parent chain |
| Risk | Must rebuild tree on startup | Can query path directly from SQLite |

#### Branch Summarization

| | Pi Agent | Meept (proposed) |
|--|----------|------------------|
| Trigger | On branch navigation away from current leaf | Same |
| Storage | Summary entry in JSONL tree | `summary` entry_type in session_messages |
| Context inclusion | Included when assembling context for sibling branches | Same approach |
| Risk | LLM call can fail, need fallback | Same risk, can use existing `Summarizer` fallback |

#### Compaction

| | Pi Agent | Meept (proposed) |
|--|----------|------------------|
| Approach | Compaction entries replace message ranges in context assembly | Same approach |
| Storage | Compaction entry in JSONL tree | `compaction` entry_type in session_messages |
| Context assembly | Walk tree, replace compressed ranges with compaction | Same |
| Integration | Built into context assembly from the start | Must integrate with existing `GetWindowedMessages()` / `CompressByImportance()` |

#### Session Forking

| | Pi Agent | Meept (proposed) |
|--|----------|------------------|
| Approach | Copy entries to new JSONL file with new session ID | Copy rows to new session_id in SQLite |
| Atomicity | File copy operation | Transaction wrapping the copy |
| Sharing | Each fork has its own file | Shared database, isolated by session_id |
| Risk | Must handle concurrent reads during copy | SQLite handles concurrency |

---

## 5. Implementation Phases

### Phase 1: Schema Migration and Message Model Enhancement

**Goal:** Extend the SQLite schema and message model to support tree structure without breaking existing functionality.

**Files to modify:**
- `internal/session/store.go` -- Add `ParentID`, `EntryType`, `BranchID`, `Model`, `Name`, `ToolCalls`, `ToolCallID` fields to `Message`. Update `Store` interface with new methods.
- `internal/session/store_sqlite.go` -- Add migration to add `parent_id`, `entry_type`, `branch_id`, `model`, `name`, `tool_calls`, `tool_call_id` columns. Update `SaveMessages` to accept new fields. Update `GetMessages` to return them. Add `GetMessagePath(sessionID, leafID)` method for tree path walking.
- `internal/session/session.go` -- Update `MemoryStore` to match new interface.

**New files:**
- `internal/session/tree.go` -- `TreeWalker` type that assembles `[]llm.ChatMessage` from a tree path, handling `compaction` and `summary` entries.

**Tests:**
- `internal/session/store_sqlite_test.go` -- Test migration from existing schema, test tree path queries, test new fields round-trip.

**Estimated effort:** Medium. Mostly additive changes. The migration must handle existing databases (add columns with defaults).

### Phase 2: Session Resumption (Bridge the Gap)

**Goal:** On daemon startup or conversation access, restore from SQLite into ConversationStore.

**Files to modify:**
- `internal/agent/conversation.go` -- Add `RestoreFromMessages([]llm.ChatMessage)` method to `Conversation`. Add `ConversationOption` for `ConversationStore` to accept a persistence callback.
- `internal/agent/loop.go` -- Modify `AgentLoop` to accept a `session.Store` reference. On `conversations.Get(id)`, if the conversation is not in memory, attempt to restore from SQLite before creating an empty one. After each turn, persist messages to SQLite.
- `internal/session/tree.go` -- Implement `AssembleBranch(sessionID, leafID) []llm.ChatMessage` that walks the tree path and produces the message slice.

**Key design decisions:**
- **Eager vs. lazy restore:** Restore only the most recent session eagerly on startup. Restore others lazily on first access.
- **Partial restore:** For very long conversations, restore only the last N messages plus compaction entries, not the full history.
- **Message format conversion:** Need a `session.Message` -> `llm.ChatMessage` converter that handles the new fields (tool_calls JSON decode, etc.).

**Tests:**
- `internal/agent/conversation_test.go` -- Test `RestoreFromMessages` with various message types including tool calls.
- Integration test: Start agent loop, send messages, create new loop, verify conversation is restored.

**Estimated effort:** High. This is the most critical phase and requires careful coordination between the agent loop and session store.

### Phase 3: Branch Navigation

**Goal:** Allow moving the conversation leaf pointer to any prior message, creating a new branch.

**Files to modify:**
- `internal/session/store.go` -- Add `NavigateToBranch(sessionID, targetMessageID)` method to `Store` interface. Add `GetBranches(sessionID)` for listing branches. Add `GetCurrentLeaf(sessionID)` for getting the current position.
- `internal/session/store_sqlite.go` -- Implement `NavigateToBranch`: validate target exists, update `leaf_message_id`, return old leaf for summary generation.
- `internal/session/session.go` -- Add bus topic handlers for `session.branch.navigate`, `session.branches.list`.
- `internal/agent/conversation.go` -- Add `SetBranchPoint(messageID)` method that truncates in-memory messages to the target and marks the conversation as branched.
- `internal/agent/loop.go` -- Handle branch navigation requests from the message bus.

**New files:**
- `internal/session/branch.go` -- `BranchManager` type that orchestrates navigation, summarization, and tree updates.

**Tests:**
- `internal/session/branch_test.go` -- Test navigation, branch creation, leaf pointer updates.
- Test that navigating to a prior message correctly creates a fork point.

**Estimated effort:** Medium-High. The tree navigation logic is straightforward, but coordinating with the in-memory ConversationStore requires care.

### Phase 4: Branch Summarization

**Goal:** When navigating away from a branch, generate an LLM summary of the abandoned branch.

**Files to modify:**
- `internal/session/summarizer.go` -- Add `SummarizeBranch(ctx, messages) (string, error)` method that summarizes a sequence of messages.
- `internal/session/branch.go` -- On navigation, if abandoning a branch with more than N messages, call `SummarizeBranch` and insert a `summary` entry.
- `internal/session/tree.go` -- Update `AssembleBranch` to include summary entries from sibling branches at fork points.

**Key design decisions:**
- **Minimum branch length for summarization:** Skip summarization for branches with fewer than 3 messages.
- **Summary placement:** Insert as a child of the fork point, tagged with `entry_type = 'summary'` and the abandoned branch ID.
- **Context inclusion:** When assembling context for a branch, include summaries of sibling branches at fork points.

**Estimated effort:** Medium. The summarization infrastructure already exists; this extends it to branch context.

### Phase 5: Session Forking

**Goal:** Copy a conversation (or subtree) to a new session.

**Files to modify:**
- `internal/session/store.go` -- Add `ForkSession(sourceSessionID, fromMessageID, newName) (*Session, error)` to `Store` interface.
- `internal/session/store_sqlite.go` -- Implement `ForkSession` as a transaction that copies messages from root to `fromMessageID`, creates new session, and re-parents.
- `internal/session/session.go` -- Add bus topic handler for `session.fork`.
- `internal/services/session_service.go` -- Add `ForkSession` service method.

**Tests:**
- `internal/session/store_sqlite_test.go` -- Test forking with various tree shapes.

**Estimated effort:** Low-Medium. Straightforward SQL copy operation wrapped in a transaction.

### Phase 6a: Compaction Entry Emission

**Goal:** Modify compaction logic to emit tree-based compaction entries instead of deleting messages.

**Files to modify:**
- `internal/agent/conversation.go` -- Modify `CompressByImportance` and `TruncateByTokens` to emit compaction entries instead of deleting messages. Add `GetCompactionEntries() []CompactionEntry`.
- `internal/session/store_sqlite.go` -- Add `InsertCompaction(sessionID, parentID, summary string, compressedIDs []int64)` method.

**Key design decisions:**
- **When to compact:** Trigger compaction when `GetWindowedMessages` would drop more than 30% of messages.
- **Compaction granularity:** Compact in chunks rather than all-at-once, to allow fine-grained context reconstruction.
- **Backward compatibility:** Existing truncated conversations (without compaction entries) continue to work normally.

**Estimated effort:** Medium. Additive changes only — no context assembly changes yet.

### Phase 6b: Compaction Integration with Context Assembly

**Goal:** Update context assembly to skip compacted messages and use compaction entries instead.

**Files to modify:**
- `internal/session/tree.go` -- Update `AssembleBranch` to skip messages that have been compacted and substitute compaction entries.
- `internal/agent/conversation.go` -- Integrate tree-based compaction with `GetWindowedMessages()`.

**Key design decisions:**
- **Context assembly order:** Walk tree, detect compaction entry ranges, substitute summary for compressed messages.
- **Testing:** Extensive tests for various tree shapes and compaction patterns.

**Estimated effort:** High. Requires careful integration with the existing context assembly pipeline. Splitting from 6a allows validating compaction entry emission before integrating with context assembly.

### Phase 7: CLI and TUI Integration

**Goal:** Expose branching and forking to the user via CLI commands and TUI keybindings.

**Files to modify:**
- `cmd/meept/` -- Add `branch` subcommand: `meept branch list`, `meept branch navigate <id>`, `meept branch summary`.
- `internal/tui/` -- Add keybinding for branch navigation (e.g., `ctrl+b` to open branch selector). Add branch indicator in the status bar.
- `internal/tui/rpc.go` -- Add `NavigateBranch`, `ListBranches`, `ForkSession` RPC client methods.
- `internal/comm/http/api_handlers.go` -- Add HTTP endpoints for branching operations.

**Estimated effort:** Medium. Mostly UI work building on the backend from earlier phases.

---

## 6. Configuration

Add a new `Session` section to the config schema:

```json5
{
  session: {
    // Enable session persistence (restore from SQLite on startup)
    persistence: true,

    // Enable conversation branching
    branching: true,

    // Maximum number of branches per session (0 = unlimited)
    max_branches: 20,

    // Auto-summarize abandoned branches longer than this many messages
    branch_summary_threshold: 5,

    // Maximum messages to restore on session resumption (0 = all)
    restore_message_limit: 0,

    // Enable compaction entries (instead of deleting messages)
    compaction: true,

    // Minimum messages before compaction is considered
    compaction_threshold: 50,

    // Target compression ratio for compaction (0.0-1.0)
    compaction_target_ratio: 0.6,

    // Auto-fork behavior: "never" | "ask" | "always"
    auto_fork: "ask",
  }
}
```

**Files to modify:**
- `internal/config/schema.go` -- Add `SessionConfig` struct and field to `Config`.

**Config template:**
- `config/meept.json5` -- Add commented-out `session` section with defaults.

---

## 7. API Changes

### 7.1 New Bus Topics (RPC)

| Topic | Direction | Payload | Response |
|-------|-----------|---------|----------|
| `session.resume` | Request | `{session_id}` | `{messages, leaf_id, restored_count}` |
| `session.branch.navigate` | Request | `{session_id, target_message_id}` | `{new_leaf_id, summary, branch_id}` |
| `session.branches.list` | Request | `{session_id}` | `{branches: [{id, leaf_id, message_count, summary}]}` |
| `session.fork` | Request | `{session_id, from_message_id, name}` | `{new_session_id, copied_count}` |
| `session.tree.get` | Request | `{session_id}` | `{nodes: [{id, parent_id, role, entry_type, branch_id}]}` |
| `session.compact` | Request | `{session_id, target_ratio}` | `{compaction_id, compressed_count, saved_tokens}` |

### 7.2 New HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/sessions/{id}/resume` | Restore session into active memory |
| `POST` | `/api/v1/sessions/{id}/branch` | Navigate to a branch point |
| `GET` | `/api/v1/sessions/{id}/branches` | List all branches |
| `POST` | `/api/v1/sessions/{id}/fork` | Fork session from a message |
| `GET` | `/api/v1/sessions/{id}/tree` | Get tree structure |
| `POST` | `/api/v1/sessions/{id}/compact` | Trigger compaction |

### 7.3 Store Interface Additions

```go
type Store interface {
    // ... existing methods ...

    // Tree operations
    GetLeafMessageID(sessionID string) (int64, error)
    SetLeafMessageID(sessionID string, messageID int64) error
    GetMessagePath(sessionID string, leafID int64) ([]Message, error)
    GetMessageBranches(sessionID string) ([]Branch, error)
    GetTree(sessionID string) ([]TreeNode, error)

    // Branch operations
    NavigateToBranch(sessionID string, targetMessageID int64) (oldLeaf int64, err error)
    ForkSession(sourceSessionID string, fromMessageID int64, newName string) (*Session, error)

    // Compaction
    InsertCompaction(sessionID string, parentID int64, summary string, compressedIDs []int64) (int64, error)
}
```

### 7.4 New Types

```go
type Branch struct {
    ID           string `json:"id"`
    LeafID       int64  `json:"leaf_id"`
    MessageCount int    `json:"message_count"`
    Summary      string `json:"summary,omitempty"`
}

type TreeNode struct {
    ID         int64  `json:"id"`
    ParentID   int64  `json:"parent_id"`
    Role       string `json:"role"`
    EntryType  string `json:"entry_type"`
    BranchID   string `json:"branch_id"`
    Content    string `json:"content,omitempty"`  // Truncated for tree view
    Timestamp  string `json:"timestamp"`
    IsLeaf     bool   `json:"is_leaf"`
}
```

---

## 8. Open Questions and Tradeoffs

### Questions for Review

1. **Backward compatibility with existing sessions.db:** The migration adds nullable columns with defaults, so existing data should work. But should we backfill `parent_id` for existing messages? (Proposed: **yes, backfill during migration** — set `parent_id` to the previous message's ID based on insertion order. This is a one-time cost that simplifies all downstream tree-walking logic and removes ambiguity for existing messages.)

2. **Compaction vs. deletion for existing behavior:** Should compaction be opt-in (requires `session.compaction: true`) or should it replace the current truncation behavior entirely? (Proposed: **opt-in initially**, with a migration path to make it default after proven in production.)

3. **Branch visibility in TUI:** How should branches be presented to the user? A tree view is complex for a terminal UI. Options: (a) linear list with branch indicators, (b) simple selector dialog showing branch summaries, (c) full tree visualization using bubbletea. (Proposed: **indented tree-like list with status metadata** — shows branch name/message count/summary status with visual hierarchy. Example:
   ```
   main (current) ────────────────┐
     └─ branch-1 (3 msgs) ────────┤
     └─ branch-2 (7 msgs, summed) ┘
   ```)

4. **Branch summarization cost:** Each branch navigation could trigger an LLM call for summarization. Should this be async? Should there be a rate limit? (Proposed: **hybrid approach** — synchronous for branches under 10 messages, async beyond that. Most branches will be short, and users prefer waiting 2 seconds over a blocking spinner. Async path uses a queue with persistence for daemon restart mid-summarization.)

5. **ConversationStore LRU interaction with branches:** If a user forks a session, the fork is a new ConversationStore entry. Should branches within a session count toward the LRU limit separately or together? (Proposed: **per-session counting** — each session counts as one entry regardless of branches; only the active branch is loaded into memory.)

6. **Multi-agent branching:** When multiple agents share a session (via worker IDs), branching could cause confusion. Should branching be restricted to single-agent sessions? (Proposed: **warn but allow** — multi-agent sessions are rare; branching is a power feature. Log a warning if branching while other workers are attached.)

7. **Tool call persistence:** Currently `session.Message` does not store tool calls. The proposed enhancement adds `tool_calls` as a JSON string. Is this sufficient, or should tool calls be normalized into a separate table? (Proposed: **JSON string first, with sidecar agent option** — start with JSON encoding for simplicity. As an optional enhancement, a dedicated "tooling" agent/sidecar can handle tool call serialization/deserialization as a capability, keeping the main agent loop focused. Normalize to a separate table only if query patterns emerge, e.g., "find all tool calls of type X".)

### Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Schema migration breaks existing databases | Low | High | Additive columns with defaults; backfill `parent_id` in migration; test on real databases |
| Backfill migration is slow on large datasets | Medium | Low | Run backfill in batches; show progress indicator; make it resumable |
| Performance regression from SQLite queries on every message | Medium | Medium | Cache tree paths in ConversationStore; only query SQLite on miss or startup |
| LLM summarization failures leave branches without summaries | Medium | Low | Fallback to simple extraction (already implemented in Summarizer); sync for short branches |
| Tree complexity causes bugs in context assembly | Medium | High | Extensive tests for tree shapes; split compaction into two phases; keep flat path as primary context source |
| ConversationStore memory usage increases with tree metadata | Low | Low | Tree metadata is small; only the active branch path is in memory |
| Tool call JSON encoding becomes a bottleneck | Low | Low | Profile early; extract to sidecar agent if needed; add normalization migration if query patterns emerge |

### What This Plan Does NOT Cover

- **Collaborative branching** (multiple users editing the same tree simultaneously) -- out of scope for Meept's single-user daemon model.
- **Branch merging** -- branches diverge permanently; merging is a future consideration.
- **Branch export/import** -- could be added later as a separate feature.
- **Branch-based diff/compare** -- interesting but not essential for initial implementation.
- **Undo/redo** -- while related to branching, this is a separate UX concern.

---

## 9. Summary of Files to Create/Modify

### New Files

| File | Purpose |
|------|---------|
| `internal/session/tree.go` | Tree walker, context assembly from tree paths |
| `internal/session/branch.go` | Branch navigation, forking, summarization orchestration |
| `internal/session/tree_test.go` | Tests for tree walking, branch operations |
| `internal/session/branch_test.go` | Tests for branch navigation, forking |
| `internal/tools/serialization.go` _(optional)_ | Tool call JSON serialization/deserialization service |

### Modified Files

| File | Changes |
|------|---------|
| `internal/session/store.go` | Extended `Message` type, new `Store` interface methods |
| `internal/session/store_sqlite.go` | Schema migration, tree query methods, compaction support |
| `internal/session/session.go` | New bus topic handlers, updated MemoryStore |
| `internal/session/summarizer.go` | `SummarizeBranch` method |
| `internal/agent/conversation.go` | `RestoreFromMessages`, branch point support, compaction entries |
| `internal/agent/loop.go` | Accept session store, restore on access, persist after turns |
| `internal/agent/conversation_test.go` | Tests for restore, branch points |
| `internal/config/schema.go` | `SessionConfig` struct, `ToolingConfig` struct |
| `internal/services/session_service.go` | Fork, branch navigation service methods |
| `internal/comm/http/api_handlers.go` | New HTTP endpoints |
| `internal/tui/rpc.go` | New RPC client methods |
| `config/meept.json5` | Session config section, Tooling config section |

---

## 10. Recommended Implementation Order

1. **Phase 1** (schema + message model) -- Foundation, no behavior change
2. **Phase 2** (session resumption) -- Highest user value, bridges the gap
3. **Phase 4** (branch summarization) -- Needed before Phase 3 for abandoned branch handling
4. **Phase 3** (branch navigation) -- Depends on Phase 1 and 4
5. **Phase 5** (session forking) -- Depends on Phase 3
6. **Phase 6a** (compaction entry emission) -- Independent, can be done in parallel with 3-5
7. **Phase 6b** (compaction integration with context assembly) -- Depends on 6a
8. **Phase 7** (CLI/TUI) -- Depends on all backend phases

Phases 1-2 are the **minimum viable improvement** that solve the core problem (data loss on restart). Phases 3-5 add branching. Phases 6a-6b add sophisticated compaction (split into two phases for reduced risk). Phase 7 makes it user-facing.

**Critical integration test checkpoint:** After Phase 2, add end-to-end tests for session resumption across daemon restarts. This is the foundation for all other features.
