# Classification & Routing — Handoff Document

**Date:** 2026-09-06
**Repo:** /Users/caimlas/git/meept @ HEAD (classifier alias flip + Qwen3-Embedding downloaded)
**Author:** Hermes session (chat-dispatch-ux → session-continuity → session-aware-intent-gate → classifier-observability → this)

---

## 1. Current Architecture — How a Chat Request Flows

```
                              ┌──────────────────────────────┐
   user message ──────────────►  meept-daemon  (ChatHandler)  │
                              │  handler.go:655              │
                              └──────────────┬───────────────┘
                                             │ ClassifyAndRoute(ctx, input,
                                             │                 sessionID, …)
                                             ▼
                              ┌──────────────────────────────┐
                              │ 1. Clarification resume gate  │  dispatcher.go:581
                              │    (pending clarifications)   │
                              └──────────────┬───────────────┘
                                             ▼
                              ┌──────────────────────────────┐
                              │ 2. buildMemoryContext         │  dispatcher.go:661
                              │    (memory search + last      │
                              │    intent + intent counts)    │
                              └──────────────┬───────────────┘
                                             ▼
                              ┌──────────────────────────────┐
                              │ 3. buildSessionContextDigest  │  session_digest.go:42
                              │    last task name/state/agent │  (session-aware-intent-gate L01)
                              │    terminal step result (400c)│
                              │    working dir (wt>proj>cwd)  │  (classifier-observability L03)
                              │    last intent type           │
                              └──────────────┬───────────────┘
                                             ▼
                       ┌────────────────────────────────────────┐
                       │ 4. INTENT ANALYZER (ambiguity gate)     │  intent_analyzer.go:189
                       │    model: classifier alias primary      │
                       │    prompt: JSON schema + 2 context      │
                       │    rules + [Recent session activity]    │
                       │    block from the digest                │
                       │    (session-aware-intent-gate L02)      │
                       │                                         │
                       │    ambiguity >= 0.6 ──► canned          │  dispatcher.go:668
                       │                        clarification    │  (buildClarificationResult)
                       │            │                            │  [turn ENDS here —
                       │            ▼                            │   no agent runs]
                       │    ambiguity <  0.6 ──► proceed         │
                       └────────────────────┬───────────────────┘
                                            ▼
                       ┌────────────────────────────────────────┐
                       │ 5. ROUTING CLASSIFIER (LLM)             │  dispatcher.go:907
                       │    model: classifier alias (shared      │
                       │    client w/ analyzer)                  │
                       │    → intent_type + agent + confidence   │
                       │                                         │
                       │    degrade chain on failure:            │  dispatcher.go:862-1025
                       │    llm → llm_empty_fallback_chat →      │
                       │    keyword(≥0.3) → semantic →           │
                       │    heuristic → chat                     │
                       └────────────────────┬───────────────────┘
                                            ▼
                       ┌────────────────────────────────────────┐
                       │ 6. PROVENANCE                           │  (classifier-observability L01)
                       │    ChatResponse.Meta = {method, model,  │
                       │    ambiguity, session_digest_used}      │
                       └────────────────────┬───────────────────┘
                                            ▼
                 ┌─────────────────────────────────────────────┐
                 │ 7. AGENT DISPATCH                            │
                 │    code→coder task loop (tools: file_write…) │
                 │    chat→chat loop │ report→chat │ etc.       │
                 │    each agent loop: own conversation, tools, │
                 │    reasoning config, cycle detection         │
                 └────────────────────┬────────────────────────┘
                                      ▼
                 ┌─────────────────────────────────────────────┐
                 │ 8. REPLY PATH                                │
                 │    task result → persistExchange → session   │
                 │    conversation (session-continuity L01)     │
                 │    cache miss → restore from DB (L02)        │
                 │    reply guard (roster/JSON leak) → user     │
                 └─────────────────────────────────────────────┘
```

### Model lineup (config/models.json5, all local)

| Role | Model | Runtime |
|---|---|---|
| General default | `local-gguf/lfm-8b-gguf` | llama.cpp :8080 (`--jinja`) |
| Classifier primary | `local-gguf/lfm-8b-gguf` | llama.cpp :8080 (shared) |
| Classifier alternate | `local/lfm-8b-mlx-4bit` (86.8% A/B) | mlx_lm :8083 |
| Classifier A/B alternate | `local-mlx/lfm-combined-sft` (54.4%) | mlx_lm :8081 |
| Embedding (new, unused yet) | `Qwen3-Embedding-0.6B-4bit-DWQ` @ /Volumes/LLMs | TBD |

### Failures this architecture already survived (fixed)

- ctx-cancellation SIGKILL of runtimes (a5312a64)
- MLX tool-call markers unparsed (483fffe5)
- classifier port collision in e2e (af797c03) + runtime health race (52bc841e)
- silent fallback degradation → provenance Meta (cb8031f3) + fail-fast mode (903644f0)

---

## 2. The Problem With the Current Approach

The gate + router both use a **1-8B causal LLM asked to output JSON**. Per-turn cost:
2+ LLM calls (analyzer + router) before any work starts. Quality is model-bound —
the 12-run e2e campaign showed even a good 8B hovering near threshold (0.6/0.8 verdicts
on clear instructions), and the SFT 1.2B collapsing to 54% accuracy.

Latency: analyzer (0.3–28s) + router (15s timeout) serialize before the agent starts.

## 3. Proposed Next Architecture (user direction: SetFit-style prefilter)

```
   user message ──► [STAGE 0: EMBEDDING PREFILTER — new]
                      Qwen3-Embedding-0.6B (0.3s, no tokens)
                      embedding → cosine vs labeled intent centroids
                      (SetFit/linear head over the frozen embedding)
                            │
              confidence ≥ τ ┼──► route DIRECTLY (skip analyzer+router LLM
              (e.g. τ=0.90)  │     calls entirely — ~0.3s pre-agent path)
                             │
              confidence < τ ┼──► [existing stages 2-5 unchanged]
                             └──► LLM analyzer + router decide (hard cases)
```

- Embedding model downloaded: /Volumes/LLMs/Qwen3-Embedding-0.6B-4bit-DWQ (336MB, MLX 4-bit)
- Training data: testdata/eval/classifier-test-corpus.json5 (136 cases) + task history
- Where it plugs in: new Stage-0 in ClassifyAndRoute before the analyzer; EmbeddingConfig
  already exists in schema.go:1235 (provider/base_url/model/dimension)

## 4. Immediate State

- classifier alias primary: local-gguf/lfm-8b-gguf (llama.cpp :8080, 2026-09-12
  driver switch — mlx_lm cannot emit tool calls); the mlx_lm 8B entry stays as
  the classifier alternate (86.8% A/B, 2026-09-06)
- combined-sft: demoted to A/B alternate slot (54.4%, platform/search/report/review 0%)
- Qwen3-Embedding weights: downloaded, NOT yet wired into config/code
- e2e: `make e2e-chat` (A5 `?`-heuristic decision still parked)
- A/B reports: /tmp/classifier-ab/ (sft), /tmp/classifier-ab-8b/ (8B)

## 5. Open Items

1. Wire Qwen3-Embedding as Stage-0 prefilter (design in §3) — needs an embedding
   server (mlx_embedding server or ollama) + corpus centroid builder + threshold config
2. A5 `?`-heuristic: strict vs accept-when-artifact-named (parked user decision)
3. Reply envelope shaping (task results arrive as raw JSON blobs)
4. task_get with empty args → return session listing, not error+cycle-abort
5. Combined-sft retrain round 2 (zero-score categories) — optional, its niche role
   may be obsolete if Stage-0 prefilter works
