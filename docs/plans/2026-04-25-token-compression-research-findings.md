# Token Compression Research Findings

> **Research completed:** 2026-04-25
> **Researchers:** External techniques subagent + Internal codebase analysis subagent

## Executive Summary

**Key finding:** Meept already has a **mature Phase 1-4 context management system** with foundational compression capabilities (summarization, chunking, importance-based truncation). The system is not missing basics -- it's missing **advanced fine-grained compression**.

**Recommendation:** Implement **code-aware compression** as the highest-impact next capability. Meept already has tree-sitter infrastructure (`internal/code/ast/`) but token truncation in tool results uses naive character-position slicing. Compressing at AST node boundaries would preserve function signatures and variable names while dropping implementation details.

**Secondary recommendation:** Add **proactive compression** that triggers at 30-40% utilization rather than waiting for 80% hard limits. This prevents sharp quality drops.

**Not recommended:** Token truncation via map-reduce would be overkill for Meept's use case. The existing chunking + summarization pipeline handles long contexts adequately.

---

## Current State (Meept Implementation)

### What Exists

| Component | Location | Capability |
|-----------|----------|------------|
| **ContextFirewall** | `internal/llm/context_firewall.go` | Budget enforcement, summarization pipeline, chunking |
| **Tokenizer** | `internal/llm/tokenizer.go` | tiktoken + heuristic (3 chars/token) + caching |
| **Conversation** | `internal/agent/conversation.go` | Multi-strategy truncation, importance ranking, anchor messages |
| **Tool Result Compression** | `internal/agent/executor.go` | `ToCompressedJSON()` with character-position truncation |
| **Memory Consolidation** | `internal/memory/consolidation.go` | Date-based grouping, snippet summarization |
| **Budget Tracking** | `internal/llm/budget.go` | Hourly/daily token budgets, RPM limiting |

### Configuration Options (Already Tunable)

| Parameter | Default | Effect |
|-----------|---------|--------|
| `SummarizeHistory` | false | Enable LLM-based old message summarization |
| `ChunkLargeInputs` | false | Split oversized user messages |
| `IterationBudgetRatio` | 0.30 | Fraction of context for single turn |
| `ConversationBudgetRatio` | 0.50 | Fraction for full conversation history |
| `WrapUpThreshold` | 0.50 | Log warning at 50% utilization |
| `HardLimit` | 0.80 | Drop old context at 80% utilization |
| `DropContextOnHardLimit` | true | Enable/disable context dropping |
| `ChunkThresholdRatio` | 0.25 | Max input size relative to context limit |

### What's Working Well

1. **Token counting** -- Model-specific tokenizers with tiktoken, fallback to heuristics
2. **Importance-based eviction** -- Classifies messages by type (user input > reasoning steps)
3. **Anchor messages** -- Content-hash-indexed messages exempt from truncation
4. **Dynamic tool budget** -- Shrinks tool result budget as conversation consumes tokens
5. **Observability** -- `FirewallStats` tracks summarization failures, dropped messages

---

## Techniques Researched

### Tier 1: Immediately Applicable to Meept

#### Technique: Structured Encoding
- **Token savings:** 20-50%
- **Complexity:** Low
- **Best for:** Both code and prose
- **Readiness:** Mature (standard practice)
- **Fit for Meept:** HIGH -- Already partial implementation via tool schemas. Could expand XML-tag formatting, compact abbreviations.

#### Technique: Hierarchical Summarization
- **Token savings:** 50-80%
- **Complexity:** Low-Medium
- **Best for:** Both
- **Readiness:** Mature (LangChain, LlamaIndex)
- **Fit for Meept:** HIGH -- `summarizeOldHistory()` exists but is single-level. Add recursive summarization of summaries.

#### Technique: Code-Aware Compression (NEW)
- **Token savings:** 40-60% for code-heavy contexts
- **Complexity:** Medium (requires tree-sitter integration)
- **Best for:** Code
- **Readiness:** Custom implementation needed
- **Fit for Meept:** VERY HIGH -- `internal/code/ast/` already parses code. Truncate at AST boundaries instead of character position.

### Tier 2: Worth Evaluating

#### Technique: LLMLingua-Style Token Pruning
- **Token savings:** Up to 20x compression
- **Complexity:** Medium (requires auxiliary model)
- **Best for:** Both
- **Readiness:** Mature (Python library)
- **Fit for Meept:** MEDIUM -- No Go implementation exists. Would need to port or shell to Python.

#### Technique: RAG-Based Relevant Context Extraction
- **Token savings:** 60-90% vs. full documents
- **Complexity:** Medium
- **Best for:** Document-heavy Q&A, code search
- **Readiness:** Mature (LangChain, LlamaIndex)
- **Fit for Meept:** MEDIUM -- Memory retrieval exists, but no re-ranking layer.

#### Technique: StreamingLLM (Attention Sinks)
- **Token savings:** Constant memory regardless of sequence length
- **Complexity:** Low (no fine-tuning)
- **Best for:** Streaming multi-round dialogue
- **Readiness:** Mature (NVIDIA TensorRT-LLM, HuggingFace)
- **Fit for Meept:** LOW -- Requires model-level cache manipulation. Better for self-hosted models.

### Tier 3: Not Recommended (Research Only)

| Technique | Why Not Recommended |
|-----------|---------------------|
| **Gisting** | Requires model modification + fine-tuning |
| **AutoCompressor** | Requires model fine-tuning; not preprocessing |
| **MemGPT/Letta** | Framework lock-in; Meept has own agent system |
| **Mem0** | Redundant with existing memory system |
| **Sliding Window** | Already implemented; too lossy for complex tasks |
| **Prompt Caching (Anthropic/OpenAI)** | Cost optimization, not compression; already available via API |

---

## Gaps Analysis (What's Missing)

### Gap 1: No Code-Aware Compression [HIGHEST PRIORITY]
**Problem:** Tool results containing code are truncated by character position. A 50-token function signature might be cut mid-function, losing the name entirely.

**Impact:** Agent loses critical context (function names, variable names, structure) while keeping less important implementation details.

**Integration point:** `internal/agent/executor.go:ToCompressedJSON()` currently calls `truncateWithMarker()` at character boundaries. Replace with `truncateAtASTBoundary()` using `internal/code/ast/`.

---

### Gap 2: No Hierarchical/Recursive Summarization [HIGH PRIORITY]
**Problem:** `summarizeOldHistory()` produces a single summary. As conversations grow beyond days, this summary itself becomes too large.

**Impact:** Either information loss (aggressive dropping) or context waste (keeping too much).

**Integration point:** `internal/llm/context_firewall.go:summarizeOldHistory()` -- add recursive summarization when summary itself exceeds threshold.

---

### Gap 3: No Proactive Compression [HIGH PRIORITY]
**Problem:** All compression is reactive (triggers at 80% utilization). No background compression runs at lower thresholds.

**Impact:** Sharp quality drops when hard limits are hit. No gradual degradation.

**Integration point:** Add `ProactiveCompressionThreshold` (default 0.30-0.40) to `ContextFirewallConfig`. Run lightweight summarization in background.

---

### Gap 4: Summarization is Not Content-Aware [MEDIUM PRIORITY]
**Problem:** Old messages are concatenated as `role: content` with generic "summarize this" prompt. No differentiation between code-heavy, plan-heavy, or factual conversations.

**Impact:** Summaries miss key decisions, file paths, task state.

**Integration point:** `internal/llm/context_firewall.go:summarizeOldHistory()` -- add structured extraction before summarization (e.g., "extract decisions, file paths, unresolved questions").

---

### Gap 5: No Compression Quality Metrics [MEDIUM PRIORITY]
**Problem:** No metrics track information preservation vs. token savings. Cannot distinguish "dropped 50 irrelevant messages" from "dropped 50 critical messages."

**Impact:** Blind compression; impossible to tune quality/savings tradeoff.

**Integration point:** Add `CompressionStats` to `FirewallStats` -- pre/post token counts, estimated quality score (e.g., % of critical messages retained).

---

### Gap 6: Memory Consolidation is Naive [LOW PRIORITY]
**Problem:** `consolidateByDate()` groups only by calendar date. `SummarizeWithLLM` placeholder exists but is never used.

**Impact:** Memories consolidated without semantic clustering; loses relationships between related events.

**Integration point:** `internal/memory/consolidation.go:SummarizeWithLLM` -- wire up the placeholder to use LLM for semantic clustering.

---

## Implementation Options

### Option A: Code-Aware Compression (RECOMMENDED)

**What:** Replace character-position truncation with AST-aware truncation for code blocks.

**Changes:**
- Modify: `internal/agent/executor.go:ToCompressedJSON()` -- detect code, parse with tree-sitter
- Add: `internal/agent/compress/ast_truncator.go` -- truncate at function/statement boundaries
- Modify: `internal/agent/executor.go:truncateWithMarker()` -- call AST truncator for code

**Token savings:** ~40-60% for code-heavy tool results

**Effort:** Medium (2-3 days)

**Risk:** Low -- tree-sitter already integrated

---

### Option B: Hierarchical Summarization (RECOMMENDED)

**What:** Add recursive summarization -- summaries of summaries for very long conversations.

**Changes:**
- Modify: `internal/llm/context_firewall.go:summarizeOldHistory()` -- check if summary exceeds threshold, recursively summarize
- Add: `CompressionLevel` tracking -- track how many times summarized

**Token savings:** 70-85% for multi-day conversations

**Effort:** Low (1 day)

**Risk:** Low -- extension of existing summarization

---

### Option C: Proactive Compression (RECOMMENDED)

**What:** Run compression at 30-40% utilization, not just 80%.

**Changes:**
- Add: `ProactiveCompressionThreshold` to `ContextFirewallConfig`
- Modify: `internal/llm/context_firewall.go:processMessages()` -- add proactive stage
- Add: Background compression goroutine with debounce

**Token savings:** N/A (prevents crisis compression, not additional savings)

**Effort:** Low-Medium (1-2 days)

**Risk:** Low -- additive feature

---

### Option D: LLMLingua-Style Token Pruning (NOT RECOMMENDED YET)

**What:** Implement token-level importance scoring using lightweight model.

**Changes:**
- Add: `internal/llm/compress/token_pruner.go` -- port LLMLingua algorithm to Go
- Add: Lightweight model (e.g., distilled BERT) for scoring
- Modify: `internal/llm/context_firewall.go:processMessages()` -- add pruning stage

**Token savings:** Up to 20x

**Effort:** High (1-2 weeks)

**Risk:** Medium -- unproven in Go, requires model serving

**Why not:** High effort, Python-only ecosystem, Meept's use case (agent conversations) differs from LLMLingua's target (long documents).

---

## Recommendation

**Implement in this order:**

1. **Code-Aware Compression** (Option A) -- Highest impact, leverages existing tree-sitter infrastructure, directly addresses current pain point

2. **Hierarchical Summarization** (Option B) -- Low effort, addresses multi-day conversation degradation

3. **Proactive Compression** (Option C) -- Improves user experience by preventing sudden context drops

**Defer:**
- LLMLingua implementation -- Wait until handling document-heavy workloads
- Memory consolidation improvements -- Lower priority than context compression
- RAG re-ranking -- Only needed if retrieval quality degrades

---

## Validation Criteria (for Implementation)

If we proceed with implementation, success metrics:

| Metric | Target | Measurement |
|--------|--------|-------------|
| Code truncation quality | Preserve 100% of function signatures | Test suite with code samples |
| Hierarchical summarization | 70% retention of key facts after 3 levels | Qualitative review of summaries |
| Proactive compression trigger | No hard-limit context drops in normal usage | `FirewallStats.DropEvents` monitoring |
| Latency overhead | <100ms added per compression stage | Timing instrumentation |

---

## References

### Papers
- [LLMLingua (EMNLP 2023)](https://github.com/microsoft/LLMLingua)
- [Selective Context (EMNLP 2023)](https://github.com/liyucheng09/Selective_Context)
- [LLMLingua-2 (ACL 2024)](https://arxiv.org/abs/2304.08467)
- [StreamingLLM (ICLR 2024)](https://github.com/mit-han-lab/streaming-llm)
- [MemGPT (ICLR 2024)](https://arxiv.org/abs/2310.08560)
- [Gisting (NeurIPS 2023)](https://arxiv.org/abs/2304.08467)
- [AutoCompressor (EMNLP 2023)](https://arxiv.org/abs/2305.14788)

### Tools/Libraries
- [tiktoken-go](https://github.com/pkoukk/tiktoken-go) -- Go tokenizer
- [LangChain ContextualCompressionRetriever](https://python.langchain.com/docs/modules/data_connection/retrievers/contextual_compression)
- [Letta (MemGPT)](https://github.com/letta-ai/letta)
- [Mem0](https://github.com/mem0ai/mem0)

### Documentation
- [Anthropic Prompt Caching](https://platform.claude.com/docs/en/docs/build-with-claude/prompt-caching)
- [OpenAI Prompt Caching](https://platform.openai.com/docs/guides/prompt-caching)

---

## Next Steps

**If user approves implementation:**
1. Create implementation plan using `writing-plans` skill
2. Start with Option A (Code-Aware Compression)
3. Use TDD -- write tests for AST truncation behavior first
4. Frequent commits, subagent-driven execution

**If user wants more research:**
- Benchmark LLMLingua on Meept's actual workloads (run Python implementation, measure impact)
- Analyze real conversation logs to quantify typical context sizes and compression opportunities
