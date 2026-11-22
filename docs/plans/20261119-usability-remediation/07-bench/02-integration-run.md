# Integration Run + Acceptance Report - Implementation Leaf (OPS)

> **For the implementing agent:** This is an OPS leaf - run the battery,
> collect evidence, write the report. Verify files-exist + verdicts, NOT
> tests-pass. Do NOT commit. Do NOT git add. Worktree (daemon build source):
> /Users/caimlas/git/meept/.hermes/worktrees/usability-remediation.
> Bench worktree: /Users/caimlas/git/meept-bench/.hermes/worktrees/usability-acceptance.
> DISPATCH ONLY AFTER root confirms branches 01-06 REVIEWED.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Prove the remediation on the isolated rig; compare vs baseline; report residuals
- **Dependencies:** 01-harness-hardening.md, ALL branches 01-06 REVIEWED
- **Estimated Context:** 70K
- **Concurrency Group:** B (last)
- **Audit references:** R6/R7/R8; I8; REPORT.md section 8 baseline

## Goal

Run the hardened usability suite + p1-p6 probes + regression suite on a
fresh isolated quota-free rig built from the remediated HEAD; prove 0
CRITICAL reproductions and terminal-delivery green; write the acceptance
comparison report.

## Context

- Rig recipe: /tmp/usability-rig/setup_rig.py (isolated MEEPT_HOME, OS-assigned
  ports avoiding 8080/8081/8082/8084/8143, local 8B + prompt-router, quotas
  off). Rebuild daemon: cd meept-worktree && go build -o /tmp/usability-rig/meept-daemon
  ./cmd/meept-daemon (+ meept CLI). Boot: MEEPT_HOME=... HOME=... meept-daemon -f.
- Probes: /tmp/usability-rig/p1_naive.py .. p6_control.py (run SERIALLY).
- Bench: hardened suites/usability-probe.json via run_bench_clean.sh pattern
  (fresh fixture repo + scratch + out dir; --auto-approved --judge-cmd
  "python3 scripts/judge-local.py"; JUDGE_URL=rig llm port).
- Baseline: /tmp/usability-rig/bench-results-clean/results.jsonl (3 pass/3
  fail) + REPORT.md section 8 + findings-orchestrator.md F-ORCH-1..29.
- Regression: suites/regression.json (dev gate; memory-recall-marker xfail last).
- Isolation guard: bench refuses live ~/.meept socket (exit 2) - prove it.

## Verified Anchors

- /tmp/usability-rig/setup_rig.py - rig provisioning
- /tmp/usability-rig/run_bench_clean.sh - bench invocation pattern
- /tmp/usability-rig/bench-results-clean/results.jsonl - baseline rows

## Interface Contracts (From Parent)

### What This Leaf Exposes
- /tmp/usability-rig/REPORT-REMEDIATION-ACCEPTANCE.md: per-task prior vs new
  verdict + evidence paths; CRITICAL reproduction matrix (F-ORCH-1..29 each
  reproduced/fixed/residual); terminal-delivery results; residual findings.

### What This Leaf Consumes
- C1-C7 (assertion targets), hardened suite (01).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Rebuild + boot rig | Build daemon+CLI from meept worktree HEAD; provision fresh rig | grep 'daemon: running' + 2x 'Runtime started and healthy' daemon.log | stale binary | healthy | Given remediated HEAD; Then rig boots quota-free; provenance.json records binary sha256 + HEAD |
| T2 | Run battery serially | Run usability-probe suite + p1-p6 + regression.json | results.jsonl rows + probe JSONs exist | - | complete | Given healthy rig; Then every task/probe runs to verdict (no harness error rows); isolation guard proven (live socket refused) |
| T3 | Compare + report | Create: /tmp/usability-rig/REPORT-REMEDIATION-ACCEPTANCE.md | report exists, per-task table complete | - | written | Given new results + baseline; Then per-task prior/new verdict + evidence; CRITICAL matrix 0 reproductions (or residuals named with cause); terminal-delivery green; limitations stated |

## Self-Verification Checklist

- [ ] Rig isolation proven (never live socket); quotas disabled proven
- [ ] Every CRITICAL F-ORCH finding marked reproduced/fixed/residual with evidence
- [ ] Per-task comparison vs CLEAN baseline (not load-contaminated run-1)
- [ ] Residual findings named with root cause + follow-up leaf suggestion
- [ ] Report ASCII-clean; evidence paths resolve

**DO NOT COMMIT.** Orchestrator handles git after review.

## Completion Report Format

1. Rig provenance path. 2. Battery verdict summary. 3. Report path. 4. Residuals.

## Review Checklist (For Review Agent)

- [ ] No CRITICAL reproduction claimed without evidence (or residual named)
- [ ] terminal-delivery checker green for EVERY submitted turn
- [ ] Baseline comparison honest (clean run only)
- [ ] Report limitations stated (GUI pixel pass operator-pending)
- [ ] No artifacts / corruption

Output: APPROVED or gaps with evidence references.

## Notes

- If a CRITICAL still reproduces: DO NOT mark the tree COMPLETE - report the
  residual to root, which re-opens the owning branch leaf (max 3 cycles).
- GUI live pixel verification remains operator-pending (master Q3/I4).
