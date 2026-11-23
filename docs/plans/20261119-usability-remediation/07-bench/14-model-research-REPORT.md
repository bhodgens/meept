# 14-model-research-REPORT.md -- memory-comparable LLM candidates for the planner role

Plan: 20261119-usability-remediation (residual: 8B model quality)
Date: 2026-11-23
Status: research only; no config changed; no commit (branch main is read-only for this task)
Scope: find a locally-runnable model, comparable in memory to the rig 8B, that fixes
the planner-role failure classes observed in runs 3-9; answer whether the planner
role specifically needs a bigger model.

## 0. TL;DR

- The rig "8B" is LFM2.5-8B-A1B: 8.3B total parameters but only ~1.5B ACTIVE
  (MoE). Independent third-party testing (freshcontext.io) found it
  essentially LACKS tool perception (called a tool in 1 of 60 cases where one
  was needed) and has 27.8% abstention accuracy -- vendor BFCL/Tau2 numbers
  did not transfer to a real harness. This matches our failure classes 1-4
  almost exactly.
- The planner role (long-horizon decomposition + constraint tracking) is the
  one tier where ALL small models collapse in published data (AgentFloor,
  arXiv 2605.00334: tier-E pass rates 0-16% at 8B, GPT-5 10%). The gap is
  MODEL-SPECIFIC, not universal at a fixed level: ministral-3:8b 16% vs
  qwen3:8b 0% on the same tier.
- Verdict on the direct question: the rig is NOT generally broken at 8B --
  it is broken AS PLANNER, and the specific checkpoint is a bad planner.
  Other roles (router/classifier, simple executor, summarizer) were less
  affected in runs 3-9 and the literature says those tiers are within reach
  of good 8-10B models.
- Recommendation: do NOT look for a drop-in 8B swap as the primary fix. Use
  a MODEL-ROLE SPLIT: keep a small model for cheap roles and promote a
  9-14B dense model to planner only. On this host (Mac Studio, 64 GB
  unified memory, hw.memsize verified 64 GiB) a planner-split costs well
  under 16 GB resident and leaves the second runtime untouched.
- Ranked candidates: (1) Qwen3.5-9B [planner-split primary], (2) Gemma 4 12B
  [planner-split alternative, strongest per-parameter], (3) Ministral-3-8B
  [same-footprint swap, best 8B planner evidence, MLX repo caveat], (4)
  Granite 4.2 8B [conservative same-footprint swap].

## 1. Failure classes observed (runs 3-9, 07-bench/02-REPORT.md sections 11-16)

| # | Failure class | Evidence in runs 3-9 | Role affected |
|---|---------------|----------------------|---------------|
| F1 | Planner phase-spirals / over-decomposition (10-step plans where 3 suffice; "name is missing" task_create retry loops; u-direct timeout) | 02-REPORT.md 16b/16d: "planner retry spiral" flagged model/runtime; residual 8 + residual 11 OPEN-model | PLANNER |
| F2 | Fabricated tool claims without evidence (claimed file_read confirmed when never run; typo'd filenames e.g. definely_missing_12345.txt) | 02-REPORT.md: fabricated-success guard probes p1/ev5/ev6 added; u-context-two-step "model never executed file_read, gate correctly fail-closed on zero evidence" | PLANNER / long-horizon executor |
| F3 | Intermittent refusals of valid actions (raw-IP fetch refusal in u-route-research-write) + +/-1-2 task seed variance | 02-REPORT.md: "run-3 model variance ran the other way"; 16b "model-bound noise both directions" | PLANNER / executor |
| F4 | Intent misrouting under heuristic fallback (u-context classified 'code' via heuristic_fallback when LLM classifier unavailable) | 02-REPORT.md run logs | ROUTER (degraded mode only) |

Run-9 disposition (02-REPORT.md 16e): infrastructure seams ALL GREEN;
"100% of the remaining red verdicts are 8B model-quality classes".

## 2. The current model, precisely

- Config (config/models.json5): `local-gguf/lfm-8b-gguf` = LFM2.5-8B-A1B
  Q4_K_M on llama.cpp; `local-mlx/lfm-combined-sft` as the small/alternate.
- LFM2.5-8B-A1B: 8.3B total, 1.5B active, 24 layers (18 conv + 6 GQA
  attention), 128K context, reasoning-only (always emits chain-of-thought)
  [VERIFIED: LiquidAI/LFM2.5-8B-A1B HF card].
- Vendor-reported agent scores (self-reported, vendor harness): BFCLv3 64.36,
  BFCLv4 48.50-49.73, Tau2-Telecom 88.07, Tau2-Retail 39.82.
- Third-party reality check [VERIFIED, independent lab runs on llama.cpp]:
  freshcontext.io/benchmarks/models/lfm2.5-8b-a1b/ found:
  - Tool perception 1/60 (1.7%): "It can emit a tool call -- it just doesn't
    perceive the need". 59 of 60 cases answered directly instead of calling.
  - Abstention 27.8%; grounding 47.2% with 46/66 failures misattributed and
    19 fabricated. Verdict recorded: "the weakest model tested".
  - Co-residency is good (few attention layers) -- explains why it sits
    beside the second runtime cheaply.
- Darkgrade Toolbench (independent, GGUF Q4_K_M): score 29/100, ranked 37 of
  37 models; 29% of tool calls invalid [VERIFIED].
- Reading: the vendor Tau2 numbers come from Liquid's own harness and do not
  transfer. This is the single most important fact for the replacement
  decision: do not rank candidates by vendor self-reported Tau2 alone.

## 3. Is the planner role too hard for 7-10B, or model-specific?

Published evidence (late 2025 - late 2026):

1. AgentFloor (arXiv 2605.00334) -- 30-task capability ladder, 16 open models
   0.27B-32B, 16,542 runs [VERIFIED, pre-registered]:
   - Tiers A0-C (instruction following, single tool call, 2-chains,
     branching): small models are adequate; "use smaller open-weight models
     for the broad base of routine actions".
   - Tier D (multi-source synthesis): collapses at 8B -- qwen3:8b 0%,
     ministral-3:8b 16%.
   - Tier E (long-horizon planning under persistent constraints, 8-12 tool
     calls): GPT-5 10%, best open-weight 16% (ministral-3:8b), qwen3:8b 0%.
     Neither frontier nor open reaches reliability.
   - Key quote: "the effects are model-specific rather than universal."
   Implication: an 8B planner will never be RELIABLE, but the choice of
   checkpoint changes pass rates by 16+ points; and the pipeline's own
   decomposition style (3-phase plans, not 12-step chains) sits closer to
   tier C/D than the hardest tier E.
2. BFCL multi-turn gap (Berkeley leaderboard via octaneural/vdf summaries,
   April 2026 board) [VERIFIED]: Qwen3-8B scores 87.6% single-turn non-live
   vs 41.75% multi-turn -- the planner-shaped skills degrade ~2x even for
   the best 8B. Llama-3.1-8B-Instruct: multi-turn 7.4%.
3. Big Iron harness test (bigiron.cc, Aug 2026) [third-party, mechanical
   verification]: 8B-class at Q4 completes 60-75% of 5-step tool loops,
   dropping to 45-55% at 40 steps; structured prompts with state-restating
   recover ~15 points. "8B models lean on scaffolding hard."
4. Agent skill spread at fixed size [VERIFIED]: BFCL overall:
   xLAM-2-8b-fc-r 46.68 (CC-BY-NC: unusable), Qwen3-8B 42.57,
   Llama-3.2-3B 21.95. Same size class, >2x spread.

CONCLUSION for question (3): the planner role is the hardest role at this
size and no 7-10B is fully reliable at it; BUT the failures we see (F1-F2)
are severe even against this distribution, and the specific LFM2.5 checkpoint
is documented by an independent lab as effectively tool-blind in real
harnesses. The rig's own run pattern (router/executor/summarizer roles
passing, planner failing) is consistent with the AgentFloor tier structure.
A materially better 8-12B checkpoint is a justified fix; expecting frontier
planner reliability at any size under 14B is not.

## 4. Candidate comparison table

All RAM figures are weight file + runtime overhead estimates at inference;
KV cache adds ~0.5-2 GB depending on context. Verified = repo confirmed to
exist on HuggingFace with 4-bit MLX weights; anecdote = single-source or
self-reported, not reproduced.

| Rank | Model | Params (total/active) | License | MLX 4-bit repo (verified) | 4-bit file | Agent evidence (source class) | Fits failure classes |
|------|-------|----------------------|---------|---------------------------|-----------|-------------------------------|----------------------|
| 1 | Qwen3.5-9B (dense) | 9.7B dense | Apache 2.0 | lmstudio-community/Qwen3.5-9B-MLX-4bit (5.95 GB); also mlx-community/Qwen3.5-9B-MLX-4bit, -4bit, -OptiQ-4bit | 5.6-6.0 GB | BFCL-V4 66.1, TAU2 79.1 (VENDOR); BFCL live single 82.4 / multi-turn 52.25 (third-party, KOPA paper arXiv 2609.05395); vLLM parser qwen3_coder documented | F1 (TAU2 = multi-turn agentic), F2/F3 (BFCL multi-turn +1200pt class jump over Qwen3-8B), F4 (well-instruction-following) |
| 2 | Gemma 4 12B-it (Unified) | 11.95B dense | Apache 2.0 (per HF/AI.google.dev gemma4 license; gated download on google/* repos) | lmstudio-community/gemma-4-12B-it-MLX-4bit; mlx-community/gemma-4-12B-4bit; AtomicChat imatrix 4bit | ~6-7 GB | Tau2 avg 69.0 over 3 domains (VENDOR, Google card); native function calling; 256K ctx | F1/F2/F3 (strongest per-parameter reasoner in class; reasoning mode helps self-verification) |
| 3 | Ministral-3-8B-Instruct-2512 | 8.9B (8.4B LM + 0.4B vision) | Apache 2.0 | CAVEAT: mlx-community 4-bit/8-bit 8B conversions are BROKEN (index lists 3 weight files, repo ships 2; per LocalLM Lab audit, all 8B/14B Dec-2025 quantized repos broken; 3B intact; 14B MXFP4 intact). Working paths: lmstudio-community GGUF (llama.cpp) or Ministral-3-14B MXFP4 | GGUF Q4_K_M 5.2 GB | BFCL multi-turn 15% / overall 62 in native-FC mode (third-party SOTA2 board); BEST open-weight on AgentFloor tier E (16%) | F1 specifically (only 8B with nonzero tier-E planning) |
| 4 | Granite 4.2 8B | 8.8B dense | Apache 2.0 | lmstudio-community/granite-4.2-8b-MLX-4bit (4.9 GB); ibm-granite/granite-4.2-8b-q4-mlx (official) | 4.9 GB | BFCLv4 52.39, IFBench 79.33, tau3-bench avg 66.34 vs 8B-class peers (VENDOR, IBM) | F2/F3 (IFBench high = instruction fidelity), F1 moderate |
| 5 | (rejected) xLAM-2-8b-fc-r | 8B | CC-BY-NC-4.0 | -- | -- | BFCL 46.68 (third-party) | License fails local-use-with-mods constraint; flagged RESTRICTED |
| 6 | (rejected) LFM2.5-8B-A1B toolcaller LoRA (hazypiff fastagent) | 8B/A1B + LoRA | inherits LFM Open License (commercial cap $10M) + uncensored base | GGUF on HF | ~5 GB | BFCL-AST non-live 82.8-84.5 (SELF-reported, non-official categories) | Fixes tool-format only; base tool-perception defect remains; flagged |

Notes:
- Qwen3.5-4B matches Qwen3.5-9B on TAU2 (79.9 vs 79.1, vendor) while losing
  ~12 pts on BFCL-V4 -- worth a cheap second experiment if memory gets tight,
  but 4B is NOT the fix for the planner role (AgentFloor shows sub-5B
  collapses at tier C already).
- Qwen3.5-9B BFCL multi-turn at 52.25 (KOPA third-party run) is the highest
  third-party-measured multi-turn number found for any local model under
  16 GB of memory. This is the closest published proxy for our harness's
  multi-step plan-and-execute shape.

## 5. Why the current model fails our specific gates (mapping F1-F4)

- F1 planner spirals: 1.5B active parameters is the architecture-level
  ceiling, per FreshContext: "At 1B active that's about what the architecture
  allows"; the model is reasoning-only so every plan step re-emits
  chain-of-thought, and over-decomposition is the characteristic failure of
  reasoning-mode small models (also flagged by Liquid's own card: "may
  over-think").
- F2 fabricated claims: FreshContext grounding run: 19 fabricated failures
  and 46 misattributions in 125 attempts; vendor non-hallucination-rate
  improvements do not transfer to llama.cpp serving with empty reasoning
  tags (41/60 responses carried malformed tags in their runs -- possible
  format interaction; a vendor-format arm was never tried, so this one
  channel is ANECDOTE-grade: the llama.cpp reasoning-format flag is a cheap
  pre-test before swapping models).
- F3 refusals + seed variance: consistent with a model at its competence
  boundary; nothing model-specific published, but the same FreshContext
  audit recorded intermittent "answered directly" instead of acting -- the
  refusal-of-valid-action shape.
- F4 heuristic fallback misroutes: this is a rig-side degraded mode, not a
  model defect; any candidate with a working classifier prompt improves it
  only indirectly. NOT a model-selection criterion.

## 6. Memory layout options (host verified: Mac Studio M2 Ultra, 64 GB unified memory)

Option A -- single-model swap (drop-in, all roles on one model):

| Component | RAM at inference |
|-----------|------------------|
| Qwen3.5-9B 4-bit MLX (5.6-6.0 GB file) | ~8 GB incl. KV cache at 16-32K ctx |
| llama.cpp router sidecar (kept as-is, 1.2B-class) | ~1-2 GB |
| daemon + harness | ~2-4 GB |
| Total | ~12-14 GB of 64 GB -- trivial fit |

Risk: the planner gets a better model, but the cheap roles lose LFM's fast
co-residency profile; classification latency rises slightly. All-roles swap
is the BLUNT option -- it also changes router/summarizer behavior that
currently passes.

Option B -- planner split (RECOMMENDED): keep the current small models for
router/classifier/summarizer/simple-executor, promote one dense model to
planner alias only:

| Component | RAM at inference |
|-----------|------------------|
| LFM2.5-8B-A1B GGUF (kept for cheap roles) | ~5-6 GB |
| Qwen3.5-9B 4-bit MLX as planner | ~8 GB |
| llama.cpp router sidecar | ~1-2 GB |
| daemon + harness | ~2-4 GB |
| Total | ~17-20 GB of 64 GB -- comfortable; both runtimes co-resident |

Variant B2 (stronger planner): Gemma 4 12B-it 4-bit (~7 GB file, ~9-10 GB
RAM) or Ministral-3-14B MXFP4 as planner instead -- still under 24 GB total.
The rig's own bench window (300 s) should be re-checked: a dense 9-12B
planner is ~4-6x slower per token than 1.5B-active; the run-5 note "8-step
plan too slow for the 8B in 300s" suggests plan-length caps, not model swap,
are also needed.

Variant B3 (cheapest probe, zero new model): re-test LFM2.5-8B-A1B via
llama.cpp with the vendor tool-call format (JSON tool calls in system
prompt) and --reasoning-format fixes before buying RAM. Cheap, likely
insufficient (the FreshContext format caveat is a possible but unproven
partial cause), worth one harness run.

Decision rule: if the bench must pass RELIABLY (multi-seed), take Option B
with Qwen3.5-9B planner + seed-count >= 3. If a single-seed green is
acceptable and latency budget is tight, try B3 first, then B.

## 7. Ranked recommendation with evidence

1. Qwen3.5-9B (planner-split primary)
   - Repo: lmstudio-community/Qwen3.5-9B-MLX-4bit [VERIFIED, 5.95 GB,
     790k downloads]; alternatives mlx-community/Qwen3.5-9B-MLX-4bit and
     mlx-community/Qwen3.5-9B-OptiQ-4bit (mixed 4/8-bit, 8.0 GB, higher
     fidelity: KL 0.029 vs 0.081, per nace-ai audit).
   - License Apache 2.0. Expected RAM ~8 GB.
   - Why: highest third-party multi-turn tool-use score in class (BFCL
     multi-turn 52.25, KOPA arXiv 2609.05395 -- independently run);
     vendor TAU2 79.1 (multi-turn agentic, exactly our planner shape);
     Apache 2.0; dense 9.7B (no 1.5B-active ceiling); documented tool
     parser across runtimes.
   - Risks: vendor TAU2 numbers are self-reported and vendor harnesses
     differ (per vdf.ai comparison notes, TAU2 settings are NOT
     cross-comparable); Qwen reasoning mode can over-decompose like the
     current model -- disable thinking for plan emission or cap plan
     length in the harness; +2 GB RAM vs current.
2. Gemma 4 12B-it (planner-split alternative)
   - Repo: lmstudio-community/gemma-4-12B-it-MLX-4bit [VERIFIED]; gated
     base (google/*) needs license acceptance for the ORIGINAL, LM Studio
     mirror is not gated.
   - License: Apache 2.0 per model card. Expected RAM ~9-10 GB.
   - Why: strongest reasoning per parameter in the size class (Google
     card: Tau2 avg 69.0 vs E4B 42.2 -- the size gradient inside one
     family is direct evidence that moving 8B-class to 12B-class buys
     planning); native function calling; 256K ctx.
   - Risks: Gemma 4 unified arch needs recent mlx tooling (mlx-vlm >= 0.6
     / gemma4_unified model type) -- verify the rig's mlx runtime
     supports it before committing; recommended sampling temp 1.0 is
     high for tool emission, pin lower for planner prompts.
3. Ministral-3-8B-Instruct-2512 (same-footprint swap; GGUF path)
   - Repos: mistralai/Ministral-3-8B-Instruct-2512 [VERIFIED, Apache 2.0];
     lmstudio-community GGUF for llama.cpp [VERIFIED]. MLX CAVEAT: the
     mlx-community 8B 4-bit conversion is BROKEN as of Oct 2026 (LocalLM
     Lab audit: index references 3 safetensors, repo ships 2; model will
     not load) -- use GGUF or wait for a fixed conversion.
   - Expected RAM: ~7 GB (GGUF Q4_K_M 5.2 GB + KV).
   - Why: the ONLY 8B with a nonzero published long-horizon planning
     score (AgentFloor tier E: 16% vs qwen3:8b 0%, GPT-5 10%); native
     function calling + JSON, 256K ctx, explicitly edge-positioned.
   - Risks: BFCL multi-turn 15% is weak (third-party); vision encoder
     adds dead weight for a text-only daemon; the broken-MLX situation
     makes MLX-native serving (mlx_lm runtime) a no-go today.
4. Granite 4.2 8B (conservative same-footprint swap)
   - Repos: lmstudio-community/granite-4.2-8b-MLX-4bit [VERIFIED, 4.9 GB]
     and official ibm-granite/granite-4.2-8b-q4-mlx.
   - License Apache 2.0. Expected RAM ~6-7 GB.
   - Why: smallest change in footprint (4.9 GB file, close to current);
     BFCLv4 52.4 and tau3 66.34 vendor-reported -- above LFM2.5's own
     vendor BFCLv4 48.5-49.7 at the same claimed size, and it is DENSE
     8.8B (no active-param ceiling).
   - Risks: vendor numbers only; planning-specific evidence thin
     (positioned for RAG/laptop use); improvement margin over the current
     model may be inside our seed noise band.

Rejected / flagged:
- xLAM-2-8b-fc-r: best-in-class tool scores but CC-BY-NC-4.0 -- restricted,
  fails the license constraint.
- LFM2.5 toolcaller LoRA (hazypiff): fixes tool FORMAT, base tool
  perception stays 1.5B-active-limited; uncensored base; self-reported
  eval only.
- Nemotron 3 Nano 4B, Gemma 4 E4B/E2B: below the size class for the
  planner role (Tau2 24.9-42.2 vendor; AgentFloor shows sub-5B tier-C
  collapse).

## 8. Answer to the direct question

Is the 8B generally problematic, or specifically as planner?

Specifically as planner -- with one sharpening: the current checkpoint is a
worse-than-typical planner because it is an 8B-total / 1.5B-active MoE, not
a dense 8B. The rig's own evidence (router/classifier, simple executor, and
summarizer roles passing while planner classes hold 100% of the residual
red verdicts) matches the published tier structure (AgentFloor: small models
adequate through tier C, collapse at D/E). No 7-10B model is a reliable
long-horizon planner (best published: 16%); but moving from a 1.5B-active
MoE to a dense 9-12B with third-party-verified multi-turn tool use
(Qwen3.5-9B at 52.25% BFCL multi-turn) is a 10x-class change on the
planner-shaped skill, and the AgentFloor authors' own design principle --
small models for routine roles, bigger models reserved for planning -- is
exactly the planner-split layout this rig can afford at under 20 GB on a
64 GB host.

## 9. Source index

Rig-internal (VERIFIED, read from worktree):
- config/models.json5 (model identities, runtimes, planner alias chain)
- docs/plans/20261119-usability-remediation/07-bench/02-REPORT.md sections
  11-16 (failure classes, residual disposition, seed variance)
- sysctl hw.memsize = 68719476736 (64 GiB), hw.model = Mac14,14

External:
- LiquidAI/LFM2.5-8B-A1B model card (params, license, vendor benchmarks):
  https://huggingface.co/LiquidAI/LFM2.5-8B-A1B and
  https://www.liquid.ai/blog/lfm2-5-8b-a1b [VENDOR]
- Independent LFM2.5 harness audit (tool perception 1/60, abstention 27.8%):
  https://freshcontext.io/benchmarks/models/lfm2.5-8b-a1b/ [THIRD-PARTY]
- Darkgrade Toolbench LFM2.5 29/100 rank 37/37:
  https://darkgrade.com/toolbench/lfm2.5-8b-a1b/ [THIRD-PARTY]
- AgentFloor tier ladder (tier E collapse, model-specific effects):
  https://arxiv.org/html/2605.00334 [THIRD-PARTY, pre-registered]
- BFCL splits for Qwen3-8B / Llama-3.1-8B / Ministral-3-8B (single vs
  multi-turn): https://www.sota2.com/research/sota/tool-calling-on-bfcl-berkeley-function-calling-leaderboard
  and https://octaneural.com/research/small-language-models-ai-agents-2026-evidence [THIRD-PARTY aggregations]
- KOPA multi-step tool benchmark incl. Qwen3.5-9B multi-turn 52.25:
  https://arxiv.org/html/2609.05395v1 [THIRD-PARTY]
- Qwen3.5-9B MLX repos: https://huggingface.co/lmstudio-community/Qwen3.5-9B-MLX-4bit ,
  https://huggingface.co/mlx-community/Qwen3.5-9B-MLX-4bit ,
  https://huggingface.co/mlx-community/Qwen3.5-9B-OptiQ-4bit , quant audit
  https://huggingface.co/nace-ai/Qwen3.5-9B-MLX-4bit [VERIFIED repos; VENDOR benchmarks]
- Gemma 4 12B card + benchmarks (Tau2 69.0):
  https://huggingface.co/google/gemma-4-12B ;
  MLX: https://huggingface.co/lmstudio-community/gemma-4-12B-it-MLX-4bit [VERIFIED repo; VENDOR benchmarks]
- Ministral 3 8B card (Apache 2.0, native FC):
  https://huggingface.co/mistralai/Ministral-3-8B-Instruct-2512 ;
  GGUF https://huggingface.co/lmstudio-community/Ministral-3-8B-Instruct-2512-GGUF ;
  broken-MLX audit https://thisbrain.ai/locallm/mlx-models.html [VERIFIED repo; THIRD-PARTY audit]
- Granite 4.2 8B MLX:
  https://huggingface.co/lmstudio-community/granite-4.2-8b-MLX-4bit ,
  https://huggingface.co/ibm-granite/granite-4.2-8b-q4-mlx ,
  benchmarks via https://slm.expert/best/best-function-calling-model/ [VERIFIED repos; VENDOR benchmarks]
- 8B tool-loop completion rates + prompt scaffolding effect:
  https://www.bigiron.cc/guides/which-small-local-models-can-actually-run-tool-loops-2026 [THIRD-PARTY]
- SLM family ladder (Qwen3.5 4B vs 9B TAU2, license table):
  https://vdf.ai/blog/best-small-language-models/ [THIRD-PARTY aggregation of vendor data]
- xLAM-2-8b license flag: via vdf.ai BFCL note (CC-BY-NC-4.0) [THIRD-PARTY]

Provenance legend: VERIFIED = repo/file existence confirmed from the source
page itself; VENDOR = self-reported by the model maker, harness not
reproduced; THIRD-PARTY = run by an independent party; ANECDOTE = single
un-reproduced observation.
