# 07-bench/03-steering-fix PROGRESS (bench-residual leaf)

## Task 1: Root-cause -- COMPLETE

### Evidence re-read
- p5_steering_run{4,5,6}.log: chat.submit acks (conv + turn ids minted), then chat.steer at +8s
  on both conv id and derived thread id -> `-32603 "queue not found"` in every run. Turns run
  6-13s, so +8s is plausibly mid-turn; timing alone cannot explain 4/4 failures.
- Rig daemon log run6: "Agent registry initialized specs=28" -- the shared registry EXISTS.

### Code-path trace (production wiring)
1. `chat.steer` -> `internal/rpc` -> `services.Chat.Steer` -> `agentRegistry.GetActiveQueue`
   (internal/services/chat_service.go:333). The registry the chat service holds is
   `c.AgentRegistry` (daemon.go:1196 -> nilSafeAgentRegistry).
2. The executing chat turn does NOT run on a registry-built loop. Direct mode:
   `ChatHandler.handleRequest` -> `h.sessionLoop(conv)` -> `loopManager.GetOrCreateWired(conv,
   wd, h.loop)` -- the template is the DAEMON SINGLETON `c.AgentLoop`
   (internal/agent/handler.go:3792).
3. The daemon singleton is built at internal/daemon/components.go:2670 with NO
   `agent.WithAgentRegistry` and NO `agent.WithMessageQueue`. The `NewAgentRegistry` site is
   components.go:4089 -- constructed AFTER the loop, and no setter ever back-fills the loop.
4. `RunOnceWithParts` registers the fresh per-turn queue only when
   `l.agentRegistry != nil && l.queue != nil` (internal/agent/loop.go:3526). For every
   direct-mode chat loop both are nil -> registration is silently skipped ->
   `GetActiveQueue` -> `ErrQueueNotFound` -> 404. Every turn, every run. 4/4 explained.
5. Registry IDENTITY: services.Chat and the (unused-for-chat) registry-built loops DO share
   the one c.AgentRegistry instance. The mismatch is not two registries; it is that the
   chat-serving loop never holds ANY registry. ConfigSnapshot carries
   `WithAgentRegistry(l.agentRegistry)` (loop.go:10178), so clones of a wired template
   inherit the registry -- but the daemon template itself was never wired.
6. Dispatched (task-path) turns clone from a REGISTRY loop (dispatcher.go:3756
   `d.registry.Get(agentID)` as template), which DOES get registry + queue at
   registry.go:996/1000 -- so only the direct chat lane is broken, matching the probe.
   The probe's primary task stays direct-mode (5 simple file writes, no dispatch).

### Verdict
- Hypothesis (1) registry identity mismatch: partially -- one registry exists, but the
  chat-serving template loop never received it (wiring-order gap, no post-construction
  setter existed).
- Hypothesis (2) task-path loops never register: ruled out for the failing probe (turns
  were direct-mode); the task path inherits a wired registry via its template.
- Hypothesis (3) probe timing: not the cause of 4/4 failures; +8s sits inside the observed
  6-13s turn window in most runs, and a closed queue would give a different error path.

### Fix plan
- Add `(*AgentLoop).SetAgentRegistry(r *AgentRegistry)` in internal/agent/registry.go (lane
  file): locks l.mu, sets the registry, and mints a default per-turn-style queue when the
  loop has none (mirrors the registry createLoop queue construction at registry.go:986-999)
  so the `l.queue != nil` registration guard holds.
- Wire it in internal/daemon/components.go immediately after `NewAgentRegistry`:
  `c.AgentLoop.SetAgentRegistry(c.AgentRegistry)` -- one call site, then every
  GetOrCreateWired chat clone inherits registry + queue via ConfigSnapshot.
- New services-level test proving steer-during-active-turn reaches the live queue through
  the production path (template loop + SetAgentRegistry + manager clone + ChatService.Steer).

## Task 2: Production wiring fix -- COMPLETE

- internal/agent/registry.go: added `(*AgentLoop).SetAgentRegistry(r *AgentRegistry)` --
  late-attach seam for the daemon singleton template; nil registry = no-op; mints the
  steering/follow-up queue (DefaultQueueConfig + RegistryConfig.Queues knobs, bus attach,
  agent-id stamp) ONLY when the loop has none, mirroring createLoop's queue construction.
  Also added read/wiring seams `SteeringQueue()`, `SteeringRegistry()`, `SetChatter()`.
- internal/daemon/components.go: `c.AgentLoop.SetAgentRegistry(c.AgentRegistry)` immediately
  after NewAgentRegistry (multi-agent block). ConfigSnapshot then carries registry + queue
  onto every GetOrCreateWired session clone at turn time; registration guard now fires.
- gofmt clean; `go build ./...` clean.

## Task 3: Proof -- COMPLETE

- internal/services/chat_steering_wiring_test.go (new, 3 tests, all PASS, -race PASS):
  1. TestSteerDuringActiveTurnThroughProductionWiring -- UNWIRED template (pre-fix daemon
     shape) -> SetAgentRegistry -> manager GetOrCreateWired clone -> real RunOnce turn ->
     ChatService.Steer (the exact services-layer RPC surface) accepted mid-turn, message
     captured in the live queue, drained with exact content after turn end. Also asserts
     clone inherits registry, inherits a DISTINCT queue (C3 isolation), and template starts
     queue-less (precondition).
  2. TestFollowUpDuringActiveTurnThroughProductionWiring -- chat.followup on the same
     production path; follow-up injection observed ("Follow-up message injected", iteration
     1 -> 2 reasoning cycle continues).
  3. TestSetAgentRegistryNilSafeAndQueuePreserving -- nil no-op; second attach preserves
     the live queue instance.
- Required verification: `go build ./...` ok;
  `go test ./internal/rpc/ ./internal/agent/ -run 'TestSteer|TestQueue|TestResolve|TestRegistry' -count=1`
  ok; full ./internal/services/ ok; queue lifecycle gates (TestFreshQueuePerTurn,
  TestCloneQueueIsolation, TestRegistry*, TestResolve*) ok.
- Full `./internal/rpc/` package note: `TestTaskApprove_RPC` fails
  ("rejection service not available", task_approval_test.go:48) -- verified
  PRE-EXISTING at HEAD 22730c39 via a throwaway baseline worktree (identical
  failure with this leaf's changes absent). Unrelated to steering; flagged for
  the orchestrator. The required steer/queue/resolve/registry gate is green.
- Test-harness note: the followup gate initially self-deadlocked because the follow-up
  drain CONTINUES the reasoning cycle (loop.go:6571) and re-called the spy chatter, which
  injected another followup each iteration (feed-forward loop in the TEST, not production);
  hook now injects on first call only.

