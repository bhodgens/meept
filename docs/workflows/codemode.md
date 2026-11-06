# Codemode Sandbox (internal/codemode)

The codemode sandbox runs model-written JavaScript scripts in-process with
nested tool calls that NEVER enter LLM context — only the one C3 `Result`
does (pi-parity, docs/plans/token-efficiency/master.md contract C3).

## Contract (C3, byte-exact)

```go
func New(opts Options) (*Sandbox, error)
func (s *Sandbox) Execute(ctx context.Context, script string) (Result, error)
func (s *Sandbox) Close() error

type Options struct {
    Timeout     time.Duration // wall-clock cap; interrupt mid-script
    MemoryLimit uint64        // bytes; script aborts past this
    Tools       []ToolBridge
}
type ToolBridge struct {
    Name        string
    Description string
    InputSchema map[string]any
    Call        func(ctx context.Context, argsJSON string) (string, error)
}
type Result struct {
    OK     bool
    Output string // captured text() output, clamped to 50KB
    Value  string // JSON-serialized return value, clamped to 50KB
    Err    string
}
```

The agent-facing tool (`codemode`, branch 02 leaf 02+) serializes `Result`
verbatim into its tool envelope — JSON field names `OK`/`Output`/`Value`/`Err`
are frozen by the e2e gate (`e2e/suites/codemode`).

## Script environment

- `tools.<name>(args)` — async bridge. A string argument passes through
  unchanged (JSON-in convention); any other argument is JSON-marshalled; no
  argument becomes `"{}"`. The call returns a **Promise** that resolves to
  the bridge's result string or rejects with an `Error` carrying the bridge's
  error message (the script can `catch` it).
- `ALL_TOOLS` — plain JS object catalog: `{<name>: {name, description,
  inputSchema}}`.
- `text(...)` — appends `String(...args)` (space-joined) to `Result.Output`
  (clamped at read time).
- `memory` — the allocation-counting surface (below).
- top-level `return <value>` — the script body is wrapped as an async IIFE;
  the exported value is `json.Marshal`-ed into `Result.Value` (clamped 50KB).

## Runtime (spike-pinned)

goja (`github.com/dop251/goja`) + ONLY the `goja_nodejs/eventloop`. The
spike verdict (docs/research/codemode-spike-report.md) pinned goja as the
one candidate passing both security-critical gates (hard timeout kill, no
fs/net). **Never register the goja_nodejs node extensions
(fs/process/http):** that breaks the no-fs gate. This package's imports are
deliberately `goja`, `goja_nodejs/eventloop`, and `goja_nodejs/require`
(only to build the rejecting registry).

### No-fs/net surface

- `require("anything")` — the eventloop's `require` registry is rebuilt with
  a source loader that rejects every path and with an empty native-module
  table, so any require fails (`Invalid module` / loader reject). No host
  file is reachable through it.
- `process`, `fetch`, `XMLHttpRequest`, `WebSocket` — undefined (the node
  extension modules are not imported).
- Timers (`setTimeout`/`setInterval`/`setImmediate` + clears) come from the
  eventloop and are safe; `console` is disabled.

### Hard timeout kill

`Execute` wraps the script as an async IIFE and runs it on the loop
goroutine. On ctx/`Options.Timeout` expiry the sandbox calls
`vm.Interrupt(err)` directly on the loop VM: the interrupt flag is checked
inside the VM bytecode run loop, so an infinite loop unwinds at the next
check (~deadline + 1ms, measured in the spike) with an uncatchable
`*goja.InterruptedError` — `Result.Err` names the deadline ("codemode:
execution deadline exceeded"). A kill while awaiting a slow tool returns the
same clean error (no hang). A `vm.ClearInterrupt()` at run start clears the
sticky flag between executions.

### Memory cap (allocation-counting interrupt)

goja exposes **no memory-limit API** (spike criterion c, soft-FAIL), so
`Options.MemoryLimit` is enforced with the master.md-sanctioned
replacement: an **allocation-counting interrupt**.

- The sandbox tracks a **per-execution** byte counter
  (reset at each `Execute`; the budget is per-run, not sandbox-lifetime).
- Allocation bytes are counted as they flow through measured entrypoints:
  - `memory.alloc(n)` / `memory.allocBytes(n)` — script-visible, exact
    (the script declares how many bytes it allocates).
  - instrumented high-volume entrypoints — `Array.prototype.push`,
    `unshift`, `concat`, global `String`, `JSON.stringify` — estimated from
    argument/result shape (exact for strings, 16-byte floor otherwise).
- When the counter crosses `MemoryLimit` (1:1 byte mapping), the sandbox
  trips `vm.Interrupt(errMemoryCap)` ONCE per execution; the run unwinds
  with `Result.Err` = "codemode: memory cap reached (counted N bytes,
  limit M bytes)".
- **This is a tripwire, not a hard cap:** allocation that never touches a
  measured entrypoint (plain object literals, string concatenation between
  push calls) is not counted. The threshold is tuned so the trip lands well
  before host-OOM territory for measured script shapes (spike: 5M
  10-char-string pushes ≈ 100–200MB host heap; a 64MB cap trips inside
  that). `MemoryLimit == 0` disables the cap.

## Tool bridges

The package is GENERIC: it imports nothing from `internal/tools` or
`internal/tools/builtin` — the dependency direction is *builtin wires the
bridge, sandbox stays generic*. Each tool-call promise body runs in a
goroutine with a **per-call ctx derived from the in-flight Execute ctx**
(execMu-guarded), so a bridge that honors ctx aborts promptly when the run
is killed. Settlement is always routed back onto the event loop
(`loop.RunOnLoop`); a stale settlement (run already replaced or loop
terminated) is dropped, never settled into a dead/new VM.

## The agent-facing `codemode` tool (branch 02 leaf 02)

### Config gate

The tool is **opt-in, default OFF**: `[agent.tools].codemode_enabled`
(default `false`). When false the daemon behaves byte-identically to
pre-codemode — the tool is absent from the shared tool registry, from every
LLM request's tool-definition array, and from every agent's filtered
allow-list. When true:

1. **Registration** — the daemon registers the `codemode` builtin on the
   shared tool registry (`internal/daemon/codemode_wiring.go`,
   `applyCodemodeToolsFromConfig`, mirroring the deterministic-wiring
   pattern).
2. **Grant** — the same flag is passed into
   `agent.RegistryConfig.CodemodeEnabled`; `filterTools` then unions
   `codemode` into every *scoped* agent's allow-list so its filtered
   registry exposes the tool. Empty-scope agents already get the unfiltered
   registry, which carries the tool when registered.
3. **Permission** — `codemode` maps to the dedicated `codemode` action in
   `security.BuiltinRules` at **RiskMedium** (it executes model-generated
   code, but the sandbox cannot widen the agent's tool access, so the risk
   is bounded by the already-granted tools). It stays below HIGH so the
   `require_confirmation_high` gate does not block it.

### Security model (never widen, only batch)

The tool's `Execute` builds the sandbox's `ToolBridge` list **strictly from
the agent's already-filtered tool registry**, which the executor injects
into the tool context (`agent.ContextWithToolRegistry`, read via
`agent.ToolRegistryFromContext`). The sandbox's `ALL_TOOLS` therefore
equals the agent's filtered tool set exactly — `shell_execute` /
`file_write` appear in the script's catalog *only if* the agent's own
filtered registry has them. A nested bridge call re-enters the **same**
filtered registry by name, so a call the agent could not make directly is
not callable inside a script. The sandbox itself is no-fs/net by design
(eventloop-only, rejecting `require`), and no env/secrets are passed into
it. The sandbox only *batches* calls the agent is already permitted to
make.

### Input / output

- **Input schema:** `{ script: string (required), timeout_s?: integer
  (default 60, clamped to [1,600]) }`.
- **Output:** the C3 `Result` serialized to JSON — object keys `OK` /
  `Output` / `Value` / `Err` (frozen by the e2e gate). `Value` is the
  JSON-serialized `return`; `Output` is the accumulated `text(...)`; `Err`
  carries a hard-kill or script throw.

### Copy-paste example script

```js
// Batch four file reads and shape them into one record. The nested
// tool_read calls never enter LLM context — only this script's Value does.
// (tool bridge names follow the agent's own filtered registry; enumerate
// them on ALL_TOOLS.)
const files = ["a.txt", "b.txt", "c.txt"];
const results = [];
for (const name of files) {
  const r = await tools.file_read(name);     // args is a JSON string
  results.push({ name, bytes: String(r).length });
}
text(`read ${results.length} files`);          // -> Result.Output
return { files: results };                     // -> Result.Value (JSON)
```

Calling the tool:

```json
{ "script": "...the script above...", "timeout_s": 30 }
```

## Concurrency & lifecycle

- Executes are serialized per sandbox (`runMu`); tool-call goroutines may
  be in flight against the in-flight Execute.
- `Close()` is idempotent: it terminates the loop, cancels pending timers,
  and drains in-flight bridge goroutines (a bridge that honors ctx returns;
  a bridge that ignores ctx is abandoned — the documented, acceptable leak
  for a non-cooperative host). `Execute` after `Close` returns a clean
  `Result.Err = "codemode: sandbox is closed"`, never a panic.

## Verification

- Unit (sandbox-internal mechanics — accepted e2e-only-rule exception, see
  policy note): `go test -p 2 -count=5 ./internal/codemode/`
- Behavioral gate: `go test -p 2 -tags e2e -count=1 ./e2e/suites/codemode/`
  (hermetic suite; full green lands with the agent-facing tool leaf).

## Policy note (recorded deviation)

New-package unit tests in `internal/codemode/codemode_test.go` are an
accepted exception to the repo's e2e-only test rule: the sandbox-internal
mechanics (timeout-kill wall-clock bound, memory-cap abort, no-fs negative
probes, 50KB clamps, determinism under -count=5) need in-process hooks the
hermetic e2e harness cannot reach. The e2e suite remains the behavioral
gate. Decided by the branch orchestrator at dispatch; recorded here.
