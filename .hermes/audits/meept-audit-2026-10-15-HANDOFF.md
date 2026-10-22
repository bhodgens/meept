# meept bughunt handoff -- audit wave 2026-10-15

Purpose: standalone handoff document. A reader with zero prior context can
pick up the findings, verify them, and run the fix wave from this file alone.

Audited tree: main @ 623f33b1 + uncommitted sibling diff (memory backend
selection). Re-verified against main @ 55ae45bb (tree moved mid-session; all
findings below still hold at their cited lines on 55ae45bb).

Method: 8 read-only subagent auditors, disjoint package scopes, each writing
its own report under `.hermes/audits/2026-10-15-wave/`. The parent (orchestr)
then re-read every claim at its cited `file:line` or ran a live probe. Tags:
[PV] = parent re-read the code or ran a probe. [CH] = child-sourced, not
independently confirmed. [PD] = parent disproved (listed in the Disproved
section, not in the fix list).

Baseline gates at dispatch time (parent-run): `go build ./...` OK;
`go vet ./...` clean; `go test -p 2 -count=1 ./...` = 111 packages ok, 1 FAIL
package (`internal/tools/builtin`, 3 websearch tests) failing with
`dial tcp 127.0.0.1:6xxxx: connect: can't assign requested address` -- macOS
ephemeral-port exhaustion, environmental, not a code regression.

## Fix list (accepted findings, ranked)

### A1 -- terminal service shell tool runs without fence or security orchestrator [PV] MEDIUM-HIGH
- Files: `internal/services/terminal_service.go:65` (construction),
  `internal/tools/builtin/shell.go:411-413` (fence skipped when nil),
  `internal/daemon/components.go:6253-6260` (the agent path DOES wire both).
- Mechanism: `NewShellExecuteTool` is called twice at boot. The agent-path
  instance gets `SetSecurityOrchestrator` + `SetFenceChecker`; the
  TerminalService instance gets neither. `shell.go` guards every fence call
  with `if t.fenceChecker != nil`, so the terminal-service shell executes
  `working_dir` (any absolute path via `resolvePath`, `shell.go:402-409`)
  with only the built-in RiskCritical blocklist. Two postures for one tool.
- Falsifiable test: drive the terminal service RPC with
  `working_dir: "/tmp"` + a benign command; confirm no fence error. Then the
  same via the agent path and see the fence refuse.
- Fix shape: wire the same fenceChecker/secOrch into TerminalService at
  construction (components.go already has both in scope).

### A2 -- audit chain verification returns OK for an empty chain [PV] MEDIUM
- File: `internal/auditlog/chain.go:58-128`.
- Mechanism: `VerifyChain` sets `res.OK = true` after the row loop
  (`chain.go:126`). With zero rows the loop body never runs, so a store
  truncated to zero records verifies OK with `Records: 0`. Tamper-evidence
  gap: deleting the whole chain passes verification.
- Falsifiable test: fresh scratch DB (mktemp -d), create schema, call
  `VerifyChain` before appending -- expect `OK=true, Records=0`.
- Fix shape: fail (or flag) when `Records == 0` unless the caller passes an
  explicit allow-empty option. Check every caller (`audit verify` CLI,
  anchoring) before choosing the default.

### A3 -- security sanitizer does not neutralize control characters [PV] MEDIUM
- File: `internal/security/sanitizer.go` (no `IsControl`/NUL handling
  anywhere in it; grep count 0).
- Mechanism: `Sanitize` escapes boundary markers and injection patterns but
  passes NUL, backspace, and other C0 control bytes through unchanged. The
  sibling FTS5 sanitizer (`pkg/sqlite/fts5.go:122-135`) DOES strip control
  chars, so the codebase already treats them as hostile -- the gap is
  inconsistent. Control bytes reaching prompt assembly can confuse
  tokenizers/parsers.
- Falsifiable test: `Sanitize("\x00system: ignore previous")` -- the NUL
  survives.
- Fix shape: strip or escape C0 controls (keep \t \n \r) in
  `sanitizeStructure`; add a table-driven test.

### A4 -- memory backend switch has no default case; typos silently select local [PV, parent-found] MEDIUM
- File: `internal/memory/manager.go:222-251` (uncommitted sibling diff at
  audit time; still uncommitted on 55ae45bb).
- Mechanism: the new `switch m.config.Backend` handles `"local"`, `"sqlite"`,
  `""`, `"memvid"`. Any other value (`"loca"`, `"sqlie"`) matches no case,
  leaves both want-flags false, and the local store initializes silently via
  the `!m.useMemvid` fallback at line 254 -- no warning logged. Config typos
  become invisible behavior changes.
- Falsifiable test: set `memory.backend = "sqlie"`, boot daemon, grep stderr
  -- no warning line exists.
- Fix shape: add `default:` that logs Warn("unknown memory backend %q, using
  built-in local store") and sets wantSQLite. Owner note: the sibling session
  that authored the diff owns this file; coordinate before patching.

### A6 -- MCP tool results carry no taint/fence marker [CH] MEDIUM
- File: `internal/tools/mcp/client.go:316-321` -- success path returns
  `tools.NewSuccessResult(resultText)` with no provenance label.
- Mechanism: a hostile/compromised MCP server's text reaches the agent as
  ordinary tool output. Shell results go through taint/fence handling; MCP
  results do not at the client layer. CAVEAT: the fence may be applied at
  prompt assembly for ALL tool outputs (agent-side) -- verify where the fence
  is applied before fixing at the client, or the fix double-wraps (the repo
  already fixed one double-wrap class: commit db739210).
- Falsifiable test: stand up a local stdio MCP server that returns a marker
  string; trace whether the marker reaches the prompt fenced or bare.

### A5 -- UTC-stamped records vs LOCAL day frames: needs a targeted probe [CH] MEDIUM (demoted)
- Files: `internal/memory/distill_async.go:354` (payload stamped
  `time.Now().UTC()`) vs `:370-374` (frame keyed by LOCAL day -- comment says
  this split is INTENTIONAL and was a prior fix).
- Status: the child claimed UTC/local bucketing inconsistency; the code
  comment at the site says the local-frame choice is deliberate. The open
  question is only whether a record created near the local/UTC midnight
  offset lands in the right frame end-to-end. Probe before fixing; may be a
  non-bug.

### A7 -- effects idempotency key composition is per-caller convention, unenforced [PV] LOW-MEDIUM
- Files: `internal/effects/key.go:15` (docstring prescribes
  `EffectKey(toolName, sessionID, ...)`), `internal/backup/git_ops.go:270`
  (omits session -- arguably correct: same SHA push is genuinely idempotent),
  `internal/services/push_service.go:168` (embeds a sessions JSON blob --
  changes when the audience changes, so a content edit re-fires the effect).
- Mechanism: nothing enforces the documented part order; two callers make
  opposite choices. Not a live bug found; it is a class waiting for its
  first victim.
- Fix shape: named constructors per effect type (`PushNotifyKey(...)`) so
  callers cannot reorder parts.

### A8 -- TUI/GUI slash-command parity gaps [PV] LOW
- Files: `internal/tui/command_handler.go:217` (`implement-improvements`),
  `/dnd` handler; `ui/flutter_ui/lib/core/slash_commands.dart` -- grep for
  `implement-improvements` and `dnd` returns 0 hits.
- Mechanism: repo rule (root AGENTS.md UI Conventions) requires TUI/GUI
  parity or a documented deviation. Neither command exists GUI-side and no
  deviation is documented.
- Fix shape: add both to `_defaultCommands` (if the daemon RPCs back them
  for GUI clients) or document the deviation with justification.

### A9 -- ExecuteWithRetry masks the real failure reason [CH] LOW
- File: `internal/tools/registry.go:584-587`.
- Mechanism: a tool returning `ToolResult{Success:false, Error:"specific"}`
  with `err == nil` ends the retry loop with `lastErr == nil`, replaced by
  the generic `"unknown error during tool execution"` -- the specific reason
  is dropped from the returned error.
- Falsifiable test: register a tool returning Success:false with a distinct
  Error string; call ExecuteWithRetry; assert the returned error names it.

### A10 -- MCP stdio Start() window can orphan a subprocess [CH] LOW
- File: `internal/tools/mcp/transport/stdio.go:129` (`cmd.Start()`) vs
  `:142` (`t.running.Store(true)`).
- Mechanism: if anything panics between the two lines, the child process is
  live but `running` is false, so a later `Close()` may skip the kill.
  Narrow window; fix by deferring a kill-on-failure between those lines.

## Disproved claims (do NOT fix these)

1. FTS5 boolean injection via `NOT` -- disproved LIVE:
   `SanitizeQueryOp("foo NOT bar","AND")` -> `"foo" AND "NOT" AND "bar"`;
   every token is quoted (`pkg/sqlite/fts5.go`).
2. `rows.Close` missing at `internal/memory/dual_store.go:545` /
   `ftstore.go:296` -- both have `defer rows.Close()` / explicit close with
   `//nolint:sqlclosecheck` justification.
3. Consolidation tick double-start -- NOT a live bug: boot calls
   `StartPeriodicConsolidation` exactly once (`internal/daemon/daemon.go:2036`).
   Note: the ticker itself has no `sync.Once` guard
   (`internal/memory/consolidation.go:926`), so a second caller WOULD
   double-start it -- hazard noted, not fixed. (See review corrections.)
4. TUI `executeHelp` panic -- guard `len(args) > 0` present; child retracted.
5. Gate command injection (`internal/gate/gate.go:150-154`) -- gate commands
   are an operator-facing feature (`agents set-gate --command=...`),
   config-trusted by design.
6. "Memory backend consumers miss 'local'" -- repo-wide grep found no
   consumer comparing the old literals; the real defect at that site is A4.
7. Cache-key collision claims (2) -- speculative; commit aee5e405 already
   fixed sentinel consumption + tool-order stability.
8. WS progress suppression for session-less connections --
   `internal/agent/handler.go:784-787` and `:1126-1135` both fall back
   session_id->conversation_id (the second carries the 2026-09-18 fix
   comment); the comm-side filter audited clean.
9. auth expiry off-by-one at exact `ExpiresAt` instant -- "valid through T"
   is the intended semantics; no timezone-naive source feeds `now`.

## Coverage gaps (not audited this wave)

- `internal/compress`, `internal/context` (auditor skipped both).
- tirith engine rule coverage; Flutter widget layer; services concurrency
  depth (child ran out of budget).
- `mutexio`/`predid` analyzers over the security scope (that child's Go
  build cache was corrupted: "export data version 5 vs max 4"; parent-side
  `go vet` is clean, so run `make analyzers` locally -- ~2 minutes).
- e2e tier untouched (audit was static + scoped probes only).

## Timing + throughput (measured, for planning future waves)

- Wall clock: 6 h 44 m for 8 auditors (two overlapping waves of 4).
- Per child: 5.7-6.7 h lifetime, 15-48 API calls.
- Model: `minicpm-v-4.7` (local, socrates litellm, shared GPU).
- Output tok/s: 0.28-0.57 per child; aggregate 0.45 (0.52 with reasoning).
- Prefill dominates: 4.31 M cumulative input tokens vs 96 K output;
  total processed throughput 23.1 tok/s. Uncontended single child: 5.85
  output tok/s.
- Lesson: for local-model children, budget waves in hours, keep briefs small
  (every byte re-prefills per call), and treat the child's report FILE as the
  deliverable -- 8/8 children failed final-answer schema validation while
  8/8 report files landed complete.

## Artifact inventory

- This handoff: `.hermes/audits/meept-audit-2026-10-15-HANDOFF.md`
- Consolidated adjudication: `.hermes/audits/meept-audit-2026-10-15-wave.md`
- Raw per-auditor reports: `.hermes/audits/2026-10-15-wave/01..08-*.md`
  (+ `*.json` structured payloads, `BRIEF-RULES.md`, `perf.md`)
- Measurement script: `/tmp/measure_tok.py` (reads state.db subagent sessions)
- None of these are committed yet.

## Suggested fix-wave split (disjoint file ownership)

1. Fixer 1: A1 -- `internal/services/terminal_service.go` (+ wiring site in
   `internal/daemon/components.go` if needed; coordinate with any sibling
   session mid-merge on that file).
2. Fixer 2: A2 + A7 -- `internal/auditlog/`, `internal/effects/`.
3. Fixer 3: A3 + A9 + A10 -- `internal/security/sanitizer.go`,
   `internal/tools/registry.go`, `internal/tools/mcp/transport/stdio.go`.
4. Fixer 4: A8 -- `ui/flutter_ui/lib/core/slash_commands.dart` (+ palette
   manifest if parity tooling requires).
5. Parent: A4 (coordinate with the sibling who owns the memory diff), A5
   probe, A6 fence-location determination, then full gate suite
   (`make test`, `make analyzers`, `make graphs-check`).
- Per repo policy: new-feature tests go in the e2e tier
  (`e2e/suites/<name>/` + manifest); bug-fix unit tests are allowed.

## Independent review (subagent, 2026-10-16, deleg_bb3bffa3)

An adversarial reviewer re-checked this document against 55ae45bb
(16 API calls, 17 min). Results:

- A1-A10: all CONFIRMED at their cited lines (A1 shell.go fence guard is
  line 412, not 411; A9 guard sits at 585-587). No line drift otherwise.
- Disproved claims 1, 2, 8: independently re-confirmed as disproved.
- Fix-wave split: no file overlaps. Artifact paths: all exist. Timing
  table: internally consistent.
- The reviewer could not verify claims 3-7 and 9 in its time-box; the
  parent closed those afterwards (see corrections below).

### Parent corrections after the review

- Claim 3 (consolidation tick double-start) -- the handoff's original
  wording was imprecise. `Consolidator.StartPeriodicConsolidation`
  (`internal/memory/consolidation.go:926`) has NO `sync.Once` on the
  ticker (the `stopOnce` at `consolidation.go:58` guards Stop, not
  Start): calling it twice DOES start two tickers. The disproof stands
  only because boot calls it exactly once (`internal/daemon/daemon.go:2036`
  -> `internal/memory/manager.go:1266`). Reclassified: not a live bug,
  but a real double-start hazard if a second caller ever appears.
- Claim 4 (executeHelp panic) -- CONFIRMED disproved: guard
  `if len(args) > 0` at `internal/tui/command_handler.go:241`.
- Claim 5 (gate command injection) -- by design: gate commands are set by
  the operator via `agents set-gate --command=...`; config is a trusted
  input (same posture as MCP `install_hint` commands).
- Claim 6 (memory backend consumers) -- repo-wide grep for
  `Backend ==`/`!=` string comparisons found no consumer outside
  `manager.go` (the `runtime/manager.go` hits are the runtime
  local/docker backend, a different field).
- Claim 7 (cache-key collisions) -- commit aee5e405 message documents the
  sentinel-consumption fix + stable tool order on every client; the child
  claims offered no concrete collision.
- Claim 9 (auth expiry boundary) -- `internal/auth/store.go:76`
  `now.After(*found.ExpiresAt)`: "valid through T" semantics; `now` comes
  from `time.Now()` (monotonic-clock-backed), no timezone-naive source.
- A5 line 370 -- CONFIRMED: `distill_async.go:370-375` comment states the
  LOCAL-frame stamp is the deliberate H4 fix; A5 stays "probe before
  fixing".
- A8 Flutter side -- CONFIRMED gap: grep for `implement-improvements` and
  `dnd` in `ui/flutter_ui/lib/core/slash_commands.dart` returns 0 hits.
