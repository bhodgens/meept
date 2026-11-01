# Codemode Runtime Spike — Verdict (branch 02, leaf 00S)

Research-only. Scratch modules: `/tmp/pi-eval/spike-goja/`, `/tmp/pi-eval/spike-wazero/`,
`/tmp/pi-eval/spike-yaegi/` (own `go.mod` each; `go build` + `CGO_ENABLED=0 go run`).
Repo untouched. Reference pi harness: `/tmp/pi-eval/pi/packages/codemode` (QuickJS/wasm,
`tools.<name>(args)` async JSON bridge, script body = async IIFE with top-level `await`).

## Candidate verdict blocks (exact leaf format)

### 1. goja (dop251/goja @ v0.0.0-20261031103838-cf2ff0ec0b31, + goja_nodejs eventloop)

```
VERDICT: goja | GO
criteria: (a) async-bridge: PASS — tools.<name>(args) returns a goja promise; script uses
          `await`; tool body runs in a goroutine, settles via eventloop.RunOnLoop;
          per-call ctx derives from the in-flight Execute ctx (execMu-guarded). Slow-tool
          case: ctx cancelled at 500ms -> tool returned "tool cancelled" at 500.4ms.
          Evidence: f1 `Value=30` (two sequential reads summed); b2 `Err="codemode:
          execution deadline exceeded (pending promise)" elapsed=2.001113708s` (kill while
          awaiting a slow tool at the 2s deadline, not a hang); case a `elapsed=500.368958ms`
          (per-call ctx cancelled at 500ms propagates to the in-flight tool).
          (b) timeout-kill: PASS — vm.Interrupt(atomic flag checked every 100 bytecode ops
          in goja/vm.go run loop) kills an infinite loop: `for(;;){}` with 2s deadline
          returned `Err="codemode: execution deadline exceeded" elapsed=2.002294084s`;
          blocked-promise case also killed cleanly (b2 above, no hang, no panic).
          (c) memory-cap: FAIL (soft-only) — goja has NO memory-limit API; script heap is
          the host process heap. 5M-append grow completed `OK=true elapsed=1.462905875s`
          with no cap. C3 MemoryLimit field stays (master.md allows replacing mechanics):
          workaround = allocation-counting interrupt (interrupt handler trips on
          allocation/bytecode counter) or process-level cgroup/RSS limit; not a hard cap.
          (d) no-fs-net: PASS — probe in the script: `require fs throws: Invalid module;
          process = undefined; fetch = undefined; XMLHttpRequest = undefined;
          WebSocket = undefined` (only setImmediate present; no fs/net/process globals
          because the node extensions are NOT registered — only the eventloop is used).
          (e) cgo-free dual-platform build: PASS — pure Go, zero cgo.
          `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build` rc=0 (and windows/amd64 rc=0);
          native macOS arm64 build+run rc=0.
          (f) idiom-compat (3 pi-style scripts): PASS — sequential (Value=30),
          map+aggregate (Value={"files":2,"total":30}), early-exit (Value="early-exit");
          plus ES2020 probe `{"big":"100","mapV":1,"opt":null,"setN":2,
          "spread":[1,2,3,4,5],"sum":6}` — bigint/optional-chain/spread/class/Map/Set all
          work. Top-level await works via async-IIFE wrap.
BRIDGE IDIOM: async. `tools.<name>` set as goja function on the event-loop VM returning
          `vm.NewPromise()`; body: marshal arg (string passthrough, else json.Marshal)
          -> run ToolBridge.Call(cctx, argsJSON) in a goroutine with per-call
          cctx=Cancel(execCtx) -> on success `loop.RunOnLoop(resolve(out))`, on error
          `loop.RunOnLoop(reject(Error{err.Error()}))` (JS `catch (e) { e.message }`).
          Timeout: Execute polls the promise; on ctx.Done() call `vm.Interrupt(err)` —
          uncatchable InterruptedError unblocks the in-loop RunString and the poller.
          Result.Value = json.Marshal of the exported return (clamped 50KB).
RISKS:
- No hard memory cap (c FAIL): need allocation-count interrupt tuning; a hostile script
  can grow until host OOM if the counter threshold is set too high.
- goja is ES5.1-core with bolt-on ES6: rare novel syntax can break model-written scripts
  (measured ES2020 idiom coverage is good, but not full V8 parity).
- Must never register goja_nodejs node extensions (fs/process) — the sandbox wires ONLY
  the eventloop; a future dev importing node.Register breaks criterion (d).
- goja_nodejs pulls deps (regexp2, sourcemap, pprof, x/text) into the module graph;
  all pure Go, no cgo.
- Interpreter speed: ~1.5s for 5M appends — fine for LLM-script workloads, not a compiler.
```

### 2. wazero + quickjs (Gaurav-Gosain/quickjs wasm + fastschema/qjs v0.0.6 on wazero v1.11.0)

```
VERDICT: wazero+quickjs | NO-GO
criteria: (a) async-bridge: PASS-with-fragility — SetAsyncFunc blocks the VM thread on a
          channel; tool runs in a goroutine; promise settled on the VM thread (settling
          from a foreign goroutine wasm-traps "invalid table access", so the bridge MUST
          stay on-thread). Slow-tool ctx-cancel case b2 did NOT return cleanly:
          `panic: failed to call QJS_Eval: failed to allocate memory: module closed with
          context canceled (recovered by wazero)` (RC=2) — CloseOnContextDone closes the
          wasm module while a host callback is in flight = hard panic, not a Result error.
          (b) timeout-kill: FAIL — `Option.MaxExecutionTime=2000` did NOT kill an infinite
          loop: case b produced no output and was SIGKILLed at the outer 60s watchdog
          (RC=124, /tmp/pi-eval/wazero-case-b.out empty). The QuickJS-NG max-execution-time
          hook is evidently not effective through this wazero build for pure-JS loops.
          No hard-kill path demonstrated.
          (c) memory-cap: PASS — `Option.MemoryLimit=8MiB`: script pushing 2M strings
          aborted `Err="InternalError: out of memory at __main (script.js:4:54)"
          elapsed=608.463417ms`. Real WASM-heap cap via JS_SetMemoryLimit — the only
          candidate with a hard cap.
          (d) no-fs-net: FAIL — the qjs.wasm build SHIPS WASI globals: probe returned
          `os = object; std = object; bjson = object; fetch = undefined; process =
          undefined; require = undefined; std read err: not a function;
          os.open(go.mod)=number` — `os.open` on a host file SUCCEEDED (fd number).
          Model-written script can reach host fs. Security-critical failure.
          (e) cgo-free dual-platform build: PASS — wazero is pure Go;
          `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build` rc=0; native darwin/arm64 run rc=0.
          (Cost: embedded qjs.wasm binary in the module graph; ~30MB spike binary.)
          (f) idiom-compat (3 pi-style scripts): PASS mechanically — f1 Value=30,
          f3 Value=early-exit both OK. BUT return-value serialization is broken in the
          naive bridge: f2/es-probe returned `[object Object]` (qjs handle .String() on
          an object) — C3 requires JSON-serialized Value; bridge must JSON.stringify
          objects before crossing back. (Fixable, one-line bridge detail.)
BRIDGE IDIOM: async. `xc.SetAsyncFunc(name, ...)` — arg read from JS (string passthrough
          or JSON.stringify), tool goroutine with per-call ctx, result settled via
          `this.Promise().Resolve/Reject` ON THE VM THREAD (foreign-goroutine settlement
          traps the wasm module). Errors: reject with `xc.NewError(err)`.
RISKS:
- (d) WASI fs globals are live in the shipped qjs.wasm — cannot pass the security
  requirement of the orchestrator ("sandbox gets no fs/net except bridges") without a
  custom WASI-syscall-filtered build of QuickJS-NG.
- (b) MaxExecutionTime empirally ineffective for pure-JS loops (hang) — hard-kill
  requirement unmet.
- ctx-cancel mid-await panics the process-recovering wazero panic path (not a clean
  error); CloseOnContextDone interacts badly with in-flight host callbacks.
- Value objects stringify to "[object Object]" unless the bridge JSON.stringify's them.
- Build complexity: embedded WASM + wazero host callbacks; largest dep footprint of the
  three.
```

### 3. yaegi (traefik/yaegi v0.16.1 — Go interpreter, model writes Go)

```
VERDICT: yaegi | NO-GO
criteria: (a) async-bridge: FAIL (idiom) — yaegi has no async/await; bridge is SYNC:
          script imports virtual `tools` package via Use(Exports{"tools/tools": symMap}),
          calls `tools.tool_read(jsonString)` which is a direct Go function. Per-call ctx
          cancellation WORKS when the tool honors ctx: slow-tool case returned
          `Err="codemode: execution deadline exceeded" elapsed=2.00106225s` at the outer
          deadline. A tool that ignores ctx is un-stoppable without killing the process
          (shared heap, no isolation).
          (b) timeout-kill: PASS (measured, with caveats) — EvalWithContext(2s) on
          `for{}` returned `Err="context deadline exceeded" elapsed=2.002038291s` and a
          follow-up leak-check confirmed the loop goroutine actually exited
          (NumGoroutine 2 at return -> 1 after 0.5s, stable at 3.5s). yaegi's
          interpreted loops/channel ops select on interp.done which stop() closes.
          Caveat: a script that spawns a REAL goroutine (`go func(){for{}}()`) leaks it
          — stop() cannot kill untracked goroutines on the host process.
          (c) memory-cap: FAIL — no cap API; script heap IS the host process heap.
          5M-append `OK=true Value=5000000 elapsed=756ms`, no limit engaged.
          (d) no-fs-net: FAIL (security-critical) — with stdlib.Symbols + restricted mode:
          `os.Open("/tmp/pi-eval/spike-yaegi/go.mod")` SUCCEEDED ("module spike-yaegi..."),
          `net/http` client dial SUCCEEDED ("connection refused" = network reachable).
          "Restricted" mode only virtualizes ENV; os, net, os/exec, syscall all live in
          the host process. Model-written untrusted Go = full host capability.
          (e) cgo-free dual-platform build: PASS — pure Go;
          `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build` rc=0; native run rc=0.
          (f) idiom-compat (3 pi-style scripts): FAIL (language shift) — the three
          pi-style scripts run ONLY after porting JS->Go (sync tool calls, no JSON.parse
          sugar; all three ported scripts returned OK: f1 Value=total=30, f2
          Value=total=30 files=2, f3 Value=early-exit). Control flow preserved, but the
          model now writes Go: security-surface shift (d) + model-idiom shift (the
          orchestrator's "model writes JS" assumption does not hold).
BRIDGE IDIOM: sync. `i.Use(stdlib.Symbols)` + `i.Use(Exports{"tools/tools": symMap})`
          where each tool is `func(argsJSON string) (string, error)` closed over
          ToolBridge.Call with per-call ctx; script does `import "tools"` +
          `tools.tool_read("{}")`; errors surface as Go error return values.
          Program shape: complete Go program (`package main` + `var ret string` at pkg
          scope, main() auto-run by Eval); Result.Value read via `i.Eval("ret")`.
RISKS:
- Host-process execution: untrusted model code gets os/net/exec/syscall — (d) unmet,
  fatal for the codemode threat model (untrusted model-generated code).
- No memory cap; no process isolation; a `go func(){for{}}()` leak is unkillable.
- Model writes Go instead of JS — the pi-parity target and model idiom assumption break.
- Use(Exports) key shape is fiddly ("tools/tools" -> import path + pkg name); script
  must be a complete Go program (no REPL-style statement eval for stateful results).
```

## Recommendation (one runtime, rationale <= 10 lines)

**Pick goja.**
1. Only candidate passing the two security-critical gates: (b) hard timeout-kill
   (vm.Interrupt kills infinite loop at 2.002s, clean error) and (d) no fs/net
   (fs/process/fetch/WebSocket all unreachable; only the eventloop is registered).
2. (a) async bridge works exactly like pi's: promise-returning tool funcs, per-call
   ctx cancellation measured at 500ms (case a); blocked-promise timeout at the 2s
   deadline returns cleanly (~2.001s, no hang, no panic) — contrast: wazero panics;
   yaegi is sync-only.
3. (e) pure Go, CGO_ENABLED=0 builds for linux/amd64 + darwin/arm64 both verified rc=0.
4. (f) all 3 pi-style scripts + an ES2020 idiom probe pass unmodified-in-spirit.
5. Sole FAIL is (c) memory-cap: master.md explicitly allows the spike to replace C3
   MemoryLimit's enforcement mechanics per runtime; goja workaround = allocation-count
   interrupt (trip vm.Interrupt on a bytecode/allocation counter) and/or host cgroup
   RSS limit. Acceptable, to be pinned by the 00T gate leaf.
6. wazero+quickjs: NO-GO — (b) MaxExecutionTime hang on infinite loop; (d) WASI
   os.open(host file) succeeds in the shipped qjs.wasm; b2 ctx-cancel panics.
7. yaegi: NO-GO — (d) model-written Go reaches host fs/net/exec in-process; (a) no
   async idiom; (f) language shift off the JS parity target.
8. Residual goja risk to manage: never register goja_nodejs node extensions (would
   break (d)); ES6 coverage is near-not total — gate leaf should add exotic-syntax
   negative cases.

## C3 MemoryLimit mechanics, named per runtime (review-checklist item)

| runtime | C3 MemoryLimit feasibility |
|---|---|
| goja | no hard cap; allocation-counting interrupt (workaround) + optional cgroup/RSS limit; field stays, mechanics replaced |
| wazero+quickjs | HARD cap — Option.MemoryLimit -> JS_SetMemoryLimit on the WASM heap; 8MiB OOM measured |
| yaegi | none — script heap is the host process heap |

## Raw evidence index (reproducible)

- `cd /tmp/pi-eval/spike-goja && go build -o /tmp/pi-eval/goja-spike-bin . && CGO_ENABLED=0 /tmp/pi-eval/goja-spike-bin`
  -> f1/f2/f3 OK; b `Err="codemode: execution deadline exceeded" elapsed=2.002294084s`;
  b2 `...(pending promise) elapsed=2.001113708s` (2s deadline, root ctx);
  a `...(pending promise) elapsed=500.368958ms` (external cancel at 500ms propagates);
  d probes as quoted; c grow 1.46s;
  es6 probe JSON as quoted.
- `cd /tmp/pi-eval/spike-wazero && CASES=<f1|f2|f3|b|b2|c|d|es> /tmp/pi-eval/wazero-spike-bin`
  -> f1 Value=30; f2/f3/es `[object Object]` serialization gap; b = HANG (outer 60s
  RC=124, empty output, file /tmp/pi-eval/wazero-case-b.out); b2 PANIC "module closed
  with context canceled"; c 8MiB OOM in 608ms; d `os.open(go.mod)=number`.
- `cd /tmp/pi-eval/spike-yaegi && /tmp/pi-eval/yaegi-spike-bin`
  -> f1/f2/f3 ported OK; b `context deadline exceeded` 2.002s; a cancelled at 2s;
  c no cap; d os.Open OK + net dial OK; leak check
  (`go run ./leakcheck`): goroutines 2->1->1 (loop goroutine reaped).
- Cross-compile: all three `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -C <dir> -o /dev/null .` rc=0
  (windows/amd64 rc=0 too).
- Pins: goja v0.0.0-20261031103838-cf2ff0ec0b31 (+goja_nodejs v0.0.0-20260918173711-b481721df8a2);
  fastschema/qjs v0.0.6 + tetratelabs/wazero v1.11.0 + Gaurav-Gosain/quickjs
  v0.0.0-20251229184449-65b9e11b7e4c; traefik/yaegi v0.16.1.

## Deviations from leaf

- wazero case b hung (its MaxExecutionTime ineffective): recorded as evidence, outer
  60s watchdog used (no output). Not a timebox overrun — it IS the evidence.
- yaegi has no async idiom: criterion (a) graded on the sync bridge's per-call ctx
  cancellation; marked FAIL with that reading stated, so the verdict stays honest.
- All three candidates incomplete? No — all three fully measured; ~no timebox overrun.
