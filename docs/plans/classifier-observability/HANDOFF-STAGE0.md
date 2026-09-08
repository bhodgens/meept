# HANDOFF — combined-sft rehabilitation + Stage-0 embedding prefilter

Written 2026-09-07 by the session that root-caused the classifier drift
and ran the live transcript-chain smoke. Resume point for a fresh agent
picking up issue #33 (combined-sft regenerate/retrain/re-benchmark) and
the Stage-0 embedding prefilter design.

Read this top to bottom before touching anything. All claims are
code-verified; file:line refs are current at HEAD `5f8b4699` but expect
drift — sibling sessions share this checkout and move fast.

---

## 1. TL;DR — what happened and why

The combined-sft classifier scored 54.4% on the 136-case corpus vs
86.8% for lfm-8b-mlx. Root cause is NOT model capability: the SFT was
trained against an OLD prompt contract (dispatcher-style schema with
`{intent, agent, priority, creates_task, requires_planning}`), and is
evaluated/served under the NEW `llm_classifier` contract
(`{intent, confidence, reasoning}`, 12 intents).
`creates_task` appears nowhere in git history — the training prompt
predates the repo's current classifier and drifted twice since.

Controlled probe (same 4 inputs, only the system prompt varied,
checkpoint live on mlx_lm :18081):

| input | current prompt (served) | old dispatcher prompt (trained) |
|---|---|---|
| "what can you do?" | schedule 0.86 + reasoning — WRONG | code/coder — defensible |
| "search for Go performance benchmarks" | plain text, no JSON | search/analyst 0.9 — correct |
| "create a function to sort users by name" | plain text, no JSON | code/coder 0.9 — correct |
| "why is this test failing?" | plain prose, no JSON | debug acknowledgment |

Conclusion: regenerate the training set against the CURRENT prompt
contract, retrain, re-benchmark (issue #33). In parallel, the Stage-0
embedding prefilter may retire the SFT entirely — see §5.

## 2. Fixed in the interim (do NOT re-investigate)

A chain of defects was found and fixed while live-smoking the
transcript chain; the empty-content/reasoning-only symptom had THREE
stacked causes upstream of any model quality issue:

| Commit | Fix |
|---|---|
| ea875e36 | media_url_guard: YouTube URLs / bare video IDs route IntentAnalyze→analyst deterministically, ahead of the LLM classifier (was: intent=code 0.95 → coder, which holds no transcript_fetch grant) |
| f1d15b6d | ToolActionMap gained "transcript_fetch": "network_request" (was "Unknown action" security denial) |
| 98fac0fe | transcript_fetch added to DefaultAlwaysFullTools() — indexed schema mode stubbed its params to empty, models called it with no args |
| 7c503973 | embedded python script rewritten for youtube-transcript-api >= 1.0 (static API removed in v1.x) |
| 5e7625d1 | LFM2.5 tool-call marker shim (parseLFMToolCalls) wired into doStreamRequest — the agent loop ALWAYS streams (loop.go reasoningCycle passes streamOnDelta for every turn), so the 483fffe5 non-streaming-only shim never covered agent turns |
| eb5563ff | reasoning-watchdog rescue turn: second reasoning-only breach now retries once with llm.DisableThinking() (last-append-wins over the agent's WithReasoning) + firmer nudge; terminate only if the rescue is also reasoning-only. ReasoningWatchRescueNext/Rescued flags on AgentLoop; reset in resetTurnGuards |
| ccc93f62 | TestConfigLoads classifier assertion tracks the lfm-8b flip (stale test, not a regression) |

## 3. Current state (resume points)

- Issue #33 OPEN: bhodgens/meept/issues/33 — full root cause, training-set
  spec, retrain steps, promotion bar (>= 80% overall, no 0% category).
- Repo: main @ 5f8b4699. Sibling sessions actively modify dispatcher.go,
  schema.go, components.go, executor.go — ALWAYS re-read before editing;
  commit by explicit path; expect mutexio/pre-commit noise from their WIP.
- Artifacts:
  - A/B reports: /tmp/classifier-ab/benchmark-report.md (sft),
    /tmp/classifier-ab-8b/benchmark-report.md (8b) — /tmp is volatile
  - Benchmark harness: cmd/meept-classifier-test (uses
    agent.NewLLMClassifier + internal/eval runner; corpus =
    testdata/eval/classifier-test-corpus.json5, 136 cases:
    coding 25, debugging 25, analyze 25, search 15, chat 10, git 10,
    platform 5, schedule 5, plan 5, review 4, report 4, recall 3 —
    the <=5-case categories are exactly where sft scored 0-33%)
  - Model: /Volumes/LLMs/lfm2.5-1.2b-combined-serialized-sft
    (1.17B params bf16; GGUF Q4_K_M/Q8_0/F16 siblings exist)
  - Training config: config/training/lora_lfm2.5_1.2b.yaml
    (LoRA r16 a32, all linear projections, 3 epochs, lr 1e-4)
  - Learning pipeline: internal/learning (CaptureRecorder ->
    raw_captures.jsonl 57KB/29 lines -> Consolidate -> DomainDatasets;
    TrainingExample{Instruction,Input,Output,Metadata})
  - Embedding model downloaded:
    /Volumes/LLMs/Qwen3-Embedding-0.6B-4bit-DWQ (336MB, not wired)
  - Prior handoff: docs/plans/classifier-observability/HANDOFF.md
    (§3 sketches Stage-0)
- Runtime: mlx_lm :18081 = combined-sft, :18082 = LFM2.5-8B-MLX-4bit;
  llama-server :8080 = LFM2.5-8B GGUF Q4_K_M. Runtimes are user-
  launched; do not kill without asking.
- Scratch probe: cmd/glmprobe-tmp/ (untracked, safe to delete) — was
  used for the controlled prompt experiment; re-create as needed.

## 4. How to run the classification probe (reproduce experiments)

cmd/glmprobe-tmp/main.go pattern: llm.NewClient with
BaseURL=http://127.0.0.1:18081/v1, ModelID=/Volumes/LLMs/lfm2.5-1.2b-
combined-serialized-sft, MaxTokens=512, then client.Chat(ctx,
[{system: <prompt under test>}, {user: <input>}]) and print
Content/Reasoning/ToolCalls raw. NOTE: client.Chat requires a real ctx
(nil → "nil Context" request error). For benchmarking use
cmd/meept-classifier-test -model-a ... -model-b ... (it builds its own
clients per model).

## 5. Stage-0 embedding prefilter — the agreed direction

User's own architecture instinct (validated): a SetFit-style gate that
makes prompt-contract drift impossible for the common case.

```
user message
  → STAGE 0: embed(message) via Qwen3-Embedding-0.6B
    → cosine vs per-intent centroids (mean of labeled examples;
      seed = the 136-case corpus + accumulated task history)
    → margin = top1 − top2 similarity
  → margin >= τ (config, start 0.90): route DIRECTLY
      (Intent{Type, AgentType, Confidence: margin, Method:"embed"})
  → margin < τ: existing chain unchanged (analyzer → LLM classifier
      → keyword → heuristic → chat)
```

Design invariants:
- Same Intent struct out; downstream (media_url_guard, guards, agent
  loops) unchanged. Failure mode is "no faster", never "worse".
- τ is config. Start 0.90; ~70-80% of real traffic should take the
  fast path.
- No training run ever: new intent = add examples + regenerate
  centroids. This is the property that retires combined-sft — an SFT
  goes stale whenever the prompt contract changes; centroids don't.
- Centroid seed data = the SAME regenerated dataset as issue #33 step 1
  (which is why that issue's step 1 is worth doing regardless).

Suggested tree (3 leaves): (1) centroid builder script + table format
+ regenerate-from-corpus; (2) Stage-0 gate in ClassifyAndRoute
(dispatcher.go:626) + EmbeddingConfig wiring (schema.go:1234 already
exists as the config seam); (3) eval harness comparing prefilter vs
current chain on the 136-case corpus + the A/B numbers.

Known risks: short/ambiguous messages embed near everything (margin
test catches them); multi-intent messages; centroid staleness (mitigate:
regenerate from routed-turn outcomes, never hand-edit).

## 6. Open items / traps

- cmd/glmprobe-tmp/ is untracked scratch — delete freely.
- TestConfigLoads in internal/llm reads ../../config/models.json5
  (repo config). The USER's ~/.meept/models.json5 is separately
  maintained (classifier flipped to lfm-8b there too). If the alias
  changes again, update BOTH the repo config and that test.
- pip --user installs are $HOME-dependent: a daemon launched with an
  alternate HOME cannot import user-site packages. Use PYTHONPATH
  (e.g. /Users/caimlas/Library/Python/3.14/lib/python/site-packages)
  when smoke-testing transcript_fetch with a scratch HOME.
- mlx_lm :18081/:18082 runtimes + llama-server :8080 are shared;
  the transcript smoke daemon on /tmp/meept-smoke.dxr4r0 was stopped.
- The user's session DB may not have the original combined-sft
  training session (predates the window) — the exact training JSONL
  was NOT recoverable; evidence ends at the model's observed output
  schema and the May 22 GGUF-conversion session.

## 7. Recommended order for the next agent

1. Read issue #33 + §5 above; decide SFT-retrain vs Stage-0-first
   (user leans Stage-0; issue #33 step 1 feeds both).
2. If Stage-0: draft the 3-leaf tree (hierarchical-planning skill),
   baseline = the A/B numbers in §1/§3.
3. If SFT-retrain first: build the generator script (issue #33 §1),
   train per §3 config, benchmark per §3 promotion bar.
4. Either way: re-run cmd/meept-classifier-test with -detailed after
   any model/data change; keep /tmp reports OUT of the repo (copy
   final numbers into docs/eval/).
