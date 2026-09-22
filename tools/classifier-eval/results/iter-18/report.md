# Iteration 18 Report — Wave 4 (plan-execution/docs expansion, +39 cases)

Date: 2026-09-08. Corpus 275 → 314 (base 139 + adversarial 175).
Subagent-authored per the M4 silver gap: 10 plan-execution→code,
8 doc-artifact→code, 3 dominant-verb compounds, 10 config/ops code,
2 report, 2 plan, 4 compound-abstain (ood). Dedup guard: 0 rejected.
Provenance `iteration-18`/`hermes-inspired` on all.

## Silver re-validation (validate_silver.py re-run)

| metric | pre-wave-4 | post-wave-4 |
|---|---|---|
| stage A | 11 routes, 8 correct (72.7%) | 9 routes, 7 correct (77.8%) |
| stage B | 4/4 (100%) | 4/5 (80%) |
| chain | 33 | 34 |
| expected system acc | 84.7% | **84.4%** |

E2E essentially unchanged (−0.3pt, within noise). Miss analysis:

1. **"[replay case 338cb3b2e9ffa8c4]" now ROUTES — to plan, confidently (0.850,
   margin 0.040).** The wave-4 anchors taught the gate that
   "[replay case 338cb3b2e9ffa8c4]" = code, but the plan centroid absorbed the
   phrasing too: the pre-existing plan class ("create a project
   roadmap", "design the architecture") is lexically closer to the
   silver label's phrasing than the new code anchors are. The silver
   label itself is contestable — "[replay case 338cb3b2e9ffa8c4]" reads plan-ish;
   under the campaign invariant this specific case is a silver-label
   ambiguity, not a clean gate miss.
2. **"[replay case 2c0434047ecad05e]" now correctly ABSTAINS**
   (margin 0.018 < 0.030): the two centroids now compete, and the gate
   falls through to B/C instead of confidently choosing platform. This
   is the invariant working: fall-through replaced a probable wrong.
3. Stage-B dropped one (git→code on "commit-omit-env [replay case effcf40fbc1160e3]"): the
   added code anchors pulled the probe's decision boundary; a
   git-classified message with heavy code vocabulary. n=5, noise-level.

## Assessment

The wave did NOT yet lift real-traffic accuracy: 84.4% vs 84.7% (noise),
still under the 86.8% chain floor. The plan/code boundary for
execution-phrased messages is now the campaign's central ambiguity —
both stages split on it. Options for iter-19:

(a) **Silver-label adjudication first**: hand-label the 48 silver cases
    (30 min of user time or a careful re-read), since two of the three
    "misses" are label-defensible either way. Measuring against
    contested labels caps the achievable score artificially.
(b) Add MORE distinctive plan-execution anchors (verb-first: "execute",
    "dispatch", "work through" — currently anchored but thin).
(c) Accept the fall-through behavior for the plan/code boundary as the
    correct outcome (the invariant: fall-through < wrong) and measure
    the cascade by how OFTEN it abstains vs misroutes there.

Recommendation: (a) then (b). The measurement is currently limited by
label quality, not model quality.
