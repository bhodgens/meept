# Bughunt wave: 2026-09-03/09-04 commits + agent loop (double pass)

Session: 2026-09-04 (evening). Request: "bughunt-audit-and-fix this repo,
fixing issues as found. focus on commits made in the past 2 days, and the
agent loop. do it twice."

Wave scope: commits 61f04bd3..HEAD at session start (~60 commits,
2026-09-03 → 2026-09-04) plus the then-uncommitted sibling diff
(session-continuity leaf 01), now committed as 2a433d8e.

A sibling session was ACTIVE on the same tree during the whole wave
(session-continuity leaves 01+02, model_parser debugging). Contested-zone
rules applied: internal/agent handler/loop/model_parser fixes deferred to
the sibling; disjoint-file commits only; `git log` re-checked before every
patch.

## Round 1 — parent fixes landed

| # | Commit | Finding | Class |
|---|--------|---------|-------|
| 1 | 32376cdf | HTTP hook retries reused one http.Request: attempt 2+ after a body-consuming failure (5xx) sent Content-Length=N with a drained body — every body-carrying retry died at the transport. Fix: request rebuilt per attempt. Regression test pins attempt-2 body fidelity. | one-shot reader reuse |
| 2 | c044e858 | Codex STREAMING 429s never read the error body → classifyQuotaDecision(429, nil, nil)=false → quota windows degraded to RateLimitError and got short-retried (quota windows are hours). Fix: error path drains body on both modes (matches anthropic.go:1379); streaming success path still incremental (preserves A-HIGH buffered-streaming fix). Regression pins QuotaResetError on streaming. | error-body + stream-mode interaction |
| 3 | 812fc4c3 | fda25177 committed internal/daemon/epistemic_wiring_test.go referencing agent.SetPerOperationBackoffOverrideForTest/Clear...ForTest, but the defining file (backoff_test_hooks.go) was never tracked → daemon TEST package unbuildable on any tree without the untracked file. Baseline sweep caught it. Fix: file restored + committed. | committed code referencing untracked file |
| 4 | 0cd5ced5 | mergeProviderConfig cloned BASE models but maps.Copy'd OVERLAY ModelDef values — overlay ExtraHeaders maps aliased the overlay source config, defeating 515dba45's own deep-copy contract on the merge path. Fix: overlay models go through the same maps.Clone treatment. Regression mutates merged map, asserts source untouched. | incomplete deep-copy sibling path |
| 5 | 47bf9725 | errors.AsType modernize in new test. | lint |
| 6 | 256b6fd7 | ineffassign (dead lastErr store in hook retry loop) + atomic.Int32 in new test. | lint |

## Parent-verified clean (no action)

- AdaptivePacer ticket reservation (fda25177): timeline math, lock scope
  (rate-hold DB read outside pacer.mu), zero-wait fast path — sound.
- retry_count three-way semantics (366f0f94): *int pointer contract,
  toml tag present, 0/-1/n boundaries correct at
  http_hooks.go:312-313; wiring applies absent→3 upstream.
- ftstore INTEGER migration (d5714492): all three legs present —
  INTEGER-typed migration columns (ftstore.go:200), read hardening
  `is_current = 1 OR is_current = ''` (manager.go:1527),
  MAX(CAST(version AS INTEGER)) (manager.go:1442).
- Pacer slot-release discipline; attempt-tagged delta rotation
  (provider_manager.go:594-785): index stamping closes over live
  counter, skipped candidates don't consume an index, classification
  order quota→ratelimit→client→default mirrors Chat().
- json5 duration tokenizer (161f9ba8): strings skipped wholesale,
  "1.5"/hex rejected, stringDurationKeys exemption applied in both
  passes, escapes never converted.
- Domain-agreement gate (c3b334cd): documented tradeoff, pass-through
  for token-less queries, lenient substring match — favors
  non-suppression.
- TUI quota status (f0cd5331): M9 daemon-local convention implemented
  as documented; dual-key wire (unblock_at + resume_at) produced AND
  consumed on both ends (parked_turn.go:520-521, app.go:1663-1672).
- Loop guards (421959a9): guards.go mutex discipline consistent;
  consolidation nil-manager guard correct (sole production constructor
  always sets manager+backend).
- parkEndpointBlockedTurn (nil,nil)-sentinel: both call sites check
  both returns; nil servedModel absorbed by EndpointBlockUntil nil
  guard; alias-scan sets identity+blockUntil together.
- Reply guard (26f276fd): agent-roster always sanitizes, prose ratio
  escape hatch preserved for status/tools.
- Race gate (early): -race -p 2 clean on llm, agent, daemon, memory,
  bus, worker.

## Deferred (contested zone — sibling session holds the files)

- D1 (MED): recordExchangeInSessionConv appends req.Message RAW to the
  session conversation; the direct path appends
  sanitizedMessage+securityOrch.WrapUserInput wrap. Same conversation
  gets unwrapped task text and wrapped direct text. Behavior question,
  sibling's leaf tree owns it.
- D2 (MED): route_to_agent early-returns "message queued (steer/
  follow-up)" — the mirror records the queue ack as the assistant reply
  and the RAW user message as the user turn, while the actual turn runs
  later with Intent.Summary. On steer/follow-up turns the session
  conversation diverges from what the model actually processed.
  Sibling's leaf 01; flagged for their NEXT-STEPS.
- D3 (LOW): LastUserMessage guard dedupes on exact-match last user
  entry; RunOnce stores the SANITIZED+WRAPPED text, so the raw==stored
  equality only coincidentally holds today. Fragile coupling, not a
  live bug.

## Observations (unacted, ledger)

- json5 array-position durations: bare durations in arrays get quoted
  by quoteBareDurations but convertQuotedDurations only converts
  post-colon values. No []time.Duration field exists today — latent
  only. json5_loader.go:219-226.
- Sidecar robustness (prompt_router_sidecar.py): malformed JSON in
  do_POST kills the connection (no 400); unbounded body read; int()
  on garbage Content-Length raises. Local-only tool, LOW.
- golangci-lint shows findings in internal/llm/metrics/store.go
  (errcheck ×2, sqlclosecheck ×2) and modernize nits in sibling test
  files — pre-existing/sibling-owned, not touched.
- Bus publish-no-subscriber WARN→DEBUG (d48d89b7): documented spam
  tradeoff; wiring-break detection at runtime is now weaker. Owner
  call, not re-litigated.
- cmd/probe-skillkey-tmp/ + internal/agent/tmpdbg_test.go + pacing_
  reservation_test.go: sibling scratch/WIP — build-verified, untouched.
