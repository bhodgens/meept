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

### Ambient Candidate Confidence Disposal (three bands)

Ambient extraction candidates are disposed by confidence band
(`AmbientExtractionConfig`):

| Band | Disposal |
|------|----------|
| `confidence < reject_below` (default 0 = disabled) | Hard-dropped, logged to `rejected_candidates.jsonl` (reason `reject_band`) |
| `reject_below <= confidence` | Stored as `status=auto` claim — librarian review pipeline confirms or rejects |
| `confidence >= confidence_threshold` (default 0.7) | Stored as `status=auto` claim (same pipeline) |

The confidence threshold no longer drops candidates: calibration data
(2026-09-17) showed the middle band is 33% correct, so hard-dropping it
loses real memory. Measured optimum for `reject_below` is 0.3 (below it
candidates are overwhelmingly decoy activations); recalibrate on live
verdicts (`claim_verdicts.jsonl`) before adjusting.

### Calibration Data Collection

Three JSONL logs in the memory data dir feed the confidence-calibration
loop:

- `rejected_candidates.jsonl` — candidates dropped by any gate, with the
  drop reason (`reject_band`, `confidence`, `category`, `max_per_turn`)
- `claim_verdicts.jsonl` — librarian promote/reject verdicts paired with
  the claim's extraction-time confidence
- `raw_responses.jsonl` — raw (prompt, response) pairs per classifier
  call, for extraction-model training

All three are size-capped at 10 MiB with one rotated generation
(`<name>.jsonl.1`). They contain raw conversation text: they live outside
the repo and are gitignored — never commit them.

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

## Conflict Detection & Merge

When `memory.conflict.enabled` is true, the L1 claim-extraction path
(`memory.l1_extract` queue jobs) runs batch conflict detection before
storing candidates: each extracted claim is recalled against similar
existing claims and ONE memory-model call decides an action per candidate
(plan forest tree 02, SHARED-CONVENTIONS §2.3).

### The four actions

| action | consumer rule |
|--------|---------------|
| `store` | the candidate is stored as a plain auto claim (default) |
| `merge` | the MERGED content is stored as a NEW claim (metadata `conflict_action=merge`, `conflict_targets=[...]`), then each target claim is superseded via the standard `MarkSuperseded` path (valid_to closed, superseded edge written, calibration row in `claim_verdicts.jsonl`) |
| `supersede` | the candidate text is stored as a new claim (`conflict_action=supersede`) and the stale targets are superseded |
| `skip` | the candidate is an exact duplicate — it is dropped and logged |

The conflict package only decides; every write goes through the Manager's
existing claim/supersede paths. `MarkSuperseded` keeps enforcing
auto-cannot-supersede-confirmed: when a merge/supersede targets a confirmed
claim, the supersede is refused and logged, and the successor claim stays.

### Fail-open rule

Any detector failure — recall error, LLM transport error, unparseable
output, missing entries — degrades to **store-all**: every candidate is
stored plainly with no conflict metadata. Extraction availability never
depends on the conflict LLM, and a failed-open batch leaves the store
byte-identical to the pre-conflict behavior.

### Configuration

```json5
"memory": {
  "conflict": {
    "enabled": false,   // opt-in; false = zero recall, zero LLM calls
    "top_k": 5,         // existing claims recalled per candidate
    "max_batch": 20,    // candidates per conflict LLM call
  },
}
```

When disabled, the L1 handler stores candidates directly — no recall, no
LLM call. Every conflict decision also emits a structured log line with the
action, candidate index, and target IDs.

### CLI

```bash
meept memory conflicts --since=48h   # audit trail of automated merges
```

Lists claims created by merge decisions (metadata `conflict_action=merge`),
newest first, with the superseded target IDs per row. Backed by the
`memory.listConflicts` RPC.

## Layered Distillation (L0/L1/L2)

The layered distillation pipeline (plan forest tree 01) gives memory a raw
tier and moves claim extraction off the turn path:

- **L0** — raw conversation turns appended to
  `<memory.data_dir>/l0/YYYY-MM-DD.jsonl` (one JSON object per line:
  `{id, ts, session_id, role, content, meta}`, append-only). Injected
  `<memory-context>` spans are stripped before persisting (feedback-loop
  guard). Recording happens in a session-end hook: append errors are logged
  and never fail the turn.
- **L1** — `memory.l1_extract` queue jobs distill a session's undistilled L0
  window into `status=auto` claims via the ambient extraction prompt. The
  per-session progress marker (an episodic memory, category `l0_progress`)
  advances ONLY on full success.
- **L2** — `memory.l2_scene` queue jobs (chained after each L1 success)
  segment the session-day into scene summary files under
  `<memory.data_dir>/l2/<session>-<date>-<scene>.json`
  (`segment_method: "llm"`, or `"fallback"` when the segmentation response
  is malformed — an L2 job never dead-letters on a malformed segmentation).

Everything is gated on `memory.l0_enabled` (default **false**): disabled
means nothing records and no distill job is ever enqueued.

### Configuration

```json5
"memory": {
  // Raw-conversation tier + async distillation. Opt-in.
  "l0_enabled": false,
  // Whole daily L0 files older than this are pruned on the consolidation
  // tick (no new ticker). <= 0 disables pruning. Default 30.
  "l0_retention_days": 30,
}
```

LLM calls resolve through the `memory_model` slot (same preference order as
ambient extraction: `memory_model` client > general provider > general
client).

### Failure semantics

- An LLM or store error inside an L1/L2 job returns an error to the queue —
  the queue's retry/backoff/dead-letter path owns failure handling (no
  private retry loop). The `l0_progress` marker never advances on failure,
  so the retry re-reads the same window (no lost lines, no duplicated
  claims).
- A duplicate L1 enqueue for an already-distilled window is suppressed by
  the marker (`status: "suppressed"`, no LLM call).
- Job dead-letters are visible in queue stats
  (`queue.stats` / `meept` queue surfaces); the L0 lines survive on disk so
  a dead-lettered window can be re-driven after fixing the model.
- Retention prunes whole daily files only — a partial day is never cut
  mid-file.

### Observability

```bash
meept memory l0 stats   # file count, line count, oldest date (no daemon)
```

Reads `<memory.data_dir>/l0/` directly. L2 scene files are plain JSON under
`<memory.data_dir>/l2/` and can be inspected with any JSON tool.

## Bridging Facts (bridges)

Bridging facts are short derived statements that link two or more stored
memories sharing an entity (IndexRAG stage 2) — e.g. three memories that each
mention ProjectX yield "projectx is led by alice". They are generated offline
in the consolidation pass from the local model, stored in their own bridge
shard with mandatory provenance (`evidence_ids`), and admitted into recall
context only in small numbers so they supplement rather than crowd out source
memories.

### Lifecycle

1. **Generation** (consolidator pass, offline only — never the chat path):
   entities with document frequency in `[2, bridge_df_max]` are bridged; per
   entity at most 5 source memories and 8 facts. The generator output is
   grammar-constrained JSON; a fact with no valid evidence IDs is rejected,
   and one citing only unknown IDs is dropped (provenance is never invented).
2. **Verification** (same pass): a grammar-constrained entailment verdict per
   bridge; unsupported bridges are deleted before ever being retrievable and
   survivors are stamped `verified=true`. Verification is a quality gate, not
   a recall gate.
3. **Recall** (post-retrieval, `SelectBalanced`): source units always pass;
   bridges are admitted while `bridges_included < kb` — at most k_b bridging
   facts enter one assembled context, regardless of how many rank highly.
4. **Invalidation** (guarantees): every bridge carries non-empty evidence
   IDs at write time, and deleting, superseding (versioned update), or
   consolidator-expiring a source memory cascade-deletes every bridge citing
   it. Invalidation is idempotent and best-effort — it never fails the
   source mutation — and the affected bridge becomes unretrievable
   immediately (vector row + metadata both removed).

### Configuration

All keys under `[memory.bridges]` (schema: `MemoryBridgesConfig` in
`internal/config/schema.go`). The whole subsystem is opt-in; the defaults
below are the shipped values.

| key | default | meaning |
|-----|---------|---------|
| `memory.bridges.enabled` | `false` | master gate; false = zero generation, zero LLM calls |
| `memory.bridges.kb` | `2` | recall-path cap: max bridges admitted into one assembled context |
| `memory.bridges.bridge_df_max` | `10` | inclusive upper bound of the entity document-frequency window (lower bound fixed at 2) |
| `memory.bridges.verify` | `true` | run entailment verification on generated bridges (requires a local model) |

### CLI / TUI visibility

```bash
meept memory --bridges      # list bridging facts (verified flag, evidence count, entity)
```

In the default `meept memory` listing and the TUI memory browser, bridge
rows carry a `[bridge]` marker with `verified: yes/no` and their evidence
count, so derived facts stay distinguishable from source memories. The TUI
badge is text-only for now (list description + detail-panel line); richer
GUI-side bridge visualization is a documented deviation pending GUI parity.

### Evaluation

Adoption is gated on the B05 bridges eval (on/off recall comparison,
`docs/research/eval/bridges-<date>.json` when run): correctness with bridges
must not regress, and context tokens must shrink or correctness must
improve, before `enabled` is flipped for any installation. See
`plans/context-reduction/09-bridges/05-bridge-eval.md` for the pass
criteria. Nobody enables bridges by default on the strength of the feature
alone.

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