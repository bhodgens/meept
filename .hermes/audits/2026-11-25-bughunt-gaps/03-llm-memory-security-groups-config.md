# Auditor 3: llm / memory / groups / security / config / queue-cluster / codemode / skills / intake

Scope: non-test Go files changed in 0a446a21..b3c03f80 under internal/llm, internal/memory, internal/groups,
internal/security, pkg/security, internal/codemode, internal/skills, internal/intake, internal/jevgate,
internal/toolprov, internal/queue, internal/cluster, internal/session, internal/config, cmd/meept.
Files read: 38 of 90 (all priority files; see Not audited).

NOTE ON HEAD: the brief named HEAD e43a7cf3; the repo HEAD at audit time is 997d6d02 (one commit ahead,
`fix(scheduler): classify job Env in the execution-time shell guard`). All line numbers are `git show HEAD:`
at 997d6d02. internal/llm/client.go was read via `git show HEAD:` (working tree has an uncommitted revert).
No repo files were modified. One throwaway probe test was written and removed under internal/config/
(it was removed; `git status` shows only the pre-existing dirty files: config.go, llm/AGENTS.md, client.go,
models.go, providers.go).

Rule docs read: AGENTS.md, internal/llm/AGENTS.md, internal/session/AGENTS.md, internal/daemon/AGENTS.md,
docs/agents/invariants.md, docs/agents/opt-in-defaults.md, docs/agents/projects-and-workdirs.md,
docs/agents/configuration.md.

## Findings

### A3-1 — HIGH — tierpolicy "compress" enqueues jobs the distill worker always rejects; the hard band then evicts the same content
**File:** internal/memory/tierpolicy/execute.go:139-160 (producer); internal/memory/distill_async.go:246-248 (consumer)
```go
// execute.go
payload := memory.L1ExtractPayload{
    SessionID: tierJobPrefix + ":" + sessionFromReason(act.Reason),   // "tierpolicy:episodic"
}
raw, err := json.Marshal(payload)
...
job.Payload = raw   // comment at :151 claims the "full target selection" rides in the job — it does not

// distill_async.go handleL1Extract
if p.L0Path == "" {
    return nil, errors.New("distill: l1 payload missing l0_path")
}
```
**Trigger:** `memory.tiers.enabled = true` and the episodic tier crosses the compress band (80% of ceiling).
**Effect:** Every `compress` action enqueues a `memory.l1_extract` job with an empty `L0Path` and NO target
IDs in the payload. `DistillWorker.Process` routes it to `handleL1Extract`, which fails unconditionally on
`l0_path == ""`. The job goes to retry/backoff/dead-letter. Meanwhile `Execute` has already counted
`CompressionEnqueued += n` and `ActionsApplied++`, and in the hard band (engine.go:399-420) the SAME oldest-
first candidate pool is then handed to `planEvict` and **deleted** from `episodic_memories` in the same pass.
The R-B4 contract ("compress precedes evict... compression is recoverable; eviction of un-distilled content
is the last resort", engine.go:11-14) is therefore structurally void: nothing is ever compressed before it is
destroyed. The `opt-in-defaults.md` rollback claim "evicted rows live in episodic recall storage, never
silently dropped; working-tier flushes are lossless" is also false for this path — the evicted rows ARE the
episodic store. `demote` (A3-2 note below) copies claims INTO the episodic store, where they become plain
eviction candidates once older than `retention_days`.
**Caller evidence:** engine.go:401 `planCompress` → execute.go:92 `enqueueCompress`; daemon routes L1 jobs to
the distill worker at internal/daemon/l0_wiring.go:252 (`case memory.MemoryJobL1Extract ... return
r.distill.Process`). No consumer of `tierJobPrefix`/`"tierpolicy:"` session IDs exists outside the engine
(`git grep -n -E '"tierpolicy"|tierpolicy:' HEAD -- '*.go'` → only components.go:6176/6208, which are the
engine Start/Stop handler).
**Confidence:** confirmed by reading the full path.

### A3-2 — MEDIUM — tierpolicy demotion's "lossless" episodic copy is immediately eligible for the eviction pool
**File:** internal/daemon/tiers_engine.go:205-223 (`CopyToEpisodic`), :107-116 (`managerCandidates.Candidates`)
```go
func (d *managerDemotion) CopyToEpisodic(ctx context.Context, claim memory.Memory) (string, error) {
    ...
    return ep.Store(ctx, claim.Content, "claim", meta)   // plain episodic row, no demoted/protected marker
}
// Candidates:
return ep.GetOldMemories(ctx, time.Now().UTC().Add(-m.retention), limit)   // oldest-first, ALL episodic rows
```
**Trigger:** `memory.tiers.enabled = true`; a claim is demoted (stale >30d or low usefulness). The copy is a
normal `episodic_memories` row carrying only `demoted_from`/`source_type` metadata. After `retention_days`
(default 30) it enters `GetOldMemories` and is ranked/evicted like any other row; with A3-1 nothing preserves it.
**Effect:** The "flag-not-delete, lossless" demotion guarantee (demote.go:4-9, opt-in-defaults.md) holds only for
`retention_days`; after that the only copy of the claim content is destroyed by the same engine that claimed to
preserve it. Not HIGH on its own because the content survives 30 days and the claim row itself is retained with
the demoted flag (visibility axis only).
**Caller evidence:** engine.go:366 `PlanDemotions` → execute.go:108 `executeDemote` → demote.go:309
`CopyToEpisodic`; evict pool via engine.go:482 `Candidates(ctx, st.Tier, evictCandidateLimit)` → tiers_engine.go:115.
**Confidence:** confirmed by reading the full path.

### A3-3 — MEDIUM — GroupFenceStage is a purely lexical prefix check (no symlink/`..` resolution); the executor feeds it raw paths
**File:** internal/agent/fence_stage.go:135-145 (`pathInRoot`), :121-131 (`CheckCommand`)
```go
func pathInRoot(path, root string) bool {
    abs, err := filepath.Abs(path)        // Clean only — NO EvalSymlinks
    ...
    rootAbs, err := filepath.Abs(root)
    ...
    return strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) || abs == rootAbs
}
func (g *GroupFenceStage) CheckCommand(command, workDir string) error {
    if workDir == "" || workDir == "." || pathInRoot(workDir, g.roots[0]) { return nil }   // "." always allowed
```
**Trigger:** A managed agent (group policy with `fs_roots`) on the step-job lane, where the group fence is the
ONLY fence armed (components.go:11437-11446: "even when the daemon-wide session fence is off"). The agent calls
`file_write` with a path that is a symlink inside a group root pointing outside it (e.g. `proj/link ->
~/.ssh`), or runs `shell` with `workdir: "."`. The executor passes `args["path"]`/`args["workdir"]` to the
permission pipeline UNRESOLVED (internal/agent/executor.go:2163-2226 — no `EvalSymlinks` anywhere in
`checkPermission`).
**Effect:** `pathInRoot` resolves `filepath.Abs` only, so `proj/link/authorized_keys` lexically sits under the
root and passes; the write lands outside every `fs_root`. `CheckCommand` admits `workdir: "."` without consulting
any root, and does not tokenize the command at all (the command surface is "owned by the session fence", which
is off on this lane). This contradicts the leaf-04 F6 contract quoted in the same file ("writes outside the
group roots are denied at the central FenceStage"). Mitigation present: the tool-level
`intsecurity.FenceChecker` (fence.go:237-316) DOES resolve symlinks and `fence_enabled` defaults true
(config/meept.json5:820), so on a default install the write is still caught at the tool — but by the SESSION
fence rooted at the project, not by the group policy; a deployment that sets `fence_enabled: false` to rely on
group `fs_roots` alone has no barrier.
**Caller evidence:** components.go:11444 `agent.InjectGroupFence(loop.Security(), loop.GroupResolution())` →
`loop.SetSecurityChecker(gf)`; consumed by pkg/security/permissions.go:680-733 (fence stage).
**Confidence:** confirmed by reading the full path; exploitability depends on `fence_enabled` posture.

### A3-4 — MEDIUM — Groups backstop (C5) compares lexically; a symlink inside the project defeats the membership-config protection
**File:** pkg/security/groups_backstop.go:47-81 (`protected`), :50-54 comment
```go
// ...All comparisons are lexically cleaned; the caller
// (executor) already resolved symlinks for file paths, and shell targets
// are best-effort substring probes...
func (g GroupsBackstopConfig) protected(path string) bool {
    ...
    clean := filepath.Clean(path)          // no EvalSymlinks
    ...
    if within(home) && (clean == filepath.Join(home, "meept.json5") || clean == filepath.Join(home, "meept.toml")) {
```
**Trigger:** `security.groups.default_managed = true`; a non-admin agent calls `file_write` on
`<project>/cfg` where `cfg -> ~/.meept/meept.json5` (symlink created by any earlier allowed write, a cloned
repo, or `ln -s` via the shell tool, which only the backstop's substring probe guards). The executor hands the
raw path to `checkPermission` (executor.go:2226) — the comment's premise is false.
**Effect:** The C5 guarantee ("non-admin agents are denied file/shell access to ... the groups config section")
is bypassed for file tools; the agent rewrites its own group policy (e.g. adds itself to `admin_agents`). The
tool-level FenceChecker then evaluates the RESOLVED target: `~/.meept/meept.json5` is under `AllowedPaths:
["~/*"]` and the session jail passes because the lexical path is inside the worktree — `CheckPathJailed`
(fence.go:262-275) jail-checks the resolved `abs` against the resolved jail, and `~/.meept/...` is NOT inside
the project jail, so on a default install the tool fence does deny it. Defense-in-depth therefore holds with
`fence_enabled: true`, and the finding is MEDIUM, not HIGH. The file_read lane is not covered by the backstop
by design (C5 says "writes").
**Caller evidence:** permissions.go:744-760 (`pc.config.GroupsBackstop.check(agentID, details["path"])`).
**Confidence:** confirmed by reading; the stated invariant in the code comment is provably wrong.

### A3-5 — MEDIUM — Working-tier eviction queue is never flushed: `Store.Flush` and `EpisodicSinkAdapter` have zero production callers
**File:** internal/memory/working/store.go:322-332 (`enqueueEvicted`), :272 (`Flush`)
```go
func (s *Store) enqueueEvicted(t Turn) {
    s.queue = append(s.queue, t)
    for len(s.queue) > s.cfg.MaxItems {    // MaxItems = 200
        s.queue = s.queue[1:]
        s.dropped++
    }
}
```
**Trigger:** `agent.context_assembler.enabled = true`. `WireWorkingProducer` appends one ring turn per completed
turn (context_assembler_wiring.go:151-230); the 8192-token / 200-item ring overflows after roughly 200 turns;
evicted turns queue; nothing ever drains the queue; after 200 more evictions the oldest are dropped.
**Effect:** Violates the package contract "Evicted turns are queued in-memory for Flush, never silently
dropped (never-delete eviction...)" (store.go:10-13) and the opt-in-defaults rollback claim "working-tier
flushes are lossless". The loss is observable only through `Store.Dropped()`, which nothing reads. Not HIGH
because the same exchange is independently captured to L0 by memory-capture (the producer comment says so,
context_assembler_wiring.go:139-141), so the raw conversation survives elsewhere.
**Caller evidence:** `git grep -n -E '(WorkingStore|workingStore|wStore|wstore|Working)\.Flush\(' HEAD --
'*.go'` (non-test) → no matches; `git grep -n 'AppendTurns(' HEAD -- '*.go'` → only the interface, the
adapter, and store.go's own call; `NewEpisodicSink(` → no non-test callers.
**Confidence:** confirmed by reading the full path and greps.

### A3-6 — MEDIUM — `scope_filter.visible` treats an UNKNOWN row scope as global (recall fails OPEN on an unrecognized scope value)
**File:** internal/memory/scope_filter.go:134-138
```go
default:
    // Unknown scope value: treat as global (legacy/default backfill
    // vocabulary). Never hide rows whose scope the writer did not set.
    return true
```
**Trigger:** Any row whose `metadata.scope` is a non-empty value outside `{global, project, user}` — a writer
typo, a future tier name, a hand-edited record. `scopeFromMetadata` (types.go:92-97) returns the raw string.
**Effect:** That row is recallable by EVERY caller, including a user-scope caller under multiuser and a
project-scope caller bound to a different project. This contradicts the file's own Flag 3 rule ("an identity
resolution gap under multiuser must never widen recall", scope_filter.go:15-17) and the `scopeRowOwner`
fail-closed posture used for the known scopes. The stated intent ("rows whose scope the writer did not set")
is already served by the `scope == ""` branch at :97-99 — the `default` arm only fires for NON-empty unknown
values, which is exactly the case that should fail closed.
**Caller evidence:** `filterScopeResults` :156 → `Manager.Search`/`GetRelevantContext` option path and
`FilterResults` :193 (loop memvid recall, dispatcher re-scope per the comment at :185-192);
`VisibleScopeRow` (types.go:114) for the vector hybrid lanes.
**Confidence:** confirmed by reading; reachability requires an out-of-vocabulary scope stamp.

### A3-7 — LOW — Semantic cache VerifyFloor band picks its candidate by Go map iteration order (nondeterministic)
**File:** internal/llm/semantic_cache.go:221-227
```go
} else if bestResp == nil && verifyFloor > 0 && score >= verifyFloor && score < float32(s.cfg.Threshold) && len(e.vector) == len(vec) {
    // ... Ties resolve by the map range; the bound on entries keeps the ambiguity cost-free.
    inBandVec = e.vector
    bestKey = key
}
```
**Trigger:** `llm.cache.semantic.enabled = true` with `verify_floor > 0` and two or more entries scoring inside
`[VerifyFloor, Threshold)`. The loop keeps overwriting `inBandVec`/`bestKey` with whichever in-band entry the
random map order visits LAST — not the best-scoring one.
**Effect:** Which candidate reaches the stage-2 re-embed is nondeterministic across identical requests; a
lower-scoring neighbor can be verified (and served) while the higher-scoring one is never checked. Serving is
still gated on direct cosine ≥ Threshold (verifyBehindBand :270), so no wrong content is served — the defect is
lost hits and nondeterminism, which the task brief calls out ("the sort/compare must be deterministic"). Note
`inBandVec` is captured but never used after assignment (stage 2 re-embeds the query and reads `e.vector`).
**Caller evidence:** client.go:965/1264/2929 `semanticLookupBehindMiss` → `LookupBehindMiss`.
**Confidence:** confirmed by reading.

### A3-8 — LOW — `Engine.lookupBaseRule` fails OPEN on an unknown action (defaults to MEDIUM risk, "Permitted")
**File:** internal/security/engine.go:832-833
```go
e.logger.Warn("No rule found for action; defaulting to MEDIUM", "action", action, "tool", toolName)
return RiskMedium, false
```
**Trigger:** `Engine.CheckForAgent` with an action absent from `tool_rules`, `ComputerUseRule`,
`ACPAgentRule`, and `BrowserToolRule`. With `require_confirmation_high` (default true) MEDIUM does not
confirm → Stage 5 passes → `Allowed: true`. Compare pkg/security/permissions.go:637-644, which DENIES
unknown actions ("Unknown action: ...", `RuleSource: "unknown_action"`) — the two permission engines
disagree on the fail-closed rule.
**Effect:** Unknown action permitted at MEDIUM. LOW reachability: `Engine.Check*` has NO non-test callers
outside `internal/eval/hitl` (metrics.go:409-411); the live tool path uses `pkg/security.PermissionChecker`
via executor.go:2226. Graded LOW for that reason; flagged because the brief's rule (c) says unknown action
must deny, and any future wiring of the SQLite engine onto the live path would inherit the fail-open.
**Caller evidence:** `git grep -n -E 'engine\.(Check|CheckForAgent)\(' HEAD -- 'internal/**/*.go'` → only
internal/eval/hitl/metrics.go:409,411.
**Confidence:** confirmed by reading.

### A3-9 — LOW — `Engine.checkOverrides` swallows the DB query error and returns nil (a stored DENY override is skipped)
**File:** internal/security/engine.go:1060-1067
```go
err := e.db.Select(&overrides, `SELECT ... FROM permission_overrides WHERE action = ? AND ...`)
if err != nil {
    return nil
}
```
**Trigger:** SQLite busy/IO error on the overrides read. The caller (Stage 4, :669) treats nil as "no
override applies" and proceeds to the confirmation gate / permitted.
**Effect:** A creator-recorded `deny` override for the action is not applied on that call. Same reachability
caveat as A3-8 (eval harness only) → LOW. Also: the entire `CheckForAgent` runs under `e.mu.RLock()`
(:527-528) while `logDecision` performs a DB INSERT on every decision (:749-756) — I/O under a lock, and with
`SetMaxOpenConns(1)` (:107) a slow insert serializes every concurrent check. Repository hard rule: "no I/O
under mutex" (AGENTS.md Coding Practices).
**Confidence:** confirmed by reading.

### A3-10 — LOW — PII SwapStore: `Put` uses a `*sql.DB` captured before `Close` nils/closes it; expired rows are never evicted from the in-memory maps
**File:** internal/security/pii/store.go:200-210 (Put), :301-321 (Close)
```go
db := st.db          // captured under lock
st.mu.Unlock()
if _, err := db.Exec(`INSERT INTO pii_swaps ...`)   // Close() may have set st.db=nil and closed db
```
**Trigger:** `Put` racing `Close` (daemon shutdown with a mask in flight) — `db.Exec` on a closed handle
returns `sql: database is closed`, which `Put` reports as an error (no panic). Separately, rows past
`ttlSec` are refused by `Resolve` (:240) but never deleted from `st.mem`/`st.fwd` or the SQLite file, so raw
PII values accumulate in memory and on disk for the process lifetime despite the "restore window" (research D4).
**Effect:** LOW — opt-in feature (`security.pii.enabled` default false), and both effects are bounded (an error
return; a slow leak). `ResolveForRestore` (:277-295) is also O(len(text) × tokens) with per-byte prefix scans.
**Caller evidence:** components.go:2589-2601 (only construction site, gated on config); agent/pii_restore_hook.go.
**Confidence:** confirmed by reading.

### A3-11 — LOW — JSON5 duration preprocessing rewrites ANY quoted duration-looking string to an integer unless its key is registered
**File:** internal/config/json5_loader.go:299-313 (`convertQuotedDurations`)
```go
if prevNonSpace(data, start) != ':' || !isSimpleQuotedString(data[start:i]) || stringDurationKeys[curKey] {
    b.WriteString(data[start:i]); continue
}
token := data[start+1 : i-1]
d, err := parseDuration(token)
if err != nil { ...continue }
fmt.Fprintf(&b, "%d", d)
```
**Trigger (verified by running the real `preprocessDurations`):** `{"label": "2h"}` → `{"label": 7200000000000}`;
`{"single": "7d"}` → int; `{"neg": "-5m"}` → int; `{"idle_timeout_x": "5m"}` → int while `"idle_timeout": "5m"`
is preserved. Any STRING-typed config field whose value happens to parse as a duration (and whose key is not in
the six-entry `stringDurationKeys` allow-list) is turned into a number.
**Effect:** `json.Unmarshal` then fails with "cannot unmarshal number into Go struct field ... of type string",
so the config load errors loudly rather than silently misbehaving — hence LOW. It is a maintainability trap
the file itself documents ("when adding a duration-STRING config field, register it here"), and it is
impossible to express a literal string like `"2h"` in any string field today.
**Confidence:** confirmed by executing the function on probe inputs.

### A3-12 — LOW — Cluster queue lease timestamps mix `RFC3339Nano` (write) and `RFC3339` (compare) as strings
**File:** internal/queue/store_cluster_claim.go:93, :127 (write `time.RFC3339Nano`), :145-151 (compare `now` as `time.RFC3339`)
```go
timeoutAt := time.Now().UTC().Add(claimTimeout).Format(time.RFC3339Nano)   // e.g. "2026-11-25T12:04:59.5Z"
...
now := time.Now().UTC().Format(time.RFC3339)                                 // "2026-11-25T12:05:00Z"
... AND timeout_at IS NOT NULL AND timeout_at < ?                            // string compare
```
**Trigger:** Any remote-reclaim sweep where `timeout_at` carries a fractional second.
**Effect:** Lexicographic string comparison of `"...59.5Z"` vs `"...12:05:00Z"`: at the character where
they first differ (`.5Z` vs `:00Z`), `'.' (0x2E) < ':' (0x3A)` is TRUE, so a lease that is still valid until
12:04:59.5 compares as ALREADY EXPIRED at 12:04:00-12:04:59 (the `now` string `12:04:xxZ` shares the
`12:04` prefix and then `.` < any digit... actually `now`= "12:04:30Z": common prefix "12:04", then
timeout has "." and now has ":" → timeout < now → expired). A remote node's claim can therefore be reclaimed
up to 59 seconds EARLY while its worker is still alive and heartbeating (worker.go:355 heartbeats every 60s
against a 5-minute timeout, so the early window is normally absorbed — but a reclaim that fires during the
window resets the row to pending and a second worker claims it; the first worker's completion is then
correctly rejected by the claim_token predicate, so the effect is duplicate EXECUTION, not duplicate
completion). LOW because the worker's 60s heartbeat cadence keeps `timeout_at` ~5 min ahead in practice.
**Confidence:** likely (format mismatch confirmed; early-reclaim window derived, not executed).

## Verified clean
- internal/llm/semantic_cache.go:168-241, :282-312 and client.go:515-539, :565-577 — a tool-call response
  cannot be served from or stored in the semantic cache: checked at the store scan (:215), the wrapper (:535),
  the store entry (:283) and the wrapper (:569). Embedder failure returns `(nil,false,nil)` / silent return
  (no error). `cacheHitResponse` copies the response so the shared entry is never mutated.
- internal/llm/spill.go:33-43, :98-113, :159-169 — session IDs are reduced to `[A-Za-z0-9-_]` before
  `filepath.Join`; traversal is impossible. `SweepToolSpills` sort key is `(mtime, path)` — deterministic.
- internal/llm/tier_policy.go:145-167 — `nextHopBlocked` depth arithmetic is correct (primary + `limit`
  degradations); `SetTierPolicy` copies the struct and nil-clears (typed-nil safe).
- internal/queue/store_cluster_claim.go:23-119 — `ClaimNextClustered` runs inside a transaction with
  `UPDATE ... WHERE id = ? AND state = 'pending'` and `RowsAffected == 0 → ErrJobAlreadyClaimed`: two workers
  cannot both win. `CompleteAttempt` (store.go:907-985) carries the claim token in the WHERE clause, so a
  superseded attempt cannot complete/fail the live one. Lease IS renewed: worker.go:346-359 heartbeats every
  60s via `ClusterQueue.Heartbeat` → `HeartbeatClaim` extends `timeout_at`.
- internal/codemode/sandbox.go:148-181, :175-181; bridge.go:20-72 — `require()` registry has an empty native
  table and a loader that rejects every path; no goja_nodejs node extensions imported; no fs/net/process/env
  surface exists in the VM. Sandbox is constructed per tool call and `Close` is deferred in the same
  goroutine (builtin/codemode.go:135-145), so the unlocked `s.closed` write is never concurrent in production.
- internal/jevgate/gate.go:113-178 — every non-allow shape (nil scorer, error, nil result, Degraded, low
  confidence, below threshold) escalates to confirmation: fail-closed.
- internal/groups/resolve.go:198-291, :266-291 — an empty effective allowlist under an active policy returns
  `Managed: true` with no `AllowedTools` (D8 deny-all); a missing `default` policy under `default_managed`
  returns a deny-all managed record. Dangling bindings are caught structurally by `Validate`
  (validate.go:157-163) and enforced at boot (daemon/groups_boot_validate.go:22-33).
- internal/groups/overrides.go:85-107, :185-231, :247-289 — overrides are add-only by type shape; malformed
  entries are named errors; save is temp-file + rename at 0600.
- internal/config/schema.go (DefaultConfig) — cross-checked the opt-in posture: `memory.tiers.enabled`,
  `security.groups.default_managed`, `llm.cache.semantic.enabled`, `metrics.otlp*`, `memory.conflict.enabled`,
  `parking.queue_backend`, `atomic_compaction.enabled` all default false; the authorized default-ons
  (`l0_enabled`, `knowledge.enabled`, `kanban.*`, `plans.approval.require_approval`) are true as documented
  in opt-in-defaults.md. No opt-in feature defaults ON.
- internal/session/store_sqlite.go:1509-1531 — `UpdateDescriptionAuto` is a single statement with
  `WHERE description_user_set = 0`; a user rename cannot be clobbered.
- internal/skills/index.go:608-727 — `RankedNames` guards `len(taskVecs)==0` before `[0]`; embedding runs
  outside the index mutex; unknown skills are kept, never dropped.
- internal/cluster/executor_bridge.go:254-278, :321-335 — dispatch signatures are verified fail-closed
  (unsigned / unknown origin / bad signature all rejected before the active map). `SetSigningKeyLookup` IS
  wired in production via gossip_handler.go:101,114 (`SetExecutorBridge`). `completeJob`/`failJob` are
  idempotent via the `finished` flag under the mutex; `cleanup` releases every Ensured ref.
- internal/memory/tierpolicy/demote.go:288-318 — demote writes the episodic copy FIRST and the flag SECOND;
  a crash between steps is recoverable; no delete seam exists on the path.
- internal/memory/tierpolicy/thresholds.go:59-77 — NaN/±Inf/out-of-range `MinUsefulness` resolves to the
  default (inverted range check is NaN-safe).
- internal/memory/tierpolicy/engine.go:600-684 — single-start `Start`/`Stop` with a stop channel and ctx
  cancel; no goroutine leak.
- internal/security/pii/detector.go:131-186 — no nested-quantifier regexes; Go RE2 has no catastrophic
  backtracking. Detected text is never logged (error strings carry lengths only, store.go:225).

## Not audited
- internal/groups/diagnose.go (+772), groups.go, membership_writer.go, memberjoin.go — read-only
  diagnosis/reporting surface and the membership file writer; not reached within budget.
- internal/config/config.go, otlp_traces.go, context_assembler_config.go, cluster_config.go,
  sync_config.go, backup_config.go, git_checkout.go, sync.go, reload_hooks.go — smaller deltas; not reached.
- internal/llm/client.go beyond the semantic-cache/spill sites (~3,400 of 3,819 lines), anthropic.go,
  codex.go, codex_sse.go, command_score.go, errors.go, errors_tier.go, models.go, provider_manager.go,
  provider_registry.go, providers.go, runtime_config.go, runtime_process.go, runtime_sweep.go,
  supervisor.go, token_cache.go, token_cache_l2.go, credentials.go, warm.go — not reached.
- internal/security/pii/swapper.go was read; internal/security/taint/taint.go, shell_guard.go,
  permission_table*.go, secrets.go, orchestrator*.go were not in the diff focus list and not read.
- internal/memory: claim_demote.go, consolidation*.go, dual_store.go, episodic.go, epistemic.go,
  fact_store.go, ftstore.go, handler.go, manager.go, personality.go, task.go, types.go — not reached.
  recall_outcome.go read to line 140 only.
- internal/skills/lifecycle/*, internal/intake/project.go (grepped for path/OS calls: none), linkage.go,
  internal/toolprov/provider.go, cmd/meept/doctor*.go, groups.go, users_cmd.go, intake.go, main.go —
  not read line-by-line.
- internal/cluster: engine.go, gossip.go, gossip_transport.go, grpc_types.go, wireguard_sync.go,
  config.go — not reached.
