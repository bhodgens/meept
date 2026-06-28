-- Schema for sync-gossip.db: replicated data from peers.
-- Mirrors local.db schema with an added source_node column so every row
-- can be traced back to the originating node.

-- Unified memories table with source attribution.
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
    bot_id     TEXT,
    source_node TEXT NOT NULL
);

-- Sessions and turns replicated from peers. Every row carries the
-- originating node's ID in source_node so merged reads can attribute
-- rows correctly and so conflict resolution has the metadata it needs.
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
    metadata_json     TEXT NOT NULL DEFAULT '{}',
    source_node       TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS turns (
    turn_id      TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL,
    role         TEXT NOT NULL,
    content      TEXT NOT NULL,
    timestamp    INTEGER NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    source_node  TEXT NOT NULL
);

-- sync_metadata for gossip-node bookkeeping.
CREATE TABLE IF NOT EXISTS sync_metadata (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Indexes for efficient merge queries.
CREATE INDEX IF NOT EXISTS idx_gossip_memories_type ON memories(type);
CREATE INDEX IF NOT EXISTS idx_gossip_memories_source ON memories(source_node);
CREATE INDEX IF NOT EXISTS idx_gossip_memories_created ON memories(created_at);
CREATE INDEX IF NOT EXISTS idx_gossip_memories_session ON memories(session_id);
CREATE INDEX IF NOT EXISTS idx_gossip_sessions_source ON sessions(source_node);
CREATE INDEX IF NOT EXISTS idx_gossip_turns_session ON turns(session_id);
CREATE INDEX IF NOT EXISTS idx_gossip_turns_source ON turns(source_node);
CREATE INDEX IF NOT EXISTS idx_gossip_turns_ts ON turns(timestamp);
