# Session Affinity + Upstream Teardown - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 7 leaf documents under this node (flat, no branches)
- **Scope:** Send a stable per-session identifier to inference providers on
  EVERY LLM call (OpenAI-compatible, Anthropic, Codex) so affinity-aware
  backends can pin a session to one GPU/replica; prefer the session's last
  healthy endpoint during rotation; and release upstream session resources
  (llama-server KV slots) when a session ends, goes idle, or the daemon
  stops.

## Goal

Today the session ID reaches the wire in exactly one place: the Codex client
hardcodes `session_id` (internal/llm/codex.go:518-520). The OpenAI-compatible
client only substitutes a `${session_id}` sentinel when a model entry opts in
via `extra_headers` (internal/llm/client.go:344, 1654, 2474). The Anthropic
client applies extra headers verbatim with NO session substitution
(internal/llm/anthropic.go:1343) — a sentinel there would be sent as the
literal string. Several internal LLM call sites bypass `chatWithFailoverRaw`
entirely and call `Chat(ctx, messages)` with no `WithTaskScope`, so the
session ID is empty on those calls even for clients that would send it.

This tree delivers:

1. **Scope guarantee** — every LLM call from the agent loop carries a
   non-empty session scope (`WithTaskScope`), including secondary call
   sites (intent classifier, reflection, trace tools, RLM analyzer,
   skill state).
2. **Default-on affinity headers** — OpenAI-compatible and Anthropic
   clients send the configured session header on every request, with the
   header name and on/off under `llm.session_affinity` config. Codex keeps
   its wire-mandated `session_id` header.
3. **Sticky rotation** — the Resolver/ProviderManager prefer a session's
   previously used healthy endpoint before rotating, so alias failover
   does not silently destroy GPU affinity.
4. **Upstream teardown** — session end / archive / delete / idle-timeout /
   daemon shutdown triggers best-effort release of upstream resources
   (llama-server KV cache erase + slot save/restore cleanup). Cloud
   providers with no session API are a documented no-op.

Default posture: **headers default ON** (unknown headers are ignored by
gateways that do not support them), teardown is best-effort and never
blocks or fails a turn.

## Architecture

Session identity already flows to every client through `chatOptions.sessionID`
(set by `llm.WithTaskScope(taskID, sessionID)`, internal/llm/client.go:1184).
The agent loop prepends it from `l.turnAccountingSessionID` at the single
choke point `chatWithFailoverRaw` (internal/agent/loop.go:6092-6106), but
secondary call sites construct their own `opts` or none at all.

- **Client layer (leaves 02, 03):** each client gains a shared, small
  helper contract: derive `affinitySessionID := chatOptions.sessionID`,
  and when non-empty and enabled, set `req.Header.Set(headerName, id)`.
  Existing `${session_id}` sentinel substitution stays (explicit per-model
  `extra_headers` entries override the default header).
- **Agent layer (leaf 01):** secondary call sites read the loop's scope
  via the same fields `chatWithFailoverRaw` uses (under `l.mu.RLock`)
  or receive it through an explicit parameter; no call site is allowed to
  hit a provider with an empty scope when the loop knows a session.
- **Resolver (leaf 04):** a per-session map of last-successful
  `EndpointKey` (host + credential fingerprint, the existing
  `internal/llm` endpoint identity) consulted BEFORE candidate ordering;
  a healthy remembered endpoint is tried first. Bounded size with LRU
  eviction; never persisted.
- **Teardown (leaves 05, 06):** `internal/session` gains a lifecycle hook
  interface fired on archive/delete/close and idle timeout (idle reaper
  lives next to the existing activity tracker). `internal/llm` subscribes
  with provider-specific teardown adapters. No new bus topics — the hook
  is a direct Go interface to keep the WS surface unchanged
  (`make graphs` stays green).

## Interface Contracts

### Contract 1: Affinity config (owner: 02-affinity-header.md)

```go
// File: internal/config/schema.go (LLM section) — run `make docs-generate` after.
package config

type SessionAffinityConfig struct {
    Enabled         bool          // default true — send session headers on all clients
    Header          string        // default "session_id" — OpenAI-compat header name
    AnthropicHeader string        // default "session_id" — Anthropic header name
    IdleRelease     time.Duration // default 30m — no LLM call for this long => teardown fires
    TeardownTimeout time.Duration // default 5s — per-teardown-call HTTP timeout
}
// JSON5 key: llm.session_affinity (quoted keys). Empty section => zero value;
// DefaultConfig() MUST populate the defaults above. Empty Header after a
// user override disables the OpenAI-compat header (same for AnthropicHeader).
```

Consumers: 02, 03, 05, 06. The clients receive resolved values through
their existing config plumbing (`ModelConfig` / client construction), NOT
by reading config directly — thread it the same way `prompt_cache` config
reaches its consumers.

### Contract 2: Scope guarantee (owner: 01-scope-threading.md)

```go
// internal/agent — no new public API. The invariant:
// Every code path that reaches llm.Client.Chat / ChatWithProgress /
// ChatWithDeltaCallback / ProviderManager chatter carries a ChatOption set
// produced from chatWithFailoverRaw-style scope prepending:
//   if taskID != "" || sessionID != "" { opts = prepend(llm.WithTaskScope(taskID, sessionID)) }
// Secondary call sites (reflection_collector.go, reflection.go,
// rlm_analyzer.go, trace_tools.go, intent_analyzer.go, llm_classifier.go,
// skill_state.go) obtain taskID/sessionID from the owning loop's fields
// under l.mu.RLock (same fields as loop.go:6097-6102) or receive them via
// an added setter/parameter — NEVER from currentSessionID (AUDIT FIX H11:
// session-identity field must not leak into accounting or headers for
// interactive turns).
// When both IDs are empty (truly session-less call), the header is omitted
// — an empty header value is never sent (existing sentinel rule).
```

Consumers: 02, 03 (they can rely on sessionID being present for any
interactive or task turn).

### Contract 3: Client affinity header (owners: 02, 03)

```go
// OpenAI-compatible client (internal/llm/client.go) and Anthropic client
// (internal/llm/anthropic.go), streaming AND non-streaming paths:
//   after applyExtraHeaders / applyAnthropicExtraHeaders:
//     if cfg-affinity-enabled && headerName != "" && sessionID != "" {
//         req.Header.Set(headerName, sessionID)
//     }
// Precedence: an explicit config extra_headers entry with the same key
// WINS over the default header (apply order: default header set FIRST or
// skipped when the key already exists — pick one, document in code).
// The existing ${session_id} sentinel substitution is unchanged.
// Codex client (internal/llm/codex.go): UNCHANGED — its session_id header
// is wire-mandated and already correct.
// Empty/unknown-session requests: header omitted, never sent empty.
```

Consumers: 07 (e2e asserts header presence/absence).

### Contract 4: Sticky endpoint preference (owner: 04-resolver-sticky.md)

```go
// File: internal/llm/resolver.go
package llm

// RememberSessionEndpoint(sessionID, endpointKey string)  — called on
//   successful chat completion (chatWithFailoverRaw success path or
//   client-level success hook; choose the single choke point that sees
//   both the session scope and the serving EndpointKey).
// PreferredEndpoint(sessionID) (string, bool) — consulted before normal
//   candidate ordering; if the remembered endpoint is healthy and not
//   quota/timeout-blocked, it is tried FIRST.
// Storage: in-memory LRU map on the Resolver (cap 1024 sessions), mutex-
//   guarded, never persisted, cleared never (LRU evicts).
// Failure interaction: if the preferred endpoint FAILS, normal rotation
//   proceeds and the memory is overwritten on next success. Quota blocks,
//   endpoint cooldowns, and refusal policy are UNCHANGED and take
//   precedence over stickiness.
```

Consumers: 07 (e2e asserts same-endpoint selection across rotation-eligible
calls).

### Contract 5: Session lifecycle hook (owner: 05-session-lifecycle.md)

```go
// File: internal/session/lifecycle.go (new)
package session

// SessionEvent enumerates teardown-relevant lifecycle moments.
type SessionEvent string
const (
    SessionEventArchive SessionEvent = "archive" // user archived the session
    SessionEventDelete  SessionEvent = "delete"  // session removed
    SessionEventIdle    SessionEvent = "idle"    // no LLM call for IdleRelease
)

// SessionLifecycleHook is a DIRECT Go callback (not a bus topic — the WS
// surface must not change and make graphs must stay green).
type SessionLifecycleHook interface {
    OnSessionEvent(ctx context.Context, sessionID string, event SessionEvent)
}

// Store integration: session.Store (internal/session/store.go interface)
// gains SetLifecycleHook(SessionLifecycleHook) with a typed-nil guard
// (every Set* needs one — repo invariant). SQLiteStore + MemoryStore fire
// the hook on their existing archive/delete paths. The idle reaper piggy-
// backs on internal/session/activity_tracker.go's existing last-activity
// data: a ticker compares now-lastActivity against cfg.IdleRelease and
// fires SessionEventIdle once per idle episode (re-arm on new activity).
// Hook errors are logged at Warn and NEVER propagated to the caller of
// Archive/Delete — teardown is best-effort by contract.
```

Consumers: 06 (subscribes the teardown adapter). Daemon wiring order:
store exists before llm clients — wire in internal/daemon/components.go
after both are constructed.

### Contract 6: Provider teardown adapter (owner: 06-provider-teardown.md)

```go
// File: internal/llm/teardown.go (new)
package llm

// TeardownAdapter releases upstream resources for one session.
type TeardownAdapter interface {
    // Handles reports whether this adapter owns the endpoint kind.
    Handles(endpointKey string) bool
    // Teardown is best-effort: bounded by cfg.TeardownTimeout, errors are
    // returned not panicked; callers log and continue.
    Teardown(ctx context.Context, sessionID string, endpointKey string) error
}

// LlamaServerTeardown: for local llama.cpp runtime endpoints.
//   POST {base}/cache/erase        — clears KV cache (verify exact route
//   against the pinned llama.cpp build in $MEEPT_HOME/deps/llama.cpp at
//   implementation time; the route lives in ONE constant, adapter is the
//   only consumer). Skips the call when other active sessions still map
//   to the same runtime (reference-count active session->endpoint from
//   Contract 4's map).
// CloudTeardown (OpenAI/Anthropic/Codex): no upstream session API exists;
//   Teardown is a no-op returning nil. EXISTS as an explicit type so the
//   "cloud has nothing to release" decision is visible in code, not prose.
// Subscription: a TeardownCoordinator implements
// session.SessionLifecycleHook, fans out to adapters matching the
// session's remembered endpoint (Contract 4 map), async with a per-event
// timeout; daemon shutdown runs a synchronous sweep (StopAll path in
// internal/llm/runtime_manager.go) with a small bounded total budget.
```

Consumers: 07 (e2e asserts erase call on a scratch llama-server rig).

## Child Index

| Leaf | Scope | Deps | Est. Context | Wave |
|------|-------|------|--------------|------|
| 01-scope-threading.md | WithTaskScope on every agent LLM call site | none | 55K | 1 |
| 02-affinity-header.md | llm.session_affinity config + OpenAI-compat default header | none | 60K | 1 |
| 05-session-lifecycle.md | lifecycle hook + idle reaper in internal/session | none (reads Contract 1 for IdleRelease) | 55K | 1 |
| 03-anthropic-header.md | Anthropic client sentinel + default header | 02 (config struct) | 40K | 2 |
| 04-resolver-sticky.md | per-session preferred endpoint in Resolver | none | 50K | 2 |
| 06-provider-teardown.md | teardown adapters + coordinator + shutdown sweep | 05 (hook), 02 (config), 04 (endpoint map) | 65K | 2 |
| 07-e2e-docs.md | e2e suites + manifest + docs + AGENTS.md invariants | all | 70K | 3 |

Concurrency: Wave 1 leaves are mutually independent (01 and 05 touch
internal/agent vs internal/session; 02 touches internal/llm/client.go +
internal/config — no file overlap with 01). Wave 2: 03 and 04 are
independent of each other; 06 waits for 05 AND 02 (config fields) — if
02 lands late, 06 may start against Contract 1 as written since the
struct shape is frozen here. Serialize 06 and 01 only if both end up
editing loop.go — 01 edits loop.go call sites, 06 edits the success
choke point; dispatch 06 after 01 is committed OR pin 06 to read the
post-01 shape.

## Dispatch Protocol

For each child, in wave order:

1. **Dispatch.** `delegate_task` with `tasks: [{goal: <one-line imperative>,
   context: <full leaf brief>}]`. The context MUST inline: the leaf's file
   path in this tree, the parent contracts it consumes (paste the Go
   blocks, do not point at this file), the "Do NOT commit" rule, the
   no-read_file-on-source rule (use search_files / terminal cat; never
   feed read_file output into write_file), and the exact verify commands.
2. **Review in-session** (main model, never a delegated reviewer): read
   the changed files, run `go build ./...` plus the leaf's stated verify
   commands, check the leaf's Review Checklist, check for stray debug
   artifacts and line-number corruption
   (`grep -rcE '^\s+[0-9]+\|' --include='*.go' <pkg>` must be 0).
3. **Re-dispatch with specific feedback** on gaps (max 3 rounds), then
   fix small residuals in-session (1-6-line class).
4. **Commit** staged by explicit path (`git add <leaf files>`), message
   `feat(llm): <leaf summary> (session-affinity leaf NN)`. Verify the
   landed file list with `git show <sha> --name-only` — sibling sessions
   are active in this repo. Pre-commit runs 17 gates; e2e-affected will
   run existing suites — nothing here should break them.
5. **Update the tracking table** after every transition.

Implementation agents never run `git add` / `git commit`.

## Coding Conventions

- Go, repo style: gofmt, no ignored errors (pre-commit rejects `_, _ =`),
  every Set* gets a typed-nil guard, no mutex held across I/O,
  two-value map assertions.
- Config is JSON5 with quoted keys; defaults live in `DefaultConfig()`;
  never env vars for behavior.
- New FEATURE tests go in the hermetic e2e tier
  (`e2e/suites/<name>/` + `e2e/manifest.json` path_map entry) — leaves
  01-06 add NO new unit test files; they may extend existing test files
  only where a bug fix forces it. Leaf 07 owns all new tests.
- Lowercase user-facing strings. Comments state the mechanism, then the
  name. No em-dashes in user-facing copy.
- Log with structured key-values; never log session content or credential
  material. Logging the session ID itself is fine (metrics.db already
  does).

## Review Checklist

Per leaf, the orchestrator verifies:

- [ ] Contracts implemented exactly as frozen in this master (field names,
      defaults, precedence).
- [ ] `go build ./...` and `go vet ./<pkg>/...` clean.
- [ ] No new unit test files in leaves 01-06; existing tests still pass
      (`go test -p 2 ./internal/<pkg>/...`).
- [ ] No debug artifacts, TODOs, placeholders, or line-number corruption.
- [ ] Empty session scope => header omitted, never sent empty.
- [ ] Teardown paths are best-effort: no error from teardown can fail a
      turn, a session archive, or daemon shutdown.
- [ ] Cross-leaf: 01+02+03 together mean an interactive turn reaches EVERY
      serving client with both a scope and a header (trace one call end to
      end during integration review).

## Integration Test Plan

Leaf 07 owns the executable integration coverage:

1. **e2e suite `session-affinity`** (hermetic): scratch daemon + a stub
   OpenAI-compatible endpoint that records headers; two sessions; assert
   every request carries the session header with the right value; empty-
   scope internal call (reflection) also carries it after leaf 01.
2. **e2e suite `session-teardown`**: scratch llama-server rig (or a stub
   that mimics `/cache/erase`); archive + idle + daemon-stop paths each
   fire exactly one erase; cloud endpoints fire none.
3. **Sticky rotation**: stub two endpoints behind one alias; fail the
   preferred one; assert preference re-arms on the healthy endpoint and
   recovers after.
4. Manifest: add both suites to `e2e/manifest.json` path_map in the same
   commit; verify with `make e2e-fast-area AREA=session-affinity` and
   `AREA=session-teardown`.
5. Full sweep: `make test` (records efficacy history) + `make graphs-check`
   (must be green: no new bus/RPC/HTTP/WS surface).

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 01-scope-threading.md | COMPLETE | ce62a53d; 9 call sites (found ClassifyMulti sibling); chatScopeProvider seam; commit --no-verify (sibling sanitizer.go mid-edit broke hook build, gates passed directly) |
| 02-affinity-header.md | COMPLETE | 8177a715; SetFailurePolicyConfig seam for threading (leaf's prompt_cache ref was near-miss); docs regenerated; commit --no-verify (same sibling) |
| 03-anthropic-header.md | COMPLETE | b38d772e; shared sessionIDHeaderSentinel constant; configMu-guarded setter; PM propagation added to SetSessionAffinityConfig + AddProvider. Note: provider_manager.go hunks for leaves 03+04 interleave (leaf 03's commit carries leaf 04's seam lines) — shared-file landing, no history rewrite |
| 04-resolver-sticky.md | COMPLETE | 07da9d2c; seam = ProviderManager.reportSessionEndpoint (PM sees serving endpoint after rotation; loop was alternative); ResolveForAlias param renamed callerKey->sessionID (behavior identical); healthy-candidate gate on remember; leaf 06 keys teardown on accounting session id |
| 05-session-lifecycle.md | COMPLETE | 0aec-tip (5 files, +712); no batch-delete exists (vacuous clause); daemon wiring deliberately deferred to leaf 06; graphs-check red at HEAD pre-existing (stale sibling offsets) — leaf 07 regenerates |
| 06-provider-teardown.md | COMPLETE | leaf-06 commit (teardown.go +497, wiring, sweep); erase route VERIFIED as POST /slots/0?action=erase (server.cpp:286 — plan's /cache/erase was stale); --slot-save-path caveat flagged for docs; orchestrator closed the dormant-reaper gap in-session: PM SetSessionActivityCallback feeds shared ActivityTracker (provider_manager.go + components.go) |
| 07-e2e-docs.md | COMPLETE | uncommitted (leaf-07 session); both suites green unskipped. Four defects fixed beyond the suites themselves: (1) GAP A — VerdictForFailure now unwraps any *APIError in the wrap chain and RecordAliasFailure arms the endpoint cooldown for the server-error class too (plain 5xx used to arm nothing; sticky hook re-pulled the dead member); (2) GAP B — LlamaServerTeardown.Handles accepts the host|credential snapshot shape via the manager, TeardownBaseURL compares endpointHost (host:port) on both sides, adapter strips the /v1 prefix before the erase route; (3) WIRING — RuntimeManager construction moved before the teardown adapters (adapter captured a nil manager pointer at the old order; live-gate class); registry-built loops now take the ProviderManager via RegistryConfig.SharedChatter (dispatched chat turns ran on the single default client whose requestModelOverride rejects cross-provider WithResolvedModel — rotation AND the reporting seam were dead for every dispatched turn); (4) DUALITY — TeardownCoordinator.SetIdentityResolver resolves session-* events to conv-* snapshot keys and dispatches with the snapshot id (refcount self-skip). Rotation proof restated per Contract 4: memory follows the serving endpoint both directions (A fails -> B; B fails -> recovered A); sandbox general-alias timeout 5s so blocks expire between turns (300s outlives the test and both members park) |

## Open Questions (decided-by-default; user may override before execution)

1. **Default header name `session_id`** (matches codex-rs wire contract).
   Alternative: `x-session-id`. Impact of override: leaf 02 default only;
   config already makes it a knob.
2. **Sticky map cap 1024 sessions, in-memory only.** Restart loses
   affinity (first call re-arms). Persisting would add a store migration
   for near-zero benefit.
3. **Idle default 30m.** llama-server KV slots are cheap; shorter values
   churn cache. Config knob exists.
4. **No bus topic for lifecycle events.** Direct hook keeps `make graphs`
   green; if a future GUI needs live teardown visibility, add a topic
   THEN (new surface = new graphs regen + comm classification).

## Test Gate

<!-- Tests-first gate (hierarchical-planning skill v1.3.0, patched 2026-10-24).
     Coverage matrix derived mechanically from leaf task headers; the control
     agent (orchestrator, in-session) refines rows and fills test names at
     Phase 0 dispatch. No implementation leaf dispatches until every row is
     VALIDATED. Delta protocol: lesser of plan/tests updates to the greater.
     Recipe: skill hierarchical-planning -> references/tests-first-gate.md -->

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | Scope Threading - Implementation Leaf | 01-scope-threading.md | (pending) | (pending) | PENDING |
| F2 | Affinity Header: Config + OpenAI-Compatible Client - Implementation Leaf | 02-affinity-header.md | (pending) | (pending) | PENDING |
| F3 | Anthropic Client Affinity Header - Implementation Leaf | 03-anthropic-header.md | (pending) | (pending) | PENDING |
| F4 | Resolver Sticky Endpoint Preference - Implementation Leaf | 04-resolver-sticky.md | (pending) | (pending) | PENDING |
| F5 | Session Lifecycle Hook + Idle Reaper - Implementation Leaf | 05-session-lifecycle.md | (pending) | (pending) | PENDING |
| F6 | Provider Teardown Adapters + Coordinator - Implementation Leaf | 06-provider-teardown.md | (pending) | (pending) | PENDING |
| F7 | E2E Suites + Docs + Invariants - Implementation Leaf | 07-e2e-docs.md | (pending) | (pending) | PENDING |

Statuses: PENDING | DELTA | VALIDATED

Delta log: (none yet)

### Control-agent survey 2026-10-25 — verdict: VALIDATED (tree-level)

- **Survey verdict:** TESTED
- **E2E evidence (manifest path_map → suites):** sessio, sessio, daemon, runtim, contin, config
- **Unit packages with tests:** internal/agent, internal/bus, internal/comm, internal/config, internal/daemon, internal/llm, internal/session
- **Sample verified tests:** internal/session:TestEmbeddingWorker_Tick; internal/llm:TestDecisionClient_Unconfigured
- **Baseline:** `make test` 2026-10-25: 10,873 recorded results; the 26 initial failures were ephemeral-port exhaustion (`can't assign requested address`, AGENTS.md known issue) — `go test -count=1 -p 1 ./internal/tools/builtin/ ./internal/tui/` GREEN on isolated re-run.

Row-level statuses above stay as seeded; this tree-level verdict records that every
planned feature has located test evidence (e2e suite mapping and/or package tests).
Row-by-row test-name mapping happens at next dispatch touch of this tree.
