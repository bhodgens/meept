# Inference-Stack Caching (llama.cpp / MLX) and the Proxy/Sidecar Verdict

Research date: 2026-10-12. Verified against llama.cpp master README/PRs, Debian
manpages of 2026 builds, mlx-lm source, and live repo pages (no cached extracts
cited). Full leg-1 report: .hermes/research-kv-cache-report.md (transcript:
/Users/caimlas/.hermes/cache/delegation/live/deleg_0c5086a2/task-0.log).

## 1. llama.cpp llama-server — what is TRUE today

- **`--cache-prompt` is ON by default.** Auto unified KV buffer, ~4 auto slots,
  slot selection by longest-common-prefix similarity. meept's config comment
  ("llama.cpp builds a fresh cache per call", config/models.json5:316) is
  outdated — the hot slot already reuses the session prefix. CORRECT THE COMMENT.
- `--cache-reuse 256`: KV-shifted non-prefix CHUNK reuse (≥256-token chunks,
  PR #9866). Recovers prefill after mid-history edits, compaction rewrites, and
  reordering. THE flag meept is missing.
- `--cache-ram N` (+ `--cache-idle-slots`): host-RAM prompt-state cache; idle-slot
  KV states hot-swap from RAM across session switches/evictions. ~93% TTFT
  reduction on restores (PR #16391). On Apple-Silicon unified memory, watch
  pressure — start 4096, raise if no Metal allocation errors.
- `--prompt-cache` is llama-cli-only and SILENTLY IGNORED by the server — do not
  add it.
- KV quantization `-ctk q8_0 -ctv q8_0`: halves KV memory (~4GB freed at 64k ctx
  for the 8B), ~1e-3 ppl delta claimed — needs a per-model quality A/B on
  LFM2.5-8B before adopting.
- `-dt/--defrag-thold` is deprecated. iSWA models would need `--swa-full`
  (LFM2.5 is not iSWA).
- Hit-rate telemetry: server logs n_past / cache reuse per request — parse these
  for a per-session prefix-hit-rate metric.

## 2. What breaks prefix hits (ranked for the daemon)

1. Tool-list instability — rendered BEFORE system under `--jinja`; any
   nondeterministic tool order/bytes busts the whole cache from token 0.
2. Timestamps/IDs in the system prompt (one volatile byte kills from that point).
3. Mid-history edits — compaction rewrites, truncation. (llama.cpp issue #19494:
   Claude Code's per-request billing header forced FULL re-prefill every turn
   until stripped — volatile bytes anywhere early are fatal.)
4. Chat-template byte drift across llama.cpp upgrades (pin builds).
Countermeasures meept already has: AssembleOrdered stable prefix
(internal/agent/prompt.go:461-485), drift hash logging (loop.go:8134-8139),
Go's deterministic JSON. Remaining: assert tool-list serialization determinism,
append-only history discipline (mask/keep tools rather than removing them
mid-session), and live n_past telemetry.

## 3. MLX (mlx-lm.server)

- Automatic in-memory LRU prompt cache across requests
  (`--prompt-cache-size` / `--prompt-cache-bytes`).
- `--prompt-cache-dir`: disk persistence across restarts (PR #1405).
- Per-request `prompt_cache_file` (PR #1283) for explicit control.
- `--kv-bits 8`: KV quantization; disables batching — acceptable single-user.

## 4. Proxy/sidecar layer — verdicts

- **LiteLLM: SKIP for meept.** Locally: llama-server ignores cache_control;
  LiteLLM response caching duplicates meept's L1/L2 with a worse whole-request
  hash key; its semantic cache is documented-bad for agent traffic; its
  context_management polyfills (clear_tool_uses_20250919, compact_20260112) and
  compress() would rewrite prompts in a Python hot path, destroying the
  byte-stable prefixes llama-server needs (LiteLLM's own Headroom guardrail
  refuses to touch cache_control-marked content for this reason). Cloud: the
  differentiating features are each ~30-50 lines of Go in internal/llm
  (cache_control placement, prompt_cache_key, tool-result clearing). MIT.
- **LMCache (Apache-2.0, alive, PyTorch Foundation): not applicable** — engine
  connectors for vLLM/SGLang only; cannot serve llama.cpp. (RFC #1949 for
  engine-agnostic access is in progress.)
- **GPTCache (MIT): stagnant** ("no longer add support for new API or models");
  semantic response caching is the wrong primitive for agent traffic. Skip.
- **caching.ai (Apache-2.0 self-host)**: provider prompt-cache hit-rate proxy —
  auto cache_control injection, cache-breaker detection, keepalive warmers
  (measured: only Anthropic benefits from pings). Worth borrowing the
  `cache-guard` CI idea: hash the cacheable prefix of request fixtures so a PR
  that destabilizes the prefix fails CI.
- **MCP tool middleware (the one genuinely useful sidecar shape — stdio, no hot
  path):** mcp-diet (MIT, Go) tool-schema pruning (50-78% off schema tokens,
  order-preserving so local caching survives); Tooner (MIT, Go) TOON-encoding of
  tool responses (~40% off JSON tokens). Wire via mcp_servers.json5 command
  substitution — or port the logic into internal/tools natively.
- Design references: vLLM APC (chained block hashes), SGLang RadixAttention
  (radix tree + agent-tail-aware tlru eviction), LMCache (tiered KV offload).

## Bottom line for the GPU fleet

The highest-value move is not a new process — it is (1) `--cache-reuse 256` +
(2) byte-stable prefixes + (3) n_past telemetry, all config/small-code. A sidecar
buys nothing here; keep any middleware stdio-wrapped and out of the hot path.
