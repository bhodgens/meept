# Plan: Telegram Integration

**Status:** Not Started
**Priority:** Low
**Estimated Effort:** 2-3 days

---

## Current State

The Telegram bot is **fully implemented** but **not integrated** into the daemon:

| Component | File | Status |
|-----------|------|--------|
| Bot | `internal/comm/telegram/bot.go` | Implemented (356 lines) |
| Types | `internal/comm/telegram/bot.go` | Implemented |

### What Exists

1. **Full Bot Implementation** (`bot.go`)
   - Long polling for updates
   - Message sending with markdown
   - Typing indicators
   - User/chat allowlisting
   - Message splitting for long responses
   - Graceful start/stop

2. **API Support**
   - `getUpdates` - Fetch incoming messages
   - `sendMessage` - Send responses
   - `sendChatAction` - Typing indicator

3. **Security**
   - Configurable allowed users
   - Configurable allowed chats
   - Unauthorized access logging

### What's Missing

1. **No daemon integration** - Bot not started
2. **No message handler** - Handler stub required
3. **No config loading** - Telegram config not in schema
4. **No agent connection** - Messages not routed to agent loop

---

## Implementation Plan

### Phase 1: Message Handler

**File:** `internal/comm/telegram/handler.go` (new)

Create a handler that routes messages to the agent system:

```go
package telegram

import (
    "context"
    "fmt"
    "log/slog"

    "github.com/caimlas/meept/internal/agent"
    "github.com/caimlas/meept/internal/session"
)

// AgentHandler routes Telegram messages to the agent system.
type AgentHandler struct {
    sessionMgr  *session.Manager
    agentLoop   *agent.AgentLoop
    logger      *slog.Logger

    // Chat ID -> Session ID mapping
    sessions    map[int64]string
}

// NewAgentHandler creates a new agent handler.
func NewAgentHandler(sessionMgr *session.Manager, agentLoop *agent.AgentLoop, logger *slog.Logger) *AgentHandler {
    return &AgentHandler{
        sessionMgr: sessionMgr,
        agentLoop:  agentLoop,
        logger:     logger,
        sessions:   make(map[int64]string),
    }
}

// Handle processes incoming messages.
func (h *AgentHandler) Handle(ctx context.Context, msg *Message) (string, error) {
    chatID := msg.Chat.ID

    // Get or create session for this chat
    sessionID, exists := h.sessions[chatID]
    if !exists {
        // Create new session
        sess, err := h.sessionMgr.CreateSession(ctx, fmt.Sprintf("telegram-%d", chatID))
        if err != nil {
            return "", fmt.Errorf("failed to create session: %w", err)
        }
        sessionID = sess.ID
        h.sessions[chatID] = sessionID
    }

    // Route to agent
    response, err := h.agentLoop.Run(ctx, msg.Text)
    if err != nil {
        h.logger.Error("agent error", "error", err)
        return fmt.Sprintf("Error: %v", err), nil
    }

    return response.Content, nil
}
```

### Phase 2: Configuration

**File:** `internal/config/schema.go`

Add Telegram config:

```go
type TelegramConfig struct {
    Enabled        bool    `toml:"enabled"`
    Token          string  `toml:"token"`          // Bot API token (or env var)
    AllowedUsers   []int64 `toml:"allowed_users"`  // User IDs
    AllowedChats   []int64 `toml:"allowed_chats"`  // Chat IDs
    PollTimeout    int     `toml:"poll_timeout"`   // Seconds
}
```

**File:** `~/.meept/meept.toml`

```toml
[telegram]
enabled = false
token = "${TELEGRAM_BOT_TOKEN}"
allowed_users = []  # Empty = allow all
allowed_chats = []  # Empty = allow all
poll_timeout = 30
```

### Phase 3: Daemon Integration

**File:** `internal/daemon/components.go`

**Changes:**

1. Add Telegram bot to components:
```go
type Components struct {
    // ... existing fields
    telegramBot *telegram.Bot
}

func NewComponents(cfg *config.Config, ...) (*Components, error) {
    // ...

    // Initialize Telegram bot
    var telegramBot *telegram.Bot
    if cfg.Telegram.Enabled {
        handler := telegram.NewAgentHandler(sessionMgr, agentLoop, logger)

        botCfg := telegram.BotConfig{
            Token:        expandEnvVar(cfg.Telegram.Token),
            AllowedUsers: cfg.Telegram.AllowedUsers,
            AllowedChats: cfg.Telegram.AllowedChats,
            PollTimeout:  cfg.Telegram.PollTimeout,
        }

        bot, err := telegram.NewBot(botCfg, handler.Handle, logger)
        if err != nil {
            logger.Error("failed to create telegram bot", "error", err)
        } else {
            telegramBot = bot
        }
    }

    c.telegramBot = telegramBot
    // ...
}
```

2. Start bot in goroutine:
```go
func (c *Components) Start(ctx context.Context) error {
    // ... existing startup ...

    // Start Telegram bot
    if c.telegramBot != nil {
        go func() {
            if err := c.telegramBot.Start(ctx); err != nil {
                c.logger.Error("telegram bot error", "error", err)
            }
        }()
    }

    return nil
}
```

3. Stop bot on shutdown:
```go
func (c *Components) Stop() error {
    if c.telegramBot != nil {
        c.telegramBot.Stop()
    }
    // ...
}
```

### Phase 4: Enhanced Bot Features

**File:** `internal/comm/telegram/bot.go`

Add additional features:

1. **Command handling**:
```go
func (b *Bot) handleMessage(ctx context.Context, msg *Message) {
    // Check for commands
    if strings.HasPrefix(msg.Text, "/") {
        b.handleCommand(ctx, msg)
        return
    }

    // Regular message handling...
}

func (b *Bot) handleCommand(ctx context.Context, msg *Message) {
    parts := strings.SplitN(msg.Text, " ", 2)
    cmd := strings.TrimPrefix(parts[0], "/")

    switch cmd {
    case "start":
        b.SendMessage(ctx, msg.Chat.ID, "Hello! I'm Meept, your AI assistant. Send me a message to get started.")
    case "status":
        // Return daemon status
    case "help":
        b.SendMessage(ctx, msg.Chat.ID, helpMessage)
    case "new":
        // Start new conversation session
    default:
        // Route to handler as regular message
    }
}

const helpMessage = `*Meept Bot Commands*

/start - Start conversation
/status - Check daemon status
/new - Start new session
/help - Show this help

Just send me a message to chat!`
```

2. **Inline keyboards** (optional):
```go
type InlineKeyboardButton struct {
    Text         string `json:"text"`
    CallbackData string `json:"callback_data,omitempty"`
    URL          string `json:"url,omitempty"`
}

type InlineKeyboardMarkup struct {
    InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

func (b *Bot) SendMessageWithKeyboard(ctx context.Context, chatID int64, text string, keyboard *InlineKeyboardMarkup) error {
    params := url.Values{}
    params.Set("chat_id", fmt.Sprintf("%d", chatID))
    params.Set("text", text)
    params.Set("parse_mode", "Markdown")

    if keyboard != nil {
        kb, _ := json.Marshal(keyboard)
        params.Set("reply_markup", string(kb))
    }

    // ... send request ...
}
```

### Phase 5: Session Persistence

**File:** `internal/comm/telegram/handler.go`

Add session persistence:

```go
type AgentHandler struct {
    // ... existing fields
    sessionsFile string
}

func (h *AgentHandler) loadSessions() error {
    data, err := os.ReadFile(h.sessionsFile)
    if os.IsNotExist(err) {
        return nil
    }
    if err != nil {
        return err
    }

    return json.Unmarshal(data, &h.sessions)
}

func (h *AgentHandler) saveSessions() error {
    data, err := json.MarshalIndent(h.sessions, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(h.sessionsFile, data, 0644)
}
```

### Phase 6: Rich Responses

**File:** `internal/comm/telegram/formatter.go` (new)

Format agent responses for Telegram:

```go
package telegram

import (
    "regexp"
    "strings"
)

// FormatResponse converts agent output to Telegram markdown.
func FormatResponse(content string) string {
    // Convert code blocks
    content = convertCodeBlocks(content)

    // Escape special characters outside code
    content = escapeMarkdown(content)

    return content
}

func convertCodeBlocks(s string) string {
    // Convert ```lang\ncode\n``` to Telegram format
    re := regexp.MustCompile("```(\\w*)\\n([\\s\\S]*?)```")
    return re.ReplaceAllString(s, "```$1\n$2```")
}

func escapeMarkdown(s string) string {
    // Escape characters that need escaping in Telegram MarkdownV2
    // (outside of code blocks)
    special := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}
    for _, char := range special {
        s = strings.ReplaceAll(s, char, "\\"+char)
    }
    return s
}
```

---

## Testing Plan

### Unit Tests

1. **Bot tests** - Message parsing, sending
2. **Handler tests** - Session management
3. **Formatter tests** - Markdown conversion

### Integration Tests

1. Test bot startup/shutdown
2. Test message routing to agent
3. Test session persistence

### Manual Testing

1. Create Telegram bot via @BotFather
2. Configure token in meept.toml
3. Start daemon
4. Send message to bot
5. Verify response

---

## Files to Modify

| File | Changes |
|------|---------|
| `internal/daemon/components.go` | Initialize and start bot |
| `internal/config/schema.go` | Add Telegram config |
| `config/meept.toml` | Add Telegram section |

## Files to Create

| File | Purpose |
|------|---------|
| `internal/comm/telegram/handler.go` | Agent message handler |
| `internal/comm/telegram/formatter.go` | Response formatting |
| `tests/integration/telegram_test.go` | Integration tests |

---

## Example Usage

1. **Create bot** via Telegram @BotFather
2. **Configure** in `~/.meept/meept.toml`:
   ```toml
   [telegram]
   enabled = true
   token = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
   allowed_users = [12345678]  # Your Telegram user ID
   ```
3. **Start daemon**: `./bin/meept-daemon -f`
4. **Send message** to your bot in Telegram

---

## Success Criteria

1. Telegram bot starts with daemon
2. Messages are routed to agent
3. Responses are sent back formatted
4. User/chat allowlisting works
5. Sessions persist across restarts
6. Commands (/start, /help, /status) work
7. Tests pass
