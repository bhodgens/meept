# meept bughunt wave 2026-10-19 — 2-week commit range (3a482477..HEAD, 49 commits)

Method: 6 read-only auditors (disjoint scopes: llm / hermes+acp / agent+session+daemon /
fix-commit regression review + infra / sibling WIP + decision-model / memory+services+config),
parent pre-reads (acp/decision.go, hermes/sse.go), parent source-verification of every HIGH,
then 6 isolated fixers with disjoint file ownership. Prior waves: 2026-10-15 and 2026-10-16
reports in this directory; the 10-16 fix commits were regression-reviewed in this wave.

Baseline gate (parent): go build ./... exit=0. go test -p 2 -count=1 ./... = 111 ok,
2 FAIL packages — internal/tools/builtin + internal/comm/http, ALL failures
`dial tcp 127.0.0.1:NNNNN: can't assign requested address` (macOS ephemeral-port
exhaustion under full-sweep load); comm/http SSE tests re-ran isolated GREEN.
Environmental, not wave regressions (known class, meept-development skill).

## ACCEPTED FINDINGS (parent-verified [PV] unless noted)

### HIGH
- H1 [PV] internal/tools/mcp/transport/stdio.go:239-252,302-310,327-331 — commit 5de0fec6
  (Close nils t.stdin under t.mu) introduced a nil-deref panic + data race: Send/
  SendNotification read t.stdin holding only reqMu after a TOCTOU running.Load().
  Regression introduced BY a 10-16-wave fix commit. → fixer C
- H2 [PV] internal/hermes/client.go:312 — do()/doHeaders() accept ANY status when
  expectStatus omitted: StopRun 500 → nil error; SetSessionModel 409 lock-unavailable →
  nil error + zero result; Capabilities 401 → caches EMPTY caps for 60s (every gated
  verb then fails with a misleading capability message). → fixer B
- H3 [PV] internal/tools/builtin/decision_gate.go:132 — gate reads only Confidence,
  never Answer; DecisionClient always returns Confidence 1.0 (decision_client.go:186)
  → in production the gate ALLOWS even when the model answers "no". ACP lane has the
  correct polarity (acp/decision.go:108-118). → fixer C
- H4 [PV] decision-compaction lane dead in every production loop class: template loop
  never gets WithResolver (zero hits in internal/daemon); firewall built before
  SetCompactionConfig; registry AgentConfig has no Compaction fields. → fixer E
- H5 [PV] shared ReflectionCollector scope provider: unsynchronized SetScopeProvider
  from every session clone's turn end (loop.go:3180, 8720-8724) read later in a
  background ReflectTurn goroutine (reflection_collector.go:46,105) → data race +
  cross-session scope misattribution (wrong affinity header/budget). → fixer E
- H6 [PV] internal/tools/mcp/client_test.go — entire file is
  `assert.True(t, true, "verified via code review")`; passes with the c9cf720a
  blocking logic deleted. Production fix correct but unpinned. → fixer C
- H7 [WIP-adjacent, committed] loop.go hookRegistry wired only onto the daemon
  template + clones; registry-built loops and verifier child loops run tools with
  nil hookRegistry → security HardError deny + decision gate inert on every
  dispatcher-routed turn. → fixer E

### MEDIUM
- M1 [PV] internal/llm/resolver.go:875-887 — LRU order-index not renumbered on splice
  → duplicate IDs, live preferences evicted early. → fixer A
- M2 [PV] internal/llm/provider_manager.go:810-822 — ChatWithProgress success never
  calls reportSessionEndpoint (asymmetric with failure path; idle reaper blind). → fixer A
- M3 [PV] internal/llm/resolver.go:686 — sticky hook mutates shared AliasHealth.
  CurrentIndex, contradicting its frozen READ-side contract comment. → fixer A
- M4 [PV] internal/llm/client.go:404-432 — sessionAffinity written/read with no lock
  (Anthropic twin uses configMu correctly). → fixer A
- M5 [PV] delete-path teardown identity resolver queries the store AFTER the row is
  deleted (components.go:2380-2386 vs store_sqlite.go:764) → conv-id-keyed snapshots
  never erased on delete. → fixer E
- M6 [PV] leaf-01 half-wired: ReflectionEngine.SetScopeProvider has 0 callers;
  RLMAnalyzer/SynthesizeTracesTool scopeProvider has no seam; classifier/intent
  ScopeProvider never set. → fixer E (+ sibling-owned dispatcher wiring reported as patch)
- M7 [PV] decision-gate confirm deadlock: confirmed=true re-invoke re-asks the client
  → ask/deny loop until the no-progress ladder vetoes. → fixer C
- M8 [PV] hermes StreamChat drops ChatOptions.SessionKey (sync path sends the header,
  stream path never reads it; tool advertises session_key on chat). → fixer B
- M9 [PV] hermes Capabilities() holds capsMu across the HTTP request (no-I/O-under-
  mutex rule). → fixer B
- M10 [PV] acp Transport.Serve dispatches permission handlers inline → human relay
  (120s default) blocks ALL inbound reads; stdout pipe back-pressures the agent. → fixer F
- M11 [PV] security sanitizer C0-strip runs LAST (sanitizer.go:393-410): markers split
  by a control char evade escaping then get REASSEMBLED ("Sys\x00tem:" → live "System:"
  role marker; "<<<END_TOOL_\x01OUTPUT>>>" → pristine boundary marker). → fixer C
- M12 [PV] .githooks/pre-push-e2e: for-each-ref "@{u}" always empty → manual runs
  silently degrade to smoke suite; basename breaks nested tracking refs; pre-push
  shebang is homebrew-only (Linux exec error blocks every push). → fixer D
- M13 [PV] GUI /dnd + /implement-improvements are autocomplete-only dead ends
  (slash_commands.dart:138,147; no chat_input.dart branch → sent to the LLM as text). → fixer F
- M14 [PV] e2e/manifest.json hermes-client suite has zero scenarios[] entries;
  completeness test only checks scenario→suite direction. → fixer D

### LOW (assigned)
- L1 forgetSessionEndpoint wipes preference on ANY provider failure (should compare
  endpoint key). → fixer A
- L2 ShutdownSweep worst case 2x documented budget. → fixer A (budget split or doc fix)
- L3 SetIdentityResolver unsynchronized vs dispatchAsync readers. → fixer A (steer)
- L4 LlamaServerTeardown.Handles true before nil-receiver guard (typed-nil). → fixer A
- L5 TeardownBaseURL exact-match now requires a provider mapping (b1af5ce6 edge). → fixer A (steer)
- L6 stale comments: teardown.go:122-124 (port claim), resolver.go:866-868 (-1 tombstones),
  provider_manager.go:1034-1039 (coverage claim). → fixer A
- L7 loop.go hard-error deny: Error field empty; comment claims repeat-breaker feed
  that never happens. → fixer E (minimal honest fix)
- L8 IdleReaper started flag unsynchronized; ActivityTracker maps unbounded;
  SetLifecycleHook unlocked write vs its own doc. → fixer E
- L9 Consolidator startOnce+stopOnce = permanently single-use. → fixer E
- L10 ac63861e range-guard has no mutation-style pin. → fixer E (providers_config_test.go only)
- L11 acp s.sessionID written without s.mu after Serve goroutine launched. → fixer F (steer)
- L12 acp allow replies hardcode optionId "allow-once" in all three lanes. → fixer F
- L13 TUI /task registered + documented but no handler case; /mcp doc-only ghost. → fixer F
- L14 dual_store.go LIKE fallback uses FTS-sanitized string, no ESCAPE (pre-existing). → fixer C
- L15 gofmt: 4 in-range files unformatted, no Go fmt gate exists. → fixer D
- L16 memory/manager.go:1257 stale stop-once comment. → fixer E (steer)

### Deferred / report-only (classification stated)
- D1 [product decision] auditlog empty-chain OK=false surfaces as a "failing chain" on
  fresh installs (agents audit CLI); no caller distinguishes empty vs tampered. The
  8fdd4708 flip is intentional tamper-evidence; a Reason-based CLI message is a UX call.
- D2 [pre-existing, out of range] GUI registers ~14 more autocomplete-only commands
  (/retry /undo /task /tasks /cancel /amend /interrupt /compact /vim /plan /review
  /edit /skill /help) with no local handler. Parity backlog, not a wave regression.
- D3 [pre-existing] SSE framing cosmetic deviations (event-without-data dispatch;
  bare-\r line endings). Not reachable against the known upstream.
- D4 [sibling WIP, owner completes] handler.go:3215 false comment (fallback still
  timestamp-only, not id.Generate); escalation.go Seq field has no reader;
  session_tracker PersistIdleSessions unlocked live-pointer reads; shallow Intent
  clone. Files belong to the in-flight sibling session — reported, not touched.
- D5 [info] No durable decision records exist anywhere (slog only) — Contract D
  auditability is an unimplemented leaf, not a bug. Feature-design deferral.
- D6 [info] components.go:984-987 — decisionClient only built when
  LoadProvidersConfigDefault succeeds; error path runs decision mode with no decider
  (fail-closed, safe). Worth a startup Warn — folded into fixer E's report if cheap.
- D7 [env] tools/analyzers/mutexio cannot run on this machine (pinned x/tools vs
  go 1.27 export data v5>4) — would have caught M9. GOTOOLCHAIN pin covers make
  targets but the standalone analyzer run hit ambient go.
- D8 [info] dynamically-registered OAuth provider endpoints are never sticky-
  remembered (no alias carries them). Likely intended; flagged for confirmation.
- D9 [info] isQuotaBlocked uses time.Now() while isEndpointBlocked uses r.clock()
  (pre-existing inconsistency, injected-clock tests can disagree). Out of range.

## FIX-COMMIT REGRESSION VERDICTS (10-16 wave)
Clean: ffc90cdf, 0528fa0b, d3cadc7e, 185b4d77, 5fd342d8, 8fdd4708 (see D1),
90ff648c, b1af5ce6 (effective; see L5).
NOT clean: 5de0fec6 (H1 — nil-stdin race), c9cf720a (H6 — vacuous test; production
code itself correct).

## FIXERS DISPATCHED (deleg_885b4fbd + deleg_71e68445, 6 total)
- A: internal/llm (M1-M4, L1-L6 + steered L3/L5)
- B: internal/hermes (H2, M8, M9)
- C: mcp stdio + mcp client_test + decision_gate + sanitizer + dual_store (H1, H3, H6, M7, M11, L14)
- D: githooks + manifest + gofmt (M12, M14, L15)
- E: agent loop/registry/reflection + session lifecycle + daemon wiring + consolidation (H4, H5, H7, M5, M6, L7-L10, L16)
- F: acp + tui/sharedclient + flutter GUI (M10, M13, L11-L13)

Ownership is disjoint; sibling WIP files (dispatcher.go, escalation.go, handler.go,
session_tracker.go, team_orchestrator.go, plan.go, go_lint.go, chat.go) are excluded
from every fixer. Fixes land in the working tree; parent gates + commits per concern
after adversarial review of each fixer diff.

## DISCLOSURE LEDGER
- Baseline full-suite FAILs are port-exhaustion environmental (isolated re-run green
  for comm/http; builtin flakes are the known rotating class) — baseline is NOT
  fully green; post-fix gate matrix will re-run and the report will state counts.
- Auditor 3's report cites HEAD 32c9a1f4 / relay leaf 93547738 commits NOT visible in
  the parent's git log at dispatch (b0055c37 tip): a sibling session committed mid-wave.
  All findings re-verified against the current tree by the parent before dispatch.
- The 10-16-wave report's own finding #1 (duplicate synthCh select case) was in the
  user's WIP at that time; not re-audited here (file internal/comm/http/api_handlers.go
  is not in this wave's committed range).
- No fix committed yet at report-write time; commit table appended after the fix wave.

## FIX-WAVE DISPOSITION (appended 2026-10-20 ~01:40 MDT)

13 commits, concern-split, explicit pathspecs; sibling WIP (dispatcher.go,
escalation.go, handler.go, session_tracker.go, team_orchestrator.go, plan.go,
go_lint.go, cmd/meept/chat.go, decision-model doc) left untouched in the tree.

| Finding(s) | Commit |
|---|---|
| M1-M4, L1-L6 (llm sticky/teardown/affinity) | be01dd6b |
| H2, M8, M9 (hermes status/session-key/mutex) | 2713d0d4 |
| H1, H6 (mcp stdio race, vacuous test) | e98b38f1 |
| H3, M7 (decision gate Answer bar, confirm bypass) | 8b1c2d6a |
| M11 (sanitizer C0 ordering) | a2955a48 |
| L9, L14, L16 (consolidator restart, dual_store LIKE) | 5715ce34 |
| M5, L8 + daemon wiring legs of H4/H7 | e2f98297 |
| H4, H5, H7, M6, L7 (agent scope/compaction/hooks) | 1b21708b |
| L10 (port-guard mutation pin) | 5acf4d3b |
| race in sibling's acp_wiring_test (found by wave gate) | 05de2989 |
| L13 (TUI /task alias, /mcp doc ghost) | dd13ef38 |
| M13 (GUI /dnd + /implement-improvements) | 23abf5ed |
| M12 (pre-push-e2e @{u}, nested refs, shebang) | 23d4b5ff |
| L15 (gofmt sweep) | f80bb3f0 |
| this report | f6956725 |

Sibling absorptions (content verified in-commit, not re-committed):
- b4323721 (sibling) absorbed fixer F's acp head-of-line fix (M10),
  resolveAllowOptionID (L12), setSessionID race (L11) + pins.
- 7d079c52 (sibling) absorbed fixer D's hermes-client manifest scenarios +
  reverse-completeness test (M14).
- 407f58e4 (sibling) absorbed the sdk_client getDndState/setDnd methods the
  GUI fix (23abf5ed) calls.
- da511d98 (sibling) absorbed the docs/reference/cli.md edits.

M6 sibling-owned wiring (NOT landed; proposed patches in fixer E report):
dispatcher.go:635/649 classifier+intent scope threading and orchestrator.go:929
reflection scope threading — both files are the sibling session's WIP.

Gates at wave close: go build ./... OK; go vet OK; full suite 112 ok + 1
environmental FAIL (builtin port exhaustion, green isolated); -race green on
llm/hermes/mcp-transport/security/agent/session/memory/acp; step-pipeline e2e
flake green isolated (19.9s); make e2e-affected: all suites ok except the
documented step-pipeline flake. All fix commits used --no-verify because the
pre-commit whole-tree build cannot pass while the sibling WIP is mid-edit
(internal/agent referenced the removed SetScopeProvider during the window);
every gate the hook would run passed directly first (recorded per skill rule).

Disclosure ledger additions:
- TestACPBusNotifierPublishes -race failure was in a SIBLING-committed test
  (acp_wiring_test.go, not wave-owned); fixed anyway (no-delays rule), 05de2989.
- 4 files remain gofmt-dirty at HEAD outside wave ownership
  (decision_compactor_test.go, sanitizer_c0_test.go, oauth_pkce_state_test.go,
  memory_turn_capture_test.go) — pre-commit fmt hook may flag them for their owner.
- internal/daemon/memory_capture_test.go gofmt-dirty at HEAD (not touched).
- /tmp/meept-red-f1 worktree (fixer E red-run evidence) left in place; remove
  with `git worktree remove --force /tmp/meept-red-f1`.
- mutexio/analyzers broken against ambient go 1.27 export data (pre-existing,
  make targets unaffected due to GOTOOLCHAIN pin).
