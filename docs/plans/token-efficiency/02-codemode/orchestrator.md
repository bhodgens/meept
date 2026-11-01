# Branch 02 - Codemode Sandbox Tool - Orchestrator

> **For the executing agent:** You are the orchestrator for this branch.
> RESEARCH-GATED: dispatch the spike leaf FIRST; its verdict picks the JS
> runtime and pins contract C3's feasibility. Implementation leaves dispatch
> only after the spike returns a GO verdict and the orchestrator records the
> chosen runtime here. If the spike returns NO-GO, mark the branch BLOCKED and
> report to the user - do not force a runtime.

## Meta

- **Parent:** ../master.md
- **Gap:** GAP-2 (docs/research/token-efficiency-2026-10/pi-evidence.md). Pi: packages/codemode (QuickJS/wasm).
- **Contract:** C3 (../master.md) - Sandbox API, `codemode` tool, config gate default OFF.
- **New package:** internal/codemode -> AGENTS.md package table + e2e suite + manifest path_map in the same commits (pre-commit check 18/18).

## Design Notes (pi parity target)

Pi's codemode: model writes JS; the sandbox's only capability is calling
injected tools; nested tool calls NEVER enter LLM context; only script output +
return value do. Meept equivalent: a `codemode` builtin tool with
`{script, timeout_s?}` input. Script environment exposes `tools.<name>(argsJSON)`
(async), `ALL_TOOLS` (name+description list), `text(...)` capture, and a return
value. Both clamped to 50KB (reuse C1 caps). Which tools are callable: the
agent's already-filtered registry (FilteredToolRegistry) - the sandbox must NOT
widen tool access; it only batches calls. Security: the script is
model-generated untrusted code - the sandbox gets no fs/net/process capability
except through injected tool bridges; timeout is a hard kill; memory cap aborts.

## Child Index

| # | Child | Type | Depends on | Est. context | Concurrency |
|---|-------|------|-----------|--------------|-------------|
| 00S | 00S-runtime-spike.md | research leaf | none | 60K | solo, FIRST |
| 00T | 00T-gate.md | test leaf | 00S verdict | 45K | solo |
| 01 | 01-sandbox-core.md | leaf | 00T | 100K | solo |
| 02 | 02-codemode-tool.md | leaf | 01 | 90K | solo (needs 01's API) |
| 03 | 03-e2e-suite.md | leaf | 02 | 60K | solo |

Sequential chain after the spike (each leaf consumes the previous API).

## Runtime candidates (spike decides)

1. **goja** (pure-Go ES5.1+ partial ES6): zero cgo, trivial embed, interrupt
   via vm.Interrupt; no wasm. Risk: ES6 gaps break model-written idioms
   (async/await support is partial - verify in spike; pi's scripts rely on
   top-level await).
2. **quickjs-emscripten / wazero+quickjs**: pi's own choice compiled to wasm;
   wazero = pure-Go wasm runtime (no cgo). Full ES2020 incl. async. Risk:
   build complexity, host-callback bridge across wasm boundary.
3. **yaegi** (Go interpreter, not JS): model writes Go instead of JS. Native
   tool bridging, no sandbox boundary as strong as wasm. Risk: security
   surface, model fluency in the bridge idiom.
Spike criteria: (a) async tool calls with per-call ctx cancellation; (b) hard
timeout kill mid-script; (c) memory cap enforcement; (d) no fs/net reachable
from script; (e) build works on macOS arm64 + linux amd64 CI with `CGO_ENABLED=0`
preferred; (f) model-written-idiom compatibility (test 3 real pi-style scripts).

## Dispatch Protocol

1. Dispatch 00S (read-only + /tmp scratch; no repo writes except its report).
2. Record verdict + chosen runtime in this doc (Completion Tracking Table notes).
3. Dispatch 00T gate -> VALIDATED -> commit.
4. Dispatch 01 -> review -> commit (includes AGENTS.md table row).
5. Dispatch 02 -> review -> commit (config gate default OFF; docs/workflows/codemode.md).
6. Dispatch 03 -> review -> commit (e2e/suites/codemode + manifest path_map).
7. Branch integration: ../master.md Integration Test Plan items 1-4.

## Interface Contracts

C3 (../master.md) is FROZEN except: the spike may replace `MemoryLimit`
enforcement mechanics (per-runtime) - the Options field stays. Tool bridge
JSON round-trip: args and results are JSON strings (mirrors pi).

## Test Gate

| Feature | Gate leaf | Test target | Status |
|---------|-----------|-------------|--------|
| sandbox core (timeout, memory, tool bridge, no fs) | 00T-gate.md | NONE unit (new feature = e2e policy); gate writes e2e/suites/codemode skeleton cases that fail RED | PENDING |
| codemode tool registration + config gate OFF | 00T-gate.md | e2e cases | PENDING |

Note: repo policy says new features get e2e suites, not new unit test files.
For a new package the gate leaf authors the e2e suite RED (build-tagged `e2e`),
and leaf 03 finalizes/extends it. Orchestrator records the policy deviation
(e2e-as-gate) in the tracking table.

## Coding Conventions

Inherited from ../master.md. Branch-specific:
- internal/codemode must not import internal/tools/builtin (keep the sandbox
  generic; builtin wires the bridge).
- No cgo if avoidable (cross-platform invariant; golang-cross-platform skill).
- Config: `[agent.tools] codemode_enabled = false` default; schema validation
  rejects unknown values; docs in config template comments.

## Completion Tracking Table

| Child | Status | Commit | Notes |
|-------|--------|--------|-------|
| 00S-runtime-spike | COMPLETE | - | verdict GO: goja (report /tmp/pi-eval/codemode-spike-report.md; repo copy docs/research/codemode-spike-report.md pending archive). Criteria: (a) async-bridge PASS (eventloop promise, per-call ctx); (b) timeout-kill PASS (vm.Interrupt at 2.002s, clean Err, no hang); (c) memory-cap FAIL soft-only (no cap API - allocation-count interrupt workaround, C3 field kept per master.md allowance); (d) no-fs-net PASS (fs/process/fetch/WebSocket unreachable; register ONLY eventloop, never goja_nodejs); (e) cgo-free PASS (linux amd64 + darwin arm64 CGO_ENABLED=0 builds rc=0); (f) idiom-compat PASS (3 pi-style scripts + ES2020 probes). wazero+quickjs NO-GO (b: MaxExecutionTime ineffective, ctx-cancel mid-await panics; d: shipped qjs.wasm WASI os.open reaches host fs). yaegi NO-GO (d: host os/net/exec live = fatal for untrusted-code threat model; no async idiom). BRIDGE IDIOM: async - tools.<name> returns a goja promise on the eventloop VM; tool body in a goroutine with per-call ctx; vm.Interrupt(err) on deadline; Result.Value = json.Marshal of exported return (50KB clamp) |
| 00T-gate | PENDING | - | |
| 01-sandbox-core | PENDING | - | |
| 02-codemode-tool | PENDING | - | |
| 03-e2e-suite | PENDING | - | |

## Integration Test Plan

1. `go build ./... && go test -p 2 ./internal/codemode/...` green.
2. `make e2e-fast-area AREA=codemode` green (hermetic; no live model).
3. Config gate: with codemode_enabled=false the tool is NOT registered
   (e2e case); with true it is (e2e case).
4. Token-savings smoke (recorded in leaf 03 report): one codemode script
   performing 5 file_read calls vs 5 direct tool calls - count context bytes
   both ways with a deterministic harness (no LLM needed: fixed script).

## Review Checklist

- [ ] Spike verdict recorded before any implementation dispatch
- [ ] Hard timeout kills mid-script (test with infinite loop)
- [ ] Script cannot reach fs/net except via bridges (negative tests)
- [ ] codemode_enabled default false; validate rejects typos
- [ ] AGENTS.md package table + docs/workflows/codemode.md in same commits
- [ ] internal/codemode does not import builtin (dependency direction)
