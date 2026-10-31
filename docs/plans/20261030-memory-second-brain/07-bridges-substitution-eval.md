# Bridges Substitution Experiment - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. This is an EVALUATION + DECISION leaf: most
> deliverables are eval runs + a decision record, plus a small harness
> addition. Do NOT commit - the orchestrator handles all git operations
> after review. Do NOT use read_file on existing source files. Write once
> and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Run the top-k-substitution experiment the bridges live eval called for; record the keep-or-flip-back decision with artifacts.
- **Dependencies:** 02-defaults-on.md (bridges now default-ON)
- **Estimated Context:** 60K
- **Concurrency Group:** D
- **Audit references:** F-matrix none (evidence leaf); R2

## Goal

The recorded verdict on bridges (IndexRAG) was "keep off pending a
top-k-substitution experiment or observed two-hop recall misses" because the
live eval TIED at 87.50% both arms with ~10% more context tokens - the
fixture's facts always fit top-8, so bridges could not demonstrate
substitution value. Per user decision D1, bridges are default-ON NOW; this
leaf runs the experiment that can flip them back OFF. The flip direction is
EVIDENCE-DRIVEN, not assumed.

## Context

- Eval harness: tools/bridges-eval/ (go run ./tools/bridges-eval/ [--live]);
  existing artifacts: docs/research/eval/bridges-eval.json (mock) and
  bridges-live-eval.json (live, LFM2.5-8B GGUF via llama-server, tie at
  87.5%).
- Live-eval lanes must handle reasoning models (reasoning_content fallback,
  max_tokens >= 2048) - the harness already fixed this (liveAnswerClient).
- The substitution experiment: build a fixture where relevant facts DO NOT
  all fit in top-k (k=8): >8 relevant memories per question so the bridge
  graph must substitute/connect what flat top-k misses. Arm A = flat top-8,
  arm B = top-8 + bridges. Hypothesis under test: bridges recover
  two-hop/missed-fact questions that flat recall cannot.
- Local model discipline: spawn the eval's OWN llama-server
  (--model <gguf> --port <p> --jinja --ctx-size 8192), verify /v1/models not
  just /health, kill it after; a /health responder is not proof of an
  OpenAI-compatible server.

## Verified Anchors

- tools/bridges-eval/ - harness main; fixture layout under it (grep fixture
  files at implementation time).
- docs/research/eval/bridges-live-eval.json - prior live artifact (the
  baseline this experiment extends).
- internal/memory/bridges.go (GenerateBridges) + bridges_store.go - the
  mechanism under test.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
docs/research/eval/bridges-substitution-eval.json  - run artifact
docs/research/eval/bridges-substitution-decision.md - decision record
(+ harness fixture addition committed under tools/bridges-eval/ if the
 harness gains a -fixture flag; keep the flag additive)
// Decision rule (frozen): bridges STAY default-on iff arm B (bridges)
// strictly beats arm A on substitution questions by >= 5 percentage
// points AND does not regress non-substitution questions by > 2 points.
// Otherwise: flip memory.bridges.enabled back to false (code default +
// template + docs) in a follow-up commit the orchestrator authorizes.
```

### What This Leaf Consumes

- tools/bridges-eval harness; local GGUF model (LFM2.5-8B or equivalent on
  /Volumes/LLMs per host convention).

## Tasks

| Task ID | Objective | Files | Verify command | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|-----------------|
| T1 | Substitution fixture (>8 relevant facts per question, bridge-answerable) | Create/Modify: tools/bridges-eval/ (fixture dir or -fixture flag) | `go run ./tools/bridges-eval/ -fixture substitution -dry` (or equivalent listing) | fixture validates: every question has >8 relevant facts; >=30% of questions require a two-hop connection top-8 flat recall misses | Given the fixture; When validated; Then constraints hold and are printed |
| T2 | Live run both arms | Run (not code): spawn own llama-server, run arm A + arm B | eval output JSON | both arms complete; per-question verdicts recorded; token counts captured | Given the fixture + local model; When both arms run; Then artifact written with both arm scores |
| T3 | Decision record | Create: docs/research/eval/bridges-substitution-decision.md | orchestrator review | decision per frozen rule, evidence cited, flip-back executed or explicitly kept with numbers | Given arm scores; When rule applied; Then decision + next action stated unambiguously |
| T4 | ASCII + commit hygiene | Modify: new docs normalized | `bash .githooks/pre-commit-ascii <new files>` | clean | Given new artifacts; When checked; Then no non-ASCII (em-dashes/arrows removed) |

## Self-Verification Checklist

- [ ] Own llama-server spawned, verified via /v1/models, killed after
- [ ] Both arms ran on the SAME fixture + model + seeds (controlled)
- [ ] Decision follows the FROZEN rule - no judgment overrides without
      explicit user sign-off in the report
- [ ] Artifacts complete; ASCII-clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched**
2. **Scores:** arm A vs arm B overall + substitution-only subset
3. **Decision:** [stay-on / flip-back per rule]
4. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Fixture genuinely requires substitution (spot-check 3 questions)
- [ ] Run controllable (same model/fixture; report timestamps + model hash)
- [ ] Decision record cites the artifact numbers; rule applied correctly
- [ ] No fabricated results - raw output paths cited for every number

Output: APPROVED or specific gaps.

## Notes

- Wall-clock: two live arm runs are ~20-40 min combined on the local stack;
  run them background + notified, poll the log, never blind-sleep.
- If the local model cannot complete the run (quota/runtime issue), STOP and
  report BLOCKED with the exact failure - do not simulate results.
- This leaf's flip-back authority: the ORCHESTRATOR executes the flip commit
  (leaf 02's knobs) after reviewing the decision record - the leaf itself
  does not edit config.
