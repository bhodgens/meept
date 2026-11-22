# Leaf 06 — Graceful errors: empty-plan fallback + user-language funnel

## DISPATCH INSTRUCTION

Implement this leaf fully. Do NOT commit. Do NOT run git add. Write code,
run tests, report results only. Working dir: the plan worktree. Grep
first (verify-or-apply).

## Meta
Parent: master.md (20261121-interactive-usability).
Scope: no request fails outright — planner empty-plan degrades, machine
errors map to user language, unknown-schedule quota keeps the queue note.
Dependencies: none (wave 1).
Estimated context: 80K.

## Goal

06 — Graceful errors: empty-plan fallback + user-language funnel.

## Contract

Implements master C-4: export `UserFacingError(err error) string` from
internal/agent; the terminal funnel composes failed-turn replies through
it.

## Tasks

1. GATE (RED first): e2e suite `e2e/suites/graceful-errors/`:
   (a) trivial non-artifact recall turn ("what was the codeword" shape)
   completes WITHOUT "planner produced no plan" hard error — reply is a
   real answer or an honest degraded note, exit 0; (b) a turn failing
   with ContextTooSmall renders the user-language sentence, not the raw
   sentinel; (c) quota error with unknown schedule reply contains the
   queued/re-ask guidance sentence.
2. Empty-plan fallback: strategic.go path that errors
   "planner produced no plan; not a single-artifact task" — instead,
   fall back to a single direct step (reuse the degraded-planning lane
   from strategic.go:997-1000) and let the turn execute. Error surface
   ONLY if the fallback step itself fails.
3. `internal/agent/errors_user.go` (new): UserFacingError mapping
   table — IsContextTooSmall -> "conversation too long for this model;
   start a new session or ask me to summarize"; TierFloorError ->
   "model pool degraded too far while retrying; try again shortly";
   ErrAllModelsQuotaBlocked -> existing quota sentence;
   ErrEmptyResponse -> "the model returned nothing usable; try
   resending"; errors.Is ErrGateBlocked -> keep the existing typed
   envelope text. Wire into the terminal funnel (handler.go:1358-1361,
   1585-1589) BEFORE response.Error assignment.
4. ContextTooSmall retry: at the loop terminal branch
   (loop.go:7714-7724), attempt ONE compaction/trim-and-retry before
   surfacing (reuse the existing compaction seam; if compaction is
   disabled/unavailable, surface the mapped sentence directly).
5. Quota unknown-schedule: handler.go:1448-1547 park branch — when
   unblockAt is zero, synthesize now + DefaultQuotaResumePollInterval*3
   as a conservative unblockAt (park as usual) OR append the re-ask
   guidance sentence and keep the FAILED terminal honest. Prefer the
   park path; keep the give-up cap semantics.
6. Tests GREEN: e2e; `go test -p 2 ./internal/agent/... ./internal/llm/...`
   (UserFacingError unit pins, error-mapping table test).
7. Report: exact wording table for review.

## Interface Contract Section

Exports: agent.UserFacingError(err error) string. Leaves 03/05 may use
it for card texts.

## Self-Verification Checklist

- [ ] Gate RED -> GREEN.
- [ ] Live-probe reproduction: the codeword-recall shape from the report
      (probeA) passes — assert in the gate.
- [ ] Raw sentinel strings ("llm: request does not fit") no longer
      reachable as reply text (grep the funnel).
- [ ] Quota give-up cap (5 attempts/24h) unchanged.

## Review Checklist

- [ ] Mapping table complete for the verified failure families.
- [ ] Compaction retry bounded (exactly one attempt).
- [ ] Wordings lowercase, actionable, no machine ids.

## Do NOT commit

The orchestrator stages and commits after review.
