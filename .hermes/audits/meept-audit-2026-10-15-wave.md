# meept bughunt -- wave 2026-10-15 (subagent audit + parent adjudication)

Audited tree: main @ 623f33b1 + uncommitted sibling diff (memory backend
selection). 8 read-only auditors, disjoint scopes, model `minicpm-v-4.7`
(local litellm on socrates). Per-auditor reports: `.hermes/audits/2026-10-15-wave/0N-*.md`.

Provenance tags: [PV] parent re-read the cited code or ran a live probe.
[CH] child-sourced, not independently confirmed. [PD] parent disproved.

## Baseline gates (parent, pre-dispatch)

- `go build ./...` -- OK
- `go vet ./...` -- clean
- `go test -p 2 -count=1 ./...` -- 111 packages ok; 1 FAIL package
  (`internal/tools/builtin`, 3 websearch tests) with
  `dial tcp 127.0.0.1:6xxxx: connect: can't assign requested address` --
  macOS ephemeral-port exhaustion, environmental, not a code regression.

## Accepted findings (severity-ranked)

### A1 MEDIUM-HIGH -- terminal service shell bypasses fence + orchestrator [PV]
- `internal/services/terminal_service.go:65` builds
  `builtin.NewShellExecuteTool(workingDir, ...)` and never calls
  `SetFenceChecker` or `SetSecurityOrchestrator`. The agent path wires both
  (`internal/daemon/components.go:6253-6260`), so the same shell tool has two
  postures: fenced for the agent, unfenced for the terminal service.
- `internal/tools/builtin/shell.go:411` skips the fence when
  `fenceChecker == nil`, and `working_dir` resolves to any absolute path
  (`shell.go:402-409`).
- Test: drive the terminal service with `working_dir: "/etc"` and a
  non-critical command; confirm no fence error is returned.

### A2 MEDIUM -- audit chain verification passes on an empty chain [PV]
- `internal/auditlog/chain.go:58-128`: `VerifyChain` initializes
  `res.OK` true-by-assignment at the end (`res.OK = true; return res, nil`);
  with zero rows the loop never runs, so a truncated-to-zero audit log
  verifies OK with `Records: 0`. Tamper-evidence gap for the compliance chain.
- Test: open a fresh store, call `VerifyChain` before appending anything --
  expect `OK=true, Records=0`.
- Fix shape: refuse OK when `Records == 0` unless an explicit
  allow-empty flag is passed by the caller.

### A3 MEDIUM -- security sanitizer does not neutralize control characters [PV]
- `internal/security/sanitizer.go` has no control-character filtering
  (grep for `IsControl`/`x00` returns nothing). The FTS5 sanitizer
  (`pkg/sqlite/fts5.go:122-135`) does strip them, so the gap is inconsistent
  across the two sanitizers. NUL/backspace can reach prompt assembly.

### A4 MEDIUM -- memory backend switch has no default case [PV, parent-found]
- `internal/memory/manager.go:222-251`: a typo value (`"loca"`, `"sqlie"`)
  matches no case, leaves both flags false, and silently initializes the
  local store via the `!m.useMemvid` fallback at line 254 with no warning.
  Config typos become invisible.
- Test: set `memory.backend = "sqlie"`, boot, grep the log -- no warning line.

### A5 MEDIUM -- UTC vs local day bucketing inconsistency [CH]
- `internal/memory/distill_async.go:370,527` name day frames by LOCAL date
  while `handler.go:271,295` and `dual_store.go` stamp UTC. Records created
  near the UTC/local midnight offset can land in a different day frame than
  their timestamp implies. Needs a targeted probe before fixing.

### A6 MEDIUM -- MCP tool results carry no taint label [CH]
- `internal/tools/mcp/client.go:316-321` returns
  `NewSuccessResult(resultText)` with no taint/fence marker, unlike shell
  results. A hostile MCP server's text reaches the agent as ordinary tool
  output. Verify against the agent-side fence (the fence may be applied at
  prompt assembly instead -- check before fixing at the client).

### A7 LOW-MEDIUM -- effects key composition is per-caller convention [PV]
- `internal/effects/key.go:15` documents
  `EffectKey(toolName, sessionID, ...)` but nothing enforces it:
  `internal/backup/git_ops.go:270` omits session (arguably correct -- pushing
  the same SHA twice is genuinely idempotent), `internal/services/push_service.go:168`
  embeds a sessions JSON blob. Same class, opposite choices, no lint.

### A8 LOW -- TUI/GUI slash-command parity gaps [PV]
- TUI has `implement-improvements` (`internal/tui/command_handler.go:217`)
  and `/dnd`; `ui/flutter_ui/lib/core/slash_commands.dart` has neither
  (grep: zero hits). Repo rule: TUI and GUI keep parity or document the
  deviation.

### A9 LOW -- ExecuteWithRetry masks the failure reason [CH]
- `internal/tools/registry.go:586`: a `ToolResult{Success:false, Error:"..."}`
  with `err == nil` becomes `"unknown error during tool execution"`.

### A10 LOW -- MCP stdio Start() window can orphan a subprocess [CH]
- `internal/tools/mcp/transport/stdio.go:129-142`: `cmd.Start()` succeeds,
  then a panic before `t.running.Store(true)` leaves the child process
  running with no owner. Narrow window.

## Disproved / rejected child claims (published per protocol)

- [PD] "FTS5 boolean injection via NOT" (03-F2): live probe against
  `pkg/sqlite` shows `SanitizeQueryOp("foo NOT bar","AND")` ->
  `"foo" AND "NOT" AND "bar"` -- every token is quoted; `NOT` is a term, not
  an operator. No injection.
- [PD] "rows.Close missing at dual_store.go:545 / ftstore.go:296" (03-F4):
  `dual_store.go:549` has `defer rows.Close()` immediately after the error
  check; `ftstore.go` closes before the update loop with an explicit
  `//nolint:sqlclosecheck` justification.
- [PD] "consolidation tick can double-start" (03-F5): the child's own text
  confirms the consolidator guards the ticker with `sync.Once`.
- [PD] "CRITICAL panic in executeHelp" (07-F1): the child retracted it in
  its own report; the `len(args) > 0` guard is present.
- [PD] "gate command injection" (06-F3): gate commands are a documented
  operator-facing feature (`agents set-gate --command=...`), config-trusted
  by design.
- [PD] "memory backend consumers miss 'local'" (03-F1 mechanism): repo-wide
  grep for `Backend ==`/`!=` string comparisons found no consumer outside
  `manager.go` (the `runtime/manager.go` hits are the runtime local/docker
  backend, a different field). The real defect at that site is the missing
  default case (A4).
- [PD-ish] cache-key findings (02-F1/F3): speculative; commit aee5e405
  already fixed sentinel consumption and tool-order stability. No concrete
  collision shown. Dropped to "not a finding".
- [PD] "WS progress suppression for session-less connections" (01-F2,
  MEDIUM): parent read both cited sites. `handler.go:784-787` falls back
  `wsSessionID = conversationID` when `req.SessionID` is empty, and
  `handler.go:1126-1135` sets `response.SessionID = persistID` with the same
  fallback -- the latter carries a comment naming the 2026-09-18 audit fix
  that introduced exactly this routing. The child never traced the comm-side
  filter it said needed tracing; the daemon-comm auditor covered that filter
  and found it clean. Not a finding.
- [CH, kept as hygiene] park-store id validation (01-F3) and the
  loader.go:35 os.Getwd() comment (01-F1): both LOW, no defect mechanism;
  park store stores both ids correctly per its schema. Not accepted as bugs.

## Coverage (checked and clean, per auditor)

- agent core: turn lifecycle, errored-step review, machine-output guard,
  completion dedup guard, ToolActionMap completeness tests -- clean [CH]
- llm: quota error classification (errors.Is everywhere), empty-completion
  handling in openai/anthropic/codex clients -- clean [CH]
- tui: tableutil.Size/SetRows compliance across all models -- clean [CH]
- queue: claim_token claim semantics -- clean [CH]
- daemon: pairing single-use zeroization, chat.response WS filter -- clean [CH]
- Not covered: internal/compress, internal/context (auditor 2 skipped),
  tirith engine rules, deeper services concurrency, Flutter widget layer.

## Timing + throughput (measured)

- Wall clock: dispatch 20:37:37Z -> last child closed 03:22:15Z = **6 h 44 m**
  (wave 1: 6 h 40 m; wave 2: 6 h 39 m, overlapping).
- Child lifetimes: 20,412-23,980 s each (5.7-6.7 h); 15-48 API calls each.
- Subagent model: `minicpm-v-4.7` (local, shared GPU via litellm on socrates).
- Output tok/s per child: 0.28-0.57; aggregate **0.45 tok/s**
  (output+reasoning: 0.52 tok/s).
- Total processed tokens (prefill+decode) per child stream: **23.3 tok/s**
  (4.24 M input + 96 K output+reasoning over 185,947 child-seconds).
  Prefill dominates: children re-sent 237 K-900 K input tokens each.
- Single-child decode rate (smoke test, no contention): 5.85 output tok/s.
  The 0.45 figure is 4 concurrent children + parent sharing one GPU, with
  prefill eating most of the time.
- 1 of 8 children "failed" on schema validation only -- its report file and
  findings landed fine; 4 children were stopped early by the parent after
  their report files existed (they kept drifting past scope).
