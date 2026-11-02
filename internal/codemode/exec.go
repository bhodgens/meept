package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dop251/goja"
)

// errTimeout is the sentinel carried in a goja interrupt when the wall-clock
// cap fires. errMemoryCap carries the allocation-counting cap trip.
var (
	errTimeout   = errors.New("codemode: execution deadline exceeded")
	errMemoryCap = errors.New("codemode: memory cap reached")
)

// hardInterrupt trips the VM interrupt directly on the loop VM. The loop
// checks the atomic interrupt flag inside its bytecode run loop (~every 100
// ops), so an infinite loop unwinds at the next check with an uncatchable
// InterruptedError — the spike-measured hard kill (~deadline + 1ms, no
// hang). It is NOT routed through RunOnLoop: when the loop goroutine is
// busy inside RunString, queueing an interrupt job would never run until
// the run returned — chicken and egg.
func (s *Sandbox) hardInterrupt(sentinel error) {
	if s.vm != nil {
		s.vm.Interrupt(sentinel)
	}
}

// Execute runs a script per C3 and returns the Result. The second return is
// a host-side failure (closed sandbox / dead loop); script-level failures
// live inside Result.Err, not the returned error.
//
// Execution shape (spike BRIDGE IDIOM): the script is wrapped as an async
// IIFE so top-level await works, run on the event loop; the returned
// promise is polled from this goroutine, and a ctx deadline interrupts the
// VM (the hard kill).
func (s *Sandbox) Execute(ctx context.Context, script string) (Result, error) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	if s.closed {
		return Result{OK: false, Err: "codemode: sandbox is closed"}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Per-execution budgets: the wall-clock deadline and the allocation
	// counter (both reset per run, not per sandbox lifetime).
	runCtx := ctx
	if s.opts.Timeout > 0 {
		var cancel func()
		runCtx, cancel = context.WithTimeout(ctx, s.opts.Timeout)
		defer cancel()
	}
	// (A caller-supplied ctx deadline is honored implicitly: the runCtx
	// select below also unblocks on the caller's Done().)
	s.memCounter.Store(0)
	s.memTripped.Store(false)
	s.runGen.Add(1)
	s.resetOutput()
	s.setExecCtx(runCtx)

	wrapped := "(async()=>{\n" + script + "\n})()"

	var (
		ranErr error
		p      *goja.Promise
	)
	done := make(chan struct{})
	queued := s.loop.RunOnLoop(func(vm *goja.Runtime) {
		// Clear any sticky interrupt from a prior killed run so this
		// run starts clean (Interrupt is sticky until consumed).
		vm.ClearInterrupt()
		v, err := vm.RunString(wrapped)
		if err != nil {
			ranErr = err
		} else if p2, ok := v.Export().(*goja.Promise); ok {
			p = p2
		}
		close(done)
	})
	if !queued {
		return Result{OK: false, Err: "codemode: event loop not running"}, nil
	}

	select {
	case <-done:
	case <-runCtx.Done():
		// Hard kill at the wall-clock cap.
		s.hardInterrupt(errTimeout)
		<-done
		return s.killedResult(errTimeout.Error()), nil
	}

	// done closed: the run (or its interrupt unwind) finished and the
	// closure has written its results; the channel close establishes
	// happens-before, so read them directly.
	if ranErr != nil {
		var ie *goja.InterruptedError
		if errors.As(ranErr, &ie) {
			if val, ok := ie.Value().(error); ok && errors.Is(val, errMemoryCap) {
				return s.killedResult(fmt.Sprintf("%s (counted allocation crossed the %d byte limit)",
					errMemoryCap.Error(), s.memThreshold)), nil
			}
			return s.killedResult(errTimeout.Error()), nil
		}
		// A JS throw: surface it as a script failure with its message.
		return Result{OK: false, Err: ranErr.Error(), Output: s.clampedOutput()}, nil
	}
	if p == nil {
		// The wrapped IIFE always yields a promise; nil means the run
		// produced nothing exportable — treat as an undefined return.
		return Result{OK: true, Value: "null", Output: s.clampedOutput()}, nil
	}

	// Poll the promise: settlement is driven by the loop goroutine (tool
	// settlements, timers). On ctx expiry the kill interrupts the loop.
	for p.State() == goja.PromiseStatePending {
		select {
		case <-runCtx.Done():
			s.hardInterrupt(errTimeout)
			return s.killedResult("codemode: execution deadline exceeded (pending promise)"), nil
		case <-time.After(5 * time.Millisecond):
		}
	}
	switch p.State() {
	case goja.PromiseStateFulfilled:
		return Result{OK: true, Value: s.exportValue(p.Result()), Output: s.clampedOutput()}, nil
	case goja.PromiseStateRejected:
		msg := "script error"
		if o, ok := p.Result().(*goja.Object); ok {
			// goja property lookup (VM state, no I/O); name-based match
			// against the analyzer's HTTP Get category.
			if m := o.Get("message"); m != nil { //nolint:mutexio // VM property lookup on the rejected promise reason, not I/O
				msg = m.String()
			}
		}
		return Result{OK: false, Err: msg, Output: s.clampedOutput()}, nil
	}
	return Result{OK: false, Err: "codemode: unknown promise state", Output: s.clampedOutput()}, nil
}

// exportValue JSON-serializes the script's exported return value, clamped to
// 50KB. nil -> "null"; unmarshalable values fall back to String().
func (s *Sandbox) exportValue(v goja.Value) string {
	var valStr string
	if v == nil {
		valStr = "null"
	} else if b, err := json.Marshal(v.Export()); err == nil {
		valStr = string(b)
	} else {
		valStr = v.String()
	}
	return clampString(valStr)
}

// clampedOutput returns the text() accumulation clamped to 50KB.
func (s *Sandbox) clampedOutput() string {
	return clampString(s.joinOutput())
}

// killedResult builds the Result for an interrupted run.
func (s *Sandbox) killedResult(errMsg string) Result {
	return Result{OK: false, Err: errMsg, Output: s.clampedOutput()}
}

// clampString truncates s to 50KB, appending a marker that names the total
// (C3's "clamped to 50KB" — the marker survives the 50KB+marker bound the
// leaf's T2/T6 assertions pin).
func clampString(s string) string {
	if len(s) <= clampN {
		return s
	}
	return s[:clampN] + fmt.Sprintf(" ...[clamped to %d of %d bytes]", clampN, len(s))
}

// ---- memory cap mechanic (allocation-counting interrupt) ----

// memoryThresholdFor maps Options.MemoryLimit (bytes) to the allocation
// threshold at which the interrupt trips. 1:1 byte mapping: the counter
// counts BYTES flowing through the measured allocation entrypoints (exact
// for memory.alloc(n), estimated for instrumented entrypoints — see
// estBytes in bridge.go), so threshold == limit reads directly. 0 disables
// the cap.
func memoryThresholdFor(limit uint64) uint64 { return limit }

// countAlloc adds n bytes to the per-execution counter and trips the VM
// interrupt on first crossing of the threshold. Loop-goroutine-only: the
// instrumented entrypoints and memory.alloc run inside the VM, so only the
// loop goroutine calls this; Execute reads the state after a trip, when
// the run has unwound.
func (s *Sandbox) countAlloc(vm *goja.Runtime, n uint64) {
	if s.memThreshold == 0 || n == 0 {
		return
	}
	total := s.memCounter.Add(n)
	if total < s.memThreshold {
		return
	}
	// First crossing (per execution) trips the interrupt; later
	// counter-adds see the trip flag already set and no-op.
	if s.memTripped.CompareAndSwap(false, true) {
		// Stick: trip once per execution; the interrupt unwinds the run
		// with a clean error (spike b2-style, no hang). %w keeps the
		// sentinel identifiable via errors.Is in Execute.
		vm.Interrupt(fmt.Errorf("%w (counted %d bytes, limit %d bytes)",
			errMemoryCap, total, s.memThreshold))
	}
}
