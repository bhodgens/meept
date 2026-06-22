package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/session"
)

// TelegramChannelConfig holds configuration for the TelegramChannel adapter.
type TelegramChannelConfig struct {
	Token        string   // Telegram bot API token
	AllowedUsers []int64  // User IDs allowed to interact (empty = all)
	AllowedChats []int64  // Chat IDs allowed (empty = all)
	PollTimeout  int      // Long polling timeout in seconds
	DataDir      string   // Directory for session persistence
	BotID        string   // Unique bot ID for framework registration
	BotName      string   // Human-readable bot name
}

// Validate checks that the config has all required fields.
func (c *TelegramChannelConfig) Validate() error {
	if c.Token == "" {
		return fmt.Errorf("telegram channel token is required")
	}
	if c.DataDir == "" {
		return fmt.Errorf("telegram channel data directory is required")
	}
	if c.BotID == "" {
		return fmt.Errorf("telegram channel bot ID is required")
	}
	if c.PollTimeout <= 0 {
		c.PollTimeout = 30
	}
	return nil
}

// TelegramChannel implements MessagingBotAdapter for bidirectional
// Telegram messaging. It wraps the existing Bot and AgentHandler,
// providing a clean interface that can be registered with the bot
// framework and used as a generic channel.
type TelegramChannel struct {
	*BaseBotAdapter // embed base for BotAdapter interface

	botClient  *Bot
	handler    *AgentHandler
	config     TelegramChannelConfig
	logger     *slog.Logger
	sessionMgr session.Store
	running    bool
	mu         sync.RWMutex
}

// NewTelegramChannel creates a new TelegramChannel adapter.
// It initializes the bot client, agent handler, and session manager.
func NewTelegramChannel(cfg TelegramChannelConfig, sessionMgr session.Store, agentLoop *agent.AgentLoop, logger *slog.Logger) (*TelegramChannel, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid telegram channel config: %w", err)
	}

	base := NewBaseBotAdapter(cfg.BotID, cfg.BotName)

	tgHandler := NewAgentHandler(sessionMgr, agentLoop, cfg.DataDir, logger.With("channel", "telegram", "component", "handler"))

	// Token env-var resolution is handled by the caller (daemon components.go)
	// before building BotConfig, so cfg.Token is already resolved here.
	token := cfg.Token

	botCfg := BotConfig{
		Token:        token,
		AllowedUsers: cfg.AllowedUsers,
		AllowedChats: cfg.AllowedChats,
		PollTimeout:  cfg.PollTimeout,
	}

	botClient, err := NewBot(botCfg, tgHandler.Handle, logger.With("channel", "telegram"))
	if err != nil {
		return nil, fmt.Errorf("create telegram bot client: %w", err)
	}

	return &TelegramChannel{
		BaseBotAdapter: base,
		botClient:      botClient,
		handler:        tgHandler,
		config:         cfg,
		logger:         logger,
		sessionMgr:     sessionMgr,
	}, nil
}

// Execute starts the channel's polling loop and runs until ctx is cancelled.
// This satisfies the BotAdapter.Execute interface.
func (t *TelegramChannel) Execute(ctx context.Context) error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return fmt.Errorf("telegram channel is already running")
	}
	t.running = true
	t.mu.Unlock()

	t.logger.Info("telegram channel starting", "bot_id", t.id)

	t.botClient.SetResetter(t.handler)

	// Start polling via the underlying bot client.
	// The bot.Start runs in a loop; it respects context cancellation.
	return t.botClient.Start(ctx)
}

// Stop gracefully stops the channel.
func (t *TelegramChannel) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		t.botClient.Stop()
		t.running = false
		t.logger.Info("telegram channel stopped", "bot_id", t.id)
	}
}

// SendMessage sends a message to a Telegram chat.
// Satisfies MessagingBotAdapter.SendMessage.
func (t *TelegramChannel) SendMessage(ctx context.Context, target string, content string) error {
	chatID, err := strconv.ParseInt(target, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram channel SendMessage: invalid chat_id %q: %w", target, err)
	}
	formatted := FormatResponse(content)
	return t.botClient.SendMessage(ctx, chatID, formatted)
}

// CanInitiate returns true -- Telegram can send unsolicited messages
// (the bot can push to users who have chatted with it).
// Satisfies MessagingBotAdapter.CanInitiate.
func (t *TelegramChannel) CanInitiate() bool { return true }

// ChatIDToTarget converts an int64 chat ID to the string target used by
// SendMessage.
func ChatIDToTarget(chatID int64) string {
	return strconv.FormatInt(chatID, 10)
}

// BotClient returns the underlying *Bot for direct access.
// Used by daemon wiring to start/stop the polling loop.
func (t *TelegramChannel) BotClient() *Bot { return t.botClient }

// Handler returns the underlying *AgentHandler used to route messages.
func (t *TelegramChannel) Handler() *AgentHandler { return t.handler }
