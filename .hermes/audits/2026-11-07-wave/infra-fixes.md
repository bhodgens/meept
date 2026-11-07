# Adversarial regression audit — 2026-11-06 wave (llm / security / surfaces / harness)

- HEAD verified against: `7b510ed4ffb2374d8609ade9ce569ef0e17ba49f`
- Audit timestamp (UTC): 2026-11-07T00:05Z (finalize ~01:00Z)
- Commits in scope: `adb69ad2` (TTFT reset), `959448eb` (observer fan-out),
  `39dfb0ca` (gatecapture modes), `fba5db2a` (intake palette parity),
  `54faad60` (AGENTS.md github invariant), `d7e4ab86` (fakellm precedence),
  `6dfa553a` (SemanticIndex dead seam), `11709a92` (property tests),
  `1ff0b303` (turn-journal tolerance).
- Method note: working tree carries sibling WIP (`internal/gatecapture/store.go`,
  `export.go`, `internal/memory/tierpolicy/thresholds*.go`, `internal/agent/dispatcher*.go`,
  and others). All findings below cite `git show HEAD:<path>` state. `go build
  ./internal/agent ./internal/llm ./internal/gatecapture ./internal/tui` and the
  targeted property-test run executed against the sibling-touched tree where noted.

## Findings

### F-1 [MEDIUM] gatecapture: the -wal sidecar stays 0644 for the process's whole life — the exact data the fix protects lives there
**File:** `internal/gatecapture/store_sqlite.go:94-104` (HEAD)

Evidence:
```go
// WAL side files are best-effort.
_ = os.Chmod(dbPath, 0o600)        //nolint:errcheck // best-effort tighten
_ = os.Chmod(dbPath+"-wal", 0o600) //nolint:errcheck // best-effort tighten

db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
```
Why it's a bug: both chmods run **before** `sql.Open`. On a first post-fix open
there is no `-wal` yet, so the `-wal` chmod silently no-ops; SQLite then creates
`<db>-wal` at `0666 & ~umask` = **0644** when WAL activates, and every
`command_gate_records` INSERT (the secret-bearing argv rows this commit exists
to protect) is written into that 0644 `-wal` until checkpoint. Any local user can
read un-checkpointed argv for the entire daemon run. The next boot's pre-open
chmod repairs it only after the exposure window. Also `-shm` is never in the
chmod list at all. The "best-effort" comment is honest about intent but the
tightening point is wrong: it must happen **after** the first open/migration
(or the DSN/`PRAGMA` path can't create the sidecar 0600 on its own), e.g.
`db.Ping()`+migrate, then chmod `dbPath+"-wal"`/`+"-shm"`. The pin
`TestOpenGateDBFileModes` (store_sqlite_test.go) closes `gdb` before stat-ing
sidecars and never asserts `-wal` mode — it cannot catch this.
Not a false-confidence-free test: the test passes for the main file only.

### F-2 [LOW] gatecapture: pre-existing 0755 directory is never tightened; commit message's "installs from the old build are repaired at next boot" is false for the dir
**File:** `internal/gatecapture/store_sqlite.go:91` (HEAD)

Evidence:
```go
if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
```
Commit message: *"existing files (plus WAL sidecar best-effort) are tightened on
open, so installs from the old build are repaired at next boot."*
Why it's a bug: `os.MkdirAll` applies the mode only to **newly created** dirs;
an existing dir (created 0755 by the pre-fix build, the exact population the
commit claims to repair) is returned as-is with no chmod — there is no
`os.Chmod(filepath.Dir(dbPath), ...)` anywhere in the package (grep of
`os.MkdirAll|os.OpenFile|Create` under `internal/gatecapture/` shows only
lines 91/94). The file chmod on the next line does repair the main DB file, so
residual exposure is dir listing + any additional files the dir may hold, but
the stated repair does not happen. The reopen leg of `TestOpenGateDBFileModes`
re-stats only `dbPath`, never the dir — the gap is untested. One-line fix:
chmod the dir unconditionally after MkdirAll (best-effort, mirroring the file
tighten).

### F-3 [LOW] llm observer fan-out: chatter field written under pm.mu but read unsynchronized on the request path
**File:** `internal/llm/provider_manager.go:344-351`, `internal/llm/client.go:320-326`, `internal/llm/anthropic.go:349-353`, `internal/llm/codex.go:349-353` (HEAD)

Evidence: `ProviderManager.SetLLMCallObserver` takes `pm.mu` and calls the
concrete setters; each setter does `c.llmCallObserver = fn` with **no client
lock**, while `recordUsageStore` reads `c.llmCallObserver` (client.go:2230,
anthropic.go:303/339, codex.go:303/339) from concurrent request paths.
Why it's flagged: a write of a func field racing reads of the same field is a
data race (`go test -race` tripwire if any caller ever attaches after boot —
OAuth AddProvider inheritance at provider_manager.go:1636-1641 happens before
serve, and the daemon attach is boot-time, so today's call sites make it benign;
`usageStore`/`AttachUsageStore` share the same pre-existing pattern). Not a
live bug at HEAD; the fan-out made the write-race surface 3× wider. Low.

## Reviewed clean

- **adb69ad2** (TTFT reset): reviewed clean. `ttftStart/ttftCaptured/ttftFirstDelta`
  appear in only the two streaming lanes (`client.go:2566-2568`,
  `anthropic.go:762-764` — both reset the pair per re-arm); `codex.go` carries no
  TTFT state at all (grep `ttft` empty), so no third chatter exists to miss.
  Non-streaming/cache-hit paths never arm (`ttftStart` zero → `ttftMS` −1),
  `captureFirstDelta` stays first-wins (client.go:2187-2193). `chatOptions` is
  per-call via `buildChatRequest` — request-scoped, no cross-turn leakage.
- **959448eb** (observer fan-out) functional review clean apart from F-3: all
  three chatter classes built by `createChatterFor` (Client / AnthropicClient /
  CodexClient, provider_manager.go:423-468) implement `SetLLMCallObserver`; no
  silently-skipped class exists (the type-assert `llmCallObserverSetter` covers
  exactly those three; test-stub chatters are not manager-constructible).
  Fan-out and `AddProvider` inheritance both run under `pm.mu`; daemon ordering is
  correct (`components.go:1826-1827` attach precedes `registerOAuthProviders` →
  `AddProvider` at components.go:9326). Observer fires from the same
  `recordUsageStore` goroutine on success **and** error records with the
  effective per-request provider/model (cfg param is the request-scoped config at
  all three call sites). The boot primary attach (components.go:1288) stays
  idempotent (field overwrite, single fn, no double-fire). Test is honest (drives
  the stored field through the real seam, incl. late AddProvider).
- **fba5db2a** (intake palette parity): reviewed clean. Palette key `i` is unique
  within the TUI `ModalCommandPalette` dispatch space (config keys c/s/t/q/m/p/e/y/n/r/o/v/a + hardcoded u/f — app.go:2404-2497; app.go:3211 "i insert" is vim-mode help, not this space). The
  legacy `internal/sharedclient/menus/palette.go` `i`="status: usage" is a
  separate termbox palette with no dispatch overlap into `handleModalKey`.
  Daemon-down handled: the `i` case runs `executeIntake` which gates on
  `h.rpc == nil || !h.rpc.IsConnected()` → `ErrNotConnected`
  (intake_view.go:81-83). Parity manifest complete: every `command_palette.dart`
  row (c,s,p,t,e,a,i,f,n,r,o,m,v,u at HEAD lines 53-131) has a
  `gui_inventory.json` entry; the two manifest rows absent from Dart (queue,
  sidebar) are `gui:false` with documented deviations. Golden regenerated.
  Micro-nit (not filed): `case "i"` no-ops silently if `a.commandHandler` is nil —
  mirrors the existing `u` branch posture.
- **54faad60** (AGENTS.md github invariant): reviewed clean; every claim verified
  at HEAD: `githubsvc/register.go:24` `ToolFactories` + `:83-88` tombstone;
  `builtin/github_register.go:34` `init()` populates `ToolFactories`,
  `:84` `SetGitHubProvider(opts, ledger, sink, broker)` 4-arg signature;
  `daemon/github_wiring.go` order is `SetGitHubProvider` (:72) → `[tool_providers]`
  filter (:78-83, early-return on `enabled=false`) → `toolprov.RegisterAll` (:85) —
  "enabled=false skips registration" is true (registration skipped; only the
  inert state-bind runs).
- **d7e4ab86** (fakellm precedence): reviewed clean. `postToolTextExplicit` is set
  only by `SetPostToolText` and cleared by `Reset()` (fakellm.go:443) alongside
  `postToolText="done"`; the constructor default (:352) leaves it false so the
  recall arm keeps its old behavior for non-scripting suites. Sweep of all
  `SetPostToolText` call sites: the memory-marker suites
  (memory-defaults, memory-layered-distill, context-memory-engine) never call it
  (pinChat-only) — recall arm unaffected; continuity-regressions
  `recall_continuity_test.go` calls both, but there `chatText` and the
  sentinel-armed `postToolText` are the same string (lines 38/105/138), so its
  fall-through detector still fires identically. Predicate scripts
  (step 0 of `classifyLocked`) still outrank everything, incl. the new gate.
- **6dfa553a** (SemanticIndex dead seam): reviewed clean. Zero leftover
  references — `BuildIndexWithCancel` / `SemanticIndex.Stop` appear nowhere in
  internal/ cmd/ e2e/ at HEAD. Lifecycle note matches reality: Dispatcher owns
  `indexCtx/indexCancel/indexWG` (dispatcher.go:409-413), arms the background
  build (:684-687), and `Stop` cancels + waits (:768-772);
  `Components.Stop` calls `c.Dispatcher.Stop()` (components.go:6929).
  `go build ./internal/agent` compiles. (The note's "BuildIndex runs
  synchronously on the caller" is accurate — `SemanticIndex` now spawns nothing.)
- **11709a92** (property tests): reviewed clean. All 18 tests pass
  (`go test -count=1 -run 'TestActionValue|TestEscapeSlackText|TestTruncateRunes|TestParseRetryAfter|TestResolveDemotion|TestFrame|TestChunkBytes' ./internal/comm/slack ./internal/llm ./internal/voiceframe ./internal/memory/tierpolicy` → 4× ok;
  note the tierpolicy leg ran against the sibling-dirtied tree — HEAD's two files
  are a strict subset of that behavior, `-0.0` special + explicit-struct init).
  Seeds are pinned consts (20261106 family), failures print seed+case. Properties
  were checked against the real contracts: the slack round-trip domain
  restriction (colon-free ids) is producer-matched, not overfit (P1 header cites
  relay.go id shape); voiceframe P1/P2 rest on json.Marshal's
  newline-escaping (verified against frame.go framing); retry-after P2's
  date/zero exclusions are each narrowed to the documented known-bugs
  (F1 int64-ns wrap, F2 year-1 sentinel) and re-pinned by the deliberately
  `t.Skip`-ed KnownBug tests with "delete-on-fix" instructions — correct
  posture, no wrong-property false confidence. The F1 counterexample itself
  (negative delta with present=true, `retry_after.go:45`) is honestly reported,
  not asserted away.
- **1ff0b303** (turn-journal tolerance): reviewed acceptable with the caveat below.
  Causal chain verified: the `"(quota wait:"` token (paren-prefixed) is produced
  **only** at `components.go:10416`, inside the same `if
  errors.AsType[*llm.QuotaResetError]` block that calls
  `p.publishQuotaWait` (:10412) — so log-presence implies the park branch ran for
  a real escaped `QuotaResetError` on a step job. Cross-traffic risk assessed:
  the restart appends to the same `daemon.log` the boot wrote
  (`bootRestart` main_test.go:332-345, `O_APPEND` on `s.Work/daemon.log`, the
  harness path daemon.go:416 — so `readWholeLog` spans BOTH the pre-kill and the
  post-restart boot), but the pre-kill phase scripts STEP-B as a tool-call (no
  quota response armed until after `kill9` + `ResetScripts`), and the scenario's
  scratch home contains exactly one task — no rehydrated prior jobs or background
  turns can emit the stamp from foreign traffic here. The claimed deterministic
  event-shape pin exists as stated: `e2e/suites/quota-park/quota-park_test.go:143-152`
  subscribes in-process **before** the park and asserts reason `quota_wait`,
  class, and RFC3339 `unblock_at`.
  **Caveat (not filed as a finding):** the tolerance is sound for *this*
  scenario's topology, not as a general idiom — on a stack that rehydrates
  multiple prior jobs the whole-file scan would accept a stamp from any job, and
  the log-scoped `served >= 1` gate is the only thing binding the proof to the
  resumed step. Keep it as-is; don't copy the pattern into multi-task suites.

## Observations (made, not acted on)

- `internal/tui/testdata/gui_inventory.json` row `find...` vs
  `command_palette.dart:95` `find…` (U+2026) — pre-existing cosmetic label-form
  divergence, no cross-language test enforces exact strings; out of this wave's
  scope.
- `internal/gatecapture/store_sqlite.go` comment at :28 references "MkdirAll on
  the parent" — stale wording post-fix; F-2's fix should update it.
- The sibling WIP in `thresholds.go` (`out := DemotionThresholds(cfg)` conversion
  form) changes F4 known-bug behavior (-0.0/NaN handling); flagging only so the
  wave coordinator doesn't double-file it against 11709a92.

AUDIT COMPLETE — 3 findings (0 high, 1 medium, 2 low), 6 commits reviewed clean.

## Closeout (2026-11-06, fix pass)
- F-1 CLOSED: 02f68f90 — gatecapture openGateDB repairs pre-existing dir mode, tightens WAL sidecars AFTER migration (they don't exist pre-open), and the DSN moved to the honored modernc _pragma= form (the deeper discovery: mattn-style keys were silently dropped, so WAL/busy_timeout/foreign_keys never bound). Pins: TestOpenGateDBFileModes, TestOpenGateDBRepairsPreexistingDir, TestOpenGateDBHonorsDriverPragmas.
- F-2 CLOSED: folded into the systemic class in 1f9f4835 — ALL 22 production modernc DSN sites converted to _pragma= form (auditlog, compress, llm routing log, memory dual/fact/ftstore/knowledge/migrate/usefulness/vector, security orchestrator/engine/pii, session, shadow x3, skills usage, daemon recents, tools change journal, pkg/sqlite pool, cmd doctor ro-path). Guard: scripts/audit-sqlite-dsn-pragmas.py wired into make lint-ci + AGENTS.md (mutation-proven). Side effects pinned: FK enforcement now binds (two latent orphan-row test bugs fixed at source — internal/session part (d), internal/agent handler_persist_toolcalls).
- F-3 CLOSED: 02f68f90 — llmCallObserver plain-field race replaced by atomic.Pointer + loader helpers on Client/AnthropicClient/CodexClient; recordUsageStore loads once before the goroutine. Pins: TestLLMCallObserver_SetReadRace (-race clean; mutation to plain field reproduces WARNING: DATA RACE).
AUDIT CLOSED - 3/3 + class fix.
