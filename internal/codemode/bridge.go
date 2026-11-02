package codemode

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/dop251/goja"
)

// toolFunc builds the JS function for one bridge tool, per the spike's
// BRIDGE IDIOM: tools.<name>(args) returns a goja promise; the tool body
// runs in a goroutine against a per-call context derived from the
// in-flight Execute's ctx; the promise is settled ON the event loop
// (loop-goroutine-only — the goja promise contract).
//
// Argument marshalling: a string argument passes through unchanged (the
// C3 JSON-in convention); any other argument is JSON-marshalled; no
// argument becomes "{}".
func (s *Sandbox) toolFunc(vm *goja.Runtime, tool ToolBridge) goja.Value {
	fn := func(call goja.FunctionCall) goja.Value {
		var argJSON string
		if len(call.Arguments) > 0 {
			switch raw := call.Argument(0).Export().(type) {
			case string:
				argJSON = raw
			default:
				b, err := json.Marshal(raw)
				if err != nil {
					argJSON = "{}"
				} else {
					argJSON = string(b)
				}
			}
		} else {
			argJSON = "{}"
		}

		promise, resolve, reject := vm.NewPromise()
		gen := s.runGen.Load()
		s.toolJobs.Add(1)
		go func() {
			defer s.toolJobs.Done()
			// Per-call ctx from the in-flight Execute ctx; if Execute
			// already finished (cancelled), the derived ctx is cancelled
			// too and the bridge is told to abort promptly.
			cctx, cancel := context.WithCancel(s.execCtxCopy())
			defer cancel()

			out, err := tool.Call(cctx, argJSON)
			// Settle only if the loop is still running and no newer run
			// has started (a new run owns the VM; a stale settlement
			// would resolve a promise the new run never created).
			if s.closedFlag.Load() || s.runGen.Load() != gen {
				return
			}
			_ = s.loop.RunOnLoop(func(vm *goja.Runtime) {
				if s.closedFlag.Load() || s.runGen.Load() != gen {
					return
				}
				if err != nil {
					e := vm.NewObject()
					e.Set("name", "Error")
					e.Set("message", err.Error())
					_ = reject(e)
					return
				}
				_ = resolve(out)
			})
		}()
		return vm.ToValue(promise)
	}
	return vm.ToValue(fn)
}

// textFunc is the script's text(...) output sink: joins all arguments
// (JS String semantics on each) with spaces and appends one chunk to
// Output. The 50KB clamp happens at Execute read time, so text() itself
// is never truncated mid-write.
func (s *Sandbox) textFunc(vm *goja.Runtime) goja.Value {
	fn := func(call goja.FunctionCall) goja.Value {
		parts := make([]string, 0, len(call.Arguments))
		for _, a := range call.Arguments {
			if a == nil {
				continue
			}
			parts = append(parts, a.String())
		}
		s.appendOutput(strings.Join(parts, " "))
		return goja.Undefined()
	}
	return vm.ToValue(fn)
}

// ---- memory cap: allocation-counting interrupt ----
//
// goja exposes NO memory-limit API (spike criterion c, soft-FAIL), so the
// C3 MemoryLimit field is enforced with the spike-sanctioned
// allocation-counting interrupt: the sandbox counts allocation bytes as
// the script passes them through measured entrypoints (the script-visible
// memory.alloc(n) hook — exact, user-declared bytes; the instrumented
// high-volume entrypoints Array.push/unshift/concat, String, JSON.stringify
// — estimated from argument shape), and when the PER-EXECUTION total
// crosses the threshold (= Options.MemoryLimit, 1:1 byte mapping) it trips
// vm.Interrupt once. The interrupt unwinds the script with a
// "codemode: memory cap reached ..." error. This is a tripwire, not a
// hard cap: allocation that never touches the measured entrypoints is not
// counted, so a hostile script that allocates only through plain object
// literals stays unmeasured — the threshold is tuned so the trip lands
// well before host-OOM territory for the measured script shapes (spike:
// 5M pushed 10-char strings ~100-200MB, aborts inside the cap).

// buildMemHook hands the script a `memory` global. memory.alloc(n) /
// memory.allocBytes(n) count n bytes against the allocation counter and
// return n (the script's own bookkeeping value). They are the "explicit"
// allocation surface: the counter stays honest because the script declares
// how many bytes it is allocating. Loop-goroutine-only.
func (s *Sandbox) buildMemHook(vm *goja.Runtime) goja.Value {
	hook := vm.NewObject()
	alloc := func(args ...goja.Value) goja.Value {
		n := hookArgBytes(args)
		s.countAlloc(vm, n)
		return vm.ToValue(n)
	}
	hook.Set("alloc", vm.ToValue(alloc))
	hook.Set("allocBytes", vm.ToValue(alloc))
	return hook
}

// hookArgBytes reads the first argument as a non-negative byte count.
func hookArgBytes(args []goja.Value) uint64 {
	if len(args) == 0 {
		return 0
	}
	nv, ok := args[0].Export().(float64)
	if !ok || nv < 0 {
		return 0
	}
	return uint64(nv)
}

// instrumentAllocations wraps the high-volume allocation entrypoints so
// their argument bytes flow through the counter. Loop-goroutine-only.
func instrumentAllocations(vm *goja.Runtime, s *Sandbox) {
	// Array.prototype.push/unshift/concat: the measured array-growth path
	// (spike case c is an append loop).
	arrProto := vm.GlobalObject().Get("Array").ToObject(vm).Get("prototype").ToObject(vm)
	for _, m := range []string{"push", "unshift", "concat"} {
		orig, ok := goja.AssertFunction(arrProto.Get(m))
		if !ok {
			continue
		}
		arrProto.Set(m, vm.ToValue(func(call goja.FunctionCall) goja.Value {
			s.countAlloc(vm, estBytes(call.Arguments))
			res, err := orig(call.This, call.Arguments...)
			if err != nil {
				panic(vm.NewGoError(err))
			}
			return res
		}))
	}
	// Global String constructor: String(n) allocation.
	if g, ok := vm.GlobalObject().Get("String").(*goja.Object); ok {
		origStr := g
		vm.Set("String", vm.ToValue(func(call goja.FunctionCall) goja.Value {
			// The String global is a constructor; call it through the
			// original object so semantics (new String vs String) hold.
			origFn, okFn := goja.AssertFunction(origStr)
			if !okFn {
				return origStr
			}
			res, err := origFn(call.This, call.Arguments...)
			if err != nil {
				panic(vm.NewGoError(err))
			}
			if out, ok := res.Export().(string); ok {
				s.countAlloc(vm, uint64(len(out)))
			}
			return res
		}))
	}
	// JSON.stringify output allocation.
	if g, ok := vm.GlobalObject().Get("JSON").(*goja.Object); ok {
		orig := g.Get("stringify")
		if fn, ok := goja.AssertFunction(orig); ok {
			g.Set("stringify", vm.ToValue(func(call goja.FunctionCall) goja.Value {
				res, err := fn(nil, call.Arguments...)
				if err != nil {
					panic(vm.NewGoError(err))
				}
				if out, ok := res.Export().(string); ok {
					s.countAlloc(vm, uint64(len(out)))
				}
				return res
			}))
		}
	}
}

// estBytes estimates the allocation bytes carried by call arguments:
// strings at exact length, enumerable slices element-by-element, other
// values at a 16-byte floor (pointer + small-object estimate). Zero-arg
// calls still count a 16-byte floor so a pure call storm also accrues.
func estBytes(args []goja.Value) uint64 {
	var n uint64
	for _, a := range args {
		if a == nil {
			n += 16
			continue
		}
		switch v := a.Export().(type) {
		case string:
			n += uint64(len(v))
		case []any:
			for _, e := range v {
				if ts, ok := e.(string); ok {
					n += uint64(len(ts))
				} else {
					n += 16
				}
			}
		case []float64:
			n += uint64(len(v)) * 8
		default:
			n += 16
		}
	}
	if n == 0 {
		n = 16
	}
	return n
}
