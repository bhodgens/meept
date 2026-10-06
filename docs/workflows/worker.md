# worker pool

**status**: implemented
**date**: 2026-06-18

## overview

meept uses a worker pool to dequeue and process jobs from the internal job queue. workers run as background goroutines that poll the queue, claim available jobs, and dispatch them to the `JobProcessor` for execution. the pool supports dynamic scaling, idle timeouts, and agent-specific job routing.

## architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                         meept platform                            │
│                                                                   │
│  ┌──────────────┐         ┌────────────────────────────────┐     │
│  │ job queue    │────────▶│ worker pool                    │     │
│  │ (internal/   │  claim  │                                │     │
│  │  queue)      │         │  ┌─────────┐  ┌─────────┐     │     │
│  └──────────────┘         │  │ worker  │  │ worker  │     │     │
│                           │  │ (coder) │  │ (chat)  │     │     │
│  ┌──────────────────┐     │  └────┬────┘  └────┬────┘     │     │
│  │ message bus      │◀────│       │             │          │     │
│  │ (worker.* events)│     │  ┌────▼─────────────▼────┐     │     │
│  └──────────────────┘     │  │ agentjobprocessor     │     │     │
│                           │  │ (dispatches to agent  │     │     │
│  ┌──────────────────┐     │  │  loop via registry)   │     │     │
│  │ worker handler    │────│  └───────────────────────┘     │     │
│  │ (bus control)     │     └────────────────────────────────┘     │
│  └──────────────────┘                                            │
│                                                                   │
│  ┌──────────────────┐                                            │
│  │ agent registry   │◀─── multi-agent dispatch                    │
│  │ (agent.AgentLoop │                                            │
│  │  per agent id)   │                                            │
│  └──────────────────┘                                            │
└────────────────────────────────────────────────────────────────────┘
```

**key properties:**
- workers claim jobs either on their poll timer (exponential backoff, 1s to
  15s when idle) or immediately on a wake signal: queues implementing
  `queue.WakeNotifier` signal each worker's registered channel on every
  `Enqueue` (non-blocking, signals drop when the worker is mid-job), so the
  typical step-to-step hop pays no poll latency
- each worker can be tagged with an `agent_id` for agent-specific job routing
- the pool supports dynamic scaling (add/remove workers at runtime)
- a monitoring goroutine publishes pool status every 30 seconds
- worker handler exposes pool control via the message bus

## configuration

### main config (`~/.meept/meept.json5`)

```json5
{
  "workers": {
    "pool_size": 4,                          // initial worker count
    "idle_timeout_seconds": 300,             // worker idle timeout (5 min)
    "default_caps": ["code", "reasoning"],   // default capabilities
  },
}
```

### defaults

| parameter | default | description |
|-----------|---------|-------------|
| `pool_size` | 4 | initial number of workers |
| `idle_timeout_seconds` | 300 (5 minutes) | worker idle timeout before exit |
| `default_caps` | `["code", "reasoning"]` | default capabilities for new workers |

### pool config

```go
type PoolConfig struct {
    Queue       queue.Queue
    Processor   JobProcessor
    MessageBus  *bus.MessageBus
    Logger      *slog.Logger
    DefaultCaps []string
    IdleTimeout time.Duration
}
```

## behavior

### job claiming

each worker runs a polling loop that:

1. checks if the worker state allows claiming (`idle`, `complete`, or `error`)
2. calls `queue.Claim(ctx, workerID, capabilities, agentID)` to attempt to claim a job
3. if a job is claimed, transitions through `claiming` -> `processing` -> `complete`/`error`
4. on success: marks the job as completed in the queue
5. on failure: marks the job as failed, checks retry eligibility (non-retryable errors like budget exhaustion go to dead letter)

### agent-specific routing

workers are tagged with an `AgentID` field. `job.agent_id` is a **soft
preference, not exclusivity**: the claiming agent's own jobs sort first
(`ORDER BY ... CASE WHEN agent_id = ? THEN 0 ELSE 1 END`), but any worker
whose `AgentID` is empty has no agent filter at all and may claim pinned
jobs. this is deliberate — pool workers are generic executors; the agent
persona (model, tools, prompt) rides the job payload and is resolved by
the job processor (`internal/worker/pool.go` starts every worker with an
empty `agentID`).

job priority order: interactive > targeted agent match > priority >
creation time (`id ASC` as the deterministic final tiebreak).

### idle backoff

when no jobs are available, workers use exponential backoff:
- initial poll interval: 1 second
- maximum idle backoff: 15 seconds
- backoff resets to 1 second when work is found

on errors: backoff doubles up to 30 seconds.

### wake-up (event-driven claim)

when the queue implements `queue.WakeNotifier` (the production
`PersistentQueue` does), `Pool.AddWorker` registers a buffered(1) channel
per worker. `Enqueue` sends a non-blocking signal to every registered
channel after the insert commits; the worker's sleep `select` wakes on it
and claims immediately. properties:

- a full channel drops the signal (the worker is mid-job and will claim on
  its next loop anyway)
- a nil channel (queue without wake support, or legacy config) blocks
  forever in the select — byte-identical to the old poll-only behavior
- a wake does NOT reset idle backoff growth; only found work does
- `RemoveWorker`/`Stop` unregister the channel (no waiter leak; `Stop`
  cleans up even when the pool was never started)

### state machine

workers transition through a defined state machine:

```
idle ──────▶ claiming ──────▶ processing ──────▶ complete ──────▶ idle
  │              │                  │                  │
  │              ▼                  ▼                  │
  │           idle (no job)       error ──────────────┘
  │
  ▼
stopping ──▶ stopped
```

valid transitions are enforced by `IsValidTransition(from, to)`.

### dynamic scaling

the pool supports runtime scaling via `pool.Scale(ctx, targetCount)`:
- **scale up**: creates new workers with default capabilities and starts them
- **scale down**: removes idle workers first, then any workers if needed
- each worker's goroutine is tracked via `sync.WaitGroup` for clean shutdown

### monitoring

a background goroutine publishes pool status every 30 seconds on the message bus:

| event topic | description |
|-------------|-------------|
| `worker.pool.started` | pool started with n workers |
| `worker.pool.stopped` | pool shut down |
| `worker.started` | individual worker added |
| `worker.stopped` | individual worker removed |
| `worker.status` | periodic status update (30s) |

## implementation details

### go package: `internal/worker/`

| file | purpose |
|------|---------|
| `worker.go` | `Worker` struct, `Config`, `WorkerStats`, polling loop, job claiming and processing |
| `pool.go` | `Pool` management, `PoolConfig`, dynamic scaling, `Handler` for bus control, `PoolStats` |
| `state.go` | `State` type, valid state transitions, state machine enforcement |

### key types

```go
type Worker struct {
    ID           string
    Capabilities []string
    AgentID      string        // agent-specific routing tag
    State        State
    CurrentJob   *queue.Job
    JobsComplete int
    JobsFailed   int
    // ... internal fields
}

type Pool struct {
    workers     map[string]*Worker
    queue       queue.Queue
    processor   JobProcessor
    bus         *bus.MessageBus
    defaultCaps []string
    idleTimeout time.Duration
}

type WorkerStats struct {
    ID           string
    AgentID      string
    State        State
    Capabilities []string
    JobsComplete int
    JobsFailed   int
    CurrentJobID string
}
```

### agent job processor

the `AgentJobProcessor` (in `internal/daemon/components.go`) implements the `JobProcessor` interface and dispatches jobs to the appropriate agent loop:

- if the job has an `AgentID` and a registry is configured, it dispatches to the agent-specific loop
- otherwise, it falls back to the main agent loop
- the processor is wired with `WithRegistry(c.AgentRegistry)` during platform initialization

```go
type AgentJobProcessor struct {
    agentLoop *agent.AgentLoop
    registry  *agent.AgentRegistry
    logger    *slog.Logger
}
```

### worker handler

the `Handler` exposes pool control via the message bus:

| bus topic | action |
|-----------|--------|
| `worker.add` | add a worker (with optional capabilities and agent_id) |
| `worker.remove` | remove a worker by id |
| `worker.list` | list all workers with stats |
| `worker.stats` | get pool statistics |
| `worker.scale` | scale pool to target count |

### platform wiring

the worker pool is created and started during platform component initialization:

1. `AgentJobProcessor` is created with the main agent loop and optional agent registry
2. `Pool` is created via `NewPool(PoolConfig)` with the queue, processor, message bus, and config
3. `Pool.Start(ctx, poolSize)` starts the initial workers and monitoring goroutine
4. `Handler` is created via `NewHandler(pool, msgBus, logger)` and started for bus control
5. on shutdown: `Pool.Stop(ctx)` cancels the context and waits for all workers via `WaitGroup`

## multi-agent dispatch

workers support the multi-agent architecture by filtering jobs based on `agent_id`:

| scenario | job `agent_id` | worker `AgentID` | result |
|----------|----------------|-------------------|---------|
| targeted dispatch | `coder` | `coder` | worker claims job |
| targeted dispatch | `coder` | `chat` | worker skips job |
| general dispatch | `""` (empty) | any | worker claims if capabilities match |

this allows the orchestrator to route specific tasks to specialist agents (coder, debugger, planner, etc.) by setting `agent_id` on the job, while general tasks are available to any worker.

## testing

```bash
# unit tests
go test ./internal/worker/... -v

# with race detection
go test -race ./internal/worker/... -v

# integration tests (platform wiring)
go test ./internal/daemon/... -v -run Worker

# end-to-end via cli
./bin/meept status
```

## troubleshooting

**"workers not picking up jobs"**
- verify the job queue is configured and jobs are being enqueued
- check `default_caps` matches the capabilities required by jobs in the queue
- if using agent-specific routing, ensure a worker with the matching `AgentID` exists
- check platform logs for `"failed to add worker"` or `"worker failed to start"` messages

**"worker pool not starting"**
- verify `pool_size` is greater than 0 in config (default: 4)
- check that the queue is properly initialized before the pool starts
- look for `"pool already started"` in logs (start is idempotent via `sync.Once`)

**"jobs going to dead letter queue"**
- check if the error is non-retryable (e.g., budget exhaustion) — these bypass retry
- review worker logs for `JobsFailed` count and error messages
- verify the queue's retry configuration (`max_retries`)

**"workers not shutting down cleanly"**
- the pool's `Stop(ctx)` waits for all workers via `WaitGroup` with a context timeout
- if a worker is stuck in `processing`, it will only exit when the context is cancelled
- check for hung job processors (e.g., agent loop not respecting context cancellation)

## related

- job scheduling: `docs/workflows/job-scheduling.md`
- multi-agent architecture: `docs/concepts/multi-agent.md`
- agent orchestration: `docs/workflows/agent-orchestration.md`
- metrics: `docs/workflows/metrics.md`
- configuration: `docs/configuration/index.md`
