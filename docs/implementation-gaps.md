# Shadow Training System - Maturity Gap Analysis

> Generated 2026-02-21. Reflects the state of `internal/shadow/` after Phase 1-4 implementation.

## Overview

The shadow training system has a solid foundation for **data collection, scoring, and export** but has gaps preventing it from functioning as a mature, end-to-end retraining tool. This document catalogs those gaps organized by priority.

---

## Tier 1: Critical Functional Gaps

### 1. Tool-use interactions are never captured

Only final text responses trigger `CaptureInteraction()` (`loop.go:309-322`). When the LLM returns tool calls (`loop.go:280-301`), the loop `continue`s without capture. Multi-step tool interactions — often the most valuable training data (code generation, debugging chains) — are completely lost.

**Files**: `internal/agent/loop.go:280-322`

### 2. Multi-agent interactions not captured

Shadow manager is only injected into the main agent loop. The specialist agents (coder, debugger, planner, analyst) created via `AgentRegistry` in `components.go` never receive the shadow manager. In multi-agent mode, most work happens in specialists.

**Files**: `internal/daemon/components.go:246-278`, `internal/agent/loop.go:155-160`

### 3. Cost calculation is wrong

`teacher.go:161` computes cost as `(CostPerMillionInput + CostPerMillionOutput) / 1000.0` — a fixed per-call estimate that ignores actual token counts. This means daily cost limits (`max_daily_cost`) won't actually work. The teacher response object has token counts available but they aren't used.

**Files**: `internal/shadow/teacher.go:154-169`

### 4. Deduplication is hash-only, threshold config is ignored

`exporter.go:424-438` uses first-100-chars as a hash fingerprint. The config field `DedupSimilarityThreshold` (0.95) is defined but never referenced. Near-duplicate paraphrases won't be caught, leading to redundant training data.

**Files**: `internal/shadow/exporter.go:424-448`, `internal/shadow/config.go`

---

## Tier 2: Missing Training Pipeline Components

### 5. No training execution exists

Config defines LoRA parameters (`rank`, `alpha`, `dropout`, `learning_rate`, `epochs`, `batch_size`, etc.) and DPO parameters (`beta`, `loss_type`), and the `adapters train` CLI command exists, but it only exports DPO data and prints instructions. There is no actual training invocation — no call to `unsloth`, `axolotl`, `trl`, or even a shell command. The system collects data but can't close the training loop without manual intervention.

**Files**: `cmd/meept/shadow.go` (`newShadowAdaptersTrainCmd`), `internal/shadow/adapters/ollama.go`

### 6. Auto-train trigger is dead code

Config has `AutoTrain bool`, `TrainThreshold int`, and `TrainSchedule string`. The manager has `GetPreferencePairCount()` to check readiness. But nothing connects them — no scheduler integration, no threshold check loop, no cron evaluation.

**Files**: `internal/shadow/config.go:145-147`, `internal/shadow/manager.go`

### 7. Ollama adapter loading doesn't actually train

`adapters/ollama.go` can register, activate, and create Ollama models with pre-existing adapters, but there is no workflow that takes exported DPO/JSONL data and produces a LoRA adapter. The Ollama adapter is a loader, not a trainer.

**Files**: `internal/shadow/adapters/ollama.go`

---

## Tier 3: Quality & Accuracy Gaps

### 8. Embedding system is defined but not implemented

`FewShotExample.EmbeddingJSON` field exists in the model. The selector mentions it. But embeddings are never computed or stored. The selector falls back to Jaccard + bigram similarity, which means few-shot example retrieval is keyword-overlap only — it won't understand semantic similarity.

**Files**: `internal/shadow/models.go:166`, `internal/shadow/selector.go:153-188`

### 9. Heuristic scoring is surface-level

- `scoreRelevance()`: Simple term overlap between query and response
- `scoreCompleteness()`: Length ratio against hardcoded per-domain expectations
- `scoreCorrectness()`: Pattern matching for error indicators (base score 0.7, subtract for red flags)
- `scoreStyle()`: Formatting checks only

None of these actually validate correctness. Code isn't compiled or tested. Facts aren't verified. This means quality scores are unreliable proxies — garbage responses that are long and well-formatted will score well.

**Files**: `internal/shadow/scorer.go:99-308`

### 10. Selector lacks diversity controls

The selector scores by similarity + recency + quality, but has no diversity mechanism. If 5 very similar examples exist for a domain, all 5 could be returned, wasting context budget. No MMR (maximal marginal relevance) or clustering.

**Files**: `internal/shadow/selector.go`

---

## Tier 4: Operational & Robustness Gaps

### 11. No test coverage

Zero test files in `internal/shadow/` or `internal/shadow/adapters/`. The SQLite store, scorer, selector, exporter, and manager have no unit tests. The few-shot injection path has no integration test. You can't refactor or upgrade with confidence.

### 12. No metrics/observability

No counters for: records collected, teacher calls made, scoring distribution, example cache hit rate, export counts. No way to monitor the health of the shadow pipeline in production without reading SQLite directly.

### 13. No retry/circuit breaker for teacher API

If the teacher model is down or rate-limited, each shadow request independently fails and logs a warning. No circuit breaker prevents hammering a dead endpoint. No retry with backoff for transient failures.

**Files**: `internal/shadow/teacher.go`

### 14. Classification is duplicated and inconsistent

Domain/task classification happens in three places with different keyword sets:

- `middleware.go:233-312` (never called since middleware isn't wired)
- `manager.go` `CaptureInteraction()`
- `loop.go:624-658` (for example retrieval)

The retrieval classification may not match the storage classification, meaning examples could be stored under one domain but queried under another.

### 15. No data validation or schema migration path

SQLite tables are created with `CREATE TABLE IF NOT EXISTS` but there's no versioning or migration system. If schema changes are needed (e.g., adding embedding columns), existing databases will fail silently or need manual migration.

**Files**: `internal/shadow/store_sqlite.go`

---

## Priority Matrix

| # | Gap | Impact | Effort | Priority |
|---|-----|--------|--------|----------|
| 1 | Tool-use capture | High (misses most valuable data) | Low | **P0** |
| 2 | Multi-agent capture | High (misses specialist work) | Medium | **P0** |
| 3 | Cost calculation fix | Medium (budget limits broken) | Low | **P1** |
| 5 | Training execution (CLI) | High (can't close loop) | Medium | **P1** |
| 11 | Test coverage | High (can't refactor safely) | High | **P1** |
| 8 | Embedding integration | Medium (poor retrieval quality) | Medium | **P2** |
| 9 | Heuristic scoring upgrade | Medium (unreliable quality gates) | High | **P2** |
| 6 | Auto-train trigger | Medium (manual-only training) | Medium | **P2** |
| 4 | Dedup threshold usage | Low (redundant training data) | Low | **P2** |
| 14 | Classification unification | Low (slight retrieval mismatch) | Low | **P3** |
| 12 | Observability/metrics | Medium (blind in production) | Medium | **P3** |
| 10 | Selector diversity (MMR) | Low (suboptimal example sets) | Medium | **P3** |
| 13 | Circuit breaker | Low (noisy logs when teacher down) | Medium | **P3** |
| 15 | Schema migrations | Low (future-proofing) | Medium | **P3** |

---

## Component Completeness

| Component | Completeness | Status |
|-----------|-------------|--------|
| Config | 100% | Ready |
| Models | 100% | Ready |
| Store interfaces | 100% | Ready |
| SQLite implementation | 95% | Minor query optimizations needed |
| Middleware | 80% | ML-based classification missing |
| Teacher client | 90% | Cost calculation fix needed |
| Scorer | 70% | Surface-level heuristics only |
| Selector | 75% | Embedding integration missing |
| Exporter | 85% | Semantic deduplication missing |
| Manager | 90% | Training workflows missing |
| Ollama adapter | 70% | Training implementation missing |
| OpenAI adapter | 95% | Production-ready |
| **Overall** | **~85%** | Data collection works; training loop not closed |
