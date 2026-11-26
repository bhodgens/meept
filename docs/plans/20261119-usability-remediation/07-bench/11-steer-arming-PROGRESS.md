# 11-steer-arming PROGRESS (ArmQueueAliases never arms on direct-dispatch turns)

## Task

Root-cause why the chat-path (direct-dispatch) clone never arms queue
aliases (F-STEER-R7, 5 consecutive rig runs), fix so every steering queue
gets aliases, add a direct-dispatch regression test. No commit.

## Trace (complete)

Where ArmQueueAliases lives:
- Defined: internal/agent/loop.go (one-shot seam, sets l.queueAliasIDs).
- Consumed: RunOnceWithParts, ONLY inside the
  `l.agentRegistry != nil && l.queue != nil` block; the consumed ids ride
  RegisterActiveQueue(conversationID, turnQueue, l, aliasIDs...) as
  exact-key aliases.
- ONLY production arm site: internal/daemon/components.go, inside
  AgentJobProcessor.Process, guarded `if isStepJob && p.sessionStore !=
  nil && p.taskStore != nil` -- aliases = the task's LinkedSessions.

Dispatch shapes and their arming status (pre-fix):
- ASYNC step-job lane: the ONLY path that arms (works).
- DIRECT-MODE chat path (handler.go sessionLoop -> GetOrCreateWired
  clone -> RunOnceWithParts): NEVER ARMS. No call site exists anywhere on
  this path.
- INTERACTIVE dispatch path (RouteToAgent -> RunOnceWithParts): also
  never arms.

Why the steer 404s (F-STEER-R7): b16b6082's alias seam armed ONLY on the
step-job lane; a chat-lane turn registers its queue under exactly ONE
key. Any identity-shape mismatch between that key and the steer key
(the session's PRIMARY session-... id; a thread-scoped id whose base
differs) finds no registration -> ErrQueueNotFound -> -32603. The zero
"Step job queue aliases armed" lines in daemon-run7.log are expected on
this shape: no step jobs ran during the probe window.

## Fix (landed)

Arm the aliases INSIDE the loop at queue-registration time so every
dispatch shape benefits with zero new call-site coupling:

- internal/agent/loop.go:
  1. `sessionStore` interface widened to embed `SessionStoreReader`
     (Get + GetByConversationID -- the production stores and every
     existing satisfier already implement both; asserted by the
     interface's own `_ sessionStore = (*session.SQLiteStore)(nil)`
     pins).
  2. New `deriveSessionQueueAliases(conversationID)`: resolves the turn
     key through session.ResolveThreadConversationID (conv id / primary
     id / thread-scoped id) against the loop's session store and returns
     every stored identity that differs from the registration key
     (session.ID, session.ConversationID). Read-only, nil-safe,
     byte-identical when nothing resolves.
  3. RunOnceWithParts registration block appends the derived aliases to
     the one-shot arm before RegisterActiveQueue.
- internal/agent/queue_alias_arm_gate_test.go (NEW): the direct-dispatch
  regression gate -- template (registry + queue + session store wired) ->
  Manager.GetOrCreateWired clone -> REAL RunOnce turn -> the live queue
  found by the session's PRIMARY id mid-turn, same QueueEntry as the
  conversation key; plus a one-shot consume pin. PASS, PASS under -race.

## Verification (all real runs)

- gofmt -l clean on touched files.
- go build ./... -- ok.
- go test ./internal/rpc/ ./internal/services/ ./internal/agent/ -run
  'TestSteer|TestQueue|TestArm|TestResolve|TestRegistry' -count=1 -- ok
  (all three packages).
- go test ./internal/services/ -count=1 (whole package) -- ok.
- go test ./internal/agent/ -run
  'TestFreshQueuePerTurn|TestCloneQueueIsolation|TestRegistry|TestResolve|TestQueueAlias'
  -- ok (C3 + alias gates unchanged and green).
- New gate PASS under -race.

Sibling-session dirty files untouched (components.go mcp-pin edits,
githooks, e2e manifest, task/step.go, etc. -- diffs verified foreign).
Nothing committed.
