# 07-bench/09 COMPONENTS BOOT-ORDER AUDIT REPORT

Plan: 20261119-usability-remediation. Leaf: 07-bench/09 (residual item 4).
Scope: `internal/daemon/components.go` `NewComponents` (lines 1082-5906, 4,825
lines), plus cross-function assignment sites in `daemon.go` and `wire*.go`.
Method: scripted (python over the source; script at /tmp/components_audit.py),
not eyeballed. Analysis only -- NO fixes implemented.

## 1. Methodology

A python script located `func NewComponents` by name match, extracted the body
via string/comment-aware brace matching (4825 lines), then:

1. Stripped `//` comments (string-aware, so URLs like `http://` survive).
2. Collected every `c.<field>` READ and every `c.<field> = / += / -=` WRITE
   (including multi-target `c.X, err := ...` forms) with line numbers.
3. Computed first-read vs first-write per field; flagged read-before-write.
4. Collected every nil guard (`c.X != nil` / `c.X == nil`) and classified each
   against its field's first in-function write: `ok` (write earlier), `LATER`
   (write after the guard -- the silent no-op signature), `EXT` (never written
   in-function -- assigned by the caller or a wire* method).
5. Repeated the write scan over `Start()` (5968-7459) to find cross-function
   use-before-assign (fields read in NewComponents but only written in Start
   or daemon.go after NewComponents returns).
6. Manually verified each flagged site by reading the surrounding source and
   the consumer-side nil-guards (false-positive pruning, e.g. `c.ToolGateAudit,
   gateAuditErr = ...` is a multi-target write, not a read).

Audited volumes:

- distinct fields READ in NewComponents: 164 (826 field-read occurrences;
  17 more occurrences are method calls on 15 receivers like
  `c.ensureSecretBroker()` / `c.wireGitHubTools()`)
- distinct fields WRITTEN in NewComponents: 146 (158 write occurrences)
- fields read-but-never-written in-function (EXT class, assigned by caller or
  wire* helpers): 32
- nil guards audited: 60 distinct guarded fields
- fields with in-function read-before-first-write: 4 (EffectsLedger,
  FenceChecker, Scheduler, WeatherFetcher -- all four verified by hand; none is
  the silent-no-op class)
- nil guards classified LATER (guard evaluates before any in-function write):
  1 (WeatherFetcher -- benign, see F7)
- cross-function use-before-assign candidates (read in NewComponents, written
  in daemon.go after NewComponents returns): PlanManager, SecretBroker,
  ClusterEngine (verified individually below)

## 2. Known bugs from this campaign (recorded for cross-check, both FIXED)

### B1. jobProc.WithBus(c.msgBus) guarded by a field that was always nil -- FIXED

- Then-site: `jobProc.WithBus(c.msgBus)` guarded by `c.msgBus != nil`
  (old line ~5107). `c.msgBus` is only assigned at employee wiring inside
  NewComponents itself (now components.go:5569, `c.msgBus = msgBus`), long
  after the job-processor site -- the guard never fired in production and the
  toolEvidenceCollector never subscribed to `tool.execution.complete`.
  Unit tests wired `WithBus` directly and never exercised the guard.
- Fix: commit 4f934dfb "fix(usability): job processor bus wiring used nil
  c.msgBus field..." -- the site now uses the `msgBus` PARAMETER
  (components.go:5170-5174, with an explanatory comment at 5165-5169).
- Status: FIXED (verified in working tree).

### B2. Daemon singleton template loop missing AgentRegistry/queue -- FIXED

- `c.AgentLoop` (template loop, constructed ~2670-2680) predates
  `agent.NewAgentRegistry` (~4089-4121) and received neither
  `WithAgentRegistry` nor a queue; every direct-mode chat turn clones the
  template (`handler.go:4001` `GetOrCreateWired`), copied the nil registry via
  ConfigSnapshot, and `RunOnceWithParts` never registered the per-turn queue --
  `chat.steer`/`chat.followup` returned `ErrQueueNotFound` on every
  conversation (07-bench/02 p5 runs 4-6).
- Fix: commit ab72e851 (residual wave) -- late-attach seam
  `(*AgentLoop).SetAgentRegistry` (internal/agent/registry.go:2049/2069) called
  at components.go:4222 immediately after registry construction, plus
  `chat_steering_wiring_test.go` production-path gate.
- Status: FIXED (verified in working tree; 03-steering-fix-REPORT.md).

## 3. Findings table

Severity classes:
- CRITICAL = a feature silently dead in production (nil-guard passes on an
  always-nil field; consumer treats nil as "off" and unit tests wire directly).
- WARNING = works today but the ordering is fragile: a reorder or a new caller
  silently breaks it, or correctness depends on an unobvious cross-function
  sequence.
- INFO = read-before-write or nil-guard that is verified benign.

| # | Sev | Site (line) | Field(s) | Symptom (one line) |
|---|-----|-------------|----------|--------------------|
| F1 | CRITICAL **FIXED (uncommitted, working tree)** | was 2823-2827, now just after the ledger open (~3397) | c.EffectsLedger | Push idempotency + backup-push claiming wire the LEDGER VALUE at 2823/2827, but the ledger is only opened at 3397 -- both setters ran with the always-nil-at-that-point field, so push delivery and git-backup pushes NEVER claimed effect keys; the 5850 backup-scheduler site (after 3397) was the only live wiring. Fixed shape mirrors B1 exactly. FIX: both SetEffectsLedger calls relocated into a post-ledger-open wiring block (components.go, immediately after the effects-ledger init block, before the execution gate). Regression: effects_ledger_wiring_test.go (PushService.EffectsLedger() armed after NewComponents; degraded open path nil-safe). |
| F2 | CRITICAL **FIXED (uncommitted, working tree)** | was 3089, now split | c.Scheduler | `wireInstructions(msgBus, c.Scheduler, ...)` ran 2,221 lines before `c.Scheduler = sched` (5310, only when cfg.Scheduler.Enabled); the nil-check in instruction_wiring.go:88-95 then skipped the instruction cron scheduler EVERY boot, so cron-type user instructions were silently never scheduled. FIX: wireInstructions split -- store/handler/listener/parser/verifier/injector stay at the early site; InstructionScheduler construction moved into `(c *Components).wireInstructionScheduler` (instruction_wiring.go) called immediately after the `c.Scheduler = sched` block. Start() lifecycle unchanged. Regression: effects_ledger_wiring_test.go (InstructionScheduler armed when scheduler enabled; nil when disabled; store guard). |
| F3 | WARNING | 3727 | c.workingStore | `WireWorkingProducer(..., c.workingStore, ...)` depends on WireContextAssembler (2738 -> context_assembler_wiring.go:87) having set the unexported field 989 lines earlier; a nil store silently drops the working-ring producer (guard = absence). Works only because the two sites are far apart and nothing between them reorders. |
| F4 | WARNING | 4917 | c.FenceChecker | Orchestrator deps read `c.FenceChecker` which is documented permanently nil (1251: "kept for compile compatibility"); guard skips fence checks by design -- correct today, but the field is a live trap: any future assignment changes orchestrator semantics silently. |
| F5 | WARNING | 3200-3212 | c.ClusterEngine | UsersSync closures read `c.ClusterEngine` at CALL time (engine constructed later in daemon.go, wired at daemon.go:487/766); guarded and comment-documented, but correctness depends on cross-function ordering invisible inside NewComponents. |
| F6 | WARNING | 4939-4960 | c.PlanManager | planPhaseSink closure captures c.PlanManager and reads it per call (plan system constructed in daemon.go:1115/1123 after NewComponents returns, daemon.go:1119 assigns components.PlanManager). Nil-guarded at capture site and at daemon.go; works, but the closure silently no-ops (Warn per phase) if plan construction failed -- feature-off-by-nil with only log evidence. Same pattern for Orchestrator/Dispatcher/RalphLoop SetPlanManager (daemon.go:2153/2161/2168). |
| F7 | INFO | 5650-5651 | c.WeatherFetcher | `if c.WeatherFetcher == nil { c.WeatherFetcher = weather.New(...) }` -- guard reads the field before its first write, but the field is never assigned anywhere earlier in NewComponents (verified: first_write@5651 IS the guard's own body). The guard is a "may be preset by embedder" convention, not a boot-order bug. Only LATER-classified nil guard in the constructor. |
| F8 | INFO | 5333 | c.SecretBroker | `builtin.SetSchedulerProvider(c.Scheduler, c.SecretBroker)` reads a field that is nil inside NewComponents when secrets proxy wiring is deferred to daemon.go:496 (`wireSecretsProxy`, secrets_wiring.go:48) unless github_wiring populated it first (github_wiring.go:118 `ensureSecretBroker`). Provider stores the possibly-nil broker; scheduler tools resolve secrets lazily through it. Fragile dual-writer (two sites assign c.SecretBroker) but not dead today. |
| F9 | INFO | 6167/6193 | c.SecretBroker | Voice Start-time callbacks read `c.SecretBroker` / `c.SecretBroker.Value` unguarded -- safe ONLY because daemon.go wires secrets proxy (496) before Start (2424). The ordering contract lives entirely in daemon.go line order, unenforced. |
| F10 | INFO | 1170/1453/2021 | c.otlpTraceSubs | Unexported field written once at 1170 and read after; fine, listed to show the scan covered unexported fields too. |

Known-bug rows (both fixed, restated for the table):
| B1 | CRITICAL (FIXED 4f934dfb) | old 5107, now 5165-5174 | c.msgBus | guard saw always-nil field; tool evidence never reached envelopes. |
| B2 | CRITICAL (FIXED ab72e851) | 2670 template vs 4222 attach | c.AgentLoop/c.AgentRegistry | session clones carried nil registry/queue; steer/followup 404 on every conversation. |

### CRITICAL count: 0 open (F1, F2 FIXED in working tree, uncommitted -- see 10-bootorder-fix-REPORT.md). 2 historical CRITICALs fixed (B1, B2).

## 4. The failure signature (why unit tests never catch this class)

Every CRITICAL/WARNING above shares one shape:

```
if c.X != nil { consumer.Use(c.X) }   // guard passes ONLY when X was assigned
```

where `c.X` is assigned at a LATER line of NewComponents (F1, F2), in a later
wire* call (F5, F8), or in daemon.go after NewComponents returns (F6). At the
guard site the field is nil in every production boot, the guard is false, and
the feature is silently off. Unit tests construct the consumer directly with a
real dependency (`NewChatHandler(loop, ..., registry)` / `jobProc.WithBus(bus)`)
and never execute the constructor's guard, so tests stay green. The 02-REPORT
acceptance run caught both historical bugs only because the bench exercises the
REAL constructor against a REAL daemon.

Secondary signature: closures that read `c.X` at CALL time (F5, F6) are
correct-by-timing rather than correct-by-construction -- safe at boot, fragile
under reorder.

## 5. Late-attach seams added this campaign (band-aid inventory)

| Seam | Site | Could root ordering be fixed instead? |
|------|------|----------------------------------------|
| `(*AgentLoop).SetAgentRegistry` (agent/registry.go:2069) | components.go:4222 | Yes -- if the template loop were constructed AFTER NewAgentRegistry, or if the registry were built before the template and passed via `WithAgentRegistry`, the seam would be unnecessary. Not free: the template currently sits inside the skills-state block at ~2670 and the registry needs TaskRegistry (~3735) + CapabilityIndex etc.; moving either side reorders a large dependency fan-out. Seam is the cheaper option today, but it is a permanent ordering contract documented only in a comment. |
| `jobProc.WithBus(msgBus parameter)` (4f934dfb) | components.go:5174 | Partially -- the REAL fix is "never read c.msgBus inside NewComponents" (it is a Start-time-adjacent field). The parameter use is correct; the residual hazard is the field itself remaining on Components and being read in Start (6935, 7109) where it IS assigned by then. A `NewComponents`-local `bus` variable convention (field assigned only at the very end or dropped entirely) would remove the trap. |
| `PushService.SetEffectsLedger` (services/push_service.go:139) + `bkpkg.SetEffectsLedger` (backup/git_ops.go:209) | components.go:2823/2827 | Yes -- F1 is exactly B1's shape: move both calls to after the ledger open at 3397 (or after 5850 where the backup scheduler also re-wires), or resolve the ledger before PushService construction. The setter seams themselves are fine; their CALL SITES are early. |
| `c.msgBus` field itself | components.go:5569 | The field is assigned inside NewComponents (line 5569, employee wiring) -- not Start as the older comments at 1912/3982 say. The comments are stale post-4f934dfb and mislead auditors. Recommendation: either delete the field (the parameter covers every NewComponents site; Start reads can take the bus from the parameter stored once at the top) or assign it at line ~1090 and fix the stale comments. |

## 6. Structural fix options for NewComponents (NOT implemented)

The root disease: a 4,825-line constructor interleaves (a) pure construction,
(b) wiring that DEPENDS on later construction, and (c) start-adjacent wiring,
with correctness resting on line order and nil-guards instead of an explicit
contract.

Option A -- Phased constructor with an explicit ordering contract.
Split NewComponents into ordered phases, each returning a struct:

```
phase1: stores + clients (no cross-deps)      -> coreDeps
phase2: bus subscribers + handlers            -> uses coreDeps only
phase3: registries + managers                 -> uses coreDeps + handlers
phase4: wiring pass (Set*/With* attach)       -> all of the above, in dep order
```

Every `Set*`/`With*`/late-attach call moves into phase4, which is BY
CONSTRUCTION after every construction; read-before-write becomes impossible
without a build-break, not a silent nil. A short `boot_order.go` documents the
contract. Tradeoffs: large mechanical diff (high review cost, golden-file and
rollback-order risk mid-flight); phases need naming discipline or it decays
back into one function. This kills F1, F2, and the F8/F9 dual-writer problem
straightforwardly and removes the need for future SetAgentRegistry-style
seams inside the constructor.

Option B -- Builder object.
`componentsBuilder` accumulates partially-built pieces with typed getters that
panic/fail on missing deps (`b.Scheduler()` returns an error if not built).
Wrong-order reads become loud errors instead of silent nil-guards. Tradeoffs:
idiomatic Go pushback (builder sprawl), and it enforces presence, not ORDER --
a getter can still be called early; the value over Option A is mostly the
loud-failure property, which a phase4-only-wiring rule in Option A also
achieves via review + a lint script (the /tmp scan script generalizes to a CI
check cheaply).

Option C -- Wire-after-construct pass, keep constructor mostly as-is.
One `wire(c *Components, msgBus, cfg)` function AFTER all construction,
containing every Set*/With*/closure-attach call, invoked at the end of
NewComponents. Smallest diff of the three; directly absorbs the late-attach
seams (SetAgentRegistry, SetEffectsLedger calls, SetPlanManager already lives
this way in daemon.go:2153). Tradeoffs: leaves construction order itself
intact, so new construction-order mistakes remain possible; wiring function
becomes the next 500-line hot spot unless subdivided per subsystem.

Recommendation: Option C as the immediate residual fix (moves F1 and F2's
wiring into one auditable place, retireable seams), with Option A as the
structural target the next time the constructor is touched for feature work.
Regardless of option: (1) delete or top-assign `c.msgBus` and fix the stale
comments at 1912/3982; (2) generalize /tmp/components_audit.py into
scripts/audit-components-boot-order.py (it is ~170 lines of stdlib python) and
wire it into `make lint-ci` so read-before-write and nil-guard-on-later-write
regressions fail CI instead of surfacing in bench runs.

## 7. Verification

- All line numbers above re-read in the working tree at HEAD 4e8b37b1 after
  the script flagged them; F1/F2/F7-F9 contexts quoted and confirmed by hand.
- Fixed-status cross-checked against git: 4f934dfb (B1), ab72e851 (B2), and
  07-bench/02-REPORT.md + 03-steering-fix-REPORT.md.
- Counts in section 1 are script-computed, not estimated.
- Repo untouched (read-only audit; the only write is this report + the /tmp
  script permitted by the task constraints).
