# Audit: Classifier and Routing Failure Paths (areas 5 and 6)

Repo: /tmp/meept-usability-head, HEAD 1cee89d7. Read-only audit; all line numbers verified against this tree.

## Area 5: Classifier unavailable / LLM classifier failure

### Verified mechanics

- Cooldown constant: `defaultUnavailableCooldown = 60 * time.Second` at internal/agent/llm_classifier.go:37. Per-call LLM timeout `defaultClassifierTimeout = 10 * time.Second` at llm_classifier.go:34. Boot-gate wait cap `defaultClassifierBootWait = 90 * time.Second` at llm_classifier.go:44.
- Failure arming: llm_classifier.go:583-593 (Classify) sets unavailable=true, unavailUntil=now+cooldown on any chat error, logs "LLM classifier unavailable, falling back to keyword", returns (nil, err). ClassifyMulti arms the SAME flag at llm_classifier.go:704-718.
- Cooldown short-circuit: llm_classifier.go:534-563. Within cooldown and no resolver/alias: returns error "LLM classifier unavailable (cooldown, retry after HH:MM:SS)" (line 541). With resolver: rotates to next alias candidate (549-558) instead of blocking. Expired cooldown clears the flag (560-561).
- Failover: chatWithFailover (llm_classifier.go:607-663) makes at most 2 attempts (primary + one alias rotation); empty response becomes llm.ErrEmptyResponse ("empty content", internal/llm/client.go:2317).
- Boot readiness: classifier_boot_readiness.go:134-151 awaitClassifierBoot is bounded (90s default), context-cancellable, and NEVER fails the call: SignalFailed -> ErrClassifierBootGateFailed (line 50) -> proceed to ordinary attempt -> degrade. Contract at lines 16-34 explicitly says warmup failures must never fail a turn. Verified honored.

### Fallback decision sites (turn still completes)

Dispatcher.classifyIntent (dispatcher.go:1554-1984) NEVER returns a non-nil error: every exit returns a non-nil Intent with nil error (verified: no `return nil,` in the function body).

1. LLM error branch: dispatcher.go:1870-1896. kind := llm.ClassifyClassificationFailure(err) (internal/llm/errors.go:559-587). Only `empty_response` short-circuits to chat (Method "llm_empty_fallback_chat", confidence 0.6, dispatcher.go:1884-1895). All other kinds (timeout, unavailable, budget, unknown -- including the cooldown error string) fall through to:
2. Keyword classifier: dispatcher.go:1899-1920 (threshold 0.3).
3. Semantic index: dispatcher.go:1922-1948.
4. heuristicFallback: dispatcher.go:1950-1963.
5. Final fallback: dispatcher.go:1965-1982 -- IntentQuickPlan, agent "orchestrator", confidence 0.3, RequiresPlanning=true, Method "fallback", Summary "Could not determine intent; planning and executing with clarification as needed".

ClassifyAndRoute consumes it at dispatcher.go:1377-1380: classifyErr is only logged ("classifyIntent failed"), never propagated. IntentAnalyzer failure is likewise swallowed (dispatcher.go:1273-1274, `if err == nil && analysis != nil`). ClassifyMulti failure returns nil intents (llm_classifier.go:710/718/724/734) and classifyMultiIntent proceeds with keyword-only results (dispatcher.go:2828+).

ANSWER: a classifier LLM error/timeout/cooldown CANNOT make ClassifyAndRoute return an error; the turn always completes via keyword/heuristic/quickplan fallback. The only ClassifyAndRoute error returns are unrelated to classification: plan creation failure (dispatcher.go:2160 "failed to create plan: %w"), compound parent-task creation failure (dispatcher.go:2746 "failed to create parent task for compound request"), skill executor missing/failure (dispatcher.go:4402/4414), and propagated ResumeAfterClarification results (dispatcher.go:2451-2461). Those reach handler.go:1044-1058 (handlerCase=dispatch_error) and the terminal funnel handler.go:1424-1473 with status "failed", Error=response.Error (set at handler.go:1354).

### ClassificationNotice text

There IS no text. `ClassificationNotice` is declared at dispatcher.go:231-234 and read at handler.go:1049-1058 (dispatch_error guidance) and handler.go:1258-1264 (append to successful reply), but a repo-wide grep shows NO assignment anywhere in non-test code. Both consumer branches are dead. The companion helper llm.ClassificationUserGuidance (internal/llm/errors.go:591-606, well-written per-kind user messages) has zero callers. Additionally, every ClassifyAndRoute error return is (nil, err), so handler.go:1049's `result != nil` guard could never fire even if the field were populated.

### Findings (Area 5)

F5-1. MAJOR -- Silent classifier degradation: ClassificationNotice never populated.
Evidence: dispatcher.go:231-234 (field), handler.go:1049, 1258 (only reads); zero writes repo-wide; llm/errors.go:591 ClassificationUserGuidance unused. The boot-readiness doc (classifier_boot_readiness.go:10-14) names "user gets no signal that routing was degraded" as the defect, yet the notice wiring that would fix it is dead. Emit site: absent. Status: turn completes (often misrouted via keyword/heuristic). Error text quality: none shown. Silent-drop: no drop, but silent QUALITY degradation -- user cannot tell the router was degraded.
Fix sketch: in classifyIntent's error/fallback branches set result.ClassificationNotice = llm.ClassificationUserGuidance(classifyErr) (thread the error out via a dispatcher field or Intent), and populate it on the cooldown path too.

F5-2. MINOR -- Cooldown/timeout failures route to keyword, contradicting the documented chat-fallback intent.
Evidence: dispatcher.go:1876-1883 comment says "when the LLM path is down we route to chat ... Keyword matching stays available only for ... platform patterns", but only ClassificationFailureEmptyResponse takes that branch (1884-1895); timeout/unavailable/cooldown errors fall into the verb-dense keyword table the comment warns misclassifies task-shaped prompts. Status: completes, possible misroute. Fix sketch: extend the chat-fallback branch to ClassificationFailureUnavailable/Timeout (or all non-empty kinds).

F5-3. MINOR -- ClassifyMulti failure arms the shared 60s cooldown used by single-intent Classify.
Evidence: llm_classifier.go:704-718 sets the same c.unavailable/unavailUntil consumed at 534-563. One failed multi-intent side-call degrades ALL classification for 60s (and vice versa). Fix sketch: separate cooldown state for the multi-intent lane, or only arm on primary-lane failures.

F5-4. MINOR -- Boot-window latency stacking with no per-turn deadline.
Evidence: intent_analyzer.go:241 awaitBootReadiness (90s cap) runs at dispatcher.go:1273 BEFORE classifyIntent's own 90s boot wait (llm_classifier.go:527) + 10s call timeout; handleRequest (handler.go:749+) applies no context.WithTimeout. Worst case a boot-window turn stalls ~3 minutes before degrading. Bounded, not infinite; ctx-cancellable. Fix sketch: share one boot-wait budget per dispatch, or cap total classification time per turn.

F5-5. MINOR (latent) -- nil-intent dereference after classify error would panic the chat goroutine.
Evidence: dispatcher.go:1377-1380 logs classifyErr and continues; dispatcher.go:1395 `intent.MemoryRefs = ...` dereferences unconditionally. Currently unreachable because classifyIntent never returns (nil, err), but the warn-and-continue shape invites it. No recover wraps handleRequest (the goroutine at handler.go:385-399 calls it directly; handler.go:221 recover covers only runEffectsResumeHook), so a panic crashes the whole daemon. The early-return safety net (handler.go:872-895) would emit one failed turn.terminal during unwind, then the process dies. Fix sketch: `if intent == nil { intent = fallbackIntent(input) }` immediately after 1380, or return the error.

## Area 6: Agent runtime dead / loop closed / manager missing agent

### Verified mechanics

- RouteToAgent entry guards: dispatcher.go:3042-3044 nil result/intent -> error "dispatch result has no intent to route"; dispatcher.go:3080-3082 nil registry -> "no agent registry configured". Handler-side twin guard: handler.go:1221-1226 nil_result_error "dispatch returned no actionable result".
- Missing agent: resolveAgent (dispatcher.go:3540+) tries session-scoped GetOrCreateWired (3598, 3670), then registry.Get(agentID); on miss falls back to registry.Get(chat) with a Warn; if chat is ALSO missing returns nil (dispatcher.go:3707-3712, "fallback agent not found"). RouteToAgent then returns error `no agent available for %q` at dispatcher.go:3265-3267. This flows to handler.go:1233 err -> response.Error=err.Error() (1354) -> terminal funnel status "failed" (1433), turnReply "I encountered an error: no agent available for \"x\"" (1441). No panic, no hang, no silent drop.
- Manager: manager.go:47 GetOrCreate, :99 GetOrCreateWired (per-(session,agent) cacheKey at :84-89; evicts+closes stale loop on workingDir change at :114-135), :160 Get, :168 Remove. GetOrCreateWired errors ("sessionID required" :144, "workingDir required" :147, "template loop required" :150) are swallowed by resolveAgent (Warn + singleton fallback, dispatcher.go:3635-3641) and by handler.sessionLoop (handler.go:3527-3537, Warn + singleton mutation). sessionLoop (handler.go:3473-3577) returns h.loop when manager nil (:3474-3476) or no working dir (:3497-3508, loud Warn with actionable hint). LookupLoop (handler.go:3617-3622) is a nil-safe wrapper over sessionLoop.
- Nil LLM client: ErrNoLLMClient = errors.New("no LLM client configured") at loop.go:56; returned at loop.go:3334-3336 (RunOnceWithParts), :4162 (RunWithSkill), :7775 (RunWithTask). Routed path wraps it: dispatcher.go:3326-3328 "agent execution failed: %w". User-facing text: response.Error = "agent execution failed: no LLM client configured"; reply empty -> funnel synthesizes "I encountered an error: agent execution failed: no LLM client configured" (handler.go:1438-1441), status "failed". Direct mode (handler.go:1254) yields the bare "no LLM client configured". Technical but truthful; no remediation hint (contrast the unused ClassificationUserGuidance).
- Closed loop: AgentLoop.Close (loop.go:4442-4451) does wg.Wait(), stopRewake, isActive=false, closed=true. There is NO ErrLoopClosed anywhere in the repo (grep: zero hits) and RunOnceWithParts never checks l.closed -- the only closed.Load() read is the streaming-accumulation guard at loop.go:9689. A turn submitted to a closed loop RUNS NORMALLY (no error, no rejection). Manager.Remove (manager.go:168-173) deletes without Close; registry.Close (registry.go:1244-1262) closes queues and drops loop references without closing loops.
- Queue lane: RouteToAgent steers into an active queue (dispatcher.go:3138-3208). Steer/FollowUp are non-blocking appends (queue.go:226-262, 265-300); ErrQueueClosed/ErrQueueFull fall through to a fresh agent run (3181-3186, 3195-3200); other errors return (3188, 3202) -> failed terminal. Closed queue detected at 3164 -> fresh run. Success returns "message queued (steer/follow-up)" (3191/3205) as the completed reply; the real answer rides the active loop's events.
- nil-result-and-nil-error: ClassifyAndRoute has no such path (the only `return nil, nil` in dispatcher.go are 2969/2978 inside reportReadbackIntents, a helper returning intents). Every nil-err exit carries a non-nil DispatchResult; the planMode toggle exit (1003) has Intent=nil but Response!="" and is caught by handler.go:1038 direct_response before the nil-intent guard. nil_result_error is defensive-only today.
- Blocking paths: no infinite block found in RouteToAgent itself (queue ops non-blocking; LLM calls bounded by client HTTP timeout default 120s, internal/llm/client.go:29,618; classifier 10s; boot wait 90s). One real hang risk below (F6-2).

### Findings (Area 6)

F6-1. MAJOR -- No closed-loop guard: turns execute on closed loops; "loop closed" is not an error state at all.
Evidence: loop.go:4442-4451 Close sets closed; loop.go:3321+ RunOnceWithParts has no closed check; ErrLoopClosed does not exist; only read of closed is loop.go:9689 (stream flush). Manager.GetOrCreateWired can return a cached loop that another path Closed (e.g. stale eviction racing an in-flight lookup, manager.go:109-141). Status: turn "completes" on a torn-down loop (isActive=false, rewake consumer stopped) -- degraded runtime with no signal, or succeeds normally; never a clean failed terminal. Silent-drop check: no drop, but no rejection either -- the closed state is invisible to the user.
Fix sketch: add ErrLoopClosed and check l.closed.Load() at the top of RunOnceWithParts (return it like ErrNoLLMClient); have GetOrCreateWired treat a closed cached loop as stale and rebuild.

F6-2. MAJOR -- Dispatch can block indefinitely in staleLoop.Close() via untimed background goroutines.
Evidence: manager.go:131-135 calls staleLoop.Close() synchronously inside GetOrCreateWired (on the dispatch path, dispatcher.go:3598/3670 and handler.go:3527) whenever the session's workingDir changed. Close does l.wg.Wait() (loop.go:4446); the wg-tracked shadow-capture goroutines run with context.Background() and NO timeout (loop.go:5903-5915, 6571-6585, comments explicitly say they outlive the request ctx). If shadow capture hangs on network I/O, Close -- and therefore the next chat turn for that session -- blocks forever; handleRequest has no turn deadline, and the chat handler goroutine processes requests serially (handler.go:385-399), so ONE hung Close stalls ALL chat traffic. No terminal event is emitted while blocked (the safety net only fires on function exit). Emit site: absent during the hang. Status: neither failed nor completed -- stuck.
Fix sketch: bound shadow-capture contexts (e.g. 30s) and/or give Close a wg-wait deadline; run stale-loop Close asynchronously.

F6-3. MINOR -- ErrNoLLMClient user text is raw plumbing.
Evidence: loop.go:56/3334-3336 -> dispatcher.go:3327 wrap -> handler.go:1354/1438-1441 "I encountered an error: agent execution failed: no LLM client configured", status failed. Truthful and reaches the terminal funnel correctly (no silent drop), but gives the user no action ("configure a model / check provider health"). Fix sketch: map ErrNoLLMClient to a friendly message at the handler error seam, like the BudgetExceededError.UserMessage() pattern at handler.go:1304-1306.

F6-4. MINOR -- Manager.Get uses the legacy bare-session key while GetOrCreateWired stores namespaced keys.
Evidence: manager.go:160-165 Get(sessionID) vs cacheKey(sessionID, agentID) at manager.go:84-89/103. Named-agent loops (e.g. "bob") are invisible to Get; caller internal/daemon/voice_wiring_contextwarm.go:180 silently misses them. Fix sketch: make Get iterate keys with the session prefix, or document it as chat-lane-only.

F6-5. MINOR -- Steer lane can silently lose a queued message if the owning loop dies before draining.
Evidence: dispatcher.go:3191/3205 returns "message queued (steer)" as a COMPLETED turn; Steer replaces any prior steering message latest-wins (queue.go:240-244); UnregisterActiveQueue closes the queue on loop exit (registry.go:2059-2074). A crash/exit between enqueue and drain drops the message with the user already told "queued". Persistence (queue persister) mitigates restarts but not in-process loss. Fix sketch: on queue close with undrained messages, re-dispatch or emit a failed turn.terminal for the queued turn id.

F6-6. MINOR -- Unguarded sessionTracker dereferences on two dispatch paths.
Evidence: dispatcher.go:1233 (prefilter route) and :1524 (main route) call d.sessionTracker.RecordIntent without the nil guard used everywhere else (e.g. 1130, 1363, 2466). Constructor always sets it (dispatcher.go:764), so production-safe; test/alt constructions panic. Fix sketch: add the same `if d.sessionTracker != nil` guard.

## Ranked top defects

1. F6-2 (MAJOR): hung shadow-capture goroutine + synchronous staleLoop.Close() in GetOrCreateWired can freeze the single serial chat goroutine forever -- no terminal, no timeout, all sessions affected.
2. F5-1 (MAJOR): ClassificationNotice/ClassificationUserGuidance dead wiring -- classifier degradation is silent to the user, the exact defect the boot-readiness doc claims to address; fallback can misroute with zero disclosure.
3. F6-1 (MAJOR): no ErrLoopClosed / closed-loop guard -- closed loops accept and run turns; torn-down runtime state is invisible instead of a clean failed terminal.
4. F5-5 (MINOR, latent): warn-and-continue on classifyErr then unconditional intent dereference (dispatcher.go:1395) -- one future (nil, err) return turns into an unrecovered panic that kills the daemon.
5. F5-2 (MINOR): cooldown/timeout failures hit the keyword table despite the documented chat-fallback policy for a down LLM lane.
6. F6-3 (MINOR): ErrNoLLMClient surfaces as raw wrapped plumbing text with no remediation hint.
7. F5-3 (MINOR): multi-intent failures arm the shared 60s cooldown, degrading unrelated single-intent classification.
8. F6-5 (MINOR): "message queued (steer)" completed-ack can orphan the message if the owning loop exits before drain.
9. F5-4 (MINOR): boot-window waits stack (analyzer 90s + classifier 90s + timeouts) with no per-turn deadline.
10. F6-4 / F6-6 (MINOR): Manager.Get key mismatch; unguarded sessionTracker calls.

Contract verdicts: (5) 60s cooldown CONFIRMED (llm_classifier.go:37); heuristic/keyword fallback CONFIRMED -- classifier failure cannot fail the turn; user-facing degradation notice NOT IMPLEMENTED (dead field). (6) missing agent / nil registry / nil LLM client all reach the failed terminal funnel with honest (if technical) text; closed-loop rejection DOES NOT EXIST; no nil-result+nil-error dispatch path; one indefinite-block path via Close/wg.Wait (F6-2).
