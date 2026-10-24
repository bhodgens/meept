# Context Reduction for meept — Landscape, Current State, and Proposals

Research date: 2026-10-12. Method: 5 parallel research legs — (1) llama.cpp/MLX
server-side caching, (2) proxy/sidecar middleware incl. LiteLLM, (3) agent-side
context engineering, (4) IndexRAG paper deep-read (arXiv:2603.16415), (5)
source-verified audit of meept's current context handling.

Companion docs:
- `02-inference-stack-and-proxy.md` — llama.cpp/MLX flags, sidecar/proxy verdict
- `03-indexrag-integration.md` — arXiv:2603.16415 mapped onto internal/memory
- `04-agent-side-techniques.md` — production + academic techniques, 2025-2026

## CRITICAL CORRECTION to prior meept config comments

`config/models.json5:316` says "llama.cpp builds a fresh cache per call". This is
WRONG. llama-server has `--cache-prompt` **enabled by default**, auto unified KV,
and longest-common-prefix slot selection (verified against llama.cpp master README
+ tools/server docs, Oct 2026). The hot slot already reuses the session prefix.
The real loss paths are: slot eviction on session switch, mid-prefix edits, and
non-prefix reuse after compaction. Fix the comment; do not "fix" caching that
already exists. (Detail: 02-inference-stack-and-proxy.md §1.)

## What meept already has (source-verified)

| Capability | Location | State |
|---|---|---|
| Stable-prefix system prompt assembly (static-first) | internal/agent/prompt.go:461-485 `AssembleOrdered` | ON by default; drift logged (loop.go:8134-8139) |
| Prompt-cache boundary sentinel + CacheScope | internal/llm/prompt_cache.go:9-60 | Anthropic-wire-only (anthropic.go:1183-1191); stripped for local (client.go:460-465) |
| Anthropic cache_control emission | internal/llm/anthropic.go:880,1183-1191 | Works; classification re-splits on "\n\n" (blank-line fragility, :1184) |
| Overflow compactor (LLM summarize + structural prune) | internal/llm/context_compactor.go (defaults :25-35) | **OFF by default** (schema.go:3657); trigger ratio 0.60 |
| Stage-based proactive compressor (warn→summarize→aggressive→hard) | internal/llm/context_compressor.go:53-56 | **OFF by default** (`proactive_compression: false`, schema.go:2993) |
| Context firewall (summaries wrapped as untrusted) | internal/llm/context_firewall.go:15-26,1099-1104 | ON; but `<<<USER_INPUT>>>`/`<<<TOOL_OUTPUT:*>>>` markers are never emitted anywhere |
| HALO-style turn snapshots + TurnCompactor | internal/agent/turn_compaction.go:244-247 | TurnCompactor used only inside RLM analyzer, not chat path |
| L1/L2 response cache | internal/llm/token_cache*.go | **Disabled by default**; metrics store is nil in production wiring (components.go:770) |
| Memory injection budget | MaxMemoryContextTokens=2000 (conversation.go:33), top-5 recall (loop.go:7059), 300-rune truncation (loop.go:7562) | Budget cut is proportional mid-string (loop.go:7163-7168) — mangles an item in half |
| Memory: dual-store, distill, consolidation, clustering, vector store (flat cosine, SQLite blobs) | internal/memory/ | Distill pipeline has grammar-constrained generation + evidence-ID + dedupe — good host for IndexRAG |

## Gaps ranked by leverage (from source audit)

1. llama-server spawned without `--cache-reuse` (config/models.json5:311); no
   per-request cache fields in the OpenAI payload builder (client.go:480-538). S.
2. Response cache can never hit in agent loops: key hashes the FULL message list
   (client.go:558 + cache_key_builder.go:190-196); prefix-aware key
   (cache_key_builder.go:271-279) is test-only. M.
3. Compaction summarizer hardwired to loop's own model (loop.go:2466-2468); the
   `[compaction] model` config key (schema.go:566) is read by NOTHING. S.
4. Token-cache metrics never recorded in stock deployments (components.go:770 nil
   metrics store). S.
5. Anthropic boundary classification blank-line-fragile (anthropic.go:1184);
   Session-scope block gets no cache_control. S-M.
6. Untrusted markers documented but never emitted (context_firewall.go:19,51). M.
7. Memory budget cut is item-mangling (loop.go:7165-7168). S.
8. Cache-alignment telemetry unused: llm_calls already stores tokens_cached /
   cache_creation_tokens (internal/metrics/store.go:254-256); stable-prefix drift
   is debug-only. S.

## PROPOSALS (ranked; effort S/M/L; savings class)

### P1 — Turn on the free wins in the local stack (S, ~1 day) — GPU prefill ↓↓
- config/models.json5: add `--cache-reuse 256` to the llama-server spawn command
  (recovers prefill after compaction/edits via KV-shifted chunk reuse, llama.cpp
  PR #9866); correct the stale "fresh cache per call" comment.
- Optionally `--cache-ram 8192` (host-RAM prompt state cache, PR #16391; watch
  unified-memory pressure on the 64GB M2 Ultra; start 4096).
- Wire llama-server `n_past`/`progress` log parsing into metrics → a real
  prefix-hit-rate number per session. Verify the default-cache behavior with a
  live probe before/after.
- Savings class: 30-60% of prefill GPU per turn in multi-turn sessions; TTFT ↓
  sharply after compaction events.

### P2 — Make the response cache work and be visible (S-M, 1-2 days) — repeat work avoided
- Wire `BuildPrefixAwareKey` (cache_key_builder.go:271) into the live client
  Get/Put path (client.go:558) so dynamic-stable re-asks hit L1/L2.
- Pass the metrics store into the cache coordinator (components.go:770) —
  cache.hit/miss/eviction become dashboards.
- Enable `[llm.cache] enabled` by default? No — keep opt-in; measure first (P1
  telemetry), then decide. meept rule: opt-in defaults.
- Savings class: 10-30% on repeated/identical requests (evals, retries, TUI
  re-asks).

### P3 — Compaction done right: cheap summarizer + enable + guardrails (M, 2-4 days)
- Wire the dead `[compaction] model` key (schema.go:566) so summarization runs on
  a small local alias (lfm-combined-sft MLX or the 8B GGUF), not the loop's
  frontier model. ~30 lines. Cost of compaction drops to ~free.
- Turn compaction ON as the default path is a product decision — keep opt-in;
  but the pieces are all present.
- Port Claude Code's proven failure mitigations (04-agent-side-techniques.md §1.3):
  (a) pin the user goal/intent VERBATIM outside the summarized region each turn;
  (b) thrash guard — defer compaction if context refills within 3 turns of the
  last one; (c) re-inject standing rules from disk instead of summarizing them.
- Savings class: 30-60% of long-session context; prevents the documented
  post-compaction intent-inversion failures (claude-code #23776, #4517).

### P4 — Provider cache alignment in the Go layer, no sidecar (S-M, 1-2 days) — cloud tokens ↓↓↓
- Anthropic: use BuildSystemPromptBlocks end-to-end instead of re-splitting on
  "\n\n" (anthropic.go:1184); emit cache_control on tools + system + last
  message; consider the new top-level automatic cache_control. Breakpoints:
  tools (stable), system (stable), trailing turn (advances).
- OpenAI: set per-session `prompt_cache_key` (routing hint); nothing else needed
  (auto caching ≥1024 tokens).
- DeepSeek/Gemini: mark NOTHING (automatic/implicit); keep prefixes byte-stable.
- Deterministic tool serialization check: Go maps sort keys, but audit any
  tool-list assembly for order stability — tool-list instability is the #1
  prefix-cache buster (rendered before system under --jinja).
- Savings class: 10x cost ratio cached-vs-uncached (Manus numbers); 30-60%
  overall on cloud turns.

### P5 — Item-intact memory budgeting + untrusted-marker emission (S-M, 1-2 days) — quality + safety
- Replace the proportional mid-string memory cut (loop.go:7163-7168) with
  whole-item drops from the lowest-ranked item until budget fits. Often saves
  MORE tokens and never leaves a mangled half-memory.
- Emit `<<<TOOL_OUTPUT:id>>>` / `<<<USER_INPUT>>>` wrappers at the tool-result
  seam so the firewall's trust contract is real (context_firewall.go:19,51 —
  currently documented-only). Gives summarizers a lossless refolding point.
- Savings class: indirect — enables safer aggressive compression; removes a
  correctness bug (trust classification by prompt politeness).

### P6 — Agent-initiated, event-sourced condensation (M, ~1 week) — 30-60%
OpenHands model (arXiv:2511.03690, MIT): condensers as pure functions
`condense(events) -> view`; the condensation (forgotten IDs + summary + offset) is
stored as an event so nothing is ever lost; the agent can trigger condensation
via a tool call when a subtask completes (agent-emitted folding beats
threshold-triggered — AgentFold, arXiv:2510.24699). Maps cleanly onto meept's
existing append-only turn snapshots. Evidence: per-turn cost < HALF baseline,
SWE-bench Verified 54% vs 53% (no quality loss).

### P7 — Restorable compression: file-system-as-context (M) — 30-60%, enables P3/P6 aggressiveness
Manus rule (manus.im/blog): "irreversible compression is fundamentally risky" —
drop a tool output's CONTENT only when a pointer (path/URL) + a re-fetch tool
remain. Meept already writes files; extend tool-result pruning
(context_compactor.go:766-777 pruneToolOutputs) to spill full outputs to
`$MEEPT_HOME/spill/` and leave a path + re-read affordance. Keep failures in
context (stack traces let the model shift priors). Pair with "When Can Agents
Forget Their Reasoning?" (arXiv:2609.29875): reasoning blocks are the safest
thing to drop once derived state is externalized (25.5%/33.3% input/cache-read
cut at maintained reward).

### P8 — Code-mode tool chaining (L, multi-week) — >60% on tool-heavy flows
Anthropic code-execution-with-MCP / Cloudflare Code Mode pattern: expose tools as
generated stubs; the model writes sandboxed code (goja MIT / starlark-go
Apache-2.0) that chains calls; intermediates stay in the sandbox, only final
values enter context. Measured: 150K→2K tokens (98.7%); independent replications
78-94%; quality-neutral. Largest single lever for tool-heavy agent flows, but the
largest build. Sandbox + timeout discipline required (meept has execution
backends already).

### P9 — Sleep-time compute on the existing scheduler (M, ~1 week) — 30-60% amortized
Letta + UC Berkeley (arXiv:2504.13171): background agent reorganizes memory
between turns with a cheap model; gains correlate with query predictability.
Meept's scheduler/worker + distill/consolidation pipeline is the natural host:
distill completed plan phases, refresh facts, pre-stage next-turn context during
idle time. Numbers: ~5x test-time compute reduction at equal accuracy; up to 2.5x
lower amortized cost.

### P10 — IndexRAG bridging facts in the Consolidator — CONDITIONAL GO (see 03)
Precompute cross-memory bridging facts offline; balanced selection caps derived
facts at k_b=2 in recall. Improves cross-session multi-hop recall at the SAME
context budget; real token savings only if recall currently over-retrieves to
compensate for multi-hop misses. Conditions: offline-only, provenance +
cascade-invalidation, entailment gate for the 8B generator, eval before default.

### Explicitly rejected
- LiteLLM sidecar: buys nothing locally (llama-server ignores cache_control and
  has its own prefix cache; LiteLLM's polyfills would rewrite prompts in a Python
  hot path, breaking byte-stable prefixes) and little on cloud vs ~30-50 lines of
  Go per feature. Full verdict: 02-inference-stack-and-proxy.md §4.
- LMCache: vLLM/SGLang engines only; cannot serve llama.cpp. GPTCache:
  maintenance mode; semantic response caching is unsafe for agent traffic
  (LiteLLM's own docs say so). Semantic caches generally: wrong primitive for
  nondeterministic tool context.
- sqlite-vec for memory: internal/memory/vector/store.go flat cosine in Go is
  single-digit ms at meept's scale (thousands-tens-of-thousands of entries).
  Revisit at ~10^6 units.

## Cautions (from the cautionary literature)
- "Beyond Token Savings" (arXiv:2609.32961, ~35k runs on SWE-bench
  Verified/Terminal-Bench): fewer tokens ≠ faster/cheaper — one policy used ~1/3
  the tokens and ran 20-80% LONGER. Every proposal here must be measured e2e
  (meept has internal/eval + meept-bench for this).
- Consensus of the 2025-2026 compression line: compress at BLOCK/SPAN level,
  never token level, for agent history (token-level pruning corrupts structured
  tool calls). LLMLingua-2 (MIT) is the exception to keep in reserve for verbose
  tool/RAG payloads — as per-item compression, never on prefixes.
- Anthropic itself: "overly aggressive compaction can result in the loss of
  subtle but critical context whose importance only becomes apparent later."

## Source index (primary)
- llama.cpp: github.com/ggml-org/llama.cpp (tools/server README; PRs #16391, #9866;
  issue #19494 volatile-prefix incident)
- LiteLLM: docs.litellm.ai (context_management, prompt_caching, headroom, caching)
- LMCache: github.com/LMCache/LMCache (Apache-2.0) — GPTCache: zilliztech/GPTCache (MIT, stagnant)
- Anthropic: anthropic.com/engineering/effective-context-engineering-for-ai-agents;
  docs.anthropic.com prompt caching + context editing
- Manus: manus.im/blog/Context-Engineering-for-AI-Agents-Lessons-from-Building-Manus
- Claude Code: dotfiles.pro auto-compact analysis; issues #52390 #4517 #23776
- OpenHands: arXiv:2511.03690; docs.openhands.dev sdk/arch/condenser.md (MIT)
- Letta: arXiv:2504.13171 (Apache-2.0) — Zep/Graphiti: arXiv:2501.13956 (Apache-2.0)
- LLMLingua-2: arXiv:2403.12968 (MIT) — RECOMP: arXiv:2310.04408 — Selective-Context: arXiv:2310.06201
- ACON: arXiv:2510.00615 — AgentFold: arXiv:2510.24699 — FOCUS: arXiv:2609.37590
- StateComp: arXiv:2609.27298 — reasoning-forgetting: arXiv:2609.29875 — GEM: arXiv:2609.27332
- Cautionary: arXiv:2609.32961 — IndexRAG: arXiv:2603.16415
- meept source audit: full report at
  /Users/caimlas/.hermes/cache/delegation/subagent-summary-4-20261012_215433_053619.txt
