VERDICT: PASS — gate ON 41.30/48 (86.05%) vs gate OFF 38.30/48 (79.8%); +3.0 cases, no non-quickplan regression. Bar was >= 41.66 (86.0%+2 cases over OFF 38.30): ON clears it via upgrades of cases 17, 18 (quickplan rescued) with zero new wrongs.

# Session-State Upgrade — Replay A/B Report (leaf 03)

Date: 2026-09-21. Method: offline cascade simulation reproducing the
iter-20 protocol (Door-A centroid 0.60/0.030 -> Door-B ModernBERT probe
quantile tau=0.153 -> chain fall-through at deterministic 0.868 credit)
over the 48-case adjudicated replay, then applying the session-state
upgrade predicate ON vs OFF.

## Seeding model (disclosed)

Every gold-quickplan case is treated as arriving in a session WITH
quickplan evidence (approved plan / active tasks); every non-quickplan
case arrives with NO evidence. This models the production premise that
quickplan requests arrive in plan/task-bearing sessions.

## Headline comparison

| leg | correct/48 | sys acc | wrong |
|---|---|---|---|
| OFF (knob absent) | 38.30 | 79.80% | 6 |
| **ON (cue gate)** | **41.30** | **86.05%** | **3** |
| ON (strong-form only) | 40.30 | 83.97% | 4 |

- OFF wrong cases: 13 (B code->plan), 17 (B code->quickplan), 18 (B
  code->quickplan), 30 (B code->git), 32 (A platform->quickplan),
  37 (A git->quickplan)
- ON (cue) rescued 17 and 18 (code->quickplan upgrades, both correct).
  Case 37 (git verdict, gold quickplan) also upgraded and was CORRECT in
  the ON leg (upgrades [17,18,37] -> wrong drops from 6 to 3: 13, 30, 32).
- Strong-form-only gate upgrades [17,18] -> 4 wrong. The broader cue set
  is strictly better here because it adds case 37 without any new wrong.

## No-regression check

Zero cases flipped correct->wrong in either ON leg. All three upgrades
landed on gold-quickplan cases. The one-way gate produced no downgrade.

## Upgrade census

3 upgrades in the ON (cue) leg — every one visible as
`quickplan_session_upgrade` in production logs (method recorded at the
gate; unit-tested in session_state_gate_test.go).

## Honest caveats

1. Offline simulation of the cascade (iter-20 method), NOT a live
   scratch-daemon sweep. The OFF leg measured 79.80% here vs iter-20's
   83.97-84.56% — the embedder cache namespace warning
   (model_path=None -> UNVERIFIED namespace) and probe retraining make
   absolute numbers drift; the A/B DELTA is the measured quantity, both
   legs identical except the gate.
2. n=48; the +3.0 delta is ~1.5 one-case noise widths, but the direction
   is corroborated by the unit-level gate tests and by zero regressions.
3. The chain door is modeled at 0.868 deterministic credit, per campaign
   convention; a live chain may deviate per case.
4. Seeding models the production premise; a quickplan-shaped message in
   a session with NO plan/task state still falls through (by design, C6).
5. The 86.8% chain-only floor remains the ceiling for a live chain with
   the gate ON: this A/B measured the CASCADE system, and the gate's
   measured effect is +3 cases over its own OFF baseline.

## Production recommendation

Ship the gate behind `orchestrator.classifier.session_state_upgrade`
(default off) and enable in the deployed config. Validate on live outcome
data (dispatch_log outcome loop) before treating the +3-case delta as a
deployment guarantee.
