# NEXT-STEPS — bughunt wave 2026-09-03 (week of 2026-08-27 + agent loop)

Full report: `.hermes/audits/meept-week-2026-09-03.md`
Baseline: build green, `go test -p 2 -count=1` green on all 10 changed package groups.
Mode: REPORT ONLY — no fixes applied; everything below awaits your go-ahead.

## Headline

Chat throttle-parking (the week's tree-03 centerpiece) is dead end-to-end in
production: the parker is never `Start()`ed (components.go:2436) and per-session
loops never inherit it via ConfigSnapshot (loop.go:6228). Three auditors found it
independently. Classic green-tests-unwired: tests call Start() themselves.

## Do now (when authorized) — suggested order

1. **C1** worker.go:313-326 — requeue branch returns holding `w.mu`; worker
   deadlocks after one throttle. 2-line fix.
2. **C2** components.go:2436 + loop.go ConfigSnapshot — add `throttleParker.Start(ctx)`
   + `WithTurnParker` in the clone options. Restores the D9 no-hang invariant.
3. **H1-H3** loop.go park contract cluster — missing `(nil, nil)` check at 3498
   (double-exec), park flows through success pipeline (false reflection/learning
   data, blank assistant msg, worker "completed"), resume duplicates the user
   message. One coherent fix in one file.
4. **H4-H6** employee parker — no dedup (96x duplicate executions on a 24h window),
   tier-2 resumes into a no-op (Assess result discarded, decideTier2 never runs),
   resume bypasses the paused-employee gate.
5. **H8-H9** — a13526d8's intent fix is a no-op (spec.ID seeds IntentTypes[0],
   matcher takes [0] → inverse mapping dead); only production NewHTTPHook gets a
   nil allowlist → every configured hook fails before sending. Both one-line-class.
6. **H7/H10/H11** — TEXT-affinity version stall on migrated DBs (latent);
   `resume_at` vs `unblock_at` wire mismatch (surfaces never show retry time);
   conv-id-as-session-id WS misroute for TUI/CLI parked turns.

## Decisions needed from you

- Explicit `retry_count: 0` now means 3 retries (df1cb627). Keep, or document the
  `-1` opt-out at the config surface?
- bus.go d48d89b7 demote also silences `PublishBlocking` no-subscriber — the
  "security-critical, drops unacceptable" path. Restore WARN for blocking mode?
- M13: `min_relevance` floor filters the strongest matches first (NormalizeRank
  inverts with score). 0.3→0.1 moved the cliff; want the direction fixed?
- H9: is the HTTP-hook allowlist supposed to come from config? Schema has no
  field; currently the feature is structurally dead.

## Not touched

- Your in-flight uncommitted diff (loop.go blank-stop + attemptStateRecovery) —
  audited, sound at HEAD call sites; one untested attached-path contract change
  flagged (blank+stop now returns whitespace success on interactive turns).
- `cmd/tmp-verify/` — sibling scratch, harmless.

## Verification debt (disclosed)

-race, mutexio/predid, lint-ci, graphs-check, Flutter tests not run in this wave.
~10 LOW findings (metrics ticker race, RPM slot leak, runtime PID-recycle, etc.)
rest on code-read only.
