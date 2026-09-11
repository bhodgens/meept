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
| classifier LLM | LFM2.5-8B (models.json5 `local/lfm-8b-mlx-4bit`) | ~5 GB RAM, ~1-2s |
| intent analyzer | `internal/agent/intent_analyzer.go` — category whitelist, compound splitting | compiled in |

Failure behavior: LLM errors, quota, alias exhaustion → falls through
to Door 3. Never blocks the user on a provider outage.

## Door 3 components in detail

| component | what |
|---|---|
| ambiguity gate | `dispatcher.go` clarify-first; answers resume in quickplan mode |
| orchestrator | strategic planner → tactical scheduler → phase/step plans |
| executor agents | coder/debugger/etc. loops, verification gates on |

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
`tools/classifier-eval/results/alt-methods-correction.md`). Cost when
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
The directory is gitignored BEFORE any text is written: verbatim
message text never enters git (design.md S4); only hash-bearing files
are trackable.

Accepted candidates flow into the corpus with provenance
`added_in: "harvest-YYYYMMDD"`, `source: "live-session"`, then pass
the dedup guard (cosine > 0.95 against the existing corpus ⇒ reject,
the `iter7_harvest.py` pattern) before
`scripts/build_prefilter_centroids.py` rebuilds the Door-1 index.
`--dry-run` prints counts plus a hash-only candidate preview and
writes nothing; exit 0 always (measurement-tool convention).
