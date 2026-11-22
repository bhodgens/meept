# Leaf 07 — Scaffolding strip at the payload boundary

## DISPATCH INSTRUCTION

Implement this leaf fully. Do NOT commit. Do NOT run git add. Write code,
run tests, report results only. Working dir: the plan worktree. Grep
first (verify-or-apply).

## Meta
Parent: master.md (20261121-interactive-usability).
Scope: prompt scaffolding never renders on ANY surface — strip at the
task/step RPC + HTTP payload boundary instead of per-render sites.
Dependencies: none (wave 1; C-group).
Estimated context: 65K.

## Goal

07 — Scaffolding strip at the payload boundary.

## Contract

Implements master C-5: step descriptions leave the daemon through
StripPromptScaffolding; results through StripClaimsEvidenceOrProse, at:
task/step RPC responses (internal/rpc/task.go and the task detail
handlers), HTTP API task payloads (internal/comm/http), and
task.progress current_step payloads. The already-clean chat card paths
stay byte-identical.

## Tasks

1. GATE (RED first): e2e suite `e2e/suites/payload-strip/`: a step whose
   stored description carries the "## Session context" block and
   "## Your Task" header; assert the RPC task detail response AND the
   HTTP task response contain NEITHER marker, and the rendered step
   text is the clean description. RED recorded.
2. Locate the shared serialization point: internal/rpc task detail +
   steps handlers (cmd surface `meept session get`/tasks detail path)
   and internal/comm/http task endpoints. Apply StripPromptScaffolding
   to Description and StripClaimsEvidenceOrProse to Result at ONE
   choke point per surface (a helper both call).
3. task.progress current_step: the progress event composer (tactical /
   progress_synthesizer) — strip before publish so live progress lines
   are clean too.
4. Do NOT change stored data (strip is a render-boundary concern; the
   store keeps raw descriptions for the planner).
5. Tests GREEN: e2e; `go test -p 2 ./internal/rpc/... ./internal/comm/...`.
6. Report the exact choke points with file:line.

## Interface Contract Section

None exported. Guarantee: after this leaf, grep "## Session context"
across RPC/HTTP response builders returns zero render paths.

## Self-Verification Checklist

- [ ] Gate RED -> GREEN.
- [ ] Chat card byte-identical (existing tests green).
- [ ] Store rows untouched (verify: strip helper not called in the
      write path).

## Review Checklist

- [ ] One choke point per surface, not N scattered calls.
- [ ] TUI modal (models/tasks.go:1260) and GUI detail (tasks_detail
      .dart:92) now clean WITHOUT client changes.

## Do NOT commit

The orchestrator stages and commits after review.
