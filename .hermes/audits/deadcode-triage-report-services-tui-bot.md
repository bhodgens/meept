# Deadcode Triage Report — scope `services-tui-bot`

Generated: 2026-11-09 · Repo: `/Users/caimlas/git/meept` · READ-ONLY survey (no source modified)

Scope: internal/services, internal/tui, internal/tui/models, internal/bot, internal/comm/telegram, internal/comm/http — **total flagged: 150** (equals scope-file line count; verified).

## Counts per class

| Class | Count |
|---|---|
| WIRED-FILE-AUX | 47 |
| UNWIRED-FEATURE | 92 |
| RTA-IMPRECISION | 11 |
| **Total** | **150** |

## Counts per disposition

| Disposition | Count |
|---|---|
| BASELINE | 8 |
| WIRE-CANDIDATE | 77 |
| DELETE-CANDIDATE | 54 |
| IMPRECISION-BASELINE | 11 |
| **Total** | **150** |

## Method

- Per FILE-CLUSTER classification. **WIRED-FILE-AUX** = the owning type has a production construction site (file:line cited); the flagged funcs are auxiliaries of a wired file. **UNWIRED-FEATURE** = no production construction anywhere (ripgrep over `*.go` excluding `_test.go`); producing commit cited via `git log --diff-filter=A -- <file>`, plan citation where found (DEFERRED-BY-PLAN / PLANNED-OPEN / UNCLAIMED noted). **RTA-IMPRECISION** = verified textual caller in production code; bubbletea `Update`/`View` bodies and message-switch cases count as real callers; interface dispatch (`timerSource.After`, `io.Writer`) counts; **test-only callers do not count**.
- All 150 entries are already in the committed ratchet baseline (`.githooks/deadcode-baseline.txt`, 13,610 lines) — none block pre-commit today.
- Push/notification wiring seam checked as instructed: `internal/daemon/components.go:2599-2652` (ChannelRegistry, PushService, QuotaNotifier, BotContext) — HTTP + Slack channels are the only registered channels.

## Per-file rows

| file | count | class | evidence | disposition |
|---|---|---|---|---|
| internal/bot/cost.go | 3 | UNWIRED-FEATURE | NewCostTracker has zero prod constructors (rg over *.go excl _test.go: definition only). Producing commit e9605179 "bot framework wiring ... CostTracker with daily budget enforcement". Plan: docs/plans/2026-06-21-automation-capability-extension.md Plan 1 Phases 3-4 (NotificationBot channels + budget-exhausted triggers). | WIRE-CANDIDATE x3 |
| internal/bot/notification_bot.go | 12 | UNWIRED-FEATURE | NewNotificationBot zero prod constructors; internal chain Start->handleEvent->shouldNotify/applyTemplate/containsKeyword (notification_bot.go:109,194-200,250) never entered. Producing commit 23998903 (plan1, Plans 1.1+1.3). Plan: automation plan 2026-06-21 Plan 1 Phase 3 "Create NotificationBot that pushes to channels". | WIRE-CANDIDATE x12 |
| internal/bot/webhook.go | 2 | UNWIRED-FEATURE | NewWebhookHandler zero callers; superseded — internal/comm/http/server.go:194: "Replaces the former botWebhookHandler; the webhook trigger endpoint now lives under /api/v1/agents/{id}/trigger". Producing commit 9a8be86c. UNCLAIMED. | DELETE-CANDIDATE x2 |
| internal/comm/http/api_templates.go | 2 | UNWIRED-FEATURE | TemplatesService{} never constructed. Live equivalents: Server.handleTemplatesList (api_handlers.go:3520) / handleTemplatesInvoke (api_handlers.go:3563), registered server.go:1779-1781. Producing commit ab5ef133; impl superseded by the Server methods from the same feature. | DELETE-CANDIDATE x2 |
| internal/comm/http/events.go | 9 | UNWIRED-FEATURE | comm/http NewEventEmitter zero prod constructors. Wired impl is daemon.EventEmitter (internal/daemon/events.go:99; constructed components.go:2584; passed via http.WithNotification daemon.go:1670) satisfying the http.NotificationEmitter interface (notification_handlers.go:62) — Subscribe/Unsubscribe/GetEventsSince are called through that interface at notification_handlers.go:102-166, not on this type. Producing commit cea17473 (analytics + menubar notifications plan). | DELETE-CANDIDATE x9 |
| internal/comm/http/server.go | 6 | WIRED-FILE-AUX | Server constructed daemon.go:1791 http.NewServer; WS relay live at server.go:878/950 via h.ShouldSend. ShouldSendProgress has textual caller server.go:712 (RTA-IMPRECISION). Addr (:1355), ChatSubmitter (:1349), Broadcast (:716), ClientCount (:547), progressRateLimiter.cleanup (:489) have zero textual callers. | DELETE-CANDIDATE x5, IMPRECISION-BASELINE x1 |
| internal/comm/http/sse.go | 2 | WIRED-FILE-AUX | NewSSEWriter used api_handlers.go:263; SendEvent/SendError/SendComment live (api_handlers.go:273-441). SSEWriter.Write implements io.Writer (RTA-IMPRECISION). CloseWithFinalEvent zero callers — final-event protocol from commit a8ee0b3e (tool streaming) never completed (PLANNED-OPEN). | DELETE-CANDIDATE x1, IMPRECISION-BASELINE x1 |
| internal/comm/http/voice.go | 1 | WIRED-FILE-AUX | VoiceRoutes IS constructed: internal/daemon/voice_wiring.go:532 commhttp.NewVoiceRoutes; wired daemon.go:1782 http.WithVoiceRoutes; routes served via HandlesPath -> serveVoiceRoute (server.go:2053->2060), never via Register(mux). Register (voice.go:212) zero callers — orphaned mount helper. | DELETE-CANDIDATE x1 |
| internal/comm/telegram/base_bot.go | 3 | UNWIRED-FEATURE | newBaseBot called only from NewTelegramChannel (channel.go:68) which is never constructed in prod (internal/bot/iface_check.go:8-9 are compile-time interface pins only). Producing commit 23998903 (plan1). Plan: automation plan 2026-06-21 Plan 1. | WIRE-CANDIDATE x3 |
| internal/comm/telegram/channel.go | 8 | UNWIRED-FEATURE | NewTelegramChannel zero prod constructors; the daemon wires telegram.Bot + AgentHandler directly (components.go:5068-5084 telegram.NewAgentHandler / telegram.NewBot), bypassing the MessagingBot adapter. Producing commit 23998903 plan1 Phase 2. Plan: automation plan 2026-06-21 Plan 1 "Create TelegramChannel type implementing MessagingBot". | WIRE-CANDIDATE x8 |
| internal/comm/telegram/handler.go | 1 | WIRED-FILE-AUX | AgentHandler IS constructed: components.go:5068 telegram.NewAgentHandler (wired). saveSessions (handler.go:203) zero prod callers — chat->session persistence path never invoked. | DELETE-CANDIDATE x1 |
| internal/services/bot_context.go | 1 | WIRED-FILE-AUX | BotContextImpl IS constructed: components.go:2652 services.NewBotContext(c.PushService,...) ("Task 8, dormant wiring closure"). PushNotification (bot_context.go:27) zero callers — the bot.BotContext interface (internal/bot/interfaces.go:41) has no consumer. Producing commit 0821df6b (Plan 3.5 Phase 4 bot integration). DEFERRED-BY-PLAN: context built, bots never handed it. | WIRE-CANDIDATE x1 |
| internal/services/employee_service.go | 1 | WIRED-FILE-AUX | EmployeeService constructed: internal/services/service.go:225 NewEmployeeService (when EmployeeManager configured). Get (employee_service.go:77) not called by any RPC/HTTP handler. UNCLAIMED. | DELETE-CANDIDATE x1 |
| internal/services/groups_service.go | 2 | WIRED-FILE-AUX | Deliberate signature-pin stubs: groups_service.go:44-51 "signature-pin stub (rpc mirrors this method set; never invoked for a value)". Intentional API surface — deleting them deletes the convention. | BASELINE x2 |
| internal/services/memory_service.go | 1 | WIRED-FILE-AUX | MemoryService constructed: service.go:133 NewMemoryService. BridgeCount (memory_service.go:166) zero callers. UNCLAIMED. | DELETE-CANDIDATE x1 |
| internal/services/model_service.go | 1 | WIRED-FILE-AUX | ModelService constructed: service.go:172 NewModelService. GetCredentialRaw (model_service.go:239) zero callers (config-export consumer never built). UNCLAIMED. | DELETE-CANDIDATE x1 |
| internal/services/notification_prefs.go | 7 | UNWIRED-FEATURE | Zero consumers repo-wide: rg DefaultNotificationPreferences|NotificationRateLimiter|IsChannelEnabled|IsInDND|IsTypeEnabled outside the def file returns nothing (non-test). Producing commit 0821df6b "Plan 3.5 - User Controls for Notifications"; plan: automation plan 2026-06-21 Plan 3 Phase 5 (per-user prefs, rate limit, DND) — plan marked COMPLETE but no consumer ever built (PLANNED-OPEN in practice). | WIRE-CANDIDATE x7 |
| internal/services/prompt_service.go | 1 | WIRED-FILE-AUX | PromptService constructed: service.go:225-231 NewPromptServiceFromDirs / NewDefaultPromptService. ProjectDir (prompt_service.go:147) zero callers. UNCLAIMED. | DELETE-CANDIDATE x1 |
| internal/services/push_channels.go | 12 | WIRED-FILE-AUX | ChannelRegistry IS constructed: components.go:2599 services.NewChannelRegistry; Register called components.go:2612 (HTTP channel) + 2625 (Slack channel). Registry.Push reachable only via PushToChannels (push_service.go:139) which has zero callers. New* for CLI/TUI/Telegram channels have zero prod constructors. Producing commit 3c705186 (plan3.3). Plan: automation plan 2026-06-21 Plan 3 Phase 3 (Telegram/CLI/TUI routing explicitly listed). | WIRE-CANDIDATE x12 |
| internal/services/push_history.go | 5 | UNWIRED-FEATURE | NewPushHistory zero prod constructors. Producing commit 0821df6b (Plan 3.5 Phase 5 "Push history/query API"). Plan claims complete; consumer absent. | WIRE-CANDIDATE x5 |
| internal/services/push_service.go | 2 | WIRED-FILE-AUX | PushService IS constructed: components.go:2615 NewPushServiceWithChannels; live path is Push (called bot_context.go:32 and by the quota notifier). NewPushService (bus-only ctor, push_service.go:75) zero prod callers; PushToChannels (push_service.go:108) zero callers — it is the plan3.3 channel-routing seam. | DELETE-CANDIDATE x1, WIRE-CANDIDATE x1 |
| internal/services/quota_notifier.go | 2 | WIRED-FILE-AUX | QuotaNotifier IS constructed: components.go:2647 NewQuotaNotifier — auto-starts its pump (quota_notifier.go:75 "Auto-start... no explicit Start call needed"). Exported Start superseded by auto-start; Cleanup zero callers — no daemon-shutdown path stops the pump. | WIRE-CANDIDATE x1, DELETE-CANDIDATE x1 |
| internal/services/security_service.go | 1 | WIRED-FILE-AUX | SecurityService constructed: service.go:162; SetAuditDB wired daemon.go:1205-1207. Audit (security_service.go:77) zero callers — no handler exposes the query. UNCLAIMED. | DELETE-CANDIDATE x1 |
| internal/services/selfimprove_service.go | 1 | WIRED-FILE-AUX | SelfImproveService constructed: service.go:156. Cancel (selfimprove_service.go:67) zero callers (producing commit 82d7a533 SkillDesigner/Q-Agent). UNCLAIMED. | DELETE-CANDIDATE x1 |
| internal/services/service.go | 2 | WIRED-FILE-AUX | Registry constructed: daemon.go:1167 services.NewRegistry. DESIGN NOTE at service.go:248 states the registry is a container/state-holder, NOT a lifecycle object — Start (:256) / Stop (:262) never called by the daemon. | DELETE-CANDIDATE x2 |
| internal/services/terminal_service.go | 1 | WIRED-FILE-AUX | TerminalService constructed: service.go:184; HTTP handlers use GetHistory/ExecuteCommand/ListSessions/ClearHistory (api_handlers.go:2750-2809). GetSession (terminal_service.go:216) zero callers. UNCLAIMED. | DELETE-CANDIDATE x1 |
| internal/services/thread_service.go | 1 | WIRED-FILE-AUX | ThreadService constructed: service.go:141; RPC surface registered daemon.go:1316 -> thread_rpc.go (new/create/list/switch/set_active/current/get_active/delete). UpdateThread (thread_service.go:118) is not among the registered methods; zero callers. | DELETE-CANDIDATE x1 |
| internal/tui/app.go | 1 | WIRED-FILE-AUX | App IS constructed in cmd/meept (chat TUI entry). renderTabs (app.go:3072) zero prod callers — tab bar rendered inline; only tests call it (app_test.go:170, activity_view_test.go:90). Orphaned by a render refactor. UNCLAIMED. | DELETE-CANDIDATE x1 |
| internal/tui/confirmation.go | 7 | UNWIRED-FEATURE | ConfirmationModel constructed only via tools.go:63 NewConfirmationModel inside ConfirmationInterceptor, whose constructor NewConfirmationInterceptor (tools.go:32) has zero prod callers — the construction chain is never entered. Producing commits e2b3ab8b + 9a9b363f. asStringField has textual callers confirmation.go:64,65,87,90,93 inside the Update body (RTA-IMPRECISION). | WIRE-CANDIDATE x6, IMPRECISION-BASELINE x1 |
| internal/tui/format_duration.go | 1 | RTA-IMPRECISION | FormatDuration has verified textual callers internal/tui/quota_status.go:152,168 (same feature chain, commit 0fa34559 quota leaves 08/09); RTA missed the intra-chain textual call. | IMPRECISION-BASELINE x1 |
| internal/tui/instructions.go | 9 | UNWIRED-FEATURE | NewInstructionConfirmationModel zero prod callers (tests only: instructions_test.go). Producing commit 98fb867c (U1 gap 3.2 claims "TUI instruction confirmation dialog ... 12 tests") — dialog built, no command path opens it. Plan: docs/plans/user-instructions-verification-2026-06-23.md. DEFERRED-BY-PLAN (construction site never named). riskBorderColor/riskLevelStyle have textual callers instructions.go:76,110 (RTA-IMPRECISION). | WIRE-CANDIDATE x7, IMPRECISION-BASELINE x2 |
| internal/tui/models/chat.go | 1 | WIRED-FILE-AUX | ChatModel IS wired: app.go:334 constructs via models.NewChatModelWithConfig (chat.go:337). NewChatModel (chat.go:325) is the legacy wrapper — callers are tests only (app_test.go:39,498,560; headless_test.go:263). | DELETE-CANDIDATE x1 |
| internal/tui/models/chat_turn.go | 1 | WIRED-FILE-AUX | chat_turn.go is live (async-turn leaf 04, commit c546cc1b; wired from app.go:334 chat construction). renderTurnStalledText (chat_turn.go:493) zero prod callers — the update path builds stalled text inline via stalledTurnText (chat_turn.go:103); only chat_async_test.go:331 calls the helper. | DELETE-CANDIDATE x1 |
| internal/tui/models/status.go | 13 | UNWIRED-FEATURE | StatusModel referenced nowhere outside its own file (whole-repo grep incl. cmd/). Producing commit 557d9ecf "Go rewrite Phases 4-11" — dashboard model never mounted into App. UNCLAIMED. | DELETE-CANDIDATE x13 |
| internal/tui/models/turn.go | 6 | WIRED-FILE-AUX | turnRouter wired via newTurnRouter (turn.go:250) from ChatModel construction. manualTimerSource.After / noopTimerSource.After are timerSource interface methods dispatched through newTurnRouter(timers) — RTA misses interface dispatch (RTA-IMPRECISION). NewTestTurnRouter/NewTestTurnRouterWithTimers/TestAwaitTurnCmd/NewManualTimerSource are test helpers living in a production file — every caller is _test.go (week_bughunt_pins_test.go:41-124, chat_async_test.go:38). Producing commit c546cc1b leaf 04. | BASELINE x4, IMPRECISION-BASELINE x2 |
| internal/tui/notification_tracker.go | 4 | UNWIRED-FEATURE | zero callers outside the def file. Producing commit 79a54e48 (harness-eval leaves 16-18 "TUI + Flutter + daemon HTTP parity") — the TUI tracker was never wired. UNCLAIMED. | DELETE-CANDIDATE x4 |
| internal/tui/quota_status.go | 4 | WIRED-FILE-AUX | File is live: QuotaWaitLabel wired agents_panel.go:992/997; RenderQuotaDetailLines agents_panel.go:1219 (commit 0fa34559 leaves 08/09). FormatQuotaCountdownAt/QuotaCountdownTextAt have textual callers quota_status.go:159/141 (RTA-IMPRECISION). FormatQuotaCountdown "kept exported for direct parity testing" (comment :154); QuotaCountdownText zero prod callers — the badge renders via QuotaWaitLabel; both are parity twins of ui/flutter_ui quota_status.dart. | BASELINE x2, IMPRECISION-BASELINE x2 |
| internal/tui/selfimprove.go | 7 | UNWIRED-FEATURE | SelfImprovePanel never constructed in app.go/command_handler.go (rg NewSelfImprovePanel in prod: none). Producing commit 82d7a533 (SkillDesigner/Q-Agent) — panel built, never mounted. UNCLAIMED. | WIRE-CANDIDATE x7 |
| internal/tui/styles.go | 2 | WIRED-FILE-AUX | styles.go is live (DefaultStyles used across the TUI). repeatChar has textual callers styles.go:336,338 inside RenderProgressBar (RTA-IMPRECISION). RenderProgressBar (styles.go:326) zero callers — plans/tasks panels implement their own renderProgressBar (models/plans.go:777, models/tasks.go:674). | DELETE-CANDIDATE x1, IMPRECISION-BASELINE x1 |
| internal/tui/tools.go | 4 | UNWIRED-FEATURE | NewConfirmationInterceptor (tools.go:32) zero prod callers — the two-phase confirmation protocol for requires_confirmation tool results (producing commit 9a9b363f) is never entered from the agent tool-result path; internal/tui/tools_test.go is the only caller. | WIRE-CANDIDATE x4 |

## Function-level detail

### internal/bot/cost.go (3 funcs — UNWIRED-FEATURE)

Evidence: NewCostTracker has zero prod constructors (rg over *.go excl _test.go: definition only). Producing commit e9605179 "bot framework wiring ... CostTracker with daily budget enforcement". Plan: docs/plans/2026-06-21-automation-capability-extension.md Plan 1 Phases 3-4 (NotificationBot channels + budget-exhausted triggers).

| func | class | disposition |
|---|---|---|
| `CostTracker.IsBudgetExhausted` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `CostTracker.RecordExecution` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NewCostTracker` | UNWIRED-FEATURE | WIRE-CANDIDATE |

### internal/bot/notification_bot.go (12 funcs — UNWIRED-FEATURE)

Evidence: NewNotificationBot zero prod constructors; internal chain Start->handleEvent->shouldNotify/applyTemplate/containsKeyword (notification_bot.go:109,194-200,250) never entered. Producing commit 23998903 (plan1, Plans 1.1+1.3). Plan: automation plan 2026-06-21 Plan 1 Phase 3 "Create NotificationBot that pushes to channels".

| func | class | disposition |
|---|---|---|
| `NotificationBot.Start` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NotificationBot.Stop` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NotificationBot.allowRateLimit` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NotificationBot.auditLog` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NotificationBot.formatNotification` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NotificationBot.handleEvent` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NotificationBot.isChannelAllowed` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NotificationBot.mapEventToTrigger` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NotificationBot.shouldNotify` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NotificationBot.teardown` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `applyTemplate` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `containsKeyword` | UNWIRED-FEATURE | WIRE-CANDIDATE |

### internal/bot/webhook.go (2 funcs — UNWIRED-FEATURE)

Evidence: NewWebhookHandler zero callers; superseded — internal/comm/http/server.go:194: "Replaces the former botWebhookHandler; the webhook trigger endpoint now lives under /api/v1/agents/{id}/trigger". Producing commit 9a8be86c. UNCLAIMED.

| func | class | disposition |
|---|---|---|
| `NewWebhookHandler` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `WebhookHandler.ServeHTTP` | UNWIRED-FEATURE | DELETE-CANDIDATE |

### internal/comm/http/api_templates.go (2 funcs — UNWIRED-FEATURE)

Evidence: TemplatesService{} never constructed. Live equivalents: Server.handleTemplatesList (api_handlers.go:3520) / handleTemplatesInvoke (api_handlers.go:3563), registered server.go:1779-1781. Producing commit ab5ef133; impl superseded by the Server methods from the same feature.

| func | class | disposition |
|---|---|---|
| `TemplatesService.HandleInvoke` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `TemplatesService.HandleList` | UNWIRED-FEATURE | DELETE-CANDIDATE |

### internal/comm/http/events.go (9 funcs — UNWIRED-FEATURE)

Evidence: comm/http NewEventEmitter zero prod constructors. Wired impl is daemon.EventEmitter (internal/daemon/events.go:99; constructed components.go:2584; passed via http.WithNotification daemon.go:1670) satisfying the http.NotificationEmitter interface (notification_handlers.go:62) — Subscribe/Unsubscribe/GetEventsSince are called through that interface at notification_handlers.go:102-166, not on this type. Producing commit cea17473 (analytics + menubar notifications plan).

| func | class | disposition |
|---|---|---|
| `EventEmitter.Close` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `EventEmitter.GetEventsSince` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `EventEmitter.Publish` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `EventEmitter.PublishTaskNotification` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `EventEmitter.Subscribe` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `EventEmitter.Unsubscribe` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `NewEventEmitter` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `generateUUID` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `subscriber.close` | UNWIRED-FEATURE | DELETE-CANDIDATE |

### internal/comm/http/server.go (6 funcs — WIRED-FILE-AUX)

Evidence: Server constructed daemon.go:1791 http.NewServer; WS relay live at server.go:878/950 via h.ShouldSend. ShouldSendProgress has textual caller server.go:712 (RTA-IMPRECISION). Addr (:1355), ChatSubmitter (:1349), Broadcast (:716), ClientCount (:547), progressRateLimiter.cleanup (:489) have zero textual callers.

| func | class | disposition |
|---|---|---|
| `Server.Addr` | WIRED-FILE-AUX | DELETE-CANDIDATE |
| `Server.ChatSubmitter` | WIRED-FILE-AUX | DELETE-CANDIDATE |
| `WebSocketHub.Broadcast` | WIRED-FILE-AUX | DELETE-CANDIDATE |
| `WebSocketHub.ClientCount` | WIRED-FILE-AUX | DELETE-CANDIDATE |
| `WebSocketHub.ShouldSendProgress` | RTA-IMPRECISION | IMPRECISION-BASELINE |
| `progressRateLimiter.cleanup` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/comm/http/sse.go (2 funcs — WIRED-FILE-AUX)

Evidence: NewSSEWriter used api_handlers.go:263; SendEvent/SendError/SendComment live (api_handlers.go:273-441). SSEWriter.Write implements io.Writer (RTA-IMPRECISION). CloseWithFinalEvent zero callers — final-event protocol from commit a8ee0b3e (tool streaming) never completed (PLANNED-OPEN).

| func | class | disposition |
|---|---|---|
| `SSEWriter.CloseWithFinalEvent` | WIRED-FILE-AUX | DELETE-CANDIDATE |
| `SSEWriter.Write` | RTA-IMPRECISION | IMPRECISION-BASELINE |

### internal/comm/http/voice.go (1 funcs — WIRED-FILE-AUX)

Evidence: VoiceRoutes IS constructed: internal/daemon/voice_wiring.go:532 commhttp.NewVoiceRoutes; wired daemon.go:1782 http.WithVoiceRoutes; routes served via HandlesPath -> serveVoiceRoute (server.go:2053->2060), never via Register(mux). Register (voice.go:212) zero callers — orphaned mount helper.

| func | class | disposition |
|---|---|---|
| `VoiceRoutes.Register` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/comm/telegram/base_bot.go (3 funcs — UNWIRED-FEATURE)

Evidence: newBaseBot called only from NewTelegramChannel (channel.go:68) which is never constructed in prod (internal/bot/iface_check.go:8-9 are compile-time interface pins only). Producing commit 23998903 (plan1). Plan: automation plan 2026-06-21 Plan 1.

| func | class | disposition |
|---|---|---|
| `baseBot.ID` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `baseBot.Name` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `newBaseBot` | UNWIRED-FEATURE | WIRE-CANDIDATE |

### internal/comm/telegram/channel.go (8 funcs — UNWIRED-FEATURE)

Evidence: NewTelegramChannel zero prod constructors; the daemon wires telegram.Bot + AgentHandler directly (components.go:5068-5084 telegram.NewAgentHandler / telegram.NewBot), bypassing the MessagingBot adapter. Producing commit 23998903 plan1 Phase 2. Plan: automation plan 2026-06-21 Plan 1 "Create TelegramChannel type implementing MessagingBot".

| func | class | disposition |
|---|---|---|
| `ChatIDToTarget` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NewTelegramChannel` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `TelegramChannel.BotClient` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `TelegramChannel.CanInitiate` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `TelegramChannel.Execute` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `TelegramChannel.Handler` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `TelegramChannel.SendMessage` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `TelegramChannel.Stop` | UNWIRED-FEATURE | WIRE-CANDIDATE |

### internal/comm/telegram/handler.go (1 funcs — WIRED-FILE-AUX)

Evidence: AgentHandler IS constructed: components.go:5068 telegram.NewAgentHandler (wired). saveSessions (handler.go:203) zero prod callers — chat->session persistence path never invoked.

| func | class | disposition |
|---|---|---|
| `AgentHandler.saveSessions` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/services/bot_context.go (1 funcs — WIRED-FILE-AUX)

Evidence: BotContextImpl IS constructed: components.go:2652 services.NewBotContext(c.PushService,...) ("Task 8, dormant wiring closure"). PushNotification (bot_context.go:27) zero callers — the bot.BotContext interface (internal/bot/interfaces.go:41) has no consumer. Producing commit 0821df6b (Plan 3.5 Phase 4 bot integration). DEFERRED-BY-PLAN: context built, bots never handed it.

| func | class | disposition |
|---|---|---|
| `BotContextImpl.PushNotification` | WIRED-FILE-AUX | WIRE-CANDIDATE |

### internal/services/employee_service.go (1 funcs — WIRED-FILE-AUX)

Evidence: EmployeeService constructed: internal/services/service.go:225 NewEmployeeService (when EmployeeManager configured). Get (employee_service.go:77) not called by any RPC/HTTP handler. UNCLAIMED.

| func | class | disposition |
|---|---|---|
| `EmployeeService.Get` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/services/groups_service.go (2 funcs — WIRED-FILE-AUX)

Evidence: Deliberate signature-pin stubs: groups_service.go:44-51 "signature-pin stub (rpc mirrors this method set; never invoked for a value)". Intentional API surface — deleting them deletes the convention.

| func | class | disposition |
|---|---|---|
| `checkShape` | WIRED-FILE-AUX | BASELINE |
| `explainShape` | WIRED-FILE-AUX | BASELINE |

### internal/services/memory_service.go (1 funcs — WIRED-FILE-AUX)

Evidence: MemoryService constructed: service.go:133 NewMemoryService. BridgeCount (memory_service.go:166) zero callers. UNCLAIMED.

| func | class | disposition |
|---|---|---|
| `MemoryService.BridgeCount` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/services/model_service.go (1 funcs — WIRED-FILE-AUX)

Evidence: ModelService constructed: service.go:172 NewModelService. GetCredentialRaw (model_service.go:239) zero callers (config-export consumer never built). UNCLAIMED.

| func | class | disposition |
|---|---|---|
| `ModelService.GetCredentialRaw` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/services/notification_prefs.go (7 funcs — UNWIRED-FEATURE)

Evidence: Zero consumers repo-wide: rg DefaultNotificationPreferences|NotificationRateLimiter|IsChannelEnabled|IsInDND|IsTypeEnabled outside the def file returns nothing (non-test). Producing commit 0821df6b "Plan 3.5 - User Controls for Notifications"; plan: automation plan 2026-06-21 Plan 3 Phase 5 (per-user prefs, rate limit, DND) — plan marked COMPLETE but no consumer ever built (PLANNED-OPEN in practice).

| func | class | disposition |
|---|---|---|
| `DefaultNotificationPreferences` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NewNotificationRateLimiter` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NotificationPreferences.IsChannelEnabled` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NotificationPreferences.IsInDND` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NotificationPreferences.IsTypeEnabled` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NotificationRateLimiter.Allow` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `parseTime` | UNWIRED-FEATURE | WIRE-CANDIDATE |

### internal/services/prompt_service.go (1 funcs — WIRED-FILE-AUX)

Evidence: PromptService constructed: service.go:225-231 NewPromptServiceFromDirs / NewDefaultPromptService. ProjectDir (prompt_service.go:147) zero callers. UNCLAIMED.

| func | class | disposition |
|---|---|---|
| `PromptService.ProjectDir` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/services/push_channels.go (12 funcs — WIRED-FILE-AUX)

Evidence: ChannelRegistry IS constructed: components.go:2599 services.NewChannelRegistry; Register called components.go:2612 (HTTP channel) + 2625 (Slack channel). Registry.Push reachable only via PushToChannels (push_service.go:139) which has zero callers. New* for CLI/TUI/Telegram channels have zero prod constructors. Producing commit 3c705186 (plan3.3). Plan: automation plan 2026-06-21 Plan 3 Phase 3 (Telegram/CLI/TUI routing explicitly listed).

| func | class | disposition |
|---|---|---|
| `CLIPushChannel.CanReceive` | WIRED-FILE-AUX | WIRE-CANDIDATE |
| `CLIPushChannel.Push` | WIRED-FILE-AUX | WIRE-CANDIDATE |
| `ChannelRegistry.Push` | WIRED-FILE-AUX | WIRE-CANDIDATE |
| `ChannelRegistry.Remove` | WIRED-FILE-AUX | WIRE-CANDIDATE |
| `NewCLIPushChannel` | WIRED-FILE-AUX | WIRE-CANDIDATE |
| `NewTUIPushChannel` | WIRED-FILE-AUX | WIRE-CANDIDATE |
| `NewTelegramPushChannel` | WIRED-FILE-AUX | WIRE-CANDIDATE |
| `TUIPushChannel.CanReceive` | WIRED-FILE-AUX | WIRE-CANDIDATE |
| `TUIPushChannel.Push` | WIRED-FILE-AUX | WIRE-CANDIDATE |
| `TelegramPushChannel.CanReceive` | WIRED-FILE-AUX | WIRE-CANDIDATE |
| `TelegramPushChannel.Push` | WIRED-FILE-AUX | WIRE-CANDIDATE |
| `formatForTelegram` | WIRED-FILE-AUX | WIRE-CANDIDATE |

### internal/services/push_history.go (5 funcs — UNWIRED-FEATURE)

Evidence: NewPushHistory zero prod constructors. Producing commit 0821df6b (Plan 3.5 Phase 5 "Push history/query API"). Plan claims complete; consumer absent.

| func | class | disposition |
|---|---|---|
| `NewPushHistory` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `PushHistory.Clear` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `PushHistory.Query` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `PushHistory.QueryAll` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `PushHistory.Record` | UNWIRED-FEATURE | WIRE-CANDIDATE |

### internal/services/push_service.go (2 funcs — WIRED-FILE-AUX)

Evidence: PushService IS constructed: components.go:2615 NewPushServiceWithChannels; live path is Push (called bot_context.go:32 and by the quota notifier). NewPushService (bus-only ctor, push_service.go:75) zero prod callers; PushToChannels (push_service.go:108) zero callers — it is the plan3.3 channel-routing seam.

| func | class | disposition |
|---|---|---|
| `NewPushService` | WIRED-FILE-AUX | DELETE-CANDIDATE |
| `PushService.PushToChannels` | WIRED-FILE-AUX | WIRE-CANDIDATE |

### internal/services/quota_notifier.go (2 funcs — WIRED-FILE-AUX)

Evidence: QuotaNotifier IS constructed: components.go:2647 NewQuotaNotifier — auto-starts its pump (quota_notifier.go:75 "Auto-start... no explicit Start call needed"). Exported Start superseded by auto-start; Cleanup zero callers — no daemon-shutdown path stops the pump.

| func | class | disposition |
|---|---|---|
| `QuotaNotifier.Cleanup` | WIRED-FILE-AUX | WIRE-CANDIDATE |
| `QuotaNotifier.Start` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/services/security_service.go (1 funcs — WIRED-FILE-AUX)

Evidence: SecurityService constructed: service.go:162; SetAuditDB wired daemon.go:1205-1207. Audit (security_service.go:77) zero callers — no handler exposes the query. UNCLAIMED.

| func | class | disposition |
|---|---|---|
| `SecurityService.Audit` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/services/selfimprove_service.go (1 funcs — WIRED-FILE-AUX)

Evidence: SelfImproveService constructed: service.go:156. Cancel (selfimprove_service.go:67) zero callers (producing commit 82d7a533 SkillDesigner/Q-Agent). UNCLAIMED.

| func | class | disposition |
|---|---|---|
| `SelfImproveService.Cancel` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/services/service.go (2 funcs — WIRED-FILE-AUX)

Evidence: Registry constructed: daemon.go:1167 services.NewRegistry. DESIGN NOTE at service.go:248 states the registry is a container/state-holder, NOT a lifecycle object — Start (:256) / Stop (:262) never called by the daemon.

| func | class | disposition |
|---|---|---|
| `ServiceRegistry.Start` | WIRED-FILE-AUX | DELETE-CANDIDATE |
| `ServiceRegistry.Stop` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/services/terminal_service.go (1 funcs — WIRED-FILE-AUX)

Evidence: TerminalService constructed: service.go:184; HTTP handlers use GetHistory/ExecuteCommand/ListSessions/ClearHistory (api_handlers.go:2750-2809). GetSession (terminal_service.go:216) zero callers. UNCLAIMED.

| func | class | disposition |
|---|---|---|
| `TerminalService.GetSession` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/services/thread_service.go (1 funcs — WIRED-FILE-AUX)

Evidence: ThreadService constructed: service.go:141; RPC surface registered daemon.go:1316 -> thread_rpc.go (new/create/list/switch/set_active/current/get_active/delete). UpdateThread (thread_service.go:118) is not among the registered methods; zero callers.

| func | class | disposition |
|---|---|---|
| `ThreadService.UpdateThread` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/tui/app.go (1 funcs — WIRED-FILE-AUX)

Evidence: App IS constructed in cmd/meept (chat TUI entry). renderTabs (app.go:3072) zero prod callers — tab bar rendered inline; only tests call it (app_test.go:170, activity_view_test.go:90). Orphaned by a render refactor. UNCLAIMED.

| func | class | disposition |
|---|---|---|
| `App.renderTabs` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/tui/confirmation.go (7 funcs — UNWIRED-FEATURE)

Evidence: ConfirmationModel constructed only via tools.go:63 NewConfirmationModel inside ConfirmationInterceptor, whose constructor NewConfirmationInterceptor (tools.go:32) has zero prod callers — the construction chain is never entered. Producing commits e2b3ab8b + 9a9b363f. asStringField has textual callers confirmation.go:64,65,87,90,93 inside the Update body (RTA-IMPRECISION).

| func | class | disposition |
|---|---|---|
| `ConfirmationModel.Init` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `ConfirmationModel.IsCancelled` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `ConfirmationModel.IsConfirmed` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `ConfirmationModel.Update` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `ConfirmationModel.View` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NewConfirmationModel` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `asStringField` | RTA-IMPRECISION | IMPRECISION-BASELINE |

### internal/tui/format_duration.go (1 funcs — RTA-IMPRECISION)

Evidence: FormatDuration has verified textual callers internal/tui/quota_status.go:152,168 (same feature chain, commit 0fa34559 quota leaves 08/09); RTA missed the intra-chain textual call.

| func | class | disposition |
|---|---|---|
| `FormatDuration` | RTA-IMPRECISION | IMPRECISION-BASELINE |

### internal/tui/instructions.go (9 funcs — UNWIRED-FEATURE)

Evidence: NewInstructionConfirmationModel zero prod callers (tests only: instructions_test.go). Producing commit 98fb867c (U1 gap 3.2 claims "TUI instruction confirmation dialog ... 12 tests") — dialog built, no command path opens it. Plan: docs/plans/user-instructions-verification-2026-06-23.md. DEFERRED-BY-PLAN (construction site never named). riskBorderColor/riskLevelStyle have textual callers instructions.go:76,110 (RTA-IMPRECISION).

| func | class | disposition |
|---|---|---|
| `InstructionConfirmationModel.Data` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `InstructionConfirmationModel.Init` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `InstructionConfirmationModel.IsCancelled` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `InstructionConfirmationModel.IsConfirmed` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `InstructionConfirmationModel.Update` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `InstructionConfirmationModel.View` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NewInstructionConfirmationModel` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `riskBorderColor` | RTA-IMPRECISION | IMPRECISION-BASELINE |
| `riskLevelStyle` | RTA-IMPRECISION | IMPRECISION-BASELINE |

### internal/tui/models/chat.go (1 funcs — WIRED-FILE-AUX)

Evidence: ChatModel IS wired: app.go:334 constructs via models.NewChatModelWithConfig (chat.go:337). NewChatModel (chat.go:325) is the legacy wrapper — callers are tests only (app_test.go:39,498,560; headless_test.go:263).

| func | class | disposition |
|---|---|---|
| `NewChatModel` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/tui/models/chat_turn.go (1 funcs — WIRED-FILE-AUX)

Evidence: chat_turn.go is live (async-turn leaf 04, commit c546cc1b; wired from app.go:334 chat construction). renderTurnStalledText (chat_turn.go:493) zero prod callers — the update path builds stalled text inline via stalledTurnText (chat_turn.go:103); only chat_async_test.go:331 calls the helper.

| func | class | disposition |
|---|---|---|
| `renderTurnStalledText` | WIRED-FILE-AUX | DELETE-CANDIDATE |

### internal/tui/models/status.go (13 funcs — UNWIRED-FEATURE)

Evidence: StatusModel referenced nowhere outside its own file (whole-repo grep incl. cmd/). Producing commit 557d9ecf "Go rewrite Phases 4-11" — dashboard model never mounted into App. UNCLAIMED.

| func | class | disposition |
|---|---|---|
| `NewStatusModel` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `StatusModel.Init` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `StatusModel.Update` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `StatusModel.View` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `StatusModel.fetchStatus` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `StatusModel.renderDashboard` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `StatusModel.renderError` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `StatusModel.renderInfoPanel` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `StatusModel.renderLoading` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `StatusModel.renderMetricsPanel` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `StatusModel.renderProgressBar` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `StatusModel.renderStatusPanel` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `StatusModel.tickCmd` | UNWIRED-FEATURE | DELETE-CANDIDATE |

### internal/tui/models/turn.go (6 funcs — WIRED-FILE-AUX)

Evidence: turnRouter wired via newTurnRouter (turn.go:250) from ChatModel construction. manualTimerSource.After / noopTimerSource.After are timerSource interface methods dispatched through newTurnRouter(timers) — RTA misses interface dispatch (RTA-IMPRECISION). NewTestTurnRouter/NewTestTurnRouterWithTimers/TestAwaitTurnCmd/NewManualTimerSource are test helpers living in a production file — every caller is _test.go (week_bughunt_pins_test.go:41-124, chat_async_test.go:38). Producing commit c546cc1b leaf 04.

| func | class | disposition |
|---|---|---|
| `NewManualTimerSource` | WIRED-FILE-AUX | BASELINE |
| `NewTestTurnRouter` | WIRED-FILE-AUX | BASELINE |
| `NewTestTurnRouterWithTimers` | WIRED-FILE-AUX | BASELINE |
| `TestAwaitTurnCmd` | WIRED-FILE-AUX | BASELINE |
| `manualTimerSource.After` | RTA-IMPRECISION | IMPRECISION-BASELINE |
| `noopTimerSource.After` | RTA-IMPRECISION | IMPRECISION-BASELINE |

### internal/tui/notification_tracker.go (4 funcs — UNWIRED-FEATURE)

Evidence: zero callers outside the def file. Producing commit 79a54e48 (harness-eval leaves 16-18 "TUI + Flutter + daemon HTTP parity") — the TUI tracker was never wired. UNCLAIMED.

| func | class | disposition |
|---|---|---|
| `NewNotifySnapshot` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `NotifySnapshot.Peek` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `NotifySnapshot.Take` | UNWIRED-FEATURE | DELETE-CANDIDATE |
| `NotifySnapshot.Update` | UNWIRED-FEATURE | DELETE-CANDIDATE |

### internal/tui/quota_status.go (4 funcs — WIRED-FILE-AUX)

Evidence: File is live: QuotaWaitLabel wired agents_panel.go:992/997; RenderQuotaDetailLines agents_panel.go:1219 (commit 0fa34559 leaves 08/09). FormatQuotaCountdownAt/QuotaCountdownTextAt have textual callers quota_status.go:159/141 (RTA-IMPRECISION). FormatQuotaCountdown "kept exported for direct parity testing" (comment :154); QuotaCountdownText zero prod callers — the badge renders via QuotaWaitLabel; both are parity twins of ui/flutter_ui quota_status.dart.

| func | class | disposition |
|---|---|---|
| `FormatQuotaCountdown` | WIRED-FILE-AUX | BASELINE |
| `FormatQuotaCountdownAt` | RTA-IMPRECISION | IMPRECISION-BASELINE |
| `QuotaCountdownText` | WIRED-FILE-AUX | BASELINE |
| `QuotaCountdownTextAt` | RTA-IMPRECISION | IMPRECISION-BASELINE |

### internal/tui/selfimprove.go (7 funcs — UNWIRED-FEATURE)

Evidence: SelfImprovePanel never constructed in app.go/command_handler.go (rg NewSelfImprovePanel in prod: none). Producing commit 82d7a533 (SkillDesigner/Q-Agent) — panel built, never mounted. UNCLAIMED.

| func | class | disposition |
|---|---|---|
| `NewSelfImprovePanel` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `SelfImprovePanel.Init` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `SelfImprovePanel.Update` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `SelfImprovePanel.View` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `SelfImprovePanel.approveCurrent` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `SelfImprovePanel.fetchPending` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `SelfImprovePanel.rejectCurrent` | UNWIRED-FEATURE | WIRE-CANDIDATE |

### internal/tui/styles.go (2 funcs — WIRED-FILE-AUX)

Evidence: styles.go is live (DefaultStyles used across the TUI). repeatChar has textual callers styles.go:336,338 inside RenderProgressBar (RTA-IMPRECISION). RenderProgressBar (styles.go:326) zero callers — plans/tasks panels implement their own renderProgressBar (models/plans.go:777, models/tasks.go:674).

| func | class | disposition |
|---|---|---|
| `RenderProgressBar` | WIRED-FILE-AUX | DELETE-CANDIDATE |
| `repeatChar` | RTA-IMPRECISION | IMPRECISION-BASELINE |

### internal/tui/tools.go (4 funcs — UNWIRED-FEATURE)

Evidence: NewConfirmationInterceptor (tools.go:32) zero prod callers — the two-phase confirmation protocol for requires_confirmation tool results (producing commit 9a9b363f) is never entered from the agent tool-result path; internal/tui/tools_test.go is the only caller.

| func | class | disposition |
|---|---|---|
| `ConfirmationInterceptor.ConfirmWithReexecute` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `ConfirmationInterceptor.HandleConfirmationResult` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `NewConfirmationInterceptor` | UNWIRED-FEATURE | WIRE-CANDIDATE |
| `defaultRunModal` | UNWIRED-FEATURE | WIRE-CANDIDATE |

## Verification notes

- **Numbers verified:** parsed 150 entries from the scope file; per-file counts recomputed and diffed against the scope file — zero mismatches; per-class and per-disposition totals recomputed from the function-level table (150).
- **Key construction sites checked:** `components.go:2584,2599,2612,2615,2625,2647,2652,5068-5084`; `daemon.go:1167,1205-1207,1316,1645,1670,1782,1791`; `internal/services/service.go:133-231`; `internal/daemon/voice_wiring.go:532`; `internal/comm/http/api_handlers.go:263,3519-3563`; `internal/comm/http/server.go:878,950,1779-1781,2053-2060`; `internal/daemon/thread_rpc.go` (registered method set); `internal/tui/app.go:334`; `internal/tui/agents_panel.go:992-997,1219`; `internal/bot/iface_check.go:8-9`.
- **RTA-imprecision mechanisms observed:** bubbletea Model interface dispatch (Init/Update/View), small interface dispatch (`timerSource.After`, `io.Writer`), and textual calls inside wired render/update chains (`format_duration.go`, `quota_status.go`, `styles.go`, `server.go:712`).
- **Plan archaeology:** commits mapped via `git log --diff-filter=A`; plans referenced: `docs/plans/2026-06-21-automation-capability-extension.md` (Plans 1.x/3.x — status line claims "COMPLETE" but several Phase 3/3.5 consumers were never built), commit 0821df6b (Plan 3.5), 98fb867c (U1 gaps / user-instructions-verification-2026-06-23), 0fa34559 (quota leaves 08/09), c546cc1b (async-turn leaf 04), 79a54e48 (harness-eval 16-18), cea17473 (menubar notifications), ab5ef133 (slash HTTP API), a8ee0b3e (tool streaming), 9a8be86c/9a9b363f/e2b3ab8b (webhook + confirmation), e9605179 (cost tracker), 3c705186 (plan3.3), 557d9ecf (Go rewrite), 82d7a533 (SkillDesigner).
- **MIXED clusters:** `internal/comm/http/server.go` (1 imprecision + 5 deletes), `internal/comm/http/sse.go` (1 imprecision + 1 planned-open delete), `internal/services/push_channels.go` (12 wire-candidates — registry wired, channels not), `internal/services/push_service.go` (1 wire + 1 delete), `internal/services/quota_notifier.go` (1 wire + 1 delete), `internal/tui/confirmation.go` (6 wire + 1 imprecision), `internal/tui/instructions.go` (7 wire + 2 imprecision), `internal/tui/models/turn.go` (4 baseline test-helpers + 2 imprecision), `internal/tui/quota_status.go` (2 baseline parity API + 2 imprecision), `internal/tui/styles.go` (1 delete + 1 imprecision).
- **READ-ONLY:** no repository files were modified by this survey.

## Wave-1 disposition (2026-11-09, delete wave)

All DELETE-CANDIDATE rows in scope re-grepped at HEAD before deletion; any row found with a real (non-test) caller was KEPT and reclassified as IMPRECISION. Committed per concern with explicit paths.

### Commits (this scope)

| Commit | Concern | Rows closed |
|---|---|---|
| 6e81daa6 | bot/webhook.go + webhook_test.go deleted | 2 (NewWebhookHandler, WebhookHandler.ServeHTTP) |
| 3ae1190d | comm/http api_templates.go + events.go + events_test.go deleted | 11 (TemplatesService.HandleInvoke/HandleList x2; EventEmitter cluster x9) |
| 14e189b0 | comm/http server.go: Server.Addr, WebSocketHub.Broadcast, WebSocketHub.ClientCount, progressRateLimiter.cleanup | 4 |
| 4adcc0f8 | comm/http sse.go CloseWithFinalEvent; voice.go VoiceRoutes.Register | 2 |
| 70b41b53 | services: NewPushService (+PushServiceOption), QuotaNotifier.Start, SecurityService.Audit cluster (+SetAuditDB/auditDB), EmployeeService.Get, MemoryService.BridgeCount, ModelService.GetCredentialRaw, PromptService.ProjectDir, TerminalService.GetSession, ThreadService.UpdateThread (+UpdateThreadRequest), SelfImproveService.Cancel (+CancelRequest) | 11 |
| e5719c48 | tui: models/status.go StatusModel cluster (whole file), notification_tracker.go NotifySnapshot cluster (whole file), models/chat.go NewChatModel, models/chat_turn.go renderTurnStalledText, app.go renderTabs | 21 |

### Rows kept (re-grep found real callers or explicit keep-directive)

- `Server.ChatSubmitter` — real caller: internal/daemon/http_only_submit_test.go:45 (composition pin, week bughunt finding 17). KEPT; report row was test-caller-imprecise. Test-shim note: Server.Addr removal is covered by testaddr_helper_test.go/export_test_addr_test.go (never shipped).
- `quota_notifier.go` file — KEPT per directive (Cleanup is the Wave-3 shutdown fix). Only the dead exported `Start` was deleted (its lifecycle test updated).
- `ServiceRegistry.Start/Stop` — KEPT per directive (design-tension row; doc fix in Wave 3). Deleted then restored before commit; final commit leaves service.go untouched.
- `tui/models/turn.go` — KEPT per directive (test-helper move is Wave 3).
- Notification-stack wire rows (bot cost.go, notification_bot.go, telegram, push_channels, push_history, notification_prefs, bot_context, ConfirmationInterceptor/instructions/selfimprove panels, PushToChannels, BotContextImpl.PushNotification) — untouched (Wave-2 wiring scope).
- groups_service.go signature pins (BASELINE) — untouched.
- All 11 RTA-IMPRECISION rows — untouched.

### Disposition corrections vs. report

- `internal/comm/http/events.go`: no EventType-style constants were shared out of this file (NotificationEvent/NotificationType live in notification_handlers.go and stay) — whole-file delete, no surgical split needed.
- `internal/tui/app.go renderTabs`: report classed test callers as not counting; the two tests pin the tab-inventory contract, so the body moved to a _test.go helper (renderTabsForTest) rather than being deleted outright — production surface still shrinks by the method.
- `NewPushService`: report said zero prod callers (true); its ~24 test call sites migrated to `NewPushServiceWithChannels(bus, nil, logger)` (identical bus-only semantics: channels=nil).

### Gates

- `go build ./...` clean at each commit (transient failures during the wave were sibling agents' in-flight edits in internal/llm / internal/agent, not this scope).
- `go test -count=1`: internal/bot, internal/comm/http, internal/services, internal/tui, internal/tui/models — all ok after respective commits.
- `.githooks/deadcode-baseline.txt` untouched.
