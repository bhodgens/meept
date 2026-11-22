# Leaf 02 — Completion honesty: unbacked claims and denied tools

## DISPATCH INSTRUCTION

Implement this leaf fully. Do NOT commit. Do NOT run git add. Write code,
run tests, report results only. Working dir: the plan worktree. Grep
target symbols first (verify-or-apply).

## Meta
Parent: master.md (20261121-interactive-usability).
Scope: turn a "completed with zero artifacts" outcome into an honest
failure or a labeled reply.
Dependencies: none (wave 1; file-disjoint from 01 except loop.go —
coordinate: leaf 01 touches loop.go:9802-9818 progress publication ONLY;
this leaf touches the unbacked-claim guard region and review path).
Estimated context: 85K.

## Goal

02 — Completion honesty: unbacked claims and denied tools.

## Contract

Implements master C-2. No exported-type changes. Two gates:
(a) unbacked-claim guard (nudge once) -> FAIL when zero tool executions
back a claimed file side-effect; (b) permission-denied primary tool call
marks the step errored so "errored steps never pass review" engages.

## Tasks

1. GATE (RED first): e2e suite `e2e/suites/completion-honesty/`:
   (a) fake model narrates a file_edit tool call as text (transcript
   fixture), task must NOT end completed — expect failed terminal with
   honest error; (b) tool permission denial on the step's primary
   action (fence denies the path) — step must be errored, task failed,
   reply carries the denial reason. Manifest + path_map (internal/agent,
   internal/tools). RED run recorded.
2. `internal/agent/loop.go` unbacked-claim guard (search
   "Unbacked file side-effect claims"): today nudges once then allows.
   Change: after the bounded nudge retry, if the turn STILL claims file
   side effects with zero successful file-tool executions, terminate
   the step as errored with "claimed file changes without executing
   tools" (mirrors the terminatedOnAnnouncedAction guard beside it).
3. Trivial-review bypass: `internal/agent/review` path ("Skipping full
   review: trivial task (<3 steps), using heuristic") — the heuristic
   must check stepHasError FIRST (contract: errored steps never pass
   review) and must not approve a step whose result is machine-shaped
   tool-call JSON (reuse the reply-guard detector shapes).
4. Permission-denial propagation: find the executor's deny path
   ("Tool blocked by security" log, internal/agent executor.go ~1591)
   — when the denied call was the step's primary intended action (the
   only/first tool call of the turn), record it on the step as an error
   result, not a silent continue. Non-primary denials keep the honest
   degrade-with-note behavior.
5. The reply renderer keeps the existing
   `[unverified: no tools were executed this turn]` label for the
   narrated-JSON shape that still completes (multi-tool turns).
6. Tests to GREEN: `make e2e-fast-area AREA=completion-honesty`;
   `go test -p 2 ./internal/agent/...`.
7. Report files changed + which guard fired in each fixture.

## Interface Contract Section

None exported. Behavioral contract: zero-artifact completions of the
two probed shapes (probe B task-...0007; bench open-ended-judge row)
become failures with honest reasons.

## Self-Verification Checklist

- [ ] Gate RED -> GREEN recorded.
- [ ] Mutation check: neutering the new guard flips the gate test RED
      (run once, restore).
- [ ] No change to the empty-completion/quota lanes (verified-graceful
      list in the report stays true).
- [ ] Existing e2e suites touching review still pass
      (make e2e-fast-area AREA=step-pipeline if present).

## Review Checklist

- [ ] Guard lives at the loop seam, not in the renderer.
- [ ] Denial propagation does not error turns where the model
      legitimately retried a different tool after denial.
- [ ] Error texts actionable (name the path/tool).

## Do NOT commit

The orchestrator stages and commits after review.
