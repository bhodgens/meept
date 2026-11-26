# Auditor 1: daemon / comm / rpc / activity / gatecapture / metrics

Scope: production Go files changed in 0a446a21..b3c03f80 under `internal/daemon/*.go`,
`internal/comm/**`, `internal/rpc/*.go`, `internal/activity/*.go`, `internal/gatecapture/*.go`,
`internal/metrics/*.go`, read at committed HEAD.

- Audit point: `997d6d02` (the brief's `e43a7cf3` is an ancestor; one scheduler-only commit sat between).
- Re-verification point: `1a4a314b` (tree moved three times mid-audit via sibling commits).
  `git diff --stat 997d6d02..HEAD -- <scope>` is EMPTY: no production code in this slice changed
  between the two points, so every finding carries over. All line numbers below are pinned at `1a4a314b`.
- Working tree was dirty with sibling edits in `internal/daemon/components.go` (uncommitted). Every
  read was `git show HEAD:<path>`; nothing in the working tree was read, written, or formatted.
  The only write by this auditor is this file.
- Method: two read-only leaf auditors (activity/comm-slack/voice-http + gatecapture/metrics/rpc)
  produced candidate findings; every HIGH and MEDIUM below was re-read by this auditor at HEAD
  before adoption. Two child claims were narrowed (C2-6 reframed, C2-7 narrowed to the top band),
  one withdrawn (child C2-9). Arithmetic claims (sampling rate, formatBand) were executed, not eyeballed.
- Files read: 70 of 70 in scope (new files in full; older files by range hunks plus the HEAD
  context around each hunk). Daemon core (`components.go` 12.4k lines, `daemon.go`, `voice_wiring.go`)
  was read by range hunks + targeted wiring traces, not end to end — see "Not audited".

Severity key: **HIGH** = reachable on the default posture or a documented default path; **MEDIUM** =
reachable but needs a non-default config, a race window, or a specific sequence; **LOW** = latent,
dead-at-HEAD, noisy, or bounded.

## Findings

### A1-1 — HIGH — `Ring.Tail(n)` returns the OLDEST n records; `activity.recent` freezes on the oldest 50 events once the ring wraps

`internal/activity/ring.go:53-69` (caller `internal/daemon/activity_rpc.go:115`)

```go
func (r *Ring) Tail(n int) ([]ActivityRecord, int) {
	...
	// Collect up to n newest records in chronological order (oldest first)
	result := make([]ActivityRecord, 0)
	for i := 0; i < r.size && len(result) < n; i++ {
		idx := (r.head + i) % r.capacity
		result = append(result, r.buf[idx])
	}
```

`head` is documented at `ring.go:15` as "index of the oldest element". The loop starts at `head`
and stops after `n` items, so it returns the oldest `n` of `size` records. Both the doc comment
("newest n records") and the RPC contract (`activity_rpc.go:98`, "live tail from the ring buffer")
say newest.

- **Trigger:** ring capacity is 1000 (`activity_wiring.go:17`); after the 1001st
  `queue.job.*`/`agent.progress` event the ring is full. Default limit is 50
  (`activity_rpc.go:112-114`; HTTP default 50 at `activity_handlers.go:114`).
- **Effect:** `GET /api/v1/activity/recent` and the TUI `activity.recent` return a static window of
  the 50 OLDEST buffered events, forever (it only shifts as eviction moves `head`). The "live tail"
  never shows recent events on a busy daemon — the surface silently lies.
- **Caller evidence:** `git grep -n '\.Tail(' HEAD -- '*.go'` → only `activity_rpc.go:115` (prod) and
  `ring_test.go` (which only exercises `n >= size` and capacity 1, never wrapped-with-`n < size`).
- **Confidence:** confirmed by reading the full path.

### A1-2 — HIGH — Data race: `Aggregator.Query` writes shared `pendingPlanIDs` with no lock

`internal/activity/aggregator.go:100-107`, read unsynchronised at `internal/activity/plans.go:50`

```go
func (a *Aggregator) Query(ctx context.Context, q Query) ([]ActivityRecord, int, error) {
	if q.PlanID != "" {
		a.pendingPlanIDs = map[string]bool{q.PlanID: true}
	} else {
		a.pendingPlanIDs = map[string]bool{}
	}
	records, err := a.collect(ctx, q)
	...
		a.stampPlanIDs(ctx, records)   // plans.go:50: for planID := range a.pendingPlanIDs
```

The package's own comment (`plans.go:9-10`) says "RPC handlers may query concurrently" and guards
`planIndex` with a mutex — but `pendingPlanIDs` is a bare field written on every query and ranged
by `stampPlanIDs` later in the same call. One `*Aggregator` is shared by `activity.query`,
`activity.overview`, and the voice day-brief seam (`awareness_wiring.go:542`).

- **Trigger:** any two concurrent `activity.query`/`activity.overview` calls (HTTP bridge
  `daemon.go:1760` is per-request goroutines; the RPC server runs one goroutine per connection).
  Two GUI clients polling is enough. `-race` would flag it.
- **Effect:** Go memory-model race (map header write vs. range). Functionally, a plan-scoped query's
  plan interest can be wiped by a concurrent plan-less query before `stampPlanIDs` runs, so plan
  stamping silently degrades.
- **Caller evidence:** `activity_rpc.go:88`, `:138`; `awareness_wiring.go:542`.
- **Confidence:** confirmed by reading the full path.

### A1-3 — HIGH — Slack and voice webhook branches bypass the rate limiter; the comments claim the opposite

`internal/comm/http/server.go:2142-2161` (both branches added in range: `71312c962` 2026-11-01,
`5e2fa6268` 2026-11-04)

```go
if s.slackInteractions != nil && r.Method == http.MethodPost && r.URL.Path == "/api/v1/slack/interactions" {
	// ... Rate limiting still
	// applies (the handler runs under the ordinary chain).
	s.slackInteractions.ServeHTTP(lrw, r)
	return
}
if s.voiceRoutes != nil && s.voiceRoutes.HandlesPath(r.URL.Path) {
	// ... Rate limiting still
	// applies (the handlers run under the ordinary chain).
	s.voiceRoutes.serveVoiceRoute(lrw, r)
	return
}
```

`handler` — the only object carrying `rateLimitMiddleware` (`server.go:2070`) — is reached at
`:2172`. Both branches dispatch directly and `return` before it. Compare the kanban pairing branch
at `:2091`, which explicitly wraps itself in `rateLimitMiddleware`; that is the pattern these
two omitted. (The `messagingRoutes` branch at `:2163-2170` has the same shape but predates the
range — listed here as the same class, not a separate finding.)

- **Trigger:** any request to `POST /api/v1/slack/interactions`, `POST /api/v1/voice/webhook`,
  or `GET /api/v1/voice/media` on the main listener, whenever that channel is enabled.
- **Effect:** an unauthenticated, unthrottled endpoint on an Internet-facing daemon: the Slack
  handler does HMAC verification per request over up to a 1 MiB body (`interactions.go:227,249`),
  the voice handler does Ed25519 verification — all unbounded in aggregate. The access log at
  `:2174-2190` is also skipped for these paths.
- **Caller evidence:** routes wired at `daemon.go:1928` (`WithSlackInteractions`) and via
  `WithVoiceRoutes`; comment text is the author's stated intent, the code does not implement it.
- **Confidence:** confirmed by reading the full middleware.

### A1-4 — HIGH — Chokepoint-2 gate-capture resolution lane is dead on the DEFAULT boot; `askArmed` grows without bound

`internal/daemon/components.go:3982, 4063-4068, 8866-8878` + `internal/daemon/gatecapture_wiring.go:106-108`

```go
// components.go:3982 — gateCap passed BY VALUE into registerBuiltinTools
registerBuiltinTools(..., cfg.Security.ShellPermissions, gateCap, securityEngine, ...)
// components.go:8866-8878 — inside registerBuiltinTools, a NEW store is opened and assigned
	// to the VALUE PARAMETER; the caller's variable is untouched
	if gateCap == nil && securityEngine != nil {
		gc, gcErr := newGateCaptureStore(gatecapture.DefaultDBPath(), logger)
	...
			gateCap = gc
	if gateCap != nil && securityEngine != nil {
		securityEngine.SetGateAskCapture(gateCap)
	}
// components.go:4063 — the OUTER gateCap is still nil → resolver never wired
	if gateCap != nil && acpMgr != nil {
		wireGateCaptureResolve(acpMgr, gateCap)
	}
```

The outer `gateCap` (`components.go:1292`) is only populated when the decision gate or the jev gate
is enabled (`:1534`). `decision_gate_enabled` is commented out in `config/models.json5:54` and
`jev_command_gate` is opt-in. The default `shell_permissions.preset` is `"workspace"`
(`internal/config/schema.go:4881`), which ASKs on 7 prefixes (`sudo`, `git push`,
`docker system prune`, `chmod 777`, `curl | sh`, `bash -c`, `sh -c`; `shell_permissions.go:243`).
`securityEngine` (`components.go:3470`) is a different object from `c.SecurityOrchestrator`
(`:1255`) and is constructed unconditionally, so `:8866` runs on the default boot.

- **Trigger:** default config + any shell command hitting a workspace-preset ASK prefix.
- **Effect:** (1) `PermissionTableAsk` (`gatecapture_wiring.go:107`) does
  `g.askArmed.Store(command, struct{}{})` — a `sync.Map` keyed by the full command string with
  no TTL and no bound (the store's own `pending` map is bounded at 256 with a TTL; this one is not).
  The only consumer is `wireGateAskResolution`, reachable only through `gateResolveNotifier`,
  which is appended only by `wireGateCaptureResolve` — never called on this boot. Memory grows with
  every distinct asked command for the daemon's life. (2) Zero `permission_table_ask` rows are
  ever recorded — the feature's stated purpose (`engine.go:205-206`). (3) The orphan store opened
  at `:8867` is never `Close()`d (`c.GateCapture` is written at `:1541` and read nowhere:
  `git grep -n '\.GateCapture\b' HEAD -- '*.go'` → only the write).
- **Caller evidence:** `git grep -n 'wireGateCaptureResolve' HEAD -- '*.go'` → only
  `components.go:4068` behind the nil guard. `git grep -n 'askArmed' HEAD -- 'internal/daemon/*.go'`
  → stores at `:107`, consumed only at `:117`/`:144` via the never-wired notifier.
- **Confidence:** confirmed by reading the full path, including the default preset contents.

### A1-5 — HIGH — Tool gate G3 effects key omits session/agent: identical `(tool, args)` in two sessions collide; the second session's success is logged as a claimant mismatch and the record is flagged "pending reconciliation" at every boot

`internal/daemon/toolgate.go:386` (key), `:289-310` (reserve), `:331-356` (after)

```go
// toolgate.go:386
func effectsGateKey(name string, args map[string]any) string {
	return effects.EffectKey("gate.tool", name, gateArgsHash(args))
}
// toolgate.go:289 — claimant comes from the per-call context
	who := effects.Claimant{
		AgentID:   tools.AgentIDFromContext(ctx),
		SessionID: tools.SessionIDFromContext(ctx),
	}
```

The key is content-only; the claimant is per-session. The ledger enforces single-owner resolution
(`effects.go:206 authorizeResolve` → `ErrClaimantMismatch` on a different claimant). The gate is
armed unconditionally (`components.go:3494`) and the effects ledger is opened unconditionally
(`components.go:3403-3410`, no config gate). Every tool is treated mutating under the postured
wrapper (`registry.go:498-500`), so EVERY tool call reserves.

- **Trigger:** session A runs `{tool: "web_fetch", url: X}` (or any tool/args pair) and completes;
  later session B — different agent or session id — runs the identical call. Common for read-shaped
  tools with stable args (`list_dir {path: "."}`, `web_fetch {url}`, `github_get_issue {n}`), and
  for the SAME session across restarts only if its ids are stable.
- **Effect:** B's `Claim` returns `granted=false` (allowed through as "idempotent re-run" — fine),
  but then B's success path calls `RecordReceipt`/`Complete` with B's claimant against A's record
  → `ErrClaimantMismatch` → `Warn("tool gate: effects receipt failed")` on every such call.
  Worse: while A is still IN-FLIGHT (state `claimed`), B's identical call is allowed to execute
  a second time — the duplicate-effect protection the ledger exists for is defeated for same-args
  calls. And a record left `claimed` by a mismatch (A crashed mid-flight) is surfaced by
  `EffectsReconciler.Reconcile` as `Error("effect pending reconciliation")` on every boot
  (`effects_wiring.go:88-93`; gate records carry `ProviderIdempotent:false` so they are never
  retried, only logged) — even though B's execution actually succeeded.
  Also: if BOTH ids are empty in ctx (`who.Zero()`), `Claim` errors and `BeforeExecute` BLOCKS
  the tool (`:175-176`). The only production stamper is `loop.go:9633-9634` and it is on the sole
  agent-loop path, so I could not find a reachable zero-claimant caller — graded as part of the
  same finding, not separately.
- **Caller evidence:** gate reached via `executor.go:1780` (`GatedExecutionRegistry.GateCheck`) →
  `registry.go:500` → `BeforeExecute` → `reserve`. Ledger semantics at
  `internal/effects/ledger_sqlite.go:116-160` (Claim), `:264-280` (requireResolve).
- **Confidence:** confirmed by reading; the cross-session collision is certain from the key
  construction. Frequency is workload-dependent.

### A1-6 — HIGH — OTLP: `chat.turn` span never adopts the minted `TurnSpanID`; every hop span parents onto an id that is never exported (latent: feature is opt-in)

`internal/daemon/otlp_traces_wiring.go:203-216` (span), `:148` (parent), `:197-199` (state)

```go
producer.RecordTurnSpan(appmetrics.TurnSpanSpec{
	ConversationID: ev.ConversationID,
	...
 TraceID:        ev.TraceID,
})                                  // TurnSpanID: ev.TurnSpanID is NOT set
```

The subscriber stores `ev.TurnSpanID` into `turnSpanByTrace` (`:199`) and the LLM-call observer
sets `ParentSpanID: st.turnSpanID` (`:148`). But the turn span itself is recorded with a zero
`SpanID`, so `TraceExporter.RecordSpan` random-fills it (`otlp_trace_exporter.go:109-111`).
`TurnSpanSpec.TurnSpanID` exists and is documented as "parent of leaf 03's hop spans"
(`otlp_trace_producer.go:33`); `RecordTurnSpan` reads it (`producer.go:179`).

- **Trigger:** any boot with `metrics.otlp_traces` enabled (opt-in, default off → **latent**).
- **Effect:** every `llm.call` span carries a `parent_span_id` matching no exported span; the trace
  tree is orphaned in the receiver (Langfuse et al. show hops unparented).
- **Caller evidence:** `components.go:1182` `wireOTLPTraces(cfg.Metrics.OTLPTraces, ...)`;
  `git grep -n 'TurnSpanID:' HEAD -- internal/daemon/otlp_traces_wiring.go` → no hits.
- **Confidence:** confirmed by reading the full path.

### A1-7 — MEDIUM — OTLP sampling keeps `rate/4`, not `rate`; step spans are 100% sampled regardless of the knob (latent)

`internal/metrics/otlp_trace_sampling.go:24-27`, `otlp_trace_producer.go:351-386`

```go
const scale = float64(1) / float64(1<<63)
threshold := uint64(rate / scale / 2)     // = rate · 2^62, compared against a uniform 2^64 value
v := binary.BigEndian.Uint64(sp.TraceID[:8])
return v < threshold
```

Executed numerically: rate 0.1 → effective 0.025; 0.25 → 0.0625; 0.5 → 0.125; 0.9 → 0.225 —
a constant ×0.25. Separately, `RecordStepSpan` builds a span with NO `TraceID` (the random fill
happens later in `RecordSpan`), so the sampler reads `v = 0`, and `0 < threshold` is true for every
rate > 0 — contradicting `record`'s own comment "TraceID is already set, so the decision is
deterministic per trace" (`:212`).

- **Trigger:** operator sets `0 < sample_rate < 1` (default `1.0` short-circuits; `<= 0` is
  normalized to `1.0` by config, so the default posture is unaffected).
- **Effect:** a quarter of the intended trace volume for traced spans; `task.step` spans ignore the
  budget entirely.
- **Confidence:** confirmed by reading + execution.

### A1-8 — MEDIUM — OTLP trace-sink circuit never re-opens after the first cooldown; every later batch does a full 10 s network attempt in silence (latent)

`internal/metrics/otlp_trace_sink.go:215-231`

```go
s.consecFails++
if s.consecFails == traceCircuitThreshold {        // == 3
	s.circuitOpenUntil = time.Now().Add(traceCircuitCooldown)
	...
	return
}
if s.consecFails < traceCircuitThreshold {
	s.logger.Debug(...)
}
```

- **Trigger:** receiver down longer than 30 s. After the cooldown, `circuitBlocked()` is false → one
  probe → `noteFailure` with `consecFails` already 4, 5, 6… — neither branch matches again.
- **Effect:** the circuit becomes probe-per-batch forever: the flush goroutine blocks up to
  `traceSinkTimeout` (10 s) on every 5 s tick with zero log output, contradicting the type doc
  "one Warn per threshold-crossing" / "one circuit line per cooldown window at most".
- **Confidence:** confirmed by reading the full path.

### A1-9 — MEDIUM — `groups.assign` / `groups.remove` race on the live config (no lock, one RPC goroutine per connection)

`internal/rpc/groups.go:580, 636, 642`; readers at `handleList`/`groupsPolicies`/`resolveCaller`

```go
g := &h.cfg.Security.Groups
...
	g.Bindings = append(g.Bindings, config.GroupsBindingConfig{...})   // writer
...
	kept := g.Bindings[:0]                                              // in-place overwrite
```

No `sync` primitive anywhere in `groups.go` (`grep -c 'sync\.\|Lock()'` → 0). The RPC server
dispatches handlers under `s.mu.RLock()` (`rpc/server.go:507+`) — the SERVER's lock, which
serialises nothing between two handlers — and spawns `go s.handleConnection` per connection
(`server.go:421`). `saveConfigAtomic` also marshals the whole live `*config.Config` unlocked.

- **Trigger:** two concurrent `groups.*` RPCs (one mutating + any reader), or two mutations.
  Requires admin-privileged callers (`resolveCaller` gate), so low frequency.
- **Effect:** data race on `Security.Groups.Bindings` (append vs. range, in-place `[:0]` overwrite
  vs. reader) — torn reads or a slice-header tear under `-race`.
- **Confidence:** confirmed by reading.

### A1-10 — MEDIUM — `groups.assign/remove` return `"assigned": true` but enforcement is a boot-time snapshot; the mutation never reaches the live gate

`internal/rpc/groups.go:486,529` (success), `internal/daemon/components.go:1155-1156` (snapshot), `internal/daemon/daemon.go:411` vs `:3329`

```go
// components.go:1155 — the Resolver is built from COPIES of the config slices at boot
groupsResolver := groups.NewResolver(
	groupsPoliciesFromConfig(cfg.Security.Groups.Policies),
	groupsBindingsFromConfig(cfg.Security.Groups.Bindings),
```

`groups.Resolver` has no mutation/reload method (`resolution.go`: only `DefaultManaged`,
`ResolveForAgent`, `ValidatePolicies`). The handler holds the boot-time `fullCfg` pointer
(`daemon.go:411`); `reloadConfig` REPLACES `d.fullConfig` with a new pointer (`daemon.go:3329`)
rather than mutating in place, so after the first SIGHUP the handler mutates a struct nothing else
reads. Either way the RPC persists to `meept.json5` and reports success while the live resolver
keeps enforcing the boot-time table until restart.

- **Trigger:** any `groups.assign`/`groups.remove` call.
- **Effect:** operator believes a policy change is live; the gate enforces the old bindings. The
  child's original framing ("reload divergence") is narrower than this — it is snapshot-at-boot
  with no hot-apply seam at all.
- **Confidence:** confirmed by reading (the resolver's method set is the proof); not exercised live.

### A1-11 — MEDIUM — `register()` duplicate-check probe executes the REAL handler with `nil` params; on every boot it logs 5 spurious Warns

`internal/daemon/activity_rpc.go:35-45`

```go
register := func(method string, handler rpc.Handler) {
	if _, err := server.CallMethod(context.Background(), method, nil); err == nil {
		slog.Warn("activity rpc: method already registered; skipping duplicate", ...)
		return
	}
	server.RegisterHandler(method, handler)
}
```

`CallMethod` (`rpc/server.go:257-268`) DISPATCHES when the method exists — it has no lookup-only
mode. On the normal boot the method is absent, so this costs one `Warn("rpc: method not found")`
per method at `server.go:263` — five Warn lines per boot for a non-event.

- **Trigger:** every boot (noise). Heavy branch: a same-named handler registered earlier — then the
  probe runs it with `nil` params and a deadline-less `context.Background()`; for
  `activity.overview` that is a full unbounded aggregation (`activity_rpc.go:138`).
  `agents.*` handlers register earlier at `daemon.go:1568-1571`, but no name collides today, so the
  heavy branch is latent.
- **Effect:** misleading boot log; a foot-gun for the next registrant.
- **Caller evidence:** `daemon.go:1618` → `wireActivityComponents` → `registerActivityRPC`.
- **Confidence:** confirmed by reading.

### A1-12 — MEDIUM — Three panic recovers added in range log `r` but no `debug.Stack()` — violates internal/daemon/AGENTS.md

`internal/daemon/intake_wiring.go:196, 311`; `internal/daemon/spill_wiring.go:71`

```go
defer func() {
	if r := recover(); r != nil {
		logger.Error("intake poll tick panic recovered", "recovered", r)
	}
}()
```

Rule (internal/daemon/AGENTS.md, "Typed-nil wiring" section, last sentence): *"A panic recover that
logs no stack makes this class undiagnosable from the log - capture `debug.Stack()` in recovers."*
All three sites were added in `0a446a21..b3c03f80` (`git diff … | grep '^+.*recover()'` → 3 hits).
(`github_wiring.go:189` and `daemon.go:2521` have the same shape but predate the range.)

- **Trigger:** any panic in an intake poll tick, an intake projection handler, or a spill sweep tick.
- **Effect:** the daemon survives but the log carries no stack, so the root cause is unrecoverable
  from logs — the exact failure the rule was written after.
- **Confidence:** confirmed by reading.

### A1-13 — LOW — `activity.findings` pages client-side over a 100-row-truncated list; rows past 100 are unreachable and `total` under-reports

`internal/daemon/activity_rpc.go:238-245, 272-284`

```go
findings, err := mgr.ListAuditFindings(ctx, employee.AuditQuery{EmployeeID: ..., Severity: ..., Since: -time.Until(since)})
...
total := len(findings)
if req.Offset > 0 || req.Limit > 0 { ...findings = findings[start:end] }
return map[string]any{"findings": findings, "total": total}, nil
```

`Manager.ListAuditFindings` never forwards a limit (`manager.go:3197-3208`) and the store caps
`LIMIT 100` when `f.Limit <= 0` (`enforcement.go:2088-2093`). `CountAuditFindings` (the exact
count, added 2026-11-19 for the same class on `agents.roster`) exists but is not used here.

- **Trigger:** an employee with >100 findings; `offset=100` or `limit=1000`.
- **Effect:** `total` is capped at 100; `offset >= 100` returns empty though rows exist.
- **Confidence:** confirmed by reading both sides.

### A1-14 — LOW — `SlackChannel` publish goroutines have no shutdown path; `Wait()` exists for it and nothing calls it

`internal/comm/slack/channel.go:105` (Wait), `:125-143`, `:162-168` (goroutines)

`c.inflight.Go(...)` with a 10 s `context.Background()` timeout per post. `git grep -n
'SlackChannel\.\(Wait\|Close\|Stop\)\|slackCh\.Wait' HEAD -- '*.go'` → no production caller;
`stopComponents` has no Slack teardown.

- **Trigger:** daemon shutdown while a card post/update is in flight.
- **Effect:** the goroutine outlives `stopComponents` by up to 10 s. Bounded; not a hang.
- **Confidence:** confirmed by reading.

### A1-15 — LOW — `handleActivityFindings` coerces any non-`"true"` `resolved` value to `false` instead of 400

`internal/comm/http/activity_handlers.go:185-189`

```go
var resolved *bool
if resolvedParam != "" {
	b := resolvedParam == "true"
	resolved = &b
}
```

- **Trigger:** `?resolved=1` or `?resolved=yes`.
- **Effect:** silently filters to OPEN findings (opposite of intent); sibling params return 400.
- **Confidence:** confirmed.

### A1-16 — LOW — Four `Set*` setters documented "nil-guarded" deref a nil receiver

`internal/rpc/groups.go:168` (`SetToolValidation`, comment "Typed-nil safe", no `h == nil`),
`groups_diagnosis.go:40, 53`, `proxy.go:134` (`SetGroupsHandler`: guards `h`, not `p`).

Root AGENTS.md: "nil-guard every `Set*`". All four production call sites (`daemon.go:421-447`)
sit inside non-nil guards, so latent; `RegisterGroupsMethods` directly below DOES guard `p == nil`,
so the omission is inconsistency, not policy.

- **Confidence:** confirmed by reading; reachability LOW.

### A1-17 — LOW — `formatBand` renders the top score band (≥0.95) as `"0.0"`; the whole gatecapture export surface is unreachable at HEAD

`internal/gatecapture/export.go:83-98`

```go
band := int(score*10 + 0.5)
whole, frac := band/10, band%10
return []string{"0.0", ..., "1.0"}[whole][0:1] + "." + string(rune('0'+frac))
```

Executed: 0.95 → band 10 → `table[1][0:1]` = `"0"` → **"0.0"** (the LOWEST band label) in DPO
`rejected` text. For whole in 2..9 the same indexing yields "0.<frac>" — right by accident.
Reachability: `git grep -n 'ExportDPO\|DeriveNearMissPairs\|ListRecords' HEAD -- '*.go' | grep -v
gatecapture/ | grep -v _test` → no caller of the gatecapture export outside its own package
(only unrelated `shadow`/`activity` symbols). Root AGENTS.md: "Every feature needs at least one
user surface … Internal-only code is incomplete."

- **Confidence:** confirmed by reading + execution; dead at HEAD.

### A1-18 — LOW — Two functions in the new daemon files have ZERO references anywhere (not even tests)

`internal/daemon/toolgate.go:131` `(*ExecGateImpl).engineOverride` (comment: "test seam") and
`internal/daemon/voice_employee.go` `renderCallRelay` (thin wrapper). Dead code; listed because
the brief prioritises unreachable wiring and so the deadcode gate owner can decide.

- **Evidence:** `git grep -n -w 'engineOverride' HEAD -- '*.go'` → only the definition;
  same for `renderCallRelay`.
- **Confidence:** confirmed.

## Verified clean

- `internal/daemon/toolgate.go:140-150, 175-176` — nil receiver fails CLOSED; G3 reserve error blocks
  the tool (never allows on infra error). `scan` (:194-227): nil Orch/Engine block; category→action
  erratum (unknown → `file_read`) is a documented policy tier, not a failure path.
- `internal/daemon/toolgate.go:184-190, 416-427` — `starts` map is mutex-guarded on both sides; key
  collision only skews a duration field (reported 0 or a sibling's start), no correctness impact.
- Gate IS reached on the live agent path: `executor.go:1774-1780` asserts `GatedExecutionRegistry`
  and calls `GateCheck`; `registry.go:489-516` wraps the tool with `posturedTool` before
  `BeforeExecute`. (The 2026-10-31 "gate dead on the live path" fix is intact.)
- `internal/daemon/tiers_engine.go:340-386` — non-positive retention/stale/minUsefulness resolve to
  defaults before the cutoff arithmetic; `EngineHandle.Start/Stop/Running` nil-safe.
- `internal/daemon/intake_wiring.go:57-62, 71-80, 86-90` — poll ctx is the lifecycle ctx; invalid
  interval falls back with a Warn; malformed `watch_repo` fails closed; token-less boot schedules
  an erroring source deliberately (visible per tick).
- `internal/daemon/intake_wiring.go:236-324` — both projection subscribers `select` on `c.ctx.Done()`
  and `Unsubscribe` on exit; handlers fail-soft.
- `internal/daemon/gate_park_wiring.go:110-122, 268-280` — three pumps exit on ctx.Done and
  unsubscribe; bus publish is under `RLock` and `Unsubscribe` takes the write lock, so no
  send-on-closed race (`bus.go:428-440, 466-480`). `stopGateParker` is nil-safe.
- `internal/daemon/dsh_wiring.go:148-181` — `StartGoalRoundDriverLoop` is stopped from
  `shutdown()` (`daemon.go:3052`); `StartDelegateResumeSweep` is synchronous but `Resume` spawns
  (`delegate_descriptor.go:74`) and is capped at 32, so boot is not blocked on LLM turns.
- `internal/daemon/spill_wiring.go:57-102` — ticker stopped via `defer`; done-channel never nil'd;
  `stopToolSpillSweep` has a 5 s bound.
- `internal/daemon/context_assembler_wiring.go:101-110, 153-160` — provider built after its stores,
  config arm after the provider; `WireWorkingProducer` returns nil (no subscriber) when the ring
  store is nil; per-event goroutines panic-recovered WITH `debug.Stack()`.
- `internal/daemon/voice_employee.go:705-706, 801-829` — `retainedReason`/`reasonSeen` reads and
  writes all under `reasonMu`; map bounded with oldest-eviction. Hold tracker (:961-1070): every
  method nil-safe except `arm` (documented), `markReady` is `sync.Once`, `released/reason` guarded.
- `internal/daemon/voice_wiring.go:2369-2373` — `SetCallerResolver` nil-guards the wiring; reached
  from `components.go:6266`.
- `internal/daemon/gatecapture_wiring.go:61-71, 96, 114-122, 133` — `newGateCaptureStore` defaults a
  nil logger; `Escalated`/`wireGateAskResolution`/`wireGateCaptureResolve` nil-safe;
  `PermissionTableResolved` consumes the armed mark exactly once (the bug is the lane never being
  wired, A1-4, not the adapter). Typed-nil rule honoured at `components.go:1564-1581, 2629-2633`.
- `internal/daemon/otlp_traces_wiring.go:222-236, 352-355` — step/tool goroutines `range` the
  subscriber channel and `bus.Unsubscribe` closes it; `stopOTLPTraces` reached at
  `components.go:7553`.
- `internal/daemon/components.go:1534-1545` — outer gateCap typed-nil guard correct (the defect is
  the value-parameter, A1-4).
- `internal/activity/aggregator.go:63-77` (`NewAggregator`), `activity_wiring.go:41-54, 116-121` —
  every concrete pointer enters an interface field only behind `if p != nil` (daemon AGENTS.md rule).
- `internal/activity/busbridge.go:38-112` — every handler recovered; typed `SubscribeT` for
  `turn.terminal`; payload keys match publishers (`queue/queue.go:400-403,608`; `agent/tactical.go:2544`).
- `internal/activity/plans.go:16-40` — `planIndex` fully mutex-guarded.
- `internal/daemon/activity_rpc.go:51-96` — `sort` validated, `since`/`until` parsed RFC3339; all
  five handlers nil-guard `agg`/`ring`/`mgr`.
- `internal/comm/http/activity_handlers.go`, `intake_handlers.go` — registered in `setupRESTRoutes`
  through the ordinary auth chain; 503 on `rpcCall == nil`.
- `internal/comm/slack/interactions.go:122-134, 213, 227, 239, 253` — signature verified over the
  raw body BEFORE parse, `hmac.Equal` constant-time, 5-min replay window, empty secret fails
  closed, 1 MiB body bound + Close, empty `AllowedUserIDs` = nobody. (The gap is the missing rate
  limit in front of it, A1-3.)
- `internal/comm/slack/channel.go:153-156` — mutex not held across the HTTP post; `Notifier()`
  enters `acp.PermissionNotifier` only via `fanSlackNotifierIntoRelay`'s nil check.
- `internal/daemon/slack_wiring.go:37-111` — config mapping nil-safe, `Validate` before construction,
  all three functions reached from `components.go:2822, 4060-4061`.
- `internal/comm/http/voice.go:216-275, 331-389` — signature before parse/upgrade, body bounded with
  `MaxBytesReader`, `Enabled=false` fail-closes to 503, start-frame read bounded (30 s / 50 frames),
  conn closed via defer. `voice_plain.go:185-268` — loopback verified twice, `errCh` buffered(1).
- `internal/gatecapture/store_sqlite.go:120-140` — `_pragma` DSN honoured by modernc; WAL/busy_timeout
  applied; migrations guarded and `db.Close()` on failure. `store.go:170-200` — vocab validated, IDs
  via `pkg/id.Generate`.
- `internal/metrics/otlp_trace_exporter.go:88-107, 150-175` — `RecordSpan` non-blocking with
  drop+counter; `Stop` is `sync.Once` + `wg.Wait` + final `Flush`; IDs from `crypto/rand`.
- `internal/metrics/otlp_trace_sink.go:131-170` — `resp.Body` drained and closed; per-request
  `WithTimeout`; mutex never held across the HTTP call.
- `internal/metrics/store.go` hunks — migrations 7–9 column/index-guarded; TTFT aggregate excludes
  the `-1` sentinel. `collector.go`, `otlp_exporter.go`, `otlp_http_sink.go`, `usage.go` hunks —
  field additions / method move, no behaviour change.
- `internal/rpc/intake_status.go` + `internal/daemon/intake_status_rpc.go` — direct handler, reached
  at `daemon.go:1624`, nil-safe on `c`/`Config`/`Scheduler`. `cluster_handler.go:91` —
  `ClusterStats` exists on `*queue.ClusterQueue`.
- `internal/rpc/groups.go:425-500` — C5 gate precedes persistence; `caller_agent_id` trusted only for
  `SpawnedByDaemon` peers. No `makeProxy` registrations in `groups.go`/`intake_status.go`, so the
  comm/AGENTS.md live-responder rule does not apply to this slice.
- `internal/daemon/components.go:8866-8878` typed-nil: `securityEngine.SetGateAskCapture(gateCap)` only
  behind `gateCap != nil` — correct at that site.
- Daemon-level recovers added in range for `Run` boot goroutines (`daemon.go:2521`) predate the range
  (checked with `git log -S`); not re-reported.

## Not audited

- `internal/daemon/components.go` (12,432 lines) and `internal/daemon/daemon.go` (3,767): read by
  range hunks + every hunk's surrounding HEAD context + targeted wiring traces (gateCap, toolgate,
  groups, OTLP, intake, goal-round driver). NOT read end to end; pre-range code in these files was
  only touched where a range hunk or a finding's path led there.
- `internal/daemon/voice_wiring.go` (2,418 lines): range hunks and the functions in the dead-wiring
  sweep only. The voice call-control state machine (hold/splice/hangup ordering) is auditor 04's
  slice (`04-voice.md`).
- `internal/daemon/memory_capture.go`, `l0_wiring.go`, `codemode_wiring.go`, `tierpolicy_wiring.go`,
  `groups_overrides_wiring.go`, `groups_boot_validate.go`, `toolprovider_config.go`,
  `github_wiring.go`, `acp_register.go`, `hermes_register.go`, `memory_fact_wiring.go`,
  `cluster_resources_wiring.go`, `http_dispatch_adapter.go`: covered by the mechanical sweep
  (every top-level func's non-test references) and the recover/`debug.Stack` grep only; their range
  hunks were not read line by line beyond that.
- `internal/comm/http/server.go` (3,756 lines): only the range hunks (middleware chain, voice/slack
  bypass) were read.
- `internal/rpc/proxy.go`, `cluster_handler.go`: range hunks only (both clean).
- No build/vet/test was run (per brief; parent owns the gates). Every claim is a HEAD source read
  plus, for the two arithmetic findings, an executed probe.

## Disclosure ledger

- Child finding "C2-6 reload divergence" reframed as A1-10 (snapshot-at-boot, no hot-apply at all).
- Child finding "C2-7 formatBand" narrowed: the child said `whole in 2..9` was "accidentally right"
  and 0.95 → "0.0"; execution confirms exactly that, so the finding stands only for the top band.
- Child finding "C2-9 RecordScore nil guard" WITHDRAWN by the child itself on re-read; not carried.
- Child "C1-5 OpenStoreFromDB mutates shared pool" not adopted: the mutation is confirmed but the
  pool is already `MaxOpenConns(1)` from earlier wirings (idempotent today) and no I/O sits under a
  mutex — coupling, not a defect at HEAD.
- Tree moved `997d6d02 → e9461a8d → 7b7ab262 → 1a4a314b` during the audit (employee/scheduler
  commits); scope diff between audit point and re-verification point is empty.
