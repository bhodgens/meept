// Package codemode is the C3 contract (docs/plans/token-efficiency/master.md)
// for running model-written scripts in a process-internal JavaScript sandbox
// whose nested tool calls never enter LLM context: only the one Result does.
//
// Runtime: goja (github.com/dop251/goja), pinned by the spike verdict
// (docs/research/codemode-spike-report.md). SECURITY INVARIANT: this package
// wires ONLY the goja_nodejs eventloop; the goja_nodejs node extensions
// (fs/process/http modules) are NEVER registered. The require() global gets
// a registry with an empty native table and a source loader that rejects
// every path, so require("fs") -> "Invalid module" and no host file is
// reachable; process/fetch/XMLHttpRequest/WebSocket are simply undefined. A
// future dev importing goja_nodejs node-extension modules breaks the no-fs
// gate; keep imports to goja + goja_nodejs/eventloop + goja_nodejs/require
// only.
//
// Memory cap (Options.MemoryLimit): goja exposes NO memory-limit API (spike
// criterion c, soft-FAIL). The cap is enforced by the master.md-sanctioned
// allocation-counting interrupt — see countAlloc/tripMemory in bridge.go and
// docs/workflows/codemode.md for the full mechanic.
package codemode

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	gnrequire "github.com/dop251/goja_nodejs/require"
)

// C3 contract types (master.md) — field names byte-exact; they serialize to
// JSON under these very names in the model-facing tool envelope.
type ToolBridge struct {
	Name        string
	Description string
	InputSchema map[string]any // JSON schema for the model
	Call        func(ctx context.Context, argsJSON string) (string, error)
}

type Result struct {
	OK     bool
	Output string // captured text() output, clamped to 50KB
	Value  string // JSON-serialized return value, clamped to 50KB
	Err    string
}

type Options struct {
	Timeout     time.Duration // wall-clock cap; interrupt mid-script
	MemoryLimit uint64        // bytes; script aborts past this
	Tools       []ToolBridge  // injected callable tools
}

// defaultTimeout is the floor applied to Options.Timeout <= 0 (hard kills
// land ~0.001s past the deadline per the spike; a floor guards
// misconfiguration without changing the C3 shape).
const defaultTimeout = 5 * time.Second

// clampN is the 50KB output/value clamp (C3).
const clampN = 50 * 1024

// Sandbox runs scripts per C3: New -> Execute -> Close. Executes are
// serialized (one owns the VM at a time); bridge-tool goroutines may be in
// flight against the in-flight Execute and are drained/abandoned on Close.
type Sandbox struct {
	loop   *eventloop.EventLoop
	tools  []ToolBridge
	byName map[string]ToolBridge
	opts   Options

	created   bool
	closed    bool
	closeOnce sync.Once

	// outMu serializes captured text() chunks; outputLen tracks the
	// pre-clamp total so Execute can bound Output at read time.
	outMu     sync.Mutex
	outputs   []string
	outputLen int

	// execMu guards execCtx: per-call tool contexts derive from the
	// in-flight Execute's ctx (spike BRIDGE IDIOM).
	execMu  sync.Mutex
	execCtx context.Context

	// vm is set by setupGlobals on the loop goroutine; Execute reads it
	// only while a run is in flight (guarded by runMu), so no race.
	vm *goja.Runtime

	// runGen increments at every Execute start; in-flight tool jobs that
	// outlive their run (interrupt/kill) must not settle promises into a
	// new run or a terminated loop. Atomic: written by Execute (runMu),
	// read by bridge goroutines.
	runGen atomic.Uint64

	// toolJobs tracks in-flight bridge goroutines so Close can drain them
	// (no goroutine leak past Close).
	toolJobs sync.WaitGroup

	// Memory cap state: memCounter is written only by the loop goroutine
	// (memory.alloc + instrumented entrypoints, bridge.go). The cap is
	// PER-EXECUTION — Execute resets the counter and trip flag before
	// running, so a script's allocation budget is measured against a
	// single run, not the sandbox lifetime. memLimit/memThreshold are
	// fixed at New.
	memCounter   atomic.Uint64
	memTripped   atomic.Bool
	closedFlag   atomic.Bool
	memLimit     uint64
	memThreshold uint64

	// runMu serializes Executes against the single shared VM.
	runMu sync.Mutex
}

// New builds a sandbox per C3. Options with a zero/negative Timeout are
// raised to defaultTimeout; MemoryLimit 0 disables the cap; a duplicate
// tool name, or a bridge with an empty Name / nil Call, is a
// configuration error.
func New(opts Options) (*Sandbox, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	byName := make(map[string]ToolBridge, len(opts.Tools))
	for _, t := range opts.Tools {
		if t.Name == "" {
			return nil, errors.New("codemode: tool bridge with empty name")
		}
		if _, dup := byName[t.Name]; dup {
			return nil, fmt.Errorf("codemode: duplicate tool bridge name %q", t.Name)
		}
		if t.Call == nil {
			return nil, fmt.Errorf("codemode: tool bridge %q has a nil Call", t.Name)
		}
		byName[t.Name] = t
	}
	s := &Sandbox{
		tools:        opts.Tools,
		byName:       byName,
		opts:         opts,
		memLimit:     opts.MemoryLimit,
		memThreshold: memoryThresholdFor(opts.MemoryLimit),
	}
	loop := eventloop.NewEventLoop(
		eventloop.EnableConsole(false),
		eventloop.WithRegistry(s.rejectingRegistry()),
	)
	s.loop = loop
	loop.Start()

	// Register the script globals on the loop goroutine (VM access is
	// loop-goroutine-only; the spike's ready-channel pattern).
	ready := make(chan struct{})
	if !loop.RunOnLoop(func(vm *goja.Runtime) {
		s.setupGlobals(vm)
		close(ready)
	}) {
		loop.Terminate()
		return nil, errors.New("codemode: event loop already terminated")
	}
	<-ready
	s.created = true
	return s, nil
}

// rejectingRegistry wires a require() that can never reach host files: no
// native modules are registered in a fresh registry, and the source loader
// rejects every path, so require("x") fails with "Invalid module" and no
// host file is ever read. This keeps the no-fs gate on while the package
// stays eventloop-only (spike verdict risk #1).
func (s *Sandbox) rejectingRegistry() *gnrequire.Registry {
	return gnrequire.NewRegistry(
		gnrequire.WithLoader(func(path string) ([]byte, error) {
			return nil, fmt.Errorf("codemode: sandbox has no module loader (path %q is not reachable)", path)
		}),
	)
}

// setupGlobals defines tools.*, text, ALL_TOOLS, and the memory hook on
// the loop VM. Loop-goroutine-only.
func (s *Sandbox) setupGlobals(vm *goja.Runtime) {
	s.vm = vm
	toolsObj := vm.NewObject()
	for _, t := range s.tools {
		t := t // closure capture
		toolsObj.Set(t.Name, s.toolFunc(vm, t))
	}
	vm.Set("tools", toolsObj)

	vm.Set("text", s.textFunc(vm))

	// ALL_TOOLS: the tool catalog a script can enumerate (name,
	// description, input schema) — plain JS objects, JSON.stringify-able.
	all := vm.NewObject()
	for _, t := range s.tools {
		t := t
		entry := vm.NewObject()
		entry.Set("name", t.Name)
		entry.Set("description", t.Description)
		if t.InputSchema != nil {
			entry.Set("inputSchema", vm.ToValue(t.InputSchema))
		}
		all.Set(t.Name, entry)
	}
	vm.Set("ALL_TOOLS", all)

	vm.Set("memory", s.buildMemHook(vm))
	instrumentAllocations(vm, s)
}

// Close shuts the sandbox down (idempotent). Terminate() stops the loop,
// cancels pending timers, and drains aux jobs — no loop goroutine survives
// (spike verified the loop reaps cleanly even after an interrupted run).
// Afterwards Execute errors cleanly instead of touching a dead VM.
func (s *Sandbox) Close() error {
	if s.loop == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		// Terminate is safe even if a prior run was interrupted: a
		// pending interrupt is only a VM flag and terminates with it.
		s.closedFlag.Store(true)
		s.loop.Terminate()
		s.drainToolJobs()
		s.closed = true
	})
	return nil
}

// closeDrainGrace bounds how long Close waits for in-flight bridge
// goroutines. A bridge that honors its ctx aborts promptly when the
// in-flight run's ctx cancels; a bridge that ignores ctx is abandoned
// past the grace period (documented, acceptable leak for a
// non-cooperative host — Close must never hang).
const closeDrainGrace = 5 * time.Second

// drainToolJobs waits for in-flight bridge goroutines, bounded by
// closeDrainGrace.
func (s *Sandbox) drainToolJobs() {
	drained := make(chan struct{})
	go func() {
		s.toolJobs.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(closeDrainGrace):
		// Abandoned: the slowest in-flight bridge outlives the sandbox.
	}
}

// SortedToolNames returns the bridge names in lexical order (deterministic
// enumeration; the tools object exposes them in the same order).
func (s *Sandbox) SortedToolNames() []string {
	names := make([]string, 0, len(s.byName))
	for n := range s.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// setExecCtx publishes the in-flight Execute's ctx for per-call derivation.
func (s *Sandbox) setExecCtx(ctx context.Context) {
	s.execMu.Lock()
	s.execCtx = ctx
	s.execMu.Unlock()
}

// execCtxCopy returns the in-flight Execute ctx (Background if none), so a
// bridge goroutine can derive its per-call ctx even after the run ends.
func (s *Sandbox) execCtxCopy() context.Context {
	s.execMu.Lock()
	defer s.execMu.Unlock()
	if s.execCtx == nil {
		return context.Background()
	}
	return s.execCtx
}

// joinOutput returns the raw text() accumulation, space-joined (unclamped;
// Execute clamps at read time).
func (s *Sandbox) joinOutput() string {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	b := make([]string, 0, len(s.outputs))
	for _, chunk := range s.outputs {
		b = append(b, chunk)
	}
	return sortJoinSpaces(b)
}

// appendOutput records one text() chunk.
func (s *Sandbox) appendOutput(chunk string) {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	s.outputs = append(s.outputs, chunk)
	s.outputLen += len(chunk)
}

// resetOutput clears captured output at the start of an execution.
func (s *Sandbox) resetOutput() {
	s.outMu.Lock()
	s.outputs = s.outputs[:0]
	s.outputLen = 0
	s.outMu.Unlock()
}

// sortJoinSpaces joins non-empty parts with a single space each.
func sortJoinSpaces(parts []string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += " "
		}
		out += p
	}
	return out
}
