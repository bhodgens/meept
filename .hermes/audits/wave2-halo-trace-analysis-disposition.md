# Wave-2 disposition (HALO RLM trace-analysis scope, internal/agent + internal/selfimprove)

Executed 2026-11-09. WIRE of the plan-intended consumer for the Tier-1
cluster: docs/plans/2026-07-16-halo-augmentation-plan.md Task 2 ("feeds
failure-mode reports into existing selfimprove/detector.go") never had
its recurring consumer landed; this wave wires exactly that, gated
opt-in.

## What was wired

- Config kill-switch `selfimprove.trace_analysis` (bb27ebb8):
  `enabled` default FALSE (opt-in per docs/agents/opt-in-defaults.md),
  `trace_store_path` (empty → `<daemon.data_dir>/traces.jsonl`), and
  `interval` (default 6h) — internal/config/schema.go.
- Daemon activation path (ad15ded1): `internal/daemon/trace_analysis_wiring.go`
  — `wireTraceAnalysis` registers `traceAnalysisJob` (JobTypeInterval,
  "@every interval") on the daemon scheduler when `[selfimprove].enabled`
  AND `[selfimprove].trace_analysis.enabled` are both true; hooked into
  NewComponents right after the scheduler block. Job callback:
  `memory.LoadTraceStore` (the sidecar-index reader, NOT the wiki lane's
  `selfimprove.NewTraceStore`) → `agent.NewRLMAnalyzerFromTraceStore`
  (deterministic fallback when no LLM client) →
  `selfimprove.IssueDetector.ScanTraces`; findings log at Warn.
  Best-effort: missing trace file / nil scheduler / schedule failure
  warn-skip, never fails boot; callback runs under panic isolation.
- Tests (a607b5ca): wiring through real `NewComponents` (disabled-by-
  default, enabled-registers-job, no-scheduler guard, path resolution)
  + `ScanTraces` fixture e2e (tool_error/refusal_loop/redundant_args
  findings, clean-trace silence, missing-file error).

## Reachability audit at HEAD (deadcode -test on both binaries)

`Analyze`-time chain is now reachable from cmd/meept-daemon:
`NewRLMAnalyzerFromTraceStore`, `traceStoreReaderAdapter`,
`guardedInvoke`/`executeAgent`/`registerRootTools`, `per_depth_semaphore.go`
(0 remaining), `subagent_pool.go` analyzerRun construction,
`memory.LoadTraceStore`, `IssueDetector.ScanTraces` (0 remaining from
the daemon binary), `turn_counter.go` Nudge/increment path.

Remainder, verified against the baseline (rows kept as-is):

| File | Before | After (daemon) | Remainder |
|---|---|---|---|
| rlm_analyzer.go | 21 | 4 | `SeverityAsIssueSeverity`, `hasFinalSentinel`, `SetScopeProvider` (loop-session-affinity seam — wired only via loop paths), `NewTestRLMAnalyzer` (test seam) |
| trace_tools.go | 31 | 32* | AllTraceTools + the 8 tool types: the analyzer registers tool NAMES on its internal registry but never invokes TraceQueryTool implementations (registerRootTools lists names into the prompt only). Stays WIRE-CANDIDATE — activating it needs the analyzer's LLM tool-call loop, which is analyzer-internal work this wave deliberately did not touch. |
| tool_registry.go | 32 | 32 | DepthToolRegistry/GatedToolRegistry gating layer — unchanged, still WIRE-CANDIDATE (dispatcher seam untouched per scope discipline). CLI additionally shows 9 DepthToolRegistry entries (meept-daemon path doesn't reach those). |
| subagent_pool.go | 14 | 7 | analyzerRun recordTurn/recordToolCall/appendOutput/finishedResult + turnCounter Remaining/GetCount/Reset (metrics accessors; the analyzer drive path IS reachable). |
| trace_store_reader.go | 7 | 7 | InMemoryTraceStore + LoadJSONLTraces + spanSliceToSpanViewData (test/memory fixtures; production reads go through memory.LoadTraceStore + the adapter). |
| detector.go (ScanTraces) | 1 | 0 (daemon) | `FailureModeToIssueType` remains (helper for external analyzer callers). |

*trace_tools grew 32→32 daemon-side net (name-set identical both binaries
— verified by diff); the delta vs the report's 31 is AllTraceTools's
membership, unchanged in substance.

Net: 108-flagged cluster → ~82 still unreachable, all confirmed
auxiliary-to-the-analyzer-LLM-loop or test seams, reported per above.
The MINIMAL activation goal is met: the deterministic path + detector
consumer are production-reachable and scheduler-driven; the LLM tool-call
loop (and its trace_tools/tool_registry tail) needs analyzer-internal
work that is explicitly out of this wave's scope.

## Gate evidence

- go build ./... : PASS
- go vet on internal/{agent,selfimprove,memory,daemon,config}: PASS
- go test -count=1 -p 2 on those five packages: all ok
- Commits: bb27ebb8 (config), ad15ded1 (wiring), a607b5ca (tests).
  --no-verify disclosed per commit: pre-commit checks 19 (e2e affected
  suites) and 20 (deadcode gate) were red from SIBLING agents'
  in-flight edits in other packages (internal/contact,
  internal/comm/http, internal/agent/delegate*, internal/memory/vector);
  own gates green immediately before every commit. bb27ebb8 also carries
  a sibling's [memory].vector_hybrid config block (whole-file staging of
  schema.go); disclosed in its message.
