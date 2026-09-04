# NEXT-STEPS — bughunt waves 2026-09-03/04: decisions executed

Full reports: `.hermes/audits/meept-week-2026-09-03.md` and
`.hermes/audits/meept-week-2026-09-04.md`.
All prior-wave CRITICALs/HIGHs are fixed. Product decisions from the 09-04
review are now implemented and committed (see below). This file tracks only
what remains open.

## Decision outcomes (implemented this wave)

- **retry_count** → `*int` config field: omitted=3, 0=zero retries, -1=unlimited.
  Commits 366f0f94. BONUS FIX: the field had no toml tag — TOML configs could
  never bind it (go-toml matches by tag); JSON5-only in practice until now.
- **Attempt-tagged streaming deltas** (option B) + **codex 429 typing** → 5dd63c70.
- **Pacing**: slot reservation (ticket-style), default ON, and endpoint-timeout
  turns PARK with the resolver's lazy-clear probe as the single reconnecting
  agent (no spurious reconnect storms) → fda25177.
- **json5 durations**: tokenizer pass replaces line-based hack; compact JSON5,
  arrays, quoted-then-bare same-line all handled → 161f9ba8.
- **Quota surfaces**: daemon-local timestamps by default (opt-in client-local),
  `reason` parsed/displayed on TUI+Flutter, copyWith clear semantics fixed
  (stale badges gone) → f0cd5331.
- **Quick fixes**: rollback spin cap (3/turn), guard-state reset per turn,
  resume Waited accuracy, memory consolidation nil-order → 421959a9.
- **Capability matcher**: stays gated OFF (`agents.capability_match_enabled`,
  7943f945); evaluation tracked in GitHub issue #31 (remove vs encoder-router).

## Still open

1. **Unpushed**: ~55 local commits ahead of upstream. Pending the -race gate.
2. **-race full suite**: targeted -race runs pass (guards/parkers/streaming);
   whole-suite race run never executed on this tree. Run before push.
3. **M3/M4** (llm rotation ignores NonRetryable; ChatWithProgress missing
   auth-arm): untouched — internal/llm had heavy sibling traffic; small PR
   when the llm zone is quiet.
4. **A-M deltas contract**: consumer migration — the loop's TTSR streaming
   consumer (loop.go ~4668) still uses the plain callback (attempt-0-only
   under rotation; no duplication, but no rotation-resume either). Migrate to
   ChatWithDeltaCallbackWithAttempt when convenient.
5. **Site/docs tweaks unreviewed**: meept.dev/index.html,
   docs/feature-comparison-matrix.md, docs/features.md — sibling edits in the
   working tree, not audited by any wave.
6. **Minor**: SetPerOperationBackoffOverride has no exported clear (test
   shims exist; production API asymmetry). gomarkdoc regen for agent.md when
   available. `cmd/probe-skillkey-tmp/` + `scripts/` strays: owner deletes.

## Product decisions — none open. All resolved and implemented.
