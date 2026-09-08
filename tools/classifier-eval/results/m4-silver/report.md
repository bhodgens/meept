# M4 Interim — Silver-replay validation (first real-traffic evidence)

Date: 2026-09-08. Corpus: 48 verbatim user messages harvested from 436
Hermes session transcripts (tools/classifier-eval/harvest_hermes.py →
replay-corpus.local.json5, UNTRACKED per user directive). Labels are
SILVER (pattern heuristics, spot-audited). Model: 3-stage cascade
(A centroid / B ModernBERT probe / C chain @ 86.8%), stage-B trained on
ALL gold cases (deployment shape), silver fully held out.

## Result

| stage | routes | correct | precision |
|---|---|---|---|
| A (centroid) | 11 | 8 | 72.7% |
| B (probe) | 4 | 4 | 100% |
| C (chain) | 33 | ~28.6 expected | 86.8% (modeled) |

**Expected system accuracy on real traffic: 84.7%** — BELOW the synthetic
corpus's 92.8% and below even the chain-only 86.8%.

## Why the gap (3 causes, all visible in the misses)

1. **Domain shift in intent mix.** Real Hermes traffic is
   plan-execution-heavy ("implement the plan using subagents" ×2,
   doc/report writing, resume creation) — the gold corpus has no
   plan-execution or document-writing intents. A routed these to
   plan/platform by surface similarity. These are taxonomy gaps, not
   gate bugs: meept will also receive such messages, so the taxonomy
   needs a `plan`-execution boundary decision or an `ops`/`docs` intent.
2. **Silver labels are themselves noisy** (48 hand-audited examples; the
   "implement the plan" → code label is defensible but the gold corpus's
   plan class pulls the other way). Real measurement needs gold-promoted
   replays or human labels.
3. **Small n**: 48 cases → single-route swings move percentages by 2+pts.

## Conclusions

- The synthetic 92.8% does NOT transfer to real traffic yet. The
  cascade's stages behaved as designed (B perfect, A below synthetic
  precision on shifted traffic), but the training distribution is too
  narrow for the real message mix.
- Highest-value next lever: **taxonomy + corpus expansion from the real
  mix** — add plan-execution/document-writing/OOD-for-meept cases to the
  gold corpus (or explicitly classify them as chat-fallthrough), retrain,
  re-validate on silver.
- The campaign should NOT wire the daemon until silver-replay system
  accuracy ≥ chain-only (86.8%) with margin.

Artifacts: results/m4-silver/cascade-validation.json (misses enumerated),
replay-corpus.local.json5 (untracked), harvest_hermes.py (tracked —
reusable harvester, no private data in it).
