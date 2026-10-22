# meept bughunt wave 2026-10-16 -- consolidated report

Range: working tree at HEAD 3a482477 + uncommitted changes. 8 read-only auditors
(local model minicpm-v-4.7), scopes covering ~all of internal/ + cmd/.
Provenance: [PV] parent re-read the cited line. [CH] child-sourced only.
[PD] parent disproved or narrowed the child claim.

Baseline gate (parent): `make test` -- 5 failures, all in internal/tools/builtin
(TestWebFetchTool_TaintLabel, TestWebSearch_* x4). Network-dependent web tests;
environmental, not wave-related.

## ACCEPTED FINDINGS

1. HIGH [PV] Duplicate `case msg, ok := <-synthCh:` in one select --
   internal/comm/http/api_handlers.go:369 AND :385 (uncommitted working-tree
   change). Both cases read the same channel; Go picks randomly among ready
   cases. The :369 case applies `sseSessionMatches(payload, filter)`; the :385
   case does NOT. Result: synthesized progress from OTHER sessions leaks into
   this SSE stream roughly half the time. Fix: delete the :385-:397 case (the
   :369 one is the correct shape). This is in the user's own WIP diff.

2. MEDIUM [PV] os.Getwd() fallback in daemon code --
   internal/runtime/bwrap.go:150-158. When cmd.Dir is empty the bwrap sandbox
   workdir falls back to the daemon CWD. Violates the root AGENTS.md invariant
   ("never use os.Getwd() as a fallback in internal/ daemon code; projects bind
   per session"). Blast radius is small (sandbox bind target), but the fix
   shape is a per-session/explicit dir or an error, not CWD.

3. LOW [PV] MCP resource URI guard is display-only and incomplete --
   internal/tools/mcp/client.go:302. The filter blocks file:// URIs containing
   /etc/ or /../ or starting file:///. No code path fetches file:// from MCP
   resources (grep: only llm content-parts resolves file:// from its own
   UploadStore), so this is a prompt-injection surface (a malicious server
   echoes arbitrary path strings to the model), not path traversal. Child
   rated HIGH; parent demoted to LOW. Clean fix: block ALL file:// schemes.

4. LOW [PV] OAuth PKCE `state` generated but never verified --
   cmd/meept/oauth.go:173, internal/auth/anthropic.go:45,122. state is put in
   the authorize URL, never stored, and ExchangeAnthropicCode does not accept
   it. Attack surface is minimal (manual paste-back flow, no redirect handler),
   so CSRF risk is low, but the state parameter currently does nothing.

5. LOW [PV] Dead-letter move is outside the state-transition transaction --
   internal/queue/store.go:905-910. A crash between tx.Commit and moveToDead
   leaves a job failed-but-not-dead-lettered. The code comment documents this
   as deliberate ("a dead-letter move failure does not roll back the state
   transition"). Probe before fixing; a reconciler sweep would close it.

6. LOW [CH] MCP stdio transport: subprocess crash without Close() leaves
   `running` true and relay goroutines parked (internal/tools/mcp/transport/
   stdio.go:315-396). Not reproduced; plausible from code shape.

7. LOW [PV] skills requires-tools gating is skipped when the executor's
   toolAvailability checker is nil (internal/skills/executor.go:241-245).
   Pinned by an existing test as intended; noted as a design gap only.

## DISPROVED / REJECTED CHILD CLAIMS (do NOT fix)

- session_tracker.go mutex-across-I/O (agent-plan #2): FALSE. The lock is
  released at :252 before the persistSession I/O loop; the I/O runs outside
  the lock. [PV]
- handler.go fetchStepSummaries nil-deref (agent-plan #4): FALSE.
  internal/agent/handler.go:2142 guards `if h.stepStore == nil { return nil }`.
  [PV]
- backoff.go:449 UnixNano "CRITICAL predictable ID": MISCLASSIFIED. It seeds a
  jitter RNG with an explicit `//nolint:gosec // jitter only, non-crypto`
  comment. The predid rule bans ID generation, not jitter seeds. tactical.go:60
  is a comment describing the OLD pattern, already fixed. [PV]
- cluster_queue.go:396,541,543 / executor_bridge.go:192 / gossip.go:349,852
  UnixNano "predictable ID": MISCLASSIFIED. All are timestamp values or
  threshold arithmetic, not IDs; the event ID at cluster_queue.go:536 correctly
  uses id.Generate("claim-"). [PV]
- auditlog Append BeginTx(ctx,nil) (memory-session top finding): documented
  design. store.go:41 and :125 comments explain SetMaxOpenConns(1) serializes
  all callers. [PV]
- bus PublishT/PublishBlockingT panic (topic.go:40,60): by-design programmer
  error, documented in the code. No non-marshalable payload found in scope.
- proxy-without-responder (infra-misc #2): the child cited AGENTS.md itself and
  found no actual instance. Not a finding.
- slot_gate.go:115 panic: child itself verified all call sites check the
  acquire error first; no reachable unbalanced release. [PV]
- SSRF guard: DNS-rebinding gap is a documented limitation; IPv6/decimal bypass
  not present; DefaultGuard() panic unreachable. auth store: ConstantTimeCompare
  correct, expiry checked at use, 0600 perms correct. All clean.
- selfimprove "6 TODOs at detector.go": FALSE ALARM confirmed by child (the
  count was grep matches of the pattern string, not TODO markers).

## Coverage gaps (residuals, parent not closed)

- internal/plan, internal/context, internal/task: not audited (agent child
  spent budget on internal/agent).
- internal/scheduler, internal/worker: not deeply inspected.
- internal/transport, internal/rpc/proxy.go: not deeply audited.
- memory/session/effects/auditlog/compress: report body lost (child overwrote
  its 5.9 KB report with a 253-byte schema stub when stopped mid-finalization).
  Surviving summary: 5 findings, top = auditlog BeginTx (rejected above);
  effects idempotency, session WAL+busy_timeout, sqlite serialization all
  verified CLEAN by the child.
- internal/calendar, stt, tts, configui: not audited.
- TUI handler nil-checks: grep-level only.
- Token cache L1/L2 vs context-firewall accounting: not verified.

## Timing + throughput (measured, state.db + delegation manifests)

Wave wall clock: 15:38:03 dispatch -> 17:24:24 last child = 106 minutes
(8 children, 2 batches of 4, model minicpm-v-4.7 via socrates-litellm).

Per-child (wall = last_activity - started):

| child | wall s | in tok | out tok | rsn tok | calls | out tok/s | out+rsn tok/s |
|---|---|---|---|---|---|---|---|
| 8c73e8 | 6380 | 660,020 | 9,915 | 2,699 | 39 | 1.55 | 1.98 |
| 4aa40e | 5693 | 435,636 | 8,397 | 2,397 | 29 | 1.48 | 1.90 |
| f01525 | 6352 | 620,228 | 6,615 | 2,104 | 28 | 1.04 | 1.37 |
| e957dc | 6188 | 863,219 | 7,263 | 2,299 | 20 | 1.17 | 1.55 |
| cf7fd4 | 6073 | 322,996 | 8,375 | 1,367 | 38 | 1.38 | 1.60 |
| 14c383 | 6305 | 247,259 | 9,638 | 1,960 | 32 | 1.53 | 1.84 |
| 0d2759 | 3843 | 149,447 | 6,206 | 1,125 | 19 | 1.62 | 1.91 |
| 04af0e | 5462 | 395,890 | 7,211 | 1,337 | 22 | 1.32 | 1.57 |

Aggregate: sum-of-lifetimes 46,296 s; in 3,694,695 tok; out 63,620 tok;
reasoning 15,288 tok.
- output tok/s per child: 1.04-1.62, aggregate 1.37
- output+reasoning tok/s: aggregate 1.70
- prefill (input) tok/s per child: 39-140; aggregate processed 81.5 tok/s
- fleet decode across the shared server: ~11 tok/s (8 x 1.37)

vs 2026-10-15 wave (0.45 out tok/s per child): 3x faster this wave -- smaller
briefs, capped scope, reports written early kept per-call context down
(149K-863K in per child vs 237K-900K before).
