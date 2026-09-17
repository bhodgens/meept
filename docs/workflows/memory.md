# Memory System

## Overview
Meept implements a multi-tiered memory architecture with different storage backends and query modes. The system includes episodic memory (FTS5), task memory, knowledge graph, distributed memory, and semantic memory with vector embeddings.

## Problem
Effective agent operation requires persistent memory across sessions. The memory system addresses:
- Context retention across conversations
- Domain-specific knowledge storage
- Efficient retrieval of relevant information
- Memory consolidation and summarization

### Usefulness Voting (opt-in)
Memories can receive explicit usefulness votes via the `memory_vote` tool (`memory_id`, `delta` of +1/-1, optional reason up to 512 bytes). Each memory's usefulness score is:

```
clamp01(base + Wv*net_votes + Wa*log1p(accesses) - Ws*age_days)
```

Defaults: `base=0.5`, `Wv=0.08`, `Wa=0.05`, `Ws=0.005`. When `[memory.usefulness] enabled = true` (default **false**), consolidation eviction reorders by this score: memories with net votes <= -2 are evicted first regardless of age, then the bottom `floor_pct` (default 0.05) by score is evicted before any age-based rule; the rest consolidate as before.

```toml
[memory.usefulness]
enabled = true
floor_pct = 0.05
base = 0.5
wv = 0.08
wa = 0.05
ws = 0.005
```

## Behavior

### Episodic Memory (FTS5)
- **SQLite Full-Text Search**: BM25 ranking for keyword relevance
- **Automatic Context Injection**: Based on recency and relevance
- **Conversation History**: Complete interaction tracking

### Task Memory
- **Domain-Specific Storage**: Separate namespaces for different task types
- **Technical Knowledge**: Code snippets, commands, patterns
- **Flattened Search Index**: FTS5 indexes `search_text` — the canonical
  flattened text (lesson principle / procedure title+steps) rather than raw
  JSON content, so BM25 ranks words instead of JSON syntax. The column
  backfills automatically from legacy rows at startup.
- **Any-Token Matching**: `MemoryQuery.MatchAny` switches FTS matching from
  ALL-token (AND) to ANY-token (OR) for relevance ranking where partial term
  overlap should still surface documents. Distilled-memory injection uses it.
- **Consolidation**: Promoted to episodic memory over time

### Knowledge Graph
- **PageRank Scoring**: Importance-based ranking
- **5 Relation Types**: `reference`, `similar`, `temporal`, `co_accessed`, `causal`
- **Similarity Edges Over Canonical Text**: similarity edges compare token
  overlap over the same canonical flattened text, never raw JSON payloads
- **Community Detection**: Clustering related memories
- **Entity-Centric Querying**: Focus on entities and relationships

### Distributed Memory (memvid)
- **2-Tier Architecture**: Local SQLite + shared memvid service
- **Hydration**: Fetch relevant memories when job claimed
- **Distillation**: Promote important memories to shared storage
- **Configurable Policies**: PageRank threshold, hub connectivity

### Semantic Memory (Vector Embeddings)
- **Vector Similarity Search**: Cosine similarity for ranking
- **Hybrid Search**: Combines keyword (FTS) and vector scores
- **Multi-Provider Support**: OpenAI and Ollama embeddings
- **Dimension Handling**: Supports different embedding sizes

### Personality Memory
- **User Preference Tracking**: Learns from conversation patterns
- **Periodic Updates**: Refreshed every N conversations
- **Response Style Influence**: Adapts to user preferences

### Claim Temporal Validity

Claims carry optional temporal bounds and a monotonic revision counter.

| Field | Metadata key | Meaning |
|-------|--------------|---------|
| ObservedAt | `observed_at` | when the claim was observed true (RFC3339; absent = store time) |
| ValidFrom | `valid_from` | earliest instant the claim is in force (RFC3339; absent = unbounded) |
| ValidTo | `valid_to` | latest instant the claim is in force (RFC3339; absent = unbounded) |
| Rev | `rev` | monotonic revision; 0 on create, +1 on each supersede |

Behavior:

- **Supersede** (`meept memory supersede`) stamps the successor with
  `rev = old rev + 1` and closes the superseded claim's window with
  `valid_to = supersede time` (in place; graph edges keep pointing at the
  same IDs).
- **Enforcement:** claims outside their window are hard-excluded from
  relationship detection and canonical-claim selection, with an explicit
  `expired` reason logged — they never silently vanish from auditability.
- **Visibility:** `meept memory expired` and the `list_expired_claims` tool
  list them; `retain_claim` accepts `valid_from` / `valid_to` /
  `observed_at` (RFC3339).
- **Backward compatibility:** claims stored before this feature have none of
  these keys and behave as unbounded, rev 0.

## Configuration

### Memory Model Slot

In `models.json5`, the optional `memory_model` slot selects a dedicated
model for **ambient epistemic extraction** and **distill summarization**
(the memory manager's consolidation/distill LLM and the epistemic detector
+ ambient extraction hook):

```json5
{
  // Dedicated model for memory extraction/distillation (provider/model-id
  // or alias). Empty = falls back to the general chat client.
  "memory_model": "zai/glm-4.5-air",
}
```

Preference order when resolving the memory-path chatter:
`memory_model` client > general LLM provider > general LLM client.
Leaving the slot empty keeps the historical behavior (the general chat
client serves memory extraction/distillation).

```toml
[memory]
backend = "memvid"  # or "sqlite"
data_dir = "~/.meept/memory"
consolidation_interval_hours = 6

[memory.episodic]
enabled = true
max_context_items = 20

[memory.task]
enabled = true
domains = ["general", "code", "commands"]

[memory.embeddings]
enabled = true
provider = "openai"  # or "ollama"
api_key = "sk-..."
model = "text-embedding-3-small"
dimension = 1536

[memvid]
enabled = false
endpoint = "http://localhost:8765"
data_dir = "~/.meept/memvid"

[distributed_memory]
enabled = false
mode = "distributed"

[distributed_memory.sync]
hydrate_on_claim = true
hydration_limit = 20
distill_on_complete = true

[distributed_memory.distillation]
pagerank_threshold = 0.3
hub_connectivity_threshold = 5
promote_task_completions = true
```

## Observability

### Logging
- Memory storage operations
- Search query performance
- Consolidation runs
- Vector embedding operations

### Metrics
- Memory storage latency
- Search hit rates
- Consolidation efficiency
- Vector search accuracy

### Debug Info
- Memory subsystem status
- Search relevance scores
- Vector embedding dimensions
- Distributed memory sync status

## Edge Cases

### Memory Storage Failure
- Graceful degradation to in-memory storage
- Logs storage errors for recovery
- Automatic retry with backoff

### Search Timeout
- Query timeout enforced
- Partial results returned
- Performance optimization suggested

### Vector Provider Unavailable
- Fallback to keyword search only
- Hybrid search disabled temporarily
- Provider status monitored for recovery

### Consolidation Conflict
- Concurrent consolidation prevented
- Locking mechanism ensures data integrity
- Failed consolidations retried