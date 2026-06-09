# Cluster F: Debugger Integration — DAP Parity

## Goal
Achieve parity with oh-my-pi's debugger capabilities and support additional debuggers (ZFS kernel, Go runtime).

## Background
Meept's `DebugTool` (`internal/tools/builtin/debug.go`) supports:
- Launching with adapter detection (dlv, gdb, lldb-dap, debugpy, codelldb)
- Basic DAP operations: breakpoints, step, continue, pause, evaluate, stack trace, threads, scopes, variables, terminate
- Single active session model

oh-my-pi's debugger (#03) supports:
- Attaching to running processes (not just launching)
- Native C debugging (lldb)
- Go debugging (dlv)
- Python debugging (debugpy)
- Live goroutine inspection for Go
- Wedged process inspection

User specifically asked about:
- ZFS development debugging (likely C/kernel-level)
- Go development debugging (dlv features we may not expose)
- Extending debugger tools generally

## Feature Checklist

### 1. Attach to Running Process
- DAP supports `attach` request alongside `launch`
- Add `action: attach` to DebugTool
- Parameters: `processId` or `processName`
- Adapter auto-detection from the target process

### 2. Extended Go/Delve Support
- Go has unique debugging features:
  - Goroutine stacks (`dlv goroutines`)
  - Goroutine switching
  - Channel inspection
  - Heap analysis
- Delve's DAP server exposes some of these
- We should expose: goroutine filtering, channel state inspection

### 3. Kernel/Native Debugging (ZFS, C)
- ZFS development typically uses:
  - `gdb` for kernel module debugging (with kgdb)
  - `mdb` on illumos
  - `crash` utility for kernel dumps
- Meept could support: kernel module symbol loading, core dump analysis
- Layer: add `core_dump` action that loads crash dump and runs post-mortem analysis

### 4. Debugger Scripting
- Save and replay debugger command sequences
- `script` action: load a script of commands to execute
- Useful for automated crash analysis workflows

## Implementation Plan

### Phase 1: Attach Support
1. Add `attach` action to DebugTool
2. Add `AttachRequest` to `internal/debug/client.go`
3. Support pid lookup by process name
4. Auto-detect adapter from process binary type

### Phase 2: Go-Specific Debugging
1. Add `goroutines` action (list all goroutines with status)
2. Add `set_goroutine` action (switch context)
3. Add goroutine filter to existing actions
4. Detect Go binary automatically and suggest dlv

### Phase 3: Core Dump / Post-Mortem
1. Add `load_core` action
2. Adapter: `gdb`, `lldb`, or `delve` (for Go)
3. Walk stack traces, variables at crash point
4. Generate crash report summary

### Phase 4: Debugger Scripting
1. Add `script` action that reads command sequence from file
2. Execute each command, collect results
3. Useful for automated triage workflows

## Files to Modify / Create
- `internal/debug/client.go` — Attach, goroutines, core dump support
- `internal/debug/session.go` — Session mode tracking (launch vs attach vs core)
- `internal/tools/builtin/debug.go` — New actions
- `internal/debug/adapter_go.go` (new) — Go/delve-specific helpers
- `internal/debug/adapter_native.go` (new) — Native/gdb core support

## Success Criteria
- [x] Can attach to a running Go process and inspect goroutines
- [x] ZFS crash dump loaded and analyzed for root cause
- [x] Debugger scripts execute sequences of commands
- [x] All existing DAP operations continue to work
