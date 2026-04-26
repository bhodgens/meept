# Token Cache CLI Reference

The `meept cache` command provides management utilities for the LLM token cache.

## Synopsis

```bash
meept cache [command]
```

## Available Commands

### `meept cache status`

Show cache statistics including hit/miss rates and entry counts.

**Usage:**
```bash
meept cache status [flags]
```

**Output Example:**
```
Token Cache Statistics
====================
L1 Cache:
  Entries:     142
  Hits:        523
  Misses:      89
  Evictions:   12

L2 Cache:
  Entries:     1847
  Hits:        234
  Misses:      156

Overall:
  Total Hits:  757
  Hit Rate:    89.5%
```

**Fields:**

| Field | Description |
|-------|-------------|
| `L1 Entries` | Current in-memory cache entries |
| `L1 Hits` | Number of L1 cache hits |
| `L1 Misses` | Number of L1 cache misses |
| `Evictions` | Entries evicted due to capacity |
| `L2 Entries` | Current SQLite cache entries |
| `L2 Hits` | Number of L2 cache hits |
| `L2 Misses` | Number of L2 cache misses |
| `Total Hits` | Combined L1 + L2 hits |
| `Hit Rate` | Percentage of requests served from cache |

---

### `meept cache clear`

Clear all cache entries from both L1 and L2 caches.

**Usage:**
```bash
meept cache clear [flags]
```

**Output Example:**
```
Token cache cleared successfully
```

**Warnings:**
- This operation is irreversible
- All cached responses will be lost
- Next requests will call the LLM API directly

---

### `meept cache invalidate`

Invalidate cache entries that reference a specific file path.

**Usage:**
```bash
meept cache invalidate --path <file> [flags]
```

**Required Flags:**

| Flag | Shorthand | Description |
|------|-----------|-------------|
| `--path` | `-p` | File path to invalidate (required) |

**Example:**
```bash
meept cache invalidate --path internal/llm/client.go
```

**Output Example:**
```
Cache entries invalidated for file: internal/llm/client.go
```

**How It Works:**
1. Searches L1 cache for entries with matching file hash
2. Runs SQL query on L2: `DELETE WHERE file_hashes_json LIKE '%"path/to/file"%'`
3. Logs number of invalidated entries at DEBUG level

**Patterns Matched:**
- Exact path matches: `/absolute/path/file.go`
- Relative paths: `relative/path/file.go`
- File references in any cache entry's `FileHashes` map

---

## Global Flags

These flags are available for all `meept cache` subcommands:

| Flag | Shorthand | Default | Description |
|------|-----------|---------|-------------|
| `--debug` | | `""` | Enable debug output (`--debug` or `--debug=file`, use `-` for stderr) |
| `--socket` | `-s` | `~/.meept/meept.sock` | Unix socket path for daemon connection |
| `--state-dir` | `-d` | `~/.meept` | State directory |

---

## Examples

### Check cache performance
```bash
# View current cache statistics
meept cache status
```

### Clear cache after code changes
```bash
# Invalidate specific file
meept cache invalidate -p internal/llm/client.go

# Or clear everything
meept cache clear
```

### Debug cache issues
```bash
# Enable debug logging
meept --debug=- cache status
```

---

## Error Handling

### Daemon not running
```
Error: failed to connect to daemon: dial unix /Users/caimlas/.meept/meept.sock: connect: no such file or directory

Make sure the daemon is running:
  meept daemon start
```

**Solution:** Start the daemon with `meept daemon start`.

### Cache not enabled
```
Error: failed to get cache stats: cache not enabled
```

**Solution:** Enable caching in `~/.meept/meept.toml`:
```toml
[llm.cache]
enabled = true
```

### Unknown subscription
```
Error: failed to invalidate cache: subscription not found
```

**Solution:** This is a transient error. Retry the command.

---

## Implementation Notes

### RPC Methods

The CLI commands use the following JSON-RPC methods:

| Command | RPC Method | Parameters |
|---------|------------|------------|
| `status` | `cache.stats` | `{}` |
| `clear` | `cache.clear` | `{}` |
| `invalidate` | `cache.invalidate` | `{"file_path": "..."}` |

### Response Types

**cache.stats response:**
```json
{
  "l1_entries": 142,
  "l1_hits": 523,
  "l1_misses": 89,
  "evictions": 12,
  "l2_entries": 1847,
  "l2_hits": 234,
  "l2_misses": 156,
  "total_hits": 757,
  "hit_rate": 89.5
}
```

**cache.clear response:**
```json
{
  "status": "cleared"
}
```

**cache.invalidate response:**
```json
{
  "status": "invalidated",
  "file": "internal/llm/client.go"
}
```

---

## Related Commands

- `meept memory` - Search and manage episodic/task memories
- `meept models` - Configure LLM providers and models
- `meept config` - View/edit configuration

## Related Documentation

- [Token Caching Concepts](../concepts/token-caching.md) - Architecture and configuration
- [Architecture](../concepts/architecture.md) - System overview
