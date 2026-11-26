# 11-steer-arming REPORT (ArmQueueAliases never arms on direct-dispatch turns)

## Defect

F-STEER-R7: chat.steer / chat.followup return -32603 "queue not found" on
direct-dispatch (chat-path) turns -- 5 consecutive acceptance-rig runs
(runs 3-7). Run-7 finding: zero arming lines in daemon-run7.log; the
ArmQueueAliases seam "never arms on this dispatch shape".

## Root cause (verified by reading every call site)

The alias seam (20261121-interactive-usability leaf 01, commit b16b6082)
is a ONE-SHOT arm consumed by RunOnceWithParts at queue-registration
time: `ArmQueueAliases(ids)` -> `l.queueAliasIDs` -> `consumeQueueAliases()`
-> `RegisterActiveQueue(conversationID, turnQueue, l, aliasIDs...)`
registers the SAME QueueEntry under each alias as an exact key.

Exactly ONE production call site exists: the daemon's
AgentJobProcessor.Process (internal/daemon/components.go), guarded by
`if isStepJob && p.sessionStore != nil && p.taskStore != nil` --
aliases = the async task's LinkedSessions. So:

- ASYNC step-job turns: aliases armed. Works.
- DIRECT-MODE chat turns (ChatHandler.sessionLoop -> Manager.
  GetOrCreateWired clone -> loop.RunOnceWithParts, handler.go): NO arm
  site anywhere on the path. The clone registers its fresh per-turn
  queue under exactly ONE key -- the raw conversationID the handler
  passed.
- INTERACTIVE dispatch turns (dispatcher RouteToAgent ->
  RunOnceWithParts): also no arm site. Same single-key registration.

The steer surfaces accept any of the session's identity shapes
(b16b6082: primary session id, conversation id, thread-scoped id ->
base unwrap). When the turn's registration key and the steer's key are
DIFFERENT shapes of the same session -- e.g. the GUI steers by the
primary session-... id while the turn registered under the conversation
id, or the probe's thread-scoped id whose base differs from the
registered base -- resolution finds no entry and the handler maps
ErrQueueNotFound to -32603. The 03-steering-fix leaf (SetAgentRegistry
late-attach) fixed the "loop holds NO registry" half (registration
never fired at all); THIS residual is the second half: even with
registration firing, the chat lanes armed NO aliases, so only one
identity shape per conversation ever resolved.

The zero "Step job queue aliases armed" lines in daemon-run7.log are
consistent and expected: that log line belongs to the step-job arm
site, and the p5 probe window ran no step jobs.

## Fix

Arm the aliases inside the LOOP at queue-registration time -- one fix
covers every dispatch shape (direct-mode, interactive dispatch, resume
lanes) instead of duplicating per-lane call sites:

`internal/agent/loop.go` (production):

1. `sessionStore` interface now embeds `SessionStoreReader` (Get +
   GetByConversationID). Both production stores satisfy it (already
   pinned by the interface assertion vars); fakes that only had Get gain
   a trivial method. This gives the loop the same dual-lookup the
   handler/dispatcher/registry resolver already use.
2. New unexported `(*AgentLoop).deriveSessionQueueAliases(conversationID)`:
   resolves the turn's conversation key through
   `session.ResolveThreadConversationID` (session's OWN conversation id
   -> primary id -> thread-scoped unwrap) against the loop's session
   store, then returns every stored identity that differs from the
   registration key (session.ID, session.ConversationID; deduped). The
   caller passes the ids to RegisterActiveQueue, so the SAME QueueEntry
   and generation become addressable by exact key under ANY of the
   session's identity shapes; the existing entry-pointer sweep in
   UnregisterActiveQueue removes them together with the primary (no new
   lifecycle). Read-only store reads; nil store, nil session, or no
   differing keys arms nothing (byte-identical legacy behavior). Runs on
   the turn goroutine at registration time only -- no new locking beyond
   the store's own read locks.
3. RunOnceWithParts' registration block appends the derived aliases to
   the one-shot arm before RegisterActiveQueue (comment block documents
   the defect and the coverage contract).

No daemon-side change was needed or made: the step-job lane keeps its
explicit ArmQueueAliases (its ephemeral step-task-... conversation id
resolves no session row, so derivation there is impossible by design --
the processor's LinkedSessions arm remains the correct mechanism for
that lane).

## Regression test (direct-dispatch path, NEW file)

`internal/agent/queue_alias_arm_gate_test.go`:

- `TestSteerBySessionIDDuringDirectDispatchTurn` -- the production
  chain end to end: session-store row with DISTINCT primary/conversation
  ids (contract guard), template loop wired with registry + queue +
  session store, `Manager.GetOrCreateWired` clone (ConfigSnapshot
  carries all three, exactly ChatHandler.sessionLoop's shape), a REAL
  `RunOnce` turn, then mid-turn `GetActiveQueue` by the session's
  PRIMARY id must find the LIVE queue and it must be the SAME entry as
  the conversation-key registration. Pre-fix this exact lookup 404'd --
  the gate fails on the pre-fix tree and passes post-fix.
- `TestQueueAliasesOneShotPerTurn` -- pins the one-shot consume
  semantics the seam relies on (empty ids dropped; second consume nil),
  guarding the stale-alias hazard across turns of a cached loop.

## Verification (all real runs)

- `gofmt -l` clean on all touched Go files.
- `go build ./...` -- ok.
- Required gate: `go test ./internal/rpc/ ./internal/services/
  ./internal/agent/ -run 'TestSteer|TestQueue|TestArm|TestResolve|TestRegistry'
  -count=1` -- ok, all three packages.
- `go test ./internal/services/ -count=1` (whole package) -- ok.
- Existing steering/queue gates:
  `TestFreshQueuePerTurn|TestCloneQueueIsolation|TestRegistry|TestResolve|TestQueueAlias`
  -- ok (C3 lifecycle + b16b6082 alias gates unchanged and green).
- New direct-dispatch gate PASS, and PASS under `-race`.

## Files touched (lane-compliant; nothing committed)

- `internal/agent/loop.go` (interface widen + derivation seam + call site)
- `internal/agent/queue_alias_arm_gate_test.go` (NEW regression gate)
- `docs/plans/20261119-usability-remediation/07-bench/11-steer-arming-PROGRESS.md`
- this report

Sibling-session dirty files untouched (verified: components.go mcp-pin
edits, githooks, e2e manifest, internal/task/step.go, notification_bot
wiring etc. are foreign diffs; loop.go's pre-existing dirty state was
re-read before every patch and my hunks are context-unique).

## Residuals / notes for the orchestrator

- The rig daemon needs a REBUILD + p5 rerun against this binary for the
  end-to-end acceptance evidence (out of this leaf's scope: bench
  runner is a sibling's live file). The in-repo gate proves the
  production path; the rig rerun proves the deployed artifact.
- Ambiguity semantics unchanged: a steer by a session id whose session
  owns multiple live thread queues still surfaces agent.ErrAmbiguousQueue
  (400-class), never a silent pick -- aliases register the session-level
  base id only, thread-key resolution stays C3.
- The interactive-dispatch lane (RouteToAgent) also gains the aliases
  for free (same RunOnceWithParts registration) -- no separate fix was
  needed there, matching the task's "every registered steering queue"
  requirement.
