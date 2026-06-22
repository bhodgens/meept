---
name: librarian-backlog-mining
description: Walk old episodic memory to recover claims and decisions worth promoting
tags:
  - memory
  - librarian
requires:
  - reasoning
risk_level: low
---

# Backlog Mining (Path C)

## Purpose

Walk existing episodic memory (filtered by age, category, or keyword), run the ambient-extraction classifier over batches, and write candidates as `status=auto`. This is Path C in the epistemic memory design.

## Process

### 1. Select Batch
- Use `memory_search` to find episodic memories older than 7 days
- Filter by category or keyword if requested
- Batch size: 20-50 memories at a time

### 2. Classify
For each memory in the batch:
- Run the same LLM classifier as ambient extraction (Path B)
- Extract candidate claims, decisions, predictions

### 3. Filter
- Drop candidates below `ConfidenceThreshold`
- Drop candidates matching `ExcludeCategories`
- Cap to `MaxPerTurn` per batch

### 4. Write
- Write each candidate as a typed memory with `status=auto`
- Tag with `source: backlog-mining`

### 5. Report
- Summary: "Processed N memories, extracted M candidates"
- List candidates for user review (they'll appear in the next promotion cycle)
