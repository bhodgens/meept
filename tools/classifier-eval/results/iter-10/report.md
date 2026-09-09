# Iteration 10 Report — Harvest wave 2 (+39 cases)

Date: 2026-09-08. Corpus 225 → 264 (base 139 + adversarial 125). Dedup
guard: max cos 0.887, all 39 accepted. Targets from iter-10 diagnosis
(132 non-OOD abstains; near-miss table in iter10_diag.py output):
code non-fix-verb anchors ×8, debug error-shaped ×6, review ×5 (weakest
class: top-1 confused with debug at 0.884), search retrieval-verbs ×4,
analyze judgment-verbs ×4, plan ×3, report ×3, recall ×3, platform ×3.

## Measurement

| config | C | P | wrong | E2E | SCORE |
|---|---|---|---|---|---|
| centroid m0.025 | ~37.6% | ~97% | 2-3 | ~90.9% | — |
| centroid m0.030 | 33.5% | 97.5% | 2 | 90.38% | +0.276 |
| centroid m0.035 | 28.0% | 98.5% | 1 | 90.08% | +0.251 |
| kNN k=5 unanimity | 9.2% | 90.9% | 2 | 87.18% | +0.034 |

## Findings

1. **E2E dipped (91.29 → 90.38 at m0.030)** — the wave pushed coverage
   cases into the test fold (all 39 first-seen in TEST per protocol),
   raising the bar while the index gains lag one iteration. Expected
   mechanical effect of corpus growth; the correct read is the
   PRECISION-COMPLIANT frontier, not raw E2E iteration-over-iteration.
2. Two new wrongs at m0.030 are HARVEST SELF-INFlicted label disputes:
   `produce a changelog for the release` [report→git] — a changelog
   IS release/git-adjacent; and `how do I connect you to my repo?`
   [platform→git] — "connect repo" reads git-ish. Hand-audit verdict:
   changelog-production is report (keep), repo-connect is platform
   (keep) — both stay as intentional boundary cases; the gate should
   ABSTAIN on them, and at m0.035 it does (wrong=1: changelog only).
3. kNN unanimity degraded further (P 90.9%) — corpus growth keeps
   hurting the unanimity head while helping centroid. The head gap is
   now decisive: centroid-margin is the only viable head at this
   embedder.

## Iteration verdict

Measurement iteration; no gate change. Next: iter-11 adds the compound
pre-stage experiment (two-pass centroid: route only when BOTH passes
agree — targets compound gold-abstain cases without sacrificing single
intent coverage), plus micro-cluster length-floor simulation.
