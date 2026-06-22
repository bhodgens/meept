package telegram

import (
	"context"
)

// BotAdapter is the base interface that bot types must implement.
// It defines the core lifecycle and identification methods.
type BotAdapter interface {
	// ID returns the bot's unique identifier.
	ID() string

	// Name returns a human-readable name for the bot.
	Name() string

	// Execute starts the bot's primary operation loop.
	// The context is used for shutdown signaling.
	Execute(ctx context.Context) error
}

// MessagingBotAdapter extends BotAdapter with bidirectional messaging capabilities.
// Bots that implement this interface can both receive and initiate messages.
type MessagingBotAdapter interface {
	BotAdapter

	// SendMessage delivers an outbound message to the given target.
	// The target is channel-specific (e.g. chat ID for Telegram).
	SendMessage(ctx context.Context, target string, content string) error

	// CanInitiate returns true if the bot can send unsolicited messages
	// (e.g. push notifications, reminders) without a prior inbound message.
	CanInitiate() bool
}

// BaseBotAdapter provides common, no-op implementations for BotAdapter interface methods.
// Embed this in concrete bot types to avoid boilerplate.
type BaseBotAdapter struct {
	id   string
	name string
}

// ID returns the bot's identifier.
func (b *BaseBotAdapter) ID() string { return b.id }

// Name returns the bot's name.
func (b *BaseBotAdapter) Name() string { return b.name }

// NewBaseBotAdapter constructs a BaseBotAdapter with the given id and name.
func NewBaseBotAdapter(id, name string) *BaseBotAdapter {
	return &BaseBotAdapter{id: id, name: name}
}
