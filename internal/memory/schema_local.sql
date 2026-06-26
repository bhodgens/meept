-- Schema for local.db: data owned by this node (unique data, backed up).
-- Unified memories table for dual-store writes, backed by sync_metadata.

-- Unified memories table used by DualStore for writes from this node.
CREATE TABLE IF NOT EXISTS memories (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL DEFAULT 'episodic',
    category   TEXT NOT NULL DEFAULT 'conversation',
    content    TEXT NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT,
    agent_id   TEXT,
    session_id TEXT,
    bot_id     TEXT
);

-- sync_metadata tracks migration state and sync pointers.
CREATE TABLE IF NOT EXISTS sync_metadata (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Indexes for efficient filtered reads.
CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type);
CREATE INDEX IF NOT EXISTS idx_memories_category ON memories(category);
CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at);
CREATE INDEX IF NOT EXISTS idx_memories_session ON memories(session_id);
