# Quota-Reset Resilience - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 10 leaf documents under this node
- **Scope:** Detect provider quota/usage-limit errors, block the affected provider-key temporarily, keep agent tasks moving via fallback rotation, and surface quota state to users with auto-resume.

## Goal

Meept users on quota-plan providers (z.ai, OpenAI/Codex, Anthropic, xAI/Grok,
Gemini, Tencent, OpenRouter, and other OpenAI-compatible gateways) hit
multi-hour usage windows. Today those errors are not distinguished from other
failures: a quota hit fails the request, and when all models in rotation share
the quota system, every agent task cascades into failure.

This tree delivers:

1. **Recognition** — a structured `QuotaResetError` parsed from HTTP 429/402
   responses (structured body fields and rate-limit headers only; no fuzzy
   string/timezone matching).
2. **Provider-key quota blocking** — the Resolver marks the affected
   provider+credential as blocked until the reset time, skips blocked
   candidates during alias rotation, and re-probes after the block expires.
   The Resolver (not the broker) is the production alias component;
   ProviderManager gets the same semantics for direct-provider aliases.
   Broker is dead in production and kept only for test compatibility.
3. **Task continuity** — agent tasks fail OVER, not down. Rotation continues
   on unblocked providers; when everything is blocked, the dispatcher defers
   the task and auto-resumes at the reset time. Agent state gains
   `quota_wait` (auto-resume) and `blocked` (24h soft-stop exceeded).
4. **Visibility** — quota state is visible in the TUI and Flutter GUI agent
   lists, with per-provider-key deduplicated push notifications and a soft
   escalation ladder (12h / 20h / 24h).

Default posture: **retry even on billing-shaped errors** — the user may top up
mid-window and queued work should continue. The 24h cap is a soft stop with
escalating notifications, not a hard failure.

## Architecture

Recognition lives in `internal/llm/errors.go` (new `QuotaResetError` +
  `ParseQuotaResponse`), integrated at the two HTTP error sites: the
  OpenAI-compat client `doRequest` (`internal/llm/client.go`, also used by
  the streaming path) and the Anthropic client `doRequest`
  (`internal/llm/anthropic.go`). State lives on the `Resolver`
  (`internal/llm/resolver.go`): per-entry quota blocks plus a
  provider-key-level block map keyed by a credential fingerprint, both
  guarded by the resolver mutex using collect-under-lock patterns. The
  existing alias rotation order (priority-ordered `Models` list) IS the
  fallback chain — no new alias config; quota blocks temporarily remove
  entries from candidacy and the existing `SmallModel` fallback remains the
  last resort. Direct calls also pass through the Resolver, so the same
  blocking logic applies there. ProviderManager gets the same semantics for
  multi-provider setups.

Up-stack, the agent loop (`internal/agent/loop.go`) gains a quota-episode
tracker and a new `QuotaResetError` branch (before the existing
`RateLimitError` handler) that triggers deferral/auto-resume instead of
burning backoff attempts. The agent state machine gains
`quota_wait` / `blocked` states. Notifications flow over the bus to the
push service with per-provider-key dedup, and the comm layer classifies
the new topic so WS clients render it as `agent_progress`, never as a chat
bubble.
## Interface Contracts

### Contract 1: QuotaResetError

```
// File: internal/llm/errors.go
package llm

// QuotaResetError is a recoverable-by-waiting provider cap: a subscription
// usage window, plan quota, or billing exhaustion. Distinct from
// RateLimitError (seconds-scale backoff). The client retry loop must NOT
// short-retry it; the broker treats it as rotate-and-block.
type QuotaResetError struct {
    ProviderID  string
    ModelID     string
    Code        string        // "usage_limit_reached", "insufficient_quota", "quota_exceeded", ...
    Message     string        // raw body detail, truncated
    ResetAt     time.Time     // absolute reset time; zero = unknown
    RetryAfter  time.Duration // derived wait; min(ResetAt-now, MaxWait) when known
    MaxWait     time.Duration // upper bound from config (default 24h)
    StatusCode  int           // 429 or 402
    Cause       error
}

func (e *QuotaResetError) Error() string
func (e *QuotaResetError) UserMessage() string
func (e *QuotaResetError) Unwrap() error
func IsQuotaResetError(err error) bool            // errors.As-based
func AsQuotaResetError(err error) (*QuotaResetError, bool)

// ParseQuotaResponse classifies an HTTP error response as a quota reset.
// statusCode must be 429 or 402; anything else returns nil.
// Precedence: structured body fields -> rate-limit headers -> nil.
// Unknown reset time => zero ResetAt (caller applies config default estimate).
func ParseQuotaResponse(statusCode int, header http.Header, body []byte) *QuotaResetError
```

Owner: 01-quota-error-type.md.
Consumers: 03, 04, 05, 06, 07.

### Contract 2: Credential fingerprint

```
// File: internal/llm/errors.go (or errors_quota.go in package llm)
package llm

// QuotaCredentialKey returns a stable identity for a provider credential:
//   literal apiKey  -> providerID + ":key:" + first 12 hex of sha256(apiKey)
//   apiKeyEnv set   -> providerID + ":env:" + envVarName
//   OAuth provider  -> providerID + ":oauth:" + oauth account/provider id
// Never returns the raw key material. Two ModelConfigs sharing one billing
// pool MUST produce the same value.
func QuotaCredentialKey(providerID string, cfg *ModelConfig) string
```

Owner: 01-quota-error-type.md.
Consumers: 03, 04, 05, 07. Verify actual `ModelConfig` credential fields
(`APIKey`, `OAuthProvider`, env-based key resolution) before implementing.

### Contract 3: Quota retry config

```
// File: internal/config (struct) + config/models.json5-adjacent docs
package config

type QuotaRetryConfig struct {
    Enabled           bool          // default true
    MaxWait           time.Duration // default 24h; block/defer upper bound
    DefaultEstimate   time.Duration // default 1h; used when reset time unknown
    DeferCheckInterval time.Duration // default 10m; requeue poll cadence
}
// Config key: llm.quota_retry (JSON5, quoted keys)
```

Owner: 02-quota-config.md.
Consumers: 03, 04, 05, 06.

### Contract 4: Resolver quota blocking

```
// File: internal/llm/resolver.go
package llm

// On QuotaResetError during ResolveForAlias / RotateToNextModel / HasHealthyModels:
//   - compute wait = min(resetHorizon, MaxWait), or DefaultEstimate when
//     reset unknown
//   - mark entryBlocks[entryKey] = now+wait AND credentialBlocks[key] = now+wait
//     (entryKey = "provider/model"; key = QuotaCredentialKey(cfg))
//   - exclude blocked entries from candidate selection (resolve to next
//     available, rotate past blocked entries without burning backoff)
//   - log Warn with provider/model/key/unblock_at (NO raw key material)
//   - do NOT call RecordAliasFailure — quota is not a health failure; the
//     alias cooldown is orthogonal (separate state fields)
//
// On success after rotating past a quota-blocked entry:
//   - lazy-clear the expired block; failed probe re-blocks.
//
// New public methods:
func (r *Resolver) QuotaBlockedUntil(credentialKey string) time.Time
func (r *Resolver) ActiveQuotaBlocks() []QuotaBlockStatus
//   QuotaBlockStatus = {AliasName, ProviderID, ModelID, CredentialKey, BlockedUntil}
```

Owner: 03-resolver-quota-blocks.md.
Consumers: 04, 05, 06, 08, 09.

### Contract 5: Direct-call wait decorator

```
// File: internal/llm/resolver.go (or resolver_direct.go, package llm)

// ChatterForModel wraps the returned Chatter: on QuotaResetError, if the
// computed wait <= MaxWait AND the caller ctx has no earlier deadline,
// sleep ctx-aware until the wait elapses, then retry ONCE. Otherwise return
// the QuotaResetError immediately (annotated with unblock time). Config-
// gated by llm.quota_retry.enabled. No retry loop — exactly one wait+retry.
// Same logic as Resolver.resolveAliasWithQuotaCheck; no separate decorator.
```

Owner: 04-direct-call-wait.md. Depends on 01, 02.

### Contract 6: Agent quota states and episode tracker

```
// File: internal/agent (exact file chosen by leaf after exploration)
package agent

// New agent states (exported constants alongside existing states):
//   AgentStateQuotaWait = "quota_wait"  // auto-resume scheduled
//   AgentStateBlocked   = "blocked"     // 24h soft-stop exceeded; human action
//
// QuotaEpisodeTracker (per provider-key, per daemon):
//   Enter(providerKey, unblockAt)   — transitions running -> quota_wait,
//                                     starts escalation timers (12h/20h/24h)
//   Clear(providerKey)              — quota lifted; quota_wait -> running
//   Escalations emit bus events; the 24h boundary transitions to blocked.
// Escalation/notification dedup is per provider-key for the life of the
// block; a cleared-then-reblocked provider starts a fresh episode.
```

Owner: 05-agent-quota-state.md. Depends on 03.

### Contract 7: Dispatcher deferral

```
// File: internal/agent (dispatcher/executor — exact file chosen by leaf)
package agent

// When a task step returns QuotaResetError and NO unblocked candidate
// remains (broker returns the error after exhausting rotation):
//   - defer the task (do not fail it): record provider-key, unblock time,
//     and a step checkpoint so resume continues at the failed step
//   - schedule requeue via internal/scheduler at min(unblockAt, now+MaxWait)
//     with DeferCheckInterval polling
//   - resume re-dispatches the task; if the original model is unblocked it
//     is used again (auto-revert); otherwise rotation/fallback applies
//   - at MaxWait: task transitions with its agent to blocked
// Task run metadata records every model switch: step number, from-model,
// to-model (partial-success transparency; see design note in 06).
```

Owner: 06-dispatcher-deferral.md. Depends on 03, 05.

### Contract 8: Quota bus event + notification

```
// Bus topic: agent.quota_wait  (published by 05's tracker on transitions)
// Payload keys (string-typed, bus convention):
//   agent_id, task_id, from, to, reason="quota_blocked",
//   provider_id, credential_key, model_id, unblock_at (RFC3339), 
//   fallback_model (set when rotation switched models), escalation ("12h"|"20h"|"24h"|"")

// Notification: internal/services push — one notification per provider-key
// per episode; escalations re-notify at 12h/20h/24h boundaries only.
// internal/comm/http/server.go transformBusEventToWS: agent.quota_wait MUST
// classify as type "agent_progress" (never "chat_message").
```

Owner: 07-notifications.md. Depends on 05.

### Contract 9: Surfaces (TUI + GUI parity)

```
// TUI (internal/tui) and Flutter GUI (ui/flutter_ui/lib) agent lists:
//   - quota_wait rendered as a DISTINCT state from running/error/blocked
//   - countdown text: "quota resets in 3h 12m" (from unblock_at)
//   - blocked rendered distinctly with "action required"
//   - when a fallback model is active: show primary (blocked until T) and
//     active model on separate lines
// Parity is an AGENTS.md requirement; both leaves implement the same
// information. Lowercase UI text per convention.
```

Owners: 08-tui-quota-status.md, 09-gui-quota-status.md. Both depend on 05, 07.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-quota-error-type.md | leaf | none | 80K | A |
| 02 | 02-quota-config.md | leaf | none | 40K | A |
| 03 | 03-resolver-quota-blocks.md | leaf | 01, 02 | 75K | B |
| 04 | 04-direct-call-wait.md | leaf | 01, 02 | 50K | B |
| 05 | 05-agent-quota-state.md | leaf | 03 | 70K | C |
| 06 | 06-dispatcher-deferral.md | leaf | 03, 05 | 70K | D |
| 07 | 07-notifications.md | leaf | 05 | 60K | E |
| 08 | 08-tui-quota-status.md | leaf | 05, 07 | 60K | F |
| 09 | 09-gui-quota-status.md | leaf | 05, 07 | 70K | F |
| 10 | 10-docs-and-invariants.md | leaf | all | 30K | G |

**Concurrency groups:** same letter = no inter-dependencies, dispatch
together (max 3 per batch).

## Dispatch Protocol

For each concurrency group, in dependency order:

### Phase 1: Dispatch Concurrency Group [A]

Dispatch these children simultaneously:

1. **Read** 01-quota-error-type.md and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 01-quota-error-type.md"
   - Context: full leaf text + Contracts 1-2 + coding conventions below +
     the current contents of internal/llm/errors.go, client.go (429 section
     ~lines 985-1035), anthropic.go (retry loop ~lines 250-280, doRequest
     ~lines 920-1040) INLINED
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests,
     report results only."
   - Include: "Do NOT use read_file on existing source files — explore with
     search_files or terminal cat instead. If you read a file, never feed its
     output into write_file."

2. **Read** 02-quota-config.md and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 02-quota-config.md"
   - Context: full leaf text + Contract 3 + coding conventions + current
     internal/config LLM config section INLINED
   - Include the same no-commit / no-read_file clauses.

### Phase 2: Review and Commit Each Child

After each implementation agent returns, the orchestrator reviews in-session
(the main model reviews directly, NOT a delegated subagent):

1. **Orchestrator reviews in-session:**
   - Read the changed files (from the implementer's file list)
   - Check against leaf spec + interface contracts + Review Checklist below
   - Run the leaf's specified test commands (scope with -run; internal/llm
     full-suite runs take 80-90s)

2. **If review finds gaps:** re-dispatch with specific feedback; max 3
   cycles, then escalate (halt and report).

3. **If review passes:** `git add <exact paths> && git commit -m
   "feat(llm): <leaf summary>"`. Update tracking table to REVIEWED.

### Phase 3: Remaining Groups

Dispatch in order: B (03, 04) → C (05) → D (06) → E (07) → F (08, 09) →
G (10). Same per-child protocol: dispatch with leaf + contracts + inlined
relevant source; review in-session; commit per leaf.

### Phase 4: Integration Review

After ALL children reach REVIEWED:

1. Run full `go build ./...` and `go test ./internal/llm/... ./internal/agent/... ./internal/config/... ./internal/comm/... -count=1`.
2. Verify contracts: QuotaResetError flows from both clients to Resolver
   block to agent state to notification to both UIs.
3. Run `make lint-ci` (golangci-lint + mutexio + predid + audit scripts).
4. Normalize formatting (`gofmt`), verify no line-number corruption:
   `grep -rcE '^\s+[0-9]+\|' --include='*.go' --include='*.dart' .` returns zero.
5. Commit integration changes; update tracking table to COMPLETE.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts from this orchestrator are satisfied
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions (see Coding Conventions below)
- [ ] No scope creep (nothing beyond spec)
- [ ] No obvious bugs or security issues (especially: no raw key material in
      logs, fingerprints only)
- [ ] No debug artifacts: no print debugging, no TODOs, no placeholder values,
      no commented-out code
- [ ] No line-number corruption: no `     N|` prefixes baked into source files

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language:** Go 1.22+ (server), Dart/Flutter (GUI leaf only)
- **Error handling:** wrap with `%w`; never `_ = err()`; two-value type
  assertions on `map[string]any` bus payloads
- **Mutex scope:** never hold a mutex across I/O; collect-under-lock then
  operate (mutexio analyzer enforces; use `//nolint:mutexio` with explanation
  only for IIFE-scope false positives)
- **Setters:** every `Set*` method gets a nil guard (setters_test.go pattern)
- **IDs:** never `time.Now().UnixNano()`/`math/rand`; use `pkg/id.Generate()`
- **Config:** JSON5 with quoted keys; behavioral settings in config, not env
- **Tests:** table-driven where natural; `_test.go` alongside; scope
  internal/llm runs with `-run` (full suite is 80-90s)
- **UI text:** all lowercase (both TUI and Flutter)
- **Docs:** `internal/<pkg>` changes map to `docs/workflows/<pkg>.md`
  (leaf 10 consolidates)
- **Formatting:** `gofmt` before reporting completion

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-quota-error-type | COMPLETE | 2 | Type/parser OK from first pass; Task 4 client integration was missing and is now implemented (both clients classify 429 quota shapes + 402; all retry loops early-exit; regression tests in quota_client_test.go) |
| 02-quota-config | COMPLETE | 0 | Defaults in DefaultConfig + Normalize; getter surface added for ConfigFromSchema |
| 03-resolver-quota-blocks | COMPLETE | 2 | Maps/readers/skip from first pass; fixed credential-block rotation, ErrAllModelsQuotaBlocked, quota-aware RotateToNextModel, lazy-clear, PM persistence + probe integration |
| 04-direct-call-wait | COMPLETE | 0 | quotaWaitChatter correct; wired at broker + config plumbing via ConfigFromSchema |
| 05-agent-quota-state | COMPLETE | 2 | Tracker from first pass; daemon construction, ConfigSnapshot/registry mirroring, state-machine hook (SetStateSetter), transition-table entries, success-path Clear, reaper, initial event added |
| 06-dispatcher-deferral | COMPLETE | 1 | Turn-level deferral via QuotaResumeWatcher + ChatHandler parking (mirrors BudgetResumeWatcher). DEVIATIONS: task-checkpoint resume and model-switch metadata in task records not implemented (no task plumbing exists); escalation vocabulary is ""/warn/action_recommended/blocked, not 12h/20h/24h strings |
| 07-notifications | COMPLETE | 1 | Notifier from first pass; pump goroutine + Start/Stop, dedup resets on quota_cleared, cleared-event routing, task_count-absent handling, WS pin test added |
| 08-tui-quota-status | COMPLETE | 1 | Badges/countdown/detail lines; formatter aligned to llm.FormatDuration semantics; episode state clears on running |
| 09-gui-quota-status | COMPLETE | 1 | Flutter parity: QuotaStatusBadge, shared countdown, AgentProgress quota fields, malformed-payload safety |
| 10-docs-and-invariants | COMPLETE | 1 | docs/workflows/quota-resilience.md corrected; docs/configuration/llm.md quota section added; AGENTS.md quota invariants section added; graphs green |

### Post-completion deviations (accepted, documented)

- **Task-level deferral**: Contract 7's task-checkpoint deferral (resume at
  the failed step) is approximated at the turn level. Meept's task pipeline
  has no checkpoint/resume plumbing to hook; the wired mechanism is
  turn-level park/resume, matching how budget exhaustion already behaves.
- **Model-switch metadata**: switches are logged (from_model/to_model) but
  not recorded into task run metadata (same missing plumbing).
- **Escalation vocabulary**: `""/warn/action_recommended/blocked` instead of
  Contract 8's `12h/20h/24h` strings; internal consistency between tracker
  and notifier is maintained.
- **Episode dedup key**: tracker keys episodes per agent+provider with a
  model-scoped proxy credential key; true credential fingerprinting at the
  tracker requires config plumbing that does not exist.
- **Restart-mid-episode visibility**: agent-status RPCs carry no quota
  fields; surfaces rebuild quota state from the next bus event.

## Post-Review Fix Log (2026-08-31)

The original implementation was scaffolded but production-dormant. The
orchestrator's post-hoc audit + fix pass closed: client classification
(leaf 01 Task 4), resolver quotaCfg never set + all-blocked semantics +
PM persistence (leaf 03), dead broker-only decorator config (leaf 04),
tracker/notifier construction + pumps + state machine reachability +
Clear-on-recovery (leaves 05/07), turn-level deferral (leaf 06), and
TUI/Flutter surfaces (leaves 08/09). See `docs/workflows/quota-resilience.md`.

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. **Unit + package:** `go test ./internal/llm/... ./internal/agent/... ./internal/config/... -count=1` (scope llm with -run during leaf reviews).
2. **Cross-boundary (httptest):** a fake OpenAI-compat server returns 429
   with `{"error":{"type":"usage_limit_reached","resets_at":<epoch>}}`;
   assert (a) client returns QuotaResetError (no 3x short retry), (b) Resolver
   marks entry + provider-key blocked, (c) alias rotation reaches a second
   fake provider and succeeds, (d) `ActiveQuotaBlocks()` reports the block.
3. **Agent boundary:** with all providers blocked, dispatcher defers the
   task; simulate scheduler tick past unblock; assert resume and agent state
   quota_wait -> running.
4. **WS classification:** bus event on agent.quota_wait; assert
   transformBusEventToWS emits type "agent_progress".
5. **Race:** `go test -race ./internal/llm/ -run TestQuota` (Resolver block
   map concurrency).
6. **Analyzers:** `make lint-ci` green.

## Open Questions

- None blocking. Documented non-goals (decided): no fuzzy string/timezone
  parsing of reset times; quota blocks are in-memory only (restart re-probes);
  meept's own Budget (BudgetExceededError) behavior unchanged; 402 defaults
  to retry-with-estimate rather than hard billing failure.

## Notes

- Provider reality check (from research): OpenAI/Codex 429
  `usage_limit_reached` + `resets_at` epoch; Anthropic 429 `rate_limit_error`
  + `retry-after` + `anthropic-ratelimit-*-reset` RFC3339 headers (its
  `quota_exceeded` is the spend-cap case); Gemini 429 `RESOURCE_EXHAUSTED`;
  Tencent 429 integer codes 429001-429006 + `Retry-After`; OpenRouter 429
  nested provider body with `retry_after` seconds; xAI/Grok 429/402 with
  OpenAI-compatible shapes. All reduce to: status 429/402 + structured field
  or header. Bodies that parse to nothing get the config default estimate.
- The two retryable meanings: `QuotaResetError` must NOT re-enter the
  client's 3-attempt exponential loop (it implements `NonRetryableError`) but
  MUST trigger Resolver rotation (`ResolveForAlias` skips blocked candidates).
  Leaf 01 owns the client-side; leaf 03 owns Resolver rotation. Do not let
  one fold into the other.
- The existing `SmallModel` fallback (when configured) remains last resort
  after alias rotation exhausts. Quota blocking only affects which alias
  candidates are considered during rotation.
- **Production wiring**: `internal/daemon/components.go:747` constructs
  `ProviderManager` when >1 provider is configured; the agent loop drives
  the Resolver for aliases (`loop.go:4219-4295`). `ModelBroker` is dead in
  production (no constructor call found in daemon components). The broker
  stubs in leaf 03 keep the test surface intact but carry no runtime path.
- Parity + WS classification — TUI/Flutter parity rule applies to the new
  agent states; new bus topic must be classified in `transformBusEventToWS`
  so it doesn't become a blank chat bubble.
- internal/llm tests are slow (~80-90s: real HTTP/TLS setup). Always scope
  with -run during iteration; full runs only at integration.
