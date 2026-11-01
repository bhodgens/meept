# Findings: LLM Context-Window Budget Allocation

> Research leaf 00-research-budgeting (tree 20261031-context-memory-engine,
> group 01-context-assembler). Status: PINNED. Date: 2026-11-01.
> Sibling leaves (01-allocator-core, 02-doc-budgets, 03-integration) MUST
> cite this doc plus an R-Ax ID in their Notes sections for every
> fraction, floor, cap, reserve, and compaction trigger they implement.
>
> Provenance: every factual claim carries a source URL. Sources are public
> provider documentation, one primary research paper, and production
> engineering write-ups; no claim rests on this agent's memory.

## Budget Allocation Practices

The consensus across sources: treat the context window as a budget to be
allocated across named components, not a container to be filled.

1. **Partition the window explicitly per component.** AWS Well-Architected
   (Agentic AI Lens) directs every agent to "an explicit token budget
   allocated across prompt components -- system instructions, conversation
   history, retrieved knowledge, and tool schemas -- with per-component"
   attribution instrumented on every request, because treating the prompt
   as one opaque string makes runaway growth impossible to attribute.
   https://docs.aws.amazon.com/wellarchitected/latest/agentic-ai-lens/agentperf03-bp02.html

2. **Allocate in priority order and enforce at assembly time.** The
   Frontier Engineering context-budgeting pattern assigns each component
   (system, user turn, tools, retrieved, history, scratch) a token budget
   plus a priority and a component-appropriate reduction callable; assembly
   is a single function that "either produces a prompt within budget or
   reports precisely what it had to drop." System instructions and the
   current user turn are priority 1 (never dropped); tools are next
   ("a missing tool silently removes a capability"); retrieved context and
   history compete for what remains and both have graceful reductions
   (fewer chunks / summarize oldest). Silent truncation "is how systems
   degrade invisibly for weeks."
   https://www.frontierengg.com/learn/patterns/context-memory/context-budgeting

3. **Reserve output and a safety margin below the nominal window.** The
   same pattern reserves 8,000 tokens for output plus a 4,000-token safety
   margin on a 128,000 window (~6.25% output reserve, ~3% safety margin):
   "Reserve output tokens and a safety margin below the nominal window."
   The ml4devs budgeting guide agrees the reserve must be "the largest
   output the deliverable can legitimately need, not the average one" --
   an average-sized reserve "will look fine for weeks and then stop
   mid-sentence on the one input that mattered."
   https://www.frontierengg.com/learn/patterns/context-memory/context-budgeting
   https://www.ml4devs.com/articles/llm-context-window-budget-and-trimming/

4. **Order by attention and cacheability: stable content first,
   decision-relevant content last.** Models "attend unevenly across a long
   context: material at the very beginning and very end is used more
   reliably than material buried in the middle" (LongLLMLingua reports the
   same position-bias finding and exploits it for compression). Put stable
   instructions at the top (also making them cacheable) and the current
   task plus most relevant retrieved context at the bottom, closest to
   where generation begins.
   https://www.frontierengg.com/learn/patterns/context-memory/context-budgeting
   https://aclanthology.org/2024.acl-long.91/

5. **MemGPT partitions the prompt into three contiguous, ordered regions.**
   The canonical academic treatment splits main context (the prompt) into:
   system instructions (read-only, static), working context (fixed-size
   read/write block for key facts, written only via function calls), and a
   FIFO queue (rolling message history whose first slot holds a recursive
   summary of evicted messages), backed by external tiers (recall storage,
   archival storage). This is the published precedent for meept's
   static-section / dynamic-section split in
   internal/agent/prompt.go AssembleOrdered.
   https://arxiv.org/abs/2310.08560

6. **Addaly's worked example allocates a 200K window with explicit
   per-section numbers**: system 1,200; tools 1,500; examples 1,000;
   retrieved 6,000 ("~8-12 chunks after reranking"); history 2,000
   (trimmed, oldest dropped first) -- small fixed sections, retrieved and
   history getting the flexible remainder, history trimmed oldest-first.
   https://addaly.com/learn/context-engineering/the-window-is-a-budget

7. **Dynamic tool schemas, not full-catalog injection.** AWS recommends
   replacing full tool-catalog injection with just-in-time, task-relevant
   tool selection, and keeping each schema compact -- directly relevant to
   meept's ToolResultMaxTokens=3000 static, which the allocator supersedes
   per-section rather than globally.
   https://docs.aws.amazon.com/wellarchitected/latest/agentic-ai-lens/agentperf03-bp02.html

Contrast with current meept statics (all verified in-repo): budgeting
today is per-limit, not per-section -- IterationTokenBudget=30000,
ToolResultMaxTokens=3000, DefaultConversationTokenBudget=50000, warn at
0.80 (internal/agent/loop.go:4164-4182); DefaultMaxMessages=200 and
MaxMemoryContextTokens=2000 (internal/agent/conversation.go:20-34);
ratio-derived budgets with a 0.7 multiplier under SmallModelContextThreshold
(internal/llm/context_firewall.go:547-571). None of these derive from the
resolved context window; the allocator (leaf 01) replaces them with
window-derived per-section budgets.

## Stable-Prefix and KV-Cache Preservation

Cache mechanics make byte-identical prompt prefixes a first-class
allocator constraint, not an optimization afterthought.

1. **Anthropic prompt caching is prefix-hash based; any change at or before
   the breakpoint invalidates everything.** Cache writes occur only at
   `cache_control` breakpoints and store "a hash of the prefix ending at
   that block"; because the hash is cumulative, "changing any block at or
   before the breakpoint produces a different hash on the next request."
   The prefix order is `tools` -> `system` -> `messages`; a change at one
   level invalidates that level and everything after it (e.g. modifying
   tool definitions invalidates the entire cache). Cache reads cost 10% of
   base input price; writes 25% extra (5-minute TTL; 1-hour TTL available
   at higher cost). Automatic caching moves the breakpoint to the last
   cacheable block as conversations grow; the backward lookback window is
   20 blocks.
   https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching
   https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-use-with-prompt-caching

2. **OpenAI prefix caching requires the ENTIRE rendered prefix to match,
   and it stores KV tensors, not tokens.** "The prompt cache stores
   key-value (KV) tensors"; "Cache reuse requires the entire rendered
   prefix to match. If content or a relevant setting changes before a
   breakpoint, the prefix after that change cannot match the existing
   cache entry." Minimum cacheable prefix is 1,024 tokens; hits accrue in
   128-token increments. Cached input is discounted up to 95%; in-memory
   caches are typically cleared after 5-10 minutes idle and removed within
   one hour (older models), with extended caching up to 24h; GPT-5.6+
   default write TTL is 30 minutes with a 12-hour option. Requests are
   routed by a hash of the first ~256 tokens; a stable `prompt_cache_key`
   raised one customer's hit rate from 60% to 87%. Time-to-first-token
   improves up to 80%.
   https://developers.openai.com/api/docs/guides/prompt-caching
   https://openai.com/index/api-prompt-caching/
   https://developers.openai.com/cookbook/examples/prompt_caching_201

3. **Byte-drift failure modes are stated explicitly by the providers
   themselves.** OpenAI's guidance: "Keep the prefix stable. Put stable
   developer instructions and shared reference material first. If
   developer instructions or shared material contain timestamps,
   user-specific content, or other dynamic content, place those at the end
   rather than the beginning"; and "Append new messages rather than
   rewriting earlier turns. Summarization, compaction, or context
   truncation can change the prefix and reset cache reuse." Anthropic's
   equivalent: place the breakpoint "at the end of the static prefix, not
   on the varying block" when the suffix varies (timestamps, per-request
   context). Drift sources to design against: embedded timestamps or
   per-request nonces in the system prompt, reordered sections,
   re-serialized tool schemas, mid-conversation tool-choice changes, and
   any compaction that rewrites history in place.
   https://developers.openai.com/api/docs/guides/prompt-caching
   https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching
   https://developers.openai.com/cookbook/examples/prompt_caching_201

4. **meept already owns the right primitive; the allocator must not break
   it.** internal/agent/prompt.go AssembleOrdered emits all stable sections
   first and returns a sha256 over the exact stable-prefix bytes
   (stablePrefixHash) as a drift signal. The allocator's per-section
   budgets therefore apply ONLY to dynamic sections; stable sections are
   byte-frozen for the life of a session and grow/shrink only at session
   boundaries, where a cache miss is acceptable.

## Compaction and Summarization Policies

When the sum of sections approaches the window, the allocator needs a
reduction ladder. Four policies, each with an applicability note for meept.

P1. **Threshold-triggered whole-history compaction (MemGPT pattern).**
MemGPT inserts a "memory pressure" warning system message when prompt
tokens exceed a warning count (their example: 70% of the context window)
so the agent can park information externally, and flushes/evicts at the
flush count (their example: 100%, evicting ~50% of the window per flush,
replacing the evicted range with a recursive summary kept in the queue's
first slot). Applicability for meept: adopt the two-level trigger shape --
warn near a high-water fraction, compact at a hard fraction -- but trigger
compaction earlier than 100% since meept also reserves output tokens; the
0.80 warn ratio in loop.go is in the right neighborhood and becomes the
allocator's compaction-trigger anchor.

P2. **Compaction as summarization-plus-continuation (Anthropic/Claude Code
pattern).** Anthropic's effective-context-engineering guidance: compaction
"takes a conversation nearing the context window limit, summarizing its
contents, and reinitiating a new context window with the summary";
Claude Code preserves "architectural decisions, unresolved bugs, and
implementation details" while discarding redundant tool outputs, then
continues "with this compressed context plus the five most recently
accessed files." Tuning advice: "Start by maximizing recall ... then
iterate to improve precision by eliminating superfluous content." Claude
Code additionally caps what survives: re-reads at most five files,
re-injects skill bodies capped at 5,000 tokens per skill and 25,000 total,
oldest dropped first. Threshold-compaction exists as a first-party API
parameter (trigger at a configured token threshold). Applicability for
meept: the post-compaction re-injection list (recent files, capped
skill/memory bodies) maps onto MaxMemoryContextTokens-style memory tiers;
keep recent turns verbatim, summarize the older prefix.
https://www.anthropic.com/research/effective-context-engineering-for-ai-agents
https://code.claude.com/docs/en/context-window
https://platform.claude.com/docs/en/build-with-claude/compaction
https://platform.claude.com/docs/en/build-with-claude/compaction-threshold

P3. **Tool-result clearing before summarization (lightest-touch lever).**
Anthropic's context-editing strategy (`clear_tool_uses_20250919`) clears
the oldest tool results in chronological order once a threshold is hit,
replacing each with placeholder text while keeping the tool_use record;
"once a tool has been called deep in the message history, why would the
agent need to see the raw result again?" Tool-result clearing is
"sub-transcript" (surgical), versus compaction which is "whole-transcript"
and lossy by design. LongLLMLingua-style budget controllers allocate
different compression ratios per prompt component -- instructions and
questions get smaller ratios (compressed less) than redundant
demonstrations -- the academic formalization of "reduce each section by
its own strategy, not blanket truncation." Applicability for meept:
ToolResultMaxTokens=3000 already truncates per-result at capture time;
add an age-based clear (placeholder + keep tool_use record) as the FIRST
reduction rung, before any summarization.
https://platform.claude.com/docs/en/build-with-claude/context-editing
https://platform.claude.com/cookbook/tool-use-context-engineering-context-engineering-tools
https://aclanthology.org/2023.emnlp-main.825/

P4. **Sliding window plus running summary (AWS pattern).** "Cap raw-turn
retention and replace older turns with a running summary after the session
exceeds a threshold expressed in turns or tokens ... inject the summary
rather than the raw transcript into the prompt, paired with a small
sliding window of recent turns." Applicability for meept: this is the
shape of DefaultMaxMessages=200 -- make the message cap and the token
budget both window-derived, and pair the capped window with a recursive
summary so evicted turns stay reachable.
https://docs.aws.amazon.com/wellarchitected/latest/agentic-ai-lens/agentperf03-bp02.html

Small-context adjustment: meept's existing 0.7 multiplier under
SmallModelContextThreshold (context_firewall.go:554-556) is consistent
with the general principle that fixed sections consume a proportionally
larger share of small windows; keep the multiplier but apply it to the
flexible sections (history, retrieved), not to the output reserve, which
must stay absolute (ml4devs: reserve the largest legitimate output, not a
fraction that shrinks to nothing).
https://www.ml4devs.com/articles/llm-context-window-budget-and-trimming/

## Recommendations for the Allocator

Numbered R-A1..R-A10 for citation by sibling leaves. Numbers are concrete
defaults, each traceable to a source above; all derive from the RESOLVED
window (internal/llm/context_discovery.go context_limit), superseding the
statics in loop.go when a real window is discovered.

R-A1. Split the resolved window into six ordered sections in this exact
assembly order (stable prefix preserved, generation-relevant last):
stable system prompt, tool schemas, dynamic docs, memory tiers, history,
current user turn -- then subtract output reserve and safety margin from
the window BEFORE distributing fractions. Basis: Anthropic prefix order
tools->system->messages (S-P1); stable-first/decision-relevant-last
ordering (Budget Practice 4).

R-A2. Output reserve: reserve the LARGER of (a) 5% of the resolved window
and (b) the model-specific max output tokens if known, with an absolute
floor of 4,096 tokens; never let the reserve shrink proportionally on
small windows. Basis: 6.25% reserve in the Frontier worked example;
ml4devs "largest output the deliverable can legitimately need."

R-A3. Safety margin: hold back an additional 3% of the resolved window
(below the nominal limit, above the reserve) for tokenizer disagreement
and provider overhead -- meept's HeuristicTokenizer is 3 chars/token and
will disagree with provider counts. Basis: Frontier pattern's 4,000 on
128,000 (~3%), explicitly for "tokenizer disagreement, provider overhead."

R-A4. Section fraction starting points of the post-reserve window, subject
to R-A5 floors/caps: system+tools (stable prefix) take what they take --
they are byte-frozen, not fractioned (R-A7); of the FLEXIBLE remainder:
dynamic docs <= 35%, memory tiers capped at 5% (floor 512 tokens), history
flexible remainder with a soft cap of 60%. Basis: Addaly's allocation
shape (small fixed sections; retrieved+history take the flexible
remainder); MemGPT's fixed working-context block maps to the memory-tier
cap; LLMLingua's budget controller (smaller compression ratios for
instructions/questions, larger for redundant content) justifies giving
docs a hard cap while history flexes.

R-A5. Floors and caps: every flexible section gets a floor below which it
is not reduced (history floor: the most recent N=10 messages verbatim,
matching Claude Code's keep-recent-turns behavior) and a cap above which
overflow is reclaimed from the lowest-priority section, in reverse
priority order: dynamic docs, then history (summarize), then memory tiers
(never below floor). System prompt and current user turn are never
reduced. Basis: Frontier priority-order allocation ("system instructions
and the current turn are never dropped"; reclaim from lowest priority);
AWS per-component budgets that "sum to the input-length target."

R-A6. Instrument per-section token counts on every assembled request as
structured log fields, plus the stablePrefixHash. Alert/log when any
section trends outside its budget. Basis: AWS step 2 (per-component
attribution with cacheRead/cacheWrite tokens alongside); Frontier ("emit
per-component token gauges -- this is where cost regressions become
visible"); meept's stablePrefixHash gives the drift signal for free.

R-A7. Stable-prefix freeze: the allocator never mutates stable sections
mid-session. Timestamps, session IDs, and any per-request data go in
dynamic sections or trailing messages only. Any change to stable content
requires a session-boundary (or explicit cache-invalidation-aware) event.
Basis: OpenAI "keep the prefix stable ... place [dynamic content] at the
end"; Anthropic cumulative prefix-hash semantics (S-P1, S-P3); meept's
AssembleOrdered/stablePrefixHash contract.

R-A8. Reduction ladder before full compaction, applied in order when
assembled size exceeds the post-reserve window: (1) per-result truncation
(existing ToolResultMaxTokens behavior, window-derived instead of fixed
3,000); (2) age-based tool-result clearing -- replace results older than a
turn-distance threshold with placeholders, keep tool_use records;
(3) history sliding window -- drop oldest raw turns into a running
recursive summary; (4) whole-history compaction per P2 (summarize, keep
recent turns verbatim, re-inject memory tiers and recent file references
under the R-A4 memory cap). Basis: Anthropic context-editing ordering
("safest, lightest-touch" clearing first), P1-P4 policies.

R-A9. Compaction triggers: warn (log + skip new tool calls, existing
0.80 warn semantics retained) at 80% of (window - safety margin); full
compaction at 90% of (window - safety margin) or when the history section
exceeds its R-A4 cap and rung-3 reduction cannot free enough. Never
compact the stable prefix. After compaction, re-inject: running summary
first slot (MemGPT), most recent turns verbatim, memory tiers up to cap,
up to five most recently accessed file references (Claude Code pattern,
capped like its 5,000-token/skill rule). Basis: P1 (70% warn / flush
shape), P2 (Claude Code preserve-list), loop.go 0.80 warn.

R-A10. Small-context adjustment: when the resolved window is below meept's
SmallModelContextThreshold, scale the FLEXIBLE section budgets by the
existing 0.7 factor but keep the output reserve (R-A2) and history
verbatim-recent floor (R-A5) absolute. Basis: context_firewall.go 0.7
multiplier (existing meept behavior, retained as precedent); ml4devs
absolute-reserve principle; MemGPT demonstrates the same fixed/flexible
split working on an 8K window.

## Source Index

- S1 https://docs.aws.amazon.com/wellarchitected/latest/agentic-ai-lens/agentperf03-bp02.html (AWS Well-Architected, agentic context budgeting)
- S2 https://www.frontierengg.com/learn/patterns/context-memory/context-budgeting (Frontier Engineering, context budgeting pattern)
- S3 https://www.ml4devs.com/articles/llm-context-window-budget-and-trimming/ (ml4devs, window budget and output reserve)
- S4 https://addaly.com/learn/context-engineering/the-window-is-a-budget (Addaly, worked per-section allocation)
- S5 https://arxiv.org/abs/2310.08560 (MemGPT: Towards LLMs as Operating Systems)
- S6 https://www.letta.com/blog/memgpt-and-letta/ (MemGPT -> Letta naming/productization)
- S7 https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching (Anthropic prompt caching)
- S8 https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-use-with-prompt-caching (Anthropic tool caching + invalidation table)
- S9 https://developers.openai.com/api/docs/guides/prompt-caching (OpenAI prompt caching guide)
- S10 https://openai.com/index/api-prompt-caching/ (OpenAI prompt caching announcement)
- S11 https://developers.openai.com/cookbook/examples/prompt_caching_201 (OpenAI Prompt Caching 201 cookbook)
- S12 https://www.anthropic.com/research/effective-context-engineering-for-ai-agents (Anthropic effective context engineering)
- S13 https://code.claude.com/docs/en/context-window (Claude Code compaction survival rules)
- S14 https://platform.claude.com/docs/en/build-with-claude/compaction (Anthropic compaction overview)
- S15 https://platform.claude.com/docs/en/build-with-claude/compaction-threshold (Anthropic threshold compaction)
- S16 https://platform.claude.com/docs/en/build-with-claude/context-editing (Anthropic context editing / tool-result clearing)
- S17 https://platform.claude.com/cookbook/tool-use-context-engineering-context-engineering-tools (Anthropic cookbook: memory, compaction, tool clearing)
- S18 https://aclanthology.org/2023.emnlp-main.825/ (LLMLingua, EMNLP 2023)
- S19 https://aclanthology.org/2024.acl-long.91/ (LongLLMLingua, ACL 2024)
- S20 https://www.microsoft.com/en-us/research/project/llmlingua/ (Microsoft Research LLMLingua series)
