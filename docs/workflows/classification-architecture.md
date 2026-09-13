# Classification Architecture — Door Hierarchy

The three-door classification pipeline: every message enters at the top
and falls to the next door only when the current one declines. Nothing
is ever dropped — Door 3 always responds.

## The hierarchy

```
message in
│
├─────────────────────────────────────────────────────┐
│ DOOR 1 — embedding prefilter (instant, free)        │
│                                                     │
│  [1a] embed server (qwen3-0.6B-4bit, :8090)         │
│       text → 1024-number vector        ~90ms        │
│  [1b] centroid store (1 MB JSON, 13 intents,       │
│       202 example vectors)                          │
│  [1c] gate logic (embedding_prefilter.go)           │
│       cosine vs 13 averages + margin 0.030          │
│       + quickplan cue guard (orchestration          │
│       evidence required for quickplan votes)        │
│                                                     │
│  confident match? ──► route            (~35% of     │
│  └─ not confident ──▼                  traffic;     │
│                                        98-100%      │
│                                        precision)   │
├─────────────────────────────────────────────────────┤
│ DOOR 2 — LLM classification chain (1-2s, local)     │
│                                                     │
│  [2a] classifier LLM (LFM2.5-8B, :8082)             │
│       reads message + conversation context          │
│  [2b] validator/normalizer (intent_analyzer.go)     │
│       whitelist check, category coercion            │
│                                                     │
│  classified? ──► route                 (~87% right  │
│  └─ failed/low-quality ──▼              lab; ~84%   │
│                                         live)       │
├─────────────────────────────────────────────────────┤
│ DOOR 3 — quickplan fallback (always responds)       │
│                                                     │
│  [3a] ambiguity gate: unclear? ask first            │
│  [3b] orchestrator plans (strategic→tactical)       │
│  [3c] executor agents run the plan                  │
│                                                     │
│  clarify if needed → plan → execute ──► report      │
│  (also catches: agent-ID resolution failures)       │
└─────────────────────────────────────────────────────┘
```

Side exits before the doors:

- **short/simple guard** — greetings, single words, arithmetic skip all
  doors → chat directly (0.9 confidence)
- **instruction parser** — standing rules never reach classification

## Door 1 components in detail

| component | what | resource |
|---|---|---|
| embed server | qwen3-embedding-0.6B, 4-bit MLX, port 8090 | ~2 GB RAM, Mac GPU |
| centroid store | `classifier_prefilter_centroids.json`, rebuilt via `scripts/build_prefilter_centroids.py` | ~1 MB disk |
| gate logic | `internal/agent/embedding_prefilter.go` + `quickplan_cue.go` | compiled in |

Failure behavior: server down or index unreadable → Door 1 goes inert
(warns once), ALL traffic flows to Door 2. No message is ever rejected
by Door 1 hardware failure.

## Door 2 components in detail

| component | what | resource |
|---|---|---|
| classifier LLM | LFM2.5-8B (models.json5 `local-gguf/lfm-8b-gguf`) | ~5 GB RAM, ~1-2s |
| intent analyzer | `internal/agent/intent_analyzer.go` — category whitelist, compound splitting | compiled in |

Failure behavior: LLM errors, quota, alias exhaustion → falls through
to Door 3. Never blocks the user on a provider outage.

## Door 3 components in detail

| component | what |
|---|---|
| ambiguity gate | `dispatcher.go` clarify-first; answers resume in quickplan mode |
| orchestrator | strategic planner → tactical scheduler → phase/step plans |
| executor agents | coder/debugger/etc. loops, verification gates on |

## Local runtime selection (LFM2.5-8B)

Two providers serve the same LFM2.5-8B-A1B weights. Only the llama.cpp one
can call tools, so it is the default (`model:` in models.json5).

| provider | runtime | port | tool calls |
|---|---|---|---|
| `local-gguf/lfm-8b-gguf` | llama-server, GGUF Q4_K_M, `--jinja` | 8080 | yes — `finish_reason: "tool_calls"` |
| `local/lfm-8b-mlx-4bit` | mlx_lm server, MLX 4-bit | 8083 | no — prose JSON or reasoning-only |

The difference is the runtime, not the model or the chat template. Measured
2026-09-12 with one identical request (the `json_extract` tool plus a short
text): llama-server answered `finish_reason=tool_calls` with a structured
call in 55 tokens, while mlx_lm returned either a bare
`\boxed{"name":...,"arguments":...}` object in prose or the whole reply in a
`reasoning` field with `content` absent. The model knows the tools exist in
both cases — its own `chat_template.jinja` injects `List of tools:` into the
system prompt — so the template is not at fault.

`--jinja` is required on the llama-server entry: without it the template does
not render the tool list and the model emits no call at all.

Port layout for the local endpoints: 8080 llama.cpp general driver,
8081 mlx combined-sft, 8082 the prompt-router sidecar (reserved — not a
provider port), 8083 mlx general, 8084 llama.cpp extractor.
`internal/llm/providers_config_test.go` fails the build if two providers
claim one loopback port, or if any provider claims 8082.

## Observability: what is logged today

Every dispatch logs ONE structured line (the regression-tracking
record):

```
level=INFO msg="Dispatched request"
  agent=coder                    ← where it went
  intent_type=review             ← what it decided
  confidence=0.8543              ← Door 2's self-reported certainty
  classification_method=llm      ← WHICH door fired
  memory_refs=0 has_task=false
```

For Door 1 hits the method reads `embedding_prefilter` — no LLM cost,
no LLM latency, attributed cleanly.

Per-door counters are kept in `Dispatcher.stats` and exposed as JSON
via the `dispatcher.stats` RPC:

- `total_dispatched`
- `by_method` — traffic per door (embedding_prefilter / llm / keyword /
  fallback / short_message_guard / ...)
- `by_agent`, `by_intent`
- `fallback_count` + last 100 fallback entries (timestamp, input
  prefix, method, confidence, routed-to)

## What is NOT logged today (the accuracy-improvement gap)

1. **No outcome feedback.** The pipeline records what was decided, not
   whether the decision was RIGHT. A message routed `code` that the
   user then redirects ("no, I meant review") leaves no correction
   trace. Accuracy can only be measured offline (the eval harness +
   gold replay), not improved in-loop.
2. **No Door 1 margin distribution capture.** Near-miss abstentions
   (margin just under 0.030) are the harvest candidates — currently
   only visible via debug logs, not persisted.
3. **Fallback inputs are capped** at the last 100 (in-memory only;
   restart clears them).

## Improvement path (when traffic justifies it)

- persist `Dispatched request` lines (they are already structured) to
  the metrics DB — gives per-door traffic + misroute investigation for
  free
- add an explicit user-correction event (user re-routes within N turns
  → negative example for the next centroid rebuild)
- periodic centroid rebuild from the accumulated log (the build script
  already accepts any corpus file)

**The two brains must agree (tfidf-veto). — SHIPPED with unvalidated
numbers** Door 1 routes only when a second, cheap classifier — a
character n-gram TF-IDF logistic, no embedder call — agrees with the
centroid's pick. Disagreement falls to the LLM chain. The gold replay
claimed 87.35% (vs 84.56% without the veto), but that decomposes to
**2 routes out of 48** plus chain credit — a 2-case effective sample
that cannot distinguish a perfect router from a coin-flip (see
`tools/classifier-eval/results/alt-methods-correction.md`). The
acceptance record carries this row as **UNVALIDATED, not PASS**: the
only committed producer was a double-confidence-only script, and the
run needs the untracked replay corpus plus a live :8090 embed server
(`results/m4-gold-acceptance.md`, CORRECTIONS 1-2). The acceptance
script now refuses PASS/FAIL when the routed sample is below its
coverage floor (`MIN_ROUTED = 20`). Cost when
adopted: ~2 MB model file, ~0.4ms per message, no new service.
Status: **code SHIPPED** (`internal/agent/tfidf_veto.go` +
`scripts/build_tfidf_veto.py`; missing model file = veto disabled =
legacy behavior), **effect UNVALIDATED** — re-validate on a larger
harvested corpus before trusting it. Until then, accuracy improvement
runs through the offline campaign loop: harvest → adjudicate →
rebuild → measure (now with live outcome capture feeding it).

## Harvest loop (classifier-outcome-loop leaf 04)

`tools/classifier-eval/harvest_outcomes.py` turns the persisted
outcome signals (dispatch_log: `outcome`, `corrected_agent`, `margin`,
`input_hash`) into corpus work. It opens metrics.db READ-ONLY
(`file:...?mode=ro`) — the only write path is `--apply-views`, which
creates the idempotent accuracy views (`v_door_accuracy`,
`v_correction_rate`, `v_margin_hist`, `v_fallback_trend`); run it
against a COPY unless you accept view objects in the live store.

Cadence: nightly is the intent, but meept does not self-schedule —
launchd/cron wiring is the operator's choice. Typical run:

    python3 tools/classifier-eval/harvest_outcomes.py \
        --db ~/.meept/metrics.db            # read-only
    # review the sheet, then apply views on a copy:
    python3 tools/classifier-eval/harvest_outcomes.py \
        --db /tmp/metrics-copy.db --apply-views

Outputs land in `tools/classifier-eval/harvest-YYYYMMDD/`
(`--out` overrides): `candidates.json` (corrected/failed_replan rows,
hashes only — safe to track), `nearmiss.json` (Door-1 margins in the
band, default 0.025–0.035, routed + abstained), `views.sql`, and
`sheet.local.md` — the user-message text recovered from
`~/.hermes/sessions/session_*.json` by session id + nearest timestamp.
The harvest OUTPUT directory is gitignored BEFORE any text is written,
so recovered text stays local; only hash-bearing files are trackable
(design.md S4).

**Corrected invariant (2026-09-12 audit wave).** An earlier revision of
this section asserted "verbatim message text never enters git". That was
false and is withdrawn: prompts harvested from live sessions and
adjudicated into the committed
`testdata/eval/classifier-adversarial-corpus.json5` ARE verbatim user
messages, committed as text. They are labelled `source: "live-session"`
(an earlier wave stamped several of them `"hermes-inspired"`, which
understated real private traffic — those rows were relabelled). Treat
the committed corpus as containing private traffic: do not publish it,
and screen every addition.

Accepted candidates flow into the corpus with provenance
`added_in: "harvest-YYYYMMDD"`, `source: "live-session"`, then pass TWO
disjointness guards before `scripts/build_prefilter_centroids.py`
rebuilds the Door-1 index: (1) cosine > 0.95 against the existing corpus
⇒ reject (the `iter7_harvest.py` pattern); and (2) exact match or cosine
> 0.95 against the adjudicated replay ruler (`replay-gold.local.json5`)
⇒ reject, so a new corpus row cannot leak into the very ruler that
scores the models (train-on-test guard; `eval_harness.replay_disjointness`,
also enforced inside `m4_gold_acceptance.py` — it refuses to score a
leaked case). `--dry-run` prints counts plus a hash-only candidate
preview and writes nothing; exit 0 always (measurement-tool convention).

**Guard hardening (2026-09-13 fix wave).** The ruler guard now refuses
three inputs it previously accepted silently: an EMPTY replay sample
(exit 4 — an empty sample is never "disjoint", and scoring it measures
nothing), a supplied embedding row with a zero or non-finite norm
(`DegenerateVectorError`; `0/(0+1e-12)` had read as a 0.0 cosine, i.e.
"disjoint", and the Embedder stores zero vectors when the server returns
one), and it now reports how many rows it actually compared. It also
prints the top-5 cosine margins (max similarity per replay row) beside
the leak list: the `sim 0.95` threshold has thin headroom, so the
near-miss band is surfaced before a new corpus anchor or embedder upgrade
turns a non-leak into a hard exit-2, with a recorded, reason-bearing
near-duplicate allowlist (`NEAR_DUP_ALLOWLIST`) as the explicit escape
hatch. `m4_gold_acceptance.py --self-test` (stdlib + numpy, no embed
server, no replay corpus) pins these behaviours and the `MIN_ROUTED = 20`
coverage floor. Full record: `results/m4-gold-acceptance.md`
CORRECTIONS 4.

## Router lanes come from the routing table (single source of truth)

The prompt-router sidecar (`scripts/prompt_router_sidecar.py`, :8082) does
not own its lane list; it resolves lanes once at startup, in this order:
`ROUTER_LANES_FILE` (alias `ROUTER_LANES_JSON`) pointing at the JSON artifact
`meept lanes --json` prints -- `{"lanes":[{"intent":...,"agent":...}],...}` --
then the comma-separated legacy `ROUTER_LANES`, then the built-in 9-lane
default. A missing or malformed artifact falls through, never crashes; the
winner is logged once to stderr (`prompt-router lanes source=json count=27`).
The prompt-router provider's `spawn_command` may set `ROUTER_LANES_FILE` so a
spawned sidecar reads the table. Regenerate after any agent/intent frontmatter
change: `./bin/meept lanes --json > /Users/caimlas/.meept/prompt_router_lanes.json`
