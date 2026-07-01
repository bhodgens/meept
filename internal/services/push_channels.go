package services

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/comm/telegram"
)

// notifier is the interface that event emitters from daemon must satisfy.
// Daemon's EventEmitter has:
//   - Publish(*http.NotificationEvent)
//   - PublishNotification(sessionID, agentID string, notifType NotificationType, title, message string)
// This local interface mirrors those signatures without importing daemon/http.
type notifier interface {
	Publish(event interface{})
	PublishNotification(sessionID, agentID string, notifType interface{}, title, message string)
}

// PushMessage carries the data routed to channel subscribers.
type PushMessage struct {
	SessionID  string                 `json:"session_id"`
	Source     string                 `json:"source"`
	ChannelID  string                 `json:"channel_id,omitempty"`
	Type       PushType               `json:"type"`
	Priority   PushPriority           `json:"priority"`
	Content    string                 `json:"content"`
	Timestamp  time.Time              `json:"timestamp"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// PushChannel defines the interface that all push delivery channels must
// implement. Each channel decides whether it can deliver to a given session
// (e.g. Telegram channel checks chat-ID mapping) and then pushes the
// message through its transport.
type PushChannel interface {
	// CanReceive reports whether this channel can deliver to the given session.
	CanReceive(ctx context.Context, sessionID string, msg *PushMessage) bool

	// Push delivers the message to the channel's transport layer.
	Push(ctx context.Context, msg *PushMessage) error
}

// ChannelRegistry holds registered push channels and routes push messages
// to each one that can receive them.
type ChannelRegistry struct {
	mu       sync.RWMutex
	channels []PushChannel
	logger   *slog.Logger
}

// NewChannelRegistry creates an empty channel registry.
func NewChannelRegistry(logger *slog.Logger) *ChannelRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	return &ChannelRegistry{
		channels: make([]PushChannel, 0),
		logger:   logger,
	}
}

// Register adds a channel to the registry.
func (r *ChannelRegistry) Register(ch PushChannel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channels = append(r.channels, ch)
	r.logger.Debug("push channel registered", "type", chanID(ch))
}

// Push routes a push message to all channels that can receive it.
// It is best-effort: individual channel failures are logged but do not
// prevent delivery through other channels. Returns the count of channels
// that accepted the message.
func (r *ChannelRegistry) Push(ctx context.Context, msg *PushMessage) int {
	r.mu.RLock()
	channels := make([]PushChannel, len(r.channels))
	copy(channels, r.channels)
	r.mu.RUnlock()

	count := 0
	for _, ch := range channels {
		if !ch.CanReceive(ctx, msg.SessionID, msg) {
			continue
		}
		if err := ch.Push(ctx, msg); err != nil {
			r.logger.Warn("push channel delivery failed",
				"channel", chanID(ch),
				"session", msg.SessionID,
				"error", err,
			)
			continue
		}
		count++
	}
	return count
}

// Remove deletes the channel with the given ID from the registry.
func (r *ChannelRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var kept []PushChannel
	for _, ch := range r.channels {
		if chanID(ch) != id {
			kept = append(kept, ch)
		}
	}
	r.channels = kept
}


// --- Channel Implementations ---

// TelegramPushChannel delivers notifications to a Telegram bot via SendMessage.
// It routes by resolving session ID to a Telegram chat ID.
type TelegramPushChannel struct {
	bot         *telegram.Bot
	sidToChat   map[string]int64 // session_id -> chat_id
	broadcastCh []int64          // if sidToChat is nil, broadcast to these chat IDs
	logger      *slog.Logger
}

// NewTelegramPushChannel creates a Telegram push channel.
// sidToChat maps session IDs to specific Telegram chat IDs for targeted delivery.
// If nil, the channel broadcasts to broadcastCh (or all known chats).
func NewTelegramPushChannel(
	bot *telegram.Bot,
	sidToChat map[string]int64,
	broadcastCh []int64,
	logger *slog.Logger,
) (*TelegramPushChannel, error) {
	if bot == nil {
		return nil, fmt.Errorf("telegram bot is required for TelegramPushChannel")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &TelegramPushChannel{
		bot:         bot,
		sidToChat:   sidToChat,
		broadcastCh: broadcastCh,
		logger:      logger,
	}, nil
}

func (t *TelegramPushChannel) CanReceive(ctx context.Context, sessionID string, msg *PushMessage) bool {
	if t.sidToChat != nil {
		_, ok := t.sidToChat[sessionID]
		return ok
	}
	return len(t.broadcastCh) > 0
}

func (t *TelegramPushChannel) Push(ctx context.Context, msg *PushMessage) error {
	formatted := formatForTelegram(msg.Content)

	if t.sidToChat != nil {
		// Targeted delivery: send to the chat linked to this session
		if chatID, ok := t.sidToChat[msg.SessionID]; ok {
			if err := t.bot.SendMessage(ctx, chatID, formatted); err != nil {
				return fmt.Errorf("send to chat %d: %w", chatID, err)
			}
		}
		// Also broadcast if the session doesn't have a direct mapping
		// (e.g. a global alert)
		if _, ok := t.sidToChat[msg.SessionID]; !ok && len(t.broadcastCh) > 0 {
			for _, chatID := range t.broadcastCh {
				if err := t.bot.SendMessage(ctx, chatID, formatted); err != nil {
					t.logger.Debug("telegram broadcast failed", "chat", chatID, "error", err)
				}
			}
		}
	} else {
		// Broadcast mode: send to all registered chats
		for _, chatID := range t.broadcastCh {
			if err := t.bot.SendMessage(ctx, chatID, formatted); err != nil {
				t.logger.Debug("telegram broadcast failed", "chat", chatID, "error", err)
			}
		}
	}
	return nil
}

// CLIPushChannel delivers notifications by printing to stdout, mimicking
// the CLI mode behavior. Useful for testing and terminal-based notification replay.
type CLIPushChannel struct {
	logger *slog.Logger
}

// NewCLIPushChannel creates a CLI push channel.
func NewCLIPushChannel(logger *slog.Logger) *CLIPushChannel {
	if logger == nil {
		logger = slog.Default()
	}
	return &CLIPushChannel{logger: logger}
}

func (c *CLIPushChannel) CanReceive(ctx context.Context, _ string, msg *PushMessage) bool {
	return true // CLI channel always accepts
}

func (c *CLIPushChannel) Push(ctx context.Context, msg *PushMessage) error {
	prefix := ""
	switch msg.Priority {
	case PushPriorityHigh:
		prefix = "[high] "
	case PushPriorityUrgent:
		prefix = "[urgent] "
	}
	fmt.Fprintf(os.Stdout, "%s%s\n", prefix, msg.Content)
	return nil
}

// TUIPushChannel delivers notifications by emitting them through an event
// emitter (daemon.EventEmitter). The TUI subscribes to this emitter for
// real-time notification display.
type TUIPushChannel struct {
	notifier notifier
	logger   *slog.Logger
}

// NewTUIPushChannel creates a TUI push channel.
// The notifier argument is typically *daemon.EventEmitter.
func NewTUIPushChannel(n notifier, logger *slog.Logger) (*TUIPushChannel, error) {
	if n == nil {
		return nil, fmt.Errorf("notifier is required for TUIPushChannel")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &TUIPushChannel{notifier: n, logger: logger}, nil
}

func (t *TUIPushChannel) CanReceive(ctx context.Context, sessionID string, msg *PushMessage) bool {
	return t.notifier != nil // TUI always shows if notifier is wired
}

func (t *TUIPushChannel) Push(ctx context.Context, msg *PushMessage) error {
	t.notifier.PublishNotification(
		msg.SessionID,
		msg.Source,
		nil, // notif type passed through as interface{}
		"Meept",
		msg.Content,
	)
	return nil
}

// HTTPPushChannel delivers notifications over the HTTP notification channel
// (WebSocket broadcast). It converts push messages to the notification
// event format that WebSocket clients consume.
type HTTPPushChannel struct {
	notifier notifier
	logger   *slog.Logger
}

// NewHTTPPushChannel creates an HTTP push channel.
// The notifier is the daemon's EventEmitter which publishes to HTTP WebSocket
// subscribers via WithNotification(serverOpt).
func NewHTTPPushChannel(n notifier, logger *slog.Logger) (*HTTPPushChannel, error) {
	if n == nil {
		return nil, fmt.Errorf("notifier is required for HTTPPushChannel")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &HTTPPushChannel{notifier: n, logger: logger}, nil
}

func (h *HTTPPushChannel) CanReceive(ctx context.Context, sessionID string, msg *PushMessage) bool {
	return h.notifier != nil
}

func (h *HTTPPushChannel) Push(ctx context.Context, msg *PushMessage) error {
	// Build a notification payload matching http.NotificationEvent shape.
	evt := map[string]interface{}{
		"id":        generatePushNotificationID(),
		"timestamp": msg.Timestamp.UTC().Format(time.RFC3339),
		"title":     "Meept",
		"message":   msg.Content,
		"session_id": msg.SessionID,
		"type":      "info",
	}
	h.notifier.Publish(evt)
	return nil
}

// --- Helpers ---

func chanID(ch PushChannel) string {
	switch ch.(type) {
	case *TelegramPushChannel:
		return "telegram"
	case *CLIPushChannel:
		return "cli"
	case *TUIPushChannel:
		return "tui"
	case *HTTPPushChannel:
		return "http"
	default:
		return fmt.Sprintf("%T", ch)
	}
}

// formatForTelegram escapes MarkdownV2-safe characters and truncates to 4096.
func formatForTelegram(text string) string {
	const maxLen = 4096
	if len(text) > maxLen {
		text = text[:maxLen] + "..."
	}
	// Escape MarkdownV2 special chars
	esc := []byte{}
	for _, r := range text {
		switch r {
		case '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!':
			esc = append(esc, '\\')
		}
		esc = append(esc, byte(r))
	}
	return string(esc)
}

// generatePushNotificationID creates a hex string ID for push notifications.
func generatePushNotificationID() string {
	b := make([]byte, 16)
	_ = b // seeded from time below
	now := time.Now().UnixNano()
	for i := range b {
		b[i] = "0123456789abcdef"[byte(int(now>>((i*4)&63)) + i)&15]
	}
	return fmt.Sprintf("%x", b)
}
