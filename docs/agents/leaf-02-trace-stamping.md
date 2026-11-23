# Agent Reference: Step-Scoped Trace Stamping (leaf 02-trace-stamping)

Step-scoped trace stamp + judge exclusion + blame wiring from
docs/plans/model-rating-metrics-plan/02-capture/02-trace-stamping.md
(contract C3), introduced 2026-11-23. Referenced from
docs/workflows/model-rating.md; the full plan lives in the plan tree.

## Step-scoped stamp lives at the step-job ctx boundary, never on the loop

The "stp-"+stepID trace is stamped with `agent.WithTraceContext` onto the
STEP JOB's context in `AgentJobProcessor.Process` (internal/daemon/
components.go, `stepJobTraceContext` beside `stepJobTurnContext`). The
existing loop seam (loop.go:7405-7406 `TraceIDFromContext`) then threads it
onto every implementer LLM call — the stamp is request-scoped ctx, never a
loop field (the loop is shared across concurrent turns/jobs). Empty step id
=> ctx unchanged (tracectx.go guard); a step job without an id dispatches
exactly as before.

## The judge lane never inherits the step trace structurally

`ReviewManager.ReviewStep` runs the reviewer turn on ITS OWN ctx (the
tactical caller's, no step stamp). Never wrap the review lane's ctx with the
step trace: C3 excludes the judge's calls by agent_id on the
(trace, agent) join, and that only holds while the judge's rows carry no
step-scoped trace. TestTJ_EXCLUDE_1 pins this.

## reviewed_model_id is resolved from the trace, not guessed

`enrichedReviewPayload` composes `reviewed_model_id` via
`metrics.Store.ResolveReviewedModel(traceID, stepAgentID)` (FINAL SUCCESSFUL
llm_calls row; error rows never blame). The modelRefForAgent value is only a
FALLBACK when the join returns "" (no stamped rows yet — e.g. legacy steps
or metrics store unavailable). metrics store is late-wired via
`ReviewManager.SetMetricsStore` (same pattern as SetSessionStore); nil store
=> fallback only.
