# OPEN QUESTIONS — effect-idempotency tree

Deviations already RESOLVED during authoring (recorded here so leaves don't
re-litigate them; see master.md Interface Contracts for the pinned outcome):

| # | Question | Resolution |
|---|----------|------------|
| R1 | Task brief targets `internal/workspace/git_ops.go` for git push wiring, but survey shows that file has NO push (`internal/workspace/manager.go:341`: Close never commits/pushes). | Wire `internal/backup/git_ops.go` (`gitPushWithRetry`, go-git, `Op: "git_push"`) instead. Impact: leaf 02 anchors differ from the brief; cluster heartbeat push deferred (Q4). |
| R2 | Task brief names the push package `internal/services/push`; the actual package is `internal/services` (`push_service.go`, `push_channels.go`, `push_history.go`). | Anchors point at `internal/services/push_service.go` (`PushService.Push`). Impact: none beyond import paths. |
| R3 | Pinned schema lacks a way for reconcile to re-execute an effect after restart (auto-retry needs the request). | Added `payload` column to `effects_ledger` + `EffectMeta.Payload`/`EffectRecord.Payload` (master.md Contracts 1/3). Impact: schema + API grew one field; receipts stay small, payloads may contain request bodies — no secrets, tool authors decide. |
| R4 | Pinned state machine includes `abandoned`, but the pinned API had no transition into it. | Added `Abandon(ctx, key, reason)` (+ `Get`) to the Ledger interface (master.md Contract 1). Impact: two extra methods on the pinned surface; CLI reconcile has a real target to call. |

Open questions — genuine forks, each with Q / Rec / Impact:

## Q1. Abandoned-state timeout policy

**Q:** Should records stuck in `claimed`/`receipted` longer than some age
(e.g. 24h, 72h, never) auto-transition to `abandoned` at reconcile time?
The pinned policy surfaces them forever; unbounded pending lists could
grow.

**Rec:** No auto-abandon in this tree. `claimed` means "maybe the effect
fired and we crashed before the receipt" — auto-abandoning converts a
possible just-delivered-but-unrecorded send into silent data loss. If list
growth becomes real, add an opt-in `--stale` FILTER to `meept effects list`
first, and only then consider auto-abandon with a configurable floor
(default ≥7d) that still logs each transition as an Error.

**Impact:** affects `internal/daemon/effects_wiring.go` (Reconcile policy),
schema unchanged; a later tree would touch only the reconciler + CLI.

## Q2. Which additional tools deserve wiring next?

**Q:** The article's protocol applies to every irreversible external
effect. Candidates found in the survey: `internal/cluster/git_sync.go`
`push()` (~line 460, heartbeat-driven cluster membership writes),
`http_request`-class tools, webhook/notification channels beyond the bus
(`internal/services/push_channels.go` Telegram delivery), queue-job
side effects, future `employee` enforcement actions that mutate external
state.

**Rec:** Wire in this order, driven by observed incidents rather than
speculatively: (1) `cluster.GitSync.push` — but NOT per-heartbeat: key it
on the member-record content being synced, not the tick; (2) any future
HTTP/webhook tool at design time (its tool schema should grow an
`idempotency_key` input the provider echoes); (3) push channels only if
channel-level delivery (Telegram) proves lossy/duplicating in practice.
Do NOT wrap the bus `push.<session>` publications themselves — those are
meept-internal events, not external effects.

**Impact:** each addition touches its tool package + reconciler executor
registration + docs; no schema change (payload/key generalize).

## Q3. Retention of completed records

**Q:** `completed` rows accumulate forever in `effects.db`. Prune after N
days? Keep keys forever to make re-claims idempotent even months later?
Archive?

**Rec:** Keep forever for now. A completed key that later re-claims is the
mechanism working correctly (idempotent no-op); pruning it converts a
would-be no-op into a re-execution. Size is trivial (one row per external
effect, keys are 64 chars). Revisit only if a high-frequency effect tool
lands (e.g. per-request webhooks); then prune `completed` older than 90d
BUT warn loudly in docs that pruning weakens the no-op guarantee.

**Impact:** none today; a future retention pass touches
`internal/effects` (a `PruneBefore` method) + reconciler startup + docs.

## Q4. Cluster heartbeat push: wire or exempt?

**Q:** `(*GitSync).push` in `internal/cluster/git_sync.go` runs on the
sync loop (frequent). Ledger-claiming every heartbeat adds a DB write per
tick and noise rows; exempting it leaves the most chatty push site
unprotected.

**Rec:** Exempt the heartbeat path; wrap only the DISCRETE membership
events (`RegisterNode`, `Leave`) which are genuinely irreversible claims on
cluster state. Key those on member ID + record content. If the cluster
node ever pushes externally-user-visible refs, revisit.

**Impact:** keeps leaf 02's file budget small (cluster wiring would be its
own leaf); Q2 ordering already reflects this.

## Q5. Audit-finding integration for surfaced effects

**Q:** The pinned decision says surfaced pending effects raise to the user
via "audit finding + CLI visibility, NOT a prompt." Meept's employee
subsystem has an audit path (`internal/employee/enforcement.go`
`AuditFinding`, `PostTurnAuditor`). Should the reconciler ALSO file an
AuditFinding so employee governance sees pending effects?

**Rec:** Not in this tree. The reconciler has no employee/AuditStore
dependency and adding one (daemon → employee edge + store wiring) exceeds
the pinned scope; slog Error + `meept effects list` satisfies the pinned
contract. If desired later: a leaf registering an
`internal/employee`-side checker that calls `ReconcilePending` on the
auditor's periodic tick and converts stuck records into findings — no
changes to the ledger itself.

**Impact:** future leaf in `internal/employee` or
`internal/daemon/effects_wiring.go`; ledger API unchanged.

## Q6. Payload retention & secrets hygiene

**Q:** The added `payload` column stores the effect request (R3). Push
content and backup repo paths are stored verbatim in
`<data_dir>/effects.db`. Any policy concern (disks, backups of the data
dir, multi-user daemons)?

**Rec:** Accept for now — `effects.db` lives beside `parks.db`/session
stores which already hold equivalent material (message bodies, session
content) under the same 0600-class daemon data dir. Add one sentence to
docs/workflows/effects.md: tool authors must NOT put credentials in
payloads (keys live in config/providers, per repo convention). If a
multi-user hardening pass happens, revisit column-level treatment.

**Impact:** docs wording (leaf 03); no code.
