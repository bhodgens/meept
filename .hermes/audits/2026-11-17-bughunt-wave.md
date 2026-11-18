# Bughunt 2026-11-17 — two-week work review (2026-11-03..2026-11-17)

Wave anchor: d69a1ad8 (last audited point, 2026-11-15 two-run report).
HEAD at wave start: 11a00045. Scope = 162 commits in the window.
Method: baseline gates -> 6 read-only auditors (+ focused sub-auditors) ->
parent source-verification of every HIGH at file:line -> parent fix wave with
disjoint file ownership -> non-vacuity probes -> per-concern commits with
explicit pathspecs through the full pre-commit gate chain -> final gates.
Fixing authorized by the request ("fix all issues you encounter, validate
them, commit them").

## Baseline

- `go build ./...` clean.
- `go test -p 2 ./... -count=1`: 133 packages ok, 2 FAIL blocks
  (internal/plan TestPlanHandlerStepFailedBusSeam, internal/project
  TestListProjects). Both pass in isolation and 10/10 repeated.
- Main worktree clean; sibling sessions active in separate worktrees and
  intermittently editing shared-tree files (client.go, stream_toolcheck.go,
  AGENTS.md, docs, deadcode baselines). All sibling files excluded by
  explicit pathspec; no stash or checkout was run.

## Verified findings and dispositions

| # | Finding | Sev | Verdict | Fix commit |
|---|---------|-----|---------|------------|
| B1 | `internal/project/store.go:389` formatTime uses RFC3339Nano (variable-width fraction); ListProjects `ORDER BY created_at DESC` (store.go:166) sorts lexically wrong ("20Z" > "20.4Z"). Baseline TestListProjects FAIL | HIGH | CONFIRMED (probe proved ordering inversion) | 187b9237 — fixed-width 9-digit layout + non-vacuous TestFormatTimeLexicalOrder |
| B2 | `internal/plan/handler_stepfailed_test.go:114` polled only failed_steps==3; OnStepFailed increments then flips phase in a second write, so the pin read the intermediate state under load | MED | CONFIRMED | 187b9237 — poll until PhaseFailed too |
| W1 | `internal/llm/resolver.go` PeekAliasForWarm called getOrCreateHealth, inserting an AliasHealth row for a cold alias (violates zero-side-effects contract; existing pin pre-armed the row so it could not see this) | HIGH | CONFIRMED | 74b92dd6 — read-only lookup with zero-value fallback + cold-alias pin (non-vacuous) |
| A1 | issue-140: bot trigger handler called `loop.SetResultTopic(bot.result)` once on a shared GoalLoop; never reset. ListEmployees wraps all bots, so a later goal round on the same entity published bot.result | HIGH | CONFIRMED | 1b0bec06 — set result topic PER ROUND in Manager.triggerViaGoalLoop from trigger source; pin TestResultTopic_IsPerRoundNotSticky (non-vacuous) |
| L1 | `resolveStickyCaller` early return in ResolveForAliasDifficulty bypassed the entire C1 selection chain and the C4 posture hard boundary. A sensitive prompt pinned to a public_only model was served by that model | HIGH (compliance) | CONFIRMED | 276a6bca — apply posture re-pin + full chain on the sticky path; pin TestResolveForAlias_StickyHonorsPosture (non-vacuous) |
| L2 | `noteAdvisoryLocked` keyed dedup on (reason|task|session) for all limits. Handler pre-check (empty scope) + client gate (real scope) emitted two budget_advisory events for one global breach (Contract C7 violation) | MED | CONFIRMED | 115a7913 — normalize scope for global limits; pin (non-vacuous). First landed as 366fa3c1, dropped by a sibling rebase, re-landed |
| K1 | kanbansvc sidecar loads tokens.json once at boot; Grants() read only memory. Daemon mints tokens at runtime via /kanban/pair/exchange, so freshly paired browsers got 401 until sidecar restart (e2e masked it by booting sidecar after exchange) | HIGH | CONFIRMED | c6ba8bfb — (mtime,size) signature reload in Grants(), disk I/O outside lock, fail-closed on bad file; pin + -race clean (non-vacuous) |
| C1 | WithWebSocket subscribed to exact + wildcard patterns matching the same topics (employee.result + employee.*, agent.turn.state + agent.*.*); bus delivers to all, so clients got two WS frames per event | MED | CONFIRMED | b1cd55d8 — skip the redundant exact duplicates; pin (non-vacuous), full comm/http suite green |

## Round 2 (late consolidated auditor reports)

The full 6-auditor batch arrived after the first report was written. The
complete reports exposed five more verified findings (the truncated early
copies had hidden them). All were source-verified at HEAD and fixed:

| # | Finding | Sev | Disposition |
|---|---------|-----|-------------|
| R2-1 | manifest: sovereign-parity implemented + path-mapped with ZERO scenarios (blocking CI: TestEveryMappedSuiteHasScenarios FAIL) and merge dropped the whole kanban suite surface | HIGH | 1d0cbdb8 — restored kanban suite + 7 scenarios + path_map keys; added 23 sovereign-parity scenarios. Manifest test + census 17/17 green |
| R2-2 | shadowSingleDecision dereferenced mc.DataPosture before its nil check; llm.route.shadow with no default model panicked | HIGH | bfb74698 — nil guard at top + pin proving no panic |
| R2-3 | R9 spillway tiebreak used the sum of all quality classes instead of capability-specific QualityFor | MED | cefeba4f — restore QualityFor(Capability); pin with code turn (non-vacuous) |
| R2-4 | GoalLoop.Execute published "running" turn-state before executor/authorization checks; early returns left agents stuck "running" | MED | 5a774c19 — validate first; pin with real bus |
| R2-5 | config template omitted llm.budget.mode and four voice.messaging schema keys | LOW | 9d47c147 — add five keys with opt-in defaults + drift annotations |
| R2-6 | SMS DLR monotonic guard compared RFC3339Nano strings; newer fractional DLR discarded as older | MED | 31e8f758 — parse instants; pin (non-vacuous) |
| R2-7 | GUI hint named only one of two required kanban gates; SetPairing lacked nil receiver guard; XFF trusted from remote peers | LOW/MED | be0b40e0 + 47a4d071 — both gates in hint, nil guard, loopback-only XFF trust |

## Confirmed but deliberately NOT changed (disclosure ledger)

1. `internal/llm/client.go`: Client.ChatWithProgress / ChatWithDeltaCallback
   have no pre-dispatch admission gate; the IsContextTooSmall check in the
   retry loop is unreachable (doRequest never constructs that error). File is
   owned by a live sibling session (tool-validation semantics) — deferred to
   that owner, not fixed here. Severity MED.
2. Voice context-warm dispatcher scope: warmFn receives the CONVERSATION id
   while resolveContextWarmDispatcher looks loops up by SESSION id; costs a
   cache hit, never the turn. Requires a per-call identity mapping design —
   LOW/MED, deferred with reason.
3. Voice clip cache: a 0-byte PCM entry with meta.Bytes=0 passes Store.Has and
   plays silence; needs a product decision on whether zero-length audio is a
   valid clip. LOW, deferred.
4. Voice per-employee BackchannelSoften reload identity divergence and
   fallback-line clip_id coverage — LOW, document/design items.
5. kanbansvc pair/exchange per-IP rate limiter trusts X-Forwarded-For without
   a trusted-proxy check — needs a deployment decision (which proxies are
   trusted). MED, deferred; operator proxy terminates TLS.
6. kanbansvc SMS RFC3339Nano ordering (same class as B1) in sms reply store —
   not in the fixed files; deferred to the sms lane.
7. Deadcode baselines retain two stale rows for functions deleted by sibling
   commit ef7a3369 (postureBlocks, publishTurnState). Gate still passes;
   over-blesses a future same-named function. The sibling staged baseline
   cleanup during this wave — left to that owner. LOW.
8. Commit-message overclaims: 7cb2e86d (sensitivePrompt constant unchanged by
   its diff), 54b4d271 (guard-order narrative imprecise). History notes only.
9. Stub-heavy WS e2e waitFor accepts the first matching frame, so double
   delivery (C1) escaped detection — test-harness improvement deferred.

## Gating

- Each fix commit passed all 20 pre-commit gates (build, vet, consumer tests,
  mutexio, selflock, U1000, gosec, error/ID/pragma audits, fmt, ascii, census,
  deadcode, feature-docs).
- Targeted suites: internal/llm, internal/employee, internal/daemon,
  internal/project, internal/plan, internal/kanbansvc (+ -race),
  internal/comm/http — all green at commit time.
- Final full `go build ./...` + `go test -p 2 ./... -count=1` after all
  fixes: build clean (0 output lines). First final run: 160 ok / 0 fail.
  Second final run (after round-2 fixes): 159 ok / 1 FAIL, the failure
  being `TestOneShotFutureDueNotFiredAtBoot` with a `t.TempDir` cleanup
  error ("directory not empty") — a macOS background-writer flake, not a
  logic failure. The scheduler package passed 5/5 targeted and a full
  package re-run clean. Baseline was 133 ok / 2 failing packages.
  Mid-run snapshots (135 ok, 92 ok) were taken while the suite was still
  writing and are not final counts.

## Git hygiene incidents

- Sibling commit a48759ca swept two of my staged files (budget.go pin) into
  its own commit during my pre-commit hook; the ref lock raced and my commit
  failed. Verified content, re-committed budget.go alone on the new HEAD with
  explicit pathspec (115a7913).
- Sibling rebase dropped commit 366fa3c1 entirely; fix + pin survived in the
  worktree and were re-landed as 115a7913.
- Two flaky package failures under parallel load (internal/daemon,
  internal/llm) reproduced clean on targeted re-run; attributed to sibling
  mid-edit windows, not to the fixes.
