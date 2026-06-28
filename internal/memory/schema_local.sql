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

-- Sessions and turns mirror the legacy sessions.db schema so that
-- local.db can be the single backed-up home for this node's sessions.
-- The sessions table intentionally mirrors the columns written by
-- internal/session/store_sqlite.go so that a migrated local.db can be
-- ATTACHed by the session store without column-mismatch errors.
CREATE TABLE IF NOT EXISTS sessions (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL DEFAULT '',
    conversation_id   TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL,
    last_activity     TEXT NOT NULL,
    attached_clients  TEXT NOT NULL DEFAULT '[]',
    worker_ids        TEXT NOT NULL DEFAULT '[]',
    description       TEXT NOT NULL DEFAULT '',
    leaf_message_id   INTEGER,
    project_id        TEXT NOT NULL DEFAULT '',
    project_path      TEXT NOT NULL DEFAULT '',
    no_fence          INTEGER NOT NULL DEFAULT 0,
    metadata_json     TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS turns (
    turn_id      TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL,
    role         TEXT NOT NULL,
    content      TEXT NOT NULL,
    timestamp    INTEGER NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}'
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
CREATE INDEX IF NOT EXISTS idx_turns_session ON turns(session_id);
CREATE INDEX IF NOT EXISTS idx_turns_ts ON turns(timestamp);
CREATE INDEX IF NOT EXISTS idx_sessions_conv ON sessions(conversation_id);
CREATE INDEX IF NOT EXISTS idx_sessions_activity ON sessions(last_activity);
