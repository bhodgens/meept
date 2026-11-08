# Bug audit: LLM + daemon wiring + intake + slack HITL
- Scope: meept commit range 0a446a21..HEAD
- HEAD SHA at audit start: 6b42213b
- Timestamp: 2026-11-06T18:42:37Z (UTC)
- Mode: READ-ONLY. Findings appended as confirmed.

## Findings

## F-1 [MEDIUM] llm-hop-tracing observer never attached to Anthropic/Codex clients — seam wired but never published
- internal/daemon/components.go:1030 — `wireOTLPTraces(..., msgBus, nil, logger)` passes a **nil** attacher (the LLM client doesn't exist yet at that point).
- internal/daemon/components.go:1288-1289 — the only post-hoc attach: `if c.otlpTraceSubs != nil && c.LLMClient != nil { c.LLMClient.SetLLMCallObserver(...) }` — this covers ONLY the primary OpenAI-compatible `*llm.Client`.
- The in-code comment at components.go:1284-1287 explicitly promises: "Secondary clients (Anthropic/Codex set below when the manager builds them) go through the same seam."
- Evidence it never happens: `grep -rn SetLLMCallObserver internal/ cmd/` (committed HEAD) returns only the three llm client definitions (client.go:320, anthropic.go:349, codex.go:349), the wiring file, and components.go:1289. `internal/llm/provider_manager.go` has propagation seams for the other cross-client configs (`SetFailurePolicyConfig` at :304, `SetSessionAffinityConfig`) that fan out to every managed client — **no equivalent fan-out exists for the observer**.
- Why it is a bug: with `metrics.otlp_traces.enabled=true`, every Anthropic- or Codex-class provider call records its llm_calls row (and observer would fire) but `llmCallObserver` is nil on those clients, so their `llm.call` hop spans silently never reach the trace producer. This is exactly the "wired a seam but never published" class: the Anthropic/Codex `recordUsageStore` changes in this range (3723542d..llm-hop-tracing wave) added the observer field + setter with a documented daemon-side attach, and the attach site was never written.
- Fix shape: mirror `SetFailurePolicyConfig`: add `ProviderManager.SetLLMCallObserver(fn)` that iterates managed entries and calls the per-client setter; call it from components.go next to the `c.LLMClient.SetLLMCallObserver` line (and pass `c.ProviderManager` too).

## F-2 [LOW] TTFT capture state not reset per streaming retry — stale first-delta stamp yields wrong (or 0-clamped) TTFTMS
- internal/llm/client.go:2562 — `chatOpts.ttftStart = time.Now()` is re-armed per short-retry attempt ("refreshed per short-retry so the number measures the serving attempt's first-token wait").
- internal/llm/client.go:2187-2192 — `captureFirstDelta` is idempotent on `chatOpts.ttftCaptured` and NOTHING resets `ttftCaptured`/`ttftFirstDelta` between attempts (grep: only writers are client.go:2191 and readers 2201/2204).
- Why it is a bug: if attempt N streams at least one content delta and then fails mid-stream (the resume/error lane), attempt N+1 re-arms `ttftStart` but the stale `ttftCaptured`/`ttftFirstDelta` from attempt N survives; `ttftMS` then computes `firstDelta(attempt N) - start(attempt N+1)`, which is negative — silently clamped to 0 (client.go:2205-2207), or a small wrong value when the clocks nearly align. The ledger's TTFTMS rollup (AvgTTFTMs, migration v9 / 0827f72c) is corrupted exactly on the retry-heavy path it is supposed to characterize. Metrics-only, hence LOW.
- Fix shape: reset `chatOpts.ttftCaptured = false` (and zero `ttftFirstDelta`) at the same site that re-arms `ttftStart` in both the openai delta loop (client.go:2562) and the anthropic progress loop (anthropic.go:760); or stamp TTFT against the attempt whose delta was captured.

## Audited-clean

Hunted per the wave brief with no confirmed defect found in:
- **Quota-error classification** (the AGENTS.md invariant): all five retry loops checked at HEAD — openai `Chat` (client.go:839) / `ChatWithProgress` (:1121) / `ChatWithDeltaCallback` (:2648), anthropic `Chat` (anthropic.go:489) / `ChatWithProgress` (:776) — keep the `*QuotaResetError` `errors.As` early-exit BEFORE the retryable/rate-limit checks; empty completion re-dispatches immediately within the short budget and the BARE `ErrEmptyResponse` sentinel is surfaced on exhaustion at every site (no wrapped ClientError); refusal/quota early-exits keep precedence over the empty branch. New `TierFloorError` (errors_tier.go) is `NonRetryable` with multi-`Unwrap` keeping `errors.Is(ErrTierFloor)` and `errors.Is(ErrEmptyResponse)` both reachable — no alias-failure contamination of quota paths. ProviderManager rotation treats quota via `blockQuotaCredential`, never `recordFailure`.
- **Affinity-header plumbing**: the range's llm diffs (client.go/anthropic.go/codex.go) touch the observer seam, TTFT, and semantic cache only — no affinity-scope changes; the header-value source invariant (turnAccountingSessionID, empty scope = no header) is untouched.
- **Intake poller scheduling when disabled**: `wireIntakePoller` returns at the `!cfg.Enabled` guard before any scheduling (intake_wiring.go:71-74); enabled-but-invalid fails closed with Warn; `intake.status` reports `poller_scheduled=false` unless enabled (rpc/intake_status.go).
- **Intake plan-projection bus seam**: `plan.executing` and `plan.phase_state` subscribers match real publishers in internal/plan/manager.go:916/:892/:962/:993 with the exact payload keys (`plan_id`/`phase_id`/`state`) the handlers decode; nil-bus/nil-ctx skips are logged, not silent; handler is panic-recovered.
- **Effects-storm guard / intake dedupe**: linkage.go returning `nil,nil` on `effects.ErrInvalidTransition` is deliberate (concurrent-poller race; winner's claim not abandoned); writeback receipt reuse per (goal,state) key intact.
- **Slack HITL wiring**: registration is enabled-gated (components.go:2572), resolver + notifier fan-in happen after the ACP manager exists (components.go:3708-3709), HTTP route wired at daemon.go:1710; interactions handler is fail-closed (empty AllowedUserIDs allows nobody; signature verified before parse; ack-in-budget then async resolve).
- **Jev-score-gate wiring (5e01e286)**: construction is opt-in-gated, typed-nil posture respected (`c.JevScoreGate != nil` before RegisterBeforeToolCall), registration present in components.go; gate tests untagged and passing per commit record (not re-run here — time-box).
- **Semantic cache two-stage verifier (e23fd815)**: band clamp (VerifyFloor<0 or >=Threshold -> 0), tool-call store+serve exemption, embed-outside-lock in `verifyBehindBand` with re-check, every failure shape -> miss — verified by reading HEAD source.
- **Tier gate math**: `nextHopBlocked` (`dispatched <= limit` legal) matches the Contract-C2 worked example (MaxDepth=2 -> 3 members dispatched, 4th refused); first hop can never trip; gate is stack-local per call, never on shared manager state.

## Observations (noted, not confirmed as in-scope bugs)

- internal/daemon/components.go:1227 — the semantic cache tier is attached ONLY to the primary OpenAI-compatible `c.LLMClient` (`WithSemanticCache` exists solely on `*Client`; Anthropic/Codex clients built per-provider by `ProviderManager` get no cache). The leaf-02 T5 plan docs cite only the one client build site, so this may be intended scope — but the serving-tier reuse promise covers one provider class. Flagged for the tier-plan owner to confirm intent.
- Status: FINALIZED at time-box (~25 tool calls). Findings evidence-backed against committed HEAD (git show/diff only; dirty working tree ignored). No code edited, nothing committed.


## Wave closeout (parent, 2026-11-06)
- F-2 CLOSED: both streaming retry arms (client.go openai path + anthropic.go)
  reset ttftCaptured/ttftFirstDelta with the ttftStart re-arm, so a delta
  captured by a failed attempt can no longer stamp the next attempt's wait.
  Full internal/llm package ok.
- F-1 OPEN (OTLP observer never attached to Anthropic/Codex clients) — parent
  next.
- F-1 CLOSED: ProviderManager.SetLLMCallObserver fan-out seam attaches the
  observer on EVERY chatter (Client/AnthropicClient/CodexClient) at boot AND
  via AddProvider; daemon calls it right after pm construction. Pinned by
  TestProviderManager_SetLLMCallObserverFanout. internal/llm + internal/daemon ok.
