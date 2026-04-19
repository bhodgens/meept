# Plan: Calendar Integration

**Status:** Not Started
**Priority:** Low
**Estimated Effort:** 2-3 days

---

## Current State

The Google Calendar client is **fully implemented** but **not integrated**:

| Component | File | Status |
|-----------|------|--------|
| Client | `internal/calendar/gcal.go` | Implemented (291 lines) |

### What Exists

1. **Full Google Calendar API Client** (`gcal.go`)
   - List events with time range
   - Get single event
   - Create event
   - Update event
   - Delete event
   - Quick add (natural language)
   - Get upcoming events
   - Get today's events

2. **Complete Type Support**
   - Event with all fields
   - EventTime (datetime/date)
   - Attendees
   - Reminders

### What's Missing

1. **No daemon integration** - Client not initialized
2. **No OAuth flow** - Need token management
3. **No calendar tools** - Agent can't use calendar
4. **No scheduler integration** - No reminder triggering
5. **No config support** - No configuration in schema

---

## Implementation Plan

### Phase 1: OAuth Token Management

**File:** `internal/calendar/oauth.go` (new)

Implement OAuth2 token handling:

```go
package calendar

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"

    "golang.org/x/oauth2"
    "golang.org/x/oauth2/google"
)

const (
    tokenFile = "calendar_token.json"
    scopes    = "https://www.googleapis.com/auth/calendar"
)

// OAuthConfig holds OAuth configuration.
type OAuthConfig struct {
    ClientID     string `json:"client_id"`
    ClientSecret string `json:"client_secret"`
    RedirectURI  string `json:"redirect_uri"`
}

// TokenManager handles OAuth token persistence and refresh.
type TokenManager struct {
    config    *oauth2.Config
    tokenPath string
    token     *oauth2.Token
}

// NewTokenManager creates a new token manager.
func NewTokenManager(cfg OAuthConfig, dataDir string) *TokenManager {
    config := &oauth2.Config{
        ClientID:     cfg.ClientID,
        ClientSecret: cfg.ClientSecret,
        RedirectURL:  cfg.RedirectURI,
        Scopes:       []string{scopes},
        Endpoint:     google.Endpoint,
    }

    return &TokenManager{
        config:    config,
        tokenPath: filepath.Join(dataDir, tokenFile),
    }
}

// LoadToken loads the saved token.
func (m *TokenManager) LoadToken() error {
    data, err := os.ReadFile(m.tokenPath)
    if err != nil {
        return err
    }

    var token oauth2.Token
    if err := json.Unmarshal(data, &token); err != nil {
        return err
    }

    m.token = &token
    return nil
}

// SaveToken saves the token to disk.
func (m *TokenManager) SaveToken() error {
    data, err := json.MarshalIndent(m.token, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(m.tokenPath, data, 0600)
}

// GetAccessToken returns a valid access token, refreshing if needed.
func (m *TokenManager) GetAccessToken(ctx context.Context) (string, error) {
    if m.token == nil {
        return "", fmt.Errorf("no token available, run auth flow")
    }

    // Check if token needs refresh
    if m.token.Expiry.Before(time.Now().Add(5 * time.Minute)) {
        src := m.config.TokenSource(ctx, m.token)
        newToken, err := src.Token()
        if err != nil {
            return "", fmt.Errorf("failed to refresh token: %w", err)
        }
        m.token = newToken
        m.SaveToken()
    }

    return m.token.AccessToken, nil
}

// GetAuthURL returns the URL for OAuth authorization.
func (m *TokenManager) GetAuthURL(state string) string {
    return m.config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// ExchangeCode exchanges an auth code for a token.
func (m *TokenManager) ExchangeCode(ctx context.Context, code string) error {
    token, err := m.config.Exchange(ctx, code)
    if err != nil {
        return err
    }
    m.token = token
    return m.SaveToken()
}

// HasToken returns true if a token is available.
func (m *TokenManager) HasToken() bool {
    return m.token != nil
}
```

### Phase 2: Calendar Tools

**File:** `internal/tools/builtin/calendar.go` (new)

Create calendar tools for the agent:

```go
package builtin

import (
    "context"
    "fmt"
    "time"

    "github.com/caimlas/meept/internal/calendar"
    "github.com/caimlas/meept/internal/llm"
    "github.com/caimlas/meept/internal/tools"
)

// CalendarListTool lists calendar events.
type CalendarListTool struct {
    client *calendar.Client
}

func NewCalendarListTool(client *calendar.Client) *CalendarListTool {
    return &CalendarListTool{client: client}
}

func (t *CalendarListTool) Name() string { return "calendar_list" }

func (t *CalendarListTool) Description() string {
    return "List calendar events within a time range"
}

func (t *CalendarListTool) Parameters() llm.FunctionParameters {
    return llm.FunctionParameters{
        Type: "object",
        Properties: map[string]llm.FunctionProperty{
            "start": {
                Type:        "string",
                Description: "Start date/time in RFC3339 format (e.g., 2024-01-15T09:00:00Z)",
            },
            "end": {
                Type:        "string",
                Description: "End date/time in RFC3339 format",
            },
            "max_results": {
                Type:        "integer",
                Description: "Maximum number of events to return (default: 10)",
            },
        },
        Required: []string{"start", "end"},
    }
}

func (t *CalendarListTool) Execute(ctx context.Context, args map[string]any) (*tools.ToolResult, error) {
    startStr, _ := args["start"].(string)
    endStr, _ := args["end"].(string)
    maxResults := 10
    if mr, ok := args["max_results"].(float64); ok {
        maxResults = int(mr)
    }

    start, err := time.Parse(time.RFC3339, startStr)
    if err != nil {
        return tools.NewErrorResult(fmt.Sprintf("invalid start time: %v", err)), nil
    }

    end, err := time.Parse(time.RFC3339, endStr)
    if err != nil {
        return tools.NewErrorResult(fmt.Sprintf("invalid end time: %v", err)), nil
    }

    events, err := t.client.ListEvents(ctx, start, end, maxResults)
    if err != nil {
        return tools.NewErrorResult(fmt.Sprintf("failed to list events: %v", err)), nil
    }

    return tools.NewSuccessResult(formatEvents(events)), nil
}

// CalendarCreateTool creates calendar events.
type CalendarCreateTool struct {
    client *calendar.Client
}

func NewCalendarCreateTool(client *calendar.Client) *CalendarCreateTool {
    return &CalendarCreateTool{client: client}
}

func (t *CalendarCreateTool) Name() string { return "calendar_create" }

func (t *CalendarCreateTool) Description() string {
    return "Create a new calendar event"
}

func (t *CalendarCreateTool) Parameters() llm.FunctionParameters {
    return llm.FunctionParameters{
        Type: "object",
        Properties: map[string]llm.FunctionProperty{
            "summary": {
                Type:        "string",
                Description: "Event title/summary",
            },
            "start": {
                Type:        "string",
                Description: "Start date/time in RFC3339 format",
            },
            "end": {
                Type:        "string",
                Description: "End date/time in RFC3339 format",
            },
            "description": {
                Type:        "string",
                Description: "Event description (optional)",
            },
            "location": {
                Type:        "string",
                Description: "Event location (optional)",
            },
        },
        Required: []string{"summary", "start", "end"},
    }
}

func (t *CalendarCreateTool) Execute(ctx context.Context, args map[string]any) (*tools.ToolResult, error) {
    event := &calendar.Event{
        Summary:     args["summary"].(string),
        Description: getString(args, "description"),
        Location:    getString(args, "location"),
        Start: calendar.EventTime{
            DateTime: args["start"].(string),
        },
        End: calendar.EventTime{
            DateTime: args["end"].(string),
        },
    }

    created, err := t.client.CreateEvent(ctx, event)
    if err != nil {
        return tools.NewErrorResult(fmt.Sprintf("failed to create event: %v", err)), nil
    }

    return tools.NewSuccessResult(fmt.Sprintf("Created event: %s (ID: %s)", created.Summary, created.ID)), nil
}

// CalendarQuickAddTool creates events using natural language.
type CalendarQuickAddTool struct {
    client *calendar.Client
}

func NewCalendarQuickAddTool(client *calendar.Client) *CalendarQuickAddTool {
    return &CalendarQuickAddTool{client: client}
}

func (t *CalendarQuickAddTool) Name() string { return "calendar_quick_add" }

func (t *CalendarQuickAddTool) Description() string {
    return "Create a calendar event using natural language (e.g., 'Meeting with John tomorrow at 3pm')"
}

func (t *CalendarQuickAddTool) Parameters() llm.FunctionParameters {
    return llm.FunctionParameters{
        Type: "object",
        Properties: map[string]llm.FunctionProperty{
            "text": {
                Type:        "string",
                Description: "Natural language event description",
            },
        },
        Required: []string{"text"},
    }
}

func (t *CalendarQuickAddTool) Execute(ctx context.Context, args map[string]any) (*tools.ToolResult, error) {
    text, _ := args["text"].(string)

    event, err := t.client.QuickAdd(ctx, text)
    if err != nil {
        return tools.NewErrorResult(fmt.Sprintf("failed to create event: %v", err)), nil
    }

    return tools.NewSuccessResult(fmt.Sprintf("Created event: %s", event.Summary)), nil
}

// CalendarTodayTool gets today's events.
type CalendarTodayTool struct {
    client *calendar.Client
}

func NewCalendarTodayTool(client *calendar.Client) *CalendarTodayTool {
    return &CalendarTodayTool{client: client}
}

func (t *CalendarTodayTool) Name() string { return "calendar_today" }

func (t *CalendarTodayTool) Description() string {
    return "Get all calendar events for today"
}

func (t *CalendarTodayTool) Parameters() llm.FunctionParameters {
    return llm.FunctionParameters{
        Type:       "object",
        Properties: map[string]llm.FunctionProperty{},
    }
}

func (t *CalendarTodayTool) Execute(ctx context.Context, args map[string]any) (*tools.ToolResult, error) {
    events, err := t.client.GetToday(ctx)
    if err != nil {
        return tools.NewErrorResult(fmt.Sprintf("failed to get events: %v", err)), nil
    }

    if len(events) == 0 {
        return tools.NewSuccessResult("No events scheduled for today"), nil
    }

    return tools.NewSuccessResult(formatEvents(events)), nil
}

func formatEvents(events []calendar.Event) string {
    var result string
    for _, e := range events {
        start, _ := e.Start.Time()
        result += fmt.Sprintf("- %s: %s\n", start.Format("15:04"), e.Summary)
    }
    return result
}

func getString(args map[string]any, key string) string {
    if v, ok := args[key].(string); ok {
        return v
    }
    return ""
}
```

### Phase 3: Configuration

**File:** `internal/config/schema.go`

Add calendar config:

```go
type CalendarConfig struct {
    Enabled      bool   `toml:"enabled"`
    ClientID     string `toml:"client_id"`
    ClientSecret string `toml:"client_secret"`
    CalendarID   string `toml:"calendar_id"` // default: "primary"
}
```

**File:** `~/.meept/meept.toml`

```toml
[calendar]
enabled = false
client_id = "${GOOGLE_CLIENT_ID}"
client_secret = "${GOOGLE_CLIENT_SECRET}"
calendar_id = "primary"
```

### Phase 4: Daemon Integration

**File:** `internal/daemon/components.go`

**Changes:**

```go
type Components struct {
    // ... existing fields
    calendarClient *calendar.Client
    tokenManager   *calendar.TokenManager
}

func NewComponents(cfg *config.Config, ...) (*Components, error) {
    // ...

    // Initialize calendar
    var calendarClient *calendar.Client
    var tokenManager *calendar.TokenManager

    if cfg.Calendar.Enabled {
        tokenManager = calendar.NewTokenManager(
            calendar.OAuthConfig{
                ClientID:     expandEnvVar(cfg.Calendar.ClientID),
                ClientSecret: expandEnvVar(cfg.Calendar.ClientSecret),
            },
            cfg.DataDir,
        )

        // Try to load existing token
        if err := tokenManager.LoadToken(); err != nil {
            logger.Warn("no calendar token found, run 'meept calendar auth'")
        } else {
            token, err := tokenManager.GetAccessToken(ctx)
            if err != nil {
                logger.Error("failed to get calendar token", "error", err)
            } else {
                calendarClient, err = calendar.NewClient(
                    calendar.ClientConfig{
                        AccessToken: token,
                        CalendarID:  cfg.Calendar.CalendarID,
                    },
                    logger,
                )
                if err != nil {
                    logger.Error("failed to create calendar client", "error", err)
                }
            }
        }

        // Register calendar tools
        if calendarClient != nil {
            toolRegistry.Register(builtin.NewCalendarListTool(calendarClient))
            toolRegistry.Register(builtin.NewCalendarCreateTool(calendarClient))
            toolRegistry.Register(builtin.NewCalendarQuickAddTool(calendarClient))
            toolRegistry.Register(builtin.NewCalendarTodayTool(calendarClient))
        }
    }

    c.calendarClient = calendarClient
    c.tokenManager = tokenManager
    // ...
}
```

### Phase 5: CLI Auth Command

**File:** `cmd/meept/calendar.go` (new)

Add calendar CLI commands:

```go
package main

import (
    "fmt"
    "net/http"

    "github.com/spf13/cobra"
)

var calendarCmd = &cobra.Command{
    Use:   "calendar",
    Short: "Calendar commands",
}

var calendarAuthCmd = &cobra.Command{
    Use:   "auth",
    Short: "Authenticate with Google Calendar",
    RunE: func(cmd *cobra.Command, args []string) error {
        // Load config
        cfg, err := loadConfig()
        if err != nil {
            return err
        }

        tokenManager := calendar.NewTokenManager(
            calendar.OAuthConfig{
                ClientID:     cfg.Calendar.ClientID,
                ClientSecret: cfg.Calendar.ClientSecret,
                RedirectURI:  "http://localhost:8888/callback",
            },
            cfg.DataDir,
        )

        // Generate auth URL
        state := generateRandomState()
        authURL := tokenManager.GetAuthURL(state)

        fmt.Printf("Open this URL in your browser:\n\n%s\n\n", authURL)
        fmt.Println("Waiting for authorization...")

        // Start local server to receive callback
        codeCh := make(chan string)

        http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
            code := r.URL.Query().Get("code")
            codeCh <- code
            fmt.Fprintln(w, "Authorization successful! You can close this window.")
        })

        go http.ListenAndServe(":8888", nil)

        code := <-codeCh

        // Exchange code for token
        if err := tokenManager.ExchangeCode(cmd.Context(), code); err != nil {
            return fmt.Errorf("failed to exchange code: %w", err)
        }

        fmt.Println("Calendar authentication successful!")
        return nil
    },
}

var calendarTodayCmd = &cobra.Command{
    Use:   "today",
    Short: "Show today's events",
    RunE: func(cmd *cobra.Command, args []string) error {
        client, err := rpc.NewClient(socketPath)
        if err != nil {
            return err
        }
        defer client.Close()

        result, err := client.Call("tools.call", map[string]any{
            "tool": "calendar_today",
            "args": map[string]any{},
        })
        if err != nil {
            return err
        }

        fmt.Println(result)
        return nil
    },
}

func init() {
    calendarCmd.AddCommand(calendarAuthCmd)
    calendarCmd.AddCommand(calendarTodayCmd)
    rootCmd.AddCommand(calendarCmd)
}
```

### Phase 6: Reminder Integration

**File:** `internal/calendar/reminder.go` (new)

Integrate with scheduler for reminders:

```go
package calendar

import (
    "context"
    "log/slog"
    "time"

    "github.com/caimlas/meept/internal/bus"
)

// ReminderWatcher watches for upcoming events and triggers reminders.
type ReminderWatcher struct {
    client   *Client
    bus      *bus.MessageBus
    logger   *slog.Logger
    interval time.Duration
    stopCh   chan struct{}
}

// NewReminderWatcher creates a new reminder watcher.
func NewReminderWatcher(client *Client, msgBus *bus.MessageBus, logger *slog.Logger) *ReminderWatcher {
    return &ReminderWatcher{
        client:   client,
        bus:      msgBus,
        logger:   logger,
        interval: 5 * time.Minute,
        stopCh:   make(chan struct{}),
    }
}

// Start starts watching for reminders.
func (w *ReminderWatcher) Start(ctx context.Context) {
    ticker := time.NewTicker(w.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-w.stopCh:
            return
        case <-ticker.C:
            w.checkUpcoming(ctx)
        }
    }
}

// Stop stops the watcher.
func (w *ReminderWatcher) Stop() {
    close(w.stopCh)
}

func (w *ReminderWatcher) checkUpcoming(ctx context.Context) {
    // Get events in next 15 minutes
    events, err := w.client.GetUpcoming(ctx, 15*time.Minute, 10)
    if err != nil {
        w.logger.Error("failed to check upcoming events", "error", err)
        return
    }

    now := time.Now()

    for _, event := range events {
        start, err := event.Start.Time()
        if err != nil {
            continue
        }

        // Check if event is about to start (within 5-10 minutes)
        until := start.Sub(now)
        if until > 5*time.Minute && until <= 10*time.Minute {
            w.triggerReminder(event, until)
        }
    }
}

func (w *ReminderWatcher) triggerReminder(event Event, until time.Duration) {
    w.logger.Info("triggering reminder",
        "event", event.Summary,
        "starts_in", until)

    if w.bus != nil {
        w.bus.Publish(bus.Message{
            Topic: "calendar.reminder",
            Data: map[string]any{
                "event_id":  event.ID,
                "summary":   event.Summary,
                "starts_in": until.String(),
            },
        })
    }
}
```

---

## Testing Plan

### Unit Tests

1. **Client tests** - API calls
2. **OAuth tests** - Token management
3. **Tool tests** - Calendar tools

### Integration Tests

1. Test OAuth flow
2. Test calendar tools with mock
3. Test reminder watcher

### Manual Testing

1. Run `./bin/meept calendar auth`
2. Complete OAuth flow in browser
3. Run `./bin/meept calendar today`
4. Ask agent to create an event
5. Verify reminders trigger

---

## Prerequisites

1. **Google Cloud Project** with Calendar API enabled
2. **OAuth 2.0 credentials** (Desktop app type)
3. **Consent screen** configured

---

## Files to Modify

| File | Changes |
|------|---------|
| `internal/daemon/components.go` | Initialize calendar, register tools |
| `internal/config/schema.go` | Add calendar config |
| `config/meept.toml` | Add calendar section |
| `cmd/meept/main.go` | Add calendar subcommand |

## Files to Create

| File | Purpose |
|------|---------|
| `internal/calendar/oauth.go` | Token management |
| `internal/calendar/reminder.go` | Reminder watcher |
| `internal/tools/builtin/calendar.go` | Calendar tools |
| `cmd/meept/calendar.go` | CLI commands |
| `tests/integration/calendar_test.go` | Integration tests |

---

## Success Criteria

1. OAuth flow works
2. Token persists across restarts
3. Calendar tools work for agent
4. CLI commands work
5. Reminders trigger correctly
6. Tests pass
