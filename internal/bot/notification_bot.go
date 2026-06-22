package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/comm/telegram"
)

// NotificationTriggerType identifies the kind of notification.
type NotificationTriggerType string

const (
	TriggerBudgetExhausted     NotificationTriggerType = "budget_exhausted"
	TriggerSecurityAlert       NotificationTriggerType = "security_alert"
	TriggerSessionDesignated   NotificationTriggerType = "session_designated"
)

// NotificationBotConfig holds configuration for the proactive notification bot.
type NotificationBotConfig struct {
	Enabled           bool     `json:"enabled"`
	TelegramChatIDs   []string `json:"telegram_chat_ids"`
	NotifyOn          []string `json:"notify_on"` // notification trigger types
	RateLimitPerHour  int      `json:"rate_limit_per_hour"`
	TelegramToken     string   `json:"telegram_token,omitempty"`
	TelegramPollTimeout int    `json:"telegram_poll_timeout,omitempty"`
}

// DefaultNotificationBotConfig returns a config with sensible defaults.
func DefaultNotificationBotConfig() NotificationBotConfig {
	return NotificationBotConfig{
		Enabled:          false,
		NotifyOn: []string{
			string(TriggerBudgetExhausted),
			string(TriggerSecurityAlert),
			string(TriggerSessionDesignated),
		},
		RateLimitPerHour: 60,
	}
}

// NotificationBot is a bot type that sends proactive notifications when
// specific daemon events occur (budget exhausted, security alerts,
// session designation changes). It uses the EventEmitter to subscribe to
// notification events and dispatches them via Telegram (optional) and/or
// logs them locally.
type NotificationBot struct {
	config  NotificationBotConfig
	emitter EventEmitterAdapter
	logger  *slog.Logger

	telegram *telegram.Bot

	mu     sync.Mutex
	rateMu sync.Mutex
	// rateWindow tracks timestamps of sent notifications for rate limiting.
	rateWindow []time.Time
}

// EventEmitterAdapter abstracts the notification emitter so the bot can
// subscribe without depending on the exact emitter type.
type EventEmitterAdapter interface {
	Subscribe() chan *http.NotificationEvent
	Unsubscribe(ch chan *http.NotificationEvent)
}

// NewNotificationBot creates a notification bot with the given config and emitter.
func NewNotificationBot(cfg NotificationBotConfig, emitter EventEmitterAdapter, logger *slog.Logger) (*NotificationBot, error) {
	if cfg.RateLimitPerHour <= 0 {
		cfg.RateLimitPerHour = 60
	}
	if logger == nil {
		logger = slog.Default()
	}

	nb := &NotificationBot{
		config:     cfg,
		emitter:    emitter,
		logger:     logger,
		rateWindow: make([]time.Time, 0),
	}

	// Create a dedicated Telegram bot for sending notifications (outbound only).
	if cfg.Enabled && len(cfg.TelegramChatIDs) > 0 && cfg.TelegramToken != "" {
		tgHandler := func(ctx context.Context, msg *telegram.Message) (string, error) {
			// The notification bot doesn't respond to incoming messages.
			return "", nil
		}
		tgBot, err := telegram.NewBot(telegram.BotConfig{
			Token:         cfg.TelegramToken,
			PollTimeout:   cfg.TelegramPollTimeout,
		}, tgHandler, logger)
		if err != nil {
			return nil, fmt.Errorf("create telegram notification bot: %w", err)
		}
		nb.telegram = tgBot
	}

	return nb, nil
}

// Start begins listening for notification events.
func (nb *NotificationBot) Start(ctx context.Context) {
	if nb.emitter == nil {
		nb.logger.Info("notification bot started (no emitter, passive mode)")
		return
	}

	nb.logger.Info("notification bot starting",
		"enabled", nb.config.Enabled,
		"triggers", nb.config.NotifyOn,
		"telegram_chat_ids", len(nb.config.TelegramChatIDs),
		"rate_limit_per_hour", nb.config.RateLimitPerHour)

	ch := nb.emitter.Subscribe() // chan *http.NotificationEvent

	go func() {
		for {
			select {
			case <-ctx.Done():
				nb.teardown()
				return
			case event, ok := <-ch:
				if !ok {
					nb.teardown()
					return
				}
				nb.handleEvent(ctx, event)
			}
		}
	}()
}

// Stop gracefully shuts down the notification bot.
func (nb *NotificationBot) Stop() {
	nb.teardown()
	nb.logger.Info("notification bot stopped")
}

// teardown releases resources held by the notification bot.
func (nb *NotificationBot) teardown() {
	// The emitter cleanup is handled by the goroutine exiting when ctx
	// is cancelled or the channel closes.
}

// handleEvent dispatches a notification event to configured channels.
func (nb *NotificationBot) handleEvent(ctx context.Context, event *http.NotificationEvent) {
	if !nb.config.Enabled {
		return
	}

	trigger := nb.mapEventToTrigger(event)
	if !nb.shouldNotify(trigger) {
		nb.logger.Debug("notification trigger not configured, skipping", "trigger", trigger)
		return
	}

	if !nb.allowRateLimit() {
		nb.logger.Debug("notification rate-limited", "trigger", trigger)
		return
	}

	nb.logger.Info("dispatching notification",
		"type", event.Type,
		"title", event.Title,
		"trigger", trigger)

	// Send via Telegram if configured.
	if nb.telegram != nil && len(nb.config.TelegramChatIDs) > 0 {
		for _, chatIDStr := range nb.config.TelegramChatIDs {
			var chatID int64
			fmt.Sscanf(chatIDStr, "%d", &chatID)
			if chatID == 0 {
				continue
			}
			msg := nb.formatNotification(event, trigger)
			go func(id int64, m string) {
				if err := nb.telegram.SendMessage(ctx, id, m); err != nil {
					nb.logger.Error("failed to send telegram notification",
						"chat_id", id, "error", err)
				}
			}(chatID, msg)
		}
	}
}

// mapEventToTrigger maps an incoming HTTP notification event to a trigger type.
func (nb *NotificationBot) mapEventToTrigger(event *http.NotificationEvent) NotificationTriggerType {
	// Infer the trigger type from the event content keywords.
	body := event.Message + " " + event.Title
	if containsKeyword(body, "budget", "cost", "limit", "exhaust", "spent", "spend") {
		return TriggerBudgetExhausted
	}
	if containsKeyword(body, "security", "threat", "injection", "unauthorized", "forbidden", "suspiciou") {
		return TriggerSecurityAlert
	}
	if containsKeyword(body, "session", "designat", "assigned", "routing") {
		return TriggerSessionDesignated
	}

	// Fallback: use event type as the trigger.
	switch event.Type {
	case "error":
		return "error"
	case "warning":
		return "warning"
	default:
		return "info"
	}
}

// containsKeyword checks if a string contains any of the given keywords (case-insensitive).
func containsKeyword(s string, keywords ...string) bool {
	lower := strings.ToLower(s)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// shouldNotify checks whether this trigger type is configured for notification.
func (nb *NotificationBot) shouldNotify(trigger NotificationTriggerType) bool {
	for _, t := range nb.config.NotifyOn {
		if t == string(trigger) {
			return true
		}
	}
	return false
}

// allowRateLimit checks and records under the per-hour rate limit.
// Uses a simple sliding window: timestamps older than 1 hour are pruned first.
func (nb *NotificationBot) allowRateLimit() bool {
	nb.rateMu.Lock()
	defer nb.rateMu.Unlock()

	now := time.Now()
	oneHourAgo := now.Add(-time.Hour)

	// Prune old entries.
	idx := 0
	for idx < len(nb.rateWindow) && nb.rateWindow[idx].Before(oneHourAgo) {
		idx++
	}
	if idx > 0 {
		nb.rateWindow = nb.rateWindow[idx:]
	}

	if len(nb.rateWindow) >= nb.config.RateLimitPerHour {
		return false
	}

	nb.rateWindow = append(nb.rateWindow, now)
	return true
}

// formatNotification formats an event into a Telegram-friendly message.
func (nb *NotificationBot) formatNotification(event *http.NotificationEvent, trigger NotificationTriggerType) string {
	msg := "**Meept Notification**\n\n"
	msg += fmt.Sprintf("*%s*\n", event.Title)
	msg += fmt.Sprintf("%s\n\n", event.Message)
	msg += fmt.Sprintf("*Trigger:* %s\n", trigger)
	msg += fmt.Sprintf("*Time:* %s", event.Timestamp)

	if event.SessionID != "" {
		msg += fmt.Sprintf("\n*Session:* %s", event.SessionID)
	}
	if event.AgentID != "" {
		msg += fmt.Sprintf("\n*Agent:* %s", event.AgentID)
	}

	return msg
}
