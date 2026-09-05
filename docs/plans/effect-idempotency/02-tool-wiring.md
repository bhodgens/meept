# Tool Wiring + Reconcile Hooks - Implementation Leaf

## DISPATCH INSTRUCTION

> You are the implementer for this leaf. Work ONLY from this document.
> Implement ALL tasks below using TDD (test first, watch it fail, implement,
> watch it pass). Do NOT commit. Do NOT run git add. The orchestrator handles
> all git operations after review.
> Do NOT use read_file on existing source files — explore with search_files
> or terminal cat. Never pipe read_file output into write_file
> (line-number corruption rule: `NN|` prefixes must never reach a source
> file). After writing a file, do NOT read it back to verify — write once
> and stop. After completing, report what you built, what files you touched,
> test results, and any deviations from the spec.

## Scope

Wire the first two external-effect tools through the `internal/effects`
ledger — push notification (`internal/services`) and backup git push
(`internal/backup`) — plus the `ReconcilePending` hooks on daemon startup
and the parked-turn resume path. This leaf changes EXISTING packages and
adds one new daemon wiring file. No CLI, no RPC, no docs (leaf 03).

**Files (≤3 groups; the leaf stays inside the ≤3-files-of-change budget by
treating the wiring file group as one unit):**
- Modify: `internal/services/push_service.go` (ledger-claiming Push path)
- Modify: `internal/services/push_service_test.go` (or new
  `push_effects_test.go` — your call, state it)
- Modify: `internal/backup/git_backup.go` (ledger-claiming backup push path —
  the actual caller of `gitPushWithRetry`; verify the call site with
  search_files before editing)
- Modify: `internal/backup/git_ops_test.go` or new `git_effects_test.go`
- Create: `internal/daemon/effects_wiring.go` (reconciler + hooks)
- Create: `internal/daemon/effects_wiring_test.go`
- Modify: `internal/daemon/components.go` (Components field + construction +
  stopComponents close) and `internal/daemon/daemon.go` (startup hook call)
- Modify: `internal/agent/handler.go` (nil-guarded reconciler hook in the two
  resume methods) — smallest possible diff.

**Test command:** `go test -p 2 ./internal/effects/... ./internal/services/...
./internal/backup/... ./internal/daemon/...` (macOS ephemeral-port rule:
always `-p 2`).

## Dependencies

- 01-effect-ledger.md must be REVIEWED first (this leaf imports
  `internal/effects`).

## Estimated context

~90K

## Interface Contract (From Parent)

Everything from master.md Contracts 1-5 applies. This leaf CONSUMES:

```go
// internal/effects (from leaf 01 — already implemented):
func EffectKey(parts ...string) string
func Run(ctx context.Context, l Ledger, key string, meta EffectMeta,
    execute func(ctx context.Context) (json.RawMessage, error)) (receipt json.RawMessage, reused bool, err error)
func (l *SQLiteLedger) /* constructor */ NewSQLiteLedger(dbPath string, logger *slog.Logger) (Ledger, error)

// Tool-integration pattern (pinned pseudocode — every wired tool follows it):
//
//   key := effects.EffectKey(toolName, ...identity-parts...)
//   receipt, reused, err := effects.Run(ctx, ledger, key, meta, execute)
//   if err != nil { return ..., err }
//   if reused { /* prior receipt == idempotent no-op result */ }
//   return ..., effects.Complete(ctx, key)
```

This leaf PRODUCES (Contract 5, pinned shape):

```go
// File: internal/daemon/effects_wiring.go
package daemon

// EffectReExecutor re-runs a pending effect for a tool that declared
// provider-side idempotency. Returns the provider receipt on success.
type EffectReExecutor func(ctx context.Context, rec effects.EffectRecord) (json.RawMessage, error)

// EffectsReconciler owns reconcile behavior over the ledger.
type EffectsReconciler struct{ /* ledger, executors map[string]EffectReExecutor, logger */ }

func NewEffectsReconciler(l effects.Ledger, logger *slog.Logger) *EffectsReconciler

// RegisterExecutor registers a re-executor for a tool name. Nil-guarded
// (setter convention); only meaningful for tools whose EffectMeta declared
// ProviderIdempotent=true.
func (r *EffectsReconciler) RegisterExecutor(tool string, fn EffectReExecutor)

// Reconcile runs the pinned policy over ReconcilePending:
//   - executor registered AND rec.ProviderIdempotent -> re-execute via
//     effects.Run + Complete on success (state -> receipted -> completed);
//     on failure: slog.Warn, record stays pending.
//   - otherwise: slog.Error("effect pending reconciliation", "key", ...,
//     "tool", ..., "claimed_at", ...) and count as surfaced.
// Returns counts for logging/tests.
type ReconcileCounts struct{ Retried, Surfaced, Failed int }
func (r *EffectsReconciler) Reconcile(ctx context.Context) ReconcileCounts

// INVARIANT (AGENTS.md park/resume rule): NO new bus topics anywhere in
// this leaf. Park/resume events keep riding the existing agent.quota_wait
// topic family. Effect visibility is CLI-only (leaf 03) + structured logs.
```

### Verified anchors (survey results — trust but re-verify line numbers with
search_files before editing; line numbers drift):

- **Push path:** `internal/services/push_service.go` —
  `func (s *PushService) Push(ctx context.Context, req *PushRequest) (*PushResult, error)`
  (~line 145). It builds a payload map, marshals it, and publishes on
  `"push."+sessID` per session with `Topic: "push.notify"`. Message IDs come
  from `id.Generate("push-")` (pkg/id — correct convention already).
  `PushToChannels` (~line 102) is channel-direct delivery — ALSO wrap it
  (same effect key derivation), or document in deviations why not, if its
  callers make claim-before-send infeasible within this leaf's file budget.
- **Backup push path:** `internal/backup/git_ops.go` —
  `func gitPushWithRetry(repo *git.Repository) error` (~line 162, go-git
  `repo.Push`, `Op: "git_push"` errors). It is called by the backup commit
  flow in `internal/backup/git_backup.go` (~line 159, `return
  gitPushWithRetry(repo)`). KEY-ID MATERIAL: the backup repo path + the
  commit SHA being pushed — derive the effect key from those, NOT from wall
  time. `AlreadyUpToDate` (`git.NoErrAlreadyUpToDate`) is already treated as
  success — that IS provider-side idempotency, so
  `ProviderIdempotent: true`.
- **Daemon startup:** `internal/daemon/daemon.go` `func New(cfg *Config)`
  (~line 118). The plan-RPC handlers register ~lines 785-796; that region is
  the startup-hook anchor. `internal/daemon/components.go` — `ParkStore`
  field ~line 139 with its lifecycle comment (open in NewComponents over
  `<data_dir>/parks.db`, closed in stopComponents) is the pattern to mirror
  for an `EffectsLedger` field over `<data_dir>/effects.db`. Data-dir
  validation lives at components.go ~line 517 (`daemon.data_dir must be
  set`).
- **Parked-turn resume:** `internal/agent/handler.go` —
  `func (h *ChatHandler) resumeQuotaParkedTurn(ctx context.Context, turn QuotaParkedTurn)`
  (~line 1648) and
  `func (h *ChatHandler) resumeParkedTurn(ctx context.Context, turn ParkedTurn)`
  (~line 1885). Add a nil-guarded setter (setter nil-guard convention) and a
  best-effort reconcile call at the TOP of each resume method.
- **PushService construction:** `internal/daemon/components.go` ~line 1488
  (`services.NewPushServiceWithChannels`) — where the ledger gets injected.
- **Test seed patterns:** `internal/workspace/manager_test.go` lines 30-40
  (local bare remote + `runGit(t, seedPath, "push", ...)`).

## Context

The pinned protocol per tool: **Claim → execute → verify → RecordReceipt →
Complete.** `effects.Run` (leaf 01) already implements claim + execute +
receipt; the tool adds Complete after. On `granted=false` with a completed
prior, the tool returns the prior receipt as an idempotent no-op — the
second caller must be indistinguishable in success semantics from the first,
without the external effect firing twice.

Why these two tools: push notification re-delivery double-renders in
TUI/Telegram (NOT provider-idempotent — reconcile must surface, never
auto-retry); backup git push re-pushing the same commit is `AlreadyUpToDate`
(provider-idempotent — reconcile MAY auto-retry). They exercise both halves
of the pinned reconcile policy.

Original-task note (recorded as a contract decision): the task brief pointed
at `internal/workspace/git_ops.go`, but that file has NO push —
`internal/workspace/manager.go:341` states Close never commits or pushes.
The verified production push sites are `internal/backup/git_ops.go` and
`internal/cluster/git_sync.go`. This leaf wires backup push; cluster
heartbeat push is deferred (OPEN-QUESTIONS.md Q4).

Receipt contents (keep small, no secrets):
- push: `{"push_id": <msgID>, "delivered": N, "sessions": [...]}`
- backup push: `{"head": <sha>, "remote": <repoPath>, "already_up_to_date": bool}`

Key derivation (fixed order — document in each call site):
- push: `EffectKey("push.notify", strings.Join(SessionIDs, ","), Source, Type, Content)` —
  identical content to identical sessions is genuinely the same effect.
  Content participates because two pushes with identical body/source/sessions
  ARE the same user-visible effect. Truncate nothing — sha256 handles length.
- backup push: `EffectKey("backup.git_push", repoPath, headSHA)`.

Dependency direction: `internal/services` and `internal/backup` now import
`internal/effects` — that is the ONLY new import edge; neither may import
`internal/daemon`.

## Tasks

### Task 1: PushService ledger wiring (TDD)

**Objective:** `PushService.Push` claims before publishing, records a
receipt, completes; duplicate calls with identical inputs are no-ops
returning the prior receipt.

**Files:**
- Test: `internal/services/push_effects_test.go` (new — keeps the existing
  push_service_test.go untouched)
- Modify: `internal/services/push_service.go`

**Steps:**

1. Add a field + nil-guarded setter (setter convention):

```go
// SetEffectsLedger wires the external-effect ledger. When nil (tests,
// ledger disabled), Push keeps its legacy behavior exactly — no claiming.
func (s *PushService) SetEffectsLedger(l effects.Ledger) {
    if l != nil {
        s.effects = l
    }
}
```

2. In `Push`, AFTER request validation and BEFORE the publish loop: when
   `s.effects != nil`, derive the key (Context section), call
   `effects.Run` with `execute` = the existing publish loop body returning
   `json.Marshal({"push_id": msgID, "delivered": result.Delivered})`, then
   `Complete` on success. On `reused=true`, parse the prior receipt and
   return its delivered count — do NOT publish. When `s.effects == nil`,
   the existing code path runs unchanged (zero diff to legacy behavior).

3. Test (in-memory ledger from leaf 01 + the existing bus fake style from
   push_service_test.go):

```go
func TestPushService_EffectsIdempotent(t *testing.T) {
    // subtests:
    // - "first send": ledger record ends completed, receipt has push_id
    // - "duplicate send": second call with identical req returns the SAME
    //   receipt, bus publish count == 1
    // - "nil ledger": legacy behavior, no ledger records created
}
```

4. `go test -p 2 ./internal/services/...` green; legacy
   push_service_test.go untouched and passing.

### Task 2: Backup push ledger wiring (TDD)

**Objective:** the backup push flow claims before pushing; reconcile can
auto-retry it.

**Files:**
- Test: `internal/backup/git_effects_test.go` (new)
- Modify: `internal/backup/git_backup.go` (the caller of
  `gitPushWithRetry` — verify the exact function with search_files first)
  and/or `internal/backup/git_ops.go` if the cleaner seam is inside
  `gitPushWithRetry` itself. Choose ONE seam; state it in your report.

**Steps:**

1. Same field + nil-guarded setter pattern as Task 1
   (`SetEffectsLedger`). On the seeded-bare-remote test pattern
   (`internal/workspace/manager_test.go` lines 30-40), the effect key is
   `EffectKey("backup.git_push", repoPath, headSHA)`.

2. Wrap: `effects.Run` (execute = commit+push returning the receipt JSON
   from the Context section) then `Complete`. `reused=true` → skip the
   push, return success (log at Debug: "backup push already completed",
   "key", key). `ProviderIdempotent: true` in the meta.

3. Test with a local bare remote:

```go
func TestBackupPush_EffectsIdempotent(t *testing.T) {
    // subtests:
    // - "first push": push happens, record completed, receipt has head sha
    // - "second run same key": repo remote ref unchanged, no second push,
    //   prior receipt returned
    // - "nil ledger": legacy path (no claims recorded)
}
```

4. `go test -p 2 ./internal/backup/...` green.

### Task 3: EffectsReconciler (TDD)

**Objective:** the pinned reconcile policy, unit-tested against the
in-memory ledger.

**Files:**
- Test: `internal/daemon/effects_wiring_test.go`
- Create: `internal/daemon/effects_wiring.go`

**Tests:**

```go
func TestEffectsReconciler(t *testing.T) {
    // subtests over a MemoryLedger seeded directly (Claim + hand-set
    // states via RecordReceipt where the interface allows):
    // - "idempotent tool retried": claimed record with
    //   ProviderIdempotent=true + registered executor whose fn runs ->
    //   record completed, counts.Retried==1
    // - "idempotent tool executor fails": executor returns error ->
    //   record still pending, counts.Failed==1, log contains warn
    // - "non-idempotent tool surfaced": claimed record with
    //   ProviderIdempotent=false -> NOT executed even though an executor
    //   exists; counts.Surfaced==1; slog Error emitted
    // - "no executor registered": idempotent record, no executor ->
    //   surfaced (never retried blind)
    // - "receipted stuck record": also returned by ReconcilePending ->
    //   same policy branches
}
```

Implementation notes:
- Constructor takes `effects.Ledger` + logger; `RegisterExecutor` is
  nil-guarded.
- Re-execution goes through `effects.Run` with the record's stored
  `Payload` — the executor receives `rec` and rebuilds the request from
  `rec.Payload` (this is why leaf 01's schema carries payload).
- Use `slog.NewTestHandler` (or capture via a buffer logger, matching
  existing daemon test style) to assert log presence.

### Task 4: Daemon construction + startup hook

**Objective:** ledger opened over `<data_dir>/effects.db`, reconciler
constructed, startup reconcile runs once.

**Files:**
- Modify: `internal/daemon/components.go`
- Modify: `internal/daemon/daemon.go`
- Modify: `internal/daemon/effects_wiring.go` (construction helpers if kept
  there)

**Steps:**

1. Components: add `EffectsLedger effects.Ledger` + `EffectsReconciler
   *EffectsReconciler` fields adjacent to `ParkStore` (~line 139) with the
   same lifecycle comment style. Open in NewComponents right after ParkStore
   construction: `effects.NewSQLiteLedger(filepath.Join(dataDir,
   "effects.db"), logger)`. On open error: log Warn and continue with nil
   (same degradation posture as ParkStore — nil ledger = feature off, tools
   fall back to legacy paths via their nil checks). Close in
   stopComponents AFTER the parkers stop (mirror order).

2. daemon.go `New`: after the plan-RPC registration block (~line 796), when
   `components.EffectsReconciler != nil`, run
   `Reconcile(ctx-with-30s-timeout)` synchronously; log the counts
   (`"effects startup reconcile"`, "retried", "surfaced", "failed").
   Errors never fatal.

3. Test: `TestEffectsStartupReconcile` in effects_wiring_test.go — construct
   the reconciler with a tempdir SQLite ledger pre-seeded with one claimed
   idempotent record + executor; run the reconcile call-shape; assert
   completed + counts. (Daemon.New itself is too heavy to construct in a
   unit test — test the helper the hook calls, and assert by inspection
   that daemon.go calls it exactly once in the startup path.)

### Task 5: Parked-turn resume hook

**Objective:** best-effort reconcile on resume.

**Files:**
- Modify: `internal/agent/handler.go` (SMALLEST possible diff: one field,
  one nil-guarded setter, two one-line calls)
- Test: `internal/agent/handler_effects_test.go` (new)

**Steps:**

1. Add `effectsReconciler EffectsResumeHook` (a tiny interface or func type
   defined IN package agent to avoid importing daemon — dependency direction
   daemon → agent must not be inverted):

```go
// EffectsResumeHook is the parked-turn-resume reconcile callback. Defined
// here so package agent does not import the daemon.
type EffectsResumeHook func(ctx context.Context)

// SetEffectsResumeHook wires the reconciler. Nil-guarded (setter
// convention); nil disables the hook.
func (h *ChatHandler) SetEffectsResumeHook(fn EffectsResumeHook) {
    if h == nil || fn == nil { return }
    h.effectsResumeHook = fn
}
```

2. At the TOP of `resumeQuotaParkedTurn` (~1648) and `resumeParkedTurn`
   (~1885):

```go
if h.effectsResumeHook != nil {
    rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
    h.effectsResumeHook(rctx) // best-effort; hook logs its own errors
    cancel()
}
```

3. daemon-side wiring (effects_wiring.go): a func adapting
   `EffectsReconciler.Reconcile` to `agent.EffectsResumeHook`, registered
   where ChatHandler is constructed/wired in components.go.

4. Test: with a counting hook, both resume methods invoke it once; nil hook
   = no panic; hook error/panic does NOT break resume (the hook is
   best-effort — wrap the call so a panicking hook is recovered+logged, or
   document why recovery is unnecessary; state the choice).

5. INVARIANT CHECK (grep your own diff): zero new `bus.Publish` topic
   strings, zero new topic constants. Park/resume events still ride
   `agent.quota_wait`; you add no topics.

### Task 6: gofmt + scoped verification

- `gofmt -l internal/services/ internal/backup/ internal/daemon/
  internal/agent/` prints nothing.
- `go test -p 2 ./internal/effects/... ./internal/services/...
  ./internal/backup/... ./internal/daemon/...` green.
- `go test -p 2 -race ./internal/effects/...` still green.
- `go build ./cmd/meept ./cmd/meept-daemon ./internal/daemon` green.
- `go run ./tools/analyzers/mutexio/... ./internal/...` (or `make mutexio`)
  clean for touched packages.

## Self-Verification Checklist

- [ ] Both wired tools follow the pinned pattern: Claim → execute →
      RecordReceipt (via `effects.Run`) → Complete
- [ ] `granted=false` with completed prior ⇒ prior receipt returned as
      idempotent no-op, external call NOT re-executed (test proves it)
- [ ] nil-ledger paths preserve legacy behavior byte-for-byte (existing
      tests untouched and green)
- [ ] Effect keys derived from content identity (sessions+content /
      repoPath+sha), never from wall time or random IDs
- [ ] Push meta: `ProviderIdempotent=false`; backup push:
      `ProviderIdempotent=true`
- [ ] Reconciler: auto-retry ONLY for idempotent+registered tools; others
      surfaced via slog Error; counts returned
- [ ] Startup reconcile runs once in daemon New; resume reconcile wired into
      BOTH resume methods via nil-guarded hook
- [ ] NO new bus topics (grep diff for new topic strings — zero)
- [ ] Setter nil guards on every new Set* method
- [ ] No mutex held across I/O (mutexio clean)
- [ ] `go test -p 2` everywhere (always `-p 2`)
- [ ] No debug artifacts: no print debugging, no TODOs, no placeholder values
- [ ] No line-number corruption (`NN|` prefixes) in any file

## Review Checklist

- [ ] All tasks implemented; scope respected (no CLI/RPC/docs — leaf 03)
- [ ] Interface contracts from master.md Contracts 4-5 satisfied
- [ ] Tests written first and passing (TDD followed)
- [ ] Dependency direction: services/backup → effects only; agent gets a
      hook type, not a daemon import
- [ ] No obvious bugs or security issues (receipts contain no secrets; no
      raw key material anywhere)
- [ ] No debug artifacts: no print debugging, no TODOs, no placeholder
      values, no commented-out code
- [ ] No line-number corruption: no `     N|` prefixes baked into source
- [ ] Do NOT commit. Do NOT run git add. — confirmed

Report: what you built, files touched, test output summary, deviations
(especially: which push seam and which backup-push seam you chose, and the
PushToChannels decision).
