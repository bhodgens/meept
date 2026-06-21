# Prompt Compression

Context compression for LLM conversation messages and tool outputs. Compresses large results to save context tokens while maintaining full reversibility via CCR (Compress-Cache-Retrieve).

- **Package:** `internal/compress/`
- **Config schema:** `AgentCompressionConfig` in `internal/config/schema.go`
- **Design plan:** `docs/plans/headroom-integration.md`
- **Implementation status:** `docs/plans/headroom-integration-findings.md`
- **Status:** Feature-flagged, off by default

---

## Overview

Prompt compression reduces the token count of tool outputs and conversation messages before they are sent to the LLM. It applies content-aware algorithms (JSON crushing, AST-aware code compression, log filtering, search-result grouping) to achieve **60-90% token reduction** on typical tool outputs, while storing the original content in a reversible CCR store so the agent can retrieve the full content on demand.

### When to use

- **Long tool outputs** — file listings, API responses, grep results, log dumps that exceed `min_tokens_to_compress`
- **Context-bound sessions** — long-running conversations where context window pressure builds up
- **Cost-sensitive models** — models charged per-token (not flat-rate) benefit most from compression

### When to skip

- **Short outputs** — nothing saved below the `min_tokens_to_compress` threshold (default 500 tokens)
- **Coding tasks that need full file content** — compression may remove implementation details the agent needs to reference
- **Debugging sessions** — the agent may need exact error messages, stack traces, or log lines

---

## Architecture

```
User / System Message
         │
         ▼
┌──────────────────────────────────────────────┐
│  Agent Loop                                  │
│                                              │
│  Tool Execution → CompressToolResult() → LLM│
│       │              │                       │
│       │              ├─ ContentRouter         ├─ Detect: JSON, code, logs, search, diff
│       │              ├─ SmartCrusher (JSON)   ├─ Array dedup, anomaly preservation
│       │              ├─ CodeCompressor        ├─ Line-based (tree-sitter planned)
│       │              ├─ LogCompressor         ├─ Keep ERROR/WARN, drop noise
│       │              ├─ SearchCompressor      ├─ Group by file, keep matches
│       │              │                         │
│       │              └─ CCR Store (SQLite)    ├─ Store originals by hash
│       │                                         └─ Add <<ccr:HASH>> marker
│       │
│       └── MCP Tools
│           - mcc_compress
│           - mcc_retrieve
│           - mcc_stats
└──────────────────────────────────────────────┘
```

### Pipeline flow

1. **ContentRouter** detects the content type (JSON, code, logs, search results, diff) based on heuristics.
2. The appropriate **compressor** processes the content and produces a compressed version plus metrics (tokens before/after, strategy used).
3. If compression was effective and CCR is enabled, the **CCR store** saves the original content keyed by content hash.
4. A **retrieval marker** (`<<ccr:abc123...>>`) is appended to the compressed output so the agent can use `mcc_retrieve` to get the original.

### CCR (Compress-Cache-Retrieve)

The CCR store is a SQLite-backed content-addressed storage that keeps original (uncompressed) content alongside its compressed version. Each entry is keyed by a 24-character SHA-256 hash of the content. Entries expire based on the configured TTL (default 1 hour).

**Marker formats:**

| Format | Example |
|--------|---------|
| Compact | `<<ccr:abc123def456789012345678>>` |
| Verbose | `[142 items compressed to 180 tokens, hash=abc123def456789012345678]` |

---

## Compression Algorithms

### SmartCrusher (JSON)

Compresses JSON and structured data by:
- Removing duplicate array elements (by content hash)
- Preserving errors and anomaly values
- Keeping first N and last N items from arrays
- Capping array size to a configurable maximum (default 50)
- Targeting a configurable compression ratio (default: auto)

Typical savings: **70-90%** on tool outputs (API responses, file listings, directory entries).

### CodeCompressor

Provides line-based compression for source code:
- Detects language from file extension or content patterns
- Preserves imports, type definitions, function signatures
- Replaces long function bodies with a summary
- Supports configurable language list (default: Go, Python, TypeScript, Rust)

A tree-sitter AST-backed version is planned (`internal/code/ast/` parsers are available but not yet wired).

Typical savings: **60-80%** on source code files.

### LogCompressor

Compresses log output by:
- Always keeping ERROR, WARN, FATAL lines
- Keeping first and last N lines of output
- Limiting repeated line blocks
- Preserving timestamps

Typical savings: **70-90%** on verbose logs.

### SearchCompressor

Compresses grep/search results by:
- Grouping matches by file
- Keeping only the matching lines with configurable context
- Capping matches per file (default 10)

Typical savings: **80-95%** on large search results.

---

## Configuration Reference

All settings live under `agent.compression` in the config file.

### AgentCompressionConfig

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | `bool` | `false` | Turns prompt compression on/off. Disabled by default for safe rollout. |
| `min_tokens_to_compress` | `int` | `500` | Minimum token count for compression to be attempted. Messages smaller than this are passed through uncompressed. |
| `strategy` | `string` | `"auto"` | Default compression strategy: `"smart_crusher"`, `"code"`, `"log"`, `"search"`, or `"auto"` (content-aware routing). |
| `ttl` | `duration` | `1h` | How long compressed originals are retained in the CCR store. |
| `log_compression` | `bool` | `true` | Enables compression for log tool outputs. |
| `code_compression` | `bool` | `true` | Enables AST-aware (line-based) code compression for file reads and edits. |
| `search_compression` | `bool` | `true` | Enables compression for grep/search result outputs. |
| `json_compression` | `bool` | `true` | Enables SmartCrusher compression for JSON tool outputs. |
| `compress_user_messages` | `bool` | `false` | Enables compression of user messages (not just tool outputs). For coding agents, keep false. Set true for document compression or RAG pipelines. |
| `target_ratio` | `float64` | `0.0` | Target compression ratio (kept tokens / original tokens). `0.3` = keep 30%, discard 70%. `0.0` uses compressor defaults. |

---

## Usage Guide

### Enabling compression

Edit your config file (`~/.meept/meept.json5`):

```json5
{
  agent: {
    compression: {
      enabled: true,
    },
  },
}
```

Restart the daemon for the change to take effect. Compression will automatically apply to tool outputs exceeding `min_tokens_to_compress` tokens.

### Agent system prompt injection

When compression is enabled, the agent loop injects this instruction into the system prompt:

```
CONTEXT COMPRESSION ACTIVE:
- Large tool outputs are compressed to save context space
- Compressed content shows: [N items compressed to X tokens, hash=abc123]
- To retrieve full content, use: mcc_retrieve(hash="abc123")
- Originals are retained for 1 hour
```

This teaches the agent how to use `mcc_retrieve` when it needs full context back.

### Compression in the agent loop

The agent loop compresses tool results automatically in `internal/agent/loop.go`:

1. After a tool call returns, if `compressionPipeline` is set, each result's output is passed to `CompressToolResult()`.
2. If the output exceeds 500 characters (hardcoded threshold at the call site), compression runs.
3. On success, the compressed result replaces the original in the response.
4. On failure (pipeline error, closed pipeline), the original is used unchanged — compression failures never break the agent loop.

### Graceful degradation

- Compression is **never a hard failure**: if the pipeline returns an error, the original content is kept.
- If `compressionPipeline` is nil (compression not enabled), the agent loop skips compression entirely.
- If the CCR store is unavailable but the pipeline runs, compression still produces compressed output — it just won't store the original for retrieval.

---

## MCP Tools

Three MCP tools are available when compression is enabled: `mcc_compress`, `mcc_retrieve`, and `mcc_stats`. They are defined in `internal/tools/mcp/compression.go`.

### mcc_compress

Compress content and store the original in the CCR store for later retrieval.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `content` | `string` | Yes | The content string to compress. |
| `tool_name` | `string` | No | Optional name of the tool that produced this content (used for analytics). |

**Response:**

```json
{
  "hash": "abc123def456789012345678",
  "original_tokens": 512,
  "compressed_tokens": 128,
  "saved": 384
}
```

**Example:**

```
Use mcc_compress to compress this directory listing:

{
  "files": [
    {"name": "go.mod", "size": 512},
    {"name": "go.sum", "size": 8192},
    ...
  ]
}
```

### mcc_retrieve

Retrieve the original (uncompressed) content by its compression hash.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `hash` | `string` | Yes | The compression hash returned by `mcc_compress`. |

**Response:**

```json
{
  "original": "... full uncompressed content ...",
  "found": true,
  "strategy": "smart_crusher",
  "hash": "abc123def456789012345678",
  "tool_name": "platform_ls",
  "created_at": "2026-06-20T12:00:00Z"
}
```

If the hash is not found or has expired:

```json
{
  "original": "",
  "found": false
}
```

### mcc_stats

Return aggregate compression statistics tracked by the handler.

**Parameters:** None.

**Response:**

```json
{
  "entry_count": 15,
  "total_saved": 48732,
  "retrieval_count": 3,
  "store_entries": 15,
  "store_retrievals": 3
}
```

| Field | Description |
|-------|-------------|
| `entry_count` | Number of entries in the CCR store (also `store_entries`). |
| `total_saved` | Total tokens saved across all `mcc_compress` calls (handler counter). |
| `retrieval_count` | Number of successful `mcc_retrieve` calls (handler counter). |
| `store_retrievals` | Total retrievals tracked in the CCR store itself. |

---

## HTTP API

### GET /api/v1/compression/stats

Returns pipeline and store statistics.

**Authentication:** Standard HTTP auth if `require_auth` is enabled in transport config. The `/health` endpoint is the only public endpoint; compression stats require authentication.

**Response (200 OK):**

```json
{
  "entry_count": 15,
  "total_saved": 48732,
  "retrieval_count": 3,
  "store_entries": 15,
  "store_retrievals": 3
}
```

**Service Unavailable (503):**

If the compression stats getter is not wired (compression not enabled in config):

```json
{
  "error": "compression service not available"
}
```

**Example curl:**

```bash
curl -H "Authorization: Bearer YOUR_API_KEY" \
     http://localhost:8081/api/v1/compression/stats
```

---

## Configuration Examples

### Minimal (just enable)

```json5
{
  agent: {
    compression: {
      enabled: true,
    },
  },
}
```

Uses all defaults: `min_tokens_to_compress=500`, `strategy="auto"`, `ttl=1h`, all compressor types enabled.

### Conservative (large items only)

```json5
{
  agent: {
    compression: {
      enabled: true,
      min_tokens_to_compress: 1000,
      log_compression: true,
      search_compression: true,
      json_compression: true,
      code_compression: false,
    },
  },
}
```

Only compress outputs larger than 1000 tokens. Disable code compression to avoid losing implementation details.

### Aggressive (maximum savings)

```json5
{
  agent: {
    compression: {
      enabled: true,
      min_tokens_to_compress: 250,
      code_compression: true,
      log_compression: true,
      search_compression: true,
      json_compression: true,
      compress_user_messages: true,
      target_ratio: 0.3,
      ttl: "2h",
    },
  },
}
```

Compress outputs as small as 250 tokens. Also compress user messages (useful for RAG/document compression). Targets keeping only 30% of tokens. Retains originals for 2 hours.

### Disabled (default)

```json5
{
  agent: {
    compression: {
      enabled: false,
    },
  },
}
```

---

## Troubleshooting

### Compression not running at all

**Check:** Is the feature flag enabled?

```bash
# Config file location
cat ~/.meept/meept.json5 | jq '.agent.compression.enabled'

# Or use the config CLI
meept config get agent.compression.enabled
```

If `enabled` is `false` (the default), nothing will be compressed. Set it to `true` and restart the daemon.

### Compression not saving tokens

**Check:** Is the output small enough to matter?

If the tool output has fewer than `min_tokens_to_compress` tokens (default 500), it passes through uncompressed. Increase the threshold to test with smaller outputs, or look at responses from larger files.

### Retrieval returns "not found"

**Cause:** The CCR entry has expired.

Default TTL is 1 hour. Check the `created_at` time in `mcc_retrieve` output and compare with TTL. If you need longer retention, increase `ttl` in config.

### Agent gets confused after compression

**Cause:** The compressed output may have changed meaning.

This can happen with aggressive settings (low `min_tokens_to_compress`, non-auto `target_ratio`). Try:
1. Set `min_tokens_to_compress` higher (e.g., 1000)
2. Disable specific compressors (`code_compression: false`, `json_compression: false`)
3. Set `strategy: "auto"` to let the router pick the right compressor

### "compression pipeline failed" in logs

**Cause:** The pipeline encountered an error during compression.

Compression failures are non-fatal — the original output is used unchanged. Check log level for details. Common causes:
- Pipeline is closed (daemon shutting down)
- CCR store SQLite errors (disk full, permission issues) — check the store path at `~/.meept/compression.db`
- Invalid JSON passed to SmartCrusher (falls back to passthrough, not an error)

### "compression service not available" on HTTP endpoint

**Cause:** Compression is not enabled in the config, so `CompressionStatsGetter` is never set on the HTTP server. The route is registered in `server.go` but will return 503 when the getter is nil. Enable compression in config to make this endpoint functional.

### Compression store growing without bound

Check the TTL setting. Entries are marked with `expires_at` but a cleanup goroutine must run to actually delete them. If entries are not expiring, check that the CCR store's TTL is set correctly and that no custom code has disabled expiry.

### SQLite busy errors

The CCR store uses WAL mode with a 5-second busy timeout (configured in `internal/compress/ccr_store_sqlite.go`). If you see "database is locked" errors, the issue is likely file I/O contention rather than a compression problem.

---

## Implementation Details

### File layout

| File | Description |
|------|-------------|
| `internal/compress/types.go` | Core types: `CCREntry`, `CompressionResult`, `CompressionStrategy`, `CCRStats` |
| `internal/compress/ccr_store.go` | `CCRStore` interface |
| `internal/compress/ccr_store_sqlite.go` | SQLite-backed CCR store (WAL mode, shared cache) |
| `internal/compress/ccr_hash.go` | SHA-256 content hashing, marker format/parsing (`<<ccr:HASH>>`) |
| `internal/compress/router.go` | `ContentRouter` — detects content type and dispatches to compressor |
| `internal/compress/smart_crusher.go` | JSON compressor (array dedup, anomaly preservation) |
| `internal/compress/code_compress.go` | Code compressor (line-based, tree-sitter planned) |
| `internal/compress/log_compress.go` | Log compressor (error/warn preservation) |
| `internal/compress/search_compress.go` | Search result compressor (group by file) |
| `internal/compress/pipeline.go` | `Pipeline` — orchestrates router + CCR store, provides `CompressToolResult()` |
| `internal/tools/mcp/compression.go` | MCP compression handler (`mcc_compress`, `mcc_retrieve`, `mcc_stats`) |
| `internal/daemon/daemon.go` | Daemon wiring: creates `CCRStore` and `Pipeline`, wires to `AgentLoop` |
| `internal/agent/loop.go` | Agent loop: applies compression to tool results via `CompressToolResult()` |
| `internal/metrics/collector.go` | Records compression metrics, subscribes to `compress.saved` bus events |
| `internal/comm/http/api_handlers.go` | HTTP handler for `GET /api/v1/compression/stats` |
| `internal/comm/http/server.go` | HTTP route registration |
