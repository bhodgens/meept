# Hierarchical Summarization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` to implement this plan task-by-task.

**Goal:** Add recursive/hierarchical summarization to handle multi-day conversations where summaries themselves become too large.

**Architecture:** Extend `ContextFirewall.summarizeOldHistory()` to recursively summarize when the first-level summary exceeds a token threshold. Track compression levels in metadata.

**Tech Stack:** Go 1.24.2, existing `internal/llm/context_firewall.go`, `internal/agent/conversation.go`

---

## Task 1: Add Hierarchical Summarization to ContextFirewall

**Files:**
- Modify: `internal/llm/context_firewall.go`
- Create: `internal/llm/context_firewall_hierarchical_test.go`

**Steps:**

1. Add `HierarchicalSummarization` bool to `ContextFirewallConfig`
2. Add `MaxSummaryLevel` int (default 3) to config
3. Add `SummaryLevel` tracking to messages metadata
4. Modify `summarizeOldHistory()` to:
   - Check if existing summary exceeds threshold
   - If yes, summarize the summary (recursive compression)
   - Track summary level in metadata
5. Write tests for recursive summarization

---

## Task 2: Add Content-Aware Summarization

**Files:**
- Modify: `internal/llm/context_firewall.go`
- Modify: `internal/agent/prompt/builder.go`

**Steps:**

1. Add structured summarization prompt that extracts:
   - Key decisions made
   - File paths referenced
   - Unresolved questions
   - Task state
2. Create `SummaryExtract` struct with fields for each category
3. Modify `summarizeOldHistory()` to use structured extraction
4. Include extracted structure in summary message

---

## Task 3: Add Compression Quality Metrics

**Files:**
- Modify: `internal/llm/context_firewall.go`
- Modify: `internal/llm/context_compressor.go`

**Steps:**

1. Add `QualityScore` to `CompressionResult`
2. Track pre/post compression token ratios
3. Track critical message retention rate
4. Add `QualityMetrics` struct to `FirewallStats`
5. Write tests for metrics accuracy

---

## Execution

Start with Task 1 (Hierarchical Summarization) - this is the core feature.
