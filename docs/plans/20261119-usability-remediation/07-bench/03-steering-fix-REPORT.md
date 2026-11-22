# 07-bench/03-steering-fix REPORT (bench-residual leaf)

## Defect

`chat.steer` / `chat.followup` returned `-32603 "queue not found"` for EVERY
conversation in every acceptance rig run (p5, runs 4-6 = 4 consecutive failures).
Evidence: `/tmp/usability-rig-acc/probes/p5_steering_run{4,5,6}.log` (steer rejected
on both the session conv id and the derived thread id), `daemon-run6.log`.

## Root cause (not registry identity, not task-path, not probe timing)

One shared `AgentRegistry` exists and the services layer holds it correctly
(`daemon.go:1196 -> services.NewRegistry -> NewChatService`). The defect was a
BOOT-ORDER WIRING GAP in `internal/daemon/components.go`:

1. The daemon singleton template loop (`c.AgentLoop`, components.go:2670) is
   constructed BEFORE `agent.NewAgentRegistry` (components.go:4089) and receives
   neither `WithAgentRegistry` nor `WithMessageQueue`.
2. Every direct-mode chat turn runs on a manager clone of that template
   (`ChatHandler.sessionLoop` -> `loopManager.GetOrCreateWired`, handler.go:3792);
   `ConfigSnapshot` faithfully copies the NIL registry.
3. `RunOnceWithParts` registers the fresh per-turn queue only when
   `l.agentRegistry != nil && l.queue != nil` (loop.go:3526). Both nil -> the
   01-safety/02 fresh-per-turn registration (already landed) never fires for chat
   turns at all.
4. `ChatService.Steer/FollowUp` -> `GetActiveQueue` -> `ErrQueueNotFound` -> 404.
   Deterministic, every turn, every conversation -- exactly the 4/4 probe record.

Hypotheses disposition:
- (1) registry identity mismatch: REJECTED in its two-instances form -- the real
  identity problem was "the chat-serving loop holds NO registry".
- (2) task-path loops never register: RULED OUT for the failing probes (the p5
  primary task is direct-mode); task/dispatched turns clone from registry-built
  templates (dispatcher.go:3756) which already carry registry + queue
  (registry.go:996/1000). The defect was chat-lane-only.
- (3) probe timing: RULED OUT as the cause of 4/4 -- steer at +8s lands inside
  the observed 6-13s turn windows; queue-not-found-after-close would be a
  different, intermittent signature. Probe timing is fine.

## Fix

- `internal/agent/registry.go` -- new `(*AgentLoop).SetAgentRegistry(r)`:
  late-attach seam for an already-constructed loop; nil = no-op; mints the
  steering/follow-up queue (DefaultQueueConfig + RegistryConfig.Queues knobs,
  bus attach, agent-id stamp) ONLY when the loop has none, byte-mirroring
  createLoop's queue construction. Plus wiring seams `SteeringQueue()`,
  `SteeringRegistry()`, `SetChatter()` (test/clone injection).
- `internal/daemon/components.go` -- `c.AgentLoop.SetAgentRegistry(c.AgentRegistry)`
  immediately after `NewAgentRegistry` (inside the multi-agent block; boot-order
  safe -- no turn can be in flight). Every session clone then inherits
  registry + queue via ConfigSnapshot and registers the fresh per-turn queue.

Files touched (lane-compliant; nothing committed):
- `internal/agent/registry.go` (lane)
- `internal/daemon/components.go` (wiring call site; allowed by "fix the
  production wiring" -- it is the one-line attach, not a loop/park file)
- `internal/services/chat_steering_wiring_test.go` (new)
- `docs/plans/20261119-usability-remediation/07-bench/03-steering-fix-PROGRESS.md`
- this report

Not touched (siblings live): loop_park.go, tactical.go, strategic.go, bench runner.

## Verification (all real runs)

- `gofmt -l` clean on all touched Go files.
- `go build ./...` -- ok.
- `go test ./internal/rpc/ ./internal/agent/ -run 'TestSteer|TestQueue|TestResolve|TestRegistry' -count=1` -- ok.
- `go test ./internal/services/ -count=1` -- ok (whole package).
- `go test ./internal/agent/ -run 'TestFreshQueuePerTurn|TestCloneQueueIsolation|TestRegistry|TestResolve'` -- ok (C3 gates unchanged and green).
- NEW production-path gate (`internal/services/chat_steering_wiring_test.go`),
  PASS, and PASS under `-race`:
  1. `TestSteerDuringActiveTurnThroughProductionWiring` -- unwired template
     (exact pre-fix shape) -> SetAgentRegistry -> manager clone -> REAL
     RunOnce turn -> `ChatService.Steer` accepted mid-turn -> exact content
     drained from the live queue. Asserts clone-registry inheritance,
     clone-queue distinctness (C3), and the template's queue-less precondition.
  2. `TestFollowUpDuringActiveTurnThroughProductionWiring` -- same path for
     `FollowUp`; drain-site injection observed (iteration 1 -> 2).
  3. `TestSetAgentRegistryNilSafeAndQueuePreserving` -- nil no-op; attach twice
     keeps the same live queue.

## Residuals / notes for the orchestrator

- The rig daemon needs a REBUILD + rerun of p5 against this binary for the
  end-to-end acceptance evidence; the services/rpc-layer gate here proves the
  wiring, the rig rerun proves the deployed artifact (out of this leaf's scope:
  bench runner is a sibling's live file).
- `chat.queue_status` on a turn-less conversation still reports inactive
  (IsActive=false) -- correct behavior, unchanged.
- Multi-agent disabled (`multiagent.enabled=false`) still means NO registry is
  constructed, so steer remains unavailable-by-design on single-agent installs
  (ChatService returns ErrUnavailable -> surface handles it). If steering should
  work there too, the registry construction must be unconditional -- a separate
  decision, not part of this residual.
- Pre-existing, unrelated failure in the full rpc package:
  `TestTaskApprove_RPC` ("rejection service not available") fails identically
  at HEAD 22730c39 (verified via throwaway baseline worktree). Not steering-
  related; the required steer/queue/resolve/registry gate is green.
