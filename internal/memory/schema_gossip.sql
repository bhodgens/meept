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
