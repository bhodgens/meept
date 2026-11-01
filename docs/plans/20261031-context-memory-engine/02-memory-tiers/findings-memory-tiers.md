# Findings: Tiered Agent Memory Architectures

> Research leaf 00-research-tiers (branch 02-memory-tiers, plan
> 20261031-context-memory-engine). Pinned reference for leaves 01
> (working-tier-store), 02 (policy-automation), 03 (demotion); cite
> recommendations by ID (R-B1..R-Bn) in your Notes. ASCII only.
> Sources verified via web search 2026-11-01.

## Tier Architectures

**1. MemGPT / Letta: OS-style main context vs external context.**
MemGPT models the LLM context window as "main context" (RAM) and treats
everything else as "external context" (disk), paging between them via
function calls (https://arxiv.org/abs/2310.08560). Main context is split
into: system instructions (read-only), a working context block (fixed-size
read/write text holding key facts/preferences), and a FIFO message queue
whose head holds a recursive summary of evicted messages. External context
is split into recall storage (the full, uncompressed message history) and
archival storage (arbitrary text, semantically searchable). Tier boundary
is the context-window token count itself: eviction is triggered at token
thresholds, not item counts. Letta (the MemGPT productionization) names
the tiers core memory (in-context, character-limited blocks, typically
2000-5000 chars per block, 5-15 blocks recommended), recall memory
(searchable full conversation history), and archival memory (agent-curated
semantic store, unlimited size) (https://docs.letta.com/v1-sdk/memory/
memory-blocks/, https://docs.letta.com/v1-sdk/memory/archival-memory/,
https://www.letta.com/blog/agent-memory/). Letta's guidance on what
belongs where is explicit: always-visible or frequently-changing state
goes in core blocks; intentional, curated facts go to archival; raw
history is searchable but not pinned (archival docs, ibid.). Letta also
runs "sleep-time agents" that rewrite memory blocks out-of-band,
consolidating context over time (letta.com blog, ibid.) - prior art for
our off-chat-path policy engine.

**2. Generative Agents: single flat stream, scored retrieval.**
Park et al. store everything in one memory stream (observation objects
with creation + last-access timestamps) with no capacity tiers; the
"tiering" happens at retrieval time via score = recency (exponential
decay, factor 0.995 per simulated hour) + importance (LLM-rated 1-10 at
write) + relevance (embedding cosine), min-max normalized, weights all 1
(https://arxiv.org/abs/2304.03442, https://dl.acm.org/doi/fullHtml/
10.1145/3586183.3606763). The tier boundary is the prompt: top-ranked
memories that fit the context window are included. Reflections (higher-
level synthesized thoughts) are written back into the same stream,
triggered when cumulative importance of recent events crosses a threshold
(roughly 2-3x/day in the sandbox). Lesson: a flat store with good
scoring is a viable alternative to hard tiers, but gives no cost bound on
the retrieval side - exactly the gap AMV-L (below) addresses.

**3. AMV-L: value-based lifecycle tiers (hot/warm/cold).**
AMV-L treats agent memory as a managed systems resource: each item
carries a continuously updated utility value V(m); tiers are hot
(retrieval-eligible), warm (retained, excluded from the default retrieval
path), cold (retained at minimal cost). Only the hot tier (plus a bounded
warm sample) feeds retrieval, so request-path cost tracks the eligible
set, not total stored memory (https://arxiv.org/html/2603.04443). Tier
transitions (promotion on access/use, demotion on decay) are applied
asynchronously, off the critical path. Their headline result vs
TTL-only retention: 3.1x throughput, 4.2x lower median latency, tail
requests >2s cut from 13.8% to 0.007%. Lesson: retention (what is kept)
and eligibility (what is searched) are separate controls; confounding
them causes unbounded retrieval cost.

**4. HippoRAG: neocortex/hippocampus indexing.**
HippoRAG keeps raw passages (the "neocortex", actual memory content)
separate from a derived KG index (the "hippocampus", pointers +
associations); retrieval runs Personalized PageRank over the index,
10-30x cheaper than iterative retrieval at comparable quality
(https://arxiv.org/html/2405.14831). Lesson: derived, lossy structures
(indices, claims) can live in a separate tier as long as the lossless
source of truth persists - directly applicable to meept's L1 claims over
the episodic FTS5 store.

**5. MemOS: memory as a schedulable resource.**
MemOS unifies plaintext, activation, and parametric memory in MemCube
units carrying payload + metadata (provenance, version chain, usage
metrics); "policy-aware scheduling" migrates memory between tiers and
formats based on usage frequency and task fit
(https://arxiv.org/abs/2507.03724, https://github.com/MemTensor/MemOS/).
Its lifecycle phases - generation, activation, fusion, archiving,
expiration - map onto our promote/compress/demote/evict action space.

**6. Memini: concrete small-scale tier policy (closest to meept's shape).**
A small production system with working (72h TTL) -> episodic (30d TTL)
-> semantic (durable) tiers: write-time distillation of fresh captures
into durable facts; working->episodic promotion at >= 3 recalls; demotion
of durable memories that were never recalled, are low-importance, and
unverified once older than 168h; short-term cap of 1000 items per
namespace with lowest-retention-first eviction
(https://github.com/eleboucher/memini/blob/main/docs/tiers.md). Its
numbers are useful defaults for our scale.

Mapping to meept: working tier = Letta core / MemGPT working context
(bounded, in/near the conversation buffer); episodic = MemGPT recall
storage (lossless, FTS5); semantic = Letta archival / HippoRAG index
(derived claims). Meept already has episodic + L1/L2 distillation; the
missing piece is the working tier and the automated policy layer, matching
the orchestrator's C4/C5/C6 plan.

## Eviction Policies

**P1. Threshold-then-flush with pre-warning (MemGPT).** Two token
triggers: a warning at ~70% of the context window ("memory pressure",
gives the model a chance to save important items) and a flush at ~100%
that evicts ~50% of the queue and regenerates the recursive summary
(https://arxiv.org/abs/2310.08560). Eviction is lossless: evicted
messages go to recall storage indefinitely. Applicability: the working
ring (C4) should warn/degrade early and never evict un-persisted turns;
the flush fraction (50%) is a proven batch size.

**P2. Scored usefulness eviction (hybrid importance + recency).** MaRS
evaluates six forgetting policies (FIFO, LRU, Priority Decay,
Reflection-Summary, Random-Drop, Hybrid); the Hybrid
(temporal -> reflection -> importance -> privacy stages) scores best
composite (~0.911) across 300 runs and five budgets
(https://arxiv.org/html/2512.12856). The HTM project's ADR argues the
same: importance alone fails when everything is equally important; recency
alone evicts critical old context; hybrid evicts low-importance-old first
(https://madbomber.github.io/htm/architecture/adrs/007-eviction-strategy/).
For semantic caches, a large survey found LFU best-or-tied in all 18
settings (no policy beats it by >0.041pp; FIFO trails by up to 8.67pp at
10% capacity), and capacity/policy are separable decisions
(https://arxiv.org/html/2608.20280v2). TraceRetain evicts the lowest
linear-feature score (success, age, last-access gap, log access
frequency, redundancy, observed downstream utility, ...) when a fixed
item capacity K is exceeded (https://arxiv.org/html/2606.29178).
Applicability: reuse meept's PlanUsefulEviction (usefulness votes,
internal/memory/consolidation.go:537-566) as the ranking core - it is
already a hybrid usefulness score; layer recency/staleness in as a
tiebreak, matching the hybrid literature.

**P3. Token triggers vs item triggers.** Token-based triggers are right
for any tier whose contents reach the prompt (working tier): the cost
driver is prompt tokens, and item counts proxy tokens badly when turn
lengths vary. Item caps are right for stores that only pay at retrieval
(episodic/semantic): Memini caps short-term tiers at 1000 items/namespace;
TraceRetain uses item capacity K. TTL alone is insufficient for either:
AMV-L shows TTL bounds item age but not request-path compute, which grows
with the retrieval-eligible pool (https://arxiv.org/html/2603.04443).
Applicability: C4's MaxTokens (primary, ring) + MaxItems (hard backstop)
+ TTL (session scoping) is the correct three-part shape; C5 TierState
should carry both Tokens and Items (it does).

**P4. Never-delete eviction (demote-to-long-term).** HTM's "never-forget
guarantee": evicted working items are marked, kept in long-term storage,
and auto-reloaded on recall (https://madbomber.github.io/htm/architecture/
adrs/007-eviction-strategy/). MemGPT likewise never discards (recall
storage keeps everything). Applicability: working-tier eviction (C4
Append overflow) must first offer the turns to the episodic sink (Flush),
not drop them; true deletion only happens at the existing L0 prune /
access-expiry paths (config/schema.go retention prior art: 30d daily-file
prune, 90d access expiry).

**P5. Small-store edge behavior.** At tight capacity the policy choice
matters most (FIFO/SISO lose up to 8.67pp vs LFU at 10% capacity; at
large capacity every policy converges - LFU-LRU gap "sharply narrows")
(https://arxiv.org/html/2608.20280v2). Consequence for C5: with
EvictOverRatio defaults and small rings, pick a deterministic ranked
policy (usefulness+recency) over FIFO; do not over-engineer, since at
generous ceilings any sane policy works. Also skip eviction entirely when
over-ceiling by only a trivial amount (hysteresis) to avoid thrash -
AMV-L applies transitions incrementally and asynchronously for the same
reason (https://arxiv.org/html/2603.04443).

## Compression and Distillation

**C-P1. Compress before you evict; keep the lossless source.**
MemGPT's flush is lossy in-context but lossless globally (recursive
summary + full recall storage). Summarization drift is the key risk:
each pass discards low-frequency details and errors compound over dozens
of passes; mitigations are (a) retain the original record in cold
storage, (b) prefer extraction of structured facts over free-text
summarization, and (c) never re-compress already-compressed summaries
(https://zylos.ai/research/2026-04-20-memory-consolidation-ai-agents/).
B4.run implements the write-ordering carefully: write the summary first,
then link sources as superseded, then stamp retention expiry - "a failure
after the summary write can leave a redundant active summary; linking
first could hide sources with no durable replacement"
(https://b4.run/docs/memory/distillation). Applicability: C5's compress
action = meept's existing distill-to-claim pipeline (memory.l1_extract /
memory.l2_scene, internal/memory/distill_async.go:31-33) producing
extracted claims (structured), with the episodic raw record retained; a
claim that fails to persist must not demote/hide its source turn.

**C-P2. Trigger conditions.** Three proven trigger families: (a) token
thresholds - compress when context exceeds X% (MemGPT warning at 70%,
AMV-L prompt caps); (b) schedules/idle windows - end-of-session or
background consolidation (Letta sleep-time agents, Auto-Dreamer's offline
consolidator which treats the region as read-only evidence and emits a
compact replacement set: https://arxiv.org/html/2605.20616); (c)
significance gates - Generative Agents' cumulative-importance threshold
(https://arxiv.org/abs/2304.03442). No head-to-head study crowns a
winner; threshold-gating sits between inline (freshest, on critical
path) and scheduled (cheapest, staler)
(https://davidamitchell.github.io/Research/research/
2026-07-20-agent-memory-consolidation-episodic-semantic.html).
Applicability: C5 uses (a) CompressOverRatio on TierState.Tokens as the
primary trigger and runs on a background tick (b); batch-level guards
from B4.run: minimum batch size (they use 5) and max records per pass
(100), oldest first (https://b4.run/docs/memory/distillation).

**C-P3. Loss accounting per transition.** Working -> episodic is
lossless (turns copied verbatim; C4 Flush). Episodic -> semantic (L1
claims) is lossy by design: extraction keeps salient facts and drops
phrasing/noise; Nemori argues distillation should keep the *unexpected*
(prediction error vs existing knowledge) rather than the predictable
(https://arxiv.org/html/2508.03341v4). Semantic -> episodic demotion
(C6) is lossless at the episodic tier (copy claim content back down,
mark the original demoted - orchestrator contract). Never compress a
derived record again: B4.run skips derived summaries in later passes;
Zylos lists "limit compression depth" as a drift mitigation
(https://b4.run/docs/memory/distillation,
https://zylos.ai/research/2026-04-20-memory-consolidation-ai-agents/).

**C-P4. Cost bounds and dedup of consolidation work.** B4.run advances a
watermark per reflection pass and writes a "no-insight sentinel" when the
model returns nothing, so the same input window is never re-billed
(https://b4.run/docs/memory/distillation). Auto-Dreamer decouples fast
per-session acquisition from slow cross-session consolidation and bounds
the consolidator's tool-use (https://arxiv.org/html/2605.20616).
Applicability: C5's background engine should persist a per-tier
last-processed cursor (or reuse consolidation run bookkeeping) so a tick
that finds nothing does zero LLM calls, and compression passes have a
max-batch cap. Meept's Consolidator single-start rule
(consolidation.go:60,96) is the same idiom: one background worker, never
two.

## Promotion and Demotion

**D1. Demotion criteria (semantic -> episodic).** Three evidenced
criteria:
- *Staleness / decay:* SurrealDB Agent Memory applies category-aware
  decay on a daily pass (context 0.95/pass, knowledge 0.995, identity
  1.0) - decay lowers importance only, never deletes; context facts
  additionally expire 7 days after creation
  (https://surrealdb.com/docs/agent-memory/mental-model/memory-lifecycle).
  Meept's internal/memory/staleness.go is the prior-art hook (orchestrator
  contract C6).
- *Low retrieval hit rate:* Memini demotes durable memories that were
  "never recalled, low-importance, and uncorroborated" once older than
  168h (7d); anything recalled even once is reinforced and kept
  (https://github.com/eleboucher/memini/blob/main/docs/tiers.md).
- *Contradiction / supersession:* TEPA revokes an active precedent when
  fresh same-key evidence conflicts (posterior below threshold after
  n_min=5 observations, or recent success rate < 0.34); revoked memory
  stays archived for audit and can be re-promoted later
  (https://arxiv.org/html/2608.07429v2). SurrealDB separates supersession
  (replaced, old value closed in time but kept) from decay (relevance
  fade) from forget (explicit removal) - three mechanisms that must not
  share one code path (https://surrealdb.com/docs/agent-memory/
  mental-model/memory-lifecycle). aimemoryworks adds: detect conflicts on
  WRITE (retrieval-time detection is too late), rank sources, timestamp
  ties, mark superseded with a pointer - do not delete
  (https://aimemoryworks.com/architecture/conflicting-memories/).

**D2. Demotion is a flag, not a delete; validity vs visibility.** MEMFSM
separates validity (is the fact still true? states: current/superseded/
expired) from visibility (should retrieval surface it for this query?);
superseded facts are hidden for current-intent queries but surfaced for
historical ones (https://aclanthology.org/2026.emnlp-main.260.pdf).
Applicability: C6 marks the semantic entry demoted via metadata flag
(never delete) and the episodic copy stays searchable; semantic recall
must filter demoted IDs out of the default path.

**D3. Promotion criteria.** Evidence-backed gates: Memini promotes
working -> episodic at >= 3 recalls (MEMINI_PROMOTE_MIN_ACCESS), then
frequently-recalled items are distilled into durable semantic facts on a
24h pass (https://github.com/eleboucher/memini/blob/main/docs/tiers.md).
CALM activates a candidate at direct user evidence or confidence >= 0.60
and promotes Active -> Stable at evidence count >= 2 or confidence >=
0.80; strong supersession caps the old record's validity at 0.15
(https://arxiv.org/abs/2610.38573). TEPA-Full trial-validates candidates
before promotion (>= 60% positive trials incl. one support success)
(https://arxiv.org/html/2608.07429v2). Corroboration (a fresh restatement
of an existing fact) should reinforce confidence, rate-limited (Memini:
once per 24h per fact) rather than duplicate
(https://github.com/eleboucher/memini/blob/main/docs/tiers.md).

**D4. Idempotency is a hard requirement.** B4.run documents what happens
without it: a partial link failure can leave a batch permanently
unreconcilable, and rejecting a whole candidate batch deletes the
watermark so the same inputs get re-billed
(https://b4.run/docs/memory/distillation). SurrealDB/TEPA keep superseded
records with pointers precisely so re-processing is safe. Applicability:
C6's contract "re-planning an already-demoted ID returns noop" matches the
literature; the same rule must extend to compress actions (an already-
distilled source turn must yield noop, not a second claim) and to evict
(evicting an absent ID is noop). A demoted-then-repromoted entry must be
representable (TEPA re-promotion), i.e. the flag is a state in a small
lifecycle FSM, not a tombstone.

## Recommendations for the Tier Engine

- **R-B1 (C4 ring sizing):** Make the working ring token-accounted with
  MaxTokens primary and MaxItems a hard backstop. Defaults: MaxTokens =
  8192 tokens (about 10% of a 64K-128K window; Letta caps core blocks at
  2000-5000 *characters* each, 5-15 blocks), MaxItems = 200 turns, TTL =
  24h (session-scoped; Memini working TTL is 72h at conversation scale).
  Append evicts oldest-when-overflow, and overflow turns are offered to
  the EpisodicSink before being dropped (never-delete eviction, P4).
- **R-B2 (C5 trigger ratios):** Adopt MemGPT's two-threshold shape:
  CompressOverRatio = 0.7 (soft: start distilling/compressing the
  coldest segments) and EvictOverRatio = 0.9 (hard: rank with
  PlanUsefulEviction usefulness + recency tiebreak and evict in a batch
  sized ~50% of the overage, MemGPT flush precedent). Compress precedes
  evict: compression is recoverable and preserves information; eviction
  of un-distilled content is the last resort (C-P1).
- **R-B3 (C5 trigger units):** Evaluate triggers on Tokens for tiers that
  reach the prompt (working; episodic recall feeds), on Items for
  semantic (retrieval-candidate-set cost, AMV-L). Keep TTL for session
  scoping only; do not use TTL as the eviction policy (P3, AMV-L).
- **R-B4 (C5 compress-vs-evict precedence):** Action ordering per tick:
  demote (stale/contradicted) -> compress (over CompressOverRatio) ->
  evict (over EvictOverRatio, usefulness-ranked). Compression reuses
  memory.l1_extract/l2_scene (distill_async.go:31-33) and must be
  extraction-to-claims, not free-text re-summarization; never compress an
  already-derived record (skip IDs whose source is itself derived,
  B4.run). Retain raw episodic rows (lossless archival).
- **R-B5 (C5 batching + cost bounds):** Per pass: min batch 5 records,
  max batch 100, oldest first, skip if under min; persist a per-tier
  watermark/cursor and a no-op sentinel so an empty pass costs zero LLM
  calls (B4.run). Engine stays off the chat path on its own background
  tick, reusing the Consolidator single-start discipline - never a second
  consolidator (C-P4, orchestrator invariant).
- **R-B6 (C6 demotion criteria, in priority order):** (1) contradiction/
  supersession: same-subject semantic claim contradicted by a newer
  source -> demote old claim to episodic and mark superseded with a
  pointer to the replacement (TEPA, aimemoryworks); detect on write when
  cheap. (2) staleness: staleness.go score below threshold OR age >
  90d with zero retrievals in 30d (aligns with existing 90d access-expiry
  prior art; Memini uses 168h at a smaller scale). (3) low utility:
  usefulness vote aggregate below threshold with >= 1 demotion-eligible
  tick. Demotion copies content to episodic (lossless) and flags the
  semantic entry; never deletes (D1, D2).
- **R-B7 (C6 idempotency):** PlanDemotions/Execute must be idempotent per
  ID: already-demoted -> noop; concurrent planning and execution of the
  same ID resolved by the flag check inside the execution transaction.
  Model demotion state as a small enum (active | demoted | superseded)
  so re-promotion is representable (MEMFSM + TEPA lifecycle states).
  Semantic recall filters demoted/superseded IDs from the default
  retrieval path (visibility axis) while episodic search still reaches
  the content.
- **R-B8 (C6/promotion, future):** Gate semantic promotion on retrieval
  evidence: >= 3 recalls of an episodic record (Memini) or corroboration
  by an independent source raises claim confidence; cap corroboration
  reinforcement at once/24h/fact (Memini). Ship this opt-in and defer if
  C6 scope tightens - demotion is the correctness-critical direction.
- **R-B9 (defaults policy):** All of this ships opt-in (Enabled=false,
  per docs/agents/opt-in-defaults.md). The flip proposal should cite the
  AMV-L and MaRS results as the expected-benefit evidence and the cost
  bound (R-B5) as the risk mitigation.

## Source Index

1. MemGPT: Towards LLMs as Operating Systems - https://arxiv.org/abs/2310.08560
2. Letta memory blocks docs - https://docs.letta.com/v1-sdk/memory/memory-blocks/
3. Letta archival memory docs - https://docs.letta.com/v1-sdk/memory/archival-memory/
4. Letta blog: Agent Memory - https://www.letta.com/blog/agent-memory/
5. Generative Agents (Park et al., UIST 2023) - https://arxiv.org/abs/2304.03442
   (also https://dl.acm.org/doi/fullHtml/10.1145/3586183.3606763)
6. AMV-L: Adaptive Memory Value Lifecycle - https://arxiv.org/html/2603.04443
7. TraceRetain: Selective Memory Retention - https://arxiv.org/html/2606.29178
8. Which Eviction Policy Should an LLM Cache Use? - https://arxiv.org/html/2608.20280v2
9. MaRS / Forgetful but Faithful (six forgetting policies) - https://arxiv.org/html/2512.12856
10. HTM ADR-007 Eviction Strategy - https://madbomber.github.io/htm/architecture/adrs/007-eviction-strategy/
11. B4.run Distillation docs - https://b4.run/docs/memory/distillation
12. Zylos: Memory Consolidation in Long-Running AI Agents - https://zylos.ai/research/2026-04-20-memory-consolidation-ai-agents/
13. Nemori: Adaptive Memory Distillation - https://arxiv.org/html/2508.03341v4
14. Auto-Dreamer: Offline Memory Consolidation - https://arxiv.org/html/2605.20616
15. HippoRAG - https://arxiv.org/html/2405.14831
16. MemOS - https://arxiv.org/abs/2507.03724 (https://github.com/MemTensor/MemOS/)
17. Memini tier docs - https://github.com/eleboucher/memini/blob/main/docs/tiers.md
18. TEPA: Revoking Stale Memories - https://arxiv.org/html/2608.07429v2
19. MEMFSM: Validity Is Not Visibility - https://aclanthology.org/2026.emnlp-main.260.pdf
20. SurrealDB Agent Memory lifecycle - https://surrealdb.com/docs/agent-memory/mental-model/memory-lifecycle
21. CALM: Conflict-Aware Lifecycle Memory Governance - https://arxiv.org/abs/2610.38573
22. aimemoryworks: Conflicting Memories - https://aimemoryworks.com/architecture/conflicting-memories/
23. Mitchell: Episodic-to-semantic consolidation architectures - https://davidamitchell.github.io/Research/research/2026-07-20-agent-memory-consolidation-episodic-semantic.html
