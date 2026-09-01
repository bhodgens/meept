# Architecture Audit — llm-resilience-forest (2026-09-01)

Independent read-only subagent audit against HEAD 90fd2afb. Findings and
their dispositions. Every BLOCKER/RISK/MINOR below has been folded into
the tree documents (marked where). Keep this file as the audit record.

## Verdicts applied

| ID | Severity | Finding (one line) | Disposition |
|----|----------|--------------------|-------------|
| B1 | BLOCKER | PrepareNextTurn hook pipeline is inert: HookRegistry.RunPrepareNextTurn (hooks.go:267) has zero production callers; loop.go reads no TurnModification (only ModelOverride via GetModelOverride, loop.go:3219). Tree 01's core mechanism was dead infrastructure. | NEW leaf 02-hook-pipeline.md wires the pipeline; tree 01 master restructured (4 leaves, groups A={01,02}); §7 pitfall added. Security/secret hooks (components.go:1245,1263) activate as a flagged side effect. |
| B2 | BLOCKER | Resolver.ResolveRef(ref string) *ModelConfig already exists (resolver.go:294; callers loop.go:3220, model_parser.go:410, registry.go:1237). Planned same-name/different-signature method = compile error. | Renamed to ResolveEscalationRef in leaf 03 contract, leaf 04 (was 03), and master Contract 3. |
| B3 | BLOCKER | endpointBlocks on AliasHealth is per-alias and cannot deliver D10 cross-alias shared fate (medium/thinkhard are different aliases). | Moved to Resolver struct in leaf 04 (was 03) Goal+Contract+Tasks; §4.3 amended; §7 pitfall updated. Cross-alias rotation test added. |
| R1 | RISK | Q2 reset × one-shot override: SetModelOverride clears after first application (loop.go:3244-3246), so the escalated model would serve exactly ONE turn of its budget. | Leaf 04 specifies SetPersistentModelOverride (loop.go:1344) held for the escalated budget, cleared on fresh-turn detection; frozen in master Contract 4 + Notes. |
| R2 | RISK | Host-only EndpointKey over-blocks across providers/credentials: gala-mlx/gala-llama share host gala (models.json5:186,205); xai/xai-oauth share api.x.ai (:251,:275). | §4.3 + leaf 04: key = host + credential fingerprint (QuotaCredentialKey precedent, models.go:356-358). Key tests include the xai-vs-xai-oauth fixture. |
| R3 | RISK | No mutable runtime context registry exists: Resolver.allModels built once (resolver.go:61); per-call copies via modelConfigFrom (providers.go:316-343); PricingSyncer writes only its own cache (pricing_sync.go:161-169); model_picker.go:37 is the static display catalog, not runtime. Runtime consumer = ModelConfig.ContextLimit via context_firewall.go:353. | Leaf 01 (tree 05) Context rewritten: merge must call NEW Resolver.SetContextLimits mutating pointer sets under r.mu (SetPricingSyncer precedent); trace context_firewall not model_picker; self-verification updated. |
| R4 | RISK | Jobs carry NO originating session: Job/StepJobPayload have no session field; worker has zero session context; enqueue sites lack *Session in scope. Enqueue-time-only evaluation also never upgrades a job when the user returns. | Leaf 02 (tree 04) Task 3 expanded: mandatory SessionID on StepJobPayload + step-store persistence + TaskID→LinkedSessions population; one_off jobs stamp false by construction; enqueue-time semantics documented as accepted. |
| R5 | RISK | ParkedTurn drops provider/credential identity into an untyped blob, but the re-park path (handler.go:1566-1586) needs ProviderID/CredentialKey. | §4.5 now declares the TurnPayload JSON encodings per class (quota: {message, parts, conversation_id, provider_id, credential_key}); throttle encoding frozen by leaf 02's report. |
| M1 | MINOR | verification_hook.go already imports llm (:12) — the "must not import llm" constraint was moot. | Leaf 03 corrected: interface retained for testability + name-collision avoidance, rationale updated. |
| M2 | MINOR | Tree 05 self-contradiction: contract said ModelConfig.ContextWindow while Context documented ContextLimit. | Fixed earlier (citation audit); contract consumes-section now names ContextLimit (runtime) vs ContextWindow (catalog). |
| M3 | MINOR | §7 cited loop.go:~4226 for the RateLimitError branch. | Fixed in citation audit (loop.go:~4298). |
| M4 | MINOR | Transport timeouts reach RecordAliasFailure without status/body — Classify never sees them; endpoint cooldowns would fire on 429s but never true timeouts. | Leaf 04 (tree 02) Task 2 gains the transport-path seam + timeout-only test; §7 pitfall added. |

## Verified OK (planner assumptions that held)

- Tree 02 Classify(statusCode, header, body, now): header+body co-located
  at all call sites — client.go doRequest (1022-1049), doStreamRequest
  (1434-1448), anthropic.go (1037-1061).
- Import cycles: none. internal/llm imports no agent/queue/employee;
  queue imports bus+stdlib only (queue.go:14); employee imports
  agent+llm (goal_loop.go:38,40) — trees 03/04 safe.
- Queue claim sites: exactly four ORDER BY priority DESC queries
  (store.go:379, 390, 624, 684).
- Tree 03 state plan: reuse StateQuotaWait + reason needs no new
  transition-table entries (agent_state.go:268-282).
- Bus topic agent.model_escalated would fall to eventType "event" today
  (server.go:703 matches only agent.quota) — tree 01 leaf 04 explicitly
  adds the prefix case; covered.
- Resume re-sends a STORED payload, not history reconstruction
  (handler.go:1594-1595) — ParkedTurn.TurnPayload is genuinely needed.

## Files touched by this audit's amendments

- 01-escalation/: master.md (4-leaf restructure), 02-hook-pipeline.md
  (NEW), 03-hook-wiring.md (renumbered + M1), 04-resolution-lifecycle.md
  (renumbered + B2/R1), 01-spec-config.md (unchanged by this audit)
- 02-failure-policy/: 04-endpoint-timeouts.md (B3/R2/M4)
- 04-scheduling/: 02-queue-priority.md (R4)
- 05-catalog/: 01-context-discovery.md (R3)
- SHARED-CONVENTIONS.md: §4.3, §4.5, §7

## Self-Verification Checklist (audit-record)

- [ ] Every BLOCKER/RISK/MINOR row above has a disposition pointing at
      the amended document (verified while writing the table)
- [ ] This record is documentation, not a dispatchable leaf: no tasks,
      no implementation. Do NOT commit this file independently — the
      orchestrator commits it with the amendment set.
