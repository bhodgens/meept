// notification_bot_wiring.go wires the proactive notification bot stack
// (automation plan 2026-06-21 Plans 1.x/3.x; deadcode wave-2 WIRE scope)
// into the daemon:
//
//   - NotificationBot subscribes to the daemon EventEmitter and fans
//     budget/security/session-designation notifications out to Telegram
//     chat IDs (Plan 1 Phase 3).
//   - CostTracker attaches to the bot store so daily-budget enforcement
//     has a production constructor (Plan 1 budget-exhausted triggers;
//     the runner's ShouldRun already enforces the persisted budget).
//   - PushHistory records push deliveries (Plan 3 Phase 5 history/query).
//   - NotificationPreferences + NotificationRateLimiter attach to the
//     push service as the per-user delivery gate (Plan 3 Phase 5).
//   - TelegramPushChannel registers on the push channel registry so
//     PushToChannels routes to Telegram (Plan 3 Phase 3).
//
// Integration (not clobber): the LIVE direct telegram.Bot/AgentHandler
// wiring (components.go [telegram] block) and the existing HTTP+Slack
// push channel registrations (components.go NewComponents) are untouched;
// this wiring adds the bot stack ALONGSIDE them.
//
// Kill-switch: the whole stack is gated on [notifications].enabled
// (config.NotificationsConfig.Enabled, default false — additive opt-in
// same posture as [intake] and [comm.slack]). Disabled = zero new
// surfaces, zero goroutines, no channel registration. The desktop
// notification emitter path does NOT read this flag.
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/services"
)

// notificationBotJobName is the scheduler job ID for the CostTracker
// budget-marker cleanup tick. Gate tests pin it; RunAtInterval is
// idempotent per name (employeeSchedulerAdapter contract).
const notificationBotJobName = "notification_bot_budget_cleanup"

// defaultBudgetCleanupInterval is the cleanup cadence for the quota
// notifier dedup-entry prune scheduled by the same seam (see
// wireNotificationBot).
const defaultBudgetCleanupInterval = 6 * time.Hour

// defaultPushHistorySize is the bounded entry count for the push
// history ring (services.PushHistory default).
const defaultPushHistorySize = 1000

// defaultNotifyRatePerHour is the notification bot's per-hour send cap
// when [notifications].bot_rate_limit_per_hour is unset.
const defaultNotifyRatePerHour = 20

// wireNotificationBot constructs the notification bot stack when
// [notifications].enabled=true. Disabled (the default) -> Debug log and
// return: no bot, no channel, no history, no goroutine. Enabled ->
// best-effort construction: individual pieces degrade independently with
// a Warn, matching the wireSlackChannel fail-closed posture, so a broken
// piece never blocks the rest of boot.
//
// Free function (not a method) per the gate-test convention
// (intake_wiring_gate_test.go calls wireIntakePoller(c)); tests call
// wireNotificationBot(c) on a minimal Components fixture.
func wireNotificationBot(c *Components) {
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	cfg := c.Config
	if cfg == nil {
		logger.Debug("notification bot wiring skipped: no config")
		return
	}
	nc := cfg.Notifications
	if !nc.Enabled {
		logger.Debug("notification bot stack disabled ([notifications].enabled=false)")
		return
	}

	// BotStore is required: the CostTracker persists per-bot budget
	// state through it. Without the store (bots disabled and no bot
	// framework initialized) the stack degrades to bot + prefs only.
	botStore := c.BotStore

	// CostTracker (Plan 1 budget enforcement): production constructor
	// for internal/bot/cost.go. Budget gates stay enforced through
	// BotRunner.ShouldRun reading the persisted state this tracker
	// maintains. Scheduled marker-cleanup rides the scheduler when one
	// exists (same seam as the intake poller).
	if botStore != nil {
		c.CostTracker = bot.NewCostTracker(botStore, logger.With("component", "bot-cost-tracker"))
		if c.Scheduler != nil {
			employeeSchedulerAdapter{sched: c.Scheduler}.RunAtInterval(
				notificationBotJobName, defaultBudgetCleanupInterval, func() {
					// Best-effort housekeeping: prune the quota
					// notifier's dedup map so long runs stay bounded
					// (deadcode report: Cleanup had zero callers).
					if c.QuotaNotifier != nil {
						c.QuotaNotifier.Cleanup()
					}
				})
		}
		logger.Info("bot cost tracker attached", "budget_cleanup_job", notificationBotJobName)
	}

	// NotificationPreferences + per-session rate limiter (Plan 3 Phase 5
	// user controls). Attached to the push service so delivery decisions
	// consult the configured prefs; defaults allow everything.
	prefs := services.DefaultNotificationPreferences()
	if nc.BotRateLimitPerHour > 0 {
		prefs.RateLimit = nc.BotRateLimitPerHour
	}
	c.NotificationPrefs = &prefs
	c.NotificationRateLimiter = services.NewNotificationRateLimiter(prefs.RateLimit)
	if c.PushService != nil {
		c.PushService.SetNotificationPreferences(&prefs)
	}

	// Push history (Plan 3 Phase 5 history/query API). The push service
	// records every delivered push into it.
	c.PushHistory = services.NewPushHistory(defaultPushHistorySize)
	if c.PushService != nil {
		c.PushService.SetPushHistory(c.PushHistory)
	}

	// NotificationBot (Plan 1 Phase 3): subscribes to the daemon event
	// emitter and fans out to Telegram. Started in Components.Start
	// with the lifecycle context; passive (no goroutine) when the
	// emitter is nil.
	botCfg := bot.NotificationBotConfig{
		Enabled:             true,
		TelegramChatIDs:     append([]string(nil), nc.BotTelegramChatIDs...),
		NotifyOn:            append([]string(nil), nc.BotNotifyOn...),
		RateLimitPerHour:    nc.BotRateLimitPerHour,
		Templates:           nc.BotTemplates,
		TelegramPollTimeout: cfg.Telegram.PollTimeout,
	}
	if botCfg.RateLimitPerHour <= 0 {
		botCfg.RateLimitPerHour = defaultNotifyRatePerHour
	}
	if len(botCfg.NotifyOn) == 0 {
		// Sensible default: all three planned triggers.
		botCfg.NotifyOn = []string{
			string(bot.TriggerBudgetExhausted),
			string(bot.TriggerSecurityAlert),
			string(bot.TriggerSessionDesignated),
		}
	}
	// Typed-nil guard (internal/daemon/AGENTS.md): a nil *EventEmitter
	// assigned into the EventEmitterAdapter interface field boxes
	// nil-as-non-nil and Start's emitter check passes into a nil-deref.
	// Pass the concrete pointer only when it is actually non-nil.
	var emitter bot.EventEmitterAdapter
	if c.NotificationEmitter != nil {
		emitter = c.NotificationEmitter
	}
	notifier := bot.NewNotificationBot(botCfg, emitter, logger.With("component", "notification-bot"))
	if notifier != nil && c.TelegramBot != nil {
		notifier.SetTelegram(c.TelegramBot)
	}
	c.NotificationBot = notifier

	// TelegramPushChannel (Plan 3 Phase 3 channel routing): registers on
	// the push registry so PushToChannels delivers to the configured
	// Telegram chats. Requires a live telegram bot; skipped (Warn) when
	// the [telegram] block is off. Broadcast mode: chat IDs come from
	// [notifications].bot_telegram_chat_ids.
	if c.TelegramBot != nil && c.PushChannels != nil && len(nc.BotTelegramChatIDs) > 0 {
		chatIDs := make([]int64, 0, len(nc.BotTelegramChatIDs))
		for _, s := range nc.BotTelegramChatIDs {
			var id int64
			// Malformed IDs parse to 0 and are skipped; Sscanf's error
			// adds nothing over that zero check.
			_, _ = fmt.Sscanf(s, "%d", &id)
			if id != 0 {
				chatIDs = append(chatIDs, id)
			}
		}
		if len(chatIDs) > 0 {
			tgCh, err := services.NewTelegramPushChannel(c.TelegramBot, nil, chatIDs,
				logger.With("component", "push-channel-telegram"))
			if err != nil {
				logger.Warn("telegram push channel not registered", "error", err)
			} else {
				c.PushChannels.Register(tgCh)
				logger.Info("telegram push channel registered",
					"chats", len(chatIDs))
			}
		}
	}

	logger.Info("notification bot stack wired",
		"triggers", botCfg.NotifyOn,
		"rate_limit_per_hour", botCfg.RateLimitPerHour,
		"telegram_chats", len(nc.BotTelegramChatIDs),
		"cost_tracker", c.CostTracker != nil,
	)
}

// startNotificationBot starts the notification bot's event loop with the
// daemon lifecycle context. Called from Components.Start; nil-safe.
func startNotificationBot(c *Components, ctx context.Context) {
	if c.NotificationBot != nil {
		c.NotificationBot.Start(ctx)
	}
}

// stopNotificationBot stops the notification bot's event loop. Called
// from stopComponents; nil-safe (Start never ran when construction was
// skipped).
func stopNotificationBot(c *Components) {
	if c.NotificationBot != nil {
		c.NotificationBot.Stop()
	}
}
