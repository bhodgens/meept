# Agent-Side Context Engineering — Techniques, Evidence, What meept Lacks

Research date: 2026-10-12. Full leg-3 report:
/Users/caimlas/.hermes/cache/delegation/subagent-summary-2-20261012_215433_051581.txt
(transcript: .../task-2.log). All numbers quoted from the cited sources.

## What meept ALREADY has (verified against source)

- LLM-summarizing compaction (context_compressor.go Stage 2 ≈ OpenHands
  LLMSummarizingCondenser / Claude Code auto-compact)
- Token-budget triggering + keep-recent (context_compactor.go ReserveTokens/
  KeepRecentTokens ≈ RecentEventsCondenser keep-first/keep-tail)
- Untrusted-summary wrapping (context firewall) — beyond most production agents
- Per-turn atomic snapshots (HALO-style recovery ≈ PreCompact/PostCompact
  checkpoints, built-in)
- Session digest reuse for the intent analyzer
- RLM analyzer (HALO-modeled) for trace analysis
- Parallel subagents returning condensed results (planner) ≈ Anthropic
  sub-agent architecture — the 1-2K-token summary-return contract is where the
  10-50x compression lives

## Ranked techniques meept does NOT have

### 1. KV-cache-aligned append-only context discipline — S — 30-60% of cost
Manus (manus.im/blog): input:output ratio ~100:1 in agent loops → prefill
dominates; cached vs uncached input is a 10x price difference. Rules: prompt
prefix stable, never mutate past messages/observations in place, deterministic
serialization, mask tools rather than removing them (removal invalidates cache
AND breaks consistency of past tool calls). Meept gap: any per-turn mutation of
past messages (truncation/redaction) silently breaks provider prefix caching;
tool-list determinism is unasserted.

### 2. Restorable compression + intent pinning + thrash guard — M — 30-60%
- Manus "file system as context": "irreversible compression is fundamentally
  risky" — drop content only when a pointer (URL/path) + re-fetch tool remain.
- Claude Code failure taxonomy (issues #4517 instruction amnesia, #23776 intent
  inversion, #10948 mid-task derailment, #52390 thrash): pin the user's goal
  VERBATIM outside the summarized region every turn; thrash guard = defer
  compaction when context refills within 3 turns of the last compact; keep
  failures + stack traces in context (model needs them to shift priors).
- arXiv:2609.29875: reasoning blocks are the safest drop once derived state is
  externalized (risk 67.3% vs 36.2% when it is not) — externalize first,
  then compress; 25.5%/33.3% input/cache-read cut at maintained reward.

### 3. Code-mode tool chaining — L — >60% on tool-heavy flows
Anthropic code-execution-with-MCP / Cloudflare Code Mode: tools become generated
stubs; the model writes sandboxed code chaining calls; intermediates stay in the
sandbox. Measured 150K→2K tokens (98.7%); replications 78-94%; quality-neutral.
Go: goja (MIT) or starlark-go (Apache-2.0). Largest lever, largest build; meept
already has execution backends for the sandbox.

### 4. Agent-initiated event-sourced condensation — M — 30-60%
OpenHands SDK (arXiv:2511.03690, MIT): condenser = pure function
condense(events)→view; condensation stored AS an event (forgotten IDs + summary
+ offset) so history is always recoverable; agent triggers condensation via a
tool call (CondensationAction, PR #9097); token-based triggers. Evidence:
per-turn cost < HALF baseline; SWE-bench Verified 54% vs 53%; V1 cut
system-attributable failures 61% in 15 days of production. AgentFold
(arXiv:2510.24699): agent-emitted folding beats threshold triggers (sub-linear
context growth: <20K tokens over 500 turns). Maps onto meept's turn snapshots.

### 5. Sleep-time compute — M — 30-60% amortized
Letta + Berkeley (arXiv:2504.13171): background reorganization of memory between
turns, cheap model for chat + strong model for background. ~5x test-time compute
reduction at equal accuracy; +13-18% accuracy at similar budget; up to 2.5x
lower amortized cost/query. Meept's scheduler/worker + distill pipeline is the
host: distill completed plan phases, refresh facts, pre-stage next-turn context.

## In reserve (library-grade compressors)
- LLMLingua-2 (MIT, arXiv:2403.12968): 110M-355M encoder, ONNX-exportable,
  CPU-viable, 0.4-0.5s/doc on V100; best for verbose tool/RAG payloads. BLOCK or
  per-item use only — token-level pruning of agent history corrupts structured
  tool calls; and it destroys prefix byte-matching wherever it rewrites.
- ACON (arXiv:2510.00615): learn a compression GUIDELINE; small-LM folders retain
  >95% of GPT-4.1 teacher; peak usage −26-54% while IMPROVING success. Cheap
  adoption: better summarizer prompt for meept's compactor.
- FOCUS (arXiv:2609.37590, training-free): counterfactual-utility scoring; peak
  context −48%, success +8.9pp. StateComp (arXiv:2609.27298): learned
  KEEP/READY router; 52% token reduction.
- Temporal KGs (Zep/Graphiti, Apache-2.0): LongMemEval-s: 115K→1.6K tokens
  (~72x) with +15-19% accuracy and ~90% latency reduction; needs a graph DB —
  effort L, keep as design reference for meept's facts layer.

## Measured baselines for comparison
- Manus: 10x cached-vs-uncached price ratio; ~100:1 input:output.
- OpenHands: <½ per-turn cost, no quality loss; GAIA 64.24% vs 51.16% (Versa
  with browsing condensation).
- Claude Code compact: reserve ~33K tokens → fires ~83% of window; survives:
  system prompt, CLAUDE.md re-injection from disk, 5 recent files; loses:
  chat-only decisions, tool outputs, nested rules (documented amnesia issues).
- Cautionary: arXiv:2609.32961 (~35k runs): fewer tokens ≠ faster — one policy
  used ~1/3 the tokens and ran 20-80% LONGER. Measure e2e (meept-bench/
  internal/eval), tune per model/workload.
