# KV-Cache & Prompt-Prefix Reuse for Local Inference (llama.cpp + MLX)

Research date: October 2026. Target stack: single-user Go agent daemon (meept) on Apple
M2 Ultra 64GB unified memory; llama.cpp `llama-server` primary (LFM2.5-8B GGUF Q4_K_M,
OpenAI-compatible endpoint on 127.0.0.1:8080), mlx-lm secondary. Current spawn command
has NO cache flags; config comments claim "llama.cpp builds a fresh cache per call" —
that claim is wrong and this report documents what actually happens.

---

## 1. llama.cpp llama-server: prefix caching flags and features

### 1.1 The baseline everyone gets for free: `cache_prompt` (default ON)

- **What it does.** llama-server keeps the KV state of finished requests per slot and,
  on the next request, computes the longest common prefix (LCP) between the new prompt
  tokens and the slot's cached tokens; the shared prefix's prefill is skipped entirely.
  `"cache_prompt": true` in the request body is the explicit switch; it has been the
  default for years (`--cache-prompt` / `--no-cache-prompt` server flag, default
  enabled, env `LLAMA_ARG_CACHE_PROMPT`).
- **Exact config:** server default on; request field `"cache_prompt": true`
  (explicit recommended).
- **Savings mechanism:** tokens prefill avoided = length of the common prefix. On a
  65536-token context that grows by ~1–2k tokens per agent turn, a hit saves re-prefill
  of everything but the new turn — typically 90 %+ of prompt compute.
- **Verdict:** production, works today, no action needed — BUT the current meept spawn
  line relies on it implicitly; verify hit behavior in server logs
  (`prompt processing progress, n_past = ..., progress = 0.02` style lines).
- **License:** MIT (llama.cpp).
- Sources: https://github.com/ggml-org/llama.cpp/blob/refs/heads/master/tools/server/README.md
  https://github.com/ggml-org/llama.cpp/discussions/13606
  https://github.com/ggml-org/llama.cpp/issues/8947 (system-prompt caching discussion)

### 1.2 `--cache-reuse N` — non-prefix chunk reuse via KV shifting

- **What it does.** With a positive N, the server attempts to reuse KV chunks of size
  >= N that match at non-prefix positions, shifting them into place
  (`llama_kv_cache_seq_add`) and skipping their prefill. `--cache-reuse 0` = prefix-only
  (default). N=512 is the value the PR author demonstrated; larger N = more conservative.
  Only requests with `cache_prompt: true` participate.
- **Exact flag:** `--cache-reuse N` (default 0), requires prompt caching enabled; env
  `LLAMA_ARG_CACHE_REUSE`.
- **Version:** merged Sep/Oct 2024 (PR #9866, available well before build floor b9660).
- **Savings:** recovers prefill on reordered/edited prompts (tool-result reordering,
  mid-history edits) that pure prefix matching misses. For strictly append-only agent
  histories the gain over 1.1 is small; for compaction/summarization rewrites it matters.
- **Verdict:** production-usable; safe with N=256–512. Quality caveat: reusing small
  chunks can place tokens in slightly different contexts (the author's own caveat), so
  prefer N >= 256 for agent workloads.
- **License:** MIT.
- Sources: https://github.com/ggerganov/llama.cpp/pull/9866
  Debian manpage (current semantics):
  https://manpages.debian.org/testing/llama.cpp-tools/llama-server.1.en.html

### 1.3 Slots, `-np`, and `-sps` (slot-prompt-similarity)

- **What they do.** `-np N` creates N server slots; each slot keeps its own hot KV
  state, so N distinct conversation prefixes can stay cached simultaneously. New
  requests are routed to slots by prompt similarity: `-sps` (slot-prompt-similarity,
  default 0.1 in current builds; 0.5 historically — check your build's `--help`) is the
  minimum LCP fraction for a slot to be considered a match. `-sps 0.0` disables auto
  selection; requests can pin a slot with `"id_slot"`.
- **Key facts for single-user:** with one agent session, `-np 1` (or the auto 4-slot
  default) is fine; slots exist for *prefix multiplicity*, not speed — tokens/sec is
  unchanged (llama.cpp discussion #15530). Slots do NOT share cache between each other;
  two slots sharing a 50k-token prefix each hold their own copy.
- **Version note:** default `-sps` changed from 0.5 to 0.1 in recent builds (tutorial
  #13606 says 0.5; the current Debian manpage of a 2026 build says default 0.1). Verify
  with `llama-server --help` on the actual binary — meept's build floor is b9660.
- **Verdict:** production. For meept: default slots are fine; consider `-np 2` only if
  you want a second session's prefix hot (costs KV memory).
- **License:** MIT.
- Sources: https://github.com/ggml-org/llama.cpp/discussions/13606
  https://github.com/ggml-org/llama.cpp/discussions/15530
  https://manpages.debian.org/testing/llama.cpp-tools/llama-server.1.en.html

### 1.4 `--kv-unified` / `--no-kv-unified` and `--kv-unified-per-slot`

- **What it does.** A single KV buffer shared across all sequences/slots instead of
  per-slot partitioning. Current default: **enabled when the slot count is auto**
  (`-np` default -1 = auto) — PR #16736 ("server: support unified cache across slots")
  made unified KV + 4 auto slots the default; `-c` then becomes one shared pool rather
  than being divided per slot. PR #14363 introduced the opposite mode (`kv_split`,
  per-sequence streams) as a high-throughput multi-sequence optimization that requires
  `ggml_set_rows` backend coverage; on Apple Silicon Metal the set_rows support gates it.
- **Exact flags:** `-kvu, --kv-unified` / `-no-kvu` (env `LLAMA_ARG_KV_UNIFIED`);
  `--kv-unified-per-slot N` sizes the shared pool as n_parallel*N when `-c` is unset.
- **Savings:** not a prefill saver by itself; it changes memory shape (one pool sized
  `-c` total instead of `-c` per slot) and is **required for `--cache-idle-slots`**
  (see 1.5) and recommended for `--cache-ram` prompt caching to behave as designed.
- **Verdict:** production; on recent builds you already have it by default (auto
  slots). If meept ever passes an explicit `-np`, also pass `--kv-unified` explicitly,
  because with explicit slots the context is split per slot otherwise.
- **License:** MIT.
- Sources: https://github.com/ggml-org/llama.cpp/blob/refs/heads/master/tools/server/README.md
  https://github.com/ggml-org/llama.cpp/pull/16736
  https://github.com/ggml-org/llama.cpp/pull/14363
  https://github.com/ggml-org/llama.cpp/pull/17997

### 1.5 `--cache-ram` + `--cache-idle-slots` — host-memory prompt cache (the big one)

- **What it does.** PR #16391 (Oct 2025) added a host-RAM prompt cache: when a slot
  goes idle, its serialized KV state is saved into a RAM cache ("extra slots"); when a
  new request's prefix matches a cached prompt better than any hot slot, the state is
  hot-swapped back into the context and prefill resumes from there. This is what makes
  *multiple* sessions or a session evicted by another job resume without full re-prefill.
  `--cache-idle-slots` (default enabled, requires `--cache-ram` and unified KV) saves
  idle slots into this cache on each new task.
- **Exact flags:** `-cram, --cache-ram N` MiB (default 8192, -1 = unlimited, 0 =
  disable); `--cache-idle-slots` / `--no-cache-idle-slots` (default enabled; env
  `LLAMA_ARG_CACHE_IDLE_SLOTS`). Also token-count limit = `--ctx-size` by default.
- **Version:** #16391 merged Oct 2025; available in any build meept can use (floor
  b9660 is much later, and LFM2.5 tool-call parser support is 2026-era, so fine).
- **Apple Silicon caveat (important for 64GB M2 Ultra):** on macOS the "host RAM" is
  the same unified pool as the Metal allocation. The default 8 GiB prompt cache
  competes with model+KV for unified memory; the writeup at jessequinn.info documents
  KV-eviction errors from this competition and the known crash with some quantized-KV +
  Metal + cache-ram combos (issue #23578, May 2026). With 64GB and an 8B Q4 model
  (~5GB weights + ~4-8GB KV at 65k ctx f16), 8192 MiB cache-ram is affordable, but if
  you also quantize KV (1.7) or see `GGML_ASSERT(buf_dst)` crashes, drop it to 4096 or
  set 0 and rely on slots alone.
- **Savings:** PR discussion reports near-93 % TTFT reduction on cache-hit restores;
  mechanism = tokens prefill avoided (state deserialized instead of recomputed).
- **Verdict:** production for single-user; the strongest lever meept isn't using. Caveat:
  a 2026 bug report (#26293) shows rerank/embedding-type tasks can pollute the cache;
  for chat-completion-only meept usage this does not apply.
- **License:** MIT.
- Sources: https://github.com/ggml-org/llama.cpp/pull/16391
  https://github.com/ggml-org/llama.cpp/blob/72d693e4/tools/server/server-context.cpp
  https://jessequinn.info/blog/llama-cpp-cache-ram-prompt-caching
  https://github.com/ggml-org/llama.cpp/issues/26293

### 1.6 `--prompt-cache` (llama-cli only) and `/slots` save/restore

- **`--prompt-cache FNAME` / `--prompt-cache-all` / `--prompt-cache-ro` are llama-cli
  flags; llama-server silently ignores them** (confirmed in issues #9135, #12437,
  discussion #8947; a PR was opened explicitly to disable them server-side). The
  config-comment fix for meept should NOT be "add --prompt-cache".
- The server-side equivalent for *persistent* (cross-restart) cache is the slot state
  API: start with `--slot-save-path DIR`, then
  `POST /slots/{id}?action=save|restore|erase` with `{"filename": "..."}`. This is
  manual, per-slot, and version-sensitive (some releases show `n_written: 0` bugs) —
  treat as best-effort, not core.
- **Verdict:** `--prompt-cache` for the server = not supported (do not add it);
  `/slots` save/restore = optional belt-and-braces for warm restarts, production-usable
  but low value for a daemon that stays up.
- **License:** MIT.
- Sources: https://github.com/ggml-org/llama.cpp/issues/9135
  https://github.com/ggml-org/llama.cpp/issues/12437
  https://github.com/ggml-org/llama.cpp/discussions/8947
  https://github.com/ggerganov/llama.cpp/tree/master/examples/server

### 1.7 KV quantization: `-ctk/-ctv` (a.k.a. `--cache-type-k/v`)

- **What it does.** Stores the K and/or V cache quantized: allowed values f32, f16,
  bf16, q8_0, q4_0, q4_1, iq4_nl, q5_0, q5_1 (default f16). q8_0 halves KV memory
  (~2 bytes/elem -> ~1.06 BPV-ish overhead, roughly 50 % KV savings) with perplexity
  deltas on the order of 1e-3 (PR #2969 data). Enabling quantized KV turns on Flash
  Attention automatically where required.
- **Exact flags:** `-ctk, --cache-type-k TYPE`, `-ctv, --cache-type-v TYPE`; draft
  variants `-ctkd/-ctvd`. Env: `LLAMA_ARG_CACHE_TYPE_K/V`.
- **Caveats:** quantized KV + FA had historical quality/perf cliffs on some
  architectures (Qwen3 + CPU KleidiAI gibberish, issue #23044; "uniform Q4_0/Q8_0 KV
  breaks specific architectures" per #21385). On Metal/M2 Ultra with LFM2.5-8B the
  q8_0/q8_0 combination is the standard safe choice, but verify output quality on your
  model before trusting it (compare a few known completions with f16 vs q8_0).
- **Savings:** KV memory halved (e.g. ~65k ctx on an 8B GQA model drops from roughly
  8 GiB to ~4 GiB), which buys either longer context or more cache-ram headroom in the
  64GB pool. Prefill-savings unchanged (orthogonal to prefix caching).
- **Verdict:** production-usable, test-before-trust per model. q8_0 both axes is the
  conservative pick; q4_0-class KV is where quality risk lives.
- **License:** MIT.
- Sources: manpage above (allowed values), https://github.com/ggerganov/llama.cpp/pull/2969
  https://github.com/ggml-org/llama.cpp/issues/23044

### 1.8 Defragmentation

- `-dt, --defrag-thold N` ("KV cache defragmentation threshold") is **deprecated** in
  current builds (manpage, 2026). Defrag still happens internally; the knob is no
  longer user-tunable. Do not build config around it.
- Sources: https://manpages.debian.org/testing/llama.cpp-tools/llama-server.1.en.html

### 1.9 SWA models caveat (not LFM2.5, but note it)

For iSWA models (Gemma 2/3 etc.), cache reuse silently degrades unless
`--swa-full` is set (stores full-window cache, more memory) — issues #15082, PR #13194.
LFM2.5 is not SWA in this sense, so meept is unaffected; flag it if the model catalog
ever adds a Gemma-class model.

### 1.10 What this means for meept concretely

Current: `["llama-server","--model",...,"--jinja","--reasoning-budget","0","--ctx-size","65536"]`
This already gets 1.1 (default prompt caching with auto unified KV). The config comment
"llama.cpp builds a fresh cache per call" is factually wrong — each call reuses the
hot slot's prefix automatically. Recommended spawn for a single-user agent:

```
llama-server --model <LFM2.5-8B> --jinja --reasoning-budget 0 \
  --ctx-size 65536 \
  --cache-reuse 256 \
  --cache-ram 8192 \
  --kv-unified \
  -ctk q8_0 -ctv q8_0        # test quality first; drop if outputs drift
```

plus prompt-shaping discipline from section 2 (this is where most of the win lives —
server flags only preserve cache that the daemon's prompt builder doesn't bust).

---

## 2. What breaks prefix-cache hits in practice (and how a daemon must shape prompts)

### 2.1 The contract

llama.cpp (like all prefix caches) matches on the exact token sequence from position 0.
Any divergence at position i invalidates reuse of everything from i onward. There is no
block-level cache in llama.cpp — the LCP is computed over the whole rendered prompt
(after chat-template rendering with `--jinja`).

### 2.2 Cache busters, ranked by real-world damage

1. **Tool-list instability.** With `--jinja`, tool definitions are rendered into the
   prompt BEFORE the system message (chat-template render order). Adding/removing/
   reordering tools, or any nondeterministic serialization of schemas (map iteration
   order in Go!), changes token 0..N and busts the entire cache every call. This is the
   single most expensive bug class for an agent daemon. (Anthropic docs: tools render
   first and any tool change invalidates everything after; MCP `tools/list` gives no
   ordering guarantee, so clients must sort.)
2. **System-prompt mutation.** Timestamps ("current date: ..."), per-request UUIDs,
   session IDs, model-mode strings, user names interpolated into the system prompt.
   Even a second-precision timestamp busts the whole session cache each call (Manus
   blog calls this out explicitly; Anthropic and Claude Code docs list it among top
   observed breakers).
3. **Message reordering / mid-history edits.** Any daemon feature that rewrites
   history (compaction inserting a summary at the top, pruning a tool result,
   re-ordering tool-result blocks) invalidates from the edit point on. Appending is
   the only safe operation.
4. **Whitespace/punctuation drift in the template.** Chat-template version changes
   (llama.cpp updates built-in Jinja templates between releases) change the rendered
   prefix bytes; after a llama.cpp upgrade the cache is effectively cold once. Pin the
   template (`--chat-template-file`) if byte-stability across upgrades matters.
5. **Thinking/reasoning stripping.** With reasoning models, if response N's reasoning
   is excluded from request N+1 but the SWA/ubatch boundary lands mid-reasoning, the
   match degrades (llama.cpp #15082 discussion). meept runs `--reasoning-budget 0`, so
   minimal exposure — keep it that way for LFM2.5.

### 2.3 The daemon-side rules (KV-cache-aligned prompting, Anthropic/Manus-style,
applied to llama.cpp)

- **Render order = stability order:** tools (deterministically sorted by name, schemas
  serialized with sorted keys — in Go, sort map keys or use ordered structs) → system
  prompt (frozen bytes; NO timestamps, NO per-session data) → retrieved docs/memory
  (per-session, stable within a session) → conversation history (append-only) →
  current turn (volatile data like "now" belongs here, at the END).
- **Session-scoped toolsets, not per-request toolsets.** If tools differ per phase,
  keep ONE static list for the session and gate callability in the daemon; never edit
  the rendered tool array mid-session. When MCP servers come online, merge and sort
  their tools ONCE at session start; don't re-emit the list per turn.
- **Forked calls (summarizer, memory extraction, classifier) must copy the parent's
  exact tools+system prefix if you want them to hit the parent's cache** — or accept
  that they rebuild cold but keep them on a separate slot so they don't evict the main
  session's state (this is what `-np 2` + `--cache-ram` buys you).
- **Compaction must preserve bytes:** summary appended as a NEW message, old messages
  retained verbatim up to a boundary; never "replace the system prompt with a summary."
- **Verification signal:** llama-server logs `n_past`/`progress` per request (and
  `/slots` endpoint exposes prompt state; `LLAMA_SERVER_SLOTS_DEBUG=1` adds prompt
  dumps). A healthy single-user agent session should show `progress` near 0.0x on every
  turn after the first. Wire an assertion/metric on it in meept (equivalent of
  Anthropic's cache_read_input_tokens alerting, which Claude Code treats as SEV-worthy).
- Sources: https://github.com/anthropics/skills/blob/main/skills/claude-api/shared/prompt-caching.md
  https://mnemoverse.com/docs/library/prompt-cache-stable-prefix
  https://agentpatterns.ai/context-engineering/prompt-caching-architectural-discipline/
  (Manus "KV-cache hit rate is the single most important metric for a production AI
  agent" is quoted across these), llama.cpp #15082 for the reasoning-token caveat.

---

## 3. MLX: prefix caching in mlx-lm and mlx_lm.server

**Yes — mlx-lm has real cross-request prefix caching, arguably more knobs than
llama-server.**

- **In-memory LRU prompt cache (server default).** `mlx_lm.server` keeps an
  `LRUPromptCache` (mlx_lm/models/cache.py): per model key, it stores completed
  request KV states keyed by token sequence; on each request it calls
  `fetch_nearest_cache(model_key, prompt)` and prefill resumes at the matched prefix
  (`prompt_cache_count = len(prompt) - len(rest)`). Flags: `--prompt-cache-size N`
  (max distinct KV caches, default 10) and `--prompt-cache-bytes SIZE` (byte budget;
  the server trims the LRU to fit). This is automatic, on by default, and works across
  requests.
- **Persistent disk cache:** `--prompt-cache-dir DIR` (PR #1405, 2025/2026) saves the
  LRU to safetensors + manifest.json at exit/SIGTERM and restores matching-model
  entries at boot — survives restarts. Per-request disk caches: `prompt_cache_file`
  in chat/completion request bodies (PR #1283) loads/refreshes an isolated
  request-local cache; `disable_prompt_cache` opts a request out.
- **KV quantization:** `--kv-bits {4,8}` (with `--kv-group-size`, `--quantized-kv-start`)
  quantizes KV cache during generation — big memory savings for long contexts; note it
  disables batching (requests served serially), which for a single-user agent is a
  non-issue.
- **CLI path:** `mlx_lm.generate --prompt-cache-file F` exists for the non-server flow
  (system-prompt prewarm); the server equivalent is the dir/per-request mechanisms above
  (issue #1178 documents the original gap and the resolution).
- **Mechanism parity note:** mlx-lm's cache is a token-sequence-keyed LRU of full KV
  states (closest to llama-server's `--cache-ram` behavior, minus the chunk-shifting of
  `--cache-reuse`); it does prefix matching, not arbitrary chunk reuse.
- **Savings:** same mechanism as llama.cpp — prefill avoided for the matched prefix;
  LFM2.5-8B MLX at 65k ctx would otherwise re-prefill the full history each turn
  (mlx-lm historically has NO server-side reuse without these features).
- **Verdict:** production (the in-memory LRU has shipped for a long time; disk
  persistence is recent — pin a mlx-lm release and test). License: MIT (mlx-lm;
  MLX itself MIT, Apple).
- Sources: https://github.com/ml-explore/mlx-lm/blob/main/mlx_lm/server.py
  https://github.com/ml-explore/mlx-lm/pull/1283
  https://github.com/ml-explore/mlx-lm/pull/1405
  https://github.com/ml-explore/mlx-lm/issues/1178
  https://github.com/ml-explore/mlx-examples/pull/1026 (original server prompt cache)

---

## 4. KV-cache offload / compression landscape (what's production vs research)

| Project | What it does | Production-ready for THIS stack? | License | URL |
|---|---|---|---|---|
| **llama.cpp `-ctk/-ctv q8_0`** | In-engine KV quantization, ~50 % KV memory, ~1e-3 ppl delta | YES (test per model) | MIT | see 1.7 |
| **llama.cpp `--cache-ram`** | Host-RAM KV state offload for idle slots (on Apple Silicon: unified memory) | YES, tuned (see 1.5 caveat) | MIT | #16391 |
| **LMCache** | KV cache management layer for vLLM/SGLang-class engines: tiered GPU→CPU→disk/S3 offload, cross-engine reuse, CacheBlend non-prefix reuse | NO for meept (needs vLLM/SGLang; no llama.cpp/Metal path; GPU/CUDA-centric) | Apache-2.0 | https://github.com/LMCache/LMCache (verified live, ~12k stars, PyTorch Foundation 2025) |
| **KVPress (NVIDIA)** | Training-free KV compression "presses" (SnapKV/KNorm/ExpectedAttention etc.) at prefill via HF transformers hooks; also decoding-time compression | NO for production serving — it's a transformers-pipeline research library, no server integration; useful as a benchmark/oracle for compression quality | Apache-2.0 | https://github.com/NVIDIA/kvpress (verified live) |
| **CacheGen** | KV cache streaming codec (SIGCOMM'24): compress + stream cached KV over network | Research-grade; upstream says "check out LMCache for latest integration" — the standalone repo is dormant (169 stars, 7 open issues, minimal commits since 2024) | No LICENSE file visible on repo page (verify before any reuse) | https://github.com/UChi-JCL/CacheGen (verified live but effectively archived in favor of LMCache) |

- Note on licenses: Apache-2.0/MIT are fine for the Go daemon as separate subprocess
  dependencies; none of LMCache/KVPress/CacheGen are linkable into meept anyway (Python
  stacks), and none currently support llama.cpp GGUF or MLX backends.
- Practical read for a 64GB M2 Ultra: compression research (KVPress-style eviction,
  CacheGen streaming) targets VRAM-starved or network-distributed serving. With 64GB
  unified memory and an 8B model, meept's constraint is not KV capacity — it's prefill
  time. Spend effort on sections 1–3, not on compression research.

## 5. Design references: how the big three solve it

- **vLLM — Automatic Prefix Caching (APC):** KV cache is stored in fixed-size blocks
  (PagedAttention); each block's hash chains from its prefix, so any shared prefix of
  blocks is reused regardless of which request produced it. Enabled with
  `enable_prefix_caching=True` (default ON in recent vLLM V1); `--prefix-caching-hash-algo`
  picks sha256/cbor/xxhash. Simplest design; block granularity; eviction via the
  standard block allocator. https://docs.vllm.ai/en/stable/design/prefix_caching/
  https://docs.vllm.ai/en/stable/features/automatic_prefix_caching/
- **SGLang — RadixAttention:** retains KV of prompts AND generations in a radix tree
  keyed by token sequences; longest-prefix match against the tree, LRU leaf eviction
  (plus newer lfu/slru/priority/tlru policies — tlru is explicitly designed for
  agentic multi-turn tail-TTFT). Always-on, cache-aware scheduling, in-batch prefix
  sharing. The richest design; the radix-tree + tlru policy is the most directly
  relevant design reference for an agent-focused server.
  https://www.lmsys.org/blog/2024-01-17-sglang/
  https://docs.sglang.io/docs/advanced_features/radix_eviction_policy
- **LMCache:** moves the problem one layer up — a standalone KV management daemon
  decoupled from the engine: tiered offload (GPU→CPU→disk→S3/Redis), cross-instance
  and cross-engine reuse, CacheBlend for non-prefix (RAG-interleaved) reuse with
  selective recompute. Apache-2.0. Design reference for "cache survives engine
  restarts/crashes," which llama-server approximates with `/slots` save and mlx-lm
  with `--prompt-cache-dir`.
  https://github.com/LMCache/LMCache

Takeaway for llama.cpp: it solves the same problem with slot-local LCP + (optional)
host-RAM state swaps — closer to vLLM APC in spirit (prefix-only unless
`--cache-reuse`), with no radix-tree cross-slot sharing: prefixes are NOT shared
between slots, only between a slot and the RAM cache.

---

## Ranked top-5 for THIS stack (llama.cpp primary, MLX secondary, single-user, 64GB)

1. **Turn on and protect default prefix caching + shape prompts for it (llama.cpp,
   free).** Keep `cache_prompt` default; freeze tools+system prefix bytes in meept's
   prompt builder (sorted deterministic tool serialization, no timestamps in system),
   append-only history. Expected: 90 %+ prefill avoided per turn on 65k contexts.
   Cost: zero. This is the highest-leverage change and it's mostly daemon code, not flags.
2. **Add `--cache-ram 8192` (+ default unified KV / `--kv-unified` if pinning `-np`)**
   to the llama-server spawn: idle-slot KV states survive session switches and slot
   evictions in RAM; ~93 % TTFT reduction on restores (PR #16391). Watch unified-memory
   pressure; drop to 4096 if Metal allocation errors appear.
3. **Add `--cache-reuse 256`**: cheap insurance that recovers prefill after
   compaction/edits/reordering that prefix matching alone would miss.
4. **KV quantization `-ctk q8_0 -ctv q8_0` (after a quality A/B on LFM2.5-8B)**: frees
   ~half the KV footprint (~4GB at 65k ctx), buying cache-ram headroom and longer
   context in the 64GB pool. Skip if any output-quality drift appears.
5. **MLX secondary: rely on mlx-lm's automatic LRU prompt cache
   (`--prompt-cache-size`, `--prompt-cache-bytes`), add `--prompt-cache-dir` for
   restart persistence, and `--kv-bits 8` if long contexts crowd memory** — accept
   serial serving (fine for single-user).

(Runners-up, not top-5: `/slots` save/restore for warm daemon restarts; `-np 2` if a
second concurrent session type ever needs its own hot prefix; `--chat-template-file`
pinning to survive llama.cpp template drift.)

Corrections to meept config comments: "llama.cpp builds a fresh cache per call" is
wrong — llama-server reuses the hot slot's prefix by default; the real risks are (a)
prompt-builder cache-busting (section 2) and (b) slot eviction, which `--cache-ram`
addresses. `--prompt-cache` is llama-cli-only and must NOT be added to the server
spawn line.
