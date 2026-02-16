// Package telegram provides the Telegram bot integration for meept.
package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	telegramAPIBase = "https://api.telegram.org/bot"
	defaultTimeout  = 30 * time.Second
	maxMessageLen   = 4096
)

// Message represents a Telegram message.
type Message struct {
	MessageID int    `json:"message_id"`
	From      *User  `json:"from,omitempty"`
	Chat      Chat   `json:"chat"`
	Date      int64  `json:"date"`
	Text      string `json:"text,omitempty"`
}

// User represents a Telegram user.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

// Chat represents a Telegram chat.
type Chat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title,omitempty"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

// Update represents an incoming update from Telegram.
type Update struct {
	UpdateID int      `json:"update_id"`
	Message  *Message `json:"message,omitempty"`
}

// getUpdatesResponse is the API response for getUpdates.
type getUpdatesResponse struct {
	OK     bool     `json:"ok"`
	Result []Update `json:"result"`
}

// MessageHandler handles incoming messages.
type MessageHandler func(ctx context.Context, msg *Message) (string, error)

// BotConfig holds configuration for the Telegram bot.
type BotConfig struct {
	Token          string   // Bot API token
	AllowedUsers   []int64  // User IDs allowed to interact (empty = all)
	AllowedChats   []int64  // Chat IDs allowed (empty = all)
	PollTimeout    int      // Long polling timeout in seconds
	MaxConnections int      // Max simultaneous connections
}

// Bot is the Telegram bot client.
type Bot struct {
	mu sync.RWMutex

	config     BotConfig
	httpClient *http.Client
	handler    MessageHandler
	logger     *slog.Logger

	running    bool
	stopCh     chan struct{}
	lastUpdate int
}

// NewBot creates a new Telegram bot.
func NewBot(cfg BotConfig, handler MessageHandler, logger *slog.Logger) (*Bot, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("telegram token is required")
	}
	if handler == nil {
		return nil, fmt.Errorf("message handler is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.PollTimeout == 0 {
		cfg.PollTimeout = 30
	}

	return &Bot{
		config: cfg,
		httpClient: &http.Client{
			Timeout: defaultTimeout + time.Duration(cfg.PollTimeout)*time.Second,
		},
		handler: handler,
		logger:  logger,
		stopCh:  make(chan struct{}),
	}, nil
}

// Start starts the bot's polling loop.
func (b *Bot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("bot is already running")
	}
	b.running = true
	b.mu.Unlock()

	b.logger.Info("telegram bot starting")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.stopCh:
			return nil
		default:
			updates, err := b.getUpdates(ctx)
			if err != nil {
				b.logger.Error("failed to get updates", "error", err)
				time.Sleep(time.Second) // Back off on error
				continue
			}

			for _, update := range updates {
				if update.Message != nil {
					go b.handleMessage(ctx, update.Message)
				}
				b.lastUpdate = update.UpdateID
			}
		}
	}
}

// Stop stops the bot.
func (b *Bot) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		close(b.stopCh)
		b.running = false
	}
}

// getUpdates fetches updates from Telegram.
func (b *Bot) getUpdates(ctx context.Context) ([]Update, error) {
	params := url.Values{}
	params.Set("timeout", fmt.Sprintf("%d", b.config.PollTimeout))
	params.Set("offset", fmt.Sprintf("%d", b.lastUpdate+1))

	apiURL := fmt.Sprintf("%s%s/getUpdates?%s", telegramAPIBase, b.config.Token, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result getUpdatesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if !result.OK {
		return nil, fmt.Errorf("telegram API error")
	}

	return result.Result, nil
}

// handleMessage processes an incoming message.
func (b *Bot) handleMessage(ctx context.Context, msg *Message) {
	// Check if user/chat is allowed
	if !b.isAllowed(msg) {
		b.logger.Warn("unauthorized access attempt",
			"user_id", msg.From.ID,
			"chat_id", msg.Chat.ID)
		return
	}

	b.logger.Info("received message",
		"from", msg.From.Username,
		"chat_id", msg.Chat.ID,
		"text", truncate(msg.Text, 50))

	// Process message
	response, err := b.handler(ctx, msg)
	if err != nil {
		b.logger.Error("handler error", "error", err)
		response = fmt.Sprintf("Error: %v", err)
	}

	// Send response
	if response != "" {
		if err := b.SendMessage(ctx, msg.Chat.ID, response); err != nil {
			b.logger.Error("failed to send response", "error", err)
		}
	}
}

// SendMessage sends a message to a chat.
func (b *Bot) SendMessage(ctx context.Context, chatID int64, text string) error {
	// Split long messages
	messages := splitMessage(text, maxMessageLen)

	for _, msg := range messages {
		params := url.Values{}
		params.Set("chat_id", fmt.Sprintf("%d", chatID))
		params.Set("text", msg)
		params.Set("parse_mode", "Markdown")

		apiURL := fmt.Sprintf("%s%s/sendMessage", telegramAPIBase, b.config.Token)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL,
			strings.NewReader(params.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := b.httpClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("telegram API returned %d", resp.StatusCode)
		}
	}

	return nil
}

// SendTyping sends a "typing" action to indicate the bot is processing.
func (b *Bot) SendTyping(ctx context.Context, chatID int64) error {
	params := url.Values{}
	params.Set("chat_id", fmt.Sprintf("%d", chatID))
	params.Set("action", "typing")

	apiURL := fmt.Sprintf("%s%s/sendChatAction", telegramAPIBase, b.config.Token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL,
		strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	return nil
}

// isAllowed checks if the message sender is authorized.
func (b *Bot) isAllowed(msg *Message) bool {
	// Check user allowlist
	if len(b.config.AllowedUsers) > 0 && msg.From != nil {
		found := false
		for _, id := range b.config.AllowedUsers {
			if id == msg.From.ID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check chat allowlist
	if len(b.config.AllowedChats) > 0 {
		found := false
		for _, id := range b.config.AllowedChats {
			if id == msg.Chat.ID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// splitMessage splits a long message into chunks.
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var messages []string
	for len(text) > 0 {
		end := maxLen
		if end > len(text) {
			end = len(text)
		}

		// Try to split at newline or space
		if end < len(text) {
			for i := end - 1; i > end-200 && i > 0; i-- {
				if text[i] == '\n' || text[i] == ' ' {
					end = i + 1
					break
				}
			}
		}

		messages = append(messages, text[:end])
		text = text[end:]
	}

	return messages
}

// truncate truncates a string to maxLen.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
