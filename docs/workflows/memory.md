# Memory System

## Overview
Meept implements a multi-tiered memory architecture with different storage backends and query modes. The system includes episodic memory (FTS5), task memory, knowledge graph, distributed memory, and semantic memory with vector embeddings.

## Problem
Effective agent operation requires persistent memory across sessions. The memory system addresses:
- Context retention across conversations
- Domain-specific knowledge storage
- Efficient retrieval of relevant information
- Memory consolidation and summarization

### Usefulness Voting (default on)
Memories can receive explicit usefulness votes via the `memory_vote` tool (`memory_id`, `delta` of +1/-1, optional reason up to 512 bytes). Each memory's usefulness score is:

```
clamp01(base + Wv*net_votes + Wa*log1p(accesses) - Ws*age_days)
```

Defaults: `base=0.5`, `Wv=0.08`, `Wa=0.05`, `Ws=0.005`. `[memory.usefulness] enabled` defaults **true** (memory second-brain decision D3, plan `docs/plans/20261030-memory-second-brain`: consolidation eviction reorders by this score — memories with net votes <= -2 are evicted first regardless of age, then the bottom `floor_pct` (default 0.05) by score is evicted before any age-based rule; the rest consolidate as before). Set `enabled = false` to restore age-only ordering.

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

Ambient extraction (`memory.epistemic.ambient_extraction`) defaults **on**
per the memory second-brain decision D3 (plan
`docs/plans/20261030-memory-second-brain`); its candidates are disposed by
confidence band (`AmbientExtractionConfig`):

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

### Recall Outcome Telemetry

`recall_outcomes.jsonl` (same memory data dir) records whether an injected
memory actually helped: the turn-terminal capture subscriber writes one row
per injected memory item per completed turn —

```json
{"ts":"RFC3339","session_id":"...","memory_id":"...","query":"...","injected":true,"cited":true,"tokens":123}
```

`cited` is conservative: the final reply must share >= 8 lowercase word
tokens with the item's salient text (reply-side tokens count with
multiplicity), else `cited=false`. The file rides the same calibration-JSONL
mechanism (10 MiB cap, one `.jsonl.1` rotation) and is gitignored — rows
carry memory and reply text; never commit one. The recall-value consumer
(feeding scores back into recall ranking) is future work; today the file is
only produced.

### Human Review Queue

Auto-extracted claims (`status=auto`) await a human verdict. The review
queue is one surface with three clients — every verdict, from any client,
goes through the same daemon path (`Manager.PromoteClaimWithReason` /
`RejectClaimWithReason`), writes the calibration row, and (when an
auditor is wired) one `memory.claim.verdict` auditlog entry:

| Surface | Entry point |
|---------|-------------|
| CLI | `meept memory review` (list), `meept memory promote ID [--reason S]`, `meept memory reject ID [--reason S]` |
| TUI | command palette → `memory review` (or the `v` palette key) |
| RPC | `memory.reviewQueue` (list: `min_confidence`, `max_confidence`, `older_than`, `limit` ≤200, `offset`), `memory.promoteClaim {id, reason?}`, `memory.rejectClaim {id, reason?}` |

Verdict reasons are optional, capped at 512 bytes (same cap as the
usefulness-vote reason), stored as claim metadata `verdict_reason`, and
hash-entered into the audit chain (sha256 first-16, raw text stays only
in the calibration log). Review scope is `status=auto` claims only —
promoted/rejected are final states and drop out of every query.

TUI review view (`internal/tui`, `MemoryReviewModal`):

- Columns: `id`, `confidence`, `category`, `age`, `preview` (content
  truncated to 60 chars; the column itself is fitted to the viewport).
- Keys: `j/k` navigate, `enter` detail panel, `p` promote the selected
  claim, `r` reject it, `esc` closes the detail panel first, then the
  modal. After a verdict the list refreshes from the daemon, dropping the
  verdict-ed row while keeping the cursor stable; the status bar shows
  `N claims to review (v)` whenever the queue fetch reported a nonzero
  total (the indicator rides the modal's own fetches — no extra per-turn
  RPC). The TUI panel records no reason; use the CLI `--reason` flag when
  one is warranted.
- The keybinding is configurable (`client.json5`:
  `keybindings.command_palette.memory_review`, default `v`).

GUI parity: a Flutter review panel is deliberately deferred to a later
tree (documented deviation `palette:memory review` in
`internal/tui/testdata/gui_inventory.json`). Until it lands, GUI users
reach the same verdicts through the RPC methods or the CLI.

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
backend = "local"  # built-in SQLite store; "sqlite" is a legacy alias; "memvid" uses the memvid service when [memvid] enabled
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
    "enabled": false,   // opt-in (NOT flipped by the D1-D3 default-on wave); false = zero recall, zero LLM calls
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
  guard). Recording is turn-terminal driven: a daemon bus subscriber on
  `turn.terminal` (`internal/daemon/memory_capture.go`, gated on
  `memory.l0_enabled`) appends the user message and the final reply for
  every turn that ends `status=completed` — direct chat turns AND
  dispatched/task-scoped turns alike, where the retired session-end
  `l0_record` hook never fired (task loops do not inherit the primary hook
  registry; see `.github/ISSUES/task-loops-no-hook-registry-l0-ambient.md`).
  Append errors are logged and never fail the turn.
- **L1** — `memory.l1_extract` queue jobs distill a session's undistilled L0
  window into `status=auto` claims via the ambient extraction prompt. The
  per-session progress marker (an episodic memory, category `l0_progress`)
  advances ONLY on full success.
- **L2** — `memory.l2_scene` queue jobs (chained after each L1 success)
  segment the session-day into scene summary files under
  `<memory.data_dir>/l2/<session>-<date>-<scene>.json`
  (`segment_method: "llm"`, or `"fallback"` when the segmentation response
  is malformed — an L2 job never dead-letters on a malformed segmentation).

Everything is gated on `memory.l0_enabled`, which defaults **true** per the
memory second-brain decision D3 (plan `docs/plans/20261030-memory-second-brain`:
full default-on — L0 capture + L1 extraction + L2 scenes on every daemon; cost
is local compute). Set it to `false` to opt out: disabled means nothing records
and no distill job is ever enqueued. The capture
subscriber's ambient epistemic extraction additionally gates on
`memory.epistemic.ambient_extraction.enabled`, which also defaults **true** per
D3 — with that gate off, L0 lines and L1 distill jobs still run, but no
turn-side ambient `status=auto` claims are written (the L1 job's own extraction
is unaffected).

### Configuration

```json5
"memory": {
  // Raw-conversation tier + async distillation. Default on (D3).
  "l0_enabled": true,
  // Whole daily L0 files older than this are pruned on the consolidation
  // tick (no new ticker). <= 0 disables pruning. Default 30.
  "l0_retention_days": 30,
}
```

LLM calls resolve through the `memory_model` slot (same preference order as
ambient extraction: `memory_model` client > general provider > general
client).

### Models-less hosts (skip-Warn degradation)

On a host with NO resolvable model (empty `memory_model` slot, no provider,
no general client), a default-on daemon must not error-loop the queue: the
L1 job SKIPS with exactly one Warn per worker cycle
(`distill: no memory model available; distill jobs skip until a model
resolves ...`) and returns `status: "skipped_no_model"` with NO error — the
job completes, the `l0_progress` marker never advances, and the L0 lines
survive on disk. When a model later resolves, the next window's job (or a
re-enqueued one) re-distills the same undisilled window — no lost lines, no
duplicated claims. L2 degrades the same way: no model means the fallback
whole-session scene (the existing `segment_method: "fallback"` path), never
a dead letter. The chat turn path is unaffected either way (capture and
extraction are off the turn path).

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
   Bridges carry a scope stamp folded from their evidence sources at
   generation (all-global sources → `global`; one bound project and no user
   sources → that `project`; anything else → `user`, with a single shared
   `user_id` when exactly one exists). The recall-side `WithScopeFilter`
   predicate binds bridge rows exactly like source rows (bughunt
   2026-11-06: bridge rows were appended AFTER the filter and had no scope
   key, so a project-scoped caller could read facts distilled from another
   project's memories). Pre-stamp bridges keep no scope key and read as
   global — the safe legacy default; the over-visible window closes for new
   bridges.
4. **Invalidation** (guarantees): every bridge carries non-empty evidence
   IDs at write time, and deleting, superseding (versioned update), or
   consolidator-expiring a source memory cascade-deletes every bridge citing
   it. Invalidation is idempotent and best-effort — it never fails the
   source mutation — and the affected bridge becomes unretrievable
   immediately (vector row + metadata both removed).

### Configuration

All keys under `[memory.bridges]` (schema: `MemoryBridgesConfig` in
`internal/config/schema.go`). The subsystem is DEFAULT-ON per the memory
second-brain decision D1 (plan `docs/plans/20261030-memory-second-brain`,
"Q1 a" — enable by default now; the leaf-07 substitution experiment may flip
it back if it loses the eval). The defaults below are the shipped values.

| key | default | meaning |
|-----|---------|---------|
| `memory.bridges.enabled` | `true` | master gate; false = zero generation, zero LLM calls |
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

The B05 bridges eval (on/off recall comparison) remains the quality gate:
correctness with bridges must not regress, and context tokens must shrink or
correctness must improve, or the leaf-07 substitution experiment flips
`enabled` back to false (per D1's flip-back clause). See
`plans/context-reduction/09-bridges/05-bridge-eval.md` for the pass criteria.

## Knowledge Assets (default on)

The `[memory.knowledge]` block gates a SEPARATE knowledge-asset store
(tree 04): SQLite pages with an FTS5 full-text index and a link graph, at
`<daemon.data_dir>/knowledge/knowledge.db`. This is **not** the evolver
wiki (`internal/selfimprove`) — separate store, separate schema, separate
code path; the wiki stays evolver-only and is never reachable from the
inference path.

### Default posture

`memory.knowledge.enabled` defaults to **true** per the memory second-brain
decisions D1-D3 (plan `docs/plans/20261030-memory-second-brain` — the
SHARED-CONVENTIONS §2.5 product decision authorized there). The knowledge_*
tools register out of the box; nothing is ambiently injected (all agent
access is on-demand through explicit tool calls). Set `enabled = false` to
opt out: nothing is stored and no `knowledge_*` tool exists — nothing from
this store can enter a prompt.

### Tools

| Tool | Class | Purpose |
|------|-------|---------|
| `knowledge_search` | memory_read | FTS5 BM25 search; `{query, limit?}` (default 5, cap 20) → ranked hits |
| `knowledge_read` | memory_read | full page by ID; resolves `[[wikilinks]]` to page IDs where the target exists |
| `knowledge_ingest` | memory_write | upsert a page; identity is title+source hash, so re-ingesting the same pair replaces the body in place |
| `knowledge_impact` | memory_read | one-hop callers of a Go symbol from the code-index call-edge table ("what breaks if I change this") |

### Watched-dir ingestion

`memory.knowledge.ingest_dir` names a directory of markdown files synced
into the store on the periodic **consolidation tick** (no new ticker):
every `*.md` file becomes a page titled by its relative path, with source
`dir:<relpath>` making the identity stable across syncs. Unchanged files
(byte-identical content) are skipped by content hash, so repeated ticks
are cheap. Passes are best-effort: failures log a warning, never abort
consolidation.

### Code indexing (`code_dirs`)

`memory.knowledge.code_dirs` lists Go repository roots indexed on the same
consolidation tick by the `go/ast` code indexer (stdlib only, no new
dependencies). Each `*.go` file becomes a CODE page (`title =
"code:<pkg-path>:<file>"`, body = package clause + imports + top-level
declaration signatures, `source = "code:<relpath>"`) so code and documents
answer one `knowledge_search` query. The indexer also records intra-repo
call edges; `knowledge_impact` reads them. Skip rules: `vendor/`,
`testdata/`, hidden dirs, and `*_test.go` are never indexed; unresolved
(external) callees are never stored as edges; unchanged files skip the
upsert by content hash.

### Eviction

`memory.knowledge.max_pages` caps the stored page count at ingest time
(oldest-ingested page evicted when the cap is hit, eviction logged;
`<= 0` = unbounded). A full store still serves search/read — only ingests
evict.

### Operator visibility

`meept knowledge stats` prints page count, code-page count, and call-edge
count via a direct read-only peek at the store file (no daemon required;
honors `MEEPT_HOME` config resolution). The e2e suite
(`e2e/suites/memory-knowledge-assets/`) covers ingest → search → wikilink
read → upsert-in-place, code-index → search → impact, and the disabled
control (zero `knowledge_*` tools offered to the model).

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

## Scope (project/user/global)

Every stored memory carries a recall scope (`memory.Scope`), stored as the
portable metadata key `scope` everywhere and as a queryable
`scope TEXT NOT NULL DEFAULT 'global'` column in the SQLite content tables
(episodic + task; the FTS index table is never altered — content-table
column only).

- `project` — visible only to sessions bound to the same project
  (matched against metadata `project_id`).
- `user` — visible to all sessions of the user (matched against metadata
  `user_id`); in single-user installs an empty filter userID matches.
- `global` — visible everywhere. The legacy backfill value: every row that
  predates scoping is `global`, so no existing memory disappears from
  recall.

Recall is scope-aware via the additive `WithScopeFilter(maxScope,
projectID, userID)` search option on `Manager.Search` /
`GetRelevantContext` (+ `WithMultiUserEnabled(bool)` carrying the
multiuser bit). Callers that do not pass the option get today's
unfiltered behavior byte-identically. `maxScope` is the CALLER's operating
scope (purview): project-scope callers see only their own project's rows;
user-scope callers see all project rows plus matching user rows; global
callers see only global rows. Fail-closed rule: multiuser enabled +
resolved empty userID ⇒ user-scope rows are NOT recallable (an identity
resolution gap must never widen recall).

### Cluster recall is explicitly unscoped across nodes

Peer-node memories that arrive via gossip ingestion (MEMORY_STORED →
`DualStore.StoreRemoteMemory`) are stamped `scope=global` plus
`remote_node=<source node>` metadata at the ingest seam. Cluster recall is
therefore deliberately UNscoped across nodes: a peer's memories are visible
everywhere on the receiving node, exactly as before scoping existed — the
stamp makes that a documented, queryable decision (provenance via
`remote_node`), not a silent backfill accident. Single-daemon installs are
unaffected. Narrowing cross-node recall per project is future work; the
`remote_node` marker is the hook it would build on.

### Agent declarations: `memory_max_scope` (chief-of-staff pattern)

An agent (AGENT.md frontmatter `memory_max_scope:`) declares the WIDEST
scope its sessions recall at: `""`/`project` (default — isolated to the
session's bound project), `user`, or `global`. Resolution
(`agent.resolveScopeInputs`) combines the declaration with the session's
bound project and OwnerID; invalid values fail SAFE to project (warned).
Every loop recall and every memory tool (`memory_store`/`memory_retain`
stamp on write; `memory_search`/`memory_recall`/`memory_get_context`
filter on read) flows through that resolution:

- Writes: an explicit `scope` argument on `memory_store` is honored
  verbatim (the writer's intent — recall-time maxScope is the enforcement
  boundary, not the write stamp); the default stamp is the session's own
  scope (project when bound, global when unbound). `project_id` /
  `user_id` ride the metadata for predicate matching.
- Reads: project-declared agents see global rows + their own project's
  rows (an unbound session sees NO project rows — never a daemon-CWD
  fallback). A `global` declaration is the chief-of-staff grant: it maps
  onto the filter's user purview (all projects' rows + user rows), since
  the committed C2 predicate treats a global-scope CALLER as
  identity-less. Under enabled multiuser (`[multiuser] enabled`), an
  identity-resolution gap (empty OwnerID) fails CLOSED: user-scope rows
  stay hidden.

### Bot `memory_scope` is superseded (Flag 2)

Bot definitions (internal/bot) carry an older `memory_scope` vocabulary
(private/shared/read_only). It is SUPERSEDED for scope governance by
`memory_max_scope`: scope enforcement now flows exclusively through the
agent declaration + `resolveScopeInputs`. The bot fields remain (bots are
a separate surface; `ScopedMemoryManager`'s bot_id namespace tagging is a
different mechanism and untouched). Mapping when reading a bot's memory
posture: private→project, shared→global, read_only→project (there is no
read-only equivalent in the Scope vocabulary — write suppression is a
tool-permission concern, not a recall scope).

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