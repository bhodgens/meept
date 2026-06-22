package services

import (
	"context"
	"log/slog"
)

// BotContextImpl provides runtime context and services to executing bots.
// It implements the bot.BotContext interface.
type BotContextImpl struct {
	pushService *PushService
	logger      *slog.Logger
}

// NewBotContext creates a new bot context with the given services.
func NewBotContext(pushService *PushService, logger *slog.Logger) *BotContextImpl {
	if logger == nil {
		logger = slog.Default()
	}
	return &BotContextImpl{
		pushService: pushService,
		logger:      logger,
	}
}

// PushNotification sends a push notification to the user.
func (c *BotContextImpl) PushNotification(ctx context.Context, sessionID string, title, message string) error {
	if c.pushService == nil {
		return nil // push service not configured, silently skip
	}

	_, err := c.pushService.Push(ctx, &PushRequest{
		SessionIDs: []string{sessionID},
		Source:     "bot",
		Type:       PushTypeNotification,
		Content:    title + ": " + message,
		Priority:   PushPriorityNormal,
	})
	return err
}
