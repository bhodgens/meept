# R0 Research Findings: Semantic Cache Thresholds, `--cache-reuse`, TTFT Conventions

Plan: docs/plans/20261031-llm-serving-tiers
Leaf: R0 (research; runs first). Consuming leaves:
- Section 1 -> 02-semantic-cache.md
- Section 2 -> 03-cache-reuse-config.md
- Section 3 -> 04-ttft-metrics.md

All claims below carry at least one source URL. Claims I could not verify
against a source are explicitly labeled UNVERIFIED. Pure ASCII.

---

## Section 1: Semantic caching for LLM responses (consumed by 02-semantic-cache.md)

### 1.1 How the tier slots in

A semantic cache sits BEHIND the exact-key cache: exact hit first (the repo's
existing ResponseCache at internal/llm/token_cache.go), then a semantic
lookup on exact misses, then the model. This is the standard cache-aside
layering (https://github.com/redis/agent-skills/blob/main/skills/redis-semantic-cache/SKILL.md,
https://docs.promptise.com/core/cache/). GPTCache is the canonical open-source
design: embed the query, nearest-neighbor search in a vector store, accept the
top candidate if cosine similarity clears a threshold
(https://aclanthology.org/2023.nlposs-1.24.pdf,
https://github.com/zilliztech/GPTCache).

### 1.2 Recommended default threshold

**Recommended default: 0.95 cosine similarity -- NOT the 0.92 currently pinned
in master.md C4.** This is an explicit superseding recommendation per the leaf
Notes; the orchestrator should amend C4 via dated addendum before dispatching
leaf 02.

Rationale:

1. Practitioner and vendor guidance for false-positive-sensitive workloads
   converges on 0.95: AWS Well-Architected names "cosine similarity greater
   than 0.95" as the configurable threshold example
   (https://docs.aws.amazon.com/wellarchitected/latest/agentic-ai-lens/agentcost02-bp03.html);
   Redis LangCache guidance says 0.95+ for "customer-facing answers where
   wrong responses are costly" and 0.9 only as a balanced default
   (https://github.com/redis/agent-skills/blob/main/skills/redis-semantic-cache/SKILL.md);
   Redis's own guide says "start at a 0.90-0.95 threshold and monitor false
   positives" (https://redis.io/blog/what-is-semantic-caching/); Vercel says
   "Start at 0.95, then sample production traffic for false positives" and
   notes practitioner guidance around 0.92-0.97
   (https://vercel.com/i/semantic-caching-llm-apps).
2. meep is an agent daemon: a large share of traffic is tool-calling and
   code-adjacent, exactly the traffic Vercel and others flag as
   anti-patterns for loose thresholds (see 1.4). A one-word operand
   difference in a prompt is semantically critical and embeddings blur it
   (https://vercel.com/i/semantic-caching-llm-apps).
3. The academic literature shows the false-hit cost is asymmetric and large:
   a fixed 0.80 default leaked a 39% false-hit rate and cost 6.3x the
   no-cache baseline under a realistic penalty
   (https://www.sunnypatel.net/semantic-cache-reliability-sunny-patel.pdf).
   The over-tight direction (missed reuses) is bounded and cheap; the
   over-loose direction (wrong answers) is unbounded
   (https://www.sunnypatel.net/semantic-cache-reliability-sunny-patel.pdf).

Counterpoint kept honest: GPTCache's own experiments found 0.7 balanced hit
vs positive ratio on their eval set, and the GPTCache default ships around
0.75 (https://aclanthology.org/2023.nlposs-1.24.pdf,
https://vercel.com/i/semantic-caching-llm-apps). Those numbers come from
FAQ-style single-turn datasets with a fine-tuned similarity evaluator; they
are not transferable to agent traffic, and the same GPTCache paper concedes
negative-hit rates "would be unacceptable in real production scenarios" and
that even the best hit rates never exceeded 90% positive
(https://aclanthology.org/2023.nlposs-1.24.pdf).

Operational caveats for whatever value ships:

- The threshold must be RE-CALIBRATED whenever the embedding model changes;
  similarity scales are not comparable across models
  (https://balazscsorba.com/blog/semantic-caching-llm-responses,
  https://aclanthology.org/2023.nlposs-1.24.pdf). Embedding-model swaps
  invalidating stored vectors is a first-class failure mode, not an edge case.
- Calibrate on labeled prompt pairs from real traffic (paraphrases that must
  hit; negations, id swaps, opposite actions that must not), sweep the
  threshold, and pick the lowest value with near-zero wrong hits
  (https://balazscsorba.com/blog/semantic-caching-llm-responses,
  https://www.scaler.com/topics/semantic-caching-for-llms/).
- Measure hit rate AND precision together; hit rate alone is meaningless
  (a threshold of 0 gives 100% hits and a useless cache)
  (https://www.scaler.com/topics/semantic-caching-for-llms/).
- Verified caches (vCache) replace the static threshold with a per-entry
  error-rate guarantee and show correct/incorrect hit similarity
  distributions overlap heavily, meaning any fixed threshold is a compromise
  (https://arxiv.org/html/2502.03771v3). A static 0.95 is an acceptable
  starting default; online calibration is a later enhancement, not a
  prerequisite.

### 1.3 False-hit risks (the leaf requires >= 3; here are 5)

1. Negation blindness: embedding models place negated sentences close
   together; "enable 2FA" vs "disable 2FA", "strengths of this design" vs
   "weaknesses of this design" score very high similarity with opposite
   answers (https://vercel.com/i/semantic-caching-llm-apps,
   https://www.scaler.com/topics/semantic-caching-for-llms/,
   https://balazscsorba.com/blog/semantic-caching-llm-responses).
2. Parameter/operand identity: prompts differing in one id, number, or
   operand embed above 0.95 because the shared frame dominates the vector,
   yet need different answers
   (https://vercel.com/i/semantic-caching-llm-apps,
   https://dev.to/michael_maurice/7-prompts-your-semantic-cache-should-never-answer-from-memory-net-10-239b).
3. Context/conversation dependence: a follow-up turn's meaning depends on
   prior turns ("it" = Paris in one conversation, London in another); the
   visible prompt underdetermines the answer
   (https://docs.promptise.com/core/cache/,
   https://vercel.com/i/semantic-caching-llm-apps).
4. Stale live data: answers that depended on tool results, prices, status
   counts, etc. are snapshots; serving them later is a wrong answer even at
   similarity 1.0 (https://balazscsorba.com/blog/semantic-caching-llm-responses,
   https://docs.promptise.com/core/cache/).
5. Cross-scope leakage: answers cached without tenant/user/model/system-prompt
   scoping leak one user's (or one model's) answer to another; personal-data
   in the cache is personal data at rest
   (https://balazscsorba.com/blog/semantic-caching-llm-responses,
   https://docs.promptise.com/core/cache/).

A documented adversarial surface also exists: an attacker who can shape
prompts can manufacture collisions that force false hits
(https://www.sunnypatel.net/semantic-cache-reliability-sunny-patel.pdf).

### 1.4 Tool-call safety

The safest and most-cited rule: **any turn that produced or consumed tool
calls must bypass the semantic cache entirely -- not looked up, not stored.**
A cached answer replaces the whole turn including the tool call, so the tool
never runs; for a write tool that means "open a ticket" gets answered with a
stale ticket id and nothing is opened
(https://docs.promptise.com/core/cache/). The .NET guidance concurs: any
call with tools offered, or tool calls/results in history, bypasses the
cache; cache the tool's data with its own TTL, never the model's summary of
it (https://dev.to/michael_maurice/7-prompts-your-semantic-cache-should-never-answer-from-memory-net-10-239b).
Vercel lists agentic pipelines (answer depends on prior tool results) and
RAG as semantic-cache anti-patterns
(https://vercel.com/i/semantic-caching-llm-apps).

Practical gate for leaf 02: if the request carries tools, or any message in
the request history has role "tool" / carries tool_call content, the
semantic tier returns "miss" unconditionally. This mirrors the existing
position of the exact-key cache (consulted before the budget gate at
internal/llm/client.go:649-657) -- the semantic tier slots behind that exact
miss with the same eligibility gate.

Additional scoping rules the same sources mandate and leaf 02 should carry:
partition the cache by (model, system-prompt/instruction hash, tenant/user);
never cache multi-turn follow-ups against a single-turn key
(https://docs.promptise.com/core/cache/,
https://balazscsorba.com/blog/semantic-caching-llm-responses,
https://github.com/redis/agent-skills/blob/main/skills/redis-semantic-cache/SKILL.md).

### 1.5 Mitigations worth building

- Two-stage lookup: vector recall + a cheap deterministic or small-model
  verifier before serving. In one published eval, threshold-only precision
  0.688 -> threshold+guard precision 1.000 at unchanged recall
  (https://dev.to/michael_maurice/7-prompts-your-semantic-cache-should-never-answer-from-memory-net-10-239b);
  GPTCache ships an LlmVerifier post-recall check for the same purpose
  (https://github.com/zilliztech/GPTCache/blob/main/examples/README.md).
- Instrument false positives BEFORE shipping: sample cached responses
  against fresh generations; a false hit returns 200 OK with no error signal,
  so hit-rate-only monitoring is blind
  (https://vercel.com/i/semantic-caching-llm-apps).
- Log and monitor user rejections of cached responses
  (https://redis.io/blog/what-is-semantic-caching/).

### 1.6 Sources consulted (Section 1)

- https://aclanthology.org/2023.nlposs-1.24.pdf (GPTCache NLPOS paper)
- https://arxiv.org/html/2411.05276v3 (GPT Semantic Cache; 0.8 empirical sweep)
- https://arxiv.org/html/2502.03771v3 (vCache; static-threshold critique)
- https://www.sunnypatel.net/semantic-cache-reliability-sunny-patel.pdf (false-hit measurement + calibration)
- https://arxiv.org/html/2403.02694v1 (MeanCache; F-score threshold selection)
- https://docs.aws.amazon.com/wellarchitected/latest/agentic-ai-lens/agentcost02-bp03.html
- https://github.com/redis/agent-skills/blob/main/skills/redis-semantic-cache/SKILL.md
- https://redis.io/blog/what-is-semantic-caching/
- https://vercel.com/i/semantic-caching-llm-apps
- https://www.scaler.com/topics/semantic-caching-for-llms/
- https://balazscsorba.com/blog/semantic-caching-llm-responses
- https://dev.to/michael_maurice/7-prompts-your-semantic-cache-should-never-answer-from-memory-net-10-239b
- https://docs.promptise.com/core/cache/
- https://github.com/zilliztech/GPTCache
- https://github.com/zilliztech/GPTCache/discussions/577
- https://zilliz.com/blog/Yet-another-cache-but-for-ChatGPT
- https://arxiv.org/html/2603.03301v1

---

## Section 2: llama-server `--cache-reuse` semantics (consumed by 03-cache-reuse-config.md)

### 2.1 What the integer means

`--cache-reuse N` sets `n_cache_reuse`: the **minimum chunk size (in
tokens) the server will attempt to reuse from a previous request's KV cache
via KV position shifting**. Default is 0, which disables the feature.
Upstream help text: "min chunk size to attempt reusing from the cache via KV
shifting, requires prompt caching to be enabled (default: 0)"
(https://github.com/ggml-org/llama.cpp/blob/HEAD/tools/server/README.md,
https://manpages.debian.org/unstable/llama.cpp-tools/llama-server.1.en.html).
The request-level equivalent is the `n_cache_reuse` JSON field on
/completion, hard-limited to 0..INT32_MAX
(https://github.com/ggml-org/llama.cpp/blob/0cea3622/tools/server/server-schema.cpp).

Mechanism (introduced in PR 9866): after the common prefix is reused the
normal way, the server walks the remainder of the cached tokens looking for
RUNS of at least N tokens that reappear later in the new prompt; matching
runs are repositioned with `llama_kv_cache_seq_add()` (seq_rm + seq_add)
instead of being re-evaluated. Chunks containing control/special tokens are
not reused. Reuse only happens for requests with `"cache_prompt": true`
(https://github.com/ggml-org/llama.cpp/pull/9866).

Worked example from the PR (cached prompt
`aaaaabbbbbcccccccdddddeeeeeeffggggg...`, new prompt
`aaaaaccccccceeeeeeffhhhhhhh...`):
- `--cache-reuse 0`: only the `aaaaa` prefix is reused (plain
  `--cache-prompt` behavior).
- `--cache-reuse 3`: `aaaaaccccccceeeeee` is reused.
- `--cache-reuse 1`: everything up to the first genuinely new token is
  reused (https://github.com/ggml-org/llama.cpp/pull/9866).

### 2.2 Interaction with `--cache-prompt`

`--cache-prompt` (prompt caching) is **default-on** in llama-server; the
server keeps the previous request's KV cache in its slot and reuses the
shared prefix positionally -- tokens can only be reused at identical
positions, from the first divergence onward everything is recomputed.
`--cache-reuse` requires prompt caching to be enabled and SOFTENS the
positional rule: instead of giving up at the first divergence, runs of >= N
tokens found elsewhere in the cache are shifted into position. It is the
flag for prompts that differ in the middle rather than only at the end
(https://github.com/ggml-org/llama.cpp/blob/HEAD/tools/server/README.md,
https://multigrid.ai/learn/llamacpp-prompt-caching,
https://github.com/ggml-org/llama.cpp/discussions/29590).

Two related knobs, NOT to be confused with this one:
- `-sps / --slot-prompt-similarity` (default 0.10): controls which SLOT a
  new request is routed to based on prompt similarity, not what gets reused
  within a slot (https://github.com/ggml-org/llama.cpp/blob/HEAD/tools/server/README.md,
  https://github.com/ggml-org/llama.cpp/discussions/13606).
- `n_keep` / `n_discard` / `--context-shift`: parameterize the automatic
  shift when the context fills up, not on-demand chunk reuse
  (https://github.com/ggml-org/llama.cpp/discussions/29590).

### 2.3 Is 256 a sane default?

**Yes -- recommend shipping `--cache-reuse 256` as pinned.** Reasoning:

- The value trades reuse breadth against KV-shift cost. N=1 maximizes reuse
  but triggers shifting for tiny fragments, where the bookkeeping
  (seq_rm/seq_add and re-deriving positions) can outweigh the few saved
  prefill tokens; large N only fires on long verbatim runs. 256 tokens is a
  conservative chunk that pays for itself whenever it fires
  (https://github.com/ggml-org/llama.cpp/pull/9866,
  https://multigrid.ai/learn/llamacpp-prompt-caching). [The cost asymmetry
  is UNVERIFIED as a measured number in current sources; it follows from the
  mechanism description -- label: engineering judgment grounded in the
  mechanism, not a benchmark.]
- llama-server community tutorial material treats the flag as opt-in with a
  chunk-style value; there is no upstream-recommended non-zero default
  (default remains 0/off)
  (https://github.com/ggml-org/llama.cpp/blob/HEAD/tools/server/README.md,
  https://github.com/ggml-org/llama.cpp/discussions/13606).
- It is safe: a too-high value degrades gracefully to plain prefix reuse
  (`--cache-prompt` still covers the common prefix), so the failure mode of
  a wrong value is "no benefit", not "wrong answers" -- unlike the semantic
  cache threshold in Section 1.

Note for leaf 03: per-request override exists (`"n_cache_reuse": N` in the
/completion body), so config can pin the conservative default and experiments
can raise/lower it without re-spawning
(https://github.com/ggml-org/llama.cpp/blob/0cea3622/tools/server/server-schema.cpp).
The repo's daemon teardown path (`POST /slots/0?action=erase`) clears the
slot cache on session archive/idle -- cache-reuse benefit is bounded to the
window before teardown fires.

### 2.4 Sources consulted (Section 2)

- https://github.com/ggml-org/llama.cpp/pull/9866 (introducing PR, semantics + example)
- https://github.com/ggml-org/llama.cpp/blob/HEAD/tools/server/README.md (flag reference)
- https://github.com/ggml-org/llama.cpp/blob/0cea3622/tools/server/server-schema.cpp (n_cache_reuse request field)
- https://github.com/ggml-org/llama.cpp/discussions/29590 (KV shifting explained)
- https://github.com/ggml-org/llama.cpp/discussions/13606 (KV cache reuse tutorial)
- https://github.com/ggml-org/llama.cpp/pull/13230 (help-text card)
- https://multigrid.ai/learn/llamacpp-prompt-caching (prefix vs chunk reuse)
- https://manpages.debian.org/unstable/llama.cpp-tools/llama-server.1.en.html (man page)

---

## Section 3: TTFT measurement conventions (consumed by 04-ttft-metrics.md)

### 3.1 Definition

**TTFT = elapsed time from request submission to receipt of the first output
token**, encompassing queueing delay, prompt (prefill) processing, and
network latency (https://developer.nvidia.com/blog/llm-benchmarking-fundamental-concepts/,
https://docs.nvidia.com/nim/benchmarking/llm/metrics,
https://arxiv.org/html/2407.07000v1). For meep, leaf 04's planned convention
-- timestamp the first content delta after request start -- is consistent with
the NVIDIA definition with one refinement (3.3 below on empty/reasoning
deltas).

Streaming vs non-streaming: in a streaming call TTFT is request -> first
byte/chunk; in a NON-streaming call no TTFT is observable -- the single HTTP
response time is end-to-end latency only. Leaf 04 must measure TTFT on
streaming requests; non-streaming calls should record end-to-end latency
into llm_calls.latency_ms (already exists) and skip TTFT
(https://maxoperf.com/docs/academy/ai-testing/streaming-and-token-latency-testing/,
https://docs.nvidia.com/nim/benchmarking/llm/metrics).

### 3.2 Clock and units

- **Clock:** client-side monotonic clock. Start the clock when the request
  leaves the client; measure from the client because that includes network
  and queue time, "which is what a user feels" -- and SAY which side you
  measured from (https://llmdojo.dev/learn/inference-optimization/measuring-inference/measuring-a-stream).
  In Go, `time.Now()`/`time.Since()` carries a monotonic reading and is the
  right primitive; wall-clock `time.Now().Unix*` arithmetic is not.
- **Units:** record milliseconds (int64 ms is the repo convention in
  llm_calls.latency_ms). Sub-ms precision is noise at LLM latencies.
- **Report percentiles, not averages:** an average TTFT of 400 ms can
  coexist with p99 of 8 s under load; p50/p95/p99 are the reporting
  convention (https://benchmark.llmnet.nl/en/time-to-first-token-meten-onder-varierende-serverbelasting,
  https://arxiv.org/html/2407.07000v1).
- Record each token arrival time (t1..tN from t0); TTFT = t1 - t0, and the
  same record yields TPOT/ITL/e2e afterwards
  (https://llmdojo.dev/learn/inference-optimization/measuring-inference/measuring-a-stream).

### 3.3 Pitfalls (the leaf requires >= 2; here are 5)

1. **Empty content chunks:** disregard initial stream events that carry no
   content (role-only deltas, empty-string deltas); the TTFT measurement is
   meaningless when the first response chunk contains no token -- this is
   NVIDIA's stated convention for GenAI-Perf/AIPerf
   (https://developer.nvidia.com/blog/llm-benchmarking-fundamental-concepts/,
   https://docs.nvidia.com/nim/benchmarking/llm/metrics).
2. **Reasoning deltas:** the mirror-image bug -- tools that only watch
   `delta.content` silently skip `delta.reasoning` chunks and overstate TTFT
   for reasoning models (guidellm had exactly this bug; TTFT "artificially
   larger"). Decide the policy explicitly: for meep the leaf-04 convention
   ("first content delta") is acceptable, but the doc must state that
   reasoning-first models will show longer TTFT under it, or count
   reasoning deltas as first token if the metric's purpose is user
   responsiveness (https://github.com/vllm-project/guidellm/issues/737).
3. **Counting prompt-queue time unknowingly:** TTFT includes client-side
   send + server queue + prefill; under concurrency, queue time becomes the
   DOMINANT component, so TTFT numbers taken at different load levels are
   not comparable, and TTFT scales with prompt length (linear to
   quadratic) -- never publish a static TTFT SLO without pinning load and
   prompt length (https://arxiv.org/html/2407.07000v1,
   https://benchmark.llmnet.nl/en/time-to-first-token-meten-onder-varierende-serverbelasting).
4. **Client-side measurement bias:** single-process async benchmark clients
   inflate TTFT/TPOT at rate because latency is recorded when the token is
   PROCESSED rather than when it arrived on the network; for load testing,
   timestamp on arrival, not after client processing
   (https://arxiv.org/html/2605.24217v2).
5. **No warm-up:** the first call pays one-off costs (weight load, kernel
   compilation); run untimed warm-up requests before recording TTFT, and
   read the token back to the client before stopping the clock (async
   hardware returns before the work is done)
   (https://llmdojo.dev/learn/inference-optimization/measuring-inference/measuring-a-stream).

Cached-response interaction (bridges Section 1): a semantic/exact cache hit
produces no model tokens; leaf 04 should record cache-hit turns with
latency but no TTFT (or a dedicated cache-hit latency metric), otherwise
cache hits pollute the TTFT distribution
(https://docs.nvidia.com/nim/benchmarking/llm/metrics for the
"meaningless when no token present" principle).

### 3.4 Sources consulted (Section 3)

- https://developer.nvidia.com/blog/llm-benchmarking-fundamental-concepts/
- https://docs.nvidia.com/nim/benchmarking/llm/metrics
- https://arxiv.org/html/2407.07000v1 (Metron)
- https://arxiv.org/html/2605.24217v2 (measurement-bias paper)
- https://github.com/vllm-project/guidellm/issues/737 (reasoning-delta TTFT bug)
- https://maxoperf.com/docs/academy/ai-testing/streaming-and-token-latency-testing/
- https://llmdojo.dev/learn/inference-optimization/measuring-inference/measuring-a-stream
- https://benchmark.llmnet.nl/en/time-to-first-token-meten-onder-varierende-serverbelasting

---

## Summary of actionable outputs for the orchestrator

1. **Threshold verdict:** recommend 0.95 (not 0.92) as the C4 semantic-cache
   default, with mandatory tool-call bypass, per-(model, instructions,
   tenant) partitioning, and re-calibration on embedding-model change. This
   supersedes master.md's 0.92 via dated addendum.
2. **`--cache-reuse 256`: confirmed sane.** Integer = minimum token-run
   length for KV-shift chunk reuse; default 0 = off; requires
   `--cache-prompt` (default-on). Per-request override via
   `n_cache_reuse`. Ship 256.
3. **TTFT convention for leaf 04:** client-side monotonic clock, ms units,
   first CONTENT delta (skip role/empty deltas; document the
   reasoning-delta caveat), streaming requests only, percentile reporting,
   warm-up before benchmarking, cache-hit turns excluded from TTFT.
