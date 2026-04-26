# LLM-Based Memory Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` to implement this plan task-by-task.

**Goal:** Wire up LLM-based summarization for memory consolidation, replacing the naive date-based grouping with semantic clustering and intelligent summarization.

**Architecture:** Add an LLM client to `Consolidator`, implement `SummarizeWithLLM()` that sends memories to the LLM with a structured extraction prompt, parse the response, and use it for consolidation.

**Tech Stack:** Go 1.24.2, existing `internal/memory/consolidation.go`, `internal/llm/` client

---

## Task 1: Wire Up SummarizeWithLLM

**Files:**
- Modify: `internal/memory/consolidation.go`
- Modify: `internal/memory/manager.go` (add LLM client accessor)

**Implementation steps:**

1. **Add LLM client to Consolidator:**
   ```go
   type Consolidator struct {
       manager  *Manager
       logger   *slog.Logger
       llm      llm.Chatter  // Add this
       // ... existing fields
   }
   ```

2. **Update ConsolidatorConfig:**
   ```go
   type ConsolidatorConfig struct {
       Manager *Manager
       Logger  *slog.Logger
       LLM     llm.Chatter  // Add this
   }
   ```

3. **Implement `SummarizeWithLLM()` method:**
   - Build prompt with memories
   - Call LLM with structured extraction prompt
   - Parse JSON response into `[]Summary`

4. **Update `consolidateEpisodic()` to:**
   - Call `SummarizeWithLLM()` instead of `summarizeByDate()` when LLM is available
   - Fall back to date-based grouping if LLM fails

5. **Write tests:**
   - Test LLM-based consolidation with mock LLM
   - Test fallback to date-based when LLM fails

**Commit when done:**
```bash
git add internal/memory/consolidation.go
git commit -m "feat: wire up LLM-based memory summarization"
```

---

## Task 2: Add Semantic Clustering

**Files:**
- Modify: `internal/memory/consolidation.go`
- Create: `internal/memory/clustering.go` (new)

**Implementation steps:**

1. **Add embedding-based similarity:**
   - Use existing embedding client from `internal/agent/`
   - Calculate cosine similarity between memories
   - Group memories by similarity threshold

2. **Add `ClusterBySimilarity()` function:**
   - Input: `[]MemoryResult`
   - Output: `[][]MemoryResult` (clusters)
   - Use simple k-means or hierarchical clustering

3. **Update `MergeRelated()` to use clustering instead of date grouping**

4. **Write tests**

**Commit when done:**
```bash
git add internal/memory/consolidation.go internal/memory/clustering.go
git commit -m "feat: add semantic clustering for memory consolidation"
```

---

## Start with Task 1 (LLM-based summarization) - this wires up the placeholder.
