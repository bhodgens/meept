# Token Caching

Token caching reduces LLM API costs and latency by caching prompt→completion pairs transparently across all agents and providers.

## Overview

Meept's token cache sits in the `internal/llm/` layer, below the agent layer, making it:

- **Agent-agnostic**: Works for all 8 agents (dispatcher, chat, coder, debugger, planner, analyst, committer, scheduler)
- **Provider-agnostic**: Works with any LLM provider (Anthropic, OpenAI, OpenAI-compatible)
- **Transparent**: No code changes needed in agents or tools

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Agent Loop                           │
│  (dispatcher, coder, debugger, etc.)                    │
└─────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────┐
│              Token Cache Coordinator                    │
│         (internal/llm/token_cache.go)                   │
│  ┌───────────────────────────────────────────────────┐  │
│  │ L1: Exact-Match Cache (in-memory, 10k entries)    │  │
│  └───────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────┐  │
│  │ L2: Content-Hash Cache (SQLite, file-aware)       │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────┐
│              LLM Client Layer                           │
│    (internal/llm/client.go, anthropic.go)               │
└─────────────────────────────────────────────────────────┘
```

## Multi-Level Caching

### L1 Cache (In-Memory)

| Property | Value |
|----------|-------|
| Storage | Go map with mutex protection |
| Capacity | 10,000 entries (configurable) |
| Latency | <1ms |
| Eviction | LRU (oldest by creation time) |
| TTL | 30 minutes default (configurable) |

**Best for**: Repeated queries within a session, rapid iteration.

### L2 Cache (SQLite)

| Property | Value |
|----------|-------|
| Storage | SQLite database (`~/.meept/token_cache.db`) |
| Capacity | Unlimited (disk-bound) |
| Latency | 5-10ms |
| Eviction | TTL-based background cleanup |
| TTL | 30 minutes default (configurable) |

**Best for**: Cross-session caching, crash recovery, file-aware caching.

## Cache Keys

Every cache entry is keyed by:

1. **Model ID**: Different models = different cache entries
2. **Prompt Hash**: SHA256 of the full prompt content
3. **File Hashes** (optional): SHA256 of referenced file contents

### File-Aware Caching

For code-focused queries, the cache automatically extracts file references from prompts:

- `file: /path/to/file.go`
- `@path/to/file.go`
- `/absolute/path/file.go:42`

When a file changes, all cache entries referencing it are automatically invalidated.

## Configuration

Enable token caching in `~/.meept/meept.toml`:

```toml
[llm.cache]
enabled = true
l1_max_entries = 10000
l2_enabled = true
l2_db_path = "~/.meept/token_cache.db"
default_ttl_min = 30
```

| Setting | Default | Description |
|---------|---------|-------------|
| `enabled` | `false` | Enable/disable caching |
| `l1_max_entries` | `10000` | Maximum L1 cache entries |
| `l2_enabled` | `true` | Enable SQLite L2 cache |
| `l2_db_path` | `~/.meept/token_cache.db` | SQLite database path |
| `default_ttl_min` | `30` | Cache entry TTL in minutes |

## CLI Commands

```bash
# Show cache statistics
meept cache status

# Clear all cache entries
meept cache clear

# Invalidate entries for a specific file
meept cache invalidate --path internal/llm/client.go
```

## Metrics

Cache metrics are recorded in the metrics store:

| Metric | Type | Description |
|--------|------|-------------|
| `cache.hit` | Counter | Cache hit (tags: `model_id`, `agent_id`) |
| `cache.miss` | Counter | Cache miss (tags: `model_id`, `agent_id`) |

View metrics via the HTTP API (MenuBar app):

```bash
curl http://localhost:8081/api/v1/metrics/live
```

## How It Works

### Cache Hit Flow

1. Agent sends chat request to LLM client
2. Client builds cache key from prompt + file hashes
3. Check L1 cache → **HIT**: Return cached response
4. Check L2 cache → **HIT**: Promote to L1, return response
5. **MISS**: Call LLM API, store in both L1 and L2

### Invalidation

Cache entries are invalidated when:

- TTL expires (background cleanup every 2 minutes)
- File content hash changes (file-aware caching)
- User runs `meept cache clear` or `meept cache invalidate`
- L1 reaches capacity (LRU eviction)

## Performance Impact

| Scenario | Hit Rate | Latency Savings |
|----------|----------|-----------------|
| Repeated queries | 80-95% | ~99% (L1 hit) |
| Same session, different prompts | 5-15% | Variable |
| Cross-session repeated | 20-40% | ~95% (L2 hit) |
| File-aware (code changed) | 0% | Forces refresh |

## Troubleshooting

### Cache not working

1. Check if caching is enabled in `meept.toml`
2. Verify daemon logs for "Token cache initialized" message
3. Run `meept cache status` to check if entries are being stored

### High memory usage

1. Reduce `l1_max_entries` in config
2. Run `meept cache clear` to clear entries
3. Check for large prompts causing big cache entries

### Stale responses

1. Run `meept cache invalidate --path <file>` for specific files
2. Run `meept cache clear` for full reset
3. Reduce `default_ttl_min` for shorter cache lifetime

## Implementation Details

### Thread Safety

- All cache operations are protected by `sync.RWMutex`
- L1 and L2 caches have independent locks
- Stats updates are atomic within coordinator lock

### Background Cleanup

- L1: Goroutine checks TTL every 2 minutes
- L2: SQLite background thread deletes expired entries
- Both cleanups log at DEBUG level

### Error Handling

- L1 failures: Silent (in-memory, no persistence)
- L2 failures: Logged as WARN, L1 continues working
- Cache is non-blocking: failures fall back to API call
