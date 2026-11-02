# Child process lifecycle - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit. Do NOT use read_file
> on existing source files - explore with search_files or terminal cat.
> Write once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** internal/voice child supervisor: lazy spawn on first call, idle stop after last call, parent-death supervisor reuse, orphan sweep integration, RSS measurement log
- **Dependencies:** leaf 07 (child binary exists)
- **Estimated Context:** 75K
- **Concurrency Group:** C
- **Files this leaf owns:** internal/voice/childproc.go

## Goal

The daemon starts cmd/meept-voice on the first call, reuses it for further
calls, stops it idle_stop_seconds after the last call ends, and never
leaves it behind: a killed daemon must not orphan the child. This leaf
follows the local-runtime lifecycle rules in internal/daemon/AGENTS.md
verbatim: parent-death supervisor wrapper, sweep-before-spawn, never kill
another instance's child, pid-file ownership token.

## Context

The daemon ALREADY has every primitive (read before writing):
- internal/llm/runtime_process.go - RuntimeProcess: spawn under
  `meept-daemon --supervise-parent <pid> -- <argv>` wrapper, process-group
  kill (SIGTERM then SIGKILL after grace), single cmd.Wait invariant,
  pid-file ownership token, ErrRuntimeNotOwned.
- internal/llm/runtime_manager.go - SweepOrphanRuntimes (ppid==1 +
  cmdline match + ownership veto rules).
- internal/daemon/orphan.go - StartupOrphanSweep wiring.
Decision: wrap RuntimeProcess rather than fork its logic. If RuntimeProcess
is too llama-endpoint-shaped (health probes etc.), extract the minimal
supervised-spawn seam; do NOT copy-paste the kill logic (a second copy
drifts). Record the choice.
Spawn shape: argv = [meept-voice, --text-socket <path>, --audio-dir
<path>]; start token via STDIN (never argv/env - leaf 07 contract);
env carries NO secrets. ONNX runtime lib path passed via env
VOICE_ONNX_LIB (a path is not a secret) or flag.
Lifecycle states: stopped -> starting (spawn + text socket connect + setup
handshake) -> running (>=1 call) -> draining (0 calls, idle timer armed)
-> stopped. A call arriving during starting queues on the handshake
(bounded wait, then ErrChildUnavailable -> call refused, Telnyx gets
hangup). A call arriving during draining cancels the timer.
Idle stop: timer fires only at zero live calls (registry Live() check,
leaf 14 interface - depend on the C8 shape, not the implementation).
Kill path: SIGTERM the wrapper, bounded grace, SIGKILL the group; socket
files under voice/sock cleaned (leaf 03 AudioPair.Close on call end;
childproc removes stale files at spawn).
Measurement (open point): log child RSS at spawn, at first call, and at
idle stop (darwin: task_info or ps -o rss; keep it a debug log line, not a
metric store write in this leaf).

## Verified Anchors

- internal/llm/runtime_process.go - supervised spawn + group kill (read fully)
- internal/llm/runtime_manager.go - SweepOrphanRuntimes rules
- internal/daemon/AGENTS.md - lifecycle invariants (sweep before spawn, never kill another instance's child, ownership token)
- internal/voice/registry.go C8 (leaf 14 interface, pinned in master.md)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
type ChildProc struct{ ... }
func NewChildProc(cfg ChildProcConfig, logger *slog.Logger) *ChildProc
type ChildProcConfig struct{ BinaryPath, TextSocketPath, AudioDir, OnnxLib string; IdleStop time.Duration; StartTimeout time.Duration }
func (p *ChildProc) EnsureRunning(ctx context.Context) error   // spawn if needed; blocks to handshake or ErrChildUnavailable
func (p *ChildProc) NoteCallStart(ccid string)
func (p *ChildProc) NoteCallEnd(ccid string)                   // arms idle timer at zero
func (p *ChildProc) Stop(ctx context.Context) error            // graceful; daemon shutdown calls this
func (p *ChildProc) Alive() bool
var ErrChildUnavailable = errors.New("voice child unavailable")
```

### What This Leaf Consumes

- C8 Registry.Live() shape for the idle check (interface-typed dep, nil = single-call assumption documented)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Read runtime_process.go; decide wrap-vs-extract; record in report | - | - | - | decision recorded | - |
| T2 | EnsureRunning: spawn fake child binary (test helper: a tiny go-run script echoing the handshake), connect text socket, handshake within StartTimeout; second EnsureRunning reuses (one spawn) | Create: internal/voice/childproc.go, childproc_test.go | go test -p 2 ./internal/voice/ -run TestChildProcEnsure | - | PASS | Given stopped; When two EnsureRunning; Then one child process |
| T3 | Idle stop: NoteCallEnd at zero live calls arms timer; NoteCallStart during drain cancels; timer fires -> Stop -> process gone | (same) | go test -p 2 ./internal/voice/ -run TestChildProcIdle | - | PASS | Given one call ended; When idle elapses; Then child reaped |
| T4 | Hard-kill safety: SIGKILL the daemon-side test parent; assert the wrapper takes the child down (skip on non-darwin/linux with t.Skip + reason) | (same) | go test -p 2 ./internal/voice/ -run TestChildProcParentDeath | - | PASS | Given supervised child; When parent dies; Then child exits within grace |
| T5 | Stale socket cleanup at spawn; ErrChildUnavailable on handshake timeout (call refusal path, no hang) | (same) | go test -p 2 ./internal/voice/ -run TestChildProcRefusal | - | PASS | Given a child that never handshakes; When EnsureRunning; Then ErrChildUnavailable within StartTimeout |
| T6 | RSS debug logs at spawn/first-call/idle-stop | (same) | go test -p 2 ./internal/voice/ -run TestChildProcRSSLog | - | PASS | Given a spawn; When logged; Then rss_kb field present |
| T7 | Format/vet/race | - | gofmt -l internal/voice; go vet ./internal/voice/; go test -p 2 -race ./internal/voice/ | - | clean | - |

## Self-Verification Checklist

- [ ] No copy-pasted kill logic (wrap or extracted seam; grep for SIGKILL duplication)
- [ ] No secrets in child env/argv (token via stdin only)
- [ ] Idle timer cannot fire while a call is live (race-tested)
- [ ] Orphan sweep compatibility: pid-file + ownership token rules honored
- [ ] ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list; wrap-vs-extract decision + RSS figures observed

## Review Checklist (For Review Agent)

- [ ] Lifecycle invariants from internal/daemon/AGENTS.md each pinned by a test or cited as covered by the reused seam
- [ ] Test helper child is hermetic (no network, no real jargo/ONNX)
- [ ] Stop() during starting state does not deadlock
- [ ] No os.Getwd fallback for binary path (explicit config/lookup)

Output: APPROVED or specific gaps.

## Notes

Binary lookup: prefer the daemon-adjacent path (same dir as the running
meept-daemon binary) then $MEEPT_HOME/bin; never PATH search for a
privileged child. The e2e harness's findDaemonBinary prefers ~/go/bin -
document that the voice child must be installed alongside (make install
adds cmd/meept-voice in leaf 16's Makefile task).
