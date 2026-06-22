// Package bot defines formal bot interfaces that channel adapters implement.
package bot

import (
	"context"
)

// Bot is the base interface that bot types must implement.
// It defines the core lifecycle and identification methods.
type Bot interface {
	// ID returns the bot's unique identifier.
	ID() string

	// Name returns a human-readable name for the bot.
	Name() string

	// Execute starts the bot's primary operation loop.
	// The context is used for shutdown signaling.
	Execute(ctx context.Context) error
}

// MessagingBot extends Bot with bidirectional messaging capabilities.
// Bots that implement this interface can both receive and initiate messages.
type MessagingBot interface {
	Bot

	// SendMessage delivers an outbound message to the given target.
	// The target is channel-specific (e.g. chat ID for Telegram).
	SendMessage(ctx context.Context, target string, content string) error

	// CanInitiate returns true if the bot can send unsolicited messages
	// (e.g. push notifications, reminders) without a prior inbound message.
	CanInitiate() bool
}
