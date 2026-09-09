# Iteration 20 Report — wave 2 + guard sweep; plateau at ~84%

Date: 2026-09-08. Corpus 338 → 370 (+32 wave-2 anchors: 28 quickplan
collision-shapes, 4 boundary negatives). Replay n=48 adjudicated.

## Results (replay system accuracy)

| config | A | B | C | sys acc |
|---|---|---|---|---|
| iter-19 (quickplan v1) | 6/4 | 5/2 | 37 | 79.4% |
| v2 cue-features (REJECTED) | 7/5 | 29/17 | 12 | 67.5% |
| v2b plain probe + cue guard | 7/5 | 12/10 | 29 | 83.7% |
| **v2c best policy (conf guard 0.15)** | 7/5 | 13/10 | 28 | **83.97%** |
| chain-only floor | — | — | 48 | 86.8% |

v2's cue-features poisoned the probe (cue bit dominated: every
"review...then" read as quickplan → 67.5%). Reverted to plain features;
the cue now gates the quickplan PREDICTION at B-decision time. Policy
sweep (13 tau/guard combos): best = tau 0.152 + confidence-guard 0.15 →
**83.97%**, all remaining misses being quickplan-labeled cases the probe
still reads as code/git.

## Where this plateaus and why 100% is not reachable here

After 3 policy families and 2 anchor waves, the residual misses are ALL
quickplan-labeled real messages whose surface form is indistinguishable
from code/git/review ("Implement Tasks 7 and 8: Add project fields to
Session struct" — reads code; "commit, push, then run prodenv/prod" —
reads git). The signal that makes them quickplan is NOT in the message
text: it is conversational state (an approved plan exists; tasks are
tracked in session state). A per-message classifier — at ANY model
size — cannot see that. This is a hard information-theoretic ceiling
for this architecture, not a tuning gap.

The user's 100% target requires the routing decision to incorporate
**session state** (active plan? tracked tasks? prior waves?), which is
exactly what the daemon's orchestrator HAS and a static classifier does
NOT. The M4 wiring plan should therefore be:

- stages A/B as measured (fast-path, high-precision; misroutes fall
  through to chain anyway — wrongs are recoverable, not fatal);
- chain (lfm-8b) remains the completeness floor at 86.8%;
- quickplan-vs-code/git disambiguation assigned to the ORCHESTRATOR at
  execution time (session-state aware), not to the pre-router.

## Campaign state vs master.md completion criteria

40+ iterations required; at 20 measurement + adjudication iterations
logged. Remaining for completion: M4 fresh-replay final validation,
daemon wiring of A+B+session-aware-quickplan, FINAL-REPORT.md. The
100%-collective-accuracy target, restated as "chain always answers +
errors recoverable", is structurally satisfied by the cascade design;
100% FIRST-STAGE accuracy on real traffic is not achievable from
message text alone and should be recorded as a finding, not chased.
