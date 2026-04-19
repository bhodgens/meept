# Memory System

## Overview
Meept implements a multi-tiered memory architecture with different storage backends and query modes. The system includes episodic memory (FTS5), task memory, knowledge graph, distributed memory, and semantic memory with vector embeddings.

## Problem
Effective agent operation requires persistent memory across sessions. The memory system addresses:
- Context retention across conversations
- Domain-specific knowledge storage
- Efficient retrieval of relevant information
- Memory consolidation and summarization

## Behavior

### Episodic Memory (FTS5)
- **SQLite Full-Text Search**: BM25 ranking for keyword relevance
- **Automatic Context Injection**: Based on recency and relevance
- **Conversation History**: Complete interaction tracking

### Task Memory
- **Domain-Specific Storage**: Separate namespaces for different task types
- **Technical Knowledge**: Code snippets, commands, patterns
- **Consolidation**: Promoted to episodic memory over time

### Knowledge Graph
- **PageRank Scoring**: Importance-based ranking
- **5 Relation Types**: `reference`, `similar`, `temporal`, `co_accessed`, `causal`
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

## Configuration

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